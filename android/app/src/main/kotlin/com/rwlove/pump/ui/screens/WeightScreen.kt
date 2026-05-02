package com.rwlove.pump.ui.screens

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.Button
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.rwlove.pump.ui.components.WeightEntry
import com.rwlove.pump.viewmodel.WeightViewModel
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.time.format.FormatStyle

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun WeightScreen(
    viewModel: WeightViewModel = hiltViewModel()
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    var weightInput by remember { mutableStateOf("") }
    var selectedDate by remember { mutableStateOf(LocalDate.now()) }
    var showDatePicker by remember { mutableStateOf(false) }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp)
    ) {
        Spacer(modifier = Modifier.height(8.dp))

        Text(
            text = "Body Weight Log",
            style = MaterialTheme.typography.titleLarge
        )

        Spacer(modifier = Modifier.height(12.dp))

        // Chart area
        if (state.entries.isNotEmpty()) {
            val chartData = state.entries
                .mapNotNull { entry ->
                    val w = entry.WEIGHT.toDoubleOrNull()
                    if (w != null) entry.DATE to w else null
                }
                .sortedBy { it.first }
                .takeLast(30) // Show last 30 entries in chart

            if (chartData.isNotEmpty()) {
                WeightLineChart(
                    data = chartData,
                    modifier = Modifier
                        .fillMaxWidth()
                        .height(180.dp)
                )
                Spacer(modifier = Modifier.height(12.dp))
            }
        }

        // Date + weight input form
        Row(
            modifier = Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            TextButton(
                onClick = { showDatePicker = true },
                modifier = Modifier.weight(1f)
            ) {
                Text(
                    text = selectedDate.format(
                        DateTimeFormatter.ofLocalizedDate(FormatStyle.MEDIUM)
                    )
                )
            }

            OutlinedTextField(
                value = weightInput,
                onValueChange = { weightInput = it },
                label = { Text("Weight (lbs)") },
                placeholder = { Text("e.g. 185.5") },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Decimal),
                singleLine = true,
                modifier = Modifier.weight(1f)
            )

            Button(
                onClick = {
                    if (weightInput.isNotEmpty()) {
                        viewModel.addWeight(selectedDate, weightInput)
                        weightInput = ""
                    }
                },
                enabled = weightInput.isNotEmpty()
            ) {
                Text("Add")
            }
        }

        Spacer(modifier = Modifier.height(12.dp))

        if (state.isLoading) {
            LinearProgressIndicator(modifier = Modifier.fillMaxWidth())
        }

        state.errorMessage?.let { error ->
            Text(
                text = error,
                color = MaterialTheme.colorScheme.error,
                style = MaterialTheme.typography.bodySmall,
                modifier = Modifier.padding(vertical = 4.dp)
            )
        }

        HorizontalDivider()

        Spacer(modifier = Modifier.height(8.dp))

        // Weight entries list
        LazyColumn(
            modifier = Modifier.fillMaxSize(),
            verticalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            items(state.entries, key = { it.ID }) { entry ->
                WeightEntry(
                    entry = entry,
                    onDelete = { viewModel.deleteWeight(entry.ID) }
                )
            }
        }
    }

    // Date picker dialog
    if (showDatePicker) {
        val datePickerState = rememberDatePickerState(
            initialSelectedDateMillis = selectedDate
                .atStartOfDay(ZoneId.systemDefault())
                .toInstant()
                .toEpochMilli()
        )
        DatePickerDialog(
            onDismissRequest = { showDatePicker = false },
            confirmButton = {
                TextButton(onClick = {
                    datePickerState.selectedDateMillis?.let { millis ->
                        selectedDate = Instant.ofEpochMilli(millis)
                            .atZone(ZoneId.systemDefault())
                            .toLocalDate()
                    }
                    showDatePicker = false
                }) {
                    Text("OK")
                }
            },
            dismissButton = {
                TextButton(onClick = { showDatePicker = false }) {
                    Text("Cancel")
                }
            }
        ) {
            DatePicker(state = datePickerState)
        }
    }
}

@Composable
private fun WeightLineChart(
    data: List<Pair<String, Double>>,
    modifier: Modifier = Modifier
) {
    if (data.isEmpty()) return

    val green = Color(0xFF28A745)
    val red = Color(0xFFDC3545)
    val grey = Color(0xFF888888)

    Canvas(modifier = modifier.padding(start = 40.dp, bottom = 4.dp, end = 16.dp, top = 8.dp)) {
        val minW = data.minOf { it.second }
        val maxW = data.maxOf { it.second }
        val range = if (maxW - minW > 0) maxW - minW else 1.0
        val padding = range * 0.1

        val yMin = minW - padding
        val yMax = maxW + padding
        val yRange = yMax - yMin

        val w = size.width
        val h = size.height

        fun xFor(i: Int) = if (data.size == 1) w / 2 else (i.toFloat() / (data.size - 1)) * w
        fun yFor(v: Double) = h - ((v - yMin) / yRange * h).toFloat()

        // Lines
        for (i in 0 until data.size - 1) {
            val x0 = xFor(i)
            val y0 = yFor(data[i].second)
            val x1 = xFor(i + 1)
            val y1 = yFor(data[i + 1].second)

            val lineColor = when {
                data[i + 1].second > data[i].second -> green
                data[i + 1].second < data[i].second -> red
                else -> grey
            }

            drawLine(
                color = lineColor,
                start = Offset(x0, y0),
                end = Offset(x1, y1),
                strokeWidth = 3f,
                cap = StrokeCap.Round
            )
        }

        // Points
        data.forEachIndexed { i, pt ->
            drawCircle(
                color = grey,
                radius = 4f,
                center = Offset(xFor(i), yFor(pt.second))
            )
        }

        // Y-axis labels
        val paint = android.graphics.Paint().apply {
            textSize = 28f
            color = android.graphics.Color.GRAY
            textAlign = android.graphics.Paint.Align.RIGHT
        }
        val labelCount = 4
        for (j in 0..labelCount) {
            val v = yMin + (yRange * j / labelCount)
            val y = yFor(v)
            drawContext.canvas.nativeCanvas.drawText(
                "%.0f".format(v),
                -8f,
                y + 10f,
                paint
            )
        }
    }
}
