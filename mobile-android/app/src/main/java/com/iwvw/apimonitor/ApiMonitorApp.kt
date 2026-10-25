package com.iwvw.apimonitor

import android.app.Application
import com.iwvw.apimonitor.data.network.ApiClient
import com.iwvw.apimonitor.data.network.MetricsSocket
import com.iwvw.apimonitor.data.settings.SettingsRepository
import com.iwvw.apimonitor.data.settings.SettingsStore

class ApiMonitorApp : Application() {
    lateinit var settingsRepository: SettingsRepository
        private set

    /** 全程复用：登录会话 cookie 存在这里，重建会丢。 */
    lateinit var apiClient: ApiClient
        private set

    /** 实时指标推送通道，全局唯一，避免多页面重复连接被服务端断开。 */
    lateinit var metricsSocket: MetricsSocket
        private set

    override fun onCreate() {
        super.onCreate()
        settingsRepository = SettingsRepository(SettingsStore(this))
        apiClient = ApiClient()
        metricsSocket = MetricsSocket.create()

        // 冷启动时用持久化的 sid 恢复登录态，否则列表接口会 401。
        val saved = settingsRepository.credentials.value
        if (saved.usesSessionLogin) {
            settingsRepository.sessionCookie()
                .takeIf { it.isNotBlank() }
                ?.let { apiClient.restoreSession(saved.normalizedBaseUrl, it) }
        }
    }
}
