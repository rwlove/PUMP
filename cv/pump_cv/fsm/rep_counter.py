"""Rep counter.

Strategy: per exercise, identify a "primary joint angle" whose flexion
cycles through a peak-and-valley once per rep (e.g. squat → knee, bench
press → elbow). Compute the angle each frame, smooth it over a short
window, and use peak detection to count reps.

This module is pure: angles in, rep count out. The exercise → joint
mapping lives in the pipeline glue, not here.
"""

from __future__ import annotations

import math
from collections import deque
from dataclasses import dataclass, field

from ..pose.types import Pose


@dataclass(frozen=True, slots=True)
class JointAngle:
    """A signed joint angle measurement with timestamp."""
    timestamp: float
    angle_deg: float


def joint_angle(a: tuple[float, float], b: tuple[float, float], c: tuple[float, float]) -> float:
    """Interior angle at vertex b formed by points a-b-c, in degrees (0..180).

    Used both with image-space pixel coordinates (acceptable, since
    angles are scale-invariant) and 3D-fused coordinates later.
    """
    v1 = (a[0] - b[0], a[1] - b[1])
    v2 = (c[0] - b[0], c[1] - b[1])
    n1 = math.hypot(*v1)
    n2 = math.hypot(*v2)
    if n1 == 0 or n2 == 0:
        return 0.0
    cosang = (v1[0] * v2[0] + v1[1] * v2[1]) / (n1 * n2)
    cosang = max(-1.0, min(1.0, cosang))
    return math.degrees(math.acos(cosang))


def keypoint_angle(pose: Pose, a_idx: int, b_idx: int, c_idx: int) -> float | None:
    """Joint angle at b_idx, computed from pose keypoints. Returns None
    if any of the three keypoints has near-zero confidence (occluded)."""
    a, b, c = pose.keypoints[a_idx], pose.keypoints[b_idx], pose.keypoints[c_idx]
    if a.confidence < 0.3 or b.confidence < 0.3 or c.confidence < 0.3:
        return None
    return joint_angle((a.x, a.y), (b.x, b.y), (c.x, c.y))


@dataclass
class RepCounter:
    """Streaming rep counter.

    Feed `push(angle_deg, ts_seconds)` once per frame. `count` is the
    running rep total since the last `reset()`. A "rep" is a complete
    cycle: extended → flexed → extended (one valley-and-recovery).

    Tunables match the YAML config (`rep:` section):
    - min_amplitude_deg: ignore wobbles smaller than this
    - min_period_s: ignore peaks closer in time than this
    - smoothing_window: moving-average length for the angle signal

    The detector is intentionally simple — peak detection over a smoothed
    one-dimensional signal. More sophisticated approaches (frequency
    analysis, ML-based) can replace it without changing the public API.
    """

    min_amplitude_deg: float = 25.0
    min_period_s: float = 0.6
    smoothing_window: int = 9

    _buffer: deque[JointAngle] = field(default_factory=deque)
    _smoothed_recent: deque[float] = field(default_factory=deque)
    _last_extreme_kind: str = "init"   # "peak" (extended) | "valley" (flexed)
    _last_extreme_angle: float = 180.0
    _last_rep_ts: float = -1e9
    count: int = 0

    def reset(self) -> None:
        self._buffer.clear()
        self._smoothed_recent.clear()
        self._last_extreme_kind = "init"
        self._last_extreme_angle = 180.0
        self._last_rep_ts = -1e9
        self.count = 0

    def push(self, angle_deg: float, ts: float) -> None:
        self._buffer.append(JointAngle(ts, angle_deg))
        if len(self._buffer) > self.smoothing_window:
            self._buffer.popleft()
        if len(self._buffer) < self.smoothing_window:
            return  # warming up

        # Smoothed value = mean of buffer.
        smoothed = sum(b.angle_deg for b in self._buffer) / len(self._buffer)
        center_ts = self._buffer[len(self._buffer) // 2].timestamp

        self._smoothed_recent.append(smoothed)
        if len(self._smoothed_recent) > 5:
            self._smoothed_recent.popleft()
        if len(self._smoothed_recent) < 3:
            return

        # Look at three consecutive smoothed samples for a local extremum.
        s = list(self._smoothed_recent)[-3:]
        prev, cur, nxt = s[0], s[1], s[2]
        if cur < prev and cur < nxt:
            self._maybe_record_extreme("valley", cur, center_ts)
        elif cur > prev and cur > nxt:
            self._maybe_record_extreme("peak", cur, center_ts)

    def _maybe_record_extreme(self, kind: str, angle: float, ts: float) -> None:
        """Update internal extreme state and count a rep on extended→flexed→extended."""
        if self._last_extreme_kind == "init":
            self._last_extreme_kind = kind
            self._last_extreme_angle = angle
            return

        # Only act when we alternate kinds.
        if kind == self._last_extreme_kind:
            # Same kind in a row — refine the extreme if more pronounced.
            if (kind == "valley" and angle < self._last_extreme_angle) or \
               (kind == "peak" and angle > self._last_extreme_angle):
                self._last_extreme_angle = angle
            return

        amplitude = abs(angle - self._last_extreme_angle)
        if amplitude < self.min_amplitude_deg:
            # Ignore wobbles. Keep the dominant extreme.
            self._last_extreme_kind = kind
            self._last_extreme_angle = angle
            return

        # We've seen a valid valley→peak or peak→valley.
        if kind == "peak":
            # extended → flexed → extended completes here: count a rep,
            # subject to refractory period.
            if ts - self._last_rep_ts >= self.min_period_s:
                self.count += 1
                self._last_rep_ts = ts

        self._last_extreme_kind = kind
        self._last_extreme_angle = angle
