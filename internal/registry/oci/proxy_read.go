package oci

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/registry/proxy"
)

// This file holds the pull-through cache: when a proxy project misses locally,
// the requested manifest or blob is fetched from the configured upstream, stored
// through the adapter's own stores, and then served. Immutable content (blobs,
// manifests by digest) is cached permanently; a tag is refreshed from upstream
// on each pull and falls back to the cached copy when the upstream is offline.

// loadManifest resolves a manifest for serving, transparently filling the cache
// for a proxy project. It returns ErrManifestNotFound when neither the local
// cache nor the upstream has the content.
func (h *handler) loadManifest(ctx context.Context, projectID, repo, ref string) (storedManifest, error) {
	up, isProxy, err := h.manifests.proxyUpstream(ctx, projectID)
	if err != nil {
		return storedManifest{}, err
	}

	if isDigestRef(ref) {
		// A digest is immutable: a cache hit is authoritative.
		m, err := h.manifests.getManifest(ctx, projectID, repo, ref)
		if err == nil || !errors.Is(err, ErrManifestNotFound) {
			return m, err
		}
		if !isProxy {
			return storedManifest{}, ErrManifestNotFound
		}
		return h.cacheManifest(ctx, up, projectID, repo, ref)
	}

	if !isProxy {
		digest, err := h.manifests.resolveTag(ctx, projectID, repo, ref)
		if err != nil {
			return storedManifest{}, err
		}
		return h.manifests.getManifest(ctx, projectID, repo, digest)
	}

	// Proxy tag pull: prefer a fresh copy, fall back to cache when offline.
	m, err := h.cacheManifest(ctx, up, projectID, repo, ref)
	if err == nil {
		return m, nil
	}
	if errors.Is(err, proxy.ErrUpstreamNotFound) {
		return storedManifest{}, ErrManifestNotFound
	}
	if cached, ok := h.cachedTag(ctx, projectID, repo, ref); ok {
		h.log.Warn("serving cached manifest; upstream unreachable",
			slog.String("repo", repo), slog.String("ref", ref), slog.String("error", err.Error()))
		return cached, nil
	}
	return storedManifest{}, err
}

// cachedTag returns the last cached manifest a tag pointed at, if any.
func (h *handler) cachedTag(ctx context.Context, projectID, repo, tag string) (storedManifest, bool) {
	digest, err := h.manifests.resolveTag(ctx, projectID, repo, tag)
	if err != nil {
		return storedManifest{}, false
	}
	m, err := h.manifests.getManifest(ctx, projectID, repo, digest)
	if err != nil {
		return storedManifest{}, false
	}
	return m, true
}

// cacheManifest fetches a manifest (by tag or digest) from the upstream and
// stores it. When a tag already resolves to the upstream's current digest and
// the manifest is present, it serves the cache without a redundant write.
func (h *handler) cacheManifest(ctx context.Context, up proxy.Upstream, projectID, repo, ref string) (storedManifest, error) {
	fm, err := h.upstream.FetchManifest(ctx, up, repo, ref)
	if err != nil {
		return storedManifest{}, err
	}

	digest, err := upstreamDigest(fm)
	if err != nil {
		return storedManifest{}, err
	}
	mediaType := manifestMediaType(fm.MediaType, jsonMediaType(fm.Bytes))
	if mediaType == "" {
		mediaType = normalizeMediaType(fm.MediaType)
	}
	stored := storedManifest{Digest: digest, MediaType: mediaType, Payload: fm.Bytes, Size: int64(len(fm.Bytes))}

	// Already cached at this exact digest, and (for a tag) the tag already points
	// here: nothing to write.
	cachedAtDigest := false
	if existing, gerr := h.manifests.getManifest(ctx, projectID, repo, digest); gerr == nil {
		cachedAtDigest = true
		stored = existing
	}
	tag := ""
	if !isDigestRef(ref) {
		tag = ref
		if cachedAtDigest {
			if current, rerr := h.manifests.resolveTag(ctx, projectID, repo, tag); rerr == nil && current == digest {
				return stored, nil
			}
		}
	} else if cachedAtDigest {
		return stored, nil
	}

	var doc manifestDoc
	_ = json.Unmarshal(fm.Bytes, &doc)
	if err := h.manifests.putManifest(ctx, manifestWrite{
		ProjectID:    projectID,
		Repository:   repo,
		Digest:       digest,
		MediaType:    mediaType,
		Payload:      fm.Bytes,
		Size:         int64(len(fm.Bytes)),
		Tag:          tag,
		Subject:      doc.subjectDigest(),
		ArtifactType: doc.effectiveArtifactType(),
		Actor:        usernameFromContext(ctx),
	}); err != nil {
		return storedManifest{}, err
	}
	return storedManifest{Digest: digest, MediaType: mediaType, Payload: fm.Bytes, Size: int64(len(fm.Bytes))}, nil
}

// cacheBlob fetches a blob from the upstream and commits it to the local store.
// Commit verifies the content hashes to digest, so a corrupted upstream response
// is rejected rather than cached.
func (h *handler) cacheBlob(ctx context.Context, up proxy.Upstream, repo, digest string) error {
	rc, _, err := h.upstream.FetchBlob(ctx, up, repo, digest)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	upload, err := h.blobs.StartUpload(ctx)
	if err != nil {
		return err
	}
	if _, err := io.Copy(upload, rc); err != nil {
		_ = upload.Abort(ctx)
		return fmt.Errorf("buffering upstream blob: %w", err)
	}
	if _, err := upload.Commit(ctx, digest); err != nil {
		return fmt.Errorf("committing upstream blob %s: %w", digest, err)
	}
	return nil
}

// upstreamDigest returns the digest to cache a fetched manifest under: the
// upstream's Docker-Content-Digest when present (verified against the bytes), or
// the canonical sha256 of the bytes when the upstream omitted the header.
func upstreamDigest(m proxy.Manifest) (string, error) {
	if m.Digest == "" {
		return blob.DigestBytes(m.Bytes), nil
	}
	if err := blob.ValidateDigest(m.Digest); err != nil {
		return "", fmt.Errorf("upstream returned an invalid digest %q: %w", m.Digest, err)
	}
	if !blob.MatchesDigest(m.Digest, m.Bytes) {
		return "", fmt.Errorf("upstream manifest does not match its digest %s", m.Digest)
	}
	return m.Digest, nil
}

// jsonMediaType extracts a manifest document's own mediaType field, used as a
// fallback when the upstream's Content-Type is missing or unrecognized.
func jsonMediaType(payload []byte) string {
	var doc struct {
		MediaType string `json:"mediaType"`
	}
	_ = json.Unmarshal(payload, &doc)
	return doc.MediaType
}
