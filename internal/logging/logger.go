package logging

import (
	"log/slog"
	"os"

	"github.com/JoaoVictorVM/ludora/internal/config"
)

// New builds the application logger: JSON to stdout, debug level in development
// and info level everywhere else. It is also installed as slog's default logger
// so third-party code logging through the package-level helpers stays consistent.
func New(env string) *slog.Logger {
	level := slog.LevelInfo
	if env == config.EnvDevelopment {
		level = slog.LevelDebug
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	return logger
}
