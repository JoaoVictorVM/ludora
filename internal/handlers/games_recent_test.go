package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JoaoVictorVM/ludora/internal/repository"
)

func getRecent(t *testing.T, pool *pgxpool.Pool, query string) *httptest.ResponseRecorder {
	t.Helper()

	handler := NewGamesRecent(repository.NewGameRepository(pool), nil)

	rec := httptest.NewRecorder()
	handler.List(rec, httptest.NewRequest(http.MethodGet, "/jogos/recentes"+query, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	return rec
}

func TestGamesRecent_ReturnsNextBatchWithButton(t *testing.T) {
	pool := migratedPool(t)
	seedReviewedGames(t, pool, 70)

	body := getRecent(t, pool, "?offset=30").Body.String()

	if got := strings.Count(body, `href="/jogos/`); got != FeedPageSize {
		t.Errorf("game cards = %d, want %d", got, FeedPageSize)
	}
	if !strings.Contains(body, `hx-get="/jogos/recentes?offset=60"`) {
		t.Error("expected the next button to point at offset=60")
	}
	// The second page must not repeat the first page's leading game.
	if strings.Contains(body, ">Jogo 69<") {
		t.Error("the second page must not repeat games from the first")
	}
}

func TestGamesRecent_OmitsButtonWhenNoMoreResults(t *testing.T) {
	pool := migratedPool(t)
	seedReviewedGames(t, pool, 45)

	body := getRecent(t, pool, "?offset=30").Body.String()

	if got := strings.Count(body, `href="/jogos/`); got != 15 {
		t.Errorf("game cards = %d, want the remaining 15", got)
	}
	if strings.Contains(body, "/jogos/recentes") {
		t.Error("the final batch must not render a Load More button")
	}
}

func TestGamesRecent_OmitsButtonWhenOffsetExhaustsCatalogExactly(t *testing.T) {
	pool := migratedPool(t)
	seedReviewedGames(t, pool, 60)

	body := getRecent(t, pool, "?offset=30").Body.String()

	if got := strings.Count(body, `href="/jogos/`); got != FeedPageSize {
		t.Errorf("game cards = %d, want %d", got, FeedPageSize)
	}
	if strings.Contains(body, "/jogos/recentes") {
		t.Error("a batch that exactly exhausts the catalog must not render a button")
	}
}

func TestGamesRecent_OffsetBeyondCatalogReturnsEmptyFragment(t *testing.T) {
	pool := migratedPool(t)
	seedReviewedGames(t, pool, 5)

	body := strings.TrimSpace(getRecent(t, pool, "?offset=500").Body.String())

	if body != "" {
		t.Errorf("expected an empty fragment past the end of the catalog, got %q", body)
	}
}

func TestGamesRecent_InvalidOffsetFallsBackToFirstPage(t *testing.T) {
	pool := migratedPool(t)
	seedReviewedGames(t, pool, 5)

	for _, query := range []string{"", "?offset=", "?offset=abc", "?offset=-10"} {
		body := getRecent(t, pool, query).Body.String()

		if got := strings.Count(body, `href="/jogos/`); got != 5 {
			t.Errorf("query %q: game cards = %d, want 5", query, got)
		}
	}
}
