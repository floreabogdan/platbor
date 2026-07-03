package rubygems_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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
	"github.com/platbor/platbor/internal/registry/rubygems"
)

type harness struct {
	router http.Handler
	auth   *auth.Service
	db     *sql.DB
	blobs  blob.Store
	token  string
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
	proj, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "rb", Name: "Rb", AllowAutoCreate: true, Actor: "admin"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	repos := repository.NewService(sqlDB)
	if _, err := repos.Create(ctx, repository.CreateInput{ProjectID: proj.ID, Key: "local", Name: "Local", Format: repository.FormatRubyGems, Mode: repository.ModeLocal, Actor: "admin"}); err != nil {
		t.Fatalf("create local repo: %v", err)
	}
	if upstreamURL != "" {
		if _, err := repos.Create(ctx, repository.CreateInput{
			ProjectID: proj.ID, Key: "rubygems-org", Name: "RubyGemsOrg", Format: repository.FormatRubyGems, Mode: repository.ModeProxy,
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
	r.Route("/rubygems", func(sub chi.Router) {
		rubygems.New().Mount(sub, registry.Deps{Blobs: store, Auth: authSvc, DB: sqlDB, Repositories: repository.NewService(sqlDB), Log: discardLogger()})
	})
	h := &harness{router: r, auth: authSvc, db: sqlDB, blobs: store}
	u, err := authSvc.Authenticate(ctx, "admin", "password")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	raw, _, err := authSvc.CreateToken(ctx, u.ID, "admin", "rubygems", 0)
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
		req.Header.Set("Authorization", h.token) // gem sends the bare key
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

// makeGem builds a minimal .gem: an uncompressed tar whose metadata.gz holds the
// gzipped gemspec YAML the adapter parses.
func makeGem(t *testing.T, name, version string) []byte {
	t.Helper()
	yaml := "--- !ruby/object:Gem::Specification\n" +
		"name: " + name + "\n" +
		"version: !ruby/object:Gem::Version\n  version: " + version + "\n" +
		"platform: ruby\n" +
		"dependencies: []\n" +
		"required_ruby_version: !ruby/object:Gem::Requirement\n" +
		"  requirements:\n  - - \">=\"\n    - !ruby/object:Gem::Version\n      version: '2.5'\n"

	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(yaml))
	_ = zw.Close()

	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	_ = tw.WriteHeader(&tar.Header{Name: "metadata.gz", Mode: 0o644, Size: int64(gz.Len())})
	_, _ = tw.Write(gz.Bytes())
	_ = tw.Close()
	return tarBuf.Bytes()
}

func TestPushCompactIndexDownloadRoundTrip(t *testing.T) {
	h := newHarness(t, "")
	gem := makeGem(t, "hello_gem", "1.0.0")

	// Unauthenticated push is rejected.
	if rr := h.req(t, http.MethodPost, "/rubygems/rb/local/api/v1/gems", gem, false); rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth push: %d, want 401", rr.Code)
	}
	// gem push.
	if rr := h.req(t, http.MethodPost, "/rubygems/rb/local/api/v1/gems", gem, true); rr.Code != http.StatusOK {
		t.Fatalf("push: %d (%s)", rr.Code, rr.Body.String())
	}

	// /info/<gem> lists the version with its checksum.
	sum := sha256.Sum256(gem)
	cksum := hex.EncodeToString(sum[:])
	info := h.req(t, http.MethodGet, "/rubygems/rb/local/info/hello_gem", nil, true)
	if info.Code != http.StatusOK {
		t.Fatalf("info: %d", info.Code)
	}
	if !strings.Contains(info.Body.String(), "1.0.0 |checksum:"+cksum) {
		t.Errorf("info missing version/checksum:\n%s", info.Body.String())
	}

	// /versions lists the gem with the md5 of its /info file.
	infoMD5 := md5.Sum(info.Body.Bytes())
	versions := h.req(t, http.MethodGet, "/rubygems/rb/local/versions", nil, true)
	wantLine := "hello_gem 1.0.0 " + hex.EncodeToString(infoMD5[:])
	if !strings.Contains(versions.Body.String(), wantLine) {
		t.Errorf("versions missing %q:\n%s", wantLine, versions.Body.String())
	}

	// /names lists the gem.
	names := h.req(t, http.MethodGet, "/rubygems/rb/local/names", nil, true)
	if !strings.Contains(names.Body.String(), "hello_gem") {
		t.Errorf("names missing gem:\n%s", names.Body.String())
	}

	// The .gem downloads byte-for-byte.
	dl := h.req(t, http.MethodGet, "/rubygems/rb/local/gems/hello_gem-1.0.0.gem", nil, true)
	if dl.Code != http.StatusOK || !bytes.Equal(dl.Body.Bytes(), gem) {
		t.Fatalf("download: status=%d match=%v", dl.Code, bytes.Equal(dl.Body.Bytes(), gem))
	}

	// A re-push is a conflict.
	if rr := h.req(t, http.MethodPost, "/rubygems/rb/local/api/v1/gems", gem, true); rr.Code != http.StatusConflict {
		t.Errorf("re-push: %d, want 409", rr.Code)
	}
}

func TestYankRemovesFromIndex(t *testing.T) {
	h := newHarness(t, "")
	h.req(t, http.MethodPost, "/rubygems/rb/local/api/v1/gems", makeGem(t, "yankme", "1.0.0"), true)
	h.req(t, http.MethodPost, "/rubygems/rb/local/api/v1/gems", makeGem(t, "yankme", "1.1.0"), true)

	if rr := h.req(t, http.MethodDelete, "/rubygems/rb/local/api/v1/gems/yank?gem_name=yankme&version=1.0.0", nil, true); rr.Code != http.StatusOK {
		t.Fatalf("yank: %d (%s)", rr.Code, rr.Body.String())
	}
	info := h.req(t, http.MethodGet, "/rubygems/rb/local/info/yankme", nil, true)
	if strings.Contains(info.Body.String(), "1.0.0 ") {
		t.Errorf("yanked version still in info:\n%s", info.Body.String())
	}
	if !strings.Contains(info.Body.String(), "1.1.0 ") {
		t.Errorf("live version missing from info:\n%s", info.Body.String())
	}
}

func TestPushToProxyRejected(t *testing.T) {
	up := httptest.NewServer(http.NewServeMux())
	defer up.Close()
	h := newHarness(t, up.URL)
	if rr := h.req(t, http.MethodPost, "/rubygems/rb/rubygems-org/api/v1/gems", makeGem(t, "x", "1.0.0"), true); rr.Code != http.StatusForbidden {
		t.Fatalf("push to proxy: %d, want 403", rr.Code)
	}
}

func TestProxyPullThrough(t *testing.T) {
	gem := []byte("upstream-gem-bytes")
	sum := sha256.Sum256(gem)
	cksum := hex.EncodeToString(sum[:])
	infoBody := "---\n1.0.0 |checksum:" + cksum + "\n"
	var gemHits int

	mux := http.NewServeMux()
	mux.HandleFunc("/info/rails", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, infoBody)
	})
	mux.HandleFunc("/gems/rails-1.0.0.gem", func(w http.ResponseWriter, _ *http.Request) {
		gemHits++
		_, _ = w.Write(gem)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	h := newHarness(t, srv.URL)

	// The proxied /info records the version and serves upstream bytes.
	info := h.req(t, http.MethodGet, "/rubygems/rb/rubygems-org/info/rails", nil, true)
	if info.Code != http.StatusOK || !strings.Contains(info.Body.String(), cksum) {
		t.Fatalf("proxy info: %d (%s)", info.Code, info.Body.String())
	}

	// The .gem caches on first download; the second is a local hit.
	for i := 0; i < 2; i++ {
		dl := h.req(t, http.MethodGet, "/rubygems/rb/rubygems-org/gems/rails-1.0.0.gem", nil, true)
		if dl.Code != http.StatusOK || !bytes.Equal(dl.Body.Bytes(), gem) {
			t.Fatalf("proxy download #%d: status=%d match=%v (%s)", i, dl.Code, bytes.Equal(dl.Body.Bytes(), gem), dl.Body.String())
		}
	}
	if gemHits != 1 {
		t.Errorf("upstream gem fetched %d times; want 1 (cached after first)", gemHits)
	}
}

// TestGCKeepsGemBlobs proves the collector marks .gem blobs.
func TestGCKeepsGemBlobs(t *testing.T) {
	h := newHarness(t, "")
	h.req(t, http.MethodPost, "/rubygems/rb/local/api/v1/gems", makeGem(t, "keepme", "1.0.0"), true)

	ctx := context.Background()
	future := time.Now().UTC().Add(48 * time.Hour)
	bare := oci.NewCollector(h.blobs, h.db)
	if rep, err := bare.Collect(ctx, "admin", 0, true, future); err != nil {
		t.Fatalf("bare Collect: %v", err)
	} else if rep.Deleted == 0 {
		t.Fatal("expected the gem blob to be sweepable without its referencer")
	}
	guarded := oci.NewCollector(h.blobs, h.db, rubygems.NewReferencer(h.db))
	rep, err := guarded.Collect(ctx, "admin", 0, true, future)
	if err != nil {
		t.Fatalf("guarded Collect: %v", err)
	}
	if rep.Deleted != 0 {
		t.Errorf("GC deleted %d blobs; gems must be kept", rep.Deleted)
	}
}
