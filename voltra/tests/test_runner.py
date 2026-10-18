"""The report path: what the sidecar sends PUMP for a completed set.

The sidecar no longer writes set rows. On a finished set it reports the rep
count to PUMP's Voltra coordination state; the browser owns the row and writes
the count into it. These tests pin that behaviour.
"""

from __future__ import annotations

import json

import httpx
import pytest
import respx

from pump_voltra import healthd
from pump_voltra.naming import ExerciseNamer
from pump_voltra.pump_client import AutoLogDisabled, PumpClient
from pump_voltra.runner import Runner
from pump_voltra.session import CompletedSet, SetTracker

BASE = "http://pump.test"

EXERCISES = [
    {"ID": 1, "Name": "Cable Row", "Voltra": True, "Color": "#aa0000"},
    {"ID": 2, "Name": "Seated Cable Pulldown", "Voltra": False, "Color": "#0000aa"},
]


def make_runner() -> tuple[Runner, PumpClient]:
    pump = PumpClient(BASE)
    # No controller: the recording() gate is exercised separately. Here the
    # report path itself is under test.
    return Runner(pump, ExerciseNamer(default_exercise="Voltra"), SetTracker()), pump


def sent_payload(route) -> dict:
    return json.loads(route.calls[0].request.read().decode())


@respx.mock
async def test_report_sends_the_rep_count() -> None:
    route = respx.post(f"{BASE}/api/voltra/reps").mock(return_value=httpx.Response(200, json={}))
    runner, pump = make_runner()

    await runner.report(CompletedSet(set_number=3, reps=5, weight_lb=40, inferred=False))
    await pump.aclose()

    assert route.called
    # The sidecar sends only the rep count — the browser owns the row, so no
    # name, weight, colour, or source crosses here.
    assert sent_payload(route) == {"Reps": 5}


@respx.mock
async def test_report_counts_a_recorded_set() -> None:
    respx.post(f"{BASE}/api/voltra/reps").mock(return_value=httpx.Response(200, json={}))
    before = healthd.state().sets_posted
    runner, pump = make_runner()

    await runner.report(CompletedSet(set_number=1, reps=8, weight_lb=25, inferred=False))
    await pump.aclose()

    assert healthd.state().sets_posted == before + 1


@respx.mock
async def test_403_is_surfaced_as_autolog_disabled_and_not_retried() -> None:
    route = respx.post(f"{BASE}/api/voltra/reps").mock(
        return_value=httpx.Response(403, json={"error": "Voltra auto-log is disabled"})
    )
    pump = PumpClient(BASE)
    with pytest.raises(AutoLogDisabled):
        await pump.report_reps(1)
    await pump.aclose()
    assert route.call_count == 1


@respx.mock
async def test_409_is_a_skip_not_a_failure() -> None:
    # Nothing armed and loaded on the server (a race against the sidecar's own
    # gate). The report is dropped quietly: not counted as posted, not failed.
    respx.post(f"{BASE}/api/voltra/reps").mock(
        return_value=httpx.Response(409, json={"error": "no Voltra set is armed and loaded"})
    )
    posted = healthd.state().sets_posted
    failed = healthd.state().sets_failed
    runner, pump = make_runner()

    await runner.report(CompletedSet(set_number=1, reps=5, weight_lb=10, inferred=False))
    await pump.aclose()

    assert healthd.state().sets_posted == posted
    assert healthd.state().sets_failed == failed


@respx.mock
async def test_a_failed_report_is_counted_not_raised() -> None:
    # A report lost to a transient PUMP outage must not kill the session; the
    # athlete is mid-workout and more sets are coming.
    respx.post(f"{BASE}/api/voltra/reps").mock(return_value=httpx.Response(500))
    before = healthd.state().sets_failed
    runner, pump = make_runner()
    await runner.report(CompletedSet(set_number=1, reps=5, weight_lb=10, inferred=False))
    await pump.aclose()
    assert healthd.state().sets_failed == before + 1


@respx.mock
async def test_exercise_refresh_populates_the_flag_set() -> None:
    respx.get(f"{BASE}/api/exercises").mock(return_value=httpx.Response(200, json=EXERCISES))
    namer = ExerciseNamer()
    pump = PumpClient(BASE)
    runner = Runner(pump, namer, SetTracker())
    await runner.refresh_exercises()
    await pump.aclose()
    assert namer.is_flagged("Cable Row")
    assert not namer.is_flagged("Seated Cable Pulldown")


def test_metrics_render() -> None:
    out = healthd.render_metrics()
    assert "pump_voltra_sets_posted_total" in out
    assert "pump_voltra_sets_inferred_total" in out


# ─── SSE stream timeout ──────────────────────────────────────────────────


@respx.mock
async def test_sse_stream_loosens_read_timeout_but_keeps_it_finite() -> None:
    """An idle SSE stream must not be torn down by the client read timeout, but
    it also must not hang forever on a half-open connection.

    PUMP only sends its keepalive comment every 25 s, so a client-wide 10 s
    read timeout kills a healthy stream every ~15 s (a reconnect loop with an
    empty error). But `read=None` blocks forever when the connection half-opens
    (PUMP rescheduled, dropped flow) and the caller only reconnects on an
    exception — that wedged the motor-control stream silently. So the read
    timeout is raised above the keepalive, not disabled: idle survives, a dead
    connection surfaces as ReadTimeout.
    """
    captured: dict = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["timeout"] = request.extensions.get("timeout")
        return httpx.Response(
            200,
            headers={"content-type": "text/event-stream"},
            content=b'event: add\ndata: {"type":"add","id":1,"date":"x","set":{}}\n\n',
        )

    respx.get(f"{BASE}/api/sets/stream").mock(side_effect=handler)

    pump = PumpClient(BASE, timeout_s=10.0, sse_read_timeout_s=45.0)
    events = [e async for e in pump.stream_set_events()]
    await pump.aclose()

    assert len(events) == 1
    # Finite and above the 25 s keepalive; connect stays on the base timeout.
    assert captured["timeout"]["read"] == 45.0
    assert captured["timeout"]["read"] > 25.0
    assert captured["timeout"]["connect"] == 10.0
