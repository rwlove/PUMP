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

data class StatsUiState(
    val exercises: List<ExerciseDto> = emptyList(),
    val allSets: List<SetDto> = emptyList(),
    val bodyWeights: List<BodyWeightDto> = emptyList(),
    val selectedExercise: String = "",
    val isLoading: Boolean = false,
    val errorMessage: String? = null
) {
    val mostPerformed: ExerciseStat?
        get() {
            val counts = allSets.groupingBy { it.Name }.eachCount()
            val max = counts.maxByOrNull { it.value }
            return max?.let { ExerciseStat(it.key, it.value) }
        }

    val leastPerformed: ExerciseStat?
        get() {
            val counts = allSets.groupingBy { it.Name }.eachCount()
            val min = counts.minByOrNull { it.value }
            return min?.let { ExerciseStat(it.key, it.value) }
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

    val exerciseNames: List<String>
        get() = allSets.map { it.Name }.distinct().sorted()

    val volumeData: List<VolumePoint>
        get() {
            if (selectedExercise.isEmpty()) return emptyList()
            val filtered = allSets.filter { it.Name == selectedExercise }
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
    private val _isLoading = MutableStateFlow(false)
    private val _errorMessage = MutableStateFlow<String?>(null)

    val uiState: StateFlow<StatsUiState> = combine(
        _exercises, _allSets, _bodyWeights, _selectedExercise, _isLoading, _errorMessage
    ) { values ->
        @Suppress("UNCHECKED_CAST")
        StatsUiState(
            exercises = values[0] as List<ExerciseDto>,
            allSets = values[1] as List<SetDto>,
            bodyWeights = values[2] as List<BodyWeightDto>,
            selectedExercise = values[3] as String,
            isLoading = values[4] as Boolean,
            errorMessage = values[5] as String?
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

            repository.getSets().onSuccess {
                _allSets.value = it
                if (_selectedExercise.value.isEmpty() && it.isNotEmpty()) {
                    _selectedExercise.value = it.first().Name
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
}
