package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/a-h/templ"

	"github.com/JoaoVictorVM/ludora/internal/views/components"
)

type GamesRecent struct {
	games  RecentGamesLister
	logger *slog.Logger
}

func NewGamesRecent(games RecentGamesLister, logger *slog.Logger) *GamesRecent {
	return &GamesRecent{games: games, logger: logger}
}

// List handles GET /jogos/recentes, returning the next batch of cards plus the
// control for the page after it — or the cards alone once the catalog runs out.
func (h *GamesRecent) List(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
	if err != nil || offset < 0 {
		offset = 0
	}

	games, hasMore, err := h.games.ListRecentlyReviewed(r.Context(), FeedPageSize, offset)
	if err != nil {
		h.log("loading the next feed page", "offset", offset, "error", err.Error())
		http.Error(w, "erro ao carregar mais jogos", http.StatusInternalServerError)
		return
	}

	h.render(w, r, components.GameCardBatch(games, offset+FeedPageSize, hasMore))
}

func (h *GamesRecent) render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	if err := component.Render(r.Context(), w); err != nil {
		h.log("rendering feed page", "error", err.Error())
	}
}

func (h *GamesRecent) log(msg string, args ...any) {
	if h.logger != nil {
		h.logger.Error(msg, args...)
	}
}
