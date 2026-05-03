"""CLI: ingest a video clip of one exercise, save its prototype to disk.

Run after recording (e.g. on your phone) a short clip of N reps of one
exercise. The CLI loads the video, runs YOLOv8-Pose, picks the athlete
each frame, derives the joint-angle feature sequence, and writes it to
the configured PrototypeStore.

Example:

    python -m pump_cv.scripts.add_reference \
        --video clips/squat-reference-001.mp4 \
        --exercise-name "Squat" \
        --store prototypes/

The full PUMP-UI record-and-upload flow is Phase 2 (waits for cameras
and the file-upload endpoint). This CLI gets us a working classifier
today against any clips you can produce by hand.
"""

from __future__ import annotations

import argparse
import asyncio
import sys
from pathlib import Path

from .. import log
from ..classify import (
    ExercisePrototype,
    PrototypeStore,
    pose_sequence_to_features,
)
from ..pose.yolo import YOLOPoseSource
from ..tracking import pick_athlete

logger = log.get(__name__)


async def _process(args) -> int:
    log.configure()
    if not Path(args.video).is_file():
        logger.error("video not found", path=args.video)
        return 1

    source = YOLOPoseSource(
        source=args.video,
        camera_name="reference",
        model=args.model,
        image_size=args.image_size,
        device=args.device,
    )

    poses_per_frame = []
    async for _frame, poses in source.poses():
        athlete = pick_athlete(poses)
        if athlete is not None:
            poses_per_frame.append(athlete)

    if not poses_per_frame:
        logger.error("no athlete detected in clip", path=args.video)
        return 2

    feats = pose_sequence_to_features(poses_per_frame)
    proto = ExercisePrototype(
        exercise_name=args.exercise_name,
        features=feats,
        source_clip=args.video,
    )
    store = PrototypeStore(Path(args.store))
    saved = store.add(proto)
    logger.info("prototype saved",
                exercise=args.exercise_name, frames=len(poses_per_frame),
                path=str(saved))
    return 0


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="pump_cv.scripts.add_reference")
    p.add_argument("--video", required=True,
                   help="path to a short clip of N reps of one exercise")
    p.add_argument("--exercise-name", required=True,
                   help="must match the PUMP exercise name verbatim")
    p.add_argument("--store", default="prototypes",
                   help="prototype store directory (default: ./prototypes)")
    p.add_argument("--model", default="yolov8m-pose.pt",
                   help="ultralytics model name or path")
    p.add_argument("--image-size", type=int, default=640)
    p.add_argument("--device", default="cuda:0",
                   help="cuda:0 / cpu / etc.")
    args = p.parse_args(argv)
    return asyncio.run(_process(args))


if __name__ == "__main__":
    sys.exit(main())
