"""Reconciliation of PUMP's desired state onto the motor."""

from __future__ import annotations

import asyncio
import contextlib

import pytest

from pump_voltra import registry
from pump_voltra.control import Controller, _parse_weight
from pump_voltra.motor import MotorController
from tests.conftest import FakeClient


class FakePump:
    def __init__(self):
        self.reports: list[tuple[bool, str]] = []

    async def report_voltra(self, loaded: bool, error: str = "") -> None:
        self.reports.append((loaded, error))


def build(workout=True, readback=None, max_load=130):
    ble = FakeClient(workout=workout, readback=readback)
    motor = MotorController(ble, max_load_lb=max_load)
    pump = FakePump()
    return ble, motor, pump, Controller(pump, motor, max_load)


async def build_connected(workout=True, readback=None, max_load=130):
    """A controller that has finished connecting.

    PUMP replays its current desired state to every sidecar that connects, and
    the controller refuses to engage on that first frame (see
    test_does_not_engage_on_the_first_state_after_connecting). Steady-state
    tests need that frame consumed, so hand them a controller past it.
    """
    ble, motor, pump, ctl = build(workout, readback, max_load)
    await ctl.apply({"ArmedSetID": 0, "WantLoad": False, "WeightLb": ""})
    ble.writes.clear()
    pump.reports.clear()
    return ble, motor, pump, ctl


async def test_arm_alone_does_not_engage() -> None:
    ble, motor, _, ctl = await build_connected()
    await ctl.apply({"ArmedSetID": 3, "WantLoad": False, "WeightLb": "50"})
    assert not motor.loaded
    assert ble.writes == [], "arming touched the device"
    assert not ctl.recording()


async def test_load_engages_and_enables_recording() -> None:
    _, motor, pump, ctl = await build_connected()
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"})
    assert motor.loaded
    assert motor.weight_lb == 50
    assert ctl.recording()
    assert pump.reports[-1] == (True, "")
    await motor.unload()


async def test_recording_needs_both_halves() -> None:
    _, motor, _, ctl = await build_connected()
    # loaded but nothing armed
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"})
    assert ctl.recording()
    await ctl.apply({"ArmedSetID": 0, "WantLoad": False, "WeightLb": ""})
    assert not ctl.recording()
    assert not motor.loaded


async def test_refusal_is_reported_and_leaves_motor_released() -> None:
    # Device disagrees with the write; the read-back gate must refuse, and the
    # UI must be told why rather than watching a load that never happens.
    _, motor, pump, ctl = await build_connected(readback=20)
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"})
    assert not motor.loaded
    assert not ctl.recording()
    loaded, err = pump.reports[-1]
    assert loaded is False
    assert "read back as 20" in err


async def test_inactive_workout_is_reported_not_raised() -> None:
    _, motor, pump, ctl = await build_connected(workout=False)
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"})
    assert not motor.loaded
    assert "workout" in pump.reports[-1][1].lower()


async def test_reapplying_the_same_state_does_not_cycle_the_motor() -> None:
    # Re-engaging under an athlete mid-set would be both alarming and wrong.
    ble, motor, _, ctl = await build_connected()
    desired = {"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"}
    await ctl.apply(desired)
    ble.writes.clear()
    await ctl.apply(desired)
    assert ble.writes == [], "re-applied identical state cycled the motor"
    await motor.unload()


async def test_weight_change_cycles_the_motor() -> None:
    ble, motor, _, ctl = await build_connected()
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"})
    ble.writes.clear()
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "60"})
    modes = [v for p, v in ble.writes if p == registry.FITNESS_MODE]
    assert modes[0] == registry.MODE_UNLOADED
    assert motor.weight_lb == 60
    await motor.unload()


async def test_unusable_weight_is_refused_without_touching_the_device() -> None:
    ble, motor, pump, ctl = await build_connected()
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "not-a-number"})
    assert not motor.loaded
    assert ble.writes == []
    assert "unusable weight" in pump.reports[-1][1]


@pytest.mark.parametrize(
    "raw,want",
    [("50", 50), ("50.00", 50), (50, 50), ("49.6", 50), ("0", None),
     ("-5", None), ("", None), (None, None), ("abc", None),
     # float() accepts these and only int() explodes, so they used to raise
     # OverflowError out of apply() and tear down the state stream.
     ("inf", None), ("-inf", None), ("Infinity", None), ("1e400", None),
     ("nan", None)],
)
def test_weight_parsing(raw, want) -> None:
    assert _parse_weight(raw) == want


# PUMP keeps desired state across a sidecar restart and replays it on connect.
# Engaging on that frame would re-tension the cable with nobody asking — the
# athlete felt it go slack and has let go.
async def test_does_not_engage_on_the_first_state_after_connecting() -> None:
    ble, motor, pump, ctl = build()
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"})
    assert not motor.loaded, "re-engaged the motor on reconnect with no human action"
    assert ble.writes == [], "touched the device on the first post-connect state"
    assert not ctl.recording()
    assert "press LOAD again" in pump.reports[-1][1]


# ...but a genuine press after that must still work.
async def test_engages_on_a_press_after_the_first_state() -> None:
    _, motor, _, ctl = build()
    desired = {"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"}
    await ctl.apply(desired)      # replayed on connect — ignored
    await ctl.apply(desired)      # the athlete presses LOAD
    assert motor.loaded
    assert motor.weight_lb == 50
    await motor.unload()


# Losing the stream cleanly is just as blind as losing it with an error, and
# used to fall straight through to reconnect with the cable still live.
async def test_clean_stream_end_still_unloads() -> None:
    _, motor, _, ctl = await build_connected()
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"})
    assert motor.loaded

    class CleanEndPump:
        """A PUMP whose SSE stream ends without raising."""
        def __init__(self):
            self.reports = []

        async def report_voltra(self, loaded, error=""):
            self.reports.append((loaded, error))

        async def stream_voltra_state(self):
            return
            yield  # pragma: no cover — makes this an async generator

    ctl._pump = CleanEndPump()
    watcher = asyncio.create_task(ctl.watch())
    await asyncio.sleep(0.05)
    watcher.cancel()
    with contextlib.suppress(asyncio.CancelledError):
        await watcher
    assert not motor.loaded, "a cleanly-ended stream left the cable live"
