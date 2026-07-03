// Package maven implements a Maven repository under
// /maven/<project>/<repo>/<path>. It speaks the plain-HTTP Maven layout that
// `mvn deploy` and `mvn dependency:get` (and Gradle) use: PUT stores a file at
// its coordinate path, GET/HEAD serves it, and a pull-through proxy mirrors an
// upstream (repo1.maven.org/maven2). Files share the content-addressable blob
// store; auth is HTTP Basic (a personal access token or the account password) or
// a bearer token.
//
// Maven is a file tree, not an API: the client uploads the pom, jar, checksums
// (.sha1/.md5), and maven-metadata.xml itself, so Platbor never generates
// metadata server-side. Configure a client with the repository URL:
//
//	<url>http://<host>/maven/<project>/<repo></url>
package maven

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

// maxFileSize caps a single Maven upload.
const maxFileSize = 5 << 30 // 5 GiB

// Adapter is the Maven registry format.
type Adapter struct{}

// New returns the Maven adapter.
func New() *Adapter { return &Adapter{} }

// Key implements registry.Adapter.
func (a *Adapter) Key() string { return "maven" }

// Mount registers the Maven routes. r is already scoped to /maven. The path is
// /maven/<project>/<repo>/<coordinate path>.
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
		sub.Handle("/*", http.HandlerFunc(h.serve))
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
	path := chi.URLParam(r, "*")
	if dec, err := url.PathUnescape(path); err == nil {
		path = dec
	}

	user, ok := h.authenticate(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="Platbor"`)
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}
	r = r.WithContext(withUser(r.Context(), user))

	if !validPath(path) {
		writeError(w, http.StatusBadRequest, "invalid path")
		return
	}

	write := r.Method == http.MethodPut || r.Method == http.MethodDelete
	repo, ok := h.resolveRepo(w, r, project, repoKey, write)
	if !ok {
		return
	}

	switch r.Method {
	case http.MethodPut:
		h.upload(w, r, repo, path)
	case http.MethodGet, http.MethodHead:
		h.download(w, r, repo, path)
	case http.MethodDelete:
		h.remove(w, r, repo, path)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// resolveRepo resolves (project, repo) to a Maven repository. On a write it
// auto-creates a local repo when the project allows it; on a read it 404s if
// missing. A format mismatch is rejected.
func (h *handler) resolveRepo(w http.ResponseWriter, r *http.Request, project, repoKey string, write bool) (repository.Repository, bool) {
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
		repo, err = h.repos.ResolveOrCreate(r.Context(), projectID, repoKey, repository.FormatMaven, actorFrom(r), allowAutoCreate)
	} else {
		repo, err = h.repos.GetForFormat(r.Context(), projectID, repoKey, repository.FormatMaven)
	}
	switch {
	case err == nil:
		return repo, true
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, "repository not found: "+repoKey)
	case errors.Is(err, repository.ErrFormatMismatch):
		writeError(w, http.StatusBadRequest, "repository "+repoKey+" is not a Maven repository")
	case errors.As(err, new(*repository.ValidationError)):
		writeError(w, http.StatusBadRequest, err.Error())
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
		writeError(w, http.StatusForbidden, "insufficient permissions for this project")
		return false
	}
	return true
}

// authenticate resolves a bearer token or Basic credentials to a user. Basic's
// password may be a personal access token or the account password.
func (h *handler) authenticate(r *http.Request) (auth.User, bool) {
	const bearer = "Bearer "
	header := r.Header.Get("Authorization")
	if len(header) > len(bearer) && strings.EqualFold(header[:len(bearer)], bearer) {
		if user, err := h.auth.AuthenticateToken(r.Context(), header[len(bearer):]); err == nil {
			return user, true
		} else if !errors.Is(err, auth.ErrInvalidToken) {
			h.log.Error("maven token auth", slog.String("error", err.Error()))
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

// validPath rejects empty, absolute, or dot-segment paths so a file can never
// escape its repository.
func validPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "//") {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return true
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
