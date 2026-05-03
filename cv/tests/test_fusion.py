"""Tests for multi-cam triangulation.

Strategy: place two synthetic cameras at known positions, project a
known set of 3D points into each, then triangulate and verify we
recover the original 3D points to within sub-millimetre.
"""

from __future__ import annotations

import numpy as np

from pump_cv.fusion import (
    CameraCalibration,
    make_pose_from_3d,
    triangulate_pose,
)


def _identity_intrinsics(fx=1000.0, fy=1000.0, cx=960.0, cy=540.0) -> np.ndarray:
    return np.array([[fx, 0, cx], [0, fy, cy], [0, 0, 1]], dtype=np.float64)


def _camera_at(position_xyz, look_at=(0.0, 0.0, 0.0)):
    """Build a CameraCalibration with the camera at `position_xyz` looking
    at `look_at` (right-handed, +z forward)."""
    pos = np.array(position_xyz, dtype=np.float64)
    target = np.array(look_at, dtype=np.float64)
    forward = target - pos
    forward /= np.linalg.norm(forward)
    up_world = np.array([0, 1, 0], dtype=np.float64)
    right = np.cross(forward, up_world)
    right /= np.linalg.norm(right)
    up = np.cross(right, forward)
    # Camera frame: x=right, y=down, z=forward → row-major
    R = np.array([right, -up, forward], dtype=np.float64)
    t = -R @ pos
    return CameraCalibration(K=_identity_intrinsics(), dist=np.zeros(5), R=R, t=t)


def test_triangulation_recovers_known_3d_points():
    cam_a = _camera_at((-1000.0, 1500.0, -3000.0))
    cam_b = _camera_at((1000.0, 1500.0, -3000.0))

    # 17 arbitrary 3D points loosely arranged like a person.
    truth = [
        (0, 1700, 0),     # nose
        (-50, 1720, 0), (50, 1720, 0),       # eyes
        (-80, 1700, 0), (80, 1700, 0),       # ears
        (-150, 1500, 0), (150, 1500, 0),     # shoulders
        (-180, 1300, 0), (180, 1300, 0),     # elbows
        (-200, 1100, 0), (200, 1100, 0),     # wrists
        (-100, 1000, 0), (100, 1000, 0),     # hips
        (-100, 600, 0), (100, 600, 0),       # knees
        (-100, 100, 0), (100, 100, 0),       # ankles
    ]

    pose_a = make_pose_from_3d(truth, cam_a, camera="A")
    pose_b = make_pose_from_3d(truth, cam_b, camera="B")

    fused = triangulate_pose(pose_a, pose_b, cam_a, cam_b)
    assert len(fused) == len(truth)
    for k3, ground in zip(fused, truth, strict=False):
        assert k3.confidence > 0
        d = np.linalg.norm(np.array([k3.x, k3.y, k3.z]) - np.array(ground))
        assert d < 1.0, f"fused {k3} too far from {ground} (d={d})"


def test_low_confidence_keypoints_skipped():
    from pump_cv.pose.types import Keypoint, Pose

    cam_a = _camera_at((-1000.0, 1500.0, -3000.0))
    cam_b = _camera_at((1000.0, 1500.0, -3000.0))

    # Pose A has high confidence everywhere; B has the first keypoint occluded.
    pose_a = make_pose_from_3d([(0, 0, 0)] * 17, cam_a)
    bad_kps = (Keypoint(0, 0, 0.0),) + tuple(Keypoint(0, 0, 0.9) for _ in range(16))
    pose_b = Pose(timestamp=0.0, bbox=(0, 0, 1, 1), score=0.99, keypoints=bad_kps)
    fused = triangulate_pose(pose_a, pose_b, cam_a, cam_b)
    assert fused[0].confidence == 0.0
    for k in fused[1:]:
        assert k.confidence > 0
