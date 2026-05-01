package com.rwlove.pump.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.rwlove.pump.data.api.BodyWeightDto
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

data class WeightUiState(
    val entries: List<BodyWeightDto> = emptyList(),
    val isLoading: Boolean = false,
    val errorMessage: String? = null
)

@HiltViewModel
class WeightViewModel @Inject constructor(
    private val repository: PumpRepository
) : ViewModel() {

    private val _entries = MutableStateFlow<List<BodyWeightDto>>(emptyList())
    private val _isLoading = MutableStateFlow(false)
    private val _errorMessage = MutableStateFlow<String?>(null)

    val uiState: StateFlow<WeightUiState> = combine(
        _entries, _isLoading, _errorMessage
    ) { entries, loading, error ->
        WeightUiState(
            entries = entries as List<BodyWeightDto>,
            isLoading = loading as Boolean,
            errorMessage = error as String?
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5000), WeightUiState())

    init {
        loadData()
    }

    fun loadData() {
        viewModelScope.launch {
            _isLoading.value = true
            _errorMessage.value = null
            repository.getWeight()
                .onSuccess { _entries.value = it }
                .onFailure { _errorMessage.value = it.message }
            _isLoading.value = false
        }
    }

    fun addWeight(date: LocalDate, weight: String) {
        viewModelScope.launch {
            val dateStr = date.format(DateTimeFormatter.ISO_LOCAL_DATE)
            val dto = BodyWeightDto(DATE = dateStr, WEIGHT = weight)
            repository.postWeight(dto)
                .onSuccess { loadData() }
                .onFailure { _errorMessage.value = it.message }
        }
    }

    fun deleteWeight(id: Int) {
        viewModelScope.launch {
            repository.deleteWeight(id)
                .onSuccess { loadData() }
                .onFailure { _errorMessage.value = it.message }
        }
    }
}
