package pypi

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
	// ErrFileNotFound is returned when a distribution file is absent.
	ErrFileNotFound = errors.New("file not found")
	// ErrFileExists is returned when an upload targets an existing filename.
	ErrFileExists = errors.New("file already exists")
)

// packageStore is the PyPI adapter's repository-scoped metadata layer.
type packageStore struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

func newPackageStore(sqlDB *sql.DB) *packageStore {
	return &packageStore{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

func (s *packageStore) resolveProject(ctx context.Context, key string) (id string, allowAutoCreate bool, err error) {
	row, err := s.q.GetProjectByKey(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, errProjectNotFound
		}
		return "", false, fmt.Errorf("resolving project %q: %w", key, err)
	}
	return row.ID, row.AllowAutoCreate != 0, nil
}

// file is a distribution file read back for the simple index and downloads.
type file struct {
	Filename       string
	Version        string
	SHA256         string
	RequiresPython string
	BlobDigest     string
	Size           int64
	UpstreamURL    string
	PackageID      string
}

// uploadInput is one `twine upload`: a single distribution file.
type uploadInput struct {
	RepositoryID   string
	ProjectID      string
	NameNormalized string
	NameOriginal   string
	Version        string
	Filename       string
	BlobDigest     string
	Size           int64
	SHA256         string
	RequiresPython string
	Actor          string
}

// upload stores an uploaded distribution file and its package atomically, with an
// audit entry. A re-upload of an existing filename returns ErrFileExists.
func (s *packageStore) upload(ctx context.Context, in uploadInput) error {
	ts := s.now().Format(time.RFC3339Nano)
	exists, err := s.q.PypiFileExists(ctx, db.PypiFileExistsParams{RepositoryID: in.RepositoryID, Filename: in.Filename})
	if err != nil {
		return fmt.Errorf("checking file: %w", err)
	}
	if exists > 0 {
		return ErrFileExists
	}
	return s.inTx(ctx, func(qtx *db.Queries) error {
		pkgID, err := qtx.UpsertPypiPackage(ctx, db.UpsertPypiPackageParams{
			ID:           id.New("pypipkg"),
			RepositoryID: in.RepositoryID,
			Name:         in.NameNormalized,
			NameOriginal: in.NameOriginal,
			CreatedAt:    ts,
			UpdatedAt:    ts,
		})
		if err != nil {
			return fmt.Errorf("upserting package: %w", err)
		}
		if err := qtx.InsertPypiFile(ctx, db.InsertPypiFileParams{
			ID:             id.New("pypifile"),
			PackageID:      pkgID,
			Version:        in.Version,
			Filename:       in.Filename,
			BlobDigest:     in.BlobDigest,
			Size:           in.Size,
			Sha256:         in.SHA256,
			RequiresPython: in.RequiresPython,
			UpstreamUrl:    "",
			CreatedAt:      ts,
		}); err != nil {
			return fmt.Errorf("inserting file: %w", err)
		}
		return s.audit(ctx, qtx, in.ProjectID, in.Actor, "pypi.upload", in.Filename, ts,
			map[string]string{"name": in.NameOriginal, "version": in.Version})
	})
}

// cacheIndexRow records (or refreshes) a proxied file discovered in an upstream
// simple index: its upstream URL and hash, with no blob until it is downloaded.
func (s *packageStore) cacheIndexRow(ctx context.Context, repositoryID, nameNormalized, nameOriginal string, f file) error {
	ts := s.now().Format(time.RFC3339Nano)
	return s.inTx(ctx, func(qtx *db.Queries) error {
		pkgID, err := qtx.UpsertPypiPackage(ctx, db.UpsertPypiPackageParams{
			ID:           id.New("pypipkg"),
			RepositoryID: repositoryID,
			Name:         nameNormalized,
			NameOriginal: nameOriginal,
			CreatedAt:    ts,
			UpdatedAt:    ts,
		})
		if err != nil {
			return fmt.Errorf("upserting package: %w", err)
		}
		return qtx.InsertPypiFile(ctx, db.InsertPypiFileParams{
			ID:             id.New("pypifile"),
			PackageID:      pkgID,
			Version:        f.Version,
			Filename:       f.Filename,
			BlobDigest:     "", // filled on first download
			Size:           0,
			Sha256:         f.SHA256,
			RequiresPython: f.RequiresPython,
			UpstreamUrl:    f.UpstreamURL,
			CreatedAt:      ts,
		})
	})
}

// setFileBlob fills a proxied file's cached blob after it is downloaded.
func (s *packageStore) setFileBlob(ctx context.Context, packageID, filename, digest string, size int64) error {
	return s.q.SetPypiFileBlob(ctx, db.SetPypiFileBlobParams{
		BlobDigest: digest, Size: size, PackageID: packageID, Filename: filename,
	})
}

// listFiles returns every distribution file of a package (by normalized name).
func (s *packageStore) listFiles(ctx context.Context, repositoryID, nameNormalized string) ([]file, error) {
	rows, err := s.q.ListPypiFiles(ctx, db.ListPypiFilesParams{RepositoryID: repositoryID, Name: nameNormalized})
	if err != nil {
		return nil, fmt.Errorf("listing files: %w", err)
	}
	out := make([]file, 0, len(rows))
	for _, r := range rows {
		out = append(out, file{
			Filename: r.Filename, Version: r.Version, SHA256: r.Sha256,
			RequiresPython: r.RequiresPython, BlobDigest: r.BlobDigest, Size: r.Size, UpstreamURL: r.UpstreamUrl,
		})
	}
	return out, nil
}

// getFile resolves a distribution filename to its content for download.
func (s *packageStore) getFile(ctx context.Context, repositoryID, filename string) (file, error) {
	row, err := s.q.GetPypiFile(ctx, db.GetPypiFileParams{RepositoryID: repositoryID, Filename: filename})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return file{}, ErrFileNotFound
		}
		return file{}, fmt.Errorf("getting file: %w", err)
	}
	return file{
		PackageID: row.PackageID, BlobDigest: row.BlobDigest, Size: row.Size,
		SHA256: row.Sha256, UpstreamURL: row.UpstreamUrl, Filename: filename,
	}, nil
}

// packageNames returns the normalized names of every package in a repository.
func (s *packageStore) packageNames(ctx context.Context, repositoryID string) ([]string, error) {
	return s.q.ListPypiPackageNames(ctx, repositoryID)
}

func (s *packageStore) inTx(ctx context.Context, fn func(*db.Queries) error) error {
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

func (s *packageStore) audit(ctx context.Context, qtx *db.Queries, projectID, actor, action, targetID, ts string, meta map[string]string) error {
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
