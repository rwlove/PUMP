package web

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
)

// rec is a small helper to build a HealthRecord with a decimal Value.
func rec(metric string, start time.Time, value float64, extra string) models.HealthRecord {
	d := decimal.NewFromFloat(value)
	end := start
	r := models.HealthRecord{
		MetricType: metric,
		StartTime:  start,
		EndTime:    &end,
		Value:      &d,
	}
	if extra != "" {
		r.Extra = json.RawMessage(extra)
	}
	return r
}

// TestAggregateHealthSleepStagesNumeric verifies that Health Connect's numeric
// stage codes (4=LIGHT, 5=DEEP, 6=REM) are decoded — the bug that left Deep
// sleep perpetually at zero. Mirrors the real on-device payload shape.
func TestAggregateHealthSleepStagesNumeric(t *testing.T) {
	day := time.Date(2026, 6, 9, 23, 0, 0, 0, time.Local)
	extra := `{"stages":[
		{"stage":"5","duration_seconds":3600},
		{"stage":"4","duration_seconds":1200},
		{"stage":"6","duration_seconds":600},
		{"stage":"1","duration_seconds":300}
	],"duration_seconds":5700}`

	out := aggregateHealth([]models.HealthRecord{rec("sleep", day, 5700, extra)})
	if len(out.Sleep) != 1 {
		t.Fatalf("want 1 sleep night, got %d", len(out.Sleep))
	}
	n := out.Sleep[0]
	if n.Deep != 60 {
		t.Errorf("Deep: want 60 min (stage 5), got %v", n.Deep)
	}
	if n.Light != 20 {
		t.Errorf("Light: want 20 min (stage 4), got %v", n.Light)
	}
	if n.REM != 10 {
		t.Errorf("REM: want 10 min (stage 6), got %v", n.REM)
	}
	if n.Awake != 5 {
		t.Errorf("Awake: want 5 min (stage 1), got %v", n.Awake)
	}
}

// TestAggregateHealthRestingHRProxy verifies that when only raw heart_rate is
// present (no resting_heart_rate), a resting proxy is synthesized from the
// day's minimum so the Resting HR tile/line populate.
func TestAggregateHealthRestingHRProxy(t *testing.T) {
	d := time.Date(2026, 6, 7, 10, 0, 0, 0, time.Local)
	out := aggregateHealth([]models.HealthRecord{
		rec("heart_rate", d, 85, ""),
		rec("heart_rate", d.Add(time.Minute), 70, ""),
		rec("heart_rate", d.Add(2*time.Minute), 100, ""),
	})
	if len(out.RestingHR) != 1 {
		t.Fatalf("want 1 resting proxy day, got %d", len(out.RestingHR))
	}
	if out.RestingHR[0].Value != 70 {
		t.Errorf("resting proxy: want 70 (daily min), got %v", out.RestingHR[0].Value)
	}
	if len(out.DailyHR) != 1 || out.DailyHR[0].Max != 100 || out.DailyHR[0].Min != 70 {
		t.Errorf("DailyHR spread wrong: %+v", out.DailyHR)
	}
}

// TestAggregateHealthRestingHRRealWins verifies a real resting_heart_rate
// record takes precedence over the heart_rate-min proxy on the same day.
func TestAggregateHealthRestingHRRealWins(t *testing.T) {
	d := time.Date(2026, 6, 7, 10, 0, 0, 0, time.Local)
	out := aggregateHealth([]models.HealthRecord{
		rec("heart_rate", d, 70, ""),
		rec("resting_heart_rate", d, 55, ""),
	})
	if len(out.RestingHR) != 1 || out.RestingHR[0].Value != 55 {
		t.Errorf("real resting_heart_rate should win (55), got %+v", out.RestingHR)
	}
}

// TestAggregateHealthDistinctIntervalsSum verifies that genuine sub-day
// intervals (distinct start_times) sum, for both steps and active calories.
func TestAggregateHealthDistinctIntervalsSum(t *testing.T) {
	d := time.Date(2026, 6, 8, 0, 0, 0, 0, time.Local)
	out := aggregateHealth([]models.HealthRecord{
		rec("active_calories", d, 174, ""),
		rec("active_calories", d.Add(time.Hour), 89, ""),
		rec("steps", d, 1000, ""),
		rec("steps", d.Add(time.Hour), 500, ""),
	})
	if len(out.ActiveCalories) != 1 || out.ActiveCalories[0].Value != 263 {
		t.Errorf("active calories distinct intervals: want 263 (174+89), got %+v", out.ActiveCalories)
	}
	if len(out.DailySteps) != 1 || out.DailySteps[0].Value != 1500 {
		t.Errorf("steps distinct intervals: want 1500 (1000+500), got %+v", out.DailySteps)
	}
}

// TestAggregateHealthCumulativeSnapshots verifies that re-reported cumulative
// daily snapshots (same midnight start, growing running total) collapse to the
// day's MAX instead of summing — the bug that inflated steps ~9x.
func TestAggregateHealthCumulativeSnapshots(t *testing.T) {
	d := time.Date(2026, 6, 8, 0, 0, 0, 0, time.Local)
	out := aggregateHealth([]models.HealthRecord{
		rec("steps", d, 2660, ""),
		rec("steps", d, 3146, ""),
		rec("steps", d, 4607, ""),
		rec("active_calories", d, 100, ""),
		rec("active_calories", d, 263, ""),
		rec("active_calories", d, 263, ""),
	})
	if len(out.DailySteps) != 1 || out.DailySteps[0].Value != 4607 {
		t.Errorf("steps: want daily max 4607 (not summed), got %+v", out.DailySteps)
	}
	if len(out.ActiveCalories) != 1 || out.ActiveCalories[0].Value != 263 {
		t.Errorf("active calories: want daily max 263 (not summed), got %+v", out.ActiveCalories)
	}
}
