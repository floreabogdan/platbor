package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/registry/usage"
)

// projectsHandler serves the /api/v1/projects endpoints. It stays thin: decode,
// call the service, encode — mapping domain errors to status codes.
type projectsHandler struct {
	svc   *project.Service
	auth  *auth.Service
	usage *usage.Computer
	log   *slog.Logger
}

func (h projectsHandler) mount(r chi.Router) {
	r.Get("/", h.list)
	r.Post("/", h.create)
	// Per-project storage: current usage and the quota, both a management concern.
	r.Route("/{project}", func(r chi.Router) {
		r.Use(requireProjectManage(h.svc, h.auth))
		r.Get("/usage", h.getUsage)
		r.Put("/quota", h.setQuota)
		r.Put("/verification-key", h.setVerificationKey)
	})
}

// projectResponse is the API view of a project (camelCase, RFC 3339 timestamps).
// A project is a tenant that contains typed repositories; allowAutoCreate governs
// whether a push to an unknown repo path auto-creates a local repo of that format.
type projectResponse struct {
	ID              string `json:"id"`
	Key             string `json:"key"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	AllowAutoCreate bool   `json:"allowAutoCreate"`
	QuotaBytes      int64  `json:"quotaBytes"` // 0 = unlimited
	VerificationKey string `json:"verificationKey,omitempty"`
	// VerificationKeyConfigured is a convenience flag so the UI can show trust
	// state without echoing the (public) key everywhere.
	VerificationKeyConfigured bool      `json:"verificationKeyConfigured"`
	CreatedAt                 time.Time `json:"createdAt"`
	UpdatedAt                 time.Time `json:"updatedAt"`
}

func toProjectResponse(p project.Project) projectResponse {
	return projectResponse{
		ID:                        p.ID,
		Key:                       p.Key,
		Name:                      p.Name,
		Description:               p.Description,
		AllowAutoCreate:           p.AllowAutoCreate,
		QuotaBytes:                p.QuotaBytes,
		VerificationKey:           p.VerificationKey,
		VerificationKeyConfigured: p.VerificationKey != "",
		CreatedAt:                 p.CreatedAt,
		UpdatedAt:                 p.UpdatedAt,
	}
}

type listProjectsResponse struct {
	Projects   []projectResponse `json:"projects"`
	NextCursor string            `json:"nextCursor,omitempty"`
}

func (h projectsHandler) list(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")

	limit := 0
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			writeProblem(w, http.StatusBadRequest, "Invalid limit", "limit must be an integer")
			return
		}
		limit = n
	}

	page, err := h.svc.List(r.Context(), cursor, limit)
	if err != nil {
		h.log.Error("listing projects", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}

	items := make([]projectResponse, 0, len(page.Projects))
	for _, p := range page.Projects {
		items = append(items, toProjectResponse(p))
	}
	writeJSON(w, h.log, http.StatusOK, listProjectsResponse{Projects: items, NextCursor: page.NextCursor})
}

type createProjectRequest struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// AllowAutoCreate defaults to true (zero-config push auto-creates repos).
	// Pass false to require repositories to be created before pushing.
	AllowAutoCreate *bool `json:"allowAutoCreate,omitempty"`
	// QuotaBytes caps the project's logical storage; 0 (default) is unlimited.
	QuotaBytes int64 `json:"quotaBytes,omitempty"`
}

func (h projectsHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}

	allowAutoCreate := true
	if req.AllowAutoCreate != nil {
		allowAutoCreate = *req.AllowAutoCreate
	}

	created, err := h.svc.Create(r.Context(), project.CreateInput{
		Key:             req.Key,
		Name:            req.Name,
		Description:     req.Description,
		AllowAutoCreate: allowAutoCreate,
		QuotaBytes:      req.QuotaBytes,
		Actor:           actorFrom(r),
	})
	if err != nil {
		h.writeCreateError(w, err)
		return
	}

	// The creator owns the project: enroll them as its admin so a non-instance
	// admin can manage what they just created. Best-effort — instance admins
	// retain access regardless, so a failure here is logged, not fatal.
	if user, ok := userFromContext(r.Context()); ok {
		if err := h.auth.SetMember(r.Context(), created.ID, user.ID, auth.RoleAdmin); err != nil {
			h.log.Error("enrolling project creator as admin", slog.String("error", err.Error()))
		}
	}

	w.Header().Set("Location", "/api/v1/projects/"+created.Key)
	writeJSON(w, h.log, http.StatusCreated, toProjectResponse(created))
}

type projectUsageResponse struct {
	ProjectKey string `json:"projectKey"`
	QuotaBytes int64  `json:"quotaBytes"` // 0 = unlimited
	UsedBytes  int64  `json:"usedBytes"`
}

// getUsage reports a project's current logical storage against its quota.
func (h projectsHandler) getUsage(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "project")
	proj, err := h.svc.GetByKey(r.Context(), key)
	if err != nil {
		h.writeProjectLookupError(w, err)
		return
	}
	used, err := h.usage.ProjectUsage(r.Context(), proj.ID)
	if err != nil {
		h.log.Error("computing project usage", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	writeJSON(w, h.log, http.StatusOK, projectUsageResponse{ProjectKey: proj.Key, QuotaBytes: proj.QuotaBytes, UsedBytes: used})
}

type setQuotaRequest struct {
	QuotaBytes int64 `json:"quotaBytes"` // 0 = unlimited
}

// setQuota sets a project's storage quota (0 = unlimited).
func (h projectsHandler) setQuota(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "project")
	var req setQuotaRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}
	if err := h.svc.SetQuota(r.Context(), key, req.QuotaBytes); err != nil {
		var ve *project.ValidationError
		if errors.As(err, &ve) {
			writeProblem(w, http.StatusBadRequest, "Invalid quota", ve.Error())
			return
		}
		h.log.Error("setting project quota", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	proj, err := h.svc.GetByKey(r.Context(), key)
	if err != nil {
		h.writeProjectLookupError(w, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, toProjectResponse(proj))
}

type setVerificationKeyRequest struct {
	// VerificationKey is a PEM public key, or "" to clear it.
	VerificationKey string `json:"verificationKey"`
}

// setVerificationKey sets (or clears) a project's cosign verification public key.
func (h projectsHandler) setVerificationKey(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "project")
	var req setVerificationKeyRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}
	if err := h.svc.SetVerificationKey(r.Context(), key, req.VerificationKey); err != nil {
		var ve *project.ValidationError
		if errors.As(err, &ve) {
			writeProblem(w, http.StatusBadRequest, "Invalid verification key", ve.Error())
			return
		}
		h.log.Error("setting verification key", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	proj, err := h.svc.GetByKey(r.Context(), key)
	if err != nil {
		h.writeProjectLookupError(w, err)
		return
	}
	writeJSON(w, h.log, http.StatusOK, toProjectResponse(proj))
}

func (h projectsHandler) writeProjectLookupError(w http.ResponseWriter, err error) {
	if errors.Is(err, project.ErrNotFound) {
		writeProblem(w, http.StatusNotFound, "Project not found", "no project with that key")
		return
	}
	h.log.Error("looking up project", slog.String("error", err.Error()))
	writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
}

func (h projectsHandler) writeCreateError(w http.ResponseWriter, err error) {
	var ve *project.ValidationError
	switch {
	case errors.As(err, &ve):
		writeProblem(w, http.StatusBadRequest, "Invalid project", ve.Error())
	case errors.Is(err, project.ErrDuplicateKey):
		writeProblem(w, http.StatusConflict, "Project key already exists", "choose a different key")
	default:
		h.log.Error("creating project", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
	}
}

// actorFrom identifies who is performing a request for the audit log. Domain
// routes run behind requireUser, so a user is always present; "system" is a
// defensive fallback.
func actorFrom(r *http.Request) string {
	if user, ok := userFromContext(r.Context()); ok {
		return user.Username
	}
	return "system"
}
