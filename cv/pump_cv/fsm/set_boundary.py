"""Set-boundary state machine.

States:
    IDLE     — no recent reps; we're not in a set
    REPPING  — reps are happening
    RESTING  — reps stopped; waiting to see if more come or if the set is done

A "set ends" event fires when the system has been in RESTING for at
least `quiet_seconds` continuously. After firing, the FSM returns to
IDLE and the next rep starts a new set.

The FSM is driven by tick() events: pass the current rep count and a
timestamp every frame (or whenever convenient). It returns events the
caller acts on.
"""

from __future__ import annotations

from collections.abc import Iterable
from dataclasses import dataclass
from enum import Enum


class SetState(str, Enum):
    IDLE = "idle"
    REPPING = "repping"
    RESTING = "resting"


@dataclass(frozen=True, slots=True)
class SetStartedEvent:
    timestamp: float


@dataclass(frozen=True, slots=True)
class RepObservedEvent:
    timestamp: float
    rep_index_in_set: int  # 1-based


@dataclass(frozen=True, slots=True)
class SetEndedEvent:
    started_at: float
    ended_at: float
    rep_count: int


SetEvent = SetStartedEvent | RepObservedEvent | SetEndedEvent


@dataclass
class SetBoundary:
    """Wraps a RepCounter's running count + a timer."""

    quiet_seconds: float = 25.0

    state: SetState = SetState.IDLE
    _set_started_at: float = 0.0
    _last_rep_at: float = 0.0
    _set_rep_count: int = 0
    _last_seen_total: int = 0

    def reset(self) -> None:
        self.state = SetState.IDLE
        self._set_started_at = 0.0
        self._last_rep_at = 0.0
        self._set_rep_count = 0
        self._last_seen_total = 0

    def tick(self, total_rep_count: int, ts: float) -> Iterable[SetEvent]:
        """Advance the FSM. Yields any events triggered by this tick."""
        events: list[SetEvent] = []

        new_reps = max(0, total_rep_count - self._last_seen_total)
        self._last_seen_total = total_rep_count

        if new_reps > 0:
            if self.state == SetState.IDLE:
                self._set_started_at = ts
                self._set_rep_count = 0
                events.append(SetStartedEvent(ts))
            self.state = SetState.REPPING
            self._last_rep_at = ts
            for _ in range(new_reps):
                self._set_rep_count += 1
                events.append(RepObservedEvent(ts, self._set_rep_count))
        else:
            # REPPING → RESTING transition: a rep just stopped happening.
            if self.state == SetState.REPPING:
                self.state = SetState.RESTING
                self._last_rep_at = self._last_rep_at or ts
            # Always re-check the close condition while RESTING — including on
            # the same tick where we just transitioned, so a long-quiet first
            # tick after the last rep can close the set immediately.
            if self.state == SetState.RESTING:
                if ts - self._last_rep_at >= self.quiet_seconds:
                    events.append(SetEndedEvent(
                        started_at=self._set_started_at,
                        ended_at=ts,
                        rep_count=self._set_rep_count,
                    ))
                    self.state = SetState.IDLE
                    self._set_rep_count = 0
                    self._set_started_at = 0.0

        return events
