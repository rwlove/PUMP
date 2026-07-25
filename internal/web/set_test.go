package web

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
)

// replaceCall records one BulkReplaceSetsByDate invocation.
type replaceCall struct {
	date string
	sets []models.Set
}

// recordingStore captures BulkReplaceSetsByDate so the handler's decision to
// call (or skip) the replace is observable. Everything else comes from
// fakeStore.
type recordingStore struct {
	fakeStore
	mu    sync.Mutex
	calls []replaceCall
}

func (r *recordingStore) BulkReplaceSetsByDate(_ context.Context, date string, sets []models.Set) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, replaceCall{date: date, sets: sets})
	return nil
}

func postSets(t *testing.T, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/set/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r := gin.New()
	r.POST("/set/", setHandler)
	r.ServeHTTP(w, req)
	return w
}

// Deleting the final set for a date leaves the autosave form with a date but
// no name fields. That must clear the day, not no-op — the bug that stranded
// one set on every day the user tried to empty.
func TestSetHandler_EmptySetListClearsTheDay(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rs := &recordingStore{}
	defer withFakes(t, rs, models.Conf{})()

	w := postSets(t, url.Values{"date": {"2026-07-24"}})

	if w.Code != 200 {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if len(rs.calls) != 1 {
		t.Fatalf("expected 1 BulkReplaceSetsByDate call, got %d — the day was never cleared", len(rs.calls))
	}
	if rs.calls[0].date != "2026-07-24" {
		t.Errorf("date=%q, want 2026-07-24", rs.calls[0].date)
	}
	if len(rs.calls[0].sets) != 0 {
		t.Errorf("sets=%d, want 0", len(rs.calls[0].sets))
	}
}

// A POST with no date has no replace target; it must be rejected rather than
// clearing an arbitrary day.
func TestSetHandler_MissingDateIsRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for name, form := range map[string]url.Values{
		"no date field": {"name": {"Squat"}, "weight": {"225"}, "reps": {"5"}},
		"empty date":    {"date": {""}},
	} {
		t.Run(name, func(t *testing.T) {
			rs := &recordingStore{}
			defer withFakes(t, rs, models.Conf{})()

			w := postSets(t, form)

			if w.Code != 400 {
				t.Errorf("status=%d, want 400", w.Code)
			}
			rs.mu.Lock()
			defer rs.mu.Unlock()
			if len(rs.calls) != 0 {
				t.Errorf("store was touched %d times, want 0", len(rs.calls))
			}
		})
	}
}

// The normal path still round-trips values into the replace.
func TestSetHandler_SavesSets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rs := &recordingStore{}
	defer withFakes(t, rs, models.Conf{})()

	w := postSets(t, url.Values{
		"date":          {"2026-07-24"},
		"name":          {"Squat", "Bench"},
		"weight":        {"225", "165"},
		"reps":          {"5", "6"},
		"workout_color": {"#3eca2b", "#3eca2b"},
		"note":          {"", "top set"},
	})

	if w.Code != 200 {
		t.Fatalf("status=%d, want 200", w.Code)
	}
	rs.mu.Lock()
	defer rs.mu.Unlock()
	if len(rs.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(rs.calls))
	}
	sets := rs.calls[0].sets
	if len(sets) != 2 {
		t.Fatalf("sets=%d, want 2", len(sets))
	}
	if sets[0].Name != "Squat" || sets[0].Reps != 5 || sets[0].Weight.String() != "225" {
		t.Errorf("set[0]=%+v, want Squat 225x5", sets[0])
	}
	if sets[1].Name != "Bench" || sets[1].Note != "top set" {
		t.Errorf("set[1]=%+v, want Bench with note", sets[1])
	}
	if sets[0].Date != "2026-07-24" {
		t.Errorf("set[0].Date=%q, want 2026-07-24", sets[0].Date)
	}
}
