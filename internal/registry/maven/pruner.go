package maven

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// Pruner applies retention to Maven artifacts: keep the newest keepLast versions
// of each artifact and delete the files of older versions. Maven has no untagged
// concept, so deleteUntagged is ignored; maven-metadata.xml files are left in
// place (they are mutable and cheap, and clients regenerate them).
type Pruner struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

// NewPruner wires a Maven pruner to the metadata database.
func NewPruner(sqlDB *sql.DB) *Pruner {
	return &Pruner{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

// Prune implements registry.Pruner. An artifact's files span several versions (a
// pom plus a jar plus checksums per version); retention counts distinct versions
// newest-first, and every file of a version beyond keepLast is removed.
func (p *Pruner) Prune(ctx context.Context, repositoryID string, keepLast int, _ bool, dryRun bool, actor string) (int, error) {
	if keepLast <= 0 {
		return 0, nil
	}
	repo, err := p.q.GetRepositoryByID(ctx, repositoryID)
	if err != nil {
		return 0, err
	}
	rows, err := p.q.ListMavenFilesForRetention(ctx, repositoryID)
	if err != nil {
		return 0, err
	}

	seen := map[string]map[string]int{} // artifact -> version -> rank
	rank := map[string]int{}            // artifact -> next rank
	deleted := 0
	for _, r := range rows {
		artifact := r.GroupID + ":" + r.ArtifactID
		versions := seen[artifact]
		if versions == nil {
			versions = map[string]int{}
			seen[artifact] = versions
		}
		vr, ok := versions[r.Version]
		if !ok {
			rank[artifact]++
			vr = rank[artifact]
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

// deleteFile removes one file and audits it, transactionally.
func (p *Pruner) deleteFile(ctx context.Context, repositoryID, projectID, path, actor string) error {
	ts := p.now().Format(time.RFC3339Nano)
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := p.q.WithTx(tx)

	if _, err := qtx.DeleteMavenFile(ctx, db.DeleteMavenFileParams{RepositoryID: repositoryID, Path: path}); err != nil {
		return fmt.Errorf("deleting file: %w", err)
	}
	if _, err := qtx.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		Actor:      actorOrSystem(actor),
		Action:     "maven.retention.prune",
		TargetType: "file",
		TargetID:   path,
		Metadata:   "{}",
		CreatedAt:  ts,
	}); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}
	return tx.Commit()
}
