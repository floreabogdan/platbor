// Package goproxy implements the Go module proxy protocol under
// /go/<project>/<repo>. It is a pull-through mirror of an upstream GOPROXY
// (proxy.golang.org): the immutable per-version files (<module>/@v/<v>.info,
// .mod, .zip) are cached lazily into the shared blob store, while the mutable
// listings (<module>/@v/list, <module>/@latest) are fetched fresh each read.
//
// Go modules originate from version control and have no upload API, so Platbor
// supports Go in proxy mode only; a local Go repository has nothing to serve.
// Point the toolchain at a repository with:
//
//	GOPROXY=http://<host>/go/<project>/<repo> GOSUMDB=off go mod download
package goproxy

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

// Adapter is the Go module proxy format.
type Adapter struct{}

// New returns the Go module proxy adapter.
func New() *Adapter { return &Adapter{} }

// Key implements registry.Adapter.
func (a *Adapter) Key() string { return "go" }

// Mount registers the Go proxy routes. r is already scoped to /go. The path is
// /go/<project>/<repo>/<module>/@v/<file> or .../<module>/@latest.
func (a *Adapter) Mount(r chi.Router, deps registry.Deps) {
	h := &handler{
		blobs:    deps.Blobs,
		auth:     deps.Auth,
		store:    newFileStore(deps.DB),
		repos:    deps.Repositories,
		upstream: newUpstreamClient(),
		log:      deps.Log,
	}
	r.Route("/{project}/{repo}", func(sub chi.Router) {
		sub.Get("/*", h.serve)
	})
}

type handler struct {
	blobs    blob.Store
	auth     *auth.Service
	store    *fileStore
	repos    *repository.Service
	upstream *upstreamClient
	log      *slog.Logger
}

func (h *handler) serve(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	repoKey := chi.URLParam(r, "repo")
	splat := strings.TrimLeft(chi.URLParam(r, "*"), "/")

	user, ok := h.authenticate(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="Platbor"`)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	r = r.WithContext(withUser(r.Context(), user))

	repo, ok := h.resolveRepo(w, r, project, repoKey)
	if !ok {
		return
	}
	if repo.Mode != repository.ModeProxy {
		writeError(w, http.StatusBadRequest, "Go modules require a proxy repository (local hosting is not supported)")
		return
	}

	req, err := parseGoPath(splat)
	if err != nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	switch req.op {
	case opList, opLatest:
		// Mutable listings: always fetched fresh, never cached.
		h.proxyFresh(w, r, repo, splat, req)
	case opFile:
		// Immutable per-version file: served from cache, filled on a miss.
		h.serveFile(w, r, repo, splat, req)
	default:
		writeError(w, http.StatusNotFound, "not found")
	}
}

// resolveRepo resolves (project, repo) to a Go repository for a read. Missing →
// 404; wrong format → 400.
func (h *handler) resolveRepo(w http.ResponseWriter, r *http.Request, project, repoKey string) (repository.Repository, bool) {
	projectID, _, err := h.store.resolveProject(r.Context(), project)
	if err != nil {
		if errors.Is(err, errProjectNotFound) {
			writeError(w, http.StatusNotFound, "project not found: "+project)
			return repository.Repository{}, false
		}
		h.internalError(w, "resolving project", err)
		return repository.Repository{}, false
	}
	user, _ := userFromContext(r.Context())
	if err := h.auth.Authorize(r.Context(), user, projectID, auth.ActionRead); err != nil {
		writeError(w, http.StatusForbidden, "insufficient permissions for this project")
		return repository.Repository{}, false
	}
	repo, err := h.repos.GetForFormat(r.Context(), projectID, repoKey, repository.FormatGo)
	switch {
	case err == nil:
		return repo, true
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, "repository not found: "+repoKey)
	case errors.Is(err, repository.ErrFormatMismatch):
		writeError(w, http.StatusBadRequest, "repository "+repoKey+" is not a Go repository")
	default:
		h.internalError(w, "resolving repository", err)
	}
	return repository.Repository{}, false
}

// authenticate resolves a bearer token or Basic credentials to a user. The go
// toolchain sends Basic from ~/.netrc for a private proxy.
func (h *handler) authenticate(r *http.Request) (auth.User, bool) {
	const bearer = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) > len(bearer) && strings.EqualFold(header[:len(bearer)], bearer) {
		if user, err := h.auth.AuthenticateToken(r.Context(), header[len(bearer):]); err == nil {
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

func (h *handler) internalError(w http.ResponseWriter, msg string, err error) {
	h.log.Error(msg, slog.String("error", err.Error()))
	writeError(w, http.StatusInternalServerError, "internal error")
}

func (h *handler) upstreamError(w http.ResponseWriter, msg string, err error) {
	h.log.Warn("go "+msg, "error", err.Error())
	writeError(w, http.StatusBadGateway, "upstream unavailable")
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
