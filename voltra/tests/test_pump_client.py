"""SSE stream timeout contract.

Both streams are idle by design between events, so PUMP's 25 s keepalive is the
only traffic on a healthy-but-quiet connection. `read=None` survives that idle —
but it also blocks forever on a half-open connection (PUMP pod rescheduled, a
dropped flow), and the watch loops only reconnect on an *exception*. That wedged
the motor-control stream silently: it stopped applying LOAD/unload and never hit
the unload-on-teardown path. These pin the read timeout finite and configurable
so a dead stream surfaces as a ReadTimeout instead of hanging.
"""

from __future__ import annotations

import httpx
import pytest

from pump_voltra import pump_client
from pump_voltra.pump_client import PumpClient


class _FakeSSE:
    """Stands in for httpx_sse's connection: an async context manager whose
    aiter_sse yields nothing, so the stream method returns immediately."""

    async def __aenter__(self):
        return self

    async def __aexit__(self, *exc):
        return False

    async def aiter_sse(self):
        return
        yield  # pragma: no cover - makes this an async generator


def _capture_timeout(monkeypatch) -> dict:
    """Patch aconnect_sse to record the timeout it is called with."""
    captured: dict = {}

    def fake_aconnect_sse(client, method, url, *, timeout=None, **kw):
        captured["timeout"] = timeout
        captured["url"] = url
        return _FakeSSE()

    monkeypatch.setattr(pump_client, "aconnect_sse", fake_aconnect_sse)
    return captured


async def _drain(agen) -> None:
    async for _ in agen:
        pass


async def test_sets_stream_uses_a_finite_read_timeout(monkeypatch) -> None:
    captured = _capture_timeout(monkeypatch)
    async with PumpClient("http://pump", sse_read_timeout_s=45.0) as c:
        await _drain(c.stream_set_events())
    t: httpx.Timeout = captured["timeout"]
    assert captured["url"] == "/api/sets/stream"
    assert t.read == 45.0, "a None read timeout hangs forever on a half-open stream"


async def test_voltra_stream_uses_a_finite_read_timeout(monkeypatch) -> None:
    captured = _capture_timeout(monkeypatch)
    async with PumpClient("http://pump", sse_read_timeout_s=45.0) as c:
        await _drain(c.stream_voltra_state())
    t: httpx.Timeout = captured["timeout"]
    assert captured["url"] == "/api/voltra/stream"
    assert t.read == 45.0


@pytest.mark.parametrize("stream", ["stream_set_events", "stream_voltra_state"])
async def test_read_timeout_exceeds_the_server_keepalive(monkeypatch, stream) -> None:
    # PUMP's sseLoop sends `: keepalive` every 25 s. The default must sit above
    # that or a healthy idle stream is torn down every cycle.
    captured = _capture_timeout(monkeypatch)
    async with PumpClient("http://pump") as c:  # default sse_read_timeout_s
        await _drain(getattr(c, stream)())
    t: httpx.Timeout = captured["timeout"]
    assert t.read is not None
    assert t.read > 25.0, "read timeout must clear the 25 s keepalive"
