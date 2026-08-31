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
    await transport.ProxyLink(api=api, manager=None, client=client).close()
    assert client.disconnected, "BLE client left connected"
    assert api.disconnected, "APIClient left open on the ESP32 — this is the leak"


async def test_close_trainer_keeps_the_proxy_session() -> None:
    """The device allows ONE advertisement subscriber and silently refuses a
    replacement while the old connection holds the slot. Dropping the proxy
    every time the trainer is absent is what makes it look permanently gone."""
    client, api = FakeClient(), FakeApi()
    link = transport.ProxyLink(api=api, manager=None, client=client)
    await link.close_trainer()
    assert client.disconnected
    assert not api.disconnected, "proxy session must survive the trainer going away"
    assert link.client is None


async def test_close_trainer_is_idempotent() -> None:
    link = transport.ProxyLink(api=FakeApi(), manager=None, client=None)
    await link.close_trainer()  # must not raise


# ─── proxy liveness ──────────────────────────────────────────────────────
#
# The proxy TCP session can drop with the ProxyLink object none the wiser —
# an ESP32 reboot, a Wi-Fi blip, an idle EOF. The registered scanner is then
# dead and hears no advertisements, so the trainer looks permanently absent.
# The supervisor rebuilds on `not link.is_alive`; once that reused a corpse for
# 19 h because nothing ever flipped the flag, and only a pod restart cleared it.


async def test_proxylink_starts_alive() -> None:
    link = transport.ProxyLink(api=FakeApi(), manager=None)
    assert link.is_alive, "a freshly connected proxy must report alive"


async def test_proxylink_dies_when_on_stop_fires() -> None:
    # connect_proxy hands aioesphomeapi an on_stop callback that clears exactly
    # this Event. Simulate the disconnect: is_alive must flip so the supervisor
    # tears the link down and reconnects instead of reusing a deaf scanner.
    link = transport.ProxyLink(api=FakeApi(), manager=None)
    link.alive.clear()
    assert not link.is_alive, "a dropped proxy must report dead so it gets rebuilt"


async def test_proxylink_survives_trainer_teardown() -> None:
    # Dropping the BLE half (an absent trainer) must NOT mark the proxy dead —
    # that is the normal empty-gym path and the proxy session is long-lived.
    link = transport.ProxyLink(api=FakeApi(), manager=None, client=FakeClient())
    await link.close_trainer()
    assert link.is_alive, "an absent trainer must not look like a dead proxy"


async def test_api_is_closed_even_if_ble_teardown_raises() -> None:
    # The API connection is the scarce resource; a failure disconnecting BLE
    # must not skip it.
    client, api = FakeClient(fail=True), FakeApi()
    await transport.ProxyLink(api=api, manager=None, client=client).close()
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


async def test_connect_proxy_requires_a_host() -> None:
    # Cheap guards, but they turn a silent hang into a clear message.
    with pytest.raises(transport.TransportError, match="VOLTRA_PROXY_HOST"):
        await transport.connect_proxy("", 6053, "psk")


async def test_connect_trainer_requires_an_address() -> None:
    with pytest.raises(transport.TransportError, match="VOLTRA_ADDRESS"):
        await transport.connect_trainer(object(), "", 30)


# ─── discovery gate ──────────────────────────────────────────────────────


class FakeScannerDevice:
    """Mirrors habluetooth's BluetoothScannerDevice, whose field is
    `ble_device` — not `device`. Using the wrong name failed only at the
    moment discovery finally succeeded, which on a trainer that advertises
    briefly then sleeps meant it burned the entire window."""

    def __init__(self, device):
        self.ble_device = device


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
