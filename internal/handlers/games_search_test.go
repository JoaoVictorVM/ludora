package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JoaoVictorVM/ludora/internal/models"
	"github.com/JoaoVictorVM/ludora/internal/services/igdb"
)

type stubSearcher struct {
	results  []models.GameSearchResult
	err      error
	calls    int
	gotQuery string
}

func (s *stubSearcher) SearchGames(_ context.Context, query string) ([]models.GameSearchResult, error) {
	s.calls++
	s.gotQuery = query
	return s.results, s.err
}

// captureLogger returns a logger writing JSON into buf, so tests can assert on
// what the handler actually logged.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

func doSearch(t *testing.T, handler *GamesSearch, query string) *httptest.ResponseRecorder {
	t.Helper()

	rec := httptest.NewRecorder()
	handler.Search(rec, httptest.NewRequest(http.MethodGet, "/jogos/buscar?q="+query, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	return rec
}

func TestSearchHandler_ReturnsResultCards(t *testing.T) {
	searcher := &stubSearcher{results: []models.GameSearchResult{
		{ExternalID: 3498, Name: "Grand Theft Auto V", CoverURL: "https://images.igdb.com/igdb/image/upload/t_cover_big/co1r7f.jpg", ReleaseYear: 2013},
		{ExternalID: 4200, Name: "Portal 2", CoverURL: "https://images.igdb.com/igdb/image/upload/t_cover_big/co1rc5.jpg", ReleaseYear: 2011},
	}}

	body := doSearch(t, NewGamesSearch(searcher, nil), "gta").Body.String()

	if searcher.gotQuery != "gta" {
		t.Errorf("client received query %q, want gta", searcher.gotQuery)
	}

	for _, fragment := range []string{
		"Grand Theft Auto V",
		"https://images.igdb.com/igdb/image/upload/t_cover_big/co1r7f.jpg",
		"2013",
		`hx-get="/jogos/3498/formulario"`,
		`hx-target="#form-area"`,
		"Portal 2",
		"2011",
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("response is missing %q", fragment)
		}
	}
}

func TestSearchHandler_EmptyResults(t *testing.T) {
	searcher := &stubSearcher{results: nil}

	body := doSearch(t, NewGamesSearch(searcher, nil), "zzzzz").Body.String()

	if !strings.Contains(body, "Nenhum jogo encontrado para &#39;zzzzz&#39;. Tente outra busca.") {
		t.Errorf("expected the empty-state message, got %q", body)
	}
}

func TestSearchHandler_ShortQuery(t *testing.T) {
	searcher := &stubSearcher{}

	body := doSearch(t, NewGamesSearch(searcher, nil), "g").Body.String()

	if body != "" {
		t.Errorf("expected an empty fragment, got %q", body)
	}
	if searcher.calls != 0 {
		t.Errorf("IGDB client was called %d times, want 0", searcher.calls)
	}
}

func TestSearchHandler_TrimsWhitespaceQuery(t *testing.T) {
	searcher := &stubSearcher{}

	doSearch(t, NewGamesSearch(searcher, nil), "%20%20")

	if searcher.calls != 0 {
		t.Errorf("a whitespace-only query should not reach the provider, got %d calls", searcher.calls)
	}
}

func TestSearchHandler_ProviderTimeout(t *testing.T) {
	searcher := &stubSearcher{err: errors.Join(errors.New("context deadline exceeded"), igdb.ErrUnavailable)}

	body := doSearch(t, NewGamesSearch(searcher, nil), "gta").Body.String()

	if !strings.Contains(body, "Não foi possível buscar jogos agora. Tente novamente em instantes.") {
		t.Errorf("expected the unavailable fallback, got %q", body)
	}
}

func TestSearchHandler_ProviderClientError(t *testing.T) {
	var logs bytes.Buffer
	searcher := &stubSearcher{err: &igdb.APIError{StatusCode: http.StatusBadRequest}}

	body := doSearch(t, NewGamesSearch(searcher, captureLogger(&logs)), "gta").Body.String()

	if !strings.Contains(body, "Algo deu errado com essa busca.") {
		t.Errorf("expected the generic failure message, got %q", body)
	}

	var entry struct {
		Query  string `json:"query"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(logs.String())), &entry); err != nil {
		t.Fatalf("parsing log entry %q: %v", logs.String(), err)
	}
	if entry.Query != "gta" {
		t.Errorf("logged query = %q, want gta", entry.Query)
	}
	if entry.Status != http.StatusBadRequest {
		t.Errorf("logged status = %d, want 400", entry.Status)
	}
}

func TestSearchHandler_ProviderServerErrorShowsUnavailable(t *testing.T) {
	searcher := &stubSearcher{err: &igdb.APIError{StatusCode: http.StatusBadGateway}}

	body := doSearch(t, NewGamesSearch(searcher, nil), "gta").Body.String()

	if !strings.Contains(body, "Não foi possível buscar jogos agora.") {
		t.Errorf("a 5xx should render the unavailable fallback, got %q", body)
	}
}
