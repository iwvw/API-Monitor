package com.iwvw.apimonitor.ui.gateway

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.CreationExtras
import com.iwvw.apimonitor.data.model.EndpointHealth
import com.iwvw.apimonitor.data.model.GatewayKey
import com.iwvw.apimonitor.data.model.GatewayLog
import com.iwvw.apimonitor.data.model.OpenAiEndpoint
import com.iwvw.apimonitor.data.model.sortedForDisplay
import com.iwvw.apimonitor.data.network.ApiClient
import com.iwvw.apimonitor.data.settings.Credentials
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class GatewayUiState(
    val endpoints: List<OpenAiEndpoint> = emptyList(),
    val keys: List<GatewayKey> = emptyList(),
    val logs: List<GatewayLog> = emptyList(),
    val loading: Boolean = false,
    val loadingMore: Boolean = false,
    val hasMore: Boolean = true,
    val page: Int = 1,
    val errorMessage: String? = null,
)

/** 端点详情页状态。 */
data class EndpointDetailUiState(
    val endpointId: String = "",
    val endpoint: OpenAiEndpoint? = null,
    val models: List<String> = emptyList(),
    val health: EndpointHealth? = null,
    val loading: Boolean = true,
    val saving: Boolean = false,
    val errorMessage: String? = null,
) {
    val disabledModelSet: Set<String>
        get() = endpoint?.disabledModels?.toSet().orEmpty()

    val enabledModelCount: Int
        get() = models.count { it !in disabledModelSet }
}

class GatewayViewModel(
    private val credentials: Credentials,
    private val apiClient: ApiClient,
) : ViewModel() {

    private val _state = MutableStateFlow(GatewayUiState(loading = true))
    val state: StateFlow<GatewayUiState> = _state.asStateFlow()

    private val _detail = MutableStateFlow(EndpointDetailUiState())
    val detail: StateFlow<EndpointDetailUiState> = _detail.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        viewModelScope.launch {
            _state.update { it.copy(loading = it.endpoints.isEmpty()) }
            apiClient.listEndpoints(credentials)
                .onSuccess { endpoints ->
                    _state.update {
                        it.copy(
                            endpoints = endpoints.sortedForDisplay(),
                            loading = false,
                            errorMessage = null,
                        )
                    }
                }
                .onFailure { error ->
                    _state.update { it.copy(loading = false, errorMessage = error.message) }
                }
            apiClient.listGatewayKeys(credentials)
                .onSuccess { keys -> _state.update { it.copy(keys = keys) } }
            loadLogsPage(reset = true)
        }
    }

    fun toggleEndpoint(endpoint: OpenAiEndpoint, onResult: (Boolean, String) -> Unit) {
        viewModelScope.launch {
            apiClient.toggleEndpoint(credentials, endpoint.id)
                .onSuccess {
                    val next = !endpoint.enabled
                    _state.update { current ->
                        current.copy(
                            endpoints = current.endpoints
                                .map { if (it.id == endpoint.id) it.copy(enabled = next) else it }
                                .sortedForDisplay(),
                        )
                    }
                    _detail.update { current ->
                        if (current.endpoint?.id == endpoint.id) {
                            current.copy(endpoint = current.endpoint.copy(enabled = next))
                        } else {
                            current
                        }
                    }
                    onResult(true, "${endpoint.name} 已${if (next) "启用" else "停用"}")
                }
                .onFailure { error ->
                    onResult(false, "操作失败：${error.message}")
                }
        }
    }

    // ---- 端点详情 ----

    fun openEndpoint(endpoint: OpenAiEndpoint) {
        _detail.value = EndpointDetailUiState(
            endpointId = endpoint.id,
            endpoint = endpoint,
            models = endpoint.models,
            loading = true,
        )
        loadEndpointDetail(endpoint.id)
    }

    fun closeEndpoint() {
        _detail.value = EndpointDetailUiState()
    }

    fun reloadEndpointDetail() {
        val id = _detail.value.endpointId
        if (id.isNotBlank()) loadEndpointDetail(id)
    }

    private fun loadEndpointDetail(id: String) {
        viewModelScope.launch {
            _detail.update { it.copy(loading = true, errorMessage = null) }

            apiClient.listEndpoints(credentials).onSuccess { endpoints ->
                endpoints.firstOrNull { it.id == id }?.let { fresh ->
                    _detail.update { it.copy(endpoint = fresh, models = fresh.models) }
                    _state.update { it.copy(endpoints = endpoints.sortedForDisplay()) }
                }
            }

            apiClient.endpointModels(credentials, id)
                .onSuccess { models ->
                    _detail.update {
                        it.copy(models = models.ifEmpty { it.endpoint?.models.orEmpty() })
                    }
                }

            apiClient.endpointHealth(credentials, id)
                .onSuccess { health -> _detail.update { it.copy(health = health) } }

            _detail.update { it.copy(loading = false) }
        }
    }

    fun saveEndpoint(
        name: String,
        baseUrl: String,
        apiKey: String,
        protocol: String,
        autoSwitch: Boolean,
        skipVerify: Boolean,
        onResult: (Boolean, String) -> Unit,
    ) {
        val id = _detail.value.endpointId
        if (id.isBlank()) return
        viewModelScope.launch {
            _detail.update { it.copy(saving = true) }
            apiClient.updateEndpoint(
                credentials = credentials,
                id = id,
                name = name,
                baseUrl = baseUrl,
                apiKey = apiKey,
                protocol = protocol,
                autoSwitch = autoSwitch,
                skipVerify = skipVerify,
            )
                .onSuccess {
                    _detail.update { current ->
                        current.copy(
                            endpoint = current.endpoint?.copy(
                                name = name,
                                baseUrl = baseUrl,
                                apiKey = apiKey,
                                protocol = protocol,
                                autoSwitch = autoSwitch,
                                skipVerify = skipVerify,
                            ),
                        )
                    }
                    _state.update { current ->
                        current.copy(
                            endpoints = current.endpoints
                                .map {
                                    if (it.id == id) {
                                        it.copy(
                                            name = name,
                                            baseUrl = baseUrl,
                                            apiKey = apiKey,
                                            protocol = protocol,
                                            autoSwitch = autoSwitch,
                                            skipVerify = skipVerify,
                                        )
                                    } else {
                                        it
                                    }
                                }
                                .sortedForDisplay(),
                        )
                    }
                    onResult(true, "已保存")
                }
                .onFailure { error -> onResult(false, "保存失败：${error.message}") }
            _detail.update { it.copy(saving = false) }
        }
    }

    fun deleteEndpoint(onResult: (Boolean, String) -> Unit) {
        val endpoint = _detail.value.endpoint ?: return
        viewModelScope.launch {
            apiClient.deleteEndpoint(credentials, endpoint.id)
                .onSuccess {
                    _state.update { current ->
                        current.copy(endpoints = current.endpoints.filterNot { it.id == endpoint.id })
                    }
                    onResult(true, "${endpoint.name} 已删除")
                }
                .onFailure { error -> onResult(false, "删除失败：${error.message}") }
        }
    }

    fun toggleModel(model: String, enabled: Boolean, onResult: (Boolean, String) -> Unit) {
        val id = _detail.value.endpointId
        if (id.isBlank()) return
        viewModelScope.launch {
            apiClient.toggleEndpointModel(credentials, id, model, enabled)
                .onSuccess {
                    _detail.update { current ->
                        val endpoint = current.endpoint ?: return@update current
                        val disabled = if (enabled) {
                            endpoint.disabledModels - model
                        } else {
                            endpoint.disabledModels + model
                        }
                        val updated = endpoint.copy(disabledModels = disabled)
                        current.copy(endpoint = updated)
                    }
                    _detail.value.endpoint?.let { updated ->
                        _state.update { current ->
                            current.copy(
                                endpoints = current.endpoints
                                    .map { if (it.id == updated.id) updated else it }
                                    .sortedForDisplay(),
                            )
                        }
                    }
                    onResult(true, if (enabled) "$model 已启用" else "$model 已停用")
                }
                .onFailure { error -> onResult(false, "操作失败：${error.message}") }
        }
    }

    // ---- 密钥 ----

    fun updateKey(
        key: GatewayKey,
        name: String,
        expiresAt: String?,
        onResult: (Boolean, String) -> Unit,
    ) {
        viewModelScope.launch {
            apiClient.updateGatewayKey(credentials, key.id, name, expiresAt)
                .onSuccess {
                    _state.update { current ->
                        current.copy(
                            keys = current.keys.map {
                                if (it.id == key.id) it.copy(name = name, expiresAt = expiresAt) else it
                            },
                        )
                    }
                    onResult(true, "密钥已更新")
                }
                .onFailure { error -> onResult(false, "更新失败：${error.message}") }
        }
    }

    fun toggleKey(key: GatewayKey, enabled: Boolean, onResult: (Boolean, String) -> Unit) {
        viewModelScope.launch {
            apiClient.toggleGatewayKey(credentials, key.id, enabled)
                .onSuccess {
                    _state.update { current ->
                        current.copy(
                            keys = current.keys.map {
                                if (it.id == key.id) it.copy(enabled = enabled) else it
                            },
                        )
                    }
                    onResult(true, "${key.name} 已${if (enabled) "启用" else "停用"}")
                }
                .onFailure { error -> onResult(false, "操作失败：${error.message}") }
        }
    }

    fun deleteKey(key: GatewayKey, onResult: (Boolean, String) -> Unit) {
        viewModelScope.launch {
            apiClient.deleteGatewayKey(credentials, key.id)
                .onSuccess {
                    _state.update { current ->
                        current.copy(keys = current.keys.filterNot { it.id == key.id })
                    }
                    onResult(true, "${key.name} 已删除")
                }
                .onFailure { error -> onResult(false, "删除失败：${error.message}") }
        }
    }

    fun rotateKey(key: GatewayKey, onResult: (Boolean, String, String) -> Unit) {
        viewModelScope.launch {
            apiClient.rotateGatewayKey(credentials, key.id)
                .onSuccess { newKey ->
                    refresh()
                    onResult(true, "密钥已轮换，旧 Key 立即失效", newKey)
                }
                .onFailure { error -> onResult(false, "轮换失败：${error.message}", "") }
        }
    }

    // ---- 日志 ----

    fun loadMoreLogs() {
        if (_state.value.loadingMore || !_state.value.hasMore) return
        viewModelScope.launch { loadLogsPage(reset = false) }
    }

    private suspend fun loadLogsPage(reset: Boolean) {
        val current = _state.value
        val nextPage = if (reset) 1 else current.page + 1
        _state.update { it.copy(loadingMore = !reset) }

        apiClient.gatewayLogs(credentials, page = nextPage, pageSize = PAGE_SIZE)
            .onSuccess { page ->
                _state.update { state ->
                    val merged = if (reset) page.records else state.logs + page.records
                    state.copy(
                        logs = merged.distinctBy { it.id },
                        page = nextPage,
                        loadingMore = false,
                        hasMore = page.records.size >= PAGE_SIZE,
                    )
                }
            }
            .onFailure { error ->
                // 分页失败不能把 hasMore 置 false：那会让「加载更多」永久失效，
                // 只能整体刷新才能恢复。保留 hasMore，仅解除 loadingMore。
                _state.update {
                    it.copy(loadingMore = false, errorMessage = error.message)
                }
            }
    }

    companion object {
        private const val PAGE_SIZE = 30

        fun factory(
            credentials: Credentials,
            apiClient: ApiClient,
        ): ViewModelProvider.Factory = object : ViewModelProvider.Factory {
            @Suppress("UNCHECKED_CAST")
            override fun <T : ViewModel> create(modelClass: Class<T>, extras: CreationExtras): T =
                GatewayViewModel(credentials, apiClient) as T
        }
    }
}
