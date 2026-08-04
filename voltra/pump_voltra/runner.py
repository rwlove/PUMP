"""Ties telemetry to PUMP: track sets, name them, post them.

Deliberately independent of the BLE layer — it consumes decoded 0xAA payloads
and a weight source, which is what lets `--replay` drive the exact same code
path from a captured file with no trainer present.
"""

from __future__ import annotations

import asyncio
import contextlib
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
    def __init__(self, pump: PumpClient, namer: ExerciseNamer, tracker: SetTracker):
        self._pump = pump
        self._namer = namer
        self._tracker = tracker

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
                logger.warning("sse stream dropped; reconnecting", error=str(e))
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

    async def post(self, done: CompletedSet) -> None:
        today = today_str()
        anchor, pending = self._namer.resolve(today)

        payload = {
            "Date": today,
            "Name": anchor.name,
            "Color": anchor.color,
            "WorkoutColor": anchor.workout_color,
            "Weight": str(done.weight_lb),
            "Reps": done.reps,
            "Source": "voltra",
            "Confidence": 1.0,
            "Pending": pending,
            "Note": "Voltra — confirm the exercise." if pending else "",
        }

        try:
            set_id = await self._pump.post_set(payload)
        except AutoLogDisabled as e:
            # Not retryable: the operator has the toggle off on purpose.
            logger.error("PUMP refused the write — VOLTRA_AUTOLOG is off", error=str(e))
            healthd.record_set_failed(str(e))
            return
        except Exception as e:
            logger.error("set write failed", error=str(e), reps=done.reps)
            healthd.record_set_failed(str(e))
            return

        healthd.record_set_posted(pending=pending, inferred=done.inferred)
        logger.info(
            "set logged",
            id=set_id,
            exercise=anchor.name,
            weight_lb=done.weight_lb,
            reps=done.reps,
            device_set=done.set_number,
            pending=pending,
            inferred=done.inferred,
        )

    async def feed(self, payload: bytes, now: float) -> None:
        """Feed one raw 0xAA payload through decode → tracker → PUMP."""
        event = telemetry.decode(payload)
        if event is None:
            return
        if (done := self._tracker.observe(event, now)) is not None:
            await self.post(done)

    async def tick_forever(self, interval_s: float = 5.0) -> None:
        """Drive the idle-timeout fallback; telemetry may simply stop."""
        loop = asyncio.get_running_loop()
        while True:
            await asyncio.sleep(interval_s)
            if (done := self._tracker.tick(loop.time())) is not None:
                await self.post(done)


async def replay(path: str, pump: PumpClient, namer: ExerciseNamer, weight_lb: int) -> int:
    """Replay a captured telemetry file into a live PUMP.

    The point of this is end-to-end verification with no trainer: it exercises
    the real decode, the real set tracker, the real naming resolver and real
    HTTP writes. Timestamps come from the capture, so it runs instantly.
    """
    tracker = SetTracker()
    tracker.note_weight(weight_lb)
    runner = Runner(pump, namer, tracker)

    await runner.refresh_exercises()
    await runner.seed_anchor()

    posted_before = healthd.state().sets_posted
    for line in open(path):
        line = line.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split("\t")
        elapsed, payload_hex = float(parts[0]), parts[-1]
        with contextlib.suppress(ValueError):
            await runner.feed(bytes.fromhex(payload_hex), elapsed)
    return healthd.state().sets_posted - posted_before
