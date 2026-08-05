package igdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

const (
	// DefaultTokenURL is Twitch's OAuth endpoint: IGDB delegates authentication
	// to its parent platform.
	DefaultTokenURL = "https://id.twitch.tv/oauth2/token"

	// renewMargin renews the token slightly before it actually expires, so a
	// request never races the expiry boundary.
	renewMargin = 5 * time.Minute
)

// tokenProvider obtains and caches the application access token. IGDB tokens
// last around two months, so fetching one per request would be wasteful; the
// cache is guarded by a mutex held across the fetch, which means concurrent
// callers on a cold cache produce exactly one round trip to Twitch.
type tokenProvider struct {
	httpClient   *http.Client
	tokenURL     string
	clientID     string
	clientSecret string

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	TokenType   string `json:"token_type"`
}

func newTokenProvider(httpClient *http.Client, tokenURL, clientID, clientSecret string) *tokenProvider {
	return &tokenProvider{
		httpClient:   httpClient,
		tokenURL:     tokenURL,
		clientID:     clientID,
		clientSecret: clientSecret,
	}
}

// Token returns a valid access token, fetching a new one when the cache is empty
// or close to expiring.
func (p *tokenProvider) Token(ctx context.Context) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.token != "" && time.Now().Add(renewMargin).Before(p.expiresAt) {
		return p.token, nil
	}

	return p.fetch(ctx)
}

// Invalidate drops the cached token so the next call fetches a fresh one. It is
// used when IGDB rejects a token that had not reached its stated expiry — a
// revoked credential looks valid to the cache until the provider says otherwise.
func (p *tokenProvider) Invalidate() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.token = ""
	p.expiresAt = time.Time{}
}

// fetch must be called with the mutex held.
func (p *tokenProvider) fetch(ctx context.Context) (string, error) {
	params := url.Values{}
	params.Set("client_id", p.clientID)
	params.Set("client_secret", p.clientSecret)
	params.Set("grant_type", "client_credentials")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.tokenURL+"?"+params.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("igdb: building token request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	res, err := p.httpClient.Do(req)
	if err != nil {
		// *url.Error embeds the full request URL, and here the query string
		// carries the client secret. Only the inner cause is propagated.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return "", fmt.Errorf("igdb: requesting access token: %w", errors.Join(err, ErrUnavailable))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		// Rejected credentials are an availability problem from the visitor's
		// point of view: there is nothing they can do, and the search simply
		// cannot be served.
		return "", fmt.Errorf("igdb: access token request returned %d: %w", res.StatusCode, ErrUnavailable)
	}

	var payload tokenResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("igdb: decoding token response: %w", err)
	}
	if payload.AccessToken == "" {
		return "", fmt.Errorf("igdb: token response carried no access token: %w", ErrUnavailable)
	}

	p.token = payload.AccessToken
	p.expiresAt = time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second)

	return p.token, nil
}
