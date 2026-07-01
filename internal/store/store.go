package store

import (
	"context"
	"time"

	"github.com/rwlove/PUMP/internal/models"
)

// Store abstracts data access for handlers and tests.
type Store interface {
	SelectEx(ctx context.Context) ([]models.Exercise, error)
	InsertEx(ctx context.Context, ex models.Exercise) error
	DeleteEx(ctx context.Context, id int) error
	UpdateExColor(ctx context.Context, id int, color string) error

	SelectSet(ctx context.Context) ([]models.Set, error)
	// SelectSetsSince returns sets dated on or after cutoff (YYYY-MM-DD),
	// in the same id order as SelectSet.
	SelectSetsSince(ctx context.Context, cutoff string) ([]models.Set, error)
	// BulkReplaceSetsByDate atomically replaces all sets for a given date.
	// Used by the manual-entry form path. Per-set ops below are used by the
	// CV auto-log path and any UI that edits one set at a time.
	BulkReplaceSetsByDate(ctx context.Context, date string, sets []models.Set) error

	GetSet(ctx context.Context, id int) (models.Set, error)
	// InsertSet appends a single set and returns its new ID. Empty Source is
	// stored as "manual"; zero Confidence is stored as 1.0; Pending defaults
	// to false. Other defaults come from the schema.
	InsertSet(ctx context.Context, set models.Set) (int, error)
	UpdateSet(ctx context.Context, id int, upd models.SetUpdate) error
	DeleteSet(ctx context.Context, id int) error

	SelectW(ctx context.Context) ([]models.BodyWeight, error)
	InsertW(ctx context.Context, w models.BodyWeight) error
	DeleteW(ctx context.Context, id int) error
}

// HealthStore persists wearable health records ingested from Android Health
// Connect (via the HC Webhook bridge → POST /api/health). The Postgres store
// implements it.
type HealthStore interface {
	// InsertHealthRecords appends records, deduping on
	// (MetricType, StartTime, EndTime) so the bridge's rolling 48h
	// re-delivery window is idempotent. Returns the count actually inserted.
	InsertHealthRecords(ctx context.Context, recs []models.HealthRecord) (inserted int, err error)
	// SelectHealthRecords returns records at or after since, optionally
	// filtered to a single metricType ("" = all), newest first.
	SelectHealthRecords(ctx context.Context, metricType string, since time.Time) ([]models.HealthRecord, error)
}
