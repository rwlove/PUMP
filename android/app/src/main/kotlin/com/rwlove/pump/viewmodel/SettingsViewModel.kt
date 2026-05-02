package com.rwlove.pump.viewmodel

import androidx.lifecycle.ViewModel
import androidx.lifecycle.viewModelScope
import com.rwlove.pump.data.api.ConfigDto
import com.rwlove.pump.data.preferences.AppPreferences
import com.rwlove.pump.data.repository.PumpRepository
import dagger.hilt.android.lifecycle.HiltViewModel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharingStarted
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.stateIn
import kotlinx.coroutines.launch
import javax.inject.Inject

/**
 * Status of the most recent save+connectivity attempt.
 */
sealed interface SaveStatus {
    data object Idle : SaveStatus
    data object Testing : SaveStatus
    data object Success : SaveStatus
    data class Failure(val message: String) : SaveStatus
}

data class SettingsUiState(
    val apiUrl: String = AppPreferences.DEFAULT_API_URL,
    val apiKey: String = AppPreferences.DEFAULT_API_KEY,
    val saveStatus: SaveStatus = SaveStatus.Idle,
    val serverConfig: ConfigDto = ConfigDto(),
    val configLoaded: Boolean = false,
    val configSaveStatus: SaveStatus = SaveStatus.Idle
)

@HiltViewModel
class SettingsViewModel @Inject constructor(
    private val appPreferences: AppPreferences,
    private val repository: PumpRepository
) : ViewModel() {

    private val _saveStatus = MutableStateFlow<SaveStatus>(SaveStatus.Idle)
    private val _serverConfig = MutableStateFlow(ConfigDto())
    private val _configLoaded = MutableStateFlow(false)
    private val _configSaveStatus = MutableStateFlow<SaveStatus>(SaveStatus.Idle)

    val uiState: StateFlow<SettingsUiState> = combine(
        appPreferences.apiUrl,
        appPreferences.apiKey,
        _saveStatus,
        combine(_serverConfig, _configLoaded, _configSaveStatus) { cfg, loaded, status ->
            Triple(cfg, loaded, status)
        }
    ) { url, key, status, (cfg, loaded, cfgStatus) ->
        SettingsUiState(
            apiUrl = url,
            apiKey = key,
            saveStatus = status,
            serverConfig = cfg,
            configLoaded = loaded,
            configSaveStatus = cfgStatus
        )
    }.stateIn(viewModelScope, SharingStarted.WhileSubscribed(5000), SettingsUiState())

    init {
        loadConfig()
    }

    private fun loadConfig() {
        viewModelScope.launch {
            repository.getConfig()
                .onSuccess {
                    _serverConfig.value = it
                    _configLoaded.value = true
                }
                .onFailure { /* config endpoint may not be reachable yet */ }
        }
    }

    fun saveSettings(url: String, key: String) {
        viewModelScope.launch {
            _saveStatus.value = SaveStatus.Testing

            // Persist the new values so the OkHttp interceptors pick them up immediately.
            appPreferences.setApiUrl(url)
            appPreferences.setApiKey(key)

            // Now probe the server with the freshly persisted credentials.
            repository.testConnection()
                .onSuccess {
                    _saveStatus.value = SaveStatus.Success
                    // Also load config after successful connection
                    loadConfig()
                }
                .onFailure { error ->
                    val msg = error.message ?: "Unknown error"
                    val friendly = when {
                        msg.contains("ECONNREFUSED", ignoreCase = true) ||
                        msg.contains("Connection refused", ignoreCase = true) ->
                            "Could not reach server: connection refused"
                        msg.contains("Unable to resolve host", ignoreCase = true) ||
                        msg.contains("UnknownHost", ignoreCase = true) ->
                            "Could not reach server: unknown host"
                        msg.contains("timeout", ignoreCase = true) ->
                            "Could not reach server: connection timed out"
                        msg.contains("401") || msg.contains("Invalid API key") ->
                            "Invalid API key -- check your key and try again"
                        else -> "Could not reach server: $msg"
                    }
                    _saveStatus.value = SaveStatus.Failure(friendly)
                }
        }
    }

    fun saveConfig(config: ConfigDto) {
        viewModelScope.launch {
            _configSaveStatus.value = SaveStatus.Testing
            repository.putConfig(config)
                .onSuccess {
                    _serverConfig.value = config
                    _configSaveStatus.value = SaveStatus.Success
                }
                .onFailure {
                    _configSaveStatus.value = SaveStatus.Failure(it.message ?: "Failed to save config")
                }
        }
    }

    fun clearSaveStatus() {
        _saveStatus.value = SaveStatus.Idle
    }

    fun clearConfigSaveStatus() {
        _configSaveStatus.value = SaveStatus.Idle
    }
}
