package terraform

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
	// ErrModuleNotFound is returned when a module or version is absent.
	ErrModuleNotFound = errors.New("module not found")
	// ErrVersionExists is returned when an upload targets an existing version.
	ErrVersionExists = errors.New("module version already exists")
)

// moduleStore is the Terraform adapter's repository-scoped metadata layer.
type moduleStore struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

func newModuleStore(sqlDB *sql.DB) *moduleStore {
	return &moduleStore{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

func (s *moduleStore) resolveProject(ctx context.Context, key string) (id string, allowAutoCreate bool, err error) {
	row, err := s.q.GetProjectByKey(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, errProjectNotFound
		}
		return "", false, fmt.Errorf("resolving project %q: %w", key, err)
	}
	return row.ID, row.AllowAutoCreate != 0, nil
}

// projectID resolves a project key to its id (for the read protocol; namespace is
// the project key).
func (s *moduleStore) projectID(ctx context.Context, key string) (string, error) {
	pid, _, err := s.resolveProject(ctx, key)
	return pid, err
}

// uploadInput is one module archive upload.
type uploadInput struct {
	RepositoryID string
	ProjectID    string
	Name         string
	Provider     string
	Version      string
	BlobDigest   string
	Size         int64
	Actor        string
}

// upload stores a module version and its archive blob atomically, with an audit
// entry. A re-upload of an existing version is 409.
func (s *moduleStore) upload(ctx context.Context, in uploadInput) error {
	ts := s.now().Format(time.RFC3339Nano)
	exists, err := s.q.TerraformVersionExists(ctx, db.TerraformVersionExistsParams{RepositoryID: in.RepositoryID, Name: in.Name, Provider: in.Provider, Version: in.Version})
	if err != nil {
		return fmt.Errorf("checking version: %w", err)
	}
	if exists > 0 {
		return ErrVersionExists
	}
	return s.inTx(ctx, func(qtx *db.Queries) error {
		moduleID, err := qtx.UpsertTerraformModule(ctx, db.UpsertTerraformModuleParams{
			ID: id.New("tfmod"), RepositoryID: in.RepositoryID, Name: in.Name, Provider: in.Provider, CreatedAt: ts, UpdatedAt: ts,
		})
		if err != nil {
			return fmt.Errorf("upserting module: %w", err)
		}
		if err := qtx.InsertTerraformVersion(ctx, db.InsertTerraformVersionParams{
			ID: id.New("tfver"), ModuleID: moduleID, Version: in.Version, BlobDigest: in.BlobDigest, Size: in.Size, CreatedAt: ts,
		}); err != nil {
			return fmt.Errorf("inserting version: %w", err)
		}
		return s.audit(ctx, qtx, in.ProjectID, in.Actor, "terraform.upload", in.Name+"/"+in.Provider+"@"+in.Version, ts,
			map[string]string{"name": in.Name, "provider": in.Provider, "version": in.Version})
	})
}

// resolveModule resolves (project, name, provider) to a module id and its
// repository, for the read protocol.
func (s *moduleStore) resolveModule(ctx context.Context, projectID, name, provider string) (moduleID, repoID string, err error) {
	row, err := s.q.ResolveTerraformModule(ctx, db.ResolveTerraformModuleParams{ProjectID: projectID, Name: name, Provider: provider})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrModuleNotFound
		}
		return "", "", fmt.Errorf("resolving module: %w", err)
	}
	return row.ModuleID, row.RepositoryID, nil
}

func (s *moduleStore) listVersions(ctx context.Context, moduleID string) ([]string, error) {
	return s.q.ListTerraformVersions(ctx, moduleID)
}

// getVersion returns a module version's archive blob for download.
func (s *moduleStore) getVersion(ctx context.Context, moduleID, version string) (digest string, size int64, err error) {
	row, err := s.q.GetTerraformVersion(ctx, db.GetTerraformVersionParams{ModuleID: moduleID, Version: version})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", 0, ErrModuleNotFound
		}
		return "", 0, fmt.Errorf("getting version: %w", err)
	}
	return row.BlobDigest, row.Size, nil
}

func (s *moduleStore) inTx(ctx context.Context, fn func(*db.Queries) error) error {
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

func (s *moduleStore) audit(ctx context.Context, qtx *db.Queries, projectID, actor, action, targetID, ts string, meta map[string]string) error {
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
		TargetType: "module",
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
