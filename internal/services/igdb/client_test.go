package igdb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}

// gamesServer stands in for IGDB's /games endpoint, recording the last request
// it received so tests can assert on the Apicalypse body and the headers.
type gamesServer struct {
	server    *httptest.Server
	lastBody  string
	headers   http.Header
	callCount atomic.Int64
}

func newGamesServer(t *testing.T, handler func(w http.ResponseWriter, body string, call int64)) *gamesServer {
	t.Helper()

	gs := &gamesServer{}
	gs.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/oauth2/token") {
			fmt.Fprint(w, `{"access_token":"test-token","expires_in":3600}`)
			return
		}

		raw, _ := io.ReadAll(r.Body)
		gs.lastBody = string(raw)
		gs.headers = r.Header.Clone()
		handler(w, gs.lastBody, gs.callCount.Add(1))
	}))
	t.Cleanup(gs.server.Close)

	return gs
}

func (gs *gamesServer) client() *Client {
	return NewClient("client-id", "client-secret",
		WithBaseURL(gs.server.URL),
		WithTokenURL(gs.server.URL+"/oauth2/token"))
}

func respondJSON(payload string) func(http.ResponseWriter, string, int64) {
	return func(w http.ResponseWriter, _ string, _ int64) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, payload)
	}
}

func TestSearchGames_Success(t *testing.T) {
	gs := newGamesServer(t, respondJSON(`[
		{"id":1029,"name":"Breath of the Wild","cover":{"image_id":"co3p2d"},"first_release_date":1488499200},
		{"id":1030,"name":"Majora's Mask"}
	]`))

	results, err := gs.client().SearchGames(context.Background(), "zelda")
	if err != nil {
		t.Fatalf("SearchGames: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	first := results[0]
	if first.ExternalID != 1029 {
		t.Errorf("ExternalID = %d, want 1029", first.ExternalID)
	}
	if first.Name != "Breath of the Wild" {
		t.Errorf("Name = %q", first.Name)
	}
	want := "https://images.igdb.com/igdb/image/upload/t_cover_big/co3p2d.jpg"
	if first.CoverURL != want {
		t.Errorf("CoverURL = %q, want %q", first.CoverURL, want)
	}
	if first.ReleaseYear != 2017 {
		t.Errorf("ReleaseYear = %d, want 2017", first.ReleaseYear)
	}
}

func TestSearchGames_SendsApicalypseQuery(t *testing.T) {
	gs := newGamesServer(t, respondJSON(`[]`))

	if _, err := gs.client().SearchGames(context.Background(), "zelda"); err != nil {
		t.Fatalf("SearchGames: %v", err)
	}

	for _, fragment := range []string{`search "zelda";`, "fields id,name,cover.image_id,first_release_date;", "limit 10;"} {
		if !strings.Contains(gs.lastBody, fragment) {
			t.Errorf("request body %q is missing %q", gs.lastBody, fragment)
		}
	}
	if got := gs.headers.Get("Client-ID"); got != "client-id" {
		t.Errorf("Client-ID header = %q, want client-id", got)
	}
	if got := gs.headers.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization header = %q, want the bearer token", got)
	}
	if got := gs.headers.Get("User-Agent"); got != "ludora/1.0" {
		t.Errorf("User-Agent = %q, want ludora/1.0", got)
	}
}

func TestSearchGames_EscapesQuotesInQuery(t *testing.T) {
	gs := newGamesServer(t, respondJSON(`[]`))

	if _, err := gs.client().SearchGames(context.Background(), `uncharted "drake"`); err != nil {
		t.Fatalf("SearchGames: %v", err)
	}

	if !strings.Contains(gs.lastBody, `search "uncharted \"drake\"";`) {
		t.Errorf("quotes were not escaped in the query body: %q", gs.lastBody)
	}
	// The body must still end with the terminator that follows the search clause.
	if !strings.Contains(gs.lastBody, "; fields ") {
		t.Errorf("escaping broke the query structure: %q", gs.lastBody)
	}
}

func TestSearchGames_MissingCoverAndDateAreTolerated(t *testing.T) {
	gs := newGamesServer(t, respondJSON(`[{"id":1030,"name":"Majora's Mask"}]`))

	results, err := gs.client().SearchGames(context.Background(), "zelda")
	if err != nil {
		t.Fatalf("SearchGames: %v", err)
	}
	if results[0].CoverURL != "" {
		t.Errorf("CoverURL = %q, want empty when the cover is absent", results[0].CoverURL)
	}
	if results[0].ReleaseYear != 0 {
		t.Errorf("ReleaseYear = %d, want 0 when the date is absent", results[0].ReleaseYear)
	}
}

func TestSearchGames_Timeout(t *testing.T) {
	release := make(chan struct{})
	gs := newGamesServer(t, func(w http.ResponseWriter, _ string, _ int64) {
		<-release
	})
	defer close(release)

	client := NewClient("client-id", "client-secret",
		WithBaseURL(gs.server.URL),
		WithTokenURL(gs.server.URL+"/oauth2/token"),
		WithTimeout(100*time.Millisecond))

	start := time.Now()
	_, err := client.SearchGames(context.Background(), "zelda")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("error %v should classify as ErrUnavailable", err)
	}
	if elapsed > defaultTimeout {
		t.Errorf("call took %v, want it bounded well under the %v budget", elapsed, defaultTimeout)
	}
}

func TestSearchGames_NonOKStatus(t *testing.T) {
	tests := []struct {
		name            string
		status          int
		wantUnavailable bool
	}{
		{name: "bad request", status: http.StatusBadRequest, wantUnavailable: false},
		{name: "rate limited", status: http.StatusTooManyRequests, wantUnavailable: true},
		{name: "server error", status: http.StatusBadGateway, wantUnavailable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := newGamesServer(t, func(w http.ResponseWriter, _ string, _ int64) {
				w.WriteHeader(tt.status)
			})

			_, err := gs.client().SearchGames(context.Background(), "zelda")

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error %v should be an *APIError", err)
			}
			if apiErr.StatusCode != tt.status {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tt.status)
			}
			if got := errors.Is(err, ErrUnavailable); got != tt.wantUnavailable {
				t.Errorf("errors.Is(err, ErrUnavailable) = %t, want %t", got, tt.wantUnavailable)
			}
		})
	}
}

func TestSearchGames_RetriesOnceAfterUnauthorized(t *testing.T) {
	gs := newGamesServer(t, func(w http.ResponseWriter, _ string, call int64) {
		if call == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `[{"id":1029,"name":"Breath of the Wild"}]`)
	})

	results, err := gs.client().SearchGames(context.Background(), "zelda")
	if err != nil {
		t.Fatalf("SearchGames: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results, want the retry to succeed with 1", len(results))
	}
	if gs.callCount.Load() != 2 {
		t.Errorf("games endpoint called %d times, want exactly 2 (original plus one retry)", gs.callCount.Load())
	}
}

func TestSearchGames_DoesNotRetryForever(t *testing.T) {
	gs := newGamesServer(t, func(w http.ResponseWriter, _ string, _ int64) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	if _, err := gs.client().SearchGames(context.Background(), "zelda"); err == nil {
		t.Fatal("expected an error when the token stays rejected")
	}
	if gs.callCount.Load() != 2 {
		t.Errorf("games endpoint called %d times, want the retry capped at 2", gs.callCount.Load())
	}
}

func TestSearchGames_ErrorNeverLeaksCredentials(t *testing.T) {
	const secret = "super-secret-value"

	t.Run("unexpected status", func(t *testing.T) {
		gs := newGamesServer(t, func(w http.ResponseWriter, _ string, _ int64) {
			w.WriteHeader(http.StatusTeapot)
		})
		client := NewClient("client-id", secret,
			WithBaseURL(gs.server.URL), WithTokenURL(gs.server.URL+"/oauth2/token"))

		_, err := client.SearchGames(context.Background(), "zelda")
		if err == nil {
			t.Fatal("expected an error")
		}
		if got := err.Error(); contains(got, secret) || contains(got, "test-token") {
			t.Errorf("error message leaks a credential: %q", got)
		}
	})

	t.Run("transport failure", func(t *testing.T) {
		gs := newGamesServer(t, respondJSON(`[]`))
		unreachable := gs.server.URL
		tokenURL := gs.server.URL + "/oauth2/token"
		gs.server.Close()

		client := NewClient("client-id", secret,
			WithBaseURL(unreachable), WithTokenURL(tokenURL), WithTimeout(100*time.Millisecond))

		_, err := client.SearchGames(context.Background(), "zelda")
		if err == nil {
			t.Fatal("expected an error")
		}
		if got := err.Error(); contains(got, secret) {
			t.Errorf("error message leaks the client secret: %q", got)
		}
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("transport failure %v should classify as ErrUnavailable", err)
		}
	})
}

func TestGetGameDetails_Success(t *testing.T) {
	gs := newGamesServer(t, respondJSON(`[{
		"id":1029,
		"name":"Breath of the Wild",
		"summary":"Step into a world of discovery.",
		"cover":{"image_id":"co3p2d"},
		"first_release_date":1488499200,
		"involved_companies":[
			{"developer":true,"company":{"name":"Nintendo EPD"}},
			{"developer":false,"company":{"name":"Nintendo"}}
		]
	}]`))

	details, err := gs.client().GetGameDetails(context.Background(), "1029")
	if err != nil {
		t.Fatalf("GetGameDetails: %v", err)
	}

	if !strings.Contains(gs.lastBody, "where id = 1029;") {
		t.Errorf("request body %q should filter by id", gs.lastBody)
	}
	if details.Name != "Breath of the Wild" {
		t.Errorf("Name = %q", details.Name)
	}
	if details.Description != "Step into a world of discovery." {
		t.Errorf("Description = %q, want the summary", details.Description)
	}
	if details.ReleasedAt == nil || details.ReleasedAt.Format(time.DateOnly) != "2017-03-03" {
		t.Errorf("ReleasedAt = %v, want 2017-03-03", details.ReleasedAt)
	}
}

func TestGetGameDetails_KeepsOnlyDeveloperCompanies(t *testing.T) {
	gs := newGamesServer(t, respondJSON(`[{
		"id":1029,
		"name":"Breath of the Wild",
		"involved_companies":[
			{"developer":false,"company":{"name":"Publisher Co"}},
			{"developer":true,"company":{"name":"Studio A"}},
			{"developer":true,"company":{"name":"Studio B"}}
		]
	}]`))

	details, err := gs.client().GetGameDetails(context.Background(), "1029")
	if err != nil {
		t.Fatalf("GetGameDetails: %v", err)
	}

	if details.Developer != "Studio A, Studio B" {
		t.Errorf("Developer = %q, want only the developer-flagged companies", details.Developer)
	}
}

func TestGetGameDetails_MapsSummaryAsPlainText(t *testing.T) {
	gs := newGamesServer(t, respondJSON(`[{"id":1029,"name":"X","summary":"Plain text, no markup."}]`))

	details, err := gs.client().GetGameDetails(context.Background(), "1029")
	if err != nil {
		t.Fatalf("GetGameDetails: %v", err)
	}
	if strings.ContainsAny(details.Description, "<>") {
		t.Errorf("Description carries markup: %q", details.Description)
	}
	if details.Developer != "" {
		t.Errorf("Developer = %q, want empty when no companies are listed", details.Developer)
	}
	if details.ReleasedAt != nil {
		t.Errorf("ReleasedAt = %v, want nil when the date is absent", details.ReleasedAt)
	}
}

func TestGetGameDetails_NotFoundReturnsEmptyResult(t *testing.T) {
	gs := newGamesServer(t, respondJSON(`[]`))

	_, err := gs.client().GetGameDetails(context.Background(), "999999")
	if !errors.Is(err, ErrGameNotFound) {
		t.Fatalf("error = %v, want ErrGameNotFound", err)
	}
}

func TestGetGameDetails_RejectsNonNumericID(t *testing.T) {
	gs := newGamesServer(t, respondJSON(`[]`))

	_, err := gs.client().GetGameDetails(context.Background(), "nao-e-id")
	if !errors.Is(err, ErrGameNotFound) {
		t.Fatalf("error = %v, want ErrGameNotFound", err)
	}
	if gs.callCount.Load() != 0 {
		t.Error("a malformed id must not reach the provider")
	}
}
