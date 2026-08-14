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
		`SELECT id, gr, place, name, descr, image, color, weight::text, reps, voltra, focus, bodyweight
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
			&ex.Image, &ex.Color, &weightStr, &ex.Reps, &ex.Voltra,
			&ex.Focus, &ex.Bodyweight); err != nil {
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
		`INSERT INTO exercises (gr, place, name, descr, image, color, weight, reps, voltra, focus, bodyweight)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		ex.Group, ex.Place, ex.Name, ex.Descr, ex.Image, ex.Color,
		ex.Weight.String(), ex.Reps, ex.Voltra, ex.Focus, ex.Bodyweight)
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

	// One transaction: the exercise row and the denormalized copy of its color
	// on sets must move together, or a failure between them leaves the palette
	// split — exercise in the new color, its whole logged history in the old.
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	var prevName, prevColor string
	err = tx.QueryRow(ctx,
		"SELECT name, color FROM exercises WHERE id = $1 FOR UPDATE", ex.ID).
		Scan(&prevName, &prevColor)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// An absent color must not erase one. PUT /api/exercises/:id binds into a
	// zero-valued Exercise, so a caller that omits Color sends "" — and since
	// the color is mirrored onto every logged set, treating that as "clear it"
	// would blank the exercise's entire history from one incomplete request.
	if ex.Color == "" {
		ex.Color = prevColor
	}

	if _, err := tx.Exec(ctx,
		`UPDATE exercises SET gr = $1, place = $2, name = $3, descr = $4,
		        image = $5, color = $6, weight = $7, reps = $8, voltra = $9,
		        focus = $10, bodyweight = $11
		 WHERE id = $12`,
		ex.Group, ex.Place, ex.Name, ex.Descr, ex.Image, ex.Color,
		ex.Weight.String(), ex.Reps, ex.Voltra, ex.Focus, ex.Bodyweight, ex.ID); err != nil {
		slog.Debug("db: UpdateEx failed", slog.Int("id", ex.ID), slog.Any("error", err))
		return false, err
	}

	// Only propagate the color when the name is unchanged.
	//
	// sets reference exercises by free text, not by id, so the name is the only
	// join. Recoloring by the *new* name after a rename would repaint whichever
	// exercise already owned that name: renaming "Incline DB Press" to the
	// existing "Bench Press" would overwrite every historical Bench Press
	// stripe with the other exercise's color, unrecoverably. Recoloring by the
	// old name is just as wrong — those rows are orphaned by the rename and
	// their color is now historical. So a rename touches no sets at all.
	//
	// The IS DISTINCT FROM guard keeps this a no-op when the color did not
	// change, so editing only reps or weight does not rewrite the largest
	// table in the database.
	if prevName == ex.Name {
		if _, err := tx.Exec(ctx,
			`UPDATE sets SET workout_color = $1
			 WHERE name = $2 AND workout_color IS DISTINCT FROM $1`,
			ex.Color, ex.Name); err != nil {
			slog.Debug("db: UpdateEx set recolor failed",
				slog.Int("id", ex.ID), slog.Any("error", err))
			return false, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
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
	weight::text, reps, note, source, confidence, pending, clip_path,
	position, added_weight::text`

func scanSet(row interface{ Scan(...any) error }) (models.Set, error) {
	var set models.Set
	var weightStr string
	var addedStr *string
	if err := row.Scan(&set.ID, &set.Date, &set.Name, &set.Color,
		&set.WorkoutColor, &weightStr, &set.Reps, &set.Note,
		&set.Source, &set.Confidence, &set.Pending, &set.ClipPath,
		&set.Position, &addedStr); err != nil {
		return set, err
	}
	set.Weight, _ = decimal.NewFromString(weightStr)
	if addedStr != nil {
		if d, err := decimal.NewFromString(*addedStr); err == nil {
			set.AddedWeight = &d
		}
	}
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
		`SELECT `+setColumns+` FROM sets WHERE date >= $1::date
		 ORDER BY date ASC, position ASC, id ASC`, cutoff)
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
	var added interface{}
	if set.AddedWeight != nil {
		added = set.AddedWeight.String()
	}

	// position defaults to one past the current max for the date so a new set
	// appends to the bottom of the day's log. COALESCE handles the first set of
	// a new day (no existing rows → -1 + 1 = 0).
	row := s.pool.QueryRow(ctx,
		`INSERT INTO sets (date, name, color, workout_color, weight, reps,
		                   note, source, confidence, pending, clip_path,
		                   position, added_weight)
		 VALUES ($1::date, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
		         (SELECT COALESCE(MAX(position), -1) + 1 FROM sets WHERE date = $1::date),
		         $12::numeric)
		 RETURNING `+setColumns,
		set.Date, set.Name, set.Color, set.WorkoutColor,
		set.Weight.String(), set.Reps, set.Note,
		source, confidence, set.Pending, set.ClipPath, added)
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
	if upd.AddedWeight != nil {
		add("added_weight", upd.AddedWeight.String())
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
	for i, set := range sets {
		var added interface{}
		if set.AddedWeight != nil {
			added = set.AddedWeight.String()
		}
		b.Queue(
			`INSERT INTO sets (date, name, color, workout_color, weight, reps, note, position, added_weight)
			 VALUES ($1::date, $2, $3, $4, $5, $6, $7, $8, $9::numeric)`,
			// Bind the date argument, not set.Date. The DELETE above clears
			// only the requested date, so honouring a per-row date would
			// append rows to a day that was never cleared — emptying the
			// target date and silently duplicating into a different one.
			//
			// position is the row's index in the submitted order, so the
			// DOM order the browser sent is what the day reloads in.
			date, set.Name, set.Color, set.WorkoutColor,
			set.Weight.String(), set.Reps, set.Note, i, added)
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

// ReorderSets rewrites the position of each set on a date to match the order
// of orderedIDs (index 0 = top of the log). Used by the per-set save path
// where rows are addressed by id — the bulk path assigns position from
// insertion order instead. Ids not on the given date are ignored, so a stale
// client list can't move rows into another day.
func (s *PostgresStore) ReorderSets(ctx context.Context, date string, orderedIDs []int) error {
	slog.Debug("db: ReorderSets", slog.String("date", date), slog.Int("count", len(orderedIDs)))
	if len(orderedIDs) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	b := &pgx.Batch{}
	for i, id := range orderedIDs {
		b.Queue(
			`UPDATE sets SET position = $1 WHERE id = $2 AND date = $3::date`,
			i, id, date)
	}
	br := tx.SendBatch(ctx, b)
	for range orderedIDs {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return err
		}
	}
	if err := br.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// LastPerformed returns, for every exercise name that has ever been logged,
// the most recent date it was performed and its position (order performed) in
// that session. It drives the picker's recency ordering (Feature 2) — an
// exercise's rank comes from when it was last done, not from a display window,
// so a group's last session is honoured however long ago it was.
func (s *PostgresStore) LastPerformed(ctx context.Context) (map[string]models.ExerciseRecency, error) {
	slog.Debug("db: LastPerformed")
	// DISTINCT ON (name) with this ORDER keeps, per name, the row on the latest
	// date and — among that date's rows — the smallest position (first done).
	rows, err := s.pool.Query(ctx,
		`SELECT DISTINCT ON (name) name, date::text, position
		 FROM sets ORDER BY name, date DESC, position ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]models.ExerciseRecency)
	for rows.Next() {
		var name string
		var r models.ExerciseRecency
		if err := rows.Scan(&name, &r.LastDate, &r.Pos); err != nil {
			return nil, err
		}
		out[name] = r
	}
	return out, rows.Err()
}

// ─── muscles (DB-editable focus-muscle catalog) ────────────────────────────────

// SelectMuscles returns the full muscle catalog ordered by group then sort
// order then name, so the UI can render each group's muscles in a stable order.
func (s *PostgresStore) SelectMuscles(ctx context.Context) ([]models.Muscle, error) {
	slog.Debug("db: SelectMuscles")
	rows, err := s.pool.Query(ctx,
		`SELECT id, gr, name, sort_order FROM muscles
		 ORDER BY gr ASC, sort_order ASC, name ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Muscle
	for rows.Next() {
		var m models.Muscle
		if err := rows.Scan(&m.ID, &m.Group, &m.Name, &m.Sort); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// InsertMuscle adds a muscle to a group. Duplicate (group, name) pairs are a
// no-op via the unique constraint, so re-adding is harmless.
func (s *PostgresStore) InsertMuscle(ctx context.Context, m models.Muscle) error {
	slog.Debug("db: InsertMuscle", slog.String("group", m.Group), slog.String("name", m.Name))
	_, err := s.pool.Exec(ctx,
		`INSERT INTO muscles (gr, name, sort_order) VALUES ($1, $2, $3)
		 ON CONFLICT ON CONSTRAINT muscles_group_name_uniq DO NOTHING`,
		m.Group, m.Name, m.Sort)
	return err
}

// DeleteMuscle removes a muscle from the catalog by id. Exercises that
// referenced it keep their focus text — the catalog only gates the create
// form's dropdown, it is not a foreign key.
func (s *PostgresStore) DeleteMuscle(ctx context.Context, id int) error {
	slog.Debug("db: DeleteMuscle", slog.Int("id", id))
	_, err := s.pool.Exec(ctx, "DELETE FROM muscles WHERE id = $1", id)
	return err
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
// gateway burst-posts the same reading several times as it settles. Deleting
// only the targeted id would leave those duplicates behind, so SelectW would
// surface the next one and the value would appear unchanged ("delete didn't
// work").
//
// So delete every row sharing that id's date *and value*, not the whole day.
// InsertW dedups identical same-day readings at ingest now, but still permits
// genuinely different ones — weighing 272.4 in the morning and 270.8 at night
// is supported and meaningful. Deleting by date alone would take the morning
// reading with the evening one.
func (s *PostgresStore) DeleteW(ctx context.Context, id int) error {
	slog.Debug("db: DeleteW", slog.Int("id", id))
	_, err := s.pool.Exec(ctx,
		`DELETE FROM weight
		 WHERE (date, weight) = (SELECT date, weight FROM weight WHERE id = $1)`, id)
	return err
}

// ─── app config (UI-editable settings) ────────────────────────────────────────

// GetAppConfig returns the persisted UI-editable settings. Returns
// (zero, false, nil) when no row exists yet, so a fresh install falls
// back to env-var defaults instead of the schema's DEFAULT values.
func (s *PostgresStore) GetAppConfig(ctx context.Context) (models.Conf, bool, error) {
	slog.Debug("db: GetAppConfig")
	var cfg models.Conf
	// frequency_days is a vestigial column (the frequency-based exercise sort
	// was replaced by recency ordering); it stays in the schema but is no
	// longer read or written here.
	err := s.pool.QueryRow(ctx,
		`SELECT color, page_step, display_days, autofill, cv_autolog
		 FROM app_config WHERE id = 1`).Scan(
		&cfg.Color, &cfg.PageStep, &cfg.DisplayDays,
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
	// frequency_days is intentionally omitted — see GetAppConfig. Fresh rows
	// keep the column's schema DEFAULT; existing rows keep their stored value.
	_, err := s.pool.Exec(ctx,
		`INSERT INTO app_config (id, color, page_step, display_days, autofill, cv_autolog, updated_at)
		 VALUES (1, $1, $2, $3, $4, $5, NOW())
		 ON CONFLICT (id) DO UPDATE SET
		     color        = EXCLUDED.color,
		     page_step    = EXCLUDED.page_step,
		     display_days = EXCLUDED.display_days,
		     autofill     = EXCLUDED.autofill,
		     cv_autolog   = EXCLUDED.cv_autolog,
		     updated_at   = NOW()`,
		cfg.Color, cfg.PageStep, cfg.DisplayDays,
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
