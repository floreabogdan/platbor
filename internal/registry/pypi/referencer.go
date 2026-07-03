package pypi

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/platbor/platbor/internal/core/db"
)

// Referencer reports the blob digests PyPI distribution files still need, so
// garbage collection marks them alongside other formats' blobs. It implements
// registry.BlobReferencer; the collector unions it into the sweep mark set.
type Referencer struct {
	q *db.Queries
}

// NewReferencer builds a PyPI blob referencer over the metadata database.
func NewReferencer(sqlDB *sql.DB) *Referencer {
	return &Referencer{q: db.New(sqlDB)}
}

// ReferencedBlobs returns the set of distribution digests every PyPI file points
// at, across all projects (blobs are a global CAS). Uncached proxy rows have no
// blob and are naturally excluded.
func (r *Referencer) ReferencedBlobs(ctx context.Context) (map[string]struct{}, error) {
	digests, err := r.q.ListPypiBlobDigests(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing pypi blob digests: %w", err)
	}
	refs := make(map[string]struct{}, len(digests))
	for _, d := range digests {
		refs[d] = struct{}{}
	}
	return refs, nil
}
