package repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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
		ExternalSource: models.SourceRAWG,
		Name:           "Grand Theft Auto V",
		CoverURL:       "https://media.rawg.io/gta5.jpg",
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
	if created.ExternalSource != models.SourceRAWG {
		t.Errorf("ExternalSource = %q, want rawg", created.ExternalSource)
	}
	if created.Name != "Grand Theft Auto V" {
		t.Errorf("Name = %q", created.Name)
	}
	if created.CoverURL != "https://media.rawg.io/gta5.jpg" {
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
		ExternalSource: models.SourceRAWG,
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

	found, err := repo.FindByExternalID(ctx, models.SourceRAWG, "3498")
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

	_, err := repo.FindByExternalID(ctx, models.SourceRAWG, "inexistente")
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
