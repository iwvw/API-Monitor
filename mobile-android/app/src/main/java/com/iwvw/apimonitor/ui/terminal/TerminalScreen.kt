package com.iwvw.apimonitor.ui.terminal

import androidx.compose.foundation.background
import androidx.compose.foundation.gestures.detectTapGestures
import androidx.compose.ui.input.pointer.pointerInput
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.collectLatest
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.navigationBarsPadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.systemBars
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.SnackbarHost
import androidx.compose.material3.SnackbarHostState
import androidx.compose.material3.Text
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.TopAppBarDefaults
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.input.nestedscroll.nestedScroll
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import com.iwvw.apimonitor.data.network.SessionCookieJar
import com.iwvw.apimonitor.data.network.TerminalSocket
import com.iwvw.apimonitor.data.settings.Credentials
import com.iwvw.apimonitor.ui.common.rememberHaptics
import org.connectbot.terminal.Terminal
import org.connectbot.terminal.TerminalEmulator
import org.connectbot.terminal.TerminalEmulatorFactory

/**
 * 服务器终端页。
 *
 * 数据通路：
 * ```
 * 键盘 → termlib onKeyboardInput(ByteArray) → TerminalSocket.sendInput → /ws/ssh
 * /ws/ssh → {"type":"data"} → TerminalSocket.output → emulator.writeInput → 屏幕
 * ```
 *
 * 布局要点（这几个都是踩过的坑）：
 * 1. **IME 由底部栏自己处理**：MD3 `Scaffold` 只把 `contentWindowInsets` 算进 content 的
 *    `innerPadding`，**不会**给 `bottomBar` 加 IME 间距。所以快捷键栏必须自己套
 *    `imePadding()`，否则键盘弹出时会把它压住。
 * 2. **不要传 `forcedSize`**：传了之后 termlib 会在 `[minFontSize, maxFontSize]` 里
 *    二分搜索字号，而搜索用的「可用宽高」正是我们传进去的容器尺寸。容器尺寸又由
 *    字符宽推算 → 字号依赖容器、容器依赖字号，形成反馈环。键盘弹出/收起时容器
 *    高度逐帧变化，这个环会持续震荡，表现为终端抖动。
 *    不传则字号恒为 `initialFontSize`，列数由 termlib 自己按 `floor` 算，
 *    绘制区域直接等于容器尺寸——既没有右侧空白，也不会画那圈硬编码的绿色调试边框。
 * 3. **不要传 `resizeSuspended`**：置 true 时 termlib 用 `retainedWidth` 当可用宽度，
 *    首次布局它还是 0，列数会被压成 1 列并发给远端 shell，导致输出按单字符折行。
 * 4. **resize 要去抖再发给服务端**：IME 动画期间 `onResize` 每帧触发，
 *    逐帧转发会让远端 shell 连续重排，看起来同样是抖动。
 */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TerminalScreen(
    serverId: String,
    serverName: String,
    credentials: Credentials,
    cookieJar: SessionCookieJar,
    onBack: () -> Unit,
) {
    val haptics = rememberHaptics()
    val snackbar = remember { SnackbarHostState() }
    val scrollBehavior = TopAppBarDefaults.pinnedScrollBehavior()
    val context = LocalContext.current

    val backgroundColor = MaterialTheme.colorScheme.surfaceContainerLowest
    val foregroundColor = MaterialTheme.colorScheme.onSurface

    val socket = remember(serverId) { TerminalSocket(credentials, cookieJar) }
    val connectionState by socket.state.collectAsState()

    var retryToken by remember { mutableStateOf(0) }

    // 尺寸变更去抖：IME 动画期间 onResize 每帧触发，逐帧发给服务端会让远端
    // shell 连续重排。collectLatest + delay 实现「静默一段时间后才发最后一次」。
    val resizeRequests = remember(serverId) {
        kotlinx.coroutines.flow.MutableSharedFlow<Pair<Int, Int>>(extraBufferCapacity = 16)
    }
    LaunchedEffect(resizeRequests) {
        var last: Pair<Int, Int>? = null
        resizeRequests.collectLatest { (cols, rows) ->
            delay(RESIZE_DEBOUNCE_MS)
            if (last != cols to rows) {
                last = cols to rows
                socket.sendResize(cols, rows)
            }
        }
    }

    val emulator: TerminalEmulator = remember(serverId, retryToken) {
        TerminalEmulatorFactory.create(
            initialRows = 24,
            initialCols = 80,
            defaultForeground = foregroundColor,
            defaultBackground = backgroundColor,
            onKeyboardInput = { bytes -> socket.sendInput(bytes) },
            onResize = { dims -> resizeRequests.tryEmit(dims.columns to dims.rows) },
            onClipboardCopy = { text ->
                val clipboard = context.getSystemService(android.content.ClipboardManager::class.java)
                clipboard?.setPrimaryClip(android.content.ClipData.newPlainText("terminal", text))
            },
            autoDetectUrls = true,
            boldAsBright = true,
        )
    }

    LaunchedEffect(serverId, retryToken) {
        socket.connect(serverId)
    }

    LaunchedEffect(emulator) {
        socket.output.collect { bytes ->
            emulator.writeInput(bytes, 0, bytes.size)
        }
    }

    LaunchedEffect(socket) {
        socket.message.collect { text -> snackbar.showSnackbar(text) }
    }

    DisposableEffect(Unit) {
        onDispose {
            socket.release()
            emulator.close()
        }
    }

    // 终端是纯等宽字符画布：必须用真正的等宽字体。
    //
    // 不能用 Typeface.MONOSPACE —— 它映射到厂商字体（一加/ColorOS 下是厂商自选字体），
    // 字宽比例与标准等宽不一致，termlib 用 measureText("M") 推算列宽时就会算偏，
    // 表现为字符错位。这里内置 JetBrains Mono 保证 ASCII 严格等宽，
    // 中文由系统 fallback 接管（每个汉字固定占 2 列，符合终端惯例）。
    val monoTypeface = remember {
        runCatching {
            android.graphics.Typeface.createFromAsset(context.assets, "font/jetbrains_mono.ttf")
        }.getOrNull() ?: android.graphics.Typeface.MONOSPACE
    }

    Scaffold(
        // 只处理状态栏与导航栏；IME 高度交给底部的 imePadding 处理。
        // 原因：MD3 Scaffold 只把 contentWindowInsets 算进 content 的 innerPadding，
        // 不会自动给 bottomBar 加 IME 间距，导致快捷键栏被键盘压住。
        contentWindowInsets = WindowInsets.systemBars,
        modifier = Modifier.nestedScroll(scrollBehavior.nestedScrollConnection),
        snackbarHost = { SnackbarHost(snackbar) },
        bottomBar = {
            // 快捷键栏放 bottomBar：Scaffold 会让 Snackbar 落在它上方。
            // imePadding 让整条栏在键盘弹出时被推高到键盘之上。
            TerminalKeyBar(
                enabled = connectionState == TerminalSocket.State.CONNECTED,
                onSend = { text ->
                    haptics.tap()
                    socket.sendInput(text.toByteArray(Charsets.UTF_8))
                },
                modifier = Modifier
                    // imePadding：键盘弹出时把整条栏推到键盘之上。
                    // navigationBarsPadding：键盘收起时避开系统手势条。
                    // 两者叠加，Scaffold 的 bottomBar 本身不消费 IME inset，必须自己处理。
                    .imePadding()
                    .navigationBarsPadding(),
            )
        },
        topBar = {
            TopAppBar(
                title = {
                    // 连接成功后不显示副标题，避免顶部多一行「已连接」占地方；
                    // 只在连接中 / 断开 / 失败时给出状态说明。
                    Column {
                        Text(text = serverName.ifBlank { "终端" }, maxLines = 1)
                        val statusLabel = when (connectionState) {
                            TerminalSocket.State.CONNECTING -> "连接中…"
                            TerminalSocket.State.CLOSED -> "已断开"
                            TerminalSocket.State.FAILED -> "连接失败"
                            TerminalSocket.State.CONNECTED -> null
                        }
                        if (statusLabel != null) {
                            Text(
                                text = statusLabel,
                                style = MaterialTheme.typography.labelMedium,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                        }
                    }
                },
                navigationIcon = {
                    IconButton(onClick = {
                        haptics.tap()
                        onBack()
                    }) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回")
                    }
                },
                actions = {
                    IconButton(onClick = {
                        haptics.tap()
                        retryToken++
                    }) {
                        Icon(Icons.Filled.Refresh, contentDescription = "重连")
                    }
                },
                scrollBehavior = scrollBehavior,
            )
        },
    ) { innerPadding ->
        // innerPadding 只含状态栏/导航栏（contentWindowInsets = systemBars）。
        // IME 高度由 bottomBar 的 imePadding 处理，content 不再重复叠加。
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding),
        ) {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .weight(1f)
                    .padding(start = 12.dp, end = 12.dp, top = 8.dp, bottom = 8.dp)
                    .clip(MaterialTheme.shapes.medium)
                    .background(backgroundColor),
            ) {
                // 不传 forcedSize：termlib 自己按 floor(可用宽 / 字符宽) 算行列，
                // 绘制区域直接等于容器尺寸，所以既没有右侧空白，也不会画那圈
                // 硬编码的绿色调试边框（那个边框只在 forcedSize != null 时出现）。
                // 字号恒为 initialFontSize，不再随容器高度变化，抖动随之消失。
                Terminal(
                    terminalEmulator = emulator,
                    modifier = Modifier.fillMaxSize(),
                    typeface = monoTypeface,
                    initialFontSize = BASE_FONT_SIZE,
                    backgroundColor = backgroundColor,
                    foregroundColor = foregroundColor,
                    keyboardEnabled = true,
                    showSoftKeyboard = true,
                )

                when (connectionState) {
                    TerminalSocket.State.CONNECTING -> TerminalOverlay(
                        title = "正在连接…",
                        showProgress = true,
                    )

                    TerminalSocket.State.FAILED -> TerminalOverlay(
                        title = "连接失败",
                        detail = "主机 Agent 离线，或面板未配置 SSH 凭据。",
                        actionLabel = "重试",
                        onAction = {
                            haptics.tap()
                            retryToken++
                        },
                    )

                    else -> Unit
                }
            }
        }
    }
}

/** 连接中 / 失败时的遮罩。 */
@Composable
private fun TerminalOverlay(
    title: String,
    detail: String? = null,
    showProgress: Boolean = false,
    actionLabel: String? = null,
    onAction: (() -> Unit)? = null,
) {
    Box(
        modifier = Modifier
            .fillMaxSize()
            .padding(24.dp),
        contentAlignment = Alignment.Center,
    ) {
        Column(
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            if (showProgress) {
                CircularProgressIndicator()
                Spacer(Modifier.height(16.dp))
            }
            Text(
                text = title,
                style = MaterialTheme.typography.titleMedium,
                fontWeight = FontWeight.Medium,
            )
            if (!detail.isNullOrBlank()) {
                Spacer(Modifier.height(8.dp))
                Text(
                    text = detail,
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
            if (actionLabel != null && onAction != null) {
                Spacer(Modifier.height(16.dp))
                Button(onClick = onAction) { Text(actionLabel) }
            }
        }
    }
}

/**
 * 软键盘上方的一排常用键。
 *
 * 手机软键盘没有 Esc / Ctrl / 方向键，没有这一排几乎无法用 vim / less。
 * 这一栏在键盘弹出时必须始终可见——它自己套了 imePadding，
 * 会被推到键盘之上，不会被遮住。
 */
@Composable
private fun TerminalKeyBar(
    enabled: Boolean,
    onSend: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxWidth()
            .background(MaterialTheme.colorScheme.surfaceContainer),
    ) {
        HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .padding(horizontal = 12.dp, vertical = 8.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            TerminalKey("Esc", enabled, onSend, Modifier.weight(1f)) { "\u001b" }
            TerminalKey("Ctrl-C", enabled, onSend, Modifier.weight(1f)) { "\u0003" }
            TerminalKey("Tab", enabled, onSend, Modifier.weight(1f)) { "\t" }
            TerminalKey("↑", enabled, onSend, Modifier.weight(1f)) { "\u001b[A" }
            TerminalKey("↓", enabled, onSend, Modifier.weight(1f)) { "\u001b[B" }
            // 长按连续发方向键：终端里移动光标常常要连按几十次，
            // 一下一下点太慢。这里长按后每 120ms 重复一次。
            RepeatKey(
                label = "←",
                enabled = enabled,
                sequence = "\u001b[D",
                onSend = onSend,
                modifier = Modifier.weight(1f),
            )
            RepeatKey(
                label = "→",
                enabled = enabled,
                sequence = "\u001b[C",
                onSend = onSend,
                modifier = Modifier.weight(1f),
            )
        }
    }
}

/**
 * 支持长按连续触发的按键。
 *
 * 首次按下立即发一次，按住超过 400ms 后按 [REPEAT_INTERVAL_MS] 连续发送。
 * 用于方向键这类需要反复触发的场景。
 */
@Composable
private fun RepeatKey(
    label: String,
    enabled: Boolean,
    sequence: String,
    onSend: (String) -> Unit,
    modifier: Modifier = Modifier,
) {
    var repeatJob by remember { mutableStateOf<kotlinx.coroutines.Job?>(null) }
    val scope = rememberCoroutineScope()

    DisposableEffect(Unit) {
        onDispose { repeatJob?.cancel() }
    }

    OutlinedButton(
        onClick = { onSend(sequence) },
        enabled = enabled,
        modifier = modifier
            .height(40.dp)
            .pointerInput(enabled, sequence) {
                if (!enabled) return@pointerInput
                detectTapGestures(
                    onPress = {
                        repeatJob?.cancel()
                        repeatJob = scope.launch {
                            delay(LONG_PRESS_DELAY_MS)
                            while (isActive) {
                                onSend(sequence)
                                delay(REPEAT_INTERVAL_MS)
                            }
                        }
                        tryAwaitRelease()
                        repeatJob?.cancel()
                        repeatJob = null
                    },
                )
            },
        contentPadding = androidx.compose.foundation.layout.PaddingValues(0.dp),
        shape = MaterialTheme.shapes.small,
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.labelMedium,
            maxLines = 1,
        )
    }
}

private const val LONG_PRESS_DELAY_MS = 400L
private const val REPEAT_INTERVAL_MS = 120L

@Composable
private fun TerminalKey(
    label: String,
    enabled: Boolean,
    onSend: (String) -> Unit,
    modifier: Modifier = Modifier,
    sequence: () -> String,
) {
    OutlinedButton(
        onClick = { onSend(sequence()) },
        enabled = enabled,
        modifier = modifier.height(40.dp),
        contentPadding = androidx.compose.foundation.layout.PaddingValues(0.dp),
        shape = MaterialTheme.shapes.small,
    ) {
        Text(
            text = label,
            style = MaterialTheme.typography.labelMedium,
            maxLines = 1,
        )
    }
}

private val BASE_FONT_SIZE = 11.sp

/** 尺寸变更去抖时长：等 IME 动画结束再一次性通知服务端，避免远端 shell 连续重排。 */
private const val RESIZE_DEBOUNCE_MS = 120L

