"""Tests for the local-weights guard in pump_cv.pose.yolo.

ultralytics downloads a missing weights file from GitHub, and YOLO_OFFLINE
does not stop it. The guard must refuse a missing file unless the operator
explicitly opts in."""

from __future__ import annotations

import pytest

from pump_cv.pose import yolo


def test_existing_relative_weights_pass_through(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    (tmp_path / "yolov8m-pose.pt").write_bytes(b"x")
    monkeypatch.delenv(yolo.ALLOW_DOWNLOAD_ENV, raising=False)
    assert yolo.resolve_local_weights("yolov8m-pose.pt") == "yolov8m-pose.pt"


def test_existing_absolute_weights_pass_through(tmp_path, monkeypatch):
    w = tmp_path / "model.pt"
    w.write_bytes(b"x")
    monkeypatch.delenv(yolo.ALLOW_DOWNLOAD_ENV, raising=False)
    assert yolo.resolve_local_weights(str(w)) == str(w)


def test_missing_weights_fail_loudly(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    monkeypatch.delenv(yolo.ALLOW_DOWNLOAD_ENV, raising=False)
    with pytest.raises(FileNotFoundError, match="yolov8m-pose.pt"):
        yolo.resolve_local_weights("yolov8m-pose.pt")


@pytest.mark.parametrize("val", ["0", "false", "", "no"])
def test_download_not_allowed_by_falsy_env(tmp_path, monkeypatch, val):
    monkeypatch.chdir(tmp_path)
    monkeypatch.setenv(yolo.ALLOW_DOWNLOAD_ENV, val)
    with pytest.raises(FileNotFoundError):
        yolo.resolve_local_weights("yolov8m-pose.pt")


def test_download_opt_in_returns_name(tmp_path, monkeypatch):
    monkeypatch.chdir(tmp_path)
    monkeypatch.setenv(yolo.ALLOW_DOWNLOAD_ENV, "1")
    assert yolo.resolve_local_weights("yolov8m-pose.pt") == "yolov8m-pose.pt"
