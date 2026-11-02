package com.iwvw.apimonitor.ui.connect

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.Visibility
import androidx.compose.material.icons.filled.VisibilityOff
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusDirection
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.text.TextRange
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.input.TextFieldValue
import androidx.compose.ui.text.input.VisualTransformation
import androidx.compose.ui.unit.dp
import com.iwvw.apimonitor.data.network.ApiClient
import com.iwvw.apimonitor.data.settings.Credentials
import com.iwvw.apimonitor.data.settings.normalizeUrlAscii
import kotlinx.coroutines.launch

@Composable
fun ConnectScreen(
    initialUrl: String,
    initialPassword: String,
    initialToken: String,
    onConnected: (Credentials, ApiClient) -> Unit,
    makeClient: () -> ApiClient,
) {
    val scope = rememberCoroutineScope()
    val apiClient = remember { makeClient() }
    val focusManager = LocalFocusManager.current

    var baseUrl by remember { mutableStateOf(TextFieldValue(initialUrl, TextRange(initialUrl.length))) }
    var password by remember { mutableStateOf(TextFieldValue(initialPassword, TextRange(initialPassword.length))) }
    var token by remember { mutableStateOf(TextFieldValue(initialToken, TextRange(initialToken.length))) }
    var totp by remember { mutableStateOf(TextFieldValue()) }
    var passwordVisible by remember { mutableStateOf(false) }
    var tokenVisible by remember { mutableStateOf(false) }
    var useTokenMode by remember { mutableStateOf(initialPassword.isBlank() && initialToken.isNotBlank()) }
    var needsTotp by remember { mutableStateOf(false) }
    var busy by remember { mutableStateOf(false) }
    var message by remember { mutableStateOf<String?>(null) }

    val normalizedUrl = remember(baseUrl.text) { normalizeUrlAscii(baseUrl.text) }
    val urlInvalid = baseUrl.text.isNotBlank() &&
        !normalizedUrl.startsWith("http://") &&
        !normalizedUrl.startsWith("https://")

    fun submit() {
        if (busy) return
        focusManager.clearFocus()

        val candidate = Credentials(
            baseUrl = baseUrl.text,
            password = if (useTokenMode) "" else password.text,
            apiToken = if (useTokenMode) token.text else "",
        )
        if (!candidate.canSubmit) {
            message = if (useTokenMode) "请填写面板地址与 API Token" else "请填写面板地址与密码"
            return
        }

        busy = true
        message = null
        scope.launch {
            if (useTokenMode) {
                when (val result = apiClient.verify(candidate)) {
                    is ApiClient.VerifyResult.Success -> {
                        busy = false
                        onConnected(candidate, apiClient)
                    }

                    is ApiClient.VerifyResult.Failure -> {
                        busy = false
                        message = "连接失败：${result.message}"
                    }
                }
                return@launch
            }

            when (val outcome = apiClient.login(candidate, totp.text)) {
                is ApiClient.LoginOutcome.Success -> {
                    when (val verified = apiClient.verify(candidate)) {
                        is ApiClient.VerifyResult.Success -> {
                            busy = false
                            onConnected(candidate, apiClient)
                        }

                        is ApiClient.VerifyResult.Failure -> {
                            busy = false
                            message = "登录成功但校验失败：${verified.message}"
                        }
                    }
                }

                is ApiClient.LoginOutcome.NeedsTwoFactor -> {
                    busy = false
                    needsTotp = true
                    message = outcome.message
                }

                is ApiClient.LoginOutcome.Failure -> {
                    busy = false
                    message = outcome.message
                }
            }
        }
    }

    Scaffold { innerPadding ->
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(innerPadding)
                .verticalScroll(rememberScrollState())
                .imePadding()
                .padding(horizontal = 24.dp, vertical = 24.dp),
        ) {
            Spacer(Modifier.height(64.dp))
            Text(
                text = "API Monitor",
                style = MaterialTheme.typography.displaySmall,
                color = MaterialTheme.colorScheme.primary,
                fontWeight = FontWeight.Medium,
            )
            Spacer(Modifier.height(8.dp))
            Text(
                text = if (useTokenMode) "使用 API Token 连接" else "使用面板账号登录",
                style = MaterialTheme.typography.bodyLarge,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(32.dp))

            OutlinedTextField(
                value = baseUrl,
                onValueChange = {
                    baseUrl = it.keepAscii()
                    message = null
                },
                label = { Text("面板地址") },
                placeholder = { Text("https://panel.example.com") },
                singleLine = true,
                enabled = !busy,
                isError = urlInvalid,
                supportingText = if (urlInvalid) {
                    { Text("需要以 http:// 或 https:// 开头") }
                } else {
                    null
                },
                keyboardOptions = AsciiUrlOptions,
                keyboardActions = KeyboardActions(
                    onNext = { focusManager.moveFocus(FocusDirection.Down) },
                ),
                modifier = Modifier.fillMaxWidth(),
            )
            Spacer(Modifier.height(12.dp))

            if (!useTokenMode) {
                OutlinedTextField(
                    value = password,
                    onValueChange = {
                        password = it.keepAscii()
                        message = null
                    },
                    label = { Text("密码") },
                    placeholder = { Text("面板管理密码") },
                    singleLine = true,
                    enabled = !busy,
                    visualTransformation = if (passwordVisible) {
                        VisualTransformation.None
                    } else {
                        PasswordVisualTransformation()
                    },
                    trailingIcon = {
                        IconButton(onClick = { passwordVisible = !passwordVisible }) {
                            Icon(
                                imageVector = if (passwordVisible) Icons.Filled.VisibilityOff else Icons.Filled.Visibility,
                                contentDescription = if (passwordVisible) "隐藏密码" else "显示密码",
                            )
                        }
                    },
                    keyboardOptions = if (needsTotp) {
                        AsciiPasswordOptions.copy(imeAction = ImeAction.Next)
                    } else {
                        AsciiPasswordOptions.copy(imeAction = ImeAction.Done)
                    },
                    keyboardActions = KeyboardActions(
                        onNext = { focusManager.moveFocus(FocusDirection.Down) },
                        onDone = { submit() },
                    ),
                    modifier = Modifier.fillMaxWidth(),
                )

                if (needsTotp) {
                    Spacer(Modifier.height(12.dp))
                    OutlinedTextField(
                        value = totp,
                        onValueChange = { input ->
                            val digits = input.text.filter { c -> c.isDigit() }.take(6)
                            totp = TextFieldValue(digits, TextRange(digits.length))
                        },
                        label = { Text("两步验证码") },
                        placeholder = { Text("6 位数字") },
                        singleLine = true,
                        enabled = !busy,
                        keyboardOptions = KeyboardOptions(
                            keyboardType = KeyboardType.NumberPassword,
                            imeAction = ImeAction.Done,
                        ),
                        keyboardActions = KeyboardActions(onDone = { submit() }),
                        modifier = Modifier.fillMaxWidth(),
                    )
                }
            } else {
                OutlinedTextField(
                    value = token,
                    onValueChange = {
                        token = it.keepAscii()
                        message = null
                    },
                    label = { Text("API Token") },
                    placeholder = { Text("ak_...") },
                    singleLine = true,
                    enabled = !busy,
                    supportingText = { Text("面板「设置 → API 密钥」创建，需要 api:read 权限") },
                    visualTransformation = if (tokenVisible) {
                        VisualTransformation.None
                    } else {
                        PasswordVisualTransformation()
                    },
                    trailingIcon = {
                        IconButton(onClick = { tokenVisible = !tokenVisible }) {
                            Icon(
                                imageVector = if (tokenVisible) Icons.Filled.VisibilityOff else Icons.Filled.Visibility,
                                contentDescription = if (tokenVisible) "隐藏 Token" else "显示 Token",
                            )
                        }
                    },
                    keyboardOptions = AsciiPasswordOptions.copy(imeAction = ImeAction.Done),
                    keyboardActions = KeyboardActions(onDone = { submit() }),
                    modifier = Modifier.fillMaxWidth(),
                )
            }

            if (message != null) {
                Spacer(Modifier.height(12.dp))
                Text(
                    text = message.orEmpty(),
                    style = MaterialTheme.typography.bodyMedium,
                    color = MaterialTheme.colorScheme.error,
                )
            }

            Spacer(Modifier.height(24.dp))

            Button(
                onClick = { submit() },
                enabled = !busy,
                modifier = Modifier
                    .fillMaxWidth()
                    .height(52.dp),
            ) {
                if (busy) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(20.dp),
                        strokeWidth = 2.dp,
                        color = MaterialTheme.colorScheme.onPrimary,
                    )
                } else {
                    Text(
                        text = if (useTokenMode) "连接并验证" else "登录",
                        style = MaterialTheme.typography.titleSmall,
                    )
                }
            }

            Spacer(Modifier.height(4.dp))
            TextButton(
                onClick = {
                    useTokenMode = !useTokenMode
                    message = null
                    needsTotp = false
                },
                enabled = !busy,
                modifier = Modifier.align(Alignment.CenterHorizontally),
            ) {
                Text(if (useTokenMode) "改用账号密码登录" else "改用 API Token 连接")
            }

            Spacer(Modifier.height(8.dp))
            Text(
                text = "账号密码用于获取实时推送；凭据经 Android Keystore 加密后仅保存在本机。",
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                modifier = Modifier.align(Alignment.CenterHorizontally),
            )
        }
    }
}
