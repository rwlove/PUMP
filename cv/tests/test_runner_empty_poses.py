"""Behaviour when nobody is in frame.

YOLO yields an empty pose list for every frame with no person in view, and the
runner used to report that as timestamp 0.0. Nothing covered this path — the
mock source emits a pose on every frame including "rest" — which is how two
silent failures shipped: the last set of a workout never closed, and the kiosk
could never be told to sleep.
"""

from __future__ import annotations

import pytest

from pump_cv.fsm.set_boundary import SetState
from pump_cv.pipeline import ExerciseSpec, PipelineRunner
from pump_cv.pose.types import LEFT_ANKLE, LEFT_HIP, LEFT_KNEE, Keypoint, Pose


class _RecordingPump:
    def __init__(self):
        self.posts: list[dict] = []
        self.wakes = 0
        self.sleeps = 0

    async def post_set(self, payload):
        self.posts.append(payload)
        return len(self.posts)

    async def patch_set(self, *a, **kw): ...
    async def confirm_set(self, *a, **kw): ...
    async def delete_set(self, *a, **kw): ...
    async def list_exercises(self): return []
    async def post_wall_wake(self): self.wakes += 1
    async def post_wall_sleep(self): self.sleeps += 1


class FakeClock:
    """A clock the test advances by hand."""

    def __init__(self, t: float = 1_000_000.0):
        self.t = t

    def __call__(self) -> float:
        return self.t


def a_pose(ts: float, knee_angle_open: bool) -> Pose:
    """A pose whose knee angle alternates, so reps can register."""
    kps = [Keypoint(x=0.0, y=0.0, confidence=0.0) for _ in range(17)]
    kps[LEFT_HIP] = Keypoint(x=0.0, y=0.0, confidence=0.9)
    kps[LEFT_KNEE] = Keypoint(x=0.0, y=100.0, confidence=0.9)
    kps[LEFT_ANKLE] = (
        Keypoint(x=0.0, y=200.0, confidence=0.9) if knee_angle_open
        else Keypoint(x=90.0, y=120.0, confidence=0.9)
    )
    return Pose(
        timestamp=ts,
        bbox=(200.0, 30.0, 440.0, 620.0),
        score=0.99,
        keypoints=tuple(kps),
    )


def build(pump, clock, quiet=5.0, sleep_after=3.0):
    return PipelineRunner(
        pump=pump,                                  # type: ignore[arg-type]
        default_exercise=ExerciseSpec("Squat", LEFT_HIP, LEFT_KNEE, LEFT_ANKLE),
        rep_amplitude_deg=20.0,
        set_quiet_seconds=quiet,
        sleep_after_absent_seconds=sleep_after,
        wake_after_present_seconds=0.0,
        clock=clock,
    )


async def feed(runner, items):
    async def gen():
        for frame, poses in items:
            yield frame, poses
    await runner.run(gen())


@pytest.mark.asyncio
async def test_time_advances_while_nobody_is_in_frame():
    """The pose clock must keep moving on empty frames.

    It used to read 0.0, which is ~1.7e9 seconds *before* the last rep, so the
    set-close condition (ts - last_rep_at >= quiet) could never be true and the
    final set of a workout stayed open indefinitely.
    """
    clock = FakeClock()
    runner = build(_RecordingPump(), clock)

    runner._last_pose_ts = 500.0
    runner._last_pose_wall = clock.t

    assert runner._now([a_pose(501.0, True)]) == 501.0

    clock.t += 30.0
    now = runner._now([])
    assert now > 501.0, "clock went backwards when the frame emptied"
    assert now == pytest.approx(531.0), "empty frames must advance on the pose scale"


@pytest.mark.asyncio
async def test_first_ever_frame_without_poses_does_not_return_zero():
    clock = FakeClock()
    runner = build(_RecordingPump(), clock)
    assert runner._now([]) == clock.t


@pytest.mark.asyncio
async def test_kiosk_sleeps_once_the_athlete_leaves():
    """POST /api/wall/sleep was unreachable.

    _absent_since was set to 0.0 and every later absent frame also read 0.0, so
    the elapsed time was permanently zero. The kiosk woke on the first
    detection after pod start and never dimmed again.
    """
    clock = FakeClock()
    pump = _RecordingPump()
    runner = build(pump, clock, sleep_after=3.0)

    # Present, so the kiosk wakes. The first frame only starts the timer; the
    # wake fires on a later one.
    await runner._update_presence(True, runner._now([a_pose(100.0, True)]))
    clock.t += 1.0
    await runner._update_presence(True, runner._now([a_pose(101.0, True)]))
    assert pump.wakes == 1

    # Gone. First absent frame starts the timer, a later one must trip it.
    clock.t += 1.0
    await runner._update_presence(False, runner._now([]))
    assert pump.sleeps == 0, "slept immediately"

    clock.t += 10.0
    await runner._update_presence(False, runner._now([]))
    assert pump.sleeps == 1, "kiosk never slept after the athlete left"


@pytest.mark.asyncio
async def test_pose_buffer_is_bounded_while_someone_lingers():
    """Standing in frame without lifting must not grow the buffer forever.

    Cardio in view of the camera used to append a pose every frame with nothing
    ever clearing it, because only a completed set reset the buffer.
    """
    clock = FakeClock()
    runner = build(_RecordingPump(), clock, quiet=5.0)
    window = runner._pose_window_s

    # Twenty minutes of someone visible but never completing a set.
    ts = 1000.0
    for i in range(4000):
        ts += 0.3
        runner._pose_buffer.append(a_pose(ts, i % 2 == 0))
        runner._trim_pose_buffer(ts)

    assert len(runner._pose_buffer) > 0
    span = runner._pose_buffer[-1].timestamp - runner._pose_buffer[0].timestamp
    assert span <= window + 1.0, f"buffer spans {span:.0f}s, window is {window:.0f}s"


@pytest.mark.asyncio
async def test_clip_buffer_is_not_overwritten_by_the_rest_period():
    """The clip must show the set, not the 25 s of standing around after it.

    The buffer holds 8 s; the set does not close until the quiet period
    elapses, so sampling through the rest turned the deque over several times
    and every clip captured the rest instead.
    """
    clock = FakeClock()
    runner = build(_RecordingPump(), clock, quiet=25.0)
    assert runner._clip_buffer is None or runner._fsm.state == SetState.IDLE

    # Whatever the buffer does, sampling must be gated on not-RESTING.
    runner._fsm.state = SetState.RESTING
    pushed = []
    if runner._clip_buffer is not None:
        runner._clip_buffer.push = lambda f, t: pushed.append(t)  # type: ignore[method-assign]

    async def gen():
        yield None, []

    await runner.run(gen())
    assert pushed == [], "kept sampling the clip buffer through the rest period"
