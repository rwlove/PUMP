"""Pose data types and the PoseSource async-iterator protocol.

Keypoint indices follow the standard COCO-17 layout used by both YOLOv8-
Pose and most pose estimators:

    0  nose
    1  left_eye        2  right_eye
    3  left_ear        4  right_ear
    5  left_shoulder   6  right_shoulder
    7  left_elbow      8  right_elbow
    9  left_wrist     10  right_wrist
   11  left_hip       12  right_hip
   13  left_knee      14  right_knee
   15  left_ankle     16  right_ankle

Tests and downstream code use the named constants below to stay
readable; never hard-code the integer indices.
"""

from __future__ import annotations

from collections.abc import AsyncIterator
from dataclasses import dataclass
from typing import Protocol, TypeAlias

import numpy as np


# Named indices into Pose.keypoints
NOSE = 0
LEFT_SHOULDER = 5
RIGHT_SHOULDER = 6
LEFT_ELBOW = 7
RIGHT_ELBOW = 8
LEFT_WRIST = 9
RIGHT_WRIST = 10
LEFT_HIP = 11
RIGHT_HIP = 12
LEFT_KNEE = 13
RIGHT_KNEE = 14
LEFT_ANKLE = 15
RIGHT_ANKLE = 16


@dataclass(frozen=True, slots=True)
class Keypoint:
    x: float           # pixels (image space)
    y: float           # pixels
    confidence: float  # 0..1


@dataclass(frozen=True, slots=True)
class Pose:
    """One person detected in one frame.

    bbox is (x1, y1, x2, y2) in pixel coordinates. score is the overall
    detector confidence for this person (separate from per-keypoint
    confidences). camera identifies which source produced this pose so
    multi-cam fusion can correlate later.
    """

    timestamp: float
    bbox: tuple[float, float, float, float]
    score: float
    keypoints: tuple[Keypoint, ...]
    camera: str = ""


FrameAndPoses: TypeAlias = tuple["np.ndarray | None", list[Pose]]


class PoseSource(Protocol):
    """A pose source yields (frame, poses) tuples over time.

    Each tuple is one frame of one camera plus all people detected in
    that frame. The frame is the raw BGR ndarray (used by downstream
    weight detection) or None when the source can't supply pixels (e.g.
    the synthetic mock). Multi-cam pipelines run one PoseSource per
    camera and interleave/triangulate downstream.
    """

    async def poses(self) -> AsyncIterator[FrameAndPoses]: ...
