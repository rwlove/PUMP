"""Motor control: the only code here that puts tension on a cable.

Every rule in this module exists because the failure mode is physical rather
than a bad row in a table. They are preconditions, not best-effort:

* **Clamp every write.** A bug computing a garbage weight must be refused here,
  not applied to a motor. The device accepts up to 200 lb; this service does
  not.
* **Verify by read-back before engaging.** A write can be silently ignored —
  the device does exactly that when no workout is active — and engaging after
  one applies the *previous* weight. Read it back and abort on mismatch.
* **Change weight only while disengaged.** Never write a target load under
  tension.
* **Never load on reconnect or startup.** Loading is only ever the direct
  result of an explicit request.
* **Never replay a queued command after reconnect.** The athlete has moved.
* **Always release on shutdown**, including SIGTERM and unhandled exceptions.

The last one is cheaper than it looks: load auto-expires ~8 s after the last
keepalive, so simply *stopping* releases the cable. The keepalive is the thing
holding tension, which makes "stop doing anything" the safe default rather than
something we have to remember to do.
"""

from __future__ import annotations

import asyncio

from . import registry
from .client import WorkoutInactive
from .log import get

logger = get(__name__)

# Re-assert cadence. The device drops the load ~8 s after the last assertion,
# so this must stay comfortably under that.
KEEPALIVE_S = 5.0


class LoadRefused(RuntimeError):
    """A load was rejected before the motor was engaged. Nothing was applied."""


class MotorController:
    """Owns the engaged/released state of the trainer's motor."""

    def __init__(self, client, max_load_lb: int):
        self._client = client
        self._max = max_load_lb
        self._task: asyncio.Task | None = None
        self._weight: int | None = None

    @property
    def loaded(self) -> bool:
        return self._task is not None and not self._task.done()

    @property
    def weight_lb(self) -> int | None:
        return self._weight if self.loaded else None

    async def load(self, weight_lb: int) -> None:
        """Engage the motor at weight_lb. Raises LoadRefused without engaging
        if anything about the request or the device state is wrong."""
        if weight_lb <= 0:
            raise LoadRefused(f"refusing a non-positive weight ({weight_lb} lb)")
        if weight_lb > self._max:
            raise LoadRefused(
                f"refusing {weight_lb} lb — above the {self._max} lb clamp. "
                "Raise VOLTRA_MAX_LOAD_LB deliberately if this is real work."
            )

        # Writes are silently ignored with no active workout: the device
        # accepts the frame, replies with a zero payload, and changes nothing.
        # Engaging after that would apply whatever load was already set.
        if not await self._client.workout_active():
            raise WorkoutInactive(
                "no active workout on the trainer — start one on the device"
            )

        # Stop the previous keepalive before touching anything.
        #
        # Re-loading at a new weight used to leave the running task alive and
        # then overwrite self._task, so unload() cancelled only the newest one.
        # The orphan kept re-asserting MODE_LOADED every 5 s and the cable
        # stayed live indefinitely while the UI reported it slack. An orphan
        # could also satisfy the mode read-back below, hiding a write that
        # never landed.
        await self._stop_keepalive()

        # Weight only ever changes while disengaged. A release that did not
        # land must abort: writing a new target under tension is exactly what
        # this ordering exists to prevent, and the device's ~8 s auto-expiry is
        # far too slow to cover the ~0.4 s until the next write.
        await self._release_motor(required=True)

        await self._client.write(registry.TARGET_LOAD, weight_lb)
        readback = (await self._client.read(registry.TARGET_LOAD)).get(registry.TARGET_LOAD)
        if readback != weight_lb:
            raise LoadRefused(
                f"target load read back as {readback!r}, expected {weight_lb} — "
                "not engaging; the device would have applied the previous weight"
            )

        await self._client.write(registry.FITNESS_MODE, registry.MODE_LOADED)
        mode = (await self._client.read(registry.FITNESS_MODE)).get(registry.FITNESS_MODE)
        if mode != registry.MODE_LOADED:
            # Leave it released rather than half-engaged.
            await self._release_motor()
            raise LoadRefused(f"fitness mode read back as {mode!r}, expected loaded")

        self._weight = weight_lb
        self._task = asyncio.create_task(self._keepalive())
        logger.info("motor engaged", weight_lb=weight_lb)

    async def unload(self) -> None:
        """Release the motor. Safe to call when already released."""
        await self._stop_keepalive()
        await self._release_motor()
        self._weight = None
        logger.info("motor released")

    async def _keepalive(self) -> None:
        """Hold the load by re-asserting it. Stopping this releases the cable,
        which is why every failure path simply stops."""
        try:
            while True:
                await asyncio.sleep(KEEPALIVE_S)
                await self._client.write(registry.FITNESS_MODE, registry.MODE_LOADED)
        except asyncio.CancelledError:
            raise
        except Exception as e:
            # Do not retry. Stopping is the safe outcome: the device releases
            # on its own within ~8 s, which is what we want if we have lost
            # confidence in our ability to talk to it.
            logger.warning("keepalive failed; letting the load expire", error=str(e))

    async def _stop_keepalive(self) -> None:
        if self._task is None:
            return
        self._task.cancel()
        try:
            await self._task
        except (asyncio.CancelledError, Exception):
            pass
        self._task = None

    async def _release_motor(self, required: bool = False) -> None:
        """Tell the device to release.

        required=True is for the disengage-before-write step, where auto-expiry
        is not an acceptable backstop because the next write follows in
        milliseconds. Everywhere else a failed release is survivable: we have
        already stopped asserting, so the device drops the load within ~8 s.
        """
        try:
            await self._client.write(registry.FITNESS_MODE, registry.MODE_UNLOADED)
        except Exception as e:
            if required:
                raise LoadRefused(
                    f"could not release the motor before changing weight ({e}); "
                    "refusing to write a new target under tension"
                ) from e
            logger.warning("explicit release failed; relying on auto-expiry", error=str(e))
