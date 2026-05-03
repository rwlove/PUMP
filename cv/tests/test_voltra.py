"""Voltra-specific behaviour: any exercise whose name contains 'Voltra'
is treated as opaque-load — pump-cv writes pending=true with weight=0
and a note prompting the athlete to enter the resistance.
"""

from __future__ import annotations

import pytest

from pump_cv.classify import ExercisePrototype, pose_sequence_to_features
from pump_cv.exercises import lookup
from pump_cv.pipeline import ExerciseSpec, PipelineRunner
from pump_cv.pose.mock import MockPoseSource
from pump_cv.pose.types import LEFT_ANKLE, LEFT_HIP, LEFT_KNEE


class _RecordingPump:
    def __init__(self):
        self.posts: list[dict] = []

    async def post_set(self, payload):
        self.posts.append(payload)
        return len(self.posts)

    async def patch_set(self, *a, **kw): ...
    async def confirm_set(self, *a, **kw): ...
    async def delete_set(self, *a, **kw): ...
    async def list_exercises(self): return []


async def _collect_squat_poses():
    src = MockPoseSource(schedule=[("rep", 6.0)], fps=30, rep_period_s=2.0)
    out = []
    async for _frame, poses in src.poses():
        if poses:
            out.append(poses[0])
    return out


@pytest.mark.asyncio
async def test_voltra_set_is_pending_with_zero_weight():
    # Build a "Voltra Squat" prototype so the classifier picks that name.
    poses = await _collect_squat_poses()
    proto = ExercisePrototype("Voltra Squat", pose_sequence_to_features(poses))

    src = MockPoseSource(
        schedule=[("rep", 10.0), ("rest", 30.0)],
        fps=30,
        rep_period_s=2.0,
    )
    pump = _RecordingPump()
    runner = PipelineRunner(
        pump=pump,                                          # type: ignore[arg-type]
        default_exercise=ExerciseSpec("Voltra Squat", LEFT_HIP, LEFT_KNEE, LEFT_ANKLE),
        prototypes=[proto],
        exercise_lookup=lookup,
        rep_amplitude_deg=20.0,
        rep_min_period_s=0.5,
        rep_smoothing_window=5,
        set_quiet_seconds=10.0,
    )
    await runner.run(src.poses())

    assert len(pump.posts) == 1
    p = pump.posts[0]
    assert p["Name"] == "Voltra Squat"
    assert p["Weight"] == "0.0"
    assert p["Pending"] is True
    assert "Voltra" in p["Note"]
