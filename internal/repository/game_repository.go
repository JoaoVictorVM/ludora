package repository

import (
	"context"
	"errors"
	"fmt"

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
		source = models.SourceRAWG
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
