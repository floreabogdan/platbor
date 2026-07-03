package cargo

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// Pruner applies retention to Cargo crates: keep the newest keepLast versions of
// each crate and delete older ones. Cargo has no untagged concept, so
// deleteUntagged is ignored. Pruned blobs are reclaimed by a later GC sweep.
type Pruner struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

// NewPruner wires a Cargo pruner to the metadata database.
func NewPruner(sqlDB *sql.DB) *Pruner {
	return &Pruner{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

// Prune implements registry.Pruner: keep the newest keepLast versions of each
// crate, delete the rest.
func (p *Pruner) Prune(ctx context.Context, repositoryID string, keepLast int, _ bool, dryRun bool, actor string) (int, error) {
	if keepLast <= 0 {
		return 0, nil
	}
	repo, err := p.q.GetRepositoryByID(ctx, repositoryID)
	if err != nil {
		return 0, err
	}
	rows, err := p.q.ListCargoVersionsForRetention(ctx, repositoryID)
	if err != nil {
		return 0, err
	}

	seen := map[string]int{} // crate -> versions kept so far
	deleted := 0
	for _, r := range rows {
		seen[r.NameLower]++
		if seen[r.NameLower] <= keepLast {
			continue
		}
		deleted++
		if dryRun {
			continue
		}
		if err := p.deleteVersion(ctx, repositoryID, repo.ProjectID, r.NameLower, r.Version, actor); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func (p *Pruner) deleteVersion(ctx context.Context, repositoryID, projectID, nameLower, ver, actor string) error {
	ts := p.now().Format(time.RFC3339Nano)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := p.q.WithTx(tx)

	if _, err := qtx.DeleteCargoVersion(ctx, db.DeleteCargoVersionParams{RepositoryID: repositoryID, NameLower: nameLower, Version: ver}); err != nil {
		return fmt.Errorf("deleting version: %w", err)
	}
	if _, err := qtx.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		Actor:      actorOrSystem(actor),
		Action:     "cargo.retention.prune",
		TargetType: "crate",
		TargetID:   nameLower + "@" + ver,
		Metadata:   "{}",
		CreatedAt:  ts,
	}); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}
	return tx.Commit()
}
