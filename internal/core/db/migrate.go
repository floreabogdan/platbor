package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.up.sql
var migrationsFS embed.FS

// migration is one forward schema step, ordered by version.
type migration struct {
	version int
	name    string
	sql     string
}

// Migrate applies every pending up-migration in version order, each inside its
// own transaction, recording applied versions in schema_migrations. It is
// idempotent: re-running with no new migrations is a no-op.
func Migrate(ctx context.Context, sqlDB *sql.DB, log *slog.Logger) error {
	if err := ensureMigrationsTable(ctx, sqlDB); err != nil {
		return err
	}
	applied, err := appliedVersions(ctx, sqlDB)
	if err != nil {
		return err
	}
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	pending := 0
	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := applyMigration(ctx, sqlDB, m); err != nil {
			return fmt.Errorf("applying migration %04d_%s: %w", m.version, m.name, err)
		}
		log.Info("applied migration", slog.Int("version", m.version), slog.String("name", m.name))
		pending++
	}
	if pending == 0 {
		log.Debug("database schema up to date")
	}
	return nil
}

func ensureMigrationsTable(ctx context.Context, sqlDB *sql.DB) error {
	const q = `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT NOT NULL,
		applied_at TEXT NOT NULL
	)`
	if _, err := sqlDB.ExecContext(ctx, q); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}
	return nil
}

func appliedVersions(ctx context.Context, sqlDB *sql.DB) (map[int]bool, error) {
	rows, err := sqlDB.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("reading applied migrations: %w", err)
	}
	defer func() { _ = rows.Close() }()

	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scanning applied migration: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating applied migrations: %w", err)
	}
	return applied, nil
}

func applyMigration(ctx context.Context, sqlDB *sql.DB, m migration) error {
	tx, err := sqlDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	if _, err := tx.ExecContext(ctx, m.sql); err != nil {
		return fmt.Errorf("exec: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
		m.version, m.name, time.Now().UTC().Format(time.RFC3339),
	); err != nil {
		return fmt.Errorf("recording version: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// loadMigrations reads and parses the embedded up-migrations, sorted ascending
// by version.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("reading embedded migrations: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".up.sql") {
			continue
		}
		version, name, err := parseMigrationName(e.Name())
		if err != nil {
			return nil, err
		}
		content, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			return nil, fmt.Errorf("reading migration %s: %w", e.Name(), err)
		}
		migrations = append(migrations, migration{version: version, name: name, sql: string(content)})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return migrations, nil
}

// parseMigrationName turns "0001_init.up.sql" into (1, "init").
func parseMigrationName(filename string) (int, string, error) {
	base := strings.TrimSuffix(filename, ".up.sql")
	prefix, name, found := strings.Cut(base, "_")
	if !found {
		return 0, "", fmt.Errorf("migration %q must be named NNNN_name.up.sql", filename)
	}
	version, err := strconv.Atoi(prefix)
	if err != nil {
		return 0, "", fmt.Errorf("migration %q has a non-numeric version: %w", filename, err)
	}
	return version, name, nil
}
