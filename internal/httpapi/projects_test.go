package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/project"
)

// emptyAssets is a minimal SPA filesystem so the router's fallback handler has
// an index.html to serve; these tests exercise the API, not the UI.
func emptyAssets(t *testing.T) fs.FS {
	t.Helper()
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>test</title>")},
	}
}

// testAPI is a router wired to a fresh migrated DB, plus a session cookie for a
// bootstrapped admin so authenticated endpoints can be exercised.
type testAPI struct {
	handler http.Handler
	cookie  *http.Cookie
}

func newTestAPI(t *testing.T) testAPI {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	ctx := context.Background()

	sqlDB, err := db.Open(ctx, cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(ctx, sqlDB, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	authSvc := auth.NewService(sqlDB)
	if _, err := authSvc.Bootstrap(ctx, "admin", "password123"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	user, err := authSvc.Authenticate(ctx, "admin", "password123")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	token, _, err := authSvc.StartSession(ctx, user.ID)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := newRouter(log, emptyAssets(t), API{Auth: authSvc, Projects: project.NewService(sqlDB)})
	return testAPI{handler: handler, cookie: &http.Cookie{Name: sessionCookieName, Value: token}}
}

func (a testAPI) do(t *testing.T, method, path, body string, authed bool) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	if authed {
		req.AddCookie(a.cookie)
	}
	rr := httptest.NewRecorder()
	a.handler.ServeHTTP(rr, req)
	return rr
}

func (a testAPI) post(t *testing.T, path, body string) *httptest.ResponseRecorder {
	return a.do(t, http.MethodPost, path, body, true)
}

func (a testAPI) get(t *testing.T, path string) *httptest.ResponseRecorder {
	return a.do(t, http.MethodGet, path, "", true)
}

func TestCreateProjectEndpoint(t *testing.T) {
	a := newTestAPI(t)

	rr := a.post(t, "/api/v1/projects", `{"key":"acme","name":"Acme Corp"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	if loc := rr.Header().Get("Location"); loc != "/api/v1/projects/acme" {
		t.Errorf("Location = %q, want /api/v1/projects/acme", loc)
	}

	var got projectResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if got.Key != "acme" || got.ID == "" {
		t.Errorf("unexpected body: %+v", got)
	}
}

func TestCreateProjectRequiresAuth(t *testing.T) {
	a := newTestAPI(t)
	rr := a.do(t, http.MethodPost, "/api/v1/projects", `{"key":"acme","name":"Acme"}`, false)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create: status = %d, want 401", rr.Code)
	}
}

func TestCreateProjectDuplicateReturns409(t *testing.T) {
	a := newTestAPI(t)
	_ = a.post(t, "/api/v1/projects", `{"key":"dup","name":"First"}`)

	rr := a.post(t, "/api/v1/projects", `{"key":"dup","name":"Second"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
}

func TestCreateProjectValidationReturns400(t *testing.T) {
	a := newTestAPI(t)

	for _, body := range []string{
		`{"key":"BadCase","name":"x"}`,
		`{"key":"acme","name":""}`,
		`{"key":"a/b","name":"x"}`,
		`not json`,
		`{"key":"acme","name":"x","bogus":true}`,
	} {
		rr := a.post(t, "/api/v1/projects", body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, rr.Code)
		}
	}
}

func TestListProjectsEndpoint(t *testing.T) {
	a := newTestAPI(t)
	_ = a.post(t, "/api/v1/projects", `{"key":"acme","name":"Acme"}`)
	_ = a.post(t, "/api/v1/projects", `{"key":"beta","name":"Beta"}`)

	rr := a.get(t, "/api/v1/projects?limit=10")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got listProjectsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding body: %v", err)
	}
	if len(got.Projects) != 2 {
		t.Fatalf("got %d projects, want 2", len(got.Projects))
	}
	if got.Projects[0].Key != "acme" || got.Projects[1].Key != "beta" {
		t.Errorf("unexpected order: %q, %q", got.Projects[0].Key, got.Projects[1].Key)
	}
}
