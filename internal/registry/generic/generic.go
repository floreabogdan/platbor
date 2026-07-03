// Package generic implements a generic artifact repository under
// /generic/<project>/<repo>/<path>: arbitrary versioned files with no format
// semantics. PUT stores bytes at a path (overwriting), GET/HEAD serve them with
// checksum headers, and a sibling "<path>.sha256" (or .sha1/.md5) returns the
// checksum as text. Bytes live in the shared content-addressable blob store;
// auth is HTTP Basic (password or personal access token) or a bearer token.
package generic

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/registry"
)

// maxFileSize caps a single generic upload.
const maxFileSize = 5 << 30 // 5 GiB

// Adapter is the generic artifact format.
type Adapter struct{}

// New returns the generic adapter.
func New() *Adapter { return &Adapter{} }

// Key implements registry.Adapter.
func (a *Adapter) Key() string { return "generic" }

// Mount registers the generic routes. r is already scoped to /generic.
func (a *Adapter) Mount(r chi.Router, deps registry.Deps) {
	h := &handler{
		blobs: deps.Blobs,
		auth:  deps.Auth,
		store: newFileStore(deps.DB),
		log:   deps.Log,
	}
	r.Route("/{project}", func(sub chi.Router) {
		sub.Handle("/*", http.HandlerFunc(h.serve))
	})
}

type handler struct {
	blobs blob.Store
	auth  *auth.Service
	store *fileStore
	log   *slog.Logger
}

// checksumSuffixes maps a sibling suffix to the stored checksum it returns.
var checksumSuffixes = []string{".sha256", ".sha1", ".md5"}

func (h *handler) serve(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
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

	projectID, err := h.store.resolveProject(r.Context(), project)
	if err != nil {
		if errors.Is(err, errProjectNotFound) {
			writeError(w, http.StatusNotFound, "project not found: "+project)
			return
		}
		h.internalError(w, "resolving project", err)
		return
	}

	switch r.Method {
	case http.MethodPut:
		h.upload(w, r, projectID, path)
	case http.MethodGet, http.MethodHead:
		h.download(w, r, projectID, path)
	case http.MethodDelete:
		h.remove(w, r, projectID, path)
	default:
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

// upload streams the request body into the blob store while computing its
// checksums, then records the file at its path. Proxy projects are read-only.
func (h *handler) upload(w http.ResponseWriter, r *http.Request, projectID, path string) {
	if proxy, err := h.store.isProxy(r.Context(), projectID); err != nil {
		h.internalError(w, "checking proxy", err)
		return
	} else if proxy {
		writeError(w, http.StatusForbidden, "cannot upload to a proxy project")
		return
	}

	upload, err := h.blobs.StartUpload(r.Context())
	if err != nil {
		h.internalError(w, "starting upload", err)
		return
	}
	h256, h1, hm := sha256.New(), sha1.New(), md5.New()
	mw := io.MultiWriter(upload, h256, h1, hm)

	size, err := io.Copy(mw, io.LimitReader(r.Body, maxFileSize))
	if err != nil {
		_ = upload.Abort(r.Context())
		h.internalError(w, "buffering upload", err)
		return
	}
	digest := "sha256:" + hex.EncodeToString(h256.Sum(nil))
	desc, err := upload.Commit(r.Context(), digest)
	if err != nil {
		h.internalError(w, "committing blob", err)
		return
	}

	if err := h.store.put(r.Context(), filePut{
		ProjectID:  projectID,
		Path:       path,
		BlobDigest: desc.Digest,
		Size:       size,
		SHA256:     hex.EncodeToString(h256.Sum(nil)),
		SHA1:       hex.EncodeToString(h1.Sum(nil)),
		MD5:        hex.EncodeToString(hm.Sum(nil)),
		Actor:      actorFrom(r),
	}); err != nil {
		h.internalError(w, "recording file", err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"path":   path,
		"size":   size,
		"sha256": hex.EncodeToString(h256.Sum(nil)),
	})
}

// download serves a file, or a checksum when the path is a "<file>.sha256"-style
// sibling of a stored file (and no real file exists at that exact path).
func (h *handler) download(w http.ResponseWriter, r *http.Request, projectID, path string) {
	file, err := h.store.get(r.Context(), projectID, path)
	if err == nil {
		h.serveFile(w, r, file)
		return
	}
	if !errors.Is(err, ErrFileNotFound) {
		h.internalError(w, "getting file", err)
		return
	}

	// Not a real file: maybe a checksum sibling of one.
	for _, suf := range checksumSuffixes {
		if base, ok := strings.CutSuffix(path, suf); ok {
			if sum, ok := h.checksum(r, projectID, base, suf); ok {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				_, _ = io.WriteString(w, sum+"\n")
				return
			}
		}
	}
	writeError(w, http.StatusNotFound, "not found")
}

// checksum returns the stored checksum of base for the given suffix.
func (h *handler) checksum(r *http.Request, projectID, base, suffix string) (string, bool) {
	file, err := h.store.get(r.Context(), projectID, base)
	if err != nil {
		return "", false
	}
	switch suffix {
	case ".sha256":
		return file.SHA256, true
	case ".sha1":
		return file.SHA1, true
	case ".md5":
		return file.MD5, true
	default:
		return "", false
	}
}

func (h *handler) serveFile(w http.ResponseWriter, r *http.Request, file storedFile) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	w.Header().Set("X-Checksum-Sha256", file.SHA256)
	w.Header().Set("X-Checksum-Sha1", file.SHA1)
	w.Header().Set("X-Checksum-Md5", file.MD5)
	w.Header().Set("ETag", `"`+file.SHA256+`"`)
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	rc, err := h.blobs.Open(r.Context(), file.BlobDigest)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "opening blob", err)
		return
	}
	defer func() { _ = rc.Close() }()
	if _, err := io.Copy(w, rc); err != nil {
		h.log.Error("streaming generic file", slog.String("error", err.Error()))
	}
}

func (h *handler) remove(w http.ResponseWriter, r *http.Request, projectID, path string) {
	if proxy, err := h.store.isProxy(r.Context(), projectID); err != nil {
		h.internalError(w, "checking proxy", err)
		return
	} else if proxy {
		writeError(w, http.StatusForbidden, "cannot delete from a proxy project")
		return
	}
	if err := h.store.delete(r.Context(), projectID, path, actorFrom(r)); err != nil {
		if errors.Is(err, ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "deleting file", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
			h.log.Error("generic token auth", slog.String("error", err.Error()))
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
