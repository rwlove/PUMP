package models

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/shopspring/decimal"
)

// Conf - web gui config
type Conf struct {
	Color       string
	NodePath    string
	PageStep    int
	DisplayDays int  // days of history shown on main page (7/30/90/365)
	AutoFill    bool // pre-fill weight/reps from last performance of that exercise
	CVAutoLog   bool // when true, accept set writes from the pump-cv sidecar

	// VoltraAutoLog gates set writes from the pump-voltra sidecar. Env-only
	// (VOLTRA_AUTOLOG); unlike CVAutoLog it is not persisted in app_config,
	// so it has no Settings-page toggle yet.
	VoltraAutoLog bool

	// Pushover credentials are read from env on startup (PUSHOVER_USER_KEY,
	// PUSHOVER_APP_TOKEN) and never serialised through the JSON API. The web
	// UI shows only a "configured / not configured" indicator.
	PushoverUserKey  string `json:"-" yaml:"-"`
	PushoverAppToken string `json:"-" yaml:"-"`
}

// PushoverConfigured reports whether both Pushover env vars are set.
func (c Conf) PushoverConfigured() bool {
	return c.PushoverUserKey != "" && c.PushoverAppToken != ""
}

// AutoLog reports whether any sidecar may write sets on its own.
//
// The workout page's save mode keys on this. With it false the page saves a
// day by POSTing the whole form to /set/, which bulk-replaces every row for
// that date from the browser's DOM — silently destroying any set a sidecar
// wrote and stripping provenance from the rest. Per-set saves are only safe
// because they address rows by id.
func (c Conf) AutoLog() bool {
	return c.CVAutoLog || c.VoltraAutoLog
}

// Exercise - one exercise
type Exercise struct {
	ID     int             `db:"ID"`
	Group  string          `db:"GR"`
	Place  string          `db:"PLACE"`
	Name   string          `db:"NAME"`
	Descr  string          `db:"DESCR"`
	Image  string          `db:"IMAGE"`
	Color  string          `db:"COLOR"`
	Weight decimal.Decimal `db:"WEIGHT"`
	Reps   int             `db:"REPS"`
	// Voltra marks an exercise as performed on the Voltra trainer. The
	// trainer reports its own resistance and rep count, so pump-voltra
	// owns these sets and pump-cv skips them (its plate detection cannot
	// see electronic resistance). A flag rather than a name heuristic:
	// plenty of exercises have "cable" in the name and use a plate stack.
	Voltra bool `db:"VOLTRA"`
	// Focus is the primary muscle this exercise targets within its group
	// (e.g. "Hamstrings" for an RDL in the Legs group). Free text validated
	// against the group's muscle catalog on the create form; used by future
	// workout plans to ensure every focus muscle in a group is covered.
	Focus string `db:"FOCUS"`
	// Bodyweight marks an exercise whose load is the athlete's own body
	// weight (lunges, pull-ups). When set, the workout log pre-fills the
	// most recent body-weight reading and offers an optional added-weight
	// field for holding dumbbells / a weight belt.
	Bodyweight bool `db:"BODYWEIGHT"`
}

// ExerciseRecency is the last-performed signal used to order the exercise
// picker (Feature 2): the most recent date an exercise was logged and its
// position within that session. Keyed by exercise name.
type ExerciseRecency struct {
	LastDate string // YYYY-MM-DD of the most recent session containing it
	Pos      int    // its position (order performed) in that session
}

// Group is one training group (Chest, Legs, …). Groups were historically an
// emergent string on exercises/muscles; this promotes them to a managed entity
// so they can be created, renamed, reordered, and deleted from the Library.
// Name is the identity (exercises.gr / muscles.gr still store it by value);
// Sort drives display order across the picker, Library, and Muscle Balance.
type Group struct {
	Name string `db:"NAME" json:"Name"`
	Sort int    `db:"SORT_ORDER" json:"Sort"`
}

// FocusMuscle is one muscle an exercise targets, resolved from the
// exercise_muscles junction joined to the muscle catalog. Primary muscles are
// constrained to the exercise's own group and get full credit; secondary
// muscles may come from any group (a compound lift's accessory involvement) and
// get partial credit in Muscle Balance.
type FocusMuscle struct {
	MuscleID int    `db:"MUSCLE_ID" json:"MuscleID"`
	Group    string `db:"GR" json:"Group"`
	Name     string `db:"NAME" json:"Name"`
	Primary  bool   `db:"IS_PRIMARY" json:"Primary"`
}

// Routine is a reusable workout template: an ordered list of exercises with
// per-exercise targets. Selecting one on the workout page scaffolds the day's
// rows; it stores intent, not logged sets.
type Routine struct {
	ID    int           `json:"ID"`
	Name  string        `json:"Name"`
	Notes string        `json:"Notes"`
	Sort  int           `json:"Sort"`
	Items []RoutineItem `json:"Items"`
}

// RoutineItem is one planned exercise within a routine. ExerciseName and Color
// are resolved from the exercise catalog for display and scaffolding; a zero
// TargetWeight/TargetReps means "fall back to the exercise's own default".
type RoutineItem struct {
	ID           int             `json:"ID"`
	RoutineID    int             `json:"RoutineID"`
	ExerciseID   int             `json:"ExerciseID"`
	Position     int             `json:"Position"`
	TargetSets   int             `json:"TargetSets"`
	TargetReps   int             `json:"TargetReps"`
	TargetWeight decimal.Decimal `json:"TargetWeight"`
	ExerciseName string          `json:"ExerciseName"`
	Color        string          `json:"Color"`
}

// Muscle is one entry in the DB-editable muscle catalog. Muscles are keyed
// by group name (matching Exercise.Group), so a group's focus options are all
// muscles sharing its Group. The group list the UI offers is the union of
// exercise groups and catalog groups, so a group created here with no
// exercises yet is still selectable.
type Muscle struct {
	ID    int    `db:"ID" json:"ID"`
	Group string `db:"GR" json:"Group"`
	Name  string `db:"NAME" json:"Name"`
	Sort  int    `db:"SORT_ORDER" json:"Sort"`
}

// Set - one set
type Set struct {
	ID           int             `db:"ID" json:"ID"`
	Date         string          `db:"DATE" json:"Date"`
	Name         string          `db:"NAME" json:"Name"`
	Color        string          `db:"COLOR" json:"Color"`
	WorkoutColor string          `db:"WORKOUT_COLOR" json:"WorkoutColor"`
	Weight       decimal.Decimal `db:"WEIGHT" json:"Weight"`
	Reps         int             `db:"REPS" json:"Reps"`
	Note         string          `db:"NOTE" json:"Note"`
	Source       string          `db:"SOURCE" json:"Source"`         // "manual" | "cv"
	Confidence   float64         `db:"CONFIDENCE" json:"Confidence"` // 0.0–1.0
	Pending      bool            `db:"PENDING" json:"Pending"`
	ClipPath     string          `db:"CLIP_PATH" json:"ClipPath"` // path under PUMP_CLIPS_DIR, served at /clips/<...>
	// Position orders sets within a single date. It is assigned on insert
	// (append to the end of the day) and rewritten by a drag-and-drop
	// reorder. Sets are read back in (date, position, id) order, so the
	// workout log renders in the athlete's chosen order rather than by id.
	Position int `db:"POSITION" json:"Position"`
	// AddedWeight is the extra load on a bodyweight exercise (dumbbells, a
	// weight belt). Nil for ordinary weighted sets — a non-nil value (even 0)
	// marks the row as a bodyweight set whose Weight is the body-weight
	// snapshot taken when it was logged. Total load = Weight + AddedWeight.
	AddedWeight *decimal.Decimal `db:"ADDED_WEIGHT" json:"AddedWeight,omitempty"`
	// ExerciseID is the normalized foreign key to exercises(id), added in
	// schema v19. The free-text Name column remains the logged record of what
	// was typed; ExerciseID augments it so reads and the prescription layer can
	// join sets -> exercises without string matching. It is resolved from Name
	// on insert and nil when the name matched no exercise (ad-hoc free-text
	// entry, or an exercise renamed after the set was logged).
	ExerciseID *int `db:"EXERCISE_ID" json:"ExerciseID,omitempty"`
}

// SetUpdate - partial update for one set. Only non-nil fields are applied.
type SetUpdate struct {
	Name   *string          `json:"Name,omitempty"`
	Weight *decimal.Decimal `json:"Weight,omitempty"`
	Reps   *int             `json:"Reps,omitempty"`
	Note   *string          `json:"Note,omitempty"`
	// Source lets the browser stamp provenance when it fills a set's reps from
	// the trainer — the row stays browser-owned (one row, PATCHed in place) but
	// records that the count came from Voltra, so the "Updated by Voltra" chip
	// survives a reload.
	Source      *string          `json:"Source,omitempty"`
	Confidence  *float64         `json:"Confidence,omitempty"`
	Pending     *bool            `json:"Pending,omitempty"`
	ClipPath    *string          `json:"ClipPath,omitempty"`
	AddedWeight *decimal.Decimal `json:"AddedWeight,omitempty"`
}

// AllExData - all sets and exercises
type AllExData struct {
	Exs    []Exercise
	Sets   []Set
	Weight []BodyWeight
}

// BodyWeight - store weight. RecordedAt is the precise instant the reading
// was taken (ISO8601 with TZ). Date is the logical day the reading belongs
// to and is what the UI groups/displays by — multiple readings can share a
// date and the latest recorded_at wins for display.
type BodyWeight struct {
	ID         int             `db:"ID"`
	Date       string          `db:"DATE"`
	RecordedAt string          `db:"RECORDED_AT"`
	Weight     decimal.Decimal `db:"WEIGHT"`
}

// Measurement is one occasional standalone metric reading — grip strength (per
// hand, in lb) or a dead hang (a time in seconds). Not tied to a workout or a
// muscle group; charted over time on the Stats "Grip & Hang" tab.
type Measurement struct {
	ID         int             `json:"ID"`
	Date       string          `json:"Date"`
	RecordedAt string          `json:"RecordedAt"`
	Metric     string          `json:"Metric"` // "grip" | "dead_hang"
	Hand       string          `json:"Hand"`   // "left" | "right" | ""
	Value      decimal.Decimal `json:"Value"`  // lb for grip, seconds for dead_hang
}

// Program is a training block / mesocycle authored by a coach (e.g. the Arnold
// agent): a start date, a length in weeks, and which week is the deload. It
// holds intent — logged work still lives in sets. SelectPrograms resolves its
// Sessions (and their Items) for display.
type Program struct {
	ID          int    `json:"ID"`
	Name        string `json:"Name"`
	StartDate   string `json:"StartDate"` // YYYY-MM-DD
	LengthWeeks int    `json:"LengthWeeks"`
	DeloadWeek  int    `json:"DeloadWeek"` // 1-based; 0 = none
	CreatedBy   string `json:"CreatedBy"`
	Notes       string `json:"Notes"`
	CreatedAt   string `json:"CreatedAt,omitempty"`
	// CursorPosition is the position of the next-due session in the rotation
	// queue. Rob trains autoregulated (by feel, no schedule), so the workout
	// page serves the committed session at this position rather than one dated
	// to today; completing a session advances it to the next position (wrap).
	CursorPosition int                 `json:"CursorPosition"`
	Sessions       []PrescribedSession `json:"Sessions"`
}

// PrescribedSession is a dated planned session within a Program. Status is
// "provisional" or "committed" — an author materializes the next session
// concretely (committed) while leaving later ones provisional, the
// one-exposure-ahead leash the plan design calls for.
type PrescribedSession struct {
	ID        int              `json:"ID"`
	ProgramID int              `json:"ProgramID"`
	Date      string           `json:"Date"` // YYYY-MM-DD
	Label     string           `json:"Label"`
	Status    string           `json:"Status"` // "provisional" | "committed"
	Notes     string           `json:"Notes"`
	Position  int              `json:"Position"`
	Items     []PrescribedItem `json:"Items"`
}

// PrescribedItem is one planned exercise within a PrescribedSession. TargetRIR
// and TargetWeight are nil when not prescribed / still provisional (a pull
// whose load waits on the prior session's feedback). Flags is a comma-separated
// tag list (e.g. "grip-offload,elbow-gated"). AltExerciseID is a substitution,
// nil when none. ExerciseName/Color/AltExerciseName are resolved from the
// catalog for display.
type PrescribedItem struct {
	ID              int              `json:"ID"`
	SessionID       int              `json:"SessionID"`
	ExerciseID      int              `json:"ExerciseID"`
	Position        int              `json:"Position"`
	TargetSets      int              `json:"TargetSets"`
	TargetRepsLow   int              `json:"TargetRepsLow"`
	TargetRepsHigh  int              `json:"TargetRepsHigh"`
	TargetRIR       *int             `json:"TargetRIR,omitempty"`
	TargetWeight    *decimal.Decimal `json:"TargetWeight,omitempty"`
	Rationale       string           `json:"Rationale"`
	Flags           string           `json:"Flags"`
	AltExerciseID   *int             `json:"AltExerciseID,omitempty"`
	ExerciseName    string           `json:"ExerciseName"`
	Color           string           `json:"Color"`
	AltExerciseName string           `json:"AltExerciseName,omitempty"`
}

// SessionFeedback is the athlete's post-session report for a given day: a
// session RPE (6-10) and a 0-3 next-day soreness/irritation flag. Either may be
// nil. Keyed by Date (PUMP has no session row); latest RecordedAt wins.
type SessionFeedback struct {
	ID         int      `json:"ID"`
	Date       string   `json:"Date"` // YYYY-MM-DD
	RPE        *float64 `json:"RPE,omitempty"`
	ElbowFlag  *int     `json:"ElbowFlag,omitempty"`
	Note       string   `json:"Note"`
	RecordedAt string   `json:"RecordedAt,omitempty"`
}

// QueuePlan is the workout page's view of the training queue. Rob trains
// autoregulated (by feel, no schedule), so the plan is a repeating rotation of
// committed sessions in position order; the active program's cursor marks the
// next-due session. Current is the session the "Arnold's Routine" button loads;
// Sessions is the full rotation for the picker; NextLabel names the session the
// cursor advances to when Current is completed.
type QueuePlan struct {
	ProgramID      int                 `json:"ProgramID"`
	CursorPosition int                 `json:"CursorPosition"`
	Current        PrescribedSession   `json:"Current"`
	Sessions       []PrescribedSession `json:"Sessions"`
	NextLabel      string              `json:"NextLabel"`
}

// BuildQueuePlan derives the queue view from the authored programs. The active
// program is the most recently authored (highest id); its committed sessions,
// in position order, are the rotation. Current is the committed session at or
// after the cursor position (wrapping to the first when the cursor has moved
// past the end). ok is false — a freestyle state — when there is no program or
// the active one has no committed sessions. Provisional sessions are drafts and
// never enter the rotation.
func BuildQueuePlan(programs []Program) (*QueuePlan, bool) {
	if len(programs) == 0 {
		return nil, false
	}
	active := programs[0]
	for _, p := range programs[1:] {
		if p.ID > active.ID {
			active = p
		}
	}
	committed := make([]PrescribedSession, 0, len(active.Sessions))
	for _, s := range active.Sessions {
		if s.Status == "committed" {
			committed = append(committed, s)
		}
	}
	if len(committed) == 0 {
		return nil, false
	}
	sort.SliceStable(committed, func(i, j int) bool {
		return committed[i].Position < committed[j].Position
	})
	cur := 0
	for i, s := range committed {
		if s.Position >= active.CursorPosition {
			cur = i
			break
		}
	}
	next := committed[(cur+1)%len(committed)]
	return &QueuePlan{
		ProgramID:      active.ID,
		CursorPosition: active.CursorPosition,
		Current:        committed[cur],
		Sessions:       committed,
		NextLabel:      next.Label,
	}, true
}

// SessionCompletion binds a calendar day to the prescribed session marked
// complete on it. It is what freezes a completed day: a day that has a
// completion renders that session as done (from the snapshotted Label), rather
// than the program cursor's next-up session — which then surfaces only on
// later, not-yet-completed days. One row per day; SessionID is nil when the
// completed session was later removed (the Label snapshot still names it).
type SessionCompletion struct {
	Date      string `json:"Date"`                // YYYY-MM-DD
	SessionID *int   `json:"SessionID,omitempty"` // nil if the session was later removed
	Label     string `json:"Label"`               // snapshot of the completed session's label
}

// HealthRecord is one wearable health datum ingested from Android Health
// Connect via the HC Webhook bridge. It is deliberately generic so new
// Health Connect types ingest without a schema change: scalar readings land
// in Value+Unit, and anything non-scalar (sleep stages, exercise distance/
// cadence, or fields of an unmapped type) lands in Extra as raw JSON. The
// store dedupes on (MetricType, StartTime, EndTime); EndTime is always set
// (instantaneous samples use EndTime == StartTime).
type HealthRecord struct {
	ID         int64            `db:"ID" json:"ID"`
	MetricType string           `db:"METRIC_TYPE" json:"MetricType"` // "steps","heart_rate","resting_heart_rate","sleep","exercise",...
	StartTime  time.Time        `db:"START_TIME" json:"StartTime"`
	EndTime    *time.Time       `db:"END_TIME" json:"EndTime,omitempty"`
	Value      *decimal.Decimal `db:"VALUE" json:"Value,omitempty"` // count/bpm/duration; nil when only Extra carries data
	Unit       string           `db:"UNIT" json:"Unit,omitempty"`   // "steps","bpm","seconds",...
	Extra      json.RawMessage  `db:"EXTRA" json:"Extra,omitempty"` // stages[], distance, cadence, unmapped fields
	Source     string           `db:"SOURCE" json:"Source,omitempty"`
	IngestedAt time.Time        `db:"INGESTED_AT" json:"IngestedAt,omitempty"`
}

// ── Health dashboard aggregates ─────────────────────────────────────────────
// Server-aggregated wearable + body metrics shared by the Health dashboard
// page and the Stats wearable tabs. Built from []HealthRecord by the web
// layer. All series are oldest-first; dates are YYYY-MM-DD in server TZ.

// DayValue is a single dated scalar (daily step total, resting HR, …).
type DayValue struct {
	Date  string  `json:"Date"`
	Value float64 `json:"Value"`
}

// DayRange is a dated min/avg/max (e.g. a day's heart-rate spread).
type DayRange struct {
	Date string  `json:"Date"`
	Min  float64 `json:"Min"`
	Avg  float64 `json:"Avg"`
	Max  float64 `json:"Max"`
}

// SleepNight is one night keyed by wake date, with per-stage minutes (zero
// when the source didn't report stages).
type SleepNight struct {
	Date     string  `json:"Date"`
	Minutes  float64 `json:"Minutes"`
	Deep     float64 `json:"Deep"`
	Light    float64 `json:"Light"`
	REM      float64 `json:"REM"`
	Awake    float64 `json:"Awake"`
	Bedtime  string  `json:"Bedtime"`  // "10:45 PM"; "" when unknown
	WakeTime string  `json:"WakeTime"` // "6:32 AM";  "" when unknown
}

// CardioSession is one band-tracked workout.
type CardioSession struct {
	Date    string  `json:"Date"`
	Type    string  `json:"Type"`
	Minutes float64 `json:"Minutes"`
	Meters  float64 `json:"Meters"`
}

// HealthStats is the aggregated wearable view. Available is false when the
// records could not be loaded — the UI then shows empty states instead of
// failing.
type HealthStats struct {
	Available      bool            `json:"Available"`
	DailySteps     []DayValue      `json:"DailySteps"`
	RestingHR      []DayValue      `json:"RestingHR"`
	DailyHR        []DayRange      `json:"DailyHR"`
	Sleep          []SleepNight    `json:"Sleep"`
	Cardio         []CardioSession `json:"Cardio"`
	ActiveCalories []DayValue      `json:"ActiveCalories"` // daily active-energy total (kcal)
}

// GuiData - web gui data
type GuiData struct {
	Config   Conf
	ExData   AllExData
	GroupMap []string // unique exercise groups, in display order
	OneEx    Exercise
	Muscles  []Muscle // DB-editable muscle catalog (focus options per group)
	Groups   []Group  // managed training groups (Library group management)
	// PrimaryIDs / SecondaryIDs mark, for the exercise being edited, which
	// catalog muscle ids are currently checked as primary vs secondary focus.
	// Keyed by muscle id so the edit form can render the multi-select state.
	PrimaryIDs   map[int]bool
	SecondaryIDs map[int]bool
	Routines     []Routine // workout templates (Library management + workout-page scaffolding)
	// Queue is the training-queue view: the cursor session the "Arnold's
	// Routine" button loads, the rotation for the picker, and the next-up
	// label. Non-nil adds the button + picker + plan header alongside the
	// always-present "Start routine" picker; nil is a freestyle state.
	Queue *QueuePlan
	// Completions are the day→completed-session bindings. The workout page
	// uses them to freeze a completed day on what was performed instead of
	// re-resolving it to the cursor's next-up session.
	Completions []SessionCompletion
	// ExerciseMuscles maps exercise id → its focus muscles, for the
	// muscle-level Muscle Balance view. JSON-encoded to the stats page.
	ExerciseMuscles map[int][]FocusMuscle
	Measurements    []Measurement // grip / dead-hang readings for the Grip & Hang tab
	Version         string
	ServerDate      string // today's date in server timezone (YYYY-MM-DD)
	// DisplayCutoff is the oldest date (YYYY-MM-DD) included in the initial
	// ExData.Sets window. The workout page fetches days older than this on
	// demand instead of them silently rendering empty.
	DisplayCutoff string
	Health        HealthStats // wearable aggregates (Health page + Stats tabs)
}
