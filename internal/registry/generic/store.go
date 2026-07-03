package generic

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

// fileStore is the generic adapter's project-scoped metadata layer: it maps a
// (project, repository, path) to a blob and its checksums, and audits mutations.
type fileStore struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

func newFileStore(sqlDB *sql.DB) *fileStore {
	return &fileStore{
		db:  sqlDB,
		q:   db.New(sqlDB),
		now: func() time.Time { return time.Now().UTC() },
	}
}

func (s *fileStore) resolveProject(ctx context.Context, key string) (string, error) {
	row, err := s.q.GetProjectByKey(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errProjectNotFound
		}
		return "", fmt.Errorf("resolving project %q: %w", key, err)
	}
	return row.ID, nil
}

// isProxy reports whether a project is a pull-through mirror, used to reject
// writes. A generic proxy has no upstream protocol, so a proxy project simply
// rejects generic uploads and deletes.
func (s *fileStore) isProxy(ctx context.Context, projectID string) (bool, error) {
	_, err := s.q.GetProxyByProjectID(ctx, projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("loading proxy config: %w", err)
	}
	return true, nil
}

// storedFile is a file read back for serving: its blob and checksums.
type storedFile struct {
	BlobDigest string
	Size       int64
	SHA256     string
	SHA1       string
	MD5        string
}

// filePut is an upload to persist: the resolved location, the committed blob,
// and the checksums computed while streaming it.
type filePut struct {
	ProjectID  string
	Repository string
	Path       string
	BlobDigest string
	Size       int64
	SHA256     string
	SHA1       string
	MD5        string
	Actor      string
}

// put records a file at its path (overwriting any existing one) and audits it.
func (s *fileStore) put(ctx context.Context, in filePut) error {
	ts := s.now().Format(time.RFC3339Nano)
	return s.inTx(ctx, func(qtx *db.Queries) error {
		if err := qtx.UpsertGenericFile(ctx, db.UpsertGenericFileParams{
			ID:         id.New("gen"),
			ProjectID:  in.ProjectID,
			Repository: in.Repository,
			Path:       in.Path,
			BlobDigest: in.BlobDigest,
			Size:       in.Size,
			Sha256:     in.SHA256,
			Sha1:       in.SHA1,
			Md5:        in.MD5,
			CreatedAt:  ts,
			UpdatedAt:  ts,
		}); err != nil {
			return fmt.Errorf("storing file: %w", err)
		}
		return s.audit(ctx, qtx, in.ProjectID, in.Actor, "generic.put", "file", in.Path, ts,
			map[string]string{"repository": in.Repository, "size": fmt.Sprintf("%d", in.Size)})
	})
}

func (s *fileStore) get(ctx context.Context, projectID, repo, path string) (storedFile, error) {
	row, err := s.q.GetGenericFile(ctx, db.GetGenericFileParams{ProjectID: projectID, Repository: repo, Path: path})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedFile{}, ErrFileNotFound
		}
		return storedFile{}, fmt.Errorf("getting file: %w", err)
	}
	return storedFile{
		BlobDigest: row.BlobDigest,
		Size:       row.Size,
		SHA256:     row.Sha256,
		SHA1:       row.Sha1,
		MD5:        row.Md5,
	}, nil
}

// delete removes a file's record (the blob is reclaimed by GC), auditing it.
func (s *fileStore) delete(ctx context.Context, projectID, repo, path, actor string) error {
	ts := s.now().Format(time.RFC3339Nano)
	return s.inTx(ctx, func(qtx *db.Queries) error {
		n, err := qtx.DeleteGenericFile(ctx, db.DeleteGenericFileParams{ProjectID: projectID, Repository: repo, Path: path})
		if err != nil {
			return fmt.Errorf("deleting file: %w", err)
		}
		if n == 0 {
			return ErrFileNotFound
		}
		return s.audit(ctx, qtx, projectID, actor, "generic.delete", "file", path, ts,
			map[string]string{"repository": repo})
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

func (s *fileStore) audit(ctx context.Context, qtx *db.Queries, projectID, actor, action, targetType, targetID, ts string, meta map[string]string) error {
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
		TargetType: targetType,
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
