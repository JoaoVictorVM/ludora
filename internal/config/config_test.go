package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// chdirTemp moves the process into a fresh temp dir for the duration of the test,
// so godotenv resolves ".env" against a directory this test fully controls.
func chdirTemp(t *testing.T) string {
	t.Helper()

	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}

	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("os.Chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(original); err != nil {
			t.Fatalf("restoring working directory: %v", err)
		}
	})

	return dir
}

func writeDotenv(t *testing.T, dir, contents string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(contents), 0o600); err != nil {
		t.Fatalf("writing .env: %v", err)
	}
}

func clearEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{"ENV", "DATABASE_URL", "PORT", "IGDB_CLIENT_ID", "IGDB_CLIENT_SECRET"} {
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unsetting %s: %v", key, err)
		}
	}
}

func TestLoadConfig_MissingDatabaseURL(t *testing.T) {
	chdirTemp(t)
	clearEnv(t)

	cfg, err := Load()
	if cfg != nil {
		t.Fatalf("expected no config, got %+v", cfg)
	}
	if !errors.Is(err, ErrMissingDatabaseURL) {
		t.Fatalf("expected ErrMissingDatabaseURL, got %v", err)
	}
}

func TestLoadConfig_DevelopmentLoadsDotenv(t *testing.T) {
	dir := chdirTemp(t)
	clearEnv(t)
	writeDotenv(t, dir, "DATABASE_URL=postgres://dotenv/ludora\nPORT=4321\n")

	t.Setenv("ENV", EnvDevelopment)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL != "postgres://dotenv/ludora" {
		t.Errorf("DatabaseURL = %q, want the .env value", cfg.DatabaseURL)
	}
	if cfg.Port != "4321" {
		t.Errorf("Port = %q, want 4321", cfg.Port)
	}
}

func TestLoadConfig_ProductionIgnoresDotenv(t *testing.T) {
	dir := chdirTemp(t)
	clearEnv(t)
	writeDotenv(t, dir, "DATABASE_URL=postgres://dotenv/ludora\nPORT=4321\n")

	t.Setenv("ENV", EnvProduction)
	t.Setenv("DATABASE_URL", "postgres://os-env/ludora")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.DatabaseURL != "postgres://os-env/ludora" {
		t.Errorf("DatabaseURL = %q, want the OS environment value", cfg.DatabaseURL)
	}
	if cfg.Port != defaultPort {
		t.Errorf("Port = %q, want the default %q (.env must not be read)", cfg.Port, defaultPort)
	}
}

func TestLoadConfig_DefaultsPortAndEnv(t *testing.T) {
	chdirTemp(t)
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://os-env/ludora")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != defaultPort {
		t.Errorf("Port = %q, want %q", cfg.Port, defaultPort)
	}
	if cfg.Env != EnvDevelopment {
		t.Errorf("Env = %q, want %q", cfg.Env, EnvDevelopment)
	}
}

func TestLoadConfig_ReadsIgdbCredentials(t *testing.T) {
	chdirTemp(t)
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://os-env/ludora")
	t.Setenv("IGDB_CLIENT_ID", "client-id")
	t.Setenv("IGDB_CLIENT_SECRET", "client-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.IgdbClientID != "client-id" {
		t.Errorf("IgdbClientID = %q, want client-id", cfg.IgdbClientID)
	}
	if cfg.IgdbClientSecret != "client-secret" {
		t.Errorf("IgdbClientSecret = %q, want client-secret", cfg.IgdbClientSecret)
	}
}

// Missing credentials must not stop the server from booting: the search feature
// degrades to its unavailable message, but every other page keeps working.
func TestLoadConfig_MissingIgdbCredentialsIsNotFatal(t *testing.T) {
	chdirTemp(t)
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://os-env/ludora")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.IgdbClientID != "" || cfg.IgdbClientSecret != "" {
		t.Error("expected empty credentials when the variables are unset")
	}
}
