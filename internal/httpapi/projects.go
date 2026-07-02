package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/project"
)

// projectsHandler serves the /api/v1/projects endpoints. It stays thin: decode,
// call the service, encode — mapping domain errors to status codes.
type projectsHandler struct {
	svc *project.Service
	log *slog.Logger
}

func (h projectsHandler) mount(r chi.Router) {
	r.Get("/", h.list)
	r.Post("/", h.create)
}

// projectResponse is the API view of a project (camelCase, RFC 3339 timestamps).
type projectResponse struct {
	ID          string    `json:"id"`
	Key         string    `json:"key"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

func toProjectResponse(p project.Project) projectResponse {
	return projectResponse{
		ID:          p.ID,
		Key:         p.Key,
		Name:        p.Name,
		Description: p.Description,
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
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
}

func (h projectsHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createProjectRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}

	created, err := h.svc.Create(r.Context(), project.CreateInput{
		Key:         req.Key,
		Name:        req.Name,
		Description: req.Description,
		Actor:       actorFrom(r),
	})
	if err != nil {
		h.writeCreateError(w, err)
		return
	}

	w.Header().Set("Location", "/api/v1/projects/"+created.Key)
	writeJSON(w, h.log, http.StatusCreated, toProjectResponse(created))
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
