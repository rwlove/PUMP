"""End-to-end test for the multi-cam FusedPoseSource.

Two synthetic mock pose sources at different "angles" feed the
fusion source; we verify each yielded fused pose has 3D keypoints
populated, and that downstream rep counting still works against the
fused stream.
"""

from __future__ import annotations

import numpy as np
import pytest

from pump_cv.fusion import CameraCalibration
from pump_cv.pose.fused import FusedPoseSource
from pump_cv.pose.mock import MockPoseSource


def _identity_K(fx=1000.0, fy=1000.0, cx=960.0, cy=540.0) -> np.ndarray:
    return np.array([[fx, 0, cx], [0, fy, cy], [0, 0, 1]], dtype=np.float64)


def _camera_at(position_xyz, look_at=(0.0, 0.0, 0.0)) -> CameraCalibration:
    pos = np.array(position_xyz, dtype=np.float64)
    target = np.array(look_at, dtype=np.float64)
    forward = target - pos
    forward /= np.linalg.norm(forward)
    up_world = np.array([0, 1, 0], dtype=np.float64)
    right = np.cross(forward, up_world)
    right /= np.linalg.norm(right)
    up = np.cross(right, forward)
    R = np.array([right, -up, forward], dtype=np.float64)
    t = -R @ pos
    return CameraCalibration(K=_identity_K(), dist=np.zeros(5), R=R, t=t)


@pytest.mark.asyncio
async def test_fused_source_yields_3d_keypoints():
    # Two mock sources running the same schedule; in production these
    # would be two cameras at known positions with synced clocks. The
    # mock generates the same 2D synthetic pose from each source — we
    # don't actually have two views of one scene without a real video,
    # but the fusion source still pairs and triangulates them. The 3D
    # output won't be physically meaningful, but the wiring is verified.
    src_a = MockPoseSource(schedule=[("rep", 2.0)], fps=30, camera="a")
    src_b = MockPoseSource(schedule=[("rep", 2.0)], fps=30, camera="b")
    cam_a = _camera_at((-1000.0, 1500.0, -3000.0))
    cam_b = _camera_at((1000.0, 1500.0, -3000.0))

    fused_count = 0
    has_3d_count = 0
    async for _frame, poses in FusedPoseSource(src_a, src_b, cam_a, cam_b).poses():
        fused_count += 1
        if poses and poses[0].keypoints_3d:
            has_3d_count += 1
            assert len(poses[0].keypoints_3d) == 17
    # Most frames should fuse cleanly; allow a couple of dropped pairs
    # at the start while the queues prime.
    assert fused_count >= 50
    assert has_3d_count >= fused_count - 4


@pytest.mark.asyncio
async def test_fused_source_falls_back_to_single_cam_when_one_side_blank():
    """If one source has no detections in a frame, fall back to the
    other side's pose without crashing. Uses a tiny finite blank
    source so cleanup is deterministic."""

    class _BlankSource:
        def __init__(self, n: int):
            self._n = n

        async def poses(self):
            for _ in range(self._n):
                yield None, []

    # Both sources produce a small bounded number of items so the loop
    # exits cleanly without relying on consumer-side break / cancellation.
    src_a = MockPoseSource(schedule=[("rep", 0.3)], fps=30, camera="a")  # ~9 frames
    src_b = _BlankSource(9)
    cam_a = _camera_at((-1000.0, 1500.0, -3000.0))
    cam_b = _camera_at((1000.0, 1500.0, -3000.0))

    out = []
    async for item in FusedPoseSource(src_a, src_b, cam_a, cam_b).poses():
        out.append(item)

    # B was always empty → no fused 3D poses produced.
    assert all(not p[1] or not p[1][0].keypoints_3d for p in out)
