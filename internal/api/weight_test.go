package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// A scale reading must record who sent it, taken from the request itself.
// Provenance fields smuggled into the JSON body are overwritten, so a client
// cannot misattribute its own reading.
func TestPostWeight_StampsProvenanceFromRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prevStore := dataStore
	t.Cleanup(func() { dataStore = prevStore })
	gs := &gateStore{}
	dataStore = gs

	r := gin.New()
	r.POST("/api/weight", postWeight)
	body := `{"Date":"2026-11-24","RecordedAt":"2026-11-24T14:45:31-05:00","Weight":256.8000043,
		"Source":"spoofed","UserAgent":"spoofed","ClientIP":"6.6.6.6","RawWeight":"1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/weight", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "scale-gateway-test")
	req.Header.Set("X-Forwarded-For", "192.0.2.59")
	req.RemoteAddr = "198.51.100.2:40000"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", w.Code, w.Body.String())
	}
	if len(gs.weights) != 1 {
		t.Fatalf("InsertW calls = %d, want 1", len(gs.weights))
	}
	got := gs.weights[0]
	if got.Source != "api" {
		t.Errorf("Source = %q, want api", got.Source)
	}
	if got.UserAgent != "scale-gateway-test" {
		t.Errorf("UserAgent = %q, want the request's User-Agent", got.UserAgent)
	}
	if got.ClientIP != "192.0.2.59" {
		t.Errorf("ClientIP = %q, want the X-Forwarded-For origin", got.ClientIP)
	}
	if got.RawWeight != "256.8000043" {
		t.Errorf("RawWeight = %q, want the unrounded posted value", got.RawWeight)
	}
}
