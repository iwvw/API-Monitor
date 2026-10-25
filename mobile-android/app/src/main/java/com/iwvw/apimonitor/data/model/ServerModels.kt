package com.iwvw.apimonitor.data.model

import kotlinx.serialization.KSerializer
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.SerializationException
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder
import kotlinx.serialization.json.JsonDecoder
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.JsonEncoder
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.contentOrNull
import kotlinx.serialization.json.intOrNull
import kotlinx.serialization.json.jsonPrimitive
import kotlinx.serialization.json.longOrNull

internal object LenientIntSerializer : KSerializer<Int?> {
    override val descriptor: SerialDescriptor =
        PrimitiveSerialDescriptor("LenientInt", PrimitiveKind.INT)

    override fun deserialize(decoder: Decoder): Int? {
        val input = decoder as? JsonDecoder ?: throw SerializationException("LenientInt requires JSON")
        val primitive = input.decodeJsonElement().jsonPrimitive
        return primitive.intOrNull
            ?: primitive.contentOrNull?.trim()?.toDoubleOrNull()?.toInt()
    }

    override fun serialize(encoder: Encoder, value: Int?) {
        val output = encoder as? JsonEncoder ?: throw SerializationException("LenientInt requires JSON")
        output.encodeJsonElement(value?.let { JsonPrimitive(it) } ?: JsonPrimitive(null as String?))
    }
}

internal object LenientLongSerializer : KSerializer<Long?> {
    override val descriptor: SerialDescriptor =
        PrimitiveSerialDescriptor("LenientLong", PrimitiveKind.LONG)

    override fun deserialize(decoder: Decoder): Long? {
        val input = decoder as? JsonDecoder ?: throw SerializationException("LenientLong requires JSON")
        val primitive = input.decodeJsonElement().jsonPrimitive
        return primitive.longOrNull
            ?: primitive.contentOrNull?.trim()?.toDoubleOrNull()?.toLong()
    }

    override fun serialize(encoder: Encoder, value: Long?) {
        val output = encoder as? JsonEncoder ?: throw SerializationException("LenientLong requires JSON")
        output.encodeJsonElement(value?.let { JsonPrimitive(it) } ?: JsonPrimitive(null as String?))
    }
}

internal object LenientDoubleSerializer : KSerializer<Double?> {
    override val descriptor: SerialDescriptor =
        PrimitiveSerialDescriptor("LenientDouble", PrimitiveKind.DOUBLE)

    override fun deserialize(decoder: Decoder): Double? {
        val input = decoder as? JsonDecoder ?: throw SerializationException("LenientDouble requires JSON")
        val primitive = input.decodeJsonElement().jsonPrimitive
        return primitive.contentOrNull?.trim()?.toDoubleOrNull()
    }

    override fun serialize(encoder: Encoder, value: Double?) {
        val output = encoder as? JsonEncoder ?: throw SerializationException("LenientDouble requires JSON")
        output.encodeJsonElement(value?.let { JsonPrimitive(it) } ?: JsonPrimitive(null as String?))
    }
}

internal object LenientBoolSerializer : KSerializer<Boolean> {
    override val descriptor: SerialDescriptor =
        PrimitiveSerialDescriptor("LenientBool", PrimitiveKind.BOOLEAN)

    override fun deserialize(decoder: Decoder): Boolean {
        val input = decoder as? JsonDecoder ?: throw SerializationException("LenientBool requires JSON")
        val primitive = input.decodeJsonElement().jsonPrimitive
        return primitive.booleanOrNull
            ?: when (primitive.contentOrNull?.trim()?.lowercase()) {
                "1", "true", "yes" -> true
                else -> false
            }
    }

    override fun serialize(encoder: Encoder, value: Boolean) {
        val output = encoder as? JsonEncoder ?: throw SerializationException("LenientBool requires JSON")
        output.encodeJsonElement(JsonPrimitive(value))
    }
}

internal object LenientStringSerializer : KSerializer<String> {
    override val descriptor: SerialDescriptor =
        PrimitiveSerialDescriptor("LenientString", PrimitiveKind.STRING)

    override fun deserialize(decoder: Decoder): String {
        val input = decoder as? JsonDecoder ?: throw SerializationException("LenientString requires JSON")
        return input.decodeJsonElement().jsonPrimitive.contentOrNull.orEmpty()
    }

    override fun serialize(encoder: Encoder, value: String) {
        val output = encoder as? JsonEncoder ?: throw SerializationException("LenientString requires JSON")
        output.encodeJsonElement(JsonPrimitive(value))
    }
}

internal object LenientNumberListSerializer : KSerializer<List<String>> {
    override val descriptor: SerialDescriptor =
        PrimitiveSerialDescriptor("LenientNumberList", PrimitiveKind.STRING)

    override fun deserialize(decoder: Decoder): List<String> {
        val input = decoder as? JsonDecoder ?: throw SerializationException("LenientNumberList requires JSON")
        val element = input.decodeJsonElement()
        return when {
            element is kotlinx.serialization.json.JsonArray ->
                element.mapNotNull { it.jsonPrimitive.contentOrNull }
            else -> element.jsonPrimitive.contentOrNull
                ?.split(",")
                ?.map { it.trim() }
                ?.filter { it.isNotEmpty() }
                .orEmpty()
        }
    }

    override fun serialize(encoder: Encoder, value: List<String>) {
        val output = encoder as? JsonEncoder ?: throw SerializationException("LenientNumberList requires JSON")
        output.encodeJsonElement(kotlinx.serialization.json.JsonArray(value.map { JsonPrimitive(it) }))
    }
}

@Serializable
data class ServerAccountsResponse(
    val success: Boolean = false,
    val data: List<ServerSummary> = emptyList(),
)

@Serializable
data class ServerSummary(
    val id: String = "",
    val name: String = "",
    val host: String = "",
    @Serializable(with = LenientIntSerializer::class) val port: Int? = null,
    @SerialName("countryCode") val countryCode: String? = null,
    val location: String? = null,
    val description: String = "",
    @Serializable(with = LenientNumberListSerializer::class) val tags: List<String> = emptyList(),
    @SerialName("agent_online")
    @Serializable(with = LenientBoolSerializer::class)
    val agentOnline: Boolean = false,
    @SerialName("agent_connected")
    @Serializable(with = LenientBoolSerializer::class)
    val agentConnected: Boolean = false,
    @SerialName("is_online")
    @Serializable(with = LenientBoolSerializer::class)
    val isOnline: Boolean = false,
    @SerialName("last_check_status") val lastCheckStatus: String? = null,
    @SerialName("response_time")
    @Serializable(with = LenientIntSerializer::class)
    val responseTime: Int? = null,
    @SerialName("monitor_mode") val monitorMode: String? = null,
    @SerialName("metrics_health") val metricsHealth: String? = null,
    @SerialName("metrics_age_ms")
    @Serializable(with = LenientLongSerializer::class)
    val metricsAgeMs: Long? = null,
    @SerialName("metrics_stale")
    @Serializable(with = LenientBoolSerializer::class)
    val metricsStale: Boolean = false,
    @SerialName("supports_metrics")
    @Serializable(with = LenientBoolSerializer::class)
    val supportsMetrics: Boolean = false,
    @SerialName("supports_docker")
    @Serializable(with = LenientBoolSerializer::class)
    val supportsDocker: Boolean = false,
    @SerialName("supports_terminal")
    @Serializable(with = LenientBoolSerializer::class)
    val supportsTerminal: Boolean = false,
    @SerialName("supports_sftp")
    @Serializable(with = LenientBoolSerializer::class)
    val supportsSftp: Boolean = false,
    @SerialName("traffic_limit_bytes")
    @Serializable(with = LenientLongSerializer::class)
    val trafficLimitBytes: Long? = null,
    @SerialName("traffic_alert_enabled")
    @Serializable(with = LenientBoolSerializer::class)
    val trafficAlertEnabled: Boolean = false,
    @SerialName("traffic_alert_percent")
    @Serializable(with = LenientIntSerializer::class)
    val trafficAlertPercent: Int? = null,
    val info: ServerInfo? = null,
) {
    val isLive: Boolean
        get() = agentOnline || isOnline

    val displayLocation: String
        get() = when {
            !location.isNullOrBlank() -> location
            !countryCode.isNullOrBlank() -> countryCode.uppercase()
            else -> "未知位置"
        }
}

@Serializable
data class ServerInfo(
    val cpu: CpuInfo? = null,
    val memory: MemoryInfo? = null,
    val disk: List<DiskInfo> = emptyList(),
    val network: NetworkInfo? = null,
    val gpu: List<GpuInfo> = emptyList(),
    val docker: DockerInfo? = null,
    val platform: String? = null,
    @SerialName("platformVersion") val platformVersion: String? = null,
    @SerialName("agentVersion") val agentVersion: String? = null,
    val uptime: String? = null,
    val ip: String? = null,
)

@Serializable
data class CpuInfo(
    @SerialName("Cores")
    @Serializable(with = LenientIntSerializer::class)
    val cores: Int? = null,
    @SerialName("LogicalCores")
    @Serializable(with = LenientIntSerializer::class)
    val logicalCores: Int? = null,
    @SerialName("PhysicalCores")
    @Serializable(with = LenientIntSerializer::class)
    val physicalCores: Int? = null,
    @SerialName("Usage")
    @Serializable(with = LenientStringSerializer::class)
    val usage: String = "0%",
    @SerialName("Load") val load: String? = null,
    @SerialName("Model") val model: String? = null,
    @SerialName("Temp")
    @Serializable(with = LenientDoubleSerializer::class)
    val temp: Double? = null,
)

@Serializable
data class MemoryInfo(
    @SerialName("Total") @Serializable(with = LenientStringSerializer::class) val total: String = "",
    @SerialName("Used") @Serializable(with = LenientStringSerializer::class) val used: String = "",
    @SerialName("Usage") @Serializable(with = LenientStringSerializer::class) val usage: String = "0%",
)

@Serializable
data class DiskInfo(
    @Serializable(with = LenientStringSerializer::class) val device: String = "",
    @Serializable(with = LenientStringSerializer::class) val total: String = "",
    @Serializable(with = LenientStringSerializer::class) val used: String = "",
    @Serializable(with = LenientStringSerializer::class) val usage: String = "0%",
)

@Serializable
data class NetworkInfo(
    @SerialName("rx_speed") @Serializable(with = LenientStringSerializer::class) val rxSpeed: String = "",
    @SerialName("tx_speed") @Serializable(with = LenientStringSerializer::class) val txSpeed: String = "",
    @SerialName("rx_total") @Serializable(with = LenientStringSerializer::class) val rxTotal: String = "",
    @SerialName("tx_total") @Serializable(with = LenientStringSerializer::class) val txTotal: String = "",
    @Serializable(with = LenientIntSerializer::class) val connections: Int? = null,
)

@Serializable
data class GpuInfo(
    val name: String = "",
    @Serializable(with = LenientIntSerializer::class) val usage: Int? = null,
    @Serializable(with = LenientIntSerializer::class) val temp: Int? = null,
    @SerialName("memoryUsed")
    @Serializable(with = LenientLongSerializer::class)
    val memoryUsed: Long? = null,
    @SerialName("memoryTotal")
    @Serializable(with = LenientLongSerializer::class)
    val memoryTotal: Long? = null,
)

@Serializable
data class DockerInfo(
    @Serializable(with = LenientBoolSerializer::class) val installed: Boolean = false,
    @Serializable(with = LenientIntSerializer::class) val running: Int? = null,
    @Serializable(with = LenientIntSerializer::class) val stopped: Int? = null,
)

fun String?.parsePercent(): Int? =
    this?.trim()?.removeSuffix("%")?.trim()?.toFloatOrNull()?.toInt()

fun List<GpuInfo>.hasDiscreteGpu(): Boolean = isNotEmpty()
