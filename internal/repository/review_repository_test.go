package repository

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JoaoVictorVM/ludora/internal/models"
)

func seedGame(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()

	game, err := NewGameRepository(pool).Create(context.Background(), sampleGame())
	if err != nil {
		t.Fatalf("seeding game: %v", err)
	}

	return game.ID
}

func TestCreate_InsertsReview(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	gameID := seedGame(t, pool)
	repo := NewReviewRepository(pool)

	reviewerUUID := uuid.NewString()
	created, err := repo.Create(ctx, &models.Review{
		GameID:       gameID,
		ReviewerUUID: reviewerUUID,
		Rating:       9,
		Comment:      "Ótimo jogo.",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.ID == 0 {
		t.Error("expected a generated id")
	}
	if created.GameID != gameID {
		t.Errorf("GameID = %d, want %d", created.GameID, gameID)
	}
	if created.ReviewerUUID != reviewerUUID {
		t.Errorf("ReviewerUUID = %q, want %q", created.ReviewerUUID, reviewerUUID)
	}
	if created.Rating != 9 {
		t.Errorf("Rating = %d, want 9", created.Rating)
	}
	if created.Stars() != 4.5 {
		t.Errorf("Stars = %v, want 4.5", created.Stars())
	}
	if created.Comment != "Ótimo jogo." {
		t.Errorf("Comment = %q", created.Comment)
	}
	if created.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be populated")
	}
}

func TestCreate_WithoutCommentStoresNull(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	gameID := seedGame(t, pool)
	repo := NewReviewRepository(pool)

	created, err := repo.Create(ctx, &models.Review{
		GameID:       gameID,
		ReviewerUUID: uuid.NewString(),
		Rating:       1,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Comment != "" {
		t.Errorf("Comment = %q, want empty", created.Comment)
	}

	var isNull bool
	err = pool.QueryRow(ctx, "SELECT comment IS NULL FROM reviews WHERE id = $1", created.ID).Scan(&isNull)
	if err != nil {
		t.Fatalf("checking comment column: %v", err)
	}
	if !isNull {
		t.Error("an omitted comment should be stored as NULL")
	}
}

func TestCreate_ViolatesUniqueConstraint(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	gameID := seedGame(t, pool)
	repo := NewReviewRepository(pool)

	reviewerUUID := uuid.NewString()
	first := &models.Review{GameID: gameID, ReviewerUUID: reviewerUUID, Rating: 8}
	if _, err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err := repo.Create(ctx, &models.Review{GameID: gameID, ReviewerUUID: reviewerUUID, Rating: 2})
	if !errors.Is(err, ErrDuplicateReview) {
		t.Fatalf("error = %v, want ErrDuplicateReview", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM reviews WHERE game_id = $1", gameID).Scan(&count); err != nil {
		t.Fatalf("counting reviews: %v", err)
	}
	if count != 1 {
		t.Errorf("reviews rows = %d, want 1", count)
	}
}

func TestCreate_RejectsRatingOutOfRange(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	gameID := seedGame(t, pool)
	repo := NewReviewRepository(pool)

	for _, rating := range []int16{0, 11} {
		_, err := repo.Create(ctx, &models.Review{
			GameID:       gameID,
			ReviewerUUID: uuid.NewString(),
			Rating:       rating,
		})
		if err == nil {
			t.Errorf("rating %d was accepted, want the CHECK constraint to reject it", rating)
		}
	}
}

func TestCreate_RejectsCommentOverLimit(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	gameID := seedGame(t, pool)
	repo := NewReviewRepository(pool)

	_, err := repo.Create(ctx, &models.Review{
		GameID:       gameID,
		ReviewerUUID: uuid.NewString(),
		Rating:       5,
		Comment:      strings.Repeat("a", models.MaxCommentLength+1),
	})
	if err == nil {
		t.Error("an 801-character comment was accepted, want the CHECK constraint to reject it")
	}
}

func TestExistsForGameAndReviewer_TrueAfterInsert(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	gameID := seedGame(t, pool)
	repo := NewReviewRepository(pool)

	reviewerUUID := uuid.NewString()
	if _, err := repo.Create(ctx, &models.Review{GameID: gameID, ReviewerUUID: reviewerUUID, Rating: 6}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	exists, err := repo.ExistsForGameAndReviewer(ctx, gameID, reviewerUUID)
	if err != nil {
		t.Fatalf("ExistsForGameAndReviewer: %v", err)
	}
	if !exists {
		t.Error("expected true after inserting a review")
	}
}

func TestExistsForGameAndReviewer_FalseWhenNone(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	gameID := seedGame(t, pool)
	repo := NewReviewRepository(pool)

	exists, err := repo.ExistsForGameAndReviewer(ctx, gameID, uuid.NewString())
	if err != nil {
		t.Fatalf("ExistsForGameAndReviewer: %v", err)
	}
	if exists {
		t.Error("expected false when the visitor has not reviewed the game")
	}
}

func TestReviews_CascadeOnGameDelete(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	gameID := seedGame(t, pool)
	repo := NewReviewRepository(pool)

	if _, err := repo.Create(ctx, &models.Review{GameID: gameID, ReviewerUUID: uuid.NewString(), Rating: 4}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if _, err := pool.Exec(ctx, "DELETE FROM games WHERE id = $1", gameID); err != nil {
		t.Fatalf("deleting game: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM reviews WHERE game_id = $1", gameID).Scan(&count); err != nil {
		t.Fatalf("counting reviews: %v", err)
	}
	if count != 0 {
		t.Errorf("reviews rows = %d after deleting the game, want 0", count)
	}
}

func TestListByGameID_OrdersByCreatedAtDesc(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	gameID := seedGame(t, pool)
	repo := NewReviewRepository(pool)

	for _, rating := range []int16{2, 6, 10} {
		if _, err := repo.Create(ctx, &models.Review{
			GameID:       gameID,
			ReviewerUUID: uuid.NewString(),
			Rating:       rating,
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	// now() is identical inside a single statement but not across statements;
	// spreading the timestamps makes the ordering unambiguous.
	if _, err := pool.Exec(ctx, `UPDATE reviews SET created_at = now() - (rating || ' hours')::interval`); err != nil {
		t.Fatalf("spreading timestamps: %v", err)
	}

	reviews, err := repo.ListByGameID(ctx, gameID)
	if err != nil {
		t.Fatalf("ListByGameID: %v", err)
	}
	if len(reviews) != 3 {
		t.Fatalf("got %d reviews, want 3", len(reviews))
	}

	for i := 1; i < len(reviews); i++ {
		if reviews[i].CreatedAt.After(reviews[i-1].CreatedAt) {
			t.Fatalf("reviews are not ordered most recent first: %v", reviews)
		}
	}
	if reviews[0].Rating != 2 {
		t.Errorf("first review rating = %d, want the most recent one (2)", reviews[0].Rating)
	}
}

func TestListByGameID_IgnoresOtherGames(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	gameID := seedGame(t, pool)
	repo := NewReviewRepository(pool)

	other, err := NewGameRepository(pool).Create(ctx, &models.Game{
		ExternalID:     "4200",
		ExternalSource: models.SourceRAWG,
		Name:           "Portal 2",
	})
	if err != nil {
		t.Fatalf("seeding second game: %v", err)
	}

	if _, err := repo.Create(ctx, &models.Review{GameID: gameID, ReviewerUUID: uuid.NewString(), Rating: 8}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.Create(ctx, &models.Review{GameID: other.ID, ReviewerUUID: uuid.NewString(), Rating: 4}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	reviews, err := repo.ListByGameID(ctx, gameID)
	if err != nil {
		t.Fatalf("ListByGameID: %v", err)
	}
	if len(reviews) != 1 {
		t.Fatalf("got %d reviews, want only the ones for this game", len(reviews))
	}
}

func TestAverageRatingByGameID_ComputesHalfStarRounding(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	gameID := seedGame(t, pool)
	repo := NewReviewRepository(pool)

	// AVG(9,8,6) = 7.6667 → /2 = 3.8333 → rounded to 3.8
	for _, rating := range []int16{9, 8, 6} {
		if _, err := repo.Create(ctx, &models.Review{
			GameID:       gameID,
			ReviewerUUID: uuid.NewString(),
			Rating:       rating,
		}); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	summary, err := repo.AverageRatingByGameID(ctx, gameID)
	if err != nil {
		t.Fatalf("AverageRatingByGameID: %v", err)
	}
	if summary.AverageStars != 3.8 {
		t.Errorf("AverageStars = %v, want 3.8", summary.AverageStars)
	}
	if summary.TotalReviews != 3 {
		t.Errorf("TotalReviews = %d, want 3", summary.TotalReviews)
	}
}

func TestAverageRatingByGameID_ZeroReviewsReturnsNull(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	gameID := seedGame(t, pool)

	summary, err := NewReviewRepository(pool).AverageRatingByGameID(ctx, gameID)
	if err != nil {
		t.Fatalf("AverageRatingByGameID: %v", err)
	}
	if summary.AverageStars != 0 {
		t.Errorf("AverageStars = %v, want 0", summary.AverageStars)
	}
	if summary.TotalReviews != 0 {
		t.Errorf("TotalReviews = %d, want 0", summary.TotalReviews)
	}
}
