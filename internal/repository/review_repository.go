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

// RatingSummary is a game's aggregated review picture. AverageStars is on the
// 0.5–5 scale; both fields are zero for a game with no reviews.
type RatingSummary struct {
	AverageStars float64
	TotalReviews int
}

// ListByGameID returns a game's reviews, most recent first.
func (r *ReviewRepository) ListByGameID(ctx context.Context, gameID int64) ([]models.Review, error) {
	const query = `SELECT id, game_id, reviewer_uuid, rating, COALESCE(comment, ''), created_at
		FROM reviews
		WHERE game_id = $1
		ORDER BY created_at DESC, id DESC`

	rows, err := r.pool.Query(ctx, query, gameID)
	if err != nil {
		return nil, fmt.Errorf("listing reviews: %w", err)
	}
	defer rows.Close()

	var reviews []models.Review
	for rows.Next() {
		var review models.Review
		err := rows.Scan(
			&review.ID, &review.GameID, &review.ReviewerUUID,
			&review.Rating, &review.Comment, &review.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning review: %w", err)
		}
		reviews = append(reviews, review)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing reviews: %w", err)
	}

	return reviews, nil
}

// AverageRatingByGameID returns the game's average on the star scale and its
// review count. A game with no reviews yields a zeroed summary, not an error.
func (r *ReviewRepository) AverageRatingByGameID(ctx context.Context, gameID int64) (RatingSummary, error) {
	const query = `SELECT
			COALESCE(ROUND(AVG(rating)::numeric / 2, 1), 0)::float8,
			COUNT(*)
		FROM reviews
		WHERE game_id = $1`

	var summary RatingSummary
	err := r.pool.QueryRow(ctx, query, gameID).Scan(&summary.AverageStars, &summary.TotalReviews)
	if err != nil {
		return RatingSummary{}, fmt.Errorf("averaging reviews: %w", err)
	}

	return summary, nil
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	return errors.As(err, &pgErr) && pgErr.SQLState() == uniqueViolationCode
}
