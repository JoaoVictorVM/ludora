package handlers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JoaoVictorVM/ludora/internal/middleware"
	"github.com/JoaoVictorVM/ludora/internal/models"
	"github.com/JoaoVictorVM/ludora/internal/repository"
)

// seedReviewedGames creates n reviewed games with spaced review timestamps, so
// the feed ordering under test is deterministic.
func seedReviewedGames(t *testing.T, pool *pgxpool.Pool, n int) {
	t.Helper()

	ctx := context.Background()
	gameRepo := repository.NewGameRepository(pool)
	reviewRepo := repository.NewReviewRepository(pool)

	for i := 0; i < n; i++ {
		game, err := gameRepo.Create(ctx, &models.Game{
			ExternalID:     strconv.Itoa(1000 + i),
			ExternalSource: models.SourceRAWG,
			Name:           fmt.Sprintf("Jogo %02d", i),
		})
		if err != nil {
			t.Fatalf("seeding game %d: %v", i, err)
		}

		review, err := reviewRepo.Create(ctx, &models.Review{
			GameID:       game.ID,
			ReviewerUUID: uuid.NewString(),
			Rating:       int16(i%10 + 1),
		})
		if err != nil {
			t.Fatalf("seeding review %d: %v", i, err)
		}

		_, err = pool.Exec(ctx,
			`UPDATE reviews SET created_at = now() - make_interval(mins => $1::int) WHERE id = $2`,
			n-i, review.ID)
		if err != nil {
			t.Fatalf("aging review %d: %v", i, err)
		}
	}
}

func getHome(t *testing.T, pool *pgxpool.Pool) *httptest.ResponseRecorder {
	t.Helper()

	handler := NewHome(repository.NewGameRepository(pool), nil)

	rec := httptest.NewRecorder()
	middleware.AnonymousID(http.HandlerFunc(handler.Show)).
		ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	return rec
}

func TestHome_ShowsInitialThirtyGames(t *testing.T) {
	pool := migratedPool(t)
	seedReviewedGames(t, pool, 40)

	body := getHome(t, pool).Body.String()

	if got := strings.Count(body, `href="/jogos/`); got != FeedPageSize {
		t.Errorf("game cards = %d, want %d", got, FeedPageSize)
	}
	if !strings.Contains(body, `hx-get="/jogos/recentes?offset=30"`) {
		t.Error("expected a Load More button pointing at offset=30")
	}
	// The newest seeded game must lead the grid.
	if !strings.Contains(body, "Jogo 39") {
		t.Error("the most recently reviewed game should be on the first page")
	}
	if strings.Contains(body, "Jogo 00") {
		t.Error("the oldest game must not fit on the first page of 30")
	}
}

func TestHome_OmitsLoadMoreWhenCatalogFitsOnePage(t *testing.T) {
	pool := migratedPool(t)
	seedReviewedGames(t, pool, FeedPageSize)

	body := getHome(t, pool).Body.String()

	if got := strings.Count(body, `href="/jogos/`); got != FeedPageSize {
		t.Errorf("game cards = %d, want %d", got, FeedPageSize)
	}
	if strings.Contains(body, "/jogos/recentes") {
		t.Error("a catalog that exactly fills one page must not render a Load More button")
	}
}

func TestHome_EmptyCatalogShowsMessage(t *testing.T) {
	pool := migratedPool(t)

	body := getHome(t, pool).Body.String()

	if !strings.Contains(body, "Nenhum jogo foi avaliado ainda. Seja o primeiro!") {
		t.Errorf("expected the empty-catalog message, got %q", body)
	}
	if strings.Contains(body, `id="game-grid"`) {
		t.Error("no grid should render for an empty catalog")
	}
	if !strings.Contains(body, `hx-get="/jogos/buscar"`) {
		t.Error("the search field must stay available so the visitor can be the first")
	}
}

// TestHome_ReviewFromF04MovesGameToTop covers the F04 → F06 hand-off: publishing
// a review must pull its game to the front of the feed.
func TestHome_ReviewFromF04MovesGameToTop(t *testing.T) {
	pool := migratedPool(t)
	seedReviewedGames(t, pool, 3)

	gameRepo := repository.NewGameRepository(pool)
	oldest, err := gameRepo.FindByExternalID(context.Background(), models.SourceRAWG, "1000")
	if err != nil {
		t.Fatalf("finding the oldest game: %v", err)
	}

	before := getHome(t, pool).Body.String()
	if strings.Index(before, "Jogo 00") < strings.Index(before, "Jogo 02") {
		t.Fatal("precondition failed: the oldest game should start at the bottom")
	}

	submit := NewReviewsSubmit(repository.NewReviewRepository(pool), gameRepo, nil)
	form := "game_id=" + strconv.FormatInt(oldest.ID, 10) + "&rating=8"
	req := httptest.NewRequest(http.MethodPost, "/reviews", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: middleware.CookieName, Value: uuid.NewString()})

	rec := httptest.NewRecorder()
	middleware.AnonymousID(http.HandlerFunc(submit.Submit)).ServeHTTP(rec, req)
	if rec.Header().Get("HX-Redirect") == "" {
		t.Fatalf("submission failed: %q", rec.Body.String())
	}

	after := getHome(t, pool).Body.String()
	if strings.Index(after, "Jogo 00") > strings.Index(after, "Jogo 02") {
		t.Error("the freshly reviewed game should have moved to the top of the feed")
	}
}
