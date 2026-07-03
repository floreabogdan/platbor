package rubygems

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// Pruner applies retention to gems: keep the newest keepLast versions of each gem
// and delete older ones. RubyGems has no untagged concept, so deleteUntagged is
// ignored. Pruned blobs are reclaimed by a later GC sweep.
type Pruner struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

// NewPruner wires a RubyGems pruner to the metadata database.
func NewPruner(sqlDB *sql.DB) *Pruner {
	return &Pruner{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

// Prune implements registry.Pruner: keep the newest keepLast versions of each
// gem, delete the rest.
func (p *Pruner) Prune(ctx context.Context, repositoryID string, keepLast int, _ bool, dryRun bool, actor string) (int, error) {
	if keepLast <= 0 {
		return 0, nil
	}
	repo, err := p.q.GetRepositoryByID(ctx, repositoryID)
	if err != nil {
		return 0, err
	}
	rows, err := p.q.ListGemVersionsForRetention(ctx, repositoryID)
	if err != nil {
		return 0, err
	}

	seen := map[string]int{}
	deleted := 0
	for _, r := range rows {
		seen[r.Name]++
		if seen[r.Name] <= keepLast {
			continue
		}
		deleted++
		if dryRun {
			continue
		}
		if err := p.deleteVersion(ctx, repositoryID, repo.ProjectID, r.Name, r.FullName, actor); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func (p *Pruner) deleteVersion(ctx context.Context, repositoryID, projectID, name, fullName, actor string) error {
	ts := p.now().Format(time.RFC3339Nano)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := p.q.WithTx(tx)

	if _, err := qtx.DeleteGemVersion(ctx, db.DeleteGemVersionParams{RepositoryID: repositoryID, Name: name, FullName: fullName}); err != nil {
		return fmt.Errorf("deleting version: %w", err)
	}
	if _, err := qtx.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		Actor:      actorOrSystem(actor),
		Action:     "rubygems.retention.prune",
		TargetType: "gem",
		TargetID:   fullName,
		Metadata:   "{}",
		CreatedAt:  ts,
	}); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}
	return tx.Commit()
}
