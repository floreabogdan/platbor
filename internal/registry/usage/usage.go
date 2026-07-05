// Package usage computes a project's logical storage across every artifact
// format. It lives above core so it can reach each format's size (OCI's is
// derived from manifest payloads, not a flat column), and is injected into the
// repository service to enforce per-project quotas and surfaced by the API for
// display.
package usage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/registry/oci"
)

// Computer sums a project's storage: the flat-sized formats via one SQL query,
// plus OCI's logical size computed from manifest payloads.
type Computer struct {
	q   *db.Queries
	oci *oci.Browser
}

// New builds a usage computer over the metadata database.
func New(sqlDB *sql.DB) *Computer {
	return &Computer{q: db.New(sqlDB), oci: oci.NewBrowser(sqlDB)}
}

// ProjectUsage returns the project's total logical storage in bytes.
func (c *Computer) ProjectUsage(ctx context.Context, projectID string) (int64, error) {
	flat, err := c.q.ProjectFlatStorageUsage(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("summing flat storage: %w", err)
	}
	ociBytes, err := c.oci.ProjectSize(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return flat + ociBytes, nil
}
