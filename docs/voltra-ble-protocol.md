# Voltra BLE protocol

Reverse-engineered BLE control and telemetry for the **Beyond Power VOLTRA I**
smart cable trainer. Every claim here was validated against real hardware on
2026-08-03 (firmware MainControl v1.6 / MotorControl 1.6 / BMS 1.5, ESP32-C3 MCU).

This document is the reference for [`voltra-integration-plan.md`](voltra-integration-plan.md).

## Provenance and licensing

The protocol *facts* below — service and characteristic UUIDs, CRC parameters,
frame layout, command ids, parameter ids — are facts, not authored expression,
and are not copyrightable.

Prior community work exists (a Home Assistant custom component) but carries **no
licence**, i.e. all rights reserved. **No code was copied from it.** The
implementation used by PUMP is independent, written from the facts above, and
validated by rebuilding captured frames byte-for-byte.

One correction worth recording: that prior work's first bootstrap frame is
**malformed** — one byte short of its declared length, with an invalid CRC16. The
corrected frame (17 zero bytes in the padding run, not 16) is given below and was
accepted by the device.

Beyond Power also ships an official CLI ("Beyond Cortex", beta). Its terms forbid
speaking to the device directly, and it exposes **no live telemetry**, so it
cannot satisfy the rep-counting requirement.

## Transport

Standard BLE GATT. No pairing or bonding. On first connect the device asks the
operator to authorise the client on its own display.

| UUID | Role |
| --- | --- |
| `e4dada34-0867-8783-9f70-2ca29216c7e4` | service; also the advertisement matcher |
| `a010891d-f50f-44f0-901f-9a2421a9e050` | **transport** — all outbound writes |
| `55ca1e52-7354-25de-6afc-b7df1e8816ac` | **command** — all responses arrive here |
| `ca94658c-0525-5046-e78b-5391b65f47ad` | notify |
| `19de84ed-0a69-482c-a8a6-c75cb5bb4389` | write-without-response (unused) |

Subscribe to notifications on command, notify, and transport. Negotiated MTU is
**517** — the widely repeated "ESP32 defaults to 23" claim is wrong, and large
frames arrive in a single notification.

The device advertises as `VTR-<digits>` with the service UUID above.

## Frame layout

| Offset | Field |
| --- | --- |
| 0 | sync, always `0x55` |
| 1 | total frame length `& 0xFF` (header + payload + 2 CRC bytes) |
| 2 | frame type — `0x04` normal, `0x05`/`0x09` extended |
| 3 | CRC8 over bytes 0–2 only |
| 4 | sender — `0xAA` app, `0x10` device |
| 5 | receiver — `0x10` device, `0xAA` app |
| 6–7 | sequence, uint16 LE |
| 8–9 | protocol, uint16 LE, always `0x0020` |
| 10 | command id |
| 11..n-3 | payload |
| n-2, n-1 | CRC16 over `frame[0:-2]`, uint16 LE |

Device→app frames swap sender/receiver to `0x10`→`0xAA` and use type `0x08`/`0x09`.

**Extended frames:** when type is `0x05` or `0x09`, the true length is
`0x100 + declared_length`. Observed in the wild: a 375-byte frame declaring
`0x77` (119).

### CRCs

Both use non-standard initial values. The CRC16 coverage span is not documented
anywhere and was recovered by brute-forcing every candidate byte range against
eight known-good frames until exactly one span satisfied all of them.

```python
def reflect(value: int, width: int) -> int:
    out = 0
    for i in range(width):
        if value & (1 << i):
            out |= 1 << (width - 1 - i)
    return out

def crc8(data: bytes) -> int:            # header only, bytes 0..2
    crc = 0xEE                            # init
    for byte in data:
        crc ^= reflect(byte, 8)
        for _ in range(8):
            crc = ((crc << 1) ^ 0x31) & 0xFF if crc & 0x80 else (crc << 1) & 0xFF
    return reflect(crc, 8)

def crc16(data: bytes) -> int:           # frame[0:-2]
    crc = 0x496C                          # init
    for byte in data:
        crc ^= reflect(byte, 8) << 8
        for _ in range(8):
            crc = ((crc << 1) ^ 0x1021) & 0xFFFF if crc & 0x8000 else (crc << 1) & 0xFFFF
    return reflect(crc, 16)
```

## Commands

```
0x0F PARAM_READ      0x10 ASYNC_STATE     0x11 PARAM_WRITE     0x19 SERIAL
0x27 HANDSHAKE       0x4F DEVICE_NAME     0x74 COMMON_STATE    0x77 FIRMWARE
0xA7 DEVICE_STATE    0xAA TELEMETRY       0xAB ACTIVATION      0xB4 STREAM
0x4E SET_DEVICE_NAME 0xAD STARTUP_IMAGE   0xAF BULK_PARAM_WRITE
```

## Bootstrap

Write these to the **transport** characteristic in order, pacing ~90 ms apart.
Responses arrive on **command**. The device is ready once any well-formed frame
comes back on command or transport.

```
1  app hello        552904c90110000020004f695061640000000000000000000000000000000000
                    84ab1a5f292001ea4f
2  connect request  550f0801aad200002000ff00aa0419
3  handshake check  551f044eaa10000020002781105eab9ef41c864ff5877a9c8c1d5f0d603e86
4  read state       550d0433aa10000020007403bc
5  firmware page 0  550e0466aa100100200077003889
6  firmware page 1  550e0466aa10020020007701cc94
7  read serial      550e0466aa100300200019002b7e
8  read activation  550e0466aa1004002000ab01ad7a
```

Frames 4–8 are ordinary `0xAA`→`0x10` type-`0x04` frames and can be rebuilt from
scratch; 1 and 2 use non-standard sender/receiver and are replayed verbatim.

Frame 3 (command `0x27`) carries an **18-byte opaque blob** that nothing in any
known implementation derives — confirmed against three independent
reverse-engineered projects, none of which computes it; all hardcode a captured
constant, exactly as we do. It is *not* the client-identity string — that lives
in the separate `0x4F` frame (frame 1, `iPad`). The blob is **lineage-specific,
not device-specific**: our value (and the unlicensed HA component's) begins
`81 10 5e ab 9e f4 …`, while the `voltra-node-sdk` lineage (`voltra-knob`,
`voltra-diy-remote`) uses a different constant beginning `81 10 65 f8 c6 b9 …`.
Both are accepted by the device, so there is no single "correct" blob to derive.

It behaves as a static magic — it works across devices — but if a firmware
update ever invalidates it, bootstrap will fail silently: no response ever
arrives. The client now detects this by **timing out on the first
command-characteristic response** (`BootstrapTimeout`) rather than sleeping a
fixed interval and assuming success — a silent dead-end becomes a loud,
retryable failure.

Frame 1 identifies the client as the ASCII string **`iPad`**, inherited from the
capture this was derived from. The device shows that name on its authorisation
prompt. Changing it re-triggers authorisation.

## Parameters

```
read    [count uint16 LE][param_id uint16 LE] * count
write   [0x01 0x00][param_id uint16 LE][value, width per param]
reply   [status uint8][count uint16 LE]([param_id uint16 LE][value]) * count
```

Value widths are **per-parameter and not inferable from the frame** — a registry
is mandatory. Getting a width wrong silently misaligns every subsequent parameter
in a multi-parameter reply.

| Param | Width | Meaning |
| --- | --- | --- |
| `0x3E86` | uint16 LE | target load, lb (valid 5–200) |
| `0x3E87` | uint16 LE | chains — added resistance that scales with cable position, lb (0–100) |
| `0x3E88` | **int16 LE** | eccentric overload — **signed**; units unconfirmed (see note) |
| `0x3E89` | uint16 LE | fitness mode — `0x0004` ready/unloaded, `0x0005` loaded |
| `0x4FB0` | uint8 | workout state — `0` inactive, `1` weight training |
| `0x3E83` | uint16 LE | instantaneous force, **tenths of a pound** |
| `0x3E82` | uint16 LE | cable position, **mm** (not cm — see note) |
| `0x53B0` | uint8 | chain direction — `0` normal, `1` inverse; pairs with `0x3E87` |
| `0x5350` | uint32 LE | isokinetic target speed, mm/s (0–2000) |
| `0x5362` | uint16 LE | resistance-band max force, lb (15–70) |
| `0x4E2D` | uint8 | battery percent (legacy alias `0x1B5D`) |
| `0x5182` | uint8 | telemetry notify rate (`0x28` = 40 Hz) |
| `0x5183` | uint32 | telemetry subscribe token (`F5 7B 65 00`) |
| `0x520A`–`0x520C` | uint16 LE | unknown; pushed via `0x10`. Appear in none of the three third-party implementations either — likely app-version-specific. |

> **Units — position and force.** Position was previously labelled cm here; it
> is almost certainly **mm**. A single rep traces roughly 11 → 566 over the
> field, and 566 cm (5.7 m) is impossible for a cable pull while 566 mm (0.57 m)
> is exactly right; `voltra-node-sdk` also decodes it as mm. Force is in tenths
> of a pound in the same stream. Neither is consumed by PUMP's set-logging path,
> so this is a documentation correction rather than a behaviour change.

> **Eccentric (`0x3E88`) is signed, and its unit is not yet confirmed.**
> `voltra-node-sdk` and `voltra-diy-remote` read it as signed **pounds**;
> `voltra-knob` reads it as a signed **percentage**. The range (±~195/200) fits
> either. PUMP registers the id so a state push carrying it decodes cleanly, but
> **does not write it** — a sign or scale error on a motor write is a physical
> hazard. Confirm the unit on hardware before driving eccentric overload.

> **Provenance of the new ids.** `0x3E87`/`0x3E88`/`0x53B0`/`0x5350`/`0x5362` are
> cross-checked across three independent MIT-licensed implementations
> (`HJewkes/voltra-node-sdk`, `omarshahine/voltra-knob`,
> `RyanMarkoff-eaton/voltra-diy-remote`), which agree on id and width. Those
> same projects independently reproduce the CRC parameters, frame layout, UUIDs,
> command ids and the workout-state enum below — strong corroboration of the
> facts in this document.

Workout-state enum (`0x4FB0`): `0` inactive, `1` weight training, `2` resistance
band, `3` rowing, `4` damper, `6` custom curve, `7` isokinetic, `8` isometric.

## Telemetry

Nothing streams until you **subscribe**: write `0x5183 = F5 7B 65 00`, then
`0x5182 = 0x28`.

### `0xB4` — high-rate stream

8-byte payload, four LE fields — force (tenths lb), cable position (mm),
velocity (**signed int16** mm/s; negative = eccentric), and a `0x0032` constant:

```
[force][cable position][velocity][0x0032 constant]
```

Position traces the rep curve cleanly — observed rising 11 → 566 and back over a
single repetition (mm, not cm — 566 mm is a plausible pull, 566 cm is not).
Roughly 40 Hz while loaded. **PUMP does not need this.**

### `0xAA` — event and summary frames

Discriminated by `payload[0]`/`payload[1]`:

| Subtype | Len | Content |
| --- | --- | --- |
| `0x85`/`0x5F` | 97 | **end-of-set summary, once per set — `[13]` = set, `[15]` = final rep count** |
| `0x84`/`0x40` | 66 | **live progress, ~1 Hz — `[2]` = set, `[3:5]` uint16 BE = rep** |
| `0x81`/`0x2B` | 45 | per-sample — `[2]` = phase, `[3]` = set, `[4:6]` uint16 BE = rep |
| `0x80`/`0x25` | 39 | session-state ping — `[3:5]` BE is a **state code, not a rep count** |
| `0x82`/`0x3B` | 61 | rep boundary event (alternates 01/02 — the up and down phase) |
| `0x87`/`0x**` | 134–214 | waveform chunks |

**`0x85/0x5F` is the authoritative set boundary.** It is emitted exactly once,
roughly half a second after the final rep, and carries both the set number and
the final rep count in single bytes. Across both capture sessions it reads
set 3 / 5 reps and set 2 / 3 reps, matching what was physically performed.
Consume this and you do not have to infer where a set ended.

`0x84/0x40` gives live progress at ~1 Hz while the set is under way. It stops
entirely when the athlete stops — it is not a heartbeat — so its *absence* is a
usable fallback boundary if the summary is lost in transport. `0x81/0x2B` was
not emitted in every session.

`0x80/0x25` is a session-state ping that continues while the device is idle.
Its `[3:5]` field walks `0 → 1 → 2 → 3 → 4 → 0` across a session independently
of how many reps were performed (`3` appears once a set has completed), so it
is a state code. An earlier revision of this document described it as a rep
count; that was wrong.

> **Rep counters are big-endian.** Every parameter value is little-endian. This
> is the single easiest mistake to make in this protocol. Note that the two
> counters in `0x85/0x5F` are single bytes, so endianness does not arise there.

### `0x10` — async state push

Unsolicited on state change, carrying up to 9 parameters in the standard
parameter-reply encoding. **Prefer this over polling** — the device volunteers
mode, target load, and workout-state transitions.

### `0xA7` — device state broadcast

Byte 0 is battery percent.

## Behaviours that will cost you a day

1. **The motor load expires.** After writing `0x3E89 = 0x0005` the device
   silently disengages after a timeout. Re-assert every ~8 s to hold it. This
   presents as "the protocol is broken" — telemetry flows but force and position
   read 0 forever.

   > **Unresolved — re-validate on hardware.** This ~8 s auto-expiry was
   > observed on 2026-08-03 and is why we re-assert `MODE_LOADED` every 5 s. But
   > three of the four third-party implementations (`voltra-node-sdk`,
   > `voltra-knob`, and the HA component for strength mode) do **not** re-assert
   > strength load at all — they rely on write-confirmation and device-side
   > persistence. Either the expiry is mode-specific (and our keepalive is
   > unnecessary churn for weight training), or those projects carry a latent
   > silent-drop bug. This is motor-safety-adjacent; confirm which is true at the
   > trainer before changing the keepalive. **Do not remove the keepalive on the
   > strength of the other implementations alone.**
2. **Parameter writes are rejected unless a workout is active.** With
   `0x4FB0 == 0` the device accepts the write, replies `0x11` with payload `00`,
   and changes nothing. Read state first and fail loudly.
3. **Telemetry requires the explicit subscribe** above. Without it the device is
   completely silent, including for reps.
4. **Verify target load by read-back before loading.** If the weight write
   silently fails, the subsequent load applies the *previous* weight — which may
   be far heavier than intended.
5. **A loaded-but-idle device streams constant values.** Static `0xB4` frames
   mean "engaged, nothing moving", not "broken".
