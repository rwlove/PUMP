"""Which exercises are performed on the Voltra trainer.

The flag lives on the exercise row in PUMP (a checkbox on its configuration
page) and reaches us through `GET /api/exercises` as `"Voltra": true`.

This replaced a `"voltra" in name.lower()` substring test. A substring is the
wrong tool: the obvious generalisation — matching "cable" — is wrong too,
because plenty of exercises have "cable" in the name and run off a plate stack.
Whether the trainer is involved is a property of the exercise, so it is stored
with the exercise.

Names are matched case-insensitively: `sets.name` is free text with no foreign
key to `exercises`, so spellings drift.
"""

from __future__ import annotations

import asyncio

from . import log

logger = log.get(__name__)

DEFAULT_REFRESH_SECONDS = 300.0


class VoltraFlags:
    """Caches the set of Voltra-flagged exercise names."""

    def __init__(self, pump, refresh_seconds: float = DEFAULT_REFRESH_SECONDS):
        self._pump = pump
        self._refresh_seconds = refresh_seconds
        self._names: set[str] = set()

    def __call__(self, name: str | None) -> bool:
        """Callable so it can be passed straight to PipelineRunner."""
        return bool(name) and name.strip().lower() in self._names

    async def refresh(self) -> None:
        exercises = await self._pump.list_exercises()
        names = {
            str(e.get("Name", "")).strip().lower()
            for e in exercises
            if e.get("Voltra") and str(e.get("Name", "")).strip()
        }
        if names != self._names:
            logger.info("voltra-flagged exercises updated", count=len(names))
        self._names = names

    async def run_forever(self) -> None:
        """Re-read on a timer so ticking the checkbox in PUMP takes effect
        without a rolling restart."""
        while True:
            try:
                await self.refresh()
            except asyncio.CancelledError:
                raise
            except Exception as e:
                logger.warning("voltra flag refresh failed", error=str(e))
            await asyncio.sleep(self._refresh_seconds)
