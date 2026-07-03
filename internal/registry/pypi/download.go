package pypi

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
)

// download serves a distribution file by name. For a proxy repository it fills
// the cache from the upstream on a miss; for a local repository it streams the
// stored blob.
func (h *handler) download(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, false)
	if !ok {
		return
	}
	filename := strings.Trim(chi.URLParam(r, "*"), "/")
	if filename == "" || strings.Contains(filename, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	f, err := h.store.getFile(r.Context(), repo.ID, filename)
	if err != nil {
		if errors.Is(err, ErrFileNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "resolving file", err)
		return
	}

	// Proxy cache miss: fetch from the upstream, store, then serve.
	if f.BlobDigest == "" {
		if repo.Mode != repository.ModeProxy || f.UpstreamURL == "" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		digest, size, err := h.cacheFile(r, upstreamOf(repo), f)
		if err != nil {
			if errors.Is(err, errUpstreamNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			h.upstreamError(w, "caching distribution", err)
			return
		}
		f.BlobDigest, f.Size = digest, size
	}

	h.streamBlob(w, r, f.BlobDigest, f.Size, filename)
}

// streamBlob writes a stored distribution to the response.
func (h *handler) streamBlob(w http.ResponseWriter, r *http.Request, digest string, size int64, filename string) {
	rc, err := h.blobs.Open(r.Context(), digest)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "opening distribution", err)
		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := io.Copy(w, rc); err != nil {
		h.log.Error("streaming distribution", "error", err.Error())
	}
}
