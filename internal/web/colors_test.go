package web

import (
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

func TestNextExerciseColor_GoldenAngleDeterministic(t *testing.T) {
	// The color for N assigned exercises is derived only from N — same input
	// count must give the same color, regardless of what colors are in the
	// slice. That property is what makes the "backfill and restart" flow
	// stable across runs.
	// n=0 vs n=1 must differ (golden-angle rotation).
	got0 := nextExerciseColor([]string{})
	got1 := nextExerciseColor([]string{"#anything"})
	if got1 == got0 {
		t.Fatalf("distinct hues expected: n=0 %q == n=1 %q", got0, got1)
	}
	// Only length matters — actual values in the slice are ignored.
	got1prime := nextExerciseColor([]string{"#totally-different"})
	if got1prime != got1 {
		t.Fatalf("only slice length should affect output: %q vs %q", got1prime, got1)
	}
	if !hexColorRE.MatchString(got0) {
		t.Fatalf("not a valid hex color: %q", got0)
	}
}

func TestNextExerciseColor_DistinctForSmallN(t *testing.T) {
	// First ten sequential assignments must all be distinct. Weaker than the
	// golden-angle "maximally distinct" promise but strong enough to catch a
	// regression that made every color the same.
	seen := map[string]int{}
	for i := 0; i < 10; i++ {
		colors := make([]string, i)
		c := nextExerciseColor(colors)
		if prev, dup := seen[c]; dup {
			t.Fatalf("collision at n=%d: %q first seen at n=%d", i, c, prev)
		}
		seen[c] = i
	}
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

func TestCollectColors(t *testing.T) {
	exs := []models.Exercise{
		{Color: "#111"},
		{Color: ""},
		{Color: "#222"},
		{Color: ""},
		{Color: "#333"},
	}
	got := collectColors(exs)
	want := []string{"#111", "#222", "#333"}
	if len(got) != len(want) {
		t.Fatalf("got %d colors, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
	// Ordering is meaningful (feeds nextExerciseColor's index), so we also
	// verify the empty case cleanly.
	if len(collectColors(nil)) != 0 {
		t.Fatal("collectColors(nil) should return empty, not nil-typed panic")
	}
}
