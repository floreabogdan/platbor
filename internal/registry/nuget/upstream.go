package nuget

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

// maxMetadataBody caps an upstream metadata document (service index,
// flat-container version list, registration page, search result).
const maxMetadataBody = 32 << 20 // 32 MiB

// errUpstreamNotFound means the upstream feed returned 404 for a resource. The
// handler maps it to a NuGet not-found response.
var errUpstreamNotFound = errors.New("upstream content not found")

// upstream describes a proxied NuGet feed: the URL of its V3 service index plus
// optional basic-auth credentials for a private upstream.
type upstream struct {
	IndexURL string
	Username string
	Password string
}

// upstreamOf builds the upstream config from a proxy repository.
func upstreamOf(repo repository.Repository) upstream {
	if repo.Upstream == nil {
		return upstream{}
	}
	return upstream{IndexURL: repo.Upstream.URL, Username: repo.Upstream.Username, Password: repo.Upstream.Password}
}

// upstreamResources are the base URLs discovered from an upstream V3 service
// index. NuGet's resources live at registry-chosen paths — api.nuget.org serves
// registration from /v3/registration5-gz-semver2/ and package content from
// /v3-flatcontainer/ — so a proxy must read the index to learn them rather than
// assume our own path layout.
type upstreamResources struct {
	flatBase   string // PackageBaseAddress/3.0.0
	regBase    string // RegistrationsBaseUrl (richest variant available)
	searchBase string // SearchQueryService
}

// upstreamClient fetches metadata and packages from a NuGet V3 feed. Discovered
// resources are memoized per index URL — the service index is effectively static.
type upstreamClient struct {
	http *http.Client
	mu   sync.Mutex
	res  map[string]upstreamResources
}

func newUpstreamClient() *upstreamClient {
	return &upstreamClient{
		http: &http.Client{Timeout: 30 * time.Second},
		res:  map[string]upstreamResources{},
	}
}

// resources returns the upstream feed's resource base URLs, discovering them
// from its service index on first use and caching the result.
func (c *upstreamClient) resources(ctx context.Context, up upstream) (upstreamResources, error) {
	c.mu.Lock()
	if r, ok := c.res[up.IndexURL]; ok {
		c.mu.Unlock()
		return r, nil
	}
	c.mu.Unlock()

	body, err := c.getJSON(ctx, up, up.IndexURL)
	if err != nil {
		return upstreamResources{}, err
	}
	var index struct {
		Resources []struct {
			ID   string `json:"@id"`
			Type string `json:"@type"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(body, &index); err != nil {
		return upstreamResources{}, fmt.Errorf("parsing upstream service index: %w", err)
	}

	var res upstreamResources
	regRank := 0 // prefer the richest registration variant (gz-semver2 == 3.6.0)
	for _, r := range index.Resources {
		switch {
		case r.Type == "PackageBaseAddress/3.0.0":
			res.flatBase = r.ID
		case strings.HasPrefix(r.Type, "SearchQueryService") && res.searchBase == "":
			res.searchBase = r.ID
		case strings.HasPrefix(r.Type, "RegistrationsBaseUrl"):
			if rank := registrationRank(r.Type); rank > regRank {
				regRank = rank
				res.regBase = r.ID
			}
		}
	}
	if res.flatBase == "" {
		return upstreamResources{}, fmt.Errorf("upstream service index has no PackageBaseAddress resource")
	}

	c.mu.Lock()
	c.res[up.IndexURL] = res
	c.mu.Unlock()
	return res, nil
}

// registrationRank scores registration variants so the most capable one wins:
// /3.6.0 (gzip, SemVer2) > /3.4.0 > the bare/legacy resource.
func registrationRank(typ string) int {
	switch typ {
	case "RegistrationsBaseUrl/3.6.0":
		return 3
	case "RegistrationsBaseUrl/3.4.0":
		return 2
	default:
		return 1
	}
}

// fetchFlatVersions retrieves a package's normalized version list.
func (c *upstreamClient) fetchFlatVersions(ctx context.Context, up upstream, res upstreamResources, idLower string) ([]byte, error) {
	return c.getJSON(ctx, up, trimSlash(res.flatBase)+"/"+idLower+"/index.json")
}

// fetchRegistration retrieves a package's registration index.
func (c *upstreamClient) fetchRegistration(ctx context.Context, up upstream, res upstreamResources, idLower string) ([]byte, error) {
	if res.regBase == "" {
		return nil, errUpstreamNotFound
	}
	return c.getJSON(ctx, up, trimSlash(res.regBase)+"/"+idLower+"/index.json")
}

// fetchURL retrieves an absolute upstream document (used to inline external
// registration pages the index references by @id).
func (c *upstreamClient) fetchURL(ctx context.Context, up upstream, url string) ([]byte, error) {
	return c.getJSON(ctx, up, url)
}

// fetchSearch retrieves search results, forwarding the client's raw query.
func (c *upstreamClient) fetchSearch(ctx context.Context, up upstream, res upstreamResources, rawQuery string) ([]byte, error) {
	if res.searchBase == "" {
		return nil, errUpstreamNotFound
	}
	url := res.searchBase
	if rawQuery != "" {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url += sep + rawQuery
	}
	return c.getJSON(ctx, up, url)
}

// fetchNupkg streams a version's .nupkg from the upstream flat container. The
// caller closes the reader.
func (c *upstreamClient) fetchNupkg(ctx context.Context, up upstream, res upstreamResources, idLower, versionLower string) (io.ReadCloser, error) {
	url := trimSlash(res.flatBase) + "/" + idLower + "/" + versionLower + "/" + idLower + "." + versionLower + ".nupkg"
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

func (c *upstreamClient) getJSON(ctx context.Context, up upstream, url string) ([]byte, error) {
	resp, err := c.get(ctx, up, url, "application/json")
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if err := statusError(resp); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxMetadataBody))
	if err != nil {
		return nil, fmt.Errorf("reading upstream response: %w", err)
	}
	return body, nil
}

// get issues a GET. It deliberately sets no Accept-Encoding: Go's transport adds
// gzip and transparently decompresses, which is what makes the gz-semver2
// registration endpoint work without special handling.
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

func trimSlash(s string) string { return strings.TrimRight(s, "/") }
