package blob_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/platbor/platbor/internal/core/blob"
)

// TestSweepUploadsRemovesStaleSessions verifies the fs store reclaims abandoned
// resumable-upload sessions older than the cutoff, while sparing recent ones and
// committed blobs.
func TestSweepUploadsRemovesStaleSessions(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, err := blob.NewFS(root)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	// A committed blob (must never be swept — it lives outside the staging dir).
	kept := putBlob(t, s, []byte("committed content"))

	// An abandoned session: start it, write, close (leaving the temp file).
	stale, err := s.StartUpload(ctx)
	if err != nil {
		t.Fatalf("StartUpload: %v", err)
	}
	mustWrite(t, stale, []byte("abandoned"))
	staleID := stale.ID()
	if err := stale.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Backdate the staging file so it falls before the cutoff.
	stalePath := filepath.Join(root, "uploads", staleID)
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(stalePath, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// A fresh session that must be spared (a slow but live upload).
	fresh, err := s.StartUpload(ctx)
	if err != nil {
		t.Fatalf("StartUpload: %v", err)
	}
	mustWrite(t, fresh, []byte("in progress"))
	freshID := fresh.ID()
	if err := fresh.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	sweeper, ok := blob.Store(s).(blob.UploadSweeper)
	if !ok {
		t.Fatal("fs store does not implement UploadSweeper")
	}
	removed, err := sweeper.SweepUploads(ctx, time.Now().Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("SweepUploads: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d sessions, want 1", removed)
	}

	// The stale session is gone and cannot be resumed.
	if _, err := s.ResumeUpload(ctx, staleID); err == nil {
		t.Error("stale session should be unresumable after sweep")
	}
	// The fresh session survives and can still be resumed and committed.
	resumed, err := s.ResumeUpload(ctx, freshID)
	if err != nil {
		t.Fatalf("fresh session should survive the sweep: %v", err)
	}
	if _, err := resumed.Commit(ctx, blob.DigestBytes([]byte("in progress"))); err != nil {
		t.Fatalf("committing spared session: %v", err)
	}
	// The committed blob is untouched.
	if _, err := s.Stat(ctx, kept.Digest); err != nil {
		t.Errorf("committed blob must survive the sweep: %v", err)
	}
}

// TestHealthCheckReportsReachable verifies the fs store's health probe passes for
// a live store.
func TestHealthCheckReportsReachable(t *testing.T) {
	s, err := blob.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	hc, ok := blob.Store(s).(blob.HealthChecker)
	if !ok {
		t.Fatal("fs store does not implement HealthChecker")
	}
	if err := hc.HealthCheck(context.Background()); err != nil {
		t.Errorf("HealthCheck on a live store: %v", err)
	}
}
