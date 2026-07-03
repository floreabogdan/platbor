package cargo_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
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
	"github.com/platbor/platbor/internal/registry/cargo"
	"github.com/platbor/platbor/internal/registry/oci"
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
	proj, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "rs", Name: "Rs", AllowAutoCreate: true, Actor: "admin"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	repos := repository.NewService(sqlDB)
	if _, err := repos.Create(ctx, repository.CreateInput{ProjectID: proj.ID, Key: "local", Name: "Local", Format: repository.FormatCargo, Mode: repository.ModeLocal, Actor: "admin"}); err != nil {
		t.Fatalf("create local repo: %v", err)
	}
	if upstreamURL != "" {
		if _, err := repos.Create(ctx, repository.CreateInput{
			ProjectID: proj.ID, Key: "crates-io", Name: "CratesIo", Format: repository.FormatCargo, Mode: repository.ModeProxy,
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
	r.Route("/cargo", func(sub chi.Router) {
		cargo.New().Mount(sub, registry.Deps{Blobs: store, Auth: authSvc, DB: sqlDB, Repositories: repository.NewService(sqlDB), Log: discardLogger()})
	})
	h := &harness{router: r, auth: authSvc, db: sqlDB, blobs: store}
	u, err := authSvc.Authenticate(ctx, "admin", "password")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	raw, _, err := authSvc.CreateToken(ctx, u.ID, "admin", "cargo", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	h.token = raw
	return h
}

func (h *harness) req(t *testing.T, method, path string, body []byte, auth bool) *httptest.ResponseRecorder {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Host = "localhost:8097"
	if auth {
		req.Header.Set("Authorization", h.token) // cargo sends the bare token
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

// publishBody builds the cargo publish binary body.
func publishBody(meta map[string]any, crate []byte) []byte {
	mj, _ := json.Marshal(meta)
	var b bytes.Buffer
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(mj)))
	b.Write(mj)
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(crate)))
	b.Write(crate)
	return b.Bytes()
}

func meta(name, vers string) map[string]any {
	return map[string]any{"name": name, "vers": vers, "deps": []any{}, "features": map[string]any{}}
}

func TestPublishIndexDownloadRoundTrip(t *testing.T) {
	h := newHarness(t, "")
	crate := []byte("fake-crate-tarball-bytes")
	body := publishBody(meta("demo", "1.0.0"), crate)

	// Unauthenticated publish is rejected.
	if rr := h.req(t, http.MethodPut, "/cargo/rs/local/api/v1/crates/new", body, false); rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth publish: %d, want 401", rr.Code)
	}
	// cargo publish.
	if rr := h.req(t, http.MethodPut, "/cargo/rs/local/api/v1/crates/new", body, true); rr.Code != http.StatusOK {
		t.Fatalf("publish: %d (%s)", rr.Code, rr.Body.String())
	}

	// The sparse index for "demo" (3-char -> 3/d/demo) lists the version with its
	// checksum.
	sum := sha256.Sum256(crate)
	cksum := hex.EncodeToString(sum[:])
	idx := h.req(t, http.MethodGet, "/cargo/rs/local/3/d/demo", nil, true)
	if idx.Code != http.StatusOK {
		t.Fatalf("index: %d", idx.Code)
	}
	if !bytes.Contains(idx.Body.Bytes(), []byte(`"vers":"1.0.0"`)) || !bytes.Contains(idx.Body.Bytes(), []byte(cksum)) {
		t.Errorf("index missing version or cksum:\n%s", idx.Body.String())
	}

	// config.json advertises our dl and auth-required.
	cfg := h.req(t, http.MethodGet, "/cargo/rs/local/config.json", nil, true)
	if !bytes.Contains(cfg.Body.Bytes(), []byte("/cargo/rs/local/api/v1/crates")) || !bytes.Contains(cfg.Body.Bytes(), []byte(`"auth-required":true`)) {
		t.Errorf("config.json unexpected:\n%s", cfg.Body.String())
	}

	// The .crate downloads byte-for-byte.
	dl := h.req(t, http.MethodGet, "/cargo/rs/local/api/v1/crates/demo/1.0.0/download", nil, true)
	if dl.Code != http.StatusOK || !bytes.Equal(dl.Body.Bytes(), crate) {
		t.Fatalf("download: status=%d match=%v", dl.Code, bytes.Equal(dl.Body.Bytes(), crate))
	}

	// Re-publishing the same version is a conflict.
	if rr := h.req(t, http.MethodPut, "/cargo/rs/local/api/v1/crates/new", body, true); rr.Code != http.StatusConflict {
		t.Errorf("re-publish: %d, want 409", rr.Code)
	}
}

func TestYankReflectedInIndex(t *testing.T) {
	h := newHarness(t, "")
	crate := []byte("crate")
	h.req(t, http.MethodPut, "/cargo/rs/local/api/v1/crates/new", publishBody(meta("demo", "1.0.0"), crate), true)

	if rr := h.req(t, http.MethodDelete, "/cargo/rs/local/api/v1/crates/demo/1.0.0/yank", nil, true); rr.Code != http.StatusOK {
		t.Fatalf("yank: %d", rr.Code)
	}
	idx := h.req(t, http.MethodGet, "/cargo/rs/local/3/d/demo", nil, true)
	if !bytes.Contains(idx.Body.Bytes(), []byte(`"yanked":true`)) {
		t.Errorf("index should show yanked:\n%s", idx.Body.String())
	}
	if rr := h.req(t, http.MethodPut, "/cargo/rs/local/api/v1/crates/demo/1.0.0/unyank", nil, true); rr.Code != http.StatusOK {
		t.Fatalf("unyank: %d", rr.Code)
	}
	idx = h.req(t, http.MethodGet, "/cargo/rs/local/3/d/demo", nil, true)
	if !bytes.Contains(idx.Body.Bytes(), []byte(`"yanked":false`)) {
		t.Errorf("index should show unyanked:\n%s", idx.Body.String())
	}
}

func TestPushToProxyRejected(t *testing.T) {
	up := httptest.NewServer(http.NewServeMux())
	defer up.Close()
	h := newHarness(t, up.URL)
	if rr := h.req(t, http.MethodPut, "/cargo/rs/crates-io/api/v1/crates/new", publishBody(meta("x", "1.0.0"), []byte("y")), true); rr.Code != http.StatusForbidden {
		t.Fatalf("publish to proxy: %d, want 403", rr.Code)
	}
}

func TestProxyPullThrough(t *testing.T) {
	crate := []byte("upstream-crate-bytes")
	sum := sha256.Sum256(crate)
	cksum := hex.EncodeToString(sum[:])
	indexLine := `{"name":"serde","vers":"1.0.0","deps":[],"cksum":"` + cksum + `","features":{},"yanked":false}`
	var crateHits int

	mux := http.NewServeMux()
	mux.HandleFunc("/config.json", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"dl":"`+serverBase(r)+`/crates","api":"`+serverBase(r)+`"}`)
	})
	mux.HandleFunc("/se/rd/serde", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, indexLine+"\n")
	})
	mux.HandleFunc("/crates/serde/1.0.0/download", func(w http.ResponseWriter, _ *http.Request) {
		crateHits++
		_, _ = w.Write(crate)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	h := newHarness(t, srv.URL)

	// The proxied index is served and the version recorded.
	idx := h.req(t, http.MethodGet, "/cargo/rs/crates-io/se/rd/serde", nil, true)
	if idx.Code != http.StatusOK || !bytes.Contains(idx.Body.Bytes(), []byte(`"vers":"1.0.0"`)) {
		t.Fatalf("proxy index: %d (%s)", idx.Code, idx.Body.String())
	}

	// The .crate caches on first download; the second is a local hit.
	for i := 0; i < 2; i++ {
		dl := h.req(t, http.MethodGet, "/cargo/rs/crates-io/api/v1/crates/serde/1.0.0/download", nil, true)
		if dl.Code != http.StatusOK || !bytes.Equal(dl.Body.Bytes(), crate) {
			t.Fatalf("proxy download #%d: status=%d match=%v (%s)", i, dl.Code, bytes.Equal(dl.Body.Bytes(), crate), dl.Body.String())
		}
	}
	if crateHits != 1 {
		t.Errorf("upstream crate fetched %d times; want 1 (cached after first)", crateHits)
	}
}

// TestGCKeepsCargoBlobs proves the collector marks .crate blobs.
func TestGCKeepsCargoBlobs(t *testing.T) {
	h := newHarness(t, "")
	h.req(t, http.MethodPut, "/cargo/rs/local/api/v1/crates/new", publishBody(meta("keepme", "1.0.0"), []byte("crate-bytes")), true)

	ctx := context.Background()
	future := time.Now().UTC().Add(48 * time.Hour)
	bare := oci.NewCollector(h.blobs, h.db)
	if rep, err := bare.Collect(ctx, "admin", 0, true, future); err != nil {
		t.Fatalf("bare Collect: %v", err)
	} else if rep.Deleted == 0 {
		t.Fatal("expected the crate blob to be sweepable without its referencer")
	}
	guarded := oci.NewCollector(h.blobs, h.db, cargo.NewReferencer(h.db))
	rep, err := guarded.Collect(ctx, "admin", 0, true, future)
	if err != nil {
		t.Fatalf("guarded Collect: %v", err)
	}
	if rep.Deleted != 0 {
		t.Errorf("GC deleted %d blobs; cargo crates must be kept", rep.Deleted)
	}
}

func serverBase(r *http.Request) string {
	return "http://" + r.Host
}
