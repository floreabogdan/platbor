package nuget

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/platbor/platbor/internal/core/db"
)

// Browser is the read side of the NuGet registry for the UI. It never mutates.
type Browser struct {
	q *db.Queries
}

// NewBrowser wires a read-only NuGet browser to the metadata store.
func NewBrowser(sqlDB *sql.DB) *Browser { return &Browser{q: db.New(sqlDB)} }

// PackageSummary is one NuGet package in the browser's project-grouped index.
type PackageSummary struct {
	ProjectKey   string
	ProjectName  string
	RepoKey      string
	ID           string
	VersionCount int
	SizeBytes    int64
	IsProxy      bool
	UpdatedAt    time.Time
}

// VersionSummary is one version for a package's detail page.
type VersionSummary struct {
	Version     string
	SizeBytes   int64
	PublishedAt time.Time
}

// PackageDetail is a package with its versions, newest first.
type PackageDetail struct {
	ID       string
	Versions []VersionSummary
}

// ErrPackageNotFound is returned by Package when the package has no versions.
var errBrowseNotFound = ErrPackageNotFound

// Packages returns every NuGet package across all projects, project-grouped.
func (b *Browser) Packages(ctx context.Context) ([]PackageSummary, error) {
	rows, err := b.q.ListAllNugetPackages(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing nuget packages: %w", err)
	}
	out := make([]PackageSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, PackageSummary{
			ProjectKey:   r.ProjectKey,
			ProjectName:  r.ProjectName,
			RepoKey:      r.RepoKey,
			ID:           r.PackageID,
			VersionCount: int(r.VersionCount),
			SizeBytes:    r.SizeBytes,
			IsProxy:      r.IsProxy != 0,
			UpdatedAt:    parseBrowseTime(r.UpdatedAt),
		})
	}
	return out, nil
}

// Package returns one package's versions (newest first) in a repository, or
// ErrPackageNotFound.
func (b *Browser) Package(ctx context.Context, projectKey, repoKey, id string) (PackageDetail, error) {
	proj, err := b.q.GetProjectByKey(ctx, projectKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PackageDetail{}, errBrowseNotFound
		}
		return PackageDetail{}, fmt.Errorf("resolving project: %w", err)
	}
	repoRow, err := b.q.GetRepository(ctx, db.GetRepositoryParams{ProjectID: proj.ID, Key: repoKey})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PackageDetail{}, errBrowseNotFound
		}
		return PackageDetail{}, fmt.Errorf("resolving repository: %w", err)
	}
	rows, err := b.q.ListNugetVersions(ctx, db.ListNugetVersionsParams{RepositoryID: repoRow.ID, IDLower: strings.ToLower(id)})
	if err != nil {
		return PackageDetail{}, fmt.Errorf("listing versions: %w", err)
	}
	if len(rows) == 0 {
		return PackageDetail{}, errBrowseNotFound
	}
	versions := make([]VersionSummary, 0, len(rows))
	displayID := id
	// Newest first.
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		versions = append(versions, VersionSummary{
			Version:     r.Version,
			SizeBytes:   r.NupkgSize,
			PublishedAt: parseBrowseTime(r.CreatedAt),
		})
	}
	return PackageDetail{ID: displayID, Versions: versions}, nil
}

func parseBrowseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
