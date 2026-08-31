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

# Reconnect backoff bounds. The ceiling is deliberately low enough that walking
# into the gym and switching the trainer on is picked up without a restart.
MIN_BACKOFF_S = 15
MAX_BACKOFF_S = 120


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
    from .control import Controller
    from .motor import MotorController
    from .transport import TrainerNotAdvertising, connect_proxy, connect_trainer

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

    # Back off on repeated failure. A fixed 15 s retry means ~240 connection
    # attempts an hour against a small ESP32, which is enough to matter when
    # the trainer is simply switched off — the normal state of an empty gym.
    backoff = MIN_BACKOFF_S

    # The proxy session is long-lived and deliberately survives the trainer
    # being absent. The device permits ONE advertisement subscriber and
    # silently refuses a new one while a prior connection still holds the slot,
    # so reconnecting every time the gym is empty means the scanner mostly
    # never hears anything.
    link = None

    try:
        while True:
            # Progress tick for the liveness watchdog. Every path through this
            # loop — connect, wait-for-trainer, back off, reconnect — passes
            # here, so a frozen heartbeat means the loop itself has wedged
            # (a hung await), which is the failure a plain scrape can't see.
            healthd.record_heartbeat()

            # Proxy and trainer failures are handled separately on purpose. The
            # proxy session owns the ESP32's single advertisement subscription,
            # and surrendering it is what makes the trainer look permanently
            # absent — so only a proxy-level failure may tear it down.
            # A live link's on_stop callback clears its `alive` flag the moment
            # the proxy TCP session drops. Rebuilding on a dead link (not only a
            # None one) is what makes a proxy EOF self-heal in seconds instead
            # of leaving a registered-but-deaf scanner running until a manual
            # pod restart — the trainer looks permanently absent the whole time.
            if link is not None and not link.is_alive:
                logger.warning("esphome proxy link is dead; rebuilding")
                with contextlib.suppress(Exception):
                    await link.close()
                link = None
            try:
                if link is None:
                    link = await connect_proxy(cfg.proxy.host, cfg.proxy.port, cfg.proxy.psk)
            except Exception as e:
                logger.warning("proxy connect failed; retrying",
                               error=str(e), retry_in_s=backoff)
                healthd.record_connected(False)
                link = None
                await asyncio.sleep(backoff)
                backoff = min(backoff * 2, MAX_BACKOFF_S)
                continue

            try:
                await connect_trainer(link, cfg.address, cfg.discovery_timeout_seconds)
                backoff = MIN_BACKOFF_S
            except TrainerNotAdvertising as e:
                # An empty gym is the normal case, not a fault. Keep the proxy
                # session — and its advertisement subscription — open.
                logger.info("waiting for the trainer", detail=str(e))
                healthd.record_connected(False)
                await asyncio.sleep(MIN_BACKOFF_S)
                continue
            except Exception as e:
                # A transient BLE failure (establish_connection exhausting its
                # attempts, say) is not a reason to drop the proxy session.
                logger.warning("trainer connect failed; retrying",
                               error=str(e), retry_in_s=backoff)
                healthd.record_connected(False)
                with contextlib.suppress(Exception):
                    await link.close_trainer()
                await asyncio.sleep(backoff)
                backoff = min(backoff * 2, MAX_BACKOFF_S)
                continue

            client = VoltraClient(link.client, on_telemetry)
            motor = MotorController(client, cfg.max_load_lb)
            controller = Controller(pump, motor, cfg.max_load_lb)
            runner.attach_controller(controller)
            # Phase 2 tasks live and die with the BLE session: a controller
            # holding a stale client would reconcile onto a dead connection.
            session: list[asyncio.Task] = []
            try:
                await client.start()
                await client.subscribe_telemetry()

                # Only now start the reconciler. Started any earlier, the first
                # await inside the bootstrap yields to watch(), which can call
                # motor.load() and interleave GATT writes with the handshake
                # frames — corrupting the handshake and driving the motor
                # before the device is configured.
                session = [
                    asyncio.create_task(controller.watch()),
                    asyncio.create_task(controller.heartbeat()),
                ]
                healthd.record_connected(True)
                healthd.record_workout_active(True)
                tracker.reset_workout()

                loop = asyncio.get_running_loop()
                poll = asyncio.create_task(
                    _poll_load(client, tracker, cfg.load_poll_seconds, cfg.max_load_lb)
                )

                async def _consume(_loop=loop) -> None:
                    while True:
                        payload = await queue.get()
                        await runner.feed(payload, _loop.time())

                consume = asyncio.create_task(_consume())
                # A session task dying must tear the session down, not be
                # ignored. watch()/heartbeat()/poll are bare tasks; before this,
                # only the consumer loop's own exit reached the finally: unload()
                # below. So if watch() died (its reconcile stops) or heartbeat()
                # died while the motor keepalive task kept the cable tensioned,
                # nothing unloaded and the UI could show slack over a live cable.
                # Treat ANY of these finishing as session death → fall through to
                # teardown, which cancels the keepalive and unloads.
                supervised = [*session, poll, consume]
                try:
                    done, _ = await asyncio.wait(
                        supervised, return_when=asyncio.FIRST_COMPLETED
                    )
                    # Retrieve every finished task's exception — both to log the
                    # cause and to avoid asyncio's "exception was never
                    # retrieved" warning at GC. WorkoutInactive is not raised
                    # here (only from client.start()/subscribe_telemetry above,
                    # which still reach the except below), so there is no
                    # backoff path to preserve — any finish means session death.
                    for t in done:
                        exc = None if t.cancelled() else t.exception()
                        which = "consumer loop" if t is consume else "session task"
                        logger.warning(
                            f"{which} ended; tearing down the session",
                            error=str(exc) if exc else "returned",
                            error_type=type(exc).__name__ if exc else "clean",
                        )
                finally:
                    poll.cancel()
                    consume.cancel()
                    for t in (poll, consume):
                        with contextlib.suppress(asyncio.CancelledError, Exception):
                            await t
            except WorkoutInactive as e:
                # Normal when nobody is training. Back off and look again.
                logger.info("waiting for a workout to start on the trainer", detail=str(e))
                healthd.record_workout_active(False)
                await asyncio.sleep(30)
            except Exception as e:
                logger.warning("session ended; reconnecting", error=str(e))
            finally:
                healthd.record_connected(False)
                # Release before anything else. Every exit from a session —
                # error, workout ended, shutdown — must leave the cable slack,
                # and stopping the keepalive is what does it.
                for t in session:
                    t.cancel()
                for t in session:
                    with contextlib.suppress(asyncio.CancelledError, Exception):
                        await t
                with contextlib.suppress(Exception):
                    await motor.unload()
                runner.attach_controller(None)
                # Only the BLE half. The proxy session stays up so its single
                # advertisement subscription is never surrendered — losing it
                # is what makes the trainer look permanently absent.
                with contextlib.suppress(Exception):
                    await link.close_trainer()
    finally:
        if link is not None:
            with contextlib.suppress(Exception):
                await link.close()
        for task in background:
            task.cancel()


async def _poll_load(
    client, tracker: SetTracker, interval_s: float, max_load_lb: int
) -> None:
    """Keep the tracker's idea of the resistance current.

    Doubles as the in-session heartbeat: it runs every interval_s for the life
    of a connected session, including while the athlete rests between sets and
    the telemetry queue is idle, so the liveness watchdog stays fresh without
    mistaking a legitimate lull for a wedge.
    """
    while True:
        try:
            load = await client.target_load()
        except asyncio.CancelledError:
            raise
        except Exception as e:
            # A failed read does NOT stamp the heartbeat: stamping before the
            # read (as this used to) kept the liveness watchdog green while the
            # in-session BLE link was dead — a GATT gone half-open reads-fail
            # every cycle, yet the loop kept spinning and the watchdog never
            # fired. Only a successful read proves the link is alive.
            logger.debug("target load read failed", error=str(e))
            await asyncio.sleep(interval_s)
            continue

        healthd.record_heartbeat()
        if load is not None:
            if 0 < load <= max_load_lb:
                tracker.note_weight(load)
            else:
                # The MAX_LOAD clamp guards what we WRITE to the motor; the
                # recorded set weight came straight off the device with no
                # bound. A CRC-valid frame carrying a garbage value (a decode
                # desync, a firmware glitch reading the 16-bit field high) must
                # not be logged as the set weight.
                logger.warning("ignoring out-of-range polled load", load=load,
                               max_load_lb=max_load_lb)
        await asyncio.sleep(interval_s)


async def _amain(args: argparse.Namespace) -> int:
    cfg = config.load(args.config)
    log.configure()
    healthd.set_heartbeat_stale_after(cfg.heartbeat_stale_seconds)

    if args.replay:
        async with PumpClient(
            cfg.pump.base_url, cfg.pump.api_key, cfg.pump.request_timeout_s,
            cfg.pump.sse_read_timeout_s,
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

    rc = 0
    async with PumpClient(
        cfg.pump.base_url, cfg.pump.api_key, cfg.pump.request_timeout_s,
        cfg.pump.sse_read_timeout_s,
    ) as pump:
        namer = ExerciseNamer(cfg.default_exercise)
        work = asyncio.create_task(_run_live(cfg, pump, namer)) if cfg.enabled else None

        if work is not None:
            # Exit non-zero if the work loop dies on its own. _run_live is meant
            # to run forever; if it returns or raises, the sidecar is a zombie
            # serving probes with no BLE activity. Letting the process exit lets
            # the kubelet restart it instead — the same recovery the liveness
            # watchdog gives, for the case where the task dies outright rather
            # than hanging.
            done, _ = await asyncio.wait(
                {work, asyncio.ensure_future(stop.wait())},
                return_when=asyncio.FIRST_COMPLETED,
            )
            if work in done and not stop.is_set():
                rc = 1
                exc = work.exception()
                logger.error("work loop exited; restarting process",
                             error=str(exc) if exc else "returned")
        else:
            await stop.wait()

        logger.info("shutting down")
        for task in (work, probes):
            if task is not None:
                task.cancel()
                with contextlib.suppress(asyncio.CancelledError, Exception):
                    await task
    return rc


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
