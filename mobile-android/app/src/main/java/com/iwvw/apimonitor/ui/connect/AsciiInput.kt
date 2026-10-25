package com.iwvw.apimonitor.ui.connect

import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.TextFieldValue

/**
 * 面板地址、用户名、密码、Token 都是 ASCII 语义。
 *
 * 中文输入法会把 `https://` 输成中文联想词。Compose 没有 API 能强制输入法切英文
 * （那是输入法的权力），所以这里双管齐下：
 * 1. `KeyboardType` 选输入法最倾向英文的变体——URL 用 [KeyboardType.Uri]，
 *    账号类用 [KeyboardType.Ascii]，密码用 [KeyboardType.Password]；
 * 2. 输入时兜底过滤非 ASCII 字符（见 [TextFieldValue.keepAscii]）。
 */
internal val AsciiUrlOptions = KeyboardOptions(
    capitalization = KeyboardCapitalization.None,
    autoCorrectEnabled = false,
    keyboardType = KeyboardType.Uri,
    imeAction = ImeAction.Next,
)

internal val AsciiTextOptions = KeyboardOptions(
    capitalization = KeyboardCapitalization.None,
    autoCorrectEnabled = false,
    keyboardType = KeyboardType.Ascii,
    imeAction = ImeAction.Next,
)

internal val AsciiPasswordOptions = KeyboardOptions(
    capitalization = KeyboardCapitalization.None,
    autoCorrectEnabled = false,
    keyboardType = KeyboardType.Password,
    imeAction = ImeAction.Next,
)

/** 只保留 ASCII 可见字符，挡掉输入法硬塞进来的中文。 */
internal fun String.keepAscii(): String = filter { it.code in 0x20..0x7E }

/**
 * 过滤非 ASCII 字符，同时**把光标位置按删除的字符数前移**。
 *
 * 直接用 `String.keepAscii()` 赋值会让光标跳到末尾：在中间插入字符时，
 * 输入法补的全角标点被过滤后，光标位置与实际文本对不上，表现为「打着打着光标乱跳」。
 */
internal fun TextFieldValue.keepAscii(): TextFieldValue {
    val filtered = text.keepAscii()
    if (filtered == text) return this

    // 统计光标之前被删掉的字符数，据此修正 selection。
    val cursor = selection.start.coerceIn(0, text.length)
    val removedBeforeCursor = text.take(cursor).count { it.code !in 0x20..0x7E }
    val newCursor = (cursor - removedBeforeCursor).coerceIn(0, filtered.length)
    return copy(text = filtered, selection = TextRange(newCursor))
}
