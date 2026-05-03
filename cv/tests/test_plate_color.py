"""Tests for the plate-color barbell weight detector.

Synthesised images: draw colored circles on a black background at known
positions. The detector should find each plate, classify its color, and
sum to the expected weight.
"""

from __future__ import annotations

import cv2
import numpy as np
import pytest

from pump_cv.weight import detect_plates, estimate_barbell_load


def _draw_plate(img: np.ndarray, center: tuple[int, int], radius: int,
                bgr: tuple[int, int, int]) -> None:
    cv2.circle(img, center, radius, bgr, thickness=-1)


def test_detects_a_red_plate():
    img = np.zeros((400, 800, 3), dtype=np.uint8)
    _draw_plate(img, (200, 200), 60, (0, 0, 220))  # red in BGR
    plates = detect_plates(img, min_area=500)
    assert len(plates) >= 1
    assert plates[0].color == "red"
    assert plates[0].weight_lb == 45.0


def test_detects_multiple_colors():
    img = np.zeros((400, 1200, 3), dtype=np.uint8)
    _draw_plate(img, (100, 200), 60, (0, 0, 220))    # red 45
    _draw_plate(img, (300, 200), 60, (220, 0, 0))    # blue 35
    _draw_plate(img, (500, 200), 60, (0, 220, 220))  # yellow 25
    _draw_plate(img, (700, 200), 60, (0, 220, 0))    # green 10
    plates = detect_plates(img, min_area=500)
    by_color = {p.color: p for p in plates}
    assert by_color["red"].weight_lb == 45
    assert by_color["blue"].weight_lb == 35
    assert by_color["yellow"].weight_lb == 25
    assert by_color["green"].weight_lb == 10


def test_estimate_load_two_plates_per_side():
    # 4 red plates + 45 lb bar = 45*4 + 45 = 225 lb.
    img = np.zeros((400, 1200, 3), dtype=np.uint8)
    _draw_plate(img, (200, 200), 60, (0, 0, 220))
    _draw_plate(img, (350, 200), 55, (0, 0, 220))
    _draw_plate(img, (700, 200), 60, (0, 0, 220))
    _draw_plate(img, (850, 200), 55, (0, 0, 220))
    total, conf = estimate_barbell_load(img, bar_weight_lb=45.0)
    assert total == pytest.approx(225.0)
    assert conf > 0.5


def test_estimate_load_with_no_plates():
    img = np.zeros((400, 800, 3), dtype=np.uint8)
    total, conf = estimate_barbell_load(img, bar_weight_lb=45.0)
    assert total == 45.0
    assert conf < 0.3


def test_estimate_load_asymmetric_warns_via_low_confidence():
    # 3 plates is asymmetric (one side has 2, the other has 1) → low confidence.
    img = np.zeros((400, 1200, 3), dtype=np.uint8)
    _draw_plate(img, (200, 200), 60, (0, 0, 220))
    _draw_plate(img, (700, 200), 60, (0, 0, 220))
    _draw_plate(img, (850, 200), 55, (0, 0, 220))
    total, conf = estimate_barbell_load(img, bar_weight_lb=45.0)
    assert total == pytest.approx(45.0 * 4)  # 3 plates + bar
    # Asymmetric → confidence penalty.
    sym_total, sym_conf = estimate_barbell_load(_make_two_plate_image(), bar_weight_lb=45.0)
    assert conf < sym_conf


def _make_two_plate_image() -> np.ndarray:
    img = np.zeros((400, 1200, 3), dtype=np.uint8)
    _draw_plate(img, (200, 200), 60, (0, 0, 220))
    _draw_plate(img, (700, 200), 60, (0, 0, 220))
    return img
