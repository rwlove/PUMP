"""Ties telemetry to PUMP: track sets and report finished-set reps.

Deliberately independent of the BLE layer — it consumes decoded 0xAA payloads
and a weight source. The sidecar does not write set rows: on a completed set it
reports the rep count for the armed set, and the browser writes it into that
set's own row.
"""

from __future__ import annotations

import asyncio
from datetime import datetime

from . import healthd, telemetry
from .log import get
from .naming import ExerciseNamer
from .pump_client import AutoLogDisabled, PumpClient
from .session import CompletedSet, SetTracker

logger = get(__name__)


def today_str() -> str:
    return datetime.now().strftime("%Y-%m-%d")


class Runner:
    def __init__(self, pump: PumpClient, namer: ExerciseNamer, tracker: SetTracker,
                 controller=None):
        self._pump = pump
        self._namer = namer
        self._tracker = tracker
        # Phase 2: when present, nothing is recorded unless a set is armed AND
        # the motor is confirmed engaged at PUMP's weight.
        self._controller = controller

    def attach_controller(self, controller) -> None:
        """Bind (or clear) the phase-2 gate. Cleared on session teardown so a
        stale controller cannot keep recording enabled after the BLE link is
        gone."""
        self._controller = controller

    # ─── naming inputs ───────────────────────────────────────────────────

    async def refresh_exercises(self) -> None:
        exercises = await self._pump.list_exercises()
        self._namer.set_exercises(exercises)
        healthd.record_flagged_exercises(self._namer.flagged_count)

    async def seed_anchor(self) -> None:
        self._namer.seed_from_sets(await self._pump.list_sets(), today_str())

    async def watch_sets(self) -> None:
        """Follow the SSE feed so the anchor tracks what the athlete taps."""
        while True:
            try:
                async for event in self._pump.stream_set_events():
                    if event.type in ("add", "update"):
                        self._namer.observe_set(event.set, event.date, today_str())
                    elif event.type == "bulk":
                        # The whole day was rewritten; re-derive from scratch.
                        await self.seed_anchor()
            except asyncio.CancelledError:
                raise
            except Exception as e:
                # Several httpx transport exceptions stringify to "", which
                # made this log useless while diagnosing a read-timeout loop.
                # Always carry the type.
                logger.warning(
                    "sse stream dropped; reconnecting",
                    error=str(e) or "<no message>",
                    error_type=type(e).__name__,
                )
                await asyncio.sleep(5)

    async def refresh_exercises_forever(self, interval_s: float) -> None:
        while True:
            await asyncio.sleep(interval_s)
            try:
                await self.refresh_exercises()
            except asyncio.CancelledError:
                raise
            except Exception as e:
                logger.warning("exercise refresh failed", error=str(e))

    # ─── the write path ──────────────────────────────────────────────────

    async def report(self, done: CompletedSet) -> None:
        if self._controller is not None and not self._controller.recording():
            # Not armed, or armed but the motor was never confirmed engaged.
            # Reporting anyway would attribute reps to a set that was never
            # loaded at PUMP's weight.
            logger.info(
                "set not recorded — no armed and loaded Voltra set",
                reps=done.reps,
                device_set=done.set_number,
            )
            return

        # The browser owns the set row; we only report the rep count for the
        # armed set, and PUMP attributes it there. Nothing is written to the DB
        # by the sidecar — the trainer updates the UI's set, it does not create
        # a parallel one.
        try:
            recorded = await self._pump.report_reps(done.reps)
        except AutoLogDisabled as e:
            # Not retryable: the operator has the toggle off on purpose.
            logger.error("PUMP refused the rep report — VOLTRA_AUTOLOG is off", error=str(e))
            healthd.record_set_failed(str(e))
            return
        except Exception as e:
            logger.error("rep report failed", error=str(e), reps=done.reps)
            healthd.record_set_failed(str(e))
            return

        if not recorded:
            return

        healthd.record_set_posted(pending=False, inferred=done.inferred)
        logger.info(
            "reps reported",
            weight_lb=done.weight_lb,
            reps=done.reps,
            device_set=done.set_number,
            inferred=done.inferred,
        )

    async def feed(self, payload: bytes, now: float) -> None:
        """Feed one raw 0xAA payload through decode → tracker → PUMP."""
        event = telemetry.decode(payload)
        if event is None:
            return
        if (done := self._tracker.observe(event, now)) is not None:
            await self.report(done)

    async def tick_forever(self, interval_s: float = 5.0) -> None:
        """Drive the idle-timeout fallback; telemetry may simply stop."""
        loop = asyncio.get_running_loop()
        while True:
            await asyncio.sleep(interval_s)
            if (done := self._tracker.tick(loop.time())) is not None:
                await self.report(done)
