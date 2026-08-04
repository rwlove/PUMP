"""Structured logging, mirroring cv/pump_cv/log.py."""

from __future__ import annotations

import logging
import os
import sys

import structlog

_configured = False


def configure(level: str | None = None) -> None:
    global _configured
    if _configured:
        return
    lvl = (level or os.getenv("LOG_LEVEL", "INFO")).upper()
    numeric = logging.getLevelName(lvl)
    if not isinstance(numeric, int):
        numeric = logging.INFO
    logging.basicConfig(format="%(message)s", stream=sys.stdout, level=numeric)
    structlog.configure(
        processors=[
            structlog.contextvars.merge_contextvars,
            structlog.processors.add_log_level,
            structlog.processors.TimeStamper(fmt="iso", utc=True),
            structlog.dev.ConsoleRenderer(colors=sys.stdout.isatty()),
        ],
        wrapper_class=structlog.make_filtering_bound_logger(numeric),
        cache_logger_on_first_use=True,
    )
    _configured = True


def get(name: str) -> structlog.stdlib.BoundLogger:
    configure()
    return structlog.get_logger(name)
