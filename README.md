<p align="center">
<a href="https://github.com/rwlove/PUMP/actions/workflows/container-publish.yml"><img src="https://github.com/rwlove/PUMP/actions/workflows/container-publish.yml/badge.svg" alt="Publish Container Images"></a>
<a href="https://goreportcard.com/report/github.com/rwlove/PUMP"><img src="https://goreportcard.com/badge/github.com/rwlove/PUMP" alt="Go Report Card"></a>
</p>

<p align="center"><img src="assets/logo.svg" alt="PUMP" width="320"></p>

**Please Use More Protein** — workout diary with daily set logging, body weight tracking, and training history stats.

| Workout | Stats: Exercise Distribution | Stats: Weight Moved |
|---|---|---|
| ![Workout](assets/screenshot-workout.png) | ![Stats Exercise Distribution](assets/screenshot-stats-overview.png) | ![Stats Weight Moved](assets/screenshot-stats-activity.png) |

| Stats: Body Weight | Config | |
|---|---|---|
| ![Stats Body Weight](assets/screenshot-stats-weight.png) | ![Config](assets/screenshot-config.png) | |

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

## Configuration

All configuration is via environment variables. No config file is required.

### `pump`

| Variable | Description | Default |
|---|---|---|
| `PORT` | Listen port | `8080` |
| `HOST` | Listen address | `0.0.0.0` |
| `POSTGRES_DSN` | PostgreSQL connection string **(required)** | — |
| `API_KEY` | Sent as `X-Api-Key` when proxying to `pump-cv` (server-to-server only — pump has no inbound auth) | `""` |
| `LOG_LEVEL` | Log verbosity: `debug`, `info`, `warn`, `error` | `info` |
| `COLOR` | UI color mode: `light` or `dark` | `dark` |
| `PAGESTEP` | Rows per page on the body weight log | `10` |
| `DISPLAY_DAYS` | Days of workout history shown on the main page (7/30/90/365) | `30` |
| `FREQUENCY_DAYS` | Look-back window (days) for sorting exercises by usage frequency | `30` |
| `AUTOFILL` | Pre-fill weight/reps from last performance when adding a set | `true` |
| `CVAUTOLOG` | Accept set writes from the `pump-cv` camera sidecar; toggleable in the UI | `false` |
| `PUSHOVER_USER_KEY` | Pushover user key for low-confidence set notifications; env-only, never in UI | `""` |
| `PUSHOVER_APP_TOKEN` | Pushover app token for low-confidence set notifications; env-only, never in UI | `""` |
| `PUSHOVER_API_URL` | Pushover API endpoint override (testing only) | Pushover |
| `PUBLIC_URL` | Externally-reachable PUMP base URL; used to build deep-links in notifications | `""` |
| `PUMP_CV_URL` | Where to forward reference-clip uploads (e.g. `http://pump-cv:8080`); empty disables the in-browser recorder | `""` |
| `NODE_PATH` | Path to local `node_modules` directory; empty = use CDN for Bootstrap/Chart.js | `""` |
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
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` | `info` |

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
```

The two services are independent Deployments, talk only over HTTP+JSON, and either can be restarted without touching the other. `pump-cv` only needs egress to PUMP and ingress from RTSP cameras — no public-internet egress (Pushover credentials live on the PUMP side, not the sidecar).

A persistent volume holding `prototypes/` and `snapshots/` is recommended; both directories are write-only from `pump-cv` and survive Pod restarts. Exercise prototypes are loaded once at sidecar startup, so a rolling restart is needed to pick up new prototypes (UI uploads work via the live `POST /api/v1/reference` endpoint without restart, but the next pipeline restart will then see them).
