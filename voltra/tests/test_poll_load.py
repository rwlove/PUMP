"""`_poll_load` is the in-session heartbeat AND the source of the recorded set
weight. Two hardening contracts:

  1. The heartbeat is stamped only on a SUCCESSFUL read. Stamping before the
     read (the old behaviour) kept the liveness watchdog green while the BLE
     link was dead — a half-open GATT reads-fail every cycle, yet the loop kept
     spinning and the watchdog never fired.
  2. The polled load is range-checked before it is recorded. The MAX_LOAD clamp
     guards what we WRITE to the motor; the recorded weight came straight off
     the device unbounded, so a garbage frame could post e.g. Weight=65535.
"""

from __future__ import annotations

import asyncio

import pytest

from pump_voltra import healthd, main


class FakePollClient:
    """target_load() replays a script; an Exception value is raised, and the
    sentinel STOP raises CancelledError to end the infinite loop deterministically."""

    STOP = object()

    def __init__(self, script):
        self._script = list(script)
        self.calls = 0

    async def target_load(self):
        self.calls += 1
        if not self._script:
            raise asyncio.CancelledError
        v = self._script.pop(0)
        if v is self.STOP:
            raise asyncio.CancelledError
        if isinstance(v, Exception):
            raise v
        return v


class FakeTracker:
    def __init__(self):
        self.weights: list[int] = []

    def note_weight(self, w: int) -> None:
        self.weights.append(w)


async def _run(script, max_load_lb=130):
    client, tracker = FakePollClient(script), FakeTracker()
    with pytest.raises(asyncio.CancelledError):
        await main._poll_load(client, tracker, 0, max_load_lb)
    return client, tracker


async def test_out_of_range_loads_are_not_recorded() -> None:
    # 50 in-range; 0 and negative rejected by `0 < load`; 200 and 65535 rejected
    # by `<= max_load_lb`. Only 50 reaches the tracker.
    _, tracker = await _run([50, 0, -5, 200, 65535, 130, FakePollClient.STOP])
    assert tracker.weights == [50, 130], "garbage/over-limit loads must not be recorded"


async def test_heartbeat_is_stamped_only_on_a_successful_read(monkeypatch) -> None:
    ticks = {"n": 0}
    monkeypatch.setattr(healthd, "record_heartbeat", lambda: ticks.__setitem__("n", ticks["n"] + 1))
    # read fails, then two successful reads (one None), then stop.
    await _run([RuntimeError("ble dead"), 50, None, FakePollClient.STOP])
    # Heartbeat stamped for the two successful reads (50 and None) — NOT for the
    # failed read, and NOT for the STOP (which raises before stamping).
    assert ticks["n"] == 2, "a failed BLE read must not keep the liveness watchdog green"


async def test_a_dead_link_never_stamps_the_heartbeat(monkeypatch) -> None:
    ticks = {"n": 0}
    monkeypatch.setattr(healthd, "record_heartbeat", lambda: ticks.__setitem__("n", ticks["n"] + 1))
    # Every read fails (half-open GATT) until the loop is stopped.
    await _run([RuntimeError("x"), RuntimeError("y"), RuntimeError("z"), FakePollClient.STOP])
    assert ticks["n"] == 0, "an all-failing read loop must let the heartbeat go stale"
