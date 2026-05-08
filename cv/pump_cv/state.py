"""Pump-cv runtime state machine.

Currently used to gate the rep-detection pipeline behind a calibration
prerequisite when multiple cameras are configured. With <2 cameras
calibration is N/A and we go straight to ``ready``; with 2+ cameras the
process holds in ``needs_calibration`` until both per-camera ``.npz``
files exist on disk.

States:

    needs_calibration    multi-cam config, missing .npz files
    calibrating          wizard is actively capturing pairs / computing
    ready                pipeline is allowed to run
    error                last calibration compute raised; UI shows reason

This module is a tiny shared singleton — no IPC or persistence beyond
the .npz files on the cache PVC. The runner waits on
``await_ready_or_pause()`` and the wizard endpoints in healthd flip the
state during capture/compute.
"""

from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass, field, asdict
from pathlib import Path
from typing import Literal


Phase = Literal["needs_calibration", "calibrating", "ready", "error"]


@dataclass
class State:
    phase: Phase = "needs_calibration"
    # Number of cameras pump-cv is operating on (informational).
    camera_count: int = 0
    # Last-attempted compute's error message (only populated when
    # phase == "error"). Cleared on the next successful compute.
    error: str | None = None
    # Per-camera capture progress. Mirrors what's on disk under
    # /cache/calibration/captures/<name>/. Reset by /reset.
    captures: dict[str, int] = field(default_factory=dict)
    # Per-camera intrinsics-RMS / stereo-RMS reported by the last
    # compute, in pixels. Anything > ~1.0 is suspect; the wizard surfaces
    # these to the user so they can re-calibrate without diving into logs.
    metrics: dict[str, float] = field(default_factory=dict)


_state = State()
# Set whenever phase flips to "ready". Used by main.py to block startup
# until the wizard has produced calibration files.
_ready_event = asyncio.Event()


def get() -> State:
    return _state


def to_dict() -> dict:
    return asdict(_state)


def set_phase(phase: Phase, *, error: str | None = None) -> None:
    _state.phase = phase
    _state.error = error if phase == "error" else None
    if phase == "ready":
        _ready_event.set()
    else:
        _ready_event.clear()


def set_camera_count(n: int) -> None:
    _state.camera_count = n


def set_capture_count(camera_name: str, n: int) -> None:
    _state.captures[camera_name] = n


def set_metrics(metrics: dict[str, float]) -> None:
    _state.metrics = dict(metrics)


async def wait_for_ready() -> None:
    """Block until the runtime is allowed to run the rep pipeline."""
    await _ready_event.wait()


def is_ready() -> bool:
    return _state.phase == "ready"


# ─── Path helpers ────────────────────────────────────────────────────
# All artifacts live under one root so pump-cv only needs one mounted
# volume to persist calibration. Default matches the home-ops
# ``cache`` PVC mount; tests override via PUMP_CV_CACHE_DIR.

import os


def cache_root() -> Path:
    return Path(os.getenv("PUMP_CV_CACHE_DIR", "/cache"))


def calibration_dir() -> Path:
    """Root for all calibration artifacts. Wizard writes captures and
    final .npz files here; pump-cv config's ``calibration_path`` for
    each camera should point at ``calibration_dir() / f"{name}.npz"``."""
    return cache_root() / "calibration"


def captures_dir(camera_name: str) -> Path:
    return calibration_dir() / "captures" / camera_name


def npz_path(camera_name: str) -> Path:
    return calibration_dir() / f"{camera_name}.npz"


def state_marker_path() -> Path:
    """JSON snapshot of the captures dict, persisted across pod restarts
    so the wizard doesn't lose progress mid-flow."""
    return calibration_dir() / ".captures.json"


def load_persisted_captures() -> None:
    """Re-hydrate _state.captures from the on-disk marker (best-effort)."""
    p = state_marker_path()
    if not p.is_file():
        return
    try:
        data = json.loads(p.read_text())
        if isinstance(data, dict):
            _state.captures = {k: int(v) for k, v in data.items() if isinstance(v, int)}
    except (OSError, ValueError):
        # Marker corrupt or unreadable — wizard will overwrite on next capture.
        pass


def persist_captures() -> None:
    p = state_marker_path()
    try:
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_text(json.dumps(_state.captures))
    except OSError:
        # Non-fatal — disk full / read-only volume; wizard still works
        # in-memory for the current session.
        pass
