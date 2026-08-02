package config

import (
	"errors"
	"os"

	"github.com/joho/godotenv"
)

const (
	EnvDevelopment = "development"
	EnvProduction  = "production"

	defaultPort = "8080"
)

// ErrMissingDatabaseURL is returned when DATABASE_URL is absent or empty.
var ErrMissingDatabaseURL = errors.New("config: DATABASE_URL is required")

type Config struct {
	DatabaseURL string
	Port        string
	Env         string
	RawgAPIKey  string
}

func (c *Config) IsDevelopment() bool {
	return c.Env == EnvDevelopment
}

// Load reads the application configuration from the environment. In development
// it first merges values from a local .env file; in any other environment the
// file is never read, since the hosting platform supplies the variables.
func Load() (*Config, error) {
	env := os.Getenv("ENV")
	if env == "" {
		env = EnvDevelopment
	}

	if env == EnvDevelopment {
		// A missing .env is a normal case for a fresh checkout, so the error is ignored.
		_ = godotenv.Load()
	}

	cfg := &Config{
		DatabaseURL: os.Getenv("DATABASE_URL"),
		Port:        os.Getenv("PORT"),
		Env:         env,
		RawgAPIKey:  os.Getenv("RAWG_API_KEY"),
	}

	if cfg.Port == "" {
		cfg.Port = defaultPort
	}

	if cfg.DatabaseURL == "" {
		return nil, ErrMissingDatabaseURL
	}

	return cfg, nil
}
