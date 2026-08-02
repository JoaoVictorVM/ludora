package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/JoaoVictorVM/ludora/internal/middleware"
	"github.com/JoaoVictorVM/ludora/internal/views"
)

// New builds the application handler: a standard ServeMux with static asset
// serving, wrapped by request logging and the anonymous-identity middleware.
func New(logger *slog.Logger, staticDir string) http.Handler {
	mux := http.NewServeMux()

	fileServer := http.FileServer(http.Dir(staticDir))
	mux.Handle("GET /static/", http.StripPrefix("/static/", fileServer))

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		if err := views.Home().Render(r.Context(), w); err != nil && logger != nil {
			logger.Error("rendering home", "error", err)
		}
	})

	return requestLogger(logger)(middleware.AnonymousID(mux))
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
