package auth_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
)

func newService(t *testing.T) *auth.Service {
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
	return auth.NewService(sqlDB)
}

func TestBootstrapCreatesAdminOnceAndGeneratesPassword(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	res, err := svc.Bootstrap(ctx, "", "")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if !res.Created || res.Username != "admin" || res.GeneratedPassword == "" {
		t.Fatalf("unexpected bootstrap result: %+v", res)
	}

	// Second run is a no-op.
	again, err := svc.Bootstrap(ctx, "", "")
	if err != nil {
		t.Fatalf("second Bootstrap: %v", err)
	}
	if again.Created {
		t.Error("second Bootstrap should not create another admin")
	}

	// The generated password authenticates.
	user, err := svc.Authenticate(ctx, "admin", res.GeneratedPassword)
	if err != nil {
		t.Fatalf("Authenticate with generated password: %v", err)
	}
	if !user.IsAdmin {
		t.Error("bootstrapped user should be an instance admin")
	}
}

func TestAuthenticateRejectsBadCredentials(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	if _, err := svc.Bootstrap(ctx, "admin", "correct-horse"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	if _, err := svc.Authenticate(ctx, "admin", "wrong"); err != auth.ErrInvalidCredentials {
		t.Errorf("wrong password: got %v, want ErrInvalidCredentials", err)
	}
	if _, err := svc.Authenticate(ctx, "ghost", "whatever"); err != auth.ErrInvalidCredentials {
		t.Errorf("unknown user: got %v, want ErrInvalidCredentials", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()
	if _, err := svc.Bootstrap(ctx, "admin", "correct-horse"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	user, err := svc.Authenticate(ctx, "admin", "correct-horse")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	token, _, err := svc.StartSession(ctx, user.ID)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	got, err := svc.ResolveSession(ctx, token)
	if err != nil {
		t.Fatalf("ResolveSession: %v", err)
	}
	if got.ID != user.ID {
		t.Errorf("resolved user %q, want %q", got.ID, user.ID)
	}

	if err := svc.EndSession(ctx, token); err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	if _, err := svc.ResolveSession(ctx, token); err != auth.ErrNoSession {
		t.Errorf("after EndSession: got %v, want ErrNoSession", err)
	}
}

func TestResolveUnknownTokenIsNoSession(t *testing.T) {
	svc := newService(t)
	if _, err := svc.ResolveSession(context.Background(), "not-a-real-token"); err != auth.ErrNoSession {
		t.Errorf("got %v, want ErrNoSession", err)
	}
}
