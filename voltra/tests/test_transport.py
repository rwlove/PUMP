"""Transport teardown contract.

The BLE stack is not installed in CI, so these exercise the parts that are
pure: that a ProxyLink tears down *both* halves, and that helpers swallow
teardown errors rather than masking the original failure.

The bug these pin down: connect() used to return only the BleakClient, so every
reconnect stranded an APIClient on the ESP32. It accepts a handful of
concurrent API connections; leaking one per 15 s retry exhausts them within
hours, after which it drops new clients right after the encrypted hello — which
looks exactly like a wrong PSK.
"""

from __future__ import annotations

import pytest

from pump_voltra import transport


class FakeClient:
    def __init__(self, fail: bool = False):
        self.disconnected = False
        self._fail = fail

    async def disconnect(self):
        if self._fail:
            raise RuntimeError("ble teardown blew up")
        self.disconnected = True


class FakeApi:
    def __init__(self, fail: bool = False):
        self.disconnected = False
        self._fail = fail

    async def disconnect(self):
        if self._fail:
            raise RuntimeError("api teardown blew up")
        self.disconnected = True


async def test_close_tears_down_both_halves() -> None:
    client, api = FakeClient(), FakeApi()
    await transport.ProxyLink(client=client, api=api).close()
    assert client.disconnected, "BLE client left connected"
    assert api.disconnected, "APIClient left open on the ESP32 — this is the leak"


async def test_api_is_closed_even_if_ble_teardown_raises() -> None:
    # The API connection is the scarce resource; a failure disconnecting BLE
    # must not skip it.
    client, api = FakeClient(fail=True), FakeApi()
    await transport.ProxyLink(client=client, api=api).close()
    assert api.disconnected


async def test_close_api_swallows_errors() -> None:
    await transport._close_api(FakeApi(fail=True))  # must not raise


async def test_unregister_scanner_is_safe_when_never_registered() -> None:
    transport._unregister = None
    transport._unregister_scanner()  # must not raise


async def test_unregister_scanner_clears_state_even_on_error() -> None:
    def boom():
        raise RuntimeError("nope")

    transport._unregister = boom
    transport._unregister_scanner()
    assert transport._unregister is None, "a failed unregister must not pin the old scanner"


def test_connect_requires_host_and_address() -> None:
    # Cheap guards, but they turn a silent hang into a clear message.
    import asyncio

    with pytest.raises(transport.TransportError, match="VOLTRA_PROXY_HOST"):
        asyncio.run(transport.connect("aa:bb", "", 6053, "psk"))
    with pytest.raises(transport.TransportError, match="VOLTRA_ADDRESS"):
        asyncio.run(transport.connect("", "host", 6053, "psk"))


# ─── discovery gate ──────────────────────────────────────────────────────


class FakeScannerDevice:
    def __init__(self, device):
        self.device = device


class FakeManager:
    """Reports the address as unseen for `misses` calls, then finds it."""

    def __init__(self, misses: int):
        self.misses = misses
        self.calls = 0

    def async_scanner_devices_by_address(self, address, connectable):
        self.calls += 1
        if self.calls <= self.misses:
            return []
        return [FakeScannerDevice(f"BLEDevice({address})")]


async def test_discovery_waits_for_the_first_advertisement() -> None:
    # A freshly registered scanner has heard nothing yet; connecting straight
    # away is what produced "never seen by any scanner" against a healthy,
    # switched-on trainer.
    mgr = FakeManager(misses=2)
    device = await transport._await_discovery(mgr, "AA:BB:CC:DD:EE:FF", timeout_s=30)
    assert device == "BLEDevice(AA:BB:CC:DD:EE:FF)"
    assert mgr.calls == 3


async def test_discovery_times_out_as_not_advertising() -> None:
    # Must be its own type: an idle gym is expected, not a fault to alarm on.
    mgr = FakeManager(misses=10_000)
    with pytest.raises(transport.TrainerNotAdvertising, match="switched off or asleep"):
        await transport._await_discovery(mgr, "AA:BB:CC:DD:EE:FF", timeout_s=0)


def test_not_advertising_is_a_transport_error() -> None:
    assert issubclass(transport.TrainerNotAdvertising, transport.TransportError)
