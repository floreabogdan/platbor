package goproxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
)

// Browser is the read side of the Go registry for the UI. It never mutates.
type Browser struct {
	q *db.Queries
}

// NewBrowser wires a read-only Go browser to the metadata store.
func NewBrowser(sqlDB *sql.DB) *Browser { return &Browser{q: db.New(sqlDB)} }

// ModuleSummary is one Go module in the browser's project-grouped index.
type ModuleSummary struct {
	ProjectKey   string
	ProjectName  string
	RepoKey      string
	Module       string
	VersionCount int
	SizeBytes    int64
	IsProxy      bool
	UpdatedAt    time.Time
}

// VersionSummary is one cached version for a module's detail page.
type VersionSummary struct {
	Version   string
	SizeBytes int64
	HasZip    bool
}

// ModuleDetail is one module's cached versions.
type ModuleDetail struct {
	Module   string
	Versions []VersionSummary
}

// ErrModuleNotFound is returned when a module has no cached files.
var ErrModuleNotFound = errors.New("module not found")

// Modules returns every cached Go module across all projects, project-grouped.
func (b *Browser) Modules(ctx context.Context) ([]ModuleSummary, error) {
	rows, err := b.q.ListAllGoModules(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing go modules: %w", err)
	}
	out := make([]ModuleSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, ModuleSummary{
			ProjectKey:   r.ProjectKey,
			ProjectName:  r.ProjectName,
			RepoKey:      r.RepoKey,
			Module:       r.Module,
			VersionCount: int(r.VersionCount),
			SizeBytes:    r.SizeBytes,
			IsProxy:      r.IsProxy != 0,
			UpdatedAt:    parseBrowseTime(r.UpdatedAt),
		})
	}
	return out, nil
}

// Module returns one module's cached versions in a repository, or
// ErrModuleNotFound.
func (b *Browser) Module(ctx context.Context, projectKey, repoKey, module string) (ModuleDetail, error) {
	proj, err := b.q.GetProjectByKey(ctx, projectKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ModuleDetail{}, ErrModuleNotFound
		}
		return ModuleDetail{}, fmt.Errorf("resolving project: %w", err)
	}
	repo, err := b.q.GetRepository(ctx, db.GetRepositoryParams{ProjectID: proj.ID, Key: repoKey})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ModuleDetail{}, ErrModuleNotFound
		}
		return ModuleDetail{}, fmt.Errorf("resolving repository: %w", err)
	}
	rows, err := b.q.ListGoFilesForModule(ctx, db.ListGoFilesForModuleParams{RepositoryID: repo.ID, Module: module})
	if err != nil {
		return ModuleDetail{}, fmt.Errorf("listing files: %w", err)
	}
	if len(rows) == 0 {
		return ModuleDetail{}, ErrModuleNotFound
	}
	// Roll the per-file rows (info/mod/zip) up into one entry per version.
	order := []string{}
	byVersion := map[string]*VersionSummary{}
	for _, r := range rows {
		v := byVersion[r.Version]
		if v == nil {
			v = &VersionSummary{Version: r.Version}
			byVersion[r.Version] = v
			order = append(order, r.Version)
		}
		v.SizeBytes += r.Size
		if r.Kind == "zip" {
			v.HasZip = true
		}
	}
	versions := make([]VersionSummary, 0, len(order))
	for _, ver := range order {
		versions = append(versions, *byVersion[ver])
	}
	return ModuleDetail{Module: module, Versions: versions}, nil
}

func parseBrowseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
