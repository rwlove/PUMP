"""Pipeline runner.

Wires a single PoseSource → athlete picker → rep counter → set FSM →
classifier → weight detector → PUMP API.

How a set's lifecycle works in this runner:

  1. Each frame: pick the athlete, push the configured joint angle into
     the rep counter, and append the pose + most recent BGR frame to
     in-memory buffers.
  2. Set FSM tracks reps and decides when a set has closed (quiet
     window after the last rep).
  3. On set close, classify the captured pose buffer against any loaded
     prototypes; the winning name overrides the default exercise. Then
     run the plate-color detector on the latest captured frame to
     estimate the loaded weight. Both gracefully degrade: no
     prototypes → keep the default name; no frame → weight = 0.
  4. POST /api/sets with the resulting (name, weight, reps,
     confidence, pending) payload.

Design choices worth noting:

  - The rep counter runs on a single hardcoded "primary joint" (the one
     specified in the default ExerciseSpec) so the FSM has something to
     drive set boundaries with. If the classifier identifies a
     different exercise, the new exercise's joint mapping is looked up
     and reps are recomputed from the buffered pose history.
  - Pending vs confident: a set is `pending=true` when EITHER the
     classifier confidence is below threshold OR the weight detector
     confidence is below threshold OR the rep count is suspiciously low
     (< 3). The athlete confirms via the PUMP UI (Phase 0 work).
"""

from __future__ import annotations

import datetime as dt
from collections.abc import AsyncIterator
from dataclasses import dataclass
from pathlib import Path

import numpy as np

from .. import log
from ..classify import (
    ExercisePrototype,
    classify_window,
    pose_sequence_to_features,
)
from ..clipper import ClipBuffer
from ..fsm import RepCounter, SetBoundary, keypoint_angle
from ..fsm.set_boundary import RepObservedEvent, SetEndedEvent, SetStartedEvent
from ..pose.types import FrameAndPoses, Pose
from ..pump_client import PumpClient
from ..snapshot import save_snapshot
from ..tracking import pick_athlete
from ..weight import estimate_barbell_load

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
        default_exercise: ExerciseSpec,
        prototypes: list[ExercisePrototype] | None = None,
        exercise_lookup=None,  # callable: name → ExerciseSpec | None
        bar_weight_lb: float = 45.0,
        rep_amplitude_deg: float = 25.0,
        rep_min_period_s: float = 0.6,
        rep_smoothing_window: int = 9,
        set_quiet_seconds: float = 25.0,
        confidence_threshold: float = 0.75,
        snapshot_dir: Path | None = None,
        clips_dir: Path | None = None,
        clip_capacity_seconds: float = 8.0,
        wake_after_present_seconds: float = 1.0,
        sleep_after_absent_seconds: float = 600.0,
        on_set_committed=None,  # callable(pending: bool) -> None — for healthd metrics
        on_set_failed=None,     # callable() -> None
    ):
        self._pump = pump
        self._default_exercise = default_exercise
        self._prototypes = prototypes or []
        self._exercise_lookup = exercise_lookup
        self._bar_weight = bar_weight_lb
        self._counter = RepCounter(
            min_amplitude_deg=rep_amplitude_deg,
            min_period_s=rep_min_period_s,
            smoothing_window=rep_smoothing_window,
        )
        self._fsm = SetBoundary(quiet_seconds=set_quiet_seconds)
        self._confidence_threshold = confidence_threshold
        self._snapshot_dir = snapshot_dir
        self._clips_dir = clips_dir
        self._clip_buffer = ClipBuffer(capacity_seconds=clip_capacity_seconds) \
            if clips_dir is not None else None
        self._on_set_committed = on_set_committed
        self._on_set_failed = on_set_failed

        # Wake/sleep tracking — pump-cv signals the wall display when the
        # athlete enters / leaves the room so the kiosk dims itself.
        self._wake_after_present = wake_after_present_seconds
        self._sleep_after_absent = sleep_after_absent_seconds
        self._present_since: float | None = None
        self._absent_since: float | None = None
        self._is_awake: bool = False

        # Per-set buffers used by the classifier and weight detector.
        self._rep_params = (rep_amplitude_deg, rep_min_period_s, rep_smoothing_window)
        self._pose_buffer: list[Pose] = []
        self._latest_frame: np.ndarray | None = None
        self._latest_athlete_pose: Pose | None = None

    async def run(self, pose_stream: AsyncIterator[FrameAndPoses]) -> None:
        async for frame, poses in pose_stream:
            if frame is not None:
                self._latest_frame = frame

            athlete = pick_athlete(poses)
            now = poses[0].timestamp if poses else 0.0

            # Wake/sleep signal to the wall display.
            await self._update_presence(athlete is not None, now)

            # Rolling clip buffer — stays warm across sets so the next
            # commit can dump it.
            if self._clip_buffer is not None and frame is not None:
                self._clip_buffer.push(frame, now)

            if athlete is not None:
                # Drive the rep counter with the default exercise's joint.
                # The classifier will (post-hoc) override the exercise name
                # at set close; reps are recomputed from the buffer if the
                # classifier picks a different joint mapping.
                ang = keypoint_angle(
                    athlete,
                    self._default_exercise.a_idx,
                    self._default_exercise.b_idx,
                    self._default_exercise.c_idx,
                )
                if ang is not None:
                    self._counter.push(ang, athlete.timestamp)
                self._pose_buffer.append(athlete)
                self._latest_athlete_pose = athlete

            for ev in self._fsm.tick(self._counter.count, now):
                await self._handle_event(ev)

    async def _update_presence(self, present: bool, now: float) -> None:
        if present:
            self._absent_since = None
            if self._present_since is None:
                self._present_since = now
            elif (not self._is_awake
                  and now - self._present_since >= self._wake_after_present):
                self._is_awake = True
                logger.info("athlete present → POST /api/wall/wake")
                try:
                    await self._pump.post_wall_wake()
                except Exception as e:
                    logger.warning("wake post failed", error=str(e))
        else:
            self._present_since = None
            if self._absent_since is None:
                self._absent_since = now
            elif (self._is_awake
                  and now - self._absent_since >= self._sleep_after_absent):
                self._is_awake = False
                logger.info("athlete absent → POST /api/wall/sleep")
                try:
                    await self._pump.post_wall_sleep()
                except Exception as e:
                    logger.warning("sleep post failed", error=str(e))

    async def _handle_event(self, ev) -> None:
        match ev:
            case SetStartedEvent():
                logger.info("set started", default_exercise=self._default_exercise.name, ts=ev.timestamp)
            case RepObservedEvent():
                logger.debug("rep", n=ev.rep_index_in_set)
            case SetEndedEvent():
                await self._commit_set(ev)

    async def _commit_set(self, ev: SetEndedEvent) -> None:
        # Classify the captured pose window.
        exercise_name = self._default_exercise.name
        classifier_conf = 1.0
        if self._prototypes and self._pose_buffer:
            feats = pose_sequence_to_features(self._pose_buffer)
            result = classify_window(feats, self._prototypes)
            if result is not None:
                exercise_name = result.name
                classifier_conf = result.confidence
                logger.info("classified", name=result.name, confidence=result.confidence)

        # Recompute reps if the classified exercise uses a different joint.
        rep_count = ev.rep_count
        spec = self._exercise_lookup(exercise_name) if self._exercise_lookup else None
        if spec is not None and (
            spec.a_idx != self._default_exercise.a_idx
            or spec.b_idx != self._default_exercise.b_idx
            or spec.c_idx != self._default_exercise.c_idx
        ):
            recounted = self._recount_reps(spec)
            logger.info("rep count recomputed for new joint mapping",
                        from_=ev.rep_count, to=recounted, exercise=exercise_name)
            rep_count = recounted

        # Estimate weight from the most recent frame, if any.
        # Voltra (smart cable trainer) is opaque to plate detection — its
        # resistance is electronic, not visible. Until we reverse-engineer
        # its BLE protocol, any exercise whose name contains "Voltra" is
        # written pending with weight=0 so the athlete enters the
        # resistance manually on confirmation.
        weight_lb, weight_conf = (0.0, 0.5)
        is_voltra = "voltra" in exercise_name.lower()
        if is_voltra:
            weight_lb, weight_conf = (0.0, 0.0)
            logger.info("voltra opaque-load: deferring weight to athlete confirmation",
                        exercise=exercise_name)
        elif self._latest_frame is not None:
            try:
                weight_lb, weight_conf = estimate_barbell_load(
                    self._latest_frame, bar_weight_lb=self._bar_weight,
                )
            except Exception as e:
                logger.warning("weight estimate failed", error=str(e))

        # A pending set is one where any sub-detector is below threshold
        # or the rep count looks suspiciously low. Voltra is always
        # pending because we need the athlete to type in the resistance.
        confidence = min(classifier_conf, weight_conf)
        pending = (
            is_voltra
            or confidence < self._confidence_threshold
            or rep_count < 3
        )

        note = "Voltra resistance — please confirm." if is_voltra else ""

        payload = {
            "Date": _today(),
            "Name": exercise_name,
            "Weight": f"{weight_lb:.1f}",
            "Reps": int(rep_count),
            "Source": "cv",
            "Confidence": round(confidence, 3),
            # `pending` arrives here as numpy.bool_ when computed from a
            # numpy comparison upstream (e.g. confidence < threshold on
            # arrays). httpx's default JSON encoder rejects numpy scalars
            # with "Object of type bool is not JSON serializable", and
            # the whole set silently never reaches /api/sets. Coerce.
            "Pending": bool(pending),
            "Note": note,
        }
        logger.info("set ended → POST /api/sets",
                    exercise=exercise_name, reps=int(rep_count),
                    weight_lb=weight_lb, pending=pending)
        try:
            set_id = await self._pump.post_set(payload)
            logger.info("set written", id=set_id)
            if self._on_set_committed:
                self._on_set_committed(pending)

            # Phase 2: dump the rolling clip buffer named by the server-
            # assigned id, then PATCH the row with its path. PUMP serves
            # the file at /clips/<date>/<id>__<exercise>.mp4.
            if (self._clip_buffer is not None
                    and self._clips_dir is not None
                    and len(self._clip_buffer) > 0):
                try:
                    rel_path = self._clip_buffer.write_clip(
                        self._clips_dir,
                        set_id=set_id,
                        exercise=exercise_name,
                    )
                    if rel_path is not None:
                        await self._pump.patch_set(set_id, {"ClipPath": str(rel_path)})
                except Exception as e:
                    logger.warning("clip write/patch failed", error=str(e))
        except Exception as e:
            logger.error("set write failed", error=str(e))
            if self._on_set_failed:
                self._on_set_failed()

        # Save an annotated debug snapshot if a frame was captured.
        if self._snapshot_dir is not None:
            try:
                save_snapshot(
                    self._snapshot_dir,
                    self._latest_frame,
                    self._latest_athlete_pose,
                    exercise=exercise_name,
                    weight_lb=weight_lb,
                    reps=int(rep_count),
                    confidence=confidence,
                    pending=pending,
                )
            except Exception as e:
                logger.warning("snapshot save failed", error=str(e))

        # Reset per-set state.
        self._counter.reset()
        self._pose_buffer = []
        self._latest_athlete_pose = None

    def _recount_reps(self, spec: ExerciseSpec) -> int:
        """Replay the pose buffer through a fresh RepCounter using a
        different joint triple. Used when the classifier picks an
        exercise whose primary joint differs from the default."""
        amp, period, win = self._rep_params
        rc = RepCounter(min_amplitude_deg=amp, min_period_s=period, smoothing_window=win)
        for p in self._pose_buffer:
            ang = keypoint_angle(p, spec.a_idx, spec.b_idx, spec.c_idx)
            if ang is not None:
                rc.push(ang, p.timestamp)
        return rc.count

    # ─── admin panel introspection ───────────────────────────────────
    # healthd's /api/v1/state and /api/v1/thresholds endpoints call
    # these to render the admin UI and hot-reload tunables. The wall's
    # calibration wizard also polls state.

    def snapshot_state(self) -> dict:
        return {
            "default_exercise": self._default_exercise.name,
            "fsm_state": self._fsm.state.value,
            "rep_count": self._counter.count,
            "is_awake": self._is_awake,
            "buffer_frames": len(self._clip_buffer) if self._clip_buffer else 0,
            "pose_buffer_len": len(self._pose_buffer),
            "prototypes_loaded": len(self._prototypes),
        }

    def snapshot_thresholds(self) -> dict:
        return {
            "rep": {
                "min_amplitude_deg": self._counter.min_amplitude_deg,
                "min_period_s":      self._counter.min_period_s,
                "smoothing_window":  self._counter.smoothing_window,
            },
            "set_boundary": {
                "quiet_seconds": self._fsm.quiet_seconds,
            },
            "confidence_threshold": self._confidence_threshold,
        }

    def update_thresholds(self, payload: dict) -> None:
        """Hot-reload tunables. Accepts dotted keys ('rep.min_period_s').
        Unknown keys are ignored. The new values take effect on the next
        frame; no pipeline restart needed."""
        for key, value in payload.items():
            if key == "rep.min_amplitude_deg":
                self._counter.min_amplitude_deg = float(value)
            elif key == "rep.min_period_s":
                self._counter.min_period_s = float(value)
            elif key == "rep.smoothing_window":
                self._counter.smoothing_window = int(value)
            elif key == "set_boundary.quiet_seconds":
                self._fsm.quiet_seconds = float(value)
            elif key == "confidence_threshold":
                self._confidence_threshold = float(value)


def _today() -> str:
    return dt.date.today().isoformat()
