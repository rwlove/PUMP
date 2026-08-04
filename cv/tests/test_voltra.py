"""Voltra behaviour in pump-cv.

Which exercises use the trainer is a flag on the exercise row in PUMP, not a
guess from the name. Two behaviours follow from it:

  * sidecar off — the exercise is opaque to plate detection, so the set is
    written pending with weight=0 and the athlete types the resistance in;
  * sidecar on — pump-voltra owns the set outright and CV writes nothing,
    because it reads the real resistance and the device's own rep count.
"""

from __future__ import annotations

import pytest

from pump_cv.classify import ExercisePrototype, pose_sequence_to_features
from pump_cv.exercises import lookup
from pump_cv.pipeline import ExerciseSpec, PipelineRunner
from pump_cv.pose.mock import MockPoseSource
from pump_cv.pose.types import LEFT_ANKLE, LEFT_HIP, LEFT_KNEE
from pump_cv.voltra import VoltraFlags

EXERCISE = "Cable Row"
# The case that motivated replacing the name heuristic: "cable" in the name,
# but a plate stack, so CV must treat it as an ordinary lift.
UNFLAGGED_CABLE_EXERCISE = "Seated Cable Pulldown"


class _RecordingPump:
    def __init__(self, exercises: list[dict] | None = None):
        self.posts: list[dict] = []
        self._exercises = exercises or []

    async def post_set(self, payload):
        self.posts.append(payload)
        return len(self.posts)

    async def patch_set(self, *a, **kw): ...
    async def confirm_set(self, *a, **kw): ...
    async def delete_set(self, *a, **kw): ...

    async def list_exercises(self):
        return self._exercises


async def _collect_squat_poses():
    src = MockPoseSource(schedule=[("rep", 6.0)], fps=30, rep_period_s=2.0)
    out = []
    async for _frame, poses in src.poses():
        if poses:
            out.append(poses[0])
    return out


async def _run(pump, exercise_name, *, is_voltra_exercise, sidecar_enabled):
    poses = await _collect_squat_poses()
    proto = ExercisePrototype(exercise_name, pose_sequence_to_features(poses))
    src = MockPoseSource(schedule=[("rep", 10.0), ("rest", 30.0)], fps=30, rep_period_s=2.0)
    runner = PipelineRunner(
        pump=pump,  # type: ignore[arg-type]
        default_exercise=ExerciseSpec(exercise_name, LEFT_HIP, LEFT_KNEE, LEFT_ANKLE),
        prototypes=[proto],
        exercise_lookup=lookup,
        is_voltra_exercise=is_voltra_exercise,
        voltra_sidecar_enabled=sidecar_enabled,
        rep_amplitude_deg=20.0,
        rep_min_period_s=0.5,
        rep_smoothing_window=5,
        set_quiet_seconds=10.0,
    )
    await runner.run(src.poses())
    return runner


@pytest.mark.asyncio
async def test_flagged_set_is_pending_with_zero_weight_when_sidecar_is_off():
    pump = _RecordingPump()
    await _run(pump, EXERCISE, is_voltra_exercise=lambda n: True, sidecar_enabled=False)

    assert len(pump.posts) == 1
    p = pump.posts[0]
    assert p["Name"] == EXERCISE
    assert p["Weight"] == "0.0"
    assert p["Pending"] is True
    assert "Voltra" in p["Note"]


@pytest.mark.asyncio
async def test_flagged_set_is_not_written_when_the_sidecar_owns_it():
    # Writing here as well would log every Voltra set twice.
    pump = _RecordingPump()
    await _run(pump, EXERCISE, is_voltra_exercise=lambda n: True, sidecar_enabled=True)
    assert pump.posts == []


@pytest.mark.asyncio
async def test_unflagged_cable_exercise_takes_the_normal_path():
    """A cable-named exercise on a plate stack is an ordinary lift.

    This is exactly what a `"cable" in name` heuristic would get wrong.
    """
    pump = _RecordingPump()
    await _run(
        pump,
        UNFLAGGED_CABLE_EXERCISE,
        is_voltra_exercise=lambda n: False,
        sidecar_enabled=True,  # on, and still irrelevant: this isn't a Voltra exercise
    )

    assert len(pump.posts) == 1
    p = pump.posts[0]
    assert p["Name"] == UNFLAGGED_CABLE_EXERCISE
    assert p["Note"] == ""


@pytest.mark.asyncio
async def test_no_flag_source_means_nothing_is_voltra():
    pump = _RecordingPump()
    await _run(pump, EXERCISE, is_voltra_exercise=None, sidecar_enabled=True)
    assert len(pump.posts) == 1
    assert pump.posts[0]["Note"] == ""


@pytest.mark.asyncio
async def test_flags_come_from_the_exercise_row_not_the_name():
    pump = _RecordingPump(
        exercises=[
            {"ID": 1, "Name": "Cable Row", "Voltra": True},
            {"ID": 2, "Name": "Seated Cable Pulldown", "Voltra": False},
            {"ID": 3, "Name": "Bench Press", "Voltra": False},
        ]
    )
    flags = VoltraFlags(pump)
    await flags.refresh()

    assert flags("Cable Row")
    assert not flags("Seated Cable Pulldown")
    assert not flags("Bench Press")
    # sets.name is free text with no foreign key to exercises.
    assert flags("cable row")
    assert not flags(None)


@pytest.mark.asyncio
async def test_unflagging_takes_effect_on_refresh():
    pump = _RecordingPump(exercises=[{"ID": 1, "Name": "Cable Row", "Voltra": True}])
    flags = VoltraFlags(pump)
    await flags.refresh()
    assert flags("Cable Row")

    pump._exercises = [{"ID": 1, "Name": "Cable Row", "Voltra": False}]
    await flags.refresh()
    assert not flags("Cable Row")
