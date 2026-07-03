package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/id"
	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/registry"
)

// RetentionService stores per-project retention policies and applies them across
// every format's pruner. It is instance-wide and admin-driven, mirroring GC:
// pruning removes version/tag metadata, and the blobs those held are reclaimed
// by a later garbage-collection sweep.
type RetentionService struct {
	db      *sql.DB
	q       *db.Queries
	pruners []registry.Pruner
	now     func() time.Time
}

// NewRetentionService wires retention to the database and the format pruners.
func NewRetentionService(sqlDB *sql.DB, pruners ...registry.Pruner) *RetentionService {
	return &RetentionService{db: sqlDB, q: db.New(sqlDB), pruners: pruners, now: func() time.Time { return time.Now().UTC() }}
}

// Policy is a project's retention configuration.
type Policy struct {
	KeepLast       int  `json:"keepLast"`
	DeleteUntagged bool `json:"deleteUntagged"`
}

// getPolicy returns a project's policy (a zero policy when none is set).
func (s *RetentionService) getPolicy(ctx context.Context, projectID string) (Policy, error) {
	row, err := s.q.GetRetentionPolicy(ctx, projectID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Policy{}, nil
		}
		return Policy{}, fmt.Errorf("getting policy: %w", err)
	}
	return Policy{KeepLast: int(row.KeepLast), DeleteUntagged: row.DeleteUntagged != 0}, nil
}

// setPolicy upserts a project's policy and audits the change.
func (s *RetentionService) setPolicy(ctx context.Context, projectID string, p Policy, actor string) error {
	ts := s.now().Format(time.RFC3339Nano)
	untagged := int64(0)
	if p.DeleteUntagged {
		untagged = 1
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	qtx := s.q.WithTx(tx)
	if err := qtx.UpsertRetentionPolicy(ctx, db.UpsertRetentionPolicyParams{
		ProjectID: projectID, KeepLast: int64(p.KeepLast), DeleteUntagged: untagged, UpdatedAt: ts,
	}); err != nil {
		return fmt.Errorf("upserting policy: %w", err)
	}
	if _, err := qtx.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		ID:         id.New("audit"),
		ProjectID:  sql.NullString{String: projectID, Valid: true},
		Actor:      actor,
		Action:     "retention.configure",
		TargetType: "policy",
		TargetID:   projectID,
		Metadata:   fmt.Sprintf(`{"keepLast":%d,"deleteUntagged":%t}`, p.KeepLast, p.DeleteUntagged),
		CreatedAt:  ts,
	}); err != nil {
		return fmt.Errorf("writing audit entry: %w", err)
	}
	return tx.Commit()
}

// ProjectResult is what a run pruned for one project.
type ProjectResult struct {
	ProjectKey string `json:"projectKey"`
	Deleted    int    `json:"deleted"`
}

// RunReport summarizes a retention run across all policied projects.
type RunReport struct {
	DryRun   bool            `json:"dryRun"`
	Deleted  int             `json:"deleted"`
	Projects []ProjectResult `json:"projects"`
}

// Run applies every policied project's policy through all format pruners.
func (s *RetentionService) Run(ctx context.Context, dryRun bool, actor string) (RunReport, error) {
	policies, err := s.q.ListRetentionPolicies(ctx)
	if err != nil {
		return RunReport{}, fmt.Errorf("listing policies: %w", err)
	}
	report := RunReport{DryRun: dryRun}
	for _, pol := range policies {
		projectDeleted := 0
		for _, pruner := range s.pruners {
			n, err := pruner.Prune(ctx, pol.ProjectID, int(pol.KeepLast), pol.DeleteUntagged != 0, dryRun, actor)
			if err != nil {
				return report, fmt.Errorf("pruning project %s: %w", pol.ProjectKey, err)
			}
			projectDeleted += n
		}
		if projectDeleted > 0 {
			report.Projects = append(report.Projects, ProjectResult{ProjectKey: pol.ProjectKey, Deleted: projectDeleted})
			report.Deleted += projectDeleted
		}
	}
	return report, nil
}

// --- HTTP handlers (mounted on the registry route) ---

// getRetention returns a project's retention policy.
func (h registryHandler) getRetention(w http.ResponseWriter, r *http.Request) {
	proj, err := h.projects.GetByKey(r.Context(), chi.URLParam(r, "project"))
	if err != nil {
		h.projectError(w, err)
		return
	}
	policy, err := h.retention.getPolicy(r.Context(), proj.ID)
	if err != nil {
		h.log.Error("getting retention policy", "error", err.Error())
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	writeJSON(w, h.log, http.StatusOK, policy)
}

// setRetention updates a project's retention policy (admin only).
func (h registryHandler) setRetention(w http.ResponseWriter, r *http.Request) {
	proj, err := h.projects.GetByKey(r.Context(), chi.URLParam(r, "project"))
	if err != nil {
		h.projectError(w, err)
		return
	}
	var policy Policy
	if err := decodeJSON(w, r, &policy); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}
	if policy.KeepLast < 0 || policy.KeepLast > 100000 {
		writeProblem(w, http.StatusBadRequest, "Invalid policy", "keepLast must be between 0 and 100000")
		return
	}
	if err := h.retention.setPolicy(r.Context(), proj.ID, policy, actorFrom(r)); err != nil {
		h.log.Error("setting retention policy", "error", err.Error())
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	writeJSON(w, h.log, http.StatusOK, policy)
}

// runRetention applies retention across all policied projects (admin only).
func (h registryHandler) runRetention(w http.ResponseWriter, r *http.Request) {
	dryRun := r.URL.Query().Get("dryRun") == "true"
	report, err := h.retention.Run(r.Context(), dryRun, actorFrom(r))
	if err != nil {
		h.log.Error("running retention", "error", err.Error())
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	writeJSON(w, h.log, http.StatusOK, report)
}

// projectError maps a project lookup failure to a response.
func (h registryHandler) projectError(w http.ResponseWriter, err error) {
	if errors.Is(err, project.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Project not found", "")
		return
	}
	h.log.Error("resolving project", "error", err.Error())
	writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
}
