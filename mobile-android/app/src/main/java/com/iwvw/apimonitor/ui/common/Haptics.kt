package com.iwvw.apimonitor.ui.common

import android.os.Build
import android.view.HapticFeedbackConstants
import android.view.View
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalView

/**
 * MD3 规范里的触觉反馈。按语义分级，不要滥用：
 * - [tap]        普通点击 / 切换（轻）
 * - [confirm]    操作成功（中）
 * - [reject]     操作失败 / 校验不通过（重）
 * - [danger]     危险操作前的确认提示（长震）
 * - [segment]    分段控件、Tab 切换（最轻）
 *
 * `CONFIRM` / `REJECT` 是 API 30 才有的常量。minSdk 是 26，在 API 26–29 上
 * 传这两个值会被系统当成未知常量静默忽略（没有震动反馈，也不会崩），
 * 所以低版本回落到语义最接近的老常量。
 */
class Haptics(private val view: View) {

    fun tap() {
        view.performHapticFeedback(HapticFeedbackConstants.CLOCK_TICK)
    }

    fun segment() {
        view.performHapticFeedback(HapticFeedbackConstants.CONTEXT_CLICK)
    }

    fun confirm() {
        view.performHapticFeedback(
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                HapticFeedbackConstants.CONFIRM
            } else {
                HapticFeedbackConstants.VIRTUAL_KEY
            },
        )
    }

    fun reject() {
        view.performHapticFeedback(
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
                HapticFeedbackConstants.REJECT
            } else {
                HapticFeedbackConstants.LONG_PRESS
            },
        )
    }

    fun danger() {
        view.performHapticFeedback(HapticFeedbackConstants.LONG_PRESS)
    }

    fun toggle(on: Boolean) {
        if (on) confirm() else tap()
    }
}

@Composable
fun rememberHaptics(): Haptics {
    val view = LocalView.current
    return remember(view) { Haptics(view) }
}
