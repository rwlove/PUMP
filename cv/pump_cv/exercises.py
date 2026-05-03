"""Per-exercise joint-angle mapping for the rep counter.

Each entry tells the rep counter which three keypoints to compute the
angle from for one exercise. The classifier picks the exercise; this
table tells the rep counter which joint to watch.

Add new entries when a new exercise needs different reps. Keys must
match PUMP exercise names (case-sensitive).
"""

from __future__ import annotations

from .pipeline import ExerciseSpec
from .pose.types import (
    LEFT_ANKLE,
    LEFT_ELBOW,
    LEFT_HIP,
    LEFT_KNEE,
    LEFT_SHOULDER,
    LEFT_WRIST,
)


_DEFAULT_SPECS: dict[str, ExerciseSpec] = {
    # Lower-body — knee flexion drives the rep
    "Squat":         ExerciseSpec("Squat",         LEFT_HIP, LEFT_KNEE, LEFT_ANKLE),
    "Front Squat":   ExerciseSpec("Front Squat",   LEFT_HIP, LEFT_KNEE, LEFT_ANKLE),
    "Lunge":         ExerciseSpec("Lunge",         LEFT_HIP, LEFT_KNEE, LEFT_ANKLE),
    "Goblet Squat":  ExerciseSpec("Goblet Squat",  LEFT_HIP, LEFT_KNEE, LEFT_ANKLE),

    # Hip-hinge — knee bends some, but hip-shoulder line is more reliable
    "Deadlift":      ExerciseSpec("Deadlift",      LEFT_KNEE, LEFT_HIP, LEFT_SHOULDER),
    "RDL":           ExerciseSpec("RDL",           LEFT_KNEE, LEFT_HIP, LEFT_SHOULDER),

    # Upper-body — elbow flexion drives the rep
    "Bench Press":           ExerciseSpec("Bench Press",           LEFT_SHOULDER, LEFT_ELBOW, LEFT_WRIST),
    "Incline Bench Press":   ExerciseSpec("Incline Bench Press",   LEFT_SHOULDER, LEFT_ELBOW, LEFT_WRIST),
    "Overhead Press":        ExerciseSpec("Overhead Press",        LEFT_SHOULDER, LEFT_ELBOW, LEFT_WRIST),
    "Bicep Curl":            ExerciseSpec("Bicep Curl",            LEFT_SHOULDER, LEFT_ELBOW, LEFT_WRIST),
    "Tricep Extension":      ExerciseSpec("Tricep Extension",      LEFT_SHOULDER, LEFT_ELBOW, LEFT_WRIST),
    "Row":                   ExerciseSpec("Row",                   LEFT_SHOULDER, LEFT_ELBOW, LEFT_WRIST),
}


def lookup(name: str, default: ExerciseSpec | None = None) -> ExerciseSpec | None:
    """Resolve an exercise name to its joint mapping. Returns the default
    (or None) when the name isn't in the table — caller decides whether
    to skip rep counting or fall back."""
    return _DEFAULT_SPECS.get(name, default)


def known_exercises() -> list[str]:
    return list(_DEFAULT_SPECS.keys())
