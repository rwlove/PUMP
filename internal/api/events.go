package api

import (
	"sync"

	"github.com/rwlove/PUMP/internal/models"
)

// SetEventType enumerates the lifecycle events broadcast over /api/sets/stream.
type SetEventType string

const (
	SetEventAdd    SetEventType = "add"
	SetEventUpdate SetEventType = "update"
	SetEventDelete SetEventType = "delete"
	SetEventBulk   SetEventType = "bulk"
)

// SetEvent is the payload sent to SSE subscribers.
//
//   - add / update: ID, Date, and Set populated
//   - delete:       ID and Date populated, Set nil
//   - bulk:         only Date populated (the day was bulk-replaced; refetch)
type SetEvent struct {
	Type SetEventType `json:"type"`
	ID   int          `json:"id,omitempty"`
	Date string       `json:"date,omitempty"`
	Set  *models.Set  `json:"set,omitempty"`
}

// setEventBroker fans events out to all current SSE subscribers. Subscribers
// that can't keep up have events dropped rather than blocking the publisher.
type setEventBroker struct {
	mu   sync.Mutex
	subs map[chan SetEvent]struct{}
}

var setBroker = &setEventBroker{subs: map[chan SetEvent]struct{}{}}

func (b *setEventBroker) subscribe() (chan SetEvent, func()) {
	ch := make(chan SetEvent, 32)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
		close(ch)
	}
}

func (b *setEventBroker) publish(ev SetEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// Drop rather than stall; slow subscribers fall behind quietly.
		}
	}
}

func publishSetEvent(ev SetEvent) { setBroker.publish(ev) }
