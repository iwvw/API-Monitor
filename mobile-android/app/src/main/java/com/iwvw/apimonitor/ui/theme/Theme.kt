package com.iwvw.apimonitor.ui.theme

import android.os.Build
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.platform.LocalContext

private val LightScheme = lightColorScheme(
    primary = LightPalette.primary,
    onPrimary = LightPalette.onPrimary,
    primaryContainer = LightPalette.primaryContainer,
    onPrimaryContainer = LightPalette.onPrimaryContainer,
    secondary = LightPalette.secondary,
    onSecondary = LightPalette.onSecondary,
    secondaryContainer = LightPalette.secondaryContainer,
    onSecondaryContainer = LightPalette.onSecondaryContainer,
    tertiary = LightPalette.tertiary,
    onTertiary = LightPalette.onTertiary,
    tertiaryContainer = LightPalette.tertiaryContainer,
    onTertiaryContainer = LightPalette.onTertiaryContainer,
    error = LightPalette.error,
    onError = LightPalette.onError,
    errorContainer = LightPalette.errorContainer,
    onErrorContainer = LightPalette.onErrorContainer,
    background = LightPalette.background,
    onBackground = LightPalette.onBackground,
    surface = LightPalette.surface,
    onSurface = LightPalette.onSurface,
    surfaceVariant = LightPalette.surfaceVariant,
    onSurfaceVariant = LightPalette.onSurfaceVariant,
    surfaceContainerLowest = LightPalette.surfaceContainerLowest,
    surfaceContainerLow = LightPalette.surfaceContainerLow,
    surfaceContainer = LightPalette.surfaceContainer,
    surfaceContainerHigh = LightPalette.surfaceContainerHigh,
    surfaceContainerHighest = LightPalette.surfaceContainerHighest,
    surfaceDim = LightPalette.surfaceDim,
    surfaceBright = LightPalette.surfaceBright,
    outline = LightPalette.outline,
    outlineVariant = LightPalette.outlineVariant,
    inverseSurface = LightPalette.inverseSurface,
    inverseOnSurface = LightPalette.inverseOnSurface,
    inversePrimary = LightPalette.inversePrimary,
    scrim = LightPalette.scrim,
)

private val DarkScheme = darkColorScheme(
    primary = DarkPalette.primary,
    onPrimary = DarkPalette.onPrimary,
    primaryContainer = DarkPalette.primaryContainer,
    onPrimaryContainer = DarkPalette.onPrimaryContainer,
    secondary = DarkPalette.secondary,
    onSecondary = DarkPalette.onSecondary,
    secondaryContainer = DarkPalette.secondaryContainer,
    onSecondaryContainer = DarkPalette.onSecondaryContainer,
    tertiary = DarkPalette.tertiary,
    onTertiary = DarkPalette.onTertiary,
    tertiaryContainer = DarkPalette.tertiaryContainer,
    onTertiaryContainer = DarkPalette.onTertiaryContainer,
    error = DarkPalette.error,
    onError = DarkPalette.onError,
    errorContainer = DarkPalette.errorContainer,
    onErrorContainer = DarkPalette.onErrorContainer,
    background = DarkPalette.background,
    onBackground = DarkPalette.onBackground,
    surface = DarkPalette.surface,
    onSurface = DarkPalette.onSurface,
    surfaceVariant = DarkPalette.surfaceVariant,
    onSurfaceVariant = DarkPalette.onSurfaceVariant,
    surfaceContainerLowest = DarkPalette.surfaceContainerLowest,
    surfaceContainerLow = DarkPalette.surfaceContainerLow,
    surfaceContainer = DarkPalette.surfaceContainer,
    surfaceContainerHigh = DarkPalette.surfaceContainerHigh,
    surfaceContainerHighest = DarkPalette.surfaceContainerHighest,
    surfaceDim = DarkPalette.surfaceDim,
    surfaceBright = DarkPalette.surfaceBright,
    outline = DarkPalette.outline,
    outlineVariant = DarkPalette.outlineVariant,
    inverseSurface = DarkPalette.inverseSurface,
    inverseOnSurface = DarkPalette.inverseOnSurface,
    inversePrimary = DarkPalette.inversePrimary,
    scrim = DarkPalette.scrim,
)

@Composable
fun APIMonitorTheme(
    darkTheme: Boolean = androidx.compose.foundation.isSystemInDarkTheme(),
    dynamicColor: Boolean = true,
    content: @Composable () -> Unit,
) {
    val colorScheme = when {
        dynamicColor && Build.VERSION.SDK_INT >= Build.VERSION_CODES.S -> {
            val context = LocalContext.current
            if (darkTheme) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        }

        darkTheme -> DarkScheme
        else -> LightScheme
    }

    MaterialTheme(
        colorScheme = colorScheme,
        shapes = AppShapes,
        typography = MaterialTheme.typography,
        content = content,
    )
}
