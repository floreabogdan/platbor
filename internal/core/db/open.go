package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/platbor/platbor/internal/core/config"

	// Registers the CGO-free "sqlite" driver.
	_ "modernc.org/sqlite"
)

// sqlitePragmas are applied on every connection via the DSN. WAL + a busy
// timeout give us concurrent readers with a single writer; foreign_keys must be
// enabled per-connection (SQLite defaults it off).
var sqlitePragmas = []string{
	"_pragma=busy_timeout(5000)",
	"_pragma=journal_mode(WAL)",
	"_pragma=foreign_keys(ON)",
	"_pragma=synchronous(NORMAL)",
}

// Open connects to the configured metadata store, applies pool settings, and
// verifies connectivity. The caller owns Close.
func Open(ctx context.Context, cfg config.Config) (*sql.DB, error) {
	switch cfg.Database.Driver {
	case "sqlite":
		return openSQLite(ctx, cfg)
	case "postgres":
		return nil, fmt.Errorf("database driver %q is configured but not yet implemented", cfg.Database.Driver)
	default:
		return nil, fmt.Errorf("unknown database driver %q", cfg.Database.Driver)
	}
}

func openSQLite(ctx context.Context, cfg config.Config) (*sql.DB, error) {
	dsn := cfg.Database.DSN
	if dsn == "" {
		if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
			return nil, fmt.Errorf("creating data dir %s: %w", cfg.DataDir, err)
		}
		dsn = cfg.SQLitePath() + "?" + strings.Join(sqlitePragmas, "&")
	}

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite: %w", err)
	}

	// SQLite is single-writer. Serializing access to one connection avoids
	// SQLITE_BUSY entirely at Phase-0 concurrency; a read pool can come later
	// if a workload proves it necessary.
	sqlDB.SetMaxOpenConns(1)

	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("pinging sqlite: %w", err)
	}
	return sqlDB, nil
}
