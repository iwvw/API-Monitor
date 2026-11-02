package com.iwvw.apimonitor.ui.aiagent

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
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
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
import androidx.lifecycle.viewmodel.compose.viewModel
import com.iwvw.apimonitor.ui.common.StatusDot
import com.iwvw.apimonitor.ui.common.motionItem
import com.iwvw.apimonitor.ui.common.tabContentWindowInsets
import com.iwvw.apimonitor.data.model.AgentAction
import com.iwvw.apimonitor.data.model.AgentInstance
import com.iwvw.apimonitor.data.model.AgentMetrics
import com.iwvw.apimonitor.data.network.ApiClient
import com.iwvw.apimonitor.data.settings.Credentials
import com.iwvw.apimonitor.ui.common.rememberHaptics
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun AiAgentScreen(
    credentials: Credentials,
    apiClient: ApiClient,
) {
    val viewModel: AiAgentViewModel = viewModel(
        key = "aiagent-${credentials.normalizedBaseUrl}",
        factory = AiAgentViewModel.factory(credentials, apiClient),
    )
    val state by viewModel.state.collectAsState()
    val haptics = rememberHaptics()
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    val scrollBehavior = TopAppBarDefaults.pinnedScrollBehavior()

    var pendingAction by remember { mutableStateOf<Pair<AgentInstance, AgentAction>?>(null) }

    Scaffold(
        modifier = Modifier.nestedScroll(scrollBehavior.nestedScrollConnection),
        contentWindowInsets = tabContentWindowInsets(),
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("AI Agent")
                        Text(
                            text = "${state.instances.count { it.running }}/${state.instances.size} 运行中",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                },
                actions = {
                    IconButton(onClick = {
                        haptics.tap()
                        viewModel.refresh()
                    }) {
                        Icon(Icons.Filled.Refresh, contentDescription = "刷新")
                    }
                },
                scrollBehavior = scrollBehavior,
            )
        },
    ) { innerPadding ->
        if (state.loading && state.instances.isEmpty()) {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(innerPadding),
                contentAlignment = Alignment.Center,
            ) {
                CircularProgressIndicator()
            }
            return@Scaffold
        }

        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            item { ModuleStatusCard(state.metrics) }

            items(state.instances, key = { it.id }) { instance ->
                Box(modifier = motionItem()) {
                    InstanceCard(
                        instance = instance,
                        busy = state.busyInstanceId == instance.id,
                        onAction = { action ->
                            if (action == AgentAction.STOP) {
                                haptics.danger()
                                pendingAction = instance to action
                            } else {
                                haptics.tap()
                                viewModel.runAction(instance, action) { ok, msg ->
                                    scope.launch { snackbar.showSnackbar(msg) }
                                    if (ok) haptics.confirm() else haptics.reject()
                                }
                            }
                        },
                    )
                }
            }

            item { Spacer(Modifier.height(8.dp)) }
        }
    }

    pendingAction?.let { (instance, action) ->
        AlertDialog(
            onDismissRequest = { pendingAction = null },
            title = { Text("确认${action.label}实例") },
            text = {
                Text(
                    "即将${action.label}「${instance.displayName}」上的 ${instance.providerLabel} 实例。\n" +
                        "端口 :${instance.port}，停止后该实例的接入地址将不可用。",
                )
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        pendingAction = null
                        haptics.confirm()
                        viewModel.runAction(instance, action) { ok, msg ->
                            scope.launch { snackbar.showSnackbar(msg) }
                            if (!ok) haptics.reject()
                        }
                    },
                ) {
                    Text("确认${action.label}", color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(onClick = { pendingAction = null }) { Text("取消") }
            },
        )
    }
}

@Composable
private fun ModuleStatusCard(metrics: AgentMetrics?) {
    OutlinedCard(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text(
                text = "模块运行",
                style = MaterialTheme.typography.titleSmall,
                color = MaterialTheme.colorScheme.onSurface,
            )
            Spacer(Modifier.height(8.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(24.dp)) {
                StatusStat("运行时长", metrics?.uptimeLabel ?: "—")
                StatusStat("收敛轮次", metrics?.convergenceRounds?.toString() ?: "—")
            }
            Spacer(Modifier.height(10.dp))
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            Spacer(Modifier.height(10.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(24.dp)) {
                StatusStat("启动", metrics?.lifecycleStarts?.toString() ?: "—")
                StatusStat("停止", metrics?.lifecycleStops?.toString() ?: "—")
                StatusStat(
                    "失败",
                    ((metrics?.lifecycleFailures ?: 0) + (metrics?.convergenceFailures ?: 0)).toString(),
                )
            }
        }
    }
}

@Composable
private fun StatusStat(label: String, value: String) {
    Column {
        Text(
            text = value,
            style = MaterialTheme.typography.bodyLarge,
            fontWeight = FontWeight.SemiBold,
        )
        Text(
            text = label,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun InstanceCard(
    instance: AgentInstance,
    busy: Boolean,
    onAction: (AgentAction) -> Unit,
) {
    OutlinedCard(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                StatusDot(
                    active = instance.running && !instance.crashed,
                    neutral = !instance.running,
                )
                Text(
                    text = instance.stateLabel,
                    style = MaterialTheme.typography.labelSmall,
                    color = when {
                        instance.crashed -> MaterialTheme.colorScheme.error
                        instance.running -> MaterialTheme.colorScheme.primary
                        else -> MaterialTheme.colorScheme.onSurfaceVariant
                    },
                )
            }

            Spacer(Modifier.height(6.dp))
            Text(
                text = "${instance.providerLabel} · :${instance.port} · 主机 ${instance.hostName}",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )

            Spacer(Modifier.height(12.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                if (busy) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(20.dp),
                        strokeWidth = 2.dp,
                    )
                } else {
                    FilledTonalButton(
                        onClick = { onAction(AgentAction.START) },
                        enabled = !instance.running && instance.hostOnline,
                    ) {
                        Text("启动")
                    }
                    OutlinedButton(
                        onClick = { onAction(AgentAction.RESTART) },
                        enabled = instance.hostOnline,
                    ) {
                        Text("重启")
                    }
                    OutlinedButton(
                        onClick = { onAction(AgentAction.STOP) },
                        enabled = instance.running,
                    ) {
                        Text("停止", color = MaterialTheme.colorScheme.error)
                    }
                }
            }
        }
    }
}
