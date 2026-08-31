"""Async HTTP client for the PUMP REST API.

A trimmed port of cv/pump_cv/pump_client.py — same endpoints, same headers,
same SSE decoding — carrying only what the Voltra sidecar uses:

  GET    /api/exercises      — which exercises carry the Voltra flag
  GET    /api/sets           — today's sets, to find the naming anchor
  POST   /api/sets           — append an auto-logged set
  GET    /api/sets/stream    — SSE: add / update / delete / bulk

The X-Api-Key header is sent on every request when configured.
"""

from __future__ import annotations

import json
from collections.abc import AsyncIterator
from dataclasses import dataclass
from typing import Any

import httpx
from httpx_sse import aconnect_sse

from . import log

logger = log.get(__name__)


@dataclass(slots=True)
class SetEvent:
    """One event from the /api/sets/stream SSE feed."""

    type: str  # "add" | "update" | "delete" | "bulk"
    id: int | None
    date: str | None
    set: dict[str, Any] | None  # the full Set struct, or None for delete/bulk


class AutoLogDisabled(PermissionError):
    """PUMP refused the write because VOLTRA_AUTOLOG is off on the server."""


class PumpClient:
    def __init__(
        self,
        base_url: str,
        api_key: str = "",
        timeout_s: float = 10.0,
        sse_read_timeout_s: float = 60.0,
    ):
        self._base = base_url.rstrip("/")
        self._timeout_s = timeout_s
        # Finite read-idle limit for the SSE streams. Must exceed PUMP's 25 s
        # keepalive so a healthy idle stream is not torn down, but stay finite
        # so a half-open connection surfaces as a ReadTimeout the watch loops
        # reconnect from instead of hanging forever (read=None did the latter).
        self._sse_read_timeout_s = sse_read_timeout_s
        headers = {"Content-Type": "application/json"}
        if api_key:
            headers["X-Api-Key"] = api_key
        self._client = httpx.AsyncClient(base_url=self._base, headers=headers, timeout=timeout_s)

    async def aclose(self) -> None:
        await self._client.aclose()

    async def __aenter__(self) -> PumpClient:
        return self

    async def __aexit__(self, *exc) -> None:
        await self.aclose()

    async def list_exercises(self) -> list[dict[str, Any]]:
        r = await self._client.get("/api/exercises")
        r.raise_for_status()
        return r.json() or []

    async def list_sets(self) -> list[dict[str, Any]]:
        r = await self._client.get("/api/sets")
        r.raise_for_status()
        return r.json() or []

    async def post_set(self, payload: dict[str, Any]) -> int:
        """Insert a single set. Returns the new id."""
        r = await self._client.post("/api/sets", json=payload)
        if r.status_code == 403:
            # The operator has the toggle off, presumably deliberately. Fail
            # loud rather than retrying a write that will never be accepted.
            raise AutoLogDisabled(r.json().get("error", "Voltra auto-log is disabled"))
        r.raise_for_status()
        return int(r.json()["id"])

    # ─── voltra coordination ──────────────────────────────────────────────

    async def report_voltra(self, loaded: bool, error: str = "") -> None:
        """Tell PUMP what the motor is actually doing.

        PUMP keeps desired and actual apart and disbelieves a stale Loaded
        claim, so this doubles as the liveness signal.
        """
        r = await self._client.post(
            "/api/voltra/report", json={"Loaded": loaded, "Error": error}
        )
        r.raise_for_status()

    async def stream_voltra_state(self) -> AsyncIterator[dict]:
        """Follow PUMP's desired Voltra state.

        Read timeout is loosened for the same reason as the sets stream: an SSE
        connection is idle by design between changes, and the client-wide 10 s
        read limit would tear down a healthy subscription. It is set to a finite
        value above PUMP's 25 s keepalive rather than disabled, so a half-open
        connection raises ReadTimeout (which watch() reconnects from and unloads
        the cable on) instead of wedging the motor-control stream forever.
        """
        timeout = httpx.Timeout(self._timeout_s, read=self._sse_read_timeout_s)
        async with aconnect_sse(
            self._client, "GET", "/api/voltra/stream", timeout=timeout
        ) as source:
            async for sse in source.aiter_sse():
                if not sse.data:
                    continue
                try:
                    yield json.loads(sse.data)
                except json.JSONDecodeError:
                    logger.warning("voltra sse: bad json", raw=sse.data[:200])

    async def stream_set_events(self) -> AsyncIterator[SetEvent]:
        """Subscribe to the SSE feed. Reconnects are the caller's business.

        The read timeout is loosened **for this request only**. An SSE stream
        is idle by design between events, and PUMP only sends its keepalive
        comment every 25 s (see getSetsStream in internal/api/server.go), so
        the client-wide 10 s read timeout fires first and tears down a
        perfectly healthy stream roughly every 15 s. It is raised to a finite
        value above the keepalive rather than disabled — `read=None` blocks
        forever on a half-open connection, and the caller only reconnects on an
        exception. Connect and write timeouts are left in place.
        """
        timeout = httpx.Timeout(self._timeout_s, read=self._sse_read_timeout_s)
        async with aconnect_sse(
            self._client, "GET", "/api/sets/stream", timeout=timeout
        ) as source:
            async for sse in source.aiter_sse():
                if not sse.data:
                    continue
                try:
                    data = json.loads(sse.data)
                except json.JSONDecodeError:
                    logger.warning("sse: bad json", raw=sse.data[:200])
                    continue
                yield SetEvent(
                    type=sse.event or data.get("type", ""),
                    id=data.get("id"),
                    date=data.get("date"),
                    set=data.get("set"),
                )
