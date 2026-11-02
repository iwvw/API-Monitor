package com.iwvw.apimonitor.data.settings

import android.content.Context
import android.content.SharedPreferences

class SettingsStore(context: Context) {
    private val prefs: SharedPreferences =
        context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
    private val cipher = KeystoreCipher()

    fun read(): Credentials {
        val token = cipher.decrypt(prefs.getString(KEY_API_TOKEN, "").orEmpty()).orEmpty()
        val password = cipher.decrypt(prefs.getString(KEY_PASSWORD, "").orEmpty()).orEmpty()
        return Credentials(
            baseUrl = prefs.getString(KEY_BASE_URL, "").orEmpty(),
            password = password,
            apiToken = token,
        )
    }

    fun write(credentials: Credentials) {
        prefs.edit()
            .putString(KEY_BASE_URL, credentials.normalizedBaseUrl)
            .putString(KEY_PASSWORD, cipher.encrypt(credentials.password).orEmpty())
            .putString(KEY_API_TOKEN, cipher.encrypt(credentials.apiToken.trim()).orEmpty())
            .apply()
    }

    fun readSessionCookie(): String = cipher.decrypt(prefs.getString(KEY_SESSION, "").orEmpty()).orEmpty()

    fun writeSessionCookie(sid: String) {
        prefs.edit().putString(KEY_SESSION, cipher.encrypt(sid).orEmpty()).apply()
    }

    fun clearSession() {
        // commit 而不是 apply：退出登录会立刻导航并可能切进程，
        // 异步落盘一旦丢失，重启后旧会话又被恢复，表现为「退不出去」。
        prefs.edit().remove(KEY_SESSION).commit()
    }

    fun clear() {
        prefs.edit().clear().commit()
    }

    private companion object {
        const val PREFS_NAME = "apimonitor_settings"
        const val KEY_BASE_URL = "base_url"
        const val KEY_PASSWORD = "password"
        const val KEY_API_TOKEN = "api_token"
        const val KEY_SESSION = "session_sid"
    }
}
