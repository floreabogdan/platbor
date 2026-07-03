// Package proxy implements pull-through caching for the OCI registry: it fetches
// manifests and blobs from an upstream registry (Docker Hub, ghcr.io, …) so a
// proxy project can serve upstream content and cache it locally.
//
// It is a thin upstream *client*, not a storage layer. The OCI adapter owns
// caching: on a miss it calls the client, then stores the result through its own
// blob and manifest stores. Keeping storage out of here avoids an import cycle
// (oci → proxy) and keeps the boundary in docs/ARCHITECTURE.md intact: proxy
// depends only on the standard library.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrUpstreamNotFound means the upstream registry returned 404 for the requested
// manifest or blob. The caller maps it to the spec's not-found envelope.
var ErrUpstreamNotFound = errors.New("upstream content not found")

// ManifestMediaTypes are the manifest and index media types the proxy accepts
// from an upstream, so it transparently handles both single-image and
// multi-arch (index) pulls.
var ManifestMediaTypes = []string{
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.v2+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
}

// Upstream describes a proxied upstream registry. Credentials are optional;
// public content on Docker Hub and ghcr.io needs none.
type Upstream struct {
	BaseURL  string // e.g. https://registry-1.docker.io
	Username string
	Password string
}

// Manifest is a manifest fetched from an upstream: the exact bytes, the media
// type the upstream reported, and its content digest.
type Manifest struct {
	Bytes     []byte
	MediaType string
	Digest    string
}

// Client fetches from upstream registries, performing the Distribution-Spec
// bearer-token handshake and caching tokens per scope for reuse.
type Client struct {
	http   *http.Client
	mu     sync.Mutex
	tokens map[string]cachedToken // key: baseURL + "|" + scope
}

type cachedToken struct {
	value   string
	expires time.Time
}

// New returns a Client with a bounded HTTP timeout.
func New() *Client {
	return &Client{
		http:   &http.Client{Timeout: 30 * time.Second},
		tokens: map[string]cachedToken{},
	}
}

// FetchManifest retrieves a manifest by tag or digest. The returned digest comes
// from the upstream's Docker-Content-Digest header, so a pull by tag still
// yields the canonical digest to cache under.
func (c *Client) FetchManifest(ctx context.Context, up Upstream, repo, ref string) (Manifest, error) {
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", strings.TrimRight(up.BaseURL, "/"), repo, ref)
	resp, err := c.doAuthed(ctx, up, repo, http.MethodGet, url, ManifestMediaTypes)
	if err != nil {
		return Manifest{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if err := statusError(resp); err != nil {
		return Manifest{}, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Manifest{}, fmt.Errorf("reading upstream manifest: %w", err)
	}
	return Manifest{
		Bytes:     body,
		MediaType: resp.Header.Get("Content-Type"),
		Digest:    resp.Header.Get("Docker-Content-Digest"),
	}, nil
}

// FetchBlob streams a blob by digest from the upstream. The caller closes the
// reader. size is the Content-Length, or -1 if the upstream omitted it.
func (c *Client) FetchBlob(ctx context.Context, up Upstream, repo, digest string) (io.ReadCloser, int64, error) {
	url := fmt.Sprintf("%s/v2/%s/blobs/%s", strings.TrimRight(up.BaseURL, "/"), repo, digest)
	resp, err := c.doAuthed(ctx, up, repo, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	if err := statusError(resp); err != nil {
		_ = resp.Body.Close()
		return nil, 0, err
	}
	size := int64(-1)
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if n, perr := strconv.ParseInt(cl, 10, 64); perr == nil {
			size = n
		}
	}
	return resp.Body, size, nil
}

// doAuthed issues a request, and on a 401 performs the bearer-token handshake
// described by the WWW-Authenticate header, then retries once with the token.
func (c *Client) doAuthed(ctx context.Context, up Upstream, repo, method, url string, accept []string) (*http.Response, error) {
	scope := "repository:" + repo + ":pull"

	// Reuse a cached token if we have one for this scope.
	if tok := c.cachedToken(up.BaseURL, scope); tok != "" {
		resp, err := c.do(ctx, method, url, accept, tok)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusUnauthorized {
			return resp, nil
		}
		_ = resp.Body.Close() // token stale; fall through to re-negotiate
	}

	resp, err := c.do(ctx, method, url, accept, "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	challenge := resp.Header.Get("WWW-Authenticate")
	_ = resp.Body.Close()

	tok, err := c.negotiateToken(ctx, up, challenge, scope)
	if err != nil {
		return nil, err
	}
	return c.do(ctx, method, url, accept, tok)
}

// do performs a single GET with the given Accept types and optional bearer token.
func (c *Client) do(ctx context.Context, method, url string, accept []string, token string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building upstream request: %w", err)
	}
	for _, a := range accept {
		req.Header.Add("Accept", a)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}
	return resp, nil
}

// statusError maps upstream response codes to errors. A 200 is success; a 404 is
// ErrUpstreamNotFound; anything else is an unexpected failure.
func statusError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return ErrUpstreamNotFound
	default:
		return fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}
}

func (c *Client) cachedToken(baseURL, scope string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	tok, ok := c.tokens[baseURL+"|"+scope]
	if !ok || time.Now().After(tok.expires) {
		return ""
	}
	return tok.value
}

func (c *Client) storeToken(baseURL, scope, value string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Expire a little early so a token is never used in its final seconds.
	c.tokens[baseURL+"|"+scope] = cachedToken{value: value, expires: time.Now().Add(ttl - 10*time.Second)}
}
