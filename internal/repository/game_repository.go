package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JoaoVictorVM/ludora/internal/models"
)

// ErrNotFound is returned when a lookup matches no row.
var ErrNotFound = errors.New("repository: not found")

const gameColumns = `id, external_id, external_source, name,
	COALESCE(cover_url, ''), released_at, COALESCE(developer, ''),
	COALESCE(description, ''), created_at`

type GameRepository struct {
	pool *pgxpool.Pool
}

func NewGameRepository(pool *pgxpool.Pool) *GameRepository {
	return &GameRepository{pool: pool}
}

func (r *GameRepository) FindByExternalID(ctx context.Context, source, externalID string) (*models.Game, error) {
	const query = `SELECT ` + gameColumns + `
		FROM games
		WHERE external_source = $1 AND external_id = $2`

	game, err := scanGame(r.pool.QueryRow(ctx, query, source, externalID))
	if err != nil {
		return nil, fmt.Errorf("finding game by external id: %w", err)
	}

	return game, nil
}

func (r *GameRepository) GetByID(ctx context.Context, id int64) (*models.Game, error) {
	const query = `SELECT ` + gameColumns + ` FROM games WHERE id = $1`

	game, err := scanGame(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		return nil, fmt.Errorf("getting game by id: %w", err)
	}

	return game, nil
}

// Create caches a game, tolerating a concurrent insert of the same external ID:
// the conflicting call inserts nothing and re-reads the row the winner wrote,
// so both callers end up with the same local record.
func (r *GameRepository) Create(ctx context.Context, game *models.Game) (*models.Game, error) {
	const query = `INSERT INTO games
			(external_id, external_source, name, cover_url, released_at, developer, description)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5, NULLIF($6, ''), NULLIF($7, ''))
		ON CONFLICT (external_source, external_id) DO NOTHING
		RETURNING ` + gameColumns

	source := game.ExternalSource
	if source == "" {
		source = models.SourceIGDB
	}

	created, err := scanGame(r.pool.QueryRow(ctx, query,
		game.ExternalID, source, game.Name, game.CoverURL,
		game.ReleasedAt, game.Developer, game.Description,
	))
	if err == nil {
		return created, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("creating game: %w", err)
	}

	return r.FindByExternalID(ctx, source, game.ExternalID)
}

// ReviewedGame is a game as it appears in the home feed: enough to render a
// card, plus the aggregates that drive the ordering.
type ReviewedGame struct {
	ID             int64
	Name           string
	CoverURL       string
	AverageStars   float64
	TotalReviews   int
	LastReviewedAt time.Time
}

// ListRecentlyReviewed returns games ordered by their most recent review. It
// reads one row beyond limit to tell the caller whether another page exists,
// which avoids a second COUNT query per page load.
func (r *GameRepository) ListRecentlyReviewed(ctx context.Context, limit, offset int) (games []ReviewedGame, hasMore bool, err error) {
	const query = `SELECT g.id, g.name, COALESCE(g.cover_url, ''),
			ROUND(AVG(rv.rating)::numeric / 2, 1)::float8 AS avg_stars,
			COUNT(rv.id) AS total_reviews,
			MAX(rv.created_at) AS last_reviewed_at
		FROM games g
		JOIN reviews rv ON rv.game_id = g.id
		GROUP BY g.id
		ORDER BY last_reviewed_at DESC, g.id DESC
		LIMIT $1 OFFSET $2`

	rows, err := r.pool.Query(ctx, query, limit+1, offset)
	if err != nil {
		return nil, false, fmt.Errorf("listing recently reviewed games: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var game ReviewedGame
		err := rows.Scan(&game.ID, &game.Name, &game.CoverURL,
			&game.AverageStars, &game.TotalReviews, &game.LastReviewedAt)
		if err != nil {
			return nil, false, fmt.Errorf("scanning recently reviewed game: %w", err)
		}
		games = append(games, game)
	}

	if err := rows.Err(); err != nil {
		return nil, false, fmt.Errorf("listing recently reviewed games: %w", err)
	}

	if len(games) > limit {
		return games[:limit], true, nil
	}

	return games, false, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanGame(row rowScanner) (*models.Game, error) {
	var game models.Game

	err := row.Scan(
		&game.ID, &game.ExternalID, &game.ExternalSource, &game.Name,
		&game.CoverURL, &game.ReleasedAt, &game.Developer,
		&game.Description, &game.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return &game, nil
}
