package com.iwvw.apimonitor.ui.common

import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxScope
import androidx.compose.material3.Surface
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color

/**
 * 给不带 `onClick` 重载的原生组件（如 `ListItem`）补上原生点击语义。
 *
 * 不要用 `Modifier.clickable`：那会绕过 MD3 的 ripple / 状态层 / 无障碍语义。
 * 这里用 `Surface(onClick = ...)` 包裹，点击态与无障碍由 MD3 原生提供。
 *
 * @param color 默认透明。嵌在 `Card` 里时必须保持透明，否则 Surface 的
 *   默认 `surface` 底色会盖住 Card 的 containerColor，看起来像「没有背景」。
 */
@Composable
fun SurfaceClickable(
    onClick: () -> Unit,
    modifier: Modifier = Modifier,
    color: Color = Color.Transparent,
    content: @Composable BoxScope.() -> Unit,
) {
    Surface(onClick = onClick, modifier = modifier, color = color) {
        Box(content = content)
    }
}
