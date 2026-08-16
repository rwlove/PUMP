package conf

import (
	"testing"

	"github.com/rwlove/PUMP/internal/models"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name               string
		inStep, inDays     int
		wantStep, wantDays int
	}{
		{"zero floors to defaults", 0, 0, 10, 30},
		{"negative floors to defaults", -5, -1, 10, 30},
		{"valid values pass through", 25, 90, 25, 90},
		{"one is the floor", 1, 1, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Normalize(models.Conf{PageStep: c.inStep, DisplayDays: c.inDays})
			if got.PageStep != c.wantStep || got.DisplayDays != c.wantDays {
				t.Fatalf("Normalize(step=%d,days=%d) = step=%d,days=%d; want step=%d,days=%d",
					c.inStep, c.inDays, got.PageStep, got.DisplayDays, c.wantStep, c.wantDays)
			}
		})
	}
}
