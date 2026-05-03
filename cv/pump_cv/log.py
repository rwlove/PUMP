"""Structured logging setup. Mirrors PUMP's structured Go logger so the
sidecar's logs sit naturally next to PUMP's in any aggregator.
"""

from __future__ import annotations

import logging
import os
import sys

import structlog


def configure(level: str | None = None) -> None:
    """Initialise structlog + stdlib logging for the process."""
    level_str = (level or os.getenv("LOG_LEVEL") or "info").upper()
    log_level = getattr(logging, level_str, logging.INFO)

    logging.basicConfig(
        format="%(message)s",
        stream=sys.stdout,
        level=log_level,
    )

    structlog.configure(
        wrapper_class=structlog.make_filtering_bound_logger(log_level),
        processors=[
            structlog.processors.add_log_level,
            structlog.processors.TimeStamper(fmt="iso"),
            structlog.processors.StackInfoRenderer(),
            structlog.processors.format_exc_info,
            structlog.processors.JSONRenderer(),
        ],
        cache_logger_on_first_use=True,
    )


def get(name: str | None = None) -> structlog.stdlib.BoundLogger:
    return structlog.get_logger(name)
