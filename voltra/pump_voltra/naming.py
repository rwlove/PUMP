"""Decides what to call an auto-logged set.

The trainer knows resistance, set number and rep count, but has no idea
whether the athlete is rowing or curling. So the name comes from PUMP itself:
the most recent set logged today for an exercise flagged `Voltra` on its
configuration page. Tap "Cable Row", log set 1 by hand, and every following set
inherits that name until you tap something else.

Two independent inputs:

  * the flagged-exercise list, from GET /api/exercises, refreshed on a timer;
  * the anchor set, seeded from GET /api/sets and then kept current from the
    SSE feed.

Names are matched case-insensitively because `sets.name` is free text with no
foreign key to `exercises` — the same exercise can be spelled differently in
an old row.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from .log import get

logger = get(__name__)


@dataclass(frozen=True, slots=True)
class Anchor:
    """The exercise an auto-logged set should be attributed to."""

    name: str
    color: str
    workout_color: str


class ExerciseNamer:
    def __init__(self, default_exercise: str = "Voltra") -> None:
        self._default = default_exercise
        self._flagged: set[str] = set()
        self._anchor: Anchor | None = None
        self._anchor_date: str | None = None

    # ─── flagged-exercise list ───────────────────────────────────────────

    def set_exercises(self, exercises: list[dict[str, Any]]) -> None:
        """Record which exercise names carry the Voltra flag."""
        flagged = {
            str(e.get("Name", "")).strip().lower()
            for e in exercises
            if e.get("Voltra") and str(e.get("Name", "")).strip()
        }
        if flagged != self._flagged:
            logger.info("voltra-flagged exercises updated", count=len(flagged))
        self._flagged = flagged

    @property
    def flagged_count(self) -> int:
        return len(self._flagged)

    def is_flagged(self, name: str | None) -> bool:
        return bool(name) and name.strip().lower() in self._flagged

    # ─── anchor tracking ─────────────────────────────────────────────────

    def seed_from_sets(self, sets: list[dict[str, Any]], today: str) -> None:
        """Pick the anchor from today's existing sets.

        /api/sets returns id-ascending, and PUMP orders a day by insertion,
        so the last flagged match is the most recent one.
        """
        for s in reversed(sets):
            if s.get("Date") != today:
                continue
            if self.is_flagged(s.get("Name")):
                self._adopt(s, today)
                return

    def observe_set(self, s: dict[str, Any] | None, date: str | None, today: str) -> None:
        """Update the anchor from an SSE add/update event."""
        if not s or date != today:
            return
        # Never anchor on our own writes: they already carry the anchor's
        # name, so adopting them is at best a no-op and at worst latches a
        # name the athlete has since corrected on a different row.
        if s.get("Source") == "voltra":
            return
        if self.is_flagged(s.get("Name")):
            self._adopt(s, today)

    def _adopt(self, s: dict[str, Any], today: str) -> None:
        anchor = Anchor(
            name=str(s.get("Name", "")),
            color=str(s.get("Color", "")),
            workout_color=str(s.get("WorkoutColor", "")),
        )
        if anchor != self._anchor:
            logger.info("naming anchor set", exercise=anchor.name)
        self._anchor = anchor
        self._anchor_date = today

    def reset_for_date(self, today: str) -> None:
        """Drop an anchor left over from a previous day."""
        if self._anchor_date is not None and self._anchor_date != today:
            logger.info("clearing stale naming anchor", previous_date=self._anchor_date)
            self._anchor = None
            self._anchor_date = None

    # ─── output ──────────────────────────────────────────────────────────

    def resolve(self, today: str) -> tuple[Anchor, bool]:
        """Return (anchor, pending).

        pending=True means we had to fall back to the configured default
        name, so the athlete needs to correct it in the UI. That is the
        degraded path — normally the athlete has already logged set 1.
        """
        self.reset_for_date(today)
        if self._anchor is not None:
            return self._anchor, False
        return Anchor(name=self._default, color="", workout_color=""), True
