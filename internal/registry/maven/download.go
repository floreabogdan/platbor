package maven

import (
	"crypto/md5"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
)

// download serves a file at a path. For a local repository it streams the stored
// blob. For a proxy it fetches from the upstream on a miss: immutable artifact
// files are cached, while maven-metadata.xml (and its checksums) are streamed
// fresh every time because they change as new versions publish.
func (h *handler) download(w http.ResponseWriter, r *http.Request, repo repository.Repository, path string) {
	metadata := isMetadataPath(path)

	if repo.Mode == repository.ModeProxy && metadata {
		h.proxyStreamFresh(w, r, repo, path)
		return
	}

	file, err := h.store.get(r.Context(), repo.ID, path)
	if err != nil && !errors.Is(err, ErrFileNotFound) {
		h.internalError(w, "getting file", err)
		return
	}

	// Cache hit (local file or already-cached proxy artifact).
	if err == nil && file.BlobDigest != "" {
		h.serveFile(w, r, file, path)
		return
	}

	// Proxy miss on an immutable artifact: fetch, cache, then serve.
	if repo.Mode == repository.ModeProxy {
		cached, err := h.cacheArtifact(r, repo, path)
		if err != nil {
			if errors.Is(err, errUpstreamNotFound) {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			h.upstreamError(w, "caching artifact", err)
			return
		}
		h.serveFile(w, r, cached, path)
		return
	}

	writeError(w, http.StatusNotFound, "not found")
}

// serveFile writes a stored file to the response, with Maven's checksum headers.
func (h *handler) serveFile(w http.ResponseWriter, r *http.Request, file storedFile, path string) {
	w.Header().Set("Content-Type", contentType(path))
	w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
	if file.SHA1 != "" {
		w.Header().Set("X-Checksum-Sha1", file.SHA1)
		w.Header().Set("ETag", `"`+file.SHA1+`"`)
	}
	if file.MD5 != "" {
		w.Header().Set("X-Checksum-Md5", file.MD5)
	}
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
		h.log.Error("streaming maven file", "error", err.Error())
	}
}

// cacheArtifact fetches an immutable artifact from the upstream, commits it to
// the blob store, records its coordinates, and returns the stored file.
func (h *handler) cacheArtifact(r *http.Request, repo repository.Repository, path string) (storedFile, error) {
	up := upstreamOf(repo)
	rc, err := h.upstream.fetchFile(r.Context(), up, path)
	if err != nil {
		return storedFile{}, err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(io.LimitReader(rc, maxFileSize))
	if err != nil {
		return storedFile{}, err
	}
	digest, sha1sum, md5sum, err := h.storeBlob(r, data)
	if err != nil {
		return storedFile{}, err
	}
	upURL := trimBase(up.Base) + "/" + path
	if err := h.store.put(r.Context(), filePut{
		RepositoryID: repo.ID,
		ProjectID:    repo.ProjectID,
		Path:         path,
		BlobDigest:   digest,
		Size:         int64(len(data)),
		SHA1:         sha1sum,
		MD5:          md5sum,
		UpstreamURL:  upURL,
		Actor:        actorFrom(r),
	}); err != nil {
		return storedFile{}, err
	}
	return storedFile{BlobDigest: digest, Size: int64(len(data)), SHA1: sha1sum, MD5: md5sum}, nil
}

// proxyStreamFresh proxies a mutable metadata file straight from the upstream
// without caching it, so version lists stay current.
func (h *handler) proxyStreamFresh(w http.ResponseWriter, r *http.Request, repo repository.Repository, path string) {
	rc, err := h.upstream.fetchFile(r.Context(), upstreamOf(repo), path)
	if err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.upstreamError(w, "fetching metadata", err)
		return
	}
	defer func() { _ = rc.Close() }()
	w.Header().Set("Content-Type", contentType(path))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := io.Copy(w, rc); err != nil {
		h.log.Error("streaming maven metadata", "error", err.Error())
	}
}

// storeBlob commits bytes to the CAS and returns the digest plus sha1/md5.
func (h *handler) storeBlob(r *http.Request, data []byte) (digest, sha1sum, md5sum string, err error) {
	up, err := h.blobs.StartUpload(r.Context())
	if err != nil {
		return "", "", "", err
	}
	if _, err := up.Write(data); err != nil {
		_ = up.Abort(r.Context())
		return "", "", "", err
	}
	desc, err := up.Commit(r.Context(), blob.DigestBytes(data))
	if err != nil {
		return "", "", "", err
	}
	s1, m5 := sha1.Sum(data), md5.Sum(data)
	return desc.Digest, hex.EncodeToString(s1[:]), hex.EncodeToString(m5[:]), nil
}

func (h *handler) upstreamError(w http.ResponseWriter, msg string, err error) {
	h.log.Warn("maven "+msg, "error", err.Error())
	writeError(w, http.StatusBadGateway, "upstream unavailable")
}

func trimBase(base string) string {
	for len(base) > 0 && base[len(base)-1] == '/' {
		base = base[:len(base)-1]
	}
	return base
}

// contentType picks a Content-Type from a Maven file's extension. Maven clients
// do not rely on it, but a sensible value keeps caches and browsers happy.
func contentType(path string) string {
	switch {
	case hasSuffix(path, ".xml"), hasSuffix(path, ".pom"):
		return "application/xml"
	case hasSuffix(path, ".jar"), hasSuffix(path, ".war"), hasSuffix(path, ".zip"):
		return "application/java-archive"
	case hasSuffix(path, ".sha1"), hasSuffix(path, ".md5"), hasSuffix(path, ".sha256"):
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

func hasSuffix(s, suf string) bool {
	return len(s) >= len(suf) && s[len(s)-len(suf):] == suf
}
