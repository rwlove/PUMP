"""Layered configuration: YAML defaults, then environment overrides.

Mirrors cv/pump_cv/config.py. Secrets — the proxy PSK, the trainer's BLE
address, the PUMP API key — come from the environment only and must never be
committed to a YAML file.
"""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any

import yaml
from pydantic import BaseModel, Field


class PumpConfig(BaseModel):
    base_url: str = "http://pump-api:8851"
    api_key: str = ""
    request_timeout_s: float = 10.0
    # Read-idle timeout for the SSE streams (set events + Voltra desired state).
    # These are deliberately idle between events, so the request_timeout_s read
    # limit would tear a healthy stream down constantly. But `read=None` blocks
    # forever on a half-open connection (PUMP pod rescheduled, dropped flow),
    # wedging the motor-control stream silently — it stops applying LOAD/unload
    # and never reaches the unload-on-teardown path. PUMP sends a `: keepalive`
    # every 25 s (internal/api/helpers.go sseLoop), so a finite timeout above
    # that survives idle but turns a dead connection into a ReadTimeout the
    # watch loops reconnect from.
    sse_read_timeout_s: float = 60.0


class ProxyConfig(BaseModel):
    host: str = ""
    port: int = 6053
    psk: str = ""


class VoltraConfig(BaseModel):
    enabled: bool = False
    address: str = ""
    proxy: ProxyConfig = Field(default_factory=ProxyConfig)
    pump: PumpConfig = Field(default_factory=PumpConfig)

    # Exercise name written when no Voltra-flagged set exists yet today. Such
    # sets are marked pending so the athlete corrects the name in the UI.
    default_exercise: str = "Voltra"
    # How often to re-read which exercises carry the Voltra flag, so newly
    # flagged exercises are picked up without a restart.
    exercise_refresh_seconds: float = 300.0
    # Fallback set-completion timeout, used only when the device's own
    # end-of-set summary never arrives.
    set_idle_seconds: float = 30.0
    # How long to wait for the trainer to advertise before giving up on this
    # pass. The proxy session is kept open across these, so a long wait costs
    # nothing and gives the scanner's advertisement subscription time to land.
    discovery_timeout_seconds: float = 300.0
    # Hard clamp on any weight this service will write to the trainer. A
    # backstop against a bug producing a garbage value, not a training limit —
    # the protocol accepts 200. Raise it deliberately if real work outgrows it
    # rather than leaving it to silently reject sets.
    max_load_lb: int = 130

    # Slow poll of the trainer's target load. Deliberately not the 40 Hz
    # stream: that is the one thing the ESPHome proxy handles badly.
    load_poll_seconds: float = 5.0

    healthd_host: str = "0.0.0.0"
    healthd_port: int = 8080

    # Liveness watchdog: max seconds the work loop may go without a progress
    # tick before /healthz fails and the kubelet restarts the pod. Must exceed
    # the trainer-discovery wait (discovery_timeout_seconds) so an empty gym is
    # never mistaken for a wedge; default leaves a comfortable margin over 300.
    heartbeat_stale_seconds: float = 600.0


def _env_str(cfg: Any, attr: str, key: str) -> None:
    if v := os.getenv(key):
        setattr(cfg, attr, v)


def _env_float(cfg: Any, attr: str, key: str) -> None:
    if v := os.getenv(key):
        try:
            setattr(cfg, attr, float(v))
        except ValueError:
            pass


def load(path: str | Path | None = None) -> VoltraConfig:
    candidates = [path, os.getenv("PUMP_VOLTRA_CONFIG"), "configs/default.yaml"]
    raw: dict = {}
    for c in candidates:
        if not c:
            continue
        p = Path(c)
        if p.is_file():
            raw = yaml.safe_load(p.read_text()) or {}
            break

    cfg = VoltraConfig(**raw)

    if v := os.getenv("VOLTRA_ENABLED"):
        cfg.enabled = v.strip().lower() in ("1", "true", "yes", "on")
    _env_str(cfg, "address", "VOLTRA_ADDRESS")
    _env_str(cfg, "default_exercise", "VOLTRA_DEFAULT_EXERCISE")
    _env_float(cfg, "exercise_refresh_seconds", "VOLTRA_EXERCISE_REFRESH_SECONDS")
    _env_float(cfg, "set_idle_seconds", "VOLTRA_SET_IDLE_SECONDS")
    _env_float(cfg, "load_poll_seconds", "VOLTRA_LOAD_POLL_SECONDS")
    _env_float(cfg, "heartbeat_stale_seconds", "VOLTRA_HEARTBEAT_STALE_SECONDS")
    if v := os.getenv("VOLTRA_MAX_LOAD_LB"):
        try:
            cfg.max_load_lb = int(float(v))
        except ValueError:
            pass

    _env_str(cfg.proxy, "host", "VOLTRA_PROXY_HOST")
    _env_str(cfg.proxy, "psk", "VOLTRA_PROXY_PSK")

    # Shared with pump-cv: same 1Password field, same server.
    _env_str(cfg.pump, "base_url", "PUMP_API_BASE_URL")
    _env_str(cfg.pump, "api_key", "PUMP_API_KEY")
    _env_float(cfg.pump, "sse_read_timeout_s", "VOLTRA_SSE_READ_TIMEOUT_S")

    return cfg
