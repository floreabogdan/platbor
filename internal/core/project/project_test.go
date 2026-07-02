package project_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/project"
)

func newService(t *testing.T) *project.Service {
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
	return project.NewService(sqlDB)
}

func TestCreateAndGet(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	created, err := svc.Create(ctx, project.CreateInput{Key: "acme", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == "" || created.CreatedAt.IsZero() {
		t.Fatalf("expected populated project, got %+v", created)
	}

	got, err := svc.GetByKey(ctx, "acme")
	if err != nil {
		t.Fatalf("GetByKey: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("id mismatch: got %q want %q", got.ID, created.ID)
	}
}

func TestCreateDuplicateKey(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	if _, err := svc.Create(ctx, project.CreateInput{Key: "dup", Name: "First"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := svc.Create(ctx, project.CreateInput{Key: "dup", Name: "Second"})
	if !errors.Is(err, project.ErrDuplicateKey) {
		t.Fatalf("expected ErrDuplicateKey, got %v", err)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	tests := map[string]project.CreateInput{
		"empty key":       {Key: "", Name: "X"},
		"uppercase key":   {Key: "Acme", Name: "X"},
		"key with slash":  {Key: "a/b", Name: "X"},
		"key with space":  {Key: "a b", Name: "X"},
		"trailing hyphen": {Key: "acme-", Name: "X"},
		"empty name":      {Key: "acme", Name: ""},
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := svc.Create(ctx, in)
			var ve *project.ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("expected *ValidationError, got %v", err)
			}
		})
	}
}

func TestGetMissingReturnsNotFound(t *testing.T) {
	svc := newService(t)
	if _, err := svc.GetByKey(context.Background(), "nope"); !errors.Is(err, project.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestListPaginates(t *testing.T) {
	svc := newService(t)
	ctx := context.Background()

	// Insert 5 projects with keys p0..p4 (key order == insertion order).
	for i := range 5 {
		if _, err := svc.Create(ctx, project.CreateInput{Key: fmt.Sprintf("p%d", i), Name: "P"}); err != nil {
			t.Fatalf("Create p%d: %v", i, err)
		}
	}

	first, err := svc.List(ctx, "", 2)
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if len(first.Projects) != 2 || first.NextCursor == "" {
		t.Fatalf("page 1: got %d projects, cursor %q", len(first.Projects), first.NextCursor)
	}
	if first.Projects[0].Key != "p0" || first.Projects[1].Key != "p1" {
		t.Fatalf("page 1 wrong order: %q, %q", first.Projects[0].Key, first.Projects[1].Key)
	}

	second, err := svc.List(ctx, first.NextCursor, 2)
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if second.Projects[0].Key != "p2" {
		t.Fatalf("page 2 should start at p2, got %q", second.Projects[0].Key)
	}

	// Last page: 1 remaining, no further cursor.
	third, err := svc.List(ctx, second.NextCursor, 2)
	if err != nil {
		t.Fatalf("List page 3: %v", err)
	}
	if len(third.Projects) != 1 || third.NextCursor != "" {
		t.Fatalf("page 3: got %d projects, cursor %q (want 1, empty)", len(third.Projects), third.NextCursor)
	}
}
