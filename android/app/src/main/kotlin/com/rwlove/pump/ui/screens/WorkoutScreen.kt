package com.rwlove.pump.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowLeft
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Search
import androidx.compose.material3.Button
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilterChip
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Surface
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
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.window.Dialog
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.rwlove.pump.ui.components.ActivityDots
import com.rwlove.pump.ui.components.ExerciseCard
import com.rwlove.pump.ui.components.SetItem
import com.rwlove.pump.viewmodel.AutoSaveStatus
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
    var showLogWeight by remember { mutableStateOf(false) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp)
    ) {
        Spacer(modifier = Modifier.height(8.dp))

        // Date navigator with friendly date
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
                    text = state.friendlyDate,
                    style = MaterialTheme.typography.titleMedium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis
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

        // Save status + Log Weight button row
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.SpaceBetween,
            verticalAlignment = Alignment.CenterVertically
        ) {
            // Save status
            val statusText = when (state.saveStatus) {
                AutoSaveStatus.SAVING -> "Saving..."
                AutoSaveStatus.SAVED -> "Saved"
                AutoSaveStatus.ERROR -> "Error saving"
                AutoSaveStatus.IDLE -> ""
            }
            val statusColor = when (state.saveStatus) {
                AutoSaveStatus.SAVING -> MaterialTheme.colorScheme.onSurfaceVariant
                AutoSaveStatus.SAVED -> MaterialTheme.colorScheme.primary
                AutoSaveStatus.ERROR -> MaterialTheme.colorScheme.error
                AutoSaveStatus.IDLE -> Color.Transparent
            }
            Text(
                text = statusText,
                style = MaterialTheme.typography.labelSmall,
                color = statusColor
            )

            OutlinedButton(
                onClick = { showLogWeight = true },
                shape = RoundedCornerShape(50)
            ) {
                Text("Log Weight")
            }
        }

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

        Spacer(modifier = Modifier.height(4.dp))

        // Muscle group chips
        LazyRow(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.fillMaxWidth()
        ) {
            items(state.muscleGroups) { group ->
                FilterChip(
                    selected = state.selectedGroup == group,
                    onClick = {
                        if (state.selectedGroup == group) viewModel.clearGroup()
                        else viewModel.selectGroup(group)
                    },
                    label = { Text(group) }
                )
            }
        }

        // Group header with back button and search when group is selected
        if (state.selectedGroup != null) {
            Spacer(modifier = Modifier.height(4.dp))
            Row(
                modifier = Modifier.fillMaxWidth(),
                verticalAlignment = Alignment.CenterVertically
            ) {
                IconButton(onClick = { viewModel.clearGroup() }) {
                    Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "Back to groups")
                }
                Text(
                    text = state.selectedGroup ?: "",
                    style = MaterialTheme.typography.titleSmall,
                    modifier = Modifier.weight(1f)
                )
                OutlinedTextField(
                    value = state.searchQuery,
                    onValueChange = { viewModel.setSearchQuery(it) },
                    placeholder = { Text("Filter...") },
                    leadingIcon = { Icon(Icons.Default.Search, contentDescription = null, modifier = Modifier.size(18.dp)) },
                    singleLine = true,
                    modifier = Modifier
                        .weight(1.5f)
                        .height(48.dp),
                    textStyle = MaterialTheme.typography.bodySmall
                )
            }
        }

        Spacer(modifier = Modifier.height(8.dp))

        // Exercise list + workout log in a single scrollable column
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            verticalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            // Exercise cards (only when a group is selected)
            if (state.selectedGroup != null) {
                items(state.filteredExercises, key = { it.ID }) { exercise ->
                    ExerciseCard(
                        exercise = exercise,
                        onClick = { addSetExercise = exercise.NAME }
                    )
                }

                if (state.filteredExercises.isEmpty()) {
                    item {
                        Text(
                            text = "Select a group above to browse exercises",
                            style = MaterialTheme.typography.bodyMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier
                                .fillMaxWidth()
                                .padding(vertical = 24.dp),
                            textAlign = TextAlign.Center
                        )
                    }
                }
            } else {
                item {
                    Text(
                        text = "Select a group above to browse exercises",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(vertical = 24.dp),
                        textAlign = TextAlign.Center
                    )
                }
            }

            // Divider before the date's sets
            if (state.setsForDate.isNotEmpty()) {
                item {
                    Spacer(modifier = Modifier.height(8.dp))
                    HorizontalDivider()
                    Spacer(modifier = Modifier.height(4.dp))
                    Text(
                        text = "Today's Workout",
                        style = MaterialTheme.typography.titleMedium,
                        modifier = Modifier.fillMaxWidth(),
                        textAlign = TextAlign.Center
                    )
                    Spacer(modifier = Modifier.height(4.dp))
                }

                val setsForDate = state.setsForDate
                itemsIndexed(setsForDate, key = { idx, it -> "${it.Name}-${it.Weight}-${it.Reps}-$idx" }) { index, set ->
                    val setNumber = setsForDate.subList(0, index).count { it.Name == set.Name }
                    val totalForExercise = setsForDate.count { it.Name == set.Name }
                    SetItem(
                        set = set,
                        setBadge = if (totalForExercise > 1) "S${setNumber + 1}" else null,
                        onDelete = { viewModel.deleteSet(set) },
                        onWeightChange = { w ->
                            viewModel.updateSet(index, w, set.Reps)
                        },
                        onRepsChange = { r ->
                            viewModel.updateSet(index, set.Weight, r)
                        }
                    )
                }
            } else if (!state.isLoading) {
                item {
                    Spacer(modifier = Modifier.height(8.dp))
                    HorizontalDivider()
                    Spacer(modifier = Modifier.height(4.dp))
                    Text(
                        text = "Today's Workout",
                        style = MaterialTheme.typography.titleMedium,
                        modifier = Modifier.fillMaxWidth(),
                        textAlign = TextAlign.Center
                    )
                    Spacer(modifier = Modifier.height(16.dp))
                    Text(
                        text = "Tap an exercise above to add it here",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        modifier = Modifier.fillMaxWidth(),
                        textAlign = TextAlign.Center
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

    // Add set dialog with auto-fill
    addSetExercise?.let { exerciseName ->
        val currentSets = state.setsForDate
        val setPosition = currentSets.count { it.Name == exerciseName }

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

        // Auto-fill: use historical data if config.AutoFill is true
        val prefillSet = if (state.config.AutoFill && historicalSets.isNotEmpty()) {
            historicalSets.getOrNull(setPosition) ?: historicalSets.last()
        } else {
            // Fall back to exercise defaults
            null
        }

        val exercise = state.exercises.find { it.NAME == exerciseName }
        val defaultWeight = prefillSet?.Weight ?: exercise?.WEIGHT ?: ""
        val defaultReps = prefillSet?.Reps?.toString() ?: exercise?.REPS?.toString() ?: ""

        AddSetDialog(
            exerciseName = exerciseName,
            initialWeight = defaultWeight,
            initialReps = defaultReps,
            onDismiss = { addSetExercise = null },
            onSave = { weight, reps ->
                viewModel.addSet(exerciseName, weight, reps)
                addSetExercise = null
            }
        )
    }

    // Log Weight bottom sheet
    if (showLogWeight) {
        LogWeightDialog(
            initialDate = state.selectedDate,
            onDismiss = { showLogWeight = false },
            onSave = { date, weight ->
                viewModel.logWeight(date, weight)
                showLogWeight = false
            }
        )
    }
}

@Composable
private fun AddSetDialog(
    exerciseName: String,
    onDismiss: () -> Unit,
    onSave: (weight: String, reps: Int) -> Unit,
    initialWeight: String = "",
    initialReps: String = ""
) {
    var weight by remember { mutableStateOf(initialWeight) }
    var reps by remember { mutableStateOf(initialReps) }

    Dialog(onDismissRequest = onDismiss) {
        Surface(
            shape = MaterialTheme.shapes.large,
            color = MaterialTheme.colorScheme.surface,
            tonalElevation = 6.dp
        ) {
            Column(
                modifier = Modifier.padding(24.dp)
            ) {
                Text(
                    text = "Add Set",
                    style = MaterialTheme.typography.headlineSmall
                )
                Spacer(modifier = Modifier.height(8.dp))
                Text(
                    text = exerciseName,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
                Spacer(modifier = Modifier.height(16.dp))

                OutlinedTextField(
                    value = weight,
                    onValueChange = { weight = it },
                    label = { Text("Weight (lbs)") },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth()
                )

                Spacer(modifier = Modifier.height(8.dp))

                OutlinedTextField(
                    value = reps,
                    onValueChange = { reps = it },
                    label = { Text("Reps") },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth()
                )

                Spacer(modifier = Modifier.height(24.dp))

                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.End
                ) {
                    TextButton(onClick = onDismiss) {
                        Text("Cancel")
                    }
                    Button(
                        onClick = {
                            val repsInt = reps.toIntOrNull() ?: 0
                            val weightStr = weight.ifEmpty { "0" }
                            onSave(weightStr, repsInt)
                        },
                        enabled = weight.isNotEmpty() && reps.isNotEmpty()
                    ) {
                        Text("Save")
                    }
                }
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun LogWeightDialog(
    initialDate: LocalDate,
    onDismiss: () -> Unit,
    onSave: (date: LocalDate, weight: String) -> Unit
) {
    var weightInput by remember { mutableStateOf("") }
    var selectedDate by remember { mutableStateOf(initialDate) }
    var showDatePicker by remember { mutableStateOf(false) }

    Dialog(onDismissRequest = onDismiss) {
        Surface(
            shape = MaterialTheme.shapes.large,
            color = MaterialTheme.colorScheme.surface,
            tonalElevation = 6.dp
        ) {
            Column(
                modifier = Modifier.padding(24.dp)
            ) {
                Text(
                    text = "Log Body Weight",
                    style = MaterialTheme.typography.headlineSmall
                )
                Spacer(modifier = Modifier.height(16.dp))

                Text(
                    text = "Date",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
                TextButton(onClick = { showDatePicker = true }) {
                    Text(
                        text = selectedDate.format(DateTimeFormatter.ofPattern("MMM d, yyyy"))
                    )
                }

                Spacer(modifier = Modifier.height(8.dp))

                OutlinedTextField(
                    value = weightInput,
                    onValueChange = { weightInput = it },
                    label = { Text("Weight (lbs)") },
                    placeholder = { Text("e.g. 185.5") },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth()
                )

                Spacer(modifier = Modifier.height(24.dp))

                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.End
                ) {
                    TextButton(onClick = onDismiss) {
                        Text("Cancel")
                    }
                    Button(
                        onClick = {
                            if (weightInput.isNotEmpty()) {
                                onSave(selectedDate, weightInput)
                            }
                        },
                        enabled = weightInput.isNotEmpty()
                    ) {
                        Text("Save")
                    }
                }
            }
        }
    }

    if (showDatePicker) {
        val datePickerState = rememberDatePickerState(
            initialSelectedDateMillis = selectedDate
                .atStartOfDay(ZoneId.systemDefault())
                .toInstant()
                .toEpochMilli()
        )
        DatePickerDialog(
            onDismissRequest = { showDatePicker = false },
            confirmButton = {
                TextButton(onClick = {
                    datePickerState.selectedDateMillis?.let { millis ->
                        selectedDate = Instant.ofEpochMilli(millis)
                            .atZone(ZoneId.systemDefault())
                            .toLocalDate()
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
}
