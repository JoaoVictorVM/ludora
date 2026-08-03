package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/JoaoVictorVM/ludora/internal/handlers"
	"github.com/JoaoVictorVM/ludora/internal/middleware"
)

// Deps carries everything the router needs to mount the application's routes.
type Deps struct {
	Logger        *slog.Logger
	StaticDir     string
	GamesSearch   *handlers.GamesSearch
	GamesDetail   *handlers.GamesDetail
	ReviewsSubmit *handlers.ReviewsSubmit
	GamesShow     *handlers.GamesShow
	Home          *handlers.Home
	GamesRecent   *handlers.GamesRecent
	ReviewsEdit   *handlers.ReviewsEdit
	ReviewsDelete *handlers.ReviewsDelete
}

// New builds the application handler: a standard ServeMux with the feature
// routes and static assets, wrapped by request logging and the
// anonymous-identity middleware.
func New(deps Deps) http.Handler {
	mux := http.NewServeMux()

	fileServer := http.FileServer(http.Dir(deps.StaticDir))
	mux.Handle("GET /static/", http.StripPrefix("/static/", fileServer))

	if deps.Home != nil {
		mux.HandleFunc("GET /{$}", deps.Home.Show)
	}

	if deps.GamesRecent != nil {
		mux.HandleFunc("GET /jogos/recentes", deps.GamesRecent.List)
	}

	if deps.GamesSearch != nil {
		mux.HandleFunc("GET /jogos/buscar", deps.GamesSearch.Search)
	}

	if deps.GamesDetail != nil {
		mux.HandleFunc("GET /jogos/{external_id}/formulario", deps.GamesDetail.Form)
	}

	if deps.ReviewsSubmit != nil {
		mux.HandleFunc("POST /reviews", deps.ReviewsSubmit.Submit)
	}

	if deps.ReviewsEdit != nil {
		mux.HandleFunc("GET /reviews/{id}/editar", deps.ReviewsEdit.Form)
		mux.HandleFunc("PUT /reviews/{id}", deps.ReviewsEdit.Update)
	}

	if deps.ReviewsDelete != nil {
		mux.HandleFunc("GET /reviews/{id}/confirmar-exclusao", deps.ReviewsDelete.Confirm)
		mux.HandleFunc("DELETE /reviews/{id}", deps.ReviewsDelete.Delete)
	}

	if deps.GamesShow != nil {
		mux.HandleFunc("GET /jogos/{id}", deps.GamesShow.Show)
	}

	return requestLogger(deps.Logger)(middleware.AnonymousID(mux))
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(recorder, r)

			if logger != nil {
				logger.Info("request",
					"method", r.Method,
					"path", r.URL.Path,
					"status", recorder.status,
					"duration_ms", time.Since(start).Milliseconds(),
				)
			}
		})
	}
}
