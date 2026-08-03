package handlers

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"

	"github.com/JoaoVictorVM/ludora/internal/repository"
	"github.com/JoaoVictorVM/ludora/internal/views"
)

// FeedPageSize is the grid's page: 6 columns by 5 rows.
const FeedPageSize = 30

// RecentGamesLister is the slice of the game repository the feed depends on.
type RecentGamesLister interface {
	ListRecentlyReviewed(ctx context.Context, limit, offset int) ([]repository.ReviewedGame, bool, error)
}

type Home struct {
	games  RecentGamesLister
	logger *slog.Logger
}

func NewHome(games RecentGamesLister, logger *slog.Logger) *Home {
	return &Home{games: games, logger: logger}
}

// Show handles GET / with the first page of recently reviewed games.
func (h *Home) Show(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	games, hasMore, err := h.games.ListRecentlyReviewed(r.Context(), FeedPageSize, 0)
	if err != nil {
		h.log("loading the home feed", "error", err.Error())
		http.Error(w, "erro ao carregar o feed", http.StatusInternalServerError)
		return
	}

	h.render(w, r, views.Home(games, FeedPageSize, hasMore))
}

func (h *Home) render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	if err := component.Render(r.Context(), w); err != nil {
		h.log("rendering home", "error", err.Error())
	}
}

func (h *Home) log(msg string, args ...any) {
	if h.logger != nil {
		h.logger.Error(msg, args...)
	}
}
