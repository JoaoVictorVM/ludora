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

// get issues an authenticated RAWG request and decodes the response into dest.
func (c *Client) get(ctx context.Context, path string, params url.Values, operation string, dest any) error {
	if c.apiKey != "" {
		params.Set("key", c.apiKey)
	}

	endpoint := c.baseURL + path
	if encoded := params.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("rawg: building %s request: %w", operation, err)
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
		return fmt.Errorf("rawg: %s: %w", operation, errors.Join(err, ErrUnavailable))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return &APIError{StatusCode: res.StatusCode}
	}

	if err := json.NewDecoder(res.Body).Decode(dest); err != nil {
		return fmt.Errorf("rawg: decoding %s response: %w", operation, err)
	}

	return nil
}

// SearchGames returns the top matches for query.
func (c *Client) SearchGames(ctx context.Context, query string) ([]SearchResult, error) {
	params := url.Values{}
	params.Set("search", query)
	params.Set("page_size", strconv.Itoa(searchPageSize))

	var payload searchResponse
	if err := c.get(ctx, "/games", params, "searching games", &payload); err != nil {
		return nil, err
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

// GameDetails is the enriched record fetched the first time a game is selected.
type GameDetails struct {
	ExternalID  int
	Name        string
	CoverURL    string
	ReleasedAt  *time.Time
	Developer   string
	Description string
}

type detailsResponse struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	BackgroundImage string `json:"background_image"`
	Released        string `json:"released"`
	DescriptionRaw  string `json:"description_raw"`
	Developers      []struct {
		Name string `json:"name"`
	} `json:"developers"`
}

// GetGameDetails fetches the full record for a game. The HTML `description`
// field is deliberately not mapped: Templ escapes output, so its tags would show
// up as literal text — `description_raw` is the plain-text equivalent.
func (c *Client) GetGameDetails(ctx context.Context, externalID string) (*GameDetails, error) {
	var payload detailsResponse
	if err := c.get(ctx, "/games/"+url.PathEscape(externalID), url.Values{}, "fetching game details", &payload); err != nil {
		return nil, err
	}

	developers := make([]string, 0, len(payload.Developers))
	for _, developer := range payload.Developers {
		if developer.Name != "" {
			developers = append(developers, developer.Name)
		}
	}

	return &GameDetails{
		ExternalID:  payload.ID,
		Name:        payload.Name,
		CoverURL:    payload.BackgroundImage,
		ReleasedAt:  releaseDate(payload.Released),
		Developer:   strings.Join(developers, ", "),
		Description: payload.DescriptionRaw,
	}, nil
}

// releaseDate parses RAWG's "YYYY-MM-DD" date, returning nil when the game has
// no known release date.
func releaseDate(released string) *time.Time {
	parsed, err := time.Parse(time.DateOnly, released)
	if err != nil {
		return nil
	}

	return &parsed
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
