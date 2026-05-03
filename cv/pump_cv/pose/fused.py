"""Multi-camera fusion source.

Wraps two PoseSources observing the same scene from different angles.
Pairs frames by timestamp (skipping any drift), runs the single-athlete
picker on each side, then triangulates the picked-athlete's keypoints
to 3D using the supplied calibrations.

Output is a (frame_a, [pose]) tuple that looks like a single-cam stream
to the rest of the pipeline — except the Pose now carries
keypoints_3d, which downstream joint-angle math prefers.

If only one camera produces a usable pose for a given frame pair, the
fused source falls back to that single-cam pose (no 3D, but the set
keeps progressing). If neither does, an empty pose list is yielded.
"""

from __future__ import annotations

import asyncio
from collections.abc import AsyncIterator

from ..fusion import CameraCalibration, triangulate_pose
from ..tracking import pick_athlete
from .types import FrameAndPoses, Pose


class FusedPoseSource:
    def __init__(
        self,
        source_a, source_b,
        cam_a: CameraCalibration, cam_b: CameraCalibration,
        sync_tolerance_s: float = 0.05,
    ):
        self._source_a = source_a
        self._source_b = source_b
        self._cam_a = cam_a
        self._cam_b = cam_b
        self._tol = sync_tolerance_s

    async def poses(self) -> AsyncIterator[FrameAndPoses]:
        """Yield (frame_a, [fused_pose]) for each synced pair."""
        # Each task pulls one (frame, poses) at a time and pushes to a queue.
        q_a: asyncio.Queue = asyncio.Queue(maxsize=2)
        q_b: asyncio.Queue = asyncio.Queue(maxsize=2)

        async def _pump(src, q):
            try:
                async for item in src.poses():
                    await q.put(item)
            finally:
                await q.put(None)

        task_a = asyncio.create_task(_pump(self._source_a, q_a))
        task_b = asyncio.create_task(_pump(self._source_b, q_b))

        try:
            cur_a = await q_a.get()
            cur_b = await q_b.get()
            while cur_a is not None and cur_b is not None:
                frame_a, poses_a = cur_a
                _frame_b, poses_b = cur_b

                # Sync only when BOTH sides have timed poses; an empty
                # side means the athlete isn't visible there and we
                # shouldn't wait for it to "catch up". Just advance.
                if poses_a and poses_b:
                    ts_a = poses_a[0].timestamp
                    ts_b = poses_b[0].timestamp
                    if ts_a + self._tol < ts_b:
                        cur_a = await q_a.get()
                        continue
                    if ts_b + self._tol < ts_a:
                        cur_b = await q_b.get()
                        continue

                ath_a = pick_athlete(poses_a) if poses_a else None
                ath_b = pick_athlete(poses_b) if poses_b else None

                fused: list[Pose] = []
                if ath_a is not None and ath_b is not None:
                    pts_3d = triangulate_pose(ath_a, ath_b, self._cam_a, self._cam_b)
                    fused.append(Pose(
                        timestamp=ath_a.timestamp,
                        bbox=ath_a.bbox,
                        score=min(ath_a.score, ath_b.score),
                        keypoints=ath_a.keypoints,
                        camera="fused",
                        keypoints_3d=tuple(pts_3d),
                    ))
                elif ath_a is not None:
                    fused.append(ath_a)
                elif ath_b is not None:
                    fused.append(ath_b)

                yield frame_a, fused

                cur_a = await q_a.get()
                cur_b = await q_b.get()
        finally:
            task_a.cancel()
            task_b.cancel()
            await asyncio.gather(task_a, task_b, return_exceptions=True)
