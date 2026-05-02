package com.rwlove.pump.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.rwlove.pump.data.api.ExerciseDto
import com.rwlove.pump.data.repository.PumpRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

data class ExerciseFormState(
    val id: Int = 0,
    val group: String = "",
    val place: String = "0",
    val name: String = "",
    val descr: String = "",
    val image: String = "",
    val color: String = "#2780e3",
    val weight: String = "0",
    val reps: String = "0"
)

data class ExerciseEditUiState(
    val exercises: List<ExerciseDto> = emptyList(),
    val editingExercise: ExerciseFormState? = null,
    val isLoading: Boolean = false,
    val errorMessage: String? = null,
    val saveSuccess: Boolean = false
)

@HiltViewModel
class ExerciseViewModel @Inject constructor(
    private val repository: PumpRepository
) : ViewModel() {

    private val _exercises = MutableStateFlow<List<ExerciseDto>>(emptyList())
    private val _editingExercise = MutableStateFlow<ExerciseFormState?>(null)
    private val _isLoading = MutableStateFlow(false)
    private val _errorMessage = MutableStateFlow<String?>(null)
    private val _saveSuccess = MutableStateFlow(false)

    val uiState: StateFlow<ExerciseEditUiState> = combine(
        _exercises, _editingExercise, _isLoading, _errorMessage, _saveSuccess
    ) { exercises, editing, loading, error, success ->
        ExerciseEditUiState(
            exercises = exercises,
            editingExercise = editing,
            isLoading = loading,
            errorMessage = error,
            saveSuccess = success
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5000), ExerciseEditUiState())

    init {
        loadExercises()
    }

    fun loadExercises() {
        viewModelScope.launch {
            _isLoading.value = true
            repository.getExercises()
                .onSuccess { _exercises.value = it }
                .onFailure { _errorMessage.value = it.message }
            _isLoading.value = false
        }
    }

    fun startNewExercise() {
        _editingExercise.value = ExerciseFormState()
        _saveSuccess.value = false
    }

    fun editExercise(ex: ExerciseDto) {
        _editingExercise.value = ExerciseFormState(
            id = ex.ID,
            group = ex.GR,
            place = ex.PLACE,
            name = ex.NAME,
            descr = ex.DESCR,
            image = ex.IMAGE,
            color = ex.COLOR,
            weight = ex.WEIGHT,
            reps = ex.REPS.toString()
        )
        _saveSuccess.value = false
    }

    fun cancelEdit() {
        _editingExercise.value = null
        _saveSuccess.value = false
    }

    fun saveExercise(form: ExerciseFormState) {
        viewModelScope.launch {
            _isLoading.value = true
            val dto = ExerciseDto(
                ID = form.id,
                GR = form.group,
                PLACE = form.place,
                NAME = form.name,
                DESCR = form.descr,
                IMAGE = form.image,
                COLOR = form.color,
                WEIGHT = form.weight,
                REPS = form.reps.toIntOrNull() ?: 0
            )
            val result = if (form.id == 0) {
                repository.createExercise(dto)
            } else {
                repository.updateExercise(form.id, dto)
            }
            result
                .onSuccess {
                    _saveSuccess.value = true
                    _editingExercise.value = null
                    loadExercises()
                }
                .onFailure { _errorMessage.value = it.message }
            _isLoading.value = false
        }
    }

    fun deleteExercise(id: Int) {
        viewModelScope.launch {
            _isLoading.value = true
            repository.deleteExercise(id)
                .onSuccess {
                    _editingExercise.value = null
                    loadExercises()
                }
                .onFailure { _errorMessage.value = it.message }
            _isLoading.value = false
        }
    }

    fun clearError() {
        _errorMessage.value = null
    }
}
