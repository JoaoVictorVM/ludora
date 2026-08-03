package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JoaoVictorVM/ludora/internal/middleware"
	"github.com/JoaoVictorVM/ludora/internal/models"
	"github.com/JoaoVictorVM/ludora/internal/repository"
)

type mutationFixture struct {
	pool     *pgxpool.Pool
	reviews  *repository.ReviewRepository
	gameID   int64
	owner    string
	reviewID int64
}

func newMutationFixture(t *testing.T) *mutationFixture {
	t.Helper()

	ctx := context.Background()
	pool := migratedPool(t)
	gameRepo := repository.NewGameRepository(pool)
	reviewRepo := repository.NewReviewRepository(pool)

	game, err := gameRepo.Create(ctx, &models.Game{
		ExternalID:     "3498",
		ExternalSource: models.SourceRAWG,
		Name:           "Grand Theft Auto V",
	})
	if err != nil {
		t.Fatalf("seeding game: %v", err)
	}

	owner := uuid.NewString()
	review, err := reviewRepo.Create(ctx, &models.Review{
		GameID:       game.ID,
		ReviewerUUID: owner,
		Rating:       8,
		Comment:      "Comentário original.",
	})
	if err != nil {
		t.Fatalf("seeding review: %v", err)
	}

	return &mutationFixture{
		pool:     pool,
		reviews:  reviewRepo,
		gameID:   game.ID,
		owner:    owner,
		reviewID: review.ID,
	}
}

// addOtherReview gives the game a second review so the recalculated average has
// something to move against.
func (f *mutationFixture) addOtherReview(t *testing.T, rating int16) {
	t.Helper()

	_, err := f.reviews.Create(context.Background(), &models.Review{
		GameID:       f.gameID,
		ReviewerUUID: uuid.NewString(),
		Rating:       rating,
	})
	if err != nil {
		t.Fatalf("seeding second review: %v", err)
	}
}

// request drives one handler directly, through the real anonymous-identity
// middleware so ownership is decided from the cookie.
func (f *mutationFixture) request(t *testing.T, method, reviewID, reviewerUUID string, form url.Values, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()

	body := ""
	if form != nil {
		body = form.Encode()
	}

	req := httptest.NewRequest(method, "/reviews/"+reviewID, strings.NewReader(body))
	req.SetPathValue("id", reviewID)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: middleware.CookieName, Value: reviewerUUID})

	rec := httptest.NewRecorder()
	middleware.AnonymousID(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	return rec
}

func (f *mutationFixture) editForm(t *testing.T, reviewID, reviewerUUID string) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, http.MethodGet, reviewID, reviewerUUID, nil, NewReviewsEdit(f.reviews, nil).Form)
}

func (f *mutationFixture) update(t *testing.T, reviewID, reviewerUUID string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return f.request(t, http.MethodPut, reviewID, reviewerUUID, form, NewReviewsEdit(f.reviews, nil).Update)
}

func (f *mutationFixture) id() string {
	return strconv.FormatInt(f.reviewID, 10)
}

func (f *mutationFixture) storedReview(t *testing.T) *models.Review {
	t.Helper()

	review, err := f.reviews.GetByID(context.Background(), f.reviewID)
	if err != nil {
		t.Fatalf("reading review: %v", err)
	}

	return review
}

func TestEditForm_PrefillsExistingValues(t *testing.T) {
	f := newMutationFixture(t)

	body := f.editForm(t, f.id(), f.owner).Body.String()

	if !strings.Contains(body, "Comentário original.") {
		t.Error("the edit form should pre-fill the existing comment")
	}

	idx := strings.Index(body, `value="8"`)
	if idx < 0 {
		t.Fatal(`missing the radio for the current rating (value="8")`)
	}
	if end := strings.Index(body[idx:], ">"); !strings.Contains(body[idx:idx+end], "checked") {
		t.Error("the current rating should come pre-selected")
	}
	if got := strings.Count(body, "checked"); got != 1 {
		t.Errorf("checked attributes = %d, want exactly 1", got)
	}
}

func TestEditForm_UnauthorizedForNonOwner(t *testing.T) {
	f := newMutationFixture(t)

	body := f.editForm(t, f.id(), uuid.NewString()).Body.String()

	if !strings.Contains(body, models.NotAuthorizedMessage) {
		t.Errorf("expected the generic message, got %q", body)
	}
	if strings.Contains(body, "Comentário original.") {
		t.Error("a non-owner must not see the review's contents")
	}
}

func TestEditForm_UnknownReviewLooksTheSameAsNotAuthorized(t *testing.T) {
	f := newMutationFixture(t)

	body := f.editForm(t, "999999", f.owner).Body.String()

	if !strings.Contains(body, models.NotAuthorizedMessage) {
		t.Errorf("a missing review must produce the same message as a foreign one, got %q", body)
	}
}

func TestEditSubmit_UpdatesReviewAndRecalculatesAverage(t *testing.T) {
	f := newMutationFixture(t)
	f.addOtherReview(t, 8)

	// Both reviews at 8 → average 4,0; dropping this one to 2 → (2+8)/2/2 = 2,5
	form := url.Values{"rating": {"2"}, "comment": {"Mudei de ideia."}}
	body := f.update(t, f.id(), f.owner, form).Body.String()

	stored := f.storedReview(t)
	if stored.Rating != 2 {
		t.Errorf("stored rating = %d, want 2", stored.Rating)
	}
	if stored.Comment != "Mudei de ideia." {
		t.Errorf("stored comment = %q", stored.Comment)
	}

	if !strings.Contains(body, `id="`+"reviews-section"+`"`) {
		t.Error("the response should be the refreshed reviews section")
	}
	if !strings.Contains(body, "2,5") {
		t.Errorf("the refreshed average should be 2,5, got %q", body)
	}
	if !strings.Contains(body, "Mudei de ideia.") {
		t.Error("the refreshed list should show the edited comment")
	}
}

func TestEditSubmit_RejectsCommentOverLimit(t *testing.T) {
	f := newMutationFixture(t)

	form := url.Values{"rating": {"4"}, "comment": {strings.Repeat("a", models.MaxCommentLength+1)}}
	body := f.update(t, f.id(), f.owner, form).Body.String()

	if !strings.Contains(body, models.CommentTooLongMessage) {
		t.Errorf("expected the shared length message, got %q", body)
	}
	if stored := f.storedReview(t); stored.Rating != 8 || stored.Comment != "Comentário original." {
		t.Errorf("the review was modified despite the rejection: %+v", stored)
	}
}

func TestEditSubmit_RejectsMissingRating(t *testing.T) {
	f := newMutationFixture(t)

	form := url.Values{"rating": {""}, "comment": {"Sem nota."}}
	body := f.update(t, f.id(), f.owner, form).Body.String()

	if !strings.Contains(body, models.MissingRatingMessage) {
		t.Errorf("expected the rating message, got %q", body)
	}
	if !strings.Contains(body, "Sem nota.") {
		t.Error("the rejected form should keep what the visitor typed")
	}
	if stored := f.storedReview(t); stored.Rating != 8 {
		t.Errorf("stored rating = %d, want it unchanged at 8", stored.Rating)
	}
}

func TestEditSubmit_UnauthorizedForNonOwner(t *testing.T) {
	f := newMutationFixture(t)

	form := url.Values{"rating": {"1"}, "comment": {"Invadido."}}
	body := f.update(t, f.id(), uuid.NewString(), form).Body.String()

	if !strings.Contains(body, models.NotAuthorizedMessage) {
		t.Errorf("expected the generic message, got %q", body)
	}
	if stored := f.storedReview(t); stored.Rating != 8 || stored.Comment != "Comentário original." {
		t.Errorf("a non-owner changed the review: %+v", stored)
	}
}

func TestEditSubmit_ClearingCommentIsAllowed(t *testing.T) {
	f := newMutationFixture(t)

	form := url.Values{"rating": {"6"}, "comment": {"   "}}
	f.update(t, f.id(), f.owner, form)

	if stored := f.storedReview(t); stored.Comment != "" {
		t.Errorf("stored comment = %q, want it cleared", stored.Comment)
	}
}
