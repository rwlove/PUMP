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
from . import log
from .classify import PrototypeStore
from .config import CVConfig, load
from .pipeline import ExerciseSpec, PipelineRunner
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


def _build_pose_source(cfg: CVConfig):
    """Construct the PoseSource implied by the config.

    Only the first camera is used in this slice; multi-cam fusion comes
    later. Backend selection: yaml `pose.backend: mock` skips ultralytics
    entirely (useful for unit tests and no-GPU dev).
    """
    if not cfg.cameras:
        raise SystemExit("pump-cv: at least one camera must be configured")

    cam = cfg.cameras[0]
    source = cam.rtsp_url or cam.video_path
    if not source:
        raise SystemExit(f"pump-cv: camera {cam.name!r} has neither rtsp_url nor video_path")

    if cfg.pose.backend == "mock":
        # Mock source ignores the camera config; useful only for smoke tests.
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


async def _amain() -> None:
    log.configure()
    cfg = load()
    logger.info("pump-cv starting",
                cameras=[c.name for c in cfg.cameras],
                pose_backend=cfg.pose.backend,
                pump_base=cfg.pump.base_url)

    pose_source = _build_pose_source(cfg)

    # Load any prototypes the operator has recorded so far. Empty list is
    # fine — runner falls back to the default exercise name.
    prototypes = []
    if PROTOTYPE_DIR.exists():
        prototypes = PrototypeStore(PROTOTYPE_DIR).load_all()
    logger.info("prototypes loaded", count=len(prototypes), dir=str(PROTOTYPE_DIR))

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
        )
        await runner.run(pose_source.poses())

    logger.info("pump-cv: pose stream ended")


def run() -> None:
    asyncio.run(_amain())


if __name__ == "__main__":
    run()
