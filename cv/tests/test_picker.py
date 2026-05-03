"""Unit tests for the single-athlete picker."""

from __future__ import annotations

from pump_cv.pose.types import Keypoint, Pose
from pump_cv.tracking import pick_athlete


def _pose(bbox, score=0.99):
    return Pose(
        timestamp=0.0,
        bbox=bbox,
        score=score,
        keypoints=tuple(Keypoint(0.0, 0.0, 0.5) for _ in range(17)),
    )


def test_returns_none_when_empty():
    assert pick_athlete([]) is None


def test_picks_the_only_pose():
    p = _pose((100, 100, 200, 200))
    assert pick_athlete([p]) is p


def test_prefers_larger_more_central():
    small_corner = _pose((10, 10, 60, 60))            # 2500 px², top-left
    big_centre = _pose((860, 440, 1060, 640), 0.98)   # 40000 px², centred
    out = pick_athlete([small_corner, big_centre], frame_w=1920, frame_h=1080)
    assert out is big_centre


def test_low_score_loses_to_high_score_when_size_close():
    a = _pose((100, 100, 500, 500), score=0.5)
    b = _pose((600, 100, 990, 490), score=0.99)
    out = pick_athlete([a, b], frame_w=1920, frame_h=1080)
    # b is comparable size but has higher detector confidence — should win.
    assert out is b
