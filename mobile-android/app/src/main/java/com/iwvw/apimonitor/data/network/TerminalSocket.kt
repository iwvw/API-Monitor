package com.iwvw.apimonitor.data.network

import android.util.Log
import com.iwvw.apimonitor.data.settings.Credentials
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.Json
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import java.util.concurrent.TimeUnit

/**
 * 服务器终端 WebSocket 客户端。
 *
 * 对接后端 `/ws/ssh?server_id=<id>&transport=auto`：
 * `transport=auto` 时由服务端决定走 Agent 原生 PTY 还是 SSH，
 * 客户端不需要关心两者差异，只处理统一的 JSON 帧。
 *
 * 协议（文本帧）：
 * ```
 * 客户端 → 服务端  {"type":"input","data":"ls\n"}
 *                  {"type":"resize","cols":120,"rows":40}
 *                  {"type":"disconnect"}
 * 服务端 → 客户端  {"type":"data","data":"..."}
 *                  {"type":"status","data":"connected"}
 *                  {"type":"error","data":"AGENT_OFFLINE: ..."}
 * ```
 */
class TerminalSocket(
    private val credentials: Credentials,
    private val cookieJar: SessionCookieJar,
) {
    /** 终端连接状态。 */
    enum class State {
        CONNECTING,
        CONNECTED,
        CLOSED,
        FAILED,
    }

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

    private val json = Json { ignoreUnknownKeys = true }

    private val client: OkHttpClient = OkHttpClient.Builder()
        .cookieJar(cookieJar)
        .connectTimeout(20, TimeUnit.SECONDS)
        // 终端是长连接：读超时设 0 交给 WebSocket 心跳，避免长时间无输出被误判断线。
        .readTimeout(0, TimeUnit.MILLISECONDS)
        .pingInterval(25, TimeUnit.SECONDS)
        .retryOnConnectionFailure(true)
        .build()

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)

    private val _state = MutableStateFlow(State.CONNECTING)
    val state: StateFlow<State> = _state.asStateFlow()

    /** PTY 输出（原始字节，交给终端仿真器）。 */
    private val _output = MutableSharedFlow<ByteArray>(
        replay = 0,
        extraBufferCapacity = 256,
    )
    val output: SharedFlow<ByteArray> = _output.asSharedFlow()

    /** 错误或状态提示文案。 */
    private val _message = MutableSharedFlow<String>(extraBufferCapacity = 16)
    val message: SharedFlow<String> = _message.asSharedFlow()

    private var webSocket: WebSocket? = null
    private var closedByUser = false

    fun connect(serverId: String) {
        val base = credentials.normalizedBaseUrl.toHttpUrlOrNull()
        if (base == null) {
            _state.value = State.FAILED
            scope.launch { _message.emit("面板地址格式不正确") }
            return
        }

        val url = base.newBuilder()
            .addPathSegments("ws/ssh")
            .addQueryParameter("server_id", serverId)
            .addQueryParameter("transport", "auto")
            .build()

        val request = Request.Builder()
            .url(url)
            .header("Accept", "*/*")
            .apply {
                // API Token 模式下 cookie 里没有 sid，需要显式带 Bearer。
                if (credentials.usesApiToken && cookieJar.currentSid() == null) {
                    header("Authorization", "Bearer ${credentials.apiToken}")
                }
            }
            .build()

        closedByUser = false
        _state.value = State.CONNECTING

        val listener = object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                Log.d(TAG, "terminal ws open")
                _state.value = State.CONNECTED
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                handleFrame(text)
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                Log.w(TAG, "terminal ws failed: ${t.message}")
                if (closedByUser) return
                _state.value = State.FAILED
                scope.launch { _message.emit("连接中断：${t.message ?: "网络异常"}") }
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                Log.d(TAG, "terminal ws closed: $code $reason")
                if (closedByUser) return
                _state.value = State.CLOSED
            }
        }

        webSocket = client.newWebSocket(request, listener)
    }

    private fun handleFrame(text: String) {
        val frame = runCatching { json.decodeFromString<InFrame>(text) }.getOrNull() ?: return
        when (frame.type) {
            // PTY 输出：转成字节交给终端仿真器。
            "data" -> frame.data?.let { data ->
                if (data.isNotEmpty()) {
                    _output.tryEmit(data.toByteArray(Charsets.UTF_8))
                }
            }

            "status" -> {
                // 连接成功的状态帧不弹提示：终端自己已经能看出连上了，
                // 再弹一个 Snackbar 会盖住底部快捷键栏。
                _state.value = State.CONNECTED
            }

            "error" -> {
                val raw = frame.data.orEmpty()
                val friendly = when {
                    raw.startsWith("AGENT_OFFLINE") -> "主机 Agent 离线，且未配置 SSH 直连"
                    raw.startsWith("TERMINAL_V2_FAILED") -> "终端会话建立失败"
                    raw.startsWith("AGENT_PTY_START_FAILED") -> "主机上启动 PTY 失败"
                    raw.startsWith("CONNECTION_FAILED") -> "SSH 连接失败"
                    raw.startsWith("SESSION_FAILED") -> "SSH 会话创建失败"
                    raw.startsWith("PTY_FAILED") -> "SSH PTY 分配失败"
                    else -> raw
                }
                _state.value = State.FAILED
                scope.launch { _message.emit(friendly) }
            }
        }
    }

    /** 发送键盘输入。字节直接按 UTF-8 转成字符串放进 JSON。 */
    fun sendInput(bytes: ByteArray) {
        if (bytes.isEmpty()) return
        val text = String(bytes, Charsets.UTF_8)
        send(json.encodeToString(OutFrame.serializer(), OutFrame(type = "input", data = text)))
    }

    /** 通知服务端终端尺寸变化，否则 vim / htop 排版会错乱。 */
    fun sendResize(cols: Int, rows: Int) {
        if (cols <= 0 || rows <= 0) return
        send(json.encodeToString(OutFrame.serializer(), OutFrame(type = "resize", cols = cols, rows = rows)))
    }

    private fun send(payload: String) {
        webSocket?.send(payload)
    }

    fun disconnect() {
        closedByUser = true
        send(json.encodeToString(OutFrame.serializer(), OutFrame(type = "disconnect")))
        webSocket?.close(NORMAL_CLOSURE, "user closed")
        webSocket = null
        _state.value = State.CLOSED
    }

    fun release() {
        disconnect()
        scope.cancel()
    }

    private companion object {
        const val TAG = "TerminalSocket"
        const val NORMAL_CLOSURE = 1000
    }
}
