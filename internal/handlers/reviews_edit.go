package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/a-h/templ"

	"github.com/JoaoVictorVM/ludora/internal/middleware"
	"github.com/JoaoVictorVM/ludora/internal/models"
	"github.com/JoaoVictorVM/ludora/internal/repository"
	"github.com/JoaoVictorVM/ludora/internal/views/components"
)

// ReviewMutations is the slice of the review repository the edit and delete
// flows share: ownership lookup, the mutation itself, and the queries needed to
// re-render the refreshed reviews section.
type ReviewMutations interface {
	GetByID(ctx context.Context, id int64) (*models.Review, error)
	OwnerOf(ctx context.Context, id int64) (string, error)
	UpdateByIDAndReviewer(ctx context.Context, id int64, reviewerUUID string, rating int16, comment string) (bool, error)
	DeleteByIDAndReviewer(ctx context.Context, id int64, reviewerUUID string) (bool, error)
	ListByGameID(ctx context.Context, gameID int64) ([]models.Review, error)
	AverageRatingByGameID(ctx context.Context, gameID int64) (repository.RatingSummary, error)
}

type ReviewsEdit struct {
	reviews ReviewMutations
	logger  *slog.Logger
}

func NewReviewsEdit(reviews ReviewMutations, logger *slog.Logger) *ReviewsEdit {
	return &ReviewsEdit{reviews: reviews, logger: logger}
}

// Form handles GET /reviews/{id}/editar.
func (h *ReviewsEdit) Form(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	reviewID, ok := h.authorize(w, r)
	if !ok {
		return
	}

	review, err := h.reviews.GetByID(r.Context(), reviewID)
	if err != nil {
		h.log("loading review for editing", "review_id", reviewID, "error", err.Error())
		renderFragment(w, r, components.ReviewActionError(models.NotAuthorizedMessage), h.logger)
		return
	}

	renderFragment(w, r, components.ReviewEditForm(*review, ""), h.logger)
}

// Update handles PUT /reviews/{id}.
func (h *ReviewsEdit) Update(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	reviewID, ok := h.authorize(w, r)
	if !ok {
		return
	}

	review, err := h.reviews.GetByID(r.Context(), reviewID)
	if err != nil {
		h.log("loading review for update", "review_id", reviewID, "error", err.Error())
		renderFragment(w, r, components.ReviewActionError(models.NotAuthorizedMessage), h.logger)
		return
	}

	if err := r.ParseForm(); err != nil {
		h.log("parsing edit form", "review_id", reviewID, "error", err.Error())
		renderFragment(w, r, components.ReviewEditForm(*review, models.MissingRatingMessage), h.logger)
		return
	}

	rating, ratingErr := parseRating(r.PostFormValue("rating"))
	comment := strings.TrimSpace(r.PostFormValue("comment"))

	// The edited values are echoed back on rejection so the visitor does not
	// lose what they typed.
	review.Comment = comment
	if ratingErr != nil {
		renderFragment(w, r, components.ReviewEditForm(*review, models.MissingRatingMessage), h.logger)
		return
	}
	review.Rating = rating

	if utf8.RuneCountInString(comment) > models.MaxCommentLength {
		renderFragment(w, r, components.ReviewEditForm(*review, models.CommentTooLongMessage), h.logger)
		return
	}

	reviewerUUID, _ := middleware.ReviewerID(r.Context())
	updated, err := h.reviews.UpdateByIDAndReviewer(r.Context(), reviewID, reviewerUUID, rating, comment)
	if err != nil {
		h.log("updating review", "review_id", reviewID, "error", err.Error())
		renderFragment(w, r, components.ReviewActionError(models.NotAuthorizedMessage), h.logger)
		return
	}
	if !updated {
		renderFragment(w, r, components.ReviewActionError(models.AlreadyRemovedMessage), h.logger)
		return
	}

	h.renderReviewsSection(w, r, review.GameID)
}

// authorize resolves the review id and confirms the requester owns it, writing
// the generic failure fragment itself when it does not.
func (h *ReviewsEdit) authorize(w http.ResponseWriter, r *http.Request) (int64, bool) {
	return authorizeReview(w, r, h.reviews, h.logger)
}

func (h *ReviewsEdit) renderReviewsSection(w http.ResponseWriter, r *http.Request, gameID int64) {
	renderReviewsSection(w, r, h.reviews, gameID, h.logger)
}

func (h *ReviewsEdit) log(msg string, args ...any) {
	if h.logger != nil {
		h.logger.Error(msg, args...)
	}
}

func authorizeReview(w http.ResponseWriter, r *http.Request, reviews ReviewMutations, logger *slog.Logger) (int64, bool) {
	reviewID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		renderFragment(w, r, components.ReviewActionError(models.NotAuthorizedMessage), logger)
		return 0, false
	}

	err = middleware.RequireOwnership(r.Context(), reviews, reviewID)
	switch {
	case err == nil:
		return reviewID, true
	case errors.Is(err, middleware.ErrNotAuthorized):
		renderFragment(w, r, components.ReviewActionError(models.NotAuthorizedMessage), logger)
	default:
		if logger != nil {
			logger.Error("verifying review ownership", "review_id", reviewID, "error", err.Error())
		}
		http.Error(w, "erro ao verificar a review", http.StatusInternalServerError)
	}

	return 0, false
}

func renderReviewsSection(w http.ResponseWriter, r *http.Request, reviews ReviewMutations, gameID int64, logger *slog.Logger) {
	list, err := reviews.ListByGameID(r.Context(), gameID)
	if err != nil {
		if logger != nil {
			logger.Error("listing reviews after mutation", "game_id", gameID, "error", err.Error())
		}
		http.Error(w, "erro ao recarregar as reviews", http.StatusInternalServerError)
		return
	}

	summary, err := reviews.AverageRatingByGameID(r.Context(), gameID)
	if err != nil {
		if logger != nil {
			logger.Error("averaging reviews after mutation", "game_id", gameID, "error", err.Error())
		}
		http.Error(w, "erro ao recarregar as reviews", http.StatusInternalServerError)
		return
	}

	viewerUUID, _ := middleware.ReviewerID(r.Context())
	renderFragment(w, r,
		components.ReviewsSection(summary.AverageStars, summary.TotalReviews,
			toReviewViews(list, viewerUUID, time.Now())),
		logger)
}

func renderFragment(w http.ResponseWriter, r *http.Request, component templ.Component, logger *slog.Logger) {
	if err := component.Render(r.Context(), w); err != nil && logger != nil {
		logger.Error("rendering review fragment", "error", err.Error())
	}
}
