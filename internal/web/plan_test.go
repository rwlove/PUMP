package web

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
	"github.com/rwlove/PUMP/internal/store"
)

// webPlanMock records feedback inserts and serves a canned committed session.
type webPlanMock struct {
	inserted    []models.SessionFeedback
	committed   models.PrescribedSession
	committedOK bool
	advancedID  int
	advanceNext int
	advanceOK   bool
}

func (m *webPlanMock) SelectPrograms(context.Context) ([]models.Program, error) { return nil, nil }
func (m *webPlanMock) InsertProgram(context.Context, models.Program) (int, error) {
	return 1, nil
}
func (m *webPlanMock) DeleteProgram(context.Context, int) error { return nil }
func (m *webPlanMock) InsertPrescribedSession(context.Context, models.PrescribedSession) (int, error) {
	return 1, nil
}
func (m *webPlanMock) UpdatePrescribedSessionStatus(context.Context, int, string) (bool, error) {
	return true, nil
}
func (m *webPlanMock) ReplacePrescribedItems(context.Context, int, []models.PrescribedItem) error {
	return nil
}
func (m *webPlanMock) SelectSessionFeedback(context.Context) ([]models.SessionFeedback, error) {
	return nil, nil
}
func (m *webPlanMock) InsertSessionFeedback(_ context.Context, f models.SessionFeedback) error {
	m.inserted = append(m.inserted, f)
	return nil
}
func (m *webPlanMock) SelectCommittedSessionOn(context.Context, string) (models.PrescribedSession, bool, error) {
	return m.committed, m.committedOK, nil
}
func (m *webPlanMock) AdvanceCursor(_ context.Context, id int) (int, bool, error) {
	m.advancedID = id
	return m.advanceNext, m.advanceOK, nil
}

var _ store.PlanStore = (*webPlanMock)(nil)

func postFeedback(t *testing.T, form url.Values) (*httptest.ResponseRecorder, *webPlanMock) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	m := &webPlanMock{}
	prev := planStore
	t.Cleanup(func() { planStore = prev })
	planStore = m

	r := gin.New()
	r.POST("/feedback/", feedbackHandler)
	req := httptest.NewRequest(http.MethodPost, "/feedback/", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w, m
}

func TestFeedback_RPEAndSorenessBecomeTwoDatedRows(t *testing.T) {
	w, m := postFeedback(t, url.Values{
		"rpe":           {"8.5"},
		"rpe_date":      {"2026-10-21"},
		"soreness":      {"2"},
		"soreness_date": {"2026-10-20"},
	})
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if len(m.inserted) != 2 {
		t.Fatalf("inserted %d rows, want 2", len(m.inserted))
	}
	// RPE row on the session date; soreness row on the prior date.
	var rpeRow, soreRow *models.SessionFeedback
	for i := range m.inserted {
		if m.inserted[i].RPE != nil {
			rpeRow = &m.inserted[i]
		}
		if m.inserted[i].ElbowFlag != nil {
			soreRow = &m.inserted[i]
		}
	}
	if rpeRow == nil || *rpeRow.RPE != 8.5 || rpeRow.Date != "2026-10-21" {
		t.Fatalf("rpe row wrong: %+v", rpeRow)
	}
	if soreRow == nil || *soreRow.ElbowFlag != 2 || soreRow.Date != "2026-10-20" {
		t.Fatalf("soreness row wrong: %+v", soreRow)
	}
}

func TestFeedback_OutOfRangeRejected(t *testing.T) {
	// RPE below the 6-10 band.
	if w, m := postFeedback(t, url.Values{"rpe": {"5"}}); w.Code != http.StatusBadRequest || len(m.inserted) != 0 {
		t.Fatalf("rpe=5: status=%d inserted=%d, want 400/0", w.Code, len(m.inserted))
	}
	// Soreness above the 0-3 band.
	if w, m := postFeedback(t, url.Values{"soreness": {"4"}}); w.Code != http.StatusBadRequest || len(m.inserted) != 0 {
		t.Fatalf("soreness=4: status=%d inserted=%d, want 400/0", w.Code, len(m.inserted))
	}
}

func TestFeedback_EmptySubmitIsNoOp(t *testing.T) {
	w, m := postFeedback(t, url.Values{})
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", w.Code)
	}
	if len(m.inserted) != 0 {
		t.Fatalf("inserted %d rows on empty submit, want 0", len(m.inserted))
	}
}

func TestFeedback_DefaultsDates(t *testing.T) {
	// No explicit dates: RPE defaults today, soreness defaults yesterday.
	_, m := postFeedback(t, url.Values{"rpe": {"7"}, "soreness": {"1"}})
	if len(m.inserted) != 2 {
		t.Fatalf("inserted %d, want 2", len(m.inserted))
	}
	var rpeDate, soreDate string
	for _, f := range m.inserted {
		if f.RPE != nil {
			rpeDate = f.Date
		}
		if f.ElbowFlag != nil {
			soreDate = f.Date
		}
	}
	if rpeDate == "" || soreDate == "" || rpeDate <= soreDate {
		t.Fatalf("expected rpe date (today) after soreness date (yesterday): rpe=%q sore=%q", rpeDate, soreDate)
	}
}

func TestCommittedSessionToday(t *testing.T) {
	gin.SetMode(gin.TestMode)
	prev := planStore
	t.Cleanup(func() { planStore = prev })

	// nil store → nil (freestyle fallback), never a panic.
	planStore = nil
	if got := committedSessionToday(testCtx(), "t", "2026-10-21"); got != nil {
		t.Fatalf("nil planStore should yield nil, got %+v", got)
	}

	// present committed session → returned.
	planStore = &webPlanMock{committed: models.PrescribedSession{ID: 9, Label: "Back"}, committedOK: true}
	if got := committedSessionToday(testCtx(), "t", "2026-10-21"); got == nil || got.ID != 9 {
		t.Fatalf("expected session id 9, got %+v", got)
	}

	// freestyle day → nil.
	planStore = &webPlanMock{committedOK: false}
	if got := committedSessionToday(testCtx(), "t", "2026-10-25"); got != nil {
		t.Fatalf("freestyle should yield nil, got %+v", got)
	}
}

func testCtx() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	return c
}

func TestAdvanceHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	m := &webPlanMock{advanceNext: 2, advanceOK: true}
	prev := planStore
	t.Cleanup(func() { planStore = prev })
	planStore = m

	r := gin.New()
	r.POST("/plan/advance/", advanceHandler)

	post := func(form url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/plan/advance/", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// Valid completed session advances and returns the new cursor position.
	if w := post(url.Values{"session_id": {"7"}}); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", w.Code, w.Body.String())
	} else if m.advancedID != 7 || !strings.Contains(w.Body.String(), "2") {
		t.Fatalf("advanced id=%d body=%s, want id 7 / next_position 2", m.advancedID, w.Body.String())
	}
	// Missing session_id → 400.
	if w := post(url.Values{}); w.Code != http.StatusBadRequest {
		t.Fatalf("empty session_id status = %d, want 400", w.Code)
	}
	// Unknown session (ok=false) → 404.
	m.advanceOK = false
	if w := post(url.Values{"session_id": {"99"}}); w.Code != http.StatusNotFound {
		t.Fatalf("unknown session status = %d, want 404", w.Code)
	}
}

// TestIndexTemplateRendersQueue parses every template with the real FuncMap and
// renders index.html in both queued and freestyle states. The plan header is
// filled client-side from window._queue, so the queued render carries the queue
// data (serialized into the script) plus the "Arnold's Routine" button, the day
// picker, and the Mark-complete control. A syntax error or a bad field/func
// reference fails here instead of in the browser.
func TestIndexTemplateRendersQueue(t *testing.T) {
	tmpl, err := template.New("").Funcs(templateFuncs()).ParseFS(templFS, "templates/*")
	if err != nil {
		t.Fatalf("ParseFS: %v", err)
	}

	rir := 2
	tw := decimal.NewFromInt(120)
	back := models.PrescribedSession{
		ID: 7, Label: "Back", Status: "committed", Position: 1,
		Items: []models.PrescribedItem{
			{ExerciseName: "Lat Pulldown", Color: "#123", TargetSets: 4, TargetRepsLow: 10, TargetRepsHigh: 12,
				TargetRIR: &rir, TargetWeight: &tw, Rationale: "hold load",
				Flags: "grip-offload,elbow-gated"},
			// provisional pull: no RIR/weight → "load TBD"; has a substitution.
			{ExerciseName: "Cable Row", Color: "#456", TargetSets: 3, TargetRepsLow: 10, TargetRepsHigh: 10,
				AltExerciseName: "Chest-Supported Row"},
		},
	}
	legs := models.PrescribedSession{ID: 8, Label: "Legs", Status: "committed", Position: 2}
	queued := models.GuiData{
		ServerDate: "2026-10-21",
		Queue: &models.QueuePlan{
			ProgramID: 1, CursorPosition: 1,
			Current:   back,
			Sessions:  []models.PrescribedSession{back, legs},
			NextLabel: "Legs",
		},
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "index.html", queued); err != nil {
		t.Fatalf("execute index.html (queued): %v", err)
	}
	out := buf.String()
	// Queue data serializes into window._queue (html/template JSON-encodes in a
	// script context); the actions + plan-header container are present.
	for _, want := range []string{"window._queue", "Lat Pulldown", "grip-offload",
		"Chest-Supported Row", "Legs", "planHeader", "Mark complete", "Session feedback"} {
		if !strings.Contains(out, want) {
			t.Errorf("queued render missing %q", want)
		}
	}
	if !strings.Contains(out, "Start routine") {
		t.Error("queued render should still show the Start routine picker")
	}
	if !strings.Contains(out, "Arnold&#39;s Routine") && !strings.Contains(out, "Arnold's Routine") {
		t.Error("queued render should show the Arnold's Routine button")
	}

	var buf2 bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf2, "index.html", models.GuiData{ServerDate: "2026-10-21"}); err != nil {
		t.Fatalf("execute index.html (freestyle): %v", err)
	}
	free := buf2.String()
	if !strings.Contains(free, "Start routine") {
		t.Error("freestyle render should show the Start routine picker")
	}
	if strings.Contains(free, "Arnold's Routine") || strings.Contains(free, "Arnold&#39;s Routine") {
		t.Error("freestyle render should not show the Arnold's Routine button")
	}
}
