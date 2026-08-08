package api

import "testing"

// drain reports whether an event is waiting on ch, without blocking.
func drain(ch chan SetEvent) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// The bug this pins down: a browser POSTs a set, the server publishes the add
// event *before* writing the HTTP response, so the echo arrives before the
// client has learned the new row's id. The client then can't recognise the row
// as its own and renders it a second time — two DOM entries aliasing one row,
// which is why deleting one appeared to delete both.
func TestBrokerDoesNotEchoToTheOriginatingClient(t *testing.T) {
	b := &setEventBroker{subs: map[chan SetEvent]string{}}
	mine, unsubA := b.subscribe("client-A")
	theirs, unsubB := b.subscribe("client-B")
	defer unsubA()
	defer unsubB()

	b.publish(SetEvent{Type: SetEventAdd, ID: 1}, "client-A")

	if drain(mine) {
		t.Error("client-A received the echo of its own write — this is the double-add bug")
	}
	if !drain(theirs) {
		t.Error("client-B missed an event it did not originate")
	}
}

// Sidecars (pump-cv, pump-voltra) and curl send no client id. Their writes
// must reach every browser, or auto-logged sets would never appear live.
func TestBrokerFansOutWritesWithNoOrigin(t *testing.T) {
	b := &setEventBroker{subs: map[chan SetEvent]string{}}
	a, unsubA := b.subscribe("client-A")
	anon, unsubAnon := b.subscribe("")
	defer unsubA()
	defer unsubAnon()

	b.publish(SetEvent{Type: SetEventAdd, ID: 2}, "")

	if !drain(a) {
		t.Error("identified subscriber missed a sidecar write")
	}
	if !drain(anon) {
		t.Error("anonymous subscriber missed a sidecar write")
	}
}

// An anonymous subscriber must never be suppressed: empty owner is "unknown",
// not "same as an empty origin".
func TestBrokerNeverSuppressesAnonymousSubscribers(t *testing.T) {
	b := &setEventBroker{subs: map[chan SetEvent]string{}}
	anon, unsub := b.subscribe("")
	defer unsub()

	b.publish(SetEvent{Type: SetEventAdd, ID: 3}, "")
	if !drain(anon) {
		t.Error("anonymous subscriber suppressed by an empty origin")
	}
}

// Two tabs are distinct clients and must still see each other.
func TestBrokerSuppressesOnlyTheMatchingClient(t *testing.T) {
	b := &setEventBroker{subs: map[chan SetEvent]string{}}
	tab1, u1 := b.subscribe("tab-1")
	tab2, u2 := b.subscribe("tab-2")
	defer u1()
	defer u2()

	b.publish(SetEvent{Type: SetEventDelete, ID: 4}, "tab-1")

	if drain(tab1) {
		t.Error("originating tab received its own delete echo")
	}
	if !drain(tab2) {
		t.Error("the other tab missed a delete it should render")
	}
}

// All four event types go through the same publish path; suppression must not
// be add-only, since delete and update echoes cause the same aliasing.
func TestBrokerSuppressionCoversEveryEventType(t *testing.T) {
	for _, typ := range []SetEventType{SetEventAdd, SetEventUpdate, SetEventDelete, SetEventBulk} {
		b := &setEventBroker{subs: map[chan SetEvent]string{}}
		own, unsub := b.subscribe("me")
		b.publish(SetEvent{Type: typ, ID: 9}, "me")
		if drain(own) {
			t.Errorf("%s echoed back to its originator", typ)
		}
		unsub()
	}
}
