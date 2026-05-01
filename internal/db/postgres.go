package db

import (
	"context"
	"fmt"
	"log"

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
}

// MigratePostgres creates the schema_version table if needed and applies any
// pending migrations in order, each wrapped in its own transaction.
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
			continue
		}

		log.Printf("INFO postgres: applying migration v%d: %s", m.Version, m.Description)

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration v%d: %w", m.Version, err)
		}

		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("apply migration v%d: %w", m.Version, err)
		}

		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_version (version, description) VALUES ($1, $2)",
			m.Version, m.Description,
		); err != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("record migration v%d: %w", m.Version, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration v%d: %w", m.Version, err)
		}

		log.Printf("INFO postgres: migration v%d applied", m.Version)
	}

	return nil
}
