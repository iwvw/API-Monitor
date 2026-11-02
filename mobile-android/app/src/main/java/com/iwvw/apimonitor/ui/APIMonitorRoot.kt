package com.iwvw.apimonitor.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.navigation.compose.NavHost
import androidx.navigation.compose.composable
import androidx.navigation.compose.rememberNavController
import com.iwvw.apimonitor.ApiMonitorApp
import com.iwvw.apimonitor.ui.common.predictiveBackEnter
import com.iwvw.apimonitor.ui.common.predictiveBackExit
import com.iwvw.apimonitor.ui.common.pushEnter
import com.iwvw.apimonitor.ui.common.pushExit
import com.iwvw.apimonitor.ui.connect.ConnectScreen
import com.iwvw.apimonitor.ui.settings.SettingsScreen
import com.iwvw.apimonitor.ui.theme.APIMonitorTheme
import kotlinx.coroutines.launch

/**
 * 根导航：只承载「登录态」这一层。
 *
 * - 未登录 → 登录页
 * - 已登录 → 主界面（内部自带一套 NavHost 管理四个 tab 与所有下钻页）
 *
 * 登录/主界面之间用推入与预测性返回，和系统手势保持一致。
 */
@Composable
fun APIMonitorRoot() {
    val context = LocalContext.current
    val app = context.applicationContext as ApiMonitorApp
    val repository = app.settingsRepository
    val credentials by repository.credentials.collectAsState()
    val scope = rememberCoroutineScope()

    val navController = rememberNavController()
    // 只在首次组合时决定起点。若随凭据变化重算，凭据一被清空就会重建导航图、
    // 重置返回栈，和显式 navigate 互相打架。登录态切换一律走 navigate。
    val startDestination = remember {
        if (repository.credentials.value.isConfigured) Route.MAIN else Route.CONNECT
    }

    var darkOverride by remember { mutableStateOf<Boolean?>(null) }
    val darkTheme = darkOverride ?: isSystemInDarkTheme()

    val logout = {
        scope.launch { app.apiClient.logout(credentials) }
        // 先导航再清凭据：反过来的话，退出动画期间 MainScaffold 仍会用空凭据
        // 重组并重建 ViewModel（key 变成 "servers-"），拿到空 baseUrl 崩溃。
        navController.navigate(Route.CONNECT) {
            popUpTo(0) { inclusive = true }
        }
        // 指标通道是全局单例，不主动断开的话重登录会复用旧会话的失效连接。
        app.metricsSocket.disconnect()
        repository.clearSession()
        repository.clear()
    }

    APIMonitorTheme(darkTheme = darkTheme) {
        NavHost(
            navController = navController,
            startDestination = startDestination,
            enterTransition = { pushEnter() },
            exitTransition = { pushExit() },
            popEnterTransition = { predictiveBackEnter() },
            popExitTransition = { predictiveBackExit() },
        ) {
            composable(Route.CONNECT) {
                ConnectScreen(
                    initialUrl = credentials.baseUrl,
                    initialPassword = credentials.password,
                    initialToken = credentials.apiToken,
                    onConnected = { saved, client ->
                        repository.save(saved)
                        client.currentSid()?.takeIf { it.isNotBlank() }?.let(repository::saveSession)
                        navController.navigate(Route.MAIN) {
                            popUpTo(Route.CONNECT) { inclusive = true }
                        }
                    },
                    makeClient = {
                        app.apiClient.also { client ->
                            client.restoreSession(credentials.normalizedBaseUrl, repository.sessionCookie())
                        }
                    },
                )
            }

            composable(Route.MAIN) {
                MainScaffold(
                    credentials = credentials,
                    apiClient = app.apiClient,
                    metricsSocket = app.metricsSocket,
                    onLogout = logout,
                    darkTheme = darkTheme,
                    onToggleDarkTheme = { darkOverride = it },
                )
            }
        }
    }
}

/** 根层级路由。 */
private object Route {
    const val CONNECT = "connect"
    const val MAIN = "main"
}
