package cargo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/platbor/platbor/internal/core/repository"
)

// maxIndexBody caps an upstream sparse-index file.
const maxIndexBody = 32 << 20 // 32 MiB

var errUpstreamNotFound = errors.New("upstream content not found")

// upstream describes a proxied Cargo sparse registry: the index base URL (e.g.
// https://index.crates.io) plus optional basic-auth credentials.
type upstream struct {
	IndexBase string
	Username  string
	Password  string
}

func upstreamOf(repo repository.Repository) upstream {
	if repo.Upstream == nil {
		return upstream{}
	}
	return upstream{IndexBase: repo.Upstream.URL, Username: repo.Upstream.Username, Password: repo.Upstream.Password}
}

// upstreamClient fetches sparse-index files, the upstream config.json, and
// .crate files. The download template (dl) from config.json is memoized per index
// base so the download host (static.crates.io) is discovered, not hard-coded.
type upstreamClient struct {
	http *http.Client
	mu   sync.Mutex
	dl   map[string]string // index base -> dl template
}

func newUpstreamClient() *upstreamClient {
	return &upstreamClient{http: &http.Client{Timeout: 120 * time.Second}, dl: map[string]string{}}
}

// fetchIndex retrieves a crate's sparse-index file from the upstream (the sharded
// path is computed from the lowercased name).
func (c *upstreamClient) fetchIndex(ctx context.Context, up upstream, nameLower string) ([]byte, error) {
	url := strings.TrimRight(up.IndexBase, "/") + "/" + indexPath(nameLower)
	resp, err := c.get(ctx, up, url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusError(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxIndexBody))
}

// downloadURL computes the upstream .crate URL for a crate version, discovering
// the dl template from the upstream config.json (memoized).
func (c *upstreamClient) downloadURL(ctx context.Context, up upstream, crate, version, cksum string) (string, error) {
	dl, err := c.resolveDL(ctx, up)
	if err != nil {
		return "", err
	}
	markers := []string{"{crate}", "{version}", "{prefix}", "{lowerprefix}", "{sha256-checksum}"}
	hasMarker := false
	for _, m := range markers {
		if strings.Contains(dl, m) {
			hasMarker = true
			break
		}
	}
	if !hasMarker {
		return strings.TrimRight(dl, "/") + "/" + crate + "/" + version + "/download", nil
	}
	prefix := indexPrefix(normalizeName(crate))
	repl := strings.NewReplacer(
		"{crate}", crate,
		"{version}", version,
		"{prefix}", prefix,
		"{lowerprefix}", strings.ToLower(prefix),
		"{sha256-checksum}", cksum,
	)
	return repl.Replace(dl), nil
}

// resolveDL fetches and memoizes the upstream config.json's dl template.
func (c *upstreamClient) resolveDL(ctx context.Context, up upstream) (string, error) {
	c.mu.Lock()
	if dl, ok := c.dl[up.IndexBase]; ok {
		c.mu.Unlock()
		return dl, nil
	}
	c.mu.Unlock()

	url := strings.TrimRight(up.IndexBase, "/") + "/config.json"
	resp, err := c.get(ctx, up, url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusError(resp); err != nil {
		return "", err
	}
	var doc struct {
		DL string `json:"dl"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return "", fmt.Errorf("parsing upstream config.json: %w", err)
	}
	if doc.DL == "" {
		return "", errors.New("upstream config.json has no dl")
	}
	c.mu.Lock()
	c.dl[up.IndexBase] = doc.DL
	c.mu.Unlock()
	return doc.DL, nil
}

// fetchCrate streams a .crate from an absolute upstream URL.
func (c *upstreamClient) fetchCrate(ctx context.Context, up upstream, url string) (io.ReadCloser, error) {
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

// indexPrefix computes the {prefix} used by some dl templates: the shard
// directories without the crate name (e.g. "ab/cd" for a 4+ char name).
func indexPrefix(nameLower string) string {
	switch n := len(nameLower); {
	case n == 1:
		return "1"
	case n == 2:
		return "2"
	case n == 3:
		return "3/" + nameLower[:1]
	default:
		return nameLower[:2] + "/" + nameLower[2:4]
	}
}
