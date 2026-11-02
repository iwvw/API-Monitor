package com.iwvw.apimonitor.ui.home

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.Logout
import androidx.compose.material.icons.automirrored.filled.KeyboardArrowRight
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.iwvw.apimonitor.ui.common.tabContentWindowInsets
import com.iwvw.apimonitor.data.model.AgentInstance
import com.iwvw.apimonitor.data.model.ApiStats
import com.iwvw.apimonitor.data.model.HostMetrics
import com.iwvw.apimonitor.data.model.OpenAiEndpoint
import com.iwvw.apimonitor.data.model.parsePercent
import com.iwvw.apimonitor.data.network.ApiClient
import com.iwvw.apimonitor.data.settings.Credentials
import com.iwvw.apimonitor.ui.common.SurfaceClickable
import com.iwvw.apimonitor.ui.common.rememberHaptics
import com.iwvw.apimonitor.ui.server.MetricBar
import com.iwvw.apimonitor.ui.server.ServerListUiState
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun HomeScreen(
    credentials: Credentials,
    apiClient: ApiClient,
    serverState: ServerListUiState,
    onOpenSettings: () -> Unit,
    onLogout: () -> Unit,
) {
    val scope = rememberCoroutineScope()
    val haptics = rememberHaptics()
    val scrollBehavior = TopAppBarDefaults.pinnedScrollBehavior()

    var host by remember { mutableStateOf<HostMetrics?>(null) }
    var stats by remember { mutableStateOf<ApiStats?>(null) }
    var endpoints by remember { mutableStateOf<List<OpenAiEndpoint>>(emptyList()) }
    var agents by remember { mutableStateOf<List<AgentInstance>>(emptyList()) }
    var loading by remember { mutableStateOf(true) }

    fun refresh() {
        scope.launch {
            loading = true
            apiClient.hostMetrics(credentials).onSuccess { host = it }
            apiClient.apiStats(credentials).onSuccess { stats = it }
            apiClient.listEndpoints(credentials).onSuccess { endpoints = it }
            apiClient.listAgentInstances(credentials).onSuccess { agents = it }
            loading = false
        }
    }

    LaunchedEffect(Unit) { refresh() }

    Scaffold(
        modifier = Modifier.nestedScroll(scrollBehavior.nestedScrollConnection),
        contentWindowInsets = tabContentWindowInsets(),
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("API Monitor")
                        Text(
                            text = "面板总览",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                },
                actions = {
                    IconButton(onClick = {
                        haptics.tap()
                        refresh()
                    }, enabled = !loading) {
                        Icon(Icons.Filled.Refresh, contentDescription = "刷新")
                    }
                    IconButton(onClick = {
                        haptics.tap()
                        onOpenSettings()
                    }) {
                        Icon(Icons.Filled.Settings, contentDescription = "设置")
                    }
                },
                scrollBehavior = scrollBehavior,
            )
        },
    ) { innerPadding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            item {
                OverviewCards(
                    serverCount = serverState.servers.size,
                    onlineCount = serverState.servers.count { it.isLive },
                    endpointCount = endpoints.size,
                    enabledEndpointCount = endpoints.count { it.enabled },
                    agentCount = agents.size,
                    runningAgents = agents.count { it.running },
                    loading = loading,
                )
            }

            item {
                GatewayTrendCard(stats = stats, loading = loading)
            }

            item {
                PanelLoadCard(host = host, loading = loading)
            }

            item {
                OutlinedCard(modifier = Modifier.fillMaxWidth()) {
                    ListItem(
                        headlineContent = {
                            Text(if (credentials.usesApiToken) "API Token 模式" else "密码登录")
                        },
                        supportingContent = { Text(credentials.normalizedBaseUrl) },
                        trailingContent = {
                            IconButton(onClick = {
                                haptics.danger()
                                onLogout()
                            }) {
                                Icon(
                                    Icons.AutoMirrored.Filled.Logout,
                                    contentDescription = "退出登录",
                                    tint = MaterialTheme.colorScheme.error,
                                )
                            }
                        },
                    )
                }
            }

            item { Spacer(Modifier.height(8.dp)) }
        }
    }
}

@Composable
private fun OverviewCards(
    serverCount: Int,
    onlineCount: Int,
    endpointCount: Int,
    enabledEndpointCount: Int,
    agentCount: Int,
    runningAgents: Int,
    loading: Boolean,
) {
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        StatCard(
            value = if (loading && serverCount == 0) "—" else "$onlineCount/$serverCount",
            label = "主机在线",
            modifier = Modifier.weight(1f),
        )
        StatCard(
            value = if (loading && endpointCount == 0) "—" else "$enabledEndpointCount/$endpointCount",
            label = "网关端点",
            modifier = Modifier.weight(1f),
        )
        StatCard(
            value = if (loading && agentCount == 0) "—" else "$runningAgents/$agentCount",
            label = "Agent 运行",
            modifier = Modifier.weight(1f),
        )
    }
}

@Composable
private fun StatCard(value: String, label: String, modifier: Modifier = Modifier) {
    OutlinedCard(modifier = modifier) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .padding(vertical = 16.dp, horizontal = 8.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            Text(
                text = value,
                style = MaterialTheme.typography.headlineSmall,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.primary,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                text = label,
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}

@Composable
private fun GatewayTrendCard(stats: ApiStats?, loading: Boolean) {
    OutlinedCard(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp)) {
            Row(
                modifier = Modifier.fillMaxWidth(),
                horizontalArrangement = Arrangement.SpaceBetween,
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Text(
                    text = "网关请求趋势",
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                if (loading && stats == null) {
                    CircularProgressIndicator(
                        modifier = Modifier.height(16.dp),
                        strokeWidth = 2.dp,
                    )
                }
            }
            Spacer(Modifier.height(4.dp))
            Text(
                text = stats?.let {
                    "总请求 ${formatCompact(it.total.all.toDouble())} · 令牌 ${formatCompact(it.tokens.toDouble())}"
                } ?: "暂无数据",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(12.dp))

            val trend = stats?.trend.orEmpty().takeLast(7)
            if (trend.isEmpty()) {
                Text(
                    text = "暂无趋势数据",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            } else {
                TrendChart(
                    values = trend.map { it.total.toDouble() },
                    labels = trend.map { it.shortLabel },
                )
            }
        }
    }
}

@Composable
private fun PanelLoadCard(host: HostMetrics?, loading: Boolean) {
    OutlinedCard(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text(
                text = "面板负载",
                style = MaterialTheme.typography.titleSmall,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Spacer(Modifier.height(2.dp))
            Text(
                text = host?.platformLabel ?: "读取中…",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(12.dp))

            MetricBar(
                label = "CPU",
                percent = host?.cpu?.usage?.toInt(),
                detail = host?.cpu?.let { "${it.logicalCores} 逻辑核" },
            )
            Spacer(Modifier.height(12.dp))
            MetricBar(
                label = "内存",
                percent = host?.memory?.usage?.toInt(),
                detail = host?.memory?.let { "${gb(it.used)} / ${gb(it.total)}" },
            )
            Spacer(Modifier.height(12.dp))
            MetricBar(
                label = "磁盘",
                percent = host?.disk?.usage?.toInt(),
                detail = host?.disk?.let { "${gb(it.used)} / ${gb(it.total)}" },
            )
        }
    }
}

private fun gb(bytes: Long): String {
    val gbValue = bytes / 1024.0 / 1024.0 / 1024.0
    return if (gbValue >= 1) "${(gbValue * 10).toInt() / 10.0} GB" else "${bytes / 1024 / 1024} MB"
}
