package oci_test

import (
	"bytes"
	"context"
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
	store, err := blob.NewFS(cfg.DataDir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	r := chi.NewRouter()
	r.Route("/v2", func(sub chi.Router) {
		oci.New().Mount(sub, registry.Deps{Blobs: store, Auth: authSvc, Log: discardLogger()})
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
