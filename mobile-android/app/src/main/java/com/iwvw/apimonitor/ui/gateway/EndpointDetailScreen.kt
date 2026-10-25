package com.iwvw.apimonitor.ui.gateway

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
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.Check
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Visibility
import androidx.compose.material.icons.filled.VisibilityOff
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.iwvw.apimonitor.data.model.EndpointHealth
import com.iwvw.apimonitor.data.model.OpenAiEndpoint
import com.iwvw.apimonitor.ui.common.StatusDot
import com.iwvw.apimonitor.ui.common.motionItem
import com.iwvw.apimonitor.ui.common.rememberHaptics
import com.iwvw.apimonitor.ui.common.fullScreenWindowInsets
import com.iwvw.apimonitor.ui.home.formatCompact
import kotlinx.coroutines.launch

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun EndpointDetailScreen(
    detail: EndpointDetailUiState,
    onBack: () -> Unit,
    onRefresh: () -> Unit,
    onToggleEnabled: (OpenAiEndpoint) -> Unit,
    onToggleModel: (String, Boolean) -> Unit,
    onSave: (String, String, String, String, Boolean, Boolean, (Boolean, String) -> Unit) -> Unit,
    onDelete: ((Boolean, String) -> Unit) -> Unit,
) {
    val endpoint = detail.endpoint
    val haptics = rememberHaptics()
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()
    val clipboard = LocalClipboardManager.current
    val scrollBehavior = TopAppBarDefaults.pinnedScrollBehavior()

    var showEdit by remember { mutableStateOf(false) }
    var showDelete by remember { mutableStateOf(false) }
    var showKey by remember { mutableStateOf(false) }
    var modelQuery by remember { mutableStateOf("") }

    LaunchedEffect(detail.endpointId) {
        modelQuery = ""
    }

    Scaffold(
        contentWindowInsets = fullScreenWindowInsets(),
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(
                            text = endpoint?.name?.ifBlank { detail.endpointId.take(12) } ?: "端点详情",
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        if (endpoint != null) {
                            Text(
                                text = endpoint.statusLabel,
                                style = MaterialTheme.typography.labelMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                },
                navigationIcon = {
                    IconButton(onClick = {
                        haptics.tap()
                        onBack()
                    }) {
                        Icon(Icons.Filled.ArrowBack, contentDescription = "返回")
                    }
                },
                actions = {
                    IconButton(onClick = {
                        haptics.tap()
                        onRefresh()
                    }) {
                        Icon(Icons.Filled.Refresh, contentDescription = "刷新")
                    }
                    IconButton(onClick = {
                        haptics.tap()
                        showEdit = true
                    }) {
                        Icon(Icons.Filled.Edit, contentDescription = "编辑")
                    }
                    IconButton(onClick = {
                        haptics.danger()
                        showDelete = true
                    }) {
                        Icon(
                            Icons.Filled.Delete,
                            contentDescription = "删除",
                            tint = MaterialTheme.colorScheme.error,
                        )
                    }
                },
                scrollBehavior = scrollBehavior,
            )
        },
    ) { innerPadding ->
        if (endpoint == null) {
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .padding(innerPadding),
                contentAlignment = Alignment.Center,
            ) {
                if (detail.loading) {
                    CircularProgressIndicator()
                } else {
                    Text(
                        text = "端点不存在或已被删除",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            return@Scaffold
        }

        val filteredModels = remember(detail.models, modelQuery) {
            if (modelQuery.isBlank()) {
                detail.models
            } else {
                detail.models.filter { it.contains(modelQuery, ignoreCase = true) }
            }
        }

        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            item("status") {
                StatusCard(
                    endpoint = endpoint,
                    health = detail.health,
                    onToggle = { onToggleEnabled(endpoint) },
                )
            }

            item("access") {
                AccessCard(
                    endpoint = endpoint,
                    revealKey = showKey,
                    onToggleReveal = {
                        haptics.tap()
                        showKey = !showKey
                    },
                    onCopy = { value, label ->
                        clipboard.setText(AnnotatedString(value))
                        haptics.confirm()
                        scope.launch { snackbar.showSnackbar("$label 已复制") }
                    },
                )
            }

            item("models-header") {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = "模型",
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.SemiBold,
                        modifier = Modifier.weight(1f),
                    )
                    Text(
                        text = "${detail.enabledModelCount}/${detail.models.size} 可用",
                        style = MaterialTheme.typography.labelMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            item("models-search") {
                OutlinedTextField(
                    value = modelQuery,
                    onValueChange = { modelQuery = it.filter(Char::isAsciiPrintable) },
                    modifier = Modifier.fillMaxWidth(),
                    label = { Text("搜索模型") },
                    singleLine = true,
                    trailingIcon = {
                        androidx.compose.animation.AnimatedVisibility(visible = modelQuery.isNotEmpty()) {
                            IconButton(onClick = { modelQuery = "" }) {
                                Icon(Icons.Filled.Clear, contentDescription = "清空")
                            }
                        }
                    },
                )
            }

            item("models-empty") {
                androidx.compose.animation.AnimatedVisibility(visible = filteredModels.isEmpty()) {
                    Text(
                        text = if (detail.models.isEmpty()) "该端点没有上报模型列表" else "没有匹配的模型",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            items(filteredModels, key = { "model-$it" }) { model ->
                ModelRow(
                    name = model,
                    enabled = model !in detail.disabledModelSet,
                    modifier = motionItem(),
                    onToggle = { enabled -> onToggleModel(model, enabled) },
                )
            }
        }
    }

    if (showEdit && endpoint != null) {
        EndpointEditDialog(
            endpoint = endpoint,
            saving = detail.saving,
            onDismiss = { showEdit = false },
            onSave = { name, baseUrl, apiKey, protocol, autoSwitch, skipVerify ->
                onSave(name, baseUrl, apiKey, protocol, autoSwitch, skipVerify) { ok, msg ->
                    scope.launch { snackbar.showSnackbar(msg) }
                    if (ok) {
                        haptics.confirm()
                        showEdit = false
                    } else {
                        haptics.reject()
                    }
                }
            },
        )
    }

    if (showDelete && endpoint != null) {
        AlertDialog(
            onDismissRequest = { showDelete = false },
            title = { Text("删除端点") },
            text = {
                Text(
                    "确定删除「${endpoint.name}」？删除后该端点的配置与模型列表都会丢失，" +
                        "正在使用它的请求会立即失败。此操作不可撤销。",
                )
            },
            confirmButton = {
                TextButton(
                    onClick = {
                        showDelete = false
                        onDelete { ok, msg ->
                            scope.launch { snackbar.showSnackbar(msg) }
                            if (ok) {
                                haptics.confirm()
                                onBack()
                            } else {
                                haptics.reject()
                            }
                        }
                    },
                ) {
                    Text("删除", color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(onClick = { showDelete = false }) { Text("取消") }
            },
        )
    }
}

private fun Char.isAsciiPrintable(): Boolean = this.code in 0x20..0x7E

private val OpenAiEndpoint.statusLabel: String
    get() = when {
        !enabled -> "已停用"
        status == "valid" -> "运行中"
        status == "invalid" -> "密钥无效"
        status.isNullOrBlank() -> "未检查"
        else -> status
    }

@Composable
private fun StatusCard(
    endpoint: OpenAiEndpoint,
    health: EndpointHealth?,
    onToggle: () -> Unit,
) {
    OutlinedCard(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                StatusDot(active = endpoint.enabled, modifier = Modifier.size(10.dp))
                Spacer(Modifier.width(10.dp))
                Column(modifier = Modifier.weight(1f)) {
                    Text(
                        text = endpoint.statusLabel,
                        style = MaterialTheme.typography.titleSmall,
                        fontWeight = FontWeight.SemiBold,
                    )
                    Text(
                        text = buildString {
                            append("权重 ${endpoint.weight}")
                            append(" · 协议 ${endpoint.protocol}")
                            if (endpoint.isPlugin) append(" · 插件 ${endpoint.pluginId}")
                        },
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Switch(checked = endpoint.enabled, onCheckedChange = { onToggle() })
            }

            Spacer(Modifier.height(12.dp))
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            Spacer(Modifier.height(12.dp))

            Row(horizontalArrangement = Arrangement.spacedBy(24.dp)) {
                DetailStat("健康", health?.statusLabel ?: "—")
                DetailStat(
                    label = "平均延迟",
                    value = health?.averageLatencyMs?.let { "${it}ms" } ?: "—",
                )
                DetailStat(
                    label = "成功率",
                    value = health?.successRate?.let { "${(it * 100).toInt()}%" } ?: "—",
                )
            }

            if (health != null && health.history.isNotEmpty()) {
                Spacer(Modifier.height(12.dp))
                val rate = (health.successRate ?: 0.0).toFloat().coerceIn(0f, 1f)
                LinearProgressIndicator(
                    progress = { rate },
                    modifier = Modifier.fillMaxWidth(),
                    trackColor = MaterialTheme.colorScheme.surfaceContainerHighest,
                )
                Spacer(Modifier.height(6.dp))
                Text(
                    text = "最近 ${health.history.size} 次检查 · 成功 ${health.history.count { it.status == "operational" }} 次",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }

            Spacer(Modifier.height(12.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(24.dp)) {
                DetailStat("上次检查", shortTime(endpoint.lastChecked))
                DetailStat("上次使用", shortTime(endpoint.lastUsed))
            }
        }
    }
}

@Composable
private fun AccessCard(
    endpoint: OpenAiEndpoint,
    revealKey: Boolean,
    onToggleReveal: () -> Unit,
    onCopy: (String, String) -> Unit,
) {
    OutlinedCard(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp)) {
            Text(
                text = "接入信息",
                style = MaterialTheme.typography.titleSmall,
                fontWeight = FontWeight.SemiBold,
            )
            Spacer(Modifier.height(12.dp))

            CopyRow(
                label = "地址",
                value = endpoint.baseUrl,
                onCopy = { onCopy(endpoint.baseUrl, "地址") },
            )
            Spacer(Modifier.height(10.dp))
            CopyRow(
                label = "API Key",
                value = if (revealKey) endpoint.apiKey else endpoint.maskedApiKey,
                onCopy = { onCopy(endpoint.apiKey, "API Key") },
                trailing = {
                    IconButton(onClick = onToggleReveal) {
                        Icon(
                            imageVector = if (revealKey) {
                                Icons.Filled.VisibilityOff
                            } else {
                                Icons.Filled.Visibility
                            },
                            contentDescription = if (revealKey) "隐藏" else "显示",
                        )
                    }
                },
            )
            if (endpoint.notes.isNotBlank()) {
                Spacer(Modifier.height(10.dp))
                Text(
                    text = endpoint.notes,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun CopyRow(
    label: String,
    value: String,
    onCopy: () -> Unit,
    trailing: @Composable (() -> Unit)? = null,
) {
    Column {
        Text(
            text = label,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Row(verticalAlignment = Alignment.CenterVertically) {
            Text(
                text = value.ifBlank { "—" },
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.weight(1f),
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            if (trailing != null) {
                trailing()
            } else {
                IconButton(onClick = onCopy, enabled = value.isNotBlank()) {
                    Icon(
                        Icons.Filled.ContentCopy,
                        contentDescription = "复制$label",
                        modifier = Modifier.size(18.dp),
                    )
                }
            }
        }
    }
}

@Composable
private fun DetailStat(label: String, value: String) {
    Column {
        Text(
            text = value,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.Medium,
        )
        Text(
            text = label,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun ModelRow(
    name: String,
    enabled: Boolean,
    modifier: Modifier = Modifier,
    onToggle: (Boolean) -> Unit,
) {
    OutlinedCard(modifier = modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier.padding(start = 16.dp, top = 6.dp, end = 8.dp, bottom = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = name,
                style = MaterialTheme.typography.bodyMedium,
                modifier = Modifier.weight(1f),
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            Switch(checked = enabled, onCheckedChange = onToggle)
        }
    }
}

@Composable
private fun EndpointEditDialog(
    endpoint: OpenAiEndpoint,
    saving: Boolean,
    onDismiss: () -> Unit,
    onSave: (String, String, String, String, Boolean, Boolean) -> Unit,
) {
    var name by remember { mutableStateOf(endpoint.name) }
    var baseUrl by remember { mutableStateOf(endpoint.baseUrl) }
    var apiKey by remember { mutableStateOf(endpoint.apiKey) }
    var protocol by remember { mutableStateOf(endpoint.protocol.ifBlank { "auto" }) }
    var autoSwitch by remember { mutableStateOf(endpoint.autoSwitch) }
    var skipVerify by remember { mutableStateOf(endpoint.skipVerify) }

    val nameError = name.isBlank()
    val baseUrlError = baseUrl.isBlank()
    val apiKeyError = apiKey.isBlank()

    AlertDialog(
        onDismissRequest = { if (!saving) onDismiss() },
        title = { Text("编辑端点") },
        text = {
            Column(modifier = Modifier.fillMaxWidth()) {
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text("名称") },
                    singleLine = true,
                    isError = nameError,
                    supportingText = if (nameError) {
                        { Text("名称不能为空") }
                    } else {
                        null
                    },
                    modifier = Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(8.dp))
                OutlinedTextField(
                    value = baseUrl,
                    onValueChange = { baseUrl = it.filter(Char::isAsciiPrintable) },
                    label = { Text("Base URL") },
                    singleLine = true,
                    isError = baseUrlError,
                    supportingText = if (baseUrlError) {
                        { Text("地址不能为空") }
                    } else {
                        null
                    },
                    modifier = Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(8.dp))
                OutlinedTextField(
                    value = apiKey,
                    onValueChange = { apiKey = it.filter(Char::isAsciiPrintable) },
                    label = { Text("API Key") },
                    singleLine = true,
                    isError = apiKeyError,
                    supportingText = if (apiKeyError) {
                        { Text("Key 不能为空") }
                    } else {
                        null
                    },
                    modifier = Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(8.dp))
                OutlinedTextField(
                    value = protocol,
                    onValueChange = { protocol = it.filter(Char::isAsciiPrintable) },
                    label = { Text("协议（auto / http1 / h2）") },
                    singleLine = true,
                    modifier = Modifier.fillMaxWidth(),
                )
                Spacer(Modifier.height(12.dp))
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = "失败自动切换",
                        style = MaterialTheme.typography.bodyMedium,
                        modifier = Modifier.weight(1f),
                    )
                    Switch(checked = autoSwitch, onCheckedChange = { autoSwitch = it })
                }
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(
                        text = "跳过证书校验",
                        style = MaterialTheme.typography.bodyMedium,
                        modifier = Modifier.weight(1f),
                    )
                    Switch(checked = skipVerify, onCheckedChange = { skipVerify = it })
                }
            }
        },
        confirmButton = {
            TextButton(
                enabled = !saving && !nameError && !baseUrlError && !apiKeyError,
                onClick = {
                    onSave(name, baseUrl, apiKey, protocol, autoSwitch, skipVerify)
                },
            ) {
                if (saving) {
                    CircularProgressIndicator(modifier = Modifier.size(16.dp), strokeWidth = 2.dp)
                    Spacer(Modifier.width(8.dp))
                }
                Text("保存")
            }
        },
        dismissButton = {
            TextButton(enabled = !saving, onClick = onDismiss) { Text("取消") }
        },
    )
}

/** 把 ISO 时间压成 MM-dd HH:mm，读不出来就原样返回。 */
internal fun shortTime(raw: String?): String {
    val value = raw?.takeIf { it.isNotBlank() } ?: return "—"
    val datePart = value.substringBefore('T')
    val timePart = value.substringAfter('T', "").take(5)
    return if (timePart.isBlank()) {
        datePart
    } else {
        "${datePart.takeLast(5)} $timePart"
    }
}
