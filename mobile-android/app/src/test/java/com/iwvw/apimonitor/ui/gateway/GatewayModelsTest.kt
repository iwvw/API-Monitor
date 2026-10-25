package com.iwvw.apimonitor.ui.gateway

import com.iwvw.apimonitor.data.model.EndpointHealth
import com.iwvw.apimonitor.data.model.EndpointHealthPoint
import com.iwvw.apimonitor.data.model.EndpointModelList
import com.iwvw.apimonitor.data.model.GatewayKey
import com.iwvw.apimonitor.data.model.GatewayKeyRotateResult
import kotlinx.serialization.json.Json
import java.time.LocalDate
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class GatewayModelsTest {

    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun `解析端点健康状态`() {
        val raw = """
            {"endpointId":"oai_1","healthStatus":"valid",
             "history":[
               {"checkedAt":"2026-10-24T05:57:52Z","responseTime":1200,"status":"operational"},
               {"checkedAt":"2026-10-24T05:57:52Z","responseTime":800,"status":"failed","errorMessage":"HTTP 429"}
             ],
             "lastHealthCheck":"2026-10-25T10:36:51Z"}
        """.trimIndent()

        val health = json.decodeFromString<EndpointHealth>(raw)

        assertEquals("valid", health.healthStatus)
        assertTrue(health.healthy)
        assertEquals("正常", health.statusLabel)
        assertEquals(1000L, health.averageLatencyMs)
        assertEquals(0.5, health.successRate!!, 0.001)
    }

    @Test
    fun `健康状态为空时显示未检查`() {
        val health = EndpointHealth()
        assertFalse(health.healthy)
        assertEquals("未检查", health.statusLabel)
        assertNull(health.averageLatencyMs)
        assertNull(health.successRate)
    }

    @Test
    fun `解析模型列表`() {
        val raw = """{"models":["gpt-4o","gpt-4"],"success":true}"""
        val list = json.decodeFromString<EndpointModelList>(raw)
        assertEquals(listOf("gpt-4o", "gpt-4"), list.models)
        assertTrue(list.success)
    }

    @Test
    fun `解析密钥字段`() {
        val raw = """
            {"id":"gk_1","name":"默认","enabled":true,"isDefault":true,
             "apiKey":"sk-am-abc","keyPrefix":"sk-am-abc","keySuffix":"xyz",
             "maskedKey":"sk-am-abc...xyz","requestCount":299604,
             "totalTokensUsed":72416804454,"expiresAt":null,
             "lastUsed":"2026-10-25T10:36:44Z","maxTokensQuota":0,
             "createdAt":"2026-07-27T16:10:36Z"}
        """.trimIndent()

        val key = json.decodeFromString<GatewayKey>(raw)

        assertEquals("默认", key.name)
        assertTrue(key.isDefault)
        assertEquals("sk-am-abc", key.displayKey)
        assertEquals("永不过期", key.expiryLabel)
        assertEquals(299604L, key.requestCount)
    }

    @Test
    fun `密钥没有明文时回落到掩码`() {
        val key = GatewayKey(maskedKey = "sk-am-abc...xyz")
        assertEquals("sk-am-abc...xyz", key.displayKey)
    }

    @Test
    fun `密钥有过期时间时直接显示`() {
        val key = GatewayKey(expiresAt = "2026-12-31T23:59:59Z")
        assertEquals("2026-12-31T23:59:59Z", key.expiryLabel)
    }

    @Test
    fun `解析轮换结果`() {
        val raw = """{"success":true,"apiKey":"sk-am-newkey"}"""
        val result = json.decodeFromString<GatewayKeyRotateResult>(raw)
        assertTrue(result.success)
        assertEquals("sk-am-newkey", result.apiKey)
    }

    @Test
    fun `ISO 日期解析`() {
        assertEquals(LocalDate.of(2026, 12, 31), parseIsoDate("2026-12-31T23:59:59Z"))
        assertEquals(LocalDate.of(2026, 12, 31), parseIsoDate("2026-12-31"))
        assertEquals(LocalDate.of(2026, 10, 25), parseIsoDate("2026-10-25T10:36:51Z"))
        assertNull(parseIsoDate(null))
        assertNull(parseIsoDate(""))
        assertNull(parseIsoDate("明天"))
    }

    @Test
    fun `时间缩写`() {
        assertEquals("10-25 10:36", shortTime("2026-10-25T10:36:51Z"))
        assertEquals("—", shortTime(null))
        assertEquals("—", shortTime(""))
        assertEquals("2026-10-25", shortTime("2026-10-25"))
    }

    @Test
    fun `健康历史为空时不计算成功率`() {
        val health = EndpointHealth(history = emptyList())
        assertNull(health.successRate)
        assertEquals(0, health.history.size)
    }

    @Test
    fun `健康历史全部失败时成功率为零`() {
        val health = EndpointHealth(
            history = listOf(
                EndpointHealthPoint(status = "failed", responseTime = 800),
            ),
        )
        assertEquals(0.0, health.successRate!!, 0.001)
        assertEquals(800L, health.averageLatencyMs)
    }
}
