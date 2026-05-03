"""Tests for DTW + prototype classifier.

We use the synthetic mock pose source to produce two distinct exercise
patterns (a "squat" and a "still" pattern), build prototypes from one
sample of each, then classify a fresh sample of the squat and verify
the classifier picks squat with non-trivial confidence.
"""

from __future__ import annotations

import math

import numpy as np
import pytest

from pump_cv.classify import (
    ExercisePrototype,
    PrototypeStore,
    classify_window,
    pose_sequence_to_features,
)
from pump_cv.classify.dtw import dtw_distance
from pump_cv.pose.mock import MockPoseSource


def _collect_poses(source: MockPoseSource) -> list:
    """Drain the async pose stream into a flat list synchronously."""
    import asyncio

    async def _run():
        out = []
        async for _frame, poses in source.poses():
            if poses:
                out.append(poses[0])
        return out

    return asyncio.run(_run())


def test_dtw_self_distance_zero():
    a = np.array([[1.0, 2.0], [3.0, 4.0], [5.0, 6.0]], dtype=np.float32)
    assert dtw_distance(a, a) == pytest.approx(0.0, abs=1e-6)


def test_dtw_distinguishes_distinct_sequences():
    a = np.array([[0.0], [1.0], [2.0], [1.0], [0.0]], dtype=np.float32)
    b = np.array([[10.0], [11.0], [12.0], [11.0], [10.0]], dtype=np.float32)
    self_d = dtw_distance(a, a)
    cross_d = dtw_distance(a, b)
    assert cross_d > self_d


def test_classify_picks_correct_exercise():
    # Reference clips: 3 squat reps, then 3 seconds of standing still.
    squat_src = MockPoseSource(schedule=[("rep", 6.0)], fps=30, rep_period_s=2.0)
    still_src = MockPoseSource(schedule=[("rest", 6.0)], fps=30)

    squat_poses = _collect_poses(squat_src)
    still_poses = _collect_poses(still_src)

    squat_proto = ExercisePrototype("Squat", pose_sequence_to_features(squat_poses))
    still_proto = ExercisePrototype("Standing", pose_sequence_to_features(still_poses))

    # Test input: a fresh squat. Classifier should pick "Squat".
    test_src = MockPoseSource(schedule=[("rep", 6.0)], fps=30, rep_period_s=2.0)
    test_poses = _collect_poses(test_src)
    test_feats = pose_sequence_to_features(test_poses)

    result = classify_window(test_feats, [squat_proto, still_proto])
    assert result is not None
    assert result.name == "Squat"
    assert result.confidence > 0.5


def test_prototype_store_roundtrip(tmp_path):
    store = PrototypeStore(tmp_path / "protos")
    feats = np.random.RandomState(0).rand(60, 6).astype(np.float32)
    proto = ExercisePrototype("Bench Press", feats, source_clip="bench-001.mp4")
    path = store.add(proto)
    assert path.exists()

    loaded = store.load_all()
    assert len(loaded) == 1
    assert loaded[0].exercise_name == "Bench Press"
    assert loaded[0].source_clip == "bench-001.mp4"
    np.testing.assert_array_equal(loaded[0].features, feats)


def test_classify_returns_none_on_empty_inputs():
    assert classify_window(np.zeros((0, 6), dtype=np.float32), []) is None
    proto = ExercisePrototype("X", np.ones((10, 6), dtype=np.float32))
    assert classify_window(np.zeros((0, 6), dtype=np.float32), [proto]) is None


# Suppress an unused-import warning when run alone — math is used by other tests.
_ = math
