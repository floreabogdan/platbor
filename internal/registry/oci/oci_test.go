package oci_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/registry"
	"github.com/platbor/platbor/internal/registry/oci"
)

type harness struct {
	router http.Handler
	auth   *auth.Service
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	ctx := context.Background()

	sqlDB, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(ctx, sqlDB, discardLogger()); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	authSvc := auth.NewService(sqlDB)
	if _, err := authSvc.Bootstrap(ctx, "admin", "password"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	// A repository name is <project>/<repo>; the manifest API requires the
	// project to exist, so seed the one the tests push to.
	if _, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "library", Name: "Library", Actor: "admin"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	store, err := blob.NewFS(cfg.DataDir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	r := chi.NewRouter()
	r.Route("/v2", func(sub chi.Router) {
		oci.New().Mount(sub, registry.Deps{Blobs: store, Auth: authSvc, DB: sqlDB, Log: discardLogger()})
	})
	return &harness{router: r, auth: authSvc}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// req issues an authenticated (Basic admin) request unless password is "".
func (h *harness) req(t *testing.T, method, path string, body []byte, password string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if password != "" {
		req.SetBasicAuth("admin", password)
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

func TestVersionCheckRequiresAuth(t *testing.T) {
	h := newHarness(t)

	rr := h.req(t, http.MethodGet, "/v2/", nil, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth /v2/: status = %d, want 401", rr.Code)
	}
	if rr.Header().Get("WWW-Authenticate") == "" {
		t.Error("401 must include a WWW-Authenticate challenge")
	}

	ok := h.req(t, http.MethodGet, "/v2/", nil, "password")
	if ok.Code != http.StatusOK {
		t.Fatalf("auth /v2/: status = %d, want 200", ok.Code)
	}
	if ok.Header().Get("Docker-Distribution-API-Version") != "registry/2.0" {
		t.Error("missing Docker-Distribution-API-Version header")
	}
}

func TestChunkedBlobPushAndPull(t *testing.T) {
	h := newHarness(t)
	content := []byte("hello oci layer, chunked")
	digest := blob.DigestBytes(content)

	// 1. Start the upload session.
	start := h.req(t, http.MethodPost, "/v2/library/alpine/blobs/uploads/", nil, "password")
	if start.Code != http.StatusAccepted {
		t.Fatalf("POST uploads: status = %d, want 202; body=%s", start.Code, start.Body.String())
	}
	loc := start.Header().Get("Location")
	if loc == "" || start.Header().Get("Docker-Upload-UUID") == "" {
		t.Fatalf("missing Location/UUID: %v", start.Header())
	}

	// 2. Append the content.
	patch := h.req(t, http.MethodPatch, loc, content, "password")
	if patch.Code != http.StatusAccepted {
		t.Fatalf("PATCH: status = %d, want 202", patch.Code)
	}

	// 3. Finalize against the digest.
	put := h.req(t, http.MethodPut, loc+"?digest="+digest, nil, "password")
	if put.Code != http.StatusCreated {
		t.Fatalf("PUT: status = %d, want 201; body=%s", put.Code, put.Body.String())
	}
	if put.Header().Get("Docker-Content-Digest") != digest {
		t.Errorf("Docker-Content-Digest = %q, want %q", put.Header().Get("Docker-Content-Digest"), digest)
	}

	// 4. HEAD reports it exists with the right size.
	head := h.req(t, http.MethodHead, "/v2/library/alpine/blobs/"+digest, nil, "password")
	if head.Code != http.StatusOK {
		t.Fatalf("HEAD: status = %d, want 200", head.Code)
	}

	// 5. GET returns the exact bytes.
	get := h.req(t, http.MethodGet, "/v2/library/alpine/blobs/"+digest, nil, "password")
	if get.Code != http.StatusOK {
		t.Fatalf("GET: status = %d, want 200", get.Code)
	}
	if !bytes.Equal(get.Body.Bytes(), content) {
		t.Fatalf("GET body = %q, want %q", get.Body.Bytes(), content)
	}
}

func TestMonolithicBlobPush(t *testing.T) {
	h := newHarness(t)
	content := []byte("single-request blob")
	digest := blob.DigestBytes(content)

	rr := h.req(t, http.MethodPost, "/v2/library/app/blobs/uploads/?digest="+digest, content, "password")
	if rr.Code != http.StatusCreated {
		t.Fatalf("monolithic POST: status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	if head := h.req(t, http.MethodHead, "/v2/library/app/blobs/"+digest, nil, "password"); head.Code != http.StatusOK {
		t.Fatalf("HEAD after monolithic push: status = %d, want 200", head.Code)
	}
}

func TestDigestMismatchRejected(t *testing.T) {
	h := newHarness(t)
	content := []byte("real content")
	wrong := blob.DigestBytes([]byte("different"))

	start := h.req(t, http.MethodPost, "/v2/library/x/blobs/uploads/", nil, "password")
	loc := start.Header().Get("Location")
	_ = h.req(t, http.MethodPatch, loc, content, "password")
	put := h.req(t, http.MethodPut, loc+"?digest="+wrong, nil, "password")
	if put.Code != http.StatusBadRequest {
		t.Fatalf("mismatched PUT: status = %d, want 400", put.Code)
	}
	// The real content must not have been stored.
	if head := h.req(t, http.MethodHead, "/v2/library/x/blobs/"+blob.DigestBytes(content), nil, "password"); head.Code != http.StatusNotFound {
		t.Fatalf("mismatched content stored: HEAD status = %d, want 404", head.Code)
	}
}

func TestHeadUnknownBlobIs404(t *testing.T) {
	h := newHarness(t)
	unknown := blob.DigestBytes([]byte("nope"))
	rr := h.req(t, http.MethodHead, "/v2/library/y/blobs/"+unknown, nil, "password")
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestUploadStatusAndCancel(t *testing.T) {
	h := newHarness(t)
	start := h.req(t, http.MethodPost, "/v2/library/z/blobs/uploads/", nil, "password")
	loc := start.Header().Get("Location")
	_ = h.req(t, http.MethodPatch, loc, []byte("partial"), "password")

	status := h.req(t, http.MethodGet, loc, nil, "password")
	if status.Code != http.StatusNoContent || status.Header().Get("Range") == "" {
		t.Fatalf("status: code = %d, range = %q", status.Code, status.Header().Get("Range"))
	}

	cancel := h.req(t, http.MethodDelete, loc, nil, "password")
	if cancel.Code != http.StatusNoContent {
		t.Fatalf("cancel: status = %d, want 204", cancel.Code)
	}
	// A canceled upload cannot be patched further.
	if again := h.req(t, http.MethodPatch, loc, []byte("more"), "password"); again.Code != http.StatusNotFound {
		t.Fatalf("patch after cancel: status = %d, want 404", again.Code)
	}
}

func TestPushWithPersonalAccessToken(t *testing.T) {
	h := newHarness(t)
	admin, err := h.auth.Authenticate(context.Background(), "admin", "password")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	rawToken, _, err := h.auth.CreateToken(context.Background(), admin.ID, "ci", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}

	// The token authenticates as the Basic password (docker login -p <token>).
	rr := h.req(t, http.MethodGet, "/v2/", nil, rawToken)
	if rr.Code != http.StatusOK {
		t.Fatalf("token auth /v2/: status = %d, want 200", rr.Code)
	}
}

// --- Manifests and tags ---

const imageManifestType = "application/vnd.oci.image.manifest.v1+json"

// testDesc / testManifest build a minimal but well-formed OCI image manifest.
type testDesc struct {
	MediaType string `json:"mediaType"`
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
}

type testManifest struct {
	SchemaVersion int        `json:"schemaVersion"`
	MediaType     string     `json:"mediaType"`
	Config        testDesc   `json:"config"`
	Layers        []testDesc `json:"layers"`
}

// pushBlob stores a blob via monolithic upload and returns its digest.
func (h *harness) pushBlob(t *testing.T, name string, content []byte) string {
	t.Helper()
	digest := blob.DigestBytes(content)
	rr := h.req(t, http.MethodPost, "/v2/"+name+"/blobs/uploads/?digest="+digest, content, "password")
	if rr.Code != http.StatusCreated {
		t.Fatalf("pushBlob %s: status = %d, want 201; body=%s", name, rr.Code, rr.Body.String())
	}
	return digest
}

// putManifest PUTs a manifest with the given Content-Type.
func (h *harness) putManifest(t *testing.T, name, ref string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, "/v2/"+name+"/manifests/"+ref, bytes.NewReader(body))
	req.SetBasicAuth("admin", "password")
	req.Header.Set("Content-Type", imageManifestType)
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

// buildImage pushes a config + layer blob and returns a manifest referencing
// them, along with the manifest's own digest.
func (h *harness) buildImage(t *testing.T, name string) (body []byte, digest string) {
	t.Helper()
	config := []byte(`{"architecture":"amd64","os":"linux"}`)
	layer := []byte("a tar layer's bytes")
	configDigest := h.pushBlob(t, name, config)
	layerDigest := h.pushBlob(t, name, layer)

	m := testManifest{
		SchemaVersion: 2,
		MediaType:     imageManifestType,
		Config:        testDesc{MediaType: "application/vnd.oci.image.config.v1+json", Digest: configDigest, Size: int64(len(config))},
		Layers:        []testDesc{{MediaType: "application/vnd.oci.image.layer.v1.tar+gzip", Digest: layerDigest, Size: int64(len(layer))}},
	}
	body, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return body, blob.DigestBytes(body)
}

func TestManifestPushPullByTagAndDigest(t *testing.T) {
	h := newHarness(t)
	const name = "library/alpine"
	body, digest := h.buildImage(t, name)

	put := h.putManifest(t, name, "v1.0", body)
	if put.Code != http.StatusCreated {
		t.Fatalf("PUT manifest: status = %d, want 201; body=%s", put.Code, put.Body.String())
	}
	if put.Header().Get("Docker-Content-Digest") != digest {
		t.Errorf("Docker-Content-Digest = %q, want %q", put.Header().Get("Docker-Content-Digest"), digest)
	}

	// GET by tag returns the exact bytes, digest, and stored media type.
	get := h.req(t, http.MethodGet, "/v2/"+name+"/manifests/v1.0", nil, "password")
	if get.Code != http.StatusOK {
		t.Fatalf("GET by tag: status = %d, want 200", get.Code)
	}
	if !bytes.Equal(get.Body.Bytes(), body) {
		t.Errorf("GET by tag body mismatch")
	}
	if got := get.Header().Get("Docker-Content-Digest"); got != digest {
		t.Errorf("GET digest header = %q, want %q", got, digest)
	}
	if got := get.Header().Get("Content-Type"); got != imageManifestType {
		t.Errorf("GET content-type = %q, want %q", got, imageManifestType)
	}

	// GET by digest works too.
	if byDigest := h.req(t, http.MethodGet, "/v2/"+name+"/manifests/"+digest, nil, "password"); byDigest.Code != http.StatusOK {
		t.Fatalf("GET by digest: status = %d, want 200", byDigest.Code)
	}

	// HEAD reports presence without a body.
	head := h.req(t, http.MethodHead, "/v2/"+name+"/manifests/v1.0", nil, "password")
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Fatalf("HEAD: status = %d, bodyLen = %d", head.Code, head.Body.Len())
	}
}

func TestManifestMissingBlobRejected(t *testing.T) {
	h := newHarness(t)
	const name = "library/alpine"

	// A manifest that references blobs which were never uploaded.
	m := testManifest{
		SchemaVersion: 2,
		MediaType:     imageManifestType,
		Config:        testDesc{MediaType: "application/vnd.oci.image.config.v1+json", Digest: blob.DigestBytes([]byte("nope-config")), Size: 11},
		Layers:        []testDesc{{Digest: blob.DigestBytes([]byte("nope-layer")), Size: 10}},
	}
	body, _ := json.Marshal(m)

	put := h.putManifest(t, name, "v1.0", body)
	if put.Code != http.StatusBadRequest {
		t.Fatalf("PUT with missing blob: status = %d, want 400; body=%s", put.Code, put.Body.String())
	}
	if !bytes.Contains(put.Body.Bytes(), []byte("MANIFEST_BLOB_UNKNOWN")) {
		t.Errorf("expected MANIFEST_BLOB_UNKNOWN, got %s", put.Body.String())
	}
}

func TestManifestUnknownProjectRejected(t *testing.T) {
	h := newHarness(t)
	// "ghost" is not a project; the config blob push targets the same name.
	body, _ := h.buildImage(t, "ghost/app")

	put := h.putManifest(t, "ghost/app", "v1.0", body)
	if put.Code != http.StatusNotFound {
		t.Fatalf("PUT to unknown project: status = %d, want 404; body=%s", put.Code, put.Body.String())
	}
	if !bytes.Contains(put.Body.Bytes(), []byte("NAME_UNKNOWN")) {
		t.Errorf("expected NAME_UNKNOWN, got %s", put.Body.String())
	}
}

func TestManifestDigestMismatchRejected(t *testing.T) {
	h := newHarness(t)
	const name = "library/alpine"
	body, _ := h.buildImage(t, name)
	wrong := blob.DigestBytes([]byte("some other content"))

	put := h.putManifest(t, name, wrong, body)
	if put.Code != http.StatusBadRequest {
		t.Fatalf("PUT by mismatched digest: status = %d, want 400; body=%s", put.Code, put.Body.String())
	}
}

func TestManifestDeleteTagAndManifest(t *testing.T) {
	h := newHarness(t)
	const name = "library/alpine"
	body, digest := h.buildImage(t, name)
	if put := h.putManifest(t, name, "v1.0", body); put.Code != http.StatusCreated {
		t.Fatalf("seed PUT: status = %d", put.Code)
	}

	// Deleting the tag leaves the manifest reachable by digest.
	if del := h.req(t, http.MethodDelete, "/v2/"+name+"/manifests/v1.0", nil, "password"); del.Code != http.StatusAccepted {
		t.Fatalf("DELETE tag: status = %d, want 202", del.Code)
	}
	if get := h.req(t, http.MethodGet, "/v2/"+name+"/manifests/v1.0", nil, "password"); get.Code != http.StatusNotFound {
		t.Fatalf("GET after tag delete: status = %d, want 404", get.Code)
	}
	if get := h.req(t, http.MethodGet, "/v2/"+name+"/manifests/"+digest, nil, "password"); get.Code != http.StatusOK {
		t.Fatalf("GET by digest after tag delete: status = %d, want 200", get.Code)
	}

	// Deleting by digest removes the manifest itself.
	if del := h.req(t, http.MethodDelete, "/v2/"+name+"/manifests/"+digest, nil, "password"); del.Code != http.StatusAccepted {
		t.Fatalf("DELETE manifest: status = %d, want 202", del.Code)
	}
	if get := h.req(t, http.MethodGet, "/v2/"+name+"/manifests/"+digest, nil, "password"); get.Code != http.StatusNotFound {
		t.Fatalf("GET after manifest delete: status = %d, want 404", get.Code)
	}
}

func TestTagsList(t *testing.T) {
	h := newHarness(t)
	const name = "library/alpine"
	body, _ := h.buildImage(t, name)
	for _, tag := range []string{"v2.0", "v1.0", "latest"} {
		if put := h.putManifest(t, name, tag, body); put.Code != http.StatusCreated {
			t.Fatalf("PUT %s: status = %d", tag, put.Code)
		}
	}

	rr := h.req(t, http.MethodGet, "/v2/"+name+"/tags/list", nil, "password")
	if rr.Code != http.StatusOK {
		t.Fatalf("tags/list: status = %d, want 200", rr.Code)
	}
	var got tagListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode tags/list: %v", err)
	}
	if got.Name != name {
		t.Errorf("name = %q, want %q", got.Name, name)
	}
	// Tags come back lexically sorted.
	want := []string{"latest", "v1.0", "v2.0"}
	if len(got.Tags) != len(want) {
		t.Fatalf("tags = %v, want %v", got.Tags, want)
	}
	for i := range want {
		if got.Tags[i] != want[i] {
			t.Fatalf("tags = %v, want %v", got.Tags, want)
		}
	}

	// n limits the page and sets a Link header for the next page.
	page := h.req(t, http.MethodGet, "/v2/"+name+"/tags/list?n=2", nil, "password")
	var first tagListResponse
	if err := json.Unmarshal(page.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode page: %v", err)
	}
	if len(first.Tags) != 2 || first.Tags[0] != "latest" || first.Tags[1] != "v1.0" {
		t.Fatalf("first page = %v, want [latest v1.0]", first.Tags)
	}
	if page.Header().Get("Link") == "" {
		t.Error("expected a Link header for the next page")
	}
}

type tagListResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}
