# Voltra integration plan

Adds automatic logging of Beyond Power VOLTRA I sets to PUMP: real resistance,
real rep counts, and software control of the trainer's weight.

Protocol reference: [`voltra-ble-protocol.md`](voltra-ble-protocol.md).
Related: [`cv-autolog-plan.md`](cv-autolog-plan.md), [`cv-autolog-todo.md`](cv-autolog-todo.md).

## Why

`pump_cv/pipeline/runner.py` treated any exercise whose name contains "voltra"
as **opaque load** — its resistance is electronic and invisible to plate
detection, so the sidecar wrote `pending=true`, `weight=0`, and a note asking
the athlete to type the resistance in afterwards.

The trainer knows its own resistance, set number, and rep count. Reading them
closes that gap and removes a manual step from every Voltra set.

## Status of the underlying work

The protocol spike is **complete**. Validated against real hardware:

| Capability | State |
| --- | --- |
| Frame codec (build/parse, both CRCs) | validated offline and on device |
| Bootstrap / handshake | works on MainControl v1.6 |
| Read parameters | target load verified against the device display |
| Write target load | verified by read-back and on-device display |
| Load / unload motor | verified engaged |
| Set + rep telemetry | a 5-rep set read back exactly as set 3, reps 1→5 |
| Transport over an ESPHome Bluetooth proxy | verified from the proxy's own logs |

No part of this plan depends on unproven protocol behaviour.

## Architecture

```
VOLTRA ──BLE──▶ ESP32 (ESPHome bluetooth_proxy, active connections)
                     │  ESPHome native API, TCP 6053, Noise-encrypted
                     ▼
              pump-voltra  (Python sidecar, in-cluster)
                     │  POST/PATCH /api/sets, X-Api-Key
                     ▼
                  PUMP (Go)
```

### Why a separate sidecar, not part of `pump-cv`

`pump-cv` exists to own the GPU. A BLE client needs no GPU, and coupling the two
would tie Voltra availability to the CV deployment's lifecycle, its ~3 GB image,
and its restart cadence. `pump-voltra` is a small pure-Python service that can
scale and roll independently.

### Why an ESPHome proxy rather than a Bluetooth adapter

The sidecar runs in-cluster; the trainer is in the gym. An ESP32 running
`bluetooth_proxy` with **active connections** bridges gym-BLE to LAN-TCP for the
price of a small board.

The known weakness of ESPHome proxies is that they silently drop notifications
under TCP backpressure, with no queue and no counter. **That does not apply
here**: set and rep counts arrive at roughly 1 Hz on `0xAA` subtype `0x84/0x40`,
and the 40 Hz `0xB4` stream is not needed. Do not enable the high-rate stream
over the proxy.

## The central design question: who owns set boundaries?

Both CV and the trainer believe they can detect sets, and they will disagree.

- **CV** has a quiet-period state machine and knows *which exercise* is being
  performed — the trainer has no idea whether you are rowing or curling.
- **The trainer** has ground truth for *resistance*, *set number*, and *rep
  count* — CV's rep counting is pose-derived and less reliable.

If both write sets, Voltra sessions produce duplicates.

### Options

**A — trainer owns the set, CV supplies the name.** `pump-voltra` writes the
set; CV is queried for the exercise detected in that time window. Most accurate,
but couples the two sidecars and needs a correlation mechanism that does not
exist yet.

**B — CV owns the set, trainer enriches it.** CV writes `pending=true` with
`weight=0` exactly as today; `pump-voltra` matches by time window and `PATCH`es
the real weight and reps, then confirms. Reuses `PATCH /api/sets/:id` and
`POST /api/sets/:id/confirm`, which already exist. Fragile if the time-window
match is wrong, and does nothing when CV is off.

**C — trainer owns the set, exercise name deferred.** `pump-voltra` writes a
complete set — real weight, real reps — with a configured default name and
`pending=true` so the athlete corrects the exercise in the UI. Inverts today's
situation: weight becomes known, name becomes the uncertain field.

### What shipped

**A refinement of C, and it removed most of C's cost.** The sidecar owns the
set, but the exercise name is not left to a fixed placeholder: it is inherited
from the most recent set logged today for a **Voltra-flagged exercise**, read
from `GET /api/sets/stream`. The athlete taps "Cable Row" and logs set 1 by
hand; every following set auto-logs under that name. The configured default and
`pending=true` remain, but only as the degraded path for a set logged before
any anchor exists.

This keeps C's properties — no cross-sidecar coupling, works with CV disabled —
while making the common case fully automatic rather than requiring a name
correction on every set.

**Which exercises count is a flag, not a name.** A `voltra` boolean on the
`exercises` table, surfaced as a checkbox on the exercise configuration page.
The original `"voltra" in name` heuristic is gone, and matching `"cable"`
instead would have been just as wrong: plenty of exercises have "cable" in the
name and run off a plate stack. This is the `equipment_type` column that the
section below defers — it turned out to be required, not optional, so it
shipped as migration **v11**.

### Original recommendation, for the record

**Ship C first, move to A later.** C has no cross-sidecar coupling, works with CV
disabled, and delivers the main win immediately — resistance and reps stop being
manual. It degrades to exactly today's behaviour if the trainer is unreachable.
A is the right end state but should not gate the first release.

Do **not** ship B. Time-window correlation between two independent detectors is
the kind of thing that works in testing and produces mismatched sets in
production.

## Phasing

### Phase 1 — read-only auto-logging

- Connect through the proxy, bootstrap, subscribe to telemetry.
- Track set/rep from `0xAA` `0x84/0x40` and target load from `0x3E86`. Close
  the set on `0xAA` `0x85/0x5F`, the device's own end-of-set summary, which was
  identified after this plan was written — see the protocol doc. The idle
  timeout is the fallback for when that frame is lost in transport.
- On set completion, `POST /api/sets` with `Source: "voltra"`, real weight and
  real reps, named from the day's most recent Voltra-flagged set. Only when no
  such set exists yet does it fall back to the configured default with
  `Pending: true`.
- **No motor commands.** Never sets the weight, never loads or unloads.
  Note this is *not* the same as "no writes": the device streams nothing until
  the telemetry subscribe (`0x5183`, then `0x5182`) is written, and every
  write is silently ignored unless a workout is active. Both are handled
  explicitly and fail loudly. An earlier revision of this document claimed
  phase 1 wrote nothing at all; that was wrong.
- Update `runner.py` so CV skips writing sets for Voltra-flagged exercises
  when `pump-voltra` is enabled, avoiding duplicates.

Exit criteria: a full workout logs correct weights and rep counts with no manual
resistance entry.

### Phase 2 — control

- `PATCH`-style control endpoint or UI action to set target load from PUMP.
- Load/unload with the safety rules below.
- Enables programmed progression: PUMP sets the weight for the next set.

### Phase 3 — CV fusion (option A)

- Correlate CV's exercise classification with the trainer's set boundaries.
- Retire the `pending` flag for Voltra sets when both agree.

## Module layout

```
voltra/
  pyproject.toml
  pump_voltra/
    __init__.py
    protocol.py      frame build/parse, CRC8/CRC16
    registry.py      parameter ids, their widths, and the parameter codec
    telemetry.py     0xAA subtype decoding
    client.py        BLE session: bootstrap, subscribe, read/write, keepalive
    transport.py     ESPHome-proxy wiring (bleak-esphome)
    session.py       set/rep state machine, set-completion detection
    naming.py        resolves which exercise an auto-logged set belongs to
    runner.py        wires telemetry to PUMP; also drives --replay
    healthd.py       /healthz, /readyz, /metrics
    pump_client.py   POST/PATCH against PUMP, mirrors cv/pump_cv/pump_client.py
    config.py
    log.py
    main.py
  tests/
    test_protocol.py   golden frames — see Testing
    test_session.py
```

`protocol.py` and `registry.py` are pure functions over bytes with no I/O, which
is what makes the golden-frame tests possible.

## Configuration

Follows the existing `TREADMILL_*` / `PUMP_CV_*` conventions.

| Variable | Purpose | Default |
| --- | --- | --- |
| `VOLTRA_ENABLED` | Master switch; false disables all BLE activity | `false` |
| `VOLTRA_PROXY_HOST` | ESPHome proxy hostname | `""` |
| `VOLTRA_PROXY_PSK` | ESPHome API encryption key; **secret, not in yaml** | `""` |
| `VOLTRA_ADDRESS` | Trainer BLE MAC; **site-specific, not in yaml** | `""` |
| `VOLTRA_DEFAULT_EXERCISE` | Exercise name written on new sets | `Voltra` |
| `VOLTRA_LOAD_REFRESH_SECONDS` | Keepalive interval for the motor load | `8` |
| `VOLTRA_MAX_LOAD_LB` | Hard clamp on any weight this service will write | `50` |
| `VOLTRA_SET_IDLE_SECONDS` | Idle time before a set is considered complete | `30` |
| `PUMP_URL` | PUMP base URL | `http://pump:8080` |
| `PUMP_API_KEY` | Sent as `X-Api-Key`; **secret** | `""` |

Deliberately **no** option to enable the 40 Hz stream — it is not needed and is
the one thing the proxy handles badly.

## PUMP-side changes

### Schema

`Source: "voltra"` needs no schema change — it is a new *value* in an existing
column. **Migration v11 was still required**, for the per-exercise `voltra`
flag; the claim that phase 1 needed no migration did not survive the decision
to stop guessing from exercise names.

Per [`AGENTS.md`](../AGENTS.md), schema migrations are irreversible once deployed
and any table/column change requires a patch bump on the `pump-vX.Y.Z` tag line.
Two candidates are deliberately deferred:

- ~~An `equipment_type` column on `exercises`~~ — **shipped in v11**, as a
  `voltra` boolean rather than an enum. If a second smart device ever appears,
  migrate the column to a text `equipment_type` rather than adding a third
  boolean.
- Persisting the device's set number. Probably unnecessary; PUMP orders sets by
  insertion within a day.

### API

No new endpoints. Phase 1 uses `POST /api/sets`; phases 2–3 use the existing
`PATCH /api/sets/:id` and `POST /api/sets/:id/confirm`.

`POST /api/sets` gates `Source: "voltra"` on `VOLTRA_AUTOLOG`, mirroring the
existing `CVAutoLog` gate on `Source: "cv"`. The two are independent: enabling
CV must not open the Voltra path.

### The workout page's save mode — the trap this plan originally missed

`index.js` chose between two save strategies based on `CVAutoLog`:

- **on** → per-set `POST /api/sets` and `PATCH /api/sets/:id`, addressing rows
  by id;
- **off** → `saveWorkoutBulk()`, which POSTs the whole form to `/set/` and calls
  `BulkReplaceSetsByDate` — deleting *every row for that date* and reinserting
  only what the browser's DOM holds.

With `CVAUTOLOG=false` and a sidecar writing, the next autosave silently
destroys every auto-logged set. Worse, `setHandler` carries no `source`,
`confidence`, `pending` or `clip_path`, so even rows that survive lose their
provenance.

The gate is now `Conf.AutoLog()` — true when *either* sidecar may write —
threaded through the template and both JS call sites. Any future ingest source
must be added to it. This applies to `pump-cv` too; it was only ever safe
because CV and its gate happened to be the same flag.

### `runner.py`

The Voltra branch must not write a set when `pump-voltra` is active, or every
Voltra set is logged twice. It is gated behind `VOLTRA_ENABLED` rather than
deleted — the opaque-load path stays the fallback when the trainer is
unreachable or the sidecar is not deployed.

`is_voltra` now comes from the exercise's `Voltra` flag via
`GET /api/exercises`, refreshed on a timer, not from its name.

## Safety

Loading engages a motor capable of substantial cable tension under software
control. These are requirements, not suggestions:

- **Never load on reconnect or startup.** Load only as a direct result of an
  explicit user action.
- **Always verify target load by read-back before loading.** A silently failed
  weight write means the *previous* weight is applied.
- **Clamp every write** to `VOLTRA_MAX_LOAD_LB`, defaulting low. The protocol
  accepts up to 200 lb; the service should not.
- **Always unload on shutdown**, including on SIGTERM and on unhandled
  exceptions.
- **Never re-send a queued command after a reconnect.** Discard pending
  intent — the athlete's position has changed.
- Rely on the device's own auto-unload as a backstop; do not defeat it.

Phase 1 sidesteps all of this by not writing at all. Do not let phase 2 merge
without these implemented and tested.

## Testing

**Golden-frame tests are the backbone.** The spike produced ~1,000 captured
frames across several sessions, plus eight known-good bootstrap frames. Because
`protocol.py` is pure, the entire codec is testable with no hardware:

- rebuild each captured bootstrap frame from scratch and assert byte equality
- assert both CRCs over every captured frame
- decode the captured telemetry logs and assert the set/rep sequence matches what
  was physically performed

That last one is a real regression test: one capture is a 5-rep set that must
decode as set 3, reps 1→5.

`session.py` gets ordinary unit tests for set-completion and the idle timeout.
Mock the BLE layer — no test should need a trainer.

## Deployment

New tag line, matching the existing split:

- `pump-voltra-vA.B.C` → `ghcr.io/rwlove/pump-voltra:vA.B.C`

Own Deployment, no GPU, small resource envelope. One replica — the trainer accepts
a single BLE central, so **two replicas would fight over the connection**. Set
`replicas: 1` with `strategy: Recreate`, not `RollingUpdate`, for the same reason.

## Failure modes

| Condition | Behaviour |
| --- | --- |
| Proxy unreachable | log, retry with backoff, no sets written |
| Trainer out of range or off | idle, retry; CV opaque-load path remains the fallback |
| Bootstrap never validates | surface loudly — likely a firmware change invalidating the handshake blob |
| Workout inactive | refuse writes with a clear error; do not silently no-op |
| PUMP unreachable | buffer completed sets in memory, retry; drop with a logged error on overflow |

## Open questions

- **Set-boundary ownership** — option C is recommended for phase 1, but A is the
  end state. The correlation mechanism for A is undesigned.
- **Exercise naming.** Does the athlete pick the exercise in the UI after the
  fact, or does the sidecar take a "current exercise" hint from somewhere?
- **Range.** The proxy-to-trainer BLE link has only been exercised with both
  devices on a desk. Real gym placement is unverified.
- **Multiple trainers.** The design assumes one. The proxy supports three
  concurrent connections, so this is a config problem rather than an architectural
  one.
