package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/a-h/templ"

	"github.com/JoaoVictorVM/ludora/internal/services/rawg"
	"github.com/JoaoVictorVM/ludora/internal/views/components"
)

// minQueryLength keeps the endpoint quiet while the visitor is still typing, so
// a single character never produces a "no games found" flash.
const minQueryLength = 2

const (
	messageUnavailable = "Não foi possível buscar jogos agora. Tente novamente em instantes."
	messageGenericFail = "Algo deu errado com essa busca."
)

// GameSearcher is the slice of the RAWG client this handler depends on.
type GameSearcher interface {
	SearchGames(ctx context.Context, query string) ([]rawg.SearchResult, error)
}

type GamesSearch struct {
	client GameSearcher
	logger *slog.Logger
}

func NewGamesSearch(client GameSearcher, logger *slog.Logger) *GamesSearch {
	return &GamesSearch{client: client, logger: logger}
}

// Search handles GET /jogos/buscar. Upstream failures are rendered as a fragment
// with a 200, since HTMX swaps the response body into the results area either way.
func (h *GamesSearch) Search(w http.ResponseWriter, r *http.Request) {
	query := strings.TrimSpace(r.URL.Query().Get("q"))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if utf8.RuneCountInString(query) < minQueryLength {
		return
	}

	results, err := h.client.SearchGames(r.Context(), query)
	if err != nil {
		h.renderFailure(w, r, query, err)
		return
	}

	h.render(w, r, components.SearchResults(query, results))
}

func (h *GamesSearch) renderFailure(w http.ResponseWriter, r *http.Request, query string, err error) {
	message := messageGenericFail
	if errors.Is(err, rawg.ErrUnavailable) {
		message = messageUnavailable
	}

	// The status code is the useful signal for a rejected call, so it is logged
	// instead of the error string, which would otherwise be redundant.
	var apiErr *rawg.APIError
	if errors.As(err, &apiErr) {
		h.log("rawg search returned an unexpected status", "query", query, "status", apiErr.StatusCode)
	} else {
		h.log("rawg search failed", "query", query, "error", err.Error())
	}

	h.render(w, r, components.SearchError(message))
}

func (h *GamesSearch) render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	if err := component.Render(r.Context(), w); err != nil {
		h.log("rendering search fragment", "error", err.Error())
	}
}

func (h *GamesSearch) log(msg string, args ...any) {
	if h.logger != nil {
		h.logger.Error(msg, args...)
	}
}
