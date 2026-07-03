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

The full PUMP-UI record-and-upload flow (`POST /api/v1/reference` on
healthd) shares the same core — `pump_cv.classify.build_prototype_from_
video` — so a fix to the pipeline never has to be replicated across
both entry points.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

from .. import log
from ..classify import NoAthleteDetectedError, build_prototype_from_video

logger = log.get(__name__)


def _process(args) -> int:
    log.configure()
    try:
        saved = build_prototype_from_video(
            video_path=Path(args.video),
            exercise_name=args.exercise_name,
            prototype_dir=Path(args.store),
            model=args.model,
            image_size=args.image_size,
            device=args.device,
        )
    except FileNotFoundError:
        logger.error("video not found", path=args.video)
        return 1
    except NoAthleteDetectedError:
        logger.error("no athlete detected in clip", path=args.video)
        return 2

    logger.info("prototype saved",
                exercise=args.exercise_name, path=str(saved))
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
    return _process(args)


if __name__ == "__main__":
    sys.exit(main())
