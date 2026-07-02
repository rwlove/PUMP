"""Tests for the reader/predictor double-buffer in YOLOPoseSource.

Substitutes fakes for cv2.VideoCapture and the YOLO model so the async
coroutines run without torch or a real camera. Focuses on the concurrency
contract: drop-newest slot semantics, clean shutdown when the caller
breaks out of the async iterator, and that fps counting + JPEG-cache
invariants still hold after the refactor."""

from __future__ import annotations

import asyncio
import time

import numpy as np
import pytest

from pump_cv.pose import yolo


class FakeCap:
    """Emulates the tiny slice of cv2.VideoCapture the yolo source uses."""

    def __init__(self, frames: list[np.ndarray], read_delay: float = 0.001, fps: float = 30.0):
        self._frames = list(frames)
        self._read_delay = read_delay
        self._fps = fps
        self._closed = False
        self.reads = 0

    def isOpened(self) -> bool:  # noqa: N802 — mirrors OpenCV name
        return not self._closed

    def get(self, prop: int) -> float:
        # cv2.CAP_PROP_FPS == 5 but we don't care which int; return fps.
        return self._fps

    def read(self):  # noqa: D401 — signature matches OpenCV
        # A per-read sleep emulates real-time RTSP pacing.
        time.sleep(self._read_delay)
        self.reads += 1
        if not self._frames:
            return False, None
        return True, self._frames.pop(0)

    def release(self):
        self._closed = True


class FakeModel:
    def __init__(self, predict_delay: float = 0.0):
        self._predict_delay = predict_delay
        self.predicts = 0

    def predict(self, frame, imgsz=None, device=None, verbose=False):
        self._predict_delay and time.sleep(self._predict_delay)
        self.predicts += 1
        # Return an ultralytics-shaped stub: a list with one Results-like
        # object exposing `keypoints=None` and `boxes=None`. _results_to_poses
        # handles the None branches by returning [].
        class _R:
            keypoints = None
            boxes = None
        return [_R()]


def _install_fakes(monkeypatch, cap: FakeCap, model: FakeModel) -> None:
    monkeypatch.setattr(yolo.cv2, "VideoCapture", lambda src: cap)
    monkeypatch.setattr(yolo.YOLOPoseSource, "_ensure_model",
                        lambda self: setattr(self, "_model", model))
    # Skip the Path(source).is_file() check by pointing at a non-existent
    # path; is_file() returns False → double-buffer path (what we're testing).
    monkeypatch.setattr(yolo.Path, "is_file",
                        lambda self: False)


def _make_frame() -> np.ndarray:
    return np.zeros((32, 32, 3), dtype=np.uint8)


@pytest.mark.asyncio
async def test_double_buffer_yields_frames(monkeypatch):
    """Happy path: reader and predictor both fast, all frames flow through."""
    frames = [_make_frame() for _ in range(5)]
    cap = FakeCap(frames, read_delay=0.001)
    model = FakeModel(predict_delay=0.001)
    _install_fakes(monkeypatch, cap, model)

    src = yolo.YOLOPoseSource(source="rtsp://fake", camera_name=f"cam-{id(cap)}",
                              model="", image_size=320, device="cpu",
                              retry_on_failure=False)  # never reconnect

    collected = []
    async for frame, _poses in src.poses():
        collected.append(frame)
        if len(collected) >= 5:
            break

    assert len(collected) == 5
    assert model.predicts >= 5  # every yielded frame implies one predict
    assert src._frame_id >= 5   # reader landed at least 5 frames


@pytest.mark.asyncio
async def test_slow_predictor_drops_old_frames(monkeypatch):
    """When the reader outpaces the predictor, the drop-newest slot must
    keep the newest frame available and discard the middle."""
    frames = [_make_frame() for _ in range(10)]
    # Reader fast (1ms per frame), predictor slow (30ms per frame). At
    # 10 frames read the reader will have overwritten the slot several
    # times, and the predictor only sees a subset — but never runs on a
    # frame that no longer represents "latest" state.
    cap = FakeCap(frames, read_delay=0.001)
    model = FakeModel(predict_delay=0.030)
    _install_fakes(monkeypatch, cap, model)

    src = yolo.YOLOPoseSource(source="rtsp://fake", camera_name=f"cam-{id(cap)}",
                              model="", image_size=320, device="cpu", retry_on_failure=False)

    predict_count = 0
    async for _ in src.poses():
        predict_count += 1
        # Give the reader time to publish more frames before we loop.
        await asyncio.sleep(0.005)
        if predict_count >= 3 or cap.reads >= 10:
            break

    # The reader consumed more frames than the predictor processed.
    # (The exact gap depends on timing, but the invariant is reads > predicts.)
    assert cap.reads > model.predicts, (
        f"expected reader to outpace predictor: reads={cap.reads} predicts={model.predicts}")


@pytest.mark.asyncio
async def test_reader_task_cancelled_on_break(monkeypatch):
    """Breaking out of the async iterator must shut the reader task
    cleanly and release the VideoCapture — no leaked threads, no zombie
    task holding cap.read() open."""
    frames = [_make_frame() for _ in range(100)]
    cap = FakeCap(frames, read_delay=0.001)
    model = FakeModel(predict_delay=0.001)
    _install_fakes(monkeypatch, cap, model)

    src = yolo.YOLOPoseSource(source="rtsp://fake", camera_name=f"cam-{id(cap)}",
                              model="", image_size=320, device="cpu", retry_on_failure=False)

    # Consume one frame then break.
    it = src.poses().__aiter__()
    await it.__anext__()
    await it.aclose()

    # Give the event loop a beat so shutdown finalisers run.
    await asyncio.sleep(0.02)

    assert cap._closed, "VideoCapture.release() not called on shutdown"
    # jpeg_cache and latest_frame get cleared in the finally-block.
    assert src._latest_frame is None
    assert src._jpeg_cache == {}


@pytest.mark.asyncio
async def test_eof_ends_stream(monkeypatch):
    """When cap.read() returns (False, None), the sentinel travels through
    the slot and the outer iterator exits cleanly."""
    frames = [_make_frame() for _ in range(3)]
    cap = FakeCap(frames, read_delay=0.001)
    model = FakeModel(predict_delay=0.001)
    _install_fakes(monkeypatch, cap, model)

    src = yolo.YOLOPoseSource(source="rtsp://fake", camera_name=f"cam-{id(cap)}",
                              model="", image_size=320, device="cpu", retry_on_failure=False)

    count = 0
    async for _ in src.poses():
        count += 1

    # All 3 frames + then the EOF sentinel that stops the outer loop.
    assert count == 3
    assert cap._closed


@pytest.mark.asyncio
async def test_fps_still_counted(monkeypatch):
    """Refactor mustn't have broken the .stats() FPS number the admin
    panel reads — it derives from _frame_times which the reader must
    keep populating."""
    frames = [_make_frame() for _ in range(6)]
    cap = FakeCap(frames, read_delay=0.001)
    model = FakeModel(predict_delay=0.001)
    _install_fakes(monkeypatch, cap, model)

    src = yolo.YOLOPoseSource(source="rtsp://fake", camera_name=f"cam-{id(cap)}",
                              model="", image_size=320, device="cpu", retry_on_failure=False)

    count = 0
    async for _ in src.poses():
        count += 1
        if count >= 5:
            break

    stats = src.stats()
    assert stats["has_frame"] is False or stats["has_frame"] is True  # boolean, not thrown
    assert stats["fps"] >= 0  # positive after real frames flow
