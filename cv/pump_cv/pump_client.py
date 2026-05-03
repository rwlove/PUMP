"""Async HTTP client for the PUMP REST API.

Mirrors the endpoints exposed by `internal/api` in the Go service:

  POST   /api/sets             — append a single set; returns {"id": N}
  PATCH  /api/sets/{id}        — partial update
  POST   /api/sets/{id}/confirm — clear pending; optional edits in body
  DELETE /api/sets/{id}
  GET    /api/sets             — list all sets
  GET    /api/sets/stream      — SSE: add / update / delete / bulk events
  GET    /api/exercises        — list exercises (used to map detections → names)

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


class PumpClient:
    def __init__(self, base_url: str, api_key: str = "", timeout_s: float = 10.0):
        self._base = base_url.rstrip("/")
        headers = {"Content-Type": "application/json"}
        if api_key:
            headers["X-Api-Key"] = api_key
        self._client = httpx.AsyncClient(
            base_url=self._base,
            headers=headers,
            timeout=timeout_s,
        )

    async def aclose(self) -> None:
        await self._client.aclose()

    async def __aenter__(self) -> PumpClient:
        return self

    async def __aexit__(self, *exc) -> None:
        await self.aclose()

    # ─── exercises ────────────────────────────────────────────────────────

    async def list_exercises(self) -> list[dict[str, Any]]:
        r = await self._client.get("/api/exercises")
        r.raise_for_status()
        return r.json() or []

    # ─── sets ─────────────────────────────────────────────────────────────

    async def list_sets(self) -> list[dict[str, Any]]:
        r = await self._client.get("/api/sets")
        r.raise_for_status()
        return r.json() or []

    async def post_set(self, payload: dict[str, Any]) -> int:
        """Insert a single set. Returns the new id."""
        r = await self._client.post("/api/sets", json=payload)
        if r.status_code == 403:
            # CVAutoLog is off on the server — fail loud, the operator
            # presumably has a reason to keep CV writes from landing.
            raise PermissionError(r.json().get("error", "CVAutoLog disabled"))
        r.raise_for_status()
        return int(r.json()["id"])

    async def patch_set(self, set_id: int, fields: dict[str, Any]) -> None:
        r = await self._client.patch(f"/api/sets/{set_id}", json=fields)
        r.raise_for_status()

    async def confirm_set(self, set_id: int, edits: dict[str, Any] | None = None) -> None:
        r = await self._client.post(f"/api/sets/{set_id}/confirm", json=edits or {})
        r.raise_for_status()

    async def delete_set(self, set_id: int) -> None:
        r = await self._client.delete(f"/api/sets/{set_id}")
        if r.status_code not in (200, 204, 404):
            r.raise_for_status()

    # ─── wall display ────────────────────────────────────────────────────

    async def post_wall_wake(self) -> None:
        r = await self._client.post("/api/wall/wake")
        r.raise_for_status()

    async def post_wall_sleep(self) -> None:
        r = await self._client.post("/api/wall/sleep")
        r.raise_for_status()

    # ─── SSE ─────────────────────────────────────────────────────────────

    async def stream_set_events(self) -> AsyncIterator[SetEvent]:
        """Subscribe to the SSE feed. Yields events forever; reconnects are
        the caller's responsibility (httpx_sse closes on disconnect)."""
        async with aconnect_sse(self._client, "GET", "/api/sets/stream") as event_source:
            async for sse in event_source.aiter_sse():
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
