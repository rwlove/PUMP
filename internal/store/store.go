package store

import "github.com/rwlove/PUMP/internal/models"

// Store abstracts data access so both the monolith (SQLite) and the
// split frontend (HTTP API client) can satisfy the same interface.
type Store interface {
	SelectEx() ([]models.Exercise, error)
	InsertEx(ex models.Exercise) error
	DeleteEx(id int) error
	UpdateExColor(id int, color string) error

	SelectSet() ([]models.Set, error)
	// BulkReplaceSetsByDate atomically replaces all sets for a given date.
	// Used by the manual-entry form path. Per-set ops below are used by the
	// CV auto-log path and any UI that edits one set at a time.
	BulkReplaceSetsByDate(date string, sets []models.Set) error

	GetSet(id int) (models.Set, error)
	// InsertSet appends a single set and returns its new ID. Empty Source is
	// stored as "manual"; zero Confidence is stored as 1.0; Pending defaults
	// to false. Other defaults come from the schema.
	InsertSet(set models.Set) (int, error)
	UpdateSet(id int, upd models.SetUpdate) error
	DeleteSet(id int) error

	SelectW() ([]models.BodyWeight, error)
	InsertW(w models.BodyWeight) error
	DeleteW(id int) error
}
