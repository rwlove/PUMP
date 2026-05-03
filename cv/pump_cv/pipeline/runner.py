"""Pipeline runner.

Wires a single PoseSource → athlete picker → rep counter → set FSM →
PUMP API. Phase 1 keeps it deliberately small:

  - one camera (multi-cam fusion is a later slice)
  - one hardcoded exercise (the one the operator told us about) — phase 2
    introduces real classification

Each completed set becomes one POST /api/sets call. While reps are
happening the runner emits no API traffic; the set is committed only
when the set-boundary FSM closes the set. This avoids creating-then-
patching a row across many reps.
"""

from __future__ import annotations

import datetime as dt
from collections.abc import AsyncIterator
from dataclasses import dataclass

from .. import log
from ..fsm import RepCounter, SetBoundary, keypoint_angle
from ..fsm.set_boundary import RepObservedEvent, SetEndedEvent, SetStartedEvent
from ..pose.types import Pose
from ..pump_client import PumpClient
from ..tracking import pick_athlete

logger = log.get(__name__)


@dataclass(frozen=True, slots=True)
class ExerciseSpec:
    """Identifies the joint angle that drives the rep counter for one
    exercise. The three keypoint indices are passed straight to
    keypoint_angle() each frame.

    `name` must match the exercise's PUMP name exactly so the auto-log
    write maps to the right exercise.
    """

    name: str
    a_idx: int  # e.g. hip
    b_idx: int  # the joint vertex (e.g. knee)
    c_idx: int  # e.g. ankle


class PipelineRunner:
    def __init__(
        self,
        pump: PumpClient,
        exercise: ExerciseSpec,
        rep_amplitude_deg: float = 25.0,
        rep_min_period_s: float = 0.6,
        rep_smoothing_window: int = 9,
        set_quiet_seconds: float = 25.0,
        confidence_threshold: float = 0.75,
    ):
        self._pump = pump
        self._exercise = exercise
        self._counter = RepCounter(
            min_amplitude_deg=rep_amplitude_deg,
            min_period_s=rep_min_period_s,
            smoothing_window=rep_smoothing_window,
        )
        self._fsm = SetBoundary(quiet_seconds=set_quiet_seconds)
        self._confidence_threshold = confidence_threshold

    async def run(self, pose_stream: AsyncIterator[list[Pose]]) -> None:
        """Consume the pose stream until it ends."""
        async for poses in pose_stream:
            athlete = pick_athlete(poses)
            now = poses[0].timestamp if poses else 0.0
            if athlete is not None:
                angle = keypoint_angle(
                    athlete,
                    self._exercise.a_idx,
                    self._exercise.b_idx,
                    self._exercise.c_idx,
                )
                if angle is not None:
                    self._counter.push(angle, athlete.timestamp)

            for ev in self._fsm.tick(self._counter.count, now):
                await self._handle_event(ev)

    async def _handle_event(self, ev) -> None:
        match ev:
            case SetStartedEvent():
                logger.info("set started",
                            exercise=self._exercise.name, ts=ev.timestamp)
            case RepObservedEvent():
                logger.debug("rep", n=ev.rep_index_in_set)
            case SetEndedEvent():
                # Compute a self-confidence: longer sets and crisp peak
                # detection imply higher confidence. For now we treat any
                # set with >= 3 reps as confident, fewer as pending.
                confident = ev.rep_count >= 3
                pending = not confident
                payload = {
                    "Date": _today(),
                    "Name": self._exercise.name,
                    "Weight": "0",   # phase 1: weight detection is a separate slice
                    "Reps": ev.rep_count,
                    "Source": "cv",
                    "Confidence": 0.95 if confident else 0.55,
                    "Pending": pending,
                }
                logger.info("set ended → POST /api/sets",
                            exercise=self._exercise.name,
                            reps=ev.rep_count,
                            pending=pending)
                try:
                    set_id = await self._pump.post_set(payload)
                    logger.info("set written", id=set_id)
                except Exception as e:
                    logger.error("set write failed", error=str(e))
                # Reset the rep counter so the next set starts at zero.
                self._counter.reset()


def _today() -> str:
    return dt.date.today().isoformat()
