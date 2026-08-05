package igdb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// tokenServer answers token requests with a sequentially numbered token, and
// counts how many times it was asked.
func tokenServer(t *testing.T, expiresIn int64) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"token-%d","expires_in":%d,"token_type":"bearer"}`, n, expiresIn)
	}))
	t.Cleanup(server.Close)

	return server, &calls
}

func newTestTokenProvider(t *testing.T, tokenURL string) *tokenProvider {
	t.Helper()

	return newTokenProvider(&http.Client{Timeout: defaultTimeout}, tokenURL, "client-id", "client-secret")
}

func TestToken_FetchesOnFirstUse(t *testing.T) {
	server, calls := tokenServer(t, 3600)
	provider := newTestTokenProvider(t, server.URL)

	token, err := provider.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "token-1" {
		t.Errorf("token = %q, want token-1", token)
	}
	if calls.Load() != 1 {
		t.Errorf("token endpoint called %d times, want 1", calls.Load())
	}
}

func TestToken_SendsClientCredentialsGrant(t *testing.T) {
	var gotID, gotSecret, gotGrant, gotMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotID = r.URL.Query().Get("client_id")
		gotSecret = r.URL.Query().Get("client_secret")
		gotGrant = r.URL.Query().Get("grant_type")
		fmt.Fprint(w, `{"access_token":"t","expires_in":3600}`)
	}))
	defer server.Close()

	if _, err := newTestTokenProvider(t, server.URL).Token(context.Background()); err != nil {
		t.Fatalf("Token: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotID != "client-id" || gotSecret != "client-secret" {
		t.Errorf("credentials = %q/%q, want client-id/client-secret", gotID, gotSecret)
	}
	if gotGrant != "client_credentials" {
		t.Errorf("grant_type = %q, want client_credentials", gotGrant)
	}
}

func TestToken_ReusesCachedToken(t *testing.T) {
	server, calls := tokenServer(t, 3600)
	provider := newTestTokenProvider(t, server.URL)

	first, err := provider.Token(context.Background())
	if err != nil {
		t.Fatalf("first Token: %v", err)
	}
	second, err := provider.Token(context.Background())
	if err != nil {
		t.Fatalf("second Token: %v", err)
	}

	if first != second {
		t.Errorf("tokens differ across calls: %q vs %q", first, second)
	}
	if calls.Load() != 1 {
		t.Errorf("token endpoint called %d times, want 1", calls.Load())
	}
}

func TestToken_RenewsBeforeExpiry(t *testing.T) {
	// A lifetime shorter than the renew margin means the cached token is always
	// considered too close to expiry to reuse.
	server, calls := tokenServer(t, int64(renewMargin.Seconds())-1)
	provider := newTestTokenProvider(t, server.URL)

	first, err := provider.Token(context.Background())
	if err != nil {
		t.Fatalf("first Token: %v", err)
	}
	second, err := provider.Token(context.Background())
	if err != nil {
		t.Fatalf("second Token: %v", err)
	}

	if first == second {
		t.Error("expected a renewed token when the cached one is inside the renew margin")
	}
	if calls.Load() != 2 {
		t.Errorf("token endpoint called %d times, want 2", calls.Load())
	}
}

func TestToken_InvalidateForcesRenewal(t *testing.T) {
	server, calls := tokenServer(t, 3600)
	provider := newTestTokenProvider(t, server.URL)

	first, _ := provider.Token(context.Background())
	provider.Invalidate()
	second, err := provider.Token(context.Background())
	if err != nil {
		t.Fatalf("Token after Invalidate: %v", err)
	}

	if first == second {
		t.Error("Invalidate should force a new token")
	}
	if calls.Load() != 2 {
		t.Errorf("token endpoint called %d times, want 2", calls.Load())
	}
}

func TestToken_ConcurrentCallersShareOneFetch(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		// A slow response widens the window in which a second caller could
		// wrongly start its own fetch.
		time.Sleep(50 * time.Millisecond)
		fmt.Fprint(w, `{"access_token":"shared","expires_in":3600}`)
	}))
	defer server.Close()

	provider := newTestTokenProvider(t, server.URL)

	const callers = 10
	var wg sync.WaitGroup
	tokens := make([]string, callers)
	errs := make([]error, callers)
	start := make(chan struct{})

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			tokens[i], errs[i] = provider.Token(context.Background())
		}(i)
	}

	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: %v", i, err)
		}
		if tokens[i] != "shared" {
			t.Fatalf("caller %d got %q, want the shared token", i, tokens[i])
		}
	}
	if calls.Load() != 1 {
		t.Errorf("token endpoint called %d times, want 1", calls.Load())
	}
}

func TestToken_RejectedCredentialsClassifyAsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	_, err := newTestTokenProvider(t, server.URL).Token(context.Background())
	if err == nil {
		t.Fatal("expected an error for rejected credentials")
	}
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("error %v should classify as ErrUnavailable", err)
	}
}

func TestToken_ErrorNeverLeaksCredentials(t *testing.T) {
	// The secret travels in the query string, which is exactly what *url.Error
	// embeds in its message on a transport failure.
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	unreachable := server.URL
	server.Close()

	provider := newTokenProvider(&http.Client{Timeout: 100 * time.Millisecond},
		unreachable, "public-id", "super-secret-value")

	_, err := provider.Token(context.Background())
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if got := err.Error(); contains(got, "super-secret-value") {
		t.Errorf("error message leaks the client secret: %q", got)
	}
}
