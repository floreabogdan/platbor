package oci

import (
	"context"
	"database/sql"
	"errors"
)

// Pruner applies retention to OCI repositories: keep the newest keepLast tags in
// each repository, and (when deleteUntagged is set) remove manifests no tag
// points at. Deleting a tag first can leave its manifest untagged, so the
// untagged sweep runs after and reclaims those too.
type Pruner struct {
	store *manifestStore
}

// NewPruner wires an OCI pruner to the metadata database.
func NewPruner(sqlDB *sql.DB) *Pruner { return &Pruner{store: newManifestStore(sqlDB)} }

// Prune implements registry.Pruner.
func (p *Pruner) Prune(ctx context.Context, projectID string, keepLast int, deleteUntagged, dryRun bool, actor string) (int, error) {
	deleted := 0

	if keepLast > 0 {
		tags, err := p.store.q.ListTagsForRetention(ctx, projectID)
		if err != nil {
			return deleted, err
		}
		// Tags arrive grouped by repository, newest first; keep the first
		// keepLast in each repository and delete the rest.
		perRepo := map[string]int{}
		for _, t := range tags {
			perRepo[t.Repository]++
			if perRepo[t.Repository] <= keepLast {
				continue
			}
			deleted++
			if dryRun {
				continue
			}
			if err := p.store.deleteTag(ctx, projectID, t.Repository, t.Tag, actor); err != nil && !errors.Is(err, ErrManifestNotFound) {
				return deleted, err
			}
		}
	}

	if deleteUntagged {
		untagged, err := p.store.q.ListUntaggedManifests(ctx, projectID)
		if err != nil {
			return deleted, err
		}
		for _, m := range untagged {
			deleted++
			if dryRun {
				continue
			}
			if err := p.store.deleteManifest(ctx, projectID, m.Repository, m.Digest, actor); err != nil && !errors.Is(err, ErrManifestNotFound) {
				return deleted, err
			}
		}
	}

	return deleted, nil
}
