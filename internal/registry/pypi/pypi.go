// Package pypi implements the Python package index protocol under
// /pypi/<project>/<repo>. It speaks the PEP 503 "simple" index (for `pip
// install`), the legacy upload API (for `twine upload`), and a pull-through proxy
// of an upstream index (pypi.org). Distribution files share the content-
// addressable blob store; auth is HTTP Basic (a personal access token as the
// password, `__token__` or any username) or a bearer token.
//
// Configure clients with the repository's URL:
//
//	pip install   --index-url http://<host>/pypi/<project>/<repo>/simple/ <pkg>
//	twine upload  --repository-url http://<host>/pypi/<project>/<repo>/ dist/*
package pypi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
	"github.com/platbor/platbor/internal/registry"
)

// Adapter is the PyPI registry format.
type Adapter struct{}

// New returns the PyPI adapter.
func New() *Adapter { return &Adapter{} }

// Key implements registry.Adapter.
func (a *Adapter) Key() string { return "pypi" }

// Mount registers the PyPI routes. r is already scoped to /pypi.
func (a *Adapter) Mount(r chi.Router, deps registry.Deps) {
	h := &handler{
		blobs:    deps.Blobs,
		auth:     deps.Auth,
		store:    newPackageStore(deps.DB),
		repos:    deps.Repositories,
		upstream: newUpstreamClient(),
		log:      deps.Log,
	}
	r.Route("/{project}/{repo}", func(sub chi.Router) {
		sub.Use(h.requireAuth)
		// twine posts the distribution to the repository URL (trailing slash).
		sub.Post("/", h.upload)
		sub.Get("/simple/", h.simpleRoot)
		sub.Get("/simple/*", h.simpleProject)
		sub.Get("/files/*", h.download)
	})
}

type handler struct {
	blobs    blob.Store
	auth     *auth.Service
	store    *packageStore
	repos    *repository.Service
	upstream *upstreamClient
	log      *slog.Logger
}

// requireAuth authenticates via bearer token or Basic (password or PAT),
// challenging otherwise.
func (h *handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := h.authenticate(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Platbor"`)
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
	})
}

// authenticate resolves a bearer token or Basic credentials to a user. twine
// sends `__token__` + a PAT, or a username + password; pip sends Basic too.
func (h *handler) authenticate(r *http.Request) (auth.User, bool) {
	const bearer = "Bearer "
	if header := r.Header.Get("Authorization"); len(header) > len(bearer) && strings.EqualFold(header[:len(bearer)], bearer) {
		if user, err := h.auth.AuthenticateToken(r.Context(), header[len(bearer):]); err == nil {
			return user, true
		}
		return auth.User{}, false
	}
	if username, password, ok := r.BasicAuth(); ok && password != "" {
		if user, err := h.auth.AuthenticateToken(r.Context(), password); err == nil {
			return user, true
		} else if !errors.Is(err, auth.ErrInvalidToken) {
			h.log.Error("pypi token auth", slog.String("error", err.Error()))
		}
		if user, err := h.auth.Authenticate(r.Context(), username, password); err == nil {
			return user, true
		}
	}
	return auth.User{}, false
}

// authorize enforces the caller's project role: reads need reader, writes need
// maintainer (instance admins bypass). It writes a 403 and returns false on deny.
func (h *handler) authorize(w http.ResponseWriter, r *http.Request, projectID string, write bool) bool {
	action := auth.ActionRead
	if write {
		action = auth.ActionWrite
	}
	user, _ := userFromContext(r.Context())
	if err := h.auth.Authorize(r.Context(), user, projectID, action); err != nil {
		writeError(w, http.StatusForbidden, "insufficient permissions for this project")
		return false
	}
	return true
}

// resolveRepo resolves the {project}/{repo} params to a PyPI repository. On a
// write it auto-creates a local repo when the project allows it; on a read a
// missing repo is 404. A format mismatch is rejected.
func (h *handler) resolveRepo(w http.ResponseWriter, r *http.Request, write bool) (repository.Repository, bool) {
	project := chi.URLParam(r, "project")
	repoKey := chi.URLParam(r, "repo")
	projectID, allowAutoCreate, err := h.store.resolveProject(r.Context(), project)
	if err != nil {
		if errors.Is(err, errProjectNotFound) {
			writeError(w, http.StatusNotFound, "project not found: "+project)
			return repository.Repository{}, false
		}
		h.internalError(w, "resolving project", err)
		return repository.Repository{}, false
	}
	if !h.authorize(w, r, projectID, write) {
		return repository.Repository{}, false
	}
	var repo repository.Repository
	if write {
		repo, err = h.repos.ResolveOrCreate(r.Context(), projectID, repoKey, repository.FormatPyPI, actorFrom(r), allowAutoCreate)
	} else {
		repo, err = h.repos.GetForFormat(r.Context(), projectID, repoKey, repository.FormatPyPI)
	}
	switch {
	case err == nil:
		return repo, true
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, "repository not found: "+repoKey)
	case errors.Is(err, repository.ErrFormatMismatch):
		writeError(w, http.StatusBadRequest, "repository "+repoKey+" is not a PyPI repository")
	case errors.As(err, new(*repository.ValidationError)):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		h.internalError(w, "resolving repository", err)
	}
	return repository.Repository{}, false
}

// baseURL is the absolute base of this index: scheme://host/pypi/<project>/<repo>.
func baseURL(r *http.Request, project, repo string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/pypi/" + project + "/" + repo
}

func (h *handler) internalError(w http.ResponseWriter, msg string, err error) {
	h.log.Error(msg, slog.String("error", err.Error()))
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
