"""Tests for the per-frame JPEG-encode cache on YOLOPoseSource.

Constructs the source without touching YOLO/torch by monkey-patching
the model-load path — the cache lives entirely on the source object and
never invokes the model."""

from __future__ import annotations

import cv2
import numpy as np
import pytest

from pump_cv.pose import yolo


@pytest.fixture
def src() -> yolo.YOLOPoseSource:
    """A YOLOPoseSource with the model constructor bypassed so tests
    don't need torch. The JPEG cache uses only ._latest_frame /
    ._frame_id, both plain attributes."""
    original = yolo.YOLOPoseSource._ensure_model
    yolo.YOLOPoseSource._ensure_model = lambda self: None
    try:
        s = yolo.YOLOPoseSource(source="unused", camera_name=f"testcam-{id(object())}",
                                model="", image_size=640, device="cpu")
    finally:
        yolo.YOLOPoseSource._ensure_model = original
    return s


def _bgr(color: tuple[int, int, int]) -> np.ndarray:
    frame = np.zeros((32, 32, 3), dtype=np.uint8)
    frame[:] = color
    return frame


def test_returns_none_before_any_frame(src):
    assert src.latest_jpeg(75) is None


def test_encodes_once_per_frame(src):
    src._latest_frame = _bgr((10, 20, 30))
    src._frame_id = 1
    a = src.latest_jpeg(75)
    assert a is not None and a.startswith(b"\xff\xd8")  # SOI marker
    # Second call at the same frame_id must hit the cache.
    b = src.latest_jpeg(75)
    assert b is a  # same bytes object, not a re-encode


def test_new_frame_invalidates_cache(src):
    src._latest_frame = _bgr((10, 20, 30))
    src._frame_id = 1
    first = src.latest_jpeg(75)
    src._latest_frame = _bgr((200, 200, 200))
    src._frame_id = 2
    second = src.latest_jpeg(75)
    assert second is not None
    assert second is not first
    assert second != first  # different pixels → different bytes


def test_different_quality_re_encodes(src):
    src._latest_frame = _bgr((100, 150, 200))
    src._frame_id = 5
    lo = src.latest_jpeg(30)
    hi = src.latest_jpeg(90)
    # Both cached under their own quality but the cache only keeps the
    # last entry — the higher-quality encode should be bigger.
    assert lo != hi
    assert len(hi) > len(lo)


def test_disconnect_drops_cache(src):
    src._latest_frame = _bgr((0, 0, 0))
    src._frame_id = 1
    assert src.latest_jpeg(75) is not None
    # Simulate the capture loop's finally-block: frame vanishes, cache
    # is dropped so healthd returns 404 instead of stale JPEG.
    src._latest_frame = None
    src._jpeg_cache = {}
    assert src.latest_jpeg(75) is None


def test_no_encode_when_frame_is_none(src):
    src._latest_frame = None
    src._frame_id = 0
    assert src.latest_jpeg(75) is None
    # And the cache stays empty (no bogus {(0,75): b''} entry).
    assert src._jpeg_cache == {}


def test_decodes_to_same_pixels(src):
    """Sanity: what we cache actually round-trips through cv2 decode."""
    original = _bgr((123, 45, 67))
    src._latest_frame = original
    src._frame_id = 1
    data = src.latest_jpeg(95)
    decoded = cv2.imdecode(np.frombuffer(data, dtype=np.uint8), cv2.IMREAD_COLOR)
    assert decoded is not None
    assert decoded.shape == original.shape
    # Lossy JPEG: mean pixel error at q=95 is tiny.
    assert np.mean(np.abs(decoded.astype(int) - original.astype(int))) < 3
