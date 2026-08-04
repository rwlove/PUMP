"""Entry point for `python -m pump_cv.main` and the `pump-cv` script.

Wires config → logger → PoseSource (real or mock) → PipelineRunner.
For Phase 1 it runs a single camera against a single hardcoded
exercise; later slices add multi-cam fusion and exercise classification.
"""

from __future__ import annotations

import asyncio
import os
from pathlib import Path

from . import exercises as ex_lookup_mod
from . import healthd, log, retention, state
from .calibration import load_camera
from .classify import PrototypeStore
from .config import CVConfig, load
from .pipeline import ExerciseSpec, PipelineRunner
from .pose.fused import FusedPoseSource
from .pose.types import (
    LEFT_ANKLE,
    LEFT_HIP,
    LEFT_KNEE,
)
from .pump_client import PumpClient
from .voltra import VoltraFlags

logger = log.get(__name__)


# Default exercise for driving the rep counter when no prototypes are
# loaded yet. Squat (knee flexion) is the most generic single-joint
# signal — covers most lower-body work and degrades gracefully on
# upper-body exercises (the classifier corrects them on set close).
DEFAULT_EXERCISE = ExerciseSpec(
    name=os.getenv("PUMP_CV_EXERCISE", "Squat"),
    a_idx=LEFT_HIP,
    b_idx=LEFT_KNEE,
    c_idx=LEFT_ANKLE,
)

PROTOTYPE_DIR = Path(os.getenv("PUMP_CV_PROTOTYPE_DIR", "prototypes"))
SNAPSHOT_DIR  = Path(os.getenv("PUMP_CV_SNAPSHOT_DIR", "snapshots"))
CLIPS_DIR     = Path(os.getenv("PUMP_CV_CLIPS_DIR", "clips"))
HEALTHD_PORT  = int(os.getenv("PUMP_CV_HEALTHD_PORT", "8080"))
RETENTION_DAYS = float(os.getenv("PUMP_CV_RETENTION_DAYS", "30"))


def _build_single_source(cam, cfg: CVConfig):
    source = cam.rtsp_url or cam.video_path
    if not source:
        raise SystemExit(f"pump-cv: camera {cam.name!r} has neither rtsp_url nor video_path")

    if cfg.pose.backend == "mock":
        from .pose.mock import MockPoseSource
        return MockPoseSource(
            schedule=[("rep", 10.0), ("rest", 30.0)],
            camera=cam.name,
        )

    from .pose.yolo import YOLOPoseSource
    return YOLOPoseSource(
        source=source,
        camera_name=cam.name,
        model=cfg.pose.model,
        image_size=cfg.pose.image_size,
        device=cfg.pose.device,
    )


def _assemble_pose_source(cfg: CVConfig, sources: list):
    """Wire up the right PoseSource using already-constructed singles.

    Used when main.py has pre-built per-camera sources to populate the
    camera registry for the wizard previews; after calibration completes
    we wrap those same instances in FusedPoseSource so we don't double-
    register cameras or open the RTSP stream twice.
    """
    if not cfg.cameras or not sources:
        raise SystemExit("pump-cv: at least one camera must be configured")
    if (
        len(cfg.cameras) >= 2
        and cfg.cameras[0].calibration_path
        and cfg.cameras[1].calibration_path
        and len(sources) >= 2
    ):
        calib_a = load_camera(Path(cfg.cameras[0].calibration_path))
        calib_b = load_camera(Path(cfg.cameras[1].calibration_path))
        return FusedPoseSource(sources[0], sources[1], calib_a, calib_b)
    return sources[0]


async def _amain() -> None:
    log.configure()
    cfg = load()
    logger.info("pump-cv starting",
                cameras=[c.name for c in cfg.cameras],
                pose_backend=cfg.pose.backend,
                pump_base=cfg.pump.base_url)

    # ─── Calibration gate ─────────────────────────────────────────────
    # When >=2 cameras are configured we enter a multi-cam fusion flow
    # that needs per-camera intrinsics+extrinsics .npz files. If those
    # files don't exist yet, hold here and let the wizard endpoints
    # in healthd drive the user through capture → compute. The runner
    # below will not start until state.set_phase("ready") is called.
    state.set_camera_count(len(cfg.cameras))
    state.load_persisted_captures()
    needs_cal = (
        len(cfg.cameras) >= 2
        and not all(
            c.calibration_path
            and Path(c.calibration_path).is_file()
            for c in cfg.cameras
        )
    )
    # Single per-camera sources are constructed regardless of calibration
    # state — YOLOPoseSource.__init__ self-registers in the global camera
    # registry, which the wall wizard's previews and /api/v1/cameras
    # depend on. The fused pose source (if needed) is assembled below
    # AFTER the wizard finishes, reusing these same instances.
    single_sources = [_build_single_source(c, cfg) for c in cfg.cameras]

    if needs_cal:
        state.set_phase("needs_calibration")
        logger.info("pump-cv: holding for calibration",
                    cameras=[c.name for c in cfg.cameras])
    else:
        state.set_phase("ready")

    # Drain tasks decode frames into each source's `latest_frame` attribute
    # so the wizard's per-camera preview tiles have something to show. We
    # only spawn these while held for calibration; once the runner takes
    # over, IT consumes the (possibly-fused) source's poses().
    drain_tasks: list[asyncio.Task] = []
    if not state.is_ready():
        async def _drain(src):
            try:
                async for _ in src.poses():
                    pass
            except asyncio.CancelledError:
                raise
            except Exception as e:
                logger.warning("drain task crashed", error=str(e))
        for src in single_sources:
            drain_tasks.append(asyncio.create_task(_drain(src)))

    pose_source = None
    if state.is_ready():
        pose_source = _assemble_pose_source(cfg, single_sources)

    # Load any prototypes the operator has recorded so far. Empty list is
    # fine — runner falls back to the default exercise name.
    prototypes = []
    if PROTOTYPE_DIR.exists():
        prototypes = PrototypeStore(PROTOTYPE_DIR).load_all()
    logger.info("prototypes loaded", count=len(prototypes), dir=str(PROTOTYPE_DIR))

    # Run the health server and the pipeline concurrently.
    async with PumpClient(cfg.pump.base_url, cfg.pump.api_key, cfg.pump.request_timeout_s) as pump:
        # Which exercises use the Voltra trainer is a flag on the exercise
        # row in PUMP. Prime it before the first set so a Voltra set at the
        # very start of a workout isn't misread as a barbell lift.
        voltra_flags = VoltraFlags(pump, cfg.voltra.flag_refresh_seconds)
        try:
            await voltra_flags.refresh()
        except Exception as e:
            logger.warning("initial voltra flag load failed", error=str(e))

        runner = PipelineRunner(
            pump=pump,
            default_exercise=DEFAULT_EXERCISE,
            prototypes=prototypes,
            exercise_lookup=ex_lookup_mod.lookup,
            is_voltra_exercise=voltra_flags,
            voltra_sidecar_enabled=cfg.voltra.enabled,
            rep_amplitude_deg=cfg.rep.min_amplitude_deg,
            rep_min_period_s=cfg.rep.min_period_s,
            rep_smoothing_window=cfg.rep.smoothing_window,
            set_quiet_seconds=cfg.set_boundary.quiet_seconds,
            confidence_threshold=cfg.confidence_threshold,
            snapshot_dir=SNAPSHOT_DIR,
            clips_dir=CLIPS_DIR,
            on_set_committed=healthd.record_set_posted,
            on_set_failed=healthd.record_set_failed,
        )

        health_task = asyncio.create_task(
            healthd.serve(
                port=HEALTHD_PORT,
                prototype_dir=PROTOTYPE_DIR,
                snapshot_dir=SNAPSHOT_DIR,
                runner=runner,
            ),
        )
        retention_task = asyncio.create_task(
            retention.run_forever(SNAPSHOT_DIR, CLIPS_DIR, RETENTION_DAYS),
        )
        # Re-reads the flag list on a timer, so ticking the checkbox on an
        # exercise in PUMP takes effect without a rolling restart.
        voltra_task = asyncio.create_task(voltra_flags.run_forever())
        healthd.mark_ready()
        try:
            # If we held for calibration, wait for the wizard to flip the
            # state to "ready" before starting the pose loop. The wizard
            # endpoints write the .npz files and call state.set_phase.
            await state.wait_for_ready()
            # Cancel the drain tasks before the runner takes over; the
            # runner consumes the source itself and we don't want two
            # generators racing on the same VideoCapture.
            for t in drain_tasks:
                t.cancel()
            if drain_tasks:
                await asyncio.gather(*drain_tasks, return_exceptions=True)
            if pose_source is None:
                # Re-load config because the wizard may have written
                # calibration files since startup; reuse the existing
                # single sources so we don't double-register cameras.
                pose_source = _assemble_pose_source(load(), single_sources)
            await runner.run(pose_source.poses())
        finally:
            for t in drain_tasks:
                t.cancel()
            health_task.cancel()
            retention_task.cancel()
            voltra_task.cancel()
            await asyncio.gather(health_task, retention_task, voltra_task, *drain_tasks,
                                 return_exceptions=True)

    logger.info("pump-cv: pose stream ended")


def run() -> None:
    asyncio.run(_amain())


if __name__ == "__main__":
    run()
