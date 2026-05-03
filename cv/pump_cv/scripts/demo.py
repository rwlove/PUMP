"""Canned-data replay against a running PUMP — demo without cameras.

Posts a realistic sequence of CV-detected sets so the wall view and the
admin panel have something to show while you wait for cameras to be
installed. Optionally writes tiny "test-card" MP4 clips into a clips
directory so the wall video pane has a real <video> to play.

  python -m pump_cv.scripts.demo \
      --pump http://localhost:8851 \
      --speed 4 \
      --clips-dir /tmp/pump-clips
"""

from __future__ import annotations

import argparse
import asyncio
import datetime as dt
import sys
from dataclasses import dataclass
from pathlib import Path

import cv2
import numpy as np

from .. import log
from ..pump_client import PumpClient

logger = log.get(__name__)


@dataclass(frozen=True, slots=True)
class _DemoSet:
    name: str
    weight: str
    reps: int
    confidence: float
    pending: bool = False
    note: str = ""
    delay_s: float = 60.0


# A typical workout: warm-up squats, working bench, finisher curls; with
# one wonky CV detection in there to exercise the pending UI.
_SCRIPT: list[_DemoSet] = [
    _DemoSet("Squat",       "135", 8, 0.96, delay_s=2),
    _DemoSet("Squat",       "185", 5, 0.94, delay_s=90),
    _DemoSet("Squat",       "225", 5, 0.91, delay_s=120),
    _DemoSet("Bench Press", "135", 8, 0.95, delay_s=180),
    _DemoSet("Bench Press", "155", 5, 0.92, delay_s=120),
    _DemoSet("Bench Press", "175", 5, 0.55, pending=True, delay_s=120,
             note="Low confidence — confirm the weight."),
    _DemoSet("Bicep Curl",  "30",  10, 0.88, delay_s=200),
    _DemoSet("Bicep Curl",  "30",  10, 0.87, delay_s=70),
]


def _write_test_clip(path: Path, exercise: str, weight: str, reps: int) -> None:
    """Write a tiny 4-second test-card MP4 with the set details on it.
    Just enough to confirm the wall view's <video> element renders."""
    path.parent.mkdir(parents=True, exist_ok=True)
    w, h, fps = 640, 360, 10
    fourcc = cv2.VideoWriter_fourcc(*"mp4v")
    writer = cv2.VideoWriter(str(path), fourcc, fps, (w, h))
    if not writer.isOpened():
        logger.warning("demo: cv2.VideoWriter failed to open", path=str(path))
        return
    try:
        for i in range(fps * 4):
            frame = np.zeros((h, w, 3), dtype=np.uint8)
            frame[:] = (24, 24, 28)
            cv2.putText(frame, exercise, (32, 96), cv2.FONT_HERSHEY_SIMPLEX,
                        1.5, (220, 220, 220), 2, cv2.LINE_AA)
            cv2.putText(frame, f"{weight} lb x {reps} reps", (32, 160),
                        cv2.FONT_HERSHEY_SIMPLEX, 1.0, (180, 180, 180), 2, cv2.LINE_AA)
            cv2.putText(frame, "DEMO CLIP", (32, h - 32),
                        cv2.FONT_HERSHEY_SIMPLEX, 0.6, (90, 90, 90), 1, cv2.LINE_AA)
            # tiny moving rectangle so it looks like motion
            x = (i * 30) % (w - 60)
            cv2.rectangle(frame, (x, h - 80), (x + 60, h - 60), (39, 128, 227), -1)
            writer.write(frame)
    finally:
        writer.release()


async def _run(args) -> int:
    log.configure()
    today = dt.date.today().isoformat()

    async with PumpClient(args.pump) as pump:
        for i, s in enumerate(_SCRIPT):
            wait = s.delay_s / max(1.0, args.speed)
            logger.info("demo: waiting", seconds=round(wait, 1))
            await asyncio.sleep(wait)

            payload = {
                "Date":       today,
                "Name":       s.name,
                "Weight":     s.weight,
                "Reps":       s.reps,
                "Source":     "cv",
                "Confidence": s.confidence,
                "Pending":    s.pending,
                "Note":       s.note,
            }
            try:
                set_id = await pump.post_set(payload)
            except PermissionError:
                logger.error("demo: 403 — toggle CVAutoLog ON in PUMP config first")
                return 2
            logger.info("demo: posted",
                        i=i + 1, of=len(_SCRIPT),
                        id=set_id, name=s.name, pending=s.pending)

            if args.clips_dir:
                rel = Path(today) / f"{set_id}__{s.name.replace(' ', '-')}.mp4"
                _write_test_clip(args.clips_dir / rel, s.name, s.weight, s.reps)
                try:
                    await pump.patch_set(set_id, {"ClipPath": str(rel)})
                    logger.info("demo: clip linked", id=set_id, path=str(rel))
                except Exception as e:
                    logger.warning("demo: clip patch failed", error=str(e))

    logger.info("demo: done")
    return 0


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="pump_cv.scripts.demo")
    p.add_argument("--pump", default="http://localhost:8851",
                   help="PUMP API base URL")
    p.add_argument("--speed", type=float, default=4.0,
                   help="time-compression factor (4 = 4× faster than real workout)")
    p.add_argument("--clips-dir", type=Path, default=None,
                   help="if set, write test-card MP4s here (mounted as PUMP_CLIPS_DIR)")
    args = p.parse_args(argv)
    return asyncio.run(_run(args))


if __name__ == "__main__":
    sys.exit(main())
