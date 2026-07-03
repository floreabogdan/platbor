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

// Prune implements registry.Pruner. keepLast keeps the newest tags per image;
// deleteUntagged then removes manifests no tag points at (including any just
// untagged). Both are scoped to the repository.
func (p *Pruner) Prune(ctx context.Context, repositoryID string, keepLast int, deleteUntagged, dryRun bool, actor string) (int, error) {
	deleted := 0
	if keepLast <= 0 && !deleteUntagged {
		return 0, nil
	}
	repo, err := p.store.q.GetRepositoryByID(ctx, repositoryID)
	if err != nil {
		return 0, err
	}
	projectID := repo.ProjectID

	if keepLast > 0 {
		tags, err := p.store.q.ListTagsForRetention(ctx, repositoryID)
		if err != nil {
			return deleted, err
		}
		// Tags arrive grouped by image, newest first; keep the first keepLast per
		// image and delete the rest.
		perImage := map[string]int{}
		for _, t := range tags {
			perImage[t.Repository]++
			if perImage[t.Repository] <= keepLast {
				continue
			}
			deleted++
			if dryRun {
				continue
			}
			if err := p.store.deleteTag(ctx, repositoryID, projectID, t.Repository, t.Tag, actor); err != nil && !errors.Is(err, ErrManifestNotFound) {
				return deleted, err
			}
		}
	}

	if deleteUntagged {
		untagged, err := p.store.q.ListUntaggedManifests(ctx, repositoryID)
		if err != nil {
			return deleted, err
		}
		for _, m := range untagged {
			deleted++
			if dryRun {
				continue
			}
			if err := p.store.deleteManifest(ctx, repositoryID, projectID, m.Repository, m.Digest, actor); err != nil && !errors.Is(err, ErrManifestNotFound) {
				return deleted, err
			}
		}
	}

	return deleted, nil
}
