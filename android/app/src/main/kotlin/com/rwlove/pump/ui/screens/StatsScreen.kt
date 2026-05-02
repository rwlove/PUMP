package com.rwlove.pump.ui.screens

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Card
import androidx.compose.material3.CardDefaults
import androidx.compose.material3.FilterChip
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Tab
import androidx.compose.material3.TabRow
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.patrykandpatrick.vico.compose.cartesian.CartesianChartHost
import com.patrykandpatrick.vico.compose.cartesian.layer.rememberColumnCartesianLayer
import com.patrykandpatrick.vico.compose.cartesian.rememberCartesianChart
import com.patrykandpatrick.vico.core.cartesian.data.CartesianChartModelProducer
import com.patrykandpatrick.vico.core.cartesian.data.columnSeries
import com.rwlove.pump.ui.components.parseColor
import com.rwlove.pump.viewmodel.BodyWeightPoint
import com.rwlove.pump.viewmodel.PieSlice
import com.rwlove.pump.viewmodel.StatsPeriod
import com.rwlove.pump.viewmodel.StatsViewModel
import com.rwlove.pump.viewmodel.VolumePoint

@Composable
fun StatsScreen(
    viewModel: StatsViewModel = hiltViewModel()
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    var selectedTab by remember { mutableIntStateOf(0) }
    val tabs = listOf("Distribution", "Weight Moved", "Body Weight")

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(horizontal = 16.dp)
    ) {
        Spacer(modifier = Modifier.height(8.dp))

        TabRow(selectedTabIndex = selectedTab) {
            tabs.forEachIndexed { index, title ->
                Tab(
                    selected = selectedTab == index,
                    onClick = { selectedTab = index },
                    text = { Text(title) }
                )
            }
        }

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

        Spacer(modifier = Modifier.height(8.dp))

        // Period selector row
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            StatsPeriod.entries.forEach { period ->
                FilterChip(
                    selected = state.selectedPeriod == period,
                    onClick = { viewModel.selectPeriod(period) },
                    label = { Text(period.label) },
                    modifier = Modifier.weight(1f)
                )
            }
        }

        Spacer(modifier = Modifier.height(8.dp))

        when (selectedTab) {
            0 -> ExerciseDistributionTab(
                pieData = state.pieData,
                totalSets = state.totalSets,
                activeDays = state.activeDays,
                mostPerformed = state.mostPerformed,
                leastPerformed = state.leastPerformed
            )
            1 -> WeightMovedTab(
                exerciseNames = state.exerciseNames,
                selectedExercise = state.selectedExercise,
                onSelectExercise = { viewModel.selectExercise(it) },
                volumeData = state.volumeData,
                exerciseColorMap = state.exerciseColorMap
            )
            2 -> BodyWeightTab(bodyWeightData = state.bodyWeightData)
        }
    }
}

@Composable
private fun ExerciseDistributionTab(
    pieData: List<PieSlice>,
    totalSets: Int,
    activeDays: Int,
    mostPerformed: com.rwlove.pump.viewmodel.ExerciseStat?,
    leastPerformed: com.rwlove.pump.viewmodel.ExerciseStat?
) {
    Column(
        modifier = Modifier.verticalScroll(rememberScrollState())
    ) {
        // Summary cards row
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            SummaryCard(
                title = "Total Sets",
                value = totalSets.toString(),
                modifier = Modifier.weight(1f)
            )
            SummaryCard(
                title = "Active Days",
                value = activeDays.toString(),
                modifier = Modifier.weight(1f)
            )
        }

        Spacer(modifier = Modifier.height(8.dp))

        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            SummaryCard(
                title = "Most Performed",
                value = mostPerformed?.name ?: "-",
                subtitle = mostPerformed?.let { "${it.count} sets" },
                modifier = Modifier.weight(1f)
            )
            SummaryCard(
                title = "Least Performed",
                value = leastPerformed?.name ?: "-",
                subtitle = leastPerformed?.let { "${it.count} sets" },
                modifier = Modifier.weight(1f)
            )
        }

        Spacer(modifier = Modifier.height(16.dp))

        // Pie chart
        if (pieData.isNotEmpty()) {
            PieChart(
                data = pieData,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(240.dp)
            )

            Spacer(modifier = Modifier.height(12.dp))

            // Legend
            pieData.forEach { slice ->
                Row(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(vertical = 2.dp),
                    verticalAlignment = Alignment.CenterVertically
                ) {
                    Box(
                        modifier = Modifier
                            .size(12.dp)
                            .clip(CircleShape)
                            .background(parseColor(slice.color))
                    )
                    Spacer(modifier = Modifier.width(8.dp))
                    Text(
                        text = slice.name,
                        style = MaterialTheme.typography.bodySmall,
                        modifier = Modifier.weight(1f),
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis
                    )
                    val total = pieData.sumOf { it.count }
                    val pct = if (total > 0) "%.1f".format(slice.count * 100.0 / total) else "0"
                    Text(
                        text = "${slice.count} ($pct%)",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                }
            }
        } else {
            Text(
                text = "No data for selected period",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(vertical = 16.dp)
            )
        }
    }
}

@Composable
private fun PieChart(
    data: List<PieSlice>,
    modifier: Modifier = Modifier
) {
    val total = data.sumOf { it.count }.toFloat()
    if (total == 0f) return

    Canvas(modifier = modifier) {
        val diameter = minOf(size.width, size.height) * 0.85f
        val topLeft = Offset(
            (size.width - diameter) / 2f,
            (size.height - diameter) / 2f
        )
        val chartSize = Size(diameter, diameter)
        var startAngle = -90f

        data.forEach { slice ->
            val sweep = (slice.count / total) * 360f
            drawArc(
                color = parseColorCompose(slice.color),
                startAngle = startAngle,
                sweepAngle = sweep,
                useCenter = true,
                topLeft = topLeft,
                size = chartSize
            )
            startAngle += sweep
        }
    }
}

private fun parseColorCompose(hex: String): Color {
    return try {
        Color(android.graphics.Color.parseColor(hex))
    } catch (_: Exception) {
        Color(0xFF2780E3)
    }
}

@Composable
private fun SummaryCard(
    title: String,
    value: String,
    subtitle: String? = null,
    modifier: Modifier = Modifier
) {
    Card(
        modifier = modifier,
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.surfaceVariant
        )
    ) {
        Column(
            modifier = Modifier.padding(12.dp)
        ) {
            Text(
                text = title,
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = value,
                style = MaterialTheme.typography.titleMedium,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis
            )
            if (subtitle != null) {
                Text(
                    text = subtitle,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }
    }
}

@Composable
private fun WeightMovedTab(
    exerciseNames: List<String>,
    selectedExercise: String,
    onSelectExercise: (String) -> Unit,
    volumeData: List<VolumePoint>,
    exerciseColorMap: Map<String, String>
) {
    Column(
        modifier = Modifier.verticalScroll(rememberScrollState())
    ) {
        // Exercise selector chips with color accent
        if (exerciseNames.isEmpty()) {
            Text(
                text = "No exercises performed in this period",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(vertical = 16.dp)
            )
        } else {
            LazyRow(
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                modifier = Modifier.fillMaxWidth()
            ) {
                items(exerciseNames) { name ->
                    val exColor = exerciseColorMap[name]
                    FilterChip(
                        selected = name == selectedExercise,
                        onClick = { onSelectExercise(name) },
                        label = { Text(name, maxLines = 1) },
                        leadingIcon = if (exColor != null) {
                            {
                                Box(
                                    modifier = Modifier
                                        .size(8.dp)
                                        .clip(CircleShape)
                                        .background(parseColor(exColor))
                                )
                            }
                        } else null
                    )
                }
            }
        }

        Spacer(modifier = Modifier.height(16.dp))

        if (volumeData.isNotEmpty()) {
            val modelProducer = remember { CartesianChartModelProducer() }

            val volumes by remember(volumeData) {
                derivedStateOf { volumeData.map { it.volume } }
            }

            LaunchedEffect(volumes) {
                modelProducer.runTransaction {
                    columnSeries { series(volumes) }
                }
            }

            CartesianChartHost(
                chart = rememberCartesianChart(
                    rememberColumnCartesianLayer()
                ),
                modelProducer = modelProducer,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(300.dp)
            )
        } else {
            Text(
                text = "No data for selected exercise",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
        }
    }
}

@Composable
private fun BodyWeightTab(
    bodyWeightData: List<BodyWeightPoint>
) {
    Column(
        modifier = Modifier.verticalScroll(rememberScrollState())
    ) {
        if (bodyWeightData.isNotEmpty()) {
            // Custom body weight chart with green/red segments
            BodyWeightLineChart(
                data = bodyWeightData,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(300.dp)
            )
        } else {
            Text(
                text = "No body weight data for selected period",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
        }
    }
}

@Composable
private fun BodyWeightLineChart(
    data: List<BodyWeightPoint>,
    modifier: Modifier = Modifier
) {
    if (data.isEmpty()) return

    val green = Color(0xFF28A745)
    val red = Color(0xFFDC3545)
    val grey = Color(0xFF888888)
    val textColor = MaterialTheme.colorScheme.onSurface

    Canvas(modifier = modifier.padding(start = 40.dp, bottom = 24.dp, end = 16.dp, top = 8.dp)) {
        val minW = data.minOf { it.weight }
        val maxW = data.maxOf { it.weight }
        val range = if (maxW - minW > 0) maxW - minW else 1.0
        val padding = range * 0.1

        val yMin = minW - padding
        val yMax = maxW + padding
        val yRange = yMax - yMin

        val w = size.width
        val h = size.height

        fun xFor(i: Int) = if (data.size == 1) w / 2 else (i.toFloat() / (data.size - 1)) * w
        fun yFor(v: Double) = h - ((v - yMin) / yRange * h).toFloat()

        // Draw data points and connecting lines
        for (i in 0 until data.size - 1) {
            val x0 = xFor(i)
            val y0 = yFor(data[i].weight)
            val x1 = xFor(i + 1)
            val y1 = yFor(data[i + 1].weight)

            val lineColor = when {
                data[i + 1].weight > data[i].weight -> green
                data[i + 1].weight < data[i].weight -> red
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

        // Draw points
        data.forEachIndexed { i, pt ->
            drawCircle(
                color = grey,
                radius = 4f,
                center = Offset(xFor(i), yFor(pt.weight))
            )
        }

        // Y-axis labels
        val labelCount = 5
        val paint = android.graphics.Paint().apply {
            textSize = 28f
            color = android.graphics.Color.GRAY
            textAlign = android.graphics.Paint.Align.RIGHT
        }
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
