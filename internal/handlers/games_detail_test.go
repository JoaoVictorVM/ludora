package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JoaoVictorVM/ludora/internal/database"
	"github.com/JoaoVictorVM/ludora/internal/models"
	"github.com/JoaoVictorVM/ludora/internal/repository"
	"github.com/JoaoVictorVM/ludora/internal/services/rawg"
	"github.com/JoaoVictorVM/ludora/internal/testutil"
)

type stubDetailsFetcher struct {
	details *models.GameDetails
	err     error
	calls   int
	gotID   string
}

func (s *stubDetailsFetcher) GetGameDetails(_ context.Context, externalID string) (*models.GameDetails, error) {
	s.calls++
	s.gotID = externalID
	return s.details, s.err
}

func gtaDetails() *models.GameDetails {
	released := time.Date(2013, time.September, 17, 0, 0, 0, 0, time.UTC)

	return &models.GameDetails{
		ExternalID:  3498,
		Name:        "Grand Theft Auto V",
		CoverURL:    "https://media.rawg.io/gta5.jpg",
		ReleasedAt:  &released,
		Developer:   "Rockstar North",
		Description: "An open world action-adventure game.",
	}
}

func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool := testutil.NewPostgresPool(t)
	if err := database.Migrate(context.Background(), pool, nil); err != nil {
		t.Fatalf("applying migrations: %v", err)
	}

	return pool
}

func requestForm(t *testing.T, handler *GamesDetail, externalID string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/jogos/"+externalID+"/formulario", nil)
	req.SetPathValue("external_id", externalID)

	rec := httptest.NewRecorder()
	handler.Form(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	return rec
}

func countGames(t *testing.T, pool *pgxpool.Pool, externalID string) int {
	t.Helper()

	var count int
	err := pool.QueryRow(context.Background(),
		"SELECT count(*) FROM games WHERE external_id = $1", externalID).Scan(&count)
	if err != nil {
		t.Fatalf("counting games: %v", err)
	}

	return count
}

func TestGamesDetailHandler_FirstSelection_FetchesAndCaches(t *testing.T) {
	pool := migratedPool(t)
	repo := repository.NewGameRepository(pool)
	fetcher := &stubDetailsFetcher{details: gtaDetails()}

	body := requestForm(t, NewGamesDetail(repo, fetcher, nil), "3498").Body.String()

	if fetcher.calls != 1 {
		t.Errorf("RAWG detail client called %d times, want 1", fetcher.calls)
	}
	if fetcher.gotID != "3498" {
		t.Errorf("client received external id %q, want 3498", fetcher.gotID)
	}
	if got := countGames(t, pool, "3498"); got != 1 {
		t.Errorf("games rows = %d, want 1", got)
	}

	cached, err := repo.FindByExternalID(context.Background(), models.SourceRAWG, "3498")
	if err != nil {
		t.Fatalf("FindByExternalID: %v", err)
	}
	if cached.Description != "An open world action-adventure game." {
		t.Errorf("cached Description = %q", cached.Description)
	}
	if cached.Developer != "Rockstar North" {
		t.Errorf("cached Developer = %q", cached.Developer)
	}

	for _, fragment := range []string{
		"Grand Theft Auto V",
		`name="game_id"`,
		`value="` + strconv.FormatInt(cached.ID, 10) + `"`,
		"2013",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("form shell is missing %q", fragment)
		}
	}
}

func TestGamesDetailHandler_CachedSelection_SkipsRawgCall(t *testing.T) {
	pool := migratedPool(t)
	repo := repository.NewGameRepository(pool)
	fetcher := &stubDetailsFetcher{details: gtaDetails()}
	handler := NewGamesDetail(repo, fetcher, nil)

	requestForm(t, handler, "3498")
	requestForm(t, handler, "3498")

	if fetcher.calls != 1 {
		t.Errorf("RAWG detail client called %d times across two selections, want 1", fetcher.calls)
	}
	if got := countGames(t, pool, "3498"); got != 1 {
		t.Errorf("games rows = %d, want 1", got)
	}
}

func TestGamesDetailHandler_RawgFailure_ShowsErrorFragment(t *testing.T) {
	pool := migratedPool(t)
	repo := repository.NewGameRepository(pool)
	fetcher := &stubDetailsFetcher{err: errors.Join(errors.New("boom"), rawg.ErrUnavailable)}

	body := requestForm(t, NewGamesDetail(repo, fetcher, nil), "3498").Body.String()

	if !strings.Contains(body, "Não foi possível carregar os detalhes deste jogo.") {
		t.Errorf("expected the load-error fragment, got %q", body)
	}
	if got := countGames(t, pool, "3498"); got != 0 {
		t.Errorf("a failed fetch persisted %d rows, want 0", got)
	}
}

func TestGamesDetailHandler_InvalidExternalID(t *testing.T) {
	pool := migratedPool(t)
	repo := repository.NewGameRepository(pool)
	fetcher := &stubDetailsFetcher{details: gtaDetails()}

	body := requestForm(t, NewGamesDetail(repo, fetcher, nil), "nao-e-um-id").Body.String()

	if !strings.Contains(body, "Não foi possível carregar os detalhes deste jogo.") {
		t.Errorf("expected the load-error fragment, got %q", body)
	}
	if fetcher.calls != 0 {
		t.Errorf("RAWG was called %d times for a malformed id, want 0", fetcher.calls)
	}
}

// TestGamesDetailHandler_ResolvesSearchSelectionIntoLocalRecord covers the
// F02 → F03 hand-off: the external id carried by a search result card must end
// up as the external_id of the local row.
func TestGamesDetailHandler_ResolvesSearchSelectionIntoLocalRecord(t *testing.T) {
	pool := migratedPool(t)
	repo := repository.NewGameRepository(pool)

	card := models.GameSearchResult{ExternalID: 3498, Name: "Grand Theft Auto V"}
	fetcher := &stubDetailsFetcher{details: gtaDetails()}

	requestForm(t, NewGamesDetail(repo, fetcher, nil), strconv.Itoa(card.ExternalID))

	cached, err := repo.FindByExternalID(context.Background(), models.SourceRAWG, "3498")
	if err != nil {
		t.Fatalf("FindByExternalID: %v", err)
	}
	if cached.ExternalID != strconv.Itoa(card.ExternalID) {
		t.Errorf("cached ExternalID = %q, want %d", cached.ExternalID, card.ExternalID)
	}
	if cached.Name != card.Name {
		t.Errorf("cached Name = %q, want %q", cached.Name, card.Name)
	}
}
