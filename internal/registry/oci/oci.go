// Package oci implements the OCI Distribution Spec v1.1 registry protocol under
// /v2. This slice covers the blob API (existence, download, and resumable
// uploads); manifests and tags follow. It speaks the spec's own error envelope
// and HTTP Basic auth (the bearer-token flow lands later).
package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
	"github.com/platbor/platbor/internal/registry"
	"github.com/platbor/platbor/internal/registry/proxy"
)

const apiVersionHeader = "Docker-Distribution-API-Version"

// Adapter is the OCI registry format.
type Adapter struct{}

// New returns the OCI adapter.
func New() *Adapter { return &Adapter{} }

// Key implements registry.Adapter.
func (a *Adapter) Key() string { return "oci" }

// Mount registers the /v2 routes. r is already scoped to /v2 by the caller.
func (a *Adapter) Mount(r chi.Router, deps registry.Deps) {
	h := &handler{
		blobs:     deps.Blobs,
		auth:      deps.Auth,
		manifests: newManifestStore(deps.DB),
		repos:     deps.Repositories,
		upstream:  proxy.New(),
		bearer:    deps.EnableOCIBearer,
		log:       deps.Log,
	}
	// The token endpoint authenticates itself (HTTP Basic), so it must sit
	// outside the requireAuth middleware that guards every other /v2 route.
	if h.bearer {
		r.Get("/token", h.issueToken)
	}
	r.Group(func(r chi.Router) {
		r.Use(h.requireAuth)
		r.Get("/", h.versionCheck)
		r.Handle("/*", http.HandlerFunc(h.serve))
	})
}

type handler struct {
	blobs     blob.Store
	auth      *auth.Service
	manifests *manifestStore
	repos     *repository.Service
	upstream  *proxy.Client
	bearer    bool
	log       *slog.Logger
}

// versionCheck answers the GET /v2/ probe that clients use to detect a v2
// registry (and to trigger the auth challenge).
func (h *handler) versionCheck(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set(apiVersionHeader, "registry/2.0")
	w.WriteHeader(http.StatusOK)
}

// serve dispatches a name-bearing /v2 request to the right operation.
func (h *handler) serve(w http.ResponseWriter, r *http.Request) {
	p := parsePath(chi.URLParam(r, "*"))
	if p.kind == opUnknown || !validName(p.name) {
		writeError(w, h.log, http.StatusNotFound, codeNameInvalid, "unsupported or malformed path")
		return
	}

	// Enforce the caller's project role before dispatching: GET/HEAD read, every
	// other method writes. This is the one authorization choke point for /v2,
	// covering the blob path (which resolves repos itself for proxy behaviour).
	if !h.authorizeName(w, r, p.name, r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return
	}

	switch p.kind {
	case opBlobUpload:
		h.serveUpload(w, r, p)
	case opBlob:
		h.serveBlob(w, r, p)
	case opManifest:
		h.serveManifest(w, r, p)
	case opTags:
		h.serveTags(w, r, p)
	case opReferrers:
		h.serveReferrers(w, r, p)
	default:
		writeError(w, h.log, http.StatusNotFound, codeNameInvalid, "unsupported path")
	}
}

// resolveRepo splits the OCI name into <project>/<repo>/<image> and resolves the
// typed repository. On a write it auto-creates a local OCI repo when the project
// allows it; on a read a missing repo is 404. A format mismatch (the repo holds
// another format) is rejected. It returns the resolved repository and the image
// name within it.
func (h *handler) resolveRepo(w http.ResponseWriter, r *http.Request, name string, write bool) (repository.Repository, string, bool) {
	project, repoKey, image, ok := splitName(name)
	if !ok {
		writeError(w, h.log, http.StatusNotFound, codeNameUnknown, "repository name must be <project>/<repository>/<image>")
		return repository.Repository{}, "", false
	}
	projectID, allowAutoCreate, err := h.manifests.resolveProject(r.Context(), project)
	if err != nil {
		if errors.Is(err, errProjectNotFound) {
			writeError(w, h.log, http.StatusNotFound, codeNameUnknown, fmt.Sprintf("project %q does not exist", project))
			return repository.Repository{}, "", false
		}
		h.internalError(w, "resolving project", err)
		return repository.Repository{}, "", false
	}
	var repo repository.Repository
	if write {
		repo, err = h.repos.ResolveOrCreate(r.Context(), projectID, repoKey, repository.FormatOCI, usernameFromContext(r.Context()), allowAutoCreate)
	} else {
		repo, err = h.repos.GetForFormat(r.Context(), projectID, repoKey, repository.FormatOCI)
	}
	switch {
	case err == nil:
		return repo, image, true
	case errors.Is(err, repository.ErrNotFound):
		writeError(w, h.log, http.StatusNotFound, codeNameUnknown, fmt.Sprintf("repository %q does not exist", repoKey))
	case errors.Is(err, repository.ErrFormatMismatch):
		writeError(w, h.log, http.StatusBadRequest, codeNameInvalid, fmt.Sprintf("repository %q is not an OCI repository", repoKey))
	case errors.As(err, new(*repository.ValidationError)):
		writeError(w, h.log, http.StatusBadRequest, codeNameInvalid, err.Error())
	default:
		h.internalError(w, "resolving repository", err)
	}
	return repository.Repository{}, "", false
}

// lookupRepo resolves the repository for a name without writing any response,
// returning ok=false when the project or repository is absent or not OCI. Used
// by the blob path, where storage is global (by digest) but the repository still
// determines proxy behaviour.
func (h *handler) lookupRepo(ctx context.Context, name string) (repository.Repository, string, bool) {
	project, repoKey, image, ok := splitName(name)
	if !ok {
		return repository.Repository{}, "", false
	}
	projectID, _, err := h.manifests.resolveProject(ctx, project)
	if err != nil {
		return repository.Repository{}, "", false
	}
	repo, err := h.repos.GetForFormat(ctx, projectID, repoKey, repository.FormatOCI)
	if err != nil {
		return repository.Repository{}, "", false
	}
	return repo, image, true
}

// authorizeName enforces the caller's project role for an OCI name. A malformed
// name or unknown project is left to the dispatched handler to answer (so a
// permission check never leaks which projects exist); only a real membership
// denial short-circuits with 403.
func (h *handler) authorizeName(w http.ResponseWriter, r *http.Request, name string, write bool) bool {
	project, _, _, ok := splitName(name)
	if !ok {
		return true
	}
	projectID, _, err := h.manifests.resolveProject(r.Context(), project)
	if err != nil {
		return true // unknown project → defer to the handler's 404
	}
	action := auth.ActionRead
	if write {
		action = auth.ActionWrite
	}
	user, _ := userFromContext(r.Context())
	if err := h.auth.Authorize(r.Context(), user, projectID, action); err != nil {
		writeError(w, h.log, http.StatusForbidden, codeDenied, "insufficient permissions for this repository")
		return false
	}
	return true
}

// requireAuth authenticates every /v2 route, challenging unauthenticated clients
// so `docker login` and friends can present credentials. The challenge is HTTP
// Basic by default, or a Bearer token-endpoint pointer when the bearer flow is
// enabled — but Basic credentials (password or PAT) are always accepted too.
func (h *handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := h.authenticate(r)
		if !ok {
			h.challenge(w, r)
			writeError(w, h.log, http.StatusUnauthorized, codeUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
	})
}

// challenge writes the WWW-Authenticate header telling a client how to
// authenticate: the bearer token endpoint when enabled, otherwise HTTP Basic.
func (h *handler) challenge(w http.ResponseWriter, r *http.Request) {
	if h.bearer {
		realm := requestScheme(r) + "://" + r.Host + "/v2/token"
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm=%q,service=%q`, realm, r.Host))
		return
	}
	w.Header().Set("WWW-Authenticate", `Basic realm="Platbor"`)
}

// authenticate resolves a request's identity: a Bearer registry token (from the
// token endpoint) or a personal access token, or Basic credentials whose
// password may be a personal access token or the account password.
func (h *handler) authenticate(r *http.Request) (auth.User, bool) {
	const bearer = "Bearer "
	if header := r.Header.Get("Authorization"); len(header) > len(bearer) && strings.EqualFold(header[:len(bearer)], bearer) {
		tok := header[len(bearer):]
		if user, err := h.auth.VerifyRegistryToken(tok); err == nil {
			return user, true
		}
		// A Bearer that is not our registry token may still be a personal token.
		if user, err := h.auth.AuthenticateToken(r.Context(), tok); err == nil {
			return user, true
		}
		return auth.User{}, false
	}

	username, password, ok := r.BasicAuth()
	if !ok || password == "" {
		return auth.User{}, false
	}
	if user, err := h.auth.AuthenticateToken(r.Context(), password); err == nil {
		return user, true
	} else if !errors.Is(err, auth.ErrInvalidToken) {
		h.log.Error("oci token auth", slog.String("error", err.Error()))
	}
	user, err := h.auth.Authenticate(r.Context(), username, password)
	if err != nil {
		if !errors.Is(err, auth.ErrInvalidCredentials) {
			h.log.Error("oci basic auth", slog.String("error", err.Error()))
		}
		return auth.User{}, false
	}
	return user, true
}

// issueToken is the OCI token endpoint (docker's registry auth flow). It
// authenticates the client with HTTP Basic, then mints a short-lived bearer
// token carrying their identity. Authorization is still enforced per-request
// against project roles, so the token grants no more than the user's roles do.
func (h *handler) issueToken(w http.ResponseWriter, r *http.Request) {
	user, ok := h.authenticate(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="Platbor"`)
		writeError(w, h.log, http.StatusUnauthorized, codeUnauthorized, "authentication required")
		return
	}
	token, _, err := h.auth.IssueRegistryToken(user)
	if err != nil {
		h.internalError(w, "issuing registry token", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"token":        token,
		"access_token": token,
		"expires_in":   int(auth.RegistryTokenTTL / time.Second),
	})
}

// requestScheme reports the external scheme so the token realm is reachable
// behind a TLS-terminating proxy.
func requestScheme(r *http.Request) string {
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return "https"
	}
	return "http"
}

// internalError logs and returns a generic 500 in the spec envelope.
func (h *handler) internalError(w http.ResponseWriter, msg string, err error) {
	h.log.Error(msg, slog.String("error", err.Error()))
	writeError(w, h.log, http.StatusInternalServerError, codeUnsupported, "internal error")
}
