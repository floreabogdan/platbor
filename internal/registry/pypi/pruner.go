package pypi

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// Pruner applies retention to PyPI packages: keep the newest keepLast versions of
// each package and delete the files of older versions. PyPI has no untagged
// concept, so deleteUntagged is ignored.
type Pruner struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

// NewPruner wires a PyPI pruner to the metadata database.
func NewPruner(sqlDB *sql.DB) *Pruner {
	return &Pruner{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

// Prune implements registry.Pruner. A package's files can span several versions
// (an sdist plus wheels); retention counts distinct versions, and every file of a
// version beyond the newest keepLast is removed.
func (p *Pruner) Prune(ctx context.Context, repositoryID string, keepLast int, _ bool, dryRun bool, actor string) (int, error) {
	if keepLast <= 0 {
		return 0, nil
	}
	repo, err := p.q.GetRepositoryByID(ctx, repositoryID)
	if err != nil {
		return 0, err
	}
	rows, err := p.q.ListPypiFilesForRetention(ctx, repositoryID)
	if err != nil {
		return 0, err
	}

	// Rows are grouped by package, newest first. Walk each package tracking the
	// distinct versions seen in order; once a version falls beyond keepLast, its
	// files are pruned.
	seenVersions := map[string]map[string]int{} // package -> version -> rank
	rankOf := map[string]int{}                  // package -> next rank
	deleted := 0
	for _, r := range rows {
		versions := seenVersions[r.Name]
		if versions == nil {
			versions = map[string]int{}
			seenVersions[r.Name] = versions
		}
		rank, ok := versions[r.Version]
		if !ok {
			rankOf[r.Name]++
			rank = rankOf[r.Name]
			versions[r.Version] = rank
		}
		if rank <= keepLast {
			continue
		}
		deleted++
		if dryRun {
			continue
		}
		if err := p.deleteFile(ctx, repositoryID, repo.ProjectID, r.Name, r.Version, r.Filename, actor); err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

// deleteFile removes one distribution file and audits it, transactionally.
func (p *Pruner) deleteFile(ctx context.Context, repositoryID, projectID, name, version, filename, actor string) error {
	ts := p.now().Format(time.RFC3339Nano)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := p.q.WithTx(tx)

	if _, err := qtx.DeletePypiFile(ctx, db.DeletePypiFileParams{RepositoryID: repositoryID, Name: name, Filename: filename}); err != nil {
		return fmt.Errorf("deleting file: %w", err)
	}
	if _, err := qtx.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		Actor:      actorOrSystem(actor),
		Action:     "pypi.retention.prune",
		TargetType: "file",
		TargetID:   name + " " + version + " " + filename,
		Metadata:   "{}",
		CreatedAt:  ts,
	}); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}
	return tx.Commit()
}
