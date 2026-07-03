package maven_test

import (
	"bytes"
	"context"
	"database/sql"
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
	"github.com/platbor/platbor/internal/core/repository"
	"github.com/platbor/platbor/internal/registry"
	"github.com/platbor/platbor/internal/registry/maven"
	"github.com/platbor/platbor/internal/registry/oci"
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
	proj, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "mv", Name: "Mv", AllowAutoCreate: true, Actor: "admin"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	repos := repository.NewService(sqlDB)
	if _, err := repos.Create(ctx, repository.CreateInput{ProjectID: proj.ID, Key: "local", Name: "Local", Format: repository.FormatMaven, Mode: repository.ModeLocal, Actor: "admin"}); err != nil {
		t.Fatalf("create local repo: %v", err)
	}
	if upstreamURL != "" {
		if _, err := repos.Create(ctx, repository.CreateInput{
			ProjectID: proj.ID, Key: "central", Name: "Central", Format: repository.FormatMaven, Mode: repository.ModeProxy,
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
	r.Route("/maven", func(sub chi.Router) {
		maven.New().Mount(sub, registry.Deps{Blobs: store, Auth: authSvc, DB: sqlDB, Repositories: repository.NewService(sqlDB), Log: discardLogger()})
	})
	return &harness{router: r, auth: authSvc, db: sqlDB, blobs: store}
}

func (h *harness) token(t *testing.T) string {
	t.Helper()
	u, err := h.auth.Authenticate(context.Background(), "admin", "password")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	raw, _, err := h.auth.CreateToken(context.Background(), u.ID, "admin", "maven", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	return raw
}

func (h *harness) do(t *testing.T, method, path string, body []byte, token string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Host = "localhost:8097"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

func TestDeployDownloadRoundTrip(t *testing.T) {
	h := newHarness(t, "")
	tok := h.token(t)
	jar := []byte("fake-jar-bytes")
	pom := []byte("<project><modelVersion>4.0.0</modelVersion></project>")
	jarPath := "/maven/mv/local/com/example/demo/1.0.0/demo-1.0.0.jar"
	pomPath := "/maven/mv/local/com/example/demo/1.0.0/demo-1.0.0.pom"

	// Unauthenticated PUT is rejected.
	if rr := h.do(t, http.MethodPut, jarPath, jar, ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth put: %d, want 401", rr.Code)
	}
	// Deploy the jar and pom.
	if rr := h.do(t, http.MethodPut, jarPath, jar, tok); rr.Code != http.StatusCreated {
		t.Fatalf("put jar: %d (%s)", rr.Code, rr.Body.String())
	}
	if rr := h.do(t, http.MethodPut, pomPath, pom, tok); rr.Code != http.StatusCreated {
		t.Fatalf("put pom: %d", rr.Code)
	}

	// The jar downloads byte-for-byte with a checksum header.
	dl := h.do(t, http.MethodGet, jarPath, nil, tok)
	if dl.Code != http.StatusOK || !bytes.Equal(dl.Body.Bytes(), jar) {
		t.Fatalf("get jar: status=%d match=%v", dl.Code, bytes.Equal(dl.Body.Bytes(), jar))
	}
	if dl.Header().Get("X-Checksum-Sha1") == "" {
		t.Errorf("missing sha1 checksum header")
	}
	// HEAD works (mvn probes existence).
	if rr := h.do(t, http.MethodHead, pomPath, nil, tok); rr.Code != http.StatusOK {
		t.Errorf("head pom: %d, want 200", rr.Code)
	}
	// A missing file is 404.
	if rr := h.do(t, http.MethodGet, "/maven/mv/local/com/example/demo/9.9.9/demo-9.9.9.jar", nil, tok); rr.Code != http.StatusNotFound {
		t.Errorf("missing get: %d, want 404", rr.Code)
	}
}

func TestPushToProxyRejected(t *testing.T) {
	up := httptest.NewServer(http.NewServeMux())
	defer up.Close()
	h := newHarness(t, up.URL)
	tok := h.token(t)
	if rr := h.do(t, http.MethodPut, "/maven/mv/central/com/x/y/1.0/y-1.0.jar", []byte("z"), tok); rr.Code != http.StatusForbidden {
		t.Fatalf("put to proxy: %d, want 403", rr.Code)
	}
}

func TestProxyPullThrough(t *testing.T) {
	jar := []byte("upstream-jar-content")
	var jarHits, metaHits int
	mux := http.NewServeMux()
	mux.HandleFunc("/com/example/lib/2.0.0/lib-2.0.0.jar", func(w http.ResponseWriter, _ *http.Request) {
		jarHits++
		_, _ = w.Write(jar)
	})
	mux.HandleFunc("/com/example/lib/maven-metadata.xml", func(w http.ResponseWriter, _ *http.Request) {
		metaHits++
		_, _ = io.WriteString(w, "<metadata><versioning><versions><version>2.0.0</version></versions></versioning></metadata>")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	h := newHarness(t, srv.URL)
	tok := h.token(t)

	// The immutable jar is cached after the first fetch.
	for i := 0; i < 2; i++ {
		dl := h.do(t, http.MethodGet, "/maven/mv/central/com/example/lib/2.0.0/lib-2.0.0.jar", nil, tok)
		if dl.Code != http.StatusOK || !bytes.Equal(dl.Body.Bytes(), jar) {
			t.Fatalf("proxy jar #%d: status=%d match=%v (%s)", i, dl.Code, bytes.Equal(dl.Body.Bytes(), jar), dl.Body.String())
		}
	}
	if jarHits != 1 {
		t.Errorf("upstream jar fetched %d times; want 1 (cached after first)", jarHits)
	}

	// maven-metadata.xml is mutable, so it is re-fetched fresh every time.
	for i := 0; i < 2; i++ {
		md := h.do(t, http.MethodGet, "/maven/mv/central/com/example/lib/maven-metadata.xml", nil, tok)
		if md.Code != http.StatusOK {
			t.Fatalf("proxy metadata #%d: %d", i, md.Code)
		}
	}
	if metaHits != 2 {
		t.Errorf("metadata fetched %d times; want 2 (never cached)", metaHits)
	}
}

// TestGCKeepsMavenBlobs proves the collector marks Maven file blobs so a sweep
// never deletes live artifact content.
func TestGCKeepsMavenBlobs(t *testing.T) {
	h := newHarness(t, "")
	tok := h.token(t)
	h.do(t, http.MethodPut, "/maven/mv/local/org/keep/thing/1.0/thing-1.0.jar", []byte("jar-bytes"), tok)

	ctx := context.Background()
	future := time.Now().UTC().Add(48 * time.Hour)
	bare := oci.NewCollector(h.blobs, h.db)
	if rep, err := bare.Collect(ctx, "admin", 0, true, future); err != nil {
		t.Fatalf("bare Collect: %v", err)
	} else if rep.Deleted == 0 {
		t.Fatal("expected the artifact blob to be sweepable without its referencer")
	}
	guarded := oci.NewCollector(h.blobs, h.db, maven.NewReferencer(h.db))
	rep, err := guarded.Collect(ctx, "admin", 0, true, future)
	if err != nil {
		t.Fatalf("guarded Collect: %v", err)
	}
	if rep.Deleted != 0 {
		t.Errorf("GC deleted %d blobs; maven artifacts must be kept", rep.Deleted)
	}
}
