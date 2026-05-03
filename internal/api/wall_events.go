package api

import (
	"sync"
)

// WallEventType enumerates the events broadcast over /api/wall/stream
// to wall-mounted kiosk displays.
type WallEventType string

const (
	// Wake / sleep are pushed by the operator (or, in phase 2, by
	// pump-cv when it sees the athlete enter / leave the room).
	WallEventWake  WallEventType = "wake"
	WallEventSleep WallEventType = "sleep"

	// BuildChanged fires once at server startup carrying the build SHA
	// in Data; kiosk JS compares to its loaded SHA and self-reloads on
	// mismatch — the "fast deploy → wall updates" path.
	WallEventBuildChanged WallEventType = "build_changed"
)

// WallEvent is the payload sent to SSE subscribers on /api/wall/stream.
type WallEvent struct {
	Type WallEventType `json:"type"`
	Data string        `json:"data,omitempty"`
}

type wallEventBroker struct {
	mu   sync.Mutex
	subs map[chan WallEvent]struct{}

	// Most recent build SHA — sent to every new subscriber on connect
	// so a kiosk that connects after the build_changed event still
	// learns the current SHA and can reload if its loaded SHA is older.
	lastBuildSHA string
}

var wallBroker = &wallEventBroker{subs: map[chan WallEvent]struct{}{}}

func (b *wallEventBroker) subscribe() (chan WallEvent, string, func()) {
	ch := make(chan WallEvent, 16)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	last := b.lastBuildSHA
	b.mu.Unlock()
	return ch, last, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
		close(ch)
	}
}

func (b *wallEventBroker) publish(ev WallEvent) {
	b.mu.Lock()
	if ev.Type == WallEventBuildChanged {
		b.lastBuildSHA = ev.Data
	}
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

func publishWallEvent(ev WallEvent) { wallBroker.publish(ev) }

// SetBuildSHA records the running build SHA so it can be sent to new
// SSE subscribers and emitted as a build_changed event on startup.
// Called from cmd/pump/main.go.
func SetBuildSHA(sha string) {
	if sha == "" {
		return
	}
	publishWallEvent(WallEvent{Type: WallEventBuildChanged, Data: sha})
}
