"""Benchmark YOLOv8-Pose throughput on the local box.

Useful before sizing camera resolution/framerate. Run on the same node
the production sidecar will use; the numbers reported are the same
ones the live pipeline will see, minus a small per-frame overhead for
keypoint conversion and the rest of the pipeline.

  python -m pump_cv.scripts.benchmark \
      --source path/to/sample.mp4 \
      --frames 300 \
      --image-size 640 \
      --device cuda:0

Reports: total elapsed, mean inference time per frame (warmup
discarded), achieved FPS, peak GPU memory used (when on cuda).
"""

from __future__ import annotations

import argparse
import asyncio
import statistics
import sys
import time
from pathlib import Path

from .. import log
from ..pose.yolo import YOLOPoseSource

logger = log.get(__name__)


async def _bench(args) -> int:
    log.configure()
    if not Path(args.source).is_file():
        logger.error("source not found", path=args.source)
        return 1

    src = YOLOPoseSource(
        source=args.source,
        camera_name="bench",
        model=args.model,
        image_size=args.image_size,
        device=args.device,
        retry_on_failure=False,
    )

    t_per_frame: list[float] = []
    n = 0
    last = time.perf_counter()
    start = last
    async for _frame, _poses in src.poses():
        now = time.perf_counter()
        t_per_frame.append(now - last)
        last = now
        n += 1
        if n >= args.frames:
            break

    elapsed = time.perf_counter() - start
    if n == 0:
        logger.error("source produced zero frames")
        return 2

    # Discard the first 10 frames as warmup (model load + cuda graph
    # priming dominate them).
    body = t_per_frame[10:] if len(t_per_frame) > 10 else t_per_frame

    fps = n / elapsed
    mean_ms = statistics.mean(body) * 1000 if body else 0.0
    p95_ms = (
        statistics.quantiles(body, n=20)[18] * 1000 if len(body) >= 20 else 0.0
    )

    peak_mem_mb = _peak_gpu_mem_mb(args.device)

    logger.info("benchmark",
                source=args.source, frames=n, image_size=args.image_size,
                device=args.device, total_s=round(elapsed, 3),
                fps=round(fps, 1), mean_ms=round(mean_ms, 2),
                p95_ms=round(p95_ms, 2),
                peak_gpu_mem_mb=peak_mem_mb)
    return 0


def _peak_gpu_mem_mb(device: str) -> float | None:
    if not device.startswith("cuda"):
        return None
    try:
        import torch
        return round(torch.cuda.max_memory_allocated() / 1024 / 1024, 1)
    except Exception:
        return None


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="pump_cv.scripts.benchmark")
    p.add_argument("--source", required=True, help="video file path")
    p.add_argument("--frames", type=int, default=300,
                   help="how many frames to process before reporting")
    p.add_argument("--model", default="yolov8m-pose.pt")
    p.add_argument("--image-size", type=int, default=640)
    p.add_argument("--device", default="cuda:0", help="cuda:0 / cpu / etc.")
    args = p.parse_args(argv)
    return asyncio.run(_bench(args))


if __name__ == "__main__":
    sys.exit(main())
