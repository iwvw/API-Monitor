package com.iwvw.apimonitor.ui.server

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Logout
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.iwvw.apimonitor.ui.common.motionItem
import com.iwvw.apimonitor.ui.common.StatusDot
import com.iwvw.apimonitor.ui.common.tabContentWindowInsets
import com.iwvw.apimonitor.data.model.ServerSummary
import com.iwvw.apimonitor.data.model.parsePercent

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServerListScreen(
    servers: List<ServerSummary>,
    loading: Boolean,
    errorMessage: String?,
    refreshing: Boolean,
    realtimeConnected: Boolean,
    onRefresh: () -> Unit,
    onDisconnect: () -> Unit,
    onOpenServer: (ServerSummary) -> Unit,
) {
    val scrollBehavior = TopAppBarDefaults.pinnedScrollBehavior()

    Scaffold(
        modifier = Modifier.nestedScroll(scrollBehavior.nestedScrollConnection),
        contentWindowInsets = tabContentWindowInsets(),
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("服务器")
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(
                                text = when {
                                    servers.isNotEmpty() -> "共 ${servers.size} 台"
                                    loading -> "加载中"
                                    errorMessage != null -> "加载失败"
                                    else -> "暂无服务器"
                                },
                                style = MaterialTheme.typography.labelMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                            Spacer(Modifier.width(8.dp))
                            RealtimeDot(connected = realtimeConnected)
                        }
                    }
                },
                actions = {
                    IconButton(onClick = onRefresh, enabled = !refreshing) {
                        Icon(Icons.Filled.Refresh, contentDescription = "刷新")
                    }
                    IconButton(onClick = onDisconnect) {
                        Icon(Icons.AutoMirrored.Filled.Logout, contentDescription = "退出登录")
                    }
                },
                scrollBehavior = scrollBehavior,
            )
        },
    ) { innerPadding ->
        PullToRefreshBox(
            isRefreshing = refreshing,
            onRefresh = onRefresh,
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding),
        ) {
            when {
                loading && servers.isEmpty() -> {
                    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        CircularProgressIndicator()
                    }
                }

                errorMessage != null && servers.isEmpty() -> {
                    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        Column(
                            horizontalAlignment = Alignment.CenterHorizontally,
                            modifier = Modifier.padding(32.dp),
                        ) {
                            Text(
                                text = "加载失败",
                                style = MaterialTheme.typography.titleMedium,
                                color = MaterialTheme.colorScheme.onSurface,
                            )
                            Spacer(Modifier.height(8.dp))
                            Text(
                                text = errorMessage,
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                            Spacer(Modifier.height(16.dp))
                            FilledTonalButton(onClick = onRefresh) { Text("重试") }
                        }
                    }
                }

                servers.isEmpty() -> {
                    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
                        Text(
                            text = "暂无服务器",
                            style = MaterialTheme.typography.bodyLarge,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }

                else -> {
                    LazyColumn(
                        modifier = Modifier.fillMaxSize(),
                        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
                        verticalArrangement = Arrangement.spacedBy(8.dp),
                    ) {
                        items(servers, key = { it.id }) { server ->
                            Box(modifier = motionItem()) {
                                ServerCard(server = server, onClick = { onOpenServer(server) })
                            }
                        }
                    }
                }
            }
        }
    }
}

@Composable
private fun ServerCard(server: ServerSummary, onClick: () -> Unit) {
    OutlinedCard(
        onClick = onClick,
        modifier = Modifier.fillMaxWidth(),
    ) {
        Column(modifier = Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                StatusDot(active = server.isLive)
                Spacer(Modifier.width(8.dp))
                Text(
                    text = server.name.ifBlank { server.id.take(8) },
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSurface,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    text = server.displayLocation,
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            val info = server.info
            if (info == null) {
                Spacer(Modifier.height(8.dp))
                Text(
                    text = "无指标数据",
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                return@Column
            }

            Spacer(Modifier.height(12.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                MetricBar(
                    label = "CPU",
                    percent = info.cpu?.usage.parsePercent(),
                    detail = info.cpu?.temp?.takeIf { it > 0 }?.let { "${it.toInt()}°C" },
                    modifier = Modifier.weight(1f),
                )
                MetricBar(
                    label = "内存",
                    percent = info.memory?.usage.parsePercent(),
                    detail = info.memory?.let { "${it.used}/${it.total}" },
                    modifier = Modifier.weight(1f),
                )
                MetricBar(
                    label = "磁盘",
                    percent = info.disk.firstOrNull()?.usage.parsePercent(),
                    detail = info.disk.firstOrNull()?.total,
                    modifier = Modifier.weight(1f),
                )
            }

            val net = info.network
            if (net != null && (net.rxSpeed.isNotBlank() || net.txSpeed.isNotBlank())) {
                Spacer(Modifier.height(12.dp))
                HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                Spacer(Modifier.height(10.dp))
                Row(horizontalArrangement = Arrangement.spacedBy(16.dp)) {
                    ServerStatChip(label = "下行", value = net.rxSpeed.ifBlank { "—" })
                    ServerStatChip(label = "上行", value = net.txSpeed.ifBlank { "—" })
                    ServerStatChip(label = "连接", value = net.connections?.toString() ?: "—")
                    info.platform?.let { ServerStatChip(label = "系统", value = it) }
                }
            }
        }
    }
}
