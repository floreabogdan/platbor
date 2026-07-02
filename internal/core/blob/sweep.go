package blob

import (
	"context"
	"fmt"
	"time"
)

// Report summarizes a garbage-collection sweep.
type Report struct {
	// Scanned is how many committed blobs were examined.
	Scanned int
	// Deleted is how many blobs were removed (or, in a dry run, would be).
	Deleted int
	// ReclaimedBytes is the total size of the deleted blobs.
	ReclaimedBytes int64
	// Kept is how many blobs were retained — referenced, or newer than the
	// grace cutoff.
	Kept int
}

// Sweep is the "sweep" half of mark-and-sweep garbage collection: it removes
// every committed blob that is not in referenced and whose last modification is
// before cutoff. The cutoff is a grace window — it spares blobs that were just
// uploaded but whose manifest has not been pushed yet, avoiding a race where GC
// deletes a layer a client is about to reference.
//
// referenced is the caller's responsibility to compute completely: it must be
// the union of blob digests needed across every format, or Sweep will delete
// live content. With dryRun set, Sweep reports what it would remove without
// deleting anything.
func Sweep(ctx context.Context, store Store, referenced map[string]struct{}, cutoff time.Time, dryRun bool) (Report, error) {
	var report Report
	err := store.Walk(ctx, func(info Info) error {
		report.Scanned++

		if _, ok := referenced[info.Digest]; ok {
			report.Kept++
			return nil
		}
		if !info.ModTime.Before(cutoff) {
			report.Kept++ // within the grace window
			return nil
		}

		report.Deleted++
		report.ReclaimedBytes += info.Size
		if dryRun {
			return nil
		}
		if err := store.Delete(ctx, info.Digest); err != nil {
			return fmt.Errorf("deleting %s: %w", info.Digest, err)
		}
		return nil
	})
	if err != nil {
		return Report{}, err
	}
	return report, nil
}
