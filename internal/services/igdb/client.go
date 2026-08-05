// Package igdb wraps the IGDB API calls Ludora depends on.
package igdb

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

	"github.com/JoaoVictorVM/ludora/internal/models"
)

const (
	DefaultBaseURL = "https://api.igdb.com/v4"

	userAgent      = "ludora/1.0"
	defaultTimeout = 5 * time.Second
	searchLimit    = 10

	// coverURLTemplate turns IGDB's image hash into a URL. t_cover_big is the
	// size that matches the card and form-shell layouts.
	coverURLTemplate = "https://images.igdb.com/igdb/image/upload/t_cover_big/%s.jpg"

	searchFields  = "id,name,cover.image_id,first_release_date"
	detailsFields = "id,name,summary,cover.image_id,first_release_date,involved_companies.developer,involved_companies.company.name"
)

// ErrUnavailable marks failures where IGDB could not be reached or answered with
// a server-side error — the cases the visitor sees as "try again in a moment".
var ErrUnavailable = errors.New("igdb: service unavailable")

// ErrGameNotFound is returned when a detail lookup matches no game.
var ErrGameNotFound = errors.New("igdb: game not found")

// APIError carries an unexpected HTTP status returned by IGDB.
type APIError struct {
	StatusCode int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("igdb: unexpected status %d", e.StatusCode)
}

// Unwrap lets callers treat upstream 5xx and rate limiting as availability
// problems, while other 4xx stay a distinct, loggable condition.
func (e *APIError) Unwrap() error {
	if e.StatusCode >= http.StatusInternalServerError || e.StatusCode == http.StatusTooManyRequests {
		return ErrUnavailable
	}
	return nil
}

type Client struct {
	httpClient *http.Client
	baseURL    string
	clientID   string
	tokens     *tokenProvider
}

type Option func(*Client)

// WithBaseURL points the client at a different host, used by tests.
func WithBaseURL(baseURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimSuffix(baseURL, "/")
	}
}

// WithTokenURL points token acquisition at a different host, used by tests.
func WithTokenURL(tokenURL string) Option {
	return func(c *Client) {
		c.tokens.tokenURL = tokenURL
	}
}

func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

func NewClient(clientID, clientSecret string, opts ...Option) *Client {
	httpClient := &http.Client{Timeout: defaultTimeout}

	client := &Client{
		httpClient: httpClient,
		baseURL:    DefaultBaseURL,
		clientID:   clientID,
		tokens:     newTokenProvider(httpClient, DefaultTokenURL, clientID, clientSecret),
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}

type gameResponse struct {
	ID               int    `json:"id"`
	Name             string `json:"name"`
	Summary          string `json:"summary"`
	FirstReleaseDate int64  `json:"first_release_date"`
	Cover            *struct {
		ImageID string `json:"image_id"`
	} `json:"cover"`
	InvolvedCompanies []struct {
		Developer bool `json:"developer"`
		Company   struct {
			Name string `json:"name"`
		} `json:"company"`
	} `json:"involved_companies"`
}

// query sends an Apicalypse body to IGDB. A rejected token gets one forced
// renewal and a single retry: tokens can be revoked before their stated expiry,
// and the visitor should never see that.
func (c *Client) query(ctx context.Context, path, body, operation string, dest any) error {
	err := c.do(ctx, path, body, operation, dest)

	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
		c.tokens.Invalidate()
		return c.do(ctx, path, body, operation, dest)
	}

	return err
}

func (c *Client) do(ctx context.Context, path, body, operation string, dest any) error {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("igdb: building %s request: %w", operation, err)
	}
	req.Header.Set("Client-ID", c.clientID)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	res, err := c.httpClient.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return fmt.Errorf("igdb: %s: %w", operation, errors.Join(err, ErrUnavailable))
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return &APIError{StatusCode: res.StatusCode}
	}

	if err := json.NewDecoder(res.Body).Decode(dest); err != nil {
		return fmt.Errorf("igdb: decoding %s response: %w", operation, err)
	}

	return nil
}

// SearchGames returns the top matches for query.
func (c *Client) SearchGames(ctx context.Context, query string) ([]models.GameSearchResult, error) {
	body := fmt.Sprintf(`search "%s"; fields %s; limit %d;`, escapeQuery(query), searchFields, searchLimit)

	var payload []gameResponse
	if err := c.query(ctx, "/games", body, "searching games", &payload); err != nil {
		return nil, err
	}

	results := make([]models.GameSearchResult, 0, len(payload))
	for _, item := range payload {
		results = append(results, models.GameSearchResult{
			ExternalID:  item.ID,
			Name:        item.Name,
			CoverURL:    coverURL(item),
			ReleaseYear: releaseYear(item.FirstReleaseDate),
		})
	}

	return results, nil
}

// GetGameDetails fetches the full record for a game. IGDB's `summary` is already
// plain text, so unlike the previous provider there is no HTML variant to avoid.
func (c *Client) GetGameDetails(ctx context.Context, externalID string) (*models.GameDetails, error) {
	id, err := strconv.Atoi(externalID)
	if err != nil {
		return nil, fmt.Errorf("igdb: %q is not a valid game id: %w", externalID, ErrGameNotFound)
	}

	body := fmt.Sprintf(`where id = %d; fields %s; limit 1;`, id, detailsFields)

	var payload []gameResponse
	if err := c.query(ctx, "/games", body, "fetching game details", &payload); err != nil {
		return nil, err
	}
	if len(payload) == 0 {
		return nil, ErrGameNotFound
	}

	game := payload[0]

	developers := make([]string, 0, len(game.InvolvedCompanies))
	for _, involved := range game.InvolvedCompanies {
		if involved.Developer && involved.Company.Name != "" {
			developers = append(developers, involved.Company.Name)
		}
	}

	return &models.GameDetails{
		ExternalID:  game.ID,
		Name:        game.Name,
		CoverURL:    coverURL(game),
		ReleasedAt:  releaseDate(game.FirstReleaseDate),
		Developer:   strings.Join(developers, ", "),
		Description: game.Summary,
	}, nil
}

// escapeQuery keeps a visitor's search term from breaking out of the quoted
// string in the Apicalypse body.
func escapeQuery(query string) string {
	escaped := strings.ReplaceAll(query, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)

	return strings.ReplaceAll(escaped, "\n", " ")
}

func coverURL(game gameResponse) string {
	if game.Cover == nil || game.Cover.ImageID == "" {
		return ""
	}

	return fmt.Sprintf(coverURLTemplate, game.Cover.ImageID)
}

// releaseDate converts IGDB's Unix timestamp, returning nil when the game has no
// known release date.
func releaseDate(timestamp int64) *time.Time {
	if timestamp == 0 {
		return nil
	}

	released := time.Unix(timestamp, 0).UTC()

	return &released
}

func releaseYear(timestamp int64) int {
	released := releaseDate(timestamp)
	if released == nil {
		return 0
	}

	return released.Year()
}
