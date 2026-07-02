package treadmill

import "testing"

// parseWatts is called on every MQTT message from zwave-js-ui — it accepts
// either the JSON envelope zwave-js-ui publishes by default or a bare number
// when "publish values as raw" is enabled on the driver.

func TestParseWatts_JSONEnvelope(t *testing.T) {
	watts, ok := parseWatts([]byte(`{"value": 123.4, "time": 1751462400000}`))
	if !ok {
		t.Fatal("expected ok=true for envelope form")
	}
	if watts != 123.4 {
		t.Fatalf("watts=%v, want 123.4", watts)
	}
}

func TestParseWatts_JSONEnvelopeNullValue(t *testing.T) {
	// zwave-js-ui sometimes publishes a null value when the device is stale;
	// the parser must not fall through to a "raw number" interpretation and
	// end up returning 0 with ok=true. It should return ok=false.
	_, ok := parseWatts([]byte(`{"value": null}`))
	if ok {
		t.Fatal("null value should return ok=false, not synthesise 0 watts")
	}
}

func TestParseWatts_JSONEnvelopeMissingValue(t *testing.T) {
	// Envelope without a value key — also a stale reading; must not report
	// 0 watts (which would immediately debounce any running session).
	_, ok := parseWatts([]byte(`{"time": 1}`))
	if ok {
		t.Fatal("envelope missing value key should return ok=false")
	}
}

func TestParseWatts_RawNumber(t *testing.T) {
	watts, ok := parseWatts([]byte("87.5"))
	if !ok {
		t.Fatal("expected ok=true for raw number form")
	}
	if watts != 87.5 {
		t.Fatalf("watts=%v, want 87.5", watts)
	}
}

func TestParseWatts_RawNumberWithSurroundingWhitespace(t *testing.T) {
	watts, ok := parseWatts([]byte(" 42.0\n"))
	if !ok {
		t.Fatal("expected ok=true — whitespace should be trimmed")
	}
	if watts != 42.0 {
		t.Fatalf("watts=%v, want 42", watts)
	}
}

func TestParseWatts_Garbage(t *testing.T) {
	if _, ok := parseWatts([]byte(`not a number`)); ok {
		t.Fatal("garbage payload should return ok=false")
	}
	if _, ok := parseWatts([]byte(``)); ok {
		t.Fatal("empty payload should return ok=false")
	}
}
