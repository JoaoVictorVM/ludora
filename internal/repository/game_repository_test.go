package repository

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JoaoVictorVM/ludora/internal/database"
	"github.com/JoaoVictorVM/ludora/internal/models"
	"github.com/JoaoVictorVM/ludora/internal/testutil"
)

func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	pool := testutil.NewPostgresPool(t)
	if err := database.Migrate(context.Background(), pool, nil); err != nil {
		t.Fatalf("applying migrations: %v", err)
	}

	return pool
}

func sampleGame() *models.Game {
	released := time.Date(2013, time.September, 17, 0, 0, 0, 0, time.UTC)

	return &models.Game{
		ExternalID:     "3498",
		ExternalSource: models.SourceIGDB,
		Name:           "Grand Theft Auto V",
		CoverURL:       "https://images.igdb.com/igdb/image/upload/t_cover_big/co1r7f.jpg",
		ReleasedAt:     &released,
		Developer:      "Rockstar North, Rockstar Games",
		Description:    "An open world action-adventure game.",
	}
}

func TestCreate_InsertsNewGame(t *testing.T) {
	ctx := context.Background()
	repo := NewGameRepository(migratedPool(t))

	created, err := repo.Create(ctx, sampleGame())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.ID == 0 {
		t.Error("expected a generated local id")
	}
	if created.ExternalID != "3498" {
		t.Errorf("ExternalID = %q, want 3498", created.ExternalID)
	}
	if created.ExternalSource != models.SourceIGDB {
		t.Errorf("ExternalSource = %q, want igdb", created.ExternalSource)
	}
	if created.Name != "Grand Theft Auto V" {
		t.Errorf("Name = %q", created.Name)
	}
	if created.CoverURL != "https://images.igdb.com/igdb/image/upload/t_cover_big/co1r7f.jpg" {
		t.Errorf("CoverURL = %q", created.CoverURL)
	}
	if created.Developer != "Rockstar North, Rockstar Games" {
		t.Errorf("Developer = %q", created.Developer)
	}
	if created.Description != "An open world action-adventure game." {
		t.Errorf("Description = %q", created.Description)
	}
	if created.ReleaseYear() != 2013 {
		t.Errorf("ReleaseYear = %d, want 2013", created.ReleaseYear())
	}
	if created.CreatedAt.IsZero() {
		t.Error("expected CreatedAt to be populated")
	}
}

func TestCreate_OptionalFieldsBecomeNull(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	repo := NewGameRepository(pool)

	created, err := repo.Create(ctx, &models.Game{
		ExternalID:     "999",
		ExternalSource: models.SourceIGDB,
		Name:           "Jogo Sem Metadados",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var nulls int
	err = pool.QueryRow(ctx, `SELECT count(*) FROM games
		WHERE id = $1 AND cover_url IS NULL AND developer IS NULL
		AND description IS NULL AND released_at IS NULL`, created.ID).Scan(&nulls)
	if err != nil {
		t.Fatalf("checking null columns: %v", err)
	}
	if nulls != 1 {
		t.Error("empty optional fields should be stored as NULL, not empty strings")
	}

	if created.CoverURL != "" || created.Developer != "" || created.Description != "" {
		t.Error("null columns should read back as empty strings")
	}
	if created.ReleasedAt != nil {
		t.Errorf("ReleasedAt = %v, want nil", created.ReleasedAt)
	}
}

func TestCreate_ConcurrentDuplicateReturnsExistingRecord(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	repo := NewGameRepository(pool)

	const callers = 8

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		ids   []int64
		errs  []error
		start = make(chan struct{})
	)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			created, err := repo.Create(ctx, sampleGame())

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			ids = append(ids, created.ID)
		}()
	}

	close(start)
	wg.Wait()

	if len(errs) > 0 {
		t.Fatalf("concurrent inserts returned %d error(s), first: %v", len(errs), errs[0])
	}
	if len(ids) != callers {
		t.Fatalf("got %d ids, want %d", len(ids), callers)
	}
	for _, id := range ids[1:] {
		if id != ids[0] {
			t.Fatalf("callers received different local ids: %v", ids)
		}
	}

	var rows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM games WHERE external_id = '3498'`).Scan(&rows); err != nil {
		t.Fatalf("counting rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("games table has %d rows for the same external id, want 1", rows)
	}
}

func TestFindByExternalID_ReturnsCachedRecord(t *testing.T) {
	ctx := context.Background()
	repo := NewGameRepository(migratedPool(t))

	created, err := repo.Create(ctx, sampleGame())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := repo.FindByExternalID(ctx, models.SourceIGDB, "3498")
	if err != nil {
		t.Fatalf("FindByExternalID: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("ID = %d, want %d", found.ID, created.ID)
	}
	if found.Name != created.Name {
		t.Errorf("Name = %q, want %q", found.Name, created.Name)
	}
}

func TestFindByExternalID_NotFound(t *testing.T) {
	ctx := context.Background()
	repo := NewGameRepository(migratedPool(t))

	_, err := repo.FindByExternalID(ctx, models.SourceIGDB, "inexistente")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
}

func TestGetByID_ReturnsCachedRecord(t *testing.T) {
	ctx := context.Background()
	repo := NewGameRepository(migratedPool(t))

	created, err := repo.Create(ctx, sampleGame())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := repo.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if found.ExternalID != "3498" {
		t.Errorf("ExternalID = %q, want 3498", found.ExternalID)
	}
}

// seedReviewedGames creates n games, each with one review, spacing the review
// timestamps so the feed ordering is deterministic: higher index = more recent.
func seedReviewedGames(t *testing.T, pool *pgxpool.Pool, n int) []int64 {
	t.Helper()

	ctx := context.Background()
	gameRepo := NewGameRepository(pool)
	reviewRepo := NewReviewRepository(pool)

	ids := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		game, err := gameRepo.Create(ctx, &models.Game{
			ExternalID:     strconv.Itoa(1000 + i),
			ExternalSource: models.SourceIGDB,
			Name:           fmt.Sprintf("Jogo %02d", i),
		})
		if err != nil {
			t.Fatalf("seeding game %d: %v", i, err)
		}

		review, err := reviewRepo.Create(ctx, &models.Review{
			GameID:       game.ID,
			ReviewerUUID: uuid.NewString(),
			Rating:       int16(i%10 + 1),
		})
		if err != nil {
			t.Fatalf("seeding review %d: %v", i, err)
		}

		_, err = pool.Exec(ctx,
			`UPDATE reviews SET created_at = now() - make_interval(mins => $1::int) WHERE id = $2`,
			n-i, review.ID)
		if err != nil {
			t.Fatalf("aging review %d: %v", i, err)
		}

		ids = append(ids, game.ID)
	}

	return ids
}

func TestListRecentlyReviewed_OrdersByLastReviewDesc(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	ids := seedReviewedGames(t, pool, 3)

	games, _, err := NewGameRepository(pool).ListRecentlyReviewed(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListRecentlyReviewed: %v", err)
	}
	if len(games) != 3 {
		t.Fatalf("got %d games, want 3", len(games))
	}

	// Seeded newest last, so the feed must return them reversed.
	for i, want := range []int64{ids[2], ids[1], ids[0]} {
		if games[i].ID != want {
			t.Errorf("position %d = game %d, want %d", i, games[i].ID, want)
		}
	}
	for i := 1; i < len(games); i++ {
		if games[i].LastReviewedAt.After(games[i-1].LastReviewedAt) {
			t.Fatal("games are not ordered by most recent review")
		}
	}
}

func TestListRecentlyReviewed_ExcludesGamesWithoutReviews(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	seedReviewedGames(t, pool, 2)

	if _, err := NewGameRepository(pool).Create(ctx, &models.Game{
		ExternalID:     "sem-review",
		ExternalSource: models.SourceIGDB,
		Name:           "Jogo Sem Review",
	}); err != nil {
		t.Fatalf("seeding unreviewed game: %v", err)
	}

	games, _, err := NewGameRepository(pool).ListRecentlyReviewed(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListRecentlyReviewed: %v", err)
	}
	if len(games) != 2 {
		t.Fatalf("got %d games, want only the reviewed ones", len(games))
	}
}

func TestListRecentlyReviewed_RespectsLimitAndOffset(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	seedReviewedGames(t, pool, 7)
	repo := NewGameRepository(pool)

	first, _, err := repo.ListRecentlyReviewed(ctx, 3, 0)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("first page has %d games, want 3", len(first))
	}

	second, _, err := repo.ListRecentlyReviewed(ctx, 3, 3)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second) != 3 {
		t.Fatalf("second page has %d games, want 3", len(second))
	}

	seen := map[int64]bool{}
	for _, game := range append(append([]ReviewedGame{}, first...), second...) {
		if seen[game.ID] {
			t.Errorf("game %d appeared on both pages", game.ID)
		}
		seen[game.ID] = true
	}

	last, _, err := repo.ListRecentlyReviewed(ctx, 3, 6)
	if err != nil {
		t.Fatalf("last page: %v", err)
	}
	if len(last) != 1 {
		t.Errorf("last page has %d games, want the remaining 1", len(last))
	}
}

func TestListRecentlyReviewed_IndicatesMoreResultsAvailable(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	seedReviewedGames(t, pool, 5)
	repo := NewGameRepository(pool)

	games, hasMore, err := repo.ListRecentlyReviewed(ctx, 3, 0)
	if err != nil {
		t.Fatalf("ListRecentlyReviewed: %v", err)
	}
	if len(games) != 3 {
		t.Errorf("got %d games, want exactly the limit (the probe row must not leak)", len(games))
	}
	if !hasMore {
		t.Error("hasMore = false, want true when more games remain")
	}

	_, hasMore, err = repo.ListRecentlyReviewed(ctx, 3, 3)
	if err != nil {
		t.Fatalf("ListRecentlyReviewed: %v", err)
	}
	if hasMore {
		t.Error("hasMore = true on the final page, want false")
	}

	_, hasMore, err = repo.ListRecentlyReviewed(ctx, 5, 0)
	if err != nil {
		t.Fatalf("ListRecentlyReviewed: %v", err)
	}
	if hasMore {
		t.Error("hasMore = true when the page exactly exhausts the catalog, want false")
	}
}

func TestListRecentlyReviewed_AggregatesRatingAndCount(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	gameID := seedGame(t, pool)
	reviewRepo := NewReviewRepository(pool)

	for _, rating := range []int16{9, 8, 6} {
		if _, err := reviewRepo.Create(ctx, &models.Review{
			GameID:       gameID,
			ReviewerUUID: uuid.NewString(),
			Rating:       rating,
		}); err != nil {
			t.Fatalf("seeding review: %v", err)
		}
	}

	games, _, err := NewGameRepository(pool).ListRecentlyReviewed(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListRecentlyReviewed: %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("got %d games, want 1", len(games))
	}
	if games[0].AverageStars != 3.8 {
		t.Errorf("AverageStars = %v, want 3.8", games[0].AverageStars)
	}
	if games[0].TotalReviews != 3 {
		t.Errorf("TotalReviews = %d, want 3", games[0].TotalReviews)
	}
}
