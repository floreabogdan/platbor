package httpapi

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/core/repository"
)

// repositoriesHandler serves /api/v1/projects/{project}/repositories: the typed
// artifact repositories inside a project.
type repositoriesHandler struct {
	repos    *repository.Service
	projects *project.Service
	log      *slog.Logger
}

func (h repositoriesHandler) mount(r chi.Router) {
	r.Get("/", h.list)
	r.With(requireAdmin).Post("/", h.create)
	r.Get("/{repo}", h.get)
	r.With(requireAdmin).Delete("/{repo}", h.delete)
}

// --- wire shapes ---

type upstreamPayload struct {
	URL      string `json:"url"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type retentionPayload struct {
	KeepLast       int  `json:"keepLast"`
	DeleteUntagged bool `json:"deleteUntagged"`
}

type repoConfigResponse struct {
	Key       string           `json:"key"`
	Name      string           `json:"name"`
	Format    string           `json:"format"`
	Mode      string           `json:"mode"`
	Upstream  *upstreamPayload `json:"upstream,omitempty"` // password never returned
	Retention retentionPayload `json:"retention"`
	CreatedAt time.Time        `json:"createdAt"`
	UpdatedAt time.Time        `json:"updatedAt"`
}

func toRepoConfigResponse(r repository.Repository) repoConfigResponse {
	resp := repoConfigResponse{
		Key:       r.Key,
		Name:      r.Name,
		Format:    string(r.Format),
		Mode:      string(r.Mode),
		Retention: retentionPayload{KeepLast: r.Retention.KeepLast, DeleteUntagged: r.Retention.DeleteUntagged},
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
	if r.Upstream != nil {
		resp.Upstream = &upstreamPayload{URL: r.Upstream.URL, Username: r.Upstream.Username}
	}
	return resp
}

type createRepositoryRequest struct {
	Key       string           `json:"key"`
	Name      string           `json:"name"`
	Format    string           `json:"format"`
	Mode      string           `json:"mode"`
	Upstream  *upstreamPayload `json:"upstream"`
	Retention retentionPayload `json:"retention"`
}

// --- handlers ---

func (h repositoriesHandler) list(w http.ResponseWriter, r *http.Request) {
	proj, ok := h.resolveProject(w, r)
	if !ok {
		return
	}
	repos, err := h.repos.List(r.Context(), proj.ID)
	if err != nil {
		h.log.Error("listing repositories", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	items := make([]repoConfigResponse, 0, len(repos))
	for _, repo := range repos {
		items = append(items, toRepoConfigResponse(repo))
	}
	writeJSON(w, h.log, http.StatusOK, map[string]any{"repositories": items})
}

func (h repositoriesHandler) get(w http.ResponseWriter, r *http.Request) {
	proj, ok := h.resolveProject(w, r)
	if !ok {
		return
	}
	repo, err := h.repos.Get(r.Context(), proj.ID, chi.URLParam(r, "repo"))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeProblem(w, http.StatusNotFound, "Repository not found", "")
			return
		}
		h.log.Error("getting repository", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	writeJSON(w, h.log, http.StatusOK, toRepoConfigResponse(repo))
}

func (h repositoriesHandler) create(w http.ResponseWriter, r *http.Request) {
	proj, ok := h.resolveProject(w, r)
	if !ok {
		return
	}
	var req createRepositoryRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeProblem(w, http.StatusBadRequest, "Invalid request body", err.Error())
		return
	}
	in := repository.CreateInput{
		ProjectID: proj.ID,
		Key:       req.Key,
		Name:      req.Name,
		Format:    repository.Format(req.Format),
		Mode:      repository.Mode(req.Mode),
		Retention: repository.Retention{KeepLast: req.Retention.KeepLast, DeleteUntagged: req.Retention.DeleteUntagged},
		Actor:     actorFrom(r),
	}
	if req.Upstream != nil {
		in.Upstream = &repository.Upstream{URL: req.Upstream.URL, Username: req.Upstream.Username, Password: req.Upstream.Password}
	}
	repo, err := h.repos.Create(r.Context(), in)
	if err != nil {
		var ve *repository.ValidationError
		switch {
		case errors.As(err, &ve):
			writeProblem(w, http.StatusBadRequest, "Invalid repository", ve.Msg)
		case errors.Is(err, repository.ErrDuplicateKey):
			writeProblem(w, http.StatusConflict, "Duplicate key", "a repository with that key already exists")
		default:
			h.log.Error("creating repository", slog.String("error", err.Error()))
			writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		}
		return
	}
	writeJSON(w, h.log, http.StatusCreated, toRepoConfigResponse(repo))
}

func (h repositoriesHandler) delete(w http.ResponseWriter, r *http.Request) {
	proj, ok := h.resolveProject(w, r)
	if !ok {
		return
	}
	if err := h.repos.Delete(r.Context(), proj.ID, chi.URLParam(r, "repo"), actorFrom(r)); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			writeProblem(w, http.StatusNotFound, "Repository not found", "")
			return
		}
		h.log.Error("deleting repository", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h repositoriesHandler) resolveProject(w http.ResponseWriter, r *http.Request) (project.Project, bool) {
	proj, err := h.projects.GetByKey(r.Context(), chi.URLParam(r, "project"))
	if err != nil {
		if errors.Is(err, project.ErrNotFound) {
			writeProblem(w, http.StatusNotFound, "Project not found", "")
			return project.Project{}, false
		}
		h.log.Error("resolving project", slog.String("error", err.Error()))
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "")
		return project.Project{}, false
	}
	return proj, true
}
