"""HTTP control + observability server for pump-cv.

Runs alongside the pipeline via asyncio. Exposes:

  GET  /healthz   — always 200 once the server starts (liveness)
  GET  /readyz    — 200 once mark_ready() is called, 503 before
  GET  /metrics   — Prometheus-style counters
  POST /api/v1/reference
                  — multipart upload of a short reference clip; the
                    server runs YOLOv8-Pose on it, extracts a prototype,
                    and persists it to the configured PrototypeStore so
                    the live pipeline picks it up on next reload
"""

from __future__ import annotations

import asyncio
import contextlib
import shutil
import tempfile
from dataclasses import dataclass
from pathlib import Path

import uvicorn
from fastapi import FastAPI, File, Form, HTTPException, Response, UploadFile

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


def build_app(prototype_dir: Path | None = None) -> FastAPI:
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


async def serve(host: str = "0.0.0.0", port: int = 8080,
                prototype_dir: Path | None = None) -> None:
    """Run the control server forever. Cancel the returned task to stop."""
    app = build_app(prototype_dir=prototype_dir)
    config = uvicorn.Config(app, host=host, port=port, log_level="warning",
                            access_log=False, lifespan="off")
    server = uvicorn.Server(config)
    with contextlib.suppress(asyncio.CancelledError):
        await server.serve()
