package npm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// errUpstreamNotFound means the upstream registry returned 404 for a package or
// tarball. The handler maps it to the npm not-found envelope.
var errUpstreamNotFound = errors.New("upstream content not found")

// upstream describes a proxied npm registry (registry.npmjs.org, or a private
// one). Credentials are optional; public registries need none.
type upstream struct {
	BaseURL  string
	Username string
	Password string
}

// upstreamClient fetches packuments and tarballs from an npm registry. Unlike
// the OCI proxy, npm needs no token handshake: it is plain HTTPS GETs, with
// optional HTTP Basic for a private upstream.
type upstreamClient struct {
	http *http.Client
}

func newUpstreamClient() *upstreamClient {
	return &upstreamClient{http: &http.Client{Timeout: 30 * time.Second}}
}

// fetchPackument retrieves a package's document. pkg may be scoped; the slash in
// a scoped name is left literal, which registry.npmjs.org accepts.
func (c *upstreamClient) fetchPackument(ctx context.Context, up upstream, pkg string) ([]byte, error) {
	url := strings.TrimRight(up.BaseURL, "/") + "/" + pkg
	resp, err := c.get(ctx, up, url, "application/json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusError(resp); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPublishBody))
	if err != nil {
		return nil, fmt.Errorf("reading upstream packument: %w", err)
	}
	return body, nil
}

// fetchTarball streams a version's tarball from the upstream. The caller closes
// the reader.
func (c *upstreamClient) fetchTarball(ctx context.Context, up upstream, pkg, filename string) (io.ReadCloser, error) {
	url := strings.TrimRight(up.BaseURL, "/") + "/" + pkg + "/-/" + filename
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
