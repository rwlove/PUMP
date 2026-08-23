"""Liveness watchdog + heartbeat metric.

The wedge these guard against: the probe server stays up while the work loop
stops making progress, so the pod looks healthy forever. The heartbeat makes
that observable (a frozen timestamp) and actionable (liveness fails → restart).
"""

from __future__ import annotations

import time

import pytest

from pump_voltra import healthd


@pytest.fixture(autouse=True)
def _fresh_state():
    # Each test gets a clean module state and the default threshold.
    healthd._state = healthd.State()
    healthd.set_heartbeat_stale_after(600.0)
    yield


def test_fresh_heartbeat_is_not_stale():
    healthd.record_heartbeat()
    assert not healthd.heartbeat_stale()
    assert healthd.heartbeat_age() < 1.0


def test_old_heartbeat_is_stale():
    healthd.set_heartbeat_stale_after(600.0)
    healthd._state.heartbeat_ts = time.time() - 700
    assert healthd.heartbeat_stale()
    assert healthd.heartbeat_age() > 600


def test_startup_is_not_immediately_stale():
    # heartbeat_ts defaults to now, so a just-started sidecar has its full
    # grace window before liveness could fail — no boot-time crash loop.
    assert not healthd.heartbeat_stale()


def test_metric_exposes_heartbeat_timestamp():
    healthd.record_heartbeat()
    out = healthd.render_metrics()
    assert "pump_voltra_heartbeat_timestamp_seconds" in out
    line = [ln for ln in out.splitlines()
            if ln.startswith("pump_voltra_heartbeat_timestamp_seconds ")][0]
    assert float(line.split()[1]) == pytest.approx(healthd._state.heartbeat_ts)


def test_healthz_fails_when_wedged():
    fastapi = pytest.importorskip("fastapi")
    from fastapi.testclient import TestClient

    client = TestClient(healthd.build_app(), raise_server_exceptions=True)

    healthd.record_heartbeat()
    assert client.get("/healthz").status_code == 200

    # Simulate a wedge: heartbeat frozen well past the threshold.
    healthd._state.heartbeat_ts = time.time() - 700
    r = client.get("/healthz")
    assert r.status_code == 503
    assert r.json()["ok"] is False
    _ = fastapi  # keep the import referenced
