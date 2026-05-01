[![Publish Container Images](https://github.com/rwlove/PUMP/actions/workflows/container-publish.yml/badge.svg)](https://github.com/rwlove/PUMP/actions/workflows/container-publish.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/rwlove/PUMP)](https://goreportcard.com/report/github.com/rwlove/PUMP)

<p align="center"><img src="assets/logo.svg" alt="PUMP" width="320"></p>

**Please Use More Protein** — workout diary with GitHub-style year visualization. Log daily sets, track body weight, and visualize training history with intensity heatmaps.

| Workout | Stats: Overview | Stats: Activity |
|---|---|---|
| ![Workout](assets/screenshot-workout.png) | ![Stats Overview](assets/screenshot-stats-overview.png) | ![Stats Activity](assets/screenshot-stats-activity.png) |

| Stats: Exercises | Stats: Body Weight | Config |
|---|---|---|
| ![Stats Exercises](assets/screenshot-stats-exercises.png) | ![Stats Body Weight](assets/screenshot-stats-weight.png) | ![Config](assets/screenshot-config.png) |

- [Architecture](#architecture)
- [Quick start](#quick-start)
- [Configuration](#configuration)

## Architecture

PUMP runs as two independent services:

```
┌──────────────────────────┐        ┌──────────────────────────┐
│  pump-frontend           │─HTTP──▶│  pump-api                │
│  Web UI  (default :8080) │        │  JSON API  (default :8851) │
└──────────────────────────┘        └───────────┬──────────────┘
                                                 │
                                            PostgreSQL
```

| Service | Image | Description |
|---|---|---|
| API backend | `ghcr.io/rwlove/pump-api` | Owns the PostgreSQL database, exposes a JSON REST API |
| Web frontend | `ghcr.io/rwlove/pump-frontend` | Serves the browser UI, talks to the API over HTTP |

## Quick start

```sh
docker compose up
```

Or run each service manually:

```sh
# Start the API backend (stores data in /data/PUMP)
docker run --name exdiary-api \
  -v ~/.dockerdata/PUMP:/data/PUMP \
  -p 8851:8851 \
  ghcr.io/rwlove/pump-api

# Start the web frontend
docker run --name exdiary-frontend \
  -e API_URL=http://<YOUR_HOST_IP>:8851 \
  -p 8080:8080 \
  ghcr.io/rwlove/pump-frontend
```

Then open **http://localhost:8080** in your browser.

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
| `HEATCOLOR` | Heatmap cell color | `#03a70c` |
| `PAGESTEP` | Rows per page | `10` |
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
