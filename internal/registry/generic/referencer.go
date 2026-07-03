package generic

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/platbor/platbor/internal/core/db"
)

// Referencer reports the blob digests generic files still need, so garbage
// collection marks them alongside OCI manifests and npm tarballs. It implements
// registry.BlobReferencer.
type Referencer struct {
	q *db.Queries
}

// NewReferencer builds a generic blob referencer over the metadata database.
func NewReferencer(sqlDB *sql.DB) *Referencer {
	return &Referencer{q: db.New(sqlDB)}
}

// ReferencedBlobs returns the set of blob digests every stored generic file
// points at, across all projects (blobs are a global CAS).
func (r *Referencer) ReferencedBlobs(ctx context.Context) (map[string]struct{}, error) {
	digests, err := r.q.ListGenericBlobDigests(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing generic blob digests: %w", err)
	}
	refs := make(map[string]struct{}, len(digests))
	for _, d := range digests {
		refs[d] = struct{}{}
	}
	return refs, nil
}
