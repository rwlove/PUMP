"""Shared test doubles.

FakeClient lives here rather than in a test module because pytest guarantees
conftest is importable; a `from tests.test_motor import ...` only works by
accident of sys.path and breaks in CI.
"""

from __future__ import annotations

from pump_voltra import registry


class FakeClient:
    """Stands in for VoltraClient, recording the exact write order.

    read() returns whatever the device would report; `readback` lets a test
    make the device disagree with what was written, which is the silent-failure
    the read-back gate exists to catch.
    """

    def __init__(self, workout=True, readback=None, mode_readback=None):
        self.writes: list[tuple[int, int]] = []
        self.params: dict[int, int] = {}
        self._workout = workout
        self._readback = readback
        self._mode_readback = mode_readback

    async def workout_active(self) -> bool:
        return self._workout

    async def write(self, param_id: int, value: int, settle_s: float = 0.0) -> None:
        self.writes.append((param_id, value))
        self.params[param_id] = value

    async def read(self, *param_ids: int, settle_s: float = 0.0) -> dict[int, int]:
        out = {}
        for p in param_ids:
            if p == registry.TARGET_LOAD and self._readback is not None:
                out[p] = self._readback
            elif p == registry.FITNESS_MODE and self._mode_readback is not None:
                out[p] = self._mode_readback
            elif p in self.params:
                out[p] = self.params[p]
        return out

    # convenience views
    def loads_written(self):
        return [v for p, v in self.writes if p == registry.TARGET_LOAD]

    def modes_written(self):
        return [v for p, v in self.writes if p == registry.FITNESS_MODE]
