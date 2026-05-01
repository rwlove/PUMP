package com.rwlove.pump.ui.screens

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
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
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
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.unit.dp
import androidx.hilt.navigation.compose.hiltViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import com.patrykandpatrick.vico.compose.cartesian.CartesianChartHost
import com.patrykandpatrick.vico.compose.cartesian.layer.rememberColumnCartesianLayer
import com.patrykandpatrick.vico.compose.cartesian.layer.rememberLineCartesianLayer
import com.patrykandpatrick.vico.compose.cartesian.rememberCartesianChart
import com.patrykandpatrick.vico.core.cartesian.data.CartesianChartModelProducer
import com.patrykandpatrick.vico.core.cartesian.data.columnSeries
import com.patrykandpatrick.vico.core.cartesian.data.lineSeries
import com.rwlove.pump.viewmodel.HeatmapDay
import com.rwlove.pump.viewmodel.StatsViewModel

@Composable
fun StatsScreen(
    viewModel: StatsViewModel = hiltViewModel()
) {
    val state by viewModel.uiState.collectAsStateWithLifecycle()
    var selectedTab by remember { mutableIntStateOf(0) }
    val tabs = listOf("Overview", "Weight Moved", "Body Weight")

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

        Spacer(modifier = Modifier.height(16.dp))

        when (selectedTab) {
            0 -> OverviewTab(
                mostPerformed = state.mostPerformed,
                leastPerformed = state.leastPerformed,
                heatmapData = state.heatmapData
            )
            1 -> WeightMovedTab(
                exerciseNames = state.exerciseNames,
                selectedExercise = state.selectedExercise,
                onSelectExercise = { viewModel.selectExercise(it) },
                volumeData = state.volumeData
            )
            2 -> BodyWeightTab(bodyWeightData = state.bodyWeightData)
        }
    }
}

@Composable
private fun OverviewTab(
    mostPerformed: com.rwlove.pump.viewmodel.ExerciseStat?,
    leastPerformed: com.rwlove.pump.viewmodel.ExerciseStat?,
    heatmapData: List<HeatmapDay>
) {
    Column(
        modifier = Modifier.verticalScroll(rememberScrollState())
    ) {
        Row(
            modifier = Modifier.fillMaxWidth(),
            horizontalArrangement = Arrangement.spacedBy(8.dp)
        ) {
            StatCard(
                title = "Most Performed",
                name = mostPerformed?.name ?: "-",
                count = mostPerformed?.count ?: 0,
                modifier = Modifier.weight(1f)
            )
            StatCard(
                title = "Least Performed",
                name = leastPerformed?.name ?: "-",
                count = leastPerformed?.count ?: 0,
                modifier = Modifier.weight(1f)
            )
        }

        Spacer(modifier = Modifier.height(16.dp))

        Text(
            text = "Year Activity",
            style = MaterialTheme.typography.titleMedium
        )

        Spacer(modifier = Modifier.height(8.dp))

        YearHeatmap(heatmapData = heatmapData)
    }
}

@Composable
private fun StatCard(
    title: String,
    name: String,
    count: Int,
    modifier: Modifier = Modifier
) {
    Card(
        modifier = modifier,
        colors = CardDefaults.cardColors(
            containerColor = MaterialTheme.colorScheme.primaryContainer
        )
    ) {
        Column(
            modifier = Modifier.padding(16.dp)
        ) {
            Text(
                text = title,
                style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onPrimaryContainer
            )
            Spacer(modifier = Modifier.height(4.dp))
            Text(
                text = name,
                style = MaterialTheme.typography.titleMedium,
                color = MaterialTheme.colorScheme.onPrimaryContainer
            )
            Text(
                text = "$count sets",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onPrimaryContainer.copy(alpha = 0.7f)
            )
        }
    }
}

@Composable
private fun YearHeatmap(
    heatmapData: List<HeatmapDay>
) {
    if (heatmapData.isEmpty()) return

    // Group by week (column) and day of week (row)
    val maxCount by remember(heatmapData) {
        derivedStateOf { heatmapData.maxOfOrNull { it.count } ?: 1 }
    }

    val weeks by remember(heatmapData) {
        derivedStateOf {
            heatmapData.chunked(7)
        }
    }

    LazyRow(
        horizontalArrangement = Arrangement.spacedBy(2.dp),
        modifier = Modifier.fillMaxWidth()
    ) {
        items(weeks) { week ->
            Column(
                verticalArrangement = Arrangement.spacedBy(2.dp)
            ) {
                week.forEach { day ->
                    val intensity = if (maxCount > 0) day.count.toFloat() / maxCount else 0f
                    Box(
                        modifier = Modifier
                            .size(12.dp)
                            .clip(RoundedCornerShape(2.dp))
                            .background(
                                when {
                                    day.count == 0 -> MaterialTheme.colorScheme.surfaceVariant
                                    intensity < 0.25f -> MaterialTheme.colorScheme.primary.copy(alpha = 0.3f)
                                    intensity < 0.5f -> MaterialTheme.colorScheme.primary.copy(alpha = 0.5f)
                                    intensity < 0.75f -> MaterialTheme.colorScheme.primary.copy(alpha = 0.75f)
                                    else -> MaterialTheme.colorScheme.primary
                                }
                            )
                    )
                }
            }
        }
    }
}

@Composable
private fun WeightMovedTab(
    exerciseNames: List<String>,
    selectedExercise: String,
    onSelectExercise: (String) -> Unit,
    volumeData: List<com.rwlove.pump.viewmodel.VolumePoint>
) {
    Column(
        modifier = Modifier.verticalScroll(rememberScrollState())
    ) {
        // Exercise selector chips
        LazyRow(
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            modifier = Modifier.fillMaxWidth()
        ) {
            items(exerciseNames) { name ->
                FilterChip(
                    selected = name == selectedExercise,
                    onClick = { onSelectExercise(name) },
                    label = { Text(name, maxLines = 1) }
                )
            }
        }

        Spacer(modifier = Modifier.height(16.dp))

        if (volumeData.isNotEmpty()) {
            val modelProducer = remember { CartesianChartModelProducer() }

            val volumes by remember(volumeData) {
                derivedStateOf { volumeData.map { it.volume } }
            }

            androidx.compose.runtime.LaunchedEffect(volumes) {
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
    bodyWeightData: List<com.rwlove.pump.viewmodel.BodyWeightPoint>
) {
    Column(
        modifier = Modifier.verticalScroll(rememberScrollState())
    ) {
        if (bodyWeightData.isNotEmpty()) {
            val modelProducer = remember { CartesianChartModelProducer() }

            val weights by remember(bodyWeightData) {
                derivedStateOf { bodyWeightData.map { it.weight } }
            }

            androidx.compose.runtime.LaunchedEffect(weights) {
                modelProducer.runTransaction {
                    lineSeries { series(weights) }
                }
            }

            CartesianChartHost(
                chart = rememberCartesianChart(
                    rememberLineCartesianLayer()
                ),
                modelProducer = modelProducer,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(300.dp)
            )
        } else {
            Text(
                text = "No body weight data",
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant
            )
        }
    }
}
