// Package treadmill turns the treadmill smart-plug's Z-Wave power feed
// (published to MQTT by zwave-js-ui) into PUMP cardio entries — without going
// through Home Assistant. A threshold + off-debounce state machine detects a
// workout from the wattage stream, and each completed session is written as an
// "exercise" HealthRecord that surfaces on the Stats cardio view.
package treadmill

import "time"

// Session is one detected treadmill workout: a contiguous interval the plug
// drew at or above the in-use wattage threshold.
type Session struct {
	Start time.Time
	End   time.Time
}

// Duration is the wall-clock length of the session.
func (s Session) Duration() time.Duration { return s.End.Sub(s.Start) }

// Detector converts a time-ordered stream of (time, watts) samples into
// completed Sessions. It is pure and has no I/O, so it is exercised directly in
// tests: the MQTT consumer feeds live samples via Sample and drives wall-clock
// expiry via Tick.
//
// A session opens when watts first reaches Threshold and closes once watts has
// stayed below Threshold continuously for OffDebounce (a brief dip does not
// split a workout). The closed session's End is the last at/above-threshold
// sample, so the trailing idle/debounce time is not counted. Sessions shorter
// than MinSession are discarded (console standby, brief tests).
//
// Detector is not safe for concurrent use; the consumer serializes access.
type Detector struct {
	Threshold   float64       // watts at/above which the treadmill is "in use"
	OffDebounce time.Duration // sustained sub-threshold time before a session closes
	MinSession  time.Duration // sessions shorter than this are discarded

	inUse      bool
	start      time.Time // session start (first at/above sample)
	lastAbove  time.Time // most recent at/above sample in the open session
	belowSince time.Time // first sub-threshold sample since lastAbove (zero while above)
}

// Sample processes one wattage reading at time t. It returns a non-nil Session
// when this reading closes a qualifying workout (only possible on a
// below-threshold reading that lands at/after the debounce window).
func (d *Detector) Sample(t time.Time, watts float64) *Session {
	if watts >= d.Threshold {
		if !d.inUse {
			d.inUse = true
			d.start = t
		}
		d.lastAbove = t
		d.belowSince = time.Time{}
		return nil
	}
	// Below threshold.
	if !d.inUse {
		return nil
	}
	if d.belowSince.IsZero() {
		d.belowSince = t
	}
	return d.maybeClose(t)
}

// Tick advances wall-clock time without a new reading. It is needed because a
// change-reporting plug may emit a single below-threshold sample (watts→0) and
// then go silent, so the debounce window must be evaluated against the clock,
// not the next message. Returns a qualifying Session if one just closed.
func (d *Detector) Tick(now time.Time) *Session {
	if !d.inUse || d.belowSince.IsZero() {
		return nil
	}
	return d.maybeClose(now)
}

// maybeClose closes the open session if the sub-threshold debounce has elapsed
// as of now, returning it only when it meets MinSession.
func (d *Detector) maybeClose(now time.Time) *Session {
	if now.Sub(d.belowSince) < d.OffDebounce {
		return nil
	}
	sess := Session{Start: d.start, End: d.lastAbove}
	d.inUse = false
	d.belowSince = time.Time{}
	if sess.Duration() < d.MinSession {
		return nil
	}
	return &sess
}
