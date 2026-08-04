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
        from habluetooth import BluetoothManager
    except ImportError as e:  # pragma: no cover - exercised only at runtime
        raise TransportError(
            "BLE dependencies are missing; install the package with its "
            f"runtime dependencies to use the proxy transport ({e})"
        ) from e

    if not host:
        raise TransportError("no ESPHome proxy host configured (VOLTRA_PROXY_HOST)")
    if not address:
        raise TransportError("no trainer BLE address configured (VOLTRA_ADDRESS)")

    manager = BluetoothManager()
    await manager.async_setup()

    api = APIClient(host, port, password="", noise_psk=psk or None)
    await api.connect(login=True)
    device_info = await api.device_info()
    await connect_scanner(api, device_info, True)
    logger.info("esphome proxy connected", host=host, name=device_info.name)

    # Attribute access, NOT `from bleak import BleakClient` — see module docs.
    client = bleak.BleakClient(address, timeout=timeout_s)
    await client.connect()
    logger.info("trainer connected", address=address, mtu=getattr(client, "mtu_size", None))
    return client
