# pump-cv

Camera-based exercise auto-detection sidecar for [PUMP](../README.md).
Runs as a separate Python service alongside the Go API; reads RTSP
streams from gym cameras, detects sets/reps/exercises, and writes them
to PUMP via the per-set HTTP API exposed in PUMP v0.0.72+.

See [`docs/cv-autolog-plan.md`](../docs/cv-autolog-plan.md) for the full
design.

## Status

Running in production against live cameras. The full pipeline is wired:

- Structured logging + yaml/env config loader
- Async PUMP API client (POST/PATCH/DELETE/confirm + SSE consumer)
- YOLOv8-Pose capture per camera + a synthetic mock source for unit testing
- Single-athlete picker, rep counter, set-boundary FSM
- Pipeline runner wiring pose → reps → set boundaries → commit, including
  the DTW exercise classifier and the plate-color barbell weight detector
- Multi-camera fusion via DLT triangulation (`pump_cv.fusion`) with a
  browser-driven stereo calibration wizard (plus the standalone
  `python -m pump_cv.calibration intrinsics|stereo` CLI)
- Reference-clip recording flow in the PUMP UI (`/exercise/` → healthd)
- healthd control/observability sidecar: liveness/readiness, Prometheus
  metrics, admin panel API (state/thresholds/prototypes/snapshots/cameras)
- Unit tests covering every pure-logic module, including triangulation
  recovery to sub-mm against synthetic ground truth and a live PUMP API
  integration test

## Quickstart (no GPU)

```
cd cv
python -m venv .venv && source .venv/bin/activate
pip install -e '.[dev]'
pytest
```

The unit tests don't load torch. They cover the rep counter, set FSM,
athlete picker, and a synthetic end-to-end pipeline.

The live integration test (`tests/test_pump_client_live.py`) auto-skips
unless `pump-api` is reachable on `localhost:8851`.

## Running against a video file

Put a clip at `cv/fixtures/sample-squat.mp4` and edit
`configs/default.yaml` to point at it (already the default), then:

```
PUMP_API_BASE_URL=http://localhost:8851 python -m pump_cv.main
```

This will load YOLOv8-Pose, process the video, and POST any detected
sets to PUMP. Without a CUDA GPU it runs CPU-only and is slow but
functional.

## Running with mock pose source (no model, no video)

Set `pose.backend: mock` in the config. The mock generates a synthetic
5-rep squat then 30 s of rest, which closes one set and writes it to
PUMP.

## Configuration

YAML config plus a few env overrides. See [`configs/default.yaml`](configs/default.yaml)
for the shape. Key env vars:

| Env var | Purpose |
|---|---|
| `PUMP_CV_CONFIG` | Path to the yaml config (default `configs/default.yaml`) |
| `PUMP_API_BASE_URL` | Override the PUMP API URL |
| `PUMP_API_KEY` | API key for X-Api-Key header |
| `CV_CONFIDENCE_THRESHOLD` | Override the confidence cutoff for pending sets |
| `PUMP_CV_RETENTION_DAYS` | Age cutoff for the snapshot/clip sweep (default `30`) |
| `PUMP_CV_RETENTION_MAX_BYTES` | Per-directory byte ceiling; oldest deleted first after the age sweep (default `5Gi`, `0` disables) |
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` |

Two liveness knobs live in the yaml (`configs/default.yaml`): `pose.read_stale_seconds`
(force a capture reconnect if no frame arrives within this window; default 15) and
`health.frame_stale_seconds` (`/healthz` fails if no live camera has produced a frame
within this window; default 120).

## Layout

```
cv/
  pump_cv/
    config.py         pydantic-typed yaml + env config
    log.py            structlog setup
    pump_client.py    httpx-based PUMP REST + SSE client
    pose/
      types.py        Pose / Keypoint / PoseSource Protocol + COCO indices
      yolo.py         YOLOv8-Pose source (real)
      mock.py         synthetic pose source (no torch)
    tracking/
      picker.py       single-athlete heuristic
    fsm/
      rep_counter.py  joint-angle peak detector
      set_boundary.py rest-period state machine
    pipeline/
      runner.py       wires source → picker → rep counter → set FSM → API
    main.py           entry point: `python -m pump_cv.main`
  tests/
  configs/
    default.yaml
  Containerfile
  pyproject.toml
```
