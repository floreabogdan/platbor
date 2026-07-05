package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
	"github.com/platbor/platbor/internal/core/project"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	ctx := context.Background()
	sqlDB, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(ctx, sqlDB, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return sqlDB
}

// insertAudit writes an audit entry directly, standing in for whatever adapter
// mutation would have produced it.
func insertAudit(t *testing.T, sqlDB *sql.DB, projectID, action, createdAt string) {
	t.Helper()
	if _, err := db.New(sqlDB).InsertAuditEntry(context.Background(), db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		Actor:      "tester",
		Action:     action,
		TargetType: "file",
		TargetID:   "thing",
		Metadata:   `{"k":"v"}`,
		CreatedAt:  createdAt,
	}); err != nil {
		t.Fatalf("InsertAuditEntry: %v", err)
	}
}

func TestDispatcherDeliversSignedEvent(t *testing.T) {
	ctx := context.Background()
	sqlDB := testDB(t)
	proj, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "p", Name: "P", Actor: "admin"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// A receiver that records the delivery.
	var (
		mu      sync.Mutex
		gotBody []byte
		gotSig  string
		gotHits int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody, gotSig, gotHits = body, r.Header.Get("X-Platbor-Signature"), gotHits+1
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	svc := NewService(sqlDB)
	hook, err := svc.Create(ctx, proj.ID, srv.URL, "generic.", "")
	if err != nil {
		t.Fatalf("create webhook: %v", err)
	}

	d := NewDispatcher(sqlDB, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// First run seeds the cursor to the newest existing entry (the project.create
	// audit) and delivers nothing.
	d.runOnce(ctx)
	if gotHits != 0 {
		t.Fatalf("historical events must not be replayed; got %d deliveries", gotHits)
	}

	// A new matching event is delivered; a non-matching one is not.
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	later := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339Nano)
	insertAudit(t, sqlDB, proj.ID, "generic.put", future)
	insertAudit(t, sqlDB, proj.ID, "npm.publish", later) // filtered out by "generic." events
	d.runOnce(ctx)

	mu.Lock()
	defer mu.Unlock()
	if gotHits != 1 {
		t.Fatalf("expected exactly 1 delivery (generic.put), got %d", gotHits)
	}
	// The signature verifies against the delivered body and the webhook secret.
	mac := hmac.New(sha256.New, []byte(hook.Secret))
	_, _ = mac.Write(gotBody)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if gotSig != want {
		t.Errorf("signature = %q, want %q", gotSig, want)
	}
	if !containsAll(string(gotBody), `"action":"generic.put"`, `"project":"p"`, `"actor":"tester"`) {
		t.Errorf("delivery body missing fields: %s", gotBody)
	}
}

func TestDispatcherAdvancesCursorPastFailures(t *testing.T) {
	ctx := context.Background()
	sqlDB := testDB(t)
	proj, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "p", Name: "P", Actor: "admin"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	// A webhook whose endpoint always fails.
	if _, err := NewService(sqlDB).Create(ctx, proj.ID, "http://127.0.0.1:1/nope", "*", ""); err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	d := NewDispatcher(sqlDB, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.runOnce(ctx) // seed

	insertAudit(t, sqlDB, proj.ID, "generic.put", time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano))
	d.runOnce(ctx) // delivery fails, but cursor must advance

	cur, err := db.New(sqlDB).GetWebhookCursor(ctx)
	if err != nil {
		t.Fatalf("GetWebhookCursor: %v", err)
	}
	// A second pass must find nothing new (cursor advanced past the failed entry).
	rows, err := db.New(sqlDB).ListAuditSince(ctx, db.ListAuditSinceParams{CursorCreatedAt: cur.LastCreatedAt, CursorID: cur.LastID, RowLimit: 10})
	if err != nil {
		t.Fatalf("ListAuditSince: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("cursor did not advance past the failed delivery; %d entries remain", len(rows))
	}
}

func containsAll(s string, subs ...string) bool {
	for _, sub := range subs {
		found := false
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
