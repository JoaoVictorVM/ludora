package router

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JoaoVictorVM/ludora/internal/handlers"
	"github.com/JoaoVictorVM/ludora/internal/middleware"
	"github.com/JoaoVictorVM/ludora/internal/repository"
	"github.com/JoaoVictorVM/ludora/internal/services/rawg"
)

func staticFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	cssDir := filepath.Join(root, "css")
	if err := os.MkdirAll(cssDir, 0o755); err != nil {
		t.Fatalf("creating css dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cssDir, "output.css"), []byte("body{color:#fff}"), 0o600); err != nil {
		t.Fatalf("writing fixture css: %v", err)
	}

	return root
}

func TestRouter_ServesStaticAsset(t *testing.T) {
	handler := New(Deps{StaticDir: staticFixture(t)})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/css/output.css", nil))

	res := rec.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	if contentType := res.Header.Get("Content-Type"); !strings.Contains(contentType, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", contentType)
	}
	if body := rec.Body.String(); body != "body{color:#fff}" {
		t.Errorf("body = %q, want the fixture contents", body)
	}
}

func TestRouter_StaticAssetNotFound(t *testing.T) {
	handler := New(Deps{StaticDir: staticFixture(t)})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/css/inexistente.css", nil))

	if rec.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Result().StatusCode)
	}
}

type stubSearcher struct {
	results []rawg.SearchResult
}

func (s *stubSearcher) SearchGames(context.Context, string) ([]rawg.SearchResult, error) {
	return s.results, nil
}

func TestRouter_RegistersGameSearchRoute(t *testing.T) {
	searcher := &stubSearcher{results: []rawg.SearchResult{
		{ExternalID: 3498, Name: "Grand Theft Auto V", ReleaseYear: 2013},
	}}
	handler := New(Deps{
		StaticDir:   staticFixture(t),
		GamesSearch: handlers.NewGamesSearch(searcher, nil),
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/jogos/buscar?q=gta", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Grand Theft Auto V") {
		t.Errorf("search route did not render results: %q", rec.Body.String())
	}
}

type stubFeed struct{}

func (stubFeed) ListRecentlyReviewed(context.Context, int, int) ([]repository.ReviewedGame, bool, error) {
	return nil, false, nil
}

func TestRouter_HomeRendersSearchField(t *testing.T) {
	handler := New(Deps{
		StaticDir: staticFixture(t),
		Home:      handlers.NewHome(stubFeed{}, nil),
	})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	body := rec.Body.String()
	for _, fragment := range []string{
		`hx-get="/jogos/buscar"`,
		`hx-trigger="keyup changed delay:300ms"`,
		`hx-target="#search-results"`,
		`id="search-results"`,
		`id="form-area"`,
		`src="/static/js/htmx.min.js"`,
	} {
		if !strings.Contains(body, fragment) {
			t.Errorf("home is missing %q", fragment)
		}
	}
}

func TestRouter_AttachesAnonymousIDMiddleware(t *testing.T) {
	handler := New(Deps{StaticDir: staticFixture(t)})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/css/output.css", nil))

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == middleware.CookieName {
			return
		}
	}

	t.Fatalf("expected the router to set the %s cookie", middleware.CookieName)
}
