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

type submitFixture struct {
	pool         *pgxpool.Pool
	handler      *ReviewsSubmit
	gameID       int64
	reviewerUUID string
}

func newSubmitFixture(t *testing.T) *submitFixture {
	t.Helper()

	pool := migratedPool(t)
	gameRepo := repository.NewGameRepository(pool)

	game, err := gameRepo.Create(context.Background(), &models.Game{
		ExternalID:     "3498",
		ExternalSource: models.SourceRAWG,
		Name:           "Grand Theft Auto V",
	})
	if err != nil {
		t.Fatalf("seeding game: %v", err)
	}

	return &submitFixture{
		pool:         pool,
		handler:      NewReviewsSubmit(repository.NewReviewRepository(pool), gameRepo, nil),
		gameID:       game.ID,
		reviewerUUID: uuid.NewString(),
	}
}

func (f *submitFixture) submit(t *testing.T, rating, comment string) *httptest.ResponseRecorder {
	t.Helper()

	return f.submitAs(t, f.reviewerUUID, rating, comment)
}

func (f *submitFixture) submitAs(t *testing.T, reviewerUUID, rating, comment string) *httptest.ResponseRecorder {
	t.Helper()

	form := url.Values{}
	form.Set("game_id", strconv.FormatInt(f.gameID, 10))
	form.Set("rating", rating)
	form.Set("comment", comment)

	req := httptest.NewRequest(http.MethodPost, "/reviews", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	// The route sits behind AnonymousID in the real router, so the middleware is
	// what puts the reviewer id in context here too.
	req.AddCookie(&http.Cookie{Name: middleware.CookieName, Value: reviewerUUID})
	middleware.AnonymousID(http.HandlerFunc(f.handler.Submit)).ServeHTTP(rec, req)

	return rec
}

func (f *submitFixture) countReviews(t *testing.T) int {
	t.Helper()

	var count int
	err := f.pool.QueryRow(context.Background(),
		"SELECT count(*) FROM reviews WHERE game_id = $1", f.gameID).Scan(&count)
	if err != nil {
		t.Fatalf("counting reviews: %v", err)
	}

	return count
}

func TestSubmit_ValidRatingNoComment_Succeeds(t *testing.T) {
	f := newSubmitFixture(t)

	rec := f.submit(t, "9", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := f.countReviews(t); got != 1 {
		t.Fatalf("reviews rows = %d, want 1", got)
	}

	var rating int16
	var comment *string
	err := f.pool.QueryRow(context.Background(),
		"SELECT rating, comment FROM reviews WHERE game_id = $1", f.gameID).Scan(&rating, &comment)
	if err != nil {
		t.Fatalf("reading review: %v", err)
	}
	if rating != 9 {
		t.Errorf("rating = %d, want 9", rating)
	}
	if comment != nil {
		t.Errorf("comment = %v, want NULL", *comment)
	}
}

func TestSubmit_Success_SetsHXRedirectToGamePage(t *testing.T) {
	f := newSubmitFixture(t)

	rec := f.submit(t, "10", "Excelente.")

	want := "/jogos/" + strconv.FormatInt(f.gameID, 10)
	if got := rec.Header().Get("HX-Redirect"); got != want {
		t.Errorf("HX-Redirect = %q, want %q", got, want)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "" {
		t.Errorf("expected an empty body on success, got %q", body)
	}
}

func TestSubmit_CommentAtLimit_Succeeds(t *testing.T) {
	f := newSubmitFixture(t)

	comment := strings.Repeat("a", models.MaxCommentLength)
	rec := f.submit(t, "6", comment)

	if rec.Header().Get("HX-Redirect") == "" {
		t.Fatalf("expected a successful submission, got body %q", rec.Body.String())
	}
	if got := f.countReviews(t); got != 1 {
		t.Errorf("reviews rows = %d, want 1", got)
	}

	var length int
	err := f.pool.QueryRow(context.Background(),
		"SELECT char_length(comment) FROM reviews WHERE game_id = $1", f.gameID).Scan(&length)
	if err != nil {
		t.Fatalf("reading comment length: %v", err)
	}
	if length != models.MaxCommentLength {
		t.Errorf("stored comment length = %d, want %d", length, models.MaxCommentLength)
	}
}

func TestSubmit_CommentOverLimit_Rejected(t *testing.T) {
	f := newSubmitFixture(t)

	rec := f.submit(t, "6", strings.Repeat("a", models.MaxCommentLength+1))

	if rec.Header().Get("HX-Redirect") != "" {
		t.Error("an over-length comment must not redirect")
	}
	if !strings.Contains(rec.Body.String(), "muito longa") {
		t.Errorf("expected the length message, got %q", rec.Body.String())
	}
	if got := f.countReviews(t); got != 0 {
		t.Errorf("reviews rows = %d, want 0", got)
	}
}

func TestSubmit_MissingRating_Rejected(t *testing.T) {
	f := newSubmitFixture(t)

	rec := f.submit(t, "", "Sem nota.")

	if rec.Header().Get("HX-Redirect") != "" {
		t.Error("a submission without a rating must not redirect")
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Selecione uma nota antes de publicar.") {
		t.Errorf("expected the rating message, got %q", body)
	}
	if !strings.Contains(body, "Sem nota.") {
		t.Error("the rejected form should keep the comment the visitor typed")
	}
	if got := f.countReviews(t); got != 0 {
		t.Errorf("reviews rows = %d, want 0", got)
	}
}

func TestSubmit_RatingOutOfRange_Rejected(t *testing.T) {
	f := newSubmitFixture(t)

	for _, rating := range []string{"0", "11", "abc"} {
		rec := f.submit(t, rating, "")

		if rec.Header().Get("HX-Redirect") != "" {
			t.Errorf("rating %q must not be accepted", rating)
		}
		if got := f.countReviews(t); got != 0 {
			t.Fatalf("rating %q created %d rows, want 0", rating, got)
		}
	}
}

func TestSubmit_DuplicateReview_Rejected(t *testing.T) {
	f := newSubmitFixture(t)

	if rec := f.submit(t, "8", "Primeira."); rec.Header().Get("HX-Redirect") == "" {
		t.Fatalf("first submission failed: %q", rec.Body.String())
	}

	rec := f.submit(t, "2", "Segunda.")

	if rec.Header().Get("HX-Redirect") != "" {
		t.Error("a duplicate submission must not redirect")
	}
	if !strings.Contains(rec.Body.String(), "Você já avaliou este jogo.") {
		t.Errorf("expected the duplicate message, got %q", rec.Body.String())
	}
	if got := f.countReviews(t); got != 1 {
		t.Errorf("reviews rows = %d, want 1", got)
	}
}

func TestSubmit_DifferentVisitorsCanReviewTheSameGame(t *testing.T) {
	f := newSubmitFixture(t)

	if rec := f.submitAs(t, uuid.NewString(), "8", ""); rec.Header().Get("HX-Redirect") == "" {
		t.Fatalf("first visitor rejected: %q", rec.Body.String())
	}
	if rec := f.submitAs(t, uuid.NewString(), "4", ""); rec.Header().Get("HX-Redirect") == "" {
		t.Fatalf("second visitor rejected: %q", rec.Body.String())
	}

	if got := f.countReviews(t); got != 2 {
		t.Errorf("reviews rows = %d, want 2", got)
	}
}

// TestSubmit_PersistsIdentifierFromAnonymousCookie covers the F01 → F04 contract:
// the value stored as reviewer_uuid must be the visitor's cookie value.
func TestSubmit_PersistsIdentifierFromAnonymousCookie(t *testing.T) {
	f := newSubmitFixture(t)

	f.submit(t, "7", "")

	var stored string
	err := f.pool.QueryRow(context.Background(),
		"SELECT reviewer_uuid::text FROM reviews WHERE game_id = $1", f.gameID).Scan(&stored)
	if err != nil {
		t.Fatalf("reading reviewer_uuid: %v", err)
	}
	if stored != f.reviewerUUID {
		t.Errorf("reviewer_uuid = %q, want the cookie value %q", stored, f.reviewerUUID)
	}
}
