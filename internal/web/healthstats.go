package web

import (
	"encoding/json"
	"log/slog"
	"sort"
	"time"

	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/store"
)

// healthLookbackDays bounds how much wearable history we pull for the Health
// dashboard + Stats tabs. A bit over a year covers the "annual" period.
const healthLookbackDays = 400

// loadHealthStats fetches and aggregates wearable records from the active
// store. Returns an empty (Available:false) struct when the store has no
// HealthStore (split-frontend / APIClient mode) or on error, so the page
// degrades to empty states rather than failing.
func loadHealthStats() models.HealthStats {
	hs, ok := dataStore.(store.HealthStore)
	if !ok {
		return models.HealthStats{}
	}
	since := time.Now().AddDate(0, 0, -healthLookbackDays)
	recs, err := hs.SelectHealthRecords("", since)
	if err != nil {
		slog.Error("loadHealthStats: SelectHealthRecords failed", slog.Any("error", err))
		return models.HealthStats{}
	}
	return aggregateHealth(recs)
}

// aggregateHealth collapses raw HealthRecords into the compact per-day series
// the UI charts. Pure (no store) so it's trivially testable. Steps sum per
// day; resting HR averages per day; heart_rate becomes a daily min/avg/max
// spread; sleep groups by wake date with per-stage minutes; exercise becomes
// individual cardio sessions.
func aggregateHealth(recs []models.HealthRecord) models.HealthStats {
	out := models.HealthStats{Available: true}

	stepsByDay := map[string]float64{}
	restingByDay := map[string][]float64{}
	hrByDay := map[string][]float64{}
	sleepByDay := map[string]*models.SleepNight{}

	val := func(r models.HealthRecord) float64 {
		if r.Value == nil {
			return 0
		}
		f, _ := r.Value.Float64()
		return f
	}

	for _, r := range recs {
		day := r.StartTime.Local().Format("2006-01-02")
		switch r.MetricType {
		case "steps":
			stepsByDay[day] += val(r)
		case "resting_heart_rate":
			restingByDay[day] = append(restingByDay[day], val(r))
		case "heart_rate":
			hrByDay[day] = append(hrByDay[day], val(r))
		case "sleep":
			// Key a night by its wake (end) date when available.
			d := day
			if r.EndTime != nil {
				d = r.EndTime.Local().Format("2006-01-02")
			}
			n := sleepByDay[d]
			if n == nil {
				n = &models.SleepNight{Date: d}
				sleepByDay[d] = n
			}
			n.Minutes += val(r) / 60.0
			addSleepStages(r.Extra, n)
		case "exercise":
			out.Cardio = append(out.Cardio, cardioFromRecord(r, day, val(r)))
		}
	}

	for d, v := range stepsByDay {
		out.DailySteps = append(out.DailySteps, models.DayValue{Date: d, Value: v})
	}
	for d, vs := range restingByDay {
		out.RestingHR = append(out.RestingHR, models.DayValue{Date: d, Value: mean(vs)})
	}
	for d, vs := range hrByDay {
		mn, av, mx := minAvgMax(vs)
		out.DailyHR = append(out.DailyHR, models.DayRange{Date: d, Min: mn, Avg: av, Max: mx})
	}
	for _, n := range sleepByDay {
		out.Sleep = append(out.Sleep, *n)
	}

	sortByDate(out.DailySteps)
	sortByDate(out.RestingHR)
	sort.Slice(out.DailyHR, func(i, j int) bool { return out.DailyHR[i].Date < out.DailyHR[j].Date })
	sort.Slice(out.Sleep, func(i, j int) bool { return out.Sleep[i].Date < out.Sleep[j].Date })
	sort.Slice(out.Cardio, func(i, j int) bool { return out.Cardio[i].Date < out.Cardio[j].Date })

	return out
}

func sortByDate(s []models.DayValue) {
	sort.Slice(s, func(i, j int) bool { return s[i].Date < s[j].Date })
}

func mean(vs []float64) float64 {
	if len(vs) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vs {
		sum += v
	}
	return sum / float64(len(vs))
}

func minAvgMax(vs []float64) (mn, av, mx float64) {
	if len(vs) == 0 {
		return 0, 0, 0
	}
	mn, mx = vs[0], vs[0]
	var sum float64
	for _, v := range vs {
		if v < mn {
			mn = v
		}
		if v > mx {
			mx = v
		}
		sum += v
	}
	return mn, sum / float64(len(vs)), mx
}

// addSleepStages parses a sleep record's Extra ({"stages":[{stage,duration_seconds}]})
// and accumulates per-stage minutes onto n. Unknown stage labels are ignored
// (their time still counts toward Minutes from the session duration).
func addSleepStages(extra json.RawMessage, n *models.SleepNight) {
	if len(extra) == 0 {
		return
	}
	var e struct {
		Stages []struct {
			Stage           string  `json:"stage"`
			DurationSeconds float64 `json:"duration_seconds"`
		} `json:"stages"`
	}
	if err := json.Unmarshal(extra, &e); err != nil {
		return
	}
	for _, s := range e.Stages {
		m := s.DurationSeconds / 60.0
		switch s.Stage {
		case "DEEP":
			n.Deep += m
		case "LIGHT":
			n.Light += m
		case "REM":
			n.REM += m
		case "AWAKE", "OUT_OF_BED", "AWAKE_IN_BED":
			n.Awake += m
		}
	}
}

// cardioFromRecord builds a CardioSession from an exercise record. Value is
// the session duration in seconds; type and distance come from Extra.
func cardioFromRecord(r models.HealthRecord, day string, durationSeconds float64) models.CardioSession {
	cs := models.CardioSession{Date: day, Minutes: durationSeconds / 60.0}
	if len(r.Extra) > 0 {
		var e struct {
			Type           string  `json:"type"`
			DistanceMeters float64 `json:"distance_meters"`
		}
		if err := json.Unmarshal(r.Extra, &e); err == nil {
			cs.Type = e.Type
			cs.Meters = e.DistanceMeters
		}
	}
	if cs.Type == "" {
		cs.Type = "Workout"
	}
	return cs
}
