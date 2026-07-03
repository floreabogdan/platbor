package nuget_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
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
	"github.com/platbor/platbor/internal/registry/nuget"
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
	proj, err := projects.Create(ctx, project.CreateInput{Key: "feed", Name: "Feed", AllowAutoCreate: true, Actor: "admin"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	repos := repository.NewService(sqlDB)
	if _, err := repos.Create(ctx, repository.CreateInput{
		ProjectID: proj.ID, Key: "local", Name: "Local",
		Format: repository.FormatNuGet, Mode: repository.ModeLocal, Actor: "admin",
	}); err != nil {
		t.Fatalf("create local repo: %v", err)
	}
	if _, err := repos.Create(ctx, repository.CreateInput{
		ProjectID: proj.ID, Key: "mirror", Name: "Mirror",
		Format: repository.FormatNuGet, Mode: repository.ModeProxy,
		Upstream: &repository.Upstream{URL: "https://api.nuget.org/v3/index.json"}, Actor: "admin",
	}); err != nil {
		t.Fatalf("create proxy repo: %v", err)
	}
	store, err := blob.NewFS(cfg.DataDir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	r := chi.NewRouter()
	r.Route("/nuget", func(sub chi.Router) {
		nuget.New().Mount(sub, registry.Deps{Blobs: store, Auth: authSvc, DB: sqlDB, Repositories: repository.NewService(sqlDB), Log: discardLogger()})
	})
	return &harness{router: r, auth: authSvc, db: sqlDB, blobs: store}
}

func (h *harness) token(t *testing.T) string {
	t.Helper()
	u, err := h.auth.Authenticate(context.Background(), "admin", "password")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	raw, _, err := h.auth.CreateToken(context.Background(), u.ID, "admin", "nuget", 0)
	if err != nil {
		t.Fatalf("CreateToken: %v", err)
	}
	return raw
}

func (h *harness) do(t *testing.T, method, path string, body []byte, apiKey string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	req.Host = "localhost:8094"
	if apiKey != "" {
		req.Header.Set("X-NuGet-ApiKey", apiKey)
	}
	rr := httptest.NewRecorder()
	h.router.ServeHTTP(rr, req)
	return rr
}

// buildNupkg builds a minimal .nupkg (a zip with a root <id>.nuspec).
func buildNupkg(t *testing.T, id, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create(id + ".nuspec")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	nuspec := `<?xml version="1.0"?><package><metadata>` +
		`<id>` + id + `</id><version>` + version + `</version>` +
		`<description>demo</description><authors>platbor</authors>` +
		`<dependencies><group targetFramework="net8.0">` +
		`<dependency id="Newtonsoft.Json" version="13.0.1" /></group></dependencies>` +
		`</metadata></package>`
	if _, err := f.Write([]byte(nuspec)); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestServiceIndexIsAnonymous(t *testing.T) {
	h := newHarness(t)
	rr := h.do(t, http.MethodGet, "/nuget/feed/local/v3/index.json", nil, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("service index: status = %d, want 200", rr.Code)
	}
	var idx struct {
		Version   string
		Resources []struct {
			Type string `json:"@type"`
			ID   string `json:"@id"`
		}
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &idx); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if idx.Version != "3.0.0" {
		t.Errorf("version = %q, want 3.0.0", idx.Version)
	}
	var hasPublish, hasFlat bool
	for _, r := range idx.Resources {
		if r.Type == "PackagePublish/2.0.0" {
			hasPublish = true
		}
		if r.Type == "PackageBaseAddress/3.0.0" {
			hasFlat = true
		}
	}
	if !hasPublish || !hasFlat {
		t.Errorf("service index missing publish/flat resources: %+v", idx.Resources)
	}
}

func TestPushRestoreRoundTrip(t *testing.T) {
	h := newHarness(t)
	key := h.token(t)
	nupkg := buildNupkg(t, "Acme.Widgets", "1.2.3")

	// Push requires auth.
	if rr := h.do(t, http.MethodPut, "/nuget/feed/local/v3/package", nupkg, ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauth push: status = %d, want 401", rr.Code)
	}
	if rr := h.do(t, http.MethodPut, "/nuget/feed/local/v3/package", nupkg, key); rr.Code != http.StatusCreated {
		t.Fatalf("push: status = %d, want 201 (%s)", rr.Code, rr.Body.String())
	}

	// Flat-container lists the version.
	fv := h.do(t, http.MethodGet, "/nuget/feed/local/v3-flatcontainer/acme.widgets/index.json", nil, key)
	if fv.Code != http.StatusOK {
		t.Fatalf("flat versions: status = %d", fv.Code)
	}
	var vers struct {
		Versions []string
	}
	_ = json.Unmarshal(fv.Body.Bytes(), &vers)
	if len(vers.Versions) != 1 || vers.Versions[0] != "1.2.3" {
		t.Errorf("versions = %v, want [1.2.3]", vers.Versions)
	}

	// The nupkg downloads byte-for-byte.
	dl := h.do(t, http.MethodGet, "/nuget/feed/local/v3-flatcontainer/acme.widgets/1.2.3/acme.widgets.1.2.3.nupkg", nil, key)
	if dl.Code != http.StatusOK || !bytes.Equal(dl.Body.Bytes(), nupkg) {
		t.Errorf("download: status=%d match=%v", dl.Code, bytes.Equal(dl.Body.Bytes(), nupkg))
	}

	// Registration carries the catalog entry with @type and the dependency group.
	reg := h.do(t, http.MethodGet, "/nuget/feed/local/v3/registrations/acme.widgets/index.json", nil, key)
	if reg.Code != http.StatusOK {
		t.Fatalf("registration: status = %d", reg.Code)
	}
	var r struct {
		Items []struct {
			Items []struct {
				CatalogEntry struct {
					Type             string `json:"@type"`
					ID               string `json:"id"`
					Version          string `json:"version"`
					DependencyGroups []struct {
						TargetFramework string `json:"targetFramework"`
						Dependencies    []struct {
							ID string `json:"id"`
						} `json:"dependencies"`
					} `json:"dependencyGroups"`
				} `json:"catalogEntry"`
				PackageContent string `json:"packageContent"`
			} `json:"items"`
		} `json:"items"`
	}
	if err := json.Unmarshal(reg.Body.Bytes(), &r); err != nil {
		t.Fatalf("registration decode: %v", err)
	}
	leaf := r.Items[0].Items[0]
	if leaf.CatalogEntry.Type != "PackageDetails" || leaf.CatalogEntry.ID != "Acme.Widgets" || leaf.CatalogEntry.Version != "1.2.3" {
		t.Errorf("catalogEntry = %+v", leaf.CatalogEntry)
	}
	if len(leaf.CatalogEntry.DependencyGroups) != 1 || leaf.CatalogEntry.DependencyGroups[0].Dependencies[0].ID != "Newtonsoft.Json" {
		t.Errorf("dependency groups not surfaced: %+v", leaf.CatalogEntry.DependencyGroups)
	}

	// Re-pushing the same version is a conflict.
	if rr := h.do(t, http.MethodPut, "/nuget/feed/local/v3/package", nupkg, key); rr.Code != http.StatusConflict {
		t.Errorf("re-push: status = %d, want 409", rr.Code)
	}
}

func TestPushToProxyRejected(t *testing.T) {
	h := newHarness(t)
	key := h.token(t)
	rr := h.do(t, http.MethodPut, "/nuget/feed/mirror/v3/package", buildNupkg(t, "X", "1.0.0"), key)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("push to proxy: status = %d, want 403", rr.Code)
	}
}

func TestSearch(t *testing.T) {
	h := newHarness(t)
	key := h.token(t)
	h.do(t, http.MethodPut, "/nuget/feed/local/v3/package", buildNupkg(t, "Acme.Widgets", "1.0.0"), key)
	rr := h.do(t, http.MethodGet, "/nuget/feed/local/v3/search?q=widg", nil, key)
	if rr.Code != http.StatusOK {
		t.Fatalf("search: status = %d", rr.Code)
	}
	var res struct {
		TotalHits int `json:"totalHits"`
		Data      []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res.TotalHits != 1 || res.Data[0].ID != "Acme.Widgets" {
		t.Errorf("search = %+v, want 1 hit Acme.Widgets", res)
	}
}

// TestGCKeepsNupkgs proves the collector marks NuGet package blobs.
func TestGCKeepsNupkgs(t *testing.T) {
	h := newHarness(t)
	key := h.token(t)
	h.do(t, http.MethodPut, "/nuget/feed/local/v3/package", buildNupkg(t, "Acme.Widgets", "1.0.0"), key)

	ctx := context.Background()
	future := time.Now().UTC().Add(48 * time.Hour)
	bare := oci.NewCollector(h.blobs, h.db)
	if rep, err := bare.Collect(ctx, "admin", 0, true, future); err != nil {
		t.Fatalf("bare Collect: %v", err)
	} else if rep.Deleted == 0 {
		t.Fatal("expected the nupkg blob to be sweepable without its referencer")
	}
	guarded := oci.NewCollector(h.blobs, h.db, nuget.NewReferencer(h.db))
	rep, err := guarded.Collect(ctx, "admin", 0, true, future)
	if err != nil {
		t.Fatalf("guarded Collect: %v", err)
	}
	if rep.Deleted != 0 {
		t.Errorf("GC deleted %d blobs; nupkgs must be kept", rep.Deleted)
	}
}
