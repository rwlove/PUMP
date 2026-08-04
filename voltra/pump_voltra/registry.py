"""Parameter registry and the parameter-list payload codec.

Value widths are **per parameter** and are not inferable from the frame — the
reply carries no length field. They must come from a registry, which is why
this module exists rather than being folded into protocol.py.

Reading a parameter this table does not know is a hard error: guessing a width
desynchronises the rest of the reply and silently corrupts every value after
it.
"""

from __future__ import annotations

from .protocol import CMD_PARAM_READ, CMD_PARAM_WRITE, build_frame

# Parameter ids.
TARGET_LOAD = 0x3E86  # lb, 5..200
CABLE_POSITION = 0x3E82  # cm
FORCE = 0x3E83  # instantaneous
FITNESS_MODE = 0x3E89  # 0x0004 ready/unloaded, 0x0005 loaded
BATTERY = 0x4E2D  # percent
WORKOUT_STATE = 0x4FB0  # 0 inactive, 1 weight training
TELEMETRY_RATE = 0x5182  # 0x28 == 40 Hz
TELEMETRY_TOKEN = 0x5183  # subscribe token

# Width in bytes of each parameter's value, little-endian.
WIDTHS: dict[int, int] = {
    TARGET_LOAD: 2,
    CABLE_POSITION: 2,
    FORCE: 2,
    FITNESS_MODE: 2,
    BATTERY: 1,
    WORKOUT_STATE: 1,
    TELEMETRY_RATE: 1,
    TELEMETRY_TOKEN: 4,
    0x520A: 2,
    0x520B: 2,
    0x520C: 2,
}

# Values for FITNESS_MODE.
MODE_UNLOADED = 0x0004
MODE_LOADED = 0x0005

# Values for WORKOUT_STATE.
WORKOUT_INACTIVE = 0
WORKOUT_WEIGHT_TRAINING = 1

# The token the device expects before it will stream telemetry at all.
TELEMETRY_TOKEN_VALUE = 0x00657BF5  # wire order F5 7B 65 00
TELEMETRY_RATE_40HZ = 0x28


class UnknownParameter(KeyError):
    """Raised for a parameter whose value width the registry does not know."""


def width_of(param_id: int) -> int:
    try:
        return WIDTHS[param_id]
    except KeyError:
        raise UnknownParameter(
            f"parameter 0x{param_id:04X} has no registered width; "
            "decoding it would desynchronise the rest of the reply"
        ) from None


def encode_read(param_ids: list[int], seq: int = 0) -> bytes:
    """Build a PARAM_READ frame for one or more parameters."""
    payload = len(param_ids).to_bytes(2, "little")
    for pid in param_ids:
        payload += pid.to_bytes(2, "little")
    return build_frame(CMD_PARAM_READ, payload, seq)


def encode_write(param_id: int, value: int, seq: int = 0) -> bytes:
    """Build a PARAM_WRITE frame. Width comes from the registry."""
    payload = b"\x01\x00" + param_id.to_bytes(2, "little")
    payload += value.to_bytes(width_of(param_id), "little")
    return build_frame(CMD_PARAM_WRITE, payload, seq)


def decode_reply(payload: bytes) -> dict[int, int]:
    """Decode a parameter-list payload into {param_id: value}.

    Layout: [status uint8][count uint16 LE] then count * ([id uint16 LE][value]).
    Unknown parameters end the walk — see the module docstring.
    """
    if len(payload) < 3:
        return {}
    count = int.from_bytes(payload[1:3], "little")
    out: dict[int, int] = {}
    offset = 3
    for _ in range(count):
        if offset + 2 > len(payload):
            break
        pid = int.from_bytes(payload[offset : offset + 2], "little")
        offset += 2
        try:
            width = width_of(pid)
        except UnknownParameter:
            break
        if offset + width > len(payload):
            break
        out[pid] = int.from_bytes(payload[offset : offset + width], "little")
        offset += width
    return out
