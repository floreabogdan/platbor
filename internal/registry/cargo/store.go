package cargo

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
	// ErrVersionNotFound is returned when a crate version is absent.
	ErrVersionNotFound = errors.New("version not found")
	// ErrVersionExists is returned when a publish targets an existing version.
	ErrVersionExists = errors.New("version already exists")
)

// crateStore is the Cargo adapter's repository-scoped metadata layer.
type crateStore struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

func newCrateStore(sqlDB *sql.DB) *crateStore {
	return &crateStore{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

func (s *crateStore) resolveProject(ctx context.Context, key string) (id string, allowAutoCreate bool, err error) {
	row, err := s.q.GetProjectByKey(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, errProjectNotFound
		}
		return "", false, fmt.Errorf("resolving project %q: %w", key, err)
	}
	return row.ID, row.AllowAutoCreate != 0, nil
}

// version is a crate version read back for download.
type version struct {
	CrateID     string
	BlobDigest  string
	Size        int64
	Cksum       string
	UpstreamURL string
}

// publishInput is one `cargo publish`.
type publishInput struct {
	RepositoryID string
	ProjectID    string
	Name         string
	Version      string
	IndexLine    string
	Cksum        string
	BlobDigest   string
	Size         int64
	Actor        string
}

// publish stores a published crate version, its index line, and the .crate blob
// atomically with an audit entry. A re-publish of an existing version is 409.
func (s *crateStore) publish(ctx context.Context, in publishInput) error {
	ts := s.now().Format(time.RFC3339Nano)
	nameLower := normalizeName(in.Name)
	exists, err := s.q.CargoVersionExists(ctx, db.CargoVersionExistsParams{RepositoryID: in.RepositoryID, NameLower: nameLower, Version: in.Version})
	if err != nil {
		return fmt.Errorf("checking version: %w", err)
	}
	if exists > 0 {
		return ErrVersionExists
	}
	return s.inTx(ctx, func(qtx *db.Queries) error {
		crateID, err := qtx.UpsertCargoCrate(ctx, db.UpsertCargoCrateParams{
			ID: id.New("crate"), RepositoryID: in.RepositoryID, Name: in.Name, NameLower: nameLower, CreatedAt: ts, UpdatedAt: ts,
		})
		if err != nil {
			return fmt.Errorf("upserting crate: %w", err)
		}
		if err := qtx.InsertCargoVersion(ctx, db.InsertCargoVersionParams{
			ID: id.New("cratever"), CrateID: crateID, Version: in.Version, IndexLine: in.IndexLine,
			Cksum: in.Cksum, BlobDigest: in.BlobDigest, Size: in.Size, Yanked: 0, UpstreamUrl: "", CreatedAt: ts,
		}); err != nil {
			return fmt.Errorf("inserting version: %w", err)
		}
		return s.audit(ctx, qtx, in.ProjectID, in.Actor, "cargo.publish", in.Name+"@"+in.Version, ts,
			map[string]string{"name": in.Name, "version": in.Version})
	})
}

// cacheIndexRow records (or refreshes) a proxied version discovered in an
// upstream index line: its index line, cksum, and upstream URL, with no blob
// until it is downloaded.
func (s *crateStore) cacheIndexRow(ctx context.Context, repositoryID, name, version, indexLine, cksum, upstreamURL string, yanked bool) error {
	ts := s.now().Format(time.RFC3339Nano)
	return s.inTx(ctx, func(qtx *db.Queries) error {
		crateID, err := qtx.UpsertCargoCrate(ctx, db.UpsertCargoCrateParams{
			ID: id.New("crate"), RepositoryID: repositoryID, Name: name, NameLower: normalizeName(name), CreatedAt: ts, UpdatedAt: ts,
		})
		if err != nil {
			return fmt.Errorf("upserting crate: %w", err)
		}
		return qtx.InsertCargoVersion(ctx, db.InsertCargoVersionParams{
			ID: id.New("cratever"), CrateID: crateID, Version: version, IndexLine: indexLine,
			Cksum: cksum, BlobDigest: "", Size: 0, Yanked: boolToInt(yanked), UpstreamUrl: upstreamURL, CreatedAt: ts,
		})
	})
}

// setVersionBlob fills a proxied version's cached blob after download.
func (s *crateStore) setVersionBlob(ctx context.Context, repositoryID, nameLower, version, digest string, size int64) error {
	return s.q.SetCargoVersionBlob(ctx, db.SetCargoVersionBlobParams{
		BlobDigest: digest, Size: size, RepositoryID: repositoryID, NameLower: nameLower, Version: version,
	})
}

// indexLines returns the newline-delimited index for a crate (by lowercased name).
func (s *crateStore) indexLines(ctx context.Context, repositoryID, nameLower string) ([]string, error) {
	rows, err := s.q.ListCargoIndexLines(ctx, db.ListCargoIndexLinesParams{RepositoryID: repositoryID, NameLower: nameLower})
	if err != nil {
		return nil, fmt.Errorf("listing index lines: %w", err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		// Keep the served line's yanked flag in sync with the column.
		out = append(out, setLineYanked(r.IndexLine, r.Yanked != 0))
	}
	return out, nil
}

// getVersion resolves a crate version for download.
func (s *crateStore) getVersion(ctx context.Context, repositoryID, nameLower, ver string) (version, error) {
	row, err := s.q.GetCargoVersion(ctx, db.GetCargoVersionParams{RepositoryID: repositoryID, NameLower: nameLower, Version: ver})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return version{}, ErrVersionNotFound
		}
		return version{}, fmt.Errorf("getting version: %w", err)
	}
	return version{CrateID: row.CrateID, BlobDigest: row.BlobDigest, Size: row.Size, Cksum: row.Cksum, UpstreamURL: row.UpstreamUrl}, nil
}

// setYanked flips a version's yanked flag, auditing it. Returns ErrVersionNotFound
// if no such version.
func (s *crateStore) setYanked(ctx context.Context, repositoryID, projectID, name, ver string, yanked bool, actor string) error {
	ts := s.now().Format(time.RFC3339Nano)
	return s.inTx(ctx, func(qtx *db.Queries) error {
		n, err := qtx.SetCargoYanked(ctx, db.SetCargoYankedParams{Yanked: boolToInt(yanked), RepositoryID: repositoryID, NameLower: normalizeName(name), Version: ver})
		if err != nil {
			return fmt.Errorf("setting yanked: %w", err)
		}
		if n == 0 {
			return ErrVersionNotFound
		}
		action := "cargo.yank"
		if !yanked {
			action = "cargo.unyank"
		}
		return s.audit(ctx, qtx, projectID, actor, action, name+"@"+ver, ts, map[string]string{"name": name, "version": ver})
	})
}

func (s *crateStore) inTx(ctx context.Context, fn func(*db.Queries) error) error {
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

func (s *crateStore) audit(ctx context.Context, qtx *db.Queries, projectID, actor, action, targetID, ts string, meta map[string]string) error {
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
		TargetType: "crate",
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
