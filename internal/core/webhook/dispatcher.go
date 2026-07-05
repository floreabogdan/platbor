package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/platbor/platbor/internal/core/db"
)

const (
	// dispatchInterval is how often the dispatcher polls the audit log.
	dispatchInterval = 5 * time.Second
	// dispatchBatch bounds one poll so a backlog is drained in chunks.
	dispatchBatch = 100
	// deliveryTimeout bounds a single POST.
	deliveryTimeout = 10 * time.Second
)

// Dispatcher tails the audit log and delivers matching entries to a project's
// active webhooks. It keeps a persisted cursor so it resumes across restarts and
// never re-delivers. Delivery is best-effort and fire-and-forget: a failing
// endpoint is logged, the cursor still advances (at-most-once), so one bad
// subscriber never blocks the stream.
type Dispatcher struct {
	q    *db.Queries
	http *http.Client
	log  *slog.Logger
}

// NewDispatcher builds a dispatcher over the database.
func NewDispatcher(sqlDB *sql.DB, log *slog.Logger) *Dispatcher {
	return &Dispatcher{
		q:    db.New(sqlDB),
		http: &http.Client{Timeout: deliveryTimeout},
		log:  log,
	}
}

// Run polls until ctx is cancelled. It runs once immediately so the cursor is
// seeded at boot (not one interval later), closing the window in which early
// events could be missed on a fresh instance.
func (d *Dispatcher) Run(ctx context.Context) {
	d.runOnce(ctx)
	t := time.NewTicker(dispatchInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.runOnce(ctx)
		}
	}
}

// runOnce advances the cursor over any new audit entries, delivering each to the
// webhooks that match. On first run (empty cursor) it seeds to the newest entry
// so historical activity is not replayed to newly-created webhooks.
func (d *Dispatcher) runOnce(ctx context.Context) {
	cur, err := d.q.GetWebhookCursor(ctx)
	if err != nil {
		d.log.Warn("webhook cursor read", slog.String("error", err.Error()))
		return
	}
	if cur.LastCreatedAt == "" {
		if m, err := d.q.MaxAuditCursor(ctx); err == nil {
			_ = d.q.SetWebhookCursor(ctx, db.SetWebhookCursorParams{LastCreatedAt: m.CreatedAt, LastID: m.ID})
		}
		return
	}

	createdAt, lastID := cur.LastCreatedAt, cur.LastID
	for {
		rows, err := d.q.ListAuditSince(ctx, db.ListAuditSinceParams{
			CursorCreatedAt: createdAt, CursorID: lastID, RowLimit: dispatchBatch,
		})
		if err != nil {
			d.log.Warn("webhook audit poll", slog.String("error", err.Error()))
			return
		}
		if len(rows) == 0 {
			return
		}
		for _, e := range rows {
			d.dispatchEntry(ctx, e)
			createdAt, lastID = e.CreatedAt, e.ID
		}
		if err := d.q.SetWebhookCursor(ctx, db.SetWebhookCursorParams{LastCreatedAt: createdAt, LastID: lastID}); err != nil {
			d.log.Warn("webhook cursor advance", slog.String("error", err.Error()))
			return
		}
		if len(rows) < dispatchBatch || ctx.Err() != nil {
			return
		}
	}
}

// dispatchEntry delivers one audit entry to every matching active webhook of its
// project.
func (d *Dispatcher) dispatchEntry(ctx context.Context, e db.ListAuditSinceRow) {
	if !e.ProjectID.Valid {
		return
	}
	whs, err := d.q.ListActiveWebhooksForProject(ctx, e.ProjectID.String)
	if err != nil {
		d.log.Warn("listing webhooks", slog.String("error", err.Error()))
		return
	}
	for _, wh := range whs {
		if matchEvents(wh.Events, e.Action) {
			d.deliver(ctx, wh, e)
		}
	}
}

// deliveryPayload is the JSON body POSTed to a webhook.
type deliveryPayload struct {
	Delivery   string          `json:"delivery"`
	Project    string          `json:"project"`
	Actor      string          `json:"actor"`
	Action     string          `json:"action"`
	TargetType string          `json:"targetType"`
	TargetID   string          `json:"targetId"`
	Metadata   json.RawMessage `json:"metadata"`
	At         string          `json:"at"`
}

// deliver posts one signed event to a webhook.
func (d *Dispatcher) deliver(ctx context.Context, wh db.ListActiveWebhooksForProjectRow, e db.ListAuditSinceRow) {
	meta := json.RawMessage(e.Metadata)
	if !json.Valid(meta) {
		meta = json.RawMessage("{}")
	}
	body, err := json.Marshal(deliveryPayload{
		Delivery: e.ID, Project: e.ProjectKey, Actor: e.Actor, Action: e.Action,
		TargetType: e.TargetType, TargetID: e.TargetID, Metadata: meta, At: e.CreatedAt,
	})
	if err != nil {
		return
	}

	mac := hmac.New(sha256.New, []byte(wh.Secret))
	_, _ = mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	reqCtx, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, wh.Url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "platbor-webhooks/1.0")
	req.Header.Set("X-Platbor-Event", e.Action)
	req.Header.Set("X-Platbor-Delivery", e.ID)
	req.Header.Set("X-Platbor-Signature", sig)

	resp, err := d.http.Do(req)
	if err != nil {
		d.log.Warn("webhook delivery failed", slog.String("webhook", wh.ID), slog.String("error", err.Error()))
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		d.log.Warn("webhook delivery non-2xx",
			slog.String("webhook", wh.ID), slog.String("event", e.Action), slog.Int("status", resp.StatusCode))
	}
}
