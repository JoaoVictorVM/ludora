package rawg

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearchGames_Success(t *testing.T) {
	var gotQuery, gotPageSize, gotKey, gotUserAgent string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("search")
		gotPageSize = r.URL.Query().Get("page_size")
		gotKey = r.URL.Query().Get("key")
		gotUserAgent = r.Header.Get("User-Agent")

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[
			{"id":3498,"name":"Grand Theft Auto V","background_image":"https://media.rawg.io/gta5.jpg","released":"2013-09-17"},
			{"id":4200,"name":"Portal 2","background_image":"https://media.rawg.io/portal2.jpg","released":null}
		]}`))
	}))
	defer server.Close()

	client := NewClient("test-key", WithBaseURL(server.URL))

	results, err := client.SearchGames(context.Background(), "gta")
	if err != nil {
		t.Fatalf("SearchGames: %v", err)
	}

	if gotQuery != "gta" {
		t.Errorf("search param = %q, want gta", gotQuery)
	}
	if gotPageSize != "10" {
		t.Errorf("page_size = %q, want 10", gotPageSize)
	}
	if gotKey != "test-key" {
		t.Errorf("key = %q, want test-key", gotKey)
	}
	if gotUserAgent != "ludora/1.0" {
		t.Errorf("User-Agent = %q, want ludora/1.0", gotUserAgent)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	first := results[0]
	if first.ExternalID != 3498 {
		t.Errorf("ExternalID = %d, want 3498", first.ExternalID)
	}
	if first.Name != "Grand Theft Auto V" {
		t.Errorf("Name = %q", first.Name)
	}
	if first.CoverURL != "https://media.rawg.io/gta5.jpg" {
		t.Errorf("CoverURL = %q", first.CoverURL)
	}
	if first.ReleaseYear != 2013 {
		t.Errorf("ReleaseYear = %d, want 2013", first.ReleaseYear)
	}

	if results[1].ReleaseYear != 0 {
		t.Errorf("missing release date should yield year 0, got %d", results[1].ReleaseYear)
	}
}

func TestSearchGames_Timeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	client := NewClient("test-key", WithBaseURL(server.URL), WithTimeout(100*time.Millisecond))

	start := time.Now()
	_, err := client.SearchGames(context.Background(), "gta")
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
		{name: "client error", status: http.StatusBadRequest, wantUnavailable: false},
		{name: "server error", status: http.StatusBadGateway, wantUnavailable: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()

			client := NewClient("test-key", WithBaseURL(server.URL))

			_, err := client.SearchGames(context.Background(), "gta")
			if err == nil {
				t.Fatal("expected an error, got nil")
			}

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

func TestSearchGames_ErrorNeverLeaksAPIKey(t *testing.T) {
	const apiKey = "super-secret-key"

	t.Run("unexpected status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}))
		defer server.Close()

		client := NewClient(apiKey, WithBaseURL(server.URL))

		_, err := client.SearchGames(context.Background(), "gta")
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if got := err.Error(); strings.Contains(got, apiKey) {
			t.Errorf("error message leaks the API key: %q", got)
		}
	})

	// Transport failures are the dangerous path: net/http embeds the full URL,
	// query string included, in *url.Error.
	t.Run("transport failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		unreachable := server.URL
		server.Close()

		client := NewClient(apiKey, WithBaseURL(unreachable), WithTimeout(100*time.Millisecond))

		_, err := client.SearchGames(context.Background(), "gta")
		if err == nil {
			t.Fatal("expected an error, got nil")
		}
		if got := err.Error(); strings.Contains(got, apiKey) {
			t.Errorf("error message leaks the API key: %q", got)
		}
		if !errors.Is(err, ErrUnavailable) {
			t.Errorf("transport failure %v should classify as ErrUnavailable", err)
		}
	})
}
