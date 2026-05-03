"""Live integration test against a running pump-api on localhost:8851.

Skipped automatically when the API isn't reachable, so this is safe to
keep in the default pytest invocation. When pump-api IS up, it
exercises the full POST → GET → PATCH → confirm → DELETE happy path.

Requires the running pump-api to have CVAutoLog=true. The Phase 0 setup
on this dev box already has that.
"""

from __future__ import annotations

import socket

import pytest

from pump_cv.pump_client import PumpClient


def _pump_reachable(host: str = "localhost", port: int = 8851) -> bool:
    s = socket.socket()
    s.settimeout(0.5)
    try:
        s.connect((host, port))
        return True
    except OSError:
        return False
    finally:
        s.close()


pytestmark = pytest.mark.skipif(
    not _pump_reachable(),
    reason="pump-api not running on localhost:8851",
)


@pytest.mark.asyncio
async def test_full_set_lifecycle_against_running_pump():
    async with PumpClient("http://localhost:8851") as p:
        new_id = await p.post_set({
            "Date": "2026-05-03",
            "Name": "Bench Press",
            "Weight": "135",
            "Reps": 5,
            "Source": "cv",
            "Confidence": 0.55,
            "Pending": True,
        })
        assert new_id > 0

        try:
            sets = await p.list_sets()
            ours = [s for s in sets if s["ID"] == new_id]
            assert len(ours) == 1
            assert ours[0]["Pending"] is True
            assert ours[0]["Source"] == "cv"

            await p.patch_set(new_id, {"Reps": 7})
            sets = await p.list_sets()
            assert next(s for s in sets if s["ID"] == new_id)["Reps"] == 7

            await p.confirm_set(new_id, {"Weight": "140"})
            sets = await p.list_sets()
            after = next(s for s in sets if s["ID"] == new_id)
            assert after["Pending"] is False
            assert after["Weight"] == "140"
        finally:
            await p.delete_set(new_id)
