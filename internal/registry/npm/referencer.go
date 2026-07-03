package npm

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/platbor/platbor/internal/core/db"
)

// Referencer reports the blob digests npm tarballs still need, so garbage
// collection marks them alongside OCI manifest blobs. It implements
// registry.BlobReferencer; the collector unions it into the sweep mark set.
type Referencer struct {
	q *db.Queries
}

// NewReferencer builds an npm blob referencer over the metadata database.
func NewReferencer(sqlDB *sql.DB) *Referencer {
	return &Referencer{q: db.New(sqlDB)}
}

// ReferencedBlobs returns the set of tarball digests every published npm version
// points at, across all projects (blobs are a global CAS).
func (r *Referencer) ReferencedBlobs(ctx context.Context) (map[string]struct{}, error) {
	digests, err := r.q.ListNpmTarballDigests(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing npm tarball digests: %w", err)
	}
	refs := make(map[string]struct{}, len(digests))
	for _, d := range digests {
		refs[d] = struct{}{}
	}
	return refs, nil
}
