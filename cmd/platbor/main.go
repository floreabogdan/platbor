// Command platbor is the single-binary Platbor server: artifact registry plus
// software catalog. It loads configuration, wires the HTTP surface, and serves
// until interrupted.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/httpapi"
	"github.com/platbor/platbor/web"
)

func main() {
	if err := run(); err != nil {
		// Logger may not exist yet on early failures, so report to stderr too.
		fmt.Fprintln(os.Stderr, "platbor:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "", "path to a YAML config file (optional; env and defaults apply otherwise)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}

	log, err := newLogger(cfg.Log)
	if err != nil {
		return err
	}
	slog.SetDefault(log)

	assets, err := web.Assets()
	if err != nil {
		return fmt.Errorf("loading embedded UI: %w", err)
	}

	// Cancel the root context on SIGINT/SIGTERM so Run drains gracefully.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("starting platbor", slog.String("addr", cfg.Addr), slog.String("dataDir", cfg.DataDir))
	return httpapi.NewServer(cfg, log, assets).Run(ctx)
}

// newLogger builds the slog handler from config: text for humans, json for log
// pipelines, at the configured level.
func newLogger(cfg config.LogConfig) (*slog.Logger, error) {
	level, err := parseLevel(cfg.Level)
	if err != nil {
		return nil, err
	}
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch cfg.Format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default: // "text" — config.validate guarantees one of the two.
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler), nil
}

func parseLevel(level string) (slog.Level, error) {
	switch level {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q", level)
	}
}
