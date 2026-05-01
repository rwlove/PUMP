package com.rwlove.pump.ui.theme

import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext

private val DarkColorScheme = darkColorScheme(
    primary = PumpBlue,
    onPrimary = androidx.compose.ui.graphics.Color.White,
    primaryContainer = PumpBlueDark,
    secondary = PumpBlueLight,
    surface = PumpSurface,
    onSurface = PumpOnSurface,
    background = PumpSurface,
    onBackground = PumpOnSurface
)

private val LightColorScheme = lightColorScheme(
    primary = PumpBlue,
    onPrimary = androidx.compose.ui.graphics.Color.White,
    primaryContainer = PumpBlueLight,
    secondary = PumpBlueDark,
    surface = PumpSurfaceLight,
    onSurface = PumpOnSurfaceLight,
    background = PumpSurfaceLight,
    onBackground = PumpOnSurfaceLight
)

@Composable
fun PumpTheme(
    darkTheme: Boolean = isSystemInDarkTheme(),
    dynamicColor: Boolean = true,
    content: @Composable () -> Unit
) {
    val colorScheme = when {
        dynamicColor && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S -> {
            val context = LocalContext.current
            if (darkTheme) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        }
        darkTheme -> DarkColorScheme
        else -> LightColorScheme
    }

    MaterialTheme(
        colorScheme = colorScheme,
        typography = PumpTypography,
        content = content
    )
}
