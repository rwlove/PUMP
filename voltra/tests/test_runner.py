"""The write path: what actually lands in PUMP for a completed set."""

from __future__ import annotations

import json

import httpx
import pytest
import respx

from pump_voltra import healthd
from pump_voltra.naming import ExerciseNamer
from pump_voltra.pump_client import AutoLogDisabled, PumpClient
from pump_voltra.runner import Runner, today_str
from pump_voltra.session import CompletedSet, SetTracker

BASE = "http://pump.test"

EXERCISES = [
    {"ID": 1, "Name": "Cable Row", "Voltra": True, "Color": "#aa0000"},
    {"ID": 2, "Name": "Seated Cable Pulldown", "Voltra": False, "Color": "#0000aa"},
]


def make_runner(namer: ExerciseNamer) -> tuple[Runner, PumpClient]:
    pump = PumpClient(BASE)
    return Runner(pump, namer, SetTracker()), pump


def anchored_namer() -> ExerciseNamer:
    """A namer whose anchor is today's "Cable Row" — the normal steady state."""
    n = ExerciseNamer(default_exercise="Voltra")
    n.set_exercises(EXERCISES)
    today = today_str()
    n.observe_set(
        {"Date": today, "Name": "Cable Row", "Color": "#aa0000", "WorkoutColor": "#bb0000"},
        today,
        today,
    )
    return n


def sent_payload(route) -> dict:
    return json.loads(route.calls[0].request.read().decode())


@respx.mock
async def test_posted_set_carries_weight_reps_and_inherited_name() -> None:
    route = respx.post(f"{BASE}/api/sets").mock(return_value=httpx.Response(201, json={"id": 42}))
    runner, pump = make_runner(anchored_namer())

    await runner.post(CompletedSet(set_number=3, reps=5, weight_lb=40, inferred=False))
    await pump.aclose()

    payload = sent_payload(route)
    assert payload["Name"] == "Cable Row"
    assert payload["Date"] == today_str()
    # Weight goes over the wire as a string: PUMP stores it as NUMERIC(10,2)
    # and binds it from text, matching what pump-cv sends.
    assert payload["Weight"] == "40"
    assert payload["Reps"] == 5
    assert payload["Source"] == "voltra"
    assert payload["Pending"] is False
    assert payload["Confidence"] == 1.0
    # Colours are inherited so the set sits visually with the ones the
    # athlete logged by hand.
    assert payload["WorkoutColor"] == "#bb0000"
    assert payload["Color"] == "#aa0000"


@respx.mock
async def test_set_without_an_anchor_is_pending_and_named_by_default() -> None:
    route = respx.post(f"{BASE}/api/sets").mock(return_value=httpx.Response(201, json={"id": 7}))
    namer = ExerciseNamer(default_exercise="Voltra")
    namer.set_exercises(EXERCISES)
    runner, pump = make_runner(namer)

    await runner.post(CompletedSet(set_number=1, reps=8, weight_lb=25, inferred=False))
    await pump.aclose()

    payload = sent_payload(route)
    assert payload["Name"] == "Voltra"
    assert payload["Pending"] is True
    assert "confirm" in payload["Note"].lower()


@respx.mock
async def test_403_is_surfaced_as_autolog_disabled_and_not_retried() -> None:
    route = respx.post(f"{BASE}/api/sets").mock(
        return_value=httpx.Response(403, json={"error": "Voltra auto-log is disabled"})
    )
    pump = PumpClient(BASE)
    with pytest.raises(AutoLogDisabled):
        await pump.post_set({"Reps": 1})
    await pump.aclose()
    assert route.call_count == 1


@respx.mock
async def test_a_failed_write_is_counted_not_raised() -> None:
    # A set lost to a transient PUMP outage must not kill the session; the
    # athlete is mid-workout and more sets are coming.
    respx.post(f"{BASE}/api/sets").mock(return_value=httpx.Response(500))
    before = healthd.state().sets_failed
    runner, pump = make_runner(anchored_namer())
    await runner.post(CompletedSet(set_number=1, reps=5, weight_lb=10, inferred=False))
    await pump.aclose()
    assert healthd.state().sets_failed == before + 1


@respx.mock
async def test_exercise_refresh_populates_the_flag_set() -> None:
    respx.get(f"{BASE}/api/exercises").mock(return_value=httpx.Response(200, json=EXERCISES))
    namer = ExerciseNamer()
    runner, pump = make_runner(namer)
    await runner.refresh_exercises()
    await pump.aclose()
    assert namer.is_flagged("Cable Row")
    assert not namer.is_flagged("Seated Cable Pulldown")


def test_metrics_render() -> None:
    out = healthd.render_metrics()
    assert "pump_voltra_sets_posted_total" in out
    assert "pump_voltra_sets_inferred_total" in out
