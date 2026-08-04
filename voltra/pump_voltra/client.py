"""BLE session with the trainer: bootstrap, subscribe, read parameters.

Phase 1 is read-only in the sense that matters — it never commands the motor.
It is not literally write-free: the device streams nothing at all until the
telemetry subscribe is written, and that write (like every write) is rejected
unless a workout is active. Both facts are load-bearing and are enforced here.
"""

from __future__ import annotations

import asyncio
from collections.abc import Callable

from . import registry
from .log import get
from .protocol import (
    CMD_TELEMETRY,
    PARAM_COMMANDS,
    parse_frame,
)

logger = get(__name__)

# GATT characteristics. All outbound writes go to TRANSPORT; responses arrive
# on COMMAND; NOTIFY carries a subset. Subscribe to all three.
SERVICE = "e4dada34-0867-8783-9f70-2ca29216c7e4"
TRANSPORT = "a010891d-f50f-44f0-901f-9a2421a9e050"
COMMAND = "55ca1e52-7354-25de-6afc-b7df1e8816ac"
NOTIFY = "ca94658c-0525-5046-e78b-5391b65f47ad"

# Bootstrap frames, in order. Byte-for-byte reproducible from build_frame()
# except the first, which carries the client-identity blob.
#
# The identity says "iPad" — inherited from the capture this was derived from.
# Changing it to "PUMP" is desirable but re-triggers the device's on-screen
# authorisation prompt, so it stays until there is a reason to re-pair.
BOOTSTRAP = [
    "552904c90110000020004f69506164000000000000000000000000000000000084ab1a5f292001ea4f",
    "550f0801aad200002000ff00aa0419",
    "551f044eaa10000020002781105eab9ef41c864ff5877a9c8c1d5f0d603e86",
    "550d0433aa10000020007403bc",
]


class WorkoutInactive(RuntimeError):
    """The trainer has no active workout, so it will silently ignore writes."""


class VoltraClient:
    """Wraps a connected BleakClient with the trainer's framing."""

    def __init__(self, ble, on_telemetry: Callable[[bytes], None]):
        self._ble = ble
        self._on_telemetry = on_telemetry
        self._params: dict[int, int] = {}
        self._seq = 0x50

    def _next_seq(self) -> int:
        self._seq = (self._seq + 1) & 0xFFFF
        return self._seq

    # ─── notification plumbing ───────────────────────────────────────────

    def _handle(self, _sender, data: bytearray) -> None:
        # Frames arrive already validated by the device; parse leniently so a
        # capture-tool truncation doesn't drop a whole set.
        frame = parse_frame(bytes(data), strict=False)
        if frame is None:
            return
        if frame.command in PARAM_COMMANDS:
            self._params.update(registry.decode_reply(frame.payload))
        elif frame.command == CMD_TELEMETRY:
            self._on_telemetry(frame.payload)

    async def start(self) -> None:
        for char in (COMMAND, NOTIFY, TRANSPORT):
            await self._ble.start_notify(char, self._handle)
        for frame_hex in BOOTSTRAP:
            await self._ble.write_gatt_char(TRANSPORT, bytes.fromhex(frame_hex), response=True)
            await asyncio.sleep(0.09)
        await asyncio.sleep(1.2)
        logger.info("bootstrap complete")

    # ─── parameters ──────────────────────────────────────────────────────

    async def read(self, *param_ids: int, settle_s: float = 0.6) -> dict[int, int]:
        """Read parameters. Replies land asynchronously, hence the settle."""
        await self._ble.write_gatt_char(
            TRANSPORT, registry.encode_read(list(param_ids), self._next_seq()), response=True
        )
        await asyncio.sleep(settle_s)
        return {pid: self._params[pid] for pid in param_ids if pid in self._params}

    def cached(self, param_id: int) -> int | None:
        return self._params.get(param_id)

    async def workout_active(self) -> bool:
        values = await self.read(registry.WORKOUT_STATE)
        return values.get(registry.WORKOUT_STATE) == registry.WORKOUT_WEIGHT_TRAINING

    async def target_load(self) -> int | None:
        return (await self.read(registry.TARGET_LOAD)).get(registry.TARGET_LOAD)

    # ─── telemetry ───────────────────────────────────────────────────────

    async def subscribe_telemetry(self) -> None:
        """Enable the telemetry stream.

        Without this the device streams nothing whatsoever — the single most
        expensive false "the protocol is broken" result of the spike. The
        token must be written before the rate.

        Requires an active workout: with WORKOUT_STATE == 0 the device accepts
        the write, replies with a zero payload, and changes nothing. Failing
        loudly here beats sitting silently in a session that will never
        produce a set.
        """
        if not await self.workout_active():
            raise WorkoutInactive(
                "no active workout on the trainer — writes are silently ignored "
                "until one is started on the device"
            )
        await self._ble.write_gatt_char(
            TRANSPORT,
            registry.encode_write(
                registry.TELEMETRY_TOKEN, registry.TELEMETRY_TOKEN_VALUE, self._next_seq()
            ),
            response=True,
        )
        await asyncio.sleep(0.2)
        await self._ble.write_gatt_char(
            TRANSPORT,
            registry.encode_write(
                registry.TELEMETRY_RATE, registry.TELEMETRY_RATE_40HZ, self._next_seq()
            ),
            response=True,
        )
        await asyncio.sleep(0.2)
        # Note: this enables the 0xAA subtypes we need, which arrive at ~1 Hz.
        # We never read the 40 Hz 0xB4 stream — it is the one thing the
        # ESPHome proxy drops under backpressure.
        logger.info("telemetry subscribed")
