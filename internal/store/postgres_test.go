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
	// execution order.
	for _, tbl := range []string{"sets", "exercises", "weight"} {
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
	if err := s.InsertEx(ctx, models.Exercise{
		Name: name, Group: group, Color: color, Weight: decimal.NewFromInt(0),
	}); err != nil {
		t.Fatalf("InsertEx(%s): %v", name, err)
	}
	var id int
	if err := s.Pool().QueryRow(ctx,
		"SELECT id FROM exercises WHERE name = $1", name).Scan(&id); err != nil {
		t.Fatalf("lookup %s: %v", name, err)
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
		if err := s.InsertW(ctx, models.BodyWeight{Date: "2026-08-01", Weight: d}); err != nil {
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

// The duplicate-suppression behaviour DeleteW exists for must still hold:
// identical same-day rows all disappear together.
func TestDeleteW_RemovesIdenticalSameDayDuplicates(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Pre-dedup rows, inserted directly to simulate history.
	for i := 0; i < 3; i++ {
		if _, err := s.Pool().Exec(ctx,
			"INSERT INTO weight (date, recorded_at, weight) VALUES ('2026-08-01', NOW(), 272.4)"); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	var id int
	if err := s.Pool().QueryRow(ctx, "SELECT id FROM weight LIMIT 1").Scan(&id); err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if err := s.DeleteW(ctx, id); err != nil {
		t.Fatalf("DeleteW: %v", err)
	}
	var n int
	if err := s.Pool().QueryRow(ctx, "SELECT count(*) FROM weight").Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("%d duplicate rows survived; the deleted entry would reappear in the log", n)
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
