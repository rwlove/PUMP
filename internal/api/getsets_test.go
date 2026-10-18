package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
)

// getSets with ?date= scopes the response to that single day and passes the
// date through to the store — this is what lets the workout page load a day
// older than its initial display window on demand.
func TestGetSets_ByDateReturnsThatDay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prev := dataStore
	t.Cleanup(func() { dataStore = prev })

	gs := &gateStore{setsOn: []models.Set{
		{ID: 1, Date: "2026-07-12", Name: "Barbell Squats", Reps: 10},
	}}
	dataStore = gs

	r := gin.New()
	r.GET("/api/sets", getSets)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sets?date=2026-07-12", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if gs.setsOnDate != "2026-07-12" {
		t.Fatalf("store queried date %q, want 2026-07-12", gs.setsOnDate)
	}
	var got []models.Set
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Barbell Squats" {
		t.Fatalf("body = %+v, want the single squat set", got)
	}
}

// A malformed ?date= is rejected before the store is touched — a bad value
// must not reach the query layer.
func TestGetSets_ByDateRejectsMalformed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prev := dataStore
	t.Cleanup(func() { dataStore = prev })

	gs := &gateStore{}
	dataStore = gs

	r := gin.New()
	r.GET("/api/sets", getSets)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sets?date=07-12-2026", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if gs.setsOnDate != "" {
		t.Fatalf("store queried with %q; a malformed date must be rejected first", gs.setsOnDate)
	}
}
