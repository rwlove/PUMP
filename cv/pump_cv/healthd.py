"""HTTP control + observability server for pump-cv.

Runs alongside the pipeline via asyncio. Exposes:

  GET    /healthz                   — liveness
  GET    /readyz                    — readiness
  GET    /metrics                   — Prometheus counters
  POST   /api/v1/reference          — upload reference clip → prototype
  GET    /api/v1/state              — current pipeline state (admin panel)
  GET    /api/v1/thresholds         — current tunable thresholds
  PATCH  /api/v1/thresholds         — hot-reload thresholds
  GET    /api/v1/prototypes         — list saved DTW prototypes
  DELETE /api/v1/prototypes         — delete one (path query param)
  GET    /api/v1/snapshots          — list per-set debug snapshots
  GET    /api/v1/snapshots/{path}   — serve one snapshot file
  GET    /api/v1/cameras            — camera health (stub for now)
"""

from __future__ import annotations

import asyncio
import contextlib
import shutil
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import uvicorn
from fastapi import FastAPI, File, Form, HTTPException, Query, Response, UploadFile
from fastapi.responses import FileResponse

from . import log

logger = log.get(__name__)


@dataclass
class _State:
    ready: bool = False
    sets_posted: int = 0
    sets_pending: int = 0
    sets_failed: int = 0


_state = _State()


def mark_ready() -> None:
    _state.ready = True


def record_set_posted(pending: bool) -> None:
    _state.sets_posted += 1
    if pending:
        _state.sets_pending += 1


def record_set_failed() -> None:
    _state.sets_failed += 1


def build_app(
    prototype_dir: Path | None = None,
    snapshot_dir: Path | None = None,
    runner: Any = None,                # PipelineRunner; typed loosely to avoid import cycle
) -> FastAPI:
    app = FastAPI(title="pump-cv", docs_url=None, redoc_url=None)

    @app.get("/healthz")
    def healthz() -> dict[str, str]:
        return {"status": "ok"}

    @app.get("/readyz")
    def readyz(response: Response) -> dict[str, object]:
        if not _state.ready:
            response.status_code = 503
            return {"status": "starting"}
        return {"status": "ready"}

    @app.get("/metrics")
    def metrics() -> Response:
        body = (
            f"pump_cv_ready {1 if _state.ready else 0}\n"
            f"pump_cv_sets_posted_total {_state.sets_posted}\n"
            f"pump_cv_sets_pending_total {_state.sets_pending}\n"
            f"pump_cv_sets_failed_total {_state.sets_failed}\n"
        )
        return Response(content=body, media_type="text/plain; version=0.0.4")

    @app.post("/api/v1/reference")
    async def post_reference(
        exercise_name: str = Form(...),
        clip: UploadFile = File(...),
    ) -> dict:
        """Ingest a short reference clip. Runs YOLOv8-Pose synchronously
        on the file; large clips will block this request for several
        seconds. Returns the saved prototype path on success."""
        if prototype_dir is None:
            raise HTTPException(status_code=503, detail="prototype storage not configured")
        # Save the upload to a temp file YOLO can read.
        suffix = Path(clip.filename or "clip.mp4").suffix or ".mp4"
        with tempfile.NamedTemporaryFile(delete=False, suffix=suffix) as tmp:
            shutil.copyfileobj(clip.file, tmp)
            tmp_path = Path(tmp.name)
        try:
            # Defer the heavy import + run on a thread so the event
            # loop isn't blocked.
            saved = await asyncio.to_thread(
                _process_reference_clip,
                tmp_path, exercise_name, prototype_dir,
            )
        finally:
            tmp_path.unlink(missing_ok=True)
        return {
            "exercise_name": exercise_name,
            "prototype_path": str(saved),
        }

    # ─── admin panel: pipeline state ───────────────────────────────────

    @app.get("/api/v1/state")
    def state() -> dict:
        if runner is None:
            return {"status": "no runner attached"}
        return runner.snapshot_state()

    # ─── admin panel: tunable thresholds ──────────────────────────────

    @app.get("/api/v1/thresholds")
    def get_thresholds() -> dict:
        if runner is None:
            return {}
        return runner.snapshot_thresholds()

    @app.patch("/api/v1/thresholds")
    async def patch_thresholds(payload: dict) -> dict:
        if runner is None:
            raise HTTPException(status_code=503, detail="no runner attached")
        runner.update_thresholds(payload or {})
        return {"applied": True, "thresholds": runner.snapshot_thresholds()}

    # ─── admin panel: prototypes ──────────────────────────────────────

    @app.get("/api/v1/prototypes")
    def list_prototypes() -> list[dict]:
        if prototype_dir is None or not prototype_dir.exists():
            return []
        from .classify import PrototypeStore
        store = PrototypeStore(prototype_dir)
        return [{
            "exercise_name": p.exercise_name,
            "frames": int(p.features.shape[0]),
            "source_clip": p.source_clip,
            "path": p.exercise_name + "/" + str(p.features.shape[0]),  # display-only
        } for p in store.load_all()]

    @app.delete("/api/v1/prototypes")
    def delete_prototype(path: str = Query(...)) -> dict:
        if prototype_dir is None:
            raise HTTPException(status_code=503, detail="no prototype dir")
        # Resolve and confine to prototype_dir to prevent path traversal.
        target = (prototype_dir / path).resolve()
        if not str(target).startswith(str(prototype_dir.resolve())):
            raise HTTPException(status_code=400, detail="bad path")
        if target.suffix == ".npz":
            target.unlink(missing_ok=True)
            target.with_suffix(".json").unlink(missing_ok=True)
        return {"deleted": str(target)}

    # ─── admin panel: snapshots ───────────────────────────────────────

    @app.get("/api/v1/snapshots")
    def list_snapshots() -> list[dict]:
        if snapshot_dir is None or not snapshot_dir.exists():
            return []
        out = []
        for p in sorted(snapshot_dir.rglob("*.png"), reverse=True)[:200]:
            out.append({"path": str(p.relative_to(snapshot_dir))})
        return out

    @app.get("/api/v1/snapshots/{path:path}")
    def serve_snapshot(path: str) -> FileResponse:
        if snapshot_dir is None:
            raise HTTPException(status_code=503, detail="no snapshot dir")
        target = (snapshot_dir / path).resolve()
        if not str(target).startswith(str(snapshot_dir.resolve())):
            raise HTTPException(status_code=400, detail="bad path")
        if not target.is_file():
            raise HTTPException(status_code=404, detail="not found")
        return FileResponse(target)

    # ─── admin panel: cameras (stub for now) ──────────────────────────

    @app.get("/api/v1/cameras")
    def list_cameras() -> list[dict]:
        # Phase 2 stub: real per-camera FPS + connection status comes
        # when the pose source layer learns to report it.
        return []

    return app


def _process_reference_clip(video_path: Path, exercise_name: str,
                            prototype_dir: Path) -> Path:
    """Run YOLOv8 on the clip → prototype → PrototypeStore. Synchronous
    helper invoked from a worker thread."""
    from .classify import (
        ExercisePrototype,
        PrototypeStore,
        pose_sequence_to_features,
    )
    from .pose.yolo import YOLOPoseSource
    from .tracking import pick_athlete

    async def _run_yolo():
        src = YOLOPoseSource(
            source=str(video_path),
            camera_name="reference",
        )
        seq = []
        async for _frame, poses in src.poses():
            athlete = pick_athlete(poses)
            if athlete is not None:
                seq.append(athlete)
        return seq

    poses = asyncio.run(_run_yolo())
    if not poses:
        raise HTTPException(
            status_code=422,
            detail="no athlete detected in clip",
        )
    feats = pose_sequence_to_features(poses)
    store = PrototypeStore(prototype_dir)
    saved = store.add(ExercisePrototype(
        exercise_name=exercise_name,
        features=feats,
        source_clip=video_path.name,
    ))
    logger.info("reference clip ingested",
                exercise=exercise_name, frames=len(poses), path=str(saved))
    return saved


async def serve(
    host: str = "0.0.0.0",
    port: int = 8080,
    prototype_dir: Path | None = None,
    snapshot_dir: Path | None = None,
    runner: Any = None,
) -> None:
    """Run the control server forever. Cancel the returned task to stop."""
    app = build_app(prototype_dir=prototype_dir, snapshot_dir=snapshot_dir, runner=runner)
    config = uvicorn.Config(app, host=host, port=port, log_level="warning",
                            access_log=False, lifespan="off")
    server = uvicorn.Server(config)
    with contextlib.suppress(asyncio.CancelledError):
        await server.serve()
