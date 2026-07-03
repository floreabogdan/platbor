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
		upstream: newUpstreamClient(),
		log:      deps.Log,
	}
	r.Route("/{project}", func(sub chi.Router) {
		sub.Handle("/*", http.HandlerFunc(h.serve))
	})
}

type handler struct {
	blobs    blob.Store
	auth     *auth.Service
	store    *packageStore
	upstream *upstreamClient
	log      *slog.Logger
}

// serve resolves the operation and dispatches it. Login is the one route that
// runs unauthenticated (it establishes credentials); everything else requires a
// valid bearer token or Basic credentials.
func (h *handler) serve(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")

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

	if op.kind != opWhoami && !validPackageName(op.pkg) {
		writeError(w, h.log, http.StatusNotFound, "not found")
		return
	}

	switch op.kind {
	case opWhoami:
		h.whoami(w, r)
	case opPackage:
		switch r.Method {
		case http.MethodGet:
			h.getPackument(w, r, project, op.pkg)
		case http.MethodPut:
			h.publish(w, r, project, op.pkg)
		default:
			writeError(w, h.log, http.StatusMethodNotAllowed, "method not allowed")
		}
	case opTarball:
		if r.Method != http.MethodGet {
			writeError(w, h.log, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		h.getTarball(w, r, project, op.pkg, op.ref)
	case opDistTags:
		h.serveDistTags(w, r, project, op)
	default:
		writeError(w, h.log, http.StatusNotFound, "not found")
	}
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

// resolveProject maps a project key to its id, writing the npm error envelope
// and returning ok=false when the project is unknown.
func (h *handler) resolveProject(w http.ResponseWriter, r *http.Request, key string) (string, bool) {
	projectID, err := h.store.resolveProject(r.Context(), key)
	if err != nil {
		if errors.Is(err, errProjectNotFound) {
			writeError(w, h.log, http.StatusNotFound, "project not found: "+key)
			return "", false
		}
		h.internalError(w, "resolving project", err)
		return "", false
	}
	return projectID, true
}

// registryBase reconstructs the absolute base URL of this npm registry
// (scheme://host/npm/<project>) so packument tarball URLs point back at us
// regardless of how the client addressed the server. The project is the
// registry: packages live directly under it.
func registryBase(r *http.Request, project string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/npm/" + project
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
