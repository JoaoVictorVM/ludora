package handlers

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/JoaoVictorVM/ludora/internal/middleware"
	"github.com/JoaoVictorVM/ludora/internal/models"
	"github.com/JoaoVictorVM/ludora/internal/repository"
	"github.com/JoaoVictorVM/ludora/internal/views/components"
)

type ReviewsDelete struct {
	reviews ReviewMutations
	logger  *slog.Logger
}

func NewReviewsDelete(reviews ReviewMutations, logger *slog.Logger) *ReviewsDelete {
	return &ReviewsDelete{reviews: reviews, logger: logger}
}

// Confirm handles GET /reviews/{id}/confirmar-exclusao, swapping the controls
// row for the inline confirmation step.
func (h *ReviewsDelete) Confirm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	reviewID, ok := authorizeReview(w, r, h.reviews, h.logger)
	if !ok {
		return
	}

	review, err := h.reviews.GetByID(r.Context(), reviewID)
	if err != nil {
		h.log("loading review for delete confirmation", "review_id", reviewID, "error", err.Error())
		renderFragment(w, r, components.ReviewActionError(models.NotAuthorizedMessage), h.logger)
		return
	}

	renderFragment(w, r, components.ReviewDeleteConfirm(review.ID, review.GameID), h.logger)
}

// Delete handles DELETE /reviews/{id}. Deleting something that is already gone
// is reported as such instead of failing, so a double submit is harmless.
func (h *ReviewsDelete) Delete(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	reviewID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		renderFragment(w, r, components.ReviewActionError(models.NotAuthorizedMessage), h.logger)
		return
	}

	// The review is read before the ownership check so an already-removed one
	// gets the idempotent message rather than a misleading "not authorized".
	review, err := h.reviews.GetByID(r.Context(), reviewID)
	if errors.Is(err, repository.ErrNotFound) {
		renderFragment(w, r, components.ReviewActionError(models.AlreadyRemovedMessage), h.logger)
		return
	}
	if err != nil {
		h.log("loading review for deletion", "review_id", reviewID, "error", err.Error())
		http.Error(w, "erro ao excluir a review", http.StatusInternalServerError)
		return
	}

	if err := middleware.RequireOwnership(r.Context(), h.reviews, reviewID); err != nil {
		if !errors.Is(err, middleware.ErrNotAuthorized) {
			h.log("verifying review ownership", "review_id", reviewID, "error", err.Error())
			http.Error(w, "erro ao excluir a review", http.StatusInternalServerError)
			return
		}
		renderFragment(w, r, components.ReviewActionError(models.NotAuthorizedMessage), h.logger)
		return
	}

	reviewerUUID, _ := middleware.ReviewerID(r.Context())
	deleted, err := h.reviews.DeleteByIDAndReviewer(r.Context(), reviewID, reviewerUUID)
	if err != nil {
		h.log("deleting review", "review_id", reviewID, "error", err.Error())
		http.Error(w, "erro ao excluir a review", http.StatusInternalServerError)
		return
	}
	if !deleted {
		renderFragment(w, r, components.ReviewActionError(models.AlreadyRemovedMessage), h.logger)
		return
	}

	renderReviewsSection(w, r, h.reviews, review.GameID, h.logger)
}

func (h *ReviewsDelete) log(msg string, args ...any) {
	if h.logger != nil {
		h.logger.Error(msg, args...)
	}
}
