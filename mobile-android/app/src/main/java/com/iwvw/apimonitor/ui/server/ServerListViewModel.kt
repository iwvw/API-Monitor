package com.iwvw.apimonitor.ui.server

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.CreationExtras
import com.iwvw.apimonitor.data.model.MetricsUpdate
import com.iwvw.apimonitor.data.model.ServerSummary
import com.iwvw.apimonitor.data.model.applyLiveMetrics
import com.iwvw.apimonitor.data.network.ApiClient
import com.iwvw.apimonitor.data.network.MetricsSocket
import com.iwvw.apimonitor.data.settings.Credentials
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json

data class ServerListUiState(
    val servers: List<ServerSummary> = emptyList(),
    val loading: Boolean = false,
    val refreshing: Boolean = false,
    val errorMessage: String? = null,
    val realtimeConnected: Boolean = false,
    val lastUpdateAt: Long = 0L,
)

/**
 * 服务器列表数据的唯一持有者。列表页与详情页共用同一个实例，
 * 保证实时推送只解析一次、两处同时刷新。
 */
class ServerListViewModel(
    private val credentials: Credentials,
    private val apiClient: ApiClient,
    private val metricsSocket: MetricsSocket = MetricsSocket.create(),
    private val json: Json = ApiClient.DefaultJson,
) : ViewModel() {

    private val _state = MutableStateFlow(ServerListUiState(loading = true))
    val state: StateFlow<ServerListUiState> = _state.asStateFlow()

    private var pollJob: Job? = null

    init {
        load(isRefresh = false)
        startRealtime()
        startPolling()
    }

    fun load(isRefresh: Boolean = true) {
        viewModelScope.launch {
            _state.update {
                it.copy(
                    loading = it.servers.isEmpty(),
                    refreshing = it.servers.isNotEmpty() && isRefresh,
                    errorMessage = null,
                )
            }
            apiClient.listServers(credentials)
                .onSuccess { servers ->
                    _state.update { current ->
                        current.copy(
                            servers = mergeWithLive(servers, current.servers, current.lastUpdateAt),
                            loading = false,
                            refreshing = false,
                            errorMessage = null,
                        )
                    }
                }
                .onFailure { error ->
                    _state.update {
                        it.copy(
                            loading = false,
                            refreshing = false,
                            errorMessage = error.message ?: "未知错误",
                        )
                    }
                }
        }
    }

    fun server(id: String): ServerSummary? = _state.value.servers.firstOrNull { it.id == id }

    private fun startRealtime() {
        metricsSocket.connect(viewModelScope, credentials.normalizedBaseUrl)

        viewModelScope.launch {
            metricsSocket.connection.collect { connection ->
                _state.update {
                    it.copy(realtimeConnected = connection == MetricsSocket.ConnectionState.Connected)
                }
            }
        }

        viewModelScope.launch {
            metricsSocket.events.collect { event ->
                if (event.name != "metrics:update" && event.name != "metrics:batch") return@collect
                val updates = parseUpdates(event.payload) ?: return@collect
                if (updates.isEmpty()) return@collect
                applyUpdates(updates)
            }
        }
    }

    /** 轮询兜底：推送断开或漏包时仍然能收敛到最新状态。 */
    private fun startPolling() {
        pollJob?.cancel()
        pollJob = viewModelScope.launch {
            while (isActive) {
                delay(POLL_INTERVAL_MS)
                if (!_state.value.realtimeConnected) {
                    apiClient.listServers(credentials).onSuccess { servers ->
                        _state.update { current ->
                            current.copy(
                                servers = mergeWithLive(servers, current.servers, current.lastUpdateAt),
                                loading = false,
                                errorMessage = null,
                            )
                        }
                    }
                }
            }
        }
    }

    private fun applyUpdates(updates: List<MetricsUpdate>) {
        val latest = updates
            .filter { it.serverId.isNotBlank() }
            .groupBy { it.serverId }
            .mapValues { (_, list) -> list.maxByOrNull { it.timestamp } ?: list.first() }

        if (latest.isEmpty()) return

        _state.update { current ->
            var touched = false
            val nextServers = current.servers.map { server ->
                val update = latest[server.id] ?: return@map server
                touched = true
                server.applyLiveMetrics(update)
            }
            if (!touched) {
                current
            } else {
                current.copy(
                    servers = nextServers,
                    lastUpdateAt = System.currentTimeMillis(),
                    realtimeConnected = true,
                )
            }
        }
    }

    private fun parseUpdates(payload: kotlinx.serialization.json.JsonObject?): List<MetricsUpdate>? {
        if (payload == null) return null
        return runCatching {
            if (payload.containsKey("serverId")) {
                listOf(json.decodeFromJsonElement(MetricsUpdate.serializer(), payload))
            } else {
                emptyList()
            }
        }.getOrNull()
    }

    /**
     * 把接口快照与已收到的实时值合并：接口返回的 info 可能比推送滞后，
     * 若该服务器刚收到推送（10 秒内），保留更“新”的实时指标。
     */
    private fun mergeWithLive(
        incoming: List<ServerSummary>,
        previous: List<ServerSummary>,
        lastUpdateAt: Long,
    ): List<ServerSummary> {
        val fresh = System.currentTimeMillis() - lastUpdateAt < LIVE_GRACE_MS
        if (!fresh || previous.isEmpty()) return incoming
        val previousById = previous.associateBy { it.id }
        return incoming.map { server ->
            val live = previousById[server.id] ?: return@map server
            val liveInfo = live.info ?: return@map server
            server.copy(
                info = liveInfo,
                agentOnline = live.agentOnline,
                isOnline = live.isOnline,
            )
        }
    }

    override fun onCleared() {
        pollJob?.cancel()
        // socket 由 Application 持有，页面退出不断开，避免来回导航反复重连。
        super.onCleared()
    }

    companion object {
        private const val POLL_INTERVAL_MS = 5_000L
        private const val LIVE_GRACE_MS = 10_000L

        fun factory(
            credentials: Credentials,
            apiClient: ApiClient,
            metricsSocket: MetricsSocket,
        ): ViewModelProvider.Factory = object : ViewModelProvider.Factory {
            @Suppress("UNCHECKED_CAST")
            override fun <T : ViewModel> create(modelClass: Class<T>, extras: CreationExtras): T =
                ServerListViewModel(credentials, apiClient, metricsSocket) as T
        }
    }
}
