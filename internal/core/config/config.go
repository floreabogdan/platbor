// Package config loads and validates Platbor's runtime configuration.
//
// Precedence, lowest to highest: built-in defaults, an optional YAML file,
// then environment variables (PLATBOR_*). Zero config must yield a working
// single-binary instance — that promise is enforced by Default().
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// envPrefix namespaces every environment override so Platbor never collides
// with unrelated variables in a shared shell.
const envPrefix = "PLATBOR_"

// Config is the fully-resolved configuration for one running instance.
type Config struct {
	// Addr is the host:port the HTTP server listens on.
	Addr string `yaml:"addr"`
	// DataDir holds the SQLite database and the filesystem blob store.
	DataDir string `yaml:"dataDir"`
	// Database configures the metadata store.
	Database DatabaseConfig `yaml:"database"`
	// Blob configures the content-addressable blob store.
	Blob BlobConfig `yaml:"blob"`
	// Maintenance configures optional scheduled background jobs.
	Maintenance MaintenanceConfig `yaml:"maintenance"`
	// Auth configures identity and sessions.
	Auth AuthConfig `yaml:"auth"`
	// Log configures structured logging.
	Log LogConfig `yaml:"log"`
	// ShutdownTimeout bounds how long graceful shutdown waits for in-flight
	// requests before forcing termination.
	ShutdownTimeout time.Duration `yaml:"shutdownTimeout"`
}

// DatabaseConfig selects and locates the metadata store.
type DatabaseConfig struct {
	// Driver is sqlite (default, zero-config) or postgres.
	Driver string `yaml:"driver"`
	// DSN is the data source name. When empty, sqlite uses
	// {DataDir}/platbor.db; postgres requires an explicit DSN.
	DSN string `yaml:"dsn"`
}

// BlobConfig selects and configures the content-addressable blob store.
type BlobConfig struct {
	// Driver is fs (default, zero-config — blobs under {DataDir}/blobs) or s3
	// (S3-compatible object storage: AWS S3, MinIO, R2, ...). Large artifacts
	// (container images, ML models) live better in object storage.
	Driver string `yaml:"driver"`
	// S3 configures the s3 driver; ignored for fs.
	S3 S3Config `yaml:"s3"`
}

// S3Config configures the S3-compatible blob driver. In-progress uploads are
// always staged locally under {DataDir}/uploads; only finished blobs go to the
// bucket.
type S3Config struct {
	// Endpoint is host[:port] of the S3 API (s3.amazonaws.com, localhost:9000, ...).
	Endpoint string `yaml:"endpoint"`
	// Bucket holds the blobs; created on first run if absent and permitted.
	Bucket string `yaml:"bucket"`
	// Region is the bucket region (optional for MinIO).
	Region string `yaml:"region"`
	// AccessKeyID / SecretAccessKey authenticate to the endpoint. When both are
	// empty, ambient credentials (env, IAM role) are used.
	AccessKeyID     string `yaml:"accessKeyId"`
	SecretAccessKey string `yaml:"secretAccessKey"`
	// UseSSL selects HTTPS to the endpoint.
	UseSSL bool `yaml:"useSSL"`
	// Prefix is an optional key prefix within the bucket.
	Prefix string `yaml:"prefix"`
}

// MaintenanceConfig schedules the housekeeping an instance can run for itself.
// Both jobs are off by default (zero-config stays hands-off) and remain available
// on demand through the admin API regardless. When several instances share one
// backend, enable these on a single instance to avoid redundant concurrent runs.
type MaintenanceConfig struct {
	// GCInterval schedules automatic garbage collection (unreferenced-blob sweep).
	// Zero disables it. Must be at least a minute when set.
	GCInterval time.Duration `yaml:"gcInterval"`
	// RetentionInterval schedules automatic retention runs (keep-last-N pruning).
	// Zero disables it. Must be at least a minute when set.
	RetentionInterval time.Duration `yaml:"retentionInterval"`
}

// AuthConfig configures the identity layer.
type AuthConfig struct {
	// AdminUsername is the instance admin created on first run (empty → "admin").
	AdminUsername string `yaml:"adminUsername"`
	// AdminPassword sets the first-run admin password. When empty, a random one
	// is generated and logged once at startup.
	AdminPassword string `yaml:"adminPassword"`
	// CookieSecure sets the Secure flag on the session cookie. Enable when
	// serving over HTTPS (directly or behind a TLS-terminating proxy).
	CookieSecure bool `yaml:"cookieSecure"`
	// OCIBearer enables the OCI bearer-token flow: /v2 challenges clients with a
	// token endpoint (/v2/token) that issues short-lived scoped tokens, instead
	// of HTTP Basic. Off by default; HTTP Basic (password or PAT) keeps working
	// either way.
	OCIBearer bool `yaml:"ociBearer"`
}

// LogConfig configures the slog handler.
type LogConfig struct {
	// Level is one of debug, info, warn, error.
	Level string `yaml:"level"`
	// Format is text (human) or json (machine).
	Format string `yaml:"format"`
}

// Default returns the zero-config configuration: a working instance that
// stores everything under ./platbor-data and listens on :8080.
func Default() Config {
	return Config{
		Addr:    ":8080",
		DataDir: "platbor-data",
		Database: DatabaseConfig{
			Driver: "sqlite",
			DSN:    "",
		},
		Blob: BlobConfig{
			Driver: "fs",
		},
		Auth: AuthConfig{
			AdminUsername: "admin",
			AdminPassword: "",
			CookieSecure:  false,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "text",
		},
		ShutdownTimeout: 15 * time.Second,
	}
}

// Load resolves configuration from defaults, the optional YAML file at path
// (skipped when path is empty or the file is absent), and PLATBOR_* env vars,
// in that order of increasing precedence. The result is validated before it
// is returned.
func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		if err := applyFile(&cfg, path); err != nil {
			return Config{}, err
		}
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	if err := cfg.validate(); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}
	return cfg, nil
}

// applyFile overlays a YAML file onto cfg. A missing file is not an error —
// zero-config is a supported mode — but an unreadable or malformed file is.
func applyFile(cfg *Config, path string) error {
	// The config path is supplied by the operator via --config or a file we
	// resolved ourselves; reading it is the intended behavior.
	data, err := os.ReadFile(path) //nolint:gosec // operator-supplied config path
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("reading config file %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("parsing config file %s: %w", path, err)
	}
	return nil
}

// applyEnv overlays PLATBOR_* environment variables. Only the keys a user is
// likely to set from a container runtime are exposed here; the file covers the
// long tail.
func applyEnv(cfg *Config) error {
	if v, ok := lookup("ADDR"); ok {
		cfg.Addr = v
	}
	if v, ok := lookup("DATA_DIR"); ok {
		cfg.DataDir = v
	}
	if v, ok := lookup("DB_DRIVER"); ok {
		cfg.Database.Driver = v
	}
	if v, ok := lookup("DB_DSN"); ok {
		cfg.Database.DSN = v
	}
	if v, ok := lookup("BLOB_DRIVER"); ok {
		cfg.Blob.Driver = v
	}
	if v, ok := lookup("S3_ENDPOINT"); ok {
		cfg.Blob.S3.Endpoint = v
	}
	if v, ok := lookup("S3_BUCKET"); ok {
		cfg.Blob.S3.Bucket = v
	}
	if v, ok := lookup("S3_REGION"); ok {
		cfg.Blob.S3.Region = v
	}
	if v, ok := lookup("S3_ACCESS_KEY_ID"); ok {
		cfg.Blob.S3.AccessKeyID = v
	}
	if v, ok := lookup("S3_SECRET_ACCESS_KEY"); ok {
		cfg.Blob.S3.SecretAccessKey = v
	}
	if v, ok := lookup("S3_USE_SSL"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parsing %sS3_USE_SSL %q: %w", envPrefix, v, err)
		}
		cfg.Blob.S3.UseSSL = b
	}
	if v, ok := lookup("S3_PREFIX"); ok {
		cfg.Blob.S3.Prefix = v
	}
	if v, ok := lookup("GC_INTERVAL"); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("parsing %sGC_INTERVAL %q: %w", envPrefix, v, err)
		}
		cfg.Maintenance.GCInterval = d
	}
	if v, ok := lookup("RETENTION_INTERVAL"); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("parsing %sRETENTION_INTERVAL %q: %w", envPrefix, v, err)
		}
		cfg.Maintenance.RetentionInterval = d
	}
	if v, ok := lookup("ADMIN_USERNAME"); ok {
		cfg.Auth.AdminUsername = v
	}
	if v, ok := lookup("ADMIN_PASSWORD"); ok {
		cfg.Auth.AdminPassword = v
	}
	if v, ok := lookup("COOKIE_SECURE"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parsing %sCOOKIE_SECURE %q: %w", envPrefix, v, err)
		}
		cfg.Auth.CookieSecure = b
	}
	if v, ok := lookup("OCI_BEARER"); ok {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("parsing %sOCI_BEARER %q: %w", envPrefix, v, err)
		}
		cfg.Auth.OCIBearer = b
	}
	if v, ok := lookup("LOG_LEVEL"); ok {
		cfg.Log.Level = v
	}
	if v, ok := lookup("LOG_FORMAT"); ok {
		cfg.Log.Format = v
	}
	if v, ok := lookup("SHUTDOWN_TIMEOUT"); ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("parsing %sSHUTDOWN_TIMEOUT %q: %w", envPrefix, v, err)
		}
		cfg.ShutdownTimeout = d
	}
	return nil
}

// lookup reads a namespaced env var, treating an empty value as unset so an
// accidental `PLATBOR_ADDR=` does not blank out a real default.
func lookup(key string) (string, bool) {
	v, ok := os.LookupEnv(envPrefix + key)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

var (
	validLevels      = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	validFormats     = map[string]bool{"text": true, "json": true}
	validDrivers     = map[string]bool{"sqlite": true, "postgres": true}
	validBlobDrivers = map[string]bool{"fs": true, "s3": true}
)

// validate rejects any configuration that would fail confusingly at runtime,
// so misconfiguration surfaces at boot with a clear message.
func (c Config) validate() error {
	if c.Addr == "" {
		return errors.New("addr must not be empty")
	}
	if _, err := parsePort(c.Addr); err != nil {
		return err
	}
	if c.DataDir == "" {
		return errors.New("dataDir must not be empty")
	}
	if !validDrivers[c.Database.Driver] {
		return fmt.Errorf("database.driver %q must be one of sqlite, postgres", c.Database.Driver)
	}
	if c.Database.Driver == "postgres" && c.Database.DSN == "" {
		return errors.New("database.dsn is required when database.driver is postgres")
	}
	if !validBlobDrivers[c.Blob.Driver] {
		return fmt.Errorf("blob.driver %q must be one of fs, s3", c.Blob.Driver)
	}
	if c.Blob.Driver == "s3" {
		if c.Blob.S3.Endpoint == "" || c.Blob.S3.Bucket == "" {
			return errors.New("blob.s3.endpoint and blob.s3.bucket are required when blob.driver is s3")
		}
	}
	if err := validateInterval("maintenance.gcInterval", c.Maintenance.GCInterval); err != nil {
		return err
	}
	if err := validateInterval("maintenance.retentionInterval", c.Maintenance.RetentionInterval); err != nil {
		return err
	}
	if !validLevels[c.Log.Level] {
		return fmt.Errorf("log.level %q must be one of debug, info, warn, error", c.Log.Level)
	}
	if !validFormats[c.Log.Format] {
		return fmt.Errorf("log.format %q must be one of text, json", c.Log.Format)
	}
	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdownTimeout %s must be positive", c.ShutdownTimeout)
	}
	return nil
}

// validateInterval rejects a negative schedule and a positive-but-too-frequent
// one (a sub-minute maintenance interval is almost always a mistake). Zero means
// the job is disabled.
func validateInterval(name string, d time.Duration) error {
	if d < 0 {
		return fmt.Errorf("%s %s must not be negative", name, d)
	}
	if d > 0 && d < time.Minute {
		return fmt.Errorf("%s %s must be at least 1m when set", name, d)
	}
	return nil
}

// parsePort validates the port portion of a host:port listen address.
func parsePort(addr string) (int, error) {
	_, portStr, found := strings.Cut(addr, ":")
	if !found {
		return 0, fmt.Errorf("addr %q must be in host:port form", addr)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("addr %q has a non-numeric port: %w", addr, err)
	}
	if port < 0 || port > 65535 {
		return 0, fmt.Errorf("addr %q port out of range", addr)
	}
	return port, nil
}

// SQLitePath is the on-disk location of the SQLite database when no explicit
// DSN is configured: {DataDir}/platbor.db.
func (c Config) SQLitePath() string {
	return filepath.Join(c.DataDir, "platbor.db")
}
