"""Unit tests for the set-boundary FSM."""

from __future__ import annotations

from pump_cv.fsm import SetBoundary, SetState
from pump_cv.fsm.set_boundary import (
    RepObservedEvent,
    SetEndedEvent,
    SetStartedEvent,
)


def test_idle_until_first_rep():
    fsm = SetBoundary(quiet_seconds=10.0)
    events = list(fsm.tick(0, 0.0))
    assert events == []
    assert fsm.state is SetState.IDLE


def test_set_started_on_first_rep():
    fsm = SetBoundary(quiet_seconds=10.0)
    events = list(fsm.tick(1, 1.0))
    assert any(isinstance(e, SetStartedEvent) for e in events)
    assert any(isinstance(e, RepObservedEvent) for e in events)
    assert fsm.state is SetState.REPPING


def test_quiet_period_closes_set():
    fsm = SetBoundary(quiet_seconds=5.0)
    list(fsm.tick(1, 1.0))
    list(fsm.tick(2, 2.0))
    list(fsm.tick(3, 3.0))
    # No new reps, but ticks keep coming.
    list(fsm.tick(3, 4.0))
    list(fsm.tick(3, 6.0))
    events = list(fsm.tick(3, 9.0))   # 6 seconds since last rep > quiet_seconds
    ended = [e for e in events if isinstance(e, SetEndedEvent)]
    assert len(ended) == 1
    assert ended[0].rep_count == 3
    assert fsm.state is SetState.IDLE


def test_new_set_after_quiet_period():
    fsm = SetBoundary(quiet_seconds=5.0)
    # First set: 2 reps
    list(fsm.tick(1, 1.0))
    list(fsm.tick(2, 2.0))
    list(fsm.tick(2, 8.0))  # close set
    assert fsm.state is SetState.IDLE

    # Second set: 4 reps
    events = list(fsm.tick(3, 30.0))
    assert any(isinstance(e, SetStartedEvent) for e in events)
    list(fsm.tick(4, 31.0))
    list(fsm.tick(5, 32.0))
    list(fsm.tick(6, 33.0))
    events = list(fsm.tick(6, 40.0))
    ended = [e for e in events if isinstance(e, SetEndedEvent)]
    assert len(ended) == 1
    assert ended[0].rep_count == 4


def test_rep_count_decreases_does_not_break_fsm():
    """If a rep counter is reset mid-stream, total_rep_count drops; the
    FSM should treat that as "no new reps" and not crash."""
    fsm = SetBoundary(quiet_seconds=5.0)
    list(fsm.tick(3, 1.0))
    events = list(fsm.tick(0, 2.0))
    # Just no new reps and no crash.
    assert all(not isinstance(e, RepObservedEvent) for e in events)
