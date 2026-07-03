package nuget

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

var (
	errProjectNotFound = errors.New("project not found")
	// ErrPackageNotFound is returned when a package or version is absent.
	ErrPackageNotFound = errors.New("package not found")
	// ErrVersionExists is returned when a push targets an existing version.
	ErrVersionExists = errors.New("version already exists")
)

// packageStore is the NuGet adapter's project-scoped metadata layer.
type packageStore struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

func newPackageStore(sqlDB *sql.DB) *packageStore {
	return &packageStore{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

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

func (s *packageStore) isProxy(ctx context.Context, projectID string) (bool, error) {
	_, err := s.q.GetProxyByProjectID(ctx, projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("loading proxy config: %w", err)
	}
	return true, nil
}

// pushInput is one `dotnet nuget push`: a single version of one package.
type pushInput struct {
	ProjectID   string
	IDOriginal  string
	Version     string
	NupkgDigest string
	NupkgSize   int64
	Nuspec      []byte
	Actor       string
}

// push stores the package and its new version atomically, with an audit entry.
// A re-push of an existing version returns ErrVersionExists.
func (s *packageStore) push(ctx context.Context, in pushInput) error {
	ts := s.now().Format(time.RFC3339Nano)
	idLower := strings.ToLower(in.IDOriginal)
	verLower := strings.ToLower(in.Version)

	return s.inTx(ctx, func(qtx *db.Queries) error {
		pkgID, err := qtx.UpsertNugetPackage(ctx, db.UpsertNugetPackageParams{
			ID:         id.New("nugetpkg"),
			ProjectID:  in.ProjectID,
			IDLower:    idLower,
			IDOriginal: in.IDOriginal,
			CreatedAt:  ts,
			UpdatedAt:  ts,
		})
		if err != nil {
			return fmt.Errorf("upserting package: %w", err)
		}
		exists, err := qtx.NugetVersionExists(ctx, db.NugetVersionExistsParams{
			ProjectID: in.ProjectID, IDLower: idLower, VersionLower: verLower,
		})
		if err != nil {
			return fmt.Errorf("checking version: %w", err)
		}
		if exists > 0 {
			return ErrVersionExists
		}
		if err := qtx.InsertNugetVersion(ctx, db.InsertNugetVersionParams{
			ID:           id.New("nugetver"),
			PackageID:    pkgID,
			Version:      in.Version,
			VersionLower: verLower,
			NupkgDigest:  in.NupkgDigest,
			NupkgSize:    in.NupkgSize,
			Nuspec:       in.Nuspec,
			CreatedAt:    ts,
		}); err != nil {
			return fmt.Errorf("inserting version: %w", err)
		}
		return s.audit(ctx, qtx, in.ProjectID, in.Actor, "nuget.push", "package", in.IDOriginal, ts,
			map[string]string{"version": in.Version})
	})
}

// storedVersion is a version read back for the flat-container, registration, and
// download resources.
type storedVersion struct {
	Version      string
	VersionLower string
	NupkgDigest  string
	NupkgSize    int64
	Nuspec       []byte
	CreatedAt    time.Time
}

// versions returns every version of a package (oldest first), or
// ErrPackageNotFound when the package has none.
func (s *packageStore) versions(ctx context.Context, projectID, idLower string) ([]storedVersion, error) {
	rows, err := s.q.ListNugetVersions(ctx, db.ListNugetVersionsParams{ProjectID: projectID, IDLower: idLower})
	if err != nil {
		return nil, fmt.Errorf("listing versions: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrPackageNotFound
	}
	out := make([]storedVersion, 0, len(rows))
	for _, r := range rows {
		created, _ := time.Parse(time.RFC3339Nano, r.CreatedAt)
		out = append(out, storedVersion{
			Version:      r.Version,
			VersionLower: r.VersionLower,
			NupkgDigest:  r.NupkgDigest,
			NupkgSize:    r.NupkgSize,
			Nuspec:       r.Nuspec,
			CreatedAt:    created,
		})
	}
	return out, nil
}

// nupkg returns the blob digest and size for one version's .nupkg.
func (s *packageStore) nupkg(ctx context.Context, projectID, idLower, versionLower string) (string, int64, error) {
	row, err := s.q.GetNugetNupkg(ctx, db.GetNugetNupkgParams{ProjectID: projectID, IDLower: idLower, VersionLower: versionLower})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, ErrPackageNotFound
		}
		return "", 0, fmt.Errorf("getting nupkg: %w", err)
	}
	return row.NupkgDigest, row.NupkgSize, nil
}

// searchResult is one package hit for the search resource.
type searchResult struct {
	ID           string
	IDLower      string
	VersionCount int
}

// search returns packages in a project whose id contains query (case-insensitive).
func (s *packageStore) search(ctx context.Context, projectID, query string, take int) ([]searchResult, error) {
	pattern := "%" + strings.ToLower(query) + "%"
	rows, err := s.q.SearchNugetPackages(ctx, db.SearchNugetPackagesParams{
		ProjectID: projectID, IDLower: pattern, Limit: int64(take),
	})
	if err != nil {
		return nil, fmt.Errorf("searching: %w", err)
	}
	out := make([]searchResult, 0, len(rows))
	for _, r := range rows {
		out = append(out, searchResult{ID: r.IDOriginal, IDLower: r.IDLower, VersionCount: int(r.VersionCount)})
	}
	return out, nil
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
