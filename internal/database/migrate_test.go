package database

import (
	"context"
	"testing"
	"testing/fstest"

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
