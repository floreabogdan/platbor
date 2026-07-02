package blob_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/platbor/platbor/internal/core/blob"
)

func TestSweepRemovesUnreferenced(t *testing.T) {
	ctx := context.Background()
	s, err := blob.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	keep := putBlob(t, s, []byte("referenced layer"))
	drop1 := putBlob(t, s, []byte("orphan one"))
	drop2 := putBlob(t, s, []byte("orphan two"))
	referenced := map[string]struct{}{keep.Digest: {}}

	// A cutoff in the future makes every blob eligible (all are "old enough").
	future := time.Now().Add(time.Hour)

	// Dry run reports the two orphans but deletes nothing.
	dry, err := blob.Sweep(ctx, s, referenced, future, true)
	if err != nil {
		t.Fatalf("Sweep (dry): %v", err)
	}
	if dry.Scanned != 3 || dry.Deleted != 2 || dry.Kept != 1 {
		t.Fatalf("dry report = %+v, want scanned=3 deleted=2 kept=1", dry)
	}
	if dry.ReclaimedBytes != drop1.Size+drop2.Size {
		t.Errorf("dry reclaimed = %d, want %d", dry.ReclaimedBytes, drop1.Size+drop2.Size)
	}
	if _, err := s.Stat(ctx, drop1.Digest); err != nil {
		t.Errorf("dry run must not delete: %v", err)
	}

	// The real run removes the orphans and keeps the referenced blob.
	got, err := blob.Sweep(ctx, s, referenced, future, false)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if got.Deleted != 2 || got.Kept != 1 {
		t.Fatalf("report = %+v, want deleted=2 kept=1", got)
	}
	if _, err := s.Stat(ctx, keep.Digest); err != nil {
		t.Errorf("referenced blob was removed: %v", err)
	}
	for _, d := range []string{drop1.Digest, drop2.Digest} {
		if _, err := s.Stat(ctx, d); !errors.Is(err, blob.ErrNotFound) {
			t.Errorf("orphan %s not removed: %v", d, err)
		}
	}
}

func TestSweepSparesBlobsWithinGraceWindow(t *testing.T) {
	ctx := context.Background()
	s, err := blob.NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	orphan := putBlob(t, s, []byte("freshly uploaded orphan"))

	// A cutoff in the past means the just-written blob is inside the grace window
	// and must be spared even though nothing references it.
	past := time.Now().Add(-time.Hour)
	report, err := blob.Sweep(ctx, s, map[string]struct{}{}, past, false)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if report.Deleted != 0 || report.Kept != 1 {
		t.Fatalf("report = %+v, want deleted=0 kept=1", report)
	}
	if _, err := s.Stat(ctx, orphan.Digest); err != nil {
		t.Errorf("grace-protected blob was removed: %v", err)
	}
}
