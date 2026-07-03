package rubygems

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
)

// Browser is the read side of the RubyGems registry for the UI. It never mutates.
type Browser struct {
	q *db.Queries
}

// NewBrowser wires a read-only RubyGems browser to the metadata store.
func NewBrowser(sqlDB *sql.DB) *Browser { return &Browser{q: db.New(sqlDB)} }

// GemSummary is one gem in the browser's project-grouped index.
type GemSummary struct {
	ProjectKey   string
	ProjectName  string
	RepoKey      string
	Name         string
	VersionCount int
	SizeBytes    int64
	IsProxy      bool
	UpdatedAt    time.Time
}

// VersionSummary is one gem version for the detail page.
type VersionSummary struct {
	Number    string
	Version   string
	Platform  string
	SizeBytes int64
	Yanked    bool
	SHA256    string
}

// GemDetail is one gem's versions.
type GemDetail struct {
	Name     string
	Versions []VersionSummary
}

// ErrGemNotFoundBrowse is returned when a gem has no versions.
var ErrGemNotFoundBrowse = errors.New("gem not found")

// Gems returns every gem across all projects, project-grouped.
func (b *Browser) Gems(ctx context.Context) ([]GemSummary, error) {
	rows, err := b.q.ListAllGems(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing gems: %w", err)
	}
	out := make([]GemSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, GemSummary{
			ProjectKey:   r.ProjectKey,
			ProjectName:  r.ProjectName,
			RepoKey:      r.RepoKey,
			Name:         r.GemName,
			VersionCount: int(r.VersionCount),
			SizeBytes:    r.SizeBytes,
			IsProxy:      r.IsProxy != 0,
			UpdatedAt:    parseBrowseTime(r.UpdatedAt),
		})
	}
	return out, nil
}

// Gem returns one gem's versions in a repository, or ErrGemNotFoundBrowse.
func (b *Browser) Gem(ctx context.Context, projectKey, repoKey, name string) (GemDetail, error) {
	proj, err := b.q.GetProjectByKey(ctx, projectKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GemDetail{}, ErrGemNotFoundBrowse
		}
		return GemDetail{}, fmt.Errorf("resolving project: %w", err)
	}
	repo, err := b.q.GetRepository(ctx, db.GetRepositoryParams{ProjectID: proj.ID, Key: repoKey})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GemDetail{}, ErrGemNotFoundBrowse
		}
		return GemDetail{}, fmt.Errorf("resolving repository: %w", err)
	}
	rows, err := b.q.ListGemVersionsForGem(ctx, db.ListGemVersionsForGemParams{RepositoryID: repo.ID, Name: name})
	if err != nil {
		return GemDetail{}, fmt.Errorf("listing versions: %w", err)
	}
	if len(rows) == 0 {
		return GemDetail{}, ErrGemNotFoundBrowse
	}
	versions := make([]VersionSummary, 0, len(rows))
	for _, r := range rows {
		versions = append(versions, VersionSummary{
			Number: r.Number, Version: r.Version, Platform: r.Platform, SizeBytes: r.Size, Yanked: r.Yanked != 0, SHA256: r.Sha256,
		})
	}
	return GemDetail{Name: name, Versions: versions}, nil
}

func parseBrowseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
