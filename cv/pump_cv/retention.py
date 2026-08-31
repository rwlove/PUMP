"""Background retention sweep for snapshots and clips.

Runs once at startup and then every `interval_hours`. Deletes any file
under the configured directories whose mtime is older than
`max_age_days`. Empty parent dirs are removed too so a YYYY-MM-DD
folder doesn't linger after its last file is swept.
"""

from __future__ import annotations

import asyncio
import time
from pathlib import Path

from . import log

logger = log.get(__name__)


def _sweep_dir(root: Path, max_age_days: float, max_bytes: float = 0.0) -> int:
    if not root.exists():
        return 0
    cutoff = time.time() - (max_age_days * 86400)
    deleted = 0
    failures = 0
    for f in list(root.rglob("*")):
        if f.is_file() and f.stat().st_mtime < cutoff:
            try:
                f.unlink()
                deleted += 1
            except OSError:
                # Was silently swallowed — a permission/FD problem that stops
                # all deletion should be visible, not invisible.
                failures += 1

    # Size cap: after the age sweep, if the directory is still over the byte
    # ceiling, delete oldest-first until under it. This is the floor the
    # age-only sweep lacked against a small PVC filling inside the age window.
    if max_bytes > 0:
        files = []
        total = 0
        for f in root.rglob("*"):
            if f.is_file():
                try:
                    st = f.stat()
                except OSError:
                    continue
                files.append((st.st_mtime, st.st_size, f))
                total += st.st_size
        if total > max_bytes:
            for _mtime, size, f in sorted(files):  # oldest first
                if total <= max_bytes:
                    break
                try:
                    f.unlink()
                    deleted += 1
                    total -= size
                except OSError:
                    failures += 1

    if failures:
        logger.warning("retention: some deletions failed", root=str(root),
                       failures=failures)
    # Prune now-empty subdirs (keep the root itself).
    for d in sorted([p for p in root.rglob("*") if p.is_dir()],
                    key=lambda p: -len(p.parts)):
        try:
            d.rmdir()
        except OSError:
            pass
    return deleted


async def run_forever(
    snapshot_dir: Path | None,
    clips_dir: Path | None,
    max_age_days: float,
    interval_hours: float = 6.0,
    max_bytes: float = 0.0,
) -> None:
    while True:
        for label, root in (("snapshots", snapshot_dir), ("clips", clips_dir)):
            if root is None:
                continue
            try:
                n = _sweep_dir(root, max_age_days, max_bytes)
                if n:
                    logger.info("retention: swept", kind=label, deleted=n,
                                older_than_days=max_age_days)
            except Exception as e:
                logger.warning("retention: sweep failed", kind=label, error=str(e))
        await asyncio.sleep(interval_hours * 3600)
