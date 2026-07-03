package npm

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// Pruner applies retention to npm packages: keep the newest keepLast versions of
// each package and delete the rest. npm has no untagged concept, so
// deleteUntagged is ignored.
type Pruner struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

// NewPruner wires an npm pruner to the metadata database.
func NewPruner(sqlDB *sql.DB) *Pruner {
	return &Pruner{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

// Prune implements registry.Pruner.
func (p *Pruner) Prune(ctx context.Context, repositoryID string, keepLast int, _ bool, dryRun bool, actor string) (int, error) {
	if keepLast <= 0 {
		return 0, nil
	}
	repo, err := p.q.GetRepositoryByID(ctx, repositoryID)
	if err != nil {
		return 0, err
	}
	rows, err := p.q.ListNpmVersionsForRetention(ctx, repositoryID)
	if err != nil {
		return 0, err
	}
	// Rows are grouped by package, newest first; keep the first keepLast per
	// package and delete the rest.
	perPkg := map[string]int{}
	deleted := 0
	for _, r := range rows {
		perPkg[r.Name]++
		if perPkg[r.Name] <= keepLast {
			continue
		}
		deleted++
		if dryRun {
			continue
		}
		if err := p.deleteVersion(ctx, repositoryID, repo.ProjectID, r.Name, r.Version, actor); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

// deleteVersion removes one version and audits it, transactionally.
func (p *Pruner) deleteVersion(ctx context.Context, repositoryID, projectID, name, version, actor string) error {
	ts := p.now().Format(time.RFC3339Nano)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := p.q.WithTx(tx)

	if _, err := qtx.DeleteNpmVersion(ctx, db.DeleteNpmVersionParams{RepositoryID: repositoryID, Name: name, Version: version}); err != nil {
		return fmt.Errorf("deleting version: %w", err)
	}
	if _, err := qtx.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		Actor:      actorOrSystem(actor),
		Action:     "npm.retention.prune",
		TargetType: "version",
		TargetID:   name + "@" + version,
		Metadata:   "{}",
		CreatedAt:  ts,
	}); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}
	return tx.Commit()
}
