package auth

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
)

// TestTokenExpiry is a white-box test: it overrides the service clock to move
// past a token's expiry, which is not possible through the public API alone.
func TestTokenExpiry(t *testing.T) {
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

	svc := NewService(sqlDB)
	base := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return base }

	if _, err := svc.Bootstrap(ctx, "admin", "pw"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	user, err := svc.Authenticate(ctx, "admin", "pw")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	raw, _, err := svc.CreateToken(ctx, user.ID, user.Username, "short-lived", time.Hour)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	// Still valid before expiry.
	if _, err := svc.AuthenticateToken(ctx, raw); err != nil {
		t.Fatalf("token should be valid before expiry: %v", err)
	}

	// Advance past expiry.
	svc.now = func() time.Time { return base.Add(2 * time.Hour) }
	if _, err := svc.AuthenticateToken(ctx, raw); err != ErrInvalidToken {
		t.Fatalf("expired token: got %v, want ErrInvalidToken", err)
	}
}

// TestTokenMutationsAreAudited is a white-box test: it reaches into the query
// layer to prove that creating and revoking a token each write an audit entry
// transactionally, so no personal-token mutation escapes the activity feed.
func TestTokenMutationsAreAudited(t *testing.T) {
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

	svc := NewService(sqlDB)
	if _, err := svc.Bootstrap(ctx, "admin", "pw"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	user, err := svc.Authenticate(ctx, "admin", "pw")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	_, tok, err := svc.CreateToken(ctx, user.ID, user.Username, "ci-deploy", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	if err := svc.DeleteToken(ctx, user.ID, user.Username, tok.ID); err != nil {
		t.Fatalf("DeleteToken: %v", err)
	}

	rows, err := svc.q.ListRecentActivity(ctx, 10)
	if err != nil {
		t.Fatalf("ListRecentActivity: %v", err)
	}
	// Newest first: revoke then create. Both name the token and the actor, and
	// carry no project (personal, instance-level events).
	byAction := map[string]db.ListRecentActivityRow{}
	for _, r := range rows {
		byAction[r.Action] = r
	}
	create, ok := byAction["token.create"]
	if !ok {
		t.Fatalf("no token.create audit entry; got %+v", rows)
	}
	if create.Actor != "admin" || create.TargetType != "token" || create.TargetID != tok.ID {
		t.Errorf("token.create entry = %+v, want actor=admin target=token/%s", create, tok.ID)
	}
	if create.ProjectKey.Valid {
		t.Errorf("token.create should be instance-level, got project %q", create.ProjectKey.String)
	}
	if create.Metadata != `{"name":"ci-deploy"}` {
		t.Errorf("token.create metadata = %q, want the token name", create.Metadata)
	}
	if _, ok := byAction["token.revoke"]; !ok {
		t.Fatalf("no token.revoke audit entry; got %+v", rows)
	}
}
