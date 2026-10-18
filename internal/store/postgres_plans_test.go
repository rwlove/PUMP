package store

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/rwlove/PUMP/internal/models"
)

// cleanPlanTables clears the prescription layer so each test starts empty.
// testStore only truncates sets/exercises/weight; deleting programs cascades to
// prescribed_sessions and prescribed_items.
func cleanPlanTables(t *testing.T, s *PostgresStore) {
	t.Helper()
	ctx := context.Background()
	for _, stmt := range []string{"DELETE FROM programs", "DELETE FROM session_feedback"} {
		if _, err := s.Pool().Exec(ctx, stmt); err != nil {
			t.Fatalf("clean plan tables (%s): %v", stmt, err)
		}
	}
}

func intp(v int) *int         { return &v }
func f64p(v float64) *float64 { return &v }

// A program's sessions and their items round-trip through SelectPrograms, with
// exercise and alternate names resolved and nullable targets preserved.
func TestPrograms_InsertSelectNested(t *testing.T) {
	s := testStore(t)
	cleanPlanTables(t, s)
	ctx := context.Background()

	rowID := mustInsertEx(t, s, "Seated Cable Row", "Back", "#111")
	altID := mustInsertEx(t, s, "Chest-Supported Row", "Back", "#112")

	progID, err := s.InsertProgram(ctx, models.Program{
		Name: "Block A", StartDate: "2026-10-20", LengthWeeks: 4, DeloadWeek: 4,
		CreatedBy: "arnold", Notes: "elbow-aware",
	})
	if err != nil {
		t.Fatalf("InsertProgram: %v", err)
	}
	sessID, err := s.InsertPrescribedSession(ctx, models.PrescribedSession{
		ProgramID: progID, Date: "2026-10-21", Label: "Back", Status: "committed",
	})
	if err != nil {
		t.Fatalf("InsertPrescribedSession: %v", err)
	}

	tw := decimal.NewFromInt(120)
	items := []models.PrescribedItem{
		{ // concrete pull with a substitution + flags
			ExerciseID: rowID, TargetRepsLow: 10, TargetRepsHigh: 15,
			TargetRIR: intp(2), TargetWeight: &tw,
			Rationale: "grip-offloaded; hold load", Flags: "grip-offload,elbow-gated",
			AltExerciseID: &altID,
		},
		{ // provisional item: no RIR, no weight, no alt
			ExerciseID: rowID, TargetRepsLow: 8, TargetRepsHigh: 12,
			Rationale: "provisional next exposure",
		},
	}
	if err := s.ReplacePrescribedItems(ctx, sessID, items); err != nil {
		t.Fatalf("ReplacePrescribedItems: %v", err)
	}

	progs, err := s.SelectPrograms(ctx)
	if err != nil {
		t.Fatalf("SelectPrograms: %v", err)
	}
	if len(progs) != 1 {
		t.Fatalf("got %d programs, want 1", len(progs))
	}
	p := progs[0]
	if p.CreatedBy != "arnold" || p.DeloadWeek != 4 || p.StartDate != "2026-10-20" {
		t.Errorf("program fields wrong: %+v", p)
	}
	if len(p.Sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(p.Sessions))
	}
	sess := p.Sessions[0]
	if sess.Status != "committed" || sess.Label != "Back" {
		t.Errorf("session fields wrong: %+v", sess)
	}
	if len(sess.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(sess.Items))
	}

	// Item 0: concrete, resolved names, nullable targets set.
	it0 := sess.Items[0]
	if it0.Position != 0 || it0.ExerciseName != "Seated Cable Row" || it0.Color != "#111" {
		t.Errorf("item0 resolution wrong: %+v", it0)
	}
	if it0.TargetWeight == nil || !it0.TargetWeight.Equal(decimal.NewFromInt(120)) {
		t.Errorf("item0 TargetWeight = %v, want 120", it0.TargetWeight)
	}
	if it0.TargetRIR == nil || *it0.TargetRIR != 2 {
		t.Errorf("item0 TargetRIR = %v, want 2", it0.TargetRIR)
	}
	if it0.AltExerciseID == nil || *it0.AltExerciseID != altID || it0.AltExerciseName != "Chest-Supported Row" {
		t.Errorf("item0 alternate wrong: id=%v name=%q", it0.AltExerciseID, it0.AltExerciseName)
	}
	if it0.Flags != "grip-offload,elbow-gated" {
		t.Errorf("item0 Flags = %q", it0.Flags)
	}

	// Item 1: provisional, nullable targets stay nil.
	it1 := sess.Items[1]
	if it1.Position != 1 {
		t.Errorf("item1 Position = %d, want 1", it1.Position)
	}
	if it1.TargetWeight != nil || it1.TargetRIR != nil || it1.AltExerciseID != nil {
		t.Errorf("item1 should have nil targets/alt: %+v", it1)
	}
	if it1.AltExerciseName != "" {
		t.Errorf("item1 AltExerciseName = %q, want empty", it1.AltExerciseName)
	}
}

// ReplacePrescribedItems is a full swap: the second call's items fully replace
// the first's, with positions reassigned from slice order.
func TestReplacePrescribedItems_Replaces(t *testing.T) {
	s := testStore(t)
	cleanPlanTables(t, s)
	ctx := context.Background()

	exID := mustInsertEx(t, s, "Lat Pulldown", "Back", "#111")
	progID, _ := s.InsertProgram(ctx, models.Program{Name: "B", StartDate: "2026-10-20"})
	sessID, _ := s.InsertPrescribedSession(ctx, models.PrescribedSession{ProgramID: progID, Date: "2026-10-21"})

	if err := s.ReplacePrescribedItems(ctx, sessID, []models.PrescribedItem{
		{ExerciseID: exID, TargetRepsLow: 10, TargetRepsHigh: 12},
		{ExerciseID: exID, TargetRepsLow: 8, TargetRepsHigh: 10},
	}); err != nil {
		t.Fatalf("first replace: %v", err)
	}
	if err := s.ReplacePrescribedItems(ctx, sessID, []models.PrescribedItem{
		{ExerciseID: exID, TargetRepsLow: 6, TargetRepsHigh: 8},
	}); err != nil {
		t.Fatalf("second replace: %v", err)
	}

	progs, _ := s.SelectPrograms(ctx)
	items := progs[0].Sessions[0].Items
	if len(items) != 1 {
		t.Fatalf("after replace got %d items, want 1", len(items))
	}
	if items[0].Position != 0 || items[0].TargetRepsLow != 6 {
		t.Errorf("replaced item wrong: %+v", items[0])
	}
}

// Session status toggles provisional -> committed; an unknown id reports not-found.
func TestUpdatePrescribedSessionStatus(t *testing.T) {
	s := testStore(t)
	cleanPlanTables(t, s)
	ctx := context.Background()

	progID, _ := s.InsertProgram(ctx, models.Program{Name: "C", StartDate: "2026-10-20"})
	sessID, _ := s.InsertPrescribedSession(ctx, models.PrescribedSession{ProgramID: progID, Date: "2026-10-21"})

	found, err := s.UpdatePrescribedSessionStatus(ctx, sessID, "committed")
	if err != nil || !found {
		t.Fatalf("update existing: found=%v err=%v", found, err)
	}
	progs, _ := s.SelectPrograms(ctx)
	if got := progs[0].Sessions[0].Status; got != "committed" {
		t.Errorf("status = %q, want committed", got)
	}

	found, err = s.UpdatePrescribedSessionStatus(ctx, 999999, "committed")
	if err != nil {
		t.Fatalf("update unknown: %v", err)
	}
	if found {
		t.Errorf("unknown id reported found=true")
	}
}

// Feedback round-trips (RPE + 0-3 elbow flag, both nullable), and the DB CHECK
// constraints reject out-of-range values.
func TestSessionFeedback_InsertSelectAndRanges(t *testing.T) {
	s := testStore(t)
	cleanPlanTables(t, s)
	ctx := context.Background()

	if err := s.InsertSessionFeedback(ctx, models.SessionFeedback{
		Date: "2026-10-21", RPE: f64p(8.5), ElbowFlag: intp(1), Note: "mild next day",
	}); err != nil {
		t.Fatalf("insert full feedback: %v", err)
	}
	// Partial: RPE only, elbow unreported.
	if err := s.InsertSessionFeedback(ctx, models.SessionFeedback{
		Date: "2026-10-22", RPE: f64p(7),
	}); err != nil {
		t.Fatalf("insert partial feedback: %v", err)
	}

	fb, err := s.SelectSessionFeedback(ctx)
	if err != nil {
		t.Fatalf("SelectSessionFeedback: %v", err)
	}
	if len(fb) != 2 {
		t.Fatalf("got %d feedback rows, want 2", len(fb))
	}
	if fb[0].RPE == nil || *fb[0].RPE != 8.5 || fb[0].ElbowFlag == nil || *fb[0].ElbowFlag != 1 {
		t.Errorf("row0 wrong: %+v", fb[0])
	}
	if fb[1].ElbowFlag != nil {
		t.Errorf("row1 elbow should be nil: %+v", fb[1])
	}

	// CHECK constraints: RPE out of 6-10 and elbow out of 0-3 must fail.
	if err := s.InsertSessionFeedback(ctx, models.SessionFeedback{Date: "2026-10-23", RPE: f64p(3)}); err == nil {
		t.Error("expected RPE=3 to violate the CHECK constraint, got nil")
	}
	if err := s.InsertSessionFeedback(ctx, models.SessionFeedback{Date: "2026-10-23", ElbowFlag: intp(5)}); err == nil {
		t.Error("expected elbow_flag=5 to violate the CHECK constraint, got nil")
	}
}

// SelectCommittedSessionOn returns only a committed session on the exact date,
// with items resolved; a provisional session on that date, and any session on
// another date, do not surface.
func TestSelectCommittedSessionOn(t *testing.T) {
	s := testStore(t)
	cleanPlanTables(t, s)
	ctx := context.Background()

	exID := mustInsertEx(t, s, "Lat Pulldown", "Back", "#123")
	progID, err := s.InsertProgram(ctx, models.Program{
		Name: "Block B", StartDate: "2026-10-20", LengthWeeks: 4,
	})
	if err != nil {
		t.Fatalf("InsertProgram: %v", err)
	}

	// Committed session on the target date, with one concrete item.
	cID, err := s.InsertPrescribedSession(ctx, models.PrescribedSession{
		ProgramID: progID, Date: "2026-10-21", Label: "Back", Status: "committed",
	})
	if err != nil {
		t.Fatalf("InsertPrescribedSession (committed): %v", err)
	}
	tw := decimal.NewFromInt(120)
	if err := s.ReplacePrescribedItems(ctx, cID, []models.PrescribedItem{
		{ExerciseID: exID, TargetRepsLow: 10, TargetRepsHigh: 12, TargetRIR: intp(2),
			TargetWeight: &tw, Rationale: "hold load", Flags: "grip-offload"},
	}); err != nil {
		t.Fatalf("ReplacePrescribedItems: %v", err)
	}

	// A provisional session on the SAME date must not win.
	if _, err := s.InsertPrescribedSession(ctx, models.PrescribedSession{
		ProgramID: progID, Date: "2026-10-21", Label: "Back (prov)", Status: "provisional",
	}); err != nil {
		t.Fatalf("InsertPrescribedSession (provisional): %v", err)
	}

	ps, ok, err := s.SelectCommittedSessionOn(ctx, "2026-10-21")
	if err != nil {
		t.Fatalf("SelectCommittedSessionOn: %v", err)
	}
	if !ok {
		t.Fatalf("expected a committed session on 2026-10-21")
	}
	if ps.ID != cID || ps.Status != "committed" || ps.Label != "Back" {
		t.Fatalf("wrong session returned: %+v", ps)
	}
	if len(ps.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(ps.Items))
	}
	it := ps.Items[0]
	if it.ExerciseName != "Lat Pulldown" || it.TargetWeight == nil || !it.TargetWeight.Equal(tw) || it.Flags != "grip-offload" {
		t.Fatalf("item not resolved: %+v", it)
	}

	// A date with no committed session is freestyle (ok=false).
	if _, ok, err := s.SelectCommittedSessionOn(ctx, "2026-10-25"); err != nil || ok {
		t.Fatalf("freestyle date: ok=%v err=%v, want ok=false", ok, err)
	}
}
