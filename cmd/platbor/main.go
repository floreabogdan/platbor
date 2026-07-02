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

	"github.com/platbor/platbor/internal/core/auth"
	"github.com/platbor/platbor/internal/core/blob"
	"github.com/platbor/platbor/internal/core/config"
	"github.com/platbor/platbor/internal/core/db"
	"github.com/platbor/platbor/internal/core/project"
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

	sqlDB, err := db.Open(ctx, cfg)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := db.Migrate(ctx, sqlDB, log); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	authSvc := auth.NewService(sqlDB)
	if err := bootstrapAdmin(ctx, authSvc, cfg.Auth, log); err != nil {
		return err
	}

	blobStore, err := blob.NewFS(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("initializing blob store: %w", err)
	}

	api := httpapi.API{
		Auth:         authSvc,
		Projects:     project.NewService(sqlDB),
		Blobs:        blobStore,
		DB:           sqlDB,
		CookieSecure: cfg.Auth.CookieSecure,
	}

	log.Info("starting platbor", slog.String("addr", cfg.Addr), slog.String("dataDir", cfg.DataDir))
	return httpapi.NewServer(cfg, log, assets, api).Run(ctx)
}

// bootstrapAdmin creates the instance admin on first run and, when it generated
// the password, prints it prominently so the operator can log in.
func bootstrapAdmin(ctx context.Context, authSvc *auth.Service, cfg config.AuthConfig, log *slog.Logger) error {
	res, err := authSvc.Bootstrap(ctx, cfg.AdminUsername, cfg.AdminPassword)
	if err != nil {
		return fmt.Errorf("bootstrapping admin: %w", err)
	}
	if !res.Created {
		return nil
	}
	if res.GeneratedPassword != "" {
		log.Warn(
			"created instance admin with a generated password — change it after logging in",
			slog.String("username", res.Username),
			slog.String("password", res.GeneratedPassword),
		)
	} else {
		log.Info("created instance admin", slog.String("username", res.Username))
	}
	return nil
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
