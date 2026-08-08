package voltra

import (
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
