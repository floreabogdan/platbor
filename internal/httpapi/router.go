package httpapi

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/platbor/platbor/internal/core/audit"
	"github.com/platbor/platbor/internal/registry"
	"github.com/platbor/platbor/internal/registry/generic"
	"github.com/platbor/platbor/internal/registry/npm"
	"github.com/platbor/platbor/internal/registry/nuget"
	"github.com/platbor/platbor/internal/registry/oci"
)

// newRouter assembles the top-level request tree. Ordering matters: operational
// endpoints and the /api/v1 tree are registered before the SPA fallback so the
// UI's catch-all never shadows a real API route.
func newRouter(log *slog.Logger, assets fs.FS, api API) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(requestLogger(log))
	r.Use(middleware.Recoverer)

	// Operational endpoints — unauthenticated, never cached.
	r.Get("/healthz", health)
	r.Get("/readyz", health)

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
			r.Route("/projects", projectsHandler{svc: api.Projects, log: log}.mount)
			r.Route("/registry", registryHandler{
				browser:   oci.NewBrowser(api.DB),
				packages:  npm.NewBrowser(api.DB),
				nugets:    nuget.NewBrowser(api.DB),
				generics:  generic.NewBrowser(api.DB),
				manager:   oci.NewManager(api.DB),
				collector: oci.NewCollector(api.Blobs, api.DB, npm.NewReferencer(api.DB), generic.NewReferencer(api.DB), nuget.NewReferencer(api.DB)),
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
	// protocol-native auth and errors.
	deps := registry.Deps{Blobs: api.Blobs, Auth: api.Auth, DB: api.DB, Log: log}
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

	// Everything else falls through to the embedded SPA.
	r.Handle("/*", spaHandler(assets))

	return r
}

// health is the liveness/readiness probe. It reports nothing but reachability
// today; readiness gains real dependency checks (DB, blob store) as those land.
func health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
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
