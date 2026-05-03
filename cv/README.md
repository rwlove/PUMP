# pump-cv

Camera-based exercise auto-detection sidecar for [PUMP](../README.md).
Runs as a separate Python service alongside the Go API; reads RTSP
streams from gym cameras, detects sets/reps/exercises, and writes them
to PUMP via the per-set HTTP API exposed in PUMP v0.0.72+.

See [`docs/cv-autolog-plan.md`](../docs/cv-autolog-plan.md) for the full
design.

## Status

Phase 1, in progress. Complete (all unit-tested without GPU):

- Repo bootstrap (this README, Dockerfile, pyproject)
- Structured logging + yaml/env config loader
- Async PUMP API client (POST/PATCH/DELETE/confirm + SSE consumer)
- Pose layer types + a YOLOv8-Pose wrapper (untested on real GPU yet) +
  a synthetic mock source for unit testing
- Single-athlete picker, rep counter, set-boundary FSM
- Pipeline runner that wires these together
- Multi-camera fusion via DLT triangulation (`pump_cv.fusion`)
- Camera calibration script (`python -m pump_cv.calibration intrinsics|stereo`)
- Plate-color barbell weight detector (`pump_cv.weight.estimate_barbell_load`)
- DTW exercise classifier + on-disk PrototypeStore (`pump_cv.classify`)
- 28 tests covering every pure-logic module, including triangulation
  recovery to sub-mm against synthetic ground truth and a live PUMP API
  integration test

Not yet (genuinely needs cameras / phase 2):

- Real-camera RTSP smoke test for the YOLOv8 wrapper
- Running the calibration CLI against actual checkerboard photos
- Reference-clip recording flow in the PUMP UI (a separate slice)
- Wiring the classifier and weight detector into the pipeline runner
  (currently only the rep counter and set FSM are wired; weight defaults
  to 0 and exercise is hardcoded)

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
