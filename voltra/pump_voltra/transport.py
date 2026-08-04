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
    try:
        import bleak
        from aioesphomeapi import APIClient
        from bleak_esphome import connect_scanner
    except ImportError as e:  # pragma: no cover - exercised only at runtime
        raise TransportError(
            "BLE dependencies are missing; install the package with its "
            f"runtime dependencies to use the proxy transport ({e})"
        ) from e

    if not host:
        raise TransportError("no ESPHome proxy host configured (VOLTRA_PROXY_HOST)")
    if not address:
        raise TransportError("no trainer BLE address configured (VOLTRA_ADDRESS)")

    manager = await _ensure_manager()

    api = APIClient(host, port, password="", noise_psk=psk or None)
    await api.connect(login=True)
    device_info = await api.device_info()

    # connect_scanner is SYNCHRONOUS and returns ESPHomeClientData — awaiting it
    # raises "object ESPHomeClientData can't be used in 'await' expression".
    # It also documents three things the caller must do itself; skip any of them
    # and the proxy connects but never produces a usable BLE client.
    client_data = connect_scanner(api, device_info, True)
    client_data.scanner.async_setup()  # also sync, despite the name
    _unregister_scanner()
    global _unregister
    _unregister = manager.async_register_scanner(client_data.scanner)

    logger.info("esphome proxy connected", host=host, name=device_info.name)

    # Attribute access, NOT `from bleak import BleakClient` — see module docs.
    client = bleak.BleakClient(address, timeout=timeout_s)
    await client.connect()
    logger.info("trainer connected", address=address, mtu=getattr(client, "mtu_size", None))
    return client
