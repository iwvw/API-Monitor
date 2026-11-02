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
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ArrowBack
import androidx.compose.material.icons.filled.ExpandMore
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Switch
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
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.rotate
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import com.iwvw.apimonitor.data.model.GatewayPlugin
import com.iwvw.apimonitor.data.model.GatewayPluginAccount
import com.iwvw.apimonitor.data.model.GatewayPluginModel
import com.iwvw.apimonitor.data.model.GatewayPluginStatus
import com.iwvw.apimonitor.data.network.ApiClient
import com.iwvw.apimonitor.data.settings.Credentials
import com.iwvw.apimonitor.ui.common.ExpandableContent
import com.iwvw.apimonitor.ui.common.Motion
import com.iwvw.apimonitor.ui.common.StatusDot
import com.iwvw.apimonitor.ui.common.SurfaceClickable
import com.iwvw.apimonitor.ui.common.motionItem
import com.iwvw.apimonitor.ui.common.rememberHaptics
import com.iwvw.apimonitor.ui.common.fullScreenWindowInsets
import kotlinx.coroutines.launch

/**
 * 网关插件页：左侧（列表）插件注册表，点进去看该插件的状态、账号与模型。
 *
 * 与面板 `OpenAIPluginsPanel` 的注册表保持一致，顺序也相同。
 */
@Composable
fun GatewayPluginsScreen(onOpen: (GatewayPlugin) -> Unit) {
    PluginRegistryList(onOpen = onOpen)
}

@Composable
private fun PluginRegistryList(onOpen: (GatewayPlugin) -> Unit) {
    LazyColumn(
        modifier = Modifier.fillMaxSize(),
        contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
        verticalArrangement = Arrangement.spacedBy(8.dp),
    ) {
        item("plugin-hint") {
            Text(
                text = "插件把第三方账号转成 OpenAI 兼容 API，可在网关端点里直接选用。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.padding(bottom = 4.dp),
            )
        }
        items(GatewayPlugin.entries, key = { it.id }) { plugin ->
            PluginRegistryCard(
                plugin = plugin,
                modifier = motionItem(),
                onOpen = { onOpen(plugin) },
            )
        }
    }
}

@Composable
private fun PluginRegistryCard(
    plugin: GatewayPlugin,
    modifier: Modifier = Modifier,
    onOpen: () -> Unit,
) {
    OutlinedCard(
        onClick = onOpen,
        modifier = modifier.fillMaxWidth(),
    ) {
        Row(
            modifier = Modifier.padding(16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Box(
                modifier = Modifier
                    .size(36.dp)
                    .background(
                        color = MaterialTheme.colorScheme.secondaryContainer,
                        shape = MaterialTheme.shapes.medium,
                    ),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = plugin.displayName.take(1),
                    style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.SemiBold,
                    color = MaterialTheme.colorScheme.onSecondaryContainer,
                )
            }
            Spacer(Modifier.width(12.dp))
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = plugin.displayName,
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                Spacer(Modifier.height(2.dp))
                Text(
                    text = plugin.description,
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
internal fun PluginDetailScreen(
    plugin: GatewayPlugin,
    credentials: Credentials,
    apiClient: ApiClient,
    onBack: () -> Unit,
) {
    val snackbar = remember { SnackbarHostState() }
    val haptics = rememberHaptics()
    val scope = rememberCoroutineScope()
    val scrollBehavior = TopAppBarDefaults.pinnedScrollBehavior()

    var status by remember(plugin.id) { mutableStateOf<GatewayPluginStatus?>(null) }
    var accounts by remember(plugin.id) { mutableStateOf<List<GatewayPluginAccount>>(emptyList()) }
    var models by remember(plugin.id) { mutableStateOf<List<GatewayPluginModel>>(emptyList()) }
    var loading by remember(plugin.id) { mutableStateOf(true) }
    var error by remember(plugin.id) { mutableStateOf<String?>(null) }
    var reloadToken by remember(plugin.id) { mutableStateOf(0) }

    LaunchedEffect(plugin.id, reloadToken) {
        loading = true
        error = null

        apiClient.pluginStatus(credentials, plugin.id)
            .onSuccess { status = it }
            .onFailure { error = it.message }

        if (plugin.supportsAccounts) {
            apiClient.pluginAccounts(credentials, plugin.id)
                .onSuccess { accounts = it }
        }
        if (plugin.supportsModels) {
            apiClient.pluginModels(credentials, plugin.id)
                .onSuccess { models = it }
        }
        loading = false
    }

    fun notify(message: String) {
        scope.launch { snackbar.showSnackbar(message) }
    }

    Scaffold(
        contentWindowInsets = fullScreenWindowInsets(),
        snackbarHost = { SnackbarHost(snackbar) },
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text(plugin.displayName, maxLines = 1, overflow = TextOverflow.Ellipsis)
                        Text(
                            text = status?.accountLabel ?: plugin.description,
                            style = MaterialTheme.typography.labelMedium,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
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
                        reloadToken++
                    }) {
                        Icon(Icons.Filled.Refresh, contentDescription = "刷新")
                    }
                },
                scrollBehavior = scrollBehavior,
            )
        },
    ) { innerPadding ->
        if (loading && status == null && accounts.isEmpty() && models.isEmpty()) {
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
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            error?.let { message ->
                item("plugin-error") {
                    Text(
                        text = "加载失败：$message",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.error,
                    )
                }
            }

            status?.let { s ->
                item("plugin-status") {
                    PluginStatusCard(plugin = plugin, status = s)
                }
            }

            if (plugin.supportsAccounts && accounts.isNotEmpty()) {
                item("accounts-header") {
                    SectionHeader("账号", "${accounts.size} 个")
                }
                items(accounts, key = { "acc-${it.id.ifBlank { it.displayName }}" }) { account ->
                    PluginAccountCard(
                        account = account,
                        modifier = motionItem(),
                        onToggle = { enabled ->
                            haptics.toggle(enabled)
                            scope.launch {
                                apiClient.togglePluginAccount(
                                    credentials = credentials,
                                    pluginId = plugin.id,
                                    accountId = account.id,
                                    disabled = !enabled,
                                )
                                    .onSuccess {
                                        accounts = accounts.map {
                                            if (it.id == account.id) it.copy(disabled = !enabled) else it
                                        }
                                        haptics.confirm()
                                        notify("${account.displayName} 已${if (enabled) "启用" else "停用"}")
                                    }
                                    .onFailure {
                                        haptics.reject()
                                        notify("操作失败：${it.message}")
                                    }
                            }
                        },
                    )
                }
            }

            if (plugin.supportsModels && models.isNotEmpty()) {
                item("models-header") {
                    SectionHeader("模型", "${models.count { it.enabled }}/${models.size} 启用")
                }
                items(models, key = { "pm-${it.id}" }) { model ->
                    PluginModelRow(
                        model = model,
                        modifier = motionItem(),
                        onToggle = { enabled ->
                            haptics.toggle(enabled)
                            scope.launch {
                                apiClient.togglePluginModel(credentials, plugin.id, model.id, enabled)
                                    .onSuccess {
                                        models = models.map {
                                            if (it.id == model.id) it.copy(enabled = enabled) else it
                                        }
                                        haptics.confirm()
                                        notify("${model.label} 已${if (enabled) "启用" else "停用"}")
                                    }
                                    .onFailure {
                                        haptics.reject()
                                        notify("操作失败：${it.message}")
                                    }
                            }
                        },
                    )
                }
            }

            if (!plugin.supportsAccounts && !plugin.supportsModels) {
                item("plugin-plain") {
                    Text(
                        text = "该插件没有可在此管理的账号或模型，请到面板处理。",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }

            item("plugin-bottom") { Spacer(Modifier.height(8.dp)) }
        }
    }
}

@Composable
private fun SectionHeader(title: String, trailing: String) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Text(
            text = title,
            style = MaterialTheme.typography.titleMedium,
            fontWeight = FontWeight.SemiBold,
            modifier = Modifier.weight(1f),
        )
        Text(
            text = trailing,
            style = MaterialTheme.typography.labelMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
    }
}

@Composable
private fun PluginStatusCard(plugin: GatewayPlugin, status: GatewayPluginStatus) {
    OutlinedCard(modifier = Modifier.fillMaxWidth()) {
        Column(modifier = Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                StatusDot(active = status.enabled, modifier = Modifier.size(10.dp))
                Spacer(Modifier.width(10.dp))
                Text(
                    text = if (status.enabled) "插件已启用" else "插件已停用",
                    style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.SemiBold,
                    modifier = Modifier.weight(1f),
                )
                if (status.upstreamReady || status.sessionReady) {
                    Text(
                        text = "上游就绪",
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.primary,
                    )
                }
            }

            Spacer(Modifier.height(12.dp))
            HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
            Spacer(Modifier.height(12.dp))

            Row(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
                PluginStat("账号", status.accountLabel)
                if (status.modelCount > 0) {
                    PluginStat("模型", status.modelCount.toString())
                }
                if (status.discoveredCount > 0) {
                    PluginStat("发现", status.discoveredCount.toString())
                }
            }

            if (status.linkBaseUrl.isNotBlank()) {
                Spacer(Modifier.height(12.dp))
                Text(
                    text = "接入地址",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
                Text(
                    text = status.linkBaseUrl,
                    style = MaterialTheme.typography.bodySmall,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
                )
            }

            if (status.region.isNotBlank() || status.strategy.isNotBlank()) {
                Spacer(Modifier.height(8.dp))
                Text(
                    text = listOfNotNull(
                        status.region.takeIf { it.isNotBlank() }?.let { "区域 $it" },
                        status.strategy.takeIf { it.isNotBlank() }?.let { "策略 $it" },
                    ).joinToString(" · "),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

@Composable
private fun PluginStat(label: String, value: String) {
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
private fun PluginAccountCard(
    account: GatewayPluginAccount,
    modifier: Modifier = Modifier,
    onToggle: ((Boolean) -> Unit)? = null,
) {
    var expanded by rememberSaveable(account.displayName) { mutableStateOf(false) }

    var localEnabled by remember(account.displayName) { mutableStateOf(!account.disabled) }
    var pending by remember(account.displayName) { mutableStateOf(false) }

    LaunchedEffect(account.disabled) {
        if (!pending) localEnabled = !account.disabled
    }

    val chevronRotation by animateFloatAsState(
        targetValue = if (expanded) 180f else 0f,
        animationSpec = tween(Motion.EXPAND_MS, easing = Motion.EmphasizedDecelerate),
        label = "account-chevron",
    )

    OutlinedCard(modifier = modifier.fillMaxWidth()) {
        Column {
            SurfaceClickable(
                onClick = { expanded = !expanded },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Row(
                    modifier = Modifier.padding(start = 16.dp, top = 12.dp, end = 8.dp, bottom = 12.dp),
                    verticalAlignment = Alignment.CenterVertically,
                ) {
                    StatusDot(
                        active = account.available && !account.disabled,
                        neutral = account.disabled,
                    )
                    Spacer(Modifier.width(10.dp))
                    Column(modifier = Modifier.weight(1f)) {
                        Text(
                            text = account.displayName,
                            style = MaterialTheme.typography.titleSmall,
                            fontWeight = FontWeight.SemiBold,
                            maxLines = 1,
                            overflow = TextOverflow.Ellipsis,
                        )
                        Spacer(Modifier.height(2.dp))
                        Text(
                            text = buildString {
                                append(account.stateLabel)
                                if (account.region.isNotBlank()) append(" · ${account.region}")
                                if (account.planType.isNotBlank()) append(" · ${account.planType}")
                            },
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                    if (onToggle != null) {
                        Switch(
                            checked = localEnabled,
                            onCheckedChange = {
                                localEnabled = it
                                pending = true
                                onToggle(it)
                            },
                        )
                    }
                    IconButton(onClick = { expanded = !expanded }) {
                        Icon(
                            imageVector = Icons.Filled.ExpandMore,
                            contentDescription = if (expanded) "收起" else "展开",
                            modifier = Modifier.rotate(chevronRotation),
                        )
                    }
                }
            }

            ExpandableContent(expanded = expanded) {
                Column(modifier = Modifier.padding(start = 16.dp, end = 16.dp, bottom = 14.dp)) {
                    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                    Spacer(Modifier.height(10.dp))
                    Row(horizontalArrangement = Arrangement.spacedBy(20.dp)) {
                        PluginStat("调用", account.callCount.toString())
                        PluginStat("额度", if (account.credits > 0) account.credits.toString() else "—")
                        PluginStat("过期", account.expiryLabel)
                        PluginStat("令牌", account.tokenState.ifBlank { "—" })
                    }
                    if (account.uid.isNotBlank()) {
                        Spacer(Modifier.height(8.dp))
                        Text(
                            text = "UID ${account.uid}",
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun PluginModelRow(
    model: GatewayPluginModel,
    modifier: Modifier = Modifier,
    onToggle: (Boolean) -> Unit,
) {
    OutlinedCard(modifier = modifier.fillMaxWidth()) {
        Row(
            modifier = Modifier.padding(start = 16.dp, top = 6.dp, end = 8.dp, bottom = 6.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Column(modifier = Modifier.weight(1f)) {
                Text(
                    text = model.label,
                    style = MaterialTheme.typography.bodyMedium,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
                val meta = buildString {
                    if (model.supportsThinking == true) append("支持思考")
                    model.maxTokens?.takeIf { it > 0 }?.let {
                        if (isNotEmpty()) append(" · ")
                        append("$it 上下文")
                    }
                }
                if (meta.isNotBlank()) {
                    Text(
                        text = meta,
                        style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                    )
                }
            }
            Switch(checked = model.enabled, onCheckedChange = onToggle)
        }
    }
}
