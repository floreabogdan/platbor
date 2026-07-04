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

// uploadSweep bounds how often stale upload sessions are reclaimed and how long
// an abandoned session is spared. The grace window is generous so a slow but live
// resumable upload (a large layer pushed over a flaky link) is never swept.
const (
	uploadSweepInterval = time.Hour
	uploadSweepGrace    = 24 * time.Hour
)

// Server owns the HTTP listener and its graceful-shutdown lifecycle, plus the
// background maintenance the instance runs while serving.
type Server struct {
	http     *http.Server
	log      *slog.Logger
	blobs    blob.Store
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
		blobs:    api.Blobs,
		shutdown: cfg.ShutdownTimeout,
	}
}

// Run serves until ctx is cancelled, then drains in-flight requests within the
// configured shutdown timeout. It returns nil on a clean shutdown.
func (s *Server) Run(ctx context.Context) error {
	go s.sweepUploads(ctx)

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

// sweepUploads periodically reclaims abandoned resumable-upload staging files
// (a client that starts an upload but never commits or aborts). It runs once
// shortly after boot and then on an interval, and exits when ctx is cancelled. A
// blob store that does not support sweeping is a no-op.
func (s *Server) sweepUploads(ctx context.Context) {
	sweeper, ok := s.blobs.(blob.UploadSweeper)
	if !ok {
		return
	}
	sweep := func() {
		removed, err := sweeper.SweepUploads(ctx, time.Now().Add(-uploadSweepGrace))
		if err != nil {
			s.log.Warn("sweeping stale uploads", slog.String("error", err.Error()))
			return
		}
		if removed > 0 {
			s.log.Info("swept stale upload sessions", slog.Int("removed", removed))
		}
	}

	sweep()
	t := time.NewTicker(uploadSweepInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sweep()
		}
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
