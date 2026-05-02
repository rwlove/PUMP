package com.rwlove.pump.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.rwlove.pump.ui.components.ActivityDots
import com.rwlove.pump.ui.components.AddSetDialog
import com.rwlove.pump.ui.components.ExerciseCard
import com.rwlove.pump.ui.components.SetItem
import com.rwlove.pump.viewmodel.WorkoutViewModel
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun WorkoutScreen(
    viewModel: WorkoutViewModel = hiltViewModel()
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    var showDatePicker by remember { mutableStateOf(false) }
    var addSetExercise by remember { mutableStateOf<String?>(null) }

    // Formatter for the "Today's Workout" label: e.g. "Monday, Apr 28"
    val workoutDateFormatter = remember {
        DateTimeFormatter.ofPattern("EEEE, MMM d")
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp)
    ) {
        Spacer(modifier = Modifier.height(8.dp))

        // Date navigator
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.Center,
            verticalAlignment = Alignment.CenterVertically
        ) {
            IconButton(onClick = { viewModel.previousDay() }) {
                Icon(Icons.AutoMirrored.Filled.KeyboardArrowLeft, contentDescription = "Previous day")
            }
            TextButton(onClick = { showDatePicker = true }) {
                Text(
                    text = state.selectedDate.format(
                        DateTimeFormatter.ofPattern("MMM d, yyyy")
                    ),
                    style = MaterialTheme.typography.titleMedium
                )
            }
            IconButton(onClick = { viewModel.nextDay() }) {
                Icon(Icons.AutoMirrored.Filled.KeyboardArrowRight, contentDescription = "Next day")
            }
            TextButton(onClick = { viewModel.goToToday() }) {
                Text("Today")
            }
        }

        // Weekly activity dots (trailing 7 real days)
        ActivityDots(
            weekActivity = state.trailingWeekActivity,
            modifier = Modifier.padding(vertical = 4.dp)
        )

        if (state.isLoading) {
            LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
        }

        state.errorMessage?.let { error ->
            Text(
                text = error,
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.padding(vertical = 4.dp)
            )
        }

        Spacer(modifier = Modifier.height(8.dp))

        // Muscle group chips
        LazyRow(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.fillMaxWidth()
        ) {
            items(state.muscleGroups) { group ->
                FilterChip(
                    selected = state.selectedGroup == group,
                    onClick = { viewModel.selectGroup(group) },
                    label = { Text(group) }
                )
            }
        }

        Spacer(modifier = Modifier.height(8.dp))

        // Exercise list + workout log in a single scrollable column
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            verticalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            // Exercise cards
            items(state.filteredExercises, key = { it.ID }) { exercise ->
                ExerciseCard(
                    exercise = exercise,
                    onClick = { addSetExercise = exercise.NAME }
                )
            }

            // Divider before the date's sets
            if (state.setsForDate.isNotEmpty()) {
                item {
                    Spacer(modifier = Modifier.height(8.dp))
                    HorizontalDivider()
                    Spacer(modifier = Modifier.height(4.dp))
                    Text(
                        text = state.selectedDate.format(workoutDateFormatter),
                        style = MaterialTheme.typography.titleMedium,
                        modifier = Modifier.fillMaxWidth(),
                        textAlign = TextAlign.Center
                    )
                    Spacer(modifier = Modifier.height(4.dp))
                }

                // Build set-number-per-exercise lookup before rendering
                val setsForDate = state.setsForDate
                // Count occurrences of each exercise name seen so far; use index to assign badge
                itemsIndexed(setsForDate, key = { _, it -> it.ID }) { index, set ->
                    // Count how many entries with the same name come before this index
                    val setNumber = setsForDate.subList(0, index).count { it.Name == set.Name }
                    // Total occurrences of this exercise name in the list
                    val totalForExercise = setsForDate.count { it.Name == set.Name }
                    SetItem(
                        set = set,
                        setBadge = if (totalForExercise > 1) "S${setNumber + 1}" else null,
                        onDelete = { viewModel.deleteSet(set) }
                    )
                }
            }
        }
    }

    // Date picker dialog
    if (showDatePicker) {
        val datePickerState = rememberDatePickerState(
            initialSelectedDateMillis = state.selectedDate
                .atStartOfDay(ZoneId.systemDefault())
                .toInstant()
                .toEpochMilli()
        )
        DatePickerDialog(
            onDismissRequest = { showDatePicker = false },
            confirmButton = {
                TextButton(onClick = {
                    datePickerState.selectedDateMillis?.let { millis ->
                        val date = Instant.ofEpochMilli(millis)
                            .atZone(ZoneId.systemDefault())
                            .toLocalDate()
                        viewModel.selectDate(date)
                    }
                    showDatePicker = false
                }) {
                    Text("OK")
                }
            },
            dismissButton = {
                TextButton(onClick = { showDatePicker = false }) {
                    Text("Cancel")
                }
            }
        ) {
            DatePicker(state = datePickerState)
        }
    }

    // Add set dialog
    addSetExercise?.let { exerciseName ->
        // Determine the set position (0-indexed) for this exercise in today's list
        val currentSets = state.setsForDate
        val setPosition = currentSets.count { it.Name == exerciseName }

        // Find the most recent past date where this exercise was performed and get the
        // (setPosition+1)-th set (or last if fewer exist) for pre-fill values
        val allSets = state.allSets
        val todayStr = state.selectedDate.format(DateTimeFormatter.ISO_LOCAL_DATE)
        val historicalDatesForExercise = allSets
            .filter { it.Name == exerciseName && it.Date < todayStr }
            .map { it.Date }
            .distinct()
            .sortedDescending()
        val mostRecentDate = historicalDatesForExercise.firstOrNull()
        val historicalSets = if (mostRecentDate != null) {
            allSets.filter { it.Name == exerciseName && it.Date == mostRecentDate }
        } else emptyList()
        // Clamp to last if setPosition is beyond bounds
        val prefillSet = if (historicalSets.isNotEmpty()) {
            historicalSets.getOrNull(setPosition) ?: historicalSets.last()
        } else null

        AddSetDialog(
            exerciseName = exerciseName,
            initialWeight = prefillSet?.Weight ?: "",
            initialReps = prefillSet?.Reps?.toString() ?: "",
            onDismiss = { addSetExercise = null },
            onSave = { weight, reps ->
                viewModel.addSet(exerciseName, weight, reps)
                addSetExercise = null
            }
        )
    }
}
