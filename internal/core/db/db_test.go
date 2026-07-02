package db_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// openTestDB opens a fresh migrated SQLite database under a temp dir.
func openTestDB(t *testing.T) *sql.DB {
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

func TestMigrateCreatesTablesAndIsIdempotent(t *testing.T) {
	sqlDB := openTestDB(t)
	ctx := context.Background()

	// Both tables from the multi-statement migration must exist.
	for _, table := range []string{"projects", "audit_log", "schema_migrations"} {
		var name string
		err := sqlDB.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("expected table %q to exist: %v", table, err)
		}
	}

	// Re-running is a no-op, not an error.
	if err := db.Migrate(ctx, sqlDB, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("second Migrate must be a no-op: %v", err)
	}
}

func TestProjectAndAuditRoundTrip(t *testing.T) {
	sqlDB := openTestDB(t)
	ctx := context.Background()
	q := db.New(sqlDB)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	proj, err := q.CreateProject(ctx, db.CreateProjectParams{
		ID:          id.New("proj"),
		Key:         "acme",
		Name:        "Acme",
		Description: "test",
		CreatedAt:   now,
		UpdatedAt:   now,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	got, err := q.GetProjectByKey(ctx, "acme")
	if err != nil {
		t.Fatalf("GetProjectByKey: %v", err)
	}
	if got.ID != proj.ID {
		t.Errorf("round-trip id mismatch: got %q want %q", got.ID, proj.ID)
	}

	// Audit entry scoped to the project.
	if _, err := q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: proj.ID, Valid: true},
		Actor:      "system",
		Action:     "project.create",
		TargetType: "project",
		TargetID:   proj.ID,
		Metadata:   "{}",
		CreatedAt:  now,
	}); err != nil {
		t.Fatalf("InsertAuditEntry: %v", err)
	}

	entries, err := q.ListAuditByProject(ctx, db.ListAuditByProjectParams{
		ProjectID: sql.NullString{String: proj.ID, Valid: true},
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("ListAuditByProject: %v", err)
	}
	if len(entries) != 1 || entries[0].Action != "project.create" {
		t.Fatalf("expected one audit entry for project.create, got %+v", entries)
	}
}

func TestUniqueKeyConflict(t *testing.T) {
	sqlDB := openTestDB(t)
	ctx := context.Background()
	q := db.New(sqlDB)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	mk := func() error {
		_, err := q.CreateProject(ctx, db.CreateProjectParams{
			ID: id.New("proj"), Key: "dup", Name: "Dup", CreatedAt: now, UpdatedAt: now,
		})
		return err
	}
	if err := mk(); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if err := mk(); err == nil {
		t.Fatal("expected a unique-constraint error on duplicate key")
	}
}
