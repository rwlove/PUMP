package treadmill

import (
	"testing"
	"time"
)

// sample is one scripted input: either a wattage reading (tick=false) or a
// clock-only advance (tick=true), at offset seconds from the base time.
type sample struct {
	offset int // seconds from base
	watts  float64
	tick   bool
}

func TestDetector(t *testing.T) {
	base := time.Date(2026, 6, 24, 7, 0, 0, 0, time.UTC)
	at := func(sec int) time.Time { return base.Add(time.Duration(sec) * time.Second) }

	cases := []struct {
		name      string
		det       Detector
		inputs    []sample
		wantStart int  // expected session start offset (seconds)
		wantEnd   int  // expected session end offset (seconds)
		wantNone  bool // expect no session emitted
	}{
		{
			name: "normal session closes via tick",
			det:  Detector{Threshold: 50, OffDebounce: 60 * time.Second, MinSession: 120 * time.Second},
			inputs: []sample{
				{offset: 0, watts: 0},     // idle
				{offset: 10, watts: 300},  // start
				{offset: 600, watts: 280}, // still running
				{offset: 605, watts: 0},   // stopped → belowSince=605
				{offset: 700, tick: true}, // 95s later → debounce elapsed, close
			},
			wantStart: 10,
			wantEnd:   600,
		},
		{
			name: "brief dip does not split the session",
			det:  Detector{Threshold: 50, OffDebounce: 60 * time.Second, MinSession: 120 * time.Second},
			inputs: []sample{
				{offset: 0, watts: 200},   // start
				{offset: 100, watts: 10},  // dip below (belowSince=100)
				{offset: 120, watts: 220}, // back above within debounce → resets
				{offset: 400, watts: 220}, // still running (lastAbove=400)
				{offset: 405, watts: 0},   // stop (belowSince=405)
				{offset: 500, tick: true}, // close
			},
			wantStart: 0,
			wantEnd:   400,
		},
		{
			name: "sub-minimum session is discarded",
			det:  Detector{Threshold: 50, OffDebounce: 60 * time.Second, MinSession: 120 * time.Second},
			inputs: []sample{
				{offset: 0, watts: 200},   // start
				{offset: 30, watts: 0},    // stop after 30s (belowSince=30)
				{offset: 120, tick: true}, // debounce elapsed → 30s < 120s, discard
			},
			wantNone: true,
		},
		{
			name: "session closes on a late below-threshold sample",
			det:  Detector{Threshold: 50, OffDebounce: 60 * time.Second, MinSession: 120 * time.Second},
			inputs: []sample{
				{offset: 0, watts: 200},   // start
				{offset: 300, watts: 200}, // running
				{offset: 305, watts: 0},   // belowSince=305
				{offset: 400, watts: 0},   // 95s sub-threshold → closes on this sample
			},
			wantStart: 0,
			wantEnd:   300,
		},
		{
			name: "never above threshold yields nothing",
			det:  Detector{Threshold: 50, OffDebounce: 60 * time.Second, MinSession: 120 * time.Second},
			inputs: []sample{
				{offset: 0, watts: 5},
				{offset: 100, watts: 20},
				{offset: 200, tick: true},
			},
			wantNone: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			det := tc.det
			var got []*Session
			for _, in := range tc.inputs {
				var s *Session
				if in.tick {
					s = det.Tick(at(in.offset))
				} else {
					s = det.Sample(at(in.offset), in.watts)
				}
				if s != nil {
					got = append(got, s)
				}
			}

			if tc.wantNone {
				if len(got) != 0 {
					t.Fatalf("expected no session, got %d: %+v", len(got), got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("expected exactly 1 session, got %d: %+v", len(got), got)
			}
			if !got[0].Start.Equal(at(tc.wantStart)) {
				t.Errorf("start = %v, want %v", got[0].Start, at(tc.wantStart))
			}
			if !got[0].End.Equal(at(tc.wantEnd)) {
				t.Errorf("end = %v, want %v", got[0].End, at(tc.wantEnd))
			}
		})
	}
}
