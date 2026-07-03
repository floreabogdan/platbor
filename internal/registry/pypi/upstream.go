package pypi

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

// maxIndexBody caps an upstream simple-index page.
const maxIndexBody = 16 << 20 // 16 MiB

// errUpstreamNotFound means the upstream returned 404 for a project or file.
var errUpstreamNotFound = errors.New("upstream content not found")

// upstream describes a proxied PyPI index: the base URL of its simple index
// (e.g. https://pypi.org/simple/) plus optional basic-auth credentials.
type upstream struct {
	SimpleBase string
	Username   string
	Password   string
}

// upstreamOf builds the upstream config from a proxy repository. The configured
// upstream URL is the simple-index base.
func upstreamOf(repo repository.Repository) upstream {
	if repo.Upstream == nil {
		return upstream{}
	}
	return upstream{SimpleBase: repo.Upstream.URL, Username: repo.Upstream.Username, Password: repo.Upstream.Password}
}

// upstreamClient fetches simple-index pages and distribution files from a PyPI
// index. It is plain HTTPS GETs with optional HTTP Basic for a private upstream.
type upstreamClient struct {
	http *http.Client
}

func newUpstreamClient() *upstreamClient {
	return &upstreamClient{http: &http.Client{Timeout: 30 * time.Second}}
}

// fetchSimple retrieves a project's simple-index page (HTML). name is already
// PEP 503 normalized.
func (c *upstreamClient) fetchSimple(ctx context.Context, up upstream, name string) ([]byte, error) {
	url := strings.TrimRight(up.SimpleBase, "/") + "/" + name + "/"
	resp, err := c.get(ctx, up, url, "text/html")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusError(resp); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxIndexBody))
}

// fetchFile streams a distribution file from an absolute upstream URL. The caller
// closes the reader.
func (c *upstreamClient) fetchFile(ctx context.Context, up upstream, url string) (io.ReadCloser, error) {
	resp, err := c.get(ctx, up, url, "application/octet-stream")
	if err != nil {
		return nil, err
	}
	if err := statusError(resp); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	return resp.Body, nil
}

func (c *upstreamClient) get(ctx context.Context, up upstream, url, accept string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building upstream request: %w", err)
	}
	req.Header.Set("Accept", accept)
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
	case http.StatusNotFound:
		return errUpstreamNotFound
	default:
		return fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
}
