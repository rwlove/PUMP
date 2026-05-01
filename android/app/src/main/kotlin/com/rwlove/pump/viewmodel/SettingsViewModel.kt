package com.rwlove.pump.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.rwlove.pump.data.preferences.AppPreferences
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

data class SettingsUiState(
    val apiUrl: String = AppPreferences.DEFAULT_API_URL,
    val apiKey: String = AppPreferences.DEFAULT_API_KEY,
    val isSaved: Boolean = false
)

@HiltViewModel
class SettingsViewModel @Inject constructor(
    private val appPreferences: AppPreferences
) : ViewModel() {

    private val _isSaved = MutableStateFlow(false)

    val uiState: StateFlow<SettingsUiState> = combine(
        appPreferences.apiUrl,
        appPreferences.apiKey,
        _isSaved
    ) { url, key, saved ->
        SettingsUiState(apiUrl = url, apiKey = key, isSaved = saved)
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5000), SettingsUiState())

    fun saveSettings(url: String, key: String) {
        viewModelScope.launch {
            appPreferences.setApiUrl(url)
            appPreferences.setApiKey(key)
            _isSaved.value = true
        }
    }

    fun clearSavedFlag() {
        _isSaved.value = false
    }
}
