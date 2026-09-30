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
    # Count of UNEXPECTED losses of the always-on ESPHome proxy session (the
    # gym ESP32 resetting, a Wi-Fi blip, an idle EOF) — i.e. on_stop with
    # expected=False. Distinct from the trainer being switched off, which is the
    # normal idle state and is never counted here. A single increment is the
    # signature of the failure that drops a mid-workout LOAD: the proxy vanished
    # from under a live BLE session.
    proxy_disconnects: int = 0
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


def record_proxy_disconnect() -> None:
    """Count one UNEXPECTED loss of the ESPHome proxy session.

    Only unexpected drops (on_stop expected=False) are recorded; a planned
    teardown on shutdown/reconnect is not a fault. The trainer being powered
    off never reaches here — that path never establishes a proxy stop event.
    """
    _state.proxy_disconnects += 1


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
        # A single increment is the mid-workout failure that drops a LOAD: the
        # always-on gym proxy vanished under a live BLE session. Alert on
        # increase(...) >= 1 over a short window. Excludes the trainer being off.
        "# HELP pump_voltra_proxy_disconnects_total Unexpected ESPHome proxy session drops.",
        "# TYPE pump_voltra_proxy_disconnects_total counter",
        f"pump_voltra_proxy_disconnects_total {s.proxy_disconnects}",
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
    from fastapi.responses import JSONResponse

    app = FastAPI(title="pump-voltra")

    # Status is set by returning a JSONResponse, not by injecting a `response:
    # Response` parameter. `from __future__ import annotations` stringizes the
    # annotation, and FastAPI then fails to recognise the Response injection —
    # it treats `response` as a required query param and every probe 422s,
    # which crash-loops the pod when this backs a liveness probe.

    @app.get("/healthz")
    async def healthz():
        # Liveness watchdog. The loop stamps a heartbeat each iteration; if it
        # goes stale the loop has wedged (a hung BLE/proxy await, a dead work
        # task) even though this server is still up. Failing here lets the
        # kubelet restart the pod instead of leaving a zombie that reports
        # healthy forever — the failure mode that silently killed a workout's
        # auto-load. Empty-gym waiting ticks well inside the threshold, so this
        # never fires on a merely-idle sidecar.
        if heartbeat_stale():
            return JSONResponse(
                status_code=503,
                content={"ok": False, "reason": "work loop stalled",
                         "heartbeat_age_s": round(heartbeat_age(), 1)},
            )
        return {"ok": True}

    @app.get("/readyz")
    async def readyz():
        # Ready means "talking to the trainer". Not ready is normal when the
        # gym is empty, so this must not page anyone on its own.
        if not _state.connected:
            return JSONResponse(
                status_code=503,
                content={"connected": False, "workout_active": _state.workout_active},
            )
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
            "proxy_disconnects": s.proxy_disconnects,
            "last_error": s.last_error,
        }

    return app
