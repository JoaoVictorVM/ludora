package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JoaoVictorVM/ludora/internal/config"
	"github.com/JoaoVictorVM/ludora/internal/database"
	"github.com/JoaoVictorVM/ludora/internal/handlers"
	"github.com/JoaoVictorVM/ludora/internal/logging"
	"github.com/JoaoVictorVM/ludora/internal/repository"
	"github.com/JoaoVictorVM/ludora/internal/router"
	"github.com/JoaoVictorVM/ludora/internal/services/rawg"
)

const (
	staticDir         = "static"
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 15 * time.Second
)

func main() {
	if err := run(); err != nil {
		// The logger may not exist yet when configuration fails, so this last
		// resort goes straight to stderr before exiting.
		os.Stderr.WriteString("fatal: " + err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := logging.New(cfg.Env)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := database.Migrate(ctx, pool, logger); err != nil {
		return err
	}

	if cfg.RawgAPIKey == "" {
		logger.Warn("RAWG_API_KEY is not set; game search will fail until it is configured")
	}
	rawgClient := rawg.NewClient(cfg.RawgAPIKey)
	gameRepo := repository.NewGameRepository(pool)
	reviewRepo := repository.NewReviewRepository(pool)

	server := &http.Server{
		Addr: ":" + cfg.Port,
		Handler: router.New(router.Deps{
			Logger:        logger,
			StaticDir:     staticDir,
			GamesSearch:   handlers.NewGamesSearch(rawgClient, logger),
			GamesDetail:   handlers.NewGamesDetail(gameRepo, rawgClient, logger),
			ReviewsSubmit: handlers.NewReviewsSubmit(reviewRepo, gameRepo, logger),
			GamesShow:     handlers.NewGamesShow(gameRepo, reviewRepo, logger),
			Home:          handlers.NewHome(gameRepo, logger),
			GamesRecent:   handlers.NewGamesRecent(gameRepo, logger),
			ReviewsEdit:   handlers.NewReviewsEdit(reviewRepo, logger),
		}),
		ReadHeaderTimeout: readHeaderTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server listening", "port", cfg.Port, "env", cfg.Env)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	return server.Shutdown(shutdownCtx)
}
