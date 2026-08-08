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
import time
from collections import deque
from collections.abc import AsyncIterator, Callable
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
from ..fsm.set_boundary import (
    RepObservedEvent,
    SetEndedEvent,
    SetStartedEvent,
    SetState,
)
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
        is_voltra_exercise=None,  # callable: name → bool
        voltra_sidecar_enabled: bool = False,
        bar_weight_lb: float = 45.0,
        rep_amplitude_deg: float = 25.0,
        rep_min_period_s: float = 0.6,
        rep_smoothing_window: int = 9,
        set_quiet_seconds: float = 25.0,
        confidence_threshold: float = 0.75,
        snapshot_dir: Path | None = None,
        clips_dir: Path | None = None,
        clip_capacity_seconds: float = 8.0,
        clock: Callable[[], float] = time.time,
        wake_after_present_seconds: float = 1.0,
        sleep_after_absent_seconds: float = 600.0,
        on_set_committed=None,  # callable(pending: bool) -> None — for healthd metrics
        on_set_failed=None,     # callable() -> None
    ):
        self._pump = pump
        self._default_exercise = default_exercise
        self._prototypes = prototypes or []
        self._exercise_lookup = exercise_lookup
        self._is_voltra_exercise = is_voltra_exercise
        self._voltra_sidecar_enabled = voltra_sidecar_enabled
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
        self._pose_buffer: deque[Pose] = deque()
        # Longest span of poses worth keeping for classification: a generous
        # set, plus the quiet period that has to elapse before it closes.
        #
        # The buffer used to be cleared only on set commit, so any stretch
        # where somebody is visible but no set ever completes — treadmill
        # cardio in view of gym-front, stretching, tidying up — appended a
        # ~1.5 KB pose every frame and never freed it. In a pod that runs for
        # weeks that is a slow, permanent climb; the deployed pod was sitting
        # at 93% of its memory limit.
        self._pose_window_s = set_quiet_seconds + 180.0
        # Wall clock used to keep time moving when nobody is in frame.
        # Injectable so replays and tests stay deterministic.
        self._clock = clock
        self._last_pose_ts: float | None = None
        self._last_pose_wall: float = 0.0
        self._latest_frame: np.ndarray | None = None
        self._latest_athlete_pose: Pose | None = None

    async def run(self, pose_stream: AsyncIterator[FrameAndPoses]) -> None:
        async for frame, poses in pose_stream:
            if frame is not None:
                self._latest_frame = frame

            athlete = pick_athlete(poses)
            now = self._now(poses)

            # Wake/sleep signal to the wall display.
            await self._update_presence(athlete is not None, now)

            # Rolling clip buffer — stays warm across sets so the next
            # commit can dump it.
            #
            # Stop sampling once reps have stopped. The buffer holds 8 s but a
            # set does not close until 25 s of quiet have passed, so recording
            # through the rest meant the deque had turned over three times by
            # the time the clip was written: every clip showed the athlete
            # standing still, never a rep. Freezing during RESTING leaves the
            # set's final seconds in the buffer, and costs nothing — enlarging
            # it instead would be ~55 MB per 8 s per camera.
            if (self._clip_buffer is not None and frame is not None
                    and self._fsm.state != SetState.RESTING):
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
                self._trim_pose_buffer(now)
                self._latest_athlete_pose = athlete

            for ev in self._fsm.tick(self._counter.count, now):
                await self._handle_event(ev)

    def _trim_pose_buffer(self, now: float) -> None:
        """Drop poses older than the classification window.

        Bounded by time rather than count so it does not depend on frame rate,
        and cheap: the buffer is ordered, so this pops a handful of entries per
        frame in the steady state.
        """
        cutoff = now - self._pose_window_s
        buf = self._pose_buffer
        while buf and buf[0].timestamp < cutoff:
            buf.popleft()

    def _now(self, poses: list[Pose]) -> float:
        """The current time on the pose clock.

        YOLO yields an empty pose list for every frame with nobody in view,
        and this used to report 0.0 for those. Two things broke as a result,
        both silently:

          * A set never closed. SetBoundary closes on
            ts - last_rep_at >= quiet_seconds, which with ts=0 is about -1.7e9
            and can never be true. The final set of a workout stayed open until
            somebody next walked in front of the camera — possibly the next
            day, and then stamped with that later date.
          * The kiosk never slept. _absent_since was set to 0.0 and every later
            absent frame also read 0.0, so the elapsed time was permanently
            zero and POST /api/wall/sleep was unreachable.

        So when there are no poses, carry the last pose timestamp forward by
        real elapsed time. That keeps the clock monotonic and moving on the
        same scale the poses use, without assuming the pose clock is the wall
        clock — it is not during file playback.
        """
        if poses:
            self._last_pose_ts = poses[0].timestamp
            self._last_pose_wall = self._clock()
            return self._last_pose_ts
        if self._last_pose_ts is None:
            # Nobody has been seen yet; any monotonic origin will do.
            return self._clock()
        return self._last_pose_ts + (self._clock() - self._last_pose_wall)

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
            feats = pose_sequence_to_features(list(self._pose_buffer))
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

        # Whether this exercise is performed on the Voltra trainer is a flag
        # on the exercise row in PUMP, not a guess from its name — plenty of
        # exercises have "cable" in the name and use a plate stack.
        is_voltra = bool(self._is_voltra_exercise and self._is_voltra_exercise(exercise_name))

        # If we have never managed to read the flag list, we cannot tell a
        # Voltra exercise from a barbell one — an empty cache and "nothing uses
        # the trainer" look identical. Skip rather than guess.
        #
        # This used to fail open, so a pump-cv that started before pump-api was
        # serving (a node reboot rolls both) wrote CV rows for up to five
        # minutes for the very sets pump-voltra was also writing — every one
        # logged twice, one of them with a plate-detected weight that is
        # meaningless for electronic resistance. Failing closed loses a few
        # sets, which is visible; failing open corrupts the log, which is not.
        flags = self._is_voltra_exercise
        flags_unknown = (
            self._voltra_sidecar_enabled
            and flags is not None
            and getattr(flags, "loaded", True) is False
        )
        if flags_unknown:
            logger.warning(
                "voltra flags never loaded — skipping the write rather than "
                "risk double-logging a trainer set",
                exercise=exercise_name, reps=int(rep_count))
            if self._on_set_failed:
                self._on_set_failed()
            self._reset_set_state()
            return

        # When pump-voltra is running it owns these sets outright: it reads
        # the real resistance and the device's own rep count off the trainer,
        # both of which beat anything we can infer. Writing here too would log
        # every Voltra set twice.
        if is_voltra and self._voltra_sidecar_enabled:
            logger.info("voltra set owned by the pump-voltra sidecar — not writing",
                        exercise=exercise_name, reps=int(rep_count))
            self._reset_set_state()
            return

        # Estimate weight from the most recent frame, if any.
        # The trainer's resistance is electronic and invisible to plate
        # detection, so with no sidecar to supply it the set is written
        # pending with weight=0 and the athlete types the resistance in on
        # confirmation. This is the fallback path when the trainer is
        # unreachable or pump-voltra is not deployed.
        weight_lb, weight_conf = (0.0, 0.5)
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

        self._reset_set_state()

    def _reset_set_state(self) -> None:
        """Clear the per-set buffers. Every exit from _commit_set must call
        this, including the early return when the Voltra sidecar owns the
        set — otherwise the next set inherits this one's pose window."""
        self._counter.reset()
        self._pose_buffer.clear()
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
