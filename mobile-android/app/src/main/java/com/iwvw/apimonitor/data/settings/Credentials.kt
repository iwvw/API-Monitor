package com.iwvw.apimonitor.data.settings

/**
 * 登录凭据。密码只保存在内存与 Keystore 加密存储中，不落明文盘。
 *
 * 面板登录只需要密码（后端 `/api/auth/login` 只接受 password），
 * 因此这里不再有用户名：密码非空即走会话登录，否则走 API Token。
 *
 * - password 非空：用面板密码登录，拿 sid 会话，可获得实时推送
 * - apiToken 非空：用 API Token 鉴权（只读）
 */
data class Credentials(
    val baseUrl: String = "",
    val password: String = "",
    val apiToken: String = "",
    val totpCode: String = "",
) {
    val normalizedBaseUrl: String
        get() = normalizeUrlAscii(baseUrl)

    val usesSessionLogin: Boolean
        get() = password.isNotBlank()

    val usesApiToken: Boolean
        get() = apiToken.isNotBlank()

    val isConfigured: Boolean
        get() = normalizedBaseUrl.isNotBlank() && (usesSessionLogin || usesApiToken)

    /** 能在连接页面直接提交（2FA 由登录过程按需追问）。 */
    val canSubmit: Boolean
        get() = normalizedBaseUrl.isNotBlank() && (password.isNotBlank() || apiToken.isNotBlank())
}

/**
 * 中文输入法会把 `/`、`:`、`.` 输成全角字符，直接拼进 URL 会握手失败。
 * 这里统一转半角并去掉首尾与内部空白。
 */
fun normalizeUrlAscii(raw: String): String {
    val halfWidth = buildString(raw.length) {
        for (ch in raw) {
            when (ch) {
                '\u3000' -> append(' ')
                else -> if (ch.code in 0xFF01..0xFF5E) {
                    append((ch.code - 0xFEE0).toChar())
                } else {
                    append(ch)
                }
            }
        }
    }
    return halfWidth.filterNot { it.isWhitespace() }.trimEnd('/')
}
