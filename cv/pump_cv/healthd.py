"""Tiny HTTP server that exposes liveness/readiness endpoints for K8s.

Runs alongside the pipeline via asyncio.gather. Doesn't share state
with the pipeline beyond a couple of mutable counters bumped via
public functions; sufficient for "is it alive" probes.

  GET /healthz   — always 200 once the server starts (liveness)
  GET /readyz    — 200 once mark_ready() is called, 503 before
  GET /metrics   — minimal text-format counters: sets posted, sets pending,
                   posted-but-failed, current pipeline state
"""

from __future__ import annotations

import contextlib
from dataclasses import dataclass

import uvicorn
from fastapi import FastAPI, Response


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


def build_app() -> FastAPI:
    app = FastAPI(title="pump-cv health", docs_url=None, redoc_url=None)

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

    return app


async def serve(host: str = "0.0.0.0", port: int = 8080) -> None:
    """Run the health server forever. Cancel the returned task to stop."""
    app = build_app()
    config = uvicorn.Config(app, host=host, port=port, log_level="warning",
                            access_log=False, lifespan="off")
    server = uvicorn.Server(config)
    with contextlib.suppress(asyncio_cancelled_error()):
        await server.serve()


def asyncio_cancelled_error():  # tiny helper for type-friendly suppression
    import asyncio
    return asyncio.CancelledError
