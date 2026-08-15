package web

import (
	"fmt"
	"math"
	"regexp"
	"testing"

	"github.com/rwlove/PUMP/internal/models"
)

var hexColorRE = regexp.MustCompile(`^#[0-9a-f]{6}$`)

func TestHslToHex_KnownAnchors(t *testing.T) {
	// Anchor a few HSL points that must land near well-known CSS colors.
	// hslToHex uses standard HSL→RGB math; these values are the reference.
	cases := []struct {
		name    string
		h, s, l float64
		want    string
	}{
		{"pure red", 0, 100, 50, "#ff0000"},
		{"pure green", 120, 100, 50, "#00ff00"},
		{"pure blue", 240, 100, 50, "#0000ff"},
		{"black at l=0", 0, 100, 0, "#000000"},
		{"white at l=100", 0, 100, 100, "#ffffff"},
		{"grey at s=0", 0, 0, 50, "#808080"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hslToHex(tc.h, tc.s, tc.l)
			if got != tc.want {
				t.Fatalf("hslToHex(%v,%v,%v) = %q, want %q", tc.h, tc.s, tc.l, got, tc.want)
			}
		})
	}
}

func TestHslToHex_AlwaysWellFormed(t *testing.T) {
	// Any HSL in [0,360)×[0,100]×[0,100] must round-trip to a valid hex.
	for h := 0.0; h < 360; h += 17 {
		for s := 0.0; s <= 100; s += 25 {
			for l := 0.0; l <= 100; l += 20 {
				got := hslToHex(h, s, l)
				if !hexColorRE.MatchString(got) {
					t.Fatalf("hslToHex(%v,%v,%v) = %q, not a valid CSS hex", h, s, l, got)
				}
			}
		}
	}
}

// fillGroup returns a group filled to n exercises using the same lowest-free
// rule the handler uses, which is how a group actually accumulates colors.
func fillGroup(group string, n int) []models.Exercise {
	exs := make([]models.Exercise, 0, n)
	for i := 0; i < n; i++ {
		exs = append(exs, models.Exercise{
			Group: group,
			Color: nextExerciseColor(group, exs),
		})
	}
	return exs
}

// Adding an exercise must never recolor the ones already there. This is the
// property the previous global golden-angle scheme lacked, and the reason
// colors drifted as the catalogue grew.
func TestNextExerciseColor_ExistingColorsAreStable(t *testing.T) {
	before := fillGroup("Legs", 9)
	after := fillGroup("Legs", 10)
	for i := range before {
		if before[i].Color != after[i].Color {
			t.Fatalf("adding an exercise recolored index %d: %q became %q",
				i, before[i].Color, after[i].Color)
		}
	}
	if after[9].Color == "" || !hexColorRE.MatchString(after[9].Color) {
		t.Fatalf("new exercise got invalid color %q", after[9].Color)
	}
}

// A full group must hand out groupSlots distinct shades.
func TestNextExerciseColor_DistinctWithinGroup(t *testing.T) {
	for _, group := range []string{"Chest", "Deltoids", "Legs", "Cardio", "Back", "Arms"} {
		seen := map[string]int{}
		for i, ex := range fillGroup(group, groupSlots) {
			if prev, dup := seen[ex.Color]; dup {
				t.Errorf("%s: collision at slot %d: %q first seen at slot %d",
					group, i, ex.Color, prev)
			}
			seen[ex.Color] = i
			if !hexColorRE.MatchString(ex.Color) {
				t.Errorf("%s slot %d: %q is not a valid hex color", group, i, ex.Color)
			}
		}
	}
}

// Freeing a slot must make it available again rather than pushing the next
// exercise to the end of the band.
func TestNextExerciseColor_ReusesAFreedSlot(t *testing.T) {
	exs := fillGroup("Back", 4)
	freed := exs[1].Color
	remaining := []models.Exercise{exs[0], exs[2], exs[3]}
	if got := nextExerciseColor("Back", remaining); got != freed {
		t.Fatalf("expected the freed slot %q to be reused, got %q", freed, got)
	}
}

// The whole point of the scheme: any two exercises in a group must be closer
// to each other than either is to an exercise in another group. Without this
// the bands stop reading as groups, which is what the rebalance fixed.
func TestGroupColors_CohereAndSeparate(t *testing.T) {
	// Sizes matching the real catalogue at the time of the rebalance.
	sizes := map[string]int{
		"Chest": 6, "Deltoids": 7, "Legs": 9, "Cardio": 1, "Back": 4, "Arms": 5,
	}
	type swatch struct{ group, color string }
	var all []swatch
	worstWithin := math.Inf(1)
	for group, n := range sizes {
		exs := fillGroup(group, n)
		for i, a := range exs {
			all = append(all, swatch{group, a.Color})
			for _, b := range exs[i+1:] {
				if d := deltaE(a.Color, b.Color); d < worstWithin {
					worstWithin = d
				}
			}
		}
	}
	bestCross := math.Inf(1)
	for i, a := range all {
		for _, b := range all[i+1:] {
			if a.group == b.group {
				continue
			}
			if d := deltaE(a.color, b.color); d < bestCross {
				bestCross = d
			}
		}
	}
	// Below ~5 two colors are hard to tell apart side by side.
	if worstWithin < 7 {
		t.Errorf("within-group ΔE %.1f is too low; exercises are hard to tell apart", worstWithin)
	}
	// Groups only read as groups while members are closer to each other than
	// to outsiders.
	if bestCross <= worstWithin {
		t.Errorf("cross-group ΔE %.1f <= within-group ΔE %.1f; the bands have merged",
			bestCross, worstWithin)
	}
	// Nothing may vanish into the dark theme background.
	for _, s := range all {
		if d := deltaE(s.color, "#212529"); d < 25 {
			t.Errorf("%s %s is only ΔE %.1f from the page background", s.group, s.color, d)
		}
	}
}

// Two groups must not be handed the same shade.
func TestGroupColors_DistinctAcrossGroups(t *testing.T) {
	seen := map[string]string{}
	for _, group := range []string{"Chest", "Deltoids", "Legs", "Cardio", "Back", "Arms"} {
		for _, ex := range fillGroup(group, groupSlots) {
			if prev, dup := seen[ex.Color]; dup {
				t.Errorf("%q used by both %s and %s", ex.Color, prev, group)
			}
			seen[ex.Color] = group
		}
	}
}

// A group with no entry in groupHues still gets a stable band, so adding one
// does not require a code change to avoid a crash or a black swatch.
func TestGroupHue_UnknownGroupIsStableAndInRange(t *testing.T) {
	a := nextExerciseColor("Forearms", nil)
	b := nextExerciseColor("Forearms", nil)
	if a != b {
		t.Fatalf("unknown group is not deterministic: %q vs %q", a, b)
	}
	if !hexColorRE.MatchString(a) {
		t.Fatalf("unknown group produced invalid color %q", a)
	}
	for _, g := range []string{"", "Forearms", "Neck", "ǅ"} {
		if h := groupHue(g); h < 0 || h >= 360 {
			t.Errorf("groupHue(%q) = %v, outside [0,360)", g, h)
		}
	}
}

// deltaE is CIE76 distance between two hex colors, used to assert the
// separation properties above rather than eyeballing swatches.
func deltaE(hexA, hexB string) float64 {
	l1, a1, b1 := labOf(hexA)
	l2, a2, b2 := labOf(hexB)
	return math.Sqrt((l1-l2)*(l1-l2) + (a1-a2)*(a1-a2) + (b1-b2)*(b1-b2))
}

func labOf(hex string) (float64, float64, float64) {
	var r8, g8, b8 int
	fmt.Sscanf(hex, "#%02x%02x%02x", &r8, &g8, &b8)
	lin := func(v int) float64 {
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	r, g, b := lin(r8), lin(g8), lin(b8)
	x := (0.4124*r + 0.3576*g + 0.1805*b) / 0.95047
	y := 0.2126*r + 0.7152*g + 0.0722*b
	z := (0.0193*r + 0.1192*g + 0.9505*b) / 1.08883
	f := func(t float64) float64 {
		if t > 0.008856 {
			return math.Cbrt(t)
		}
		return 7.787*t + 16.0/116.0
	}
	fx, fy, fz := f(x), f(y), f(z)
	return 116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)
}

func TestNeedsColorBackfill(t *testing.T) {
	if !needsColorBackfill([]models.Exercise{{Color: "#aaa"}, {Color: ""}}) {
		t.Fatal("any empty color should trigger backfill")
	}
	if needsColorBackfill([]models.Exercise{{Color: "#a"}, {Color: "#b"}}) {
		t.Fatal("all-non-empty should not trigger backfill")
	}
	if needsColorBackfill(nil) {
		t.Fatal("empty slice should not trigger backfill")
	}
}

// A missing color and a broken black both count as unset (needing backfill);
// real palette shades do not. The palette never produces #000000.
func TestColorUnset(t *testing.T) {
	for _, c := range []string{"", "#000000", "#000", "#000000"} {
		if !colorUnset(c) {
			t.Errorf("colorUnset(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"#96db8a", "#14a30a", "#ffffff", "#2780e3"} {
		if colorUnset(c) {
			t.Errorf("colorUnset(%q) = true, want false", c)
		}
	}
}
