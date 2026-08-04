"""Exercise-name resolution for auto-logged sets."""

from __future__ import annotations

from pump_voltra.naming import ExerciseNamer

TODAY = "2026-08-03"

EXERCISES = [
    {"ID": 1, "Name": "Cable Row", "Voltra": True, "Color": "#aa0000"},
    {"ID": 2, "Name": "Cable Crossover", "Voltra": True, "Color": "#00aa00"},
    # The whole reason the flag exists: a cable-named exercise on a plate
    # stack, which the trainer has nothing to do with.
    {"ID": 3, "Name": "Seated Cable Pulldown", "Voltra": False, "Color": "#0000aa"},
    {"ID": 4, "Name": "Bench Press", "Voltra": False, "Color": "#aaaa00"},
]


def namer() -> ExerciseNamer:
    n = ExerciseNamer(default_exercise="Voltra")
    n.set_exercises(EXERCISES)
    return n


def a_set(name: str, **kw):
    return {"Date": TODAY, "Name": name, "Color": "#123456", "WorkoutColor": "#654321", **kw}


def test_flag_not_name_decides() -> None:
    n = namer()
    assert n.is_flagged("Cable Row")
    assert not n.is_flagged("Seated Cable Pulldown")
    assert not n.is_flagged("Bench Press")


def test_matching_is_case_insensitive() -> None:
    # sets.name is free text with no foreign key to exercises.
    n = namer()
    assert n.is_flagged("cable row")
    assert n.is_flagged("  CABLE ROW  ")


def test_anchor_seeded_from_todays_last_flagged_set() -> None:
    n = namer()
    n.seed_from_sets(
        [
            a_set("Bench Press"),
            a_set("Cable Row"),
            a_set("Cable Crossover"),
            a_set("Bench Press"),
        ],
        TODAY,
    )
    anchor, pending = n.resolve(TODAY)
    assert anchor.name == "Cable Crossover"
    assert pending is False


def test_unflagged_cable_exercise_is_not_an_anchor() -> None:
    n = namer()
    n.seed_from_sets([a_set("Seated Cable Pulldown")], TODAY)
    anchor, pending = n.resolve(TODAY)
    assert anchor.name == "Voltra"
    assert pending is True


def test_no_anchor_falls_back_to_default_and_marks_pending() -> None:
    n = namer()
    anchor, pending = n.resolve(TODAY)
    assert anchor.name == "Voltra"
    assert pending is True


def test_yesterdays_sets_do_not_anchor_today() -> None:
    n = namer()
    n.seed_from_sets([{"Date": "2026-08-02", "Name": "Cable Row"}], TODAY)
    _, pending = n.resolve(TODAY)
    assert pending is True


def test_anchor_follows_the_athlete_mid_workout() -> None:
    n = namer()
    n.observe_set(a_set("Cable Row"), TODAY, TODAY)
    assert n.resolve(TODAY)[0].name == "Cable Row"
    n.observe_set(a_set("Cable Crossover"), TODAY, TODAY)
    assert n.resolve(TODAY)[0].name == "Cable Crossover"


def test_own_writes_are_ignored() -> None:
    """Anchoring on our own sets would latch a name the athlete has since
    corrected on a different row."""
    n = namer()
    n.observe_set(a_set("Cable Row"), TODAY, TODAY)
    n.observe_set(a_set("Cable Crossover", Source="voltra"), TODAY, TODAY)
    assert n.resolve(TODAY)[0].name == "Cable Row"


def test_events_for_other_dates_are_ignored() -> None:
    n = namer()
    n.observe_set(a_set("Cable Row"), "2026-08-02", TODAY)
    assert n.resolve(TODAY)[1] is True


def test_anchor_colors_are_inherited() -> None:
    # So an auto-logged set is visually part of the same block in the UI.
    n = namer()
    n.observe_set(a_set("Cable Row"), TODAY, TODAY)
    anchor, _ = n.resolve(TODAY)
    assert anchor.color == "#123456"
    assert anchor.workout_color == "#654321"


def test_stale_anchor_is_dropped_at_midnight() -> None:
    n = namer()
    n.observe_set(a_set("Cable Row"), TODAY, TODAY)
    assert n.resolve(TODAY)[1] is False
    anchor, pending = n.resolve("2026-08-04")
    assert pending is True
    assert anchor.name == "Voltra"


def test_unflagging_an_exercise_takes_effect_on_refresh() -> None:
    n = namer()
    assert n.is_flagged("Cable Row")
    n.set_exercises([{**e, "Voltra": False} for e in EXERCISES])
    assert not n.is_flagged("Cable Row")
    assert n.flagged_count == 0


def test_exercises_with_blank_names_are_skipped() -> None:
    n = ExerciseNamer()
    n.set_exercises([{"Name": "   ", "Voltra": True}, {"Voltra": True}])
    assert n.flagged_count == 0
