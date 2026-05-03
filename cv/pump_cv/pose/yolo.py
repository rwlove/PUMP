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
    ):
        self._source = source
        self._camera = camera_name or source
        self._model_name = model
        self._image_size = image_size
        self._device = device
        self._model = None

    def _ensure_model(self) -> None:
        if self._model is None:
            from ultralytics import YOLO

            self._model = YOLO(self._model_name)
            # YOLO autodetects device on .predict; we set it per call.
            logger.info("yolo: model loaded", model=self._model_name, device=self._device)

    async def poses(self) -> AsyncIterator[FrameAndPoses]:
        self._ensure_model()
        cap = cv2.VideoCapture(self._source)
        if not cap.isOpened():
            raise RuntimeError(f"yolo: cannot open source {self._source}")

        fps = cap.get(cv2.CAP_PROP_FPS) or 30.0
        frame_period = 1.0 / fps
        # If the source is a video file, use frame index for timestamps so
        # tests are deterministic. For RTSP, prefer wall clock.
        is_file = Path(self._source).is_file()
        frame_idx = 0

        try:
            while True:
                ok, frame = await asyncio.to_thread(cap.read)
                if not ok or frame is None:
                    break

                ts = frame_idx * frame_period if is_file else time.time()
                frame_idx += 1

                results = await asyncio.to_thread(
                    self._model.predict,
                    frame,
                    imgsz=self._image_size,
                    device=self._device,
                    verbose=False,
                )
                yield frame, self._results_to_poses(results, ts)
        finally:
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
