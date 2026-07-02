// Package oci implements the OCI Distribution Spec v1.1 registry protocol under
// /v2. This slice covers the blob API (existence, download, and resumable
// uploads); manifests and tags follow. It speaks the spec's own error envelope
// and HTTP Basic auth (the bearer-token flow lands later).
package oci

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/registry"
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
		log:       deps.Log,
	}
	r.Use(h.requireAuth)
	r.Get("/", h.versionCheck)
	r.Handle("/*", http.HandlerFunc(h.serve))
}

type handler struct {
	blobs     blob.Store
	auth      *auth.Service
	manifests *manifestStore
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

	switch p.kind {
	case opBlobUpload:
		h.serveUpload(w, r, p)
	case opBlob:
		h.serveBlob(w, r, p)
	case opManifest:
		h.serveManifest(w, r, p)
	case opTags:
		h.serveTags(w, r, p)
	default:
		writeError(w, h.log, http.StatusNotFound, codeNameInvalid, "unsupported path")
	}
}

// requireAuth enforces HTTP Basic auth on every /v2 route, challenging
// unauthenticated clients so `docker login` and friends can present credentials.
func (h *handler) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := h.authenticate(r)
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="Platbor"`)
			writeError(w, h.log, http.StatusUnauthorized, codeUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), user)))
	})
}

// authenticate resolves Basic credentials: the password may be a personal
// access token (username ignored) or the account password.
func (h *handler) authenticate(r *http.Request) (auth.User, bool) {
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

// internalError logs and returns a generic 500 in the spec envelope.
func (h *handler) internalError(w http.ResponseWriter, msg string, err error) {
	h.log.Error(msg, slog.String("error", err.Error()))
	writeError(w, h.log, http.StatusInternalServerError, codeUnsupported, "internal error")
}
