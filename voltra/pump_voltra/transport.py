"""BLE transport over an ESPHome bluetooth_proxy.

The sidecar runs in-cluster; the trainer is in the gym. An ESP32 running
`bluetooth_proxy` with **active** connections bridges gym-BLE to LAN-TCP.

⚠ The monkeypatch trap. `habluetooth.BluetoothManager().async_setup()`
replaces the `BleakClient` symbol *inside the bleak module* so that clients
route through the proxy. A module-level `from bleak import BleakClient` binds
the original class before that happens and silently bypasses the proxy — the
connection then fails with a plain "device not found" from the local adapter,
with nothing in the traceback pointing at the cause. Always go through
`bleak.BleakClient` attribute access, as below.

Negotiated MTU over this path is 517, not the 23 that ESP32 folklore claims.
"""

from __future__ import annotations

import asyncio
from dataclasses import dataclass

from .log import get

logger = get(__name__)


class TransportError(RuntimeError):
    pass


# habluetooth's manager is a process-global singleton (set_manager/get_manager),
# and connect_scanner reaches for it internally. Build it once and reuse it —
# a fresh manager per reconnect would orphan the previous one's state.
_manager = None
_unregister = None


async def _ensure_manager():
    """Create, set up and install the process-wide habluetooth manager."""
    global _manager
    if _manager is not None:
        return _manager

    import habluetooth

    class _Manager(habluetooth.BluetoothManager):
        """Discovery-free manager.

        The base class refuses to consume advertisement data unless a subclass
        implements this, and logs "does not implement _discover_service_info"
        on every connect if you don't. We address the trainer by MAC and never
        browse discovery results, so discarding them is correct rather than
        merely tolerable.
        """

        def _discover_service_info(self, service_info) -> None:
            return None

    _manager = _Manager()
    await _manager.async_setup()
    habluetooth.set_manager(_manager)
    return _manager


class TrainerNotAdvertising(TransportError):
    """The proxy is up but has not heard the trainer.

    Distinct from a connect failure: nothing is broken, the trainer is just
    switched off or asleep. The caller should back off and look again rather
    than treat it as an error worth alarming about.
    """


async def _await_discovery(manager, address: str, timeout_s: float):
    """Wait until a registered scanner has actually heard `address` advertise.

    Returns the BLEDevice. Raises TrainerNotAdvertising on timeout.
    """
    deadline = asyncio.get_running_loop().time() + timeout_s
    while True:
        devices = manager.async_scanner_devices_by_address(address, True)
        if devices:
            return devices[0].device
        if asyncio.get_running_loop().time() >= deadline:
            raise TrainerNotAdvertising(
                f"no scanner has seen {address} in {timeout_s:.0f}s — "
                "the trainer is probably switched off or asleep"
            )
        await asyncio.sleep(1.0)


async def _close_api(api) -> None:
    """Disconnect an APIClient, swallowing teardown errors."""
    try:
        await api.disconnect()
    except Exception as e:  # pragma: no cover - defensive
        logger.debug("api disconnect failed", error=str(e))


@dataclass
class ProxyLink:
    """A live path to the trainer: the BLE client plus the proxy session behind it.

    Both halves are returned together because both have to be torn down. Earlier
    this function returned only the BleakClient, so every reconnect stranded an
    APIClient on the ESP32.
    """

    client: object
    api: object

    async def close(self) -> None:
        try:
            await self.client.disconnect()
        except Exception as e:  # pragma: no cover - defensive
            logger.debug("ble disconnect failed", error=str(e))
        _unregister_scanner()
        await _close_api(self.api)


def _unregister_scanner() -> None:
    """Drop the previous connection's scanner, if any.

    Reconnects would otherwise stack a new scanner onto the manager on every
    retry — and a retry loop against an offline proxy runs all day.
    """
    global _unregister
    if _unregister is None:
        return
    try:
        _unregister()
    except Exception as e:  # pragma: no cover - defensive
        logger.debug("scanner unregister failed", error=str(e))
    finally:
        _unregister = None


async def connect(address: str, host: str, port: int, psk: str, timeout_s: float = 30.0):
    """Connect to the trainer through the ESPHome proxy. Returns a BleakClient.

    Imports are deliberately function-local: the BLE stack is a heavy
    dependency that CI does not install, and protocol/session/naming must stay
    importable without it.
    """
    # Config guards first: a missing host is a misconfiguration whether or not
    # the BLE stack happens to be installed, and saying so beats reporting a
    # dependency error that sends you looking in the wrong place.
    if not host:
        raise TransportError("no ESPHome proxy host configured (VOLTRA_PROXY_HOST)")
    if not address:
        raise TransportError("no trainer BLE address configured (VOLTRA_ADDRESS)")

    try:
        import bleak
        from aioesphomeapi import APIClient
        from bleak_esphome import connect_scanner
        from bleak_retry_connector import establish_connection
    except ImportError as e:  # pragma: no cover - exercised only at runtime
        raise TransportError(
            "BLE dependencies are missing; install the package with its "
            f"runtime dependencies to use the proxy transport ({e})"
        ) from e

    manager = await _ensure_manager()

    api = APIClient(host, port, password="", noise_psk=psk or None)
    # Everything from here on must hand the APIClient back to the caller or
    # close it. The ESP32 accepts only a handful of concurrent API connections;
    # leaking one per failed attempt exhausts them within hours, after which it
    # drops new clients right after the encrypted hello — which reads like a
    # wrong key and sends you chasing the wrong bug entirely.
    try:
        await api.connect(login=True)
        device_info = await api.device_info()

        # connect_scanner is SYNCHRONOUS and returns ESPHomeClientData —
        # awaiting it raises "object ESPHomeClientData can't be used in 'await'
        # expression". It also documents three things the caller must do
        # itself; skip any of them and the proxy connects but never produces a
        # usable BLE client.
        client_data = connect_scanner(api, device_info, True)
        client_data.scanner.async_setup()  # also sync, despite the name
        _unregister_scanner()
        global _unregister
        _unregister = manager.async_register_scanner(client_data.scanner)

        logger.info("esphome proxy connected", host=host, name=device_info.name)

        # A scanner that has just been registered has not heard anything yet.
        # BLE devices are only reachable once they advertise, and connecting
        # immediately fails with "never seen by any scanner" even when the
        # trainer is switched on and healthy.
        device = await _await_discovery(manager, address, timeout_s)

        # establish_connection wants a BLEDevice, not an address, and handles
        # the retry/backoff that bleak's own connect() warns about omitting.
        # Attribute access on bleak, NOT `from bleak import BleakClient` — see
        # the module docstring.
        client = await establish_connection(
            bleak.BleakClient, device, address, max_attempts=3
        )
        logger.info(
            "trainer connected", address=address, mtu=getattr(client, "mtu_size", None)
        )
        return ProxyLink(client=client, api=api)
    except BaseException:
        # Includes CancelledError: a shutdown mid-connect must not strand an
        # API connection on the device either.
        await _close_api(api)
        _unregister_scanner()
        raise
