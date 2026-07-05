package httpapi

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
)

// TestScheduledGCDeletesUnreferencedBlob proves the maintenance scheduler
// actually fires: with a short GC interval it collects an unreferenced blob whose
// modtime predates the grace window.
func TestScheduledGCDeletesUnreferencedBlob(t *testing.T) {
	ctx := context.Background()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	sqlDB, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(ctx, sqlDB, discard()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	store, err := blob.NewFS(cfg.DataDir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	// Commit an unreferenced blob and backdate it past the GC grace window.
	content := []byte("orphan blob nobody references")
	up, err := store.StartUpload(ctx)
	if err != nil {
		t.Fatalf("StartUpload: %v", err)
	}
	if _, err := up.Write(content); err != nil {
		t.Fatalf("Write: %v", err)
	}
	desc, err := up.Commit(ctx, blob.DigestBytes(content))
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	hex := desc.Digest[len("sha256:"):]
	blobPath := filepath.Join(cfg.DataDir, "blobs", "sha256", hex[:2], hex)
	old := time.Now().Add(-2 * gcGracePeriod)
	if err := os.Chtimes(blobPath, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	s := &Server{
		log:        discard(),
		blobs:      store,
		collector:  newCollector(sqlDB, store),
		retention:  newRetention(sqlDB),
		gcInterval: 15 * time.Millisecond,
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.runScheduledMaintenance(runCtx)

	// Poll until the scheduled GC removes the orphan (generous margin).
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := store.Stat(ctx, desc.Digest); err == blob.ErrNotFound {
			return // swept — success
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduled GC did not delete the unreferenced blob within the deadline")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestScheduledMaintenanceDisabledReturns verifies the loop exits immediately
// when both intervals are zero, so a zero-config instance spawns no ticker.
func TestScheduledMaintenanceDisabledReturns(t *testing.T) {
	s := &Server{log: discard()}
	done := make(chan struct{})
	go func() {
		s.runScheduledMaintenance(context.Background())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runScheduledMaintenance should return immediately when disabled")
	}
}

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
