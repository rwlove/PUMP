package store

import (
	"context"
	"os"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/db"
	"github.com/rwlove/PUMP/internal/models"
)

// These exercise the persistence layer against a real PostgreSQL, because the
// defects worth catching here are ones an in-memory fake cannot have: an
// UPDATE that matches more rows than intended, a transaction that isn't one, a
// DELETE whose subquery selects too much. Set PUMP_TEST_DSN to a database the
// test may freely destroy; without it these skip.
//
//	docker run -d -e POSTGRES_PASSWORD=x -p 5433:5432 postgres:16-alpine
//	PUMP_TEST_DSN='postgres://postgres:x@localhost:5433/pumptest?sslmode=disable' go test ./internal/store/
func testStore(t *testing.T) *PostgresStore {
	t.Helper()
	dsn := os.Getenv("PUMP_TEST_DSN")
	if dsn == "" {
		t.Skip("PUMP_TEST_DSN not set; skipping database-backed store tests")
	}
	s, err := NewPostgres(dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := db.MigratePostgres(s.Pool()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// Start every test from a known-empty state rather than depending on
	// execution order. programs is cleared first so its ON DELETE CASCADE
	// removes prescribed_sessions / prescribed_items (which FK exercises)
	// before exercises is truncated; sets (which also FK exercises via
	// exercise_id) is cleared before exercises for the same reason.
	for _, tbl := range []string{"programs", "session_feedback", "sets", "exercises", "weight"} {
		if _, err := s.Pool().Exec(context.Background(), "DELETE FROM "+tbl); err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}
	t.Cleanup(s.Pool().Close)
	return s
}

func mustInsertEx(t *testing.T, s *PostgresStore, name, group, color string) int {
	t.Helper()
	ctx := context.Background()
	id, err := s.InsertEx(ctx, models.Exercise{
		Name: name, Group: group, Color: color, Weight: decimal.NewFromInt(0),
	})
	if err != nil {
		t.Fatalf("InsertEx(%s): %v", name, err)
	}
	return id
}

func mustInsertSet(t *testing.T, s *PostgresStore, date, name, workoutColor string) {
	t.Helper()
	if _, err := s.Pool().Exec(context.Background(),
		`INSERT INTO sets (date, name, color, workout_color, weight, reps, note)
		 VALUES ($1::date, $2, '', $3, 0, 0, '')`, date, name, workoutColor); err != nil {
		t.Fatalf("insert set: %v", err)
	}
}

func setColors(t *testing.T, s *PostgresStore, name string) []string {
	t.Helper()
	rows, err := s.Pool().Query(context.Background(),
		"SELECT workout_color FROM sets WHERE name = $1 ORDER BY id", name)
	if err != nil {
		t.Fatalf("query colors: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, c)
	}
	return out
}

// Renaming an exercise onto a name another exercise already owns must not
// repaint that other exercise's logged history. sets join to exercises by free
// text, so recoloring by the post-rename name would overwrite every stripe
// belonging to the collided-with exercise, unrecoverably.
func TestUpdateEx_RenameOntoExistingNameLeavesItsHistoryAlone(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	inclineID := mustInsertEx(t, s, "Incline DB Press", "Chest", "#c0392b")
	mustInsertEx(t, s, "Bench Press", "Chest", "#2780e3")
	mustInsertSet(t, s, "2026-08-01", "Bench Press", "#2780e3")
	mustInsertSet(t, s, "2026-08-02", "Bench Press", "#2780e3")

	// Consolidate by renaming Incline onto the existing Bench Press.
	if _, err := s.UpdateEx(ctx, models.Exercise{
		ID: inclineID, Name: "Bench Press", Group: "Chest",
		Color: "#c0392b", Weight: decimal.NewFromInt(0),
	}); err != nil {
		t.Fatalf("UpdateEx: %v", err)
	}

	for i, got := range setColors(t, s, "Bench Press") {
		if got != "#2780e3" {
			t.Errorf("set %d repainted to %q; Bench Press history must keep #2780e3", i, got)
		}
	}
}

// A caller that omits Color (PUT /api/exercises/:id binds into a zero-valued
// struct) must not blank the exercise or its whole logged history.
func TestUpdateEx_EmptyColorDoesNotEraseHistory(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id := mustInsertEx(t, s, "Lat Pulldown", "Back", "#6f97d7")
	mustInsertSet(t, s, "2026-08-01", "Lat Pulldown", "#6f97d7")

	if _, err := s.UpdateEx(ctx, models.Exercise{
		ID: id, Name: "Lat Pulldown", Group: "Back",
		Color: "", Reps: 12, Weight: decimal.NewFromInt(0),
	}); err != nil {
		t.Fatalf("UpdateEx: %v", err)
	}

	var exColor string
	if err := s.Pool().QueryRow(ctx,
		"SELECT color FROM exercises WHERE id = $1", id).Scan(&exColor); err != nil {
		t.Fatalf("read exercise: %v", err)
	}
	if exColor != "#6f97d7" {
		t.Errorf("exercise color = %q, want the previous #6f97d7 preserved", exColor)
	}
	for i, got := range setColors(t, s, "Lat Pulldown") {
		if got != "#6f97d7" {
			t.Errorf("set %d color = %q, want #6f97d7", i, got)
		}
	}
}

// Renaming with no collision must leave the orphaned history untouched rather
// than dragging its color along.
func TestUpdateEx_RenameLeavesOrphanedHistoryUntouched(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id := mustInsertEx(t, s, "Old Name", "Legs", "#51b20f")
	mustInsertSet(t, s, "2026-08-01", "Old Name", "#51b20f")

	if _, err := s.UpdateEx(ctx, models.Exercise{
		ID: id, Name: "New Name", Group: "Legs",
		Color: "#88d963", Weight: decimal.NewFromInt(0),
	}); err != nil {
		t.Fatalf("UpdateEx: %v", err)
	}

	for i, got := range setColors(t, s, "Old Name") {
		if got != "#51b20f" {
			t.Errorf("orphaned set %d changed to %q, want #51b20f", i, got)
		}
	}
}

// The normal case still has to work: same name, new color, history follows.
func TestUpdateEx_ColorChangePropagatesToHistory(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id := mustInsertEx(t, s, "Push Ups", "Chest", "#111111")
	mustInsertSet(t, s, "2026-08-01", "Push Ups", "#111111")
	mustInsertSet(t, s, "2026-08-02", "Push Ups", "#111111")

	if _, err := s.UpdateEx(ctx, models.Exercise{
		ID: id, Name: "Push Ups", Group: "Chest",
		Color: "#d93530", Weight: decimal.NewFromInt(0),
	}); err != nil {
		t.Fatalf("UpdateEx: %v", err)
	}

	for i, got := range setColors(t, s, "Push Ups") {
		if got != "#d93530" {
			t.Errorf("set %d color = %q, want #d93530", i, got)
		}
	}
}

func TestUpdateEx_UnknownIDReportsNotFound(t *testing.T) {
	s := testStore(t)
	found, err := s.UpdateEx(context.Background(), models.Exercise{
		ID: 999999, Name: "Nope", Weight: decimal.NewFromInt(0),
	})
	if err != nil {
		t.Fatalf("UpdateEx: %v", err)
	}
	if found {
		t.Error("UpdateEx reported found for an id that does not exist")
	}
}

// UpdateExColor must also keep the denormalized copy in step.
func TestUpdateExColor_PropagatesToHistory(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	id := mustInsertEx(t, s, "Lunges", "Legs", "#000000")
	mustInsertSet(t, s, "2026-08-01", "Lunges", "#000000")

	if err := s.UpdateExColor(ctx, id, "#69d94f"); err != nil {
		t.Fatalf("UpdateExColor: %v", err)
	}
	for i, got := range setColors(t, s, "Lunges") {
		if got != "#69d94f" {
			t.Errorf("set %d color = %q, want #69d94f", i, got)
		}
	}
}

// The DELETE clears only the requested date, so the INSERT must bind that same
// date. Honouring a per-row Date would empty the target day and append
// duplicates to a day that was never cleared.
func TestBulkReplaceSetsByDate_IgnoresPerRowDate(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	mustInsertSet(t, s, "2026-08-02", "Squats", "#111")

	err := s.BulkReplaceSetsByDate(ctx, "2026-08-01", []models.Set{
		{Date: "2026-08-02", Name: "Squats", Weight: decimal.NewFromInt(100), Reps: 5},
	})
	if err != nil {
		t.Fatalf("BulkReplaceSetsByDate: %v", err)
	}

	var on1, on2 int
	if err := s.Pool().QueryRow(ctx,
		`SELECT count(*) FILTER (WHERE date = '2026-08-01'),
		        count(*) FILTER (WHERE date = '2026-08-02') FROM sets`).
		Scan(&on1, &on2); err != nil {
		t.Fatalf("count: %v", err)
	}
	if on1 != 1 {
		t.Errorf("target date has %d rows, want 1 — the row was written to the wrong day", on1)
	}
	if on2 != 1 {
		t.Errorf("untargeted date has %d rows, want its original 1 — it was appended to", on2)
	}
}

// Two genuinely different weigh-ins on one day are supported by InsertW, so
// deleting one must not take the other with it.
func TestDeleteW_KeepsOtherDistinctReadingSameDay(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for _, w := range []string{"272.4", "270.8"} {
		d, _ := decimal.NewFromString(w)
		if _, err := s.InsertW(ctx, models.BodyWeight{Date: "2026-08-01", Weight: d}); err != nil {
			t.Fatalf("InsertW %s: %v", w, err)
		}
	}

	var id int
	if err := s.Pool().QueryRow(ctx,
		"SELECT id FROM weight WHERE weight = 270.8").Scan(&id); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if err := s.DeleteW(ctx, id); err != nil {
		t.Fatalf("DeleteW: %v", err)
	}

	var remaining int
	if err := s.Pool().QueryRow(ctx,
		"SELECT count(*) FROM weight WHERE weight = 272.4").Scan(&remaining); err != nil {
		t.Fatalf("count: %v", err)
	}
	if remaining != 1 {
		t.Errorf("morning reading count = %d, want 1 — deleting the evening reading destroyed it", remaining)
	}
}

// Migration v25 collapses the identical same-day rows the racy pre-v25 dedup
// let through, keeping the earliest, and leaves distinct readings alone. The
// history is recreated by dropping the index, seeding, and re-running v25.
func TestMigrationV25_CollapsesDuplicateWeights(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.Pool().Exec(ctx, "DROP INDEX weight_date_weight_uniq"); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	for _, w := range []string{"272.4", "272.40", "272.4", "270.8"} {
		if _, err := s.Pool().Exec(ctx,
			"INSERT INTO weight (date, recorded_at, weight) VALUES ('2026-08-01', NOW(), $1::numeric)", w); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	var firstID int
	if err := s.Pool().QueryRow(ctx,
		"SELECT min(id) FROM weight WHERE weight = 272.4").Scan(&firstID); err != nil {
		t.Fatalf("lookup: %v", err)
	}

	var v25 string
	for _, m := range db.Migrations() {
		if m.Version == 25 {
			v25 = m.SQL
		}
	}
	if _, err := s.Pool().Exec(ctx, v25); err != nil {
		t.Fatalf("re-run v25: %v", err)
	}

	rows, err := s.Pool().Query(ctx, "SELECT id, weight::text FROM weight ORDER BY weight DESC")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var got []string
	var keptID int
	for rows.Next() {
		var id int
		var w string
		if err := rows.Scan(&id, &w); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, w)
		if len(got) == 1 {
			keptID = id
		}
	}
	if len(got) != 2 {
		t.Fatalf("rows after v25 = %v, want one 272.4 and one 270.8", got)
	}
	if keptID != firstID {
		t.Errorf("kept id %d, want the earliest copy %d", keptID, firstID)
	}

	// The index is back, so the schema itself now refuses a copy.
	if _, err := s.Pool().Exec(ctx,
		"INSERT INTO weight (date, recorded_at, weight) VALUES ('2026-08-01', NOW(), 272.4)"); err == nil {
		t.Error("duplicate (date, weight) insert succeeded after v25; unique index missing")
	}
}

// Guards the project's prime directive: a gap in the version sequence makes the
// runner silently skip every migration above it.
func TestMigrations_VersionsAreConsecutiveFromOne(t *testing.T) {
	for i, m := range db.Migrations() {
		if want := i + 1; m.Version != want {
			t.Fatalf("migration index %d has Version %d, want %d — gaps make the runner skip everything above",
				i, m.Version, want)
		}
	}
}

// Running the full migration set twice must be a no-op, so a partially applied
// state from a transient failure can be recovered by simply re-running.
func TestMigrations_AreIdempotent(t *testing.T) {
	s := testStore(t)
	if err := db.MigratePostgres(s.Pool()); err != nil {
		t.Fatalf("second migrate run failed: %v", err)
	}
	var n int
	if err := s.Pool().QueryRow(context.Background(),
		"SELECT count(*) FROM schema_version").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if want := len(db.Migrations()); n != want {
		t.Errorf("schema_version has %d rows, want %d", n, want)
	}
}

// The scale burst-posts one reading several times within a second, and those
// requests arrive concurrently. A check-then-insert dedup lets every one of
// them through; the (date, weight) unique index must collapse them to one row.
func TestInsertW_ConcurrentBurstStoresOneRow(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	d, _ := decimal.NewFromString("255.3")

	const burst = 8
	type result struct {
		res models.WeightInsert
		err error
	}
	results := make(chan result, burst)
	start := make(chan struct{})
	for i := 0; i < burst; i++ {
		go func() {
			<-start
			res, err := s.InsertW(ctx, models.BodyWeight{Date: "2026-10-14", Weight: d})
			results <- result{res, err}
		}()
	}
	close(start)
	inserted := 0
	for i := 0; i < burst; i++ {
		r := <-results
		if r.err != nil {
			t.Fatalf("InsertW: %v", r.err)
		}
		if r.res.Inserted {
			inserted++
		}
	}
	if inserted != 1 {
		t.Errorf("InsertW reported Inserted for %d of %d burst posts, want exactly 1", inserted, burst)
	}
	var count int
	if err := s.Pool().QueryRow(ctx,
		"SELECT receive_count FROM weight WHERE date = '2026-10-14'").Scan(&count); err != nil {
		t.Fatalf("receive_count: %v", err)
	}
	if count != burst {
		t.Errorf("receive_count after a %d-post burst = %d, want %d", burst, count, burst)
	}

	var n int
	if err := s.Pool().QueryRow(ctx,
		"SELECT count(*) FROM weight WHERE date = '2026-10-14'").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("rows after a concurrent burst = %d, want 1", n)
	}
}

// The regression that actually shipped duplicates: the scale posts an
// unrounded float (kg x 2.20462) that the NUMERIC(10,2) column rounds, so a
// dedup comparing the raw value never matched. Sequential re-sends of the same
// reading -- including ones whose float noise differs below the cent -- must
// still land as one row.
func TestInsertW_UnroundedScaleResendsStoreOneRow(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, raw := range []string{"255.8000071", "255.8000071", "255.79999542"} {
		d, _ := decimal.NewFromString(raw)
		if _, err := s.InsertW(ctx, models.BodyWeight{Date: "2026-11-04", Weight: d}); err != nil {
			t.Fatalf("InsertW %s: %v", raw, err)
		}
	}
	var n int
	if err := s.Pool().QueryRow(ctx,
		"SELECT count(*) FROM weight WHERE date = '2026-11-04'").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("rows after three re-sends of one reading = %d, want 1", n)
	}
}

// Each stored reading records the request that produced it, so a weigh-in can
// be traced to a device. A re-send merges into the first post's row: it bumps
// the count and last_received_at but keeps the first post's provenance.
func TestInsertW_RecordsProvenanceAndCountsResends(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	d, _ := decimal.NewFromString("256.8000043")
	first := models.BodyWeight{
		Date: "2026-11-24", RecordedAt: "2026-11-24T14:45:31-05:00", Weight: d,
		Source: "api", UserAgent: "scale-gateway-test",
		ClientIP: "192.0.2.59", RawWeight: d.String(),
	}
	res, err := s.InsertW(ctx, first)
	if err != nil {
		t.Fatalf("InsertW first: %v", err)
	}
	if !res.Inserted || res.ReceiveCount != 1 || res.ID == 0 {
		t.Fatalf("first post = %+v, want Inserted, count 1, non-zero id", res)
	}
	resend := first
	resend.UserAgent = "some-other-sender"
	res2, err := s.InsertW(ctx, resend)
	if err != nil {
		t.Fatalf("InsertW resend: %v", err)
	}
	if res2.Inserted || res2.ID != res.ID || res2.ReceiveCount != 2 {
		t.Fatalf("re-send = %+v, want merged into id %d with count 2", res2, res.ID)
	}

	ws, err := s.SelectW(ctx)
	if err != nil {
		t.Fatalf("SelectW: %v", err)
	}
	var got models.BodyWeight
	for _, w := range ws {
		if w.Date == "2026-11-24" {
			got = w
		}
	}
	if got.Source != "api" || got.UserAgent != "scale-gateway-test" ||
		got.ClientIP != "192.0.2.59" || got.RawWeight != "256.8000043" {
		t.Errorf("provenance = %+v, want the first post's source/ua/ip/raw", got)
	}
	if got.ReceivedAt == "" || got.LastReceivedAt == "" || got.ReceiveCount != 2 {
		t.Errorf("receive fields = received %q last %q count %d, want both set and count 2",
			got.ReceivedAt, got.LastReceivedAt, got.ReceiveCount)
	}
}

// Rows from before migration v27 have no provenance. They must read back as
// unknown (empty / zero), not be stamped with the migration's time or a count.
func TestSelectW_PreV27RowsReadAsUnknownProvenance(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, err := s.Pool().Exec(ctx, `INSERT INTO weight (date, recorded_at, weight, received_at, receive_count)
		VALUES ('2026-10-26', '2026-10-26T04:13:42-04:00', 256.8, NULL, NULL)`); err != nil {
		t.Fatalf("seed legacy row: %v", err)
	}
	ws, err := s.SelectW(ctx)
	if err != nil {
		t.Fatalf("SelectW: %v", err)
	}
	for _, w := range ws {
		if w.Date != "2026-10-26" {
			continue
		}
		if w.Source != "" || w.UserAgent != "" || w.ClientIP != "" || w.RawWeight != "" ||
			w.ReceivedAt != "" || w.LastReceivedAt != "" || w.ReceiveCount != 0 {
			t.Errorf("legacy row provenance = %+v, want all empty/zero", w)
		}
		return
	}
	t.Fatal("legacy row not returned by SelectW")
}
