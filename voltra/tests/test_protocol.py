"""Golden-frame tests for the codec.

These are the spike's offline self-test, turned into assertions. They prove
both CRC parameter sets and the CRC16 coverage span against frames the real
device accepted, and need no hardware.

None of these frames carry the device's MAC or serial — the frames that do
(the 0x4F device-name reply and bootstrap packet 1) are deliberately absent.
"""

from __future__ import annotations

import pytest

from pump_voltra import registry
from pump_voltra.protocol import (
    CMD_PARAM_READ,
    CMD_PARAM_WRITE,
    build_frame,
    crc8,
    crc16,
    expected_length,
    parse_frame,
    verify_frame,
)

# Frames captured from a real session, all APP → DEVICE.
CAPTURES = {
    "commonConnectRequest": "550f0801aad200002000ff00aa0419",
    "handshake finish/check": "551f044eaa10000020002781105eab9ef41c864ff5877a9c8c1d5f0d603e86",
    "read common state": "550d0433aa10000020007403bc",
    "read firmware page 0": "550e0466aa100100200077003889",
    "read firmware page 1": "550e0466aa10020020007701cc94",
    "read serial page": "550e0466aa100300200019002b7e",
    "read activation/security page": "550e0466aa1004002000ab01ad7a",
}

# (command, payload, seq) recipes that must reproduce the captures byte for byte.
REBUILD = {
    "handshake finish/check": (0x27, bytes.fromhex("81105eab9ef41c864ff5877a9c8c1d5f0d60"), 0),
    "read common state": (0x74, b"", 0),
    "read firmware page 0": (0x77, bytes((0x00,)), 1),
    "read firmware page 1": (0x77, bytes((0x01,)), 2),
    "read serial page": (0x19, bytes((0x00,)), 3),
    "read activation/security page": (0xAB, bytes((0x01,)), 4),
}


@pytest.mark.parametrize("label", sorted(CAPTURES))
def test_captured_frames_verify(label: str) -> None:
    raw = bytes.fromhex(CAPTURES[label])
    length_ok, header_ok, body_ok = verify_frame(raw)
    assert length_ok, f"{label}: declared length disagrees with actual"
    assert header_ok, f"{label}: CRC8 (init 0xEE, poly 0x31) failed"
    assert body_ok, f"{label}: CRC16 (init 0x496C, poly 0x1021) over frame[0:-2] failed"


@pytest.mark.parametrize("label", sorted(REBUILD))
def test_rebuild_is_byte_identical(label: str) -> None:
    command, payload, seq = REBUILD[label]
    assert build_frame(command, payload, seq) == bytes.fromhex(CAPTURES[label])


def test_crc_parameters_are_the_nonstandard_ones() -> None:
    # Pin the constants: a "helpful" switch to a standard CRC preset would
    # break every frame and these golden tests are the only thing that says so.
    raw = bytes.fromhex(CAPTURES["read common state"])
    assert crc8(raw[0:3]) == raw[3]
    assert crc16(raw[:-2]) == int.from_bytes(raw[-2:], "little")


def test_extended_frames_add_a_high_byte_to_the_length() -> None:
    # Types 0x05 and 0x09 mean the true length is 0x100 + the declared byte;
    # the length field is only 8 bits. A 375-byte frame declares 0x77.
    normal = bytes([0x55, 0x0D, 0x04]) + bytes(10)
    extended = bytes([0x55, 0x77, 0x05]) + bytes(10)
    assert expected_length(normal) == 0x0D
    assert expected_length(extended) == 0x177


def test_parse_frame_rejects_corruption() -> None:
    raw = bytearray.fromhex(CAPTURES["read common state"])
    assert parse_frame(bytes(raw)) is not None
    raw[-1] ^= 0xFF  # flip a CRC16 bit
    assert parse_frame(bytes(raw)) is None
    assert parse_frame(bytes(raw), strict=False) is not None


def test_parse_frame_rejects_runts_and_bad_sync() -> None:
    assert parse_frame(b"") is None
    assert parse_frame(b"\x55\x0d") is None
    assert parse_frame(b"\x00" * 20) is None


def test_parse_frame_extracts_command_and_payload() -> None:
    frame = parse_frame(bytes.fromhex(CAPTURES["read firmware page 1"]))
    assert frame is not None
    assert frame.command == 0x77
    assert frame.seq == 2
    assert frame.payload == b"\x01"


# ─── parameter codec ─────────────────────────────────────────────────────


def test_param_read_and_write_roundtrip() -> None:
    read = parse_frame(registry.encode_read([registry.TARGET_LOAD], seq=5))
    assert read is not None and read.command == CMD_PARAM_READ
    assert read.payload == b"\x01\x00" + registry.TARGET_LOAD.to_bytes(2, "little")

    write = parse_frame(registry.encode_write(registry.TARGET_LOAD, 40, seq=6))
    assert write is not None and write.command == CMD_PARAM_WRITE
    # target load is uint16 LE — 40 lb encodes as 28 00, not a bare 28
    assert write.payload == b"\x01\x00\x86\x3e\x28\x00"


def test_write_uses_the_registered_width_not_a_guess() -> None:
    # WORKOUT_STATE is one byte; TARGET_LOAD is two. Width is per parameter
    # and cannot be inferred from the value.
    assert len(parse_frame(registry.encode_write(registry.WORKOUT_STATE, 1)).payload) == 5
    assert len(parse_frame(registry.encode_write(registry.TARGET_LOAD, 1)).payload) == 6


def test_decode_reply_mixed_widths() -> None:
    # status, count=2, then (uint8 workout state)(uint16 target load)
    payload = bytes([0x00, 0x02, 0x00]) + b"\xb0\x4f\x01" + b"\x86\x3e\x28\x00"
    assert registry.decode_reply(payload) == {
        registry.WORKOUT_STATE: 1,
        registry.TARGET_LOAD: 40,
    }


def test_decode_reply_stops_at_an_unknown_parameter() -> None:
    # Guessing a width would desynchronise everything after it, so the walk
    # must stop rather than emit garbage for the remaining parameters.
    payload = bytes([0x00, 0x02, 0x00]) + b"\x86\x3e\x28\x00" + b"\xff\xff\x01"
    assert registry.decode_reply(payload) == {registry.TARGET_LOAD: 40}


def test_decode_reply_tolerates_truncation() -> None:
    assert registry.decode_reply(b"") == {}
    assert registry.decode_reply(bytes([0x00, 0x01, 0x00]) + b"\x86\x3e") == {}


def test_unknown_parameter_write_raises() -> None:
    with pytest.raises(registry.UnknownParameter):
        registry.encode_write(0xDEAD, 1)
