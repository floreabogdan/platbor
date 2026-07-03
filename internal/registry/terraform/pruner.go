package terraform

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// Pruner applies retention to Terraform modules: keep the newest keepLast
// versions of each module and delete older ones. Terraform has no untagged
// concept, so deleteUntagged is ignored. Pruned blobs are reclaimed by a later GC
// sweep.
type Pruner struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

// NewPruner wires a Terraform pruner to the metadata database.
func NewPruner(sqlDB *sql.DB) *Pruner {
	return &Pruner{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

// Prune implements registry.Pruner: keep the newest keepLast versions of each
// module (keyed by name/provider), delete the rest.
func (p *Pruner) Prune(ctx context.Context, repositoryID string, keepLast int, _ bool, dryRun bool, actor string) (int, error) {
	if keepLast <= 0 {
		return 0, nil
	}
	repo, err := p.q.GetRepositoryByID(ctx, repositoryID)
	if err != nil {
		return 0, err
	}
	rows, err := p.q.ListTerraformVersionsForRetention(ctx, repositoryID)
	if err != nil {
		return 0, err
	}

	seen := map[string]int{}
	deleted := 0
	for _, r := range rows {
		key := r.Name + "/" + r.Provider
		seen[key]++
		if seen[key] <= keepLast {
			continue
		}
		deleted++
		if dryRun {
			continue
		}
		if err := p.deleteVersion(ctx, repositoryID, repo.ProjectID, r.Name, r.Provider, r.Version, actor); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

func (p *Pruner) deleteVersion(ctx context.Context, repositoryID, projectID, name, provider, version, actor string) error {
	ts := p.now().Format(time.RFC3339Nano)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := p.q.WithTx(tx)

	if _, err := qtx.DeleteTerraformVersion(ctx, db.DeleteTerraformVersionParams{RepositoryID: repositoryID, Name: name, Provider: provider, Version: version}); err != nil {
		return fmt.Errorf("deleting version: %w", err)
	}
	if _, err := qtx.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		Actor:      actorOrSystem(actor),
		Action:     "terraform.retention.prune",
		TargetType: "module",
		TargetID:   name + "/" + provider + "@" + version,
		Metadata:   "{}",
		CreatedAt:  ts,
	}); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}
	return tx.Commit()
}
