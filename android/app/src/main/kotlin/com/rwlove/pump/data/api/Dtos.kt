package com.rwlove.pump.data.api

data class ExerciseDto(
    val ID: Int = 0,
    val GR: String = "",
    val PLACE: String = "",
    val NAME: String = "",
    val DESCR: String = "",
    val IMAGE: String = "",
    val COLOR: String = "#2780e3",
    val WEIGHT: String = "0",
    val REPS: Int = 0
)

data class SetDto(
    val ID: Int = 0,
    val Date: String = "",
    val Name: String = "",
    val Color: String = "",
    val WorkoutColor: String = "",
    val Weight: String = "0",
    val Reps: Int = 0
)

data class BodyWeightDto(
    val ID: Int = 0,
    val DATE: String = "",
    val WEIGHT: String = "0"
)

data class ColorBody(
    val color: String
)

data class ConfigDto(
    val Theme: String = "cosmo",
    val Color: String = "dark",
    val HeatColor: String = "#2780e3",
    val PageStep: Int = 10,
    val FrequencyDays: Int = 30,
    val DisplayDays: Int = 30,
    val AutoFill: Boolean = true
)
