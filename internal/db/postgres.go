package db

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

// pgMigration is a single versioned schema change.
type pgMigration struct {
	Version     int
	Description string
	SQL         string
}

// pgMigrations is the ordered list of all schema versions.
// Always append — never modify an existing entry.
var pgMigrations = []pgMigration{
	{
		Version:     1,
		Description: "create exercises, sets, and weight tables",
		SQL: `
CREATE TABLE IF NOT EXISTS exercises (
    id        SERIAL PRIMARY KEY,
    gr        TEXT NOT NULL DEFAULT '',
    place     TEXT NOT NULL DEFAULT '',
    name      TEXT NOT NULL DEFAULT '',
    descr     TEXT NOT NULL DEFAULT '',
    image     TEXT NOT NULL DEFAULT '',
    color     TEXT NOT NULL DEFAULT '',
    weight    NUMERIC(10,2) NOT NULL DEFAULT 0,
    reps      INTEGER NOT NULL DEFAULT 0,
    intensity INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS sets (
    id            SERIAL PRIMARY KEY,
    date          DATE NOT NULL,
    name          TEXT NOT NULL DEFAULT '',
    color         TEXT NOT NULL DEFAULT '',
    workout_color TEXT NOT NULL DEFAULT '',
    weight        NUMERIC(10,2) NOT NULL DEFAULT 0,
    reps          INTEGER NOT NULL DEFAULT 0,
    intensity     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS weight (
    id     SERIAL PRIMARY KEY,
    date   DATE NOT NULL,
    weight NUMERIC(10,2) NOT NULL DEFAULT 0
);
`,
	},
	{
		Version:     2,
		Description: "drop intensity columns",
		SQL: `
ALTER TABLE exercises DROP COLUMN IF EXISTS intensity;
ALTER TABLE sets DROP COLUMN IF EXISTS intensity;
`,
	},
	{
		Version:     3,
		Description: "add note column to sets",
		SQL: `
ALTER TABLE sets ADD COLUMN IF NOT EXISTS note TEXT NOT NULL DEFAULT '';
`,
	},
}

// MigratePostgres creates the schema_version table if needed and applies any
// pending migrations in order, each wrapped in its own transaction.
//
// Multi-version upgrades: the runner always iterates the full pgMigrations
// slice in order and applies every version not yet recorded in schema_version.
// This guarantees a database at any past version (e.g. v1) is safely and
// completely brought to the latest version (e.g. v5) without skipping steps
// or corrupting data. Each migration runs in its own transaction; if one
// fails the database is left at the last successfully applied version.
func MigratePostgres(pool *pgxpool.Pool) error {
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_version (
			version     INTEGER     NOT NULL PRIMARY KEY,
			applied_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			description TEXT        NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema_version table: %w", err)
	}

	// Determine current schema version for logging.
	var currentVersion int
	_ = pool.QueryRow(ctx,
		"SELECT COALESCE(MAX(version), 0) FROM schema_version",
	).Scan(&currentVersion)

	latestVersion := pgMigrations[len(pgMigrations)-1].Version
	pending := 0
	for _, m := range pgMigrations {
		if m.Version > currentVersion {
			pending++
		}
	}

	slog.Info("db migrations: checking schema",
		slog.Int("current_version", currentVersion),
		slog.Int("latest_version", latestVersion),
		slog.Int("pending", pending),
	)

	applied := 0
	for _, m := range pgMigrations {
		var exists bool
		err := pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_version WHERE version = $1)",
			m.Version,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check schema version %d: %w", m.Version, err)
		}
		if exists {
			slog.Debug("db migrations: skipping already-applied version",
				slog.Int("version", m.Version),
				slog.String("description", m.Description),
			)
			continue
		}

		slog.Info("db migrations: applying",
			slog.Int("version", m.Version),
			slog.String("description", m.Description),
		)

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration v%d: %w", m.Version, err)
		}

		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration v%d (%s): %w", m.Version, m.Description, err)
		}

		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_version (version, description) VALUES ($1, $2)",
			m.Version, m.Description,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration v%d: %w", m.Version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration v%d: %w", m.Version, err)
		}

		slog.Info("db migrations: applied successfully",
			slog.Int("version", m.Version),
			slog.String("description", m.Description),
		)
		applied++
	}

	if applied == 0 {
		slog.Info("db migrations: schema is up to date", slog.Int("version", latestVersion))
	} else {
		slog.Info("db migrations: all pending migrations applied",
			slog.Int("applied", applied),
			slog.Int("version", latestVersion),
		)
	}

	return nil
}
