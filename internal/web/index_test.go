package web

import (
	"testing"

	"github.com/rwlove/PUMP/internal/models"
)

func TestBuildGroupList_PreservesFirstSeenOrder(t *testing.T) {
	exs := []models.Exercise{
		{Group: "Chest"},
		{Group: "Legs"},
		{Group: "Chest"}, // duplicate, drop
		{Group: "Back"},
		{Group: "Legs"}, // duplicate, drop
	}
	got := buildGroupList(exs)
	want := []string{"Chest", "Legs", "Back"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, g := range want {
		if got[i] != g {
			t.Fatalf("index %d: got %q, want %q", i, got[i], g)
		}
	}
}

func TestBuildGroupList_Empty(t *testing.T) {
	if got := buildGroupList(nil); len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestBuildGroupList_EmptyGroupIsGroup(t *testing.T) {
	// The picker relies on "" being a valid group name; buildGroupList must
	// not silently drop it.
	got := buildGroupList([]models.Exercise{{Group: ""}, {Group: "Cardio"}})
	if len(got) != 2 || got[0] != "" || got[1] != "Cardio" {
		t.Fatalf("got %v", got)
	}
}

func TestSortExsByRecency_MostRecentFirst(t *testing.T) {
	// Squat done most recently → top; never-done Deadlift → bottom.
	exs := []models.Exercise{
		{Name: "Bench"},
		{Name: "Squat"},
		{Name: "Deadlift"},
	}
	recency := map[string]models.ExerciseRecency{
		"Bench": {LastDate: "2026-08-10", Pos: 0},
		"Squat": {LastDate: "2026-08-13", Pos: 0},
		// Deadlift: never performed.
	}
	sortExsByRecency(exs, recency)

	if exs[0].Name != "Squat" {
		t.Fatalf("most-recent Squat should come first, got %q", exs[0].Name)
	}
	if exs[1].Name != "Bench" {
		t.Fatalf("2nd should be Bench, got %q", exs[1].Name)
	}
	if exs[2].Name != "Deadlift" {
		t.Fatalf("never-performed Deadlift should sink to last, got %q", exs[2].Name)
	}
}

func TestSortExsByRecency_LastSessionOrderPreserved(t *testing.T) {
	// All three performed in the same last session; the order they were done
	// that day (Pos) must be preserved: Squat, RDL, Leg Press.
	exs := []models.Exercise{
		{Name: "Leg Press"},
		{Name: "Squat"},
		{Name: "RDL"},
	}
	recency := map[string]models.ExerciseRecency{
		"Squat":     {LastDate: "2026-08-14", Pos: 0},
		"RDL":       {LastDate: "2026-08-14", Pos: 1},
		"Leg Press": {LastDate: "2026-08-14", Pos: 2},
	}
	sortExsByRecency(exs, recency)
	if exs[0].Name != "Squat" || exs[1].Name != "RDL" || exs[2].Name != "Leg Press" {
		t.Fatalf("last-session order not preserved, got %q %q %q",
			exs[0].Name, exs[1].Name, exs[2].Name)
	}
}

func TestSortExsByRecency_NeverPerformedAlphabetical(t *testing.T) {
	// No history at all → stable, alphabetical.
	exs := []models.Exercise{{Name: "Z"}, {Name: "A"}, {Name: "M"}}
	sortExsByRecency(exs, nil)
	if exs[0].Name != "A" || exs[1].Name != "M" || exs[2].Name != "Z" {
		t.Fatalf("expected alphabetical for no-history, got %q %q %q",
			exs[0].Name, exs[1].Name, exs[2].Name)
	}
}
