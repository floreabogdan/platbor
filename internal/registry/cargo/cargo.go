// Package cargo implements the Cargo sparse registry protocol under
// /cargo/<project>/<repo>. It serves config.json, the per-crate sparse index
// files (newline-delimited JSON at a sharded path), and .crate downloads, and
// accepts `cargo publish` (a binary PUT to /api/v1/crates/new). A pull-through
// proxy mirrors an upstream sparse index (index.crates.io): the index is fetched
// fresh, and each .crate is cached on first download into the shared blob store.
//
// config.json advertises auth-required, so the cargo client sends the token on
// every request. Configure a client with a [registries] entry pointing at:
//
//	http://<host>/cargo/<project>/<repo>
package cargo

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

// Adapter is the Cargo registry format.
type Adapter struct{}

// New returns the Cargo adapter.
func New() *Adapter { return &Adapter{} }

// Key implements registry.Adapter.
func (a *Adapter) Key() string { return "cargo" }

// Mount registers the Cargo routes. r is already scoped to /cargo.
func (a *Adapter) Mount(r chi.Router, deps registry.Deps) {
	h := &handler{
		blobs:    deps.Blobs,
		auth:     deps.Auth,
		store:    newCrateStore(deps.DB),
		repos:    deps.Repositories,
		upstream: newUpstreamClient(),
		log:      deps.Log,
	}
	r.Route("/{project}/{repo}", func(sub chi.Router) {
		sub.Use(h.requireAuth)
		sub.Get("/config.json", h.config)
		sub.Put("/api/v1/crates/new", h.publish)
		sub.Get("/api/v1/crates/{crate}/{version}/download", h.download)
		sub.Delete("/api/v1/crates/{crate}/{version}/yank", h.yank)
		sub.Put("/api/v1/crates/{crate}/{version}/unyank", h.unyank)
		// Everything else is a sparse index file request; the crate name is the
		// final path segment (preceded by the shard directories).
		sub.Get("/*", h.index)
	})
}

type handler struct {
	blobs    blob.Store
	auth     *auth.Service
	store    *crateStore
	repos    *repository.Service
	upstream *upstreamClient
	log      *slog.Logger
}

// requireAuth authenticates the caller (config.json declares auth-required, so
// cargo sends the token on every request).
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

// authenticate resolves credentials to a user. cargo sends `Authorization: <token>`
// (the raw token, no scheme); Basic (password or PAT) is also accepted for curl
// and the browser.
func (h *handler) authenticate(r *http.Request) (auth.User, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return auth.User{}, false
	}
	if strings.HasPrefix(strings.ToLower(header), "basic ") {
		if username, password, ok := r.BasicAuth(); ok && password != "" {
			if user, err := h.auth.AuthenticateToken(r.Context(), password); err == nil {
				return user, true
			}
			if user, err := h.auth.Authenticate(r.Context(), username, password); err == nil {
				return user, true
			}
		}
		return auth.User{}, false
	}
	// A bare token (cargo) or an explicit Bearer token.
	token := header
	if strings.HasPrefix(header, "Bearer ") {
		token = strings.TrimSpace(header[len("Bearer "):])
	}
	if user, err := h.auth.AuthenticateToken(r.Context(), token); err == nil {
		return user, true
	}
	return auth.User{}, false
}

// authorize enforces the caller's project role.
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

// resolveRepo resolves (project, repo) to a Cargo repository.
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
		repo, err = h.repos.ResolveOrCreate(r.Context(), projectID, repoKey, repository.FormatCargo, actorFrom(r), allowAutoCreate)
	} else {
		repo, err = h.repos.GetForFormat(r.Context(), projectID, repoKey, repository.FormatCargo)
	}
	switch {
	case err == nil:
		return repo, true
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, "repository not found: "+repoKey)
	case errors.Is(err, repository.ErrFormatMismatch):
		writeError(w, http.StatusBadRequest, "repository "+repoKey+" is not a Cargo repository")
	case errors.As(err, new(*repository.ValidationError)):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		h.internalError(w, "resolving repository", err)
	}
	return repository.Repository{}, false
}

// baseURL is the absolute base of this registry.
func baseURL(r *http.Request, project, repo string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/cargo/" + project + "/" + repo
}

func (h *handler) internalError(w http.ResponseWriter, msg string, err error) {
	h.log.Error(msg, slog.String("error", err.Error()))
	writeError(w, http.StatusInternalServerError, "internal error")
}

func (h *handler) upstreamError(w http.ResponseWriter, msg string, err error) {
	h.log.Warn("cargo "+msg, "error", err.Error())
	writeError(w, http.StatusBadGateway, "upstream unavailable")
}

// writeError renders a Cargo API error: an { "errors": [ { "detail": ... } ] }
// body, which the client surfaces to the user.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]string{{"detail": msg}}})
}
