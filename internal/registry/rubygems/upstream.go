package rubygems

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/platbor/platbor/internal/core/repository"
)

// maxIndexBody caps an upstream compact-index file.
const maxIndexBody = 64 << 20 // 64 MiB

var errUpstreamNotFound = errors.New("upstream content not found")

// upstream describes a proxied RubyGems source: its base URL (e.g.
// https://rubygems.org) plus optional basic-auth credentials.
type upstream struct {
	Base     string
	Username string
	Password string
}

func upstreamOf(repo repository.Repository) upstream {
	if repo.Upstream == nil {
		return upstream{}
	}
	return upstream{Base: repo.Upstream.URL, Username: repo.Upstream.Username, Password: repo.Upstream.Password}
}

type upstreamClient struct {
	http *http.Client
}

func newUpstreamClient() *upstreamClient {
	return &upstreamClient{http: &http.Client{Timeout: 120 * time.Second}}
}

// fetchText retrieves a compact-index text file (/versions, /names, /info/<gem>).
func (c *upstreamClient) fetchText(ctx context.Context, up upstream, path string) ([]byte, error) {
	resp, err := c.get(ctx, up, strings.TrimRight(up.Base, "/")+path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusError(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxIndexBody))
}

// fetchGem streams a .gem from an absolute upstream URL.
func (c *upstreamClient) fetchGem(ctx context.Context, up upstream, url string) (io.ReadCloser, error) {
	resp, err := c.get(ctx, up, url)
	if err != nil {
		return nil, err
	}
	if err := statusError(resp); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	return resp.Body, nil
}

func (c *upstreamClient) get(ctx context.Context, up upstream, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building upstream request: %w", err)
	}
	req.Header.Set("User-Agent", "platbor/1.0 (+https://github.com/floreabogdan/platbor)")
	if up.Username != "" || up.Password != "" {
		req.SetBasicAuth(up.Username, up.Password)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}
	return resp, nil
}

func statusError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound, http.StatusGone:
		return errUpstreamNotFound
	default:
		return fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
}

// --- proxy handlers ---

// proxyText proxies a compact-index text file (/versions, /names) verbatim.
func (h *handler) proxyText(w http.ResponseWriter, r *http.Request, repo repository.Repository, path string) {
	body, err := h.upstream.fetchText(r.Context(), upstreamOf(repo), path)
	if err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		h.upstreamError(w, "fetching "+path, err)
		return
	}
	writeText(w, string(body))
}

// proxyInfo proxies a gem's /info file, recording each version for the browser
// and lazy caching, then serving the upstream bytes verbatim (cargo-style: the
// info line already carries the checksum, and downloads flow through us).
func (h *handler) proxyInfo(w http.ResponseWriter, r *http.Request, repo repository.Repository, name string) {
	up := upstreamOf(repo)
	body, err := h.upstream.fetchText(r.Context(), up, "/info/"+name)
	if err != nil {
		if errors.Is(err, errUpstreamNotFound) {
			writeError(w, http.StatusNotFound, "gem not found")
			return
		}
		h.upstreamError(w, "fetching info", err)
		return
	}
	base := strings.TrimRight(up.Base, "/")
	for _, line := range strings.Split(string(body), "\n") {
		v, ok := parseInfoLine(line)
		if !ok {
			continue
		}
		dlURL := base + "/gems/" + name + "-" + v.Number + ".gem"
		if err := h.store.cacheIndexRow(r.Context(), repo.ID, name, v, dlURL); err != nil {
			h.log.Warn("rubygems caching index row", "gem", name, "version", v.Number, "error", err.Error())
		}
	}
	writeText(w, string(body))
}

// parsedInfoVersion is one version parsed from an upstream /info line.
type parsedInfoVersion struct {
	Version  string
	Platform string
	Number   string
	Deps     string
	Reqs     string
	Checksum string
}

// parseInfoLine parses a compact-index /info line: "<number> <deps>|<reqs>".
// Header/separator lines (---, empty) return ok=false.
func parseInfoLine(line string) (parsedInfoVersion, bool) {
	line = strings.TrimRight(line, "\r")
	if line == "" || line == "---" || strings.HasPrefix(line, "created_at:") {
		return parsedInfoVersion{}, false
	}
	sp := strings.IndexByte(line, ' ')
	if sp < 0 {
		return parsedInfoVersion{}, false
	}
	number := line[:sp]
	rest := line[sp+1:]
	bar := strings.IndexByte(rest, '|')
	deps, reqs := rest, ""
	if bar >= 0 {
		deps, reqs = rest[:bar], rest[bar+1:]
	}
	version, platform := number, "ruby"
	if i := strings.LastIndexByte(number, '-'); i > 0 {
		version, platform = number[:i], number[i+1:]
	}
	return parsedInfoVersion{
		Version:  version,
		Platform: platform,
		Number:   number,
		Deps:     deps,
		Reqs:     reqs,
		Checksum: checksumFromReqs(reqs),
	}, true
}

func checksumFromReqs(reqs string) string {
	for _, part := range splitComma(reqs) {
		if strings.HasPrefix(part, "checksum:") {
			return part[len("checksum:"):]
		}
	}
	return ""
}
