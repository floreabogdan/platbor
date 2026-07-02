package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().validate(); err != nil {
		t.Fatalf("default config must be valid: %v", err)
	}
}

func TestLoadZeroConfig(t *testing.T) {
	t.Setenv("PLATBOR_ADDR", "") // ensure a stray empty var is treated as unset

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load with no file or env must succeed: %v", err)
	}
	if cfg.Addr != ":8080" {
		t.Errorf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.DataDir != "platbor-data" {
		t.Errorf("DataDir = %q, want platbor-data", cfg.DataDir)
	}
}

func TestEnvOverridesFileOverridesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "platbor.yaml")
	writeFile(t, path, "addr: \":9000\"\ndataDir: from-file\nlog:\n  level: debug\n")

	// File wins over default...
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":9000" || cfg.DataDir != "from-file" || cfg.Log.Level != "debug" {
		t.Fatalf("file overlay not applied: %+v", cfg)
	}

	// ...and env wins over file.
	t.Setenv("PLATBOR_ADDR", ":9999")
	t.Setenv("PLATBOR_DATA_DIR", "from-env")
	cfg, err = Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Addr != ":9999" || cfg.DataDir != "from-env" {
		t.Fatalf("env overlay not applied: %+v", cfg)
	}
	// Unset env keys keep the file value.
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q, want debug (from file)", cfg.Log.Level)
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "absent.yaml")); err != nil {
		t.Fatalf("a missing config file must be tolerated: %v", err)
	}
}

func TestValidateRejectsBadValues(t *testing.T) {
	tests := map[string]func(*Config){
		"empty addr":        func(c *Config) { c.Addr = "" },
		"port not numeric":  func(c *Config) { c.Addr = ":http" },
		"port out of range": func(c *Config) { c.Addr = ":70000" },
		"empty dataDir":     func(c *Config) { c.DataDir = "" },
		"bad level":         func(c *Config) { c.Log.Level = "verbose" },
		"bad format":        func(c *Config) { c.Log.Format = "xml" },
		"zero timeout":      func(c *Config) { c.ShutdownTimeout = 0 },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := Default()
			mutate(&cfg)
			if err := cfg.validate(); err == nil {
				t.Errorf("expected validation error for %s", name)
			}
		})
	}
}

func TestBadShutdownTimeoutEnv(t *testing.T) {
	t.Setenv("PLATBOR_SHUTDOWN_TIMEOUT", "not-a-duration")
	if _, err := Load(""); err == nil {
		t.Fatal("expected error for unparseable PLATBOR_SHUTDOWN_TIMEOUT")
	}
}

func TestValidTimeoutParsing(t *testing.T) {
	t.Setenv("PLATBOR_SHUTDOWN_TIMEOUT", "30s")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout = %s, want 30s", cfg.ShutdownTimeout)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
