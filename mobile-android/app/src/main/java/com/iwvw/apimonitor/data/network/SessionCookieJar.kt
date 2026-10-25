package com.iwvw.apimonitor.data.network

import com.iwvw.apimonitor.data.settings.Credentials
import okhttp3.Cookie
import okhttp3.CookieJar
import okhttp3.HttpUrl
import java.util.concurrent.ConcurrentHashMap

/**
 * 内存 CookieJar。面板的登录态是 HttpOnly 的 sid cookie，
 * 我们只在内存里持有它，由 SettingsRepository 负责落盘加密。
 */
class SessionCookieJar : CookieJar {
    private val store = ConcurrentHashMap<String, MutableList<Cookie>>()

    @Volatile
    private var restoredSid: Cookie? = null

    override fun saveFromResponse(url: HttpUrl, cookies: List<Cookie>) {
        if (cookies.isEmpty()) return
        val list = store.getOrPut(url.host) { mutableListOf() }
        synchronized(list) {
            cookies.forEach { cookie ->
                list.removeAll { it.name == cookie.name }
                if (cookie.value.isNotBlank()) list.add(cookie)
            }
        }
    }

    override fun loadForRequest(url: HttpUrl): List<Cookie> {
        val now = System.currentTimeMillis()
        val list = store[url.host] ?: mutableListOf()
        val valid = synchronized(list) {
            list.removeAll { it.expiresAt < now }
            list.toList()
        }
        if (valid.isNotEmpty() && valid.any { it.name == SID }) return valid
        restoredSid?.let { cookie ->
            if (cookie.expiresAt > now) return listOf(cookie)
        }
        return valid
    }

    /** 用持久化的 sid 恢复会话，不需要重新登录。 */
    fun restore(host: String, sid: String) {
        if (sid.isBlank()) return
        restoredSid = Cookie.Builder()
            .name(SID)
            .value(sid)
            .domain(host)
            .path("/")
            .httpOnly()
            .build()
    }

    fun currentSid(): String? {
        val now = System.currentTimeMillis()
        store.values.forEach { list ->
            synchronized(list) {
                list.firstOrNull { it.name == SID && it.expiresAt > now }?.let { return it.value }
            }
        }
        return restoredSid?.takeIf { it.expiresAt > now }?.value
    }

    fun clear() {
        store.clear()
        restoredSid = null
    }

    companion object {
        const val SID = "sid"
    }
}
