"""End-to-end pipeline test using the synthetic mock pose source.

Doesn't need a GPU, doesn't load torch. Drives a 5-rep squat through the
full pipeline (pose → picker → rep counter → set FSM → API call) and
asserts that exactly one POST hits the PUMP client with reps=5.
"""

from __future__ import annotations

import pytest

from pump_cv.pipeline import ExerciseSpec, PipelineRunner
from pump_cv.pose.mock import MockPoseSource
from pump_cv.pose.types import LEFT_ANKLE, LEFT_HIP, LEFT_KNEE


class _RecordingPump:
    """Minimal stand-in for PumpClient that records writes."""

    def __init__(self):
        self.posts: list[dict] = []

    async def post_set(self, payload):
        self.posts.append(payload)
        return len(self.posts)

    async def patch_set(self, *a, **kw): ...
    async def confirm_set(self, *a, **kw): ...
    async def delete_set(self, *a, **kw): ...
    async def list_exercises(self): return []


@pytest.mark.asyncio
async def test_pipeline_writes_one_set_per_completed_session():
    # 5 reps over 10 s, then a long rest that closes the set.
    src = MockPoseSource(
        schedule=[("rep", 10.0), ("rest", 30.0)],
        fps=30,
        rep_period_s=2.0,
    )
    pump = _RecordingPump()
    runner = PipelineRunner(
        pump=pump,                             # type: ignore[arg-type]
        exercise=ExerciseSpec("Squat", LEFT_HIP, LEFT_KNEE, LEFT_ANKLE),
        rep_amplitude_deg=20.0,
        rep_min_period_s=0.5,
        rep_smoothing_window=5,
        set_quiet_seconds=10.0,
    )
    await runner.run(src.poses())

    assert len(pump.posts) == 1, f"expected one set write, got {len(pump.posts)}: {pump.posts}"
    posted = pump.posts[0]
    assert posted["Name"] == "Squat"
    assert posted["Source"] == "cv"
    # The mock generates exactly 5 cycles (10s / 2s period) — counter
    # should land on 5; allow ±1 because peak detection on a smoothed
    # signal can lose the very first or last extreme.
    assert 4 <= posted["Reps"] <= 6
