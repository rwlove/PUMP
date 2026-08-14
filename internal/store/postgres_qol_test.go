package store

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
)

// InsertSet assigns each new set the next position within its date, so the
// per-day order is dense and append-at-bottom, and a new date restarts at 0.
func TestInsertSet_AssignsSequentialPosition(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	mk := func(date, name string) models.Set {
		return models.Set{Date: date, Name: name, Weight: decimal.NewFromInt(100), Reps: 5}
	}
	for _, n := range []string{"A", "B", "C"} {
		if _, err := s.InsertSet(ctx, mk("2026-01-01", n)); err != nil {
			t.Fatalf("InsertSet %s: %v", n, err)
		}
	}
	// A fresh date restarts positions at 0.
	if _, err := s.InsertSet(ctx, mk("2026-01-02", "D")); err != nil {
		t.Fatalf("InsertSet D: %v", err)
	}

	sets, err := s.SelectSetsSince(ctx, "2026-01-01")
	if err != nil {
		t.Fatalf("SelectSetsSince: %v", err)
	}
	got := map[string]int{}
	for _, st := range sets {
		got[st.Date+"/"+st.Name] = st.Position
	}
	want := map[string]int{
		"2026-01-01/A": 0, "2026-01-01/B": 1, "2026-01-01/C": 2,
		"2026-01-02/D": 0,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("position[%s] = %d, want %d", k, got[k], v)
		}
	}
}

// ReorderSets rewrites positions to the given id order and never touches a row
// on another date, even if a stale client lists its id.
func TestReorderSets(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	var ids []int
	for _, n := range []string{"A", "B", "C"} {
		st, err := s.InsertSet(ctx, models.Set{Date: "2026-02-01", Name: n, Reps: 1})
		if err != nil {
			t.Fatalf("InsertSet %s: %v", n, err)
		}
		ids = append(ids, st.ID)
	}
	// A set on a different date whose id must stay put.
	other, _ := s.InsertSet(ctx, models.Set{Date: "2026-02-02", Name: "Z", Reps: 1})

	// New order: C, A, B — plus the foreign-date id, which must be ignored.
	if err := s.ReorderSets(ctx, "2026-02-01", []int{ids[2], ids[0], ids[1], other.ID}); err != nil {
		t.Fatalf("ReorderSets: %v", err)
	}

	sets, _ := s.SelectSetsSince(ctx, "2026-02-01")
	pos := map[string]int{}
	for _, st := range sets {
		if st.Date == "2026-02-01" {
			pos[st.Name] = st.Position
		}
	}
	if pos["C"] != 0 || pos["A"] != 1 || pos["B"] != 2 {
		t.Fatalf("reordered positions wrong: %+v", pos)
	}
	// The foreign-date row keeps position 0 (untouched by the reorder).
	got, err := s.GetSet(ctx, other.ID)
	if err != nil {
		t.Fatalf("GetSet: %v", err)
	}
	if got.Position != 0 {
		t.Errorf("foreign-date set moved to position %d; reorder must not cross dates", got.Position)
	}
}

// LastPerformed reports each exercise's most recent date and its position in
// that session — the recency signal that orders the picker.
func TestLastPerformed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Squat done both days; RDL only on the earlier day. On the latest day
	// (02-10) the order was Bench(0), Squat(1).
	for _, st := range []models.Set{
		{Date: "2026-02-05", Name: "Squat", Reps: 5},
		{Date: "2026-02-05", Name: "RDL", Reps: 5},
		{Date: "2026-02-10", Name: "Bench", Reps: 5},
		{Date: "2026-02-10", Name: "Squat", Reps: 5},
	} {
		if _, err := s.InsertSet(ctx, st); err != nil {
			t.Fatalf("InsertSet: %v", err)
		}
	}

	rec, err := s.LastPerformed(ctx)
	if err != nil {
		t.Fatalf("LastPerformed: %v", err)
	}
	if rec["Squat"].LastDate != "2026-02-10" {
		t.Errorf("Squat last date = %q, want 2026-02-10", rec["Squat"].LastDate)
	}
	if rec["Bench"].LastDate != "2026-02-10" || rec["Bench"].Pos != 0 {
		t.Errorf("Bench = %+v, want {2026-02-10, 0}", rec["Bench"])
	}
	if rec["Squat"].Pos != 1 {
		t.Errorf("Squat pos in last session = %d, want 1", rec["Squat"].Pos)
	}
	if rec["RDL"].LastDate != "2026-02-05" {
		t.Errorf("RDL last date = %q, want 2026-02-05 (not done on the latest day)", rec["RDL"].LastDate)
	}
}

// A bodyweight set round-trips its AddedWeight; an ordinary set stores NULL
// (nil), which is how the UI tells the two apart.
func TestAddedWeight_RoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	added := decimal.NewFromInt(25)
	bw, err := s.InsertSet(ctx, models.Set{
		Date: "2026-03-01", Name: "Lunges", Weight: decimal.NewFromInt(270),
		Reps: 12, AddedWeight: &added,
	})
	if err != nil {
		t.Fatalf("InsertSet bodyweight: %v", err)
	}
	if bw.AddedWeight == nil || !bw.AddedWeight.Equal(added) {
		t.Fatalf("AddedWeight round-trip = %v, want 25", bw.AddedWeight)
	}

	normal, err := s.InsertSet(ctx, models.Set{
		Date: "2026-03-01", Name: "Bench", Weight: decimal.NewFromInt(185), Reps: 8,
	})
	if err != nil {
		t.Fatalf("InsertSet normal: %v", err)
	}
	if normal.AddedWeight != nil {
		t.Errorf("ordinary set AddedWeight = %v, want nil", normal.AddedWeight)
	}
}

// The muscle catalog inserts, lists, dedupes, and deletes.
func TestMuscles_CRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	// testStore doesn't truncate muscles (the migration seeds defaults), so
	// work in an isolated group and clean it up.
	if _, err := s.Pool().Exec(ctx, "DELETE FROM muscles WHERE gr = 'TestGrp'"); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	t.Cleanup(func() { _, _ = s.Pool().Exec(ctx, "DELETE FROM muscles WHERE gr = 'TestGrp'") })

	if err := s.InsertMuscle(ctx, models.Muscle{Group: "TestGrp", Name: "M1"}); err != nil {
		t.Fatalf("InsertMuscle: %v", err)
	}
	// Duplicate (group, name) is a silent no-op via the unique constraint.
	if err := s.InsertMuscle(ctx, models.Muscle{Group: "TestGrp", Name: "M1"}); err != nil {
		t.Fatalf("InsertMuscle dup: %v", err)
	}

	list, err := s.SelectMuscles(ctx)
	if err != nil {
		t.Fatalf("SelectMuscles: %v", err)
	}
	var id, count int
	for _, m := range list {
		if m.Group == "TestGrp" {
			count++
			id = m.ID
		}
	}
	if count != 1 {
		t.Fatalf("TestGrp muscle count = %d, want 1 (dup should be a no-op)", count)
	}

	if err := s.DeleteMuscle(ctx, id); err != nil {
		t.Fatalf("DeleteMuscle: %v", err)
	}
	list, _ = s.SelectMuscles(ctx)
	for _, m := range list {
		if m.Group == "TestGrp" {
			t.Fatalf("muscle %d not deleted", m.ID)
		}
	}
}
