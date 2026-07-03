package terraform

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/platbor/platbor/internal/core/db"
)

// Referencer reports the blob digests Terraform module archives still need, so
// garbage collection marks them alongside other formats. It implements
// registry.BlobReferencer.
type Referencer struct {
	q *db.Queries
}

// NewReferencer builds a Terraform blob referencer over the metadata database.
func NewReferencer(sqlDB *sql.DB) *Referencer {
	return &Referencer{q: db.New(sqlDB)}
}

// ReferencedBlobs returns the set of digests every stored module archive points
// at, across all projects (blobs are a global CAS).
func (r *Referencer) ReferencedBlobs(ctx context.Context) (map[string]struct{}, error) {
	digests, err := r.q.ListTerraformBlobDigests(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing terraform blob digests: %w", err)
	}
	refs := make(map[string]struct{}, len(digests))
	for _, d := range digests {
		refs[d] = struct{}{}
	}
	return refs, nil
}
