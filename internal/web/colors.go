package web

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/rwlove/PUMP/internal/models"
)

// Exercise colors are organised by muscle group: each group owns a sector of
// the hue wheel, and its exercises are shades within a narrow band centred on
// that sector. Reading the workout list, the group is recognisable at a glance
// from hue alone, while individual exercises stay tellable apart.
//
// Hue cannot do both jobs. Nine Legs exercises inside a 28° band are only ~3°
// apart, which is invisible, so lightness carries the within-group separation
// and saturation moves opposite it — keeping pale shades vivid instead of
// letting them wash out to grey against the dark theme.
const (
	// groupHueBand is the width of a group's hue band. Groups are spaced 60°
	// apart, so 28° leaves a 32° gap between adjacent bands. Measured on the
	// current catalogue this yields a worst-case CIE76 ΔE of ~10.7 within a
	// group and ~18.5 between groups: members are distinct, but any two
	// members of one group are still closer to each other than to anything
	// outside it, which is what makes the grouping read.
	groupHueBand = 28.0
	// groupSlots is how many exercises one group's band can hold. Fixing this
	// is what keeps colors stable: a slot's color depends on its index alone,
	// never on how many exercises the group currently has, so adding a tenth
	// Legs exercise does not recolor the other nine. Past this the band wraps
	// and shades repeat — widen the band rather than let that happen.
	groupSlots = 12
)

// groupHues anchors each muscle group at the centre of its own 60° sector.
// Unknown groups fall back to a hue derived from the name, so a group added
// later still gets a consistent band without a code change.
var groupHues = map[string]float64{
	"Chest":    350, // red / crimson
	"Deltoids": 50,  // amber / gold
	"Legs":     110, // green
	"Cardio":   170, // teal
	"Back":     230, // blue
	"Arms":     290, // purple / magenta
}

// slotHue and slotLight map a slot index to a position in its band. They are
// deliberately not sequential: the pairing and the order slots are handed out
// were chosen to maximise the *worst* separation across every group size from
// two up to groupSlots, so a group looks right part-filled as well as full.
// Sequential slots would put the first few exercises adjacent in both hue and
// lightness, which is precisely when a group is small and most often seen.
var (
	slotHue   = [groupSlots]int{0, 2, 3, 6, 5, 10, 9, 7, 1, 8, 11, 4}
	slotLight = [groupSlots]int{1, 7, 11, 9, 6, 8, 2, 4, 3, 0, 5, 10}
)

// groupHue returns the band centre for a muscle group.
func groupHue(group string) float64 {
	if h, ok := groupHues[group]; ok {
		return h
	}
	// Deterministic fallback for a group not in the table: hash the name to a
	// hue so it is at least stable across restarts.
	var sum int
	for _, r := range group {
		sum = sum*31 + int(r)
	}
	return math.Mod(float64(sum), 360)
}

// groupSlotColor returns the color for slot i of a group's band.
func groupSlotColor(group string, i int) string {
	i = ((i % groupSlots) + groupSlots) % groupSlots
	hs := float64(slotHue[i]) / float64(groupSlots-1)
	u := float64(slotLight[i]) / float64(groupSlots-1)
	hue := groupHue(group) - groupHueBand/2 + hs*groupHueBand
	// Saturation runs opposite lightness: the lightest shades need the most
	// saturation to stay legible, the darkest need the least to avoid glowing.
	return hslToHex(math.Mod(hue+360, 360), 88-u*(88-45), 34+u*(78-34))
}

// nextExerciseColor returns a color for a new exercise in the given group: the
// first slot in that group's band not already taken by one of its exercises.
//
// Lowest-free rather than next-in-sequence so that deleting an exercise frees
// its shade for reuse, and so a group's colors never depend on how the caller
// happened to order the slice.
func nextExerciseColor(group string, groupExercises []models.Exercise) string {
	taken := make(map[string]bool, len(groupExercises))
	for _, ex := range groupExercises {
		if ex.Color != "" {
			taken[strings.ToLower(ex.Color)] = true
		}
	}
	for i := 0; i < groupSlots; i++ {
		c := groupSlotColor(group, i)
		if !taken[strings.ToLower(c)] {
			return c
		}
	}
	// Band is full; wrapping repeats a shade, which beats returning nothing.
	return groupSlotColor(group, len(groupExercises))
}

// exercisesInGroup returns the members of one muscle group.
func exercisesInGroup(exs []models.Exercise, group string) []models.Exercise {
	out := make([]models.Exercise, 0, len(exs))
	for _, ex := range exs {
		if ex.Group == group {
			out = append(out, ex)
		}
	}
	return out
}

// hslToHex converts HSL (h: 0-360, s: 0-100, l: 0-100) to a CSS hex color.
func hslToHex(h, s, l float64) string {
	s /= 100
	l /= 100

	c := (1 - math.Abs(2*l-1)) * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := l - c/2

	var r1, g1, b1 float64
	switch {
	case h < 60:
		r1, g1, b1 = c, x, 0
	case h < 120:
		r1, g1, b1 = x, c, 0
	case h < 180:
		r1, g1, b1 = 0, c, x
	case h < 240:
		r1, g1, b1 = 0, x, c
	case h < 300:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}

	r := int(math.Round((r1 + m) * 255))
	g := int(math.Round((g1 + m) * 255))
	b := int(math.Round((b1 + m) * 255))
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// colorUnset reports whether an exercise color needs (re)assignment. Empty is
// the obvious case; pure black (#000000) is treated the same because the palette
// never produces black — a shade is HSL with lightness 34–78 — so a stored
// #000000 is legacy/broken data, not a real assigned color, and should be
// backfilled rather than rendered as a black dot.
func colorUnset(c string) bool {
	return c == "" || strings.EqualFold(c, "#000000") || strings.EqualFold(c, "#000")
}

// backfillColors assigns colors to any exercise that has no real color yet
// (empty or a broken black), persisting the change to the store. Called lazily
// on the first page load that finds such exercises.
func backfillColors(ctx context.Context, exs []models.Exercise) {
	// Work against a local copy so each assignment is visible to the next one:
	// two uncolored exercises in the same group must not both be handed the
	// same lowest-free slot.
	assigned := make([]models.Exercise, len(exs))
	copy(assigned, exs)

	for i, ex := range assigned {
		if !colorUnset(ex.Color) {
			continue
		}
		// Clear the local color first so a broken #000000 doesn't count itself
		// as "taken" when picking the lowest-free slot.
		assigned[i].Color = ""
		color := nextExerciseColor(ex.Group, exercisesInGroup(assigned, ex.Group))
		assigned[i].Color = color
		if err := dataStore.UpdateExColor(ctx, ex.ID, color); err != nil {
			slog.Warn("backfillColors failed", slog.Int("id", ex.ID), slog.String("name", ex.Name), slog.Any("error", err))
		}
	}
}

// needsColorBackfill returns true if any exercise lacks a real color (empty or
// a broken black).
func needsColorBackfill(exs []models.Exercise) bool {
	for _, ex := range exs {
		if colorUnset(ex.Color) {
			return true
		}
	}
	return false
}
