package com.iwvw.apimonitor.data.network

import android.util.Log
import com.iwvw.apimonitor.data.model.AgentAction
import com.iwvw.apimonitor.data.model.AgentInstance
import com.iwvw.apimonitor.data.model.AgentInstanceList
import com.iwvw.apimonitor.data.model.AgentMetrics
import com.iwvw.apimonitor.data.model.AgentMetricsResponse
import com.iwvw.apimonitor.data.model.ApiStats
import com.iwvw.apimonitor.data.model.ApiStatsResponse
import com.iwvw.apimonitor.data.model.EndpointHealth
import com.iwvw.apimonitor.data.model.EndpointModelList
import com.iwvw.apimonitor.data.model.GatewayKey
import com.iwvw.apimonitor.data.model.GatewayKeyRotateResult
import com.iwvw.apimonitor.data.model.GatewayLogPage
import com.iwvw.apimonitor.data.model.GatewayPluginAccount
import com.iwvw.apimonitor.data.model.GatewayPluginAccountsResponse
import com.iwvw.apimonitor.data.model.GatewayPluginModel
import com.iwvw.apimonitor.data.model.GatewayPluginModelsResponse
import com.iwvw.apimonitor.data.model.GatewayPluginStatus
import com.iwvw.apimonitor.data.model.HostMetrics
import com.iwvw.apimonitor.data.model.HostMetricsResponse
import com.iwvw.apimonitor.data.model.OpenAiEndpoint
import com.iwvw.apimonitor.data.model.ServerAccountsResponse
import com.iwvw.apimonitor.data.model.ServerSummary
import com.iwvw.apimonitor.data.settings.Credentials
import kotlinx.coroutines.suspendCancellableCoroutine
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.jsonPrimitive
import okhttp3.Call
import okhttp3.Callback
import okhttp3.HttpUrl
import okhttp3.HttpUrl.Companion.toHttpUrlOrNull
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import okhttp3.Response
import java.io.IOException
import java.util.concurrent.TimeUnit
import kotlin.coroutines.resume
import kotlin.coroutines.resumeWithException

class ApiClient(
    private val cookieJar: SessionCookieJar = SessionCookieJar(),
    private val json: Json = DefaultJson,
) {
    private val client: OkHttpClient = OkHttpClient.Builder()
        .cookieJar(cookieJar)
        .connectTimeout(15, TimeUnit.SECONDS)
        .readTimeout(30, TimeUnit.SECONDS)
        .writeTimeout(30, TimeUnit.SECONDS)
        .retryOnConnectionFailure(true)
        .build()

    fun restoreSession(baseUrl: String, sid: String) {
        baseUrl.toHttpUrlOrNull()?.let { cookieJar.restore(it.host, sid) }
    }

    fun currentSid(): String? = cookieJar.currentSid()

    /** 供终端等长连接复用同一份会话 Cookie。 */
    fun cookieJar(): SessionCookieJar = cookieJar

    suspend fun login(credentials: Credentials, totpCode: String = ""): LoginOutcome {
        val base = credentials.normalizedBaseUrl.toHttpUrlOrNull()
            ?: return LoginOutcome.Failure("地址格式不正确，请填写 http(s)://主机:端口")

        val payload = buildString {
            append("{\"password\":").append(quote(credentials.password))
            if (totpCode.isNotBlank()) append(",\"totpToken\":").append(quote(totpCode))
            append("}")
        }

        val request = Request.Builder()
            .url(base.newBuilder().addPathSegments("api/auth/login").build())
            .header("Accept", "application/json")
            .post(payload.toRequestBody(JSON_MEDIA))
            .build()

        val response = try {
            client.newCall(request).await()
        } catch (e: IOException) {
            return LoginOutcome.Failure("无法连接面板：${e.message ?: "网络不可达"}")
        }

        if (response.code in 200..299) {
            val sid = cookieJar.currentSid()
                ?: return LoginOutcome.Failure("登录成功但未取得会话 Cookie")
            return LoginOutcome.Success(sid)
        }

        val message = extractMessage(response.body)
        return when {
            response.code == 401 && isTotpRequired(response.body, message) ->
                LoginOutcome.NeedsTwoFactor(message.ifBlank { "需要两步验证码" })

            response.code == 401 || response.code == 403 ->
                LoginOutcome.Failure(message.ifBlank { "账号或密码错误" })

            else -> LoginOutcome.Failure(message.ifBlank { "登录失败（HTTP ${response.code}）" })
        }
    }

    suspend fun verify(credentials: Credentials): VerifyResult {
        val base = credentials.normalizedBaseUrl.toHttpUrlOrNull()
            ?: return VerifyResult.Failure("地址格式不正确，请填写 http(s)://主机:端口")

        val health = try {
            request(base.newBuilder().addPathSegments("health").build(), credentials)
        } catch (e: IOException) {
            return VerifyResult.Failure("无法连接面板：${e.message ?: "网络不可达"}")
        }
        if (health.code == 401 || health.code == 403) {
            return VerifyResult.Failure("Token 无效或权限不足（HTTP ${health.code}）")
        }

        val check = try {
            request(base.newBuilder().addPathSegments("api/settings").build(), credentials)
        } catch (e: IOException) {
            return VerifyResult.Failure("鉴权校验失败：${e.message ?: "网络不可达"}")
        }

        return when (check.code) {
            401, 403 -> VerifyResult.Failure("鉴权失败（HTTP ${check.code}），请检查凭据")
            in 200..299 -> VerifyResult.Success(
                siteName = runCatching {
                    (json.parseToJsonElement(check.body) as? JsonObject)
                        ?.let { it["siteName"] ?: (it["data"] as? JsonObject)?.get("siteName") }
                        ?.jsonPrimitive?.content
                }.getOrNull().orEmpty(),
                baseUrl = credentials.normalizedBaseUrl,
            )

            else -> VerifyResult.Failure("面板返回异常状态 HTTP ${check.code}")
        }
    }

    suspend fun listServers(credentials: Credentials): Result<List<ServerSummary>> =
        call(credentials, "api/server/accounts") { body ->
            json.decodeFromString<ServerAccountsResponse>(body).data
        }

    // ---- 模型网关 ----

    suspend fun listEndpoints(credentials: Credentials): Result<List<OpenAiEndpoint>> =
        call(credentials, "api/openai/endpoints") { body ->
            json.decodeFromString<List<OpenAiEndpoint>>(body)
        }

    suspend fun toggleEndpoint(credentials: Credentials, id: String): Result<Unit> =
        callUnit(credentials, "api/openai/endpoints/$id/toggle", method = "POST")

    suspend fun endpointModels(credentials: Credentials, id: String): Result<List<String>> =
        call(credentials, "api/openai/endpoints/$id/models") { body ->
            json.decodeFromString<EndpointModelList>(body).models
        }

    suspend fun endpointHealth(credentials: Credentials, id: String): Result<EndpointHealth> =
        call(credentials, "api/openai/endpoints/$id/health") { body ->
            json.decodeFromString<EndpointHealth>(body)
        }

    suspend fun updateEndpoint(
        credentials: Credentials,
        id: String,
        name: String,
        baseUrl: String,
        apiKey: String,
        protocol: String,
        autoSwitch: Boolean,
        skipVerify: Boolean,
    ): Result<Unit> = callUnit(
        credentials,
        "api/openai/endpoints/$id",
        method = "PUT",
        body = buildString {
            append("{\"name\":").append(quote(name))
            append(",\"baseUrl\":").append(quote(baseUrl))
            append(",\"apiKey\":").append(quote(apiKey))
            append(",\"protocol\":").append(quote(protocol))
            append(",\"autoSwitch\":").append(autoSwitch)
            append(",\"skipVerify\":").append(skipVerify)
            append('}')
        },
    )

    suspend fun deleteEndpoint(credentials: Credentials, id: String): Result<Unit> =
        callUnit(credentials, "api/openai/endpoints/$id", method = "DELETE")

    suspend fun toggleEndpointModel(
        credentials: Credentials,
        id: String,
        model: String,
        enabled: Boolean,
    ): Result<Unit> = callUnit(
        credentials,
        "api/openai/endpoints/$id/models/toggle",
        method = "POST",
        body = "{\"model\":${quote(model)},\"enabled\":$enabled}",
    )

    suspend fun listGatewayKeys(credentials: Credentials): Result<List<GatewayKey>> =
        call(credentials, "api/openai/keys") { body ->
            json.decodeFromString<List<GatewayKey>>(body)
        }

    suspend fun updateGatewayKey(
        credentials: Credentials,
        id: String,
        name: String,
        expiresAt: String?,
    ): Result<Unit> = callUnit(
        credentials,
        "api/openai/keys/$id",
        method = "PUT",
        body = "{\"name\":${quote(name)},\"expiresAt\":${quote(expiresAt.orEmpty())}}",
    )

    suspend fun deleteGatewayKey(credentials: Credentials, id: String): Result<Unit> =
        callUnit(credentials, "api/openai/keys/$id", method = "DELETE")

    suspend fun toggleGatewayKey(
        credentials: Credentials,
        id: String,
        enabled: Boolean,
    ): Result<Unit> = callUnit(
        credentials,
        "api/openai/keys/$id/toggle",
        method = "POST",
        body = "{\"enabled\":$enabled}",
    )

    suspend fun rotateGatewayKey(credentials: Credentials, id: String): Result<String> {
        val base = credentials.normalizedBaseUrl.toHttpUrlOrNull()
            ?: return Result.failure(IllegalArgumentException("面板地址格式不正确"))
        val result = try {
            request(
                base.newBuilder().addPathSegments("api/openai/keys/$id/rotate").build(),
                credentials,
                method = "POST",
                body = "{}",
            )
        } catch (e: IOException) {
            return Result.failure(IOException(e.message ?: "网络不可达", e))
        }
        if (result.code == 401 || result.code == 403) {
            return Result.failure(ApiError(result.code, extractMessage(result.body).ifBlank { "鉴权失效" }))
        }
        if (result.code !in 200..299) {
            return Result.failure(ApiError(result.code, extractMessage(result.body).ifBlank { "HTTP ${result.code}" }))
        }
        return runCatching {
            json.decodeFromString<GatewayKeyRotateResult>(result.body).apiKey
        }.onFailure { Log.w(TAG, "decode rotate key failed", it) }
    }

    suspend fun gatewayLogs(
        credentials: Credentials,
        page: Int,
        pageSize: Int = 30,
    ): Result<GatewayLogPage> {
        val base = credentials.normalizedBaseUrl.toHttpUrlOrNull()
            ?: return Result.failure(IllegalArgumentException("面板地址格式不正确"))
        val url = base.newBuilder()
            .addPathSegments("api/openai/analytics/logs")
            .addQueryParameter("page", page.toString())
            .addQueryParameter("pageSize", pageSize.toString())
            .build()
        val result = try {
            request(url, credentials)
        } catch (e: IOException) {
            return Result.failure(IOException(e.message ?: "网络不可达", e))
        }
        if (result.code == 401 || result.code == 403) {
            return Result.failure(ApiError(result.code, extractMessage(result.body).ifBlank { "鉴权失效" }))
        }
        if (result.code !in 200..299) {
            return Result.failure(ApiError(result.code, result.body.take(200)))
        }
        return runCatching { json.decodeFromString<GatewayLogPage>(result.body) }
            .onFailure { Log.w(TAG, "decode gateway logs failed", it) }
    }

    // ---- AI Agent ----

    suspend fun listAgentInstances(credentials: Credentials): Result<List<AgentInstance>> =
        call(credentials, "api/aiagent/instances") { body ->
            json.decodeFromString<AgentInstanceList>(body).data
        }

    suspend fun agentMetrics(credentials: Credentials): Result<AgentMetrics> =
        call(credentials, "api/aiagent/metrics") { body ->
            json.decodeFromString<AgentMetricsResponse>(body).data
        }

    suspend fun agentLifecycle(
        credentials: Credentials,
        instanceId: String,
        action: AgentAction,
    ): Result<Unit> = callUnit(
        credentials,
        "api/aiagent/instances/$instanceId/lifecycle",
        method = "POST",
        body = """{"action":"${action.apiValue}"}""",
    )

    // ---- 网关插件 ----

    suspend fun pluginStatus(
        credentials: Credentials,
        pluginId: String,
    ): Result<GatewayPluginStatus> =
        call(credentials, "api/$pluginId/status") { body ->
            json.decodeFromString<GatewayPluginStatus>(body)
        }

    suspend fun pluginAccounts(
        credentials: Credentials,
        pluginId: String,
    ): Result<List<GatewayPluginAccount>> =
        call(credentials, "api/$pluginId/accounts") { body ->
            json.decodeFromString<GatewayPluginAccountsResponse>(body).accounts
        }

    suspend fun pluginModels(
        credentials: Credentials,
        pluginId: String,
    ): Result<List<GatewayPluginModel>> =
        call(credentials, "api/$pluginId/models") { body ->
            json.decodeFromString<GatewayPluginModelsResponse>(body).models
        }

    suspend fun togglePluginModel(
        credentials: Credentials,
        pluginId: String,
        modelId: String,
        enabled: Boolean,
    ): Result<Unit> = callUnit(
        credentials,
        "api/$pluginId/models/toggle/${java.net.URLEncoder.encode(modelId, "UTF-8")}",
        method = "POST",
        body = "{\"enabled\":$enabled}",
    )

    /** 启用 / 停用插件的某个账号（`POST /api/{plugin}/accounts/{id}/toggle`）。 */
    suspend fun togglePluginAccount(
        credentials: Credentials,
        pluginId: String,
        accountId: String,
        disabled: Boolean,
    ): Result<Unit> = callUnit(
        credentials,
        "api/$pluginId/accounts/${java.net.URLEncoder.encode(accountId, "UTF-8")}/toggle",
        method = "POST",
        body = "{\"disabled\":$disabled}",
    )

    // ---- 系统 ----

    suspend fun hostMetrics(credentials: Credentials): Result<HostMetrics> =
        call(credentials, "api/system/host-metrics") { body ->
            json.decodeFromString<HostMetricsResponse>(body).data
        }

    suspend fun apiStats(credentials: Credentials): Result<ApiStats> =
        call(credentials, "api/system/api-stats") { body ->
            json.decodeFromString<ApiStatsResponse>(body).data
        }

    private suspend fun callUnit(
        credentials: Credentials,
        path: String,
        method: String,
        body: String? = null,
    ): Result<Unit> {
        val base = credentials.normalizedBaseUrl.toHttpUrlOrNull()
            ?: return Result.failure(IllegalArgumentException("面板地址格式不正确"))
        val result = try {
            request(base.newBuilder().addPathSegments(path).build(), credentials, method, body)
        } catch (e: IOException) {
            return Result.failure(IOException(e.message ?: "网络不可达", e))
        }
        if (result.code == 401 || result.code == 403) {
            return Result.failure(ApiError(result.code, extractMessage(result.body).ifBlank { "鉴权失效" }))
        }
        if (result.code !in 200..299) {
            return Result.failure(ApiError(result.code, extractMessage(result.body).ifBlank { "HTTP ${result.code}" }))
        }
        return Result.success(Unit)
    }

    suspend fun logout(credentials: Credentials) {
        val base = credentials.normalizedBaseUrl.toHttpUrlOrNull() ?: return
        runCatching {
            request(
                base.newBuilder().addPathSegments("api/auth/logout").build(),
                credentials,
                method = "POST",
                body = "{}",
            )
        }
        cookieJar.clear()
    }

    private suspend fun <T> call(
        credentials: Credentials,
        path: String,
        decode: (String) -> T,
    ): Result<T> {
        val base = credentials.normalizedBaseUrl.toHttpUrlOrNull()
            ?: return Result.failure(IllegalArgumentException("面板地址格式不正确"))
        val result = try {
            request(base.newBuilder().addPathSegments(path).build(), credentials)
        } catch (e: IOException) {
            return Result.failure(IOException(e.message ?: "网络不可达", e))
        }
        if (result.code == 401 || result.code == 403) {
            return Result.failure(ApiError(result.code, extractMessage(result.body).ifBlank { "鉴权失效" }))
        }
        if (result.code !in 200..299) {
            return Result.failure(ApiError(result.code, result.body.take(200)))
        }
        return runCatching { decode(result.body) }
            .onFailure { Log.w(TAG, "decode failed for $path", it) }
    }

    private suspend fun request(
        url: HttpUrl,
        credentials: Credentials,
        method: String = "GET",
        body: String? = null,
    ): HttpResult {
        val builder = Request.Builder()
            .url(url)
            .header("Accept", "application/json")
        when (method) {
            "POST" -> builder.post((body ?: "{}").toRequestBody(JSON_MEDIA))
            "PUT" -> builder.put((body ?: "{}").toRequestBody(JSON_MEDIA))
            "PATCH" -> builder.patch((body ?: "{}").toRequestBody(JSON_MEDIA))
            "DELETE" -> builder.delete()
            else -> builder.get()
        }
        if (credentials.usesApiToken && cookieJar.currentSid() == null) {
            builder.header("Authorization", "Bearer ${credentials.apiToken}")
        }
        return client.newCall(builder.build()).await()
    }

    private suspend fun Call.await(): HttpResult = suspendCancellableCoroutine { cont ->
        enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                if (cont.isActive) cont.resumeWithException(e)
            }

            override fun onResponse(call: Call, response: Response) {
                response.use { res ->
                    val text = res.body.string()
                    if (cont.isActive) cont.resume(HttpResult(res.code, text))
                }
            }
        })
        cont.invokeOnCancellation { runCatching { cancel() } }
    }

    private fun extractMessage(body: String): String = runCatching {
        val obj = json.parseToJsonElement(body) as? JsonObject ?: return ""
        (obj["message"] ?: obj["error"])?.jsonPrimitive?.content.orEmpty()
    }.getOrDefault("")

    private fun isTotpRequired(body: String, message: String): Boolean {
        val text = (body + message).lowercase()
        return text.contains("2fa") || text.contains("totp") || text.contains("两步") || text.contains("验证码")
    }

    private fun quote(value: String): String = buildString {
        append('"')
        value.forEach { ch ->
            when (ch) {
                '"' -> append("\\\"")
                '\\' -> append("\\\\")
                '\n' -> append("\\n")
                '\r' -> append("\\r")
                '\t' -> append("\\t")
                else -> if (ch < ' ') append("\\u%04x".format(ch.code)) else append(ch)
            }
        }
        append('"')
    }

    private data class HttpResult(val code: Int, val body: String)

    sealed interface VerifyResult {
        data class Success(val siteName: String, val baseUrl: String) : VerifyResult
        data class Failure(val message: String) : VerifyResult
    }

    sealed interface LoginOutcome {
        data class Success(val sessionId: String) : LoginOutcome
        data class NeedsTwoFactor(val message: String) : LoginOutcome
        data class Failure(val message: String) : LoginOutcome
    }

    class ApiError(val statusCode: Int, val body: String) :
        IOException("HTTP $statusCode: $body")

    companion object {
        private const val TAG = "ApiClient"
        private val JSON_MEDIA = "application/json; charset=utf-8".toMediaType()

        val DefaultJson = Json {
            ignoreUnknownKeys = true
            isLenient = true
            explicitNulls = false
            coerceInputValues = true
        }
    }
}
