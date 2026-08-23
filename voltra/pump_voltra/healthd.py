"""Probe and metrics endpoints, a thin version of cv/pump_cv/healthd.py.

Probes stay unauthenticated so the kubelet can reach them.
"""

from __future__ import annotations

import time
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
    # Unix time of the last work-loop progress tick. Initialised to now so the
    # liveness gate has a full grace window at startup rather than tripping
    # before the first tick. A frozen value is the signature of a wedged loop:
    # the pod keeps serving probes while _run_live has stopped making progress.
    heartbeat_ts: float = field(default_factory=time.time)
    extra: dict = field(default_factory=dict)


_state = State()

# How long the work loop may go without a progress tick before liveness fails
# and the kubelet restarts the pod. Must exceed the longest legitimate gap
# between ticks — the trainer-discovery wait — so an empty gym is never
# mistaken for a wedge. Overridable from config.
_heartbeat_stale_after = 600.0


def state() -> State:
    return _state


def set_heartbeat_stale_after(seconds: float) -> None:
    global _heartbeat_stale_after
    _heartbeat_stale_after = seconds


def record_heartbeat() -> None:
    """Mark the work loop as having made progress just now."""
    _state.heartbeat_ts = time.time()


def heartbeat_age() -> float:
    return time.time() - _state.heartbeat_ts


def heartbeat_stale() -> bool:
    return heartbeat_age() > _heartbeat_stale_after


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
        # Freshness of the work loop. `time() - this` in a Prometheus rule
        # detects a wedged sidecar (probes still up, loop dead) that a plain
        # scrape can't — the other metrics simply freeze at their last values.
        "# HELP pump_voltra_heartbeat_timestamp_seconds Unix time of the last work-loop tick.",
        "# TYPE pump_voltra_heartbeat_timestamp_seconds gauge",
        f"pump_voltra_heartbeat_timestamp_seconds {s.heartbeat_ts}",
    ]
    return "\n".join(lines) + "\n"


def build_app():
    """Build the FastAPI probe app. Imported lazily so tests need no fastapi."""
    from fastapi import FastAPI, Response

    app = FastAPI(title="pump-voltra")

    @app.get("/healthz")
    async def healthz(response: Response) -> dict:
        # Liveness watchdog. The loop stamps a heartbeat each iteration; if it
        # goes stale the loop has wedged (a hung BLE/proxy await, a dead work
        # task) even though this server is still up. Failing here lets the
        # kubelet restart the pod instead of leaving a zombie that reports
        # healthy forever — the failure mode that silently killed a workout's
        # auto-load. Empty-gym waiting ticks well inside the threshold, so this
        # never fires on a merely-idle sidecar.
        if heartbeat_stale():
            response.status_code = 503
            return {"ok": False, "reason": "work loop stalled",
                    "heartbeat_age_s": round(heartbeat_age(), 1)}
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
