package com.rwlove.pump.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.rwlove.pump.data.api.ExerciseDto
import com.rwlove.pump.data.api.SetDto
import com.rwlove.pump.data.repository.PumpRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import javax.inject.Inject

data class WorkoutUiState(
    val selectedDate: LocalDate = LocalDate.now(),
    val exercises: List<ExerciseDto> = emptyList(),
    val allSets: List<SetDto> = emptyList(),
    val selectedGroup: String = "All",
    val isLoading: Boolean = false,
    val errorMessage: String? = null
) {
    val setsForDate: List<SetDto>
        get() {
            val dateStr = selectedDate.format(DateTimeFormatter.ISO_LOCAL_DATE)
            return allSets.filter { it.Date == dateStr }
        }

    val muscleGroups: List<String>
        get() {
            val groups = exercises.map { it.GR }.distinct().sorted()
            return listOf("All") + groups
        }

    val filteredExercises: List<ExerciseDto>
        get() = if (selectedGroup == "All") exercises
        else exercises.filter { it.GR == selectedGroup }

    /** Days in the current week (Mon-Sun) that have at least one set. */
    val weekActivity: Map<java.time.DayOfWeek, Boolean>
        get() {
            val startOfWeek = selectedDate.with(java.time.DayOfWeek.MONDAY)
            val weekDates = (0L..6L).map { startOfWeek.plusDays(it) }
            val setDates = allSets.mapNotNull {
                runCatching { LocalDate.parse(it.Date, DateTimeFormatter.ISO_LOCAL_DATE) }.getOrNull()
            }.toSet()
            return weekDates.associate { it.dayOfWeek to (it in setDates) }
        }

    /**
     * Trailing 7 real calendar days ending today (today, yesterday, … 6 days ago),
     * regardless of which date the user is browsing.
     * Keyed by the actual LocalDate (not DayOfWeek) so ActivityDots can display them in order.
     */
    val trailingWeekActivity: Map<LocalDate, Boolean>
        get() {
            val today = LocalDate.now()
            val setDates = allSets.mapNotNull {
                runCatching { LocalDate.parse(it.Date, DateTimeFormatter.ISO_LOCAL_DATE) }.getOrNull()
            }.toSet()
            // oldest first so index 0 = 6 days ago, index 6 = today
            return (6L downTo 0L).map { today.minusDays(it) }
                .associate { it to (it in setDates) }
        }
}

@HiltViewModel
class WorkoutViewModel @Inject constructor(
    private val repository: PumpRepository
) : ViewModel() {

    private val _selectedDate = MutableStateFlow(LocalDate.now())
    private val _exercises = MutableStateFlow<List<ExerciseDto>>(emptyList())
    private val _allSets = MutableStateFlow<List<SetDto>>(emptyList())
    private val _selectedGroup = MutableStateFlow("All")
    private val _isLoading = MutableStateFlow(false)
    private val _errorMessage = MutableStateFlow<String?>(null)

    val uiState: StateFlow<WorkoutUiState> = combine(
        _selectedDate, _exercises, _allSets, _selectedGroup, _isLoading, _errorMessage
    ) { values ->
        @Suppress("UNCHECKED_CAST")
        WorkoutUiState(
            selectedDate = values[0] as LocalDate,
            exercises = values[1] as List<ExerciseDto>,
            allSets = values[2] as List<SetDto>,
            selectedGroup = values[3] as String,
            isLoading = values[4] as Boolean,
            errorMessage = values[5] as String?
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5000), WorkoutUiState())

    init {
        loadData()
    }

    fun loadData() {
        viewModelScope.launch {
            _isLoading.value = true
            _errorMessage.value = null

            repository.getExercises().onSuccess { _exercises.value = it }
                .onFailure { _errorMessage.value = it.message }

            repository.getSets().onSuccess { _allSets.value = it }
                .onFailure { _errorMessage.value = it.message }

            _isLoading.value = false
        }
    }

    fun selectDate(date: LocalDate) {
        _selectedDate.value = date
    }

    fun selectGroup(group: String) {
        _selectedGroup.value = group
    }

    fun previousDay() {
        _selectedDate.value = _selectedDate.value.minusDays(1)
    }

    fun nextDay() {
        _selectedDate.value = _selectedDate.value.plusDays(1)
    }

    fun goToToday() {
        _selectedDate.value = LocalDate.now()
    }

    fun addSet(exerciseName: String, weight: String, reps: Int) {
        viewModelScope.launch {
            val dateStr = _selectedDate.value.format(DateTimeFormatter.ISO_LOCAL_DATE)
            val exercise = _exercises.value.find { it.NAME == exerciseName } ?: return@launch
            val currentSets = _allSets.value.filter { it.Date == dateStr }

            val newSet = SetDto(
                Date = dateStr,
                Name = exerciseName,
                Color = exercise.COLOR,
                WorkoutColor = exercise.COLOR,
                Weight = weight,
                Reps = reps
            )
            val updatedSets = currentSets + newSet
            repository.putSetsByDate(dateStr, updatedSets)
                .onSuccess { loadData() }
                .onFailure { _errorMessage.value = it.message }
        }
    }

    fun deleteSet(set: SetDto) {
        viewModelScope.launch {
            val dateStr = set.Date
            val currentSets = _allSets.value.filter { it.Date == dateStr }
            val updatedSets = currentSets.filterNot { it.ID == set.ID }
            repository.putSetsByDate(dateStr, updatedSets)
                .onSuccess { loadData() }
                .onFailure { _errorMessage.value = it.message }
        }
    }
}
