package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
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

const reviewColumns = `id, game_id, reviewer_uuid, rating, COALESCE(comment, ''), created_at`

func (r *ReviewRepository) GetByID(ctx context.Context, id int64) (*models.Review, error) {
	const query = `SELECT ` + reviewColumns + ` FROM reviews WHERE id = $1`

	var review models.Review
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&review.ID, &review.GameID, &review.ReviewerUUID,
		&review.Rating, &review.Comment, &review.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("getting review by id: %w", err)
	}

	return &review, nil
}

// OwnerOf returns the reviewer that owns the review, or an empty string when no
// such review exists — the caller must not be able to tell those cases apart.
func (r *ReviewRepository) OwnerOf(ctx context.Context, id int64) (string, error) {
	var reviewerUUID string
	err := r.pool.QueryRow(ctx, "SELECT reviewer_uuid FROM reviews WHERE id = $1", id).Scan(&reviewerUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("resolving review owner: %w", err)
	}

	return reviewerUUID, nil
}

// UpdateByIDAndReviewer scopes the update to the requesting visitor, so a
// non-owner touches nothing even if the ownership check were ever bypassed.
// It reports whether a row was actually changed.
func (r *ReviewRepository) UpdateByIDAndReviewer(ctx context.Context, id int64, reviewerUUID string, rating int16, comment string) (bool, error) {
	const query = `UPDATE reviews
		SET rating = $1, comment = NULLIF($2, '')
		WHERE id = $3 AND reviewer_uuid = $4`

	tag, err := r.pool.Exec(ctx, query, rating, comment, id, reviewerUUID)
	if err != nil {
		return false, fmt.Errorf("updating review: %w", err)
	}

	return tag.RowsAffected() > 0, nil
}

// DeleteByIDAndReviewer removes the visitor's own review. Zero rows affected
// means the review is already gone, which the caller reports as such rather
// than as an error — deleting twice must not blow up.
func (r *ReviewRepository) DeleteByIDAndReviewer(ctx context.Context, id int64, reviewerUUID string) (bool, error) {
	const query = `DELETE FROM reviews WHERE id = $1 AND reviewer_uuid = $2`

	tag, err := r.pool.Exec(ctx, query, id, reviewerUUID)
	if err != nil {
		return false, fmt.Errorf("deleting review: %w", err)
	}

	return tag.RowsAffected() > 0, nil
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
