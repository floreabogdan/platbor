// Package registry defines the format-adapter boundary. Each artifact format
// (oci, npm, nuget, generic) implements Adapter and mounts its own protocol
// routes; adapters never import one another, and adding a format never touches
// core. See docs/ARCHITECTURE.md.
package registry

import (
	"log/slog"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/blob"
)

// Deps is exactly what an adapter needs from the core: blob storage,
// authentication, and a logger. It is deliberately small (interface
// segregation) and grows only when an adapter proves a need.
type Deps struct {
	Blobs blob.Store
	Auth  *auth.Service
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
