package com.iwvw.apimonitor.ui.gateway

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
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
import androidx.compose.foundation.lazy.rememberLazyListState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Edit
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Sync
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.DropdownMenu
import androidx.compose.material3.DropdownMenuItem
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SegmentedButton
import androidx.compose.material3.SegmentedButtonDefaults
import androidx.compose.material3.SingleChoiceSegmentedButtonRow
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.iwvw.apimonitor.data.model.GatewayKey
import com.iwvw.apimonitor.data.model.GatewayLog
import com.iwvw.apimonitor.data.model.GatewayPlugin
import com.iwvw.apimonitor.data.model.OpenAiEndpoint
import com.iwvw.apimonitor.data.network.ApiClient
import com.iwvw.apimonitor.data.settings.Credentials
import com.iwvw.apimonitor.ui.common.ExpandableContent
import com.iwvw.apimonitor.ui.common.Motion
import com.iwvw.apimonitor.ui.common.SharedAxisXContent
import com.iwvw.apimonitor.ui.common.StatusBadge
import com.iwvw.apimonitor.ui.common.StatusDot
import com.iwvw.apimonitor.ui.common.motionItem
import com.iwvw.apimonitor.ui.common.predictiveBackEnter
import com.iwvw.apimonitor.ui.common.predictiveBackExit
import com.iwvw.apimonitor.ui.common.pushEnter
import com.iwvw.apimonitor.ui.common.pushExit
import com.iwvw.apimonitor.ui.common.rememberHaptics
import com.iwvw.apimonitor.ui.common.tabContentWindowInsets
import com.iwvw.apimonitor.ui.home.formatCompact
import kotlinx.coroutines.launch

private enum class GatewayTab(val label: String) {
    ENDPOINTS("端点"),
    KEYS("密钥"),
    LOGS("日志"),
    PLUGINS("插件"),
}

@Composable
fun GatewayScreen(
    viewModel: GatewayViewModel,
    onOpenEndpoint: (OpenAiEndpoint) -> Unit,
    onOpenPlugin: (GatewayPlugin) -> Unit,
) {
    val state by viewModel.state.collectAsState()
    val haptics = rememberHaptics()
    val snackbar = remember { SnackbarHostState() }
    val scope = rememberCoroutineScope()

    var tab by rememberSaveable { mutableStateOf(GatewayTab.ENDPOINTS) }
    var editingKey by remember { mutableStateOf<GatewayKey?>(null) }
    var deletingKey by remember { mutableStateOf<GatewayKey?>(null) }
    var rotatingKey by remember { mutableStateOf<GatewayKey?>(null) }
    var rotatedKey by remember { mutableStateOf("") }

    fun notify(message: String) {
        scope.launch { snackbar.showSnackbar(message) }
    }

    GatewayTabs(
        state = state,
        tab = tab,
        onTabChange = { next ->
            haptics.segment()
            tab = next
        },
        snackbar = snackbar,
        onRefresh = {
            haptics.tap()
            viewModel.refresh()
        },
        onOpenEndpoint = onOpenEndpoint,
        onToggleEndpoint = { endpoint ->
            haptics.toggle(!endpoint.enabled)
            viewModel.toggleEndpoint(endpoint) { ok, msg ->
                notify(msg)
                if (!ok) haptics.reject()
            }
        },
        onLoadMoreLogs = { viewModel.loadMoreLogs() },
        onEditKey = { key ->
            haptics.tap()
            editingKey = key
        },
        onToggleKey = { key, enabled ->
            haptics.toggle(enabled)
            viewModel.toggleKey(key, enabled) { ok, msg ->
                notify(msg)
                if (!ok) haptics.reject()
            }
        },
        onRotateKey = { key ->
            haptics.danger()
            rotatingKey = key
        },
        onDeleteKey = { key ->
            haptics.danger()
            deletingKey = key
        },
        onOpenPlugin = onOpenPlugin,
    )

    GatewayDialogs(
        editingKey = editingKey,
        deletingKey = deletingKey,
        rotatingKey = rotatingKey,
        rotatedKey = rotatedKey,
        onDismissEdit = { editingKey = null },
        onSaveKey = { key, name, expiresAt ->
            viewModel.updateKey(key, name, expiresAt) { ok, msg ->
                notify(msg)
                if (ok) {
                    haptics.confirm()
                    editingKey = null
                } else {
                    haptics.reject()
                }
            }
        },
        onDismissDelete = { deletingKey = null },
        onConfirmDelete = { key ->
            deletingKey = null
            viewModel.deleteKey(key) { ok, msg ->
                notify(msg)
                if (ok) haptics.confirm() else haptics.reject()
            }
        },
        onDismissRotate = { rotatingKey = null },
        onConfirmRotate = { key ->
            rotatingKey = null
            viewModel.rotateKey(key) { ok, msg, newKey ->
                notify(msg)
                if (ok) {
                    haptics.confirm()
                    rotatedKey = newKey
                } else {
                    haptics.reject()
                }
            }
        },
        onDismissNewKey = { rotatedKey = "" },
    )
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun GatewayTabs(
    state: GatewayUiState,
    tab: GatewayTab,
    onTabChange: (GatewayTab) -> Unit,
    snackbar: SnackbarHostState,
    onRefresh: () -> Unit,
    onOpenEndpoint: (OpenAiEndpoint) -> Unit,
    onToggleEndpoint: (OpenAiEndpoint) -> Unit,
    onLoadMoreLogs: () -> Unit,
    onEditKey: (GatewayKey) -> Unit,
    onToggleKey: (GatewayKey, Boolean) -> Unit,
    onRotateKey: (GatewayKey) -> Unit,
    onDeleteKey: (GatewayKey) -> Unit,
    onOpenPlugin: (GatewayPlugin) -> Unit,
) {
    val scrollBehavior = TopAppBarDefaults.pinnedScrollBehavior()

    Scaffold(
        modifier = Modifier.nestedScroll(scrollBehavior.nestedScrollConnection),
        contentWindowInsets = tabContentWindowInsets(),
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("模型网关")
                        Text(
                            text = "${state.endpoints.count { it.enabled }}/${state.endpoints.size} 端点启用",
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                },
                actions = {
                    IconButton(onClick = onRefresh) {
                        Icon(Icons.Filled.Refresh, contentDescription = "刷新")
                    }
                },
                scrollBehavior = scrollBehavior,
            )
        },
    ) { innerPadding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding),
        ) {
            SingleChoiceSegmentedButtonRow(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(horizontal = 16.dp, vertical = 8.dp),
            ) {
                GatewayTab.entries.forEachIndexed { index, entry ->
                    SegmentedButton(
                        selected = tab == entry,
                        onClick = { onTabChange(entry) },
                        shape = SegmentedButtonDefaults.itemShape(index, GatewayTab.entries.size),
                        label = { Text(entry.label) },
                    )
                }
            }

            SharedAxisXContent(
                targetKey = tab,
                order = GatewayTab.entries,
                modifier = Modifier.fillMaxSize(),
            ) { current ->
                when (current) {
                    GatewayTab.ENDPOINTS -> EndpointList(
                        endpoints = state.endpoints,
                        loading = state.loading,
                        onOpen = onOpenEndpoint,
                        onToggle = onToggleEndpoint,
                    )

                    GatewayTab.KEYS -> KeyList(
                        keys = state.keys,
                        loading = state.loading,
                        onEdit = onEditKey,
                        onToggle = onToggleKey,
                        onRotate = onRotateKey,
                        onDelete = onDeleteKey,
                    )

                    GatewayTab.LOGS -> LogList(
                        logs = state.logs,
                        loading = state.loading,
                        loadingMore = state.loadingMore,
                        hasMore = state.hasMore,
                        onLoadMore = onLoadMoreLogs,
                    )

                    GatewayTab.PLUGINS -> GatewayPluginsScreen(
                        onOpen = onOpenPlugin,
                    )
                }
            }
        }
    }
}

@Composable
private fun EndpointList(
    endpoints: List<OpenAiEndpoint>,
    loading: Boolean,
    onOpen: (OpenAiEndpoint) -> Unit,
    onToggle: (OpenAiEndpoint) -> Unit,
) {
    if (loading && endpoints.isEmpty()) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            CircularProgressIndicator()
        }
        return
    }

    if (endpoints.isEmpty()) {
        EmptyHint("还没有配置端点")
        return
    }

    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(endpoints, key = { it.id }) { endpoint ->
            EndpointCard(
                endpoint = endpoint,
                modifier = motionItem(),
                onOpen = { onOpen(endpoint) },
                onToggle = { onToggle(endpoint) },
            )
        }
    }
}

@Composable
private fun EndpointCard(
    endpoint: OpenAiEndpoint,
    modifier: Modifier = Modifier,
    onOpen: () -> Unit,
    onToggle: () -> Unit,
) {
    var expanded by rememberSaveable(endpoint.id) { mutableStateOf(false) }

    // 乐观开关：本地先翻，等刷新回来再对齐。
    // 直接用 endpoint.enabled 会在轮询/重组时闪回旧值（表现为「上下滑动不断刷新状态」）。
    var localEnabled by remember(endpoint.id) { mutableStateOf(endpoint.enabled) }
    var pending by remember(endpoint.id) { mutableStateOf(false) }

    LaunchedEffect(endpoint.enabled) {
        if (!pending) localEnabled = endpoint.enabled
    }

    val chevronRotation by animateFloatAsState(
        targetValue = if (expanded) 180f else 0f,
        animationSpec = tween(Motion.EXPAND_MS, easing = Motion.EmphasizedDecelerate),
        label = "endpoint-chevron",
    )

    OutlinedCard(
        onClick = onOpen,
        modifier = modifier.fillMaxWidth(),
    ) {
        Column {
            Row(
                modifier = Modifier.padding(start = 16.dp, top = 12.dp, end = 8.dp, bottom = 12.dp),
                verticalAlignment = Alignment.CenterVertically,
            ) {
                Column(modifier = Modifier.weight(1f)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            text = endpoint.name.ifBlank { endpoint.id.take(12) },
                            style = MaterialTheme.typography.titleSmall,
                            fontWeight = FontWeight.SemiBold,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                            modifier = Modifier.weight(1f, fill = false),
                        )
                        if (endpoint.isPlugin) {
                            Spacer(Modifier.width(6.dp))
                            StatusBadge("插件")
                        }
                    }
                    Spacer(Modifier.height(2.dp))
                    Text(
                        text = endpoint.hostLabel.ifBlank { "本地网关" },
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    Spacer(Modifier.height(4.dp))
                    Text(
                        text = "${endpoint.enabledModelCount}/${endpoint.models.size} 模型可用 · 权重 ${endpoint.weight}",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
                Switch(
                    checked = localEnabled,
                    onCheckedChange = {
                        localEnabled = it
                        pending = true
                        onToggle()
                    },
                )
                IconButton(onClick = { expanded = !expanded }) {
                    Icon(
                        imageVector = Icons.Filled.ExpandMore,
                        contentDescription = if (expanded) "收起" else "展开",
                        modifier = Modifier.rotate(chevronRotation),
                    )
                }
            }

            ExpandableContent(expanded = expanded) {
                EndpointExpandedInfo(endpoint = endpoint)
            }
        }
    }
}

@Composable
private fun EndpointExpandedInfo(endpoint: OpenAiEndpoint) {
    Column(modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 14.dp)) {
        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        Spacer(Modifier.height(10.dp))
        Text(
            text = "接入地址",
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Text(
            text = endpoint.baseUrl.ifBlank { "—" },
            style = MaterialTheme.typography.bodySmall,
            maxLines = 2,
            overflow = TextOverflow.Ellipsis,
        )
        Spacer(Modifier.height(10.dp))
        Row(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
            KeyStat("API Key", endpoint.maskedApiKey)
            KeyStat("协议", endpoint.protocol.ifBlank { "auto" })
            KeyStat("状态", endpoint.status.orEmpty().ifBlank { "未检查" })
        }
    }
}

@Composable
private fun KeyList(
    keys: List<GatewayKey>,
    loading: Boolean,
    onEdit: (GatewayKey) -> Unit,
    onToggle: (GatewayKey, Boolean) -> Unit,
    onRotate: (GatewayKey) -> Unit,
    onDelete: (GatewayKey) -> Unit,
) {
    if (loading && keys.isEmpty()) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            CircularProgressIndicator()
        }
        return
    }

    if (keys.isEmpty()) {
        EmptyHint("还没有创建密钥")
        return
    }

    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        items(keys, key = { it.id }) { key ->
            KeyCard(
                key = key,
                modifier = motionItem(),
                onEdit = { onEdit(key) },
                onToggle = { enabled -> onToggle(key, enabled) },
                onRotate = { onRotate(key) },
                onDelete = { onDelete(key) },
            )
        }
    }
}

@Composable
private fun KeyCard(
    key: GatewayKey,
    modifier: Modifier = Modifier,
    onEdit: () -> Unit,
    onToggle: (Boolean) -> Unit,
    onRotate: () -> Unit,
    onDelete: () -> Unit,
) {
    var menuOpen by remember { mutableStateOf(false) }
    var expanded by rememberSaveable(key.id) { mutableStateOf(false) }

    var localEnabled by remember(key.id) { mutableStateOf(key.enabled) }
    var pending by remember(key.id) { mutableStateOf(false) }

    LaunchedEffect(key.enabled) {
        if (!pending) localEnabled = key.enabled
    }

    val chevronRotation by animateFloatAsState(
        targetValue = if (expanded) 180f else 0f,
        animationSpec = tween(Motion.EXPAND_MS, easing = Motion.EmphasizedDecelerate),
        label = "key-chevron",
    )

    OutlinedCard(modifier = modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(start = 16.dp, top = 12.dp, end = 8.dp, bottom = 12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(modifier = Modifier.weight(1f)) {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        Text(
                            text = key.name.ifBlank { "未命名" },
                            style = MaterialTheme.typography.titleSmall,
                            fontWeight = FontWeight.SemiBold,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        if (key.isDefault) {
                            Spacer(Modifier.width(6.dp))
                            Text(
                                text = "默认",
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.primary,
                            )
                        }
                    }
                    Spacer(Modifier.height(4.dp))
                    Text(
                        text = key.displayKey,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                }
                Switch(
                    checked = localEnabled,
                    onCheckedChange = {
                        localEnabled = it
                        pending = true
                        onToggle(it)
                    },
                )
                IconButton(onClick = { expanded = !expanded }) {
                    Icon(
                        imageVector = Icons.Filled.ExpandMore,
                        contentDescription = if (expanded) "收起" else "展开",
                        modifier = Modifier.rotate(chevronRotation),
                    )
                }
                Box {
                    IconButton(onClick = { menuOpen = true }) {
                        Icon(Icons.Filled.MoreVert, contentDescription = "更多操作")
                    }
                    DropdownMenu(expanded = menuOpen, onDismissRequest = { menuOpen = false }) {
                        DropdownMenuItem(
                            text = { Text("编辑名称与过期时间") },
                            leadingIcon = { Icon(Icons.Filled.Edit, contentDescription = null) },
                            onClick = {
                                menuOpen = false
                                onEdit()
                            },
                        )
                        DropdownMenuItem(
                            text = { Text("轮换 Key") },
                            leadingIcon = { Icon(Icons.Filled.Sync, contentDescription = null) },
                            onClick = {
                                menuOpen = false
                                onRotate()
                            },
                        )
                        DropdownMenuItem(
                            text = { Text("删除", color = MaterialTheme.colorScheme.error) },
                            leadingIcon = {
                                Icon(
                                    Icons.Filled.Delete,
                                    contentDescription = null,
                                    tint = MaterialTheme.colorScheme.error,
                                )
                            },
                            onClick = {
                                menuOpen = false
                                onDelete()
                            },
                        )
                    }
                }
            }

            ExpandableContent(expanded = expanded) {
                Column {
                    Spacer(Modifier.height(10.dp))
                    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                    Spacer(Modifier.height(10.dp))
                    Row(horizontalArrangement = Arrangement.spacedBy(24.dp)) {
                        KeyStat("请求", formatCompact(key.requestCount.toDouble()))
                        KeyStat("令牌", formatCompact(key.totalTokensUsed.toDouble()))
                        if (key.maxTokensQuota > 0) {
                            KeyStat("配额", formatCompact(key.maxTokensQuota.toDouble()))
                        }
                        KeyStat("过期", key.expiryLabel)
                    }
                    Spacer(Modifier.height(8.dp))
                    Text(
                        text = "创建 ${shortTime(key.createdAt)} · 上次使用 ${shortTime(key.lastUsed)}",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

@Composable
private fun KeyStat(label: String, value: String) {
    Column {
        Text(
            text = value,
            style = MaterialTheme.typography.bodyMedium,
            fontWeight = FontWeight.Medium,
            maxLines = 1,
            overflow = TextOverflow.Ellipsis,
        )
        Text(
            text = label,
            style = MaterialTheme.typography.labelSmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun EmptyHint(text: String) {
    Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
        Text(
            text = text,
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun LogList(
    logs: List<GatewayLog>,
    loading: Boolean,
    loadingMore: Boolean,
    hasMore: Boolean,
    onLoadMore: () -> Unit,
) {
    if (loading && logs.isEmpty()) {
        Box(Modifier.fillMaxSize(), contentAlignment = Alignment.Center) {
            CircularProgressIndicator()
        }
        return
    }

    val listState = rememberLazyListState()
    val shouldLoadMore by remember {
        derivedStateOf {
            val last = listState.layoutInfo.visibleItemsInfo.lastOrNull()?.index ?: 0
            val total = listState.layoutInfo.totalItemsCount
            total > 0 && last >= total - 3
        }
    }

    LaunchedEffect(shouldLoadMore, hasMore, loadingMore) {
        if (shouldLoadMore && hasMore && !loadingMore) onLoadMore()
    }

    LazyColumn(
        state = listState,
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(6.dp),
    ) {
        items(logs, key = { it.id }) { log ->
            Box(modifier = motionItem()) {
                LogCard(log)
            }
        }
        if (loadingMore) {
            item {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(16.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator(modifier = Modifier.size(20.dp), strokeWidth = 2.dp)
                }
            }
        }
        if (!hasMore && logs.isNotEmpty()) {
            item {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(16.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    Text(
                        text = "已加载全部",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
        }
    }
}

@Composable
private fun LogCard(log: GatewayLog) {
    OutlinedCard(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(12.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = clockTime(log.timestamp),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Spacer(Modifier.width(8.dp))
                Text(
                    text = log.displayModel.ifBlank { "未知模型" },
                    style = MaterialTheme.typography.bodyMedium,
                    fontWeight = FontWeight.Medium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                    modifier = Modifier.weight(1f),
                )
                Text(
                    text = log.statusCode.toString(),
                    style = MaterialTheme.typography.labelMedium,
                    color = if (log.isSuccess) {
                        MaterialTheme.colorScheme.primary
                    } else {
                        MaterialTheme.colorScheme.error
                    },
                )
            }
            Spacer(Modifier.height(4.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Text(
                    text = log.endpointName.ifBlank { "—" },
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    text = log.latencyLabel,
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    text = "${formatCompact(log.totalTokens.toDouble())} 令牌",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (!log.isSuccess && log.errorMessage.isNotBlank()) {
                Spacer(Modifier.height(4.dp))
                Text(
                    text = log.errorMessage.take(120),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.error,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

/**
 * 日志时间只显示 HH:mm。后端 `openai_gateway_analytics.timestamp` 目前是
 * `YYYY-MM-DD HH:MM:SS`，但也兼容 RFC3339（`...THH:MM:SSZ`）——直接 takeLast
 * 会在格式切换时切出 "0:36:" 这种残片。
 */
internal fun clockTime(raw: String): String {
    val timePart = if (raw.contains('T')) {
        raw.substringAfter('T')
    } else {
        raw.substringAfter(' ', raw)
    }
    return timePart.take(5).ifBlank { "—" }
}
