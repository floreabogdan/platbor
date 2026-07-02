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

	raw, _, err := svc.CreateToken(ctx, user.ID, "short-lived", time.Hour)
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
