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
- [Android App](#android-app)
- [Configuration](#configuration)

## Architecture

```
Browser / Android app ──▶ pump  :8080  ──▶ PostgreSQL
                          (UI + API)
```

The `pump` monolith serves both the web UI and the JSON API on a **single port (default `8080`)**.
There is no separate API or frontend port — all traffic goes through `:8080`.

| Path prefix | Purpose |
|---|---|
| `/` | Web UI (HTML, CSS, JS) |
| `/api/` | JSON REST API (used by the Android app and direct integrations) |

Use image `ghcr.io/rwlove/pump`. Set `POSTGRES_DSN` and optionally `API_KEY`.

## Android App

The PUMP Android app provides the same workout logging experience as the web UI, connecting to any PUMP API server you specify.

**Requires Android 16 (API 36) or later.**

| Workout | Stats | Weight |
|---|---|---|
| *(screenshot coming soon)* | *(screenshot coming soon)* | *(screenshot coming soon)* |

### Installation

Scan to download the latest APK:

<p align="center"><img src="assets/qr-android-install.png" width="180" alt="QR code — PUMP Android APK"></p>

Or download directly from the [Releases page](https://github.com/rwlove/PUMP/releases). You may need to allow installation from unknown sources on your device.

### Configuration

On first launch, open **Settings** and enter:

| Field | Description |
|---|---|
| API URL | Base URL of your PUMP API server (e.g. `http://192.168.1.10:8080`) |
| API Key | Optional — must match `API_KEY` on the server |

## Configuration

All configuration is via environment variables. No config file is required.

### `pump`

| Variable | Description | Default |
|---|---|---|
| `PORT` | Listen port | `8080` |
| `HOST` | Listen address | `0.0.0.0` |
| `POSTGRES_DSN` | PostgreSQL connection string **(required)** | — |
| `API_KEY` | Require this value on every `X-Api-Key` request header; empty = no auth | `""` |
| `LOG_LEVEL` | Log verbosity: `debug`, `info`, `warn`, `error` | `info` |
| `COLOR` | UI color mode: `light` or `dark` | `dark` |
| `PAGESTEP` | Rows per page on the body weight log | `10` |
| `DISPLAY_DAYS` | Days of workout history shown on the main page (7/30/90/365) | `30` |
| `FREQUENCY_DAYS` | Look-back window (days) for sorting exercises by usage frequency | `30` |
| `AUTOFILL` | Pre-fill weight/reps from last performance when adding a set | `true` |
| `CVAUTOLOG` | Accept set writes from the `pump-cv` camera sidecar; toggleable in the UI | `false` |
| `PUSHOVER_USER_KEY` | Pushover user key for low-confidence set notifications; env-only, never in UI | `""` |
| `PUSHOVER_APP_TOKEN` | Pushover app token for low-confidence set notifications; env-only, never in UI | `""` |
| `NODE_PATH` | Path to local `node_modules` directory; empty = use CDN for Bootstrap/Chart.js | `""` |
| `TZ` | Timezone | `""` |

`POSTGRES_DSN` must be set or the server will not start:

```
POSTGRES_DSN=postgres://user:password@host:5432/pump
```

The schema is versioned and managed automatically on startup — no manual `CREATE TABLE` needed.
