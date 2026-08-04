"""Decoder for the 0xAA telemetry command's subtypes.

Subtypes are discriminated by payload[0]/payload[1]. Offsets below were
verified against the two capture sessions in the protocol spike, in which the
athlete physically performed a 5-rep set (reported as set 3) and a 3-rep set
(reported as set 2).

⚠ Rep and set counters here are **big-endian**, while every *parameter* value
in registry.py is little-endian. That asymmetry is real, not a transcription
error.
"""

from __future__ import annotations

from dataclasses import dataclass

# Subtype discriminators, as (payload[0], payload[1]).
PROGRESS = (0x84, 0x40)  # ~1 Hz per-rep summary while a set is under way
SET_SUMMARY = (0x85, 0x5F)  # emitted once, just after the final rep
HEARTBEAT = (0x80, 0x25)  # session-state ping, also sent while idle
REP_BOUNDARY = (0x82, 0x3B)  # rep up/down transition
SAMPLE = (0x81, 0x2B)  # per-sample position/phase
WAVEFORM = 0x87  # payload[0] only; chunked rep waveform

# Session-state codes carried by HEARTBEAT and SET_SUMMARY at [3:5] big-endian.
# Observed progression across both captures: 0 idle → 1 → 2 while starting →
# 3 once a set has completed → 4 while ending → 0 at disconnect. This is a
# state code, NOT a rep count.
STATE_IDLE = 0
STATE_SET_COMPLETE = 3


@dataclass(frozen=True, slots=True)
class Progress:
    """Live rep counter during a set. Arrives at roughly 1 Hz."""

    set_number: int
    reps: int


@dataclass(frozen=True, slots=True)
class SetSummary:
    """Definitive end-of-set record. Emitted exactly once per set.

    This is the authoritative set boundary: it names both the set number and
    the final rep count, and it arrives about half a second after the last
    Progress frame. In hold.log it reads set 3 / 5 reps and in pull.log set 2 /
    3 reps, each matching what was physically performed.
    """

    set_number: int
    reps: int


@dataclass(frozen=True, slots=True)
class Heartbeat:
    state: int


def decode(payload: bytes) -> Progress | SetSummary | Heartbeat | None:
    """Decode one 0xAA payload. Returns None for subtypes we don't act on."""
    if len(payload) < 2:
        return None
    subtype = (payload[0], payload[1])

    if subtype == PROGRESS and len(payload) >= 5:
        return Progress(set_number=payload[2], reps=int.from_bytes(payload[3:5], "big"))

    if subtype == SET_SUMMARY and len(payload) >= 16:
        # [13] set number, [15] final rep count. Both single bytes; the
        # 16-bit fields elsewhere in this frame are other quantities.
        return SetSummary(set_number=payload[13], reps=payload[15])

    if subtype == HEARTBEAT and len(payload) >= 5:
        return Heartbeat(state=int.from_bytes(payload[3:5], "big"))

    return None
