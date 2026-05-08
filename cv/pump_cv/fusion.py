"""Multi-camera fusion via DLT triangulation.

Given two calibrated cameras observing the same person, lift each
keypoint from 2D pixel coords to a 3D world point. The 3D pose is more
robust to camera-specific occlusion than either 2D pose alone, and
joint angles computed from 3D coordinates are scale-invariant and
viewpoint-independent.

This module is purely numerical: feed it two Pose objects and the
calibration data, get a list of 3D keypoints back. Calibration loading
lives in pump_cv.calibration.
"""

from __future__ import annotations

from dataclasses import dataclass

import cv2
import numpy as np

from .pose.types import Keypoint, Keypoint3D, Pose


@dataclass(frozen=True, slots=True)
class CameraCalibration:
    """Per-camera intrinsics + extrinsics in a shared world frame.

    K is the 3×3 intrinsic matrix; dist is the OpenCV distortion vector
    (5 elements: k1 k2 p1 p2 k3); R is the 3×3 rotation from world to
    camera frame; t is the 3-vector translation (world origin in camera
    coordinates).
    """

    K: np.ndarray
    dist: np.ndarray
    R: np.ndarray
    t: np.ndarray

    def projection_matrix(self) -> np.ndarray:
        """Return the 3×4 projection matrix P = K [R | t]."""
        Rt = np.hstack([self.R, self.t.reshape(3, 1)])
        return self.K @ Rt


def triangulate_pose(
    pose_a: Pose, pose_b: Pose,
    cam_a: CameraCalibration, cam_b: CameraCalibration,
    min_keypoint_confidence: float = 0.3,
) -> list[Keypoint3D]:
    """Lift a 17-keypoint 2D pose pair to a 17-element 3D keypoint list.

    Keypoints with low confidence in either view are returned with
    confidence 0 (so downstream code can skip them).

    Coordinates assume both poses come from frames captured at the same
    instant. Time-sync is the caller's job.
    """
    if len(pose_a.keypoints) != len(pose_b.keypoints):
        raise ValueError("triangulate_pose: pose keypoint counts differ")

    P_a = cam_a.projection_matrix()
    P_b = cam_b.projection_matrix()

    pts_a = []
    pts_b = []
    keep = []
    for _i, (ka, kb) in enumerate(zip(pose_a.keypoints, pose_b.keypoints, strict=False)):
        if ka.confidence < min_keypoint_confidence or kb.confidence < min_keypoint_confidence:
            keep.append(False)
            pts_a.append([0.0, 0.0])
            pts_b.append([0.0, 0.0])
            continue
        keep.append(True)
        pts_a.append([ka.x, ka.y])
        pts_b.append([kb.x, kb.y])

    arr_a = np.array(pts_a, dtype=np.float64).T  # 2 × N
    arr_b = np.array(pts_b, dtype=np.float64).T  # 2 × N

    # cv2.triangulatePoints returns 4 × N homogeneous; normalise.
    pts_h = cv2.triangulatePoints(P_a, P_b, arr_a, arr_b)
    pts_3d = (pts_h[:3] / pts_h[3:4]).T  # N × 3

    out: list[Keypoint3D] = []
    for i, (xyz, k) in enumerate(zip(pts_3d, keep, strict=False)):
        if not k:
            out.append(Keypoint3D(0.0, 0.0, 0.0, 0.0))
            continue
        ka, kb = pose_a.keypoints[i], pose_b.keypoints[i]
        out.append(Keypoint3D(
            float(xyz[0]), float(xyz[1]), float(xyz[2]),
            min(ka.confidence, kb.confidence),
        ))
    return out


def project_point(world_xyz: tuple[float, float, float], cam: CameraCalibration) -> tuple[float, float]:
    """Project a single 3D world point to 2D pixel coordinates. Used in
    tests to synthesise input poses from known 3D ground truth."""
    X = np.array([*world_xyz, 1.0]).reshape(4, 1)
    p = cam.projection_matrix() @ X
    return float(p[0, 0] / p[2, 0]), float(p[1, 0] / p[2, 0])


def make_pose_from_3d(
    points_3d: list[tuple[float, float, float]],
    cam: CameraCalibration,
    timestamp: float = 0.0,
    score: float = 0.99,
    confidence: float = 0.95,
    camera: str = "",
) -> Pose:
    """Synthesise a 2D Pose from a list of 3D world points and a calibrated
    camera. Intended for fusion tests; not for production use."""
    kps = []
    for x, y, z in points_3d:
        u, v = project_point((x, y, z), cam)
        kps.append(Keypoint(u, v, confidence))
    xs = [k.x for k in kps]
    ys = [k.y for k in kps]
    bbox = (min(xs), min(ys), max(xs), max(ys))
    return Pose(timestamp=timestamp, bbox=bbox, score=score, keypoints=tuple(kps), camera=camera)
