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

func newTestHandler(t *testing.T) http.Handler {
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

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return newRouter(log, emptyAssets(t), API{Projects: project.NewService(sqlDB)})
}

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestCreateProjectEndpoint(t *testing.T) {
	h := newTestHandler(t)

	rr := post(t, h, "/api/v1/projects", `{"key":"acme","name":"Acme Corp"}`)
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

func TestCreateProjectDuplicateReturns409(t *testing.T) {
	h := newTestHandler(t)
	_ = post(t, h, "/api/v1/projects", `{"key":"dup","name":"First"}`)

	rr := post(t, h, "/api/v1/projects", `{"key":"dup","name":"Second"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("Content-Type = %q, want application/problem+json", ct)
	}
}

func TestCreateProjectValidationReturns400(t *testing.T) {
	h := newTestHandler(t)

	for _, body := range []string{
		`{"key":"BadCase","name":"x"}`,
		`{"key":"acme","name":""}`,
		`{"key":"a/b","name":"x"}`,
		`not json`,
		`{"key":"acme","name":"x","bogus":true}`,
	} {
		rr := post(t, h, "/api/v1/projects", body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, rr.Code)
		}
	}
}

func TestListProjectsEndpoint(t *testing.T) {
	h := newTestHandler(t)
	_ = post(t, h, "/api/v1/projects", `{"key":"acme","name":"Acme"}`)
	_ = post(t, h, "/api/v1/projects", `{"key":"beta","name":"Beta"}`)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/projects?limit=10", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

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
