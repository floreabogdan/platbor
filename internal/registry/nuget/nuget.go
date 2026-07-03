// Package nuget implements the NuGet V3 registry protocol under
// /nuget/<project>. The project is the feed: a service index at
// /nuget/<project>/v3/index.json advertises the publish, flat-container
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
		blobs: deps.Blobs,
		auth:  deps.Auth,
		store: newPackageStore(deps.DB),
		log:   deps.Log,
	}
	r.Route("/{project}", func(sub chi.Router) {
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
	blobs blob.Store
	auth  *auth.Service
	store *packageStore
	log   *slog.Logger
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

// resolveProject maps the {project} param to an id, writing the error response
// and returning ok=false when unknown.
func (h *handler) resolveProject(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := chi.URLParam(r, "project")
	projectID, err := h.store.resolveProject(r.Context(), key)
	if err != nil {
		if errors.Is(err, errProjectNotFound) {
			writeError(w, http.StatusNotFound, "project not found: "+key)
			return "", false
		}
		h.internalError(w, "resolving project", err)
		return "", false
	}
	return projectID, true
}

// baseURL is the absolute base of this feed: scheme://host/nuget/<project>.
func baseURL(r *http.Request, project string) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/nuget/" + project
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
