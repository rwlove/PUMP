package com.rwlove.pump.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.rwlove.pump.data.api.BodyWeightDto
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

data class ExerciseStat(val name: String, val count: Int)

data class VolumePoint(val date: LocalDate, val volume: Double)

data class HeatmapDay(val date: LocalDate, val count: Int)

data class BodyWeightPoint(val date: LocalDate, val weight: Double)

enum class StatsPeriod(val label: String) {
    WEEK("Week"),
    MONTH("Month"),
    YEAR("Year"),
    ALL("All time")
}

data class StatsUiState(
    val exercises: List<ExerciseDto> = emptyList(),
    val allSets: List<SetDto> = emptyList(),
    val bodyWeights: List<BodyWeightDto> = emptyList(),
    val selectedExercise: String = "",
    val selectedPeriod: StatsPeriod = StatsPeriod.ALL,
    val isLoading: Boolean = false,
    val errorMessage: String? = null
) {
    private val periodStart: LocalDate?
        get() {
            val today = LocalDate.now()
            return when (selectedPeriod) {
                StatsPeriod.WEEK -> today.minusDays(6)
                StatsPeriod.MONTH -> today.minusDays(29)
                StatsPeriod.YEAR -> today.minusDays(364)
                StatsPeriod.ALL -> null
            }
        }

    /** Sets filtered to the selected period. */
    val setsInPeriod: List<SetDto>
        get() {
            val start = periodStart ?: return allSets
            val startStr = start.format(DateTimeFormatter.ISO_LOCAL_DATE)
            return allSets.filter { it.Date >= startStr }
        }

    val mostPerformed: ExerciseStat?
        get() {
            val counts = setsInPeriod.groupingBy { it.Name }.eachCount()
            val max = counts.maxByOrNull { it.value }
            return max?.let { ExerciseStat(it.key, it.value) }
        }

    val leastPerformed: ExerciseStat?
        get() {
            val counts = setsInPeriod.groupingBy { it.Name }.eachCount()
            val min = counts.minByOrNull { it.value }
            return min?.let { ExerciseStat(it.key, it.value) }
        }

    /** Total number of sets in the selected period. */
    val totalSets: Int
        get() = setsInPeriod.size

    /** Number of unique dates with at least one set in the selected period. */
    val activeDays: Int
        get() = setsInPeriod.map { it.Date }.distinct().size

    val heatmapData: List<HeatmapDay>
        get() {
            val today = LocalDate.now()
            val startDate = today.minusDays(364)
            val setsPerDay = allSets.groupBy { it.Date }.mapValues { it.value.size }
            return (0L..364L).map { offset ->
                val date = startDate.plusDays(offset)
                val dateStr = date.format(DateTimeFormatter.ISO_LOCAL_DATE)
                HeatmapDay(date, setsPerDay[dateStr] ?: 0)
            }
        }

    /**
     * Exercise names sorted by frequency (set count) in the current period, descending.
     */
    val exerciseNames: List<String>
        get() {
            val counts = setsInPeriod.groupingBy { it.Name }.eachCount()
            return counts.entries
                .sortedByDescending { it.value }
                .map { it.key }
        }

    val volumeData: List<VolumePoint>
        get() {
            if (selectedExercise.isEmpty()) return emptyList()
            val filtered = setsInPeriod.filter { it.Name == selectedExercise }
            val byDate = filtered.groupBy { it.Date }
            return byDate.mapNotNull { (dateStr, sets) ->
                val date = runCatching {
                    LocalDate.parse(dateStr, DateTimeFormatter.ISO_LOCAL_DATE)
                }.getOrNull() ?: return@mapNotNull null
                val volume = sets.sumOf { set ->
                    val w = set.Weight.toDoubleOrNull() ?: 0.0
                    w * set.Reps
                }
                VolumePoint(date, volume)
            }.sortedBy { it.date }
        }

    val bodyWeightData: List<BodyWeightPoint>
        get() = bodyWeights.mapNotNull { bw ->
            val date = runCatching {
                LocalDate.parse(bw.DATE, DateTimeFormatter.ISO_LOCAL_DATE)
            }.getOrNull() ?: return@mapNotNull null
            val weight = bw.WEIGHT.toDoubleOrNull() ?: return@mapNotNull null
            BodyWeightPoint(date, weight)
        }.sortedBy { it.date }
}

@HiltViewModel
class StatsViewModel @Inject constructor(
    private val repository: PumpRepository
) : ViewModel() {

    private val _exercises = MutableStateFlow<List<ExerciseDto>>(emptyList())
    private val _allSets = MutableStateFlow<List<SetDto>>(emptyList())
    private val _bodyWeights = MutableStateFlow<List<BodyWeightDto>>(emptyList())
    private val _selectedExercise = MutableStateFlow("")
    private val _selectedPeriod = MutableStateFlow(StatsPeriod.ALL)
    private val _isLoading = MutableStateFlow(false)
    private val _errorMessage = MutableStateFlow<String?>(null)

    val uiState: StateFlow<StatsUiState> = combine(
        _exercises, _allSets, _bodyWeights, _selectedExercise, _selectedPeriod, _isLoading, _errorMessage
    ) { exercises, allSets, bodyWeights, selectedExercise, selectedPeriod, isLoading, errorMessage ->
        StatsUiState(
            exercises = exercises,
            allSets = allSets,
            bodyWeights = bodyWeights,
            selectedExercise = selectedExercise,
            selectedPeriod = selectedPeriod,
            isLoading = isLoading,
            errorMessage = errorMessage
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5000), StatsUiState())

    init {
        loadData()
    }

    fun loadData() {
        viewModelScope.launch {
            _isLoading.value = true
            _errorMessage.value = null

            repository.getExercises().onSuccess { _exercises.value = it }
                .onFailure { _errorMessage.value = it.message }

            repository.getSets().onSuccess { sets ->
                _allSets.value = sets
                // Auto-select the most frequent exercise in the current period
                if (_selectedExercise.value.isEmpty() && sets.isNotEmpty()) {
                    val mostFrequent = sets.groupingBy { it.Name }
                        .eachCount()
                        .maxByOrNull { it.value }
                        ?.key
                    _selectedExercise.value = mostFrequent ?: sets.first().Name
                }
            }.onFailure { _errorMessage.value = it.message }

            repository.getWeight().onSuccess { _bodyWeights.value = it }
                .onFailure { _errorMessage.value = it.message }

            _isLoading.value = false
        }
    }

    fun selectExercise(name: String) {
        _selectedExercise.value = name
    }

    fun selectPeriod(period: StatsPeriod) {
        _selectedPeriod.value = period
        // Re-select best exercise for new period if current selection has no data
        val currentSets = _allSets.value
        if (currentSets.isNotEmpty()) {
            val state = uiState.value.copy(selectedPeriod = period)
            if (state.setsInPeriod.none { it.Name == _selectedExercise.value }) {
                val mostFrequent = state.setsInPeriod.groupingBy { it.Name }
                    .eachCount()
                    .maxByOrNull { it.value }
                    ?.key
                if (mostFrequent != null) _selectedExercise.value = mostFrequent
            }
        }
    }
}
