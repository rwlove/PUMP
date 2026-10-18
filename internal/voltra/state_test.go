package voltra

import (
	"context"
	"testing"
	"time"
)

func TestRecordingRequiresBothArmedAndLoaded(t *testing.T) {
	cases := []struct {
		name   string
		set    int
		loaded bool
		want   bool
	}{
		{"nothing", 0, false, false},
		{"armed only", 7, false, false},
		{"loaded only", 0, true, false},
		{"armed and loaded", 7, true, true},
	}
	for _, c := range cases {
		got := State{ArmedSetID: c.set, Loaded: c.loaded}.Recording()
		if got != c.want {
			t.Errorf("%s: Recording()=%v, want %v", c.name, got, c.want)
		}
	}
}

// Arming must never engage the motor. Loading is always separate and explicit.
func TestArmDoesNotLoad(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	s := Arm(4, "50")
	if s.WantLoad || s.Loaded {
		t.Fatalf("arming engaged the motor: WantLoad=%v Loaded=%v", s.WantLoad, s.Loaded)
	}
	if s.Recording() {
		t.Error("armed-but-unloaded must not permit recording")
	}
}

// Weight is only ever changed with the motor disengaged, so moving to a
// different set has to drop the load rather than silently carry it over at the
// previous set's weight.
func TestArmingADifferentSetDropsTheLoad(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Arm(4, "50")
	SetWantLoad(true)
	Report(true, "")
	if !Get().Recording() {
		t.Fatal("precondition: should be recording")
	}

	s := Arm(5, "60")
	if s.Loaded || s.WantLoad {
		t.Errorf("load survived a set change: Loaded=%v WantLoad=%v", s.Loaded, s.WantLoad)
	}
	if s.WeightLb != "60" {
		t.Errorf("weight = %q, want the new set's 60", s.WeightLb)
	}
}

// Re-arming the same set (e.g. a UI refresh re-asserting state) must not drop
// a load the athlete is mid-set on.
func TestReArmingTheSameSetKeepsTheLoad(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Arm(4, "50")
	SetWantLoad(true)
	Report(true, "")

	s := Arm(4, "50")
	if !s.Loaded {
		t.Error("re-arming the same set dropped the load")
	}
}

func TestDisarmClearsEverything(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Arm(4, "50")
	SetWantLoad(true)
	Report(true, "")

	s := Disarm()
	if s.ArmedSetID != 0 || s.Loaded || s.WantLoad || s.Recording() {
		t.Errorf("disarm left state behind: %+v", s)
	}
}

// Intent must not masquerade as fact: pressing LOAD cannot by itself make the
// UI show engaged, or an athlete could lift against a weight never applied.
func TestWantLoadDoesNotImplyLoaded(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Arm(4, "50")
	s := SetWantLoad(true)
	if s.Loaded {
		t.Error("pressing LOAD reported the motor as engaged before the sidecar confirmed")
	}
	if s.Recording() {
		t.Error("recording enabled on intent alone")
	}
}

func TestReportEnablesRecording(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Arm(4, "50")
	SetWantLoad(true)
	s := Report(true, "")
	if !s.Recording() {
		t.Error("confirmed load did not enable recording")
	}
}

// A sidecar that dies holding the motor stops re-asserting the keepalive, and
// the device releases after ~8 s. Continuing to report Loaded past that would
// be a lie, and would keep recording enabled against a slack cable.
func TestStaleSidecarReportIsDisbelieved(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Arm(4, "50")
	Report(true, "")

	fresh := Get()
	if !fresh.Loaded {
		t.Fatal("precondition: should be loaded")
	}

	mu.Lock()
	current.SidecarSeen = time.Now().Add(-StaleAfter - time.Second)
	mu.Unlock()

	s := Get()
	if s.Loaded {
		t.Error("stale sidecar report still claims the motor is engaged")
	}
	if s.Recording() {
		t.Error("stale state still permits recording")
	}
	if s.Error == "" {
		t.Error("stale state gave no reason")
	}
}

// An armed-but-unloaded set can carry a load error the sidecar reported just
// before it went away (trainer switched off, sidecar stopped). Stale() only
// covers loaded sets, so nothing used to expire this error — it stayed pinned
// to the row indefinitely. Once the heartbeat lapses the complaint is history.
func TestStaleSidecarErrorIsCleared(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Arm(4, "50")
	Report(false, "Characteristic a0180000-… write failed")

	if Get().Error == "" {
		t.Fatal("precondition: a fresh sidecar error should be visible")
	}

	mu.Lock()
	current.SidecarSeen = time.Now().Add(-StaleAfter - time.Second)
	mu.Unlock()

	if s := Get(); s.Error != "" {
		t.Errorf("stale sidecar error still shown: %q", s.Error)
	}
}

// The cleared error must reach the page: the browser follows SSE after init, so
// the transition has to be broadcast, not merely corrected on the next read.
func TestStaleErrorClearIsBroadcast(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Arm(4, "50")
	Report(false, "Characteristic a0180000-… write failed")

	ch, unsub := Subscribe()
	defer unsub()

	mu.Lock()
	current.SidecarSeen = time.Now().Add(-StaleAfter - time.Second)
	mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go WatchStale(ctx, 10*time.Millisecond)

	select {
	case s := <-ch:
		if s.Error != "" {
			t.Errorf("broadcast still carries the dead error: %q", s.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stale error clear was never broadcast; the row would keep showing it")
	}
}

func TestSubscribersSeeChanges(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	ch, unsub := Subscribe()
	defer unsub()

	Arm(9, "40")
	select {
	case s := <-ch:
		if s.ArmedSetID != 9 {
			t.Errorf("got ArmedSetID=%d, want 9", s.ArmedSetID)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber never saw the arm")
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	ch, unsub := Subscribe()
	unsub()
	Arm(1, "10") // must not panic on a closed channel
	if _, open := <-ch; open {
		t.Error("channel still delivering after unsubscribe")
	}
}

// Correcting only on read left the browser believing the motor was engaged
// after the sidecar died: the page reads state once at init and then follows
// SSE, and nothing published the transition.
func TestStalenessIsBroadcastToSubscribers(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Arm(4, "50")
	SetWantLoad(true)
	Report(true, "")

	ch, unsub := Subscribe()
	defer unsub()

	mu.Lock()
	current.SidecarSeen = time.Now().Add(-StaleAfter - time.Second)
	mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go WatchStale(ctx, 10*time.Millisecond)

	select {
	case s := <-ch:
		if s.Loaded {
			t.Error("broadcast still claims the motor is engaged")
		}
		if s.Recording() {
			t.Error("broadcast still permits recording")
		}
		if s.Error == "" {
			t.Error("broadcast gave no reason")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("staleness was never broadcast; the UI would keep saying 'loaded'")
	}
}

// Re-arming the already-armed set must not republish a dead sidecar's last
// "loaded" as if it were current.
func TestReArmingDoesNotRepublishAStaleLoad(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Arm(4, "50")
	SetWantLoad(true)
	Report(true, "")

	mu.Lock()
	current.SidecarSeen = time.Now().Add(-StaleAfter - time.Second)
	mu.Unlock()

	if s := Arm(4, "50"); s.Loaded {
		t.Error("re-arming republished a stale load as engaged")
	}
}

// RecordReps attributes a finished set's reps to the armed set only when a set
// is armed and loaded — the same gate as Recording — and bumps the completion
// sequence so the browser can apply it.
func TestRecordRepsAttributesToArmedWhenRecording(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Arm(11, "40")
	Report(true, "") // armed and loaded → recording

	s := RecordReps(12)
	if s.CompletedSetID != 11 || s.CompletedReps != 12 {
		t.Fatalf("CompletedSetID=%d CompletedReps=%d, want 11/12", s.CompletedSetID, s.CompletedReps)
	}
	if s.CompletedSeq != 1 {
		t.Fatalf("CompletedSeq=%d, want 1", s.CompletedSeq)
	}
}

// Reps reported with nothing armed, or armed-but-unloaded, must not be
// attributed to any set — otherwise a count would land on a set performed at a
// load that was never confirmed.
func TestRecordRepsIgnoredWhenNotRecording(t *testing.T) {
	t.Cleanup(Reset)

	Reset()
	Arm(5, "30") // armed, never loaded
	if s := RecordReps(8); s.CompletedSeq != 0 || s.CompletedSetID != 0 {
		t.Fatalf("armed-but-unloaded recorded reps: seq=%d setID=%d", s.CompletedSeq, s.CompletedSetID)
	}

	Reset()
	Report(true, "") // loaded, nothing armed
	if s := RecordReps(8); s.CompletedSeq != 0 {
		t.Fatalf("recorded reps with nothing armed: seq=%d", s.CompletedSeq)
	}
}

// A non-positive count is not a set and must not bump the sequence.
func TestRecordRepsIgnoresZeroReps(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Arm(3, "20")
	Report(true, "")
	if s := RecordReps(0); s.CompletedSeq != 0 {
		t.Fatalf("zero reps bumped the sequence: seq=%d", s.CompletedSeq)
	}
}

// Each completion advances the sequence monotonically, so the browser can tell
// a new finished set from a re-delivered state and apply each exactly once.
func TestRecordRepsSequenceIsMonotonic(t *testing.T) {
	t.Cleanup(Reset)
	Reset()
	Arm(1, "10")
	Report(true, "")

	first := RecordReps(5).CompletedSeq
	Arm(2, "10") // advance to the next set
	Report(true, "")
	second := RecordReps(7).CompletedSeq

	if first != 1 || second != 2 {
		t.Fatalf("sequence not monotonic: first=%d second=%d, want 1,2", first, second)
	}
}
