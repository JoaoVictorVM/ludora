package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/a-h/templ"

	"github.com/JoaoVictorVM/ludora/internal/middleware"
	"github.com/JoaoVictorVM/ludora/internal/models"
	"github.com/JoaoVictorVM/ludora/internal/repository"
	"github.com/JoaoVictorVM/ludora/internal/views/components"
)

// ReviewStore is the slice of the review repository this handler depends on.
type ReviewStore interface {
	Create(ctx context.Context, review *models.Review) (*models.Review, error)
	ExistsForGameAndReviewer(ctx context.Context, gameID int64, reviewerUUID string) (bool, error)
}

// GameFinder resolves the local game a submission refers to.
type GameFinder interface {
	GetByID(ctx context.Context, id int64) (*models.Game, error)
}

type ReviewsSubmit struct {
	reviews ReviewStore
	games   GameFinder
	logger  *slog.Logger
}

func NewReviewsSubmit(reviews ReviewStore, games GameFinder, logger *slog.Logger) *ReviewsSubmit {
	return &ReviewsSubmit{reviews: reviews, games: games, logger: logger}
}

// Submit handles POST /reviews.
func (h *ReviewsSubmit) Submit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	reviewerUUID, ok := middleware.ReviewerID(r.Context())
	if !ok {
		h.log("submission without an anonymous identifier")
		http.Error(w, "identificação anônima ausente", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.log("parsing review form", "error", err.Error())
		http.Error(w, "formulário inválido", http.StatusBadRequest)
		return
	}

	gameID, err := strconv.ParseInt(r.PostFormValue("game_id"), 10, 64)
	if err != nil {
		h.log("invalid game_id on submission", "value", r.PostFormValue("game_id"))
		http.Error(w, "jogo inválido", http.StatusBadRequest)
		return
	}

	game, err := h.games.GetByID(r.Context(), gameID)
	if err != nil {
		h.log("resolving game for submission", "game_id", gameID, "error", err.Error())
		h.render(w, r, components.GameLoadError())
		return
	}

	rating, err := parseRating(r.PostFormValue("rating"))
	comment := strings.TrimSpace(r.PostFormValue("comment"))

	if err != nil {
		h.reject(w, r, game, 0, comment, models.MissingRatingMessage)
		return
	}
	if utf8.RuneCountInString(comment) > models.MaxCommentLength {
		h.reject(w, r, game, rating, comment, models.CommentTooLongMessage)
		return
	}

	exists, err := h.reviews.ExistsForGameAndReviewer(r.Context(), gameID, reviewerUUID)
	if err != nil {
		h.log("checking for an existing review", "game_id", gameID, "error", err.Error())
		h.render(w, r, components.GameLoadError())
		return
	}
	if exists {
		h.reject(w, r, game, rating, comment, models.DuplicateReviewMessage)
		return
	}

	_, err = h.reviews.Create(r.Context(), &models.Review{
		GameID:       gameID,
		ReviewerUUID: reviewerUUID,
		Rating:       rating,
		Comment:      comment,
	})
	if err != nil {
		// The unique constraint is the safety net for two submissions racing
		// past the pre-check above.
		if errors.Is(err, repository.ErrDuplicateReview) {
			h.reject(w, r, game, rating, comment, models.DuplicateReviewMessage)
			return
		}

		h.log("creating review", "game_id", gameID, "error", err.Error())
		h.render(w, r, components.GameLoadError())
		return
	}

	w.Header().Set("HX-Redirect", "/jogos/"+strconv.FormatInt(gameID, 10))
	w.WriteHeader(http.StatusOK)
}

func parseRating(raw string) (int16, error) {
	rating, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if rating < models.MinRating || rating > models.MaxRating {
		return 0, errors.New("rating out of range")
	}

	return int16(rating), nil
}

func (h *ReviewsSubmit) reject(w http.ResponseWriter, r *http.Request, game *models.Game, rating int16, comment, message string) {
	h.render(w, r, components.ReviewForm(game, rating, comment, message))
}

func (h *ReviewsSubmit) render(w http.ResponseWriter, r *http.Request, component templ.Component) {
	if err := component.Render(r.Context(), w); err != nil {
		h.log("rendering review form", "error", err.Error())
	}
}

func (h *ReviewsSubmit) log(msg string, args ...any) {
	if h.logger != nil {
		h.logger.Error(msg, args...)
	}
}
