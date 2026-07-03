package maven

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
)

// Browser is the read side of the Maven registry for the UI. It never mutates.
type Browser struct {
	q *db.Queries
}

// NewBrowser wires a read-only Maven browser to the metadata store.
func NewBrowser(sqlDB *sql.DB) *Browser { return &Browser{q: db.New(sqlDB)} }

// ArtifactSummary is one Maven artifact (group:artifact) in the browser's
// project-grouped index.
type ArtifactSummary struct {
	ProjectKey   string
	ProjectName  string
	RepoKey      string
	GroupID      string
	ArtifactID   string
	VersionCount int
	SizeBytes    int64
	IsProxy      bool
	UpdatedAt    time.Time
}

// FileSummary is one Maven file for an artifact's detail page.
type FileSummary struct {
	Path       string
	Version    string
	Filename   string
	IsMetadata bool
	SizeBytes  int64
	SHA1       string
}

// ArtifactDetail is one artifact's files.
type ArtifactDetail struct {
	GroupID    string
	ArtifactID string
	Files      []FileSummary
}

// ErrArtifactNotFound is returned when an artifact has no files.
var ErrArtifactNotFound = errors.New("artifact not found")

// Artifacts returns every Maven artifact across all projects, project-grouped.
func (b *Browser) Artifacts(ctx context.Context) ([]ArtifactSummary, error) {
	rows, err := b.q.ListAllMavenArtifacts(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing maven artifacts: %w", err)
	}
	out := make([]ArtifactSummary, 0, len(rows))
	for _, r := range rows {
		out = append(out, ArtifactSummary{
			ProjectKey:   r.ProjectKey,
			ProjectName:  r.ProjectName,
			RepoKey:      r.RepoKey,
			GroupID:      r.GroupID,
			ArtifactID:   r.ArtifactID,
			VersionCount: int(r.VersionCount),
			SizeBytes:    r.SizeBytes,
			IsProxy:      r.IsProxy != 0,
			UpdatedAt:    parseBrowseTime(r.UpdatedAt),
		})
	}
	return out, nil
}

// Artifact returns one artifact's files in a repository, or ErrArtifactNotFound.
// The artifact is addressed as "<groupId>:<artifactId>".
func (b *Browser) Artifact(ctx context.Context, projectKey, repoKey, groupID, artifactID string) (ArtifactDetail, error) {
	proj, err := b.q.GetProjectByKey(ctx, projectKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ArtifactDetail{}, ErrArtifactNotFound
		}
		return ArtifactDetail{}, fmt.Errorf("resolving project: %w", err)
	}
	repo, err := b.q.GetRepository(ctx, db.GetRepositoryParams{ProjectID: proj.ID, Key: repoKey})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ArtifactDetail{}, ErrArtifactNotFound
		}
		return ArtifactDetail{}, fmt.Errorf("resolving repository: %w", err)
	}
	rows, err := b.q.ListMavenFilesForArtifact(ctx, db.ListMavenFilesForArtifactParams{
		RepositoryID: repo.ID, GroupID: groupID, ArtifactID: artifactID,
	})
	if err != nil {
		return ArtifactDetail{}, fmt.Errorf("listing files: %w", err)
	}
	if len(rows) == 0 {
		return ArtifactDetail{}, ErrArtifactNotFound
	}
	files := make([]FileSummary, 0, len(rows))
	for _, r := range rows {
		files = append(files, FileSummary{
			Path: r.Path, Version: r.Version, Filename: r.Filename,
			IsMetadata: r.IsMetadata != 0, SizeBytes: r.Size, SHA1: r.Sha1,
		})
	}
	return ArtifactDetail{GroupID: groupID, ArtifactID: artifactID, Files: files}, nil
}

func parseBrowseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
