package oci

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

// Store-level sentinels. Handlers translate these into spec error envelopes;
// the store itself never speaks HTTP.
var (
	errProjectNotFound = errors.New("project not found")
	// ErrManifestNotFound is returned by reads (Browser) and mutations when a
	// manifest or tag reference resolves to nothing. Exported so the HTTP layer
	// can map it to 404.
	ErrManifestNotFound = errors.New("manifest not found")
)

// manifestStore is the OCI adapter's own project-scoped metadata layer. The SQL
// lives in core/db (sqlc), but the registry semantics — digests, tags, and the
// audit actions they produce — stay here in the adapter, so adding a format
// never reaches into a core domain service.
type manifestStore struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

func newManifestStore(sqlDB *sql.DB) *manifestStore {
	return &manifestStore{
		db:  sqlDB,
		q:   db.New(sqlDB),
		now: func() time.Time { return time.Now().UTC() },
	}
}

// resolveProject maps the leading name component to a project id and its
// auto-create policy.
func (s *manifestStore) resolveProject(ctx context.Context, key string) (id string, allowAutoCreate bool, err error) {
	row, err := s.q.GetProjectByKey(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, errProjectNotFound
		}
		return "", false, fmt.Errorf("resolving project %q: %w", key, err)
	}
	return row.ID, row.AllowAutoCreate != 0, nil
}

// storedManifest is a manifest read back for serving: its exact bytes plus the
// metadata needed for the response headers.
type storedManifest struct {
	Digest    string
	MediaType string
	Payload   []byte
	Size      int64
}

func (s *manifestStore) getManifest(ctx context.Context, repositoryID, repo, digest string) (storedManifest, error) {
	row, err := s.q.GetManifest(ctx, db.GetManifestParams{RepositoryID: repositoryID, Repository: repo, Digest: digest})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return storedManifest{}, ErrManifestNotFound
		}
		return storedManifest{}, fmt.Errorf("getting manifest: %w", err)
	}
	return storedManifest{Digest: row.Digest, MediaType: row.MediaType, Payload: row.Payload, Size: row.Size}, nil
}

// resolveTag returns the digest a tag currently points at, or ErrManifestNotFound.
func (s *manifestStore) resolveTag(ctx context.Context, repositoryID, repo, tag string) (string, error) {
	row, err := s.q.GetTag(ctx, db.GetTagParams{RepositoryID: repositoryID, Repository: repo, Tag: tag})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrManifestNotFound
		}
		return "", fmt.Errorf("resolving tag: %w", err)
	}
	return row.Digest, nil
}

func (s *manifestStore) manifestExists(ctx context.Context, repositoryID, repo, digest string) (bool, error) {
	n, err := s.q.ManifestExists(ctx, db.ManifestExistsParams{RepositoryID: repositoryID, Repository: repo, Digest: digest})
	if err != nil {
		return false, fmt.Errorf("checking manifest existence: %w", err)
	}
	return n > 0, nil
}

// listTags returns up to limit tags in lexical order after last (exclusive), for
// keyset pagination.
func (s *manifestStore) listTags(ctx context.Context, repositoryID, repo, last string, limit int) ([]string, error) {
	tags, err := s.q.ListTags(ctx, db.ListTagsParams{
		RepositoryID: repositoryID,
		Repository:   repo,
		Tag:          last,
		Limit:        int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("listing tags: %w", err)
	}
	return tags, nil
}

// referencedBlobs returns the set of blob digests every stored manifest needs —
// the config and layer descriptors across all manifests, spanning all projects
// (blobs are a global CAS). An index references child manifests rather than
// blobs and contributes nothing directly; each child's blobs are counted when
// its own payload is parsed.
func (s *manifestStore) referencedBlobs(ctx context.Context) (map[string]struct{}, error) {
	payloads, err := s.q.ListManifestPayloads(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing manifest payloads: %w", err)
	}
	refs := make(map[string]struct{})
	for _, payload := range payloads {
		var doc manifestDoc
		if err := json.Unmarshal(payload, &doc); err != nil {
			continue // a stored manifest is well-formed; skip anything odd
		}
		if doc.Config != nil && doc.Config.Digest != "" {
			refs[doc.Config.Digest] = struct{}{}
		}
		for _, l := range doc.Layers {
			if l.Digest != "" {
				refs[l.Digest] = struct{}{}
			}
		}
	}
	return refs, nil
}

// referrerRow is a manifest that refers to a subject, read back for the
// referrers API (payload carried so the handler can extract annotations).
type referrerRow struct {
	Digest       string
	MediaType    string
	Size         int64
	ArtifactType string
	Payload      []byte
}

// listReferrers returns the manifests whose subject is the given digest.
func (s *manifestStore) listReferrers(ctx context.Context, repositoryID, repo, subject string) ([]referrerRow, error) {
	rows, err := s.q.ListReferrers(ctx, db.ListReferrersParams{RepositoryID: repositoryID, Repository: repo, Subject: subject})
	if err != nil {
		return nil, fmt.Errorf("listing referrers: %w", err)
	}
	out := make([]referrerRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, referrerRow{
			Digest:       r.Digest,
			MediaType:    r.MediaType,
			Size:         r.Size,
			ArtifactType: r.ArtifactType,
			Payload:      r.Payload,
		})
	}
	return out, nil
}

// manifestWrite is the input to putManifest. Tag is empty for a push by digest;
// Subject/ArtifactType are denormalized from the payload for the referrers API.
// RepositoryID scopes storage; ProjectID scopes the audit entry.
type manifestWrite struct {
	RepositoryID string
	ProjectID    string
	Repository   string
	Digest       string
	MediaType    string
	Payload      []byte
	Size         int64
	Tag          string
	Subject      string
	ArtifactType string
	Actor        string
}

// putManifest stores a manifest and, when Tag is set, repoints that tag at it —
// atomically, with an audit entry. Re-pushing identical content is a no-op on
// the manifest row (dedup); retagging just moves the tag.
func (s *manifestStore) putManifest(ctx context.Context, m manifestWrite) error {
	ts := s.now().Format(time.RFC3339Nano)

	return s.inTx(ctx, func(qtx *db.Queries) error {
		if err := qtx.UpsertManifest(ctx, db.UpsertManifestParams{
			ID:           id.New("mfst"),
			RepositoryID: m.RepositoryID,
			Repository:   m.Repository,
			Digest:       m.Digest,
			MediaType:    m.MediaType,
			Payload:      m.Payload,
			Size:         m.Size,
			Subject:      m.Subject,
			ArtifactType: m.ArtifactType,
			CreatedAt:    ts,
		}); err != nil {
			return fmt.Errorf("storing manifest: %w", err)
		}
		if m.Tag != "" {
			if err := qtx.UpsertTag(ctx, db.UpsertTagParams{
				RepositoryID: m.RepositoryID,
				Repository:   m.Repository,
				Tag:          m.Tag,
				Digest:       m.Digest,
				UpdatedAt:    ts,
			}); err != nil {
				return fmt.Errorf("tagging manifest: %w", err)
			}
		}
		return s.audit(ctx, qtx, m.ProjectID, m.Actor, "oci.manifest.push", "manifest", m.Digest, ts,
			map[string]string{"repository": m.Repository, "reference": refOrDigest(m.Tag, m.Digest)})
	})
}

// deleteManifest removes a manifest by digest along with every tag that pointed
// at it. Deleting an unknown manifest returns ErrManifestNotFound.
func (s *manifestStore) deleteManifest(ctx context.Context, repositoryID, projectID, repo, digest, actor string) error {
	ts := s.now().Format(time.RFC3339Nano)

	return s.inTx(ctx, func(qtx *db.Queries) error {
		n, err := qtx.DeleteManifest(ctx, db.DeleteManifestParams{RepositoryID: repositoryID, Repository: repo, Digest: digest})
		if err != nil {
			return fmt.Errorf("deleting manifest: %w", err)
		}
		if n == 0 {
			return ErrManifestNotFound
		}
		if err := qtx.DeleteTagsForDigest(ctx, db.DeleteTagsForDigestParams{RepositoryID: repositoryID, Repository: repo, Digest: digest}); err != nil {
			return fmt.Errorf("deleting tags for digest: %w", err)
		}
		return s.audit(ctx, qtx, projectID, actor, "oci.manifest.delete", "manifest", digest, ts,
			map[string]string{"repository": repo})
	})
}

// deleteTag removes a single tag, leaving the manifest it referenced in place.
func (s *manifestStore) deleteTag(ctx context.Context, repositoryID, projectID, repo, tag, actor string) error {
	ts := s.now().Format(time.RFC3339Nano)

	return s.inTx(ctx, func(qtx *db.Queries) error {
		n, err := qtx.DeleteTag(ctx, db.DeleteTagParams{RepositoryID: repositoryID, Repository: repo, Tag: tag})
		if err != nil {
			return fmt.Errorf("deleting tag: %w", err)
		}
		if n == 0 {
			return ErrManifestNotFound
		}
		return s.audit(ctx, qtx, projectID, actor, "oci.tag.delete", "tag", tag, ts,
			map[string]string{"repository": repo})
	})
}

// inTx runs fn inside a transaction, committing on success and rolling back on
// any error.
func (s *manifestStore) inTx(ctx context.Context, fn func(*db.Queries) error) error {
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

// audit writes one audit entry inside the caller's transaction, so a mutation
// and its record commit together.
func (s *manifestStore) audit(ctx context.Context, qtx *db.Queries, projectID, actor, action, targetType, targetID, ts string, meta map[string]string) error {
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

func refOrDigest(tag, digest string) string {
	if tag != "" {
		return tag
	}
	return digest
}
