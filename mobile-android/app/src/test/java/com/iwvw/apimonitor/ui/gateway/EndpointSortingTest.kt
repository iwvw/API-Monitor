package com.iwvw.apimonitor.ui.gateway

import com.iwvw.apimonitor.data.model.OpenAiEndpoint
import com.iwvw.apimonitor.data.model.sortedForDisplay
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class EndpointSortingTest {

    private fun endpoint(
        id: String,
        name: String,
        pluginId: String = "",
        priority: Int = 0,
        weight: Int = 0,
    ) = OpenAiEndpoint(
        id = id,
        name = name,
        pluginId = pluginId,
        priority = priority,
        weight = weight,
    )

    @Test
    fun `插件端点始终置顶`() {
        val list = listOf(
            endpoint("1", "普通-高优先级", priority = 100, weight = 100),
            endpoint("2", "插件", pluginId = "antigravity", priority = 0, weight = 100),
            endpoint("3", "普通-低优先级", priority = 1, weight = 100),
        )

        val sorted = list.sortedForDisplay()

        assertEquals("插件", sorted[0].name)
        assertEquals("普通-高优先级", sorted[1].name)
        assertEquals("普通-低优先级", sorted[2].name)
    }

    @Test
    fun `非插件端点按 priority 降序再按 weight 降序`() {
        val list = listOf(
            endpoint("a", "低", priority = 1, weight = 100),
            endpoint("b", "高-轻", priority = 9, weight = 10),
            endpoint("c", "高-重", priority = 9, weight = 90),
        )

        val sorted = list.sortedForDisplay()

        assertEquals(listOf("高-重", "高-轻", "低"), sorted.map { it.name })
    }

    @Test
    fun `同优先级同权重保持原始顺序`() {
        val list = listOf(
            endpoint("a", "第一"),
            endpoint("b", "第二"),
            endpoint("c", "第三"),
        )

        assertEquals(listOf("第一", "第二", "第三"), list.sortedForDisplay().map { it.name })
    }

    @Test
    fun `插件之间保持原始顺序`() {
        val list = listOf(
            endpoint("p1", "插件A", pluginId = "a", weight = 100),
            endpoint("p2", "插件B", pluginId = "b", weight = 100),
            endpoint("p3", "插件C", pluginId = "c", weight = 100),
        )

        assertEquals(listOf("插件A", "插件B", "插件C"), list.sortedForDisplay().map { it.name })
    }

    @Test
    fun `启用模型数排除 disabledModels`() {
        val ep = OpenAiEndpoint(
            models = listOf("gpt-4o", "gpt-4", "gpt-3.5"),
            disabledModels = listOf("gpt-4"),
        )

        assertEquals(2, ep.enabledModelCount)
    }

    @Test
    fun `apiKey 掩码保留前后片段`() {
        val ep = OpenAiEndpoint(apiKey = "sk-test-0000000000000000")
        assertTrue(ep.maskedApiKey.startsWith("sk-test"))
        assertTrue(ep.maskedApiKey.endsWith("0000"))
        assertFalse(ep.maskedApiKey.contains("0000000000"))
    }

    @Test
    fun `空 apiKey 掩码为占位符`() {
        assertEquals("—", OpenAiEndpoint(apiKey = "").maskedApiKey)
    }

    @Test
    fun `hostLabel 去掉协议与尾斜杠`() {
        assertEquals(
            "runanytime.hxi.me/v1",
            OpenAiEndpoint(baseUrl = "https://runanytime.hxi.me/v1/").hostLabel,
        )
    }
}
