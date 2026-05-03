"""Single-athlete picker.

Personal home gym → at most one athlete per frame, but bystanders
(family member walking through, reflection, etc.) can show up. Pick the
person whose bounding box is largest AND closest to the frame centre,
and assume that's our athlete. This is a stateless pick per frame; a
proper tracker is overkill at this scope.
"""

from __future__ import annotations

from ..pose.types import Pose


def pick_athlete(poses: list[Pose], frame_w: int = 1920, frame_h: int = 1080) -> Pose | None:
    """Return the most-likely athlete pose, or None if no people detected."""
    if not poses:
        return None
    cx0, cy0 = frame_w / 2, frame_h / 2

    def score(p: Pose) -> float:
        x1, y1, x2, y2 = p.bbox
        area = max(0.0, (x2 - x1) * (y2 - y1))
        cx, cy = (x1 + x2) / 2, (y1 + y2) / 2
        # Distance to frame centre, normalised by diagonal.
        diag = (frame_w**2 + frame_h**2) ** 0.5
        d = ((cx - cx0) ** 2 + (cy - cy0) ** 2) ** 0.5
        centre_bonus = 1.0 - (d / diag)  # 1.0 dead-centre, 0.0 corner
        # Equal-ish weight: bigger and more central wins.
        return area * (0.5 + 0.5 * centre_bonus) * p.score

    return max(poses, key=score)
