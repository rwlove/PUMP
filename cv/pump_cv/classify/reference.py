"""Build one exercise prototype from a video clip.

Shared core for the two ways prototypes get created today:

- The healthd `POST /api/v1/reference` endpoint (web UI upload)
- The `python -m pump_cv.scripts.add_reference` CLI

Both wrap `build_prototype_from_video` and translate the two failure
cases (missing/unreadable video, no athlete detected) into whatever
their transport needs — HTTP 4xx from healthd, an argparse-style exit
code from the CLI.
"""

from __future__ import annotations

import asyncio
from pathlib import Path

from ..pose.yolo import YOLOPoseSource
from ..tracking import pick_athlete
from .prototypes import ExercisePrototype, PrototypeStore, pose_sequence_to_features


class NoAthleteDetectedError(Exception):
    """Raised when pick_athlete never returned a pose for any frame.

    Distinct from generic errors so the two callers can translate it
    into the right transport-level status: HTTP 422 from healthd, a
    non-zero exit code from the CLI."""


async def _collect_athlete_poses(source: YOLOPoseSource) -> list:
    """Iterate a pose source to end-of-stream, keeping only frames with
    an athlete detected. Extracted so tests can inject a mock source."""
    poses = []
    async for _frame, per_frame in source.poses():
        athlete = pick_athlete(per_frame)
        if athlete is not None:
            poses.append(athlete)
    return poses


def build_prototype_from_video(
    video_path: Path,
    exercise_name: str,
    prototype_dir: Path,
    *,
    model: str = "yolov8m-pose.pt",
    image_size: int = 640,
    device: str = "cuda:0",
) -> Path:
    """Run YOLOv8-Pose over a clip, distil into an ExercisePrototype,
    save it via PrototypeStore, and return the .npz path on disk.

    Raises FileNotFoundError when video_path doesn't exist, and
    NoAthleteDetectedError when no frame produced an athlete pose.

    Synchronous by design: healthd calls this inside a worker thread
    (asyncio.to_thread) so its own event loop stays responsive, and the
    CLI wraps it in asyncio.run at the argparse boundary. Keeping the
    core sync avoids leaking asyncio semantics into both call sites.
    """
    video_path = Path(video_path)
    if not video_path.is_file():
        raise FileNotFoundError(f"video not found: {video_path}")

    source = YOLOPoseSource(
        source=str(video_path),
        camera_name="reference",
        model=model,
        image_size=image_size,
        device=device,
    )

    poses = asyncio.run(_collect_athlete_poses(source))
    if not poses:
        raise NoAthleteDetectedError(str(video_path))

    features = pose_sequence_to_features(poses)
    store = PrototypeStore(prototype_dir)
    return store.add(ExercisePrototype(
        exercise_name=exercise_name,
        features=features,
        source_clip=video_path.name,
    ))
