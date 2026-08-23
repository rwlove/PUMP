<p align="center">
<a href="https://github.com/rwlove/PUMP/actions/workflows/container-publish.yml"><img src="https://github.com/rwlove/PUMP/actions/workflows/container-publish.yml/badge.svg" alt="Publish Container Images"></a>
<a href="https://goreportcard.com/report/github.com/rwlove/PUMP"><img src="https://goreportcard.com/badge/github.com/rwlove/PUMP" alt="Go Report Card"></a>
</p>

<p align="center"><img src="assets/logo.svg" alt="PUMP" width="320"></p>

**Please Use More Protein** — workout diary with daily set logging, body weight tracking, an exercise **Library** (managed groups, reusable routines/templates, and per-exercise primary/secondary focus muscles), training stats, and a whole-body **Health** dashboard fed by your fitness tracker via Android Health Connect.

| Exercise Library — groups, routines & exercise management |
|---|
| ![Exercise Library](assets/screenshot-library.png) |

| Overall Health | Workout | Stats: Exercise Distribution |
|---|---|---|
| ![Overall Health](assets/screenshot-health.png) | ![Workout](assets/screenshot-workout.png) | ![Stats Exercise Distribution](assets/screenshot-stats-overview.png) |

| Stats: Weight Moved | Stats: Body Weight | Config |
|---|---|---|
| ![Stats Weight Moved](assets/screenshot-stats-activity.png) | ![Stats Body Weight](assets/screenshot-stats-weight.png) | ![Config](assets/screenshot-config.png) |

| Stats: Steps | Stats: Heart Rate | Stats: Sleep |
|---|---|---|
| ![Stats Steps](assets/screenshot-stats-steps.png) | ![Stats Heart Rate](assets/screenshot-stats-hr.png) | ![Stats Sleep](assets/screenshot-stats-sleep.png) |

| Stats: Cardio | Stats: Muscle Balance | Stats: Personal Records |
|---|---|---|
| ![Stats Cardio](assets/screenshot-stats-cardio.png) | ![Stats Muscle Balance](assets/screenshot-stats-balance.png) | ![Stats Personal Records](assets/screenshot-stats-prs.png) |

| Stats: Progressive Overload | Stats: Consistency | Stats: Recovery |
|---|---|---|
| ![Stats Progressive Overload](assets/screenshot-stats-overload.png) | ![Stats Consistency](assets/screenshot-stats-consistency.png) | ![Stats Recovery](assets/screenshot-stats-recovery.png) |

| Stats: Grip & Hang — grip strength (left/right) and dead-hang time |
|---|
| ![Stats Grip & Hang](assets/screenshot-stats-grip.png) |

- [Architecture](#architecture)
- [Configuration](#configuration)

## Architecture

```
Browser ──▶ pump  :8080  ──▶ PostgreSQL
            (UI + API)
```

The `pump` monolith serves both the web UI and the JSON API on a **single port (default `8080`)**.
There is no separate API or frontend port — all traffic goes through `:8080`.

| Path prefix | Purpose |
|---|---|
| `/` | Web UI (HTML, CSS, JS) |
| `/api/` | JSON REST API (used by `pump-cv` and other in-cluster integrations) |

Use image `ghcr.io/rwlove/pump`. Set `POSTGRES_DSN`. Front the deployment with an OIDC reverse proxy (e.g. `oauth2-proxy`) on the gateway — pump itself ships with no inbound auth.

### Optional: pump-cv camera sidecar

A separate Python service under [`cv/`](cv/) watches gym cameras, detects exercises/reps/sets, and writes them to PUMP via the per-set REST API. Disabled by default — enable on the config page (`CVAutoLog`) once cameras are installed and the sidecar is running. See [`docs/cv-autolog-plan.md`](docs/cv-autolog-plan.md) for the full design and [`cv/README.md`](cv/README.md) for runtime details.

### Optional: pump-voltra trainer sidecar

A separate Python service under [`voltra/`](voltra/) that reads sets, reps and
resistance off a **Beyond Power VOLTRA I** cable trainer over BLE and logs them
automatically. The trainer's resistance is electronic, so plate detection
cannot see it — without this, every Voltra set has to have its weight typed in
by hand.

Which exercises use the trainer is a per-exercise checkbox ("Uses Voltra
trainer") on the exercise configuration page, not a guess from the name.
Disabled by default — set `VOLTRA_AUTOLOG=true` on PUMP and `VOLTRA_ENABLED=true`
on the sidecar. See [`voltra/README.md`](voltra/README.md).

### Health dashboard & wearable metrics

The **Health** page (`/health/`) is a one-page, whole-body view that pulls from every source PUMP tracks — body weight, strength training, and wearable metrics — as summary tiles (latest value, trend delta, sparkline) that deep-link into the matching Stats tab.

Wearable data is ingested generically from **Android Health Connect** via the [HC Webhook](https://github.com/mcnaveen/health-connect-webhook) bridge app, which POSTs a Health Connect envelope to **`POST /api/health`** (gated by `HEALTH_INGEST_KEY`). Each datum is stored in the `health_record` table, deduped on `(metric_type, start_time, end_time)`. The known types (steps, active calories, heart rate, resting heart rate, sleep, exercise) are charted on dedicated Stats tabs — **Steps**, **Heart Rate**, **Sleep**, **Cardio** — and every other Health Connect type is preserved generically, so no schema change is needed to ingest new metrics.

Notes on source coverage: sleep stages arrive as Health Connect numeric stage codes and are decoded into Deep/Light/REM/Awake minutes. When a source exports raw `heart_rate` but no `resting_heart_rate`, the day's minimum heart rate is used as a resting-HR estimate. The **Cardio** tab charts daily **active calories** (`active_calories`); per-session cardio also appears there once the bridge exports `ExerciseSession` records.

Cardio sessions can also be captured **directly from the gym treadmill** with no wearable involved. The treadmill's Z-Wave metering smart plug publishes its wattage to MQTT (via `zwave-js-ui`); when `TREADMILL_MQTT_ENABLED` is set, PUMP subscribes to that feed, detects a workout with a wattage threshold + off-debounce state machine, and writes each session as an `exercise` record (duration only — a plug can't measure distance). These land on the **Cardio** tab alongside wearable sessions and dedupe on `(metric_type, start_time, end_time)` like every other health record. See the `TREADMILL_MQTT_*` settings below.

## Configuration

All configuration is via environment variables. No config file is required.

### `pump`

| Variable | Description | Default |
|---|---|---|
| `PORT` | Listen port | `8080` |
| `HOST` | Listen address | `0.0.0.0` |
| `POSTGRES_DSN` | PostgreSQL connection string **(required)** | — |
| `API_KEY` | Sent as `X-Api-Key` when proxying to `pump-cv` (server-to-server only — pump has no inbound auth) | `""` |
| `WEIGHT_INGEST_KEY` | When set, `POST /api/weight` requires header `X-Api-Key` matching this value. Enables off-cluster ingest (e.g. a BLE-scale ESPHome device) via an oauth2-bypassing internal Route scoped to `/api/weight`. Unset preserves the legacy no-inbound-auth posture. | `""` |
| `WEIGHT_MIN_LBS` | Lower bound (lbs) for `POST /api/weight`. A reading below this is rejected `422` and logged (`warn`) instead of stored — a backstop below the scale firmware's own band, so a bad reading from any source can't corrupt the log. | `50` |
| `WEIGHT_MAX_LBS` | Upper bound (lbs) for `POST /api/weight`. A reading above this is rejected `422` and logged. | `500` |
| `HEALTH_INGEST_KEY` | When set, `POST /api/health` requires header `X-Api-Key` matching this value. Enables off-cluster wearable-metrics ingest from Android Health Connect (via the HC Webhook bridge app) over a path-scoped internal Route. The endpoint accepts a Health Connect envelope (per-type arrays: steps, active_calories, heart_rate, sleep, exercise, …) and stores each datum in `health_record`, deduped on `(metric_type, start_time, end_time)`. Unset preserves the no-inbound-auth posture. | `""` |
| `LOG_LEVEL` | Log verbosity: `debug`, `info`, `warn`, `error` | `info` |
| `COLOR` | UI color mode: `light` or `dark` | `dark` |
| `PAGESTEP` | Rows per page on the body weight log | `10` |
| `DISPLAY_DAYS` | Days of workout history shown on the main page (7/30/90/365) | `30` |
| `AUTOFILL` | Pre-fill weight/reps from last performance when adding a set | `true` |
| `CVAUTOLOG` | Accept set writes from the `pump-cv` camera sidecar; toggleable in the UI | `false` |
| `VOLTRA_AUTOLOG` | Accept set writes from the `pump-voltra` trainer sidecar; env-only | `false` |
| `PUSHOVER_USER_KEY` | Pushover user key for low-confidence set notifications; env-only, never in UI | `""` |
| `PUSHOVER_APP_TOKEN` | Pushover app token for low-confidence set notifications; env-only, never in UI | `""` |
| `PUSHOVER_API_URL` | Pushover API endpoint override (testing only) | Pushover |
| `PUBLIC_URL` | Externally-reachable PUMP base URL; used to build deep-links in notifications | `""` |
| `PUMP_CV_URL` | Where to forward reference-clip uploads (e.g. `http://pump-cv:8080`); empty disables the in-browser recorder | `""` |
| `PUMP_CLIPS_DIR` | Local directory of per-set clip mp4s written by `pump-cv` (shared volume), served at `/clips/`; empty disables clip serving | `""` |
| `NODE_PATH` | Path to local `node_modules` directory; empty = use CDN for Bootstrap/Chart.js | `""` |
| `TREADMILL_MQTT_ENABLED` | Enable treadmill cardio auto-capture: subscribe to the smart-plug wattage feed `zwave-js-ui` publishes to MQTT, detect each workout, and log it as a cardio session (no Home Assistant in the path). | `false` |
| `TREADMILL_MQTT_BROKER` | MQTT broker URL, e.g. `tcp://emqx-headless.home.svc.cluster.local:1883` | `""` |
| `TREADMILL_MQTT_USERNAME` | MQTT broker username | `""` |
| `TREADMILL_MQTT_PASSWORD` | MQTT broker password | `""` |
| `TREADMILL_MQTT_TOPIC` | Topic carrying the plug's instantaneous wattage (Z-Wave Meter CC) | `zwave/Gym/Treadmill/50/0/value/66049` |
| `TREADMILL_WATTS_THRESHOLD` | Watts at/above which the treadmill counts as "in use" | `50` |
| `TREADMILL_OFF_DEBOUNCE_SECONDS` | Sustained sub-threshold time before a session closes (ignores brief dips) | `60` |
| `TREADMILL_MIN_SESSION_SECONDS` | Sessions shorter than this are discarded | `120` |
| `TREADMILL_SESSION_TYPE` | Cardio type label stored with each session | `Treadmill` |
| `WHISPER_WYOMING_ADDR` | `host:port` of a self-hosted [Wyoming](https://github.com/rhasspy/wyoming) whisper (faster-whisper) service. When set, the per-set note mic dictates via `POST /api/stt` → this service (16 kHz mono PCM), so audio never leaves the network. Unset returns `503` and the mic surfaces "not configured". | `""` |
| `TZ` | Timezone | `""` |

`POSTGRES_DSN` must be set or the server will not start:

```
POSTGRES_DSN=postgres://user:password@host:5432/pump
```

The schema is versioned and managed automatically on startup — no manual `CREATE TABLE` needed.

### `pump-cv` (optional camera sidecar)

A separate Python service under [`cv/`](cv/) that watches gym RTSP cameras, detects exercises/reps/sets, and writes them to PUMP via the per-set REST API. Disabled by default — set `CVAUTOLOG=true` (or flip the toggle in PUMP's settings) once cameras are installed and the sidecar is running.

Reads its configuration from a yaml file (mounted as a Kubernetes ConfigMap, default path `/app/configs/default.yaml`) plus a few env overrides. See [`cv/README.md`](cv/README.md) for the full module breakdown.

| Variable | Description | Default |
|---|---|---|
| `PUMP_API_BASE_URL` | Override `pump.base_url` from the yaml | yaml value |
| `PUMP_API_KEY` | Sent as `X-Api-Key` on every PUMP request; **secret, not in yaml** | `""` |
| `PUMP_CV_CONFIG` | Path to the yaml config | `configs/default.yaml` |
| `PUMP_CV_PROTOTYPE_DIR` | Where DTW exercise prototypes are stored | `prototypes` |
| `PUMP_CV_SNAPSHOT_DIR` | Where annotated debug snapshots are written per detected set | `snapshots` |
| `PUMP_CV_HEALTHD_PORT` | Listen port for `/healthz`, `/readyz`, `/metrics`, and `POST /api/v1/reference` | `8080` |
| `CV_CONFIDENCE_THRESHOLD` | Override the cutoff for marking a CV-detected set pending | yaml value |
| `VOLTRA_ENABLED` | When true, `pump-voltra` owns sets for Voltra-flagged exercises and `pump-cv` writes none of them | `false` |
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` | `info` |

### `pump-voltra` (optional trainer sidecar)

A separate Python service under [`voltra/`](voltra/) that reads set number, rep count and target load off a **Beyond Power VOLTRA I** cable trainer over BLE — via an ESPHome `bluetooth_proxy` — and logs each set automatically. The trainer's resistance is electronic and invisible to plate detection, so without it every Voltra set needs its weight typed in by hand.

Which exercises use the trainer is a per-exercise checkbox ("Uses Voltra trainer") on the exercise configuration page. Set names are inherited from the day's most recent set for a flagged exercise.

It also drives the motor. Clicking a Voltra set on the workout page **arms** it, so telemetry is attributed to that set; a separate **LOAD** press writes that set's weight to the trainer and engages it. Arming never engages anything by itself, and a set is only recorded once the sidecar confirms by read-back that the motor is holding the requested weight — an armed-but-unloaded set is drawn as deliberately unfinished rather than looking ready.

Disabled by default — set `VOLTRA_AUTOLOG=true` on PUMP and `VOLTRA_ENABLED=true` on the sidecar. Same yaml + env-override pattern as `pump-cv`. See [`voltra/README.md`](voltra/README.md).

| Variable | Description | Default |
|---|---|---|
| `VOLTRA_ENABLED` | Master switch; false disables all BLE activity | `false` |
| `VOLTRA_ADDRESS` | Trainer BLE MAC; **secret, not in yaml** | `""` |
| `VOLTRA_PROXY_HOST` | ESPHome `bluetooth_proxy` hostname | `""` |
| `VOLTRA_PROXY_PSK` | ESPHome API Noise key; **secret, not in yaml** | `""` |
| `VOLTRA_DEFAULT_EXERCISE` | Name used when no flagged set exists yet today (written `pending`) | `Voltra` |
| `VOLTRA_EXERCISE_REFRESH_SECONDS` | How often to re-read which exercises carry the flag | `300` |
| `VOLTRA_SET_IDLE_SECONDS` | Fallback set-completion timeout, used only if the device's end-of-set summary is lost | `30` |
| `VOLTRA_LOAD_POLL_SECONDS` | Target-load poll interval | `5` |
| `VOLTRA_MAX_LOAD_LB` | Ceiling on any weight written to the motor; requests above it are clamped | `130` |
| `PUMP_API_BASE_URL` | PUMP base URL | `http://pump-api:8851` |
| `PUMP_API_KEY` | Sent as `X-Api-Key`; **secret, not in yaml** | `""` |
| `PUMP_VOLTRA_CONFIG` | Path to the yaml config | `configs/default.yaml` |

Run **one replica with `strategy: Recreate`** — the trainer accepts a single BLE central, so two replicas would fight over the connection.

#### Architecture sketch

```
┌────────────────────────┐         ┌──────────────────────────┐
│ pump (Go) Deployment   │         │ pump-cv (Python)         │
│  ports: 8080           │         │  ports: 8080 (healthd)   │
│  env: POSTGRES_DSN     │ ──────▶ │  resources: nvidia.com/  │
│       PUMP_CV_URL      │  HTTP   │             gpu: 1       │
│       PUSHOVER_*       │ ◀────── │  env: PUMP_API_BASE_URL  │
│       CVAUTOLOG=true   │ /api/   │       PUMP_API_KEY (Sec) │
└────────────────────────┘  sets   │  vol: /data/pump-cv      │
            │                      │       (prototypes,       │
            ▼                      │        snapshots)        │
   PostgreSQL (Service)            │  config: ConfigMap →     │
                                   │          /app/configs/   │
                                   └──────────────────────────┘
                                              ▲
                                              │ RTSP
                                  ┌───────────┴───────────┐
                                  │     IP cameras        │
                                  └───────────────────────┘

┌────────────────────────┐         ┌──────────────────────────┐
│ pump (Go) Deployment   │ ◀────── │ pump-voltra (Python)     │
│  env: VOLTRA_AUTOLOG   │  /api/  │  replicas: 1, Recreate   │
│            =true       │  sets   │  env: VOLTRA_PROXY_HOST  │
└────────────────────────┘         │       VOLTRA_PROXY_PSK   │
                                   │       VOLTRA_ADDRESS     │
                                   └──────────────────────────┘
                                              ▲
                                              │ ESPHome API (Noise)
                                  ┌───────────┴───────────┐
                                  │ ESP32 bluetooth_proxy │
                                  └───────────┬───────────┘
                                              │ BLE
                                  ┌───────────┴───────────┐
                                  │   VOLTRA I trainer    │
                                  └───────────────────────┘
```

The services are independent Deployments, talk only over HTTP+JSON, and any can be restarted without touching the others. `pump-cv` only needs egress to PUMP and ingress from RTSP cameras — no public-internet egress (Pushover credentials live on the PUMP side, not the sidecar).

`pump-voltra` runs **one replica with `strategy: Recreate`** — the trainer accepts a single BLE central, so two replicas would fight over the connection.

A persistent volume holding `prototypes/` and `snapshots/` is recommended; both directories are write-only from `pump-cv` and survive Pod restarts. Exercise prototypes are loaded once at sidecar startup, so a rolling restart is needed to pick up new prototypes (UI uploads work via the live `POST /api/v1/reference` endpoint without restart, but the next pipeline restart will then see them).
