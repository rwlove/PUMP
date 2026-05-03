"""pump-cv: camera-based exercise auto-detection sidecar for PUMP.

The package is structured as a few cooperating layers:

- pump_cv.config       — yaml + env config
- pump_cv.log          — structured logging setup
- pump_cv.pump_client  — async HTTP client for the PUMP REST API + SSE
- pump_cv.pose         — pose extraction (real YOLOv8 + a synthetic mock)
- pump_cv.tracking     — single-athlete picker
- pump_cv.fsm          — rep counter + set-boundary state machine
- pump_cv.pipeline     — wiring that turns a frame stream into PUMP set writes
- pump_cv.main         — entry point

The split is designed so that everything except pose extraction can be
unit-tested without a GPU or real video.
"""

__version__ = "0.1.0"
