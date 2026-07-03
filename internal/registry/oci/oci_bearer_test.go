package oci_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/project"
	"github.com/platbor/platbor/internal/core/repository"
	"github.com/platbor/platbor/internal/registry"
	"github.com/platbor/platbor/internal/registry/oci"
)

// newBearerRouter builds a /v2 mount with the bearer flow enabled.
func newBearerRouter(t *testing.T) http.Handler {
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
	if _, err := authSvc.Bootstrap(ctx, "admin", "password"); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if _, err := project.NewService(sqlDB).Create(ctx, project.CreateInput{Key: "library", Name: "Library", AllowAutoCreate: true, Actor: "admin"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	store, err := blob.NewFS(cfg.DataDir)
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	r := chi.NewRouter()
	r.Route("/v2", func(sub chi.Router) {
		oci.New().Mount(sub, registry.Deps{
			Blobs: store, Auth: authSvc, DB: sqlDB, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
			Repositories: repository.NewService(sqlDB), EnableOCIBearer: true,
		})
	})
	return r
}

func TestOCIBearerFlow(t *testing.T) {
	router := newBearerRouter(t)
	do := func(method, path string, setAuth func(*http.Request)) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, http.NoBody)
		req.Host = "localhost:8080"
		if setAuth != nil {
			setAuth(req)
		}
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		return rr
	}

	// 1. An unauthenticated /v2/ is challenged toward the token endpoint.
	chal := do(http.MethodGet, "/v2/", nil)
	if chal.Code != http.StatusUnauthorized {
		t.Fatalf("unauth /v2/: %d, want 401", chal.Code)
	}
	www := chal.Header().Get("WWW-Authenticate")
	if !strings.HasPrefix(www, "Bearer ") || !strings.Contains(www, "/v2/token") {
		t.Fatalf("challenge = %q, want a Bearer token-endpoint pointer", www)
	}

	// 2. The token endpoint issues a token for Basic credentials.
	tok := do(http.MethodGet, "/v2/token?service=localhost:8080&scope=repository:library/app:pull", func(req *http.Request) {
		req.SetBasicAuth("admin", "password")
	})
	if tok.Code != http.StatusOK {
		t.Fatalf("token endpoint: %d (%s)", tok.Code, tok.Body.String())
	}
	var out struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := json.Unmarshal(tok.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode token: %v", err)
	}
	if out.Token == "" || out.ExpiresIn <= 0 {
		t.Fatalf("token response = %+v", out)
	}

	// 3. The issued token authenticates a /v2/ request.
	ok := do(http.MethodGet, "/v2/", func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer "+out.Token)
	})
	if ok.Code != http.StatusOK {
		t.Fatalf("bearer /v2/: %d, want 200", ok.Code)
	}

	// 4. A garbage bearer token is rejected.
	bad := do(http.MethodGet, "/v2/", func(req *http.Request) {
		req.Header.Set("Authorization", "Bearer not-a-real-token")
	})
	if bad.Code != http.StatusUnauthorized {
		t.Errorf("garbage bearer: %d, want 401", bad.Code)
	}

	// 5. HTTP Basic still works even with the bearer flow enabled.
	basic := do(http.MethodGet, "/v2/", func(req *http.Request) {
		req.SetBasicAuth("admin", "password")
	})
	if basic.Code != http.StatusOK {
		t.Errorf("basic /v2/ with bearer enabled: %d, want 200", basic.Code)
	}
}
