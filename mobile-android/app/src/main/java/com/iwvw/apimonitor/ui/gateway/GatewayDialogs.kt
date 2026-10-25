package com.iwvw.apimonitor.ui.gateway

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.CalendarMonth
import androidx.compose.material.icons.filled.Clear
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.DatePicker
import androidx.compose.material3.DatePickerDialog
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.rememberDatePickerState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.unit.dp
import com.iwvw.apimonitor.data.model.GatewayKey
import com.iwvw.apimonitor.ui.common.SurfaceClickable
import com.iwvw.apimonitor.ui.common.rememberHaptics
import java.time.Instant
import java.time.LocalDate
import java.time.ZoneOffset

/** 网关密钥相关的全部弹窗，集中在这里，避免主页面被对话框代码淹没。 */
@Composable
internal fun GatewayDialogs(
    editingKey: GatewayKey?,
    deletingKey: GatewayKey?,
    rotatingKey: GatewayKey?,
    rotatedKey: String,
    onDismissEdit: () -> Unit,
    onSaveKey: (GatewayKey, String, String?) -> Unit,
    onDismissDelete: () -> Unit,
    onConfirmDelete: (GatewayKey) -> Unit,
    onDismissRotate: () -> Unit,
    onConfirmRotate: (GatewayKey) -> Unit,
    onDismissNewKey: () -> Unit,
) {
    editingKey?.let { key ->
        KeyEditDialog(
            key = key,
            onDismiss = onDismissEdit,
            onSave = { name, expiresAt -> onSaveKey(key, name, expiresAt) },
        )
    }

    deletingKey?.let { key ->
        AlertDialog(
            onDismissRequest = onDismissDelete,
            title = { Text("删除密钥") },
            text = {
                Text("确定删除「${key.name}」？使用该 Key 的客户端会立即无法访问网关，此操作不可撤销。")
            },
            confirmButton = {
                TextButton(onClick = { onConfirmDelete(key) }) {
                    Text("删除", color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(onClick = onDismissDelete) { Text("取消") }
            },
        )
    }

    rotatingKey?.let { key ->
        AlertDialog(
            onDismissRequest = onDismissRotate,
            title = { Text("轮换密钥") },
            text = {
                Text(
                    "轮换后「${key.name}」的旧 Key 立即失效，请求计数清零。" +
                        "正在使用该 Key 的客户端需要更新配置。",
                )
            },
            confirmButton = {
                TextButton(onClick = { onConfirmRotate(key) }) {
                    Text("确认轮换", color = MaterialTheme.colorScheme.error)
                }
            },
            dismissButton = {
                TextButton(onClick = onDismissRotate) { Text("取消") }
            },
        )
    }

    if (rotatedKey.isNotBlank()) {
        NewKeyDialog(newKey = rotatedKey, onDismiss = onDismissNewKey)
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun KeyEditDialog(
    key: GatewayKey,
    onDismiss: () -> Unit,
    onSave: (String, String?) -> Unit,
) {
    var name by remember { mutableStateOf(key.name) }
    var expiresDate by remember { mutableStateOf(parseIsoDate(key.expiresAt)) }
    var showPicker by remember { mutableStateOf(false) }

    val nameError = name.isBlank()

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("编辑密钥") },
        text = {
            Column(modifier = Modifier.fillMaxWidth()) {
                OutlinedTextField(
                    value = name,
                    onValueChange = { name = it },
                    label = { Text("名称") },
                    singleLine = true,
                    isError = nameError,
                    supportingText = if (nameError) {
                        { Text("名称不能为空") }
                    } else {
                        null
                    },
                    modifier = Modifier.fillMaxWidth(),
                )

                Spacer(Modifier.height(12.dp))

                // 过期时间用官方 DatePicker，避免手输 ISO 字符串。
                SurfaceClickable(
                    onClick = { showPicker = true },
                    modifier = Modifier.fillMaxWidth(),
                ) {
                    Row(
                        modifier = Modifier.padding(vertical = 12.dp),
                        verticalAlignment = androidx.compose.ui.Alignment.CenterVertically,
                    ) {
                        Icon(
                            imageVector = Icons.Filled.CalendarMonth,
                            contentDescription = null,
                            tint = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                        Spacer(Modifier.width(12.dp))
                        Column(modifier = Modifier.weight(1f)) {
                            Text(
                                text = "过期时间",
                                style = MaterialTheme.typography.labelSmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant,
                            )
                            Text(
                                text = expiresDate?.toString() ?: "永不过期",
                                style = MaterialTheme.typography.bodyLarge,
                            )
                        }
                        if (expiresDate != null) {
                            IconButton(onClick = { expiresDate = null }) {
                                Icon(Icons.Filled.Clear, contentDescription = "清除过期时间")
                            }
                        }
                    }
                }
            }
        },
        confirmButton = {
            TextButton(
                enabled = !nameError,
                onClick = {
                    onSave(name.trim(), expiresDate?.toIsoEndOfDay())
                },
            ) {
                Text("保存")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text("取消") }
        },
    )

    if (showPicker) {
        val pickerState = rememberDatePickerState(
            initialSelectedDateMillis = expiresDate
                ?.atStartOfDay(ZoneOffset.UTC)
                ?.toInstant()
                ?.toEpochMilli(),
        )
        DatePickerDialog(
            onDismissRequest = { showPicker = false },
            confirmButton = {
                TextButton(
                    onClick = {
                        pickerState.selectedDateMillis?.let { millis ->
                            expiresDate = Instant.ofEpochMilli(millis)
                                .atZone(ZoneOffset.UTC)
                                .toLocalDate()
                        }
                        showPicker = false
                    },
                ) {
                    Text("确定")
                }
            },
            dismissButton = {
                TextButton(onClick = { showPicker = false }) { Text("取消") }
            },
        ) {
            DatePicker(state = pickerState)
        }
    }
}

@Composable
private fun NewKeyDialog(newKey: String, onDismiss: () -> Unit) {
    val clipboard = LocalClipboardManager.current
    val haptics = rememberHaptics()

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("新密钥已生成") },
        text = {
            Column {
                Text(
                    text = "这是唯一一次显示完整 Key，请立刻复制保存。",
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.error,
                )
                Spacer(Modifier.height(10.dp))
                Text(text = newKey, style = MaterialTheme.typography.bodySmall)
            }
        },
        confirmButton = {
            TextButton(
                onClick = {
                    clipboard.setText(AnnotatedString(newKey))
                    haptics.confirm()
                    onDismiss()
                },
            ) {
                Text("复制并关闭")
            }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text("关闭") }
        },
    )
}

/** 解析后端返回的 ISO 8601 时间，只取日期部分。 */
internal fun parseIsoDate(raw: String?): LocalDate? {
    val value = raw?.takeIf { it.isNotBlank() } ?: return null
    return runCatching { Instant.parse(value).atZone(ZoneOffset.UTC).toLocalDate() }
        .recoverCatching { LocalDate.parse(value.take(10)) }
        .getOrNull()
}

/** 把日期转成后端要的 ISO 8601（当天 23:59:59 UTC）。 */
private fun LocalDate.toIsoEndOfDay(): String =
    atTime(23, 59, 59).toInstant(ZoneOffset.UTC).toString()
