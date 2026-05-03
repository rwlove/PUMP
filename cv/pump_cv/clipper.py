"""Per-set rolling-buffer video clip writer.

Maintains a deque of recent frames; on demand, encodes them to an MP4
that PUMP serves at /clips/<...>. Phase 2 scope: one camera (whatever
the runner sees yielded from the pose source). Phase 3 will pick the
best-view camera per set when multi-cam is active.

Memory footprint:
    capacity_seconds × fps × W × H × 3 bytes (BGR)
    e.g. 8 s × 10 fps × 640 × 360 × 3 ≈ 55 MB per camera

Frames are downscaled on insertion so memory usage stays bounded
regardless of the source resolution.
"""

from __future__ import annotations

import datetime as dt
from collections import deque
from pathlib import Path

import cv2
import numpy as np

from . import log

logger = log.get(__name__)


class ClipBuffer:
    """A rolling buffer that records the last `capacity_seconds` of frames."""

    def __init__(
        self,
        capacity_seconds: float = 8.0,
        sample_fps: float = 10.0,
        target_size: tuple[int, int] = (640, 360),
    ):
        self._capacity = int(capacity_seconds * sample_fps)
        self._target = target_size
        self._sample_period = 1.0 / sample_fps
        self._buffer: deque[np.ndarray] = deque(maxlen=self._capacity)
        self._last_sample_ts: float = 0.0

    def push(self, frame: np.ndarray | None, timestamp: float) -> None:
        """Drop a frame into the buffer if enough time has elapsed since
        the last sample. Frames before the source's first valid frame
        and frames during the sleep window are skipped."""
        if frame is None:
            return
        if timestamp - self._last_sample_ts < self._sample_period:
            return
        self._last_sample_ts = timestamp
        # cv2.resize(dst_w, dst_h) — note OpenCV's (w, h) convention.
        small = cv2.resize(frame, self._target, interpolation=cv2.INTER_AREA)
        self._buffer.append(small)

    def clear(self) -> None:
        self._buffer.clear()
        self._last_sample_ts = 0.0

    def __len__(self) -> int:
        return len(self._buffer)

    def write_clip(
        self,
        out_dir: Path,
        set_id: int,
        exercise: str,
        fps_out: float | None = None,
    ) -> Path | None:
        """Encode the buffered frames as MP4 under
        <out_dir>/<YYYY-MM-DD>/<set_id>__<safe_exercise>.mp4. Returns
        the relative path (under out_dir) so the caller can store it as
        the Set's ClipPath field. Returns None if the buffer is empty.
        """
        if not self._buffer:
            return None

        date_dir = dt.date.today().isoformat()
        out_dir = Path(out_dir) / date_dir
        out_dir.mkdir(parents=True, exist_ok=True)

        safe_ex = "".join(c if c.isalnum() else "-" for c in exercise).strip("-") or "set"
        fname = f"{set_id}__{safe_ex}.mp4"
        out_path = out_dir / fname

        h, w = self._buffer[0].shape[:2]
        fps = fps_out or 1.0 / max(1e-3, self._sample_period)
        fourcc = cv2.VideoWriter_fourcc(*"mp4v")
        writer = cv2.VideoWriter(str(out_path), fourcc, fps, (w, h))
        if not writer.isOpened():
            logger.warning("clip: VideoWriter failed to open", path=str(out_path))
            return None

        try:
            for frame in self._buffer:
                writer.write(frame)
        finally:
            writer.release()

        logger.info("clip: wrote", path=str(out_path), frames=len(self._buffer))
        # Return the path relative to out_dir's *parent* (the volume
        # root), so it slots into PUMP's /clips/<...> URL directly.
        return Path(date_dir) / fname
