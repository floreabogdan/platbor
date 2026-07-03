package npm

import (
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/platbor/platbor/internal/core/blob"
)

// This file holds the pull-through cache. A proxy project mirrors an upstream
// npm registry (registry.npmjs.org, or a private one): the packument is fetched
// fresh on each read (the index is mutable) with a fall back to any cached copy
// when the upstream is offline, and tarballs are cached lazily on first miss
// (they are immutable, so a cache hit is authoritative).

// proxyPackument fetches a package document from the upstream, rewrites its
// tarball URLs to point at this registry, and serves it. On an upstream failure
// it falls back to whatever versions have already been cached locally.
func (h *handler) proxyPackument(w http.ResponseWriter, r *http.Request, up upstream, repositoryID, project, repoKey, pkg string) {
	body, err := h.upstream.fetchPackument(r.Context(), up, pkg)
	if err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			// Nothing upstream; the local cache is the last word.
			h.getLocalPackument(w, r, repositoryID, project, repoKey, pkg, http.StatusNotFound)
			return
		}
		h.log.Warn("npm upstream packument unreachable; trying cache",
			slog.String("pkg", pkg), slog.String("error", err.Error()))
		h.getLocalPackument(w, r, repositoryID, project, repoKey, pkg, http.StatusBadGateway)
		return
	}

	rewritten, err := rewritePackument(body, registryBase(r, project, repoKey), pkg)
	if err != nil {
		h.internalError(w, "rewriting packument", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(rewritten)
}

// getLocalPackument serves the locally cached packument, used as the offline
// fallback for a proxy. notFoundStatus is what to return when nothing is cached.
func (h *handler) getLocalPackument(w http.ResponseWriter, r *http.Request, repositoryID, project, repoKey, pkg string, notFoundStatus int) {
	versions, distTags, err := h.store.packument(r.Context(), repositoryID, pkg)
	if err != nil {
		if errors.Is(err, ErrPackageNotFound) {
			writeError(w, h.log, notFoundStatus, "package not found: "+pkg)
			return
		}
		h.internalError(w, "reading cached packument", err)
		return
	}
	h.writePackument(w, r, project, repoKey, pkg, versions, distTags)
}

// proxyTarball serves a tarball from the local cache, filling it from the
// upstream on a miss. Cached tarballs are immutable, so a hit is authoritative.
func (h *handler) proxyTarball(w http.ResponseWriter, r *http.Request, up upstream, repositoryID, pkg, filename string) {
	version, ok := versionFromFilename(pkg, filename)
	if !ok {
		writeError(w, h.log, http.StatusNotFound, "not found")
		return
	}

	// Cache hit: serve locally.
	if digest, size, err := h.store.tarball(r.Context(), repositoryID, pkg, version); err == nil {
		h.streamBlob(w, r, digest, size)
		return
	} else if !errors.Is(err, ErrPackageNotFound) {
		h.internalError(w, "resolving tarball", err)
		return
	}

	// Cache miss: fetch from upstream, store, then serve.
	digest, size, err := h.cacheTarball(r.Context(), up, repositoryID, pkg, version, filename)
	if err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			writeError(w, h.log, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "caching tarball", err)
		return
	}
	h.streamBlob(w, r, digest, size)
}

// cacheTarball fetches a tarball from the upstream, commits it to the blob
// store, and records a cached version row (so it is GC-safe and future hits are
// local). It returns the stored digest and size.
func (h *handler) cacheTarball(ctx context.Context, up upstream, repositoryID, pkg, version, filename string) (string, int64, error) {
	rc, err := h.upstream.fetchTarball(ctx, up, pkg, filename)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = rc.Close() }()

	tarball, err := io.ReadAll(io.LimitReader(rc, maxPublishBody))
	if err != nil {
		return "", 0, fmt.Errorf("buffering upstream tarball: %w", err)
	}

	up1, err := h.blobs.StartUpload(ctx)
	if err != nil {
		return "", 0, err
	}
	if _, err := up1.Write(tarball); err != nil {
		_ = up1.Abort(ctx)
		return "", 0, fmt.Errorf("writing tarball: %w", err)
	}
	desc, err := up1.Commit(ctx, blob.DigestBytes(tarball))
	if err != nil {
		return "", 0, fmt.Errorf("committing tarball: %w", err)
	}

	shasum := hexSHA1(tarball)
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(sha512Sum(tarball))
	// A minimal version document is enough for the offline-fallback packument;
	// the fresh upstream packument is what clients normally read.
	manifest, _ := json.Marshal(map[string]any{"name": pkg, "version": version})
	if err := h.store.cacheVersion(ctx, versionInput{
		Version:       version,
		Manifest:      manifest,
		TarballDigest: desc.Digest,
		TarballSize:   desc.Size,
		Shasum:        shasum,
		Integrity:     integrity,
	}, repositoryID, pkg); err != nil {
		return "", 0, err
	}
	return desc.Digest, desc.Size, nil
}

// rewritePackument rewrites every versions[*].dist.tarball URL in an upstream
// packument to point at this registry, so downloads flow through (and are cached
// by) us. Other fields, including the upstream dist.integrity/shasum, are left
// intact.
func rewritePackument(body []byte, base, pkg string) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	versionsRaw, ok := doc["versions"]
	if !ok {
		return body, nil
	}
	var versions map[string]json.RawMessage
	if err := json.Unmarshal(versionsRaw, &versions); err != nil {
		return nil, err
	}
	for ver, raw := range versions {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue
		}
		var dist map[string]json.RawMessage
		if d, ok := obj["dist"]; ok {
			_ = json.Unmarshal(d, &dist)
		}
		if dist == nil {
			dist = map[string]json.RawMessage{}
		}
		dist["tarball"] = jsonString(tarballURL(base, pkg, ver))
		if distBytes, err := json.Marshal(dist); err == nil {
			obj["dist"] = distBytes
		}
		if objBytes, err := json.Marshal(obj); err == nil {
			versions[ver] = objBytes
		}
	}
	if versionsBytes, err := json.Marshal(versions); err == nil {
		doc["versions"] = versionsBytes
	}
	return json.Marshal(doc)
}

// streamBlob writes a stored tarball to the response.
func (h *handler) streamBlob(w http.ResponseWriter, r *http.Request, digest string, size int64) {
	rc, err := h.blobs.Open(r.Context(), digest)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeError(w, h.log, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "opening tarball", err)
		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", itoa(size))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := io.Copy(w, rc); err != nil {
		h.log.Error("streaming tarball", slog.String("error", err.Error()))
	}
}

func hexSHA1(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

func sha512Sum(data []byte) []byte {
	sum := sha512.Sum512(data)
	return sum[:]
}
