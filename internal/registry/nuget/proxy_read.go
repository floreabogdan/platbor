package nuget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/platbor/platbor/internal/core/blob"
)

// This file holds the NuGet pull-through cache. A proxy repository mirrors an
// upstream V3 feed (api.nuget.org, or a private one): metadata (version lists,
// registration, search) is fetched fresh on each read — with the upstream's
// resource URLs rewritten to point back at us so downloads flow through the
// cache — while immutable .nupkg blobs are cached lazily on first miss.

// proxyFlatVersions serves the flat-container version list from the upstream.
// Version strings need no rewriting, so the body passes through verbatim.
func (h *handler) proxyFlatVersions(w http.ResponseWriter, r *http.Request, up upstream, idLower string) {
	res, err := h.upstream.resources(r.Context(), up)
	if err != nil {
		h.upstreamError(w, "discovering upstream", err)
		return
	}
	body, err := h.upstream.fetchFlatVersions(r.Context(), up, res, idLower)
	if err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			writeError(w, http.StatusNotFound, "package not found")
			return
		}
		h.upstreamError(w, "fetching upstream versions", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(body)
}

// proxyFlatDownload serves a .nupkg from the local cache, filling it from the
// upstream on a miss. Cached .nupkg blobs are immutable, so a hit is authoritative.
func (h *handler) proxyFlatDownload(w http.ResponseWriter, r *http.Request, up upstream, repositoryID, idLower, versionLower string) {
	// Cache hit: serve locally.
	if digest, size, err := h.store.nupkg(r.Context(), repositoryID, idLower, versionLower); err == nil {
		h.streamNupkg(w, r, digest, size)
		return
	} else if !errors.Is(err, ErrPackageNotFound) {
		h.internalError(w, "resolving cached nupkg", err)
		return
	}

	res, err := h.upstream.resources(r.Context(), up)
	if err != nil {
		h.upstreamError(w, "discovering upstream", err)
		return
	}
	digest, size, err := h.cacheNupkg(r.Context(), up, res, repositoryID, idLower, versionLower)
	if err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.upstreamError(w, "caching nupkg", err)
		return
	}
	h.streamNupkg(w, r, digest, size)
}

// cacheNupkg fetches a .nupkg from the upstream, commits it to the blob store,
// and records a cached version row (so it is GC-safe and future hits are local).
// The version is keyed by the URL's lowercased id/version — what later lookups
// use — while the display id/version come from the embedded .nuspec when readable.
func (h *handler) cacheNupkg(ctx context.Context, up upstream, res upstreamResources, repositoryID, idLower, versionLower string) (string, int64, error) {
	rc, err := h.upstream.fetchNupkg(ctx, up, res, idLower, versionLower)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(io.LimitReader(rc, maxNupkgSize))
	if err != nil {
		return "", 0, fmt.Errorf("buffering upstream nupkg: %w", err)
	}

	upl, err := h.blobs.StartUpload(ctx)
	if err != nil {
		return "", 0, err
	}
	if _, err := upl.Write(data); err != nil {
		_ = upl.Abort(ctx)
		return "", 0, fmt.Errorf("writing nupkg: %w", err)
	}
	desc, err := upl.Commit(ctx, blob.DigestBytes(data))
	if err != nil {
		return "", 0, fmt.Errorf("committing nupkg: %w", err)
	}

	// The .nuspec gives the authoritative display id/version and dependency
	// groups for registration; fall back to the URL identity if it is unreadable.
	idOriginal, version, nuspec := idLower, versionLower, []byte(nil)
	if pid, ver, spec, perr := parseNupkg(data); perr == nil {
		idOriginal, version, nuspec = pid, ver, spec
	}
	if err := h.store.cacheVersion(ctx, cacheInput{
		RepositoryID: repositoryID,
		IDOriginal:   idOriginal,
		IDLower:      idLower,
		Version:      version,
		VersionLower: versionLower,
		NupkgDigest:  desc.Digest,
		NupkgSize:    desc.Size,
		Nuspec:       nuspec,
	}); err != nil {
		return "", 0, err
	}
	return desc.Digest, desc.Size, nil
}

// proxyRegistration serves package metadata from the upstream, inlining any
// externally-paged items and rewriting the upstream's resource URLs to point
// back at this feed so the client resolves dependencies and downloads through us.
func (h *handler) proxyRegistration(w http.ResponseWriter, r *http.Request, up upstream, project, repoKey, idLower string) {
	res, err := h.upstream.resources(r.Context(), up)
	if err != nil {
		h.upstreamError(w, "discovering upstream", err)
		return
	}
	body, err := h.upstream.fetchRegistration(r.Context(), up, res, idLower)
	if err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			writeError(w, http.StatusNotFound, "package not found")
			return
		}
		h.upstreamError(w, "fetching upstream registration", err)
		return
	}
	inlined, err := h.inlineRegistrationPages(r.Context(), up, body)
	if err != nil {
		h.upstreamError(w, "inlining registration pages", err)
		return
	}
	rewritten := rewriteUpstreamURLs(inlined, res, baseURL(r, project, repoKey))
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(rewritten)
}

// inlineRegistrationPages replaces any registration page that is a bare
// reference (an `@id` with no inline `items`) with its fetched contents, so the
// client never has to follow a page link back to the upstream.
func (h *handler) inlineRegistrationPages(ctx context.Context, up upstream, body []byte) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(body, &doc); err != nil {
		return body, nil // not an object we understand; pass through
	}
	pagesRaw, ok := doc["items"]
	if !ok {
		return body, nil
	}
	var pages []map[string]json.RawMessage
	if err := json.Unmarshal(pagesRaw, &pages); err != nil {
		return body, nil
	}
	for i, page := range pages {
		if _, hasItems := page["items"]; hasItems {
			continue
		}
		idRaw, ok := page["@id"]
		if !ok {
			continue
		}
		var pageURL string
		if json.Unmarshal(idRaw, &pageURL) != nil || pageURL == "" {
			continue
		}
		pageBody, err := h.upstream.fetchURL(ctx, up, pageURL)
		if err != nil {
			return nil, err
		}
		var full map[string]json.RawMessage
		if json.Unmarshal(pageBody, &full) == nil {
			pages[i] = full
		}
	}
	if b, err := json.Marshal(pages); err == nil {
		doc["items"] = b
	}
	return json.Marshal(doc)
}

// proxySearch forwards the client's search to the upstream and rewrites the
// registration URLs in the result to point back at this feed.
func (h *handler) proxySearch(w http.ResponseWriter, r *http.Request, up upstream, project, repoKey string) {
	res, err := h.upstream.resources(r.Context(), up)
	if err != nil {
		h.upstreamError(w, "discovering upstream", err)
		return
	}
	if res.searchBase == "" {
		writeJSON(w, http.StatusOK, map[string]any{"totalHits": 0, "data": []any{}})
		return
	}
	body, err := h.upstream.fetchSearch(r.Context(), up, res, r.URL.RawQuery)
	if err != nil {
		h.upstreamError(w, "fetching upstream search", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(rewriteUpstreamURLs(body, res, baseURL(r, project, repoKey)))
}

// rewriteUpstreamURLs replaces the upstream's registration and flat-container
// base URLs with this feed's equivalents. NuGet does not escape slashes in JSON,
// so a prefix replacement over the raw body is exact: the id/version path
// structure after each base is identical between the upstream and us.
func rewriteUpstreamURLs(body []byte, res upstreamResources, ourBase string) []byte {
	s := string(body)
	if res.regBase != "" {
		s = strings.ReplaceAll(s, trimSlash(res.regBase)+"/", ourBase+"/v3/registrations/")
	}
	if res.flatBase != "" {
		s = strings.ReplaceAll(s, trimSlash(res.flatBase)+"/", ourBase+"/v3-flatcontainer/")
	}
	return []byte(s)
}

// streamNupkg writes a stored .nupkg to the response.
func (h *handler) streamNupkg(w http.ResponseWriter, r *http.Request, digest string, size int64) {
	rc, err := h.blobs.Open(r.Context(), digest)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.internalError(w, "opening nupkg", err)
		return
	}
	defer func() { _ = rc.Close() }()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if _, err := io.Copy(w, rc); err != nil {
		h.log.Error("streaming nupkg", "error", err.Error())
	}
}

// upstreamError reports an upstream fetch failure as a 502, logging the cause.
func (h *handler) upstreamError(w http.ResponseWriter, msg string, err error) {
	h.log.Warn("nuget "+msg, "error", err.Error())
	writeError(w, http.StatusBadGateway, "upstream unavailable")
}
