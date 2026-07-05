// Package webhook delivers a project's mutation events to subscribed HTTP
// endpoints. Events are sourced from the audit log — every mutation already
// writes an audit entry, so the audit log is the event stream and webhooks need
// no changes to any format adapter. The Service manages subscriptions; the
// Dispatcher tails the audit log and posts signed deliveries.
package webhook

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

var (
	// ErrNotFound means no webhook matched.
	ErrNotFound = errors.New("webhook not found")
	// ErrInvalid describes invalid input.
	ErrInvalid = errors.New("invalid webhook")
)

// Webhook is a project's subscription. Secret signs deliveries and is returned
// only when created.
type Webhook struct {
	ID        string
	ProjectID string
	URL       string
	Secret    string
	Events    string // comma-separated action prefixes, or "*"
	Active    bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Service manages webhook subscriptions.
type Service struct {
	q   *db.Queries
	now func() time.Time
}

// NewService wires the service to an open database.
func NewService(sqlDB *sql.DB) *Service {
	return &Service{q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

// Create adds a webhook to a project. A blank secret is generated; a blank events
// list defaults to all ("*"). The URL must be http(s).
func (s *Service) Create(ctx context.Context, projectID, rawURL, events, secret string) (Webhook, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Webhook{}, fmt.Errorf("%w: url must be an absolute http(s) URL", ErrInvalid)
	}
	events = strings.TrimSpace(events)
	if events == "" {
		events = "*"
	}
	if secret == "" {
		secret, err = randomSecret()
		if err != nil {
			return Webhook{}, err
		}
	}
	ts := s.now().Format(time.RFC3339Nano)
	row, err := s.q.CreateWebhook(ctx, db.CreateWebhookParams{
		ID: id.New("wh"), ProjectID: projectID, Url: u.String(), Secret: secret, Events: events, CreatedAt: ts, UpdatedAt: ts,
	})
	if err != nil {
		return Webhook{}, fmt.Errorf("creating webhook: %w", err)
	}
	return toDomain(row), nil
}

// List returns a project's webhooks.
func (s *Service) List(ctx context.Context, projectID string) ([]Webhook, error) {
	rows, err := s.q.ListWebhooksByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("listing webhooks: %w", err)
	}
	out := make([]Webhook, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomain(r))
	}
	return out, nil
}

// Delete removes a webhook from a project.
func (s *Service) Delete(ctx context.Context, projectID, webhookID string) error {
	n, err := s.q.DeleteWebhook(ctx, db.DeleteWebhookParams{ID: webhookID, ProjectID: projectID})
	if err != nil {
		return fmt.Errorf("deleting webhook: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func toDomain(r db.Webhook) Webhook {
	created, _ := time.Parse(time.RFC3339Nano, r.CreatedAt)
	updated, _ := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	return Webhook{
		ID: r.ID, ProjectID: r.ProjectID, URL: r.Url, Secret: r.Secret, Events: r.Events,
		Active: r.Active != 0, CreatedAt: created, UpdatedAt: updated,
	}
}

func randomSecret() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating webhook secret: %w", err)
	}
	return "whsec_" + hex.EncodeToString(buf), nil
}

// matchEvents reports whether an action matches a webhook's event filter: "*"
// matches everything, otherwise any comma-separated prefix that the action starts
// with.
func matchEvents(events, action string) bool {
	events = strings.TrimSpace(events)
	if events == "" || events == "*" {
		return true
	}
	for _, p := range strings.Split(events, ",") {
		p = strings.TrimSpace(p)
		if p != "" && strings.HasPrefix(action, p) {
			return true
		}
	}
	return false
}
