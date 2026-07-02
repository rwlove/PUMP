package web

import (
	"testing"
	"time"

	"github.com/rwlove/PUMP/internal/models"
)

func TestBuildGroupList_PreservesFirstSeenOrder(t *testing.T) {
	exs := []models.Exercise{
		{Group: "Chest"},
		{Group: "Legs"},
		{Group: "Chest"},   // duplicate, drop
		{Group: "Back"},
		{Group: "Legs"},    // duplicate, drop
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

func TestSortExsByFrequency(t *testing.T) {
	// Frequency window covers everything.
	today := time.Now().Format("2006-01-02")
	older := time.Now().AddDate(0, 0, -3).Format("2006-01-02")

	exs := []models.Exercise{
		{Name: "Bench", Place: "3"},
		{Name: "Squat", Place: "1"},
		{Name: "Deadlift", Place: "2"},
	}
	sets := []models.Set{
		{Name: "Bench", Date: today},
		{Name: "Bench", Date: today},
		{Name: "Bench", Date: older},
		{Name: "Squat", Date: today},
		// Deadlift: zero sets in window.
	}
	sortExsByFrequency(exs, sets, 30)

	if exs[0].Name != "Bench" {
		t.Fatalf("most-frequent should come first, got %q", exs[0].Name)
	}
	if exs[1].Name != "Squat" {
		t.Fatalf("2nd should be next-frequent Squat, got %q", exs[1].Name)
	}
	// Deadlift is untouched (0 sets), sorted by Place among ties.
	if exs[2].Name != "Deadlift" {
		t.Fatalf("least-frequent Deadlift last, got %q", exs[2].Name)
	}
}

func TestSortExsByFrequency_TiebreakByPlace(t *testing.T) {
	// All zero-set exercises tie on frequency; sort must fall through to Place.
	exs := []models.Exercise{
		{Name: "Z", Place: "3"},
		{Name: "A", Place: "1"},
		{Name: "M", Place: "2"},
	}
	sortExsByFrequency(exs, nil, 30)
	if exs[0].Place != "1" || exs[1].Place != "2" || exs[2].Place != "3" {
		t.Fatalf("expected Place ascending as tiebreak, got %v %v %v",
			exs[0].Place, exs[1].Place, exs[2].Place)
	}
}

func TestSortExsByFrequency_WindowRespected(t *testing.T) {
	// Set outside the frequency window must not count.
	today := time.Now().Format("2006-01-02")
	stale := time.Now().AddDate(0, 0, -60).Format("2006-01-02")
	exs := []models.Exercise{
		{Name: "Rare", Place: "1"},
		{Name: "Today", Place: "2"},
	}
	sets := []models.Set{
		{Name: "Rare", Date: stale},
		{Name: "Rare", Date: stale},
		{Name: "Rare", Date: stale},
		{Name: "Today", Date: today},
	}
	sortExsByFrequency(exs, sets, 30) // 30-day window excludes stale sets
	if exs[0].Name != "Today" {
		t.Fatalf("Today should win when Rare's sets are outside the window, got %q", exs[0].Name)
	}
}

func TestSortExsByFrequency_StableForZeroDays(t *testing.T) {
	// days=0 → cutoff is today; only today's sets count.
	today := time.Now().Format("2006-01-02")
	exs := []models.Exercise{{Name: "A", Place: "1"}, {Name: "B", Place: "2"}}
	sortExsByFrequency(exs, []models.Set{{Name: "B", Date: today}}, 0)
	if exs[0].Name != "B" {
		t.Fatalf("B (only today's set) should come first, got %q", exs[0].Name)
	}
}
