"""Reconciliation of PUMP's desired state onto the motor."""

from __future__ import annotations

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


async def test_arm_alone_does_not_engage() -> None:
    ble, motor, _, ctl = build()
    await ctl.apply({"ArmedSetID": 3, "WantLoad": False, "WeightLb": "50"})
    assert not motor.loaded
    assert ble.writes == [], "arming touched the device"
    assert not ctl.recording()


async def test_load_engages_and_enables_recording() -> None:
    _, motor, pump, ctl = build()
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"})
    assert motor.loaded
    assert motor.weight_lb == 50
    assert ctl.recording()
    assert pump.reports[-1] == (True, "")
    await motor.unload()


async def test_recording_needs_both_halves() -> None:
    _, motor, _, ctl = build()
    # loaded but nothing armed
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"})
    assert ctl.recording()
    await ctl.apply({"ArmedSetID": 0, "WantLoad": False, "WeightLb": ""})
    assert not ctl.recording()
    assert not motor.loaded


async def test_refusal_is_reported_and_leaves_motor_released() -> None:
    # Device disagrees with the write; the read-back gate must refuse, and the
    # UI must be told why rather than watching a load that never happens.
    _, motor, pump, ctl = build(readback=20)
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"})
    assert not motor.loaded
    assert not ctl.recording()
    loaded, err = pump.reports[-1]
    assert loaded is False
    assert "read back as 20" in err


async def test_inactive_workout_is_reported_not_raised() -> None:
    _, motor, pump, ctl = build(workout=False)
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"})
    assert not motor.loaded
    assert "workout" in pump.reports[-1][1].lower()


async def test_reapplying_the_same_state_does_not_cycle_the_motor() -> None:
    # Re-engaging under an athlete mid-set would be both alarming and wrong.
    ble, motor, _, ctl = build()
    desired = {"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"}
    await ctl.apply(desired)
    ble.writes.clear()
    await ctl.apply(desired)
    assert ble.writes == [], "re-applied identical state cycled the motor"
    await motor.unload()


async def test_weight_change_cycles_the_motor() -> None:
    ble, motor, _, ctl = build()
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "50"})
    ble.writes.clear()
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "60"})
    modes = [v for p, v in ble.writes if p == registry.FITNESS_MODE]
    assert modes[0] == registry.MODE_UNLOADED
    assert motor.weight_lb == 60
    await motor.unload()


async def test_unusable_weight_is_refused_without_touching_the_device() -> None:
    ble, motor, pump, ctl = build()
    await ctl.apply({"ArmedSetID": 3, "WantLoad": True, "WeightLb": "not-a-number"})
    assert not motor.loaded
    assert ble.writes == []
    assert "unusable weight" in pump.reports[-1][1]


@pytest.mark.parametrize(
    "raw,want",
    [("50", 50), ("50.00", 50), (50, 50), ("49.6", 50), ("0", None),
     ("-5", None), ("", None), (None, None), ("abc", None)],
)
def test_weight_parsing(raw, want) -> None:
    assert _parse_weight(raw) == want
