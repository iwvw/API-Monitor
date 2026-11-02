package com.iwvw.apimonitor.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.automirrored.filled.Logout
import androidx.compose.material.icons.filled.DarkMode
import androidx.compose.material.icons.filled.Info
import androidx.compose.material.icons.filled.Person
import androidx.compose.material.icons.filled.Router
import androidx.compose.material3.OutlinedCard
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.ListItem
import androidx.compose.material3.ListItemDefaults
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import com.iwvw.apimonitor.ui.common.fullScreenWindowInsets
import com.iwvw.apimonitor.BuildConfig
import com.iwvw.apimonitor.data.settings.Credentials
import com.iwvw.apimonitor.ui.common.SurfaceClickable
import com.iwvw.apimonitor.ui.common.rememberHaptics

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(
    credentials: Credentials,
    darkTheme: Boolean,
    onToggleDarkTheme: (Boolean) -> Unit,
    onLogout: () -> Unit,
    onBack: () -> Unit,
) {
    val haptics = rememberHaptics()
    var confirmLogout by remember { mutableStateOf(false) }

    Scaffold(

        contentWindowInsets = fullScreenWindowInsets(),
        topBar = {
            TopAppBar(
                title = { Text("设置") },
                navigationIcon = {
                    IconButton(onClick = onBack) {
                        Icon(Icons.AutoMirrored.Filled.ArrowBack, contentDescription = "返回")
                    }
                },
            )
        },
    ) { innerPadding ->
        LazyColumn(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding),
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            item {
                OutlinedCard(modifier = Modifier.fillMaxWidth()) {
                    ListItem(
                        leadingContent = { Icon(Icons.Filled.Router, contentDescription = null) },
                        headlineContent = { Text("面板地址") },
                        supportingContent = { Text(credentials.normalizedBaseUrl) },
                    )
                    HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                    ListItem(
                        leadingContent = { Icon(Icons.Filled.Person, contentDescription = null) },
                        headlineContent = { Text("登录方式") },
                        supportingContent = {
                            Text(
                                if (credentials.usesApiToken) "API Token 模式" else "密码登录",
                            )
                        },
                    )
                }
            }

            item {
                OutlinedCard(modifier = Modifier.fillMaxWidth()) {
                    ListItem(
                        leadingContent = { Icon(Icons.Filled.DarkMode, contentDescription = null) },
                        headlineContent = { Text("深色主题") },
                        supportingContent = { Text(if (darkTheme) "已开启" else "跟随系统") },
                        trailingContent = {
                            Switch(
                                checked = darkTheme,
                                onCheckedChange = {
                                    haptics.toggle(it)
                                    onToggleDarkTheme(it)
                                },
                            )
                        },
                    )
                }
            }

            item {
                OutlinedCard(modifier = Modifier.fillMaxWidth()) {
                    ListItem(
                        leadingContent = { Icon(Icons.Filled.Info, contentDescription = null) },
                        headlineContent = { Text("版本") },
                        supportingContent = { Text("${BuildConfig.VERSION_NAME} (${BuildConfig.VERSION_CODE})") },
                    )
                }
            }

            item {
                OutlinedCard(modifier = Modifier.fillMaxWidth()) {
                    SurfaceClickable(onClick = {
                        haptics.danger()
                        confirmLogout = true
                    }) {
                        ListItem(
                            leadingContent = {
                                Icon(
                                    Icons.AutoMirrored.Filled.Logout,
                                    contentDescription = null,
                                    tint = MaterialTheme.colorScheme.error,
                                )
                            },
                            headlineContent = {
                                Text("退出登录", color = MaterialTheme.colorScheme.error)
                            },
                            supportingContent = { Text("清除本机保存的凭据与会话") },
                        )
                    }
                }
            }
        }
    }

    if (confirmLogout) {
        AlertDialog(
            onDismissRequest = { confirmLogout = false },
            title = { Text("退出登录？") },
            text = { Text("将清除本机保存的面板地址、账号与加密凭据，下次需要重新登录。") },
            confirmButton = {
                TextButton(
                    onClick = {
                        confirmLogout = false
                        haptics.confirm()
                        onLogout()
                    },
                ) {
                    Text("退出", color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmLogout = false }) { Text("取消") }
            },
        )
    }
}
