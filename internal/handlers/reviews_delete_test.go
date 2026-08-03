package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/JoaoVictorVM/ludora/internal/models"
	"github.com/JoaoVictorVM/ludora/internal/repository"
)

func (f *mutationFixture) confirmDelete(t *testing.T, reviewID, reviewerUUID string) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, http.MethodGet, reviewID, reviewerUUID, nil, NewReviewsDelete(f.reviews, nil).Confirm)
}

func (f *mutationFixture) deleteReview(t *testing.T, reviewID, reviewerUUID string) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, http.MethodDelete, reviewID, reviewerUUID, nil, NewReviewsDelete(f.reviews, nil).Delete)
}

func (f *mutationFixture) countReviews(t *testing.T) int {
	t.Helper()

	var count int
	err := f.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM reviews WHERE game_id = $1", f.gameID).Scan(&count)
	if err != nil {
		t.Fatalf("counting reviews: %v", err)
	}

	return count
}

func TestDeleteConfirm_RendersConfirmationFragment(t *testing.T) {
	f := newMutationFixture(t)

	body := f.confirmDelete(t, f.id(), f.owner).Body.String()

	if !strings.Contains(body, "Excluir esta review?") {
		t.Errorf("expected the inline confirmation, got %q", body)
	}
	if !strings.Contains(body, `hx-delete="/reviews/`+f.id()+`"`) {
		t.Error("the confirmation should carry the delete action")
	}
	if !strings.Contains(body, "Cancelar") {
		t.Error("the confirmation should offer a way out")
	}
	if f.countReviews(t) != 1 {
		t.Error("asking for confirmation must not delete anything")
	}
}

func TestDeleteConfirm_UnauthorizedForNonOwner(t *testing.T) {
	f := newMutationFixture(t)

	body := f.confirmDelete(t, f.id(), uuid.NewString()).Body.String()

	if !strings.Contains(body, models.NotAuthorizedMessage) {
		t.Errorf("expected the generic message, got %q", body)
	}
	if strings.Contains(body, "hx-delete") {
		t.Error("a non-owner must not be handed a delete action")
	}
}

func TestDelete_RemovesReviewAndRecalculatesAverage(t *testing.T) {
	f := newMutationFixture(t)
	f.addOtherReview(t, 4)

	// Reviews at 8 and 4 average 3,0; removing the 8 leaves 4 → 2,0
	body := f.deleteReview(t, f.id(), f.owner).Body.String()

	if f.countReviews(t) != 1 {
		t.Errorf("reviews remaining = %d, want 1", f.countReviews(t))
	}
	if _, err := f.reviews.GetByID(context.Background(), f.reviewID); err == nil {
		t.Error("the deleted review is still readable")
	}

	if !strings.Contains(body, `id="reviews-section"`) {
		t.Error("the response should be the refreshed reviews section")
	}
	if !strings.Contains(body, "2") || !strings.Contains(body, "1 review") {
		t.Errorf("the summary should reflect the removal, got %q", body)
	}
	if strings.Contains(body, "Comentário original.") {
		t.Error("the deleted review must not appear in the refreshed list")
	}
}

func TestDelete_LastReviewLeavesEmptyState(t *testing.T) {
	f := newMutationFixture(t)

	body := f.deleteReview(t, f.id(), f.owner).Body.String()

	if f.countReviews(t) != 0 {
		t.Errorf("reviews remaining = %d, want 0", f.countReviews(t))
	}
	if !strings.Contains(body, "ainda não tem reviews") {
		t.Errorf("removing the last review should reveal the empty state, got %q", body)
	}
}

func TestDelete_UnauthorizedForNonOwner(t *testing.T) {
	f := newMutationFixture(t)

	body := f.deleteReview(t, f.id(), uuid.NewString()).Body.String()

	if !strings.Contains(body, models.NotAuthorizedMessage) {
		t.Errorf("expected the generic message, got %q", body)
	}
	if f.countReviews(t) != 1 {
		t.Error("a non-owner deleted the review")
	}
	if _, err := f.reviews.GetByID(context.Background(), f.reviewID); err != nil {
		t.Errorf("the review should still exist: %v", err)
	}
}

func TestDelete_AlreadyDeletedIsIdempotent(t *testing.T) {
	f := newMutationFixture(t)

	if body := f.deleteReview(t, f.id(), f.owner).Body.String(); strings.Contains(body, models.AlreadyRemovedMessage) {
		t.Fatalf("the first delete should succeed, got %q", body)
	}

	body := f.deleteReview(t, f.id(), f.owner).Body.String()

	if !strings.Contains(body, models.AlreadyRemovedMessage) {
		t.Errorf("expected the already-removed message on the second delete, got %q", body)
	}
}

func TestDelete_UnknownReviewReportsAlreadyRemoved(t *testing.T) {
	f := newMutationFixture(t)

	body := f.deleteReview(t, "999999", f.owner).Body.String()

	if !strings.Contains(body, models.AlreadyRemovedMessage) {
		t.Errorf("expected the already-removed message, got %q", body)
	}
}

// TestDelete_CascadeKeepsGameOnTheFeedOnlyWhileReviewsRemain ties the delete
// flow back to F06: a game whose last review is removed drops off the feed.
func TestDelete_CascadeKeepsGameOnTheFeedOnlyWhileReviewsRemain(t *testing.T) {
	f := newMutationFixture(t)
	gameRepo := repository.NewGameRepository(f.pool)

	games, _, err := gameRepo.ListRecentlyReviewed(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("ListRecentlyReviewed: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("precondition failed: feed has %d games, want 1", len(games))
	}

	f.deleteReview(t, f.id(), f.owner)

	games, _, err = gameRepo.ListRecentlyReviewed(context.Background(), 10, 0)
	if err != nil {
		t.Fatalf("ListRecentlyReviewed: %v", err)
	}
	if len(games) != 0 {
		t.Errorf("feed still lists %d games after the last review was deleted, want 0", len(games))
	}
}
