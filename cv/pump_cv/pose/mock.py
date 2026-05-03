"""Synthetic pose source.

Used by unit tests and by anyone without GPU access. Generates a single-
person Pose per frame whose joint angles trace a configurable repeating
pattern — used to exercise the rep counter and set FSM end-to-end without
ever loading torch.
"""

from __future__ import annotations

import math
from collections.abc import AsyncIterator, Sequence

from .types import Keypoint, Pose


# Skeleton topology used to back-compute (x, y) keypoint positions from
# joint angles. We model a side-on view of a squat: hips at a fixed
# height, knees flex/extend through the rep cycle, ankles fixed.
_REST_NOSE = (320.0, 50.0)
_REST_HIP = (320.0, 350.0)
_REST_ANKLE = (320.0, 600.0)
_HIP_TO_KNEE = 130.0
_KNEE_TO_ANKLE = 130.0


def _squat_keypoints(knee_angle_deg: float) -> tuple[Keypoint, ...]:
    """Build a 17-keypoint COCO list for a side-on squat at the given knee angle.

    knee_angle_deg = 180 means fully extended (standing); smaller = deeper.
    Side-on view: hips drop and knees travel forward (smaller x) as the
    athlete descends. Keypoint positions are constructed so that the
    *actual* joint angle at the knee — computed from (hip, knee, ankle)
    pixel coordinates — matches the requested knee_angle_deg.
    """
    rad = math.radians(knee_angle_deg)
    # Drop hip vertically as the knee flexes.
    flex = 1.0 - math.sin(rad / 2)            # 0 when extended, ≈0.29 at 80°
    hip_y = _REST_HIP[1] - _HIP_TO_KNEE * flex  # hip *rises* relative to ankle? No — drops.
    hip_y = _REST_HIP[1] + _HIP_TO_KNEE * flex * 0.6
    # Place knee so the thigh and shin form the requested angle.
    # Thigh goes from hip down-forward to knee; shin from knee down-back to ankle.
    half = (math.pi - rad) / 2  # half the supplement
    forward_offset = _HIP_TO_KNEE * math.sin(half)
    knee_x = _REST_HIP[0] - forward_offset    # knee tracks forward (–x) when flexed
    knee_y = _REST_ANKLE[1] - _KNEE_TO_ANKLE * math.cos(half)

    nose = Keypoint(_REST_NOSE[0], hip_y - 250, 0.95)
    l_sh = Keypoint(310.0, hip_y - 150, 0.95)
    r_sh = Keypoint(330.0, hip_y - 150, 0.95)
    l_el = Keypoint(295.0, hip_y - 80, 0.9)
    r_el = Keypoint(345.0, hip_y - 80, 0.9)
    l_wr = Keypoint(285.0, hip_y - 20, 0.85)
    r_wr = Keypoint(355.0, hip_y - 20, 0.85)
    l_hp = Keypoint(_REST_HIP[0], hip_y, 0.97)
    r_hp = Keypoint(_REST_HIP[0] + 20, hip_y, 0.97)
    l_kn = Keypoint(knee_x, knee_y, 0.95)
    r_kn = Keypoint(knee_x + 20, knee_y, 0.95)
    l_an = Keypoint(_REST_HIP[0], _REST_ANKLE[1], 0.95)
    r_an = Keypoint(_REST_HIP[0] + 20, _REST_ANKLE[1], 0.95)
    l_eye = Keypoint(315.0, hip_y - 260, 0.9)
    r_eye = Keypoint(325.0, hip_y - 260, 0.9)
    l_ear = Keypoint(308.0, hip_y - 252, 0.85)
    r_ear = Keypoint(332.0, hip_y - 252, 0.85)

    return (nose, l_eye, r_eye, l_ear, r_ear, l_sh, r_sh, l_el, r_el,
            l_wr, r_wr, l_hp, r_hp, l_kn, r_kn, l_an, r_an)


class MockPoseSource:
    """Yields synthetic squat poses driven by a programmable schedule.

    schedule: a sequence of (kind, duration_seconds) pairs.
        kind = "rep"  → one squat cycle (extended → flexed → extended)
               "rest" → still, fully extended
        Per-frame timestamps advance by 1/fps regardless of kind.
    """

    def __init__(
        self,
        schedule: Sequence[tuple[str, float]],
        fps: float = 30.0,
        rep_period_s: float = 2.0,
        camera: str = "mock",
    ):
        self._schedule = list(schedule)
        self._fps = fps
        self._rep_period = rep_period_s
        self._camera = camera

    async def poses(self) -> AsyncIterator[list[Pose]]:
        ts = 0.0
        dt = 1.0 / self._fps
        for kind, duration in self._schedule:
            n_frames = max(1, int(duration * self._fps))
            for i in range(n_frames):
                if kind == "rep":
                    # Sinusoidal flexion: 180° → 80° → 180° per cycle.
                    phase = (i * dt) % self._rep_period / self._rep_period
                    angle = 180.0 - 100.0 * math.sin(math.pi * phase)
                else:
                    angle = 180.0
                kps = _squat_keypoints(angle)
                bbox = (200.0, 30.0, 440.0, 620.0)
                yield [Pose(
                    timestamp=ts,
                    bbox=bbox,
                    score=0.99,
                    keypoints=kps,
                    camera=self._camera,
                )]
                ts += dt
