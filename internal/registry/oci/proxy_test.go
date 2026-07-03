package oci_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/core/repository"
)

// fakeRegistry is a minimal upstream OCI registry (no auth) that serves one
// image: a manifest referencing a config and a layer blob. It records how many
// times each object was requested so a test can prove caching.
type fakeRegistry struct {
	manifest    []byte
	manifestDig string
	config      []byte
	configDig   string
	layer       []byte
	layerDig    string
	hits        map[string]int
}

func newFakeRegistry() *fakeRegistry {
	config := []byte(`{"architecture":"amd64","os":"linux"}`)
	layer := []byte("upstream layer bytes for the proxy test")
	configDig := blob.DigestBytes(config)
	layerDig := blob.DigestBytes(layer)

	m := map[string]any{
		"schemaVersion": 2,
		"mediaType":     imageManifestType,
		"config":        map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": configDig, "size": len(config)},
		"layers":        []any{map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": layerDig, "size": len(layer)}},
	}
	manifest, _ := json.Marshal(m)

	return &fakeRegistry{
		manifest: manifest, manifestDig: blob.DigestBytes(manifest),
		config: config, configDig: configDig,
		layer: layer, layerDig: layerDig,
		hits: map[string]int{},
	}
}

func (f *fakeRegistry) serve(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	// Manifest by tag "v1" and by its digest.
	manifestHandler := func(w http.ResponseWriter, r *http.Request) {
		f.hits[r.URL.Path]++
		w.Header().Set("Content-Type", imageManifestType)
		w.Header().Set("Docker-Content-Digest", f.manifestDig)
		_, _ = w.Write(f.manifest)
	}
	mux.HandleFunc("/v2/library/thing/manifests/v1", manifestHandler)
	mux.HandleFunc("/v2/library/thing/manifests/"+f.manifestDig, manifestHandler)
	mux.HandleFunc("/v2/library/thing/blobs/"+f.configDig, func(w http.ResponseWriter, r *http.Request) {
		f.hits[r.URL.Path]++
		_, _ = w.Write(f.config)
	})
	mux.HandleFunc("/v2/library/thing/blobs/"+f.layerDig, func(w http.ResponseWriter, r *http.Request) {
		f.hits[r.URL.Path]++
		_, _ = w.Write(f.layer)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// newProxyRepo creates a project with a proxy OCI repository mirroring upstream.
// Images are addressed at /v2/<projectKey>/<repoKey>/<image>.
func (h *harness) newProxyRepo(t *testing.T, projectKey, repoKey, upstream string) {
	t.Helper()
	proj, err := project.NewService(h.db).Create(context.Background(), project.CreateInput{
		Key: projectKey, Name: projectKey, AllowAutoCreate: true, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := repository.NewService(h.db).Create(context.Background(), repository.CreateInput{
		ProjectID: proj.ID, Key: repoKey, Name: repoKey,
		Format: repository.FormatOCI, Mode: repository.ModeProxy,
		Upstream: &repository.Upstream{URL: upstream}, Actor: "admin",
	}); err != nil {
		t.Fatalf("create proxy repo: %v", err)
	}
}

func TestProxyPullThroughCachesUpstream(t *testing.T) {
	h := newHarness(t)
	fake := newFakeRegistry()
	srv := fake.serve(t)
	h.newProxyRepo(t, "mirror", "hub", srv.URL)

	const repo = "mirror/hub/library/thing"

	// 1. Pull the manifest by tag: fetched from upstream and served.
	get := h.req(t, http.MethodGet, "/v2/"+repo+"/manifests/v1", nil, "password")
	if get.Code != http.StatusOK {
		t.Fatalf("GET manifest: status = %d, want 200; body=%s", get.Code, get.Body.String())
	}
	if get.Header().Get("Docker-Content-Digest") != fake.manifestDig {
		t.Errorf("digest header = %q, want %q", get.Header().Get("Docker-Content-Digest"), fake.manifestDig)
	}

	// 2. Pull both blobs: fetched from upstream, digest-verified, cached.
	for _, dig := range []string{fake.configDig, fake.layerDig} {
		b := h.req(t, http.MethodGet, "/v2/"+repo+"/blobs/"+dig, nil, "password")
		if b.Code != http.StatusOK {
			t.Fatalf("GET blob %s: status = %d, want 200", dig, b.Code)
		}
	}

	// 3. The blobs are now in the local CAS.
	if _, err := h.blobs.Stat(context.Background(), fake.layerDig); err != nil {
		t.Fatalf("layer not cached locally: %v", err)
	}

	// 4. Offline: with the upstream stopped, a by-digest manifest and a cached
	//    blob still serve from the local cache.
	srv.Close()
	if m := h.req(t, http.MethodGet, "/v2/"+repo+"/manifests/"+fake.manifestDig, nil, "password"); m.Code != http.StatusOK {
		t.Errorf("offline by-digest manifest: status = %d, want 200", m.Code)
	}
	if b := h.req(t, http.MethodGet, "/v2/"+repo+"/blobs/"+fake.layerDig, nil, "password"); b.Code != http.StatusOK {
		t.Errorf("offline cached blob: status = %d, want 200", b.Code)
	}
}

func TestProxyRejectsPush(t *testing.T) {
	h := newHarness(t)
	fake := newFakeRegistry()
	srv := fake.serve(t)
	h.newProxyRepo(t, "mirror", "hub", srv.URL)

	// Starting an upload against a proxy repository is denied (read-only mirror).
	up := h.req(t, http.MethodPost, "/v2/mirror/hub/library/thing/blobs/uploads/", nil, "password")
	if up.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST upload to proxy: status = %d, want 405; body=%s", up.Code, up.Body.String())
	}
	if !bytes.Contains(up.Body.Bytes(), []byte("DENIED")) {
		t.Errorf("expected DENIED, got %s", up.Body.String())
	}

	// So is pushing a manifest.
	body, _ := h.buildImageBytes()
	man := h.putManifest(t, "mirror/hub/library/thing", "v1", body)
	if man.Code != http.StatusMethodNotAllowed {
		t.Errorf("PUT manifest to proxy: status = %d, want 405", man.Code)
	}
}

// buildImageBytes returns a valid manifest body without pushing its blobs (the
// proxy rejects the push before references are checked).
func (h *harness) buildImageBytes() ([]byte, string) {
	m := testManifest{
		SchemaVersion: 2,
		MediaType:     imageManifestType,
		Config:        testDesc{MediaType: "application/vnd.oci.image.config.v1+json", Digest: blob.DigestBytes([]byte("c")), Size: 1},
		Layers:        []testDesc{{Digest: blob.DigestBytes([]byte("l")), Size: 1}},
	}
	body, _ := json.Marshal(m)
	return body, blob.DigestBytes(body)
}
