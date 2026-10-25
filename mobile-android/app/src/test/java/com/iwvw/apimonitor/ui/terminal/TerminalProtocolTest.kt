package com.iwvw.apimonitor.ui.terminal

import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * 终端 WebSocket 协议帧的编解码回归。
 *
 * 这些字段必须与后端 `backend-go/internal/serveragent/terminal_ws.go`
 * 里的 `terminalWSMessage` 保持一致，否则输入发不出去或输出收不到。
 */
class TerminalProtocolTest {

    private val json = Json { ignoreUnknownKeys = true }

    @Serializable
    private data class OutFrame(
        val type: String,
        val data: String? = null,
        val cols: Int = 0,
        val rows: Int = 0,
    )

    @Serializable
    private data class InFrame(
        val type: String = "",
        val data: String? = null,
        @SerialName("transport") val transport: String? = null,
    )

    @Test
    fun `输入帧编码为 input + data`() {
        val frame = OutFrame(type = "input", data = "ls -la\n")
        val encoded = json.encodeToString(OutFrame.serializer(), frame)
        assertEquals("""{"type":"input","data":"ls -la\n"}""", encoded)
    }

    @Test
    fun `resize 帧带上 cols 与 rows`() {
        val frame = OutFrame(type = "resize", cols = 120, rows = 40)
        val encoded = json.encodeToString(OutFrame.serializer(), frame)
        assertEquals("""{"type":"resize","cols":120,"rows":40}""", encoded)
    }

    @Test
    fun `解析 data 输出帧`() {
        val frame = json.decodeFromString<InFrame>("""{"type":"data","data":"hello","transport":"agent"}""")
        assertEquals("data", frame.type)
        assertEquals("hello", frame.data)
        assertEquals("agent", frame.transport)
    }

    @Test
    fun `解析 status 帧`() {
        val frame = json.decodeFromString<InFrame>("""{"type":"status","data":"connected"}""")
        assertEquals("status", frame.type)
        assertEquals("connected", frame.data)
    }

    @Test
    fun `解析 error 帧`() {
        val frame = json.decodeFromString<InFrame>(
            """{"type":"error","data":"AGENT_OFFLINE: agent is not connected"}""",
        )
        assertEquals("error", frame.type)
        assertTrue(frame.data!!.startsWith("AGENT_OFFLINE"))
    }

    @Test
    fun `未知字段不会导致解析失败`() {
        val frame = json.decodeFromString<InFrame>(
            """{"type":"data","data":"x","unknownField":123}""",
        )
        assertEquals("x", frame.data)
    }

    @Test
    fun `中文按 UTF-8 往返不丢字节`() {
        val original = "中文输出\n"
        val bytes = original.toByteArray(Charsets.UTF_8)
        assertEquals(original, String(bytes, Charsets.UTF_8))
    }

    @Test
    fun `控制字符序列保持原样`() {
        // Esc、Ctrl-C、方向键都是控制字符，JSON 转义后必须能还原
        val sequences = mapOf(
            "Esc" to "\u001b",
            "Ctrl-C" to "\u0003",
            "Ctrl-D" to "\u0004",
            "Tab" to "\t",
            "Up" to "\u001b[A",
            "Down" to "\u001b[B",
        )
        sequences.forEach { (label, seq) ->
            val encoded = json.encodeToString(OutFrame.serializer(), OutFrame(type = "input", data = seq))
            val decoded = json.decodeFromString<InFrame>(encoded)
            assertEquals("$label 序列丢失", seq, decoded.data)
        }
    }
}
