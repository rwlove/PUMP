"""Configuration for pump-cv.

Layered:
  1. Defaults baked into the dataclass
  2. YAML file at $PUMP_CV_CONFIG (or ./configs/default.yaml)
  3. Environment variable overrides for a few common knobs

The YAML lives in a Kubernetes ConfigMap; secrets (PUMP_API_KEY) come
from the env, never from the file.
"""

from __future__ import annotations

import os
from pathlib import Path
from typing import Literal

import yaml
from pydantic import BaseModel, Field


class CameraConfig(BaseModel):
    """One physical camera or, during development, a video file."""

    name: str
    rtsp_url: str | None = None
    video_path: str | None = None  # for file-source dev/testing
    calibration_path: str | None = None  # path to .npz with intrinsics+extrinsics


class PoseConfig(BaseModel):
    backend: Literal["yolo", "mock"] = "yolo"
    model: str = "yolov8m-pose.pt"
    image_size: int = 640
    device: str = "cuda:0"
    # Force a capture reconnect if no frame arrives within this window — the
    # guard against a half-open RTSP stream wedging cap.read() forever.
    read_stale_seconds: float = 15.0


class HealthConfig(BaseModel):
    """Liveness freshness gate: /healthz fails if no live camera has produced a
    frame within this window, so a wedged pipeline is restarted instead of
    sitting Ready-but-dead. Generous by default (well above read_stale_seconds ×
    a reconnect cycle) so a single camera reboot does not flap the pod."""

    frame_stale_seconds: float = 120.0


class RepConfig(BaseModel):
    """Tunables for the rep-counter peak detector."""

    min_amplitude_deg: float = 25.0
    min_period_s: float = 0.6
    smoothing_window: int = 9


class SetBoundaryConfig(BaseModel):
    """Tunables for the set-boundary FSM."""

    quiet_seconds: float = 25.0


class PumpConfig(BaseModel):
    base_url: str = "http://pump-api:8851"
    api_key: str = ""  # env: PUMP_API_KEY
    request_timeout_s: float = 10.0


class VoltraConfig(BaseModel):
    """Coordination with the pump-voltra sidecar.

    When it is running it owns sets for Voltra-flagged exercises outright —
    it reads the real resistance and the device's own rep count. CV must not
    also write them, or every Voltra set is logged twice.
    """

    enabled: bool = False  # env: VOLTRA_ENABLED
    flag_refresh_seconds: float = 300.0


class CVConfig(BaseModel):
    cameras: list[CameraConfig] = Field(default_factory=list)
    pose: PoseConfig = Field(default_factory=PoseConfig)
    rep: RepConfig = Field(default_factory=RepConfig)
    set_boundary: SetBoundaryConfig = Field(default_factory=SetBoundaryConfig)
    pump: PumpConfig = Field(default_factory=PumpConfig)
    confidence_threshold: float = 0.75
    voltra: VoltraConfig = Field(default_factory=VoltraConfig)
    health: HealthConfig = Field(default_factory=HealthConfig)


def load(path: str | Path | None = None) -> CVConfig:
    """Load a config from disk plus environment overrides.

    Path resolution order:
      1. explicit `path` argument
      2. $PUMP_CV_CONFIG
      3. ./configs/default.yaml (relative to cwd; fine for local dev)
      4. defaults only, if no file found
    """
    candidates = [
        path,
        os.getenv("PUMP_CV_CONFIG"),
        "configs/default.yaml",
    ]
    raw: dict = {}
    for c in candidates:
        if not c:
            continue
        p = Path(c)
        if p.is_file():
            raw = yaml.safe_load(p.read_text()) or {}
            break

    cfg = CVConfig(**raw)

    # Env overrides — only the things you really want to flip from K8s without
    # editing the ConfigMap.
    if v := os.getenv("PUMP_API_BASE_URL"):
        cfg.pump.base_url = v
    if v := os.getenv("PUMP_API_KEY"):
        cfg.pump.api_key = v
    if v := os.getenv("CV_CONFIDENCE_THRESHOLD"):
        cfg.confidence_threshold = float(v)
    # Same variable the sidecar reads, so one setting governs both sides.
    if v := os.getenv("VOLTRA_ENABLED"):
        cfg.voltra.enabled = v.strip().lower() in ("1", "true", "yes", "on")

    return cfg
