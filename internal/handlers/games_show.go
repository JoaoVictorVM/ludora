package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/a-h/templ"

	"github.com/JoaoVictorVM/ludora/internal/middleware"
	"github.com/JoaoVictorVM/ludora/internal/models"
	"github.com/JoaoVictorVM/ludora/internal/repository"
	"github.com/JoaoVictorVM/ludora/internal/util"
	"github.com/JoaoVictorVM/ludora/internal/views"
	"github.com/JoaoVictorVM/ludora/internal/views/components"
)

// ReviewLister is the slice of the review repository the detail page needs.
type ReviewLister interface {
	ListByGameID(ctx context.Context, gameID int64) ([]models.Review, error)
	AverageRatingByGameID(ctx context.Context, gameID int64) (repository.RatingSummary, error)
}

type GamesShow struct {
	games   GameFinder
	reviews ReviewLister
	logger  *slog.Logger
}

func NewGamesShow(games GameFinder, reviews ReviewLister, logger *slog.Logger) *GamesShow {
	return &GamesShow{games: games, reviews: reviews, logger: logger}
}

// Show handles GET /jogos/{id}.
func (h *GamesShow) Show(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	gameID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.renderNotFound(w, r)
		return
	}

	game, err := h.games.GetByID(r.Context(), gameID)
	if err != nil {
		if !errors.Is(err, repository.ErrNotFound) {
			h.log("loading game", "game_id", gameID, "error", err.Error())
		}
		h.renderNotFound(w, r)
		return
	}

	reviews, err := h.reviews.ListByGameID(r.Context(), gameID)
	if err != nil {
		h.log("listing reviews", "game_id", gameID, "error", err.Error())
		http.Error(w, "erro ao carregar as reviews", http.StatusInternalServerError)
		return
	}

	summary, err := h.reviews.AverageRatingByGameID(r.Context(), gameID)
	if err != nil {
		h.log("averaging reviews", "game_id", gameID, "error", err.Error())
		http.Error(w, "erro ao carregar as reviews", http.StatusInternalServerError)
		return
	}

	// The identifier is optional here: a visitor whose cookie is missing simply
	// owns no review, so the page still renders — just without any controls.
	viewerUUID, _ := middleware.ReviewerID(r.Context())

	h.render(w, r, views.GameDetail(game, summary.AverageStars, summary.TotalReviews,
		toReviewViews(reviews, viewerUUID, time.Now())))
}

func toReviewViews(reviews []models.Review, viewerUUID string, now time.Time) []components.ReviewView {
	views := make([]components.ReviewView, 0, len(reviews))
	for _, review := range reviews {
		views = append(views, components.ReviewView{
			Review:       review,
			IsOwner:      viewerUUID != "" && review.ReviewerUUID == viewerUUID,
			RelativeTime: util.RelativeTime(review.CreatedAt, now),
		})
	}

	return views
}

func (h *GamesShow) renderNotFound(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusNotFound)
	h.render(w, r, views.NotFound())
}

func (h *GamesShow) render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	if err := component.Render(r.Context(), w); err != nil {
		h.log("rendering game detail page", "error", err.Error())
	}
}

func (h *GamesShow) log(msg string, args ...any) {
	if h.logger != nil {
		h.logger.Error(msg, args...)
	}
}
