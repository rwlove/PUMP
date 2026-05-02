package com.rwlove.pump.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.rwlove.pump.data.api.BodyWeightDto
import com.rwlove.pump.data.api.ConfigDto
import com.rwlove.pump.data.api.ExerciseDto
import com.rwlove.pump.data.api.SetDto
import com.rwlove.pump.data.repository.PumpRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import java.time.LocalDate
import java.time.format.DateTimeFormatter
import javax.inject.Inject

enum class AutoSaveStatus { IDLE, SAVING, SAVED, ERROR }

data class WorkoutUiState(
    val selectedDate: LocalDate = LocalDate.now(),
    val exercises: List<ExerciseDto> = emptyList(),
    val allSets: List<SetDto> = emptyList(),
    val selectedGroup: String? = null,
    val searchQuery: String = "",
    val isLoading: Boolean = false,
    val errorMessage: String? = null,
    val saveStatus: AutoSaveStatus = AutoSaveStatus.IDLE,
    val config: ConfigDto = ConfigDto()
) {
    val setsForDate: List<SetDto>
        get() {
            val dateStr = selectedDate.format(DateTimeFormatter.ISO_LOCAL_DATE)
            return allSets.filter { it.Date == dateStr }
        }

    val muscleGroups: List<String>
        get() = exercises.map { it.GR }.distinct().sorted()

    val filteredExercises: List<ExerciseDto>
        get() {
            val groupFiltered = if (selectedGroup == null) emptyList()
            else exercises.filter { it.GR == selectedGroup }
            return if (searchQuery.isBlank()) groupFiltered
            else groupFiltered.filter { it.NAME.contains(searchQuery, ignoreCase = true) }
        }

    /**
     * Trailing 7 real calendar days ending today (today, yesterday, ... 6 days ago),
     * regardless of which date the user is browsing.
     * Keyed by the actual LocalDate so ActivityDots can display them in order.
     */
    val trailingWeekActivity: Map<LocalDate, Boolean>
        get() {
            val today = LocalDate.now()
            val setDates = allSets.mapNotNull {
                runCatching { LocalDate.parse(it.Date, DateTimeFormatter.ISO_LOCAL_DATE) }.getOrNull()
            }.toSet()
            return (6L downTo 0L).map { today.minusDays(it) }
                .associate { it to (it in setDates) }
        }

    /** Friendly date string like "Saturday, May 2nd 2026" */
    val friendlyDate: String
        get() {
            val day = selectedDate.dayOfMonth
            val suffix = when {
                day in 11..13 -> "th"
                day % 10 == 1 -> "st"
                day % 10 == 2 -> "nd"
                day % 10 == 3 -> "rd"
                else -> "th"
            }
            val formatter = DateTimeFormatter.ofPattern("EEEE, MMMM")
            return "${selectedDate.format(formatter)} $day$suffix ${selectedDate.year}"
        }
}

@HiltViewModel
class WorkoutViewModel @Inject constructor(
    private val repository: PumpRepository
) : ViewModel() {

    private val _selectedDate = MutableStateFlow(LocalDate.now())
    private val _exercises = MutableStateFlow<List<ExerciseDto>>(emptyList())
    private val _allSets = MutableStateFlow<List<SetDto>>(emptyList())
    private val _selectedGroup = MutableStateFlow<String?>(null)
    private val _searchQuery = MutableStateFlow("")
    private val _isLoading = MutableStateFlow(false)
    private val _errorMessage = MutableStateFlow<String?>(null)
    private val _saveStatus = MutableStateFlow(AutoSaveStatus.IDLE)
    private val _config = MutableStateFlow(ConfigDto())

    private var autosaveJob: Job? = null

    val uiState: StateFlow<WorkoutUiState> = combine(
        _selectedDate, _exercises, _allSets, _selectedGroup,
        combine(_searchQuery, _isLoading, _errorMessage, _saveStatus, _config) { s, l, e, ss, c ->
            listOf(s, l, e, ss, c)
        }
    ) { date, exercises, allSets, selectedGroup, rest ->
        @Suppress("UNCHECKED_CAST")
        WorkoutUiState(
            selectedDate = date,
            exercises = exercises,
            allSets = allSets,
            selectedGroup = selectedGroup,
            searchQuery = rest[0] as String,
            isLoading = rest[1] as Boolean,
            errorMessage = rest[2] as String?,
            saveStatus = rest[3] as AutoSaveStatus,
            config = rest[4] as ConfigDto
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

            repository.getConfig().onSuccess { _config.value = it }
                .onFailure { /* config is optional, use defaults */ }

            _isLoading.value = false
        }
    }

    fun selectDate(date: LocalDate) {
        _selectedDate.value = date
    }

    fun selectGroup(group: String?) {
        _selectedGroup.value = group
        _searchQuery.value = ""
    }

    fun clearGroup() {
        _selectedGroup.value = null
        _searchQuery.value = ""
    }

    fun setSearchQuery(query: String) {
        _searchQuery.value = query
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
            // Update local state immediately for responsiveness
            _allSets.value = _allSets.value.filter { it.Date != dateStr } + updatedSets
            scheduleAutosave(dateStr, updatedSets)
        }
    }

    fun updateSet(index: Int, weight: String, reps: Int) {
        viewModelScope.launch {
            val dateStr = _selectedDate.value.format(DateTimeFormatter.ISO_LOCAL_DATE)
            val currentSets = _allSets.value.filter { it.Date == dateStr }.toMutableList()
            if (index < 0 || index >= currentSets.size) return@launch
            currentSets[index] = currentSets[index].copy(Weight = weight, Reps = reps)
            _allSets.value = _allSets.value.filter { it.Date != dateStr } + currentSets
            scheduleAutosave(dateStr, currentSets)
        }
    }

    fun updateSetColor(index: Int, color: String) {
        viewModelScope.launch {
            val dateStr = _selectedDate.value.format(DateTimeFormatter.ISO_LOCAL_DATE)
            val currentSets = _allSets.value.filter { it.Date == dateStr }.toMutableList()
            if (index < 0 || index >= currentSets.size) return@launch
            currentSets[index] = currentSets[index].copy(WorkoutColor = color)
            _allSets.value = _allSets.value.filter { it.Date != dateStr } + currentSets
            scheduleAutosave(dateStr, currentSets)
        }
    }

    fun deleteSet(set: SetDto) {
        viewModelScope.launch {
            val dateStr = set.Date
            val currentSets = _allSets.value.filter { it.Date == dateStr }
            val updatedSets = currentSets.filterNot { it.ID == set.ID && it.Name == set.Name && it.Weight == set.Weight && it.Reps == set.Reps }
            // If filtering by ID didn't help (new sets have ID=0), remove first match
            val finalSets = if (updatedSets.size == currentSets.size) {
                val mutable = currentSets.toMutableList()
                val idx = mutable.indexOfFirst { it.Name == set.Name && it.Weight == set.Weight && it.Reps == set.Reps }
                if (idx >= 0) mutable.removeAt(idx)
                mutable
            } else updatedSets
            _allSets.value = _allSets.value.filter { it.Date != dateStr } + finalSets
            scheduleAutosave(dateStr, finalSets)
        }
    }

    fun logWeight(date: LocalDate, weight: String) {
        viewModelScope.launch {
            val dateStr = date.format(DateTimeFormatter.ISO_LOCAL_DATE)
            val dto = BodyWeightDto(DATE = dateStr, WEIGHT = weight)
            repository.postWeight(dto)
                .onFailure { _errorMessage.value = it.message }
        }
    }

    private fun scheduleAutosave(dateStr: String, sets: List<SetDto>) {
        autosaveJob?.cancel()
        _saveStatus.value = AutoSaveStatus.SAVING
        autosaveJob = viewModelScope.launch {
            delay(600) // 600ms debounce like the web
            repository.putSetsByDate(dateStr, sets)
                .onSuccess {
                    _saveStatus.value = AutoSaveStatus.SAVED
                    // Reload to get server-assigned IDs
                    repository.getSets().onSuccess { _allSets.value = it }
                    delay(2000)
                    if (_saveStatus.value == AutoSaveStatus.SAVED) {
                        _saveStatus.value = AutoSaveStatus.IDLE
                    }
                }
                .onFailure {
                    _saveStatus.value = AutoSaveStatus.ERROR
                    _errorMessage.value = it.message
                }
        }
    }
}
