package goproxy

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

// errUpstreamNotFound means the upstream returned 404/410 for a path.
var errUpstreamNotFound = errors.New("upstream content not found")

// upstream describes a proxied GOPROXY: its base URL (e.g. https://proxy.golang.org)
// plus optional basic-auth credentials.
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

// upstreamClient fetches Go module proxy resources over HTTP.
type upstreamClient struct {
	http *http.Client
}

func newUpstreamClient() *upstreamClient {
	return &upstreamClient{http: &http.Client{Timeout: 120 * time.Second}}
}

// fetch streams a resource at an escaped proxy path (e.g. mod/@v/v1.info) from
// the upstream. The caller closes the reader.
func (c *upstreamClient) fetch(ctx context.Context, up upstream, escapedPath string) (io.ReadCloser, error) {
	url := strings.TrimRight(up.Base, "/") + "/" + escapedPath
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
	switch resp.StatusCode {
	case http.StatusOK:
		return resp.Body, nil
	case http.StatusNotFound, http.StatusGone:
		_ = resp.Body.Close()
		return nil, errUpstreamNotFound
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
}
