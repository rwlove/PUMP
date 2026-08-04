"""Set-boundary state machine.

Pure and clock-injected: every method takes `now` as a float monotonic
timestamp, so tests drive time directly and no test needs to sleep.

Boundary detection, in priority order:

1. **SetSummary (0xAA 0x85/0x5F)** — the device's own end-of-set record,
   carrying both the set number and the final rep count. Definitive, and the
   normal path.
2. **Idle timeout** — Progress frames stop entirely when the athlete stops;
   they are not a heartbeat. If a SetSummary is dropped (the ESPHome proxy
   discards notifications under backpressure without telling anyone), this
   still closes the set using the last Progress count.
3. **Set-number change** — a Progress frame for a new set closes the previous
   one, covering a back-to-back set whose summary went missing.

Every completed set is emitted at most once, keyed on its set number, so a
reconnect that re-reads device state cannot double-post.
"""

from __future__ import annotations

from dataclasses import dataclass

from . import telemetry
from .log import get

logger = get(__name__)

DEFAULT_IDLE_SECONDS = 30.0


@dataclass(frozen=True, slots=True)
class CompletedSet:
    set_number: int
    reps: int
    weight_lb: int
    # True when the set was closed by the idle timeout or a set-number change
    # rather than by the device's own summary. Worth logging: a run full of
    # these means summaries are being dropped in transport.
    inferred: bool


class SetTracker:
    def __init__(self, idle_seconds: float = DEFAULT_IDLE_SECONDS) -> None:
        self._idle_seconds = idle_seconds
        self._set_number: int | None = None
        self._reps = 0
        self._weight_lb = 0
        self._last_progress: float | None = None
        self._emitted: set[int] = set()

    # ─── inputs ──────────────────────────────────────────────────────────

    def note_weight(self, weight_lb: int) -> None:
        """Record the trainer's current target load.

        Snapshotted when a set opens: that is the resistance the athlete
        actually pulled against, even if they change the dial afterwards.
        """
        self._weight_lb = weight_lb

    def observe(self, event: object, now: float) -> CompletedSet | None:
        """Feed one decoded telemetry event. Returns a set if one just closed."""
        if isinstance(event, telemetry.Progress):
            return self._on_progress(event, now)
        if isinstance(event, telemetry.SetSummary):
            return self._on_summary(event)
        return None

    def tick(self, now: float) -> CompletedSet | None:
        """Advance the idle timeout. Call on a timer; telemetry may have stopped."""
        if self._set_number is None or self._last_progress is None:
            return None
        if now - self._last_progress < self._idle_seconds:
            return None
        logger.info(
            "set closed by idle timeout — no device summary arrived",
            set_number=self._set_number,
            reps=self._reps,
            idle_seconds=round(now - self._last_progress, 1),
        )
        return self._close(self._set_number, self._reps, inferred=True)

    # ─── transitions ─────────────────────────────────────────────────────

    def _on_progress(self, ev: telemetry.Progress, now: float) -> CompletedSet | None:
        closed: CompletedSet | None = None

        if self._set_number is not None and ev.set_number != self._set_number:
            # A new set started without the previous one's summary arriving.
            closed = self._close(self._set_number, self._reps, inferred=True)

        if self._set_number != ev.set_number:
            self._set_number = ev.set_number
            self._reps = 0

        # Rep counts only ever climb within a set; a lower value is a stale or
        # reordered frame, not a correction.
        self._reps = max(self._reps, ev.reps)
        self._last_progress = now
        return closed

    def _on_summary(self, ev: telemetry.SetSummary) -> CompletedSet | None:
        # The summary is authoritative even if no Progress frame was seen —
        # a whole set's worth of them can be dropped in transport.
        return self._close(ev.set_number, ev.reps, inferred=False)

    def _close(self, set_number: int, reps: int, *, inferred: bool) -> CompletedSet | None:
        self._set_number = None
        self._reps = 0
        self._last_progress = None

        if set_number in self._emitted:
            logger.debug("ignoring duplicate set", set_number=set_number)
            return None
        if reps <= 0:
            # Loading the cable without pulling produces frames but no set.
            logger.debug("ignoring zero-rep set", set_number=set_number)
            return None

        self._emitted.add(set_number)
        return CompletedSet(
            set_number=set_number,
            reps=reps,
            weight_lb=self._weight_lb,
            inferred=inferred,
        )

    def reset_workout(self) -> None:
        """Clear the emitted-set memory at the start of a new workout.

        The device restarts set numbering per workout, so without this a
        second workout's set 1 would be suppressed as a duplicate.
        """
        self._emitted.clear()
        self._set_number = None
        self._reps = 0
        self._last_progress = None
