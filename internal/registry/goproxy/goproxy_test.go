package goproxy_test

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
	"github.com/platbor/platbor/internal/registry/goproxy"
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
	proj, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "go", Name: "Go", AllowAutoCreate: true, Actor: "admin"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	repos := repository.NewService(sqlDB)
	if _, err := repos.Create(ctx, repository.CreateInput{ProjectID: proj.ID, Key: "local", Name: "Local", Format: repository.FormatGo, Mode: repository.ModeLocal, Actor: "admin"}); err != nil {
		t.Fatalf("create local repo: %v", err)
	}
	if upstreamURL != "" {
		if _, err := repos.Create(ctx, repository.CreateInput{
			ProjectID: proj.ID, Key: "proxy", Name: "Proxy", Format: repository.FormatGo, Mode: repository.ModeProxy,
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
	r.Route("/go", func(sub chi.Router) {
		goproxy.New().Mount(sub, registry.Deps{Blobs: store, Auth: authSvc, DB: sqlDB, Repositories: repository.NewService(sqlDB), Log: discardLogger()})
	})
	return &harness{router: r, auth: authSvc, db: sqlDB, blobs: store}
}

func (h *harness) token(t *testing.T) string {
	t.Helper()
	u, err := h.auth.Authenticate(context.Background(), "admin", "password")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	raw, _, err := h.auth.CreateToken(context.Background(), u.ID, "admin", "go", 0)
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

// fakeGoProxy serves a minimal Go module proxy for one module version.
func fakeGoProxy(zip []byte, hits map[string]*int) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/example.com/mod/@v/list", func(w http.ResponseWriter, _ *http.Request) {
		if hits["list"] != nil {
			*hits["list"]++
		}
		_, _ = io.WriteString(w, "v1.0.0\n")
	})
	mux.HandleFunc("/example.com/mod/@v/v1.0.0.info", func(w http.ResponseWriter, _ *http.Request) {
		if hits["info"] != nil {
			*hits["info"]++
		}
		_, _ = io.WriteString(w, `{"Version":"v1.0.0","Time":"2020-01-01T00:00:00Z"}`)
	})
	mux.HandleFunc("/example.com/mod/@v/v1.0.0.mod", func(w http.ResponseWriter, _ *http.Request) {
		if hits["mod"] != nil {
			*hits["mod"]++
		}
		_, _ = io.WriteString(w, "module example.com/mod\n\ngo 1.20\n")
	})
	mux.HandleFunc("/example.com/mod/@v/v1.0.0.zip", func(w http.ResponseWriter, _ *http.Request) {
		if hits["zip"] != nil {
			*hits["zip"]++
		}
		_, _ = w.Write(zip)
	})
	mux.HandleFunc("/example.com/mod/@latest", func(w http.ResponseWriter, _ *http.Request) {
		if hits["latest"] != nil {
			*hits["latest"]++
		}
		_, _ = io.WriteString(w, `{"Version":"v1.0.0","Time":"2020-01-01T00:00:00Z"}`)
	})
	return mux
}

func TestProxyCachesImmutableFiles(t *testing.T) {
	zip := []byte("PK\x03\x04 fake module zip bytes")
	hits := map[string]*int{"info": new(int), "mod": new(int), "zip": new(int), "list": new(int), "latest": new(int)}
	srv := httptest.NewServer(fakeGoProxy(zip, hits))
	defer srv.Close()

	h := newHarness(t, srv.URL)
	tok := h.token(t)

	// Immutable per-version files are cached after the first fetch.
	for _, f := range []struct {
		path, want string
	}{
		{"/go/go/proxy/example.com/mod/@v/v1.0.0.info", `"Version":"v1.0.0"`},
		{"/go/go/proxy/example.com/mod/@v/v1.0.0.mod", "module example.com/mod"},
	} {
		for i := 0; i < 2; i++ {
			rr := h.get(t, f.path, tok)
			if rr.Code != http.StatusOK || !bytes.Contains(rr.Body.Bytes(), []byte(f.want)) {
				t.Fatalf("%s #%d: status=%d body=%q", f.path, i, rr.Code, rr.Body.String())
			}
		}
	}
	// The zip caches byte-for-byte and only hits the upstream once.
	for i := 0; i < 2; i++ {
		rr := h.get(t, "/go/go/proxy/example.com/mod/@v/v1.0.0.zip", tok)
		if rr.Code != http.StatusOK || !bytes.Equal(rr.Body.Bytes(), zip) {
			t.Fatalf("zip #%d: status=%d match=%v", i, rr.Code, bytes.Equal(rr.Body.Bytes(), zip))
		}
	}
	if *hits["info"] != 1 || *hits["mod"] != 1 || *hits["zip"] != 1 {
		t.Errorf("immutable files fetched more than once: info=%d mod=%d zip=%d", *hits["info"], *hits["mod"], *hits["zip"])
	}

	// list and @latest are mutable: fetched fresh every time, never cached.
	for i := 0; i < 2; i++ {
		if rr := h.get(t, "/go/go/proxy/example.com/mod/@v/list", tok); rr.Code != http.StatusOK {
			t.Fatalf("list #%d: %d", i, rr.Code)
		}
		if rr := h.get(t, "/go/go/proxy/example.com/mod/@latest", tok); rr.Code != http.StatusOK {
			t.Fatalf("latest #%d: %d", i, rr.Code)
		}
	}
	if *hits["list"] != 2 || *hits["latest"] != 2 {
		t.Errorf("mutable listings cached: list=%d latest=%d (want 2 each)", *hits["list"], *hits["latest"])
	}
}

func TestLocalRepoRejected(t *testing.T) {
	h := newHarness(t, "")
	tok := h.token(t)
	// A local Go repo has nothing to serve; the protocol is proxy-only.
	if rr := h.get(t, "/go/go/local/example.com/mod/@v/v1.0.0.info", tok); rr.Code != http.StatusBadRequest {
		t.Fatalf("local repo request: %d, want 400", rr.Code)
	}
}

func TestUnauthenticatedRejected(t *testing.T) {
	srv := httptest.NewServer(fakeGoProxy([]byte("z"), map[string]*int{}))
	defer srv.Close()
	h := newHarness(t, srv.URL)
	if rr := h.get(t, "/go/go/proxy/example.com/mod/@v/list", ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth: %d, want 401", rr.Code)
	}
}

// TestGCKeepsGoBlobs proves the collector marks cached Go blobs so a sweep never
// deletes live module content.
func TestGCKeepsGoBlobs(t *testing.T) {
	zip := []byte("PK\x03\x04 keep this module")
	srv := httptest.NewServer(fakeGoProxy(zip, map[string]*int{}))
	defer srv.Close()
	h := newHarness(t, srv.URL)
	tok := h.token(t)
	// Prime the cache with the zip.
	if rr := h.get(t, "/go/go/proxy/example.com/mod/@v/v1.0.0.zip", tok); rr.Code != http.StatusOK {
		t.Fatalf("prime: %d", rr.Code)
	}

	ctx := context.Background()
	future := time.Now().UTC().Add(48 * time.Hour)
	bare := oci.NewCollector(h.blobs, h.db)
	if rep, err := bare.Collect(ctx, "admin", 0, true, future); err != nil {
		t.Fatalf("bare Collect: %v", err)
	} else if rep.Deleted == 0 {
		t.Fatal("expected the module blob to be sweepable without its referencer")
	}
	guarded := oci.NewCollector(h.blobs, h.db, goproxy.NewReferencer(h.db))
	rep, err := guarded.Collect(ctx, "admin", 0, true, future)
	if err != nil {
		t.Fatalf("guarded Collect: %v", err)
	}
	if rep.Deleted != 0 {
		t.Errorf("GC deleted %d blobs; go modules must be kept", rep.Deleted)
	}
}
