package com.rwlove.pump.ui.screens

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.rwlove.pump.BuildConfig
import com.rwlove.pump.viewmodel.SaveStatus
import com.rwlove.pump.viewmodel.SettingsViewModel

@Composable
fun SettingsScreen(
    viewModel: SettingsViewModel = hiltViewModel()
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()

    // rememberSaveable keeps values across recompositions and process death.
    // We seed from the persisted prefs only once on first composition (initialised
    // flag prevents the DataStore flow from overwriting user edits mid-type).
    var initialised by rememberSaveable { mutableStateOf(false) }
    var apiUrl by rememberSaveable { mutableStateOf("") }
    var apiKey by rememberSaveable { mutableStateOf("") }

    // Seed fields from prefs on the very first emission only.
    LaunchedEffect(state.apiUrl, state.apiKey) {
        if (!initialised) {
            apiUrl = state.apiUrl
            apiKey = state.apiKey
            initialised = true
        }
    }

    val isTesting = state.saveStatus is SaveStatus.Testing

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(16.dp)
    ) {
        Spacer(modifier = Modifier.height(8.dp))

        Text(
            text = "Settings",
            style = MaterialTheme.typography.titleLarge
        )

        Spacer(modifier = Modifier.height(24.dp))

        OutlinedTextField(
            value = apiUrl,
            onValueChange = {
                apiUrl = it
                viewModel.clearSaveStatus()
            },
            label = { Text("API URL") },
            placeholder = { Text("http://192.168.1.10:8080") },
            singleLine = true,
            enabled = !isTesting,
            modifier = Modifier.fillMaxWidth()
        )

        Spacer(modifier = Modifier.height(16.dp))

        OutlinedTextField(
            value = apiKey,
            onValueChange = {
                apiKey = it
                viewModel.clearSaveStatus()
            },
            label = { Text("API Key") },
            placeholder = { Text("Optional") },
            singleLine = true,
            enabled = !isTesting,
            modifier = Modifier.fillMaxWidth()
        )

        Spacer(modifier = Modifier.height(24.dp))

        Button(
            onClick = { viewModel.saveSettings(apiUrl, apiKey) },
            enabled = !isTesting,
            modifier = Modifier.fillMaxWidth()
        ) {
            if (isTesting) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(18.dp),
                        strokeWidth = 2.dp,
                        color = MaterialTheme.colorScheme.onPrimary
                    )
                    Spacer(modifier = Modifier.size(8.dp))
                    Text("Testing connection…")
                }
            } else {
                Text("Save")
            }
        }

        // Inline status message directly below the Save button — always visible.
        when (val status = state.saveStatus) {
            is SaveStatus.Success -> {
                Spacer(modifier = Modifier.height(12.dp))
                Text(
                    text = "Settings saved — server is reachable.",
                    color = MaterialTheme.colorScheme.primary,
                    style = MaterialTheme.typography.bodyMedium
                )
            }
            is SaveStatus.Failure -> {
                Spacer(modifier = Modifier.height(12.dp))
                Text(
                    text = status.message,
                    color = MaterialTheme.colorScheme.error,
                    style = MaterialTheme.typography.bodyMedium
                )
            }
            else -> {
                // Idle or Testing: no status text shown.
            }
        }

        Spacer(modifier = Modifier.weight(1f))

        Text(
            text = "PUMP ${BuildConfig.VERSION_NAME}",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.align(Alignment.CenterHorizontally)
        )

        Spacer(modifier = Modifier.height(16.dp))
    }
}
