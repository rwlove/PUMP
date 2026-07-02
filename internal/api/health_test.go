package api

import (
	"encoding/json"
	"testing"
	"time"
)

const rfc3339Sample = "2026-07-02T12:34:56Z"

func TestParseHealthEnvelope_StripsMetaAndCountsPerType(t *testing.T) {
	body := []byte(`{
		"timestamp": "2026-07-02T12:00:00Z",
		"app_version": "1.2.3",
		"steps": [
			{"start_time":"` + rfc3339Sample + `","end_time":"2026-07-02T13:34:56Z","count":5000}
		],
		"heart_rate": [
			{"start_time":"` + rfc3339Sample + `","bpm":72},
			{"start_time":"2026-07-02T12:35:56Z","bpm":74}
		]
	}`)
	recs, received, err := parseHealthEnvelope(body)
	if err != nil {
		t.Fatalf("parseHealthEnvelope: %v", err)
	}
	if received != 3 {
		t.Fatalf("received count = %d, want 3", received)
	}
	if len(recs) != 3 {
		t.Fatalf("expected 3 records, got %d", len(recs))
	}
	byType := map[string]int{}
	for _, r := range recs {
		byType[r.MetricType]++
	}
	if byType["steps"] != 1 || byType["heart_rate"] != 2 {
		t.Fatalf("wrong per-type counts: %v", byType)
	}
}

func TestParseHealthEnvelope_IgnoresScalarMetadataFields(t *testing.T) {
	// Root-level scalars other than the metadata whitelist just fail JSON
	// unmarshal to []; parser must skip them silently instead of erroring.
	body := []byte(`{"steps":[{"start_time":"` + rfc3339Sample + `","count":1}], "some_scalar":"oops"}`)
	recs, received, err := parseHealthEnvelope(body)
	if err != nil {
		t.Fatalf("parseHealthEnvelope: %v", err)
	}
	if received != 1 || len(recs) != 1 {
		t.Fatalf("received=%d recs=%d, want 1/1", received, len(recs))
	}
}

func TestParseHealthEnvelope_MalformedRoot(t *testing.T) {
	_, _, err := parseHealthEnvelope([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error on invalid JSON")
	}
}

func TestParseHealthEnvelope_DropsUntimeableRecords(t *testing.T) {
	// A record with no start_time / time / session_end_time can't be
	// deduped or displayed, so parseHealthEnvelope silently drops it while
	// still counting it in `received`.
	body := []byte(`{"steps":[
		{"start_time":"` + rfc3339Sample + `","count":100},
		{"count":999}
	]}`)
	recs, received, err := parseHealthEnvelope(body)
	if err != nil {
		t.Fatalf("parseHealthEnvelope: %v", err)
	}
	if received != 2 {
		t.Fatalf("received=%d, want 2", received)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 record kept, got %d", len(recs))
	}
}

func TestHealthRecordFromElement_StepsScalarAndExtra(t *testing.T) {
	el := json.RawMessage(`{"start_time":"` + rfc3339Sample + `","count":8123,"source_app":"fitbit"}`)
	r, ok := healthRecordFromElement("steps", el)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if r.MetricType != "steps" {
		t.Fatalf("type=%q, want steps", r.MetricType)
	}
	if r.Value == nil || r.Value.String() != "8123" {
		t.Fatalf("count didn't land in Value: %+v", r.Value)
	}
	if r.Unit != "steps" {
		t.Fatalf("unit=%q, want steps", r.Unit)
	}
	// End time falls back to start when only start is present.
	if r.EndTime == nil || !r.EndTime.Equal(r.StartTime) {
		t.Fatalf("EndTime should default to StartTime, got %v", r.EndTime)
	}
	// Non-timestamp / non-scalar fields land in Extra.
	if len(r.Extra) == 0 {
		t.Fatal("expected source_app to be preserved in Extra")
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal(r.Extra, &extra); err != nil {
		t.Fatalf("extra JSON invalid: %v", err)
	}
	if _, ok := extra["source_app"]; !ok {
		t.Fatalf("source_app dropped: %v", extra)
	}
	// Extra must NOT contain the resolved-timestamp fields.
	for _, k := range []string{"start_time", "end_time", "time", "session_end_time"} {
		if _, dup := extra[k]; dup {
			t.Fatalf("%s should be stripped from Extra", k)
		}
	}
}

func TestHealthRecordFromElement_UnmappedTypeUsesFirstNumeric(t *testing.T) {
	// A type PUMP doesn't recognise still gets a Value/Unit via firstNumeric
	// fallback so the payload isn't lost entirely.
	el := json.RawMessage(`{"start_time":"` + rfc3339Sample + `","celsius":36.9}`)
	r, ok := healthRecordFromElement("body_temperature", el)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if r.Value == nil {
		t.Fatal("expected scalar Value from firstNumeric fallback")
	}
	if r.Unit != "celsius" {
		t.Fatalf("unit=%q, want celsius", r.Unit)
	}
}

func TestHealthRecordFromElement_NoTimestamp(t *testing.T) {
	_, ok := healthRecordFromElement("steps", json.RawMessage(`{"count":1}`))
	if ok {
		t.Fatal("record with no timestamp should be dropped")
	}
}

func TestFirstTime_TriesEachKeyInOrder(t *testing.T) {
	m := map[string]json.RawMessage{
		"time":              json.RawMessage(`"2026-07-02T00:00:00Z"`),
		"session_end_time":  json.RawMessage(`"2026-07-02T01:00:00Z"`),
	}
	// start_time missing, time present — should match time.
	got, ok := firstTime(m, "start_time", "time", "session_end_time")
	if !ok {
		t.Fatal("expected match on time")
	}
	want, _ := time.Parse(time.RFC3339, "2026-07-02T00:00:00Z")
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestFirstNumeric_PicksFirstMatchingKey(t *testing.T) {
	m := map[string]json.RawMessage{
		"count":       json.RawMessage(`42`),
		"level":       json.RawMessage(`99`),
		"unrelated":   json.RawMessage(`"nope"`),
	}
	// firstNumeric walks value, count, bpm, level, ...
	v, unit, ok := firstNumeric(m)
	if !ok {
		t.Fatal("expected match")
	}
	if unit != "count" || v.String() != "42" {
		t.Fatalf("got (%q,%v), want (count,42)", unit, v.String())
	}
}
