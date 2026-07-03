// Package project owns the root scoping entity. Every artifact and catalog
// record hangs off a project, so this is the first domain service: it creates
// and lists projects and writes an audit entry for each mutation, transactionally.
package project

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
)

// Sentinel errors let callers (HTTP handlers) map failures to status codes
// without depending on the storage layer.
var (
	ErrNotFound     = errors.New("project not found")
	ErrDuplicateKey = errors.New("project key already exists")
)

// keyPattern constrains project keys to a URL- and protocol-safe slug: they
// appear in registry paths like /v2/<project>/<repo>, so they must be lowercase
// and free of separators that would confuse clients.
var keyPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,38}[a-z0-9])?$`)

// ValidationError describes a rejected input field. Handlers surface its
// message in an RFC 7807 problem response.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Upstream is a proxied registry a project mirrors. Password is write-only: it
// is accepted on create but never returned, so it must not leak through the API.
type Upstream struct {
	URL      string
	Username string
	Password string
}

// Project is the domain view: timestamps are real time.Time, not the strings
// the store keeps. Upstream is set only for pull-through proxy projects; it is
// nil for ordinary local projects.
type Project struct {
	ID          string
	Key         string
	Name        string
	Description string
	Upstream    *Upstream
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IsProxy reports whether the project is a pull-through proxy.
func (p Project) IsProxy() bool { return p.Upstream != nil }

// Service provides project operations backed by the metadata store.
type Service struct {
	db  *sql.DB
	q   *db.Queries
	now func() time.Time
}

// NewService wires the service to an open database.
func NewService(sqlDB *sql.DB) *Service {
	return &Service{
		db:  sqlDB,
		q:   db.New(sqlDB),
		now: func() time.Time { return time.Now().UTC() },
	}
}

// CreateInput is the data needed to create a project. Actor identifies who is
// performing the action for the audit log.
type CreateInput struct {
	Key         string
	Name        string
	Description string
	// Upstream, when set, makes the project a pull-through proxy of that registry.
	Upstream *Upstream
	Actor    string
}

// Create validates the input and, in a single transaction, inserts the project
// and its audit entry. A duplicate key returns ErrDuplicateKey; invalid input
// returns a *ValidationError.
func (s *Service) Create(ctx context.Context, in CreateInput) (Project, error) {
	if err := validateCreate(in); err != nil {
		return Project{}, err
	}

	now := s.now()
	ts := now.Format(time.RFC3339Nano)
	projectID := id.New("proj")

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.q.WithTx(tx)

	row, err := qtx.CreateProject(ctx, db.CreateProjectParams{
		ID:          projectID,
		Key:         in.Key,
		Name:        in.Name,
		Description: in.Description,
		CreatedAt:   ts,
		UpdatedAt:   ts,
	})
	if err != nil {
		if db.IsUniqueViolation(err) {
			return Project{}, ErrDuplicateKey
		}
		return Project{}, fmt.Errorf("creating project: %w", err)
	}

	if in.Upstream != nil {
		if err := qtx.CreateProxy(ctx, db.CreateProxyParams{
			ProjectID:   projectID,
			UpstreamUrl: in.Upstream.URL,
			Username:    in.Upstream.Username,
			Password:    in.Upstream.Password,
			CreatedAt:   ts,
			UpdatedAt:   ts,
		}); err != nil {
			return Project{}, fmt.Errorf("configuring proxy: %w", err)
		}
	}

	action := "project.create"
	metadata := "{}"
	if in.Upstream != nil {
		action = "project.create.proxy"
		metadata = fmt.Sprintf(`{"upstream":%q}`, in.Upstream.URL)
	}
	if _, err := qtx.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		Actor:      actorOrSystem(in.Actor),
		Action:     action,
		TargetType: "project",
		TargetID:   projectID,
		Metadata:   metadata,
		CreatedAt:  ts,
	}); err != nil {
		return Project{}, fmt.Errorf("writing audit entry: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Project{}, fmt.Errorf("commit: %w", err)
	}

	created, err := toDomain(row)
	if err != nil {
		return Project{}, err
	}
	created.Upstream = in.Upstream
	return created, nil
}

// GetByKey returns a single project, or ErrNotFound. A proxy project carries its
// upstream URL and username, but never the stored password.
func (s *Service) GetByKey(ctx context.Context, key string) (Project, error) {
	row, err := s.q.GetProjectByKey(ctx, key)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Project{}, ErrNotFound
		}
		return Project{}, fmt.Errorf("getting project %s: %w", key, err)
	}
	p, err := toDomain(row)
	if err != nil {
		return Project{}, err
	}
	proxyRow, err := s.q.GetProxyByProjectID(ctx, row.ID)
	switch {
	case err == nil:
		p.Upstream = upstreamFromRow(proxyRow)
	case !errors.Is(err, sql.ErrNoRows):
		return Project{}, fmt.Errorf("loading proxy config for %s: %w", key, err)
	}
	return p, nil
}

// upstreamFromRow maps a stored proxy row to the display view, deliberately
// dropping the password so it cannot leak through a read path.
func upstreamFromRow(row db.RegistryProxy) *Upstream {
	return &Upstream{URL: row.UpstreamUrl, Username: row.Username}
}

// Count returns the total number of projects (for the dashboard summary).
func (s *Service) Count(ctx context.Context) (int, error) {
	n, err := s.q.CountProjects(ctx)
	if err != nil {
		return 0, fmt.Errorf("counting projects: %w", err)
	}
	return int(n), nil
}

// Page is a slice of projects plus the cursor to fetch the next page, empty
// when the last page has been reached.
type Page struct {
	Projects   []Project
	NextCursor string
}

// List returns projects in key order using keyset pagination. An empty cursor
// starts at the beginning. limit is clamped to a sane range.
func (s *Service) List(ctx context.Context, cursor string, limit int) (Page, error) {
	limit = clampLimit(limit)

	// Fetch one extra row to know whether another page exists.
	rows, err := s.q.ListProjects(ctx, db.ListProjectsParams{
		Key:   cursor,
		Limit: int64(limit + 1),
	})
	if err != nil {
		return Page{}, fmt.Errorf("listing projects: %w", err)
	}

	var next string
	if len(rows) > limit {
		next = rows[limit-1].Key
		rows = rows[:limit]
	}

	// One lookup marks which of these projects are proxies, instead of a query
	// per row.
	proxyRows, err := s.q.ListProxies(ctx)
	if err != nil {
		return Page{}, fmt.Errorf("listing proxies: %w", err)
	}
	upstreams := make(map[string]*Upstream, len(proxyRows))
	for _, pr := range proxyRows {
		upstreams[pr.ProjectID] = upstreamFromRow(pr)
	}

	projects := make([]Project, 0, len(rows))
	for _, row := range rows {
		p, err := toDomain(row)
		if err != nil {
			return Page{}, err
		}
		p.Upstream = upstreams[row.ID]
		projects = append(projects, p)
	}
	return Page{Projects: projects, NextCursor: next}, nil
}

func validateCreate(in CreateInput) error {
	if !keyPattern.MatchString(in.Key) {
		return &ValidationError{
			Field:   "key",
			Message: "must be 1–40 chars, lowercase letters, digits, or hyphens, and start and end alphanumeric",
		}
	}
	if in.Name == "" {
		return &ValidationError{Field: "name", Message: "must not be empty"}
	}
	if len(in.Name) > 100 {
		return &ValidationError{Field: "name", Message: "must be at most 100 characters"}
	}
	if in.Upstream != nil {
		if err := validateUpstream(in.Upstream); err != nil {
			return err
		}
	}
	return nil
}

// validateUpstream checks a proxy's upstream URL is an absolute http(s) URL. A
// password without a username (or vice versa) is allowed — some registries take
// a token in one field — but the URL must be well-formed.
func validateUpstream(u *Upstream) error {
	parsed, err := url.Parse(u.URL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return &ValidationError{Field: "upstream.url", Message: "must be an absolute http(s) URL, e.g. https://registry-1.docker.io"}
	}
	return nil
}

const (
	defaultLimit = 50
	maxLimit     = 200
)

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}

func actorOrSystem(actor string) string {
	if actor == "" {
		return "system"
	}
	return actor
}

func toDomain(row db.Project) (Project, error) {
	created, err := time.Parse(time.RFC3339Nano, row.CreatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("parsing created_at for project %s: %w", row.ID, err)
	}
	updated, err := time.Parse(time.RFC3339Nano, row.UpdatedAt)
	if err != nil {
		return Project{}, fmt.Errorf("parsing updated_at for project %s: %w", row.ID, err)
	}
	return Project{
		ID:          row.ID,
		Key:         row.Key,
		Name:        row.Name,
		Description: row.Description,
		CreatedAt:   created,
		UpdatedAt:   updated,
	}, nil
}
