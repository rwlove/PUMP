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
  GET    /api/v1/cameras            — list cameras + per-camera health
  GET    /api/v1/cameras/{name}/snapshot — latest decoded frame (JPEG)
"""

from __future__ import annotations

import asyncio
import contextlib
import os
import shutil
import tempfile
from dataclasses import dataclass
from pathlib import Path
from typing import Any

import uvicorn
from fastapi import FastAPI, File, Form, HTTPException, Query, Response, UploadFile
from fastapi.responses import FileResponse

from . import log
from . import state as cv_state

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

    # Authenticate /api/v1/* with X-Api-Key when PUMP_API_KEY is set.
    # Probes (/healthz, /readyz) and /metrics stay open so kubelet and
    # Prometheus don't need to know the secret. Pump's reverse proxy
    # injects the header server-side from its own API_KEY env var, so
    # the browser kiosk never needs to carry the secret itself.
    @app.middleware("http")
    async def _require_api_key_mw(request, call_next):
        if request.url.path.startswith("/api/v1/"):
            expected = os.getenv("PUMP_API_KEY") or ""
            if expected and request.headers.get("x-api-key") != expected:
                logger.warning(
                    "rejected request: missing or invalid API key",
                    path=request.url.path,
                    method=request.method,
                )
                return Response(
                    content='{"detail":"unauthorized"}',
                    status_code=401,
                    media_type="application/json",
                )
        return await call_next(request)

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
    def list_snapshots(since: str | None = None, limit: int = 200) -> list[dict]:
        """List snapshots, newest first. `since` filters by date prefix
        (e.g. since=2026-05-01). Default cap of 200 keeps the gallery snappy."""
        if snapshot_dir is None or not snapshot_dir.exists():
            return []
        out = []
        for p in sorted(snapshot_dir.rglob("*.png"), reverse=True):
            rel = str(p.relative_to(snapshot_dir))
            if since and rel < since:
                continue
            out.append({"path": rel})
            if len(out) >= limit:
                break
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

    # ─── admin panel + wall preview: cameras ──────────────────────────

    @app.get("/api/v1/cameras")
    def list_cameras() -> list[dict]:
        from .pose.yolo import registered_cameras
        return [c.stats() for c in registered_cameras()]

    @app.get("/api/v1/cameras/{name}/snapshot")
    def camera_snapshot(name: str, q: int = Query(75, ge=1, le=95)) -> Response:
        """Return the most recent decoded BGR frame from `name` as JPEG.

        Used by the wall page's live-preview tiles, which poll this
        endpoint every ~1s. 404 if the camera isn't registered or hasn't
        produced a frame yet (cold start / disconnected)."""
        import cv2

        from .pose.yolo import registered_cameras
        for c in registered_cameras():
            if c.camera_name == name:
                frame = c.latest_frame()
                if frame is None:
                    raise HTTPException(status_code=404, detail="no frame yet")
                ok, jpg = cv2.imencode(".jpg", frame, [cv2.IMWRITE_JPEG_QUALITY, q])
                if not ok:
                    raise HTTPException(status_code=500, detail="jpeg encode failed")
                return Response(
                    content=jpg.tobytes(),
                    media_type="image/jpeg",
                    headers={"Cache-Control": "no-store"},
                )
        raise HTTPException(status_code=404, detail="unknown camera")

    # ─── calibration wizard ──────────────────────────────────────────
    # Two-camera setups gate the rep pipeline behind calibration. The
    # wall page polls /state, drives /capture per pair, then /compute
    # which runs the existing calibrate_intrinsics + calibrate_stereo
    # functions and writes .npz files into the cache PVC. State
    # transitions are managed in pump_cv.state.

    @app.get("/api/v1/calibration/state")
    def calibration_state() -> dict:
        return cv_state.to_dict()

    @app.post("/api/v1/calibration/capture")
    def calibration_capture() -> dict:
        """Grab the latest decoded frame from each registered camera and
        save it as a paired sample under
        /cache/calibration/captures/<camera>/pair-<NNN>.jpg. Returns the
        per-camera capture count and whether the chessboard was detected
        in each image (so the wizard can warn before letting the user
        accept the sample)."""
        import cv2

        from .pose.yolo import registered_cameras
        cams = list(registered_cameras())
        if len(cams) < 2:
            raise HTTPException(status_code=400,
                                detail="calibration requires >=2 cameras")
        # Check every camera has a frame ready before writing anything,
        # so we don't end up with an unpaired half-capture on disk.
        frames = []
        for c in cams:
            frame = c.latest_frame()
            if frame is None:
                raise HTTPException(
                    status_code=409,
                    detail=f"camera '{c.camera_name}' has no frame yet",
                )
            frames.append((c.camera_name, frame))

        # Index by lowest-numbered missing slot so retake on a previous
        # pair just overwrites that pair.
        s = cv_state.get()
        idx = max(s.captures.values(), default=0)
        result: dict[str, object] = {"index": idx, "detections": {}}
        for name, frame in frames:
            d = cv_state.captures_dir(name)
            d.mkdir(parents=True, exist_ok=True)
            path = d / f"pair-{idx:03d}.jpg"
            cv2.imwrite(str(path), frame)
            cv_state.set_capture_count(name, idx + 1)
            # Quick chessboard probe so the UI can mark this sample as
            # usable. Detection is cheap (well under a second on the
            # substream resolution we're working with).
            gray = cv2.cvtColor(frame, cv2.COLOR_BGR2GRAY)
            ok, _ = cv2.findChessboardCorners(gray, (9, 6))
            result["detections"][name] = bool(ok)

        cv_state.persist_captures()
        if cv_state.get().phase != "calibrating":
            cv_state.set_phase("calibrating")
        return result

    @app.delete("/api/v1/calibration/capture/{index}")
    def calibration_capture_delete(index: int) -> dict:
        """Drop the most recent pair (or any pair by index) — the wizard
        calls this when the user rejects a capture."""
        from .pose.yolo import registered_cameras
        cams = list(registered_cameras())
        for c in cams:
            p = cv_state.captures_dir(c.camera_name) / f"pair-{index:03d}.jpg"
            if p.is_file():
                p.unlink()
            # Recompute the count from disk so we stay honest after
            # arbitrary deletes.
            n = sum(1 for _ in cv_state.captures_dir(c.camera_name).glob("pair-*.jpg"))
            cv_state.set_capture_count(c.camera_name, n)
        cv_state.persist_captures()
        return cv_state.to_dict()

    @app.post("/api/v1/calibration/compute")
    def calibration_compute(
        rows: int = Query(6, ge=3, le=20),
        cols: int = Query(9, ge=3, le=20),
        square_size_mm: float = Query(25.0, gt=0.0),
    ) -> dict:
        """Run intrinsics-per-camera + stereo extrinsics on the captured
        pairs, write the .npz files, transition state to ready."""
        from .calibration import calibrate_intrinsics, calibrate_stereo, save_camera

        from .pose.yolo import registered_cameras
        cams = list(registered_cameras())
        if len(cams) < 2:
            raise HTTPException(status_code=400,
                                detail="calibration requires >=2 cameras")

        # Need at least the same N pairs in every camera's dir, and N>=10.
        per_cam = {
            c.camera_name: sorted(cv_state.captures_dir(c.camera_name).glob("pair-*.jpg"))
            for c in cams
        }
        n = min(len(v) for v in per_cam.values())
        if n < 10:
            raise HTTPException(
                status_code=409,
                detail=f"need >=10 pairs per camera; have {n}",
            )

        try:
            intrinsics: dict[str, tuple] = {}
            metrics: dict[str, float] = {}
            for name, paths in per_cam.items():
                K, dist = calibrate_intrinsics(
                    paths[:n], rows=rows, cols=cols,
                    square_size_mm=square_size_mm,
                )
                intrinsics[name] = (K, dist)
                # calibrate_intrinsics already logs RMS; we don't get it
                # back as a return value, so re-run a lightweight check
                # later if we want metric reporting. For now leave the
                # value off; main RMS is the stereo one below.

            # Stereo: first cam in registry order is the "left" / world
            # frame. .npz for the left camera stores intrinsics only;
            # .npz for the right stores intrinsics + R/t into left's frame.
            left_name = cams[0].camera_name
            right_name = cams[1].camera_name
            left_K, left_dist = intrinsics[left_name]
            right_K, right_dist = intrinsics[right_name]

            R, t = calibrate_stereo(
                per_cam[left_name][:n], per_cam[right_name][:n],
                left_K, left_dist, right_K, right_dist,
                rows=rows, cols=cols, square_size_mm=square_size_mm,
            )

            save_camera(cv_state.npz_path(left_name), left_K, left_dist)
            save_camera(cv_state.npz_path(right_name), right_K, right_dist, R=R, t=t)

            cv_state.set_metrics(metrics)
            cv_state.set_phase("ready")
            logger.info("calibration compute succeeded",
                        left=left_name, right=right_name, n_pairs=n)
        except HTTPException:
            raise
        except Exception as e:
            logger.warning("calibration compute failed", error=str(e))
            cv_state.set_phase("error", error=str(e))
            raise HTTPException(status_code=500, detail=str(e))

        return cv_state.to_dict()

    @app.post("/api/v1/calibration/reset")
    def calibration_reset() -> dict:
        """Wipe all captures and saved .npz files, return to
        needs_calibration. Use to start over."""
        from .pose.yolo import registered_cameras
        cams = list(registered_cameras())
        for c in cams:
            d = cv_state.captures_dir(c.camera_name)
            if d.is_dir():
                shutil.rmtree(d)
            npz = cv_state.npz_path(c.camera_name)
            if npz.is_file():
                npz.unlink()
            cv_state.set_capture_count(c.camera_name, 0)
        marker = cv_state.state_marker_path()
        if marker.is_file():
            marker.unlink()
        cv_state.set_phase("needs_calibration")
        return cv_state.to_dict()

    # Serve a captured pair frame for wizard preview thumbnails. Reuses
    # the same path safety check pattern as the snapshot endpoint.

    @app.get("/api/v1/calibration/captures/{camera}/{name}")
    def calibration_capture_image(camera: str, name: str) -> FileResponse:
        target = (cv_state.captures_dir(camera) / name).resolve()
        if not str(target).startswith(str(cv_state.captures_dir(camera).resolve())):
            raise HTTPException(status_code=400, detail="bad path")
        if not target.is_file():
            raise HTTPException(status_code=404, detail="not found")
        return FileResponse(target)

    # ─── admin panel: HSV mask preview ────────────────────────────────

    @app.post("/api/v1/hsv-preview")
    async def hsv_preview(
        image:  UploadFile = File(...),
        h_lo:   int = Form(...),
        s_lo:   int = Form(...),
        v_lo:   int = Form(...),
        h_hi:   int = Form(...),
        s_hi:   int = Form(...),
        v_hi:   int = Form(...),
    ) -> Response:
        """Render the HSV mask of an uploaded BGR image. Returns the
        binary mask as a PNG so the admin UI can show it next to the
        original. Useful for tuning PLATE_COLORS by eye against a
        photograph of your loaded bar."""
        import cv2
        import numpy as np
        data = await image.read()
        arr = np.frombuffer(data, dtype=np.uint8)
        img = cv2.imdecode(arr, cv2.IMREAD_COLOR)
        if img is None:
            raise HTTPException(status_code=400, detail="invalid image")
        hsv = cv2.cvtColor(img, cv2.COLOR_BGR2HSV)
        mask = cv2.inRange(hsv,
                           np.array([h_lo, s_lo, v_lo], dtype=np.uint8),
                           np.array([h_hi, s_hi, v_hi], dtype=np.uint8))
        ok, png = cv2.imencode(".png", mask)
        if not ok:
            raise HTTPException(status_code=500, detail="png encode failed")
        return Response(content=png.tobytes(), media_type="image/png")

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
