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


def _build_pose_source(cfg: CVConfig):
    """Construct the PoseSource (single or fused) implied by the config.

    Single camera (or two-camera config without calibration paths) →
    one PoseSource. Two cameras with calibration → FusedPoseSource that
    pairs frames by timestamp and triangulates the athlete to 3D.
    """
    if not cfg.cameras:
        raise SystemExit("pump-cv: at least one camera must be configured")

    if len(cfg.cameras) >= 2 and cfg.cameras[0].calibration_path and cfg.cameras[1].calibration_path:
        cam_a, cam_b = cfg.cameras[0], cfg.cameras[1]
        src_a = _build_single_source(cam_a, cfg)
        src_b = _build_single_source(cam_b, cfg)
        calib_a = load_camera(Path(cam_a.calibration_path))
        calib_b = load_camera(Path(cam_b.calibration_path))
        return FusedPoseSource(src_a, src_b, calib_a, calib_b)

    return _build_single_source(cfg.cameras[0], cfg)


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
    if needs_cal:
        state.set_phase("needs_calibration")
        logger.info("pump-cv: holding for calibration",
                    cameras=[c.name for c in cfg.cameras])
    else:
        state.set_phase("ready")

    pose_source = _build_pose_source(cfg) if state.is_ready() else None

    # Load any prototypes the operator has recorded so far. Empty list is
    # fine — runner falls back to the default exercise name.
    prototypes = []
    if PROTOTYPE_DIR.exists():
        prototypes = PrototypeStore(PROTOTYPE_DIR).load_all()
    logger.info("prototypes loaded", count=len(prototypes), dir=str(PROTOTYPE_DIR))

    # Run the health server and the pipeline concurrently.
    async with PumpClient(cfg.pump.base_url, cfg.pump.api_key, cfg.pump.request_timeout_s) as pump:
        runner = PipelineRunner(
            pump=pump,
            default_exercise=DEFAULT_EXERCISE,
            prototypes=prototypes,
            exercise_lookup=ex_lookup_mod.lookup,
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
        healthd.mark_ready()
        try:
            # If we held for calibration, wait for the wizard to flip the
            # state to "ready" before starting the pose loop. The wizard
            # endpoints write the .npz files and call state.set_phase.
            # They also re-construct the pose_source via this branch on
            # the next loop iteration.
            while True:
                await state.wait_for_ready()
                if pose_source is None:
                    pose_source = _build_pose_source(load())
                await runner.run(pose_source.poses())
                # runner.run only returns on a finite source (video file)
                # or fatal upstream error — break out of the gate loop
                # in either case so we don't spin.
                break
        finally:
            health_task.cancel()
            retention_task.cancel()
            await asyncio.gather(health_task, retention_task, return_exceptions=True)

    logger.info("pump-cv: pose stream ended")


def run() -> None:
    asyncio.run(_amain())


if __name__ == "__main__":
    run()
