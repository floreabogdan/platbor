package maven

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

var (
	errProjectNotFound = errors.New("project not found")
	// ErrFileNotFound is returned when no file exists at a path.
	ErrFileNotFound = errors.New("file not found")
)

// fileStore is the Maven adapter's repository-scoped metadata layer: it maps a
// (repository, path) to a blob and its coordinates, and audits mutations.
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

// storedFile is a file read back for serving.
type storedFile struct {
	BlobDigest  string
	Size        int64
	SHA1        string
	MD5         string
	UpstreamURL string
	IsMetadata  bool
}

// filePut is an upload (or a cached proxy file) to persist.
type filePut struct {
	RepositoryID string
	ProjectID    string
	Path         string
	BlobDigest   string
	Size         int64
	SHA1         string
	MD5          string
	UpstreamURL  string
	Actor        string
	audit        bool
}

// put records a file at its path (overwriting any existing one) and, when
// in.audit is set, audits it. Coordinates are parsed from the path for browsing.
func (s *fileStore) put(ctx context.Context, in filePut) error {
	ts := s.now().Format(time.RFC3339Nano)
	group, artifact, version, filename, isMeta := coordinates(in.Path)
	return s.inTx(ctx, func(qtx *db.Queries) error {
		if err := qtx.UpsertMavenFile(ctx, db.UpsertMavenFileParams{
			ID:           id.New("mvn"),
			RepositoryID: in.RepositoryID,
			Path:         in.Path,
			GroupID:      group,
			ArtifactID:   artifact,
			Version:      version,
			Filename:     filename,
			IsMetadata:   boolToInt(isMeta),
			BlobDigest:   in.BlobDigest,
			Size:         in.Size,
			Sha1:         in.SHA1,
			Md5:          in.MD5,
			UpstreamUrl:  in.UpstreamURL,
			CreatedAt:    ts,
			UpdatedAt:    ts,
		}); err != nil {
			return fmt.Errorf("storing file: %w", err)
		}
		if !in.audit {
			return nil
		}
		return s.auditEntry(ctx, qtx, in.ProjectID, in.Actor, "maven.deploy", in.Path, ts,
			map[string]string{"size": fmt.Sprintf("%d", in.Size)})
	})
}

func (s *fileStore) get(ctx context.Context, repositoryID, path string) (storedFile, error) {
	row, err := s.q.GetMavenFile(ctx, db.GetMavenFileParams{RepositoryID: repositoryID, Path: path})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedFile{}, ErrFileNotFound
		}
		return storedFile{}, fmt.Errorf("getting file: %w", err)
	}
	return storedFile{
		BlobDigest:  row.BlobDigest,
		Size:        row.Size,
		SHA1:        row.Sha1,
		MD5:         row.Md5,
		UpstreamURL: row.UpstreamUrl,
		IsMetadata:  row.IsMetadata != 0,
	}, nil
}

// delete removes a file's record (the blob is reclaimed by GC), auditing it.
func (s *fileStore) delete(ctx context.Context, repositoryID, projectID, path, actor string) error {
	ts := s.now().Format(time.RFC3339Nano)
	return s.inTx(ctx, func(qtx *db.Queries) error {
		n, err := qtx.DeleteMavenFile(ctx, db.DeleteMavenFileParams{RepositoryID: repositoryID, Path: path})
		if err != nil {
			return fmt.Errorf("deleting file: %w", err)
		}
		if n == 0 {
			return ErrFileNotFound
		}
		return s.auditEntry(ctx, qtx, projectID, actor, "maven.delete", path, ts, nil)
	})
}

func (s *fileStore) inTx(ctx context.Context, fn func(*db.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func (s *fileStore) auditEntry(ctx context.Context, qtx *db.Queries, projectID, actor, action, targetID, ts string, meta map[string]string) error {
	payload := "{}"
	if len(meta) > 0 {
		if b, err := json.Marshal(meta); err == nil {
			payload = string(b)
		}
	}
	if _, err := qtx.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		Actor:      actorOrSystem(actor),
		Action:     action,
		TargetType: "file",
		TargetID:   targetID,
		Metadata:   payload,
		CreatedAt:  ts,
	}); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}
	return nil
}

func actorOrSystem(actor string) string {
	if actor == "" {
		return "system"
	}
	return actor
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
