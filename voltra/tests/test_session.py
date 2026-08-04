"""Set-boundary detection, including replay of two real captured sessions.

The replay tests are the backbone: each fixture is a set the athlete
physically performed, so a decode regression shows up as the wrong rep count
rather than as a subtle behaviour change nobody notices.
"""

from __future__ import annotations

from pathlib import Path

import pytest

from pump_voltra import telemetry
from pump_voltra.session import SetTracker

FIXTURES = Path(__file__).parent / "fixtures"


def load_fixture(name: str) -> list[tuple[float, bytes]]:
    rows = []
    for line in (FIXTURES / name).read_text().splitlines():
        if not line or line.startswith("#"):
            continue
        elapsed, hexpayload = line.split("\t")
        rows.append((float(elapsed), bytes.fromhex(hexpayload)))
    return rows


def replay(name: str, idle_seconds: float = 30.0, weight_lb: int = 5) -> list:
    tracker = SetTracker(idle_seconds=idle_seconds)
    tracker.note_weight(weight_lb)
    out = []
    for elapsed, payload in load_fixture(name):
        event = telemetry.decode(payload)
        if event is None:
            continue
        if (done := tracker.observe(event, elapsed)) is not None:
            out.append(done)
    return out


# ─── replay of real sessions ─────────────────────────────────────────────


def test_replay_five_rep_set() -> None:
    """hold.log: the athlete did 5 reps; the device reported set 3."""
    sets = replay("session-set3-5reps.tsv")
    assert len(sets) == 1
    assert sets[0].set_number == 3
    assert sets[0].reps == 5
    assert sets[0].weight_lb == 5
    # Closed by the device's own summary, not by our timeout.
    assert sets[0].inferred is False


def test_replay_three_rep_set() -> None:
    """pull.log: 3 reps, reported as set 2."""
    sets = replay("session-set2-3reps.tsv")
    assert len(sets) == 1
    assert sets[0].set_number == 2
    assert sets[0].reps == 3
    assert sets[0].inferred is False


def test_replay_is_idempotent_across_a_reconnect() -> None:
    """Re-feeding the same frames must not post the set twice."""
    tracker = SetTracker()
    tracker.note_weight(5)
    frames = load_fixture("session-set3-5reps.tsv")
    emitted = []
    for _ in range(2):
        for elapsed, payload in frames:
            event = telemetry.decode(payload)
            if event and (done := tracker.observe(event, elapsed)):
                emitted.append(done)
    assert len(emitted) == 1


# ─── decoding ────────────────────────────────────────────────────────────


def test_summary_carries_set_and_rep_at_distinct_offsets() -> None:
    payload = next(
        p for _, p in load_fixture("session-set3-5reps.tsv") if p[:2] == bytes([0x85, 0x5F])
    )
    event = telemetry.decode(payload)
    assert isinstance(event, telemetry.SetSummary)
    assert (event.set_number, event.reps) == (3, 5)


def test_rep_counters_are_big_endian() -> None:
    # 0x0100 little-endian would be 1; big-endian it is 256. Every parameter
    # value is little-endian, so this asymmetry is easy to get wrong.
    payload = bytes([0x84, 0x40, 0x02]) + b"\x01\x00" + bytes(60)
    event = telemetry.decode(payload)
    assert isinstance(event, telemetry.Progress)
    assert event.reps == 256


def test_heartbeat_state_is_not_a_rep_count() -> None:
    # Across both captures the 0x80/0x25 field walks 0→1→2→3→4→0 regardless
    # of how many reps were performed. It is a session-state code.
    states = [
        telemetry.decode(p).state
        for _, p in load_fixture("session-set3-5reps.tsv")
        if p[:2] == bytes([0x80, 0x25])
    ]
    assert max(states) <= 4


def test_decode_ignores_unknown_and_short_payloads() -> None:
    assert telemetry.decode(b"") is None
    assert telemetry.decode(b"\x87\x0c") is None  # waveform chunk
    assert telemetry.decode(bytes([0x84, 0x40])) is None  # truncated


# ─── fallback paths ──────────────────────────────────────────────────────


def test_idle_timeout_closes_a_set_when_the_summary_is_dropped() -> None:
    """The ESPHome proxy discards notifications under backpressure silently."""
    tracker = SetTracker(idle_seconds=30.0)
    tracker.note_weight(40)
    tracker.observe(telemetry.Progress(set_number=1, reps=8), now=10.0)

    assert tracker.tick(now=39.0) is None  # still inside the window
    done = tracker.tick(now=41.0)
    assert done is not None
    assert (done.set_number, done.reps, done.weight_lb) == (1, 8, 40)
    assert done.inferred is True

    # And it must not fire again afterwards.
    assert tracker.tick(now=200.0) is None


def test_new_set_number_closes_the_previous_set() -> None:
    tracker = SetTracker()
    tracker.note_weight(30)
    tracker.observe(telemetry.Progress(set_number=1, reps=6), now=1.0)
    done = tracker.observe(telemetry.Progress(set_number=2, reps=1), now=2.0)
    assert done is not None
    assert (done.set_number, done.reps) == (1, 6)
    assert done.inferred is True


def test_summary_wins_even_without_progress_frames() -> None:
    # A whole set's worth of Progress frames can be dropped; the summary is
    # self-contained and authoritative.
    tracker = SetTracker()
    tracker.note_weight(25)
    done = tracker.observe(telemetry.SetSummary(set_number=4, reps=12), now=1.0)
    assert done is not None
    assert (done.set_number, done.reps, done.weight_lb) == (4, 12, 25)


def test_zero_rep_sets_are_discarded() -> None:
    # Loading the cable and not pulling produces frames but is not a set.
    tracker = SetTracker()
    assert tracker.observe(telemetry.SetSummary(set_number=1, reps=0), now=1.0) is None


def test_rep_count_never_goes_backwards_within_a_set() -> None:
    tracker = SetTracker()
    tracker.note_weight(10)
    tracker.observe(telemetry.Progress(set_number=1, reps=5), now=1.0)
    tracker.observe(telemetry.Progress(set_number=1, reps=3), now=2.0)  # stale frame
    done = tracker.tick(now=100.0)
    assert done is not None and done.reps == 5


def test_weight_is_snapshotted_per_set() -> None:
    tracker = SetTracker()
    tracker.note_weight(40)
    first = tracker.observe(telemetry.SetSummary(set_number=1, reps=10), now=1.0)
    tracker.note_weight(60)
    second = tracker.observe(telemetry.SetSummary(set_number=2, reps=8), now=2.0)
    assert first.weight_lb == 40
    assert second.weight_lb == 60


def test_reset_workout_allows_set_numbers_to_repeat() -> None:
    # The device restarts numbering per workout; without the reset, the second
    # workout's set 1 would be suppressed as a duplicate.
    tracker = SetTracker()
    tracker.note_weight(20)
    assert tracker.observe(telemetry.SetSummary(set_number=1, reps=5), now=1.0) is not None
    assert tracker.observe(telemetry.SetSummary(set_number=1, reps=5), now=2.0) is None
    tracker.reset_workout()
    assert tracker.observe(telemetry.SetSummary(set_number=1, reps=5), now=3.0) is not None


@pytest.mark.parametrize("fixture", ["session-set3-5reps.tsv", "session-set2-3reps.tsv"])
def test_fixtures_carry_no_device_identifiers(fixture: str) -> None:
    # PUMP is public; the trainer's MAC and serial must never land here.
    text = (FIXTURES / fixture).read_text().lower()
    assert "9888e05123d6" not in text
    assert "5654522d" not in text  # "VTR-" in hex
