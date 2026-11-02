package com.iwvw.apimonitor.data.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * 模型网关插件（与面板 `OpenAIPluginsPanel` 的注册表一致）。
 *
 * 插件是「账号转 API」的上游适配器：Antigravity / DS2API / Gemini CLI 等。
 * 每个插件有独立的 status / accounts / settings / models 接口，
 * 统一前缀为 `/api/{pluginId}`。
 */
enum class GatewayPlugin(
    val id: String,
    val displayName: String,
    val description: String,
    val supportsModels: Boolean,
    val supportsAccounts: Boolean,
    val supportsCheckin: Boolean,
) {
    PROXY_POOL(
        id = "proxypool",
        displayName = "代理池",
        description = "可被插件或网关端点复用的出口代理池。",
        supportsModels = false,
        supportsAccounts = false,
        supportsCheckin = false,
    ),
    ANTIGRAVITY(
        id = "antigravity",
        displayName = "Antigravity",
        description = "Antigravity 账号转 API。",
        supportsModels = true,
        supportsAccounts = true,
        supportsCheckin = false,
    ),
    DS2API(
        id = "ds2api",
        displayName = "DS2API",
        description = "DeepSeek 网页版转 API。",
        supportsModels = false,
        supportsAccounts = true,
        supportsCheckin = false,
    ),
    GEMINI_CLI(
        id = "geminicli",
        displayName = "Gemini CLI",
        description = "Google Gemini CLI 账号转 API。",
        supportsModels = true,
        supportsAccounts = true,
        supportsCheckin = false,
    ),
    WORK_BUDDY(
        id = "workbuddy",
        displayName = "WorkBuddy",
        description = "腾讯 CodeBuddy / WorkBuddy 转 API。",
        supportsModels = true,
        supportsAccounts = true,
        supportsCheckin = true,
    ),
    LOBSTER_AI(
        id = "lobsterai",
        displayName = "LobsterAI",
        description = "网易有道 LobsterAI（龙虾）转 API。",
        supportsModels = true,
        supportsAccounts = true,
        supportsCheckin = true,
    ),
    POSTHOG_CODE(
        id = "posthogcode",
        displayName = "PostHog Code",
        description = "PostHog LLM Gateway 转 API。",
        supportsModels = true,
        supportsAccounts = true,
        supportsCheckin = false,
    ),
    QODER(
        id = "qoder",
        displayName = "Qoder",
        description = "Qoder（qoder.cn / qoder.com）账号转 API。",
        supportsModels = true,
        supportsAccounts = true,
        supportsCheckin = true,
    ),
    OPENCODE(
        id = "opencode",
        displayName = "OpenCode",
        description = "OpenCode Zen 免费模型转 OpenAI 兼容 API。",
        supportsModels = true,
        supportsAccounts = false,
        supportsCheckin = false,
    ),
}

/** 插件运行状态。各插件字段不完全一致，这里只取公共部分。 */
@Serializable
data class GatewayPluginStatus(
    val enabled: Boolean = false,
    @SerialName("accountCount") val accountCount: Int = 0,
    @SerialName("availableCount") val availableCount: Int = 0,
    @SerialName("enabledCount") val enabledCount: Int = 0,
    @SerialName("modelCount") val modelCount: Int = 0,
    @SerialName("coolingCount") val coolingCount: Int = 0,
    @SerialName("upstreamReady") val upstreamReady: Boolean = false,
    @SerialName("linkBaseUrl") val linkBaseUrl: String = "",
    @SerialName("discoveredCount") val discoveredCount: Int = 0,
    @SerialName("verifiedCount") val verifiedCount: Int = 0,
    @SerialName("sessionReady") val sessionReady: Boolean = false,
    val region: String = "",
    val strategy: String = "",
) {
    val accountLabel: String
        get() = when {
            accountCount <= 0 -> "无账号"
            availableCount > 0 -> "$availableCount/$accountCount 可用"
            enabledCount > 0 -> "${enabledCount} 个启用"
            else -> "$accountCount 个账号"
        }
}

/** 插件账号。字段按插件不同，缺失字段留空。 */
@Serializable
data class GatewayPluginAccount(
    val id: String = "",
    val name: String = "",
    val nickname: String = "",
    val email: String = "",
    val uid: String = "",
    val region: String = "",
    val available: Boolean = false,
    val disabled: Boolean = false,
    val cooling: Boolean = false,
    @SerialName("callCount") val callCount: Long = 0,
    val credits: Double = 0.0,
    @SerialName("planType") val planType: String = "",
    @SerialName("tokenState") val tokenState: String = "",
    @SerialName("expiresInSeconds") val expiresInSeconds: Long = 0,
    @SerialName("lastCheckinAt") val lastCheckinAt: String? = null,
) {
    val displayName: String
        get() = email.ifBlank { nickname.ifBlank { name.ifBlank { uid.ifBlank { id.take(12) } } } }

    val stateLabel: String
        get() = when {
            disabled -> "已禁用"
            cooling -> "冷却中"
            available -> "可用"
            else -> "不可用"
        }

    val expiryLabel: String
        get() = when {
            expiresInSeconds <= 0 -> "—"
            expiresInSeconds < 3600 -> "${expiresInSeconds / 60} 分钟后过期"
            else -> "${expiresInSeconds / 3600} 小时后过期"
        }
}

@Serializable
data class GatewayPluginAccountsResponse(
    val accounts: List<GatewayPluginAccount> = emptyList(),
    val success: Boolean = false,
)

/** 插件模型。 */
@Serializable
data class GatewayPluginModel(
    val id: String = "",
    val alias: String = "",
    @SerialName("displayName") val displayNameRaw: String = "",
    val enabled: Boolean = true,
    @SerialName("supportsThinking") val supportsThinking: Boolean? = null,
    @SerialName("maxTokens") val maxTokens: Long? = null,
) {
    val label: String
        get() = displayNameRaw.ifBlank { alias.ifBlank { id } }
}

@Serializable
data class GatewayPluginModelsResponse(
    val models: List<GatewayPluginModel> = emptyList(),
    val success: Boolean = false,
)
