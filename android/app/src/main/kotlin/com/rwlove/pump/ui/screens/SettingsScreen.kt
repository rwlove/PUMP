package com.rwlove.pump.ui.screens

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.ExposedDropdownMenuBox
import androidx.compose.material3.ExposedDropdownMenuDefaults
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.MenuAnchorType
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.rwlove.pump.BuildConfig
import com.rwlove.pump.data.api.ConfigDto
import com.rwlove.pump.viewmodel.SaveStatus
import com.rwlove.pump.viewmodel.SettingsViewModel

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    viewModel: SettingsViewModel = hiltViewModel()
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()

    // Connection settings
    var initialised by rememberSaveable { mutableStateOf(false) }
    var apiUrl by rememberSaveable { mutableStateOf("") }
    var apiKey by rememberSaveable { mutableStateOf("") }

    LaunchedEffect(state.apiUrl, state.apiKey) {
        if (!initialised) {
            apiUrl = state.apiUrl
            apiKey = state.apiKey
            initialised = true
        }
    }

    // Server config settings
    var configInitialised by rememberSaveable { mutableStateOf(false) }
    var colorMode by rememberSaveable { mutableStateOf("dark") }
    var pageStep by rememberSaveable { mutableStateOf("10") }
    var frequencyDays by rememberSaveable { mutableStateOf("30") }
    var displayDays by rememberSaveable { mutableIntStateOf(30) }
    var autoFill by rememberSaveable { mutableStateOf(true) }

    LaunchedEffect(state.configLoaded) {
        if (state.configLoaded && !configInitialised) {
            colorMode = state.serverConfig.Color
            pageStep = state.serverConfig.PageStep.toString()
            frequencyDays = state.serverConfig.FrequencyDays.toString()
            displayDays = state.serverConfig.DisplayDays
            autoFill = state.serverConfig.AutoFill
            configInitialised = true
        }
    }

    val isTesting = state.saveStatus is SaveStatus.Testing

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(16.dp)
            .verticalScroll(rememberScrollState())
    ) {
        Spacer(modifier = Modifier.height(8.dp))

        Text(
            text = "Settings",
            style = MaterialTheme.typography.titleLarge
        )

        Spacer(modifier = Modifier.height(16.dp))

        // Connection settings card
        Card(
            modifier = Modifier.fillMaxWidth(),
            colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant)
        ) {
            Column(modifier = Modifier.padding(16.dp)) {
                Text(
                    text = "Server Connection",
                    style = MaterialTheme.typography.titleMedium
                )

                Spacer(modifier = Modifier.height(12.dp))

                OutlinedTextField(
                    value = apiUrl,
                    onValueChange = {
                        apiUrl = it
                        viewModel.clearSaveStatus()
                    },
                    label = { Text("Server URL") },
                    placeholder = { Text("http://192.168.1.10:8080") },
                    singleLine = true,
                    enabled = !isTesting,
                    modifier = Modifier.fillMaxWidth()
                )

                Spacer(modifier = Modifier.height(12.dp))

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

                Spacer(modifier = Modifier.height(16.dp))

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
                            Text("Testing connection...")
                        }
                    } else {
                        Text("Test & Save")
                    }
                }

                when (val status = state.saveStatus) {
                    is SaveStatus.Success -> {
                        Spacer(modifier = Modifier.height(8.dp))
                        Text(
                            text = "Settings saved -- server is reachable.",
                            color = MaterialTheme.colorScheme.primary,
                            style = MaterialTheme.typography.bodySmall
                        )
                    }
                    is SaveStatus.Failure -> {
                        Spacer(modifier = Modifier.height(8.dp))
                        Text(
                            text = status.message,
                            color = MaterialTheme.colorScheme.error,
                            style = MaterialTheme.typography.bodySmall
                        )
                    }
                    else -> {}
                }
            }
        }

        Spacer(modifier = Modifier.height(16.dp))

        // Server config card
        Card(
            modifier = Modifier.fillMaxWidth(),
            colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant)
        ) {
            Column(modifier = Modifier.padding(16.dp)) {
                Text(
                    text = "Server Configuration",
                    style = MaterialTheme.typography.titleMedium
                )

                Spacer(modifier = Modifier.height(12.dp))

                // Color mode
                var colorExpanded by remember { mutableStateOf(false) }
                ExposedDropdownMenuBox(
                    expanded = colorExpanded,
                    onExpandedChange = { colorExpanded = it }
                ) {
                    OutlinedTextField(
                        value = if (colorMode == "dark") "Dark" else "Light",
                        onValueChange = {},
                        readOnly = true,
                        label = { Text("Color mode") },
                        trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = colorExpanded) },
                        modifier = Modifier
                            .fillMaxWidth()
                            .menuAnchor(MenuAnchorType.PrimaryNotEditable)
                    )
                    ExposedDropdownMenu(
                        expanded = colorExpanded,
                        onDismissRequest = { colorExpanded = false }
                    ) {
                        DropdownMenuItem(
                            text = { Text("Dark") },
                            onClick = { colorMode = "dark"; colorExpanded = false }
                        )
                        DropdownMenuItem(
                            text = { Text("Light") },
                            onClick = { colorMode = "light"; colorExpanded = false }
                        )
                    }
                }

                Spacer(modifier = Modifier.height(12.dp))

                // Display days
                var displayExpanded by remember { mutableStateOf(false) }
                val displayOptions = listOf(7 to "Weekly (7 days)", 30 to "Monthly (30 days)", 90 to "Quarterly (90 days)", 365 to "Annually (365 days)")
                ExposedDropdownMenuBox(
                    expanded = displayExpanded,
                    onExpandedChange = { displayExpanded = it }
                ) {
                    OutlinedTextField(
                        value = displayOptions.find { it.first == displayDays }?.second ?: "$displayDays days",
                        onValueChange = {},
                        readOnly = true,
                        label = { Text("Main page history") },
                        trailingIcon = { ExposedDropdownMenuDefaults.TrailingIcon(expanded = displayExpanded) },
                        modifier = Modifier
                            .fillMaxWidth()
                            .menuAnchor(MenuAnchorType.PrimaryNotEditable)
                    )
                    ExposedDropdownMenu(
                        expanded = displayExpanded,
                        onDismissRequest = { displayExpanded = false }
                    ) {
                        displayOptions.forEach { (value, label) ->
                            DropdownMenuItem(
                                text = { Text(label) },
                                onClick = { displayDays = value; displayExpanded = false }
                            )
                        }
                    }
                }

                Spacer(modifier = Modifier.height(12.dp))

                OutlinedTextField(
                    value = pageStep,
                    onValueChange = { pageStep = it },
                    label = { Text("Page step") },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth()
                )

                Spacer(modifier = Modifier.height(12.dp))

                OutlinedTextField(
                    value = frequencyDays,
                    onValueChange = { frequencyDays = it },
                    label = { Text("Exercise sort window (days)") },
                    keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Number),
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth()
                )

                Spacer(modifier = Modifier.height(12.dp))

                // Auto-fill toggle
                Row(
                    modifier = Modifier.fillMaxWidth(),
                    horizontalArrangement = Arrangement.SpaceBetween,
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Column {
                        Text(
                            text = "Auto-fill sets",
                            style = MaterialTheme.typography.bodyLarge
                        )
                        Text(
                            text = "Pre-fill weight/reps from last performance",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )
                    }
                    Switch(
                        checked = autoFill,
                        onCheckedChange = { autoFill = it }
                    )
                }

                Spacer(modifier = Modifier.height(16.dp))

                Button(
                    onClick = {
                        viewModel.saveConfig(
                            ConfigDto(
                                Color = colorMode,
                                PageStep = pageStep.toIntOrNull() ?: 10,
                                FrequencyDays = frequencyDays.toIntOrNull() ?: 30,
                                DisplayDays = displayDays,
                                AutoFill = autoFill
                            )
                        )
                    },
                    modifier = Modifier.fillMaxWidth()
                ) {
                    Text("Save Configuration")
                }

                when (val status = state.configSaveStatus) {
                    is SaveStatus.Success -> {
                        Spacer(modifier = Modifier.height(8.dp))
                        Text(
                            text = "Configuration saved.",
                            color = MaterialTheme.colorScheme.primary,
                            style = MaterialTheme.typography.bodySmall
                        )
                    }
                    is SaveStatus.Failure -> {
                        Spacer(modifier = Modifier.height(8.dp))
                        Text(
                            text = status.message,
                            color = MaterialTheme.colorScheme.error,
                            style = MaterialTheme.typography.bodySmall
                        )
                    }
                    else -> {}
                }
            }
        }

        Spacer(modifier = Modifier.height(16.dp))

        // About card
        Card(
            modifier = Modifier.fillMaxWidth(),
            colors = CardDefaults.cardColors(containerColor = MaterialTheme.colorScheme.surfaceVariant)
        ) {
            Column(
                modifier = Modifier.padding(16.dp),
                horizontalAlignment = Alignment.CenterHorizontally
            ) {
                Text(
                    text = "About",
                    style = MaterialTheme.typography.titleMedium
                )
                Spacer(modifier = Modifier.height(8.dp))
                Text(
                    text = "PUMP -- Please Use More Protein",
                    style = MaterialTheme.typography.bodyMedium
                )
                Spacer(modifier = Modifier.height(4.dp))
                Text(
                    text = "Open source workout tracker",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
                Spacer(modifier = Modifier.height(8.dp))
                Text(
                    text = "Version: ${BuildConfig.VERSION_NAME}",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }

        Spacer(modifier = Modifier.height(16.dp))
    }
}
