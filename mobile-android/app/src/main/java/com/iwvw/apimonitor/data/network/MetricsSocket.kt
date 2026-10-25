package com.iwvw.apimonitor.data.network

import android.util.Log
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableSharedFlow
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.SharedFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asSharedFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import java.util.concurrent.TimeUnit

/**
 * 极简 Socket.IO v4 (EIO=4) 客户端，只覆盖本项目需要的部分：
 * 直连 WebSocket 传输、`/metrics` 命名空间、服务端事件订阅、心跳应答。
 *
 * 服务端实现见 backend-go/internal/serveragent/engineio.go：
 * - 连接后服务端先发 `0{...}` Engine.IO 握手包
 * - 客户端发 `40/metrics,` 进入指标命名空间
 * - 服务端回 `40/metrics,{sid}` 后开始推送 `42/metrics,[event,payload]`
 */
class MetricsSocket(
    private val httpClient: OkHttpClient,
    private val json: Json,
) {
    private val _connection = MutableStateFlow<ConnectionState>(ConnectionState.Disconnected)
    val connection: StateFlow<ConnectionState> = _connection.asStateFlow()

    private val _events = MutableSharedFlow<SocketEvent>(extraBufferCapacity = 64)
    val events: SharedFlow<SocketEvent> = _events.asSharedFlow()

    private var webSocket: WebSocket? = null
    private var supervisor: Job? = null
    private var pingIntervalMs = 25_000L
    private var lastServerPacketAt = 0L
    private var connectedBaseUrl = ""

    fun connect(scope: CoroutineScope, baseUrl: String) {
        val normalized = baseUrl.trim().trimEnd('/')
        // 凭据被清空（如退出登录）时 MainScaffold 仍可能在退场动画里重组，
        // 此时 baseUrl 是空串。拼出来的 "/socket.io/..." 没有 scheme，
        // OkHttp 的 Request.Builder.url() 会直接抛 IllegalArgumentException 把进程打崩。
        if (!normalized.startsWith("http://") && !normalized.startsWith("https://")) {
            Log.w(TAG, "skip connect: invalid base url '$normalized'")
            connectedBaseUrl = ""
            disconnect()
            return
        }
        val state = _connection.value
        if (normalized == connectedBaseUrl && state != ConnectionState.Disconnected) {
            // 已经连上或正在连同一个地址，不重建连接。
            return
        }
        disconnect()
        connectedBaseUrl = normalized
        val url = normalized
            .replaceFirst("http://", "ws://")
            .replaceFirst("https://", "wss://") +
            "/socket.io/?transport=websocket&EIO=4"

        _connection.value = ConnectionState.Connecting
        val request = Request.Builder()
            .url(url)
            .header("Accept", "*/*")
            .build()

        val listener = object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                Log.d(TAG, "socket opened")
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                lastServerPacketAt = System.currentTimeMillis()
                handlePacket(webSocket, text)
            }

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                Log.w(TAG, "socket failure: ${t.message}")
                _connection.value = ConnectionState.Disconnected
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) {
                Log.d(TAG, "socket closed: $code $reason")
                _connection.value = ConnectionState.Disconnected
            }
        }

        webSocket = httpClient.newWebSocket(request, listener)

        supervisor = scope.launch {
            while (isActive) {
                delay(WATCHDOG_MS)
                val stale = lastServerPacketAt > 0 &&
                    System.currentTimeMillis() - lastServerPacketAt > pingIntervalMs + WATCHDOG_MS
                if (stale) {
                    Log.w(TAG, "no packets for too long, reconnecting")
                    _connection.value = ConnectionState.Disconnected
                    webSocket?.close(1000, "stale")
                    webSocket = null
                    deliver(ConnectionState.Disconnected)
                    break
                }
            }
        }
    }

    fun disconnect() {
        supervisor?.cancel()
        supervisor = null
        webSocket?.close(1000, "client closing")
        webSocket = null
        lastServerPacketAt = 0
        if (_connection.value != ConnectionState.Disconnected) {
            _connection.value = ConnectionState.Disconnected
        }
    }

    private fun handlePacket(webSocket: WebSocket, packet: String) {
        if (packet.isEmpty()) return
        Log.d(TAG, "recv ${packet.take(60)}")
        when (packet[0]) {
            '0' -> {
                runCatching {
                    val obj = json.parseToJsonElement(packet.substring(1)) as? JsonObject
                    obj?.get("pingInterval")?.let { element ->
                        (element as? kotlinx.serialization.json.JsonPrimitive)
                            ?.content?.toLongOrNull()
                            ?.let { pingIntervalMs = it }
                    }
                }
                // 只在尚未进入命名空间时连接一次，避免服务端重复回 ACK 造成循环。
                if (_connection.value != ConnectionState.Connected) {
                    webSocket.send("40/metrics,")
                }
            }

            '1' -> webSocket.send("2")

            '2' -> webSocket.send("3")

            '4' -> handleSocketIoPacket(webSocket, packet.substring(1))

            else -> Unit
        }
    }

    private fun handleSocketIoPacket(webSocket: WebSocket, body: String) {
        if (body.isEmpty()) return
        // Socket.IO 包格式：<type>[/namespace,][payload]
        // 例：40/metrics,{sid} → type='0' 是 CONNECT，namespace="/metrics"，payload="{sid}"
        var rest = body
        val type = rest[0]
        rest = rest.substring(1)

        var namespace = ""
        if (rest.startsWith("/")) {
            val comma = rest.indexOf(',')
            namespace = if (comma >= 0) rest.substring(0, comma) else rest
            rest = if (comma >= 0) rest.substring(comma + 1) else ""
        }

        when (type) {
            '0' -> {
                // CONNECT ACK
                if (namespace == NAMESPACE) {
                    if (_connection.value != ConnectionState.Connected) {
                        _connection.value = ConnectionState.Connected
                    }
                } else {
                    webSocket.send("40$NAMESPACE,")
                }
            }

            '1' -> _connection.value = ConnectionState.Disconnected

            '2' -> {
                if (namespace.isNotEmpty() && namespace != NAMESPACE) return
                val payload = if (rest.isEmpty()) "[]" else rest
                runCatching {
                    val array = json.parseToJsonElement(payload) as? kotlinx.serialization.json.JsonArray
                        ?: return
                    val eventName = array.firstOrNull()
                        ?.let { it as? kotlinx.serialization.json.JsonPrimitive }
                        ?.content ?: return
                    val data = array.getOrNull(1) as? JsonObject
                    _events.tryEmit(SocketEvent(eventName, data))
                }.onFailure { Log.w(TAG, "bad event payload", it) }
            }

            '4' -> {
                // CONNECT_ERROR
                Log.w(TAG, "namespace connect error: ${rest.take(120)}")
                _connection.value = ConnectionState.Disconnected
            }

            else -> Unit
        }
    }

    private fun deliver(state: ConnectionState) {
        _events.tryEmit(SocketEvent(CONNECTION_EVENT, null))
    }

    sealed interface ConnectionState {
        data object Disconnected : ConnectionState
        data object Connecting : ConnectionState
        data object Connected : ConnectionState
    }

    data class SocketEvent(val name: String, val payload: JsonObject?)

    companion object {
        private const val TAG = "MetricsSocket"
        private const val WATCHDOG_MS = 90_000L
        private const val NAMESPACE = "/metrics"
        const val CONNECTION_EVENT = "connection:changed"

        fun create(): MetricsSocket {
            val client = OkHttpClient.Builder()
                .connectTimeout(15, TimeUnit.SECONDS)
                .readTimeout(0, TimeUnit.SECONDS)
                .pingInterval(0, TimeUnit.SECONDS)
                .retryOnConnectionFailure(true)
                .build()
            return MetricsSocket(client, ApiClient.DefaultJson)
        }
    }
}
