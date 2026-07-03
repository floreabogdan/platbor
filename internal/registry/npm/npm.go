// Package npm implements the npm registry protocol under
// /npm/<project>/<repo>. It speaks npm's JSON packument + tarball API, its
// legacy login (CouchDB user document) token flow, and dist-tags, backed by the
// shared blob store and metadata DB. Auth is a bearer token (a Platbor personal
// access token) or HTTP Basic, matching what `npm` sends.
package npm

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
	"github.com/platbor/platbor/internal/registry"
)

// Adapter is the npm registry format.
type Adapter struct{}

// New returns the npm adapter.
func New() *Adapter { return &Adapter{} }

// Key implements registry.Adapter.
func (a *Adapter) Key() string { return "npm" }

// Mount registers the npm routes. r is already scoped to /npm by the caller.
// Every request is dispatched through one catch-all so the adapter parses npm's
// slash-bearing package names itself, rather than fighting the router over
// scoped names and encoded separators.
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
		sub.Handle("/*", http.HandlerFunc(h.serve))
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

// serve resolves the operation and dispatches it. Login is the one route that
// runs unauthenticated (it establishes credentials); everything else requires a
// valid bearer token or Basic credentials.
func (h *handler) serve(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	repoKey := chi.URLParam(r, "repo")

	tail := chi.URLParam(r, "*")
	// The package name may arrive percent-encoded (scoped names as @scope%2fname).
	// Decode so parsing sees literal slashes regardless of the router's config.
	if dec, err := url.PathUnescape(tail); err == nil {
		tail = dec
	}
	op := parsePath(tail)

	if op.kind == opLogin {
		h.login(w, r, op.user)
		return
	}

	user, ok := h.authenticate(r)
	if !ok {
		writeError(w, h.log, http.StatusUnauthorized, "authentication required")
		return
	}
	r = r.WithContext(withUser(r.Context(), user))

	if op.kind == opWhoami {
		h.whoami(w, r)
		return
	}
	if !validPackageName(op.pkg) {
		writeError(w, h.log, http.StatusNotFound, "not found")
		return
	}

	// publish is the only write; everything else reads (auto-create only on write).
	write := op.kind == opPackage && r.Method == http.MethodPut
	repo, ok := h.resolveRepo(w, r, project, repoKey, write)
	if !ok {
		return
	}

	switch op.kind {
	case opPackage:
		switch r.Method {
		case http.MethodGet:
			h.getPackument(w, r, repo, project, op.pkg)
		case http.MethodPut:
			h.publish(w, r, repo, op.pkg)
		default:
			writeError(w, h.log, http.StatusMethodNotAllowed, "method not allowed")
		}
	case opTarball:
		if r.Method != http.MethodGet {
			writeError(w, h.log, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.getTarball(w, r, repo, project, op.pkg, op.ref)
	case opDistTags:
		h.serveDistTags(w, r, repo, op)
	default:
		writeError(w, h.log, http.StatusNotFound, "not found")
	}
}

// resolveRepo resolves (project, repo) to an npm repository, auto-creating a
// local one on writes when the project allows it, or 404ing a missing repo on
// reads. A format mismatch is rejected.
func (h *handler) resolveRepo(w http.ResponseWriter, r *http.Request, project, repoKey string, write bool) (repository.Repository, bool) {
	projectID, allowAutoCreate, err := h.store.resolveProject(r.Context(), project)
	if err != nil {
		if errors.Is(err, errProjectNotFound) {
			writeError(w, h.log, http.StatusNotFound, "project not found: "+project)
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
		repo, err = h.repos.ResolveOrCreate(r.Context(), projectID, repoKey, repository.FormatNPM, actorFrom(r), allowAutoCreate)
	} else {
		repo, err = h.repos.GetForFormat(r.Context(), projectID, repoKey, repository.FormatNPM)
	}
	switch {
	case err == nil:
		return repo, true
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, h.log, http.StatusNotFound, "repository not found: "+repoKey)
	case errors.Is(err, repository.ErrFormatMismatch):
		writeError(w, h.log, http.StatusBadRequest, "repository "+repoKey+" is not an npm repository")
	case errors.As(err, new(*repository.ValidationError)):
		writeError(w, h.log, http.StatusBadRequest, err.Error())
	default:
		h.internalError(w, "resolving repository", err)
	}
	return repository.Repository{}, false
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
		writeError(w, h.log, http.StatusForbidden, "insufficient permissions for this project")
		return false
	}
	return true
}

// authenticate resolves a bearer token or Basic credentials to a user. For
// Basic, the password may be a personal access token (username ignored) or the
// account password — the same dual meaning `npm` and `docker` both rely on.
func (h *handler) authenticate(r *http.Request) (auth.User, bool) {
	const bearer = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) > len(bearer) && strings.EqualFold(header[:len(bearer)], bearer) {
		token := header[len(bearer):]
		if user, err := h.auth.AuthenticateToken(r.Context(), token); err == nil {
			return user, true
		} else if !errors.Is(err, auth.ErrInvalidToken) {
			h.log.Error("npm token auth", slog.String("error", err.Error()))
		}
		return auth.User{}, false
	}

	if username, password, ok := r.BasicAuth(); ok && password != "" {
		if user, err := h.auth.AuthenticateToken(r.Context(), password); err == nil {
			return user, true
		}
		if user, err := h.auth.Authenticate(r.Context(), username, password); err == nil {
			return user, true
		} else if !errors.Is(err, auth.ErrInvalidCredentials) {
			h.log.Error("npm basic auth", slog.String("error", err.Error()))
		}
	}
	return auth.User{}, false
}

// whoami answers `npm whoami` with the authenticated username.
func (h *handler) whoami(w http.ResponseWriter, r *http.Request) {
	user, _ := userFromContext(r.Context())
	writeJSON(w, h.log, http.StatusOK, map[string]string{"username": user.Username})
}

// registryBase reconstructs the absolute base URL of this npm repository
// (scheme://host/npm/<project>/<repo>) so packument tarball URLs point back at
// us regardless of how the client addressed the server.
func registryBase(r *http.Request, project, repo string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/npm/" + project + "/" + repo
}

// writeError emits npm's JSON error envelope: clients surface the "error" field.
func writeError(w http.ResponseWriter, log *slog.Logger, status int, msg string) {
	writeJSON(w, log, status, map[string]string{"error": msg})
}

// writeJSON writes v as JSON with the given status.
func writeJSON(w http.ResponseWriter, log *slog.Logger, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Error("npm write response", slog.String("error", err.Error()))
	}
}

// internalError logs and returns a generic 500 in the npm envelope.
func (h *handler) internalError(w http.ResponseWriter, msg string, err error) {
	h.log.Error(msg, slog.String("error", err.Error()))
	writeError(w, h.log, http.StatusInternalServerError, "internal error")
}
