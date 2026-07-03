package goproxy

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
)

// maxFileSize caps a single cached module file (zips can be large).
const maxFileSize = 2 << 30 // 2 GiB

// serveFile serves an immutable per-version file (.info/.mod/.zip): a cache hit
// streams the stored blob; a miss fetches it from the upstream, caches it, and
// serves it.
func (h *handler) serveFile(w http.ResponseWriter, r *http.Request, repo repository.Repository, splat string, req goRequest) {
	file, err := h.store.get(r.Context(), repo.ID, splat)
	if err != nil && !errors.Is(err, ErrFileNotFound) {
		h.internalError(w, "getting file", err)
		return
	}
	if err == nil && file.BlobDigest != "" {
		h.streamBlob(w, r, file.BlobDigest, file.Size, req)
		return
	}

	// Cache miss: fetch, store, serve.
	up := upstreamOf(repo)
	rc, err := h.upstream.fetch(r.Context(), up, splat)
	if err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.upstreamError(w, "fetching module file", err)
		return
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(io.LimitReader(rc, maxFileSize))
	if err != nil {
		h.internalError(w, "buffering module file", err)
		return
	}
	digest, size, err := h.storeBlob(r, data)
	if err != nil {
		h.internalError(w, "caching module file", err)
		return
	}
	if err := h.store.cache(r.Context(), cacheInput{
		RepositoryID: repo.ID,
		Module:       req.module,
		Version:      req.version,
		Kind:         req.ext,
		Path:         splat,
		BlobDigest:   digest,
		Size:         size,
		UpstreamURL:  trimBase(up.Base) + "/" + splat,
	}); err != nil {
		h.internalError(w, "recording module file", err)
		return
	}

	w.Header().Set("Content-Type", contentType(req))
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(data)
}

// streamBlob writes a cached file from the blob store.
func (h *handler) streamBlob(w http.ResponseWriter, r *http.Request, digest string, size int64, req goRequest) {
	rc, err := h.blobs.Open(r.Context(), digest)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "opening blob", err)
		return
	}
	defer func() { _ = rc.Close() }()
	w.Header().Set("Content-Type", contentType(req))
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := io.Copy(w, rc); err != nil {
		h.log.Error("streaming go module file", "error", err.Error())
	}
}

// proxyFresh proxies a mutable listing (list or @latest) straight from the
// upstream without caching, so version data stays current.
func (h *handler) proxyFresh(w http.ResponseWriter, r *http.Request, repo repository.Repository, splat string, req goRequest) {
	rc, err := h.upstream.fetch(r.Context(), upstreamOf(repo), splat)
	if err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.upstreamError(w, "fetching listing", err)
		return
	}
	defer func() { _ = rc.Close() }()
	w.Header().Set("Content-Type", contentType(req))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := io.Copy(w, rc); err != nil {
		h.log.Error("streaming go listing", "error", err.Error())
	}
}

// storeBlob commits bytes to the content-addressable store.
func (h *handler) storeBlob(r *http.Request, data []byte) (string, int64, error) {
	up, err := h.blobs.StartUpload(r.Context())
	if err != nil {
		return "", 0, err
	}
	if _, err := up.Write(data); err != nil {
		_ = up.Abort(r.Context())
		return "", 0, err
	}
	desc, err := up.Commit(r.Context(), blob.DigestBytes(data))
	if err != nil {
		return "", 0, err
	}
	return desc.Digest, desc.Size, nil
}

func trimBase(base string) string {
	for len(base) > 0 && base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}
	return base
}
