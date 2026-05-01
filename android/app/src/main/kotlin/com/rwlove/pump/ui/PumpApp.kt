package com.rwlove.pump.ui

import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.FitnessCenter
import androidx.compose.material.icons.filled.MonitorWeight
import androidx.compose.material.icons.filled.QueryStats
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.Icon
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.navigation.NavDestination.Companion.hasRoute
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import com.rwlove.pump.ui.screens.SettingsScreen
import com.rwlove.pump.ui.screens.StatsScreen
import com.rwlove.pump.ui.screens.WeightScreen
import com.rwlove.pump.ui.screens.WorkoutScreen
import kotlinx.serialization.Serializable

@Serializable object WorkoutRoute
@Serializable object StatsRoute
@Serializable object WeightRoute
@Serializable object SettingsRoute

data class BottomNavItem(
    val label: String,
    val icon: ImageVector,
    val route: Any
)

@Composable
fun PumpApp() {
    val navController = rememberNavController()
    val navBackStackEntry by navController.currentBackStackEntryAsState()
    val currentDestination = navBackStackEntry?.destination

    val bottomNavItems = listOf(
        BottomNavItem("Workout", Icons.Default.FitnessCenter, WorkoutRoute),
        BottomNavItem("Stats", Icons.Default.QueryStats, StatsRoute),
        BottomNavItem("Weight", Icons.Default.MonitorWeight, WeightRoute),
        BottomNavItem("Settings", Icons.Default.Settings, SettingsRoute)
    )

    Scaffold(
        bottomBar = {
            NavigationBar {
                bottomNavItems.forEach { item ->
                    NavigationBarItem(
                        icon = { Icon(item.icon, contentDescription = item.label) },
                        label = { Text(item.label) },
                        selected = currentDestination?.hasRoute(item.route::class) == true,
                        onClick = {
                            navController.navigate(item.route) {
                                popUpTo(navController.graph.findStartDestination().id) {
                                    saveState = true
                                }
                                launchSingleTop = true
                                restoreState = true
                            }
                        }
                    )
                }
            }
        }
    ) { innerPadding ->
        NavHost(
            navController = navController,
            startDestination = WorkoutRoute,
            modifier = Modifier.padding(innerPadding)
        ) {
            composable<WorkoutRoute> { WorkoutScreen() }
            composable<StatsRoute> { StatsScreen() }
            composable<WeightRoute> { WeightScreen() }
            composable<SettingsRoute> { SettingsScreen() }
        }
    }
}
