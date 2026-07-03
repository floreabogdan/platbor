package httpapi_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/core/repository"
	"github.com/platbor/platbor/internal/httpapi"
	"github.com/platbor/platbor/internal/registry"
	"github.com/platbor/platbor/internal/registry/npm"
)

// TestRetentionKeepLast seeds an npm repository with five versions of a package,
// gives it keep-last-2, and checks a dry run reports three prunable while a real
// run deletes exactly three (the oldest).
func TestRetentionKeepLast(t *testing.T) {
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
	proj, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "lib", Name: "Lib", Actor: "admin"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	// An npm repository with a keep-last-2 policy.
	repo, err := repository.NewService(sqlDB).Create(ctx, repository.CreateInput{
		ProjectID: proj.ID, Key: "npm-local", Name: "npm-local",
		Format: repository.FormatNPM, Mode: repository.ModeLocal,
		Retention: repository.Retention{KeepLast: 2}, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}

	// Seed a package with five versions, oldest to newest by created_at.
	q := db.New(sqlDB)
	pkgID, err := q.UpsertNpmPackage(ctx, db.UpsertNpmPackageParams{
		ID: "pkg1", RepositoryID: repo.ID, Name: "widget", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("UpsertNpmPackage: %v", err)
	}
	for i := 1; i <= 5; i++ {
		ts := fmt.Sprintf("2026-01-0%dT00:00:00Z", i)
		if err := q.InsertNpmVersion(ctx, db.InsertNpmVersionParams{
			ID: fmt.Sprintf("v%d", i), PackageID: pkgID, Version: fmt.Sprintf("1.0.%d", i),
			Manifest: []byte("{}"), TarballDigest: fmt.Sprintf("sha256:%064d", i), TarballSize: 10, Shasum: "x", CreatedAt: ts,
		}); err != nil {
			t.Fatalf("InsertNpmVersion: %v", err)
		}
	}

	svc := httpapi.NewRetentionService(sqlDB, map[repository.Format]registry.Pruner{
		repository.FormatNPM: npm.NewPruner(sqlDB),
	})

	// Dry run reports three prunable, deletes nothing.
	rep, err := svc.Run(ctx, true, "admin")
	if err != nil {
		t.Fatalf("dry Run: %v", err)
	}
	if rep.Deleted != 3 {
		t.Errorf("dry run deleted = %d, want 3", rep.Deleted)
	}
	if n := countVersions(t, ctx, q, repo.ID); n != 5 {
		t.Errorf("after dry run %d versions remain, want 5", n)
	}

	// Real run deletes the three oldest, leaving the two newest.
	rep, err = svc.Run(ctx, false, "admin")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Deleted != 3 {
		t.Errorf("run deleted = %d, want 3", rep.Deleted)
	}
	rows, _ := q.ListNpmVersionsForRetention(ctx, repo.ID)
	if len(rows) != 2 {
		t.Fatalf("after prune %d versions remain, want 2", len(rows))
	}
	// The two newest (1.0.5, 1.0.4) survive.
	if rows[0].Version != "1.0.5" || rows[1].Version != "1.0.4" {
		t.Errorf("survivors = %s, %s; want 1.0.5, 1.0.4", rows[0].Version, rows[1].Version)
	}

	// A second run is a no-op.
	rep, _ = svc.Run(ctx, false, "admin")
	if rep.Deleted != 0 {
		t.Errorf("second run deleted = %d, want 0", rep.Deleted)
	}
}

func countVersions(t *testing.T, ctx context.Context, q *db.Queries, repositoryID string) int {
	t.Helper()
	rows, err := q.ListNpmVersionsForRetention(ctx, repositoryID)
	if err != nil {
		t.Fatalf("ListNpmVersionsForRetention: %v", err)
	}
	return len(rows)
}
