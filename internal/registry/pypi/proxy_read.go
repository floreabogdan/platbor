package pypi

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/blob"
)

// This file holds the PyPI pull-through cache. A proxy repository mirrors an
// upstream simple index (pypi.org): the index is fetched fresh on each read, its
// file links rewritten to point back at us (so downloads flow through the cache),
// and each file's upstream URL recorded so the content can be fetched lazily on
// first download. Distribution files are immutable, so a cached file is
// authoritative.

var (
	anchorRe = regexp.MustCompile(`(?is)<a\b([^>]*)>(.*?)</a>`)
	hrefRe   = regexp.MustCompile(`(?i)href="([^"]*)"`)
	reqPyRe  = regexp.MustCompile(`(?i)data-requires-python="([^"]*)"`)
)

// proxySimple fetches a project's simple index from the upstream, records each
// file (URL + hash) for lazy caching, and serves our own page whose links point
// back at this repository.
func (h *handler) proxySimple(w http.ResponseWriter, r *http.Request, up upstream, repositoryID, name string) {
	body, err := h.upstream.fetchSimple(r.Context(), up, name)
	if err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			writeError(w, http.StatusNotFound, "package not found")
			return
		}
		h.upstreamError(w, "fetching upstream index", err)
		return
	}

	pageURL := strings.TrimRight(up.SimpleBase, "/") + "/" + name + "/"
	files := parseSimple(body, pageURL, name)
	if len(files) == 0 {
		writeError(w, http.StatusNotFound, "package not found")
		return
	}
	// Record the files so a later download knows where to fetch them. Best-effort:
	// a cache-bookkeeping failure must not break serving the (fresh) index.
	for _, f := range files {
		if err := h.store.cacheIndexRow(r.Context(), repositoryID, name, name, f); err != nil {
			h.log.Warn("pypi caching index row", "file", f.Filename, "error", err.Error())
		}
	}
	base := baseURL(r, chi.URLParam(r, "project"), chi.URLParam(r, "repo"))
	writeHTML(w, renderSimplePage(name, files, base))
}

// parseSimple extracts the distribution files from an upstream simple-index page.
// Each anchor's href gives the download URL (with a #sha256 fragment) and the
// optional data-requires-python attribute; the link text is the filename.
func parseSimple(body []byte, pageURL, name string) []file {
	pageRef, _ := url.Parse(pageURL)
	var files []file
	for _, m := range anchorRe.FindAllSubmatch(body, -1) {
		attrs := string(m[1])
		filename := strings.TrimSpace(html.UnescapeString(stripTags(string(m[2]))))
		if filename == "" {
			continue
		}
		href := ""
		if hm := hrefRe.FindStringSubmatch(attrs); hm != nil {
			href = html.UnescapeString(hm[1])
		}
		if href == "" {
			continue
		}
		rawURL, sha := href, ""
		if i := strings.Index(href, "#"); i >= 0 {
			rawURL = href[:i]
			if frag := href[i+1:]; strings.HasPrefix(strings.ToLower(frag), "sha256=") {
				sha = frag[len("sha256="):]
			}
		}
		// Resolve relative file URLs against the page (pypi.org uses absolute ones).
		if abs, err := url.Parse(rawURL); err == nil && pageRef != nil {
			rawURL = pageRef.ResolveReference(abs).String()
		}
		reqPy := ""
		if rm := reqPyRe.FindStringSubmatch(attrs); rm != nil {
			reqPy = html.UnescapeString(rm[1])
		}
		files = append(files, file{
			Filename:       filename,
			Version:        versionFromFilename(filename, name),
			SHA256:         sha,
			RequiresPython: reqPy,
			UpstreamURL:    rawURL,
		})
	}
	return files
}

// stripTags removes any nested tags from an anchor's inner text (defensive; the
// filename is normally plain text).
func stripTags(s string) string {
	for {
		open := strings.IndexByte(s, '<')
		if open < 0 {
			return s
		}
		end := strings.IndexByte(s[open:], '>')
		if end < 0 {
			return s[:open]
		}
		s = s[:open] + s[open+end+1:]
	}
}

// cacheFile fetches a distribution from its upstream URL, verifies its hash,
// commits it to the blob store, and records the cached digest so future hits are
// local. It returns the stored digest and size.
func (h *handler) cacheFile(r *http.Request, up upstream, f file) (string, int64, error) {
	rc, err := h.upstream.fetchFile(r.Context(), up, f.UpstreamURL)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = rc.Close() }()

	data, err := io.ReadAll(io.LimitReader(rc, maxFileSize))
	if err != nil {
		return "", 0, fmt.Errorf("buffering upstream distribution: %w", err)
	}
	if f.SHA256 != "" {
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), f.SHA256) {
			return "", 0, fmt.Errorf("upstream distribution hash mismatch for %s", f.Filename)
		}
	}

	up1, err := h.blobs.StartUpload(r.Context())
	if err != nil {
		return "", 0, err
	}
	if _, err := up1.Write(data); err != nil {
		_ = up1.Abort(r.Context())
		return "", 0, fmt.Errorf("writing distribution: %w", err)
	}
	desc, err := up1.Commit(r.Context(), blob.DigestBytes(data))
	if err != nil {
		return "", 0, fmt.Errorf("committing distribution: %w", err)
	}
	if err := h.store.setFileBlob(r.Context(), f.PackageID, f.Filename, desc.Digest, desc.Size); err != nil {
		return "", 0, err
	}
	return desc.Digest, desc.Size, nil
}

// versionFromFilename best-effort extracts the version from a distribution
// filename (used only for display on proxied files; local uploads carry the
// authoritative version). Wheels are name-version-python-abi-platform.whl and
// sdists are name-version.tar.gz.
func versionFromFilename(filename, _ string) string {
	base := filename
	for _, ext := range []string{".tar.gz", ".tar.bz2", ".zip", ".whl", ".egg"} {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
			break
		}
	}
	parts := strings.Split(base, "-")
	if len(parts) >= 2 {
		return parts[1]
	}
	return ""
}

// upstreamError reports an upstream fetch failure as a 502, logging the cause.
func (h *handler) upstreamError(w http.ResponseWriter, msg string, err error) {
	h.log.Warn("pypi "+msg, "error", err.Error())
	writeError(w, http.StatusBadGateway, "upstream unavailable")
}
