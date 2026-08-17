package conf

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestWeightOutOfRange(t *testing.T) {
	// Exercise against the default band (50–500 lb) read at package init.
	cases := []struct {
		name string
		lbs  string
		want bool
	}{
		{"far below (partial-load glitch)", "8", true},
		{"just below min", "49.9", true},
		{"at min boundary", "50", false},
		{"normal weigh-in", "272.2", false},
		{"at max boundary", "500", false},
		{"just above max", "500.1", true},
		{"absurd garbage", "9999", true},
		{"zero (blank manual entry)", "0", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, err := decimal.NewFromString(c.lbs)
			if err != nil {
				t.Fatalf("bad test input %q: %v", c.lbs, err)
			}
			if got := WeightOutOfRange(w); got != c.want {
				t.Fatalf("WeightOutOfRange(%s) = %v, want %v", c.lbs, got, c.want)
			}
		})
	}
}

func TestEnvDecimal(t *testing.T) {
	def := decimal.NewFromInt(50)

	// Unset -> default.
	t.Setenv("PUMP_TEST_DECIMAL", "")
	if got := envDecimal("PUMP_TEST_DECIMAL", def); !got.Equal(def) {
		t.Fatalf("unset: got %s, want %s", got, def)
	}

	// Valid -> parsed.
	t.Setenv("PUMP_TEST_DECIMAL", "123.5")
	if got := envDecimal("PUMP_TEST_DECIMAL", def); !got.Equal(decimal.RequireFromString("123.5")) {
		t.Fatalf("valid: got %s, want 123.5", got)
	}

	// Unparseable -> default.
	t.Setenv("PUMP_TEST_DECIMAL", "not-a-number")
	if got := envDecimal("PUMP_TEST_DECIMAL", def); !got.Equal(def) {
		t.Fatalf("invalid: got %s, want default %s", got, def)
	}
}
