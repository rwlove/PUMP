package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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

func (s *PostgresStore) SelectEx(ctx context.Context) ([]models.Exercise, error) {
	slog.Debug("db: SelectEx")
	rows, err := s.pool.Query(ctx,
		`SELECT id, gr, place, name, descr, image, color, weight::text, reps, voltra
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
			&ex.Image, &ex.Color, &weightStr, &ex.Reps, &ex.Voltra); err != nil {
			return nil, err
		}
		ex.Weight, _ = decimal.NewFromString(weightStr)
		exes = append(exes, ex)
	}
	slog.Debug("db: SelectEx complete", slog.Int("rows", len(exes)))
	return exes, rows.Err()
}

func (s *PostgresStore) InsertEx(ctx context.Context, ex models.Exercise) error {
	slog.Debug("db: InsertEx", slog.String("name", ex.Name), slog.String("group", ex.Group))
	_, err := s.pool.Exec(ctx,
		`INSERT INTO exercises (gr, place, name, descr, image, color, weight, reps, voltra)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		ex.Group, ex.Place, ex.Name, ex.Descr, ex.Image, ex.Color,
		ex.Weight.String(), ex.Reps, ex.Voltra)
	if err != nil {
		slog.Debug("db: InsertEx failed", slog.Any("error", err))
	}
	return err
}

// UpdateEx rewrites an existing exercise in place, preserving its id.
// Returns false when no row has that id (caller may fall back to insert,
// preserving the old delete+insert upsert semantics).
func (s *PostgresStore) UpdateEx(ctx context.Context, ex models.Exercise) (bool, error) {
	slog.Debug("db: UpdateEx", slog.Int("id", ex.ID), slog.String("name", ex.Name))
	ct, err := s.pool.Exec(ctx,
		`UPDATE exercises SET gr = $1, place = $2, name = $3, descr = $4,
		        image = $5, color = $6, weight = $7, reps = $8, voltra = $9
		 WHERE id = $10`,
		ex.Group, ex.Place, ex.Name, ex.Descr, ex.Image, ex.Color,
		ex.Weight.String(), ex.Reps, ex.Voltra, ex.ID)
	if err != nil {
		slog.Debug("db: UpdateEx failed", slog.Int("id", ex.ID), slog.Any("error", err))
		return false, err
	}
	if ct.RowsAffected() == 0 {
		return false, nil
	}
	// Keep the denormalized copy on sets in step with the exercise, for the
	// same reason as UpdateExColor. Matching on the new name means a rename
	// does not carry history with it — that is pre-existing behaviour here,
	// since sets reference exercises by free text and not by id.
	if _, err := s.pool.Exec(ctx,
		"UPDATE sets SET workout_color = $1 WHERE name = $2", ex.Color, ex.Name,
	); err != nil {
		slog.Debug("db: UpdateEx set recolor failed", slog.Int("id", ex.ID), slog.Any("error", err))
		return true, err
	}
	return true, nil
}

func (s *PostgresStore) DeleteEx(ctx context.Context, id int) error {
	slog.Debug("db: DeleteEx", slog.Int("id", id))
	_, err := s.pool.Exec(ctx, "DELETE FROM exercises WHERE id = $1", id)
	if err != nil {
		slog.Debug("db: DeleteEx failed", slog.Int("id", id), slog.Any("error", err))
	}
	return err
}

func (s *PostgresStore) UpdateExColor(ctx context.Context, id int, color string) error {
	slog.Debug("db: UpdateExColor", slog.Int("id", id), slog.String("color", color))
	// Recolor the exercise's logged sets in the same statement. sets carries a
	// denormalized copy of the color, taken when the set was logged, and the
	// workout page draws its stripes from that copy — so without this a color
	// change leaves every past set showing the old shade, and one exercise ends
	// up with several stripe colors across its history.
	_, err := s.pool.Exec(ctx,
		`WITH ex AS (
		     UPDATE exercises SET color = $1 WHERE id = $2 RETURNING name
		 )
		 UPDATE sets SET workout_color = $1 FROM ex WHERE sets.name = ex.name`,
		color, id)
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

func (s *PostgresStore) SelectSet(ctx context.Context) ([]models.Set, error) {
	slog.Debug("db: SelectSet")
	rows, err := s.pool.Query(ctx,
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

// SelectSetsSince returns sets dated on or after cutoff (YYYY-MM-DD), in
// the same id order as SelectSet. Page handlers with bounded display
// windows use this so render cost doesn't grow with total history.
func (s *PostgresStore) SelectSetsSince(ctx context.Context, cutoff string) ([]models.Set, error) {
	slog.Debug("db: SelectSetsSince", slog.String("cutoff", cutoff))
	rows, err := s.pool.Query(ctx,
		`SELECT `+setColumns+` FROM sets WHERE date >= $1::date ORDER BY id ASC`, cutoff)
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
	slog.Debug("db: SelectSetsSince complete", slog.Int("rows", len(sets)))
	return sets, rows.Err()
}

func (s *PostgresStore) GetSet(ctx context.Context, id int) (models.Set, error) {
	slog.Debug("db: GetSet", slog.Int("id", id))
	row := s.pool.QueryRow(ctx,
		`SELECT `+setColumns+` FROM sets WHERE id = $1`, id)
	return scanSet(row)
}

func (s *PostgresStore) InsertSet(ctx context.Context, set models.Set) (models.Set, error) {
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

	row := s.pool.QueryRow(ctx,
		`INSERT INTO sets (date, name, color, workout_color, weight, reps,
		                   note, source, confidence, pending, clip_path)
		 VALUES ($1::date, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 RETURNING `+setColumns,
		set.Date, set.Name, set.Color, set.WorkoutColor,
		set.Weight.String(), set.Reps, set.Note,
		source, confidence, set.Pending, set.ClipPath)
	stored, err := scanSet(row)
	if err != nil {
		slog.Debug("db: InsertSet failed", slog.Any("error", err))
	}
	return stored, err
}

func (s *PostgresStore) UpdateSet(ctx context.Context, id int, upd models.SetUpdate) (models.Set, error) {
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
		// Nothing to change; return the current row so callers can still
		// publish an SSE payload.
		return s.GetSet(ctx, id)
	}

	args = append(args, id)
	q := fmt.Sprintf("UPDATE sets SET %s WHERE id = $%d RETURNING %s",
		strings.Join(cols, ", "), len(args), setColumns)

	stored, err := scanSet(s.pool.QueryRow(ctx, q, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		// No row with that id — same silent no-op as the previous UPDATE.
		return models.Set{}, nil
	}
	if err != nil {
		slog.Debug("db: UpdateSet failed", slog.Int("id", id), slog.Any("error", err))
	}
	return stored, err
}

// DeleteSet removes a set and returns its date so SSE events can carry it
// without a separate lookup. Deleting a nonexistent id returns ("", nil).
func (s *PostgresStore) DeleteSet(ctx context.Context, id int) (string, error) {
	slog.Debug("db: DeleteSet", slog.Int("id", id))
	var date string
	err := s.pool.QueryRow(ctx,
		"DELETE FROM sets WHERE id = $1 RETURNING date::text", id).Scan(&date)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		slog.Debug("db: DeleteSet failed", slog.Int("id", id), slog.Any("error", err))
	}
	return date, err
}

func (s *PostgresStore) BulkReplaceSetsByDate(ctx context.Context, date string, sets []models.Set) error {
	slog.Debug("db: BulkReplaceSetsByDate", slog.String("date", date), slog.Int("sets", len(sets)))
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, "DELETE FROM sets WHERE date = $1::date", date); err != nil {
		return err
	}
	slog.Debug("db: deleted existing sets for date", slog.String("date", date))

	b := &pgx.Batch{}
	for _, set := range sets {
		b.Queue(
			`INSERT INTO sets (date, name, color, workout_color, weight, reps, note)
			 VALUES ($1::date, $2, $3, $4, $5, $6, $7)`,
			set.Date, set.Name, set.Color, set.WorkoutColor,
			set.Weight.String(), set.Reps, set.Note)
	}
	br := tx.SendBatch(ctx, b)
	for i := range sets {
		if _, err := br.Exec(); err != nil {
			slog.Debug("db: insert set failed", slog.Int("index", i), slog.Any("error", err))
			_ = br.Close()
			return err
		}
	}
	if err := br.Close(); err != nil {
		return err
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
func (s *PostgresStore) SelectW(ctx context.Context) ([]models.BodyWeight, error) {
	slog.Debug("db: SelectW")
	rows, err := s.pool.Query(ctx,
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
func (s *PostgresStore) InsertW(ctx context.Context, w models.BodyWeight) error {
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
	_, err := s.pool.Exec(ctx,
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
func (s *PostgresStore) DeleteW(ctx context.Context, id int) error {
	slog.Debug("db: DeleteW", slog.Int("id", id))
	_, err := s.pool.Exec(ctx,
		"DELETE FROM weight WHERE date = (SELECT date FROM weight WHERE id = $1)", id)
	return err
}

// ─── app config (UI-editable settings) ────────────────────────────────────────

// GetAppConfig returns the persisted UI-editable settings. Returns
// (zero, false, nil) when no row exists yet, so a fresh install falls
// back to env-var defaults instead of the schema's DEFAULT values.
func (s *PostgresStore) GetAppConfig(ctx context.Context) (models.Conf, bool, error) {
	slog.Debug("db: GetAppConfig")
	var cfg models.Conf
	err := s.pool.QueryRow(ctx,
		`SELECT color, page_step, frequency_days, display_days, autofill, cv_autolog
		 FROM app_config WHERE id = 1`).Scan(
		&cfg.Color, &cfg.PageStep, &cfg.FrequencyDays, &cfg.DisplayDays,
		&cfg.AutoFill, &cfg.CVAutoLog)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Conf{}, false, nil
	}
	if err != nil {
		return models.Conf{}, false, err
	}
	return cfg, true, nil
}

// SaveAppConfig upserts the UI-editable settings. Pushover credentials
// and NodePath are env-only and never persisted (see the Conf comment).
func (s *PostgresStore) SaveAppConfig(ctx context.Context, cfg models.Conf) error {
	slog.Debug("db: SaveAppConfig",
		slog.String("color", cfg.Color),
		slog.Bool("cv_autolog", cfg.CVAutoLog))
	_, err := s.pool.Exec(ctx,
		`INSERT INTO app_config (id, color, page_step, frequency_days, display_days, autofill, cv_autolog, updated_at)
		 VALUES (1, $1, $2, $3, $4, $5, $6, NOW())
		 ON CONFLICT (id) DO UPDATE SET
		     color          = EXCLUDED.color,
		     page_step      = EXCLUDED.page_step,
		     frequency_days = EXCLUDED.frequency_days,
		     display_days   = EXCLUDED.display_days,
		     autofill       = EXCLUDED.autofill,
		     cv_autolog     = EXCLUDED.cv_autolog,
		     updated_at     = NOW()`,
		cfg.Color, cfg.PageStep, cfg.FrequencyDays, cfg.DisplayDays,
		cfg.AutoFill, cfg.CVAutoLog)
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
func (s *PostgresStore) InsertHealthRecords(ctx context.Context, recs []models.HealthRecord) (int, error) {
	slog.Debug("db: InsertHealthRecords", slog.Int("count", len(recs)))
	if len(recs) == 0 {
		return 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	b := &pgx.Batch{}
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
		b.Queue(
			`INSERT INTO health_record (metric_type, start_time, end_time, value, unit, extra, source)
			 VALUES ($1, $2, $3, $4::numeric, $5, $6::jsonb, $7)
			 ON CONFLICT ON CONSTRAINT health_record_dedupe DO NOTHING`,
			r.MetricType, r.StartTime, endTime, value, r.Unit, extra, source)
	}
	br := tx.SendBatch(ctx, b)
	inserted := 0
	for _, r := range recs {
		ct, err := br.Exec()
		if err != nil {
			slog.Debug("db: InsertHealthRecords row failed",
				slog.String("type", r.MetricType), slog.Any("error", err))
			_ = br.Close()
			return inserted, err
		}
		inserted += int(ct.RowsAffected())
	}
	if err := br.Close(); err != nil {
		return inserted, err
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
func (s *PostgresStore) SelectHealthRecords(ctx context.Context, metricType string, since time.Time) ([]models.HealthRecord, error) {
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

	rows, err := s.pool.Query(ctx, q, args...)
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
