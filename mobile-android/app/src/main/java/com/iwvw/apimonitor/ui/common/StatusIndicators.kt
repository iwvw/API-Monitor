package com.iwvw.apimonitor.ui.common

import androidx.compose.foundation.layout.size
import androidx.compose.material3.Badge
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp

/**
 * 状态点：用 MD3 [Badge] 表达「在线 / 离线 / 冷却」这类二态或三态。
 *
 * MD3 没有专门的 dot 组件，[Badge] 就是官方的状态标记原语，
 * 自带 `containerColor` 语义色与固定尺寸，不要用 `Box + CircleShape` 手绘。
 */
@Composable
fun StatusDot(
    active: Boolean,
    modifier: Modifier = Modifier,
    neutral: Boolean = false,
) {
    val color = when {
        neutral -> MaterialTheme.colorScheme.outlineVariant
        active -> MaterialTheme.colorScheme.primary
        else -> MaterialTheme.colorScheme.outline
    }
    Badge(modifier = modifier.size(8.dp), containerColor = color)
}

/**
 * 带文字的状态徽标（如「插件」「默认」）。
 *
 * 用 MD3 [Badge] 而非手写 `Text + background`。
 */
@Composable
fun StatusBadge(
    text: String,
    modifier: Modifier = Modifier,
) {
    Badge(
        modifier = modifier,
        containerColor = MaterialTheme.colorScheme.secondaryContainer,
        contentColor = MaterialTheme.colorScheme.onSecondaryContainer,
    ) {
        Text(text = text, style = MaterialTheme.typography.labelSmall)
    }
}
