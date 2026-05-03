# pump-cv

Camera-based exercise auto-detection sidecar for [PUMP](../README.md).
Runs as a separate Python service alongside the Go API; reads RTSP
streams from gym cameras, detects sets/reps/exercises, and writes them
to PUMP via the per-set HTTP API exposed in PUMP v0.0.72+.

See [`docs/cv-autolog-plan.md`](../docs/cv-autolog-plan.md) for the full
design.

## Status

Phase 1, in progress. Complete:

- Repo bootstrap (this README, Dockerfile, pyproject)
- Structured logging + yaml/env config loader
- Async PUMP API client (POST/PATCH/DELETE/confirm + SSE consumer)
- Pose layer types + a YOLOv8-Pose wrapper (untested on real GPU yet) +
  a synthetic mock source for unit testing
- Single-athlete picker, rep counter, set-boundary FSM
- Pipeline runner that wires these together
- Unit tests for every pure-logic module + a live integration test
  against a running pump-api

Not yet:

- Real-camera RTSP smoke test (waiting on physical install)
- Camera intrinsics/extrinsics calibration script
- Multi-camera fusion (3D triangulation)
- Plate-color barbell weight detector
- DTW exercise classifier
- Reference-clip recording flow

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
| `LOG_LEVEL` | `debug` / `info` / `warn` / `error` |

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
  Dockerfile
  pyproject.toml
```
