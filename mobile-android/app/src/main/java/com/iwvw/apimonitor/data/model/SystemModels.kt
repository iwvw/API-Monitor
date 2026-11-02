package com.iwvw.apimonitor.data.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

@Serializable
data class HostMetricsResponse(
    val success: Boolean = false,
    val data: HostMetrics = HostMetrics(),
)

@Serializable
data class HostMetrics(
    val cpu: HostCpu? = null,
    val memory: HostMemory? = null,
    val disk: HostDisk? = null,
    val platform: String? = null,
    @SerialName("platformLabel") val platformLabel: String? = null,
    val uptime: Long = 0,
    val hostname: String? = null,
)

@Serializable
data class HostCpu(
    val usage: Double = 0.0,
    val cores: Int = 0,
    val logicalCores: Int = 0,
    val model: String? = null,
    @SerialName("loadAverage") val loadAverage: List<Double> = emptyList(),
)

@Serializable
data class HostMemory(
    val total: Long = 0,
    val used: Long = 0,
    val free: Long = 0,
    val usage: Double = 0.0,
)

@Serializable
data class HostDisk(
    val root: String = "/",
    val total: Long = 0,
    val used: Long = 0,
    val free: Long = 0,
    val usage: Double = 0.0,
)

@Serializable
data class ApiStatsResponse(
    val success: Boolean = false,
    val data: ApiStats = ApiStats(),
)

@Serializable
data class ApiStats(
    val tokens: Long = 0,
    val traffic: Long = 0,
    val total: ApiStatsTotal = ApiStatsTotal(),
    val trend: List<ApiStatsBucket> = emptyList(),
)

@Serializable
data class ApiStatsTotal(
    val all: Long = 0,
    val audit: Long = 0,
    val ops: Long = 0,
)

@Serializable
data class ApiStatsBucket(
    val bucket: String = "",
    val total: Long = 0,
    val tokens: Long = 0,
    val traffic: Long = 0,
) {
    /**
     * bucket 形如 2026-10-12，取 MM-DD 做横轴标签。
     * 非 `yyyy-MM-dd` 的输入（例如后端改了时间格式）直接原样返回，
     * 避免把 `20261012` 切成 `20261` 这种残片。
     */
    val shortLabel: String
        get() {
            if (bucket.length < 10 || bucket.count { it == '-' } != 2) return bucket
            return bucket.substringAfter('-').take(5)
        }
}
