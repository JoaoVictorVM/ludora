package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JoaoVictorVM/ludora/internal/middleware"
	"github.com/JoaoVictorVM/ludora/internal/models"
	"github.com/JoaoVictorVM/ludora/internal/repository"
)

type showFixture struct {
	pool    *pgxpool.Pool
	handler *GamesShow
	reviews *repository.ReviewRepository
	gameID  int64
}

func newShowFixture(t *testing.T) *showFixture {
	t.Helper()

	pool := migratedPool(t)
	gameRepo := repository.NewGameRepository(pool)
	reviewRepo := repository.NewReviewRepository(pool)

	released := time.Date(2013, time.September, 17, 0, 0, 0, 0, time.UTC)
	game, err := gameRepo.Create(context.Background(), &models.Game{
		ExternalID:     "3498",
		ExternalSource: models.SourceRAWG,
		Name:           "Grand Theft Auto V",
		CoverURL:       "https://media.rawg.io/gta5.jpg",
		ReleasedAt:     &released,
		Developer:      "Rockstar North",
		Description:    "Um jogo de mundo aberto.",
	})
	if err != nil {
		t.Fatalf("seeding game: %v", err)
	}

	return &showFixture{
		pool:    pool,
		handler: NewGamesShow(gameRepo, reviewRepo, nil),
		reviews: reviewRepo,
		gameID:  game.ID,
	}
}

func (f *showFixture) addReview(t *testing.T, reviewerUUID string, rating int16, comment string) {
	t.Helper()

	_, err := f.reviews.Create(context.Background(), &models.Review{
		GameID:       f.gameID,
		ReviewerUUID: reviewerUUID,
		Rating:       rating,
		Comment:      comment,
	})
	if err != nil {
		t.Fatalf("seeding review: %v", err)
	}
}

func (f *showFixture) show(t *testing.T, gameID string, reviewerUUID string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/jogos/"+gameID, nil)
	req.SetPathValue("id", gameID)
	if reviewerUUID != "" {
		req.AddCookie(&http.Cookie{Name: middleware.CookieName, Value: reviewerUUID})
	}

	rec := httptest.NewRecorder()
	middleware.AnonymousID(http.HandlerFunc(f.handler.Show)).ServeHTTP(rec, req)

	return rec
}

func (f *showFixture) id() string {
	return strconv.FormatInt(f.gameID, 10)
}

func TestGamesShow_DisplaysGameInfo(t *testing.T) {
	f := newShowFixture(t)
	f.addReview(t, uuid.NewString(), 9, "Muito bom.")

	rec := f.show(t, f.id(), uuid.NewString())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	for _, fragment := range []string{
		"Grand Theft Auto V",
		"https://media.rawg.io/gta5.jpg",
		"2013",
		"Rockstar North",
		"Um jogo de mundo aberto.",
		"Muito bom.",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("page is missing %q", fragment)
		}
	}
}

func TestGamesShow_DisplaysAverageAndCount(t *testing.T) {
	f := newShowFixture(t)
	for _, rating := range []int16{9, 8, 6} {
		f.addReview(t, uuid.NewString(), rating, "")
	}

	body := f.show(t, f.id(), uuid.NewString()).Body.String()

	// AVG(9,8,6)/2 rounded to one decimal = 3.8
	if !strings.Contains(body, "3,8") {
		t.Errorf("expected the average 3,8 on the page, got %q", body)
	}
	if !strings.Contains(body, "3 reviews") {
		t.Error("expected the review count on the page")
	}
}

func TestGamesShow_ListsReviewsMostRecentFirst(t *testing.T) {
	f := newShowFixture(t)
	f.addReview(t, uuid.NewString(), 2, "Review antiga.")
	f.addReview(t, uuid.NewString(), 10, "Review nova.")

	_, err := f.pool.Exec(context.Background(),
		`UPDATE reviews SET created_at = now() - interval '2 days' WHERE comment = 'Review antiga.'`)
	if err != nil {
		t.Fatalf("aging the older review: %v", err)
	}

	body := f.show(t, f.id(), uuid.NewString()).Body.String()

	newIdx := strings.Index(body, "Review nova.")
	oldIdx := strings.Index(body, "Review antiga.")
	if newIdx < 0 || oldIdx < 0 {
		t.Fatal("both reviews should be rendered")
	}
	if newIdx > oldIdx {
		t.Error("the most recent review should come first")
	}
	if !strings.Contains(body, "há 2 dias") {
		t.Error("expected the relative timestamp of the older review")
	}
}

func TestGamesShow_ShowsOwnershipControlsForMatchingCookie(t *testing.T) {
	f := newShowFixture(t)
	owner := uuid.NewString()
	f.addReview(t, owner, 9, "Minha review.")

	body := f.show(t, f.id(), owner).Body.String()

	if !strings.Contains(body, `id="review-controls-`) {
		t.Error("expected the ownership slot on the visitor's own review")
	}
}

func TestGamesShow_HidesOwnershipControlsForOtherReviewers(t *testing.T) {
	f := newShowFixture(t)
	f.addReview(t, uuid.NewString(), 9, "Review de outra pessoa.")
	f.addReview(t, uuid.NewString(), 4, "Review de mais alguém.")

	body := f.show(t, f.id(), uuid.NewString()).Body.String()

	if strings.Contains(body, `id="review-controls-`) {
		t.Error("no ownership slot should render for reviews the visitor does not own")
	}
}

func TestGamesShow_ShowsOwnershipOnlyOnOwnReviewAmongMany(t *testing.T) {
	f := newShowFixture(t)
	owner := uuid.NewString()
	f.addReview(t, uuid.NewString(), 3, "Outra pessoa.")
	f.addReview(t, owner, 9, "Minha review.")
	f.addReview(t, uuid.NewString(), 7, "Mais outra.")

	body := f.show(t, f.id(), owner).Body.String()

	if got := strings.Count(body, `id="review-controls-`); got != 1 {
		t.Errorf("ownership slots = %d, want exactly 1", got)
	}
}

func TestGamesShow_EmptyStateWhenNoReviews(t *testing.T) {
	f := newShowFixture(t)

	body := f.show(t, f.id(), uuid.NewString()).Body.String()

	if !strings.Contains(body, "ainda não tem reviews") {
		t.Errorf("expected the empty-state message, got %q", body)
	}
	if strings.Contains(body, "/ 5 ·") {
		t.Error("a game with no reviews must not render a summary line")
	}
}

func TestGamesShow_NotFoundForUnknownID(t *testing.T) {
	f := newShowFixture(t)

	for _, id := range []string{"999999", "nao-e-id"} {
		rec := f.show(t, id, uuid.NewString())
		if rec.Code != http.StatusNotFound {
			t.Errorf("id %q: status = %d, want 404", id, rec.Code)
		}
	}
}

// TestGamesShow_ReflectsReviewSubmittedThroughF04 covers the F04 → F05 hand-off:
// a review published through the submission handler must show up here and be
// counted in the average.
func TestGamesShow_ReflectsReviewSubmittedThroughF04(t *testing.T) {
	f := newShowFixture(t)
	reviewer := uuid.NewString()

	submit := NewReviewsSubmit(f.reviews, repository.NewGameRepository(f.pool), nil)
	form := "game_id=" + f.id() + "&rating=7&comment=Publicada+pela+F04"
	req := httptest.NewRequest(http.MethodPost, "/reviews", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: middleware.CookieName, Value: reviewer})

	rec := httptest.NewRecorder()
	middleware.AnonymousID(http.HandlerFunc(submit.Submit)).ServeHTTP(rec, req)
	if rec.Header().Get("HX-Redirect") == "" {
		t.Fatalf("submission failed: %q", rec.Body.String())
	}

	body := f.show(t, f.id(), reviewer).Body.String()

	if !strings.Contains(body, "Publicada pela F04") {
		t.Error("the submitted review should appear in the list")
	}
	if !strings.Contains(body, "3,5") {
		t.Error("rating 7 should be reflected as an average of 3,5 stars")
	}
	if !strings.Contains(body, `id="review-controls-`) {
		t.Error("the submitting visitor should own the review they just published")
	}
}
