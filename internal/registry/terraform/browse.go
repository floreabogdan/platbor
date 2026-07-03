package terraform

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
)

// Browser is the read side of the Terraform registry for the UI. It never mutates.
type Browser struct {
	q *db.Queries
}

// NewBrowser wires a read-only Terraform browser to the metadata store.
func NewBrowser(sqlDB *sql.DB) *Browser { return &Browser{q: db.New(sqlDB)} }

// ModuleSummary is one module in the browser's project-grouped index.
type ModuleSummary struct {
	ProjectKey   string
	ProjectName  string
	RepoKey      string
	Name         string
	Provider     string
	VersionCount int
	SizeBytes    int64
	IsProxy      bool
	UpdatedAt    time.Time
}

// VersionSummary is one module version for the detail page.
type VersionSummary struct {
	Version   string
	SizeBytes int64
}

// ModuleDetail is one module's versions.
type ModuleDetail struct {
	Name     string
	Provider string
	Versions []VersionSummary
}

// ErrModuleNotFoundBrowse is returned when a module has no versions.
var ErrModuleNotFoundBrowse = errors.New("module not found")

// Modules returns every Terraform module across all projects, project-grouped.
func (b *Browser) Modules(ctx context.Context) ([]ModuleSummary, error) {
	rows, err := b.q.ListAllTerraformModules(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing terraform modules: %w", err)
	}
	out := make([]ModuleSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, ModuleSummary{
			ProjectKey:   r.ProjectKey,
			ProjectName:  r.ProjectName,
			RepoKey:      r.RepoKey,
			Name:         r.ModuleName,
			Provider:     r.Provider,
			VersionCount: int(r.VersionCount),
			SizeBytes:    r.SizeBytes,
			IsProxy:      r.IsProxy != 0,
			UpdatedAt:    parseBrowseTime(r.UpdatedAt),
		})
	}
	return out, nil
}

// Module returns one module's versions in a repository, addressed as
// "<name>/<provider>", or ErrModuleNotFoundBrowse.
func (b *Browser) Module(ctx context.Context, projectKey, repoKey, name, provider string) (ModuleDetail, error) {
	proj, err := b.q.GetProjectByKey(ctx, projectKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ModuleDetail{}, ErrModuleNotFoundBrowse
		}
		return ModuleDetail{}, fmt.Errorf("resolving project: %w", err)
	}
	repo, err := b.q.GetRepository(ctx, db.GetRepositoryParams{ProjectID: proj.ID, Key: repoKey})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ModuleDetail{}, ErrModuleNotFoundBrowse
		}
		return ModuleDetail{}, fmt.Errorf("resolving repository: %w", err)
	}
	rows, err := b.q.ListTerraformVersionsForModule(ctx, db.ListTerraformVersionsForModuleParams{RepositoryID: repo.ID, Name: name, Provider: provider})
	if err != nil {
		return ModuleDetail{}, fmt.Errorf("listing versions: %w", err)
	}
	if len(rows) == 0 {
		return ModuleDetail{}, ErrModuleNotFoundBrowse
	}
	versions := make([]VersionSummary, 0, len(rows))
	for _, r := range rows {
		versions = append(versions, VersionSummary{Version: r.Version, SizeBytes: r.Size})
	}
	return ModuleDetail{Name: name, Provider: provider, Versions: versions}, nil
}

func parseBrowseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
