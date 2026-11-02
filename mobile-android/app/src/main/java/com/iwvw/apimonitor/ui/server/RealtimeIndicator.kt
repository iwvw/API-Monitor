package com.iwvw.apimonitor.ui.server

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.iwvw.apimonitor.ui.common.StatusDot

@Composable
fun RealtimeDot(
    connected: Boolean,
    modifier: Modifier = Modifier,
) {
    StatusDot(
        active = connected,
        modifier = modifier,
    )
}

@Composable
fun RealtimeBadge(
    connected: Boolean,
    modifier: Modifier = Modifier,
) {
    Row(
        modifier = modifier.padding(end = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        RealtimeDot(connected = connected)
        Text(
            text = if (connected) "实时" else "轮询",
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}
