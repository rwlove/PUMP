# pump-voltra

Auto-logs sets from a **Beyond Power VOLTRA I** cable trainer into
[PUMP](../README.md): real resistance, real rep counts, no manual entry.

The trainer's resistance is electronic, so plate detection cannot see it. Until
now a Voltra set was logged with `weight=0` and a note asking the athlete to
type the resistance in afterwards. This service reads it off the device.

Protocol reference: [`../docs/voltra-ble-protocol.md`](../docs/voltra-ble-protocol.md).
Design and phasing: [`../docs/voltra-integration-plan.md`](../docs/voltra-integration-plan.md).

## How it works

```
VOLTRA ──BLE──▶ ESP32 (ESPHome bluetooth_proxy, active connections)
                     │  ESPHome native API, TCP 6053, Noise-encrypted
                     ▼
              pump-voltra
                     │  GET /api/exercises    which exercises are Voltra
                     │  GET /api/sets/stream  which one is being done now
                     │  POST /api/sets        Source: "voltra"
                     ▼
                  PUMP
```

**Which exercises count** is a checkbox on each exercise's configuration page
in PUMP, not a name heuristic — plenty of exercises have "cable" in the name
and run off a plate stack.

**What a set is called** comes from PUMP. The sidecar tracks the most recent
set logged today for a Voltra-flagged exercise: tap "Cable Row", log set 1 by
hand, and every following set inherits that name. With no such set yet today it
falls back to a configured default and marks the set `pending` so the athlete
fixes the name in the UI.

**When a set ends** comes from the device. It emits an end-of-set summary
(`0xAA` subtype `0x85/0x5F`) carrying both the set number and the final rep
count, about half a second after the last rep. An idle timeout is kept as a
fallback for when that frame is lost in transport.

## Phase 1 scope

Read-only: **no motor commands**. It never sets the weight and never loads or
unloads the cable. The athlete drives the trainer with its own controls.

It is not literally write-free — the device streams nothing until the telemetry
subscribe is written, and every write is silently ignored unless a workout is
active. Both are handled explicitly and fail loudly rather than hanging.

## Configuration

Layered: `configs/default.yaml`, then environment overrides. **Secrets and
site-specific identifiers are environment-only and must never be committed.**

| Variable | Purpose | Default |
| --- | --- | --- |
| `VOLTRA_ENABLED` | master switch; false disables all BLE activity | `false` |
| `VOLTRA_ADDRESS` | trainer BLE MAC — **secret** | `""` |
| `VOLTRA_PROXY_HOST` | ESPHome proxy hostname | `""` |
| `VOLTRA_PROXY_PSK` | ESPHome API Noise key — **secret** | `""` |
| `VOLTRA_DEFAULT_EXERCISE` | name used when no anchor exists today | `Voltra` |
| `VOLTRA_EXERCISE_REFRESH_SECONDS` | how often to re-read the flag list | `300` |
| `VOLTRA_SET_IDLE_SECONDS` | fallback set-completion timeout | `30` |
| `VOLTRA_LOAD_POLL_SECONDS` | target-load poll interval | `5` |
| `VOLTRA_MAX_LOAD_LB` | ceiling on any weight written to the motor; higher requests are clamped | `130` |
| `VOLTRA_HEARTBEAT_STALE_SECONDS` | work-loop tick age before `/healthz` fails and the pod is restarted | `600` |
| `VOLTRA_SSE_READ_TIMEOUT_S` | read-idle timeout for the PUMP SSE streams; above the 25 s keepalive so a dead stream reconnects instead of hanging | `60` |
| `PUMP_API_BASE_URL` | PUMP base URL | `http://pump-api:8851` |
| `PUMP_API_KEY` | sent as `X-Api-Key` — **secret** | `""` |
| `PUMP_VOLTRA_CONFIG` | path to a YAML config file | `configs/default.yaml` |

PUMP must also be started with `VOLTRA_AUTOLOG=true`, or it refuses
`Source: "voltra"` writes with a 403.

There is deliberately **no** option to enable the device's 40 Hz stream. It is
not needed — set and rep counts arrive at ~1 Hz — and it is the one thing an
ESPHome proxy handles badly, dropping notifications under backpressure with no
queue and no counter.

## Deployment

One replica, `strategy: Recreate`. The trainer accepts a single BLE central, so
two replicas would fight over the connection.

Probes: `/healthz`, `/readyz`, `/metrics` on port 8080. `/readyz` reports 503
when the trainer is not connected, which is the normal state of an empty gym —
do not page on it.

A rising `pump_voltra_sets_inferred_total` means end-of-set summaries are being
dropped in transport: sets are still logged, but from the idle timeout.

## Testing

```
pip install --no-deps -e . && pip install -r <(...)   # see .github/workflows/voltra-test.yml
ruff check pump_voltra tests
pytest -q
```

No hardware required. `protocol.py`, `registry.py`, `telemetry.py`,
`session.py` and `naming.py` are pure, and the suite includes:

- golden-frame tests that rebuild captured bootstrap frames byte-for-byte and
  verify both CRCs, including the CRC16 coverage span;
- replay of two real captured sessions — a 5-rep set reported as set 3, and a
  3-rep set reported as set 2 — asserting the decoded counts match what was
  physically performed.

To exercise the whole path against a running PUMP without a trainer:

```
python -m pump_voltra.main --replay tests/fixtures/session-set3-5reps.tsv --weight 40
```

This drives the real decoder, set tracker, naming resolver and HTTP client.

## Provenance and licensing

Protocol *facts* — service and characteristic UUIDs, CRC parameters, frame
layout, command and parameter ids — are not copyrightable and were
reimplemented here from those facts alone.

The only prior community work, `dylanmaniatakes/Beyond-Power-HomeAssistant`,
carries **no licence** (all rights reserved). No code from it was copied. Its
bootstrap packet 1 is also malformed — one byte short of its declared length,
with an invalid CRC16; the corrected frame is used here and was accepted by the
device.

Beyond Power ships an official CLI whose terms forbid speaking to the device
directly. It exposes no live telemetry, so it cannot count reps regardless.

The trainer's MAC address and serial number appear in this repository nowhere,
including in test fixtures. PUMP is public.
