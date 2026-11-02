package com.iwvw.apimonitor.data.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class AgentInstanceList(
    val success: Boolean = false,
    val data: List<AgentInstance> = emptyList(),
)

@Serializable
data class AgentInstance(
    val id: String = "",
    val label: String = "",
    @SerialName("hostName") val hostName: String = "",
    @SerialName("serverId") val serverId: String = "",
    val provider: String = "",
    @SerialName("providerLabel") val providerLabel: String = "",
    val port: Int = 0,
    val enabled: Boolean = true,
    @SerialName("desiredState") val desiredState: String = "running",
    @SerialName("hostOnline") val hostOnline: Boolean = false,
    val status: AgentInstanceStatus? = null,
    val lifecycle: AgentLifecycle? = null,
    @SerialName("gatewayUrl") val gatewayUrl: String = "",
    @SerialName("createdAt") val createdAt: String = "",
    @SerialName("updatedAt") val updatedAt: String = "",
) {
    val running: Boolean
        get() = status?.running ?: lifecycle?.running ?: false

    val crashed: Boolean
        get() = lifecycle?.crashed ?: false

    val supported: Boolean
        get() = lifecycle?.supported ?: true

    val displayName: String
        get() = label.ifBlank { hostName.ifBlank { id.take(8) } }

    val stateLabel: String
        get() = when {
            !hostOnline -> "主机离线"
            crashed -> "已崩溃"
            running -> "运行中"
            else -> "已停止"
        }
}

@Serializable
data class AgentInstanceStatus(
    val running: Boolean = false,
    val online: Boolean = false,
    @SerialName("hostOnline") val hostOnline: Boolean = false,
    @SerialName("portListening") val portListening: Boolean = false,
    @SerialName("processRunning") val processRunning: Boolean = false,
)

@Serializable
data class AgentLifecycle(
    val running: Boolean = false,
    val crashed: Boolean = false,
    val managed: Boolean = false,
    val supported: Boolean = true,
    @SerialName("desiredRunning") val desiredRunning: Boolean = false,
    @SerialName("portListening") val portListening: Boolean = false,
)

@Serializable
data class AgentMetricsResponse(
    val success: Boolean = false,
    val data: AgentMetrics = AgentMetrics(),
)

@Serializable
data class AgentMetrics(
    @SerialName("convergenceFailures") val convergenceFailures: Long = 0,
    @SerialName("convergenceRounds") val convergenceRounds: Long = 0,
    @SerialName("convergenceStarts") val convergenceStarts: Long = 0,
    @SerialName("convergenceStops") val convergenceStops: Long = 0,
    @SerialName("lifecycleFailures") val lifecycleFailures: Long = 0,
    @SerialName("lifecycleStarts") val lifecycleStarts: Long = 0,
    @SerialName("lifecycleStops") val lifecycleStops: Long = 0,
    @SerialName("uptimeSeconds") val uptimeSeconds: Long = 0,
    @SerialName("lastConvergenceAt") val lastConvergenceAt: Long = 0,
) {
    val uptimeLabel: String
        get() {
            val total = uptimeSeconds
            val days = total / 86400
            val hours = (total % 86400) / 3600
            val minutes = (total % 3600) / 60
            return when {
                days > 0 -> "${days}d ${hours}h"
                hours > 0 -> "${hours}h ${minutes}m"
                else -> "${minutes}m"
            }
        }
}

/** 生命周期动作。 */
enum class AgentAction(val apiValue: String, val label: String) {
    START("start", "启动"),
    STOP("stop", "停止"),
    RESTART("restart", "重启"),
}
