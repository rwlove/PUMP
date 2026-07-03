"""Tests for the shared prototype-build core in pump_cv.classify.reference.

Both entry points (healthd `/api/v1/reference` and the
`add_reference` CLI) call `build_prototype_from_video` and translate
its failures. Verifying the core here catches breakage that would
otherwise show up in only one of them.
"""

from __future__ import annotations

from pathlib import Path

import numpy as np
import pytest

from pump_cv.classify import (
    ExercisePrototype,
    NoAthleteDetectedError,
    PrototypeStore,
    build_prototype_from_video,
)
from pump_cv.pose.types import Keypoint, Pose

# COCO-17 keypoint order — see pose/types.py.
_K = 17


def _fake_pose(t: float) -> Pose:
    """A pose with all keypoints at confidence 1.0 so pick_athlete keeps it."""
    kps = tuple(
        Keypoint(x=100.0 + t, y=200.0 + i * 5, confidence=1.0)
        for i in range(_K)
    )
    return Pose(
        timestamp=t,
        bbox=(90.0, 190.0, 120.0, 300.0),
        score=1.0,
        keypoints=kps,
    )


class _FakeSource:
    """Emulates YOLOPoseSource — just yields the pose sequence it was given."""

    def __init__(self, frames):
        self._frames = frames

    async def poses(self):
        for f in self._frames:
            yield None, f


@pytest.fixture
def stub_source(monkeypatch):
    """Replace YOLOPoseSource in classify.reference so tests don't load
    torch. The frames a test wants delivered land in `stub_source.frames`."""
    holder = {"frames": []}

    def _factory(**_kwargs):
        return _FakeSource(holder["frames"])

    from pump_cv.classify import reference as ref_mod
    monkeypatch.setattr(ref_mod, "YOLOPoseSource", _factory)
    return holder


def _touch_video(tmp_path: Path) -> Path:
    v = tmp_path / "clip.mp4"
    v.write_bytes(b"not a real video, is_file() just needs it to exist")
    return v


def test_builds_prototype_and_returns_path(tmp_path, stub_source):
    # 10 frames, one athlete each — enough to make a prototype.
    stub_source["frames"] = [[_fake_pose(float(t))] for t in range(10)]
    video = _touch_video(tmp_path)
    proto_dir = tmp_path / "protos"

    saved = build_prototype_from_video(video, "Squat", proto_dir)

    assert saved.exists()
    assert saved.suffix == ".npz"
    # Round-trip via PrototypeStore.load_all — proves the .npz + .json
    # pair the store expects were both written.
    protos = PrototypeStore(proto_dir).load_all()
    assert len(protos) == 1
    assert protos[0].exercise_name == "Squat"
    assert protos[0].source_clip == "clip.mp4"
    assert isinstance(protos[0].features, np.ndarray)


def test_missing_video_raises_file_not_found(tmp_path, stub_source):
    with pytest.raises(FileNotFoundError):
        build_prototype_from_video(
            tmp_path / "does-not-exist.mp4", "Squat", tmp_path / "protos")


def test_no_athlete_raises_dedicated_error(tmp_path, stub_source):
    # Empty per-frame lists — pick_athlete returns None each time so
    # nothing accumulates and the core raises the sentinel error rather
    # than saving an empty prototype.
    stub_source["frames"] = [[] for _ in range(5)]
    video = _touch_video(tmp_path)

    with pytest.raises(NoAthleteDetectedError):
        build_prototype_from_video(video, "Squat", tmp_path / "protos")


def test_no_athlete_error_is_exception_subclass():
    # Distinct type but must still inherit from Exception — some call
    # sites catch broadly and log. Static check, no fixture needed.
    assert issubclass(NoAthleteDetectedError, Exception)


def test_multiple_frames_land_in_feature_array(tmp_path, stub_source):
    # 20 frames → features array must have T=20 rows.
    stub_source["frames"] = [[_fake_pose(float(t))] for t in range(20)]
    video = _touch_video(tmp_path)

    build_prototype_from_video(video, "Bench Press", tmp_path / "protos")

    proto = PrototypeStore(tmp_path / "protos").load_all()[0]
    assert proto.features.shape[0] == 20
    assert isinstance(proto, ExercisePrototype)
