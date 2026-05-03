"""Unit tests for the rep counter.

The counter is fed a pre-computed angle time-series so we don't need
real video or a pose model to exercise it.
"""

from __future__ import annotations

import math

from pump_cv.fsm import RepCounter, joint_angle


def _sinusoid(reps: int, period_s: float = 2.0, fps: float = 30.0,
              flexed_angle: float = 80.0, extended_angle: float = 180.0):
    """Generate a clean (angle, ts) stream with `reps` complete cycles."""
    samples = []
    n = int(reps * period_s * fps)
    amp = (extended_angle - flexed_angle) / 2
    mid = (extended_angle + flexed_angle) / 2
    for i in range(n):
        t = i / fps
        # cos starts at +amp (extended) → swings to -amp (flexed) once per period.
        a = mid + amp * math.cos(2 * math.pi * t / period_s)
        samples.append((a, t))
    return samples


def test_joint_angle_basic():
    # 90° angle: vertex at origin, arms along +x and +y.
    assert abs(joint_angle((1, 0), (0, 0), (0, 1)) - 90.0) < 1e-6
    # Straight line: 180°.
    assert abs(joint_angle((1, 0), (0, 0), (-1, 0)) - 180.0) < 1e-6
    # Degenerate (zero-length vector): 0°, no crash.
    assert joint_angle((0, 0), (0, 0), (1, 0)) == 0.0


def test_counts_clean_reps():
    rc = RepCounter(min_amplitude_deg=20, min_period_s=0.5, smoothing_window=5)
    for a, t in _sinusoid(reps=8, period_s=2.0):
        rc.push(a, t)
    # ±1 tolerance: smoothing-window warmup eats at most one cycle at the start.
    assert 7 <= rc.count <= 8


def test_ignores_subthreshold_wobbles():
    rc = RepCounter(min_amplitude_deg=30, min_period_s=0.5, smoothing_window=5)
    # 10° amplitude — well below 30° threshold.
    for a, t in _sinusoid(reps=5, period_s=2.0, flexed_angle=170, extended_angle=180):
        rc.push(a, t)
    assert rc.count == 0


def test_refractory_period_ignores_double_counts():
    rc = RepCounter(min_amplitude_deg=20, min_period_s=2.0, smoothing_window=5)
    # 5 reps over 5 seconds = 1 rep/s. With refractory at 2s, expect ≤ 3.
    for a, t in _sinusoid(reps=5, period_s=1.0):
        rc.push(a, t)
    assert rc.count <= 3


def test_reset_zeroes_state():
    rc = RepCounter(min_amplitude_deg=20, min_period_s=0.5, smoothing_window=5)
    for a, t in _sinusoid(reps=3, period_s=2.0):
        rc.push(a, t)
    assert 2 <= rc.count <= 3
    rc.reset()
    assert rc.count == 0
    for a, t in _sinusoid(reps=2, period_s=2.0):
        rc.push(a, t + 100)
    assert 1 <= rc.count <= 2
