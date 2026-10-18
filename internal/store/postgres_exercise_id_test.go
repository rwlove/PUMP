package store

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
)

// InsertSet resolves exercise_id (schema v19) from the free-text name using the
// same normalized (trim + lower) match the migration backfill uses, so new sets
// carry the FK automatically. A name that matches no exercise leaves it nil
// rather than failing the insert.
func TestInsertSet_ResolvesExerciseID(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	pulldownID := mustInsertEx(t, s, "Lat Pulldown", "Back", "#111")
	benchID := mustInsertEx(t, s, "Bench Press", "Chest", "#222")

	cases := []struct {
		name    string // the set's typed name
		wantID  *int   // expected resolved exercise_id (nil = no match)
		comment string
	}{
		{"Lat Pulldown", &pulldownID, "exact match"},
		{"  bench press  ", &benchID, "normalized match: case + surrounding whitespace"},
		{"Nonexistent Move", nil, "no catalog match leaves exercise_id NULL"},
	}

	for _, tc := range cases {
		stored, err := s.InsertSet(ctx, models.Set{
			Date: "2026-01-05", Name: tc.name,
			Weight: decimal.NewFromInt(100), Reps: 8,
		})
		if err != nil {
			t.Fatalf("InsertSet(%q): %v", tc.name, err)
		}
		switch {
		case tc.wantID == nil && stored.ExerciseID != nil:
			t.Errorf("%s: name %q resolved to exercise_id %d, want NULL", tc.comment, tc.name, *stored.ExerciseID)
		case tc.wantID != nil && stored.ExerciseID == nil:
			t.Errorf("%s: name %q resolved to NULL, want %d", tc.comment, tc.name, *tc.wantID)
		case tc.wantID != nil && *stored.ExerciseID != *tc.wantID:
			t.Errorf("%s: name %q resolved to %d, want %d", tc.comment, tc.name, *stored.ExerciseID, *tc.wantID)
		}
	}

	// The FK round-trips through the read path (setColumns / scanSet).
	sets, err := s.SelectSetsOn(ctx, "2026-01-05")
	if err != nil {
		t.Fatalf("SelectSetsOn: %v", err)
	}
	for _, st := range sets {
		if st.Name == "Lat Pulldown" {
			if st.ExerciseID == nil || *st.ExerciseID != pulldownID {
				t.Errorf("read back Lat Pulldown exercise_id = %v, want %d", st.ExerciseID, pulldownID)
			}
		}
	}
}

// The FK is deterministic when two exercises share a normalized name: the lowest
// id wins, matching the migration backfill's DISTINCT ON (...) ORDER BY id.
func TestInsertSet_ExerciseIDDuplicateNamePrefersLowestID(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	firstID := mustInsertEx(t, s, "Row", "Back", "#111")
	_ = mustInsertEx(t, s, "row", "Back", "#222") // normalized duplicate, higher id

	stored, err := s.InsertSet(ctx, models.Set{
		Date: "2026-01-06", Name: "ROW",
		Weight: decimal.NewFromInt(135), Reps: 6,
	})
	if err != nil {
		t.Fatalf("InsertSet: %v", err)
	}
	if stored.ExerciseID == nil || *stored.ExerciseID != firstID {
		t.Errorf("duplicate normalized name resolved to %v, want lowest id %d", stored.ExerciseID, firstID)
	}
}
