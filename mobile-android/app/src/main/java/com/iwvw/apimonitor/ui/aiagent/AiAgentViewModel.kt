package com.iwvw.apimonitor.ui.aiagent

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.CreationExtras
import com.iwvw.apimonitor.data.model.AgentAction
import com.iwvw.apimonitor.data.model.AgentInstance
import com.iwvw.apimonitor.data.model.AgentMetrics
import com.iwvw.apimonitor.data.network.ApiClient
import com.iwvw.apimonitor.data.settings.Credentials
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class AiAgentUiState(
    val instances: List<AgentInstance> = emptyList(),
    val metrics: AgentMetrics? = null,
    val loading: Boolean = false,
    val busyInstanceId: String? = null,
    val errorMessage: String? = null,
)

class AiAgentViewModel(
    private val credentials: Credentials,
    private val apiClient: ApiClient,
) : ViewModel() {

    private val _state = MutableStateFlow(AiAgentUiState(loading = true))
    val state: StateFlow<AiAgentUiState> = _state.asStateFlow()

    init {
        refresh()
    }

    fun refresh() {
        viewModelScope.launch { reload() }
    }

    private suspend fun reload() {
        _state.update { it.copy(loading = it.instances.isEmpty()) }
        apiClient.listAgentInstances(credentials)
            .onSuccess { instances ->
                _state.update { it.copy(instances = instances, loading = false, errorMessage = null) }
            }
            .onFailure { error ->
                _state.update { it.copy(loading = false, errorMessage = error.message) }
            }
        apiClient.agentMetrics(credentials)
            .onSuccess { metrics -> _state.update { it.copy(metrics = metrics) } }
            .onFailure { error ->
                _state.update { current ->
                    current.copy(errorMessage = current.errorMessage ?: error.message)
                }
            }
    }

    fun runAction(
        instance: AgentInstance,
        action: AgentAction,
        onResult: (Boolean, String) -> Unit,
    ) {
        viewModelScope.launch {
            _state.update { it.copy(busyInstanceId = instance.id) }
            try {
                apiClient.agentLifecycle(credentials, instance.id, action)
                    .onSuccess {
                        onResult(true, "${instance.displayName} 已下发${action.label}指令")
                        // 生命周期是异步收敛的，等一拍再拉最新状态；必须在刷新
                        // 结束后才解除 busy，否则 spinner 会先于刷新消失。
                        delay(1500)
                        reload()
                    }
                    .onFailure { error ->
                        onResult(false, "${action.label}失败：${error.message}")
                    }
            } finally {
                _state.update { it.copy(busyInstanceId = null) }
            }
        }
    }

    companion object {
        fun factory(
            credentials: Credentials,
            apiClient: ApiClient,
        ): ViewModelProvider.Factory = object : ViewModelProvider.Factory {
            @Suppress("UNCHECKED_CAST")
            override fun <T : ViewModel> create(modelClass: Class<T>, extras: CreationExtras): T =
                AiAgentViewModel(credentials, apiClient) as T
        }
    }
}
