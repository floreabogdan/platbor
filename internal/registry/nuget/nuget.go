// Package nuget implements the NuGet V3 registry protocol under
// /nuget/<project>/<repo>. A NuGet repository is a feed: a service index at
// /nuget/<project>/<repo>/v3/index.json advertises the publish, flat-container
// (restore), registration (metadata), and search resources. Auth is a NuGet API
// key (X-NuGet-ApiKey, a Platbor personal access token), HTTP Basic, or a bearer
// token; the service index itself is anonymous so a client can discover it.
package nuget

import (
	"context"
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

// Adapter is the NuGet registry format.
type Adapter struct{}

// New returns the NuGet adapter.
func New() *Adapter { return &Adapter{} }

// Key implements registry.Adapter.
func (a *Adapter) Key() string { return "nuget" }

// Mount registers the NuGet routes. r is already scoped to /nuget.
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
		// The service index is anonymous so clients can discover the feed.
		sub.Get("/v3/index.json", h.serviceIndex)

		// Everything else requires authentication.
		sub.Group(func(sub chi.Router) {
			sub.Use(h.requireAuth)
			sub.Put("/v3/package", h.push)
			sub.Put("/v3/package/", h.push)
			sub.Get("/v3-flatcontainer/*", h.flatContainer)
			sub.Get("/v3/registrations/*", h.registration)
			sub.Get("/v3/search", h.search)
		})
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

// requireAuth authenticates via API key, Basic, or bearer, challenging otherwise.
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

// authenticate resolves the NuGet API key (X-NuGet-ApiKey, a personal access
// token), Basic credentials, or a bearer token to a user.
func (h *handler) authenticate(r *http.Request) (auth.User, bool) {
	if key := r.Header.Get("X-NuGet-ApiKey"); key != "" {
		if user, err := h.auth.AuthenticateToken(r.Context(), key); err == nil {
			return user, true
		} else if !errors.Is(err, auth.ErrInvalidToken) {
			h.log.Error("nuget apikey auth", slog.String("error", err.Error()))
		}
	}
	const bearer = "Bearer "
	if header := r.Header.Get("Authorization"); len(header) > len(bearer) && strings.EqualFold(header[:len(bearer)], bearer) {
		if user, err := h.auth.AuthenticateToken(r.Context(), header[len(bearer):]); err == nil {
			return user, true
		}
	}
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

// authorize enforces the caller's project role: reads need reader, writes need
// maintainer (instance admins bypass). It writes a 403 and returns false on deny.
func (h *handler) authorize(w http.ResponseWriter, r *http.Request, projectID string, write bool) bool {
	action := auth.ActionRead
	if write {
		action = auth.ActionWrite
	}
	user, _ := r.Context().Value(userContextKey).(auth.User)
	if err := h.auth.Authorize(r.Context(), user, projectID, action); err != nil {
		writeError(w, http.StatusForbidden, "insufficient permissions for this project")
		return false
	}
	return true
}

// resolveRepo resolves the {project}/{repo} params to a NuGet repository. On a
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
		repo, err = h.repos.ResolveOrCreate(r.Context(), projectID, repoKey, repository.FormatNuGet, actorFrom(r), allowAutoCreate)
	} else {
		repo, err = h.repos.GetForFormat(r.Context(), projectID, repoKey, repository.FormatNuGet)
	}
	switch {
	case err == nil:
		return repo, true
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, "repository not found: "+repoKey)
	case errors.Is(err, repository.ErrFormatMismatch):
		writeError(w, http.StatusBadRequest, "repository "+repoKey+" is not a NuGet repository")
	case errors.As(err, new(*repository.ValidationError)):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		h.internalError(w, "resolving repository", err)
	}
	return repository.Repository{}, false
}

// baseURL is the absolute base of this feed: scheme://host/nuget/<project>/<repo>.
func baseURL(r *http.Request, project, repo string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/nuget/" + project + "/" + repo
}

func (h *handler) internalError(w http.ResponseWriter, msg string, err error) {
	h.log.Error(msg, slog.String("error", err.Error()))
	writeError(w, http.StatusInternalServerError, "internal error")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

type contextKey int

const userContextKey contextKey = iota

func withUser(ctx context.Context, user auth.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

func actorFrom(r *http.Request) string {
	if user, ok := r.Context().Value(userContextKey).(auth.User); ok {
		return user.Username
	}
	return "system"
}
