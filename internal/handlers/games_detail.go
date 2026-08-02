package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/a-h/templ"

	"github.com/JoaoVictorVM/ludora/internal/models"
	"github.com/JoaoVictorVM/ludora/internal/repository"
	"github.com/JoaoVictorVM/ludora/internal/services/rawg"
	"github.com/JoaoVictorVM/ludora/internal/views/components"
)

// GameDetailsFetcher is the slice of the RAWG client used on a cache miss.
type GameDetailsFetcher interface {
	GetGameDetails(ctx context.Context, externalID string) (*rawg.GameDetails, error)
}

// GameCache is the slice of the game repository this handler depends on.
type GameCache interface {
	FindByExternalID(ctx context.Context, source, externalID string) (*models.Game, error)
	Create(ctx context.Context, game *models.Game) (*models.Game, error)
}

type GamesDetail struct {
	games  GameCache
	client GameDetailsFetcher
	logger *slog.Logger
}

func NewGamesDetail(games GameCache, client GameDetailsFetcher, logger *slog.Logger) *GamesDetail {
	return &GamesDetail{games: games, client: client, logger: logger}
}

// Form handles GET /jogos/{external_id}/formulario: it resolves the selected
// game into a local record — fetching from RAWG only on a cache miss — and
// renders the review form shell.
func (h *GamesDetail) Form(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	externalID := r.PathValue("external_id")
	if _, err := strconv.Atoi(externalID); err != nil {
		h.log("invalid rawg external id", "external_id", externalID)
		h.render(w, r, components.GameLoadError())
		return
	}

	game, err := h.resolve(r.Context(), externalID)
	if err != nil {
		h.render(w, r, components.GameLoadError())
		return
	}

	h.render(w, r, components.ReviewFormShell(game))
}

func (h *GamesDetail) resolve(ctx context.Context, externalID string) (*models.Game, error) {
	game, err := h.games.FindByExternalID(ctx, models.SourceRAWG, externalID)
	if err == nil {
		return game, nil
	}
	if !errors.Is(err, repository.ErrNotFound) {
		h.log("looking up cached game", "external_id", externalID, "error", err.Error())
		return nil, err
	}

	details, err := h.client.GetGameDetails(ctx, externalID)
	if err != nil {
		// Nothing is persisted on a failed fetch, so a retry starts clean.
		h.log("fetching game details from rawg", "external_id", externalID, "error", err.Error())
		return nil, err
	}

	game, err = h.games.Create(ctx, &models.Game{
		ExternalID:     externalID,
		ExternalSource: models.SourceRAWG,
		Name:           details.Name,
		CoverURL:       details.CoverURL,
		ReleasedAt:     details.ReleasedAt,
		Developer:      details.Developer,
		Description:    details.Description,
	})
	if err != nil {
		h.log("caching game", "external_id", externalID, "error", err.Error())
		return nil, err
	}

	return game, nil
}

func (h *GamesDetail) render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	if err := component.Render(r.Context(), w); err != nil {
		h.log("rendering game form fragment", "error", err.Error())
	}
}

func (h *GamesDetail) log(msg string, args ...any) {
	if h.logger != nil {
		h.logger.Error(msg, args...)
	}
}
