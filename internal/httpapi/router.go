package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/platbor/platbor/internal/core/audit"
	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/repository"
	"github.com/platbor/platbor/internal/core/webhook"
	"github.com/platbor/platbor/internal/registry"
	"github.com/platbor/platbor/internal/registry/cargo"
	"github.com/platbor/platbor/internal/registry/generic"
	"github.com/platbor/platbor/internal/registry/goproxy"
	"github.com/platbor/platbor/internal/registry/maven"
	"github.com/platbor/platbor/internal/registry/npm"
	"github.com/platbor/platbor/internal/registry/nuget"
	"github.com/platbor/platbor/internal/registry/oci"
	"github.com/platbor/platbor/internal/registry/pypi"
	"github.com/platbor/platbor/internal/registry/rubygems"
	"github.com/platbor/platbor/internal/registry/terraform"
	"github.com/platbor/platbor/internal/registry/usage"
)

// newRouter assembles the top-level request tree. Ordering matters: operational
// endpoints and the /api/v1 tree are registered before the SPA fallback so the
// UI's catch-all never shadows a real API route.
func newRouter(log *slog.Logger, assets fs.FS, api API) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(requestLogger(log))
	r.Use(middleware.Recoverer)

	// Operational endpoints — unauthenticated, never cached. Liveness reports the
	// process is up; readiness additionally probes the dependencies an orchestrator
	// should gate traffic on (metadata DB and blob store).
	r.Get("/healthz", health)
	r.Get("/readyz", readyz(api.DB, api.Blobs, log))

	// Terraform service discovery is instance-global (per hostname) and
	// unauthenticated; it must live at the host root, not under a format prefix.
	r.Get("/.well-known/terraform.json", terraform.Discovery())

	// Application/automation API.
	r.Route("/api/v1", func(r chi.Router) {
		// Attach the authenticated user (if any) to every API request.
		r.Use(authenticate(api.Auth, log))
		r.Get("/health", health)

		ah := authHandler{svc: api.Auth, log: log, cookieSecure: api.CookieSecure}
		r.Route("/auth", func(r chi.Router) {
			ah.mountPublic(r) // POST /login — public
			r.Group(func(r chi.Router) {
				r.Use(requireUser)
				ah.mountAuthed(r) // /logout, /me
			})
		})

		// Authenticated routes require a valid session or token.
		r.Group(func(r chi.Router) {
			r.Use(requireUser)
			r.Route("/tokens", tokensHandler{svc: api.Auth, log: log}.mount)
			r.Route("/projects", projectsHandler{svc: api.Projects, auth: api.Auth, usage: usage.New(api.DB), log: log}.mount)
			r.Route("/projects/{project}/repositories", repositoriesHandler{
				repos:    repository.NewService(api.DB),
				projects: api.Projects,
				auth:     api.Auth,
				log:      log,
			}.mount)
			r.Route("/projects/{project}/members", membersHandler{
				auth:     api.Auth,
				projects: api.Projects,
				log:      log,
			}.mount)
			r.Route("/projects/{project}/webhooks", webhooksHandler{
				svc:      webhook.NewService(api.DB),
				projects: api.Projects,
				auth:     api.Auth,
				log:      log,
			}.mount)
			r.Route("/registry", registryHandler{
				browser:   oci.NewBrowser(api.DB),
				packages:  npm.NewBrowser(api.DB),
				nugets:    nuget.NewBrowser(api.DB),
				generics:  generic.NewBrowser(api.DB),
				pypis:     pypi.NewBrowser(api.DB),
				mavens:    maven.NewBrowser(api.DB),
				gomods:    goproxy.NewBrowser(api.DB),
				crates:    cargo.NewBrowser(api.DB),
				gems:      rubygems.NewBrowser(api.DB),
				modules:   terraform.NewBrowser(api.DB),
				manager:   oci.NewManager(api.DB),
				collector: newCollector(api.DB, api.Blobs),
				retention: newRetention(api.DB),
				repos:     repository.NewService(api.DB),
				blobs:     api.Blobs,
				projects:  api.Projects,
				log:       log,
			}.mount)
			r.Route("/dashboard", dashboardHandler{
				projects: api.Projects,
				browser:  oci.NewBrowser(api.DB),
				audit:    audit.NewService(api.DB),
				log:      log,
			}.mount)
		})
	})

	// Format-protocol routes. Each adapter owns its URL prefix and its own
	// protocol-native auth and errors. The repository service the adapters share
	// carries the storage-usage computer so writes enforce per-project quotas.
	adapterRepos := repository.NewService(api.DB)
	adapterRepos.SetUsageFunc(usage.New(api.DB).ProjectUsage)
	deps := registry.Deps{Blobs: api.Blobs, Auth: api.Auth, DB: api.DB, Log: log, Repositories: adapterRepos, EnableOCIBearer: api.EnableOCIBearer}
	r.Route("/v2", func(sub chi.Router) {
		oci.New().Mount(sub, deps)
	})
	r.Route("/npm", func(sub chi.Router) {
		npm.New().Mount(sub, deps)
	})
	r.Route("/generic", func(sub chi.Router) {
		generic.New().Mount(sub, deps)
	})
	r.Route("/nuget", func(sub chi.Router) {
		nuget.New().Mount(sub, deps)
	})
	r.Route("/pypi", func(sub chi.Router) {
		pypi.New().Mount(sub, deps)
	})
	r.Route("/maven", func(sub chi.Router) {
		maven.New().Mount(sub, deps)
	})
	r.Route("/go", func(sub chi.Router) {
		goproxy.New().Mount(sub, deps)
	})
	r.Route("/cargo", func(sub chi.Router) {
		cargo.New().Mount(sub, deps)
	})
	r.Route("/rubygems", func(sub chi.Router) {
		rubygems.New().Mount(sub, deps)
	})
	r.Route("/terraform", func(sub chi.Router) {
		terraform.New().Mount(sub, deps)
	})

	// Everything else falls through to the embedded SPA.
	r.Handle("/*", spaHandler(assets))

	return r
}

// health is the liveness probe: it answers as long as the process can serve, so
// an orchestrator only restarts a truly hung instance. It deliberately checks no
// dependencies — a transient DB blip should not trigger a restart loop.
func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// readyz is the readiness probe: it reports whether the instance can actually
// serve requests by probing its dependencies — the metadata database and the
// blob store. On any failure it returns 503 with a per-check breakdown so an
// orchestrator stops routing traffic here until it recovers. The checks are
// bounded by a short timeout so a hung dependency cannot hang the probe.
func readyz(db *sql.DB, blobs blob.Store, log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		checks := map[string]string{}
		ready := true

		if err := db.PingContext(ctx); err != nil {
			checks["database"] = "unavailable"
			ready = false
			log.Warn("readiness: database unreachable", slog.String("error", err.Error()))
		} else {
			checks["database"] = "ok"
		}

		// The blob store is probed only when it advertises a health check; a driver
		// without one is treated as always reachable.
		if hc, ok := blobs.(blob.HealthChecker); ok {
			if err := hc.HealthCheck(ctx); err != nil {
				checks["blobStore"] = "unavailable"
				ready = false
				log.Warn("readiness: blob store unreachable", slog.String("error", err.Error()))
			} else {
				checks["blobStore"] = "ok"
			}
		} else {
			checks["blobStore"] = "ok"
		}

		status := http.StatusOK
		state := "ready"
		if !ready {
			status = http.StatusServiceUnavailable
			state = "unready"
		}
		writeJSON(w, log, status, map[string]any{"status": state, "checks": checks})
	}
}

// spaHandler serves the embedded single-page app: static files when they exist,
// falling back to index.html so client-side routes resolve on deep links and
// refreshes.
func spaHandler(assets fs.FS) http.HandlerFunc {
	fileServer := http.FileServer(http.FS(assets))

	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/" {
			serveIndex(w, assets)
			return
		}

		// A concrete asset (has an extension and exists) is served directly;
		// anything else is a client-side route and gets index.html.
		if _, err := fs.Stat(assets, cleanAssetPath(path)); errors.Is(err, fs.ErrNotExist) {
			serveIndex(w, assets)
			return
		}
		fileServer.ServeHTTP(w, r)
	}
}

// cleanAssetPath maps a request path to an fs.FS key (no leading slash, "." for
// root), which is what fs.Stat expects.
func cleanAssetPath(p string) string {
	trimmed := p
	if len(trimmed) > 0 && trimmed[0] == '/' {
		trimmed = trimmed[1:]
	}
	if trimmed == "" {
		return "."
	}
	return trimmed
}

// serveIndex writes index.html with no-cache so a freshly deployed SPA is picked
// up immediately, while hashed assets under /assets remain cacheable.
func serveIndex(w http.ResponseWriter, assets fs.FS) {
	index, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.Error(w, "UI not built", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(index)
}
