package cargo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
)

// Browser is the read side of the Cargo registry for the UI. It never mutates.
type Browser struct {
	q *db.Queries
}

// NewBrowser wires a read-only Cargo browser to the metadata store.
func NewBrowser(sqlDB *sql.DB) *Browser { return &Browser{q: db.New(sqlDB)} }

// CrateSummary is one crate in the browser's project-grouped index.
type CrateSummary struct {
	ProjectKey   string
	ProjectName  string
	RepoKey      string
	Name         string
	VersionCount int
	SizeBytes    int64
	IsProxy      bool
	UpdatedAt    time.Time
}

// VersionSummary is one crate version for the detail page.
type VersionSummary struct {
	Version   string
	SizeBytes int64
	Yanked    bool
	Cksum     string
}

// CrateDetail is one crate's versions.
type CrateDetail struct {
	Name     string
	Versions []VersionSummary
}

// ErrCrateNotFound is returned when a crate has no versions.
var ErrCrateNotFound = errors.New("crate not found")

// Crates returns every Cargo crate across all projects, project-grouped.
func (b *Browser) Crates(ctx context.Context) ([]CrateSummary, error) {
	rows, err := b.q.ListAllCargoCrates(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing cargo crates: %w", err)
	}
	out := make([]CrateSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, CrateSummary{
			ProjectKey:   r.ProjectKey,
			ProjectName:  r.ProjectName,
			RepoKey:      r.RepoKey,
			Name:         r.CrateName,
			VersionCount: int(r.VersionCount),
			SizeBytes:    r.SizeBytes,
			IsProxy:      r.IsProxy != 0,
			UpdatedAt:    parseBrowseTime(r.UpdatedAt),
		})
	}
	return out, nil
}

// Crate returns one crate's versions in a repository, or ErrCrateNotFound.
func (b *Browser) Crate(ctx context.Context, projectKey, repoKey, name string) (CrateDetail, error) {
	proj, err := b.q.GetProjectByKey(ctx, projectKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CrateDetail{}, ErrCrateNotFound
		}
		return CrateDetail{}, fmt.Errorf("resolving project: %w", err)
	}
	repo, err := b.q.GetRepository(ctx, db.GetRepositoryParams{ProjectID: proj.ID, Key: repoKey})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return CrateDetail{}, ErrCrateNotFound
		}
		return CrateDetail{}, fmt.Errorf("resolving repository: %w", err)
	}
	rows, err := b.q.ListCargoVersionsForCrate(ctx, db.ListCargoVersionsForCrateParams{RepositoryID: repo.ID, NameLower: normalizeName(name)})
	if err != nil {
		return CrateDetail{}, fmt.Errorf("listing versions: %w", err)
	}
	if len(rows) == 0 {
		return CrateDetail{}, ErrCrateNotFound
	}
	versions := make([]VersionSummary, 0, len(rows))
	for _, r := range rows {
		versions = append(versions, VersionSummary{Version: r.Version, SizeBytes: r.Size, Yanked: r.Yanked != 0, Cksum: r.Cksum})
	}
	return CrateDetail{Name: name, Versions: versions}, nil
}

func parseBrowseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
