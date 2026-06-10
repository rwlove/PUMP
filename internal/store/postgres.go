package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

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
	slog.Debug("postgres: dialing")
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	slog.Debug("postgres: ping OK")
	return &PostgresStore{pool: pool}, nil
}

// Pool exposes the underlying connection pool (used by the migrate package).
func (s *PostgresStore) Pool() *pgxpool.Pool {
	return s.pool
}

// ─── exercises ────────────────────────────────────────────────────────────────

func (s *PostgresStore) SelectEx() ([]models.Exercise, error) {
	slog.Debug("db: SelectEx")
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
	slog.Debug("db: SelectEx complete", slog.Int("rows", len(exes)))
	return exes, rows.Err()
}

func (s *PostgresStore) InsertEx(ex models.Exercise) error {
	slog.Debug("db: InsertEx", slog.String("name", ex.Name), slog.String("group", ex.Group))
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO exercises (gr, place, name, descr, image, color, weight, reps)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		ex.Group, ex.Place, ex.Name, ex.Descr, ex.Image, ex.Color,
		ex.Weight.String(), ex.Reps)
	if err != nil {
		slog.Debug("db: InsertEx failed", slog.Any("error", err))
	}
	return err
}

func (s *PostgresStore) DeleteEx(id int) error {
	slog.Debug("db: DeleteEx", slog.Int("id", id))
	_, err := s.pool.Exec(context.Background(), "DELETE FROM exercises WHERE id = $1", id)
	if err != nil {
		slog.Debug("db: DeleteEx failed", slog.Int("id", id), slog.Any("error", err))
	}
	return err
}

func (s *PostgresStore) UpdateExColor(id int, color string) error {
	slog.Debug("db: UpdateExColor", slog.Int("id", id), slog.String("color", color))
	_, err := s.pool.Exec(context.Background(),
		"UPDATE exercises SET color = $1 WHERE id = $2", color, id)
	return err
}

// ─── sets ─────────────────────────────────────────────────────────────────────

const setColumns = `id, date::text, name, color, workout_color,
	weight::text, reps, note, source, confidence, pending, clip_path`

func scanSet(row interface{ Scan(...any) error }) (models.Set, error) {
	var set models.Set
	var weightStr string
	if err := row.Scan(&set.ID, &set.Date, &set.Name, &set.Color,
		&set.WorkoutColor, &weightStr, &set.Reps, &set.Note,
		&set.Source, &set.Confidence, &set.Pending, &set.ClipPath); err != nil {
		return set, err
	}
	set.Weight, _ = decimal.NewFromString(weightStr)
	return set, nil
}

func (s *PostgresStore) SelectSet() ([]models.Set, error) {
	slog.Debug("db: SelectSet")
	rows, err := s.pool.Query(context.Background(),
		`SELECT `+setColumns+` FROM sets ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sets []models.Set
	for rows.Next() {
		set, err := scanSet(rows)
		if err != nil {
			return nil, err
		}
		sets = append(sets, set)
	}
	slog.Debug("db: SelectSet complete", slog.Int("rows", len(sets)))
	return sets, rows.Err()
}

func (s *PostgresStore) GetSet(id int) (models.Set, error) {
	slog.Debug("db: GetSet", slog.Int("id", id))
	row := s.pool.QueryRow(context.Background(),
		`SELECT `+setColumns+` FROM sets WHERE id = $1`, id)
	return scanSet(row)
}

func (s *PostgresStore) InsertSet(set models.Set) (int, error) {
	slog.Debug("db: InsertSet",
		slog.String("date", set.Date), slog.String("name", set.Name),
		slog.String("source", set.Source))

	source := set.Source
	if source == "" {
		source = "manual"
	}
	confidence := set.Confidence
	if confidence == 0 {
		confidence = 1.0
	}

	var id int
	err := s.pool.QueryRow(context.Background(),
		`INSERT INTO sets (date, name, color, workout_color, weight, reps,
		                   note, source, confidence, pending, clip_path)
		 VALUES ($1::date, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING id`,
		set.Date, set.Name, set.Color, set.WorkoutColor,
		set.Weight.String(), set.Reps, set.Note,
		source, confidence, set.Pending, set.ClipPath).Scan(&id)
	if err != nil {
		slog.Debug("db: InsertSet failed", slog.Any("error", err))
	}
	return id, err
}

func (s *PostgresStore) UpdateSet(id int, upd models.SetUpdate) error {
	slog.Debug("db: UpdateSet", slog.Int("id", id))

	cols := []string{}
	args := []any{}
	add := func(col string, val any) {
		cols = append(cols, fmt.Sprintf("%s = $%d", col, len(args)+1))
		args = append(args, val)
	}

	if upd.Name != nil {
		add("name", *upd.Name)
	}
	if upd.Weight != nil {
		add("weight", upd.Weight.String())
	}
	if upd.Reps != nil {
		add("reps", *upd.Reps)
	}
	if upd.Note != nil {
		add("note", *upd.Note)
	}
	if upd.Confidence != nil {
		add("confidence", *upd.Confidence)
	}
	if upd.Pending != nil {
		add("pending", *upd.Pending)
	}
	if upd.ClipPath != nil {
		add("clip_path", *upd.ClipPath)
	}

	if len(cols) == 0 {
		return nil
	}

	args = append(args, id)
	q := fmt.Sprintf("UPDATE sets SET %s WHERE id = $%d",
		strings.Join(cols, ", "), len(args))

	_, err := s.pool.Exec(context.Background(), q, args...)
	if err != nil {
		slog.Debug("db: UpdateSet failed", slog.Int("id", id), slog.Any("error", err))
	}
	return err
}

func (s *PostgresStore) DeleteSet(id int) error {
	slog.Debug("db: DeleteSet", slog.Int("id", id))
	_, err := s.pool.Exec(context.Background(), "DELETE FROM sets WHERE id = $1", id)
	if err != nil {
		slog.Debug("db: DeleteSet failed", slog.Int("id", id), slog.Any("error", err))
	}
	return err
}

func (s *PostgresStore) BulkReplaceSetsByDate(date string, sets []models.Set) error {
	slog.Debug("db: BulkReplaceSetsByDate", slog.String("date", date), slog.Int("sets", len(sets)))
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "DELETE FROM sets WHERE date = $1::date", date); err != nil {
		return err
	}
	slog.Debug("db: deleted existing sets for date", slog.String("date", date))

	for i, set := range sets {
		if _, err := tx.Exec(ctx,
			`INSERT INTO sets (date, name, color, workout_color, weight, reps, note)
			 VALUES ($1::date, $2, $3, $4, $5, $6, $7)`,
			set.Date, set.Name, set.Color, set.WorkoutColor,
			set.Weight.String(), set.Reps, set.Note); err != nil {
			slog.Debug("db: insert set failed", slog.Int("index", i), slog.Any("error", err))
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	slog.Debug("db: BulkReplaceSetsByDate committed", slog.String("date", date), slog.Int("count", len(sets)))
	return nil
}

// ─── weight ───────────────────────────────────────────────────────────────────

// SelectW returns one BodyWeight per distinct date — the latest by
// recorded_at — sorted oldest-first. Raw per-reading rows persist in the
// table; this query just collapses to one-displayed-per-date so the UI
// can show "today's weight" and the chart plots one point per day.
// Delete the displayed row to reveal the next-latest reading for that date.
func (s *PostgresStore) SelectW() ([]models.BodyWeight, error) {
	slog.Debug("db: SelectW")
	rows, err := s.pool.Query(context.Background(),
		`SELECT DISTINCT ON (date) id, date::text, recorded_at::text, weight::text
		 FROM weight
		 ORDER BY date, recorded_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ws []models.BodyWeight
	for rows.Next() {
		var w models.BodyWeight
		var weightStr string
		if err := rows.Scan(&w.ID, &w.Date, &w.RecordedAt, &weightStr); err != nil {
			return nil, err
		}
		w.Weight, _ = decimal.NewFromString(weightStr)
		ws = append(ws, w)
	}
	slog.Debug("db: SelectW complete", slog.Int("rows", len(ws)))
	return ws, rows.Err()
}

// InsertW persists one raw weight reading. If RecordedAt is empty the DB
// default (NOW()) is used — covers the manual web-form path where the
// browser hasn't supplied a precise instant. Scale clients (ESPHome) send
// the precise reading time.
func (s *PostgresStore) InsertW(w models.BodyWeight) error {
	slog.Debug("db: InsertW", slog.String("date", w.Date), slog.String("recorded_at", w.RecordedAt), slog.String("weight", w.Weight.String()))
	var recordedAt interface{}
	if w.RecordedAt != "" {
		recordedAt = w.RecordedAt
	}
	// Dedup identical same-day readings. The bt-scale gateway burst-posts the
	// same value several times as the reading settles/re-sends, which would
	// otherwise pile up duplicate rows (the reason DeleteW deletes by date).
	// A genuinely different value on the same day still inserts, so multiple
	// distinct weigh-ins per day remain supported (latest recorded_at wins for
	// display). NUMERIC comparison ignores decimal-string formatting, so
	// "272.2" and "272.20" are treated as equal.
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO weight (date, recorded_at, weight)
		 SELECT $1::date, COALESCE($2::timestamptz, NOW()), $3::numeric
		 WHERE NOT EXISTS (
		     SELECT 1 FROM weight WHERE date = $1::date AND weight = $3::numeric
		 )`,
		w.Date, recordedAt, w.Weight.String())
	return err
}

// DeleteW removes the body-weight entry the log shows for the given reading's
// day. The log is day-oriented — SelectW returns one row per date via
// DISTINCT ON — but a single day can hold several raw rows: the bt-scale
// gateway burst-posts the same reading several times and there is no
// ingest-side dedup. Deleting only the targeted id would leave the day's
// duplicates behind, so SelectW would surface the next one and the value would
// appear unchanged ("delete didn't work"). Delete every row sharing that id's
// date so the displayed entry actually disappears.
func (s *PostgresStore) DeleteW(id int) error {
	slog.Debug("db: DeleteW", slog.Int("id", id))
	_, err := s.pool.Exec(context.Background(),
		"DELETE FROM weight WHERE date = (SELECT date FROM weight WHERE id = $1)", id)
	return err
}

// ─── health records (Android Health Connect) ───────────────────────────────────

// InsertHealthRecords persists a batch of wearable health records in one
// transaction. Each row is deduped via ON CONFLICT on
// (metric_type, start_time, end_time) DO NOTHING, so the HC Webhook bridge's
// rolling-48h re-delivery collapses to no-ops. Returns the count inserted
// (excludes conflicts). end_time is expected non-NULL (the parser sets it to
// start_time for instantaneous samples) so dedupe works with standard
// NULL-distinct semantics.
func (s *PostgresStore) InsertHealthRecords(recs []models.HealthRecord) (int, error) {
	slog.Debug("db: InsertHealthRecords", slog.Int("count", len(recs)))
	if len(recs) == 0 {
		return 0, nil
	}
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	inserted := 0
	for _, r := range recs {
		var endTime interface{}
		if r.EndTime != nil {
			endTime = *r.EndTime
		}
		var value interface{}
		if r.Value != nil {
			value = r.Value.String()
		}
		var extra interface{}
		if len(r.Extra) > 0 {
			extra = []byte(r.Extra)
		}
		source := r.Source
		if source == "" {
			source = "health-connect"
		}
		ct, err := tx.Exec(ctx,
			`INSERT INTO health_record (metric_type, start_time, end_time, value, unit, extra, source)
			 VALUES ($1, $2, $3, $4::numeric, $5, $6::jsonb, $7)
			 ON CONFLICT ON CONSTRAINT health_record_dedupe DO NOTHING`,
			r.MetricType, r.StartTime, endTime, value, r.Unit, extra, source)
		if err != nil {
			slog.Debug("db: InsertHealthRecords row failed",
				slog.String("type", r.MetricType), slog.Any("error", err))
			return inserted, err
		}
		inserted += int(ct.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return inserted, err
	}
	slog.Debug("db: InsertHealthRecords committed",
		slog.Int("received", len(recs)), slog.Int("inserted", inserted))
	return inserted, nil
}

// SelectHealthRecords returns records at or after since, newest first.
// metricType "" returns all types. Capped at 5000 rows to bound the
// response for the UI.
func (s *PostgresStore) SelectHealthRecords(metricType string, since time.Time) ([]models.HealthRecord, error) {
	slog.Debug("db: SelectHealthRecords",
		slog.String("type", metricType), slog.Time("since", since))

	q := `SELECT id, metric_type, start_time, end_time, value::text, unit, extra, source, ingested_at
	      FROM health_record
	      WHERE start_time >= $1`
	args := []any{since}
	if metricType != "" {
		q += ` AND metric_type = $2`
		args = append(args, metricType)
	}
	q += ` ORDER BY start_time DESC LIMIT 5000`

	rows, err := s.pool.Query(context.Background(), q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.HealthRecord
	for rows.Next() {
		var r models.HealthRecord
		var endTime *time.Time
		var valueStr *string
		var extra []byte
		if err := rows.Scan(&r.ID, &r.MetricType, &r.StartTime, &endTime,
			&valueStr, &r.Unit, &extra, &r.Source, &r.IngestedAt); err != nil {
			return nil, err
		}
		r.EndTime = endTime
		if valueStr != nil {
			if d, err := decimal.NewFromString(*valueStr); err == nil {
				r.Value = &d
			}
		}
		if len(extra) > 0 {
			r.Extra = json.RawMessage(extra)
		}
		out = append(out, r)
	}
	slog.Debug("db: SelectHealthRecords complete", slog.Int("rows", len(out)))
	return out, rows.Err()
}
