// Package httpapi wires Platbor's HTTP surface: operational probes, the
// /api/v1 application API, format-protocol routes (added as adapters land),
// and the embedded SPA. Handlers here stay thin — they decode, validate, call
// a service, and encode — per docs/CODING-STANDARDS.md.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"time"

	"github.com/platbor/platbor/internal/core/config"
)

// Server owns the HTTP listener and its graceful-shutdown lifecycle.
type Server struct {
	http     *http.Server
	log      *slog.Logger
	shutdown time.Duration
}

// NewServer builds the server from resolved config, a logger, and the embedded
// UI assets. It does not begin listening; call Run for that.
func NewServer(cfg config.Config, log *slog.Logger, assets fs.FS) *Server {
	return &Server{
		http: &http.Server{
			Addr:              cfg.Addr,
			Handler:           newRouter(log, assets),
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
