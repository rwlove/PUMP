"""Motor safety rules.

These are the tests that matter most in this repo. Everything else here risks a
wrong row; this risks a cable under tension. Each test names the rule it pins
down, because a future refactor that quietly drops one of these would not be
caught by anything else.
"""

from __future__ import annotations

import asyncio

import pytest

from pump_voltra import registry
from pump_voltra.client import WorkoutInactive
from pump_voltra.motor import LoadRefused, MotorController
from tests.conftest import FakeClient


async def test_clamp_refuses_overweight_before_touching_the_device() -> None:
    c = FakeClient()
    m = MotorController(c, max_load_lb=130)
    with pytest.raises(LoadRefused, match="above the 130 lb clamp"):
        await m.load(200)
    assert c.writes == [], "a refused weight must never reach the device"
    assert not m.loaded


async def test_clamp_allows_the_boundary() -> None:
    c = FakeClient()
    m = MotorController(c, max_load_lb=130)
    await m.load(130)
    assert m.loaded
    await m.unload()


async def test_nonpositive_weight_refused() -> None:
    c = FakeClient()
    m = MotorController(c, max_load_lb=130)
    for bad in (0, -5):
        with pytest.raises(LoadRefused):
            await m.load(bad)
    assert c.writes == []


async def test_refuses_when_no_workout_is_active() -> None:
    # Writes are silently ignored with no active workout, so engaging anyway
    # would apply whatever load the device already held.
    c = FakeClient(workout=False)
    m = MotorController(c, max_load_lb=130)
    with pytest.raises(WorkoutInactive):
        await m.load(50)
    assert c.writes == []
    assert not m.loaded


async def test_readback_mismatch_aborts_without_engaging() -> None:
    # THE important one. A silently-failed weight write followed by an engage
    # applies the PREVIOUS weight — the athlete lifts something they did not ask
    # for. The read-back gate must stop before the motor is engaged.
    c = FakeClient(readback=20)  # device claims 20 no matter what we write
    m = MotorController(c, max_load_lb=130)
    with pytest.raises(LoadRefused, match="read back as 20"):
        await m.load(50)
    assert registry.MODE_LOADED not in c.modes_written(), "engaged despite a bad read-back"
    assert not m.loaded


async def test_mode_readback_mismatch_leaves_it_released() -> None:
    c = FakeClient(mode_readback=registry.MODE_UNLOADED)
    m = MotorController(c, max_load_lb=130)
    with pytest.raises(LoadRefused, match="fitness mode"):
        await m.load(50)
    assert not m.loaded
    # Must end released, not half-engaged.
    assert c.modes_written()[-1] == registry.MODE_UNLOADED


async def test_weight_is_only_written_while_disengaged() -> None:
    # Never change target load under tension: disengage must precede the write.
    c = FakeClient()
    m = MotorController(c, max_load_lb=130)
    await m.load(50)

    seq = c.writes
    load_idx = next(i for i, (p, _) in enumerate(seq) if p == registry.TARGET_LOAD)
    before = [v for p, v in seq[:load_idx] if p == registry.FITNESS_MODE]
    assert registry.MODE_UNLOADED in before, "target load written without disengaging first"
    await m.unload()


async def test_changing_weight_cycles_the_motor() -> None:
    c = FakeClient()
    m = MotorController(c, max_load_lb=130)
    await m.load(50)
    c.writes.clear()
    await m.load(60)

    modes = c.modes_written()
    assert modes[0] == registry.MODE_UNLOADED, "did not disengage before the new weight"
    assert modes[-1] == registry.MODE_LOADED, "did not re-engage after the new weight"
    assert c.loads_written() == [60]
    await m.unload()


async def test_unload_is_idempotent() -> None:
    c = FakeClient()
    m = MotorController(c, max_load_lb=130)
    await m.unload()  # never loaded
    await m.load(50)
    await m.unload()
    await m.unload()  # again
    assert not m.loaded


async def test_keepalive_reasserts_the_load() -> None:
    # Tension is held by re-assertion; without this the device drops it in ~8 s.
    import pump_voltra.motor as motor

    orig = motor.KEEPALIVE_S
    motor.KEEPALIVE_S = 0.01
    try:
        c = FakeClient()
        m = MotorController(c, max_load_lb=130)
        await m.load(50)
        c.writes.clear()
        await asyncio.sleep(0.06)
        assert c.modes_written().count(registry.MODE_LOADED) >= 2, "load was not re-asserted"
        await m.unload()
    finally:
        motor.KEEPALIVE_S = orig


async def test_unload_stops_the_keepalive() -> None:
    # Stopping re-assertion IS the release, so this must actually stop.
    import pump_voltra.motor as motor

    orig = motor.KEEPALIVE_S
    motor.KEEPALIVE_S = 0.01
    try:
        c = FakeClient()
        m = MotorController(c, max_load_lb=130)
        await m.load(50)
        await m.unload()
        c.writes.clear()
        await asyncio.sleep(0.06)
        assert c.writes == [], "keepalive still running after unload — cable stays live"
    finally:
        motor.KEEPALIVE_S = orig


async def test_loaded_reports_false_before_any_load() -> None:
    m = MotorController(FakeClient(), max_load_lb=130)
    assert not m.loaded
    assert m.weight_lb is None
