package cargo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
)

// proxyIndex fetches a crate's index from the upstream, records each version for
// later caching and the browser, and serves the upstream index verbatim (cargo
// downloads via our config.json dl, so the lines need no rewriting).
func (h *handler) proxyIndex(w http.ResponseWriter, r *http.Request, repo repository.Repository, name string) {
	up := upstreamOf(repo)
	body, err := h.upstream.fetchIndex(r.Context(), up, normalizeName(name))
	if err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			writeError(w, http.StatusNotFound, "crate not found")
			return
		}
		h.upstreamError(w, "fetching upstream index", err)
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry struct {
			Name   string `json:"name"`
			Vers   string `json:"vers"`
			Cksum  string `json:"cksum"`
			Yanked bool   `json:"yanked"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Vers == "" {
			continue
		}
		crateName := entry.Name
		if crateName == "" {
			crateName = name
		}
		dlURL, err := h.upstream.downloadURL(r.Context(), up, crateName, entry.Vers, entry.Cksum)
		if err != nil {
			h.log.Warn("cargo resolving download url", "error", err.Error())
			continue
		}
		if err := h.store.cacheIndexRow(r.Context(), repo.ID, crateName, entry.Vers, line, entry.Cksum, dlURL, entry.Yanked); err != nil {
			h.log.Warn("cargo caching index row", "crate", crateName, "version", entry.Vers, "error", err.Error())
		}
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(body)
}

// download serves a crate version's .crate file. For a local repo it streams the
// stored blob; for a proxy it fills the cache from the upstream on a miss.
func (h *handler) download(w http.ResponseWriter, r *http.Request) {
	repo, ok := h.resolveRepo(w, r, false)
	if !ok {
		return
	}
	name := chi.URLParam(r, "crate")
	ver := chi.URLParam(r, "version")
	if name == "" || ver == "" {
		writeError(w, http.StatusNotFound, "not found")
		return
	}

	v, err := h.store.getVersion(r.Context(), repo.ID, normalizeName(name), ver)
	if err != nil {
		if errors.Is(err, ErrVersionNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "resolving version", err)
		return
	}

	if v.BlobDigest == "" {
		if repo.Mode != repository.ModeProxy || v.UpstreamURL == "" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		digest, size, err := h.cacheCrate(r, repo, name, ver, v)
		if err != nil {
			if errors.Is(err, errUpstreamNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			h.upstreamError(w, "caching crate", err)
			return
		}
		v.BlobDigest, v.Size = digest, size
	}

	h.streamBlob(w, r, v.BlobDigest, v.Size, name+"-"+ver+".crate")
}

// cacheCrate fetches a .crate from its upstream URL, verifies its checksum,
// stores it, and records the cached blob.
func (h *handler) cacheCrate(r *http.Request, repo repository.Repository, name, ver string, v version) (string, int64, error) {
	rc, err := h.upstream.fetchCrate(r.Context(), upstreamOf(repo), v.UpstreamURL)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = rc.Close() }()
	data, err := io.ReadAll(io.LimitReader(rc, maxCrateSize))
	if err != nil {
		return "", 0, err
	}
	if v.Cksum != "" {
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), v.Cksum) {
			return "", 0, errors.New("upstream crate checksum mismatch")
		}
	}
	digest, size, err := h.storeBlob(r, data)
	if err != nil {
		return "", 0, err
	}
	if err := h.store.setVersionBlob(r.Context(), repo.ID, normalizeName(name), ver, digest, size); err != nil {
		return "", 0, err
	}
	return digest, size, nil
}

// streamBlob writes a stored .crate to the response.
func (h *handler) streamBlob(w http.ResponseWriter, r *http.Request, digest string, size int64, filename string) {
	rc, err := h.blobs.Open(r.Context(), digest)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "opening crate", err)
		return
	}
	defer func() { _ = rc.Close() }()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	if _, err := io.Copy(w, rc); err != nil {
		h.log.Error("streaming crate", "error", err.Error())
	}
}
