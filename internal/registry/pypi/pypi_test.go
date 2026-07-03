package pypi_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"
	"log/slog"
	"mime/multipart"
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
	"github.com/platbor/platbor/internal/core/repository"
	"github.com/platbor/platbor/internal/registry"
	"github.com/platbor/platbor/internal/registry/oci"
	"github.com/platbor/platbor/internal/registry/pypi"
)

type harness struct {
	router http.Handler
	auth   *auth.Service
	db     *sql.DB
	blobs  blob.Store
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newHarness(t *testing.T, upstreamURL string) *harness {
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
	proj, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "py", Name: "Py", AllowAutoCreate: true, Actor: "admin"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	repos := repository.NewService(sqlDB)
	if _, err := repos.Create(ctx, repository.CreateInput{ProjectID: proj.ID, Key: "local", Name: "Local", Format: repository.FormatPyPI, Mode: repository.ModeLocal, Actor: "admin"}); err != nil {
		t.Fatalf("create local repo: %v", err)
	}
	if upstreamURL != "" {
		if _, err := repos.Create(ctx, repository.CreateInput{
			ProjectID: proj.ID, Key: "mirror", Name: "Mirror", Format: repository.FormatPyPI, Mode: repository.ModeProxy,
			Upstream: &repository.Upstream{URL: upstreamURL}, Actor: "admin",
		}); err != nil {
			t.Fatalf("create proxy repo: %v", err)
		}
	}
	store, err := blob.NewFS(cfg.DataDir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	r := chi.NewRouter()
	r.Route("/pypi", func(sub chi.Router) {
		pypi.New().Mount(sub, registry.Deps{Blobs: store, Auth: authSvc, DB: sqlDB, Repositories: repository.NewService(sqlDB), Log: discardLogger()})
	})
	return &harness{router: r, auth: authSvc, db: sqlDB, blobs: store}
}

func (h *harness) token(t *testing.T) string {
	t.Helper()
	u, err := h.auth.Authenticate(context.Background(), "admin", "password")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	raw, _, err := h.auth.CreateToken(context.Background(), u.ID, "admin", "pypi", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	return raw
}

func (h *harness) get(t *testing.T, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "localhost:8097"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

// upload posts a distribution the way twine does: multipart with metadata fields
// and a "content" file part.
func (h *harness) upload(t *testing.T, repo, name, version, filename string, content []byte, token string) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField(":action", "file_upload")
	_ = mw.WriteField("name", name)
	_ = mw.WriteField("version", version)
	sum := sha256.Sum256(content)
	_ = mw.WriteField("sha256_digest", hex.EncodeToString(sum[:]))
	fw, err := mw.CreateFormFile("content", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	_, _ = fw.Write(content)
	_ = mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/pypi/py/"+repo+"/", &buf)
	req.Host = "localhost:8097"
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

func TestUploadSimpleDownloadRoundTrip(t *testing.T) {
	h := newHarness(t, "")
	tok := h.token(t)
	content := []byte("wheel-bytes-for-demo")
	filename := "demo_pkg-1.0.0-py3-none-any.whl"

	// Unauthenticated upload is rejected.
	if rr := h.upload(t, "local", "Demo-Pkg", "1.0.0", filename, content, ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth upload: %d, want 401", rr.Code)
	}
	// twine upload.
	if rr := h.upload(t, "local", "Demo-Pkg", "1.0.0", filename, content, tok); rr.Code != http.StatusOK {
		t.Fatalf("upload: %d (%s)", rr.Code, rr.Body.String())
	}

	// The simple index (by PEP 503 normalized name) lists the file with a
	// #sha256 fragment pointing at us.
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	idx := h.get(t, "/pypi/py/local/simple/demo-pkg/", tok)
	if idx.Code != http.StatusOK {
		t.Fatalf("simple index: %d", idx.Code)
	}
	body := idx.Body.String()
	if !bytes.Contains([]byte(body), []byte(filename)) || !bytes.Contains([]byte(body), []byte("#sha256="+sha)) {
		t.Errorf("simple index missing file or hash:\n%s", body)
	}
	if !bytes.Contains([]byte(body), []byte("http://localhost:8097/pypi/py/local/files/"+filename)) {
		t.Errorf("simple index link not pointed at us:\n%s", body)
	}

	// The file downloads byte-for-byte.
	dl := h.get(t, "/pypi/py/local/files/"+filename, tok)
	if dl.Code != http.StatusOK || !bytes.Equal(dl.Body.Bytes(), content) {
		t.Errorf("download: status=%d match=%v", dl.Code, bytes.Equal(dl.Body.Bytes(), content))
	}

	// Re-uploading the same filename is a conflict.
	if rr := h.upload(t, "local", "Demo-Pkg", "1.0.0", filename, content, tok); rr.Code != http.StatusConflict {
		t.Errorf("re-upload: %d, want 409", rr.Code)
	}
}

func TestNameNormalization(t *testing.T) {
	h := newHarness(t, "")
	tok := h.token(t)
	h.upload(t, "local", "Flask_Foo", "2.0.0", "flask_foo-2.0.0.tar.gz", []byte("sdist"), tok)

	// A normalized request resolves the underscore-named upload.
	if rr := h.get(t, "/pypi/py/local/simple/flask-foo/", tok); rr.Code != http.StatusOK {
		t.Errorf("normalized simple index: %d, want 200", rr.Code)
	}
}

func TestPushToProxyRejected(t *testing.T) {
	up := httptest.NewServer(http.NewServeMux())
	defer up.Close()
	h := newHarness(t, up.URL+"/simple")
	tok := h.token(t)
	if rr := h.upload(t, "mirror", "x", "1.0.0", "x-1.0.0.tar.gz", []byte("y"), tok); rr.Code != http.StatusForbidden {
		t.Fatalf("upload to proxy: %d, want 403", rr.Code)
	}
}

func TestProxyPullThrough(t *testing.T) {
	dist := []byte("upstream-distribution-content")
	filename := "left_pad-1.0.0-py3-none-any.whl"
	sum := sha256.Sum256(dist)
	sha := hex.EncodeToString(sum[:])
	var fileHits int

	mux := http.NewServeMux()
	var serverURL string
	mux.HandleFunc("/simple/left-pad/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `<!DOCTYPE html><html><body><a href="`+serverURL+`/files/`+filename+`#sha256=`+sha+`">`+filename+`</a></body></html>`)
	})
	mux.HandleFunc("/files/"+filename, func(w http.ResponseWriter, _ *http.Request) {
		fileHits++
		_, _ = w.Write(dist)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	serverURL = srv.URL

	h := newHarness(t, srv.URL+"/simple")
	tok := h.token(t)

	// The simple index is proxied and its link rewritten to point at us.
	idx := h.get(t, "/pypi/py/mirror/simple/left-pad/", tok)
	if idx.Code != http.StatusOK {
		t.Fatalf("proxy simple index: %d (%s)", idx.Code, idx.Body.String())
	}
	want := "http://localhost:8097/pypi/py/mirror/files/" + filename
	if !bytes.Contains(idx.Body.Bytes(), []byte(want)) {
		t.Errorf("proxy index link not rewritten to us:\n%s", idx.Body.String())
	}
	if bytes.Contains(idx.Body.Bytes(), []byte(srv.URL)) {
		t.Errorf("proxy index still references the upstream host:\n%s", idx.Body.String())
	}

	// First download fills the cache from upstream; the second is a local hit.
	for i := 0; i < 2; i++ {
		dl := h.get(t, "/pypi/py/mirror/files/"+filename, tok)
		if dl.Code != http.StatusOK || !bytes.Equal(dl.Body.Bytes(), dist) {
			t.Fatalf("proxy download #%d: status=%d match=%v", i, dl.Code, bytes.Equal(dl.Body.Bytes(), dist))
		}
	}
	if fileHits != 1 {
		t.Errorf("upstream file fetched %d times; want 1 (cached after first)", fileHits)
	}
}

// TestGCKeepsPypiBlobs proves the collector marks PyPI distribution blobs so a
// sweep never deletes live package content.
func TestGCKeepsPypiBlobs(t *testing.T) {
	h := newHarness(t, "")
	tok := h.token(t)
	h.upload(t, "local", "keep-me", "1.0.0", "keep_me-1.0.0.tar.gz", []byte("dist-bytes"), tok)

	ctx := context.Background()
	future := time.Now().UTC().Add(48 * time.Hour)
	bare := oci.NewCollector(h.blobs, h.db)
	if rep, err := bare.Collect(ctx, "admin", 0, true, future); err != nil {
		t.Fatalf("bare Collect: %v", err)
	} else if rep.Deleted == 0 {
		t.Fatal("expected the distribution blob to be sweepable without its referencer")
	}
	guarded := oci.NewCollector(h.blobs, h.db, pypi.NewReferencer(h.db))
	rep, err := guarded.Collect(ctx, "admin", 0, true, future)
	if err != nil {
		t.Fatalf("guarded Collect: %v", err)
	}
	if rep.Deleted != 0 {
		t.Errorf("GC deleted %d blobs; pypi distributions must be kept", rep.Deleted)
	}
}
