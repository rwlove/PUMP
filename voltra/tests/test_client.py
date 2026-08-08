"""VoltraClient — the parameter cache that every safety gate reads from.

The motor's read-back gates, and the workout-active check that authorises any
write at all, all resolve through this cache. Both defects covered here made a
gate pass on something the device never said.
"""

from __future__ import annotations

import asyncio

import pytest

from pump_voltra import registry
from pump_voltra.client import TRANSPORT, VoltraClient
from pump_voltra.protocol import CMD_PARAM_READ, build_frame


def param_reply(values: dict[int, int]) -> bytes:
    """A well-formed parameter-reply frame carrying the given values."""
    payload = bytes((0,)) + len(values).to_bytes(2, "little")
    for pid, val in values.items():
        payload += pid.to_bytes(2, "little")
        payload += val.to_bytes(registry.width_of(pid), "little")
    return build_frame(CMD_PARAM_READ, payload)


class FakeBLE:
    """Records writes; delivers whatever notifications a test chooses."""

    def __init__(self):
        self.writes: list[bytes] = []
        self._handlers: dict[str, object] = {}

    async def start_notify(self, char, handler):
        self._handlers[char] = handler

    async def write_gatt_char(self, char, data, response=True):
        self.writes.append(bytes(data))

    def deliver(self, frame: bytes) -> None:
        self._handlers[TRANSPORT](None, bytearray(frame))


async def attached() -> tuple[FakeBLE, VoltraClient]:
    ble = FakeBLE()
    client = VoltraClient(ble, lambda _payload: None)
    # Register the notification handler without running the bootstrap writes.
    await ble.start_notify(TRANSPORT, client._handle)
    return ble, client


# ─── S2: a dropped reply must not read as agreement ──────────────────────────


async def answering(ble: FakeBLE, values: dict[int, int]) -> asyncio.Task:
    """Deliver a reply while a read is settling, as the device would."""

    async def go():
        await asyncio.sleep(0.01)
        ble.deliver(param_reply(values))

    return asyncio.create_task(go())


async def test_read_returns_nothing_when_no_reply_arrives() -> None:
    """The whole point: absent reply → absent key, not a stale value.

    read() used to return whatever was already cached, so a reply lost in the
    ESPHome proxy was indistinguishable from a confirming one and the motor
    read-back gate passed on a minutes-old value.
    """
    ble, client = await attached()

    task = await answering(ble, {registry.TARGET_LOAD: 50})
    assert await client.read(registry.TARGET_LOAD, settle_s=0.05) == {
        registry.TARGET_LOAD: 50
    }
    await task

    # Same read again, but the device stays silent this time.
    got = await client.read(registry.TARGET_LOAD, settle_s=0.05)
    assert got == {}, f"returned a stale cached value {got}"


async def test_read_returns_the_value_when_a_reply_does_arrive() -> None:
    ble, client = await attached()

    async def reply_soon():
        await asyncio.sleep(0.01)
        ble.deliver(param_reply({registry.FITNESS_MODE: registry.MODE_LOADED}))

    task = asyncio.create_task(reply_soon())
    got = await client.read(registry.FITNESS_MODE, settle_s=0.05)
    await task
    assert got == {registry.FITNESS_MODE: registry.MODE_LOADED}


async def test_workout_active_is_false_when_the_device_does_not_answer() -> None:
    """Failing closed matters: workout_active authorises every motor write."""
    _, client = await attached()
    assert await client.workout_active() is False


async def test_workout_active_does_not_reuse_an_earlier_answer() -> None:
    ble, client = await attached()
    task = await answering(
        ble, {registry.WORKOUT_STATE: registry.WORKOUT_WEIGHT_TRAINING}
    )
    assert await client.workout_active() is True
    await task
    # Device has gone quiet — the previous "yes" must not carry over, or a
    # dead link would keep authorising motor writes.
    assert await client.workout_active() is False


# ─── S3: CRC is verified on the live path ────────────────────────────────────


async def test_corrupt_frame_is_rejected() -> None:
    """A corrupt frame must not be able to assert 'workout active'.

    The device is not the last hop — an ESP32 and a TCP link sit in between —
    and this frame's payload feeds the gate that authorises motor writes.
    """
    ble, client = await attached()

    frame = bytearray(
        param_reply({registry.WORKOUT_STATE: registry.WORKOUT_WEIGHT_TRAINING})
    )
    frame[-1] ^= 0xFF  # break the body CRC16
    ble.deliver(bytes(frame))

    assert client.cached(registry.WORKOUT_STATE) is None
    assert await client.workout_active() is False


async def test_frame_with_bad_header_crc_is_rejected() -> None:
    ble, client = await attached()
    frame = bytearray(param_reply({registry.TARGET_LOAD: 99}))
    frame[3] ^= 0xFF  # break the header CRC8
    ble.deliver(bytes(frame))
    assert client.cached(registry.TARGET_LOAD) is None


async def test_valid_frame_is_accepted() -> None:
    """Guards against the CRC check being too strict to accept real traffic."""
    ble, client = await attached()
    ble.deliver(param_reply({registry.TARGET_LOAD: 65}))
    assert client.cached(registry.TARGET_LOAD) == 65


@pytest.mark.parametrize("junk", [b"", b"\x55", b"\x00" * 20, b"\x55" + b"\x00" * 12])
async def test_malformed_input_does_not_raise(junk: bytes) -> None:
    ble, client = await attached()
    ble.deliver(junk)  # must not raise
    assert client.cached(registry.TARGET_LOAD) is None
