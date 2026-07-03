package terraform_test

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/core/repository"
	"github.com/platbor/platbor/internal/registry"
	"github.com/platbor/platbor/internal/registry/oci"
	"github.com/platbor/platbor/internal/registry/terraform"
)

type harness struct {
	router http.Handler
	auth   *auth.Service
	db     *sql.DB
	blobs  blob.Store
	token  string
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
	if _, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "infra", Name: "Infra", AllowAutoCreate: true, Actor: "admin"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	store, err := blob.NewFS(cfg.DataDir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	r := chi.NewRouter()
	r.Get("/.well-known/terraform.json", terraform.Discovery())
	r.Route("/terraform", func(sub chi.Router) {
		terraform.New().Mount(sub, registry.Deps{Blobs: store, Auth: authSvc, DB: sqlDB, Repositories: repository.NewService(sqlDB), Log: discardLogger()})
	})
	h := &harness{router: r, auth: authSvc, db: sqlDB, blobs: store}
	u, err := authSvc.Authenticate(ctx, "admin", "password")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	raw, _, err := authSvc.CreateToken(ctx, u.ID, "admin", "terraform", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	h.token = raw
	return h
}

func (h *harness) req(t *testing.T, method, path string, body []byte, authOn bool) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Host = "localhost:8097"
	if authOn {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

func TestUploadDiscoverDownloadRoundTrip(t *testing.T) {
	h := newHarness(t)
	archive := []byte("fake-module-tar-gz-bytes")

	// Discovery is unauthenticated and points terraform at the module base.
	disc := h.req(t, http.MethodGet, "/.well-known/terraform.json", nil, false)
	if disc.Code != http.StatusOK || !strings.Contains(disc.Body.String(), `"modules.v1":"/terraform/v1/modules/"`) {
		t.Fatalf("discovery: %d (%s)", disc.Code, disc.Body.String())
	}

	// Unauthenticated upload is rejected.
	if rr := h.req(t, http.MethodPut, "/terraform/upload/infra/modules/vpc/aws/1.0.0", archive, false); rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth upload: %d, want 401", rr.Code)
	}
	// Upload the module.
	if rr := h.req(t, http.MethodPut, "/terraform/upload/infra/modules/vpc/aws/1.0.0", archive, true); rr.Code != http.StatusCreated {
		t.Fatalf("upload: %d (%s)", rr.Code, rr.Body.String())
	}
	// A second version.
	if rr := h.req(t, http.MethodPut, "/terraform/upload/infra/modules/vpc/aws/1.1.0", archive, true); rr.Code != http.StatusCreated {
		t.Fatalf("upload v2: %d", rr.Code)
	}

	// versions lists both (namespace = project key "infra").
	vers := h.req(t, http.MethodGet, "/terraform/v1/modules/infra/vpc/aws/versions", nil, true)
	if vers.Code != http.StatusOK {
		t.Fatalf("versions: %d", vers.Code)
	}
	if !strings.Contains(vers.Body.String(), `"1.0.0"`) || !strings.Contains(vers.Body.String(), `"1.1.0"`) {
		t.Errorf("versions missing entries:\n%s", vers.Body.String())
	}

	// download returns 204 with X-Terraform-Get pointing at our archive endpoint.
	dl := h.req(t, http.MethodGet, "/terraform/v1/modules/infra/vpc/aws/1.0.0/download", nil, true)
	if dl.Code != http.StatusNoContent {
		t.Fatalf("download: %d, want 204", dl.Code)
	}
	get := dl.Header().Get("X-Terraform-Get")
	if !strings.Contains(get, "/terraform/v1/modules/infra/vpc/aws/1.0.0/archive") {
		t.Errorf("unexpected X-Terraform-Get: %q", get)
	}

	// The archive downloads byte-for-byte.
	arc := h.req(t, http.MethodGet, "/terraform/v1/modules/infra/vpc/aws/1.0.0/archive", nil, true)
	if arc.Code != http.StatusOK || !bytes.Equal(arc.Body.Bytes(), archive) {
		t.Fatalf("archive: status=%d match=%v", arc.Code, bytes.Equal(arc.Body.Bytes(), archive))
	}

	// A re-upload of an existing version is a conflict.
	if rr := h.req(t, http.MethodPut, "/terraform/upload/infra/modules/vpc/aws/1.0.0", archive, true); rr.Code != http.StatusConflict {
		t.Errorf("re-upload: %d, want 409", rr.Code)
	}
}

func TestUnknownModuleIs404(t *testing.T) {
	h := newHarness(t)
	if rr := h.req(t, http.MethodGet, "/terraform/v1/modules/infra/nope/aws/versions", nil, true); rr.Code != http.StatusNotFound {
		t.Fatalf("unknown module: %d, want 404", rr.Code)
	}
}

// TestGCKeepsModuleBlobs proves the collector marks module archive blobs.
func TestGCKeepsModuleBlobs(t *testing.T) {
	h := newHarness(t)
	h.req(t, http.MethodPut, "/terraform/upload/infra/modules/keep/aws/1.0.0", []byte("module-bytes"), true)

	ctx := context.Background()
	future := time.Now().UTC().Add(48 * time.Hour)
	bare := oci.NewCollector(h.blobs, h.db)
	if rep, err := bare.Collect(ctx, "admin", 0, true, future); err != nil {
		t.Fatalf("bare Collect: %v", err)
	} else if rep.Deleted == 0 {
		t.Fatal("expected the module blob to be sweepable without its referencer")
	}
	guarded := oci.NewCollector(h.blobs, h.db, terraform.NewReferencer(h.db))
	rep, err := guarded.Collect(ctx, "admin", 0, true, future)
	if err != nil {
		t.Fatalf("guarded Collect: %v", err)
	}
	if rep.Deleted != 0 {
		t.Errorf("GC deleted %d blobs; module archives must be kept", rep.Deleted)
	}
}
