"""Entrypoint.

Two modes:

  pump-voltra              connect to the trainer and auto-log sets
  pump-voltra --replay F   feed a captured telemetry file into PUMP instead

Replay exists so the whole path — decode, set tracking, naming, HTTP — can be
verified against a real PUMP without a trainer in the room.
"""

from __future__ import annotations

import argparse
import asyncio
import contextlib
import signal

from . import config, healthd, log
from .naming import ExerciseNamer
from .pump_client import PumpClient
from .runner import Runner, replay
from .session import SetTracker

logger = log.get(__name__)


async def _serve_probes(cfg) -> asyncio.Task | None:
    try:
        import uvicorn
    except ImportError:
        logger.warning("uvicorn not installed; probe endpoints disabled")
        return None
    server = uvicorn.Server(
        uvicorn.Config(
            healthd.build_app(),
            host=cfg.healthd_host,
            port=cfg.healthd_port,
            log_level="warning",
        )
    )
    return asyncio.create_task(server.serve())


async def _run_live(cfg, pump: PumpClient, namer: ExerciseNamer) -> None:
    from .client import VoltraClient, WorkoutInactive
    from .transport import connect

    tracker = SetTracker(idle_seconds=cfg.set_idle_seconds)
    runner = Runner(pump, namer, tracker)

    await runner.refresh_exercises()
    await runner.seed_anchor()

    background = [
        asyncio.create_task(runner.watch_sets()),
        asyncio.create_task(runner.refresh_exercises_forever(cfg.exercise_refresh_seconds)),
        asyncio.create_task(runner.tick_forever()),
    ]

    queue: asyncio.Queue[bytes] = asyncio.Queue(maxsize=512)

    def on_telemetry(payload: bytes) -> None:
        # Called from the BLE callback; never block it.
        with contextlib.suppress(asyncio.QueueFull):
            queue.put_nowait(payload)

    try:
        while True:
            try:
                ble = await connect(
                    cfg.address, cfg.proxy.host, cfg.proxy.port, cfg.proxy.psk
                )
            except Exception as e:
                logger.warning("connect failed; retrying", error=str(e))
                healthd.record_connected(False)
                await asyncio.sleep(15)
                continue

            client = VoltraClient(ble, on_telemetry)
            try:
                await client.start()
                await client.subscribe_telemetry()
                healthd.record_connected(True)
                healthd.record_workout_active(True)
                tracker.reset_workout()

                loop = asyncio.get_running_loop()
                poll = asyncio.create_task(_poll_load(client, tracker, cfg.load_poll_seconds))
                try:
                    while True:
                        payload = await queue.get()
                        await runner.feed(payload, loop.time())
                finally:
                    poll.cancel()
                    with contextlib.suppress(asyncio.CancelledError):
                        await poll
            except WorkoutInactive as e:
                # Normal when nobody is training. Back off and look again.
                logger.info("waiting for a workout to start on the trainer", detail=str(e))
                healthd.record_workout_active(False)
                await asyncio.sleep(30)
            except Exception as e:
                logger.warning("session ended; reconnecting", error=str(e))
            finally:
                healthd.record_connected(False)
                with contextlib.suppress(Exception):
                    await ble.disconnect()
    finally:
        for task in background:
            task.cancel()


async def _poll_load(client, tracker: SetTracker, interval_s: float) -> None:
    """Keep the tracker's idea of the resistance current."""
    while True:
        try:
            if (load := await client.target_load()) is not None:
                tracker.note_weight(load)
        except asyncio.CancelledError:
            raise
        except Exception as e:
            logger.debug("target load read failed", error=str(e))
        await asyncio.sleep(interval_s)


async def _amain(args: argparse.Namespace) -> int:
    cfg = config.load(args.config)
    log.configure()

    if args.replay:
        async with PumpClient(
            cfg.pump.base_url, cfg.pump.api_key, cfg.pump.request_timeout_s
        ) as pump:
            namer = ExerciseNamer(cfg.default_exercise)
            posted = await replay(args.replay, pump, namer, args.weight)
            logger.info("replay complete", sets_posted=posted, source=args.replay)
            return 0 if posted else 1

    if not cfg.enabled:
        logger.info("VOLTRA_ENABLED is false — no BLE activity; idling")

    probes = await _serve_probes(cfg)
    stop = asyncio.Event()
    loop = asyncio.get_running_loop()
    for sig in (signal.SIGINT, signal.SIGTERM):
        with contextlib.suppress(NotImplementedError):
            loop.add_signal_handler(sig, stop.set)

    async with PumpClient(cfg.pump.base_url, cfg.pump.api_key, cfg.pump.request_timeout_s) as pump:
        namer = ExerciseNamer(cfg.default_exercise)
        work = asyncio.create_task(_run_live(cfg, pump, namer)) if cfg.enabled else None
        await stop.wait()
        logger.info("shutting down")
        for task in (work, probes):
            if task is not None:
                task.cancel()
                with contextlib.suppress(asyncio.CancelledError, Exception):
                    await task
    return 0


def run() -> None:
    parser = argparse.ArgumentParser(prog="pump-voltra")
    parser.add_argument("--config", help="path to a YAML config file")
    parser.add_argument(
        "--replay",
        metavar="FILE",
        help="replay a captured telemetry file into PUMP instead of connecting",
    )
    parser.add_argument(
        "--weight",
        type=int,
        default=5,
        help="resistance in lb to attribute to replayed sets (default: 5)",
    )
    args = parser.parse_args()
    raise SystemExit(asyncio.run(_amain(args)))


if __name__ == "__main__":
    run()
