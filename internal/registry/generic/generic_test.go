package generic_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/registry"
	"github.com/platbor/platbor/internal/registry/generic"
	"github.com/platbor/platbor/internal/registry/oci"
)

type harness struct {
	router http.Handler
	auth   *auth.Service
	db     *sql.DB
	blobs  blob.Store
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

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
	projects := project.NewService(sqlDB)
	if _, err := projects.Create(ctx, project.CreateInput{Key: "files", Name: "Files", Actor: "admin"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := projects.Create(ctx, project.CreateInput{
		Key: "mirror", Name: "Mirror", Actor: "admin",
		Upstream: &project.Upstream{URL: "https://example.com"},
	}); err != nil {
		t.Fatalf("create proxy project: %v", err)
	}
	store, err := blob.NewFS(cfg.DataDir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	r := chi.NewRouter()
	r.Route("/generic", func(sub chi.Router) {
		generic.New().Mount(sub, registry.Deps{Blobs: store, Auth: authSvc, DB: sqlDB, Log: discardLogger()})
	})
	return &harness{router: r, auth: authSvc, db: sqlDB, blobs: store}
}

func (h *harness) do(t *testing.T, method, path string, body []byte, withAuth bool) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if withAuth {
		req.SetBasicAuth("admin", "password")
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

func TestUploadDownloadRoundTrip(t *testing.T) {
	h := newHarness(t)
	content := []byte("hello generic artifact")
	sum := sha256.Sum256(content)
	wantHex := hex.EncodeToString(sum[:])

	put := h.do(t, http.MethodPut, "/generic/files/tools/mytool/1.0.0/mytool.bin", content, true)
	if put.Code != http.StatusCreated {
		t.Fatalf("PUT: status = %d, want 201 (%s)", put.Code, put.Body.String())
	}

	get := h.do(t, http.MethodGet, "/generic/files/tools/mytool/1.0.0/mytool.bin", nil, true)
	if get.Code != http.StatusOK {
		t.Fatalf("GET: status = %d, want 200", get.Code)
	}
	if !bytes.Equal(get.Body.Bytes(), content) {
		t.Error("downloaded bytes do not match uploaded")
	}
	if got := get.Header().Get("X-Checksum-Sha256"); got != wantHex {
		t.Errorf("X-Checksum-Sha256 = %q, want %q", got, wantHex)
	}

	// HEAD returns headers, no body.
	head := h.do(t, http.MethodHead, "/generic/files/tools/mytool/1.0.0/mytool.bin", nil, true)
	if head.Code != http.StatusOK || head.Body.Len() != 0 {
		t.Errorf("HEAD: status=%d bodyLen=%d, want 200 and empty", head.Code, head.Body.Len())
	}
	if head.Header().Get("Content-Length") == "" {
		t.Error("HEAD missing Content-Length")
	}

	// Checksum sibling.
	cs := h.do(t, http.MethodGet, "/generic/files/tools/mytool/1.0.0/mytool.bin.sha256", nil, true)
	if cs.Code != http.StatusOK {
		t.Fatalf("checksum GET: status = %d, want 200", cs.Code)
	}
	if got := string(bytes.TrimSpace(cs.Body.Bytes())); got != wantHex {
		t.Errorf("checksum body = %q, want %q", got, wantHex)
	}
}

func TestOverwriteAndDelete(t *testing.T) {
	h := newHarness(t)
	p := "/generic/files/bucket/data.txt"

	h.do(t, http.MethodPut, p, []byte("v1"), true)
	if rr := h.do(t, http.MethodPut, p, []byte("v2-longer"), true); rr.Code != http.StatusCreated {
		t.Fatalf("overwrite PUT: status = %d, want 201", rr.Code)
	}
	get := h.do(t, http.MethodGet, p, nil, true)
	if string(get.Body.Bytes()) != "v2-longer" {
		t.Errorf("after overwrite = %q, want v2-longer", get.Body.String())
	}

	if rr := h.do(t, http.MethodDelete, p, nil, true); rr.Code != http.StatusNoContent {
		t.Fatalf("DELETE: status = %d, want 204", rr.Code)
	}
	if rr := h.do(t, http.MethodGet, p, nil, true); rr.Code != http.StatusNotFound {
		t.Errorf("GET after delete: status = %d, want 404", rr.Code)
	}
	if rr := h.do(t, http.MethodDelete, p, nil, true); rr.Code != http.StatusNotFound {
		t.Errorf("DELETE missing: status = %d, want 404", rr.Code)
	}
}

func TestAuthAndValidation(t *testing.T) {
	h := newHarness(t)
	if rr := h.do(t, http.MethodGet, "/generic/files/bucket/x", nil, false); rr.Code != http.StatusUnauthorized {
		t.Errorf("no auth: status = %d, want 401", rr.Code)
	}
	// A dot-segment path is rejected (the ".." arrives encoded so it survives URL
	// cleaning and reaches validPath).
	if rr := h.do(t, http.MethodPut, "/generic/files/bucket/..%2fescape.bin", []byte("x"), true); rr.Code != http.StatusBadRequest {
		t.Errorf("traversal: status = %d, want 400", rr.Code)
	}
	// Proxy projects are read-only.
	if rr := h.do(t, http.MethodPut, "/generic/mirror/bucket/x.bin", []byte("x"), true); rr.Code != http.StatusForbidden {
		t.Errorf("proxy upload: status = %d, want 403", rr.Code)
	}
}

// TestGCKeepsGenericBlobs proves the collector marks generic blobs so a sweep
// never deletes live file content.
func TestGCKeepsGenericBlobs(t *testing.T) {
	h := newHarness(t)
	h.do(t, http.MethodPut, "/generic/files/bucket/keep.bin", []byte("live-generic-content"), true)

	ctx := context.Background()
	future := time.Now().UTC().Add(48 * time.Hour)

	bare := oci.NewCollector(h.blobs, h.db)
	if rep, err := bare.Collect(ctx, "admin", 0, true, future); err != nil {
		t.Fatalf("bare Collect: %v", err)
	} else if rep.Deleted == 0 {
		t.Fatal("expected the generic blob to be sweepable without its referencer")
	}

	guarded := oci.NewCollector(h.blobs, h.db, generic.NewReferencer(h.db))
	rep, err := guarded.Collect(ctx, "admin", 0, true, future)
	if err != nil {
		t.Fatalf("guarded Collect: %v", err)
	}
	if rep.Deleted != 0 {
		t.Errorf("GC deleted %d blobs; generic files must be kept", rep.Deleted)
	}
}
