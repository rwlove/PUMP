package web

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/rwlove/PUMP/internal/models"
)

// nextExerciseColor returns the next maximally-distinct color for a new exercise.
// It uses the golden-angle method in HSL space: each successive hue is offset by
// ~137.5° from the previous, which distributes colors evenly around the wheel
// regardless of how many exercises already exist.
func nextExerciseColor(existingColors []string) string {
	hue := math.Mod(float64(len(existingColors))*137.508, 360)
	return hslToHex(hue, 65, 48)
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

// backfillColors assigns colors to any exercise that has an empty Color field,
// persisting the change to the store. Called lazily on the first page load that
// finds exercises without colors.
func backfillColors(ctx context.Context, exs []models.Exercise) {
	// Collect the ordered list of already-assigned colors (preserving insertion
	// order so the golden-angle sequence stays consistent across restarts).
	assigned := make([]string, 0, len(exs))
	for _, ex := range exs {
		if ex.Color != "" {
			assigned = append(assigned, ex.Color)
		}
	}

	for _, ex := range exs {
		if ex.Color != "" {
			continue
		}
		color := nextExerciseColor(assigned)
		assigned = append(assigned, color)
		if err := dataStore.UpdateExColor(ctx, ex.ID, color); err != nil {
			slog.Warn("backfillColors failed", slog.Int("id", ex.ID), slog.String("name", ex.Name), slog.Any("error", err))
		}
	}
}

// needsColorBackfill returns true if any exercise has an empty Color.
func needsColorBackfill(exs []models.Exercise) bool {
	for _, ex := range exs {
		if ex.Color == "" {
			return true
		}
	}
	return false
}

// collectColors returns the non-empty Color values from a slice of exercises.
func collectColors(exs []models.Exercise) []string {
	colors := make([]string, 0, len(exs))
	for _, ex := range exs {
		if ex.Color != "" {
			colors = append(colors, ex.Color)
		}
	}
	return colors
}
