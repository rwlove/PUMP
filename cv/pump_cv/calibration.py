"""Camera calibration: intrinsics (one camera) and stereo extrinsics (a pair).

Workflow when cameras are physically installed:

  1. Print a chessboard target (e.g. 9×6 inner corners, 25 mm squares).
  2. Capture ~20 photos per camera at varied poses (intrinsics).
  3. Capture ~20 SYNCHRONISED pairs of the same target visible to both
     cameras at the same time (extrinsics).
  4. Run intrinsics for each camera independently:
        python -m pump_cv.calibration intrinsics \
            --images cam-a/*.jpg --rows 6 --cols 9 --square-size 25 \
            --out calibration/cam-a.npz
  5. Run stereo to lock the two cameras into a shared world frame:
        python -m pump_cv.calibration stereo \
            --left  cam-a/pair-*.jpg \
            --right cam-b/pair-*.jpg \
            --left-intrinsics  calibration/cam-a.npz \
            --right-intrinsics calibration/cam-b.npz \
            --rows 6 --cols 9 --square-size 25 \
            --out calibration/stereo.npz

  The resulting .npz files are loaded by `load_camera()` /
  `load_stereo()` and consumed by `pump_cv.fusion`.

Until cameras are installed, this module is exercised only by import-
time tests; the chessboard CLI runs only against real photos.
"""

from __future__ import annotations

import argparse
import sys
from pathlib import Path

import cv2
import numpy as np

from . import log
from .fusion import CameraCalibration

logger = log.get(__name__)


def find_chessboard_corners(
    image_paths: list[Path], rows: int, cols: int,
) -> tuple[list[np.ndarray], list[np.ndarray], tuple[int, int] | None]:
    """Run cv2.findChessboardCorners over a set of images.

    Returns (object_points, image_points, image_size). object_points is
    the 3D coords of the corners on the board (z=0 plane); image_points
    is the matching 2D detections per image. image_size is (w, h).
    """
    objp = np.zeros((rows * cols, 3), np.float32)
    objp[:, :2] = np.mgrid[0:cols, 0:rows].T.reshape(-1, 2)

    obj_points: list[np.ndarray] = []
    img_points: list[np.ndarray] = []
    image_size: tuple[int, int] | None = None

    for path in image_paths:
        img = cv2.imread(str(path))
        if img is None:
            logger.warning("calibrate: could not read", path=str(path))
            continue
        gray = cv2.cvtColor(img, cv2.COLOR_BGR2GRAY)
        if image_size is None:
            image_size = (gray.shape[1], gray.shape[0])
        ok, corners = cv2.findChessboardCorners(gray, (cols, rows))
        if not ok:
            logger.warning("calibrate: no chessboard", path=str(path))
            continue
        criteria = (cv2.TERM_CRITERIA_EPS | cv2.TERM_CRITERIA_MAX_ITER, 30, 0.001)
        corners = cv2.cornerSubPix(gray, corners, (11, 11), (-1, -1), criteria)
        obj_points.append(objp.copy())
        img_points.append(corners)

    return obj_points, img_points, image_size


def calibrate_intrinsics(
    image_paths: list[Path], rows: int, cols: int, square_size_mm: float,
) -> tuple[np.ndarray, np.ndarray]:
    obj_points, img_points, image_size = find_chessboard_corners(image_paths, rows, cols)
    if not obj_points:
        raise RuntimeError("calibrate: no usable chessboard images")
    obj_points = [op * square_size_mm for op in obj_points]
    rms, K, dist, _, _ = cv2.calibrateCamera(obj_points, img_points, image_size, None, None)
    logger.info("calibrate: intrinsics done", rms_pixels=float(rms), n_images=len(obj_points))
    return K, dist.ravel()


def calibrate_stereo(
    left_paths: list[Path], right_paths: list[Path],
    left_K: np.ndarray, left_dist: np.ndarray,
    right_K: np.ndarray, right_dist: np.ndarray,
    rows: int, cols: int, square_size_mm: float,
) -> tuple[np.ndarray, np.ndarray]:
    if len(left_paths) != len(right_paths):
        raise ValueError("calibrate stereo: image counts differ between cameras")

    obj_l, pts_l, size_l = find_chessboard_corners(left_paths, rows, cols)
    obj_r, pts_r, size_r = find_chessboard_corners(right_paths, rows, cols)

    if not obj_l or not obj_r or len(obj_l) != len(obj_r):
        raise RuntimeError("calibrate stereo: chessboard mismatch between paired images")
    if size_l != size_r:
        raise RuntimeError(f"calibrate stereo: image size mismatch {size_l} vs {size_r}")

    obj_l = [op * square_size_mm for op in obj_l]
    flags = cv2.CALIB_FIX_INTRINSIC
    rms, _, _, _, _, R, T, _, _ = cv2.stereoCalibrate(
        obj_l, pts_l, pts_r, left_K, left_dist, right_K, right_dist, size_l, flags=flags,
    )
    logger.info("calibrate: stereo done", rms_pixels=float(rms), n_pairs=len(obj_l))
    return R, T.ravel()


def save_camera(path: Path, K: np.ndarray, dist: np.ndarray,
                R: np.ndarray | None = None, t: np.ndarray | None = None) -> None:
    """Persist intrinsics (and optional extrinsics) to a .npz file."""
    payload = {"K": K, "dist": dist}
    if R is not None:
        payload["R"] = R
    if t is not None:
        payload["t"] = t
    np.savez(path, **payload)
    logger.info("calibrate: saved", path=str(path))


def load_camera(path: Path) -> CameraCalibration:
    """Load a .npz produced by save_camera. R/t default to identity/zero
    (single-camera world frame)."""
    data = np.load(path)
    K = data["K"]
    dist = data["dist"]
    R = data["R"] if "R" in data else np.eye(3)
    t = data["t"] if "t" in data else np.zeros(3)
    return CameraCalibration(K=K, dist=dist, R=R, t=t)


def _cli(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="pump_cv.calibration")
    sub = parser.add_subparsers(dest="cmd", required=True)

    p_in = sub.add_parser("intrinsics", help="calibrate one camera from chessboard photos")
    p_in.add_argument("--images", nargs="+", type=Path, required=True)
    p_in.add_argument("--rows", type=int, required=True)
    p_in.add_argument("--cols", type=int, required=True)
    p_in.add_argument("--square-size", type=float, required=True, help="mm")
    p_in.add_argument("--out", type=Path, required=True)

    p_st = sub.add_parser("stereo", help="calibrate stereo extrinsics from paired photos")
    p_st.add_argument("--left", nargs="+", type=Path, required=True)
    p_st.add_argument("--right", nargs="+", type=Path, required=True)
    p_st.add_argument("--left-intrinsics", type=Path, required=True)
    p_st.add_argument("--right-intrinsics", type=Path, required=True)
    p_st.add_argument("--rows", type=int, required=True)
    p_st.add_argument("--cols", type=int, required=True)
    p_st.add_argument("--square-size", type=float, required=True)
    p_st.add_argument("--out", type=Path, required=True,
                      help="written as the LEFT camera with R/t into the right frame")

    args = parser.parse_args(argv)
    log.configure()

    if args.cmd == "intrinsics":
        K, dist = calibrate_intrinsics(args.images, args.rows, args.cols, args.square_size)
        save_camera(args.out, K, dist)
    elif args.cmd == "stereo":
        left = load_camera(args.left_intrinsics)
        right = load_camera(args.right_intrinsics)
        R, t = calibrate_stereo(
            args.left, args.right, left.K, left.dist, right.K, right.dist,
            args.rows, args.cols, args.square_size,
        )
        # Convention: store the RIGHT camera's extrinsics relative to LEFT
        # (LEFT is the world frame).
        save_camera(args.out, right.K, right.dist, R=R, t=t)

    return 0


if __name__ == "__main__":
    sys.exit(_cli(sys.argv[1:]))
