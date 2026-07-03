// Package registry defines the format-adapter boundary. Each artifact format
// (oci, npm, nuget, generic) implements Adapter and mounts its own protocol
// routes; adapters never import one another, and adding a format never touches
// core. See docs/ARCHITECTURE.md.
package registry

import (
	"context"
	"database/sql"
	"log/slog"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/blob"
)

// Deps is exactly what an adapter needs from the core: blob storage,
// authentication, the metadata database, and a logger. It is deliberately small
// (interface segregation) and grows only when an adapter proves a need.
//
// DB is the shared metadata store. An adapter that persists format-specific
// tables (OCI manifests/tags, npm dist-tags, ...) owns its own project-scoped
// queries through sqlc — core domain services stay format-agnostic.
type Deps struct {
	Blobs blob.Store
	Auth  *auth.Service
	DB    *sql.DB
	Log   *slog.Logger
}

// Adapter is one registry format. Mount registers the format's protocol routes
// on r (already scoped to the format's URL prefix).
type Adapter interface {
	// Key is the format identifier: "oci", "npm", "nuget", "generic".
	Key() string
	// Mount registers protocol routes on r using deps.
	Mount(r chi.Router, deps Deps)
}

// BlobReferencer reports the blob digests a format still needs, so garbage
// collection marks them before sweeping. Blobs are a shared CAS across every
// format, so the collector must union the referrers of all of them — a missing
// referrer means live content gets deleted. Each adapter implements this over
// its own metadata; adapters never see one another, the collector unions them.
type BlobReferencer interface {
	ReferencedBlobs(ctx context.Context) (map[string]struct{}, error)
}
