package pypi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
)

// Browser is the read side of the PyPI registry for the UI. It never mutates.
type Browser struct {
	q *db.Queries
}

// NewBrowser wires a read-only PyPI browser to the metadata store.
func NewBrowser(sqlDB *sql.DB) *Browser { return &Browser{q: db.New(sqlDB)} }

// PackageSummary is one PyPI package in the browser's project-grouped index.
type PackageSummary struct {
	ProjectKey  string
	ProjectName string
	RepoKey     string
	Name        string
	FileCount   int
	SizeBytes   int64
	IsProxy     bool
	UpdatedAt   time.Time
}

// FileSummary is one distribution file for a package's detail page.
type FileSummary struct {
	Filename       string
	Version        string
	SizeBytes      int64
	SHA256         string
	RequiresPython string
}

// PackageDetail is a package with its distribution files.
type PackageDetail struct {
	Name  string
	Files []FileSummary
}

// ErrPackageNotFound is returned when a package has no files.
var ErrPackageNotFound = errors.New("package not found")

// Packages returns every PyPI package across all projects, project-grouped.
func (b *Browser) Packages(ctx context.Context) ([]PackageSummary, error) {
	rows, err := b.q.ListAllPypiPackages(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing pypi packages: %w", err)
	}
	out := make([]PackageSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, PackageSummary{
			ProjectKey:  r.ProjectKey,
			ProjectName: r.ProjectName,
			RepoKey:     r.RepoKey,
			Name:        r.PackageName,
			FileCount:   int(r.FileCount),
			SizeBytes:   r.SizeBytes,
			IsProxy:     r.IsProxy != 0,
			UpdatedAt:   parseBrowseTime(r.UpdatedAt),
		})
	}
	return out, nil
}

// Package returns one package's distribution files in a repository, or
// ErrPackageNotFound.
func (b *Browser) Package(ctx context.Context, projectKey, repoKey, name string) (PackageDetail, error) {
	proj, err := b.q.GetProjectByKey(ctx, projectKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PackageDetail{}, ErrPackageNotFound
		}
		return PackageDetail{}, fmt.Errorf("resolving project: %w", err)
	}
	repo, err := b.q.GetRepository(ctx, db.GetRepositoryParams{ProjectID: proj.ID, Key: repoKey})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return PackageDetail{}, ErrPackageNotFound
		}
		return PackageDetail{}, fmt.Errorf("resolving repository: %w", err)
	}
	rows, err := b.q.ListPypiFiles(ctx, db.ListPypiFilesParams{RepositoryID: repo.ID, Name: normalizeName(name)})
	if err != nil {
		return PackageDetail{}, fmt.Errorf("listing files: %w", err)
	}
	if len(rows) == 0 {
		return PackageDetail{}, ErrPackageNotFound
	}
	files := make([]FileSummary, 0, len(rows))
	for _, r := range rows {
		files = append(files, FileSummary{
			Filename: r.Filename, Version: r.Version, SizeBytes: r.Size,
			SHA256: r.Sha256, RequiresPython: r.RequiresPython,
		})
	}
	return PackageDetail{Name: name, Files: files}, nil
}

func parseBrowseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
