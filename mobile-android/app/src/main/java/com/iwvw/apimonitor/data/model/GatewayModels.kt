package com.iwvw.apimonitor.data.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class OpenAiEndpoint(
    val id: String = "",
    val name: String = "",
    @SerialName("baseUrl") val baseUrl: String = "",
    val enabled: Boolean = false,
    val models: List<String> = emptyList(),
    @SerialName("disabledModels") val disabledModels: List<String> = emptyList(),
    val notes: String = "",
    val protocol: String = "auto",
    @SerialName("lastChecked") val lastChecked: String? = null,
    @SerialName("lastUsed") val lastUsed: String? = null,
    @SerialName("autoSwitch") val autoSwitch: Boolean = false,
    @SerialName("proxyEnabled") val proxyEnabled: Boolean = false,
    @SerialName("forceProxy") val forceProxy: Boolean = false,
    @SerialName("apiKey") val apiKey: String = "",
    @SerialName("pluginId") val pluginId: String = "",
    val priority: Int = 0,
    val weight: Int = 0,
    val status: String? = null,
    @SerialName("upstreamType") val upstreamType: String? = null,
    @SerialName("skipVerify") val skipVerify: Boolean = false,
    @SerialName("modelMappings") val modelMappings: Map<String, String> = emptyMap(),
    @SerialName("createdAt") val createdAt: String = "",
    @SerialName("keyRetryRounds") val keyRetryRounds: Int = 0,
    @SerialName("rateLimitRetryEnabled") val rateLimitRetryEnabled: Boolean = false,
    @SerialName("rateLimitRetryWaitSeconds") val rateLimitRetryWaitSeconds: Int = 0,
) {
    val enabledModelCount: Int
        get() = models.count { it !in disabledModels }

    val hostLabel: String
        get() = baseUrl
            .removePrefix("https://")
            .removePrefix("http://")
            .trimEnd('/')

    val isPlugin: Boolean
        get() = pluginId.isNotBlank()

    val maskedApiKey: String
        get() = when {
            apiKey.isBlank() -> "—"
            apiKey.length <= 12 -> "****"
            else -> apiKey.take(7) + "..." + apiKey.takeLast(4)
        }
}

/**
 * 端点排序，与面板 Web 端 `sortEndpoints` 完全一致：
 * 插件端点（pluginId 非空）始终置顶且内部保持原始顺序；
 * 其余按 priority 降序、同级按 weight 降序。
 *
 * 用稳定排序保证「内部保持原始顺序」的语义。
 */
fun List<OpenAiEndpoint>.sortedForDisplay(): List<OpenAiEndpoint> =
    sortedWith(
        compareByDescending<OpenAiEndpoint> { if (it.isPlugin) 1 else 0 }
            .thenByDescending { it.priority }
            .thenByDescending { it.weight },
    )

@Serializable
data class GatewayKey(
    val id: String = "",
    val name: String = "",
    val enabled: Boolean = false,
    @SerialName("isDefault") val isDefault: Boolean = false,
    @SerialName("maskedKey") val maskedKey: String = "",
    @SerialName("keyPrefix") val keyPrefix: String = "",
    @SerialName("keySuffix") val keySuffix: String = "",
    @SerialName("apiKey") val apiKey: String = "",
    @SerialName("requestCount") val requestCount: Long = 0,
    @SerialName("totalTokensUsed") val totalTokensUsed: Long = 0,
    @SerialName("lastUsed") val lastUsed: String? = null,
    @SerialName("maxTokensQuota") val maxTokensQuota: Long = 0,
    @SerialName("expiresAt") val expiresAt: String? = null,
    @SerialName("allowedEndpoints") val allowedEndpoints: List<String> = emptyList(),
    @SerialName("createdAt") val createdAt: String = "",
) {
    val expiryLabel: String
        get() = expiresAt?.takeIf { it.isNotBlank() } ?: "永不过期"

    val displayKey: String
        get() = apiKey.ifBlank { maskedKey }
}

/** 端点健康状态（`GET /api/openai/endpoints/{id}/health`）。 */
@Serializable
data class EndpointHealth(
    @SerialName("endpointId") val endpointId: String = "",
    @SerialName("healthStatus") val healthStatus: String = "",
    val history: List<EndpointHealthPoint> = emptyList(),
    @SerialName("lastHealthCheck") val lastHealthCheck: String? = null,
) {
    val healthy: Boolean
        get() = healthStatus == "valid" || healthStatus == "operational"

    val statusLabel: String
        get() = when (healthStatus) {
            "valid", "operational" -> "正常"
            "invalid" -> "密钥无效"
            "failed" -> "检查失败"
            "" -> "未检查"
            else -> healthStatus
        }

    val averageLatencyMs: Long?
        get() {
            val ok = history.filter { it.responseTime > 0 }
            if (ok.isEmpty()) return null
            return ok.sumOf { it.responseTime } / ok.size
        }

    val successRate: Double?
        get() {
            if (history.isEmpty()) return null
            return history.count { it.status == "operational" }.toDouble() / history.size
        }
}

@Serializable
data class EndpointHealthPoint(
    @SerialName("checkedAt") val checkedAt: String = "",
    @SerialName("responseTime") val responseTime: Long = 0,
    val status: String = "",
    @SerialName("errorMessage") val errorMessage: String? = null,
)

/** 端点可用模型（`GET /api/openai/endpoints/{id}/models` 返回 `{models:[...], success}`）。 */
@Serializable
data class EndpointModelList(
    val models: List<String> = emptyList(),
    val success: Boolean = false,
)

/** 密钥轮换结果（`POST /api/openai/keys/{id}/rotate` 返回明文新 Key）。 */
@Serializable
data class GatewayKeyRotateResult(
    val success: Boolean = false,
    @SerialName("apiKey") val apiKey: String = "",
)

@Serializable
data class GatewayLogPage(
    val records: List<GatewayLog> = emptyList(),
    val total: Long = 0,
)

@Serializable
data class GatewayLog(
    val id: Long = 0,
    val timestamp: String = "",
    val model: String = "",
    @SerialName("realModel") val realModel: String = "",
    @SerialName("endpointName") val endpointName: String = "",
    @SerialName("gatewayKeyName") val gatewayKeyName: String = "",
    @SerialName("statusCode") val statusCode: Int = 0,
    @SerialName("latencyMs") val latencyMs: Long = 0,
    @SerialName("ttfbMs") val ttfbMs: Long = 0,
    @SerialName("promptTokens") val promptTokens: Long = 0,
    @SerialName("completionTokens") val completionTokens: Long = 0,
    @SerialName("cachedTokens") val cachedTokens: Long = 0,
    @SerialName("totalTokens") val totalTokens: Long = 0,
    @SerialName("errorKind") val errorKind: String = "",
    @SerialName("errorMessage") val errorMessage: String = "",
    val stream: Boolean = false,
    val route: String = "",
) {
    val isSuccess: Boolean
        get() = statusCode in 200..299

    val displayModel: String
        get() = model.ifBlank { realModel }

    val latencyLabel: String
        get() = when {
            latencyMs >= 1000 -> "${(latencyMs / 100) / 10.0}s"
            else -> "${latencyMs}ms"
        }
}
