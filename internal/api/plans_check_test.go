package api

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsCheckViolation(t *testing.T) {
	v28 := &pgconn.PgError{Code: "23514", Message: "target_weight 17.50 for exercise 51 is not a whole number: Voltra exercises load in whole pounds"}
	if !isCheckViolation(fmt.Errorf("replace items: %w", v28)) {
		t.Error("wrapped check_violation not recognised")
	}
	if got := checkViolationMessage(fmt.Errorf("replace items: %w", v28)); got != v28.Message {
		t.Errorf("message = %q, want the database's explanation", got)
	}
	if isCheckViolation(&pgconn.PgError{Code: "23503"}) {
		t.Error("foreign-key violation misread as check_violation")
	}
	if isCheckViolation(errors.New("connection refused")) {
		t.Error("plain error misread as check_violation")
	}
}
