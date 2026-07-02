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
	// Log configures structured logging.
	Log LogConfig `yaml:"log"`
	// ShutdownTimeout bounds how long graceful shutdown waits for in-flight
	// requests before forcing termination.
	ShutdownTimeout time.Duration `yaml:"shutdownTimeout"`
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
	data, err := os.ReadFile(path)
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
	validLevels  = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	validFormats = map[string]bool{"text": true, "json": true}
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
