package models

import "testing"

func qsess(id, pos int, label, status string) PrescribedSession {
	return PrescribedSession{ID: id, Position: pos, Label: label, Status: status}
}

func TestBuildQueuePlan_CursorAndNext(t *testing.T) {
	progs := []Program{{
		ID: 1, CursorPosition: 2,
		Sessions: []PrescribedSession{
			qsess(10, 1, "Legs", "committed"),
			qsess(11, 2, "Chest", "committed"),
			qsess(12, 3, "Back", "committed"),
		},
	}}
	qp, ok := BuildQueuePlan(progs)
	if !ok {
		t.Fatal("expected a queue")
	}
	if qp.Current.ID != 11 || qp.Current.Label != "Chest" {
		t.Fatalf("current = %+v, want Chest(11)", qp.Current)
	}
	if qp.NextLabel != "Back" {
		t.Fatalf("next = %q, want Back", qp.NextLabel)
	}
	if len(qp.Sessions) != 3 {
		t.Fatalf("rotation len = %d, want 3", len(qp.Sessions))
	}
}

func TestBuildQueuePlan_WrapsAtEnd(t *testing.T) {
	progs := []Program{{
		ID: 1, CursorPosition: 3,
		Sessions: []PrescribedSession{
			qsess(10, 1, "Legs", "committed"),
			qsess(11, 2, "Chest", "committed"),
			qsess(12, 3, "Back", "committed"),
		},
	}}
	qp, _ := BuildQueuePlan(progs)
	if qp.Current.Label != "Back" {
		t.Fatalf("current = %q, want Back", qp.Current.Label)
	}
	if qp.NextLabel != "Legs" {
		t.Fatalf("next = %q, want Legs (wrap)", qp.NextLabel)
	}
}

func TestBuildQueuePlan_CursorPastEndWrapsToFirst(t *testing.T) {
	progs := []Program{{
		ID: 1, CursorPosition: 99,
		Sessions: []PrescribedSession{
			qsess(10, 1, "Legs", "committed"),
			qsess(11, 2, "Chest", "committed"),
		},
	}}
	qp, ok := BuildQueuePlan(progs)
	if !ok || qp.Current.Label != "Legs" {
		t.Fatalf("cursor past end should wrap to first; got ok=%v cur=%+v", ok, qp)
	}
}

func TestBuildQueuePlan_OnlyCommittedInRotation(t *testing.T) {
	progs := []Program{{
		ID: 1, CursorPosition: 1,
		Sessions: []PrescribedSession{
			qsess(10, 1, "Legs", "committed"),
			qsess(11, 2, "Chest", "provisional"),
		},
	}}
	qp, ok := BuildQueuePlan(progs)
	if !ok || len(qp.Sessions) != 1 || qp.NextLabel != "Legs" {
		t.Fatalf("provisional should be excluded; got ok=%v sessions=%d next=%q", ok, len(qp.Sessions), qp.NextLabel)
	}
}

func TestBuildQueuePlan_PicksLatestProgram(t *testing.T) {
	progs := []Program{
		{ID: 1, CursorPosition: 1, Sessions: []PrescribedSession{qsess(1, 1, "Old", "committed")}},
		{ID: 5, CursorPosition: 1, Sessions: []PrescribedSession{qsess(9, 1, "New", "committed")}},
	}
	qp, ok := BuildQueuePlan(progs)
	if !ok || qp.ProgramID != 5 || qp.Current.Label != "New" {
		t.Fatalf("should pick latest program (id 5); got %+v", qp)
	}
}

func TestBuildQueuePlan_Empty(t *testing.T) {
	if _, ok := BuildQueuePlan(nil); ok {
		t.Fatal("no programs → no queue")
	}
	if _, ok := BuildQueuePlan([]Program{{ID: 1}}); ok {
		t.Fatal("program with no committed sessions → no queue")
	}
}
