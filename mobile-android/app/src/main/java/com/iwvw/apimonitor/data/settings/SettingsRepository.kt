package com.iwvw.apimonitor.data.settings

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

class SettingsRepository(private val store: SettingsStore) {
    private val _credentials = MutableStateFlow(store.read())
    val credentials: StateFlow<Credentials> = _credentials.asStateFlow()

    val isConfigured: Boolean
        get() = _credentials.value.canSubmit

    fun save(credentials: Credentials) {
        val normalized = credentials.copy(
            baseUrl = credentials.normalizedBaseUrl,
            password = credentials.password,
            apiToken = credentials.apiToken.trim(),
        )
        store.write(normalized)
        _credentials.value = normalized
    }

    fun sessionCookie(): String = store.readSessionCookie()

    fun saveSession(cookie: String) {
        store.writeSessionCookie(cookie)
    }

    fun clearSession() {
        store.clearSession()
    }

    fun clear() {
        store.clear()
        _credentials.value = Credentials()
    }
}
