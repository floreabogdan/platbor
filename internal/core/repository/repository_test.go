package repository_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/core/repository"
)

func setup(t *testing.T) (*repository.Service, string) {
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
	proj, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "acme", Name: "Acme", Actor: "admin"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return repository.NewService(sqlDB), proj.ID
}

func TestCreateGetListDelete(t *testing.T) {
	svc, projectID := setup(t)
	ctx := context.Background()

	repo, err := svc.Create(ctx, repository.CreateInput{
		ProjectID: projectID, Key: "docker-prod", Name: "Docker Prod",
		Format: repository.FormatOCI, Mode: repository.ModeLocal,
		Retention: repository.Retention{KeepLast: 10}, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if repo.Format != repository.FormatOCI || repo.Mode != repository.ModeLocal || repo.Retention.KeepLast != 10 {
		t.Errorf("unexpected repo: %+v", repo)
	}
	if repo.Upstream != nil {
		t.Error("local repo should have no upstream")
	}

	// Duplicate key rejected.
	if _, err := svc.Create(ctx, repository.CreateInput{
		ProjectID: projectID, Key: "docker-prod", Name: "dup", Format: repository.FormatOCI, Mode: repository.ModeLocal, Actor: "admin",
	}); !errors.Is(err, repository.ErrDuplicateKey) {
		t.Errorf("duplicate: got %v, want ErrDuplicateKey", err)
	}

	got, err := svc.Get(ctx, projectID, "docker-prod")
	if err != nil || got.Name != "Docker Prod" {
		t.Errorf("Get: %v / %+v", err, got)
	}
	if _, err := svc.Get(ctx, projectID, "missing"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("Get missing: got %v, want ErrNotFound", err)
	}

	repos, err := svc.List(ctx, projectID)
	if err != nil || len(repos) != 1 {
		t.Fatalf("List: %v / %d", err, len(repos))
	}

	if err := svc.Delete(ctx, projectID, "docker-prod", "admin"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := svc.Get(ctx, projectID, "docker-prod"); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("after delete: got %v, want ErrNotFound", err)
	}
}

func TestValidation(t *testing.T) {
	svc, projectID := setup(t)
	ctx := context.Background()
	var ve *repository.ValidationError

	// Bad key.
	if _, err := svc.Create(ctx, repository.CreateInput{ProjectID: projectID, Key: "Bad Key!", Name: "x", Format: repository.FormatNPM, Mode: repository.ModeLocal}); !errors.As(err, &ve) {
		t.Errorf("bad key: got %v, want ValidationError", err)
	}
	// Bad format.
	if _, err := svc.Create(ctx, repository.CreateInput{ProjectID: projectID, Key: "ok", Name: "x", Format: "docker", Mode: repository.ModeLocal}); !errors.As(err, &ve) {
		t.Errorf("bad format: got %v, want ValidationError", err)
	}
	// Proxy without upstream.
	if _, err := svc.Create(ctx, repository.CreateInput{ProjectID: projectID, Key: "ok", Name: "x", Format: repository.FormatNPM, Mode: repository.ModeProxy}); !errors.As(err, &ve) {
		t.Errorf("proxy no upstream: got %v, want ValidationError", err)
	}
}

func TestProxyRepository(t *testing.T) {
	svc, projectID := setup(t)
	ctx := context.Background()
	repo, err := svc.Create(ctx, repository.CreateInput{
		ProjectID: projectID, Key: "npmjs", Name: "npm.js mirror",
		Format: repository.FormatNPM, Mode: repository.ModeProxy,
		Upstream: &repository.Upstream{URL: "https://registry.npmjs.org", Username: "u", Password: "p"},
		Actor:    "admin",
	})
	if err != nil {
		t.Fatalf("Create proxy: %v", err)
	}
	if repo.Upstream == nil || repo.Upstream.URL != "https://registry.npmjs.org" {
		t.Fatalf("proxy upstream not set: %+v", repo.Upstream)
	}
	// Password is persisted (stored as given) but the HTTP layer strips it.
	if repo.Upstream.Password != "p" {
		t.Errorf("upstream password not stored")
	}
}
