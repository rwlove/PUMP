package web

import (
	"testing"

	"github.com/rwlove/PUMP/internal/models"
)

// Standing rule: every exercise is reachable in the workout picker under a
// selectable group chip. No exercise may be left without a chip, including
// one saved with a blank group or one whose group is missing from the
// managed groups table.
func TestEveryExerciseHasAPickerGroup(t *testing.T) {
	exs := []models.Exercise{
		{Name: "Dumbbell Hammer Curls", Group: "Arms"},
		{Name: "Cable Reverse Flyes", Group: "Deltoids"},
		{Name: "Farmer Carry", Group: "Grip"}, // not a managed group
		{Name: "Mystery Move", Group: ""},
		{Name: "Spaced Out", Group: "   "},
	}
	managed := []models.Group{{Name: "Arms"}, {Name: "Deltoids"}, {Name: "Legs"}}

	fillBlankGroups(exs)
	chips := map[string]bool{}
	for _, g := range orderedGroups(managed, exs, nil) {
		if g == "" {
			t.Error("picker has an empty group chip")
		}
		chips[g] = true
	}
	for _, ex := range exs {
		if !chips[ex.Group] {
			t.Errorf("%q (group %q) has no picker chip", ex.Name, ex.Group)
		}
	}
	if exs[3].Group != UngroupedLabel || exs[4].Group != UngroupedLabel {
		t.Errorf("blank groups not filled: %q, %q", exs[3].Group, exs[4].Group)
	}
}
