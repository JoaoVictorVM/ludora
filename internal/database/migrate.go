package database

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const migrationsDir = "migrations"

type migration struct {
	version string
	name    string
	sql     string
}

// Migrate applies every embedded migration not yet recorded in schema_migrations,
// in filename order, each inside its own transaction.
func Migrate(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	return migrateFS(ctx, pool, migrationsFS, migrationsDir, logger)
}

func migrateFS(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, dir string, logger *slog.Logger) error {
	migrations, err := loadMigrations(fsys, dir)
	if err != nil {
		return err
	}

	applied, err := appliedVersions(ctx, pool)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if _, ok := applied[m.version]; ok {
			continue
		}

		if err := applyMigration(ctx, pool, m); err != nil {
			return err
		}

		if logger != nil {
			logger.Info("migration applied", "version", m.version, "name", m.name)
		}
	}

	return nil
}

func loadMigrations(fsys fs.FS, dir string) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("reading migrations directory: %w", err)
	}

	migrations := make([]migration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version, _, found := strings.Cut(entry.Name(), "_")
		if !found || version == "" {
			return nil, fmt.Errorf("migration %q must be named <version>_<name>.sql", entry.Name())
		}

		contents, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("reading migration %q: %w", entry.Name(), err)
		}

		migrations = append(migrations, migration{
			version: version,
			name:    entry.Name(),
			sql:     string(contents),
		})
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].name < migrations[j].name
	})

	return migrations, nil
}

// appliedVersions returns an empty set when schema_migrations does not exist yet,
// since the table is itself created by the first migration.
func appliedVersions(ctx context.Context, pool *pgxpool.Pool) (map[string]struct{}, error) {
	var exists bool
	if err := pool.QueryRow(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return nil, fmt.Errorf("checking schema_migrations: %w", err)
	}

	applied := make(map[string]struct{})
	if !exists {
		return applied, nil
	}

	rows, err := pool.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("reading applied migrations: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scanning applied migration: %w", err)
		}
		applied[version] = struct{}{}
	}

	return applied, rows.Err()
}

func applyMigration(ctx context.Context, pool *pgxpool.Pool, m migration) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting transaction for migration %q: %w", m.name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, m.sql); err != nil {
		return fmt.Errorf("applying migration %q: %w", m.name, err)
	}

	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", m.version); err != nil {
		return fmt.Errorf("recording migration %q: %w", m.name, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing migration %q: %w", m.name, err)
	}

	return nil
}
