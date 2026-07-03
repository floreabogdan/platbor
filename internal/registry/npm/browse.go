package npm

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
)

// Browser is the read side of the npm registry: it answers the UI's browse
// queries (packages, versions) over the same project-scoped tables the protocol
// writes to. It never mutates.
type Browser struct {
	q *db.Queries
}

// NewBrowser wires a read-only npm browser to the metadata store.
func NewBrowser(sqlDB *sql.DB) *Browser { return &Browser{q: db.New(sqlDB)} }

// PackageSummary is one npm package in the browser's project-grouped index.
type PackageSummary struct {
	ProjectKey   string
	ProjectName  string
	RepoKey      string
	Name         string
	VersionCount int
	SizeBytes    int64
	IsProxy      bool
	UpdatedAt    time.Time
}

// VersionSummary is one published version, for a package's detail page.
type VersionSummary struct {
	Version     string
	SizeBytes   int64
	Shasum      string
	Integrity   string
	PublishedAt time.Time
}

// PackageDetail is a package with its versions (newest first) and dist-tags.
type PackageDetail struct {
	Name     string
	DistTags map[string]string
	Versions []VersionSummary
	Readme   string // the latest version's README markdown, when published with one
}

// ErrPackageNotFound is returned by Package when the package has no versions in
// the target repository.
var errBrowseNotFound = ErrPackageNotFound

// Packages returns every npm package across all projects, ordered by project
// then name, for the browser's grouped index.
func (b *Browser) Packages(ctx context.Context) ([]PackageSummary, error) {
	rows, err := b.q.ListAllNpmPackages(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing npm packages: %w", err)
	}
	out := make([]PackageSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, PackageSummary{
			ProjectKey:   r.ProjectKey,
			ProjectName:  r.ProjectName,
			RepoKey:      r.RepoKey,
			Name:         r.PackageName,
			VersionCount: int(r.VersionCount),
			SizeBytes:    r.SizeBytes,
			IsProxy:      r.IsProxy != 0,
			UpdatedAt:    parseBrowseTime(r.UpdatedAt),
		})
	}
	return out, nil
}

// Package returns a single package's versions (newest first) and dist-tags in a
// repository, or ErrPackageNotFound when it has no versions.
func (b *Browser) Package(ctx context.Context, projectKey, repoKey, name string) (PackageDetail, error) {
	repoID, err := b.resolveRepo(ctx, projectKey, repoKey)
	if err != nil {
		return PackageDetail{}, err
	}

	rows, err := b.q.ListNpmVersions(ctx, db.ListNpmVersionsParams{RepositoryID: repoID, Name: name})
	if err != nil {
		return PackageDetail{}, fmt.Errorf("listing versions: %w", err)
	}
	if len(rows) == 0 {
		return PackageDetail{}, errBrowseNotFound
	}
	versions := make([]VersionSummary, 0, len(rows))
	// ListNpmVersions is oldest-first; reverse into newest-first for display.
	for i := len(rows) - 1; i >= 0; i-- {
		r := rows[i]
		versions = append(versions, VersionSummary{
			Version:     r.Version,
			SizeBytes:   r.TarballSize,
			Shasum:      r.Shasum,
			Integrity:   r.Integrity,
			PublishedAt: parseBrowseTime(r.CreatedAt),
		})
	}

	// The README ships in the version manifest; use the newest version that has
	// one (proxied packages cache a minimal manifest, so this is empty for them).
	readme := readmeFromManifest(rows[len(rows)-1].Manifest)

	tagRows, err := b.q.ListNpmDistTags(ctx, db.ListNpmDistTagsParams{RepositoryID: repoID, Name: name})
	if err != nil {
		return PackageDetail{}, fmt.Errorf("listing dist-tags: %w", err)
	}
	tags := make(map[string]string, len(tagRows))
	for _, t := range tagRows {
		tags[t.Tag] = t.Version
	}

	return PackageDetail{Name: name, DistTags: tags, Versions: versions, Readme: readme}, nil
}

// readmeFromManifest extracts the "readme" field npm records in a version
// manifest. Absent or unreadable manifests yield an empty string.
func readmeFromManifest(manifest []byte) string {
	if len(manifest) == 0 {
		return ""
	}
	var doc struct {
		Readme string `json:"readme"`
	}
	if err := json.Unmarshal(manifest, &doc); err != nil {
		return ""
	}
	return doc.Readme
}

// resolveRepo maps (projectKey, repoKey) to a repository id, or ErrPackageNotFound.
func (b *Browser) resolveRepo(ctx context.Context, projectKey, repoKey string) (string, error) {
	proj, err := b.q.GetProjectByKey(ctx, projectKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errBrowseNotFound
		}
		return "", fmt.Errorf("resolving project: %w", err)
	}
	repo, err := b.q.GetRepository(ctx, db.GetRepositoryParams{ProjectID: proj.ID, Key: repoKey})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errBrowseNotFound
		}
		return "", fmt.Errorf("resolving repository: %w", err)
	}
	return repo.ID, nil
}

func parseBrowseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
