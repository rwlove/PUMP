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
# Retry cadence before the first successful read (see run_forever).
FIRST_LOAD_RETRY_SECONDS = 5.0


class VoltraFlags:
    """Caches the set of Voltra-flagged exercise names."""

    def __init__(self, pump, refresh_seconds: float = DEFAULT_REFRESH_SECONDS):
        self._pump = pump
        self._refresh_seconds = refresh_seconds
        self._names: set[str] = set()
        self._loaded = False

    @property
    def loaded(self) -> bool:
        """Whether the flag list has ever been read successfully.

        Callers must consult this before trusting a False from __call__: an
        empty cache and "no exercise uses the trainer" are indistinguishable
        otherwise, and guessing wrong means every Voltra set gets logged twice.
        """
        return self._loaded

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
        if names != self._names or not self._loaded:
            logger.info("voltra-flagged exercises updated", count=len(names))
        self._names = names
        self._loaded = True

    async def run_forever(self) -> None:
        """Re-read on a timer so ticking the checkbox in PUMP takes effect
        without a rolling restart.

        Until the first successful read, retry quickly rather than waiting the
        full interval. The usual way to be un-loaded is that pump-cv started
        before pump-api was serving — a node reboot rolls both — and every
        second spent in that state is a second where the sidecar's exercises
        are unaccounted for.
        """
        while True:
            try:
                await self.refresh()
            except asyncio.CancelledError:
                raise
            except Exception as e:
                logger.warning("voltra flag refresh failed",
                               error=str(e), ever_loaded=self._loaded)
            if self._loaded:
                await asyncio.sleep(self._refresh_seconds)
            else:
                await asyncio.sleep(FIRST_LOAD_RETRY_SECONDS)
