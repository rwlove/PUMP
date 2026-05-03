"""End-to-end self-test: drive the full pipeline against a running PUMP.

Uses the synthetic mock pose source (no GPU, no torch). Runs the
pipeline through one set's worth of squats, posts to the configured
PUMP base URL, then queries it back to verify the set landed.

Run before hooking up real cameras to confirm:
  - PUMP API is reachable
  - CVAutoLog is on (otherwise POST /api/sets returns 403)
  - the per-set CRUD path works for CV-source writes
  - the SSE stream emits events for those writes (not asserted here, but
    visible in PUMP logs)

  Usage:
    PUMP_API_BASE_URL=http://localhost:8851 \
        python -m pump_cv.scripts.self_test
"""

from __future__ import annotations

import argparse
import asyncio
import os
import sys

from .. import log
from ..exercises import lookup as exercise_lookup
from ..pipeline import ExerciseSpec, PipelineRunner
from ..pose.mock import MockPoseSource
from ..pose.types import LEFT_ANKLE, LEFT_HIP, LEFT_KNEE
from ..pump_client import PumpClient

logger = log.get(__name__)


async def _run(args) -> int:
    log.configure()
    base_url = args.base_url or os.getenv("PUMP_API_BASE_URL", "http://localhost:8851")
    api_key = args.api_key or os.getenv("PUMP_API_KEY", "")

    spec = ExerciseSpec("Squat", LEFT_HIP, LEFT_KNEE, LEFT_ANKLE)
    src = MockPoseSource(
        schedule=[("rep", 10.0), ("rest", 30.0)],
        fps=30,
        rep_period_s=2.0,
    )

    async with PumpClient(base_url, api_key) as pump:
        # Capture the IDs of any sets posted during this run.
        posted_ids: list[int] = []
        original_post = pump.post_set

        async def capturing_post(payload):
            new_id = await original_post(payload)
            posted_ids.append(new_id)
            return new_id

        pump.post_set = capturing_post  # type: ignore[assignment]

        runner = PipelineRunner(
            pump=pump,
            default_exercise=spec,
            exercise_lookup=exercise_lookup,
            rep_amplitude_deg=20.0,
            rep_min_period_s=0.5,
            rep_smoothing_window=5,
            set_quiet_seconds=10.0,
        )
        try:
            await runner.run(src.poses())
        except PermissionError as e:
            logger.error("CVAutoLog appears to be off — toggle it on in the PUMP config UI",
                         error=str(e))
            return 2

    if not posted_ids:
        logger.error("no sets were posted — pipeline did not detect a set")
        return 3

    # Read back, then clean up.
    async with PumpClient(base_url, api_key) as pump:
        sets = await pump.list_sets()
        for sid in posted_ids:
            row = next((s for s in sets if s["ID"] == sid), None)
            if row is None:
                logger.error("posted set not found on read-back", id=sid)
                return 4
            logger.info("self-test: set verified",
                        id=sid, name=row["Name"], reps=row["Reps"],
                        source=row["Source"], pending=row["Pending"])
            if not args.keep:
                await pump.delete_set(sid)

    logger.info("self-test: PASSED", sets_posted=len(posted_ids))
    return 0


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(prog="pump_cv.scripts.self_test")
    p.add_argument("--base-url", default=None,
                   help="PUMP API base URL (default: $PUMP_API_BASE_URL or http://localhost:8851)")
    p.add_argument("--api-key", default=None,
                   help="PUMP API key (default: $PUMP_API_KEY)")
    p.add_argument("--keep", action="store_true",
                   help="don't delete the posted set after verifying it")
    args = p.parse_args(argv)
    return asyncio.run(_run(args))


if __name__ == "__main__":
    sys.exit(main())
