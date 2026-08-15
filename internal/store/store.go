package store

import (
	"context"
	"time"

	"github.com/rwlove/PUMP/internal/models"
)

// Store abstracts data access for handlers and tests.
type Store interface {
	SelectEx(ctx context.Context) ([]models.Exercise, error)
	// InsertEx appends an exercise and returns its new id (needed to write
	// the exercise_muscles junction for a freshly created exercise).
	InsertEx(ctx context.Context, ex models.Exercise) (int, error)
	// UpdateEx rewrites an existing exercise in place (id preserved);
	// false means no row had that id.
	UpdateEx(ctx context.Context, ex models.Exercise) (bool, error)
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
	// ReorderSets rewrites the per-day position of the given set ids to match
	// their order in orderedIDs (index 0 = top). Used by the drag-and-drop
	// reorder in per-set save mode.
	ReorderSets(ctx context.Context, date string, orderedIDs []int) error

	GetSet(ctx context.Context, id int) (models.Set, error)
	// InsertSet appends a single set and returns the stored row. Empty
	// Source is stored as "manual"; zero Confidence is stored as 1.0;
	// Pending defaults to false. Other defaults come from the schema.
	InsertSet(ctx context.Context, set models.Set) (models.Set, error)
	// UpdateSet applies the non-nil fields and returns the updated row.
	// A nonexistent id returns a zero-ID Set with a nil error.
	UpdateSet(ctx context.Context, id int, upd models.SetUpdate) (models.Set, error)
	// DeleteSet removes a set, returning its date ("" when the id did not
	// exist) so event payloads don't need a separate lookup.
	DeleteSet(ctx context.Context, id int) (string, error)

	SelectW(ctx context.Context) ([]models.BodyWeight, error)
	InsertW(ctx context.Context, w models.BodyWeight) error
	DeleteW(ctx context.Context, id int) error

	// GetAppConfig returns the persisted UI-editable settings. ok=false
	// when nothing has ever been saved (fresh install) so callers know to
	// fall back to env-var defaults instead of overwriting them.
	GetAppConfig(ctx context.Context) (cfg models.Conf, ok bool, err error)
	// SaveAppConfig upserts the UI-editable settings (single row).
	// Pushover credentials and NodePath are env-only and never persisted.
	SaveAppConfig(ctx context.Context, cfg models.Conf) error

	// LastPerformed returns each exercise name's most recent session date and
	// its position in that session, driving the picker's recency ordering.
	LastPerformed(ctx context.Context) (map[string]models.ExerciseRecency, error)

	// SelectMuscles returns the full DB-editable muscle catalog, ordered by
	// group then sort order.
	SelectMuscles(ctx context.Context) ([]models.Muscle, error)
	// InsertMuscle adds a muscle to a group (duplicate group+name is a no-op).
	InsertMuscle(ctx context.Context, m models.Muscle) error
	// DeleteMuscle removes a catalog muscle by id.
	DeleteMuscle(ctx context.Context, id int) error

	// SelectGroups returns the managed groups ordered by sort_order then name.
	SelectGroups(ctx context.Context) ([]models.Group, error)
	// InsertGroup adds a group at the end of the order (duplicate name is a
	// no-op).
	InsertGroup(ctx context.Context, name string) error
	// RenameGroup renames a group and cascades the new name onto every
	// exercise and catalog muscle that referenced it, atomically. Renaming
	// onto an existing group name is rejected (merge is not supported).
	RenameGroup(ctx context.Context, oldName, newName string) error
	// DeleteGroup removes a group. It refuses (returning inUse > 0) when any
	// exercise or catalog muscle still references it, so a group is never
	// deleted out from under its members.
	DeleteGroup(ctx context.Context, name string) (inUse int, err error)
	// ReorderGroups sets sort_order to match the given name order (index 0
	// first). Names not present are ignored.
	ReorderGroups(ctx context.Context, names []string) error

	// SelectExerciseMuscles returns the focus muscles for one exercise
	// (primary first), resolved from the exercise_muscles junction.
	SelectExerciseMuscles(ctx context.Context, exerciseID int) ([]models.FocusMuscle, error)
	// ReplaceExerciseMuscles atomically replaces an exercise's focus muscles
	// with the given set.
	ReplaceExerciseMuscles(ctx context.Context, exerciseID int, fms []models.FocusMuscle) error

	// SelectRoutines returns all workout templates with items resolved to
	// exercise name/color, ordered for display.
	SelectRoutines(ctx context.Context) ([]models.Routine, error)
	// InsertRoutine creates a routine and returns its id.
	InsertRoutine(ctx context.Context, name, notes string) (int, error)
	// UpdateRoutine updates a routine's name and notes.
	UpdateRoutine(ctx context.Context, id int, name, notes string) error
	// DeleteRoutine removes a routine and its items (cascade).
	DeleteRoutine(ctx context.Context, id int) error
	// ReplaceRoutineItems atomically replaces a routine's items; positions are
	// assigned from slice order.
	ReplaceRoutineItems(ctx context.Context, routineID int, items []models.RoutineItem) error
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
