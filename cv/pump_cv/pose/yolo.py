"""YOLOv8-Pose source.

NOT verified against a real GPU yet — this code is written following the
Ultralytics docs and will need a smoke test on the P40 once it's the
target node. The shape of the public API is intentionally tiny so the
GPU/no-GPU split happens entirely behind it.
"""

from __future__ import annotations

import asyncio
import time
from collections.abc import AsyncIterator
from pathlib import Path

import cv2

from .. import log
from .types import FrameAndPoses, Keypoint, Pose

logger = log.get(__name__)


class YOLOPoseSource:
    """Reads frames from a video path or RTSP URL and emits Pose objects.

    Detection is delegated to ultralytics.YOLO (loaded lazily so unit
    tests that don't need it never import torch).
    """

    def __init__(
        self,
        source: str,
        camera_name: str = "",
        model: str = "yolov8m-pose.pt",
        image_size: int = 640,
        device: str = "cuda:0",
        retry_on_failure: bool = True,
        retry_initial_seconds: float = 1.0,
        retry_max_seconds: float = 30.0,
    ):
        self._source = source
        self._camera = camera_name or source
        self._model_name = model
        self._image_size = image_size
        self._device = device
        self._retry = retry_on_failure
        self._retry_initial = retry_initial_seconds
        self._retry_max = retry_max_seconds
        self._model = None

        # FPS + connection state — surfaced to the admin panel via the
        # global registry below.
        from collections import deque
        self._frame_times: deque[float] = deque(maxlen=60)
        self._connected: bool = False
        # Most-recent decoded BGR frame, exposed via healthd for the
        # wall-page live preview. Single-writer (capture loop), reader
        # tolerates stale frames so no lock is needed.
        self._latest_frame = None
        # Monotonic id incremented on each new frame; drives the
        # JPEG-encode cache below (only re-encode when the frame changes,
        # not on every ~1s wall poll).
        self._frame_id: int = 0
        # (frame_id, quality) -> encoded JPEG bytes.
        self._jpeg_cache: dict[tuple[int, int], bytes] = {}
        register_camera(self)

    # ─── admin introspection ─────────────────────────────────────────
    @property
    def camera_name(self) -> str: return self._camera
    @property
    def source_url(self) -> str: return self._source

    def stats(self) -> dict:
        """Return current FPS + connected status for the admin panel."""
        n = len(self._frame_times)
        fps = 0.0
        if n >= 2:
            span = self._frame_times[-1] - self._frame_times[0]
            if span > 0:
                fps = (n - 1) / span
        return {
            "name": self._camera,
            "source": self._source,
            "fps": round(fps, 1),
            "connected": self._connected,
            "has_frame": self._latest_frame is not None,
        }

    def latest_frame(self):
        """Return the most recent decoded BGR frame, or None if the
        capture loop hasn't produced one yet (or has disconnected)."""
        return self._latest_frame

    def latest_jpeg(self, quality: int) -> bytes | None:
        """Return the most recent frame encoded as JPEG, cached by
        (frame_id, quality). None when no frame is available.

        The wall page polls per-camera snapshot endpoints every ~1s from
        multiple viewers (wall + admin panel + calibration wizard) so an
        unchanged frame — during a disconnect, a slow camera, or two
        polls landing between decode events — reuses the previous
        encode instead of re-running libjpeg."""
        import cv2

        # Snapshot both atomically-enough for Python: sample id, then
        # frame; on a torn read the cache lookup below misses and we
        # re-encode with the fresh frame, which is the safe outcome.
        fid = self._frame_id
        frame = self._latest_frame
        if frame is None:
            return None
        key = (fid, quality)
        hit = self._jpeg_cache.get(key)
        if hit is not None:
            return hit
        ok, jpg = cv2.imencode(".jpg", frame, [cv2.IMWRITE_JPEG_QUALITY, quality])
        if not ok:
            return None
        data = jpg.tobytes()
        # Drop older entries so the cache doesn't grow — realistic
        # concurrent quality-parameter count is 1 (wall polls all use
        # the default q=75), so keeping only the last entry is fine.
        self._jpeg_cache = {key: data}
        return data

    def _ensure_model(self) -> None:
        if self._model is None:
            from ultralytics import YOLO

            self._model = YOLO(self._model_name)
            # YOLO autodetects device on .predict; we set it per call.
            logger.info("yolo: model loaded", model=self._model_name, device=self._device)

    async def poses(self) -> AsyncIterator[FrameAndPoses]:
        """Yield (frame, poses) forever.

        For a finite source (a video file) we exit on EOF — there is
        nothing to reconnect to. For a live source (RTSP) and any other
        non-file source we retry indefinitely with capped exponential
        backoff: a brief disconnect just costs a few dropped frames.
        """
        self._ensure_model()
        is_file = Path(self._source).is_file()

        backoff = self._retry_initial
        while True:
            try:
                async for item in self._stream_one_capture():
                    yield item
                    backoff = self._retry_initial   # any success resets backoff
            except Exception as e:
                logger.warning("yolo: capture loop crashed",
                               source=self._source, error=str(e))

            if is_file or not self._retry:
                return

            logger.warning("yolo: source disconnected, retrying",
                           source=self._source, backoff_s=backoff)
            await asyncio.sleep(backoff)
            backoff = min(self._retry_max, backoff * 2)

    async def _stream_one_capture(self) -> AsyncIterator[FrameAndPoses]:
        """One open-read-close cycle. Exits on EOF or read failure; the
        outer poses() handles reconnection."""
        cap = cv2.VideoCapture(self._source)
        if not cap.isOpened():
            raise RuntimeError(f"yolo: cannot open source {self._source}")
        self._connected = True

        fps = cap.get(cv2.CAP_PROP_FPS) or 30.0
        frame_period = 1.0 / fps
        is_file = Path(self._source).is_file()
        frame_idx = 0

        try:
            while True:
                ok, frame = await asyncio.to_thread(cap.read)
                if not ok or frame is None:
                    return

                ts = frame_idx * frame_period if is_file else time.time()
                frame_idx += 1
                self._frame_times.append(time.time())
                self._latest_frame = frame
                self._frame_id += 1

                results = await asyncio.to_thread(
                    self._model.predict,
                    frame,
                    imgsz=self._image_size,
                    device=self._device,
                    verbose=False,
                )
                yield frame, self._results_to_poses(results, ts)
        finally:
            self._connected = False
            self._latest_frame = None
            # Frame gone → JPEG cache is stale; drop it so /snapshot 404s
            # instead of serving the last frame from a disconnected camera.
            self._jpeg_cache = {}
            cap.release()

    def _results_to_poses(self, results, ts: float) -> list[Pose]:
        poses: list[Pose] = []
        for r in results:
            kps = r.keypoints  # ultralytics Keypoints
            boxes = r.boxes
            if kps is None or boxes is None:
                continue
            xy = kps.xy.cpu().numpy()       # (N, 17, 2)
            conf = kps.conf.cpu().numpy()   # (N, 17)
            bb = boxes.xyxy.cpu().numpy()   # (N, 4)
            sc = boxes.conf.cpu().numpy()   # (N,)
            for i in range(xy.shape[0]):
                kp_objs = tuple(
                    Keypoint(float(xy[i, j, 0]), float(xy[i, j, 1]), float(conf[i, j]))
                    for j in range(xy.shape[1])
                )
                poses.append(
                    Pose(
                        timestamp=ts,
                        bbox=tuple(map(float, bb[i])),
                        score=float(sc[i]),
                        keypoints=kp_objs,
                        camera=self._camera,
                    )
                )
        return poses


# Module-level camera registry — pose sources self-register on construction;
# healthd reads the list to render the admin "Cameras" tab.
_CAMERA_REGISTRY: list = []


def register_camera(source) -> None:
    _CAMERA_REGISTRY.append(source)


def registered_cameras() -> list:
    return list(_CAMERA_REGISTRY)
