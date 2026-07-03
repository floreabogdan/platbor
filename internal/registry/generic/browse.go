package generic

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
)

// Browser is the read side of the generic registry for the UI. It never mutates.
type Browser struct {
	q *db.Queries
}

// NewBrowser wires a read-only generic browser to the metadata store.
func NewBrowser(sqlDB *sql.DB) *Browser { return &Browser{q: db.New(sqlDB)} }

// FileSummary is one generic file in the browser's project-grouped index.
type FileSummary struct {
	ProjectKey  string
	ProjectName string
	RepoKey     string
	Path        string
	SizeBytes   int64
	IsProxy     bool
	UpdatedAt   time.Time
}

// Files returns every generic file across all projects, project-grouped.
func (b *Browser) Files(ctx context.Context) ([]FileSummary, error) {
	rows, err := b.q.ListAllGenericFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing generic files: %w", err)
	}
	out := make([]FileSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, FileSummary{
			ProjectKey:  r.ProjectKey,
			ProjectName: r.ProjectName,
			RepoKey:     r.RepoKey,
			Path:        r.Path,
			SizeBytes:   r.Size,
			IsProxy:     r.IsProxy != 0,
			UpdatedAt:   parseBrowseTime(r.UpdatedAt),
		})
	}
	return out, nil
}

func parseBrowseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
