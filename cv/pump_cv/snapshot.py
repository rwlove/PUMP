"""Annotated frame snapshots for debugging CV detections.

When the pipeline closes a set, save one PNG to disk with the detected
skeleton drawn over it and a caption summarising what the system
inferred (exercise, weight, reps, confidence, pending). Tuning is
dramatically easier when wrong detections leave a visual trail.

Files land at <root>/<YYYY-MM-DD>/<HH-MM-SS>__<exercise>.png so the
operator can browse by date.
"""

from __future__ import annotations

import datetime as dt
from pathlib import Path

import cv2
import numpy as np

from .pose.types import Pose

# COCO-17 skeleton edges (pairs of keypoint indices).
_SKELETON_EDGES = (
    (5, 7), (7, 9),       # left arm
    (6, 8), (8, 10),      # right arm
    (5, 6),               # shoulders
    (5, 11), (6, 12),     # torso sides
    (11, 12),             # hips
    (11, 13), (13, 15),   # left leg
    (12, 14), (14, 16),   # right leg
    (0, 1), (0, 2),       # nose-eyes
    (1, 3), (2, 4),       # eyes-ears
)


def _draw_pose(img: np.ndarray, pose: Pose, color=(0, 255, 0)) -> None:
    """Overlay a stick-figure of the pose onto img (in place)."""
    for a, b in _SKELETON_EDGES:
        ka = pose.keypoints[a]
        kb = pose.keypoints[b]
        if ka.confidence < 0.3 or kb.confidence < 0.3:
            continue
        cv2.line(img, (int(ka.x), int(ka.y)), (int(kb.x), int(kb.y)), color, 2, cv2.LINE_AA)
    for k in pose.keypoints:
        if k.confidence < 0.3:
            continue
        cv2.circle(img, (int(k.x), int(k.y)), 4, color, -1, cv2.LINE_AA)


def _draw_caption(img: np.ndarray, lines: list[str]) -> None:
    """Draw a black-on-yellow caption box at the top of the image."""
    h, w = img.shape[:2]
    pad = 8
    line_h = 22
    box_h = pad * 2 + line_h * len(lines)
    cv2.rectangle(img, (0, 0), (w, box_h), (0, 0, 0), -1)
    for i, text in enumerate(lines):
        cv2.putText(img, text, (pad, pad + line_h * (i + 1) - 6),
                    cv2.FONT_HERSHEY_SIMPLEX, 0.55, (0, 255, 255), 1, cv2.LINE_AA)


def save_snapshot(
    root: Path,
    frame: np.ndarray | None,
    pose: Pose | None,
    exercise: str,
    weight_lb: float,
    reps: int,
    confidence: float,
    pending: bool,
) -> Path | None:
    """Write one annotated snapshot to disk. Returns the file path, or
    None when there's no frame to draw on (mock mode)."""
    if frame is None:
        return None
    out_dir = Path(root) / dt.date.today().isoformat()
    out_dir.mkdir(parents=True, exist_ok=True)

    annotated = frame.copy()
    if pose is not None:
        color = (0, 255, 255) if pending else (0, 255, 0)
        _draw_pose(annotated, pose, color=color)

    state_label = "PENDING" if pending else "OK"
    _draw_caption(annotated, [
        f"{exercise} — {state_label}",
        f"{weight_lb:.1f} lb x {reps} reps",
        f"confidence {confidence:.2f}",
    ])

    safe_name = "".join(c if c.isalnum() else "-" for c in exercise).strip("-")
    fname = dt.datetime.now().strftime("%H-%M-%S") + f"__{safe_name or 'unknown'}.png"
    path = out_dir / fname
    cv2.imwrite(str(path), annotated)
    return path
