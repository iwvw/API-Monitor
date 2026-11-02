package com.iwvw.apimonitor.data.model

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable

/**
 * `metrics:update` 推送载荷。
 * 服务端格式：{ serverId, metrics: {...}, timestamp }
 */
@Serializable
data class MetricsUpdate(
    val serverId: String = "",
    val timestamp: Long = 0,
    val metrics: LiveMetrics = LiveMetrics(),
)

@Serializable
data class LiveMetrics(
    @SerialName("cpu") val cpu: Double? = null,
    @SerialName("cpu_usage") val cpuUsage: String? = null,
    @SerialName("cpu_temp") val cpuTemp: Double? = null,
    @SerialName("cpu_model") val cpuModel: String? = null,
    @SerialName("cpu_power") val cpuPower: Double? = null,
    @SerialName("cores") val cores: Int? = null,
    @SerialName("physical_cores") val physicalCores: Int? = null,
    @SerialName("logical_cores") val logicalCores: Int? = null,
    @SerialName("load") val load: String? = null,

    @SerialName("mem_percent") val memPercent: Double? = null,
    @SerialName("mem_usage_percent") val memUsagePercent: Double? = null,
    @SerialName("mem") val mem: String? = null,
    @SerialName("mem_usage") val memUsage: String? = null,
    @SerialName("mem_used_mb") val memUsedMb: Long? = null,
    @SerialName("mem_total_mb") val memTotalMb: Long? = null,

    @SerialName("disk_percent") val diskPercent: Double? = null,
    @SerialName("disk_usage") val diskUsage: Double? = null,
    @SerialName("disk") val disk: String? = null,
    @SerialName("disk_used") val diskUsed: String? = null,
    @SerialName("disk_total") val diskTotal: String? = null,

    @SerialName("net_in_speed") val netInSpeed: Double? = null,
    @SerialName("net_out_speed") val netOutSpeed: Double? = null,
    @SerialName("net_in_transfer") val netInTransfer: Long? = null,
    @SerialName("net_out_transfer") val netOutTransfer: Long? = null,
    @SerialName("tcp_conn_count") val tcpConnCount: Int? = null,
    @SerialName("udp_conn_count") val udpConnCount: Int? = null,

    @SerialName("gpu_usage") val gpuUsage: Double? = null,
    @SerialName("gpu_temp") val gpuTemp: Double? = null,
    @SerialName("gpu_model") val gpuModel: String? = null,
    @SerialName("gpu_mem_percent") val gpuMemPercent: Double? = null,

    @SerialName("uptime") val uptime: String? = null,
    @SerialName("process_count") val processCount: Int? = null,
    @SerialName("platform") val platform: String? = null,
    @SerialName("metrics_health") val metricsHealth: String? = null,
    @SerialName("metrics_last_seen") val metricsLastSeen: String? = null,
    @SerialName("agent_version") val agentVersion: String? = null,

    @SerialName("network") val network: NetworkInfo? = null,
    @SerialName("docker") val docker: DockerInfo? = null,
) {
    val cpuPercent: Int?
        get() = cpuUsage.parsePercent() ?: cpu?.let { it.roundToIntSafe() }

    val memoryPercent: Int?
        get() = memPercent?.roundToIntSafe()
            ?: memUsagePercent?.roundToIntSafe()
            ?: memUsage.parsePercent()

    val diskPercentValue: Int?
        get() = diskPercent?.roundToIntSafe()
            ?: diskUsage?.roundToIntSafe()

    val memoryDetail: String?
        get() = when {
            !memUsage.isNullOrBlank() -> memUsage.replace("/", " / ")
            !mem.isNullOrBlank() -> mem.replace("/", " / ")
            memUsedMb != null && memTotalMb != null -> "${memUsedMb}MB / ${memTotalMb}MB"
            else -> null
        }

    val rxSpeedText: String?
        get() = network?.rxSpeed?.takeIf { it.isNotBlank() }
            ?: netInSpeed?.let(::formatSpeed)

    val txSpeedText: String?
        get() = network?.txSpeed?.takeIf { it.isNotBlank() }
            ?: netOutSpeed?.let(::formatSpeed)

    val connCount: Int?
        get() = network?.connections ?: tcpConnCount?.let { tcp ->
            udpConnCount?.let { tcp + it } ?: tcp
        }
}

private fun Double.roundToIntSafe(): Int = when {
    isNaN() -> 0
    this < 0 -> 0
    this > 100 -> 100
    // 与面板 Web 端一致：百分比向下取整，避免 76.79% 显示成 77%
    else -> kotlin.math.floor(this).toInt()
}

fun formatSpeed(bytesPerSecond: Double): String {
    if (!bytesPerSecond.isFinite() || bytesPerSecond < 0) return "0 B/s"
    val units = listOf("B/s", "KB/s", "MB/s", "GB/s")
    var value = bytesPerSecond
    var index = 0
    while (value >= 1024 && index < units.lastIndex) {
        value /= 1024
        index += 1
    }
    return if (index == 0) {
        "${value.toInt()} ${units[index]}"
    } else {
        val rounded = kotlin.math.round(value * 10) / 10
        "$rounded ${units[index]}"
    }
}

/** 把一条实时推送合并进服务器快照。 */
fun ServerSummary.applyLiveMetrics(update: MetricsUpdate): ServerSummary {
    val live = update.metrics
    val current = info ?: ServerInfo()

    val nextCpu = (current.cpu ?: CpuInfo()).copy(
        usage = live.cpuPercent?.let { "$it%" } ?: current.cpu?.usage ?: "0%",
        temp = live.cpuTemp?.takeIf { it > 0 } ?: current.cpu?.temp,
        load = live.load ?: current.cpu?.load,
        model = live.cpuModel ?: current.cpu?.model,
        cores = live.cores ?: current.cpu?.cores,
        physicalCores = live.physicalCores ?: current.cpu?.physicalCores,
        logicalCores = live.logicalCores ?: current.cpu?.logicalCores,
    )

    val nextMemory = (current.memory ?: MemoryInfo()).copy(
        usage = live.memoryPercent?.let { "$it%" } ?: current.memory?.usage ?: "0%",
        used = live.memUsedMb?.toString() ?: current.memory?.used ?: "",
        total = live.memTotalMb?.takeIf { it > 0 }?.let { "${it}MB" } ?: current.memory?.total ?: "",
    )

    val nextDisk = if (current.disk.isEmpty()) {
        listOf(
            DiskInfo(
                device = "/",
                used = live.diskUsed.orEmpty(),
                total = live.diskTotal.orEmpty(),
                usage = live.diskPercentValue?.let { "$it%" } ?: "0%",
            ),
        )
    } else {
        val head = current.disk.first()
        listOf(
            head.copy(
                used = live.diskUsed ?: head.used,
                total = live.diskTotal ?: head.total,
                usage = live.diskPercentValue?.let { "$it%" } ?: head.usage,
            ),
        ) + current.disk.drop(1)
    }

    // 累计流量不参与实时合并（推送里没有累计字段，聚合口径以接口快照为准），
    // 直接沿用快照值，避免被空串覆盖。
    val nextNetwork = NetworkInfo(
        rxSpeed = live.rxSpeedText ?: current.network?.rxSpeed.orEmpty(),
        txSpeed = live.txSpeedText ?: current.network?.txSpeed.orEmpty(),
        rxTotal = current.network?.rxTotal.orEmpty(),
        txTotal = current.network?.txTotal.orEmpty(),
        connections = live.connCount ?: current.network?.connections,
    )

    val nextGpu = if (current.gpu.isEmpty()) {
        live.gpuUsage?.let { usage ->
            listOf(
                GpuInfo(
                    name = live.gpuModel.orEmpty(),
                    usage = usage.roundToIntSafe(),
                    temp = live.gpuTemp?.let { kotlin.math.round(it).toInt() },
                ),
            )
        }.orEmpty()
    } else {
        current.gpu.mapIndexed { index, gpu ->
            if (index != 0) {
                gpu
            } else {
                gpu.copy(
                    name = live.gpuModel ?: gpu.name,
                    usage = live.gpuUsage?.roundToIntSafe() ?: gpu.usage,
                    temp = live.gpuTemp?.let { kotlin.math.round(it).toInt() } ?: gpu.temp,
                )
            }
        }
    }

    val nextInfo = current.copy(
        cpu = nextCpu,
        memory = nextMemory,
        disk = nextDisk,
        network = if (
            nextNetwork.rxSpeed.isBlank() &&
            nextNetwork.txSpeed.isBlank() &&
            nextNetwork.connections == null
        ) {
            current.network
        } else {
            nextNetwork
        },
        gpu = nextGpu,
        docker = live.docker ?: current.docker,
        platform = live.platform ?: current.platform,
        uptime = live.uptime ?: current.uptime,
        agentVersion = live.agentVersion ?: current.agentVersion,
    )

    return copy(
        info = nextInfo,
        agentOnline = true,
        isOnline = true,
        metricsHealth = live.metricsHealth ?: metricsHealth,
        metricsAgeMs = 0,
        metricsStale = false,
    )
}
