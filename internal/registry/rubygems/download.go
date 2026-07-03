package rubygems

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
)

// download serves a .gem file by its full name. For a local repo it streams the
// stored blob; for a proxy it fills the cache from the upstream on a miss.
func (h *handler) download(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, false)
	if !ok {
		return
	}
	file := chi.URLParam(r, "file")
	fullName, ok := strings.CutSuffix(file, ".gem")
	if !ok || fullName == "" || strings.Contains(fullName, "/") {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	f, err := h.store.getFile(r.Context(), repo.ID, fullName)
	if err != nil {
		if errors.Is(err, ErrGemNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "resolving gem", err)
		return
	}

	if f.BlobDigest == "" {
		if repo.Mode != repository.ModeProxy || f.UpstreamURL == "" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		digest, size, err := h.cacheGem(r, repo, fullName, f)
		if err != nil {
			if errors.Is(err, errUpstreamNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			h.upstreamError(w, "caching gem", err)
			return
		}
		f.BlobDigest, f.Size = digest, size
	}

	h.streamBlob(w, r, f.BlobDigest, f.Size, fullName+".gem")
}

// cacheGem fetches a .gem from its upstream URL, verifies its checksum, stores
// it, and records the cached blob.
func (h *handler) cacheGem(r *http.Request, repo repository.Repository, fullName string, f gemFile) (string, int64, error) {
	rc, err := h.upstream.fetchGem(r.Context(), upstreamOf(repo), f.UpstreamURL)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, maxGemSize))
	if err != nil {
		return "", 0, err
	}
	if f.SHA256 != "" {
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), f.SHA256) {
			return "", 0, errors.New("upstream gem checksum mismatch")
		}
	}
	digest, size, err := h.storeBlob(r, data)
	if err != nil {
		return "", 0, err
	}
	if err := h.store.setVersionBlob(r.Context(), repo.ID, fullName, digest, size); err != nil {
		return "", 0, err
	}
	return digest, size, nil
}

// streamBlob writes a stored .gem to the response.
func (h *handler) streamBlob(w http.ResponseWriter, r *http.Request, digest string, size int64, filename string) {
	rc, err := h.blobs.Open(r.Context(), digest)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "opening gem", err)
		return
	}
	defer func() { _ = rc.Close() }()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	if _, err := io.Copy(w, rc); err != nil {
		h.log.Error("streaming gem", "error", err.Error())
	}
}
