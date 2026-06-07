package models

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"
)

// Conf - web gui config
type Conf struct {
	Host          string
	Port          string
	Color         string
	Icon          string
	ConfPath      string
	NodePath      string
	PageStep      int
	FrequencyDays int  // days to look back when sorting exercises by usage frequency
	DisplayDays   int  // days of history shown on main page (7/30/90/365)
	AutoFill      bool // pre-fill weight/reps from last performance of that exercise
	CVAutoLog     bool // when true, accept set writes from the pump-cv sidecar

	// Pushover credentials are read from env on startup (PUSHOVER_USER_KEY,
	// PUSHOVER_APP_TOKEN) and never serialised through the JSON API or
	// persisted to the on-disk config file. The web UI shows only a
	// "configured / not configured" indicator.
	PushoverUserKey  string `json:"-" yaml:"-"`
	PushoverAppToken string `json:"-" yaml:"-"`
}

// PushoverConfigured reports whether both Pushover env vars are set.
func (c Conf) PushoverConfigured() bool {
	return c.PushoverUserKey != "" && c.PushoverAppToken != ""
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
	ClipPath     string          `db:"CLIP_PATH" json:"ClipPath"`    // path under PUMP_CLIPS_DIR, served at /clips/<...>
}

// SetUpdate - partial update for one set. Only non-nil fields are applied.
type SetUpdate struct {
	Name       *string          `json:"Name,omitempty"`
	Weight     *decimal.Decimal `json:"Weight,omitempty"`
	Reps       *int             `json:"Reps,omitempty"`
	Note       *string          `json:"Note,omitempty"`
	Confidence *float64         `json:"Confidence,omitempty"`
	Pending    *bool            `json:"Pending,omitempty"`
	ClipPath   *string          `json:"ClipPath,omitempty"`
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

// GuiData - web gui data
type GuiData struct {
	Config     Conf
	ExData     AllExData
	GroupMap   []string // unique exercise groups, in display order
	OneEx      Exercise
	Version    string
	ServerDate string // today's date in server timezone (YYYY-MM-DD)
}
