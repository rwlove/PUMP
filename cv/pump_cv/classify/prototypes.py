"""Exercise prototypes and the prototype-matching classifier.

A prototype is one canonical rep's worth of pose features for one
exercise. Phase 2 will record prototypes from short reference clips the
athlete records via the PUMP UI; for now they're produced and consumed
in code (e.g. by tests).

Pose features: rather than raw (x, y) keypoints — which are translation-
and scale-dependent — we extract a small vector of joint angles per
frame. The vector is the same for everyone regardless of body size or
camera position, which is exactly what we need for cross-clip matching.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from pathlib import Path

import numpy as np

from ..fsm.rep_counter import joint_angle
from ..pose.types import (
    LEFT_ANKLE,
    LEFT_ELBOW,
    LEFT_HIP,
    LEFT_KNEE,
    LEFT_SHOULDER,
    LEFT_WRIST,
    RIGHT_ANKLE,
    RIGHT_ELBOW,
    RIGHT_HIP,
    RIGHT_KNEE,
    RIGHT_SHOULDER,
    RIGHT_WRIST,
    Pose,
)
from .dtw import dtw_distance

# (a_idx, b_idx, c_idx) triples — joint angle at b. Picked to be roughly
# exhaustive across upper and lower body; the classifier doesn't need to
# know which exercise uses which joint, just that the vector is the same
# shape everywhere.
_FEATURE_JOINTS: tuple[tuple[int, int, int], ...] = (
    (LEFT_HIP, LEFT_KNEE, LEFT_ANKLE),
    (RIGHT_HIP, RIGHT_KNEE, RIGHT_ANKLE),
    (LEFT_SHOULDER, LEFT_ELBOW, LEFT_WRIST),
    (RIGHT_SHOULDER, RIGHT_ELBOW, RIGHT_WRIST),
    (LEFT_KNEE, LEFT_HIP, LEFT_SHOULDER),
    (RIGHT_KNEE, RIGHT_HIP, RIGHT_SHOULDER),
)


def pose_sequence_to_features(poses: list[Pose]) -> np.ndarray:
    """Turn a list of Poses into an (T, D) feature array.

    Missing/low-confidence keypoints fall through joint_angle's
    confidence guard inside keypoint_angle (returning None / 0). For
    DTW we replace 0 with the previous valid value to avoid spurious
    distance jumps; if no prior value exists, 180° (extended).
    """
    T = len(poses)
    D = len(_FEATURE_JOINTS)
    feats = np.full((T, D), 180.0, dtype=np.float32)
    last_valid = np.full(D, 180.0, dtype=np.float32)
    for t, p in enumerate(poses):
        for d, (a, b, c) in enumerate(_FEATURE_JOINTS):
            ka, kb, kc = p.keypoints[a], p.keypoints[b], p.keypoints[c]
            if ka.confidence < 0.3 or kb.confidence < 0.3 or kc.confidence < 0.3:
                feats[t, d] = last_valid[d]
                continue
            ang = joint_angle((ka.x, ka.y), (kb.x, kb.y), (kc.x, kc.y))
            feats[t, d] = ang
            last_valid[d] = ang
    return feats


@dataclass(frozen=True, slots=True)
class ExercisePrototype:
    """One canonical rep for one exercise. The features array is (T, D)
    where D == len(_FEATURE_JOINTS)."""

    exercise_name: str  # must match a PUMP exercise name verbatim
    features: np.ndarray
    source_clip: str = ""  # informational: which clip this came from


@dataclass(frozen=True, slots=True)
class ClassificationResult:
    name: str
    distance: float
    confidence: float  # 0..1; how much we beat the runner-up by


def classify_window(
    window_features: np.ndarray,
    prototypes: list[ExercisePrototype],
    band: float | None = 0.1,
) -> ClassificationResult | None:
    """Score `window_features` against every prototype; return best match.

    Confidence is the relative gap between best and second-best:
        conf = (second - best) / second   (clamped to [0, 1])
    With one prototype, confidence is 1.0 (no ambiguity is possible);
    with two equally-good prototypes, it's 0. With no prototypes,
    returns None.
    """
    if not prototypes or window_features.shape[0] == 0:
        return None

    scored: list[tuple[ExercisePrototype, float]] = []
    for proto in prototypes:
        d = dtw_distance(window_features, proto.features, band=band)
        # Length-normalise so prototypes of different durations compete
        # on equal terms.
        norm = d / max(1.0, window_features.shape[0] + proto.features.shape[0])
        scored.append((proto, norm))
    scored.sort(key=lambda x: x[1])

    best_proto, best_d = scored[0]
    if len(scored) == 1:
        conf = 1.0
    else:
        second_d = scored[1][1]
        conf = max(0.0, min(1.0, (second_d - best_d) / max(1e-9, second_d)))

    return ClassificationResult(name=best_proto.exercise_name, distance=best_d, confidence=conf)


class PrototypeStore:
    """Disk-backed library of prototypes.

    Layout:
        <root>/
          squat__001.npz       features array
          squat__001.json      metadata (exercise name, source clip)
          bench-press__001.npz
          bench-press__001.json
          ...

    Multiple prototypes per exercise are supported — useful for capturing
    different camera angles or athlete fatigue states.
    """

    def __init__(self, root: Path):
        self._root = Path(root)
        self._root.mkdir(parents=True, exist_ok=True)

    def add(self, prototype: ExercisePrototype) -> Path:
        slug = _slug(prototype.exercise_name)
        # find next free index
        idx = 1
        while (self._root / f"{slug}__{idx:03d}.npz").exists():
            idx += 1
        npz_path = self._root / f"{slug}__{idx:03d}.npz"
        json_path = self._root / f"{slug}__{idx:03d}.json"
        np.savez(npz_path, features=prototype.features)
        json_path.write_text(json.dumps({
            "exercise_name": prototype.exercise_name,
            "source_clip": prototype.source_clip,
        }))
        return npz_path

    def load_all(self) -> list[ExercisePrototype]:
        prototypes: list[ExercisePrototype] = []
        for npz_path in sorted(self._root.glob("*.npz")):
            json_path = npz_path.with_suffix(".json")
            if not json_path.exists():
                continue
            meta = json.loads(json_path.read_text())
            arr = np.load(npz_path)["features"]
            prototypes.append(ExercisePrototype(
                exercise_name=meta["exercise_name"],
                features=arr,
                source_clip=meta.get("source_clip", ""),
            ))
        return prototypes


def _slug(name: str) -> str:
    return "".join(ch.lower() if ch.isalnum() else "-" for ch in name).strip("-")
