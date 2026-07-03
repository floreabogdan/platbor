package goproxy

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// Pruner applies retention to cached Go modules: keep the newest keepLast
// versions of each module and delete the cached files of older versions. Go has
// no untagged concept, so deleteUntagged is ignored. Pruned blobs are reclaimed
// by a later GC sweep; a proxy re-fetches anything requested again.
type Pruner struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

// NewPruner wires a Go pruner to the metadata database.
func NewPruner(sqlDB *sql.DB) *Pruner {
	return &Pruner{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

// Prune implements registry.Pruner. A module's cached files span several versions
// (info + mod + zip per version); retention counts distinct versions newest-first
// and removes every file of a version beyond keepLast.
func (p *Pruner) Prune(ctx context.Context, repositoryID string, keepLast int, _ bool, dryRun bool, actor string) (int, error) {
	if keepLast <= 0 {
		return 0, nil
	}
	repo, err := p.q.GetRepositoryByID(ctx, repositoryID)
	if err != nil {
		return 0, err
	}
	rows, err := p.q.ListGoFilesForRetention(ctx, repositoryID)
	if err != nil {
		return 0, err
	}

	seen := map[string]map[string]int{}
	rank := map[string]int{}
	deleted := 0
	for _, r := range rows {
		versions := seen[r.Module]
		if versions == nil {
			versions = map[string]int{}
			seen[r.Module] = versions
		}
		vr, ok := versions[r.Version]
		if !ok {
			rank[r.Module]++
			vr = rank[r.Module]
			versions[r.Version] = vr
		}
		if vr <= keepLast {
			continue
		}
		deleted++
		if dryRun {
			continue
		}
		if err := p.deleteFile(ctx, repositoryID, repo.ProjectID, r.Path, actor); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func (p *Pruner) deleteFile(ctx context.Context, repositoryID, projectID, path, actor string) error {
	ts := p.now().Format(time.RFC3339Nano)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := p.q.WithTx(tx)

	if _, err := qtx.DeleteGoFile(ctx, db.DeleteGoFileParams{RepositoryID: repositoryID, Path: path}); err != nil {
		return fmt.Errorf("deleting file: %w", err)
	}
	if _, err := qtx.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		Actor:      actorOrSystem(actor),
		Action:     "go.retention.prune",
		TargetType: "file",
		TargetID:   path,
		Metadata:   "{}",
		CreatedAt:  ts,
	}); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}
	return tx.Commit()
}
