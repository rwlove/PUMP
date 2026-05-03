"""Plate-color barbell weight detector.

Approach: HSV thresholding for the standardised competition plate colors,
connected-components on each color mask, and a weight summation per plate
detected. This is the simple-and-fast version; for the home gym it
should be adequate as long as the plates are visible against the wall
and the bar isn't cluttered with collars.

Standard color → weight (lb) mapping comes from IPF/IWF colors plus the
common 5/2.5 lb microplates:

    red    → 45 (and 25 kg in metric — lb gym is the assumption here)
    blue   → 35 (and 20 kg)
    yellow → 25 (and 15 kg)
    green  → 10 (and 10 kg)
    white  → 5  (and  5 kg)
    black  → 2.5

If the home gym uses different colors the operator can override
PLATE_COLORS at startup. The detector also returns per-plate confidence
so wrong/ambiguous detections become PUMP-side pending sets that the
athlete confirms.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import NamedTuple

import cv2
import numpy as np


class HSVRange(NamedTuple):
    """A pair of HSV bounds (OpenCV uses H ∈ [0, 180], S/V ∈ [0, 255])."""
    lo: tuple[int, int, int]
    hi: tuple[int, int, int]


@dataclass(frozen=True, slots=True)
class PlateColor:
    name: str
    weight_lb: float
    # Some colors (red) wrap around H=0; allow multiple ranges per color.
    ranges: tuple[HSVRange, ...]


# Default mapping. Tunable per gym.
PLATE_COLORS: tuple[PlateColor, ...] = (
    PlateColor("red", 45.0, (
        HSVRange(lo=(0,   120, 80),  hi=(10,  255, 255)),
        HSVRange(lo=(170, 120, 80),  hi=(180, 255, 255)),
    )),
    PlateColor("blue", 35.0, (
        HSVRange(lo=(100, 120, 60),  hi=(130, 255, 255)),
    )),
    PlateColor("yellow", 25.0, (
        HSVRange(lo=(20,  120, 120), hi=(35,  255, 255)),
    )),
    PlateColor("green", 10.0, (
        HSVRange(lo=(40,  60,  60),  hi=(85,  255, 255)),
    )),
    PlateColor("white", 5.0, (
        HSVRange(lo=(0,   0,   200), hi=(180, 30,  255)),
    )),
    PlateColor("black", 2.5, (
        HSVRange(lo=(0,   0,   0),   hi=(180, 80,  60)),
    )),
)


@dataclass(frozen=True, slots=True)
class DetectedPlate:
    color: str
    weight_lb: float
    center_xy: tuple[int, int]
    radius_px: int
    confidence: float  # 0..1 — based on shape circularity and area consistency


def _color_mask(hsv: np.ndarray, color: PlateColor) -> np.ndarray:
    """Combine all HSV ranges for one color into one binary mask."""
    mask = np.zeros(hsv.shape[:2], dtype=np.uint8)
    for r in color.ranges:
        m = cv2.inRange(hsv, np.array(r.lo, dtype=np.uint8),
                        np.array(r.hi, dtype=np.uint8))
        mask = cv2.bitwise_or(mask, m)
    # Clean up: fill small gaps, remove tiny specks.
    kernel = cv2.getStructuringElement(cv2.MORPH_ELLIPSE, (5, 5))
    mask = cv2.morphologyEx(mask, cv2.MORPH_OPEN, kernel)
    mask = cv2.morphologyEx(mask, cv2.MORPH_CLOSE, kernel)
    return mask


def _components_to_plates(mask: np.ndarray, color: PlateColor,
                          min_area: int, max_area: int) -> list[DetectedPlate]:
    """Turn a color mask into DetectedPlate objects using connected-components."""
    n, labels, stats, centroids = cv2.connectedComponentsWithStats(mask, connectivity=8)
    plates: list[DetectedPlate] = []
    for i in range(1, n):  # skip background (label 0)
        x, y, w, h, area = stats[i]
        if area < min_area or area > max_area:
            continue
        cx, cy = centroids[i]
        radius = int((w + h) / 4)
        # Circularity heuristic: ratio of mask area to bounding circle area.
        bounding_area = max(1.0, np.pi * radius * radius)
        circularity = area / bounding_area
        # Penalise very non-circular blobs (bar shaft, banners, walls).
        if circularity < 0.35:
            continue
        plates.append(DetectedPlate(
            color=color.name,
            weight_lb=color.weight_lb,
            center_xy=(int(cx), int(cy)),
            radius_px=radius,
            confidence=min(1.0, circularity * 1.4),
        ))
    return plates


def detect_plates(
    image: np.ndarray,
    palette: tuple[PlateColor, ...] = PLATE_COLORS,
    min_area: int = 200,
    max_area: int = 200_000,
) -> list[DetectedPlate]:
    """Find plates of any palette color in `image` (BGR ndarray).

    Returns a flat list of plates across all colors. Caller is
    responsible for grouping per side of the bar / per loaded sleeve;
    estimate_barbell_load does the simple sum-them-all variant.
    """
    if image is None or image.size == 0:
        return []
    hsv = cv2.cvtColor(image, cv2.COLOR_BGR2HSV)
    plates: list[DetectedPlate] = []
    for color in palette:
        mask = _color_mask(hsv, color)
        plates.extend(_components_to_plates(mask, color, min_area, max_area))
    return plates


def estimate_barbell_load(
    image: np.ndarray,
    bar_weight_lb: float = 45.0,
    palette: tuple[PlateColor, ...] = PLATE_COLORS,
) -> tuple[float, float]:
    """Estimate the loaded bar weight from a single image. Returns
    (total_weight_lb, confidence ∈ [0..1]).

    Phase 1 is naive: sum every detected plate (assuming both sides are
    visible) plus the bar weight. Confidence drops when no plates are
    detected, when same-color stacks are ambiguous (current heuristic:
    1 plate == 1 plate's worth, can't see depth), or when many plates
    of mixed colors clash with typical loadings.

    For better accuracy, Phase 3 will:
      - locate the bar's two ends (YOLO detector)
      - cluster plates per side
      - use the second camera to disambiguate stack depth
    """
    plates = detect_plates(image, palette=palette)
    if not plates:
        return bar_weight_lb, 0.2  # no plates → just the bar; low confidence
    total = bar_weight_lb + sum(p.weight_lb for p in plates)
    # Confidence: average per-plate confidence, scaled down when plate
    # count is uneven (must be even for a bar loaded symmetrically).
    avg_conf = sum(p.confidence for p in plates) / len(plates)
    asymmetry_penalty = 0.6 if len(plates) % 2 == 1 else 1.0
    return total, max(0.0, min(1.0, avg_conf * asymmetry_penalty))
