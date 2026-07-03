// Package httpapi wires Platbor's HTTP surface: operational probes, the
// /api/v1 application API, format-protocol routes (added as adapters land),
// and the embedded SPA. Handlers here stay thin — they decode, validate, call
// a service, and encode — per docs/CODING-STANDARDS.md.
package httpapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/project"
)

// API bundles the application services the HTTP layer depends on, plus the few
// HTTP-facing settings it needs. It grows one field per domain as features land.
type API struct {
	Auth     *auth.Service
	Projects *project.Service
	Blobs    blob.Store
	// DB is the shared metadata store, handed to format adapters that persist
	// their own project-scoped tables (e.g. OCI manifests/tags).
	DB *sql.DB
	// CookieSecure sets the Secure flag on the session cookie.
	CookieSecure bool
	// EnableOCIBearer turns on the OCI bearer-token auth flow (see config.OCIBearer).
	EnableOCIBearer bool
}

// Server owns the HTTP listener and its graceful-shutdown lifecycle.
type Server struct {
	http     *http.Server
	log      *slog.Logger
	shutdown time.Duration
}

// NewServer builds the server from resolved config, a logger, the embedded UI
// assets, and the application services. It does not begin listening; call Run.
func NewServer(cfg config.Config, log *slog.Logger, assets fs.FS, api API) *Server {
	return &Server{
		http: &http.Server{
			Addr:              cfg.Addr,
			Handler:           newRouter(log, assets, api),
			ReadHeaderTimeout: 10 * time.Second,
		},
		log:      log,
		shutdown: cfg.ShutdownTimeout,
	}
}

// Run serves until ctx is cancelled, then drains in-flight requests within the
// configured shutdown timeout. It returns nil on a clean shutdown.
func (s *Server) Run(ctx context.Context) error {
	errc := make(chan error, 1)
	go func() {
		s.log.Info("http server listening", slog.String("addr", s.http.Addr))
		if err := s.http.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- fmt.Errorf("http server: %w", err)
			return
		}
		errc <- nil
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		s.log.Info("shutdown signal received, draining", slog.Duration("timeout", s.shutdown))
		return s.drain()
	}
}

// drain gives in-flight requests a bounded window to finish before the process
// exits.
func (s *Server) drain() error {
	ctx, cancel := context.WithTimeout(context.Background(), s.shutdown)
	defer cancel()

	if err := s.http.Shutdown(ctx); err != nil {
		return fmt.Errorf("draining http server: %w", err)
	}
	s.log.Info("http server stopped cleanly")
	return nil
}
