package npm

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

// Store-level sentinels. Handlers translate these into the npm JSON error
// envelope; the store never speaks HTTP.
var (
	errProjectNotFound = errors.New("project not found")
	// ErrPackageNotFound is returned when a package (or a version of it) is not
	// present in the target repository.
	ErrPackageNotFound = errors.New("package not found")
	// ErrVersionExists is returned when a publish targets a version that already
	// exists; npm forbids overwriting a published version.
	ErrVersionExists = errors.New("version already exists")
)

// packageStore is the npm adapter's own project-scoped metadata layer. The SQL
// lives in core/db (sqlc); the npm semantics — packages, versions, dist-tags,
// and the audit actions they produce — stay here, so adding a format never
// reaches into a core domain service.
type packageStore struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

func newPackageStore(sqlDB *sql.DB) *packageStore {
	return &packageStore{
		db:  sqlDB,
		q:   db.New(sqlDB),
		now: func() time.Time { return time.Now().UTC() },
	}
}

// resolveProject maps a project key to its id. Every publish and install targets
// an existing project (Harbor-style); unknown keys are rejected so packages
// never land outside a project's scope.
func (s *packageStore) resolveProject(ctx context.Context, key string) (string, error) {
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
// writes (a proxy is read-only).
func (s *packageStore) isProxy(ctx context.Context, projectID string) (bool, error) {
	_, _, err := s.proxyUpstream(ctx, projectID)
	if errors.Is(err, errNotProxy) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// errNotProxy is an internal sentinel: the project is an ordinary local project,
// not a proxy. Callers translate it to "no upstream".
var errNotProxy = errors.New("not a proxy project")

// proxyUpstream returns the upstream a project mirrors, or errNotProxy when the
// project is an ordinary local project.
func (s *packageStore) proxyUpstream(ctx context.Context, projectID string) (upstream, bool, error) {
	row, err := s.q.GetProxyByProjectID(ctx, projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return upstream{}, false, errNotProxy
		}
		return upstream{}, false, fmt.Errorf("loading proxy config: %w", err)
	}
	return upstream{BaseURL: row.UpstreamUrl, Username: row.Username, Password: row.Password}, true, nil
}

// cacheVersion stores a proxied version and its tarball digest, deduping if it
// already exists. A cache fill is not a user mutation, so it writes no audit
// entry (unlike publish).
func (s *packageStore) cacheVersion(ctx context.Context, in versionInput, projectID, repo, name string) error {
	ts := s.now().Format(time.RFC3339Nano)
	return s.inTx(ctx, func(qtx *db.Queries) error {
		pkgID, err := qtx.UpsertNpmPackage(ctx, db.UpsertNpmPackageParams{
			ID:         id.New("npmpkg"),
			ProjectID:  projectID,
			Repository: repo,
			Name:       name,
			CreatedAt:  ts,
			UpdatedAt:  ts,
		})
		if err != nil {
			return fmt.Errorf("upserting package: %w", err)
		}
		if err := qtx.InsertNpmVersion(ctx, db.InsertNpmVersionParams{
			ID:            id.New("npmver"),
			PackageID:     pkgID,
			Version:       in.Version,
			Manifest:      in.Manifest,
			TarballDigest: in.TarballDigest,
			TarballSize:   in.TarballSize,
			Shasum:        in.Shasum,
			Integrity:     in.Integrity,
			CreatedAt:     ts,
		}); err != nil {
			return fmt.Errorf("caching version: %w", err)
		}
		return nil
	})
}

// versionInput is one published version: its metadata document (stored verbatim)
// and the digests of its tarball, already committed to the blob store.
type versionInput struct {
	Version       string
	Manifest      []byte
	TarballDigest string
	TarballSize   int64
	Shasum        string
	Integrity     string
}

// publishInput is a single `npm publish`: one or more versions of one package
// plus the dist-tags to move (usually just "latest").
type publishInput struct {
	ProjectID  string
	Repository string
	Name       string
	Versions   []versionInput
	DistTags   map[string]string
	Actor      string
}

// publish stores the package, its new versions, and its dist-tags atomically,
// with an audit entry. A version that already exists aborts the whole publish
// with ErrVersionExists (npm never overwrites a published version).
func (s *packageStore) publish(ctx context.Context, in publishInput) error {
	ts := s.now().Format(time.RFC3339Nano)

	return s.inTx(ctx, func(qtx *db.Queries) error {
		pkgID, err := qtx.UpsertNpmPackage(ctx, db.UpsertNpmPackageParams{
			ID:         id.New("npmpkg"),
			ProjectID:  in.ProjectID,
			Repository: in.Repository,
			Name:       in.Name,
			CreatedAt:  ts,
			UpdatedAt:  ts,
		})
		if err != nil {
			return fmt.Errorf("upserting package: %w", err)
		}

		for _, v := range in.Versions {
			exists, err := qtx.NpmVersionExists(ctx, db.NpmVersionExistsParams{
				ProjectID:  in.ProjectID,
				Repository: in.Repository,
				Name:       in.Name,
				Version:    v.Version,
			})
			if err != nil {
				return fmt.Errorf("checking version: %w", err)
			}
			if exists > 0 {
				return ErrVersionExists
			}
			if err := qtx.InsertNpmVersion(ctx, db.InsertNpmVersionParams{
				ID:            id.New("npmver"),
				PackageID:     pkgID,
				Version:       v.Version,
				Manifest:      v.Manifest,
				TarballDigest: v.TarballDigest,
				TarballSize:   v.TarballSize,
				Shasum:        v.Shasum,
				Integrity:     v.Integrity,
				CreatedAt:     ts,
			}); err != nil {
				return fmt.Errorf("inserting version: %w", err)
			}
		}

		for tag, version := range in.DistTags {
			if err := qtx.UpsertNpmDistTag(ctx, db.UpsertNpmDistTagParams{
				PackageID: pkgID,
				Tag:       tag,
				Version:   version,
				UpdatedAt: ts,
			}); err != nil {
				return fmt.Errorf("setting dist-tag: %w", err)
			}
		}

		versions := make([]string, 0, len(in.Versions))
		for _, v := range in.Versions {
			versions = append(versions, v.Version)
		}
		return s.audit(ctx, qtx, in.ProjectID, in.Actor, "npm.publish", "package", in.Name, ts,
			map[string]string{"repository": in.Repository, "versions": joinComma(versions)})
	})
}

// storedVersion is a version read back for the packument: its number, the
// verbatim metadata document npm published, and the authoritative tarball
// digests the handler stamps into dist.
type storedVersion struct {
	Version   string
	Manifest  []byte
	Shasum    string
	Integrity string
}

// packument returns everything needed to rebuild a package's packument: its
// versions (oldest first) and current dist-tags. An empty package (no versions)
// is reported as ErrPackageNotFound.
func (s *packageStore) packument(ctx context.Context, projectID, repo, name string) ([]storedVersion, map[string]string, error) {
	rows, err := s.q.ListNpmVersions(ctx, db.ListNpmVersionsParams{ProjectID: projectID, Repository: repo, Name: name})
	if err != nil {
		return nil, nil, fmt.Errorf("listing versions: %w", err)
	}
	if len(rows) == 0 {
		return nil, nil, ErrPackageNotFound
	}
	versions := make([]storedVersion, 0, len(rows))
	for _, r := range rows {
		versions = append(versions, storedVersion{
			Version:   r.Version,
			Manifest:  r.Manifest,
			Shasum:    r.Shasum,
			Integrity: r.Integrity,
		})
	}

	tags, err := s.distTags(ctx, projectID, repo, name)
	if err != nil {
		return nil, nil, err
	}
	return versions, tags, nil
}

// tarball returns the blob digest and size for one version's tarball.
func (s *packageStore) tarball(ctx context.Context, projectID, repo, name, version string) (digest string, size int64, err error) {
	row, err := s.q.GetNpmTarball(ctx, db.GetNpmTarballParams{ProjectID: projectID, Repository: repo, Name: name, Version: version})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, ErrPackageNotFound
		}
		return "", 0, fmt.Errorf("getting tarball: %w", err)
	}
	return row.TarballDigest, row.TarballSize, nil
}

// distTags returns the package's current dist-tags as tag -> version.
func (s *packageStore) distTags(ctx context.Context, projectID, repo, name string) (map[string]string, error) {
	rows, err := s.q.ListNpmDistTags(ctx, db.ListNpmDistTagsParams{ProjectID: projectID, Repository: repo, Name: name})
	if err != nil {
		return nil, fmt.Errorf("listing dist-tags: %w", err)
	}
	tags := make(map[string]string, len(rows))
	for _, r := range rows {
		tags[r.Tag] = r.Version
	}
	return tags, nil
}

// setDistTag points a tag at a version, auditing the change. The package must
// exist; an unknown package is ErrPackageNotFound.
func (s *packageStore) setDistTag(ctx context.Context, projectID, repo, name, tag, version, actor string) error {
	ts := s.now().Format(time.RFC3339Nano)
	pkgID, err := s.packageID(ctx, projectID, repo, name)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(qtx *db.Queries) error {
		if err := qtx.UpsertNpmDistTag(ctx, db.UpsertNpmDistTagParams{
			PackageID: pkgID,
			Tag:       tag,
			Version:   version,
			UpdatedAt: ts,
		}); err != nil {
			return fmt.Errorf("setting dist-tag: %w", err)
		}
		return s.audit(ctx, qtx, projectID, actor, "npm.disttag.set", "disttag", name, ts,
			map[string]string{"repository": repo, "tag": tag, "version": version})
	})
}

// deleteDistTag removes a tag from a package, auditing the change.
func (s *packageStore) deleteDistTag(ctx context.Context, projectID, repo, name, tag, actor string) error {
	ts := s.now().Format(time.RFC3339Nano)
	pkgID, err := s.packageID(ctx, projectID, repo, name)
	if err != nil {
		return err
	}
	return s.inTx(ctx, func(qtx *db.Queries) error {
		n, err := qtx.DeleteNpmDistTag(ctx, db.DeleteNpmDistTagParams{PackageID: pkgID, Tag: tag})
		if err != nil {
			return fmt.Errorf("deleting dist-tag: %w", err)
		}
		if n == 0 {
			return ErrPackageNotFound
		}
		return s.audit(ctx, qtx, projectID, actor, "npm.disttag.delete", "disttag", name, ts,
			map[string]string{"repository": repo, "tag": tag})
	})
}

// packageID resolves a package to its row id, or ErrPackageNotFound.
func (s *packageStore) packageID(ctx context.Context, projectID, repo, name string) (string, error) {
	row, err := s.q.GetNpmPackage(ctx, db.GetNpmPackageParams{ProjectID: projectID, Repository: repo, Name: name})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrPackageNotFound
		}
		return "", fmt.Errorf("getting package: %w", err)
	}
	return row.ID, nil
}

// inTx runs fn inside a transaction, committing on success and rolling back on
// any error.
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

// audit writes one audit entry inside the caller's transaction, so a mutation
// and its record commit together.
func (s *packageStore) audit(ctx context.Context, qtx *db.Queries, projectID, actor, action, targetType, targetID, ts string, meta map[string]string) error {
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

func joinComma(vs []string) string {
	out := ""
	for i, v := range vs {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}
