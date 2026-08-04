"""Frame codec for the Beyond Power VOLTRA I BLE protocol.

Pure functions over bytes — no I/O, no BLE imports. That is what makes the
whole codec testable against captured frames with no hardware present.

Clean-room: written from protocol facts (sync byte, field offsets, CRC
parameters, command ids), which are not copyrightable. No code was taken from
the unlicensed prior art. See README.md.

Both CRCs use non-standard initial values that were recovered empirically:

  CRC8   init 0xEE,   poly 0x31,   reflected in and out, no final XOR
  CRC16  init 0x496C, poly 0x1021, reflected in and out, no final XOR

The CRC16 *coverage span* is documented nowhere. It was recovered by brute
forcing every candidate byte range against eight known-good frames until
exactly one span — frame[0:-2] — satisfied all of them.
"""

from __future__ import annotations

from dataclasses import dataclass

SYNC = 0x55
FRAME_NORMAL = 0x04
FRAME_EXTENDED = (0x05, 0x09)
PROTO = 0x0020
APP = 0xAA
DEVICE = 0x10

HEADER_LEN = 11  # sync .. command inclusive
CRC_LEN = 2
MIN_FRAME_LEN = HEADER_LEN + CRC_LEN

# Command ids.
CMD_PARAM_READ = 0x0F
CMD_ASYNC_STATE = 0x10
CMD_PARAM_WRITE = 0x11
CMD_SERIAL = 0x19
CMD_HANDSHAKE = 0x27
CMD_DEVICE_NAME = 0x4F
CMD_COMMON_STATE = 0x74
CMD_FIRMWARE = 0x77
CMD_DEVICE_STATE = 0xA7
CMD_TELEMETRY = 0xAA
CMD_ACTIVATION = 0xAB
CMD_STREAM = 0xB4

# Commands whose payload is a parameter list (read reply, write reply, and the
# unsolicited async push the device sends on state change).
PARAM_COMMANDS = (CMD_PARAM_READ, CMD_PARAM_WRITE, CMD_ASYNC_STATE)


def reflect(value: int, width: int) -> int:
    out = 0
    for i in range(width):
        if value & (1 << i):
            out |= 1 << (width - 1 - i)
    return out


def crc8(data: bytes, init: int = 0xEE, poly: int = 0x31) -> int:
    crc = init
    for byte in data:
        crc ^= reflect(byte, 8)
        for _ in range(8):
            crc = ((crc << 1) ^ poly) & 0xFF if crc & 0x80 else (crc << 1) & 0xFF
    return reflect(crc, 8)


def crc16(data: bytes, init: int = 0x496C, poly: int = 0x1021) -> int:
    crc = init
    for byte in data:
        crc ^= reflect(byte, 8) << 8
        for _ in range(8):
            crc = ((crc << 1) ^ poly) & 0xFFFF if crc & 0x8000 else (crc << 1) & 0xFFFF
    return reflect(crc, 16)


def build_frame(
    command: int,
    payload: bytes = b"",
    seq: int = 0,
    sender: int = APP,
    receiver: int = DEVICE,
    frame_type: int = FRAME_NORMAL,
) -> bytes:
    """Assemble a complete frame. CRC16 covers everything before the CRC field."""
    total = HEADER_LEN + len(payload) + CRC_LEN
    frame = bytes((SYNC, total & 0xFF, frame_type))
    frame += bytes((crc8(frame),))
    frame += bytes((sender, receiver))
    frame += seq.to_bytes(2, "little")
    frame += PROTO.to_bytes(2, "little")
    frame += bytes((command,))
    frame += payload
    return frame + crc16(frame).to_bytes(2, "little")


def expected_length(raw: bytes) -> int:
    """Declared frame length, resolving the extended-frame encoding.

    Frame types 0x05 and 0x09 mean the true length is 0x100 + the declared
    byte — the length field is only 8 bits wide. Observed in the wild: a
    375-byte frame declaring 0x77 (119).
    """
    declared = raw[1]
    return 0x100 + declared if raw[2] in FRAME_EXTENDED else declared


def verify_frame(raw: bytes) -> tuple[bool, bool, bool]:
    """Return (length_ok, header_crc_ok, body_crc_ok)."""
    if len(raw) < MIN_FRAME_LEN:
        return (False, False, False)
    return (
        expected_length(raw) == len(raw),
        raw[3] == crc8(raw[0:3]),
        int.from_bytes(raw[-2:], "little") == crc16(raw[:-2]),
    )


@dataclass(frozen=True, slots=True)
class Frame:
    frame_type: int
    sender: int
    receiver: int
    seq: int
    command: int
    payload: bytes


def parse_frame(raw: bytes, *, strict: bool = True) -> Frame | None:
    """Decode one frame. Returns None when it is not a usable frame.

    strict=False skips CRC verification, which is what the capture-replay
    tests want: the logs record payloads the device already validated, and a
    few were truncated by the capture tooling rather than by the device.
    """
    if len(raw) < MIN_FRAME_LEN or raw[0] != SYNC:
        return None
    if strict:
        length_ok, header_ok, body_ok = verify_frame(raw)
        if not (length_ok and header_ok and body_ok):
            return None
    return Frame(
        frame_type=raw[2],
        sender=raw[4],
        receiver=raw[5],
        seq=int.from_bytes(raw[6:8], "little"),
        command=raw[10],
        payload=raw[11:-2],
    )
