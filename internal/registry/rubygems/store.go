package rubygems

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
	// ErrGemNotFound is returned when a gem or version is absent.
	ErrGemNotFound = errors.New("gem not found")
	// ErrVersionExists is returned when a push targets an existing version.
	ErrVersionExists = errors.New("gem version already exists")
)

// gemStore is the RubyGems adapter's repository-scoped metadata layer.
type gemStore struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

func newGemStore(sqlDB *sql.DB) *gemStore {
	return &gemStore{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

func (s *gemStore) resolveProject(ctx context.Context, key string) (id string, allowAutoCreate bool, err error) {
	row, err := s.q.GetProjectByKey(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, errProjectNotFound
		}
		return "", false, fmt.Errorf("resolving project %q: %w", key, err)
	}
	return row.ID, row.AllowAutoCreate != 0, nil
}

// gemFile is a version read back for download.
type gemFile struct {
	GemID       string
	BlobDigest  string
	Size        int64
	SHA256      string
	UpstreamURL string
}

// pushInput is one `gem push`.
type pushInput struct {
	RepositoryID string
	ProjectID    string
	Spec         gemSpec
	BlobDigest   string
	Size         int64
	Actor        string
}

// push stores a pushed gem version and the .gem blob atomically, with an audit
// entry. A re-push of an existing version is 409.
func (s *gemStore) push(ctx context.Context, in pushInput) error {
	ts := s.now().Format(time.RFC3339Nano)
	exists, err := s.q.GemVersionExists(ctx, db.GemVersionExistsParams{RepositoryID: in.RepositoryID, Name: in.Spec.Name, FullName: in.Spec.FullName})
	if err != nil {
		return fmt.Errorf("checking version: %w", err)
	}
	if exists > 0 {
		return ErrVersionExists
	}
	return s.inTx(ctx, func(qtx *db.Queries) error {
		gemID, err := qtx.UpsertGem(ctx, db.UpsertGemParams{ID: id.New("gem"), RepositoryID: in.RepositoryID, Name: in.Spec.Name, CreatedAt: ts, UpdatedAt: ts})
		if err != nil {
			return fmt.Errorf("upserting gem: %w", err)
		}
		if err := qtx.InsertGemVersion(ctx, db.InsertGemVersionParams{
			ID: id.New("gemver"), GemID: gemID, Version: in.Spec.Version, Platform: in.Spec.Platform, Number: in.Spec.Number,
			FullName: in.Spec.FullName, InfoDeps: in.Spec.InfoDeps, InfoReqs: in.Spec.InfoReqs, Sha256: cksumOf(in.Spec.InfoReqs),
			BlobDigest: in.BlobDigest, Size: in.Size, Yanked: 0, UpstreamUrl: "", CreatedAt: ts,
		}); err != nil {
			return fmt.Errorf("inserting version: %w", err)
		}
		return s.audit(ctx, qtx, in.ProjectID, in.Actor, "rubygems.push", in.Spec.FullName, ts,
			map[string]string{"name": in.Spec.Name, "version": in.Spec.Number})
	})
}

// cacheIndexRow records (or refreshes) a proxied version parsed from an upstream
// info line, with no blob until it is downloaded.
func (s *gemStore) cacheIndexRow(ctx context.Context, repositoryID, name string, v parsedInfoVersion, upstreamURL string) error {
	ts := s.now().Format(time.RFC3339Nano)
	return s.inTx(ctx, func(qtx *db.Queries) error {
		gemID, err := qtx.UpsertGem(ctx, db.UpsertGemParams{ID: id.New("gem"), RepositoryID: repositoryID, Name: name, CreatedAt: ts, UpdatedAt: ts})
		if err != nil {
			return fmt.Errorf("upserting gem: %w", err)
		}
		return qtx.InsertGemVersion(ctx, db.InsertGemVersionParams{
			ID: id.New("gemver"), GemID: gemID, Version: v.Version, Platform: v.Platform, Number: v.Number,
			FullName: name + "-" + v.Number, InfoDeps: v.Deps, InfoReqs: v.Reqs, Sha256: v.Checksum,
			BlobDigest: "", Size: 0, Yanked: 0, UpstreamUrl: upstreamURL, CreatedAt: ts,
		})
	})
}

func (s *gemStore) setVersionBlob(ctx context.Context, repositoryID, fullName, digest string, size int64) error {
	return s.q.SetGemVersionBlob(ctx, db.SetGemVersionBlobParams{BlobDigest: digest, Size: size, RepositoryID: repositoryID, FullName: fullName})
}

// infoVersion is one line's data for the /info/<gem> file.
type infoVersion struct {
	Number string
	Deps   string
	Reqs   string
	Yanked bool
}

func (s *gemStore) infoVersions(ctx context.Context, repositoryID, name string) ([]infoVersion, error) {
	rows, err := s.q.ListGemInfoVersions(ctx, db.ListGemInfoVersionsParams{RepositoryID: repositoryID, Name: name})
	if err != nil {
		return nil, fmt.Errorf("listing info versions: %w", err)
	}
	out := make([]infoVersion, 0, len(rows))
	for _, r := range rows {
		out = append(out, infoVersion{Number: r.Number, Deps: r.InfoDeps, Reqs: r.InfoReqs, Yanked: r.Yanked != 0})
	}
	return out, nil
}

func (s *gemStore) getFile(ctx context.Context, repositoryID, fullName string) (gemFile, error) {
	row, err := s.q.GetGemFile(ctx, db.GetGemFileParams{RepositoryID: repositoryID, FullName: fullName})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return gemFile{}, ErrGemNotFound
		}
		return gemFile{}, fmt.Errorf("getting file: %w", err)
	}
	return gemFile{GemID: row.GemID, BlobDigest: row.BlobDigest, Size: row.Size, SHA256: row.Sha256, UpstreamURL: row.UpstreamUrl}, nil
}

func (s *gemStore) names(ctx context.Context, repositoryID string) ([]string, error) {
	return s.q.ListGemNames(ctx, repositoryID)
}

// indexVersion is one gem's presence in the /versions file.
type indexGemVersion struct {
	Name   string
	Number string
	Deps   string
	Reqs   string
}

func (s *gemStore) versionsForIndex(ctx context.Context, repositoryID string) ([]indexGemVersion, error) {
	rows, err := s.q.ListGemVersionsForIndex(ctx, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("listing index versions: %w", err)
	}
	out := make([]indexGemVersion, 0, len(rows))
	for _, r := range rows {
		out = append(out, indexGemVersion{Name: r.Name, Number: r.Number, Deps: r.InfoDeps, Reqs: r.InfoReqs})
	}
	return out, nil
}

// setYanked flips a version's yanked flag (by number), auditing it.
func (s *gemStore) setYanked(ctx context.Context, repositoryID, projectID, name, number string, yanked bool, actor string) error {
	ts := s.now().Format(time.RFC3339Nano)
	return s.inTx(ctx, func(qtx *db.Queries) error {
		n, err := qtx.SetGemYanked(ctx, db.SetGemYankedParams{Yanked: boolToInt(yanked), RepositoryID: repositoryID, Name: name, Number: number})
		if err != nil {
			return fmt.Errorf("setting yanked: %w", err)
		}
		if n == 0 {
			return ErrGemNotFound
		}
		return s.audit(ctx, qtx, projectID, actor, "rubygems.yank", name+"-"+number, ts, map[string]string{"name": name, "version": number})
	})
}

func (s *gemStore) inTx(ctx context.Context, fn func(*db.Queries) error) error {
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

func (s *gemStore) audit(ctx context.Context, qtx *db.Queries, projectID, actor, action, targetID, ts string, meta map[string]string) error {
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
		TargetType: "gem",
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

// cksumOf extracts the checksum from an info-reqs string ("checksum:<sha>,...")
// so the stored sha256 column matches what the info line advertises.
func cksumOf(reqs string) string {
	for _, part := range splitComma(reqs) {
		if len(part) > len("checksum:") && part[:len("checksum:")] == "checksum:" {
			return part[len("checksum:"):]
		}
	}
	return ""
}
