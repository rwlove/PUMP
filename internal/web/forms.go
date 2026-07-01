package web

import (
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

// formDecimal parses a form value as a decimal. Empty means zero (the
// forms allow blank weight); a non-empty unparsable value reports ok=false
// instead of silently becoming zero.
func formDecimal(v string) (decimal.Decimal, bool) {
	if strings.TrimSpace(v) == "" {
		return decimal.Zero, true
	}
	d, err := decimal.NewFromString(v)
	return d, err == nil
}

// formInt parses a form value as an int with the same empty-is-zero,
// garbage-is-rejected semantics as formDecimal.
func formInt(v string) (int, bool) {
	if strings.TrimSpace(v) == "" {
		return 0, true
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}
