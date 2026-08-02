// Package rawg wraps the RAWG API calls Ludora depends on.
package rawg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.rawg.io/api"

	userAgent      = "ludora/1.0"
	defaultTimeout = 5 * time.Second
	searchPageSize = 10
)

// ErrUnavailable marks failures where RAWG could not be reached or answered with
// a server-side error — the cases the visitor sees as "try again in a moment".
var ErrUnavailable = errors.New("rawg: service unavailable")

// APIError carries an unexpected HTTP status returned by RAWG.
type APIError struct {
	StatusCode int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("rawg: unexpected status %d", e.StatusCode)
}

// Unwrap lets callers treat upstream 5xx as an availability problem while 4xx
// stays a distinct, loggable condition.
func (e *APIError) Unwrap() error {
	if e.StatusCode >= http.StatusInternalServerError {
		return ErrUnavailable
	}
	return nil
}

// SearchResult is one game returned by RAWG's search endpoint. It intentionally
// carries only what a result card renders — full details are fetched separately.
type SearchResult struct {
	ExternalID  int
	Name        string
	CoverURL    string
	ReleaseYear int
}

type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
}

type Option func(*Client)

// WithBaseURL points the client at a different host, used by tests.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimSuffix(baseURL, "/")
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

func NewClient(apiKey string, opts ...Option) *Client {
	client := &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    DefaultBaseURL,
		apiKey:     apiKey,
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

type searchResponse struct {
	Results []struct {
		ID              int    `json:"id"`
		Name            string `json:"name"`
		BackgroundImage string `json:"background_image"`
		Released        string `json:"released"`
	} `json:"results"`
}

// SearchGames returns the top matches for query.
func (c *Client) SearchGames(ctx context.Context, query string) ([]SearchResult, error) {
	params := url.Values{}
	params.Set("search", query)
	params.Set("page_size", strconv.Itoa(searchPageSize))
	if c.apiKey != "" {
		params.Set("key", c.apiKey)
	}

	endpoint := c.baseURL + "/games?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("rawg: building search request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	res, err := c.httpClient.Do(req)
	if err != nil {
		// net/http wraps transport failures in *url.Error, whose message embeds
		// the full request URL — and the URL carries the API key. Only the inner
		// cause is propagated so the key can never reach a log line.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, fmt.Errorf("rawg: searching games: %w", errors.Join(err, ErrUnavailable))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, &APIError{StatusCode: res.StatusCode}
	}

	var payload searchResponse
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("rawg: decoding search response: %w", err)
	}

	results := make([]SearchResult, 0, len(payload.Results))
	for _, item := range payload.Results {
		results = append(results, SearchResult{
			ExternalID:  item.ID,
			Name:        item.Name,
			CoverURL:    item.BackgroundImage,
			ReleaseYear: releaseYear(item.Released),
		})
	}

	return results, nil
}

// releaseYear extracts the year from RAWG's "YYYY-MM-DD" date, returning 0 when
// the game has no known release date.
func releaseYear(released string) int {
	year, _, found := strings.Cut(released, "-")
	if !found {
		return 0
	}

	parsed, err := strconv.Atoi(year)
	if err != nil {
		return 0
	}

	return parsed
}
