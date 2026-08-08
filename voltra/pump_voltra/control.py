"""Reconciles PUMP's desired state onto the trainer.

PUMP holds intent (which set is armed, whether it should be loaded); this loop
makes the hardware match, and reports back what actually happened. The two are
kept apart on purpose: a load can be refused, and the UI must show what the
motor is doing rather than what was asked of it.

Reconciliation rather than command-handling is deliberate. A command queue
would need replay semantics after a reconnect, and replaying a stale load onto
a machine the athlete has since walked away from is exactly what the safety
rules forbid. Converging on current desired state has no such hazard: whatever
we missed while disconnected is simply irrelevant by the time we reconnect.
"""

from __future__ import annotations

import asyncio

from .client import WorkoutInactive
from .log import get
from .motor import LoadRefused, MotorController

logger = get(__name__)


class Controller:
    def __init__(self, pump, motor: MotorController, max_load_lb: int):
        self._pump = pump
        self._motor = motor
        self._max = max_load_lb
        self._armed_set_id = 0

    @property
    def armed_set_id(self) -> int:
        return self._armed_set_id

    def recording(self) -> bool:
        """Whether a completed set may be written right now.

        Both halves required: armed selects what to attribute the set to, and
        loaded is what makes PUMP's weight a true statement about the machine.
        PUMP enforces this too — this is the near side of the same gate.
        """
        return self._armed_set_id != 0 and self._motor.loaded

    async def apply(self, state: dict) -> None:
        """Converge the hardware on one desired state from PUMP."""
        self._armed_set_id = int(state.get("ArmedSetID") or 0)
        want_load = bool(state.get("WantLoad"))
        weight = _parse_weight(state.get("WeightLb"))

        if not want_load or self._armed_set_id == 0:
            if self._motor.loaded:
                await self._motor.unload()
                await self._report()
            return

        if weight is None:
            await self._report(error=f"unusable weight {state.get('WeightLb')!r}")
            return

        # Already holding exactly this — do nothing. Re-engaging would cycle
        # the motor under an athlete mid-set.
        if self._motor.loaded and self._motor.weight_lb == weight:
            return

        try:
            await self._motor.load(weight)
            await self._report()
        except (LoadRefused, WorkoutInactive) as e:
            # Expected refusals: report them so the UI shows why, and leave the
            # motor released.
            logger.info("load refused", reason=str(e), weight_lb=weight)
            await self._report(error=str(e))
        except Exception as e:
            logger.error("load failed", error=str(e), weight_lb=weight)
            await self._report(error=str(e))

    async def _report(self, error: str = "") -> None:
        try:
            await self._pump.report_voltra(self._motor.loaded, error)
        except Exception as e:
            logger.warning("could not report motor state to PUMP", error=str(e))

    async def watch(self) -> None:
        """Follow PUMP's desired state forever, reconnecting as needed."""
        while True:
            try:
                async for state in self._pump.stream_voltra_state():
                    await self.apply(state)
            except asyncio.CancelledError:
                raise
            except Exception as e:
                logger.warning(
                    "voltra state stream dropped; reconnecting",
                    error=str(e) or "<no message>",
                    error_type=type(e).__name__,
                )
                # Losing sight of intent must not leave the cable live. Stop
                # asserting and let the device release.
                if self._motor.loaded:
                    await self._motor.unload()
                await asyncio.sleep(5)

    async def heartbeat(self, interval_s: float = 5.0) -> None:
        """Tell PUMP we are alive and what the motor is doing.

        PUMP disbelieves a Loaded flag that has gone stale, because a sidecar
        that died holding the motor left a claim the device has since released.
        This is what keeps a true claim from going stale.
        """
        while True:
            await asyncio.sleep(interval_s)
            await self._report()


def _parse_weight(raw) -> int | None:
    """PUMP sends weight as a decimal string. The trainer takes whole pounds."""
    if raw in (None, ""):
        return None
    try:
        w = int(round(float(raw)))
    except (TypeError, ValueError):
        return None
    return w if w > 0 else None
