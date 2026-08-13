// Package voltra holds the runtime coordination state between the workout UI
// and the pump-voltra sidecar.
//
// The browser can see the trainer but cannot talk to it; the sidecar holds the
// BLE connection but cannot see the DOM. This package is the shared ground
// between them, and it is why armed state lives on the server at all.
//
// The model is desired-vs-actual. The browser expresses intent (arm this set,
// load it); the sidecar reports what the hardware actually did. They are kept
// separate because a load can fail — the trainer may be asleep, the workout
// inactive, the read-back mismatched — and a UI that showed intent as if it
// were fact would let an athlete lift against a weight that was never applied.
//
// State is deliberately in-memory and lost on restart. It describes the
// current moment of a workout, not history: re-arming after a pod restart is
// one click, and persisting it would mean a stale row claiming the motor is
// engaged when it plainly is not.
package voltra

import (
	"context"
	"sync"
	"time"
)

// State is the whole coordination surface, desired and actual together.
type State struct {
	// ─── desired: written by the browser ──────────────────────────────
	//
	// ArmedSetID is the set telemetry should be attributed to. Zero means
	// nothing is armed and nothing may be recorded.
	ArmedSetID int `json:"ArmedSetID"`
	// WantLoad is the athlete having pressed LOAD for the armed set.
	WantLoad bool `json:"WantLoad"`
	// WeightLb is the weight the sidecar should write, taken from the armed
	// set's row. PUMP is authoritative for this; the trainer's own reading
	// is only ever used to verify the write landed.
	WeightLb string `json:"WeightLb"`

	// ─── actual: reported by the sidecar ──────────────────────────────
	//
	// Loaded is the motor confirmed engaged at WeightLb, read back from the
	// device. Recording requires this, not WantLoad.
	Loaded bool `json:"Loaded"`
	// Error is the last load failure, surfaced to the UI so a refused load
	// is visible rather than looking like a slow one.
	Error string `json:"Error"`
	// SidecarSeen is when the sidecar last reported. A stale timestamp with
	// Loaded=true means the sidecar died holding the motor, and the device's
	// own ~8 s expiry has since released it.
	SidecarSeen time.Time `json:"SidecarSeen"`
}

// Recording reports whether a set may be recorded right now.
//
// Both halves are required. Armed alone is not enough: PUMP's weight is only
// truthful because it was written to the device and read back, and an
// armed-but-unloaded set would attribute that number to reps performed at
// whatever load the trainer happened to be holding.
func (s State) Recording() bool {
	return s.ArmedSetID != 0 && s.Loaded
}

// Stale reports whether the sidecar has gone quiet while claiming the motor is
// engaged. The device releases on its own after ~8 s without a keepalive, so
// past that the Loaded flag is a lie.
func (s State) Stale(now time.Time, after time.Duration) bool {
	return s.Loaded && !s.SidecarSeen.IsZero() && now.Sub(s.SidecarSeen) > after
}

// sidecarQuiet reports whether the sidecar has gone silent for long enough that
// its last Error no longer describes the trainer. Unlike Stale it does not
// require Loaded: an armed-but-unloaded set can carry a load error whose sidecar
// has since disconnected (or the trainer was switched off), and that dead
// complaint should expire rather than stay pinned to the row.
func (s State) sidecarQuiet(now time.Time, after time.Duration) bool {
	return s.SidecarSeen.IsZero() || now.Sub(s.SidecarSeen) > after
}

// StaleAfter is how long without a sidecar report before Loaded is disbelieved.
// Comfortably above the sidecar's report cadence, comfortably below the point
// where a human would notice the UI lying.
const StaleAfter = 20 * time.Second

var (
	mu      sync.RWMutex
	current State
	subs    = map[chan State]struct{}{}
)

const staleMsg = "sidecar stopped reporting; the trainer has released by now"

// corrected applies the staleness rule to a snapshot. Everything that hands a
// State outward goes through this, so no caller — HTTP response, SSE event, or
// direct read — can observe a claim that the motor is engaged after the
// sidecar has gone quiet.
func corrected(s State) State {
	now := time.Now()
	if s.Stale(now, StaleAfter) {
		s.Loaded = false
		s.Error = staleMsg
		return s
	}
	// A sidecar-reported error outlives its truth once the sidecar goes quiet:
	// the failure it described no longer reflects the trainer, yet it stays
	// pinned to the armed set and lingers after the trainer is switched off.
	// Drop it once the heartbeat lapses. staleMsg is exempt — it is set by this
	// very correction and carries no fresh SidecarSeen of its own.
	if s.Error != "" && s.Error != staleMsg && s.sidecarQuiet(now, StaleAfter) {
		s.Error = ""
	}
	return s
}

// Get returns a copy of the current state, with Loaded corrected to false when
// the sidecar has gone quiet.
func Get() State {
	mu.RLock()
	s := current
	mu.RUnlock()
	return corrected(s)
}

// WatchStale flips Loaded false and tells subscribers once the sidecar stops
// reporting.
//
// Correcting only on read is not enough: the browser reads /api/voltra/state
// once at init and then relies entirely on SSE. Nothing published an event
// when the sidecar simply went quiet, so a page that had been told
// "loaded — recording" kept saying so indefinitely while the trainer had
// physically released — and the athlete would perform the set believing it was
// being captured against the right load. Publishing the transition is what
// makes the UI's claim expire with the state it describes.
func WatchStale(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			now := time.Now()
			mu.RLock()
			loadedStale := current.Stale(now, StaleAfter)
			errStale := current.Error != "" && current.Error != staleMsg &&
				current.sidecarQuiet(now, StaleAfter)
			mu.RUnlock()
			if !loadedStale && !errStale {
				continue
			}
			// Correcting only on read is not enough: the browser reads state
			// once at init and then follows SSE, so a lingering error (or a
			// released motor) never reaches the page unless the transition is
			// broadcast. Clearing Loaded/Error here also makes each case a
			// one-shot — the next tick's predicate finds nothing left to do.
			mutate(func(s *State) {
				now := time.Now()
				switch {
				case s.Stale(now, StaleAfter):
					s.Loaded = false
					s.Error = staleMsg
				case s.Error != "" && s.Error != staleMsg && s.sidecarQuiet(now, StaleAfter):
					s.Error = ""
				}
			})
		}
	}
}

// Arm points recording at a set and clears any prior load. Arming never
// engages the motor — that is always a separate, explicit LOAD.
func Arm(setID int, weightLb string) State {
	return mutate(func(s *State) {
		if s.ArmedSetID != setID {
			// Moving to a different set drops the load: its weight may
			// differ, and weight is only ever changed with the motor
			// disengaged.
			s.WantLoad = false
			s.Loaded = false
			s.Error = ""
		}
		s.ArmedSetID = setID
		s.WeightLb = weightLb
	})
}

// Disarm stops recording and asks for the load to be dropped.
func Disarm() State {
	return mutate(func(s *State) {
		s.ArmedSetID = 0
		s.WantLoad = false
		s.Loaded = false
		s.WeightLb = ""
		s.Error = ""
	})
}

// SetWantLoad records the athlete pressing LOAD or UNLOAD. It expresses intent
// only; Loaded stays false until the sidecar confirms the read-back.
func SetWantLoad(want bool) State {
	return mutate(func(s *State) {
		s.WantLoad = want
		if !want {
			s.Loaded = false
		}
		s.Error = ""
	})
}

// Report records what the sidecar observed on the hardware.
func Report(loaded bool, errMsg string) State {
	return mutate(func(s *State) {
		s.Loaded = loaded
		s.Error = errMsg
		s.SidecarSeen = time.Now()
	})
}

func mutate(f func(*State)) State {
	mu.Lock()
	f(&current)
	// Broadcast and return the corrected view, never the raw struct. Arming
	// the already-armed set keeps Loaded as-is, so a dead sidecar's last
	// "loaded" would otherwise be re-published as fact by the act of tapping
	// the row again.
	s := corrected(current)
	for ch := range subs {
		select {
		case ch <- s:
		default: // drop rather than stall a slow subscriber
		}
	}
	mu.Unlock()
	return s
}

// Subscribe returns a channel of state changes and an unsubscribe func. Used
// by the sidecar to learn about load requests without polling, and by the
// browser to reflect what the hardware is actually doing.
func Subscribe() (chan State, func()) {
	ch := make(chan State, 8)
	mu.Lock()
	subs[ch] = struct{}{}
	mu.Unlock()
	return ch, func() {
		mu.Lock()
		delete(subs, ch)
		mu.Unlock()
		close(ch)
	}
}

// Reset clears everything. Tests only.
func Reset() {
	mu.Lock()
	current = State{}
	mu.Unlock()
}
