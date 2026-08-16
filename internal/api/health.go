package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/store"
)

// healthStore is the subset of the active store that persists wearable
// health records. Set by RegisterRoutes.
var healthStore store.HealthStore

// healthIngestKey, when non-empty, requires POST /api/health callers to
// present a matching X-Api-Key header — same posture as weightIngestKey.
// The Android HC Webhook bridge sends it as a configured custom header,
// and the off-cluster Route is path-scoped to /api/health.
var healthIngestKey string

// healthEnvelopeMeta are root-object keys that carry payload metadata, not
// per-type record arrays.
var healthEnvelopeMeta = map[string]bool{
	"timestamp":   true,
	"app_version": true,
}

// postHealth ingests an Android Health Connect payload from the HC Webhook
// bridge. The body is a root object with metadata (timestamp, app_version)
// plus one snake_case array per data type (steps, heart_rate, sleep, ...).
// Re-delivery is safe — the store dedupes on (metric_type, start_time,
// end_time), so the bridge's rolling 48h window collapses to no-ops.
func postHealth(c *gin.Context) {
	if healthStore == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "health ingest unavailable"})
		return
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	recs, received, err := parseHealthEnvelope(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	inserted, err := healthStore.InsertHealthRecords(c.Request.Context(), recs)
	if err != nil {
		slog.Error("postHealth: InsertHealthRecords failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Info("health records ingested",
		slog.Int("received", received),
		slog.Int("parsed", len(recs)),
		slog.Int("inserted", inserted))
	c.JSON(http.StatusOK, gin.H{
		"received": received,
		"parsed":   len(recs),
		"inserted": inserted,
	})
}

// getHealth returns recent health records, optionally filtered by ?type=
// and ?since= (RFC3339). Defaults to the last 30 days, all types.
func getHealth(c *gin.Context) {
	if healthStore == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "health store unavailable"})
		return
	}
	metricType := c.Query("type")
	since := time.Now().AddDate(0, 0, -30)
	if s := c.Query("since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			// Reject rather than silently fall back to the 30-day default: a
			// caller asking for a wide window with a mistyped/date-only value
			// would otherwise get a narrow one with no way to tell.
			c.JSON(http.StatusBadRequest,
				gin.H{"error": "since must be RFC3339 (e.g. 2026-01-01T00:00:00Z)"})
			return
		}
		since = t
	}
	recs, err := healthStore.SelectHealthRecords(c.Request.Context(), metricType, since)
	if err != nil {
		slog.Error("getHealth: SelectHealthRecords failed", slog.Any("error", err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	slog.Debug("getHealth", slog.String("type", metricType), slog.Int("count", len(recs)))
	c.JSON(http.StatusOK, recs)
}

// parseHealthEnvelope flattens the HC Webhook root object into HealthRecords.
// Every non-metadata key whose value is a JSON array is treated as a metric
// type, and each element becomes one record. Known types (steps, heart_rate,
// resting_heart_rate, sleep, exercise) get a mapped scalar Value+Unit; any
// other type stores its first numeric field generically. In all cases every
// field other than the resolved timestamps is preserved in Extra, so "all
// exportable metrics" land even when a type isn't explicitly mapped.
// received is the total element count seen (before time-resolution drops).
func parseHealthEnvelope(raw []byte) (recs []models.HealthRecord, received int, err error) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, 0, fmt.Errorf("decode health payload: %w", err)
	}
	for key, val := range root {
		if healthEnvelopeMeta[key] {
			continue
		}
		var elems []json.RawMessage
		if err := json.Unmarshal(val, &elems); err != nil {
			// Not an array (e.g. a scalar metadata field) — skip.
			continue
		}
		for _, el := range elems {
			received++
			if r, ok := healthRecordFromElement(key, el); ok {
				recs = append(recs, r)
			}
		}
	}
	return recs, received, nil
}

// timeFields are the element keys, in priority order, from which a record's
// StartTime is resolved.
var startTimeFields = []string{"start_time", "time", "session_end_time"}

// healthRecordFromElement maps one array element of the given metricType into
// a HealthRecord. Returns ok=false when no timestamp can be resolved (a record
// with no time can't be stored or deduped).
func healthRecordFromElement(metricType string, el json.RawMessage) (models.HealthRecord, bool) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(el, &m); err != nil {
		return models.HealthRecord{}, false
	}

	start, ok := firstTime(m, startTimeFields...)
	if !ok {
		slog.Debug("health: skipping record with no resolvable time",
			slog.String("type", metricType))
		return models.HealthRecord{}, false
	}

	r := models.HealthRecord{
		MetricType: metricType,
		StartTime:  start,
		Source:     "health-connect",
	}
	if end, ok := firstTime(m, "end_time"); ok {
		r.EndTime = &end
	} else {
		// Instantaneous sample (or interval with no explicit end): make
		// EndTime == StartTime so dedupe has a non-NULL key.
		r.EndTime = &start
	}

	switch metricType {
	case "steps":
		setScalar(&r, m, "count", "steps")
	case "active_calories":
		setScalar(&r, m, "calories", "calories")
	case "heart_rate", "resting_heart_rate":
		setScalar(&r, m, "bpm", "bpm")
	case "sleep", "exercise":
		setScalar(&r, m, "duration_seconds", "seconds")
	default:
		if v, unit, ok := firstNumeric(m); ok {
			r.Value = &v
			r.Unit = unit
		}
	}

	// Preserve everything that isn't a resolved timestamp in Extra so no
	// field is lost (sleep stages[], exercise distance/cadence, etc.).
	extra := map[string]json.RawMessage{}
	for k, v := range m {
		if k == "start_time" || k == "end_time" || k == "time" || k == "session_end_time" {
			continue
		}
		extra[k] = v
	}
	if len(extra) > 0 {
		if b, err := json.Marshal(extra); err == nil {
			r.Extra = b
		}
	}
	return r, true
}

// firstTime returns the first parseable RFC3339 timestamp among keys.
func firstTime(m map[string]json.RawMessage, keys ...string) (time.Time, bool) {
	for _, k := range keys {
		raw, ok := m[k]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || s == "" {
			continue
		}
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// setScalar sets r.Value/r.Unit from a numeric field, if present.
func setScalar(r *models.HealthRecord, m map[string]json.RawMessage, field, unit string) {
	if v, ok := numField(m, field); ok {
		r.Value = &v
		r.Unit = unit
	}
}

// numField parses a numeric element field into a decimal.
func numField(m map[string]json.RawMessage, key string) (decimal.Decimal, bool) {
	raw, ok := m[key]
	if !ok {
		return decimal.Decimal{}, false
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		return decimal.Decimal{}, false
	}
	d, err := decimal.NewFromString(n.String())
	if err != nil {
		return decimal.Decimal{}, false
	}
	return d, true
}

// firstNumeric picks a representative scalar for an unmapped metric type from
// common field names, returning the field name as a best-effort unit label.
// The full element is preserved in Extra regardless.
func firstNumeric(m map[string]json.RawMessage) (decimal.Decimal, string, bool) {
	for _, k := range []string{"value", "count", "bpm", "level", "percentage",
		"grams", "calories", "meters", "liters", "millimeters_of_mercury",
		"celsius", "milligrams_per_deciliter"} {
		if v, ok := numField(m, k); ok {
			return v, k, true
		}
	}
	return decimal.Decimal{}, "", false
}
