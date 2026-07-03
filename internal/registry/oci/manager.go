package oci

import (
	"context"
	"database/sql"
)

// Manager is the write side of the registry for the application API
// (session-authenticated): it deletes tags and manifests through the same
// audited store the /v2 protocol uses. Browser reads; Manager mutates.
type Manager struct {
	store *manifestStore
}

// NewManager wires a registry manager to the metadata store.
func NewManager(sqlDB *sql.DB) *Manager { return &Manager{store: newManifestStore(sqlDB)} }

// DeleteTag removes a single tag, leaving the manifest it referenced in place.
// Returns ErrManifestNotFound when the tag does not exist.
func (m *Manager) DeleteTag(ctx context.Context, repositoryID, projectID, image, tag, actor string) error {
	return m.store.deleteTag(ctx, repositoryID, projectID, image, tag, actor)
}

// DeleteManifest removes a manifest by digest along with every tag pointing at
// it. Returns ErrManifestNotFound when the manifest does not exist.
func (m *Manager) DeleteManifest(ctx context.Context, repositoryID, projectID, image, digest, actor string) error {
	return m.store.deleteManifest(ctx, repositoryID, projectID, image, digest, actor)
}
