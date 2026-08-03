package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JoaoVictorVM/ludora/internal/models"
)

// ErrDuplicateReview is returned when the uniqueness constraint on
// (game_id, reviewer_uuid) rejects an insert — the race-condition safety net
// behind the handler's pre-check.
var ErrDuplicateReview = errors.New("repository: reviewer already reviewed this game")

// uniqueViolationCode is Postgres' SQLSTATE for a unique constraint violation.
const uniqueViolationCode = "23505"

type ReviewRepository struct {
	pool *pgxpool.Pool
}

func NewReviewRepository(pool *pgxpool.Pool) *ReviewRepository {
	return &ReviewRepository{pool: pool}
}

func (r *ReviewRepository) Create(ctx context.Context, review *models.Review) (*models.Review, error) {
	const query = `INSERT INTO reviews (game_id, reviewer_uuid, rating, comment)
		VALUES ($1, $2, $3, NULLIF($4, ''))
		RETURNING id, game_id, reviewer_uuid, rating, COALESCE(comment, ''), created_at`

	var created models.Review
	err := r.pool.QueryRow(ctx, query,
		review.GameID, review.ReviewerUUID, review.Rating, review.Comment,
	).Scan(
		&created.ID, &created.GameID, &created.ReviewerUUID,
		&created.Rating, &created.Comment, &created.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrDuplicateReview
		}
		return nil, fmt.Errorf("creating review: %w", err)
	}

	return &created, nil
}

func (r *ReviewRepository) ExistsForGameAndReviewer(ctx context.Context, gameID int64, reviewerUUID string) (bool, error) {
	const query = `SELECT EXISTS (
		SELECT 1 FROM reviews WHERE game_id = $1 AND reviewer_uuid = $2
	)`

	var exists bool
	if err := r.pool.QueryRow(ctx, query, gameID, reviewerUUID).Scan(&exists); err != nil {
		return false, fmt.Errorf("checking existing review: %w", err)
	}

	return exists, nil
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == uniqueViolationCode
}
