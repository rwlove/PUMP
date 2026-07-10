package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"time"

	"github.com/rwlove/PUMP/internal/models"
)

// healthLookbackDays bounds how much wearable history we pull for the Health
// dashboard + Stats tabs. A bit over a year covers the "annual" period.
const healthLookbackDays = 400

// loadHealthStats fetches and aggregates wearable records from the active
// store. Returns an empty (Available:false) struct on error, so the page
// degrades to empty states rather than failing.
func loadHealthStats(ctx context.Context) models.HealthStats {
	since := time.Now().AddDate(0, 0, -healthLookbackDays)
	recs, err := healthStore.SelectHealthRecords(ctx, "", since)
	if err != nil {
		slog.Error("loadHealthStats: SelectHealthRecords failed", slog.Any("error", err))
		return models.HealthStats{}
	}
	return aggregateHealth(recs)
}

// aggregateHealth collapses raw HealthRecords into the compact per-day series
// the UI charts. Pure (no store) so it's trivially testable. Steps sum per
// day; resting HR averages per day; heart_rate becomes a daily min/avg/max
// spread; sleep dedupes cumulative snapshots per session then groups by wake
// date with per-stage minutes and bed/wake times; exercise becomes individual
// cardio sessions.
func aggregateHealth(recs []models.HealthRecord) models.HealthStats {
	out := models.HealthStats{Available: true}

	// steps and active_calories arrive as cumulative daily-total snapshots:
	// the bridge re-reports the running daily figure with the same midnight
	// start_time and a growing end_time (and dedup keys on end_time, so every
	// snapshot persists). Summing them multiplies the real total by the number
	// of snapshots. Accumulate per (day, start_time) taking the MAX, then sum
	// across distinct start_times per day — correct for both re-reported daily
	// snapshots (one start ⇒ the final total) and genuine sub-day intervals
	// (distinct starts ⇒ summed).
	stepsByDay := map[string]map[int64]float64{}
	caloriesByDay := map[string]map[int64]float64{}
	restingByDay := map[string][]float64{}
	hrByDay := map[string][]float64{}
	sleepByDay := map[string]*models.SleepNight{}
	// Sleep records arrive as cumulative snapshots of an in-progress session:
	// the bridge re-reports the same night with a growing duration and a
	// growing stages[] (and a growing top-level start_time, so — unlike steps —
	// they can't be deduped by start_time). Collapse them per session, keyed by
	// the session's true start (earliest stage start = bed time), keeping the
	// snapshot with the largest duration (the final, complete reading). Summing
	// the raw snapshots would multiply a night's sleep by its snapshot count.
	sleepSessions := map[int64]sleepSnapshot{}

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
			addSnapshot(stepsByDay, day, r.StartTime.Unix(), val(r))
		case "active_calories":
			addSnapshot(caloriesByDay, day, r.StartTime.Unix(), val(r))
		case "resting_heart_rate":
			restingByDay[day] = append(restingByDay[day], val(r))
		case "heart_rate":
			hrByDay[day] = append(hrByDay[day], val(r))
		case "sleep":
			start, end := sleepSpan(r)
			key := start.UnixNano()
			if cur, ok := sleepSessions[key]; !ok || val(r) > cur.dur {
				sleepSessions[key] = sleepSnapshot{start: start, end: end, dur: val(r), extra: r.Extra}
			}
		case "exercise":
			out.Cardio = append(out.Cardio, cardioFromRecord(r, day, val(r)))
		}
	}

	for d, m := range stepsByDay {
		out.DailySteps = append(out.DailySteps, models.DayValue{Date: d, Value: sumSnapshots(m)})
	}
	for d, m := range caloriesByDay {
		out.ActiveCalories = append(out.ActiveCalories, models.DayValue{Date: d, Value: sumSnapshots(m)})
	}
	for d, vs := range hrByDay {
		mn, av, mx := minAvgMax(vs)
		out.DailyHR = append(out.DailyHR, models.DayRange{Date: d, Min: mn, Avg: av, Max: mx})
		// Resting HR proxy: when a source only exports raw heart_rate (no
		// dedicated resting_heart_rate samples), use the day's minimum as a
		// resting estimate so the Resting HR tile/line aren't perpetually
		// blank. A real resting_heart_rate record for the day always wins.
		if _, ok := restingByDay[d]; !ok {
			restingByDay[d] = []float64{mn}
		}
	}
	for d, vs := range restingByDay {
		out.RestingHR = append(out.RestingHR, models.DayValue{Date: d, Value: mean(vs)})
	}
	// Fold the deduped sessions into per-night rollups, keyed by wake date, so
	// genuine multi-session nights (e.g. a nap plus the main sleep) still sum
	// while snapshot duplicates do not. Bed time is the night's earliest start,
	// wake time its latest end.
	sleepStartByDay := map[string]time.Time{}
	sleepEndByDay := map[string]time.Time{}
	for _, s := range sleepSessions {
		d := s.end.Local().Format("2006-01-02")
		n := sleepByDay[d]
		if n == nil {
			n = &models.SleepNight{Date: d}
			sleepByDay[d] = n
		}
		n.Minutes += s.dur / 60.0
		addSleepStages(s.extra, n)
		if t, ok := sleepStartByDay[d]; !ok || s.start.Before(t) {
			sleepStartByDay[d] = s.start
		}
		if t, ok := sleepEndByDay[d]; !ok || s.end.After(t) {
			sleepEndByDay[d] = s.end
		}
	}
	for d, n := range sleepByDay {
		if s, ok := sleepStartByDay[d]; ok {
			n.Bedtime = s.Local().Format("3:04 PM")
		}
		if e, ok := sleepEndByDay[d]; ok {
			n.WakeTime = e.Local().Format("3:04 PM")
		}
		out.Sleep = append(out.Sleep, *n)
	}

	sortByDate(out.DailySteps)
	sortByDate(out.ActiveCalories)
	sortByDate(out.RestingHR)
	sort.Slice(out.DailyHR, func(i, j int) bool { return out.DailyHR[i].Date < out.DailyHR[j].Date })
	sort.Slice(out.Sleep, func(i, j int) bool { return out.Sleep[i].Date < out.Sleep[j].Date })
	sort.Slice(out.Cardio, func(i, j int) bool { return out.Cardio[i].Date < out.Cardio[j].Date })

	return out
}

// addSnapshot records v as the max seen for (day, start) — collapsing the
// bridge's re-reported cumulative snapshots of the same interval to their
// final value. See the comment in aggregateHealth.
func addSnapshot(m map[string]map[int64]float64, day string, start int64, v float64) {
	if m[day] == nil {
		m[day] = map[int64]float64{}
	}
	if v > m[day][start] {
		m[day][start] = v
	}
}

// sumSnapshots totals the per-start maxima for a day.
func sumSnapshots(m map[int64]float64) float64 {
	var s float64
	for _, v := range m {
		s += v
	}
	return s
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

// sleepSnapshot is one deduped sleep session: its true span (bed→wake), total
// duration in seconds, and the Extra of the fullest snapshot (for stage rollup).
type sleepSnapshot struct {
	start, end time.Time
	dur        float64
	extra      json.RawMessage
}

// sleepSpan returns a sleep record's true span. The bridge frequently omits a
// top-level session end_time (so StartTime == EndTime) and re-reports the same
// session with a growing top-level start_time; the real bed/wake times live on
// the per-stage segments. Prefer the earliest stage start and latest stage end
// when stages are present, falling back to the record's own timestamps.
func sleepSpan(r models.HealthRecord) (start, end time.Time) {
	start = r.StartTime
	if r.EndTime != nil {
		end = *r.EndTime
	} else {
		end = r.StartTime
	}
	var e struct {
		Stages []struct {
			Start string `json:"start_time"`
			End   string `json:"end_time"`
		} `json:"stages"`
	}
	if len(r.Extra) == 0 || json.Unmarshal(r.Extra, &e) != nil {
		return start, end
	}
	var haveStart, haveEnd bool
	for _, s := range e.Stages {
		if t, err := time.Parse(time.RFC3339, s.Start); err == nil {
			if !haveStart || t.Before(start) {
				start, haveStart = t, true
			}
		}
		if t, err := time.Parse(time.RFC3339, s.End); err == nil {
			if !haveEnd || t.After(end) {
				end, haveEnd = t, true
			}
		}
	}
	return start, end
}

// addSleepStages parses a sleep record's Extra ({"stages":[{stage,duration_seconds}]})
// and accumulates per-stage minutes onto n. Unknown stages are ignored (their
// time still counts toward Minutes from the session duration).
//
// Health Connect serialises the stage as the integer SleepSessionRecord.Stage
// constant (e.g. "4", "5", "6"), not a label, so we map both the numeric codes
// and the human labels — different export bridges emit one or the other.
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
		case "DEEP", "5":
			n.Deep += m
		case "LIGHT", "SLEEPING", "4", "2":
			n.Light += m
		case "REM", "6":
			n.REM += m
		case "AWAKE", "OUT_OF_BED", "AWAKE_IN_BED", "1", "3", "7":
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
