package npm_test

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
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
	"github.com/platbor/platbor/internal/registry/npm"
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
	proj, err := projects.Create(ctx, project.CreateInput{Key: "npm-local", Name: "Local", AllowAutoCreate: true, Actor: "admin"})
	if err != nil {
		t.Fatalf("create local project: %v", err)
	}
	// A proxy npm repository for the publish-rejected assertion. Local repos
	// auto-create on first publish.
	if _, err := repository.NewService(sqlDB).Create(ctx, repository.CreateInput{
		ProjectID: proj.ID, Key: "proxy", Name: "Proxy",
		Format: repository.FormatNPM, Mode: repository.ModeProxy,
		Upstream: &repository.Upstream{URL: "https://registry.npmjs.org"}, Actor: "admin",
	}); err != nil {
		t.Fatalf("create proxy repo: %v", err)
	}
	store, err := blob.NewFS(cfg.DataDir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}

	r := chi.NewRouter()
	r.Route("/npm", func(sub chi.Router) {
		npm.New().Mount(sub, registry.Deps{Blobs: store, Auth: authSvc, DB: sqlDB, Repositories: repository.NewService(sqlDB), Log: discardLogger()})
	})
	return &harness{router: r, auth: authSvc, db: sqlDB, blobs: store}
}

// token issues a personal access token for the admin, as npm login would.
func (h *harness) token(t *testing.T) string {
	t.Helper()
	raw, _, err := h.auth.CreateToken(context.Background(), adminID(t, h), "admin", "npm-test", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	return raw
}

func adminID(t *testing.T, h *harness) string {
	t.Helper()
	u, err := h.auth.Authenticate(context.Background(), "admin", "password")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	return u.ID
}

// do issues a request with an optional bearer token.
func (h *harness) do(t *testing.T, method, path string, body []byte, token string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Host = "localhost:8099"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

// publishBody builds the document `npm publish` PUTs, with the tarball inlined
// as a base64 attachment and authoritative dist digests.
func publishBody(name, version string, tarball []byte) []byte {
	sha1sum := sha1.Sum(tarball)
	sha512sum := sha512.Sum512(tarball)
	integrity := "sha512-" + base64.StdEncoding.EncodeToString(sha512sum[:])
	ver := map[string]any{
		"name":    name,
		"version": version,
		"dist": map[string]any{
			"shasum":    hex.EncodeToString(sha1sum[:]),
			"integrity": integrity,
		},
	}
	doc := map[string]any{
		"name":      name,
		"dist-tags": map[string]string{"latest": version},
		"versions":  map[string]any{version: ver},
		"_attachments": map[string]any{
			name + "-" + version + ".tgz": map[string]any{
				"content_type": "application/octet-stream",
				"data":         base64.StdEncoding.EncodeToString(tarball),
				"length":       len(tarball),
			},
		},
	}
	b, _ := json.Marshal(doc)
	return b
}

const base = "/npm/npm-local/lib"

func TestLoginIssuesTokenAndWhoami(t *testing.T) {
	h := newHarness(t)

	body := []byte(`{"name":"admin","password":"password"}`)
	rr := h.do(t, http.MethodPut, base+"/-/user/org.couchdb.user:admin", body, "")
	if rr.Code != http.StatusCreated {
		t.Fatalf("login: status = %d, want 201 (%s)", rr.Code, rr.Body.String())
	}
	var login struct{ Token string }
	if err := json.Unmarshal(rr.Body.Bytes(), &login); err != nil || login.Token == "" {
		t.Fatalf("login response missing token: %s", rr.Body.String())
	}

	// The issued token authenticates whoami.
	who := h.do(t, http.MethodGet, base+"/-/whoami", nil, login.Token)
	if who.Code != http.StatusOK {
		t.Fatalf("whoami: status = %d, want 200", who.Code)
	}
	if got := who.Body.String(); !bytes.Contains([]byte(got), []byte(`"admin"`)) {
		t.Errorf("whoami = %s, want username admin", got)
	}

	// Wrong password is rejected.
	bad := h.do(t, http.MethodPut, base+"/-/user/org.couchdb.user:admin", []byte(`{"name":"admin","password":"nope"}`), "")
	if bad.Code != http.StatusUnauthorized {
		t.Errorf("bad login: status = %d, want 401", bad.Code)
	}
}

func TestRequiresAuth(t *testing.T) {
	h := newHarness(t)
	rr := h.do(t, http.MethodGet, base+"/anything", nil, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous read: status = %d, want 401", rr.Code)
	}
}

func TestPublishInstallRoundTrip(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t)
	tarball := []byte("fake-tarball-bytes-v1")

	rr := h.do(t, http.MethodPut, base+"/platbor-demo", publishBody("platbor-demo", "1.0.0", tarball), tok)
	if rr.Code != http.StatusCreated {
		t.Fatalf("publish: status = %d, want 201 (%s)", rr.Code, rr.Body.String())
	}

	// Packument reports the version with a tarball URL pointing back at us.
	pk := h.do(t, http.MethodGet, base+"/platbor-demo", nil, tok)
	if pk.Code != http.StatusOK {
		t.Fatalf("packument: status = %d, want 200", pk.Code)
	}
	var doc struct {
		DistTags map[string]string          `json:"dist-tags"`
		Versions map[string]json.RawMessage `json:"versions"`
	}
	if err := json.Unmarshal(pk.Body.Bytes(), &doc); err != nil {
		t.Fatalf("packument decode: %v", err)
	}
	if doc.DistTags["latest"] != "1.0.0" {
		t.Errorf("latest dist-tag = %q, want 1.0.0", doc.DistTags["latest"])
	}
	var v struct {
		Dist struct {
			Tarball   string `json:"tarball"`
			Shasum    string `json:"shasum"`
			Integrity string `json:"integrity"`
		} `json:"dist"`
	}
	if err := json.Unmarshal(doc.Versions["1.0.0"], &v); err != nil {
		t.Fatalf("version decode: %v", err)
	}
	wantURL := "http://localhost:8099/npm/npm-local/lib/platbor-demo/-/platbor-demo-1.0.0.tgz"
	if v.Dist.Tarball != wantURL {
		t.Errorf("tarball URL = %q, want %q", v.Dist.Tarball, wantURL)
	}
	wantSha := sha1.Sum(tarball)
	if v.Dist.Shasum != hex.EncodeToString(wantSha[:]) {
		t.Errorf("shasum = %q, want authoritative sha1", v.Dist.Shasum)
	}

	// The tarball downloads byte-for-byte.
	dl := h.do(t, http.MethodGet, base+"/platbor-demo/-/platbor-demo-1.0.0.tgz", nil, tok)
	if dl.Code != http.StatusOK {
		t.Fatalf("tarball download: status = %d, want 200", dl.Code)
	}
	if !bytes.Equal(dl.Body.Bytes(), tarball) {
		t.Errorf("downloaded tarball does not match published bytes")
	}

	// Re-publishing the same version is rejected.
	dup := h.do(t, http.MethodPut, base+"/platbor-demo", publishBody("platbor-demo", "1.0.0", tarball), tok)
	if dup.Code != http.StatusConflict {
		t.Errorf("re-publish: status = %d, want 409", dup.Code)
	}
}

func TestScopedPackage(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t)
	tarball := []byte("scoped-tarball")

	// npm sends scoped names percent-encoded (@scope%2fname).
	rr := h.do(t, http.MethodPut, base+"/@acme%2fwidgets", publishBody("@acme/widgets", "2.0.0", tarball), tok)
	if rr.Code != http.StatusCreated {
		t.Fatalf("scoped publish: status = %d, want 201 (%s)", rr.Code, rr.Body.String())
	}

	pk := h.do(t, http.MethodGet, base+"/@acme%2fwidgets", nil, tok)
	if pk.Code != http.StatusOK {
		t.Fatalf("scoped packument: status = %d, want 200", pk.Code)
	}
	var doc struct {
		Versions map[string]struct {
			Dist struct {
				Tarball string `json:"tarball"`
			} `json:"dist"`
		} `json:"versions"`
	}
	if err := json.Unmarshal(pk.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The download URL uses the unscoped basename in the filename.
	wantURL := "http://localhost:8099/npm/npm-local/lib/@acme/widgets/-/widgets-2.0.0.tgz"
	if got := doc.Versions["2.0.0"].Dist.Tarball; got != wantURL {
		t.Errorf("scoped tarball URL = %q, want %q", got, wantURL)
	}

	dl := h.do(t, http.MethodGet, base+"/@acme/widgets/-/widgets-2.0.0.tgz", nil, tok)
	if dl.Code != http.StatusOK || !bytes.Equal(dl.Body.Bytes(), tarball) {
		t.Errorf("scoped tarball download: status=%d match=%v", dl.Code, bytes.Equal(dl.Body.Bytes(), tarball))
	}
}

func TestDistTags(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t)
	h.do(t, http.MethodPut, base+"/pkg", publishBody("pkg", "1.0.0", []byte("a")), tok)
	h.do(t, http.MethodPut, base+"/pkg", publishBody("pkg", "1.0.0", []byte("a")), tok) // idempotent no-op path exercised elsewhere

	// Set a new tag.
	set := h.do(t, http.MethodPut, base+"/-/package/pkg/dist-tags/beta", []byte(`"1.0.0"`), tok)
	if set.Code != http.StatusCreated {
		t.Fatalf("set dist-tag: status = %d, want 201 (%s)", set.Code, set.Body.String())
	}

	ls := h.do(t, http.MethodGet, base+"/-/package/pkg/dist-tags", nil, tok)
	var tags map[string]string
	if err := json.Unmarshal(ls.Body.Bytes(), &tags); err != nil {
		t.Fatalf("dist-tags decode: %v", err)
	}
	if tags["beta"] != "1.0.0" || tags["latest"] != "1.0.0" {
		t.Errorf("dist-tags = %v, want beta and latest at 1.0.0", tags)
	}

	// latest cannot be removed.
	if rr := h.do(t, http.MethodDelete, base+"/-/package/pkg/dist-tags/latest", nil, tok); rr.Code != http.StatusBadRequest {
		t.Errorf("delete latest: status = %d, want 400", rr.Code)
	}
	// beta can.
	if rr := h.do(t, http.MethodDelete, base+"/-/package/pkg/dist-tags/beta", nil, tok); rr.Code != http.StatusOK {
		t.Errorf("delete beta: status = %d, want 200", rr.Code)
	}
}

func TestPublishToProxyRejected(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t)
	rr := h.do(t, http.MethodPut, "/npm/npm-local/proxy/pkg", publishBody("pkg", "1.0.0", []byte("x")), tok)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("publish to proxy: status = %d, want 403", rr.Code)
	}
}

func TestIntegrityMismatchRejected(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t)
	// Build a valid body, then corrupt the tarball bytes so the digests no
	// longer match what the body claims.
	body := publishBody("pkg", "1.0.0", []byte("original"))
	var doc map[string]any
	_ = json.Unmarshal(body, &doc)
	att := doc["_attachments"].(map[string]any)["pkg-1.0.0.tgz"].(map[string]any)
	att["data"] = base64.StdEncoding.EncodeToString([]byte("tampered"))
	tampered, _ := json.Marshal(doc)

	rr := h.do(t, http.MethodPut, base+"/pkg", tampered, tok)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("tampered publish: status = %d, want 400", rr.Code)
	}
}

// fakeUpstream is a stand-in npm registry: it serves one package's packument
// and tarball, counts tarball hits (to prove caching), and can be switched
// offline to exercise the cache fallback.
type fakeUpstream struct {
	server      *httptest.Server
	tarball     []byte
	tarballHits int
	offline     bool
}

func newFakeUpstream(t *testing.T, name, version string, tarball []byte) *fakeUpstream {
	t.Helper()
	f := &fakeUpstream{tarball: tarball}
	filename := name + "-" + version + ".tgz"
	if i := strings.LastIndex(name, "/"); i >= 0 {
		filename = name[i+1:] + "-" + version + ".tgz"
	}
	mux := http.NewServeMux()
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.offline {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	mux.HandleFunc("/"+name+"/-/"+filename, func(w http.ResponseWriter, _ *http.Request) {
		f.tarballHits++
		_, _ = w.Write(f.tarball)
	})
	mux.HandleFunc("/"+name, func(w http.ResponseWriter, _ *http.Request) {
		doc := map[string]any{
			"name":      name,
			"dist-tags": map[string]string{"latest": version},
			"versions": map[string]any{
				version: map[string]any{
					"name": name, "version": version,
					"dist": map[string]any{"tarball": f.server.URL + "/" + name + "/-/" + filename},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(doc)
	})
	t.Cleanup(f.server.Close)
	return f
}

func TestProxyPullThrough(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t)
	tarball := []byte("upstream-tarball-content")
	up := newFakeUpstream(t, "left-pad", "1.0.0", tarball)

	// A project with a proxy npm repository mirroring the fake upstream.
	proj, err := project.NewService(h.db).Create(context.Background(), project.CreateInput{
		Key: "up", Name: "Upstream", AllowAutoCreate: true, Actor: "admin",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := repository.NewService(h.db).Create(context.Background(), repository.CreateInput{
		ProjectID: proj.ID, Key: "cache", Name: "Cache",
		Format: repository.FormatNPM, Mode: repository.ModeProxy,
		Upstream: &repository.Upstream{URL: up.server.URL}, Actor: "admin",
	}); err != nil {
		t.Fatalf("create proxy repo: %v", err)
	}

	// Packument is fetched fresh and its tarball URL rewritten to us.
	pk := h.do(t, http.MethodGet, "/npm/up/cache/left-pad", nil, tok)
	if pk.Code != http.StatusOK {
		t.Fatalf("proxy packument: status = %d (%s)", pk.Code, pk.Body.String())
	}
	var doc struct {
		Versions map[string]struct {
			Dist struct {
				Tarball string `json:"tarball"`
			} `json:"dist"`
		} `json:"versions"`
	}
	_ = json.Unmarshal(pk.Body.Bytes(), &doc)
	want := "http://localhost:8099/npm/up/cache/left-pad/-/left-pad-1.0.0.tgz"
	if got := doc.Versions["1.0.0"].Dist.Tarball; got != want {
		t.Errorf("rewritten tarball URL = %q, want %q", got, want)
	}

	// First tarball GET fills the cache from upstream; second is a local hit.
	for i := 0; i < 2; i++ {
		dl := h.do(t, http.MethodGet, "/npm/up/cache/left-pad/-/left-pad-1.0.0.tgz", nil, tok)
		if dl.Code != http.StatusOK || !bytes.Equal(dl.Body.Bytes(), tarball) {
			t.Fatalf("tarball GET #%d: status=%d match=%v", i, dl.Code, bytes.Equal(dl.Body.Bytes(), tarball))
		}
	}
	if up.tarballHits != 1 {
		t.Errorf("upstream tarball fetched %d times; want 1 (cached after first)", up.tarballHits)
	}

	// Offline: the packument falls back to the cached version.
	up.offline = true
	off := h.do(t, http.MethodGet, "/npm/up/cache/left-pad", nil, tok)
	if off.Code != http.StatusOK {
		t.Fatalf("offline packument: status = %d, want cached 200", off.Code)
	}
	if !bytes.Contains(off.Body.Bytes(), []byte("1.0.0")) {
		t.Errorf("offline packument missing cached version: %s", off.Body.String())
	}
}

func TestBrowse(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t)
	h.do(t, http.MethodPut, base+"/browse-pkg", publishBody("browse-pkg", "1.0.0", []byte("aa")), tok)
	h.do(t, http.MethodPut, base+"/browse-pkg", publishBody("browse-pkg", "1.1.0", []byte("bbbb")), tok)

	br := npm.NewBrowser(h.db)
	pkgs, err := br.Packages(context.Background())
	if err != nil {
		t.Fatalf("Packages: %v", err)
	}
	var found *npm.PackageSummary
	for i := range pkgs {
		if pkgs[i].Name == "browse-pkg" {
			found = &pkgs[i]
		}
	}
	if found == nil {
		t.Fatalf("browse-pkg not in package index: %+v", pkgs)
	}
	if found.VersionCount != 2 || found.ProjectKey != "npm-local" {
		t.Errorf("summary = %+v, want 2 versions in npm-local", *found)
	}
	if found.SizeBytes != int64(len("aa")+len("bbbb")) {
		t.Errorf("size = %d, want %d", found.SizeBytes, len("aa")+len("bbbb"))
	}

	detail, err := br.Package(context.Background(), "npm-local", "lib", "browse-pkg")
	if err != nil {
		t.Fatalf("Package: %v", err)
	}
	if detail.DistTags["latest"] != "1.1.0" {
		t.Errorf("latest = %q, want 1.1.0", detail.DistTags["latest"])
	}
	// Newest first.
	if len(detail.Versions) != 2 || detail.Versions[0].Version != "1.1.0" {
		t.Errorf("versions = %+v, want newest-first [1.1.0, 1.0.0]", detail.Versions)
	}

	if _, err := br.Package(context.Background(), "npm-local", "lib", "nope"); err != npm.ErrPackageNotFound {
		t.Errorf("missing package: got %v, want ErrPackageNotFound", err)
	}
}

// TestGCKeepsNpmTarballs proves the collector marks npm tarball blobs so a sweep
// never deletes live package content — the cross-format GC guarantee.
func TestGCKeepsNpmTarballs(t *testing.T) {
	h := newHarness(t)
	tok := h.token(t)
	h.do(t, http.MethodPut, base+"/pkg", publishBody("pkg", "1.0.0", []byte("live-tarball")), tok)

	ctx := context.Background()
	// A cutoff far in the future makes every blob old enough to sweep, so only
	// the mark set protects them.
	future := time.Now().UTC().Add(48 * time.Hour)

	// Without the npm referencer, the tarball is unreferenced and swept.
	bare := oci.NewCollector(h.blobs, h.db)
	if rep, err := bare.Collect(ctx, "admin", 0, true, future); err != nil {
		t.Fatalf("bare Collect: %v", err)
	} else if rep.Deleted == 0 {
		t.Fatal("expected the tarball to be sweepable without the npm referencer")
	}

	// With it, the tarball is marked and kept.
	guarded := oci.NewCollector(h.blobs, h.db, npm.NewReferencer(h.db))
	rep, err := guarded.Collect(ctx, "admin", 0, true, future)
	if err != nil {
		t.Fatalf("guarded Collect: %v", err)
	}
	if rep.Deleted != 0 {
		t.Errorf("GC deleted %d blobs; npm tarballs must be kept", rep.Deleted)
	}
}
