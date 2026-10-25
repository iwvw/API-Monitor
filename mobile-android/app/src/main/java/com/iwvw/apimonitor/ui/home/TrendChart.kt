package com.iwvw.apimonitor.ui.home

import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.unit.dp
import com.patrykandpatrick.vico.compose.cartesian.CartesianChartHost
import com.patrykandpatrick.vico.compose.cartesian.axis.HorizontalAxis
import com.patrykandpatrick.vico.compose.cartesian.axis.VerticalAxis
import com.patrykandpatrick.vico.compose.cartesian.axis.rememberAxisLabelComponent
import com.patrykandpatrick.vico.compose.cartesian.data.CartesianChartModelProducer
import com.patrykandpatrick.vico.compose.cartesian.data.CartesianValueFormatter
import com.patrykandpatrick.vico.compose.cartesian.data.columnSeries
import com.patrykandpatrick.vico.compose.cartesian.layer.ColumnCartesianLayer
import com.patrykandpatrick.vico.compose.cartesian.layer.rememberColumnCartesianLayer
import com.patrykandpatrick.vico.compose.cartesian.rememberCartesianChart
import com.patrykandpatrick.vico.compose.cartesian.rememberVicoScrollState
import com.patrykandpatrick.vico.compose.common.Fill
import com.patrykandpatrick.vico.compose.common.ProvideVicoTheme
import com.patrykandpatrick.vico.compose.common.component.rememberLineComponent
import com.patrykandpatrick.vico.compose.m3.common.rememberM3VicoTheme

@Composable
fun TrendChart(
    values: List<Double>,
    labels: List<String>,
    modifier: Modifier = Modifier,
) {
    if (values.isEmpty()) return

    val producer = remember { CartesianChartModelProducer() }
    val primary = MaterialTheme.colorScheme.primary
    val outline = MaterialTheme.colorScheme.outlineVariant
    val axisLabelColor = MaterialTheme.colorScheme.onSurfaceVariant

    // 颜色必须写进 TextStyle：rememberAxisLabelComponent 没有 color 参数
    // （它的两个 Int 参数是 maxLines / overflow，传颜色会变成 maxLines=0 直接崩溃）。
    val labelStyle = MaterialTheme.typography.labelSmall.copy(color = axisLabelColor)

    LaunchedEffect(values) {
        producer.runTransaction {
            columnSeries { series(values) }
        }
    }

    val column = rememberLineComponent(fill = Fill(primary), thickness = 12.dp)
    val axisLabel = rememberAxisLabelComponent(labelStyle)

    val bottomFormatter = remember(labels) {
        CartesianValueFormatter { _, value, _ ->
            labels.getOrElse(value.toInt()) { "" }
        }
    }

    val chart = rememberCartesianChart(
        rememberColumnCartesianLayer(
            columnProvider = ColumnCartesianLayer.ColumnProvider.series(column),
        ),
        startAxis = VerticalAxis.rememberStart(
            label = axisLabel,
            valueFormatter = CartesianValueFormatter { _, value, _ -> formatCompact(value) },
            guideline = rememberLineComponent(fill = Fill(outline), thickness = 1.dp),
        ),
        bottomAxis = HorizontalAxis.rememberBottom(
            label = axisLabel,
            valueFormatter = bottomFormatter,
            guideline = null,
        ),
    )

    ProvideVicoTheme(rememberM3VicoTheme()) {
        CartesianChartHost(
            chart = chart,
            modelProducer = producer,
            modifier = modifier
                .fillMaxWidth()
                .height(140.dp),
            scrollState = rememberVicoScrollState(scrollEnabled = false),
            animationSpec = androidx.compose.animation.core.tween(durationMillis = 600),
        )
    }
}

internal fun formatCompact(value: Double): String = when {
    value >= 1_000_000_000 -> "${(value / 1_000_000_000).round1()}B"
    value >= 1_000_000 -> "${(value / 1_000_000).round1()}M"
    value >= 1_000 -> "${(value / 1_000).round1()}K"
    else -> value.toInt().toString()
}

private fun Double.round1(): String {
    val rounded = kotlin.math.round(this * 10) / 10
    return if (rounded % 1.0 == 0.0) rounded.toInt().toString() else rounded.toString()
}
