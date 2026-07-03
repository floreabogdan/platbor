package goproxy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

var (
	errProjectNotFound = errors.New("project not found")
	// ErrFileNotFound is returned when no cached file exists at a path.
	ErrFileNotFound = errors.New("file not found")
)

// fileStore is the Go adapter's repository-scoped metadata layer.
type fileStore struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

func newFileStore(sqlDB *sql.DB) *fileStore {
	return &fileStore{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

func (s *fileStore) resolveProject(ctx context.Context, key string) (id string, allowAutoCreate bool, err error) {
	row, err := s.q.GetProjectByKey(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, errProjectNotFound
		}
		return "", false, fmt.Errorf("resolving project %q: %w", key, err)
	}
	return row.ID, row.AllowAutoCreate != 0, nil
}

// storedFile is a cached file read back for serving.
type storedFile struct {
	BlobDigest string
	Size       int64
}

// cacheInput records a freshly cached, immutable Go module file.
type cacheInput struct {
	RepositoryID string
	Module       string
	Version      string
	Kind         string
	Path         string
	BlobDigest   string
	Size         int64
	UpstreamURL  string
}

// get returns a cached file at its escaped path, or ErrFileNotFound.
func (s *fileStore) get(ctx context.Context, repositoryID, path string) (storedFile, error) {
	row, err := s.q.GetGoFile(ctx, db.GetGoFileParams{RepositoryID: repositoryID, Path: path})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedFile{}, ErrFileNotFound
		}
		return storedFile{}, fmt.Errorf("getting file: %w", err)
	}
	return storedFile{BlobDigest: row.BlobDigest, Size: row.Size}, nil
}

// cache records a downloaded immutable file (no audit; a cache fill is not a
// user mutation).
func (s *fileStore) cache(ctx context.Context, in cacheInput) error {
	ts := s.now().Format(time.RFC3339Nano)
	return s.q.UpsertGoFile(ctx, db.UpsertGoFileParams{
		ID:           id.New("go"),
		RepositoryID: in.RepositoryID,
		Module:       in.Module,
		Version:      in.Version,
		Kind:         in.Kind,
		Path:         in.Path,
		BlobDigest:   in.BlobDigest,
		Size:         in.Size,
		UpstreamUrl:  in.UpstreamURL,
		CreatedAt:    ts,
		UpdatedAt:    ts,
	})
}

func actorOrSystem(actor string) string {
	if actor == "" {
		return "system"
	}
	return actor
}
