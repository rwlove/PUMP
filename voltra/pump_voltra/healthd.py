"""Probe and metrics endpoints, a thin version of cv/pump_cv/healthd.py.

Probes stay unauthenticated so the kubelet can reach them.
"""

from __future__ import annotations

from dataclasses import dataclass, field


@dataclass
class State:
    connected: bool = False
    workout_active: bool = False
    sets_posted: int = 0
    sets_failed: int = 0
    sets_pending: int = 0
    sets_inferred: int = 0
    last_error: str = ""
    flagged_exercises: int = 0
    extra: dict = field(default_factory=dict)


_state = State()


def state() -> State:
    return _state


def record_connected(ok: bool) -> None:
    _state.connected = ok


def record_workout_active(active: bool) -> None:
    _state.workout_active = active


def record_flagged_exercises(n: int) -> None:
    _state.flagged_exercises = n


def record_set_posted(*, pending: bool, inferred: bool) -> None:
    _state.sets_posted += 1
    if pending:
        _state.sets_pending += 1
    if inferred:
        _state.sets_inferred += 1


def record_set_failed(err: str) -> None:
    _state.sets_failed += 1
    _state.last_error = err


def render_metrics() -> str:
    s = _state
    lines = [
        "# HELP pump_voltra_connected 1 when the trainer is connected.",
        "# TYPE pump_voltra_connected gauge",
        f"pump_voltra_connected {int(s.connected)}",
        "# HELP pump_voltra_workout_active 1 when the trainer reports an active workout.",
        "# TYPE pump_voltra_workout_active gauge",
        f"pump_voltra_workout_active {int(s.workout_active)}",
        "# HELP pump_voltra_flagged_exercises Exercises flagged as using the trainer.",
        "# TYPE pump_voltra_flagged_exercises gauge",
        f"pump_voltra_flagged_exercises {s.flagged_exercises}",
        "# HELP pump_voltra_sets_posted_total Sets written to PUMP.",
        "# TYPE pump_voltra_sets_posted_total counter",
        f"pump_voltra_sets_posted_total {s.sets_posted}",
        "# HELP pump_voltra_sets_pending_total Sets written without a name anchor.",
        "# TYPE pump_voltra_sets_pending_total counter",
        f"pump_voltra_sets_pending_total {s.sets_pending}",
        # A rising inferred count means end-of-set summaries are being dropped
        # in transport — the set is still logged, but from the idle timeout.
        "# HELP pump_voltra_sets_inferred_total Sets closed without a device summary.",
        "# TYPE pump_voltra_sets_inferred_total counter",
        f"pump_voltra_sets_inferred_total {s.sets_inferred}",
        "# HELP pump_voltra_sets_failed_total Sets that could not be written.",
        "# TYPE pump_voltra_sets_failed_total counter",
        f"pump_voltra_sets_failed_total {s.sets_failed}",
    ]
    return "\n".join(lines) + "\n"


def build_app():
    """Build the FastAPI probe app. Imported lazily so tests need no fastapi."""
    from fastapi import FastAPI, Response

    app = FastAPI(title="pump-voltra")

    @app.get("/healthz")
    async def healthz() -> dict:
        return {"ok": True}

    @app.get("/readyz")
    async def readyz(response: Response) -> dict:
        # Ready means "talking to the trainer". Not ready is normal when the
        # gym is empty, so this must not page anyone on its own.
        if not _state.connected:
            response.status_code = 503
        return {"connected": _state.connected, "workout_active": _state.workout_active}

    @app.get("/metrics")
    async def metrics() -> Response:
        return Response(content=render_metrics(), media_type="text/plain; version=0.0.4")

    @app.get("/api/v1/state")
    async def get_state() -> dict:
        s = _state
        return {
            "connected": s.connected,
            "workout_active": s.workout_active,
            "flagged_exercises": s.flagged_exercises,
            "sets_posted": s.sets_posted,
            "sets_pending": s.sets_pending,
            "sets_inferred": s.sets_inferred,
            "sets_failed": s.sets_failed,
            "last_error": s.last_error,
        }

    return app
