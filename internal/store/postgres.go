package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
)

// PostgresStore implements Store using a PostgreSQL connection pool.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgres dials the PostgreSQL DSN, pings the server, and returns a
// ready-to-use store. Call db.MigratePostgres before using the store.
func NewPostgres(dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

// Pool exposes the underlying connection pool (used by the migrate package).
func (s *PostgresStore) Pool() *pgxpool.Pool {
	return s.pool
}

// ─── exercises ────────────────────────────────────────────────────────────────

func (s *PostgresStore) SelectEx() ([]models.Exercise, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT id, gr, place, name, descr, image, color, weight::text, reps
		 FROM exercises ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var exes []models.Exercise
	for rows.Next() {
		var ex models.Exercise
		var weightStr string
		if err := rows.Scan(&ex.ID, &ex.Group, &ex.Place, &ex.Name, &ex.Descr,
			&ex.Image, &ex.Color, &weightStr, &ex.Reps); err != nil {
			return nil, err
		}
		ex.Weight, _ = decimal.NewFromString(weightStr)
		exes = append(exes, ex)
	}
	return exes, rows.Err()
}

func (s *PostgresStore) InsertEx(ex models.Exercise) error {
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO exercises (gr, place, name, descr, image, color, weight, reps)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		ex.Group, ex.Place, ex.Name, ex.Descr, ex.Image, ex.Color,
		ex.Weight.String(), ex.Reps)
	return err
}

func (s *PostgresStore) DeleteEx(id int) error {
	_, err := s.pool.Exec(context.Background(), "DELETE FROM exercises WHERE id = $1", id)
	return err
}

func (s *PostgresStore) UpdateExColor(id int, color string) error {
	_, err := s.pool.Exec(context.Background(),
		"UPDATE exercises SET color = $1 WHERE id = $2", color, id)
	return err
}

// ─── sets ─────────────────────────────────────────────────────────────────────

func (s *PostgresStore) SelectSet() ([]models.Set, error) {
	rows, err := s.pool.Query(context.Background(),
		`SELECT id, date::text, name, color, workout_color, weight::text, reps
		 FROM sets ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sets []models.Set
	for rows.Next() {
		var set models.Set
		var weightStr string
		if err := rows.Scan(&set.ID, &set.Date, &set.Name, &set.Color,
			&set.WorkoutColor, &weightStr, &set.Reps); err != nil {
			return nil, err
		}
		set.Weight, _ = decimal.NewFromString(weightStr)
		sets = append(sets, set)
	}
	return sets, rows.Err()
}

func (s *PostgresStore) BulkReplaceSetsByDate(date string, sets []models.Set) error {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "DELETE FROM sets WHERE date = $1::date", date); err != nil {
		return err
	}

	for _, set := range sets {
		if _, err := tx.Exec(ctx,
			`INSERT INTO sets (date, name, color, workout_color, weight, reps)
			 VALUES ($1::date, $2, $3, $4, $5, $6)`,
			set.Date, set.Name, set.Color, set.WorkoutColor,
			set.Weight.String(), set.Reps); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// ─── weight ───────────────────────────────────────────────────────────────────

func (s *PostgresStore) SelectW() ([]models.BodyWeight, error) {
	rows, err := s.pool.Query(context.Background(),
		"SELECT id, date::text, weight::text FROM weight ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ws []models.BodyWeight
	for rows.Next() {
		var w models.BodyWeight
		var weightStr string
		if err := rows.Scan(&w.ID, &w.Date, &weightStr); err != nil {
			return nil, err
		}
		w.Weight, _ = decimal.NewFromString(weightStr)
		ws = append(ws, w)
	}
	return ws, rows.Err()
}

func (s *PostgresStore) InsertW(w models.BodyWeight) error {
	_, err := s.pool.Exec(context.Background(),
		"INSERT INTO weight (date, weight) VALUES ($1::date, $2)",
		w.Date, w.Weight.String())
	return err
}

func (s *PostgresStore) DeleteW(id int) error {
	_, err := s.pool.Exec(context.Background(), "DELETE FROM weight WHERE id = $1", id)
	return err
}
