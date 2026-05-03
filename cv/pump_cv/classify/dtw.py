"""Dynamic Time Warping distance between two multivariate time-series.

Used by the exercise classifier to compare a live window of pose
features against pre-recorded reference prototypes. Each time-series
is shape (T, D) — T frames, D feature dimensions per frame.

Implementation notes:
- Uses a Sakoe-Chiba band (window radius) to bound the search and rule
  out crazy alignments. Default radius ≈ 10% of the longer sequence.
- Pure NumPy; vectorised over the inner dimension D.
- Returns the un-normalised path cost. Caller divides by path length
  if a length-normalised score is desired.

A C-extension would be faster, but for prototype matching against ~10
reference clips this Python implementation is more than fast enough.
"""

from __future__ import annotations

import numpy as np


def dtw_distance(
    a: np.ndarray, b: np.ndarray,
    band: float | None = 0.1,
) -> float:
    """Compute DTW distance between two (T, D) sequences.

    Distance metric is squared L2 between feature vectors at each frame
    pair. `band` is the Sakoe-Chiba band as a fraction of the longer
    sequence length; pass None to disable banding.
    """
    if a.ndim != 2 or b.ndim != 2:
        raise ValueError("dtw_distance: inputs must be 2D (T, D)")
    if a.shape[1] != b.shape[1]:
        raise ValueError(f"dtw_distance: feature dims differ {a.shape[1]} vs {b.shape[1]}")

    n, m = a.shape[0], b.shape[0]
    if n == 0 or m == 0:
        return float("inf")

    if band is not None:
        radius = max(1, int(band * max(n, m)))
    else:
        radius = max(n, m)

    INF = float("inf")
    cost = np.full((n + 1, m + 1), INF)
    cost[0, 0] = 0.0

    for i in range(1, n + 1):
        j_lo = max(1, i - radius)
        j_hi = min(m, i + radius) + 1
        ai = a[i - 1]
        for j in range(j_lo, j_hi):
            d = ai - b[j - 1]
            d = float(d @ d)  # squared L2
            cost[i, j] = d + min(cost[i - 1, j], cost[i, j - 1], cost[i - 1, j - 1])

    return float(cost[n, m])
