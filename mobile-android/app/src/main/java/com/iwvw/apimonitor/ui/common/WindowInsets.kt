package com.iwvw.apimonitor.ui.common

import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.systemBars
import androidx.compose.runtime.Composable

/**
 * 底栏页面专用的 Scaffold inset。
 *
 * MainScaffold 的外层 Scaffold 已为 NavigationBar 预留底部空间，
 * 内层页面若再用默认 contentWindowInsets，会把导航栏高度算第二次，
 * 表现为底栏上方多出一条空白。因此内层只保留顶部（状态栏）。
 */
@Composable
fun tabContentWindowInsets(): WindowInsets =
    WindowInsets.systemBars.only(WindowInsetsSides.Top)

/** 全屏页面（无底栏）用的 inset：上下都要，避免内容被状态栏/导航栏遮挡。 */
@Composable
fun fullScreenWindowInsets(): WindowInsets = WindowInsets.systemBars
