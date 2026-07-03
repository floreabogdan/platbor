// Package terraform implements the Terraform module registry protocol under
// /terraform. It speaks the module registry API (service discovery, version
// listing, and download) so `terraform init` can install modules addressed as
// <host>/<namespace>/<name>/<provider>, where the namespace maps to a Platbor
// project. Terraform has no standard module upload API, so Platbor accepts a
// module archive via its own /terraform/upload endpoint.
//
// Scope: modules only. The provider registry protocol requires GPG signing-key
// plumbing and is out of scope; there is also no proxy mode (the public registry
// resolves modules from version control, not a cacheable archive endpoint).
package terraform

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

// maxArchiveSize caps a single uploaded module archive.
const maxArchiveSize = 512 << 20 // 512 MiB

// Adapter is the Terraform module registry format.
type Adapter struct{}

// New returns the Terraform adapter.
func New() *Adapter { return &Adapter{} }

// Key implements registry.Adapter.
func (a *Adapter) Key() string { return "terraform" }

// Mount registers the Terraform routes. r is already scoped to /terraform. The
// instance-global service-discovery document (/.well-known/terraform.json) is
// registered separately at the host root; see Discovery.
func (a *Adapter) Mount(r chi.Router, deps registry.Deps) {
	h := newHandler(deps)
	r.Group(func(sub chi.Router) {
		sub.Use(h.requireAuth)
		// Platbor-specific upload (namespace is the project; a module lives in a
		// typed repository within it).
		sub.Put("/upload/{project}/{repo}/{name}/{provider}/{version}", h.upload)
		// Terraform module registry protocol (namespace = project key).
		sub.Get("/v1/modules/{namespace}/{name}/{provider}/versions", h.versions)
		sub.Get("/v1/modules/{namespace}/{name}/{provider}/{version}/download", h.download)
		sub.Get("/v1/modules/{namespace}/{name}/{provider}/{version}/archive", h.archive)
	})
}

// Discovery returns the handler for /.well-known/terraform.json. It is
// instance-global (per hostname) and unauthenticated, and points terraform at
// the module registry base.
func Discovery() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"modules.v1": "/terraform/v1/modules/"})
	}
}

type handler struct {
	blobs blob.Store
	auth  *auth.Service
	store *moduleStore
	repos *repository.Service
	log   *slog.Logger
}

func newHandler(deps registry.Deps) *handler {
	return &handler{
		blobs: deps.Blobs,
		auth:  deps.Auth,
		store: newModuleStore(deps.DB),
		repos: deps.Repositories,
		log:   deps.Log,
	}
}

func (h *handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := h.authenticate(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Bearer`)
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
	})
}

// authenticate resolves a bearer token (terraform CLI credentials) or Basic
// credentials to a user.
func (h *handler) authenticate(r *http.Request) (auth.User, bool) {
	const bearer = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) > len(bearer) && strings.EqualFold(header[:len(bearer)], bearer) {
		if user, err := h.auth.AuthenticateToken(r.Context(), strings.TrimSpace(header[len(bearer):])); err == nil {
			return user, true
		}
		return auth.User{}, false
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

// resolveUploadRepo resolves (project, repo) to a Terraform repository for an
// upload, auto-creating a local repo when the project allows it.
func (h *handler) resolveUploadRepo(w http.ResponseWriter, r *http.Request, project, repoKey string) (repository.Repository, bool) {
	projectID, allowAutoCreate, err := h.store.resolveProject(r.Context(), project)
	if err != nil {
		if errors.Is(err, errProjectNotFound) {
			writeError(w, http.StatusNotFound, "project not found: "+project)
			return repository.Repository{}, false
		}
		h.internalError(w, "resolving project", err)
		return repository.Repository{}, false
	}
	if !h.authorize(w, r, projectID, true) {
		return repository.Repository{}, false
	}
	repo, err := h.repos.ResolveOrCreate(r.Context(), projectID, repoKey, repository.FormatTerraform, actorFrom(r), allowAutoCreate)
	switch {
	case err == nil:
		return repo, true
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, "repository not found: "+repoKey)
	case errors.Is(err, repository.ErrFormatMismatch):
		writeError(w, http.StatusBadRequest, "repository "+repoKey+" is not a Terraform repository")
	case errors.As(err, new(*repository.ValidationError)):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		h.internalError(w, "resolving repository", err)
	}
	return repository.Repository{}, false
}

func (h *handler) internalError(w http.ResponseWriter, msg string, err error) {
	h.log.Error(msg, slog.String("error", err.Error()))
	writeError(w, http.StatusInternalServerError, "internal error")
}

// writeError renders a Terraform registry error: { "errors": [ "..." ] }.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"errors": []string{msg}})
}
