package proxy_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/platbor/platbor/internal/registry/proxy"
)

// fakeUpstream stands in for an upstream registry that requires a bearer token,
// so the whole handshake is exercised without touching the network.
func fakeUpstream(t *testing.T) (base string, tokenHits *int, manifestHits *int) {
	t.Helper()
	var tHits, mHits int

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tHits++
		if r.URL.Query().Get("scope") == "" {
			t.Errorf("token request missing scope: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"secret-token","expires_in":300}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	mux.HandleFunc("/v2/library/alpine/manifests/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			w.Header().Set("WWW-Authenticate",
				`Bearer realm="`+srv.URL+`/token",service="fake.registry",scope="repository:library/alpine:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		mHits++
		ref := strings.TrimPrefix(r.URL.Path, "/v2/library/alpine/manifests/")
		if ref == "missing" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		w.Header().Set("Docker-Content-Digest", "sha256:deadbeef")
		_, _ = w.Write([]byte(`{"schemaVersion":2}`))
	})
	mux.HandleFunc("/v2/library/alpine/blobs/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+srv.URL+`/token",service="fake.registry"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Length", "5")
		_, _ = w.Write([]byte("bytes"))
	})

	return srv.URL, &tHits, &mHits
}

func TestFetchManifestPerformsTokenHandshake(t *testing.T) {
	base, tokenHits, manifestHits := fakeUpstream(t)
	c := proxy.New()
	up := proxy.Upstream{BaseURL: base}
	ctx := context.Background()

	m, err := c.FetchManifest(ctx, up, "library/alpine", "latest")
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if m.Digest != "sha256:deadbeef" || m.MediaType != "application/vnd.oci.image.manifest.v1+json" {
		t.Errorf("manifest = %+v", m)
	}
	if string(m.Bytes) != `{"schemaVersion":2}` {
		t.Errorf("bytes = %s", m.Bytes)
	}
	if *tokenHits != 1 {
		t.Errorf("token endpoint hit %d times, want 1", *tokenHits)
	}

	// A second fetch must reuse the cached token: no new token request.
	if _, err := c.FetchManifest(ctx, up, "library/alpine", "3.19"); err != nil {
		t.Fatalf("second FetchManifest: %v", err)
	}
	if *tokenHits != 1 {
		t.Errorf("token re-negotiated; hits = %d, want 1 (cached)", *tokenHits)
	}
	if *manifestHits < 2 {
		t.Errorf("manifest served %d times, want >= 2", *manifestHits)
	}
}

func TestFetchManifestMissingIsNotFound(t *testing.T) {
	base, _, _ := fakeUpstream(t)
	c := proxy.New()
	_, err := c.FetchManifest(context.Background(), proxy.Upstream{BaseURL: base}, "library/alpine", "missing")
	if err != proxy.ErrUpstreamNotFound {
		t.Fatalf("err = %v, want ErrUpstreamNotFound", err)
	}
}

func TestFetchBlobStreamsContent(t *testing.T) {
	base, _, _ := fakeUpstream(t)
	c := proxy.New()
	rc, size, err := c.FetchBlob(context.Background(), proxy.Upstream{BaseURL: base}, "library/alpine", "sha256:abc")
	if err != nil {
		t.Fatalf("FetchBlob: %v", err)
	}
	defer func() { _ = rc.Close() }()
	if size != 5 {
		t.Errorf("size = %d, want 5", size)
	}
}

// TestFetchAgainstDockerHub is an opt-in end-to-end check against the real
// Docker Hub, gated so CI and offline runs skip it: PLATBOR_PROXY_E2E=1 go test.
func TestFetchAgainstDockerHub(t *testing.T) {
	if os.Getenv("PLATBOR_PROXY_E2E") == "" {
		t.Skip("set PLATBOR_PROXY_E2E=1 to run the live Docker Hub check")
	}
	c := proxy.New()
	up := proxy.Upstream{BaseURL: "https://registry-1.docker.io"}
	m, err := c.FetchManifest(context.Background(), up, "library/alpine", "latest")
	if err != nil {
		t.Fatalf("FetchManifest(alpine:latest): %v", err)
	}
	if m.Digest == "" || len(m.Bytes) == 0 {
		t.Fatalf("empty manifest: %+v", m)
	}
	t.Logf("alpine:latest digest=%s bytes=%d type=%s", m.Digest, len(m.Bytes), m.MediaType)
}
