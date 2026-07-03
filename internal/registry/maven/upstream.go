package maven

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

// errUpstreamNotFound means the upstream returned 404 for a path.
var errUpstreamNotFound = errors.New("upstream content not found")

// upstream describes a proxied Maven repository: the base URL of its layout
// (e.g. https://repo1.maven.org/maven2) plus optional basic-auth credentials.
type upstream struct {
	Base     string
	Username string
	Password string
}

// upstreamOf builds the upstream config from a proxy repository.
func upstreamOf(repo repository.Repository) upstream {
	if repo.Upstream == nil {
		return upstream{}
	}
	return upstream{Base: repo.Upstream.URL, Username: repo.Upstream.Username, Password: repo.Upstream.Password}
}

// upstreamClient fetches files from a Maven repository over HTTP.
type upstreamClient struct {
	http *http.Client
}

func newUpstreamClient() *upstreamClient {
	return &upstreamClient{http: &http.Client{Timeout: 60 * time.Second}}
}

// fetchFile streams a file at a repository path from the upstream. The caller
// closes the reader.
func (c *upstreamClient) fetchFile(ctx context.Context, up upstream, path string) (io.ReadCloser, error) {
	url := strings.TrimRight(up.Base, "/") + "/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building upstream request: %w", err)
	}
	// Maven Central throttles clients that send no User-Agent (the Go default is
	// often rate-limited); identify ourselves like a normal repository client.
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
	case http.StatusNotFound:
		_ = resp.Body.Close()
		return nil, errUpstreamNotFound
	default:
		_ = resp.Body.Close()
		return nil, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
}
