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
//
// Each subscriber records the client id it registered with, so an event can be
// withheld from the very client that caused it. Without that, a browser gets
// its own write echoed back and — because the event is published before the
// HTTP response is written — the echo arrives before the client has learned
// the new row's id, so it cannot recognise the row as its own and renders it a
// second time.
type setEventBroker struct {
	mu   sync.Mutex
	subs map[chan SetEvent]string // channel -> owning client id ("" = anonymous)
}

var setBroker = &setEventBroker{subs: map[chan SetEvent]string{}}

func (b *setEventBroker) subscribe(clientID string) (chan SetEvent, func()) {
	ch := make(chan SetEvent, 32)
	b.mu.Lock()
	b.subs[ch] = clientID
	b.mu.Unlock()
	return ch, func() {
		b.mu.Lock()
		delete(b.subs, ch)
		b.mu.Unlock()
		close(ch)
	}
}

// publish fans an event out to every subscriber except the one identified by
// origin. An empty origin (a write with no client id — pump-cv, pump-voltra,
// curl) suppresses nothing, so those still reach every browser.
func (b *setEventBroker) publish(ev SetEvent, origin string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch, owner := range b.subs {
		if origin != "" && owner == origin {
			continue // don't echo a client's own write back at it
		}
		select {
		case ch <- ev:
		default:
			// Drop rather than stall; slow subscribers fall behind quietly.
		}
	}
}

func publishSetEvent(ev SetEvent, origin string) { setBroker.publish(ev, origin) }
