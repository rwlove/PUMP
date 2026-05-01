<p align="center">
<a href="https://github.com/rwlove/PUMP/actions/workflows/container-publish.yml"><img src="https://github.com/rwlove/PUMP/actions/workflows/container-publish.yml/badge.svg" alt="Publish Container Images"></a>
<a href="https://goreportcard.com/report/github.com/rwlove/PUMP"><img src="https://goreportcard.com/badge/github.com/rwlove/PUMP" alt="Go Report Card"></a>
</p>

<p align="center"><img src="assets/logo.svg" alt="PUMP" width="320"></p>

**Please Use More Protein** — workout diary with GitHub-style year visualization. Log daily sets, track body weight, and visualize training history with intensity heatmaps.

| Workout | Stats: Overview | Stats: Weight Moved |
|---|---|---|
| ![Workout](assets/screenshot-workout.png) | ![Stats Overview](assets/screenshot-stats-overview.png) | ![Stats Weight Moved](assets/screenshot-stats-activity.png) |

| Stats: Body Weight | Config | |
|---|---|---|
| ![Stats Body Weight](assets/screenshot-stats-weight.png) | ![Config](assets/screenshot-config.png) | |

- [Architecture](#architecture)
- [Configuration](#configuration)

## Architecture

PUMP can be deployed in two ways:

### All-in-one (recommended)

```
Browser ──▶ pump  :8080  ──▶ PostgreSQL
            (UI + API)
```

Use image `ghcr.io/rwlove/pump`. Set `POSTGRES_DSN` and optionally `API_KEY`.

### Split services (advanced)

```
Browser ──▶ pump-frontend :8080 ──HTTP──▶ pump-api :8851 ──▶ PostgreSQL
```

Use images `ghcr.io/rwlove/pump-frontend` + `ghcr.io/rwlove/pump-api`.

## Android App

The PUMP Android app provides the same workout logging experience as the web UI, connecting to any PUMP API server you specify.

**Requires Android 16 (API 36) or later.**

| Workout | Stats | Weight |
|---|---|---|
| *(screenshot coming soon)* | *(screenshot coming soon)* | *(screenshot coming soon)* |

### Installation

Download the latest APK from the [Releases page](https://github.com/rwlove/PUMP/releases) and install it on your device. You may need to allow installation from unknown sources.

### Configuration

On first launch, open **Settings** and enter:

| Field | Description |
|---|---|
| API URL | Base URL of your PUMP API server (e.g. `http://192.168.1.10:8080`) |
| API Key | Optional — must match `API_KEY` on the server |

## Configuration

Both services are configured exclusively via environment variables. No config file is required.

### API server (`pump-api`)

| Variable | Description | Default |
|---|---|---|
| `PORT` | Listen port | `8851` |
| `HOST` | Listen address | `0.0.0.0` |
| `POSTGRES_DSN` | PostgreSQL connection string **(required)** | — |
| `API_KEY` | Require this value on every `X-Api-Key` request header; empty = no auth | `""` |
| `THEME` | Any [Bootswatch](https://bootswatch.com) theme (lowercase) or extras: `emerald`, `grass`, `grayscale`, `ocean`, `sand`, `wood` | `cosmo` |
| `COLOR` | Background: `light` or `dark` | `dark` |
| `HEATCOLOR` | Heatmap cell color | `#2780e3` |
| `PAGESTEP` | Rows per page | `10` |
| `DISPLAY_DAYS` | Days of history shown on the main page (7/30/90/365) | `30` |
| `TZ` | Timezone | `""` |

`POSTGRES_DSN` must be set or the API server will not start:

```
POSTGRES_DSN=postgres://user:password@host:5432/pump
```

The schema is versioned and managed automatically on startup — no manual `CREATE TABLE` needed.

### Frontend server (`pump-frontend`)

| Variable | Description | Default |
|---|---|---|
| `PORT` | Listen port | `8080` |
| `API_URL` | Base URL of the API server | `http://localhost:8851` |
| `API_KEY` | `X-Api-Key` value sent to the API (must match API server `API_KEY`) | `""` |
| `TZ` | Timezone | `""` |
