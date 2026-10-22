package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/store"
)

// mockPlanStore is a minimal PlanStore for handler tests: it records the date
// SelectCommittedSessionOn was asked for and returns canned data.
type mockPlanStore struct {
	committed   models.PrescribedSession
	committedOK bool
	committedOn string
	feedback    []models.SessionFeedback
	programs    []models.Program
}

func (m *mockPlanStore) SelectPrograms(context.Context) ([]models.Program, error) {
	return m.programs, nil
}
func (m *mockPlanStore) InsertProgram(context.Context, models.Program) (int, error) {
	return 1, nil
}
func (m *mockPlanStore) DeleteProgram(context.Context, int) error { return nil }
func (m *mockPlanStore) InsertPrescribedSession(context.Context, models.PrescribedSession) (int, error) {
	return 1, nil
}
func (m *mockPlanStore) UpdatePrescribedSessionStatus(context.Context, int, string) (bool, error) {
	return true, nil
}
func (m *mockPlanStore) ReplacePrescribedItems(context.Context, int, []models.PrescribedItem) error {
	return nil
}
func (m *mockPlanStore) SelectSessionFeedback(context.Context) ([]models.SessionFeedback, error) {
	return m.feedback, nil
}
func (m *mockPlanStore) InsertSessionFeedback(context.Context, models.SessionFeedback) error {
	return nil
}
func (m *mockPlanStore) SelectCommittedSessionOn(_ context.Context, date string) (models.PrescribedSession, bool, error) {
	m.committedOn = date
	return m.committed, m.committedOK, nil
}
func (m *mockPlanStore) AdvanceCursor(context.Context, int) (int, bool, error) {
	return 0, true, nil
}
func (m *mockPlanStore) CompleteSession(context.Context, string, int) (int, bool, error) {
	return 0, true, nil
}
func (m *mockPlanStore) SelectSessionCompletions(context.Context) ([]models.SessionCompletion, error) {
	return nil, nil
}

var _ store.PlanStore = (*mockPlanStore)(nil)

func withPlanStore(t *testing.T, m *mockPlanStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	prev, prevKey := planStore, planIngestKey
	t.Cleanup(func() { planStore = prev; planIngestKey = prevKey })
	planStore = m
	planIngestKey = ""
}

func doGET(path string, register func(*gin.Engine)) *httptest.ResponseRecorder {
	r := gin.New()
	register(r)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func regPrescription(r *gin.Engine) { r.GET("/api/prescription", getPrescription) }
func regFeedback(r *gin.Engine)     { r.GET("/api/session-feedback", getSessionFeedback) }
func regPlanCurrent(r *gin.Engine)  { r.GET("/api/plan/current", getPlanCurrent) }

func TestGetPlanCurrent_ReturnsQueue(t *testing.T) {
	withPlanStore(t, &mockPlanStore{programs: []models.Program{{
		ID: 1, CursorPosition: 2,
		Sessions: []models.PrescribedSession{
			{ID: 10, Position: 1, Label: "Legs", Status: "committed"},
			{ID: 11, Position: 2, Label: "Chest", Status: "committed"},
		},
	}}})
	w := doGET("/api/plan/current", regPlanCurrent)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var qp models.QueuePlan
	if err := json.Unmarshal(w.Body.Bytes(), &qp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if qp.Current.Label != "Chest" || qp.NextLabel != "Legs" {
		t.Fatalf("got current=%q next=%q, want Chest/Legs(wrap)", qp.Current.Label, qp.NextLabel)
	}
}

func TestGetPlanCurrent_NoQueueIs204(t *testing.T) {
	withPlanStore(t, &mockPlanStore{programs: nil})
	if w := doGET("/api/plan/current", regPlanCurrent); w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 when no queue", w.Code)
	}
}

func TestGetPrescription_CommittedReturnsSession(t *testing.T) {
	m := &mockPlanStore{
		committed:   models.PrescribedSession{ID: 7, Label: "Back", Status: "committed", Date: "2026-10-21"},
		committedOK: true,
	}
	withPlanStore(t, m)
	w := doGET("/api/prescription?date=2026-10-21", regPrescription)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var ps models.PrescribedSession
	if err := json.Unmarshal(w.Body.Bytes(), &ps); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ps.ID != 7 || ps.Label != "Back" {
		t.Fatalf("got %+v", ps)
	}
	if m.committedOn != "2026-10-21" {
		t.Fatalf("date passed to store = %q, want 2026-10-21", m.committedOn)
	}
}

func TestGetPrescription_FreestyleIs204(t *testing.T) {
	withPlanStore(t, &mockPlanStore{committedOK: false})
	w := doGET("/api/prescription?date=2026-10-22", regPrescription)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 on a freestyle day", w.Code)
	}
}

func TestGetPrescription_BadDateIs400(t *testing.T) {
	withPlanStore(t, &mockPlanStore{})
	w := doGET("/api/prescription?date=not-a-date", regPrescription)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestGetPrescription_DefaultsToToday(t *testing.T) {
	m := &mockPlanStore{committedOK: false}
	withPlanStore(t, m)
	_ = doGET("/api/prescription", regPrescription)
	if _, err := time.Parse("2006-01-02", m.committedOn); err != nil {
		t.Fatalf("no ?date defaulted to %q, not a YYYY-MM-DD today", m.committedOn)
	}
}

func TestGetSessionFeedback_SinceFilters(t *testing.T) {
	withPlanStore(t, &mockPlanStore{feedback: []models.SessionFeedback{
		{Date: "2026-10-01"}, {Date: "2026-10-10"}, {Date: "2026-10-20"},
	}})
	w := doGET("/api/session-feedback?since=2026-10-10", regFeedback)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var out []models.SessionFeedback
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d rows, want 2 (>= since)", len(out))
	}
}

func TestGetSessionFeedback_BadSinceIs400(t *testing.T) {
	withPlanStore(t, &mockPlanStore{})
	w := doGET("/api/session-feedback?since=nope", regFeedback)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}
