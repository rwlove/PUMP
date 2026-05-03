package models

import (
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
}

// SetUpdate - partial update for one set. Only non-nil fields are applied.
type SetUpdate struct {
	Name       *string          `json:"Name,omitempty"`
	Weight     *decimal.Decimal `json:"Weight,omitempty"`
	Reps       *int             `json:"Reps,omitempty"`
	Note       *string          `json:"Note,omitempty"`
	Confidence *float64         `json:"Confidence,omitempty"`
	Pending    *bool            `json:"Pending,omitempty"`
}

// AllExData - all sets and exercises
type AllExData struct {
	Exs    []Exercise
	Sets   []Set
	Weight []BodyWeight
}

// BodyWeight - store weight
type BodyWeight struct {
	ID     int             `db:"ID"`
	Date   string          `db:"DATE"`
	Weight decimal.Decimal `db:"WEIGHT"`
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
