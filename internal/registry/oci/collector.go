package oci

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// Collector runs mark-and-sweep garbage collection over the blob store: it marks
// the blobs every stored manifest references, then sweeps the rest (subject to a
// grace window). It is instance-wide, since blobs are a shared CAS.
type Collector struct {
	blobs blob.Store
	store *manifestStore
}

// NewCollector wires a collector to the blob store and metadata database.
func NewCollector(blobs blob.Store, sqlDB *sql.DB) *Collector {
	return &Collector{blobs: blobs, store: newManifestStore(sqlDB)}
}

// Collect marks referenced blobs and sweeps unreferenced ones whose last write
// is older than grace. With dryRun it only reports. A real run that frees
// anything is audited as actor.
func (c *Collector) Collect(ctx context.Context, actor string, grace time.Duration, dryRun bool, now time.Time) (blob.Report, error) {
	referenced, err := c.store.referencedBlobs(ctx)
	if err != nil {
		return blob.Report{}, err
	}
	report, err := blob.Sweep(ctx, c.blobs, referenced, now.Add(-grace), dryRun)
	if err != nil {
		return blob.Report{}, err
	}
	if !dryRun && report.Deleted > 0 {
		if err := c.audit(ctx, actor, report, now); err != nil {
			return report, err
		}
	}
	return report, nil
}

// audit records an instance-level garbage-collection event (no project scope).
func (c *Collector) audit(ctx context.Context, actor string, report blob.Report, now time.Time) error {
	meta, _ := json.Marshal(map[string]string{
		"deleted":        strconv.Itoa(report.Deleted),
		"reclaimedBytes": strconv.FormatInt(report.ReclaimedBytes, 10),
	})
	if _, err := c.store.q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{}, // instance-level: not scoped to a project
		Actor:      actorOrSystem(actor),
		Action:     "registry.gc",
		TargetType: "blobs",
		TargetID:   "",
		Metadata:   string(meta),
		CreatedAt:  now.Format(time.RFC3339Nano),
	}); err != nil {
		return fmt.Errorf("writing gc audit entry: %w", err)
	}
	return nil
}
