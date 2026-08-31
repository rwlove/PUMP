"""Hardening contracts from the 2026-08 adversarial review:

  F2 — a half-open RTSP stream (reader parked in cap.read() forever) must force
       a reconnect within read_stale_seconds, not hang the pipeline silently.
  F1 — /healthz freshness gate: age of the freshest live-camera frame, ignoring
       file sources, so a wedged capture fails liveness instead of staying green.
  F6 — retention size cap: after the age sweep, oldest files are deleted until
       the directory is under the byte ceiling.
"""

from __future__ import annotations

import asyncio
import os
import time

import pytest

from pump_cv import healthd, retention
from pump_cv.pose import yolo

# ─── F2: capture stall watchdog ──────────────────────────────────────────


class StallCap:
    """cap.read() blocks for `stall_s` — emulates a half-open RTSP stream whose
    socket never returns. Without the watchdog the consumer waits on it forever."""

    def __init__(self, stall_s: float = 5.0):
        self._stall_s = stall_s

    def isOpened(self) -> bool:  # noqa: N802 — mirrors OpenCV
        return True

    def get(self, prop: int) -> float:
        return 30.0

    def read(self):
        time.sleep(self._stall_s)
        return False, None

    def release(self):
        pass


@pytest.mark.asyncio
async def test_stalled_capture_forces_reconnect_not_a_hang(monkeypatch):
    monkeypatch.setattr(yolo.cv2, "VideoCapture", lambda src: StallCap(stall_s=5.0))
    monkeypatch.setattr(yolo.YOLOPoseSource, "_ensure_model",
                        lambda self: setattr(self, "_model", object()))
    monkeypatch.setattr(yolo.Path, "is_file", lambda self: False)

    src = yolo.YOLOPoseSource(
        source="rtsp://fake", camera_name=f"cam-{time.time()}", model="",
        image_size=320, device="cpu", retry_on_failure=False,  # one cycle then return
        read_stale_seconds=0.1,
    )

    async def _drain():
        async for _ in src.poses():
            pass

    # With no frame ever produced, the watchdog must trip at ~0.1s and poses()
    # (retry off) returns. Without it, this awaits the 5s blocked read → timeout.
    await asyncio.wait_for(_drain(), timeout=1.5)


# ─── F1: /healthz freshness ───────────────────────────────────────────────


class FakeCamera:
    def __init__(self, ts, is_file=False):
        self._ts = ts
        self._is_file = is_file

    def last_frame_ts(self):
        return self._ts

    def is_file_source(self):
        return self._is_file


def test_freshest_age_ignores_file_sources_and_none(monkeypatch):
    now = time.time()
    cams = [
        FakeCamera(now - 100.0),          # live, stale
        FakeCamera(now - 2.0),            # live, fresh  ← should win
        FakeCamera(now - 0.5, is_file=True),  # file source, ignored
        FakeCamera(None),                 # never produced a frame, ignored
    ]
    monkeypatch.setattr(yolo, "registered_cameras", lambda: cams)
    age = healthd._freshest_live_frame_age()
    assert age is not None and 1.0 < age < 5.0, "must report the freshest LIVE frame"


def test_freshest_age_none_when_no_live_frames(monkeypatch):
    monkeypatch.setattr(yolo, "registered_cameras",
                        lambda: [FakeCamera(None), FakeCamera(time.time(), is_file=True)])
    assert healthd._freshest_live_frame_age() is None


# ─── F6: retention size cap ───────────────────────────────────────────────


def test_size_cap_deletes_oldest_until_under_ceiling(tmp_path):
    # Five 100-byte files, mtimes 5..1 days old (f0 oldest). Cap at 250 bytes →
    # keep the 2 newest (200 bytes), delete the 3 oldest.
    now = time.time()
    for i in range(5):
        f = tmp_path / f"f{i}.bin"
        f.write_bytes(b"x" * 100)
        os.utime(f, (now - (5 - i) * 86400, now - (5 - i) * 86400))

    # max_age huge so the age sweep deletes nothing; only the size cap acts.
    deleted = retention._sweep_dir(tmp_path, max_age_days=10_000, max_bytes=250)
    survivors = sorted(p.name for p in tmp_path.glob("*.bin"))
    assert deleted == 3
    assert survivors == ["f3.bin", "f4.bin"], "oldest deleted first, under the cap"


def test_size_cap_disabled_by_zero(tmp_path):
    f = tmp_path / "a.bin"
    f.write_bytes(b"x" * 1000)
    os.utime(f, (time.time(), time.time()))
    assert retention._sweep_dir(tmp_path, max_age_days=10_000, max_bytes=0) == 0
    assert f.exists()
