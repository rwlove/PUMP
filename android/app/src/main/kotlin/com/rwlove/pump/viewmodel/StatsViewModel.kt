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

/** One set's data within a day, for stacked bar chart tooltip */
data class VolumeSetDetail(
    val setNumber: Int,
    val weight: Double,
    val reps: Int,
    val volume: Double,
    val bodyWeightUsed: Boolean
)

/** All sets for a date, for stacked bar chart */
data class VolumeDayData(
    val date: LocalDate,
    val sets: List<VolumeSetDetail>
) {
    val totalVolume: Double get() = sets.sumOf { it.volume }
}

data class HeatmapDay(val date: LocalDate, val count: Int)

data class BodyWeightPoint(val date: LocalDate, val weight: Double)

/** Pie chart slice data */
data class PieSlice(val name: String, val count: Int, val color: String)

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
    val selectedPeriod: StatsPeriod = StatsPeriod.WEEK,
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

    val totalSets: Int get() = setsInPeriod.size

    val activeDays: Int get() = setsInPeriod.map { it.Date }.distinct().size

    /** Pie chart data: exercise distribution in period */
    val pieData: List<PieSlice>
        get() {
            val counts = setsInPeriod.groupingBy { it.Name }.eachCount()
            val colorMap = exercises.associate { it.NAME to it.COLOR }
            return counts.entries
                .sortedByDescending { it.value }
                .map { (name, count) ->
                    PieSlice(name, count, colorMap[name] ?: "#2780e3")
                }
        }

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

    /** Exercise color map */
    val exerciseColorMap: Map<String, String>
        get() = exercises.associate { it.NAME to it.COLOR }

    /** Exercise group map */
    val exerciseGroupMap: Map<String, String>
        get() = exercises.associate { it.NAME to it.GR }

    /**
     * Stacked volume data per day for the selected exercise.
     * Each day has a list of sets with volume details.
     */
    val volumeDayData: List<VolumeDayData>
        get() {
            if (selectedExercise.isEmpty()) return emptyList()
            val filtered = setsInPeriod.filter { it.Name == selectedExercise }
            val byDate = filtered.groupBy { it.Date }

            // Check if exercise is in a "leg" group
            val group = exerciseGroupMap[selectedExercise]?.lowercase() ?: ""
            val isLegs = group.contains("leg")

            // Sort body weights for lookup
            val sortedBw = bodyWeights
                .mapNotNull { bw ->
                    val date = runCatching { LocalDate.parse(bw.DATE, DateTimeFormatter.ISO_LOCAL_DATE) }.getOrNull()
                    val weight = bw.WEIGHT.toDoubleOrNull()
                    if (date != null && weight != null) date to weight else null
                }
                .sortedBy { it.first }

            return byDate.mapNotNull { (dateStr, sets) ->
                val date = runCatching {
                    LocalDate.parse(dateStr, DateTimeFormatter.ISO_LOCAL_DATE)
                }.getOrNull() ?: return@mapNotNull null

                val setDetails = sets.mapIndexed { idx, set ->
                    var w = set.Weight.toDoubleOrNull() ?: 0.0
                    var bodyWeightUsed = false
                    if (isLegs && w == 0.0) {
                        // Find nearest body weight on or before this date
                        val bw = sortedBw.lastOrNull { it.first <= date }?.second
                        if (bw != null) {
                            w = bw
                            bodyWeightUsed = true
                        }
                    }
                    val r = set.Reps
                    VolumeSetDetail(
                        setNumber = idx + 1,
                        weight = w,
                        reps = r,
                        volume = w * r,
                        bodyWeightUsed = bodyWeightUsed
                    )
                }
                VolumeDayData(date, setDetails)
            }.sortedBy { it.date }
        }

    val volumeData: List<VolumePoint>
        get() = volumeDayData.map { VolumePoint(it.date, it.totalVolume) }

    /** Body weight data filtered by the selected period. */
    val bodyWeightData: List<BodyWeightPoint>
        get() {
            val start = periodStart
            return bodyWeights.mapNotNull { bw ->
                val date = runCatching {
                    LocalDate.parse(bw.DATE, DateTimeFormatter.ISO_LOCAL_DATE)
                }.getOrNull() ?: return@mapNotNull null
                if (start != null && date < start) return@mapNotNull null
                val weight = bw.WEIGHT.toDoubleOrNull() ?: return@mapNotNull null
                BodyWeightPoint(date, weight)
            }.sortedBy { it.date }
        }
}

@HiltViewModel
class StatsViewModel @Inject constructor(
    private val repository: PumpRepository
) : ViewModel() {

    private val _exercises = MutableStateFlow<List<ExerciseDto>>(emptyList())
    private val _allSets = MutableStateFlow<List<SetDto>>(emptyList())
    private val _bodyWeights = MutableStateFlow<List<BodyWeightDto>>(emptyList())
    private val _selectedExercise = MutableStateFlow("")
    private val _selectedPeriod = MutableStateFlow(StatsPeriod.WEEK)
    private val _isLoading = MutableStateFlow(false)
    private val _errorMessage = MutableStateFlow<String?>(null)

    val uiState: StateFlow<StatsUiState> = combine(
        _exercises, _allSets, _bodyWeights, _selectedExercise, _selectedPeriod, _isLoading, _errorMessage
    ) { values ->
        @Suppress("UNCHECKED_CAST")
        StatsUiState(
            exercises = values[0] as List<ExerciseDto>,
            allSets = values[1] as List<SetDto>,
            bodyWeights = values[2] as List<BodyWeightDto>,
            selectedExercise = values[3] as String,
            selectedPeriod = values[4] as StatsPeriod,
            isLoading = values[5] as Boolean,
            errorMessage = values[6] as String?
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
