// Package repository owns typed artifact repositories: the format-specific,
// configured containers that live inside a project. A project is the tenant
// boundary; a repository is where artifacts of one format (oci, npm, nuget,
// generic) actually live, created and configured before anything is pushed.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// Format is the artifact format a repository holds.
type Format string

const (
	FormatOCI       Format = "oci"
	FormatNPM       Format = "npm"
	FormatNuGet     Format = "nuget"
	FormatGeneric   Format = "generic"
	FormatPyPI      Format = "pypi"
	FormatMaven     Format = "maven"
	FormatGo        Format = "go"
	FormatCargo     Format = "cargo"
	FormatRubyGems  Format = "rubygems"
	FormatTerraform Format = "terraform"
)

// Mode is whether a repository stores its own content or proxies an upstream.
type Mode string

const (
	ModeLocal Mode = "local"
	ModeProxy Mode = "proxy"
	// ModeVirtual aggregates several member repositories behind one URL: a read is
	// resolved against the members in order, and writes are rejected.
	ModeVirtual Mode = "virtual"
)

var (
	// ErrNotFound means no repository matched.
	ErrNotFound = errors.New("repository not found")
	// ErrDuplicateKey means a repository with that key already exists in the project.
	ErrDuplicateKey = errors.New("repository key already exists")
	// ErrFormatMismatch means the repository exists but holds a different format
	// than the request (e.g. an npm push into an OCI repository).
	ErrFormatMismatch = errors.New("repository format mismatch")
)

// ValidationError describes invalid input.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

// Upstream is the mirrored registry for a proxy repository.
type Upstream struct {
	URL      string
	Username string
	Password string
}

// Retention is a repository's cleanup policy.
type Retention struct {
	KeepLast       int
	DeleteUntagged bool
}

// Repository is the domain view.
type Repository struct {
	ID        string
	ProjectID string
	Key       string
	Name      string
	Format    Format
	Mode      Mode
	Upstream  *Upstream // set only for proxy repositories; password never surfaced by the API
	Retention Retention
	// MemberKeys is the ordered member repository keys of a virtual repository;
	// empty for local and proxy repositories. Populated on demand (not by Get).
	MemberKeys []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ProjectUsageFunc reports a project's current logical storage in bytes. The
// size of every format lives above core, so it is injected; nil disables quota
// enforcement (used only for the shared write-path service).
type ProjectUsageFunc func(ctx context.Context, projectID string) (int64, error)

// Service manages repositories.
type Service struct {
	db    *sql.DB
	q     *db.Queries
	now   func() time.Time
	usage ProjectUsageFunc
}

// NewService wires the repository service to an open database.
func NewService(sqlDB *sql.DB) *Service {
	return &Service{db: sqlDB, q: db.New(sqlDB), now: func() time.Time { return time.Now().UTC() }}
}

// SetUsageFunc installs the storage-usage computer used to enforce per-project
// quotas on writes. Wire it on the service the format adapters share; leaving it
// unset (management/read services) simply skips quota checks.
func (s *Service) SetUsageFunc(f ProjectUsageFunc) { s.usage = f }

// enforceQuota rejects a write when the project is at or over its storage quota.
// It is a no-op when no usage computer is installed or the project is unlimited.
// The at-or-over check lets the crossing write through and blocks the next one —
// simple, predictable semantics. It surfaces as a *ValidationError so every
// adapter reports it uniformly.
func (s *Service) enforceQuota(ctx context.Context, projectID string) error {
	if s.usage == nil {
		return nil
	}
	proj, err := s.q.GetProjectByID(ctx, projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil // a missing project is reported by the caller's own resolution
		}
		return fmt.Errorf("loading project quota: %w", err)
	}
	if proj.QuotaBytes <= 0 {
		return nil // unlimited
	}
	used, err := s.usage(ctx, projectID)
	if err != nil {
		return fmt.Errorf("computing project usage: %w", err)
	}
	if used >= proj.QuotaBytes {
		return &ValidationError{fmt.Sprintf("project storage quota exceeded: %d of %d bytes used", used, proj.QuotaBytes)}
	}
	return nil
}

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// CreateInput is a new repository.
type CreateInput struct {
	ProjectID string
	Key       string
	Name      string
	Format    Format
	Mode      Mode
	Upstream  *Upstream
	Retention Retention
	// MemberKeys is the ordered member repository keys for a virtual repository.
	MemberKeys []string
	Actor      string
}

// Create validates and inserts a repository with an audit entry.
func (s *Service) Create(ctx context.Context, in CreateInput) (Repository, error) {
	if err := validate(in.Key, in.Name, in.Format, in.Mode, in.Upstream); err != nil {
		return Repository{}, err
	}
	// A virtual repository's members are resolved and validated before the write so
	// a bad member list fails cleanly without a half-created aggregate.
	var memberIDs []string
	if in.Mode == ModeVirtual {
		var err error
		memberIDs, err = s.resolveMembers(ctx, in.ProjectID, in.Key, in.MemberKeys)
		if err != nil {
			return Repository{}, err
		}
	}
	ts := s.now().Format(time.RFC3339Nano)
	repoID := id.New("repo")
	up := Upstream{}
	if in.Upstream != nil {
		up = *in.Upstream
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Repository{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.q.WithTx(tx)

	row, err := qtx.CreateRepository(ctx, db.CreateRepositoryParams{
		ID:               repoID,
		ProjectID:        in.ProjectID,
		Key:              in.Key,
		Name:             in.Name,
		Format:           string(in.Format),
		Mode:             string(in.Mode),
		UpstreamUrl:      up.URL,
		UpstreamUsername: up.Username,
		UpstreamPassword: up.Password,
		KeepLast:         int64(in.Retention.KeepLast),
		DeleteUntagged:   boolToInt(in.Retention.DeleteUntagged),
		CreatedAt:        ts,
		UpdatedAt:        ts,
	})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return Repository{}, ErrDuplicateKey
		}
		return Repository{}, fmt.Errorf("creating repository: %w", err)
	}
	for pos, memberID := range memberIDs {
		if err := qtx.AddVirtualMember(ctx, db.AddVirtualMemberParams{
			VirtualRepoID: repoID, MemberRepoID: memberID, Position: int64(pos),
		}); err != nil {
			return Repository{}, fmt.Errorf("adding virtual member: %w", err)
		}
	}
	if err := s.audit(ctx, qtx, in.ProjectID, in.Actor, "repository.create", repoID, ts,
		fmt.Sprintf(`{"key":%q,"format":%q,"mode":%q}`, in.Key, in.Format, in.Mode)); err != nil {
		return Repository{}, err
	}
	if err := tx.Commit(); err != nil {
		return Repository{}, fmt.Errorf("commit: %w", err)
	}
	repo := toDomain(row)
	repo.MemberKeys = in.MemberKeys
	return repo, nil
}

// resolveMembers validates a virtual repository's member keys and returns their
// repository IDs in order. Members must be existing OCI local/proxy repositories
// in the same project; virtual repositories do not nest, and duplicates and the
// aggregate's own key are rejected.
func (s *Service) resolveMembers(ctx context.Context, projectID, ownKey string, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, &ValidationError{"a virtual repository requires at least one member"}
	}
	seen := make(map[string]struct{}, len(keys))
	ids := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" {
			return nil, &ValidationError{"member repository key must not be empty"}
		}
		if k == ownKey {
			return nil, &ValidationError{"a virtual repository cannot contain itself"}
		}
		if _, dup := seen[k]; dup {
			return nil, &ValidationError{"duplicate member repository: " + k}
		}
		seen[k] = struct{}{}
		member, err := s.Get(ctx, projectID, k)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return nil, &ValidationError{"member repository does not exist: " + k}
			}
			return nil, err
		}
		if member.Format != FormatOCI {
			return nil, &ValidationError{"member must be an OCI repository: " + k}
		}
		if member.Mode == ModeVirtual {
			return nil, &ValidationError{"a virtual repository cannot contain another virtual repository: " + k}
		}
		ids = append(ids, member.ID)
	}
	return ids, nil
}

// Members returns a virtual repository's member repositories in configured order.
func (s *Service) Members(ctx context.Context, virtualRepoID string) ([]Repository, error) {
	rows, err := s.q.ListVirtualMembers(ctx, virtualRepoID)
	if err != nil {
		return nil, fmt.Errorf("listing virtual members: %w", err)
	}
	out := make([]Repository, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomain(r))
	}
	return out, nil
}

// Get returns a repository by project and key, or ErrNotFound.
func (s *Service) Get(ctx context.Context, projectID, key string) (Repository, error) {
	row, err := s.q.GetRepository(ctx, db.GetRepositoryParams{ProjectID: projectID, Key: key})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Repository{}, ErrNotFound
		}
		return Repository{}, fmt.Errorf("getting repository: %w", err)
	}
	return toDomain(row), nil
}

// GetForFormat returns a repository for a read, requiring it to hold the given
// format. Missing → ErrNotFound; wrong format → ErrFormatMismatch.
func (s *Service) GetForFormat(ctx context.Context, projectID, key string, format Format) (Repository, error) {
	repo, err := s.Get(ctx, projectID, key)
	if err != nil {
		return Repository{}, err
	}
	if repo.Format != format {
		return Repository{}, ErrFormatMismatch
	}
	return repo, nil
}

// ResolveOrCreate resolves the repository for a write. If it exists it must hold
// the given format (else ErrFormatMismatch). If it does not exist, it is
// auto-created as a local repository of that format when allowAutoCreate is set;
// otherwise ErrNotFound is returned (the repository must be created first).
func (s *Service) ResolveOrCreate(ctx context.Context, projectID, key string, format Format, actor string, allowAutoCreate bool) (Repository, error) {
	if err := s.enforceQuota(ctx, projectID); err != nil {
		return Repository{}, err
	}
	repo, err := s.Get(ctx, projectID, key)
	switch {
	case err == nil:
		if repo.Format != format {
			return Repository{}, ErrFormatMismatch
		}
		return repo, nil
	case !errors.Is(err, ErrNotFound):
		return Repository{}, err
	}
	if !allowAutoCreate {
		return Repository{}, ErrNotFound
	}
	created, err := s.Create(ctx, CreateInput{
		ProjectID: projectID, Key: key, Name: key, Format: format, Mode: ModeLocal, Actor: actor,
	})
	if errors.Is(err, ErrDuplicateKey) {
		// Created concurrently by another push; fetch it and re-check the format.
		return s.GetForFormat(ctx, projectID, key, format)
	}
	return created, err
}

// UpdateInput is the desired repository configuration for an update.
type UpdateInput struct {
	Name      string
	Upstream  *Upstream
	Retention Retention
}

// Update replaces a repository's name, upstream, and retention policy, auditing
// the change. The format and mode are immutable.
func (s *Service) Update(ctx context.Context, projectID, key string, in UpdateInput, actor string) (Repository, error) {
	existing, err := s.Get(ctx, projectID, key)
	if err != nil {
		return Repository{}, err
	}
	if in.Name == "" {
		in.Name = existing.Name
	}
	up := Upstream{}
	if in.Upstream != nil {
		up = *in.Upstream
	} else if existing.Upstream != nil {
		up = *existing.Upstream
	}
	ts := s.now().Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Repository{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.q.WithTx(tx)

	row, err := qtx.UpdateRepository(ctx, db.UpdateRepositoryParams{
		Name:             in.Name,
		UpstreamUrl:      up.URL,
		UpstreamUsername: up.Username,
		UpstreamPassword: up.Password,
		KeepLast:         int64(in.Retention.KeepLast),
		DeleteUntagged:   boolToInt(in.Retention.DeleteUntagged),
		UpdatedAt:        ts,
		ID:               existing.ID,
	})
	if err != nil {
		return Repository{}, fmt.Errorf("updating repository: %w", err)
	}
	if err := s.audit(ctx, qtx, projectID, actor, "repository.update", existing.ID, ts, fmt.Sprintf(`{"key":%q}`, key)); err != nil {
		return Repository{}, err
	}
	if err := tx.Commit(); err != nil {
		return Repository{}, fmt.Errorf("commit: %w", err)
	}
	return toDomain(row), nil
}

// List returns every repository in a project.
func (s *Service) List(ctx context.Context, projectID string) ([]Repository, error) {
	rows, err := s.q.ListRepositoriesByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("listing repositories: %w", err)
	}
	out := make([]Repository, 0, len(rows))
	for _, r := range rows {
		out = append(out, toDomain(r))
	}
	return out, nil
}

// Delete removes a repository (its artifacts cascade), auditing it.
func (s *Service) Delete(ctx context.Context, projectID, key, actor string) error {
	ts := s.now().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.q.WithTx(tx)

	n, err := qtx.DeleteRepository(ctx, db.DeleteRepositoryParams{ProjectID: projectID, Key: key})
	if err != nil {
		return fmt.Errorf("deleting repository: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	if err := s.audit(ctx, qtx, projectID, actor, "repository.delete", key, ts, fmt.Sprintf(`{"key":%q}`, key)); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) audit(ctx context.Context, qtx *db.Queries, projectID, actor, action, targetID, ts, meta string) error {
	if actor == "" {
		actor = "system"
	}
	if _, err := qtx.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		Actor:      actor,
		Action:     action,
		TargetType: "repository",
		TargetID:   targetID,
		Metadata:   meta,
		CreatedAt:  ts,
	}); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}
	return nil
}

func validate(key, name string, format Format, mode Mode, up *Upstream) error {
	if !keyPattern.MatchString(key) {
		return &ValidationError{"key must be lowercase alphanumeric with hyphens (max 63 chars)"}
	}
	if name == "" {
		return &ValidationError{"name is required"}
	}
	switch format {
	case FormatOCI, FormatNPM, FormatNuGet, FormatGeneric, FormatPyPI, FormatMaven, FormatGo, FormatCargo, FormatRubyGems, FormatTerraform:
	default:
		return &ValidationError{"format must be one of oci, npm, nuget, generic, pypi, maven, go, cargo, rubygems, terraform"}
	}
	switch mode {
	case ModeLocal:
	case ModeProxy:
		if up == nil || up.URL == "" {
			return &ValidationError{"a proxy repository requires an upstream url"}
		}
	case ModeVirtual:
		if format != FormatOCI {
			return &ValidationError{"virtual repositories are supported for the oci format only"}
		}
		if up != nil && up.URL != "" {
			return &ValidationError{"a virtual repository has members, not an upstream"}
		}
	default:
		return &ValidationError{"mode must be local, proxy, or virtual"}
	}
	return nil
}

func toDomain(r db.Repository) Repository {
	created, _ := time.Parse(time.RFC3339Nano, r.CreatedAt)
	updated, _ := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	repo := Repository{
		ID:        r.ID,
		ProjectID: r.ProjectID,
		Key:       r.Key,
		Name:      r.Name,
		Format:    Format(r.Format),
		Mode:      Mode(r.Mode),
		Retention: Retention{KeepLast: int(r.KeepLast), DeleteUntagged: r.DeleteUntagged != 0},
		CreatedAt: created,
		UpdatedAt: updated,
	}
	if Mode(r.Mode) == ModeProxy {
		repo.Upstream = &Upstream{URL: r.UpstreamUrl, Username: r.UpstreamUsername, Password: r.UpstreamPassword}
	}
	return repo
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
