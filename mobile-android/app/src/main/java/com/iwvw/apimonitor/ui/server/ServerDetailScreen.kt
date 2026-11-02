package com.iwvw.apimonitor.ui.server

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ColumnScope
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Terminal
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import com.iwvw.apimonitor.ui.common.fullScreenWindowInsets
import com.iwvw.apimonitor.data.model.ServerSummary
import com.iwvw.apimonitor.data.model.parsePercent

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun ServerDetailScreen(
    server: ServerSummary?,
    realtimeConnected: Boolean,
    onBack: () -> Unit,
    onOpenTerminal: () -> Unit,
) {
    val scrollBehavior = TopAppBarDefaults.pinnedScrollBehavior()

    Scaffold(

        contentWindowInsets = fullScreenWindowInsets(),
        modifier = Modifier.nestedScroll(scrollBehavior.nestedScrollConnection),
        topBar = {
            TopAppBar(
                title = { Text(server?.name?.ifBlank { "服务器详情" } ?: "服务器详情") },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回")
                    }
                },
                actions = {
                    RealtimeBadge(connected = realtimeConnected)
                    // 主机离线或 Agent 未声明终端能力时不给入口：点进去只会
                    // 在终端页报错，不如直接禁用并说明原因。
                    IconButton(
                        onClick = onOpenTerminal,
                        enabled = server?.isLive == true && server.supportsTerminal,
                    ) {
                        Icon(
                            Icons.Filled.Terminal,
                            contentDescription = if (server?.supportsTerminal == true) {
                                "终端"
                            } else {
                                "当前主机不支持终端"
                            },
                        )
                    }
                },                scrollBehavior = scrollBehavior,
            )
        },
    ) { innerPadding ->
        if (server == null) {
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(innerPadding)
                    .padding(32.dp),
                verticalArrangement = Arrangement.Center,
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                Text(
                    text = "服务器已不存在或已被移除",
                    style = MaterialTheme.typography.bodyLarge,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            return@Scaffold
        }
        DetailContent(innerPadding = innerPadding, server = server)
    }
}

@Composable
private fun DetailContent(innerPadding: PaddingValues, server: ServerSummary) {
    val info = server.info

    Column(
        modifier = Modifier
            .fillMaxSize()
            .padding(innerPadding)
            .verticalScroll(rememberScrollState())
            .padding(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        SectionCard {
            Text(
                text = server.displayLocation,
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.SemiBold,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Spacer(Modifier.height(6.dp))
            Text(
                text = listOfNotNull(
                    info?.platform,
                    info?.platformVersion,
                    info?.agentVersion?.let { "Agent $it" },
                    info?.uptime?.let { "已运行 $it" },
                ).joinToString(" · ").ifBlank { "—" },
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            info?.ip?.let {
                Spacer(Modifier.height(2.dp))
                Text(
                    text = it,
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }

        if (info != null) {
            SectionCard {
                Text(
                    text = "实时负载",
                    style = MaterialTheme.typography.titleSmall,
                    color = MaterialTheme.colorScheme.onSurface,
                )
                Spacer(Modifier.height(14.dp))
                MetricBar(
                    label = "CPU",
                    percent = info.cpu?.usage.parsePercent(),
                    detail = info.cpu?.let { cpu ->
                        listOfNotNull(
                            cpu.model,
                            cpu.temp?.takeIf { t -> t > 0 }?.let { "温度 ${it.toInt()}°C" },
                            cpu.load?.let { "负载 $it" },
                        ).joinToString(" · ")
                    },
                    detailMaxLines = 2,
                )
                Spacer(Modifier.height(14.dp))
                MetricBar(
                    label = "内存",
                    percent = info.memory?.usage.parsePercent(),
                    detail = info.memory?.let {
                        "${it.used} / ${it.total}".trim()
                    },
                )
                info.disk.forEach { disk ->
                    Spacer(Modifier.height(14.dp))
                    MetricBar(
                        label = "磁盘 ${disk.device}",
                        percent = disk.usage.parsePercent(),
                        detail = "${disk.used} / ${disk.total}",
                    )
                }
            }

            if (info.gpu.isNotEmpty() || info.network != null) {
                SectionCard {
                    Text(
                        text = "网络与加速卡",
                        style = MaterialTheme.typography.titleSmall,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Spacer(Modifier.height(12.dp))
                    if (info.gpu.size > 1) {
                        Text(
                            text = "GPU × ${info.gpu.size}",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        Spacer(Modifier.height(6.dp))
                    }
                    info.gpu.firstOrNull()?.let { gpu ->
                        if (gpu.name.isNotBlank()) {
                            Text(
                                text = gpu.name,
                                style = MaterialTheme.typography.bodyMedium,
                                color = MaterialTheme.colorScheme.onSurface,
                                maxLines = 2,
                            )
                        }
                        gpu.usage?.let { usage ->
                            Spacer(Modifier.height(8.dp))
                            MetricBar(
                                label = "GPU",
                                percent = usage,
                                detail = gpu.temp?.takeIf { it > 0 }?.let { "温度 $it°C" },
                            )
                        }
                    }
                    info.network?.let { net ->
                        Spacer(Modifier.height(14.dp))
                        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                        Spacer(Modifier.height(12.dp))
                        Row(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
                            ServerStatChip("下行", net.rxSpeed.ifBlank { "—" })
                            ServerStatChip("上行", net.txSpeed.ifBlank { "—" })
                            ServerStatChip("连接", net.connections?.toString() ?: "—")
                        }
                        Spacer(Modifier.height(10.dp))
                        Row(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
                            ServerStatChip("累计接收", net.rxTotal.ifBlank { "—" })
                            ServerStatChip("累计发送", net.txTotal.ifBlank { "—" })
                        }
                    }
                }
            }

            info.docker?.let { docker ->
                SectionCard {
                    Text(
                        text = "Docker",
                        style = MaterialTheme.typography.titleSmall,
                        color = MaterialTheme.colorScheme.onSurface,
                    )
                    Spacer(Modifier.height(6.dp))
                    Text(
                        text = if (docker.installed) {
                            "运行 ${docker.running ?: 0} · 停止 ${docker.stopped ?: 0}"
                        } else {
                            "未安装"
                        },
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

@Composable
private fun SectionCard(content: @Composable ColumnScope.() -> Unit) {
    OutlinedCard(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp), content = content)
    }
}
