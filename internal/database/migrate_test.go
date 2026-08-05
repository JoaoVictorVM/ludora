package database

import (
	"context"
	"io/fs"
	"path"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JoaoVictorVM/ludora/internal/testutil"
)

func TestMigrate_AppliesPendingMigrations(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewPostgresPool(t)

	if err := Migrate(ctx, pool, nil); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&count); err != nil {
		t.Fatalf("counting applied migrations: %v", err)
	}

	expected, err := loadMigrations(migrationsFS, migrationsDir)
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if count != len(expected) {
		t.Fatalf("applied %d migrations, want %d", count, len(expected))
	}

	var version string
	if err := pool.QueryRow(ctx, "SELECT version FROM schema_migrations ORDER BY version LIMIT 1").Scan(&version); err != nil {
		t.Fatalf("reading first migration version: %v", err)
	}
	if version != "0001" {
		t.Errorf("first recorded version = %q, want 0001", version)
	}
}

func TestMigrate_SkipsAlreadyApplied(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewPostgresPool(t)

	if err := Migrate(ctx, pool, nil); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}

	var before int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&before); err != nil {
		t.Fatalf("counting after first run: %v", err)
	}

	if err := Migrate(ctx, pool, nil); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	var after int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&after); err != nil {
		t.Fatalf("counting after second run: %v", err)
	}

	if after != before {
		t.Errorf("second run applied %d extra migrations, want 0", after-before)
	}
}

func TestMigrate_FailsOnBadSQL(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewPostgresPool(t)

	fsys := fstest.MapFS{
		"migrations/0001_create_schema_migrations.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE schema_migrations (version VARCHAR(14) PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now());"),
		},
		"migrations/0002_broken.sql": &fstest.MapFile{
			Data: []byte("CREATE TABLE ;;; not valid sql"),
		},
	}

	err := migrateFS(ctx, pool, fsys, migrationsDir, nil)
	if err == nil {
		t.Fatal("expected an error for invalid SQL, got nil")
	}

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations WHERE version = '0002'").Scan(&count); err != nil {
		t.Fatalf("checking schema_migrations: %v", err)
	}
	if count != 0 {
		t.Errorf("failed migration was recorded as applied (%d rows), want 0", count)
	}
}

// migrationsBefore returns an FS holding every embedded migration whose version
// sorts below the given one, so a test can reach the state just before it.
func migrationsBefore(t *testing.T, version string) fs.FS {
	t.Helper()

	entries, err := fs.ReadDir(migrationsFS, migrationsDir)
	if err != nil {
		t.Fatalf("reading embedded migrations: %v", err)
	}

	fsys := fstest.MapFS{}
	for _, entry := range entries {
		if entry.Name() >= version {
			continue
		}

		data, err := fs.ReadFile(migrationsFS, path.Join(migrationsDir, entry.Name()))
		if err != nil {
			t.Fatalf("reading migration %q: %v", entry.Name(), err)
		}
		fsys[path.Join(migrationsDir, entry.Name())] = &fstest.MapFile{Data: data}
	}

	return fsys
}

// seedCatalogBeforeCutover puts one game per provider in place, each with a
// review, so the cleanup migration has both a target and a bystander.
func seedCatalogBeforeCutover(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	for _, source := range []string{"rawg", "igdb"} {
		var gameID int64
		err := pool.QueryRow(ctx,
			`INSERT INTO games (external_id, external_source, name) VALUES ($1, $2, $3) RETURNING id`,
			"3498", source, "Jogo "+source).Scan(&gameID)
		if err != nil {
			t.Fatalf("seeding %s game: %v", source, err)
		}

		_, err = pool.Exec(ctx,
			`INSERT INTO reviews (game_id, reviewer_uuid, rating) VALUES ($1, gen_random_uuid(), 8)`, gameID)
		if err != nil {
			t.Fatalf("seeding %s review: %v", source, err)
		}
	}
}

func countBySource(t *testing.T, ctx context.Context, pool *pgxpool.Pool, source string) (games, reviews int) {
	t.Helper()

	err := pool.QueryRow(ctx, `SELECT
			(SELECT count(*) FROM games WHERE external_source = $1),
			(SELECT count(*) FROM reviews rv JOIN games g ON g.id = rv.game_id WHERE g.external_source = $1)`,
		source).Scan(&games, &reviews)
	if err != nil {
		t.Fatalf("counting %s rows: %v", source, err)
	}

	return games, reviews
}

func TestMigrate_DropsRawgCatalog(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewPostgresPool(t)

	if err := migrateFS(ctx, pool, migrationsBefore(t, "0004"), migrationsDir, nil); err != nil {
		t.Fatalf("migrating to the pre-cutover state: %v", err)
	}
	seedCatalogBeforeCutover(t, ctx, pool)

	if err := Migrate(ctx, pool, nil); err != nil {
		t.Fatalf("applying the cutover migration: %v", err)
	}

	games, reviews := countBySource(t, ctx, pool, "rawg")
	if games != 0 {
		t.Errorf("rawg games remaining = %d, want 0", games)
	}
	if reviews != 0 {
		t.Errorf("reviews of rawg games remaining = %d, want 0 (cascade)", reviews)
	}
}

func TestMigrate_KeepsNonRawgCatalog(t *testing.T) {
	ctx := context.Background()
	pool := testutil.NewPostgresPool(t)

	if err := migrateFS(ctx, pool, migrationsBefore(t, "0004"), migrationsDir, nil); err != nil {
		t.Fatalf("migrating to the pre-cutover state: %v", err)
	}
	seedCatalogBeforeCutover(t, ctx, pool)

	if err := Migrate(ctx, pool, nil); err != nil {
		t.Fatalf("applying the cutover migration: %v", err)
	}

	games, reviews := countBySource(t, ctx, pool, "igdb")
	if games != 1 {
		t.Errorf("igdb games = %d, want the record to survive", games)
	}
	if reviews != 1 {
		t.Errorf("reviews of igdb games = %d, want them untouched", reviews)
	}
}
