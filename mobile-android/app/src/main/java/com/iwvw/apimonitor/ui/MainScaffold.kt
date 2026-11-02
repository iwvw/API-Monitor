package com.iwvw.apimonitor.ui

import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.tween
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.AutoAwesome
import androidx.compose.material.icons.filled.Cloud
import androidx.compose.material.icons.filled.Dashboard
import androidx.compose.material.icons.filled.Dns
import androidx.compose.material.icons.outlined.AutoAwesome
import androidx.compose.material.icons.outlined.Cloud
import androidx.compose.material.icons.outlined.Dashboard
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.NavigationBar
import androidx.compose.material3.NavigationBarItem
import androidx.compose.material3.NavigationBarItemDefaults
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.derivedStateOf
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import androidx.navigation.NavGraph.Companion.findStartDestination
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.currentBackStackEntryAsState
import androidx.navigation.compose.rememberNavController
import com.iwvw.apimonitor.data.model.GatewayPlugin
import com.iwvw.apimonitor.data.network.ApiClient
import com.iwvw.apimonitor.data.settings.Credentials
import com.iwvw.apimonitor.ui.aiagent.AiAgentScreen
import com.iwvw.apimonitor.ui.common.isTabToTab
import com.iwvw.apimonitor.ui.common.predictiveBackEnter
import com.iwvw.apimonitor.ui.common.predictiveBackExit
import com.iwvw.apimonitor.ui.common.pushEnter
import com.iwvw.apimonitor.ui.common.pushExit
import com.iwvw.apimonitor.ui.common.rememberHaptics
import com.iwvw.apimonitor.ui.common.tabEnter
import com.iwvw.apimonitor.ui.common.tabExit
import com.iwvw.apimonitor.ui.gateway.EndpointDetailScreen
import com.iwvw.apimonitor.ui.gateway.GatewayScreen
import com.iwvw.apimonitor.ui.gateway.GatewayViewModel
import com.iwvw.apimonitor.ui.gateway.PluginDetailScreen
import com.iwvw.apimonitor.ui.home.HomeScreen
import com.iwvw.apimonitor.ui.server.ServerDetailScreen
import com.iwvw.apimonitor.ui.server.ServerListScreen
import com.iwvw.apimonitor.ui.server.ServerListViewModel
import com.iwvw.apimonitor.ui.settings.SettingsScreen
import com.iwvw.apimonitor.ui.terminal.TerminalScreen

/** 底栏四个一级入口。 */
enum class BottomTab(
    val route: String,
    val label: String,
    val selectedIcon: ImageVector,
    val unselectedIcon: ImageVector,
) {
    HOME(MainRoute.HOME, "首页", Icons.Filled.Dashboard, Icons.Outlined.Dashboard),
    SERVERS(MainRoute.SERVERS, "主机实例", Icons.Filled.Dns, Icons.Outlined.Dns),
    GATEWAY(MainRoute.GATEWAY, "模型网关", Icons.Filled.Cloud, Icons.Outlined.Cloud),
    AI_AGENT(MainRoute.AI_AGENT, "AI Agent", Icons.Filled.AutoAwesome, Icons.Outlined.AutoAwesome),
}

/**
 * 主界面的层级路由。
 *
 * 只有四个一级 tab 显示底栏；设置、主机详情、端点详情、插件详情都是
 * 「进入下一层」，全屏展示并隐藏底栏——这是 MD3 的层级约定：
 * 底栏只属于顶层目的地，下钻页面靠返回箭头 + 系统返回手势离开。
 */
object MainRoute {
    const val HOME = "home"
    const val SERVERS = "servers"
    const val GATEWAY = "gateway"
    const val AI_AGENT = "aiagent"

    const val SETTINGS = "settings"
    const val SERVER_DETAIL = "server/{serverId}"
    const val TERMINAL = "server/{serverId}/terminal"
    const val ENDPOINT_DETAIL = "gateway/endpoint"
    const val PLUGIN_DETAIL = "gateway/plugin/{pluginId}"

    val tabs = listOf(HOME, SERVERS, GATEWAY, AI_AGENT)

    fun serverDetail(id: String) = "server/$id"
    fun terminal(id: String) = "server/$id/terminal"
    fun pluginDetail(id: String) = "gateway/plugin/$id"

    fun isTab(route: String?): Boolean = route != null && route in tabs
}

@Composable
fun MainScaffold(
    credentials: Credentials,
    apiClient: ApiClient,
    metricsSocket: com.iwvw.apimonitor.data.network.MetricsSocket,
    onLogout: () -> Unit,
    darkTheme: Boolean,
    onToggleDarkTheme: (Boolean) -> Unit,
) {
    val haptics = rememberHaptics()
    val navController = rememberNavController()

    val backStackEntry by navController.currentBackStackEntryAsState()
    val currentRoute = backStackEntry?.destination?.route
    val showBottomBar = MainRoute.isTab(currentRoute)

    // 主机数据在底栏层共享：首页与主机页复用同一份实时数据，只建一条 socket。
    val serverViewModel: ServerListViewModel = viewModel(
        key = "servers-${credentials.normalizedBaseUrl}",
        factory = ServerListViewModel.factory(credentials, apiClient, metricsSocket),
    )
    val serverState by serverViewModel.state.collectAsState()

    // 网关数据在底栏层共享：端点列表与端点详情共用同一份状态。
    val gatewayViewModel: GatewayViewModel = viewModel(
        key = "gateway-${credentials.normalizedBaseUrl}",
        factory = GatewayViewModel.factory(credentials, apiClient),
    )
    val gatewayDetail by gatewayViewModel.detail.collectAsState()

    Scaffold(
        contentWindowInsets = WindowInsets(0, 0, 0, 0),
        bottomBar = {
            if (!showBottomBar) return@Scaffold
            NavigationBar(
                containerColor = MaterialTheme.colorScheme.surfaceContainer,
                tonalElevation = 0.dp,
            ) {
                BottomTab.entries.forEach { tab ->
                    val selected = currentRoute == tab.route
                    val indicatorAlpha by animateFloatAsState(
                        targetValue = if (selected) 1f else 0f,
                        animationSpec = tween(durationMillis = 220),
                        label = "tab-indicator",
                    )
                    NavigationBarItem(
                        selected = selected,
                        onClick = {
                            if (!selected) {
                                haptics.segment()
                                navController.navigate(tab.route) {
                                    popUpTo(navController.graph.findStartDestination().id) {
                                        saveState = true
                                    }
                                    launchSingleTop = true
                                    restoreState = true
                                }
                            }
                        },
                        icon = {
                            Icon(
                                imageVector = if (selected) tab.selectedIcon else tab.unselectedIcon,
                                contentDescription = tab.label,
                            )
                        },
                        label = { Text(tab.label) },
                        colors = NavigationBarItemDefaults.colors(
                            selectedIconColor = MaterialTheme.colorScheme.onSecondaryContainer,
                            selectedTextColor = MaterialTheme.colorScheme.onSurface,
                            indicatorColor = MaterialTheme.colorScheme.secondaryContainer.copy(
                                alpha = indicatorAlpha,
                            ),
                            unselectedIconColor = MaterialTheme.colorScheme.onSurfaceVariant,
                            unselectedTextColor = MaterialTheme.colorScheme.onSurfaceVariant,
                        ),
                        alwaysShowLabel = true,
                    )
                }
            }
        },
    ) { innerPadding ->
        NavHost(
            navController = navController,
            startDestination = MainRoute.HOME,
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding),
            // Tab 之间是同层切换 → 共享轴横移；下钻/返回 → 推入与预测性返回。
            enterTransition = {
                if (isTabToTab(initialState.destination.route, targetState.destination.route, MainRoute.tabs)) {
                    tabEnter(forward = tabForward(initialState.destination.route, targetState.destination.route))
                } else {
                    pushEnter()
                }
            },
            exitTransition = {
                if (isTabToTab(initialState.destination.route, targetState.destination.route, MainRoute.tabs)) {
                    tabExit(forward = tabForward(initialState.destination.route, targetState.destination.route))
                } else {
                    pushExit()
                }
            },
            popEnterTransition = { predictiveBackEnter() },
            popExitTransition = { predictiveBackExit() },
        ) {
            composable(MainRoute.HOME) {
                HomeScreen(
                    credentials = credentials,
                    apiClient = apiClient,
                    serverState = serverState,
                    onOpenSettings = { navController.navigate(MainRoute.SETTINGS) },
                    onLogout = onLogout,
                )
            }

            composable(MainRoute.SERVERS) {
                ServerListScreen(
                    servers = serverState.servers,
                    loading = serverState.loading,
                    errorMessage = serverState.errorMessage,
                    refreshing = serverState.refreshing,
                    realtimeConnected = serverState.realtimeConnected,
                    onRefresh = { serverViewModel.load(isRefresh = true) },
                    onDisconnect = onLogout,
                    onOpenServer = { server -> navController.navigate(MainRoute.serverDetail(server.id)) },
                )
            }

            composable(MainRoute.GATEWAY) {
                GatewayScreen(
                    viewModel = gatewayViewModel,
                    onOpenEndpoint = { endpoint ->
                        haptics.tap()
                        gatewayViewModel.openEndpoint(endpoint)
                        navController.navigate(MainRoute.ENDPOINT_DETAIL)
                    },
                    onOpenPlugin = { plugin: GatewayPlugin ->
                        haptics.tap()
                        navController.navigate(MainRoute.pluginDetail(plugin.id))
                    },
                )
            }

            composable(MainRoute.AI_AGENT) {
                AiAgentScreen(
                    credentials = credentials,
                    apiClient = apiClient,
                )
            }

            composable(MainRoute.SETTINGS) {
                SettingsScreen(
                    credentials = credentials,
                    darkTheme = darkTheme,
                    onToggleDarkTheme = onToggleDarkTheme,
                    onLogout = onLogout,
                    onBack = { navController.popBackStack() },
                )
            }

            composable(MainRoute.SERVER_DETAIL) { entry ->
                val serverId = entry.arguments?.getString("serverId").orEmpty()
                val server by remember(serverId) {
                    derivedStateOf { serverState.servers.firstOrNull { it.id == serverId } }
                }
                ServerDetailScreen(
                    server = server,
                    realtimeConnected = serverState.realtimeConnected,
                    onBack = { navController.popBackStack() },
                    onOpenTerminal = { navController.navigate(MainRoute.terminal(serverId)) },
                )
            }

            composable(MainRoute.TERMINAL) { entry ->
                val serverId = entry.arguments?.getString("serverId").orEmpty()
                val server = serverState.servers.firstOrNull { it.id == serverId }
                TerminalScreen(
                    serverId = serverId,
                    serverName = server?.name.orEmpty(),
                    credentials = credentials,
                    cookieJar = apiClient.cookieJar(),
                    onBack = { navController.popBackStack() },
                )
            }

            composable(MainRoute.ENDPOINT_DETAIL) {
                EndpointDetailScreen(
                    detail = gatewayDetail,
                    onBack = { navController.popBackStack() },
                    onRefresh = { gatewayViewModel.reloadEndpointDetail() },
                    onToggleEnabled = { endpoint ->
                        haptics.toggle(!endpoint.enabled)
                        gatewayViewModel.toggleEndpoint(endpoint) { _, _ -> }
                    },
                    onToggleModel = { model, enabled ->
                        haptics.toggle(enabled)
                        gatewayViewModel.toggleModel(model, enabled) { _, _ -> }
                    },
                    onSave = { name, baseUrl, apiKey, protocol, autoSwitch, skipVerify, onResult ->
                        gatewayViewModel.saveEndpoint(
                            name = name,
                            baseUrl = baseUrl,
                            apiKey = apiKey,
                            protocol = protocol,
                            autoSwitch = autoSwitch,
                            skipVerify = skipVerify,
                            onResult = onResult,
                        )
                    },
                    // 导航与反馈由 EndpointDetailScreen 自己负责（它拿得到 snackbar 与返回
                    // 回调）。这里再 pop 一次会把「模型网关」tab 一起弹掉，震动也会重复。
                    onDelete = { onResult ->
                        gatewayViewModel.deleteEndpoint(onResult)
                    },
                )
            }

            composable(MainRoute.PLUGIN_DETAIL) { entry ->
                val pluginId = entry.arguments?.getString("pluginId").orEmpty()
                val plugin = GatewayPlugin.entries.firstOrNull { it.id == pluginId }
                if (plugin == null) {
                    navController.popBackStack()
                } else {
                    PluginDetailScreen(
                        plugin = plugin,
                        credentials = credentials,
                        apiClient = apiClient,
                        onBack = { navController.popBackStack() },
                    )
                }
            }
        }
    }
}

/** 判断 tab 切换方向：序号变大即前进（向右推入）。 */
private fun tabForward(fromRoute: String?, toRoute: String?): Boolean {
    val from = MainRoute.tabs.indexOf(fromRoute)
    val to = MainRoute.tabs.indexOf(toRoute)
    if (from < 0 || to < 0) return true
    return to >= from
}
