// Package rubygems implements the RubyGems "compact index" protocol under
// /rubygems/<project>/<repo>. It serves /versions, /info/<gem>, and /names, plus
// .gem downloads from /gems/<full-name>.gem, and accepts `gem push` (a POST of
// the raw .gem to /api/v1/gems). A pull-through proxy mirrors an upstream
// (rubygems.org): the index is proxied and each .gem cached on first download.
//
// Configure a client with the repository URL (credentials in the URL or
// ~/.gem/credentials):
//
//	gem push --host http://<host>/rubygems/<project>/<repo> pkg.gem
//	gem install pkg --source http://<host>/rubygems/<project>/<repo>
package rubygems

import (
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

// Adapter is the RubyGems registry format.
type Adapter struct{}

// New returns the RubyGems adapter.
func New() *Adapter { return &Adapter{} }

// Key implements registry.Adapter.
func (a *Adapter) Key() string { return "rubygems" }

// Mount registers the RubyGems routes. r is already scoped to /rubygems.
func (a *Adapter) Mount(r chi.Router, deps registry.Deps) {
	h := &handler{
		blobs:    deps.Blobs,
		auth:     deps.Auth,
		store:    newGemStore(deps.DB),
		repos:    deps.Repositories,
		upstream: newUpstreamClient(),
		log:      deps.Log,
	}
	r.Route("/{project}/{repo}", func(sub chi.Router) {
		sub.Use(h.requireAuth)
		sub.Get("/versions", h.versions)
		sub.Get("/names", h.names)
		sub.Get("/info/{gem}", h.info)
		sub.Post("/api/v1/gems", h.push)
		sub.Delete("/api/v1/gems/yank", h.yank)
		sub.Get("/gems/{file}", h.download)
	})
}

type handler struct {
	blobs    blob.Store
	auth     *auth.Service
	store    *gemStore
	repos    *repository.Service
	upstream *upstreamClient
	log      *slog.Logger
}

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

// authenticate resolves credentials to a user. `gem push` sends
// `Authorization: <api_key>` (the bare key); reads use Basic (from the source
// URL's userinfo or ~/.gem/credentials).
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
	token := header
	if strings.HasPrefix(header, "Bearer ") {
		token = strings.TrimSpace(header[len("Bearer "):])
	}
	if user, err := h.auth.AuthenticateToken(r.Context(), token); err == nil {
		return user, true
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

// resolveRepo resolves (project, repo) to a RubyGems repository.
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
		repo, err = h.repos.ResolveOrCreate(r.Context(), projectID, repoKey, repository.FormatRubyGems, actorFrom(r), allowAutoCreate)
	} else {
		repo, err = h.repos.GetForFormat(r.Context(), projectID, repoKey, repository.FormatRubyGems)
	}
	switch {
	case err == nil:
		return repo, true
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, http.StatusNotFound, "repository not found: "+repoKey)
	case errors.Is(err, repository.ErrFormatMismatch):
		writeError(w, http.StatusBadRequest, "repository "+repoKey+" is not a RubyGems repository")
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

func (h *handler) upstreamError(w http.ResponseWriter, msg string, err error) {
	h.log.Warn("rubygems "+msg, "error", err.Error())
	writeError(w, http.StatusBadGateway, "upstream unavailable")
}

// writeError renders a plain-text error (what the gem client surfaces).
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(msg + "\n"))
}
