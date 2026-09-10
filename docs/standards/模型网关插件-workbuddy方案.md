# 模型网关插件：WorkBuddy 方案与脚手架说明

最后更新：2026-09-10
状态：**已实现**（上游协议层已补全；`upstreamImplemented = true`）

本文档给出「模型网关 → 插件中心」新增 `workbuddy` 插件的接口设计、文件清单、接线点、登记与卸载步骤，
并说明当前脚手架**已实现**与**待实现**的边界。实现事实来源：
[LiuJiaCheng11/workbuddy-cliproxy](https://github.com/LiuJiaCheng11/workbuddy-cliproxy)（CLIProxyAPI 的 c-shared 插件，
原始设计归属 Sliverkiss）。

---

## 1. 目标

把**腾讯 CodeBuddy**（`copilot.tencent.com`）封装成模型网关的一个 OpenAI 兼容上游：

- 插件内完成 CodeBuddy **扫码登录**与 **access token 自动刷新**，凭据落库；
- 模型目录**动态**来自上游 `GET /v3/config`（不硬编码），随上游发布自动生效；
- 对外暴露 `POST /api/workbuddy/v1/chat/completions`（流式 + 非流式），
  按 ds2api 同样的方式**接入网关端点列表**，由网关统一计费/日志/路由；
- 前端在「模型网关 → 插件」页提供一张插件卡片：启用开关、扫码登录、账号列表、模型启停、网关接入。

## 2. 移植策略

参考仓库是 CLIProxyAPI 的 **c-shared（CGO 动态库）** 插件，需要 CPA v7 运行时加载 `.so`/`.dll`。
本项目是**内嵌 Go 进程**架构，`server.go` 直接组装各插件 Service，不存在外部插件宿主，
因此**不引入 CGO 动态库**，改为按其记录的上游协议事实**原生实现**。

| 参考仓库 | 本项目对应 |
| --- | --- |
| `main.go` 的 C ABI 导出（`cliproxy_plugin_init` / `cliproxyPluginCall`） | 无需：`Service` 直接实现 `http.Handler` |
| `pluginabi` / `pluginapi` RPC 方法分派（`handleMethod`） | `ServeHTTP` 路径分派 |
| `host.stream.emit` 异步流式回推 | 直接把上游 SSE 边读边 `Flush` 给下游 |
| `workbuddy.json` 单凭据文件 | `workbuddy_settings` 表内的账号数组（支持多账号） |
| CPA `plugins/` + `config.yaml` 启用 | `manifest.go` 路由登记 + `server.go` 组装接线 |
| `wrappers`（`pluginapi.ModelInfo` 等） | 本项目自有 `ModelInfo` 结构 |

参照物是项目内的 **DS2API 插件**（同为「网页产品账号池 → OpenAI 兼容」形态），
目录结构、表命名、link 语义、前缀语义全部对齐，便于后续维护与统一卸载。

## 3. 上游协议事实清单（来自参考仓库）

以下事实直接决定实现细节，**照抄不可改动**：

| 事实 | 说明 |
| --- | --- |
| 上游基址 | `https://copilot.tencent.com` |
| 客户端 UA | `CLI/2.63.2 CodeBuddy/2.63.2` |
| Origin / Referer | `https://www.codebuddy.cn`，**缺失时 `/v3/config` 返回 400** |
| 登录状态 | `POST /v2/plugin/auth/state?platform=CLI` → `{state, authUrl}`，二维码即 `authUrl` |
| 登录轮询 | `GET /v2/plugin/auth/token?state=...`：未完成返回业务码 `11217`（"login ing"）；完成返回 token 包 |
| 账号信息 | `GET /v2/plugin/login/account?state=...`，**必须带 Bearer**，openresty 在未登录时直接 401 |
| Token 刷新 | `POST /v2/plugin/auth/token/refresh`，头 `X-Refresh-Token`，可选 `X-Enterprise-Id` |
| 聊天转发 | `POST /v2/chat/completions`，头 `Authorization: Bearer <accessToken>` / `X-User-Id` / `X-Enterprise-Id` / `X-Refresh-Token` / `X-Domain` / `X-Product: SaaS` |
| 空值约定 | 上述头为空时改用 `X-No-Authorization` / `X-No-User-Id` / `X-No-Enterprise-Id` / `X-No-Department-Info` 置 `1` |
| 非流式被拒 | 上游对非流式返回业务码 `11101`，**必须强制 `stream:true` 再聚合** |
| 模型目录 | `GET /v3/config`，只需 **`X-User-Id` 存在**（值不校验）即从匿名 `models:null` 切到完整目录；目录与账号无关，可登录前获取 |
| 目录字段 | `id` / `name` / `maxInputTokens` / `maxOutputTokens` / `supportsImages` / `supportsReasoning`；`maxInputTokens <= 0` 的条目（如 `hunyuan-image-v3.0-art`）不能服务对话，需跳过 |
| 登录态隔离 | 一个 state 一个 cookie jar，**不可共用**（浏览器登录与 state 绑定） |
| 内容审核 | 两句 Claude Code 固定 system 模板被逐字拉黑，需最小改写：`official CLI for Claude` → `official CLI tool for Claude`、`Main branch` → `Default branch`（精确匹配，非语义） |
| 思考模式 | `hy3` / `hy4` 前缀模型自动置 `reasoning_effort=high`（上游只认 `high`） |
| SSE 分帧 | `/v1/chat/completions`、`/v1/completions` 入口的 chunk **不带** `data: ` 前缀；其它跨协议翻译入口**必须带** |

## 4. 后端模块

```
backend-go/internal/workbuddy/
├── service.go     Service / Settings / New / 建表 / 设置读写 / ServeHTTP 分派 / 调用计数
├── upstream.go    ★ 上游协议层：常量、类型、客户端头、登录/刷新/目录/对话四类上游调用
├── auth.go        扫码登录 start/poll、token 刷新、账号 CRUD / 启用停用 / 导出导入
├── models.go      模型目录读取、缓存、启停（含批量）
├── relay.go       /api/workbuddy/v1：模型列表 + chat/completions（流式/非流式）
├── usage.go       实际用量与扣费（站点时区日聚合）、选号权重、usage 接口
├── diag.go        /_diag 排障面：上游原始 usage 与归一化后实际写往下游的 usage
└── link.go        与网关端点列表的接入/断开/状态（openai_endpoints）
```

### 4.1 Settings（持久化于 `workbuddy_settings.data`）

```jsonc
{
  "enabled": false,              // 总开关；关闭时上游调用全部拒绝
  "modelPrefix": "",             // 对外模型名前缀（如 "wb-"），转发时剥离
  "proxyPoolId": "",             // 复用「代理池」插件的出口
  "disabledModels": [],          // 不对外提供的模型
  "accounts": [                  // 扫码登录产生的凭据（含 token，仅服务端可见）
    {
      "id": "<uid>", "uid": "...", "enterpriseId": "...", "nickname": "...",
      "domain": "...", "accessToken": "...", "refreshToken": "...",
      "expiresAt": 1780000000, "disabled": false,
      "createdAt": "...", "lastRefreshAt": "...", "lastError": ""
    }
  ]
}
```

`Settings` 的 `Accounts` 字段**永不直接下发**给前端；对外一律经 `toAccountView()` 脱敏（不下发任何 token）。

### 4.2 数据表

| 表 | 用途 |
| --- | --- |
| `workbuddy_settings` | 单行（`id = 1`）JSON 设置，含账号凭据 |
| `workbuddy_call_stats` | 每账号累计调用次数（`identifier` 主键，逐条 UPSERT 累加） |
| `workbuddy_usage_daily` | 实际用量与扣费，按「站点时区日期 × 账号 × 模型」聚合（主键 `day, account_id, model`） |
| `workbuddy_diagnostics` | 单行（`id = 1`）最近一次 usage 诊断快照，由每分钟的落盘 ticker 写入 |

`openai_endpoints` 表复用既有结构（`plugin_id = 'workbuddy'`），不新增列。

## 5. HTTP 接口契约

### 5.1 管理面 `/api/workbuddy/*`（会话鉴权，`AuthSession`）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/workbuddy/settings` | 读取插件设置（账号已脱敏） |
| PUT | `/api/workbuddy/settings` | 保存设置并生效 |
| GET | `/api/workbuddy/status` | `{enabled, upstreamReady, accountCount, availableCount}` |
| POST | `/api/workbuddy/test` | 连通性自检 |
| POST | `/api/workbuddy/login/start` | 发起扫码登录 → `{state, url, expiresAt}` |
| POST | `/api/workbuddy/login/poll` | 轮询登录态 → `{status: pending\|success, account?}` |
| GET | `/api/workbuddy/accounts` | 账号列表（脱敏 + 调用次数 + token 状态） |
| DELETE | `/api/workbuddy/accounts/{id}` | 删除账号 |
| PUT | `/api/workbuddy/accounts/{id}` | 改备注名 |
| POST | `/api/workbuddy/accounts/{id}/toggle` | 启用/停用（停用即摘出转发与轮询） |
| POST | `/api/workbuddy/accounts/{id}/refresh` | 立即刷新 token |
| POST | `/api/workbuddy/accounts/{id}/test` | 单账号连通性测试 |
| GET | `/api/workbuddy/accounts/export` | 导出账号（**含 token**，仅本机会话可下载） |
| POST | `/api/workbuddy/accounts/import` | 导入账号（按 `id` 去重合并） |
| GET | `/api/workbuddy/models` | `{success, upstreamReady, models:[{id, enabled, contextLength, maxOutputTokens, supportsImages, supportsReasoning}]}` |
| POST | `/api/workbuddy/models/toggle/{id}` | 单模型启停 |
| POST | `/api/workbuddy/models/toggle-batch` | 批量启停 |
| GET | `/api/workbuddy/link` | 网关接入状态 |
| POST | `/api/workbuddy/link` | 接入网关端点列表 |
| DELETE | `/api/workbuddy/link` | 断开并从端点列表移除 |

### 5.2 中继面 `/api/workbuddy/v1/*`（`AuthInternal`，仅本机回环）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/workbuddy/v1/models` | 带前缀的对外模型列表（网关校验/刷新端点模型时调用） |
| POST | `/api/workbuddy/v1/chat/completions` | OpenAI 兼容对话（流式 + 非流式） |
| GET | `/api/workbuddy/v1/_diag` | 排障面：上游原始 usage 与归一化后实际发往下游的 usage（只读，不发上游请求） |

> `/_diag` 挂在**中继前缀**下，因此复用 `AuthInternal`（仅本机回环、无需凭据），不新增鉴权面。
> 用于定位「插件有值、网关统计为 0」这类字节级不一致：网关用字面量正则
> `"cached_tokens"\s*:\s*(\d+)` 取第一个匹配，只认不带引号的整数。

网关接入后，`openai_endpoints` 里会出现一条固定端点：

- `id = "workbuddy-internal"`
- `name = "WorkBuddy"`
- `base_url = "http://127.0.0.1:<PORT>/api/workbuddy/v1"`
- `api_key = "sk-workbuddy-internal"`（内部固定密钥，仅回环注入）
- `plugin_id = "workbuddy"`

## 6. 路由与接线点

### 6.1 `backend-go/internal/manifest/manifest.go`

在插件区块（`/api/ds2api` 之后）追加两条：

```go
{Prefix: "/api/workbuddy", Module: "workbuddy", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "WorkBuddy 插件（腾讯 CodeBuddy 转 OpenAI 兼容 API）管理"},
{Prefix: "/api/workbuddy/v1", Module: "workbuddy-compatible", Owner: OwnerGo, Auth: AuthInternal, ResponseMode: ResponseStream, Description: "WorkBuddy 插件 OpenAI 兼容中继（仅本机内部网关调用）"},
```

### 6.2 `backend-go/internal/server/server.go`

| 位置 | 改动 |
| --- | --- |
| import 块 | `"github.com/iwvw/api-monitor/backend-go/internal/workbuddy"` |
| `Server` 结构 | `workbuddy *workbuddy.Service` |
| `newServer` 构造 | `workbuddy: workbuddy.New(cfg),` |
| 注入 | `server.workbuddy.SetProxyPoolSelector(server.proxypool)` |
| 后台任务 | `server.workbuddy.StartCallStatsFlush(warmupCtx)`（调用计数落盘） |
| `authorizeGoRoute` | `AuthAPIKey` 分支的兼容模块列表加入 `"workbuddy-compatible"` |
| `serveGoRoute` | `case "/api/workbuddy", "/api/workbuddy/v1": s.workbuddy.ServeHTTP(w, r)` |

> `checkRouteImplementation` 要求 prefix 路由在 `server.go` 出现 `case "<prefix>"`，上述 case 满足。

## 7. 前端

### 7.1 文件与注册

- 新增 `src/js/pages/openai/plugins/WorkBuddyPlugin.jsx`
- `src/js/pages/openai/OpenAIPluginsPanel.jsx` 的 `PLUGINS` 数组追加：

```js
{ id: 'workbuddy', name: 'WorkBuddy', description: '腾讯 CodeBuddy 扫码登录转 OpenAI 兼容 API。', icon: Sparkle, detail: WorkBuddyPlugin }
```

### 7.2 组件结构（单列 SectionCard 堆叠）

1. **WorkBuddy**（配置）：启用中继、接入模型网关、模型前缀。
2. **账号**：`Table`（启用 / 账号 / 调用 / 状态 / 操作），顶部「扫码登录」按钮 + 刷新。
3. **模型**：`Table`（启用 / 模型 / 上下文 / 输出上限 / 图像），顶部批量开关。

对话框：扫码登录（二维码 + 轮询状态 + 过期重取）、删除账号（`useConfirmPress` 二次确认）。

样式全部走 §「样式红线」：Kumo 组件 + `SectionCard`/`FieldRow`、无硬编码颜色、说明文字走 `title` 悬浮、
`getAuthHeaders()` 统一请求头、`toast` 统一提示、写操作后回读验证。

## 8. 登记与卸载

### 8.1 `docs/plugins/registry.json`

追加 `workbuddy` 条目（`kind: "engine-plugin"`），如实记录后端目录、manifest 行、server 接线、
表、前端文件与注册点、`goDeps`（预期为空，仅标准库）、卸载步骤。

### 8.2 卸载步骤

1. `rm -rf backend-go/internal/workbuddy/`
2. `manifest.go`：删除 `/api/workbuddy` 与 `/api/workbuddy/v1` 两行
3. `server.go`：删除 import、`workbuddy` 字段、`workbuddy.New(cfg)`、`SetProxyPoolSelector` 注入、
   `StartCallStatsFlush`、`serveGoRoute` case、`workbuddy-compatible` 鉴权分支
4. DB：`DROP TABLE workbuddy_call_stats; DROP TABLE workbuddy_settings;`
   并 `DELETE FROM openai_endpoints WHERE id = 'workbuddy-internal';`
5. 删除 `src/js/pages/openai/plugins/WorkBuddyPlugin.jsx`
6. `OpenAIPluginsPanel.jsx`：移除 `workbuddy` 注册条目
7. 删除本文档

## 9. 实现状态

### 后端（全部实现）

| 文件 | 内容 |
| --- | --- |
| `service.go` | Service/Settings、建表（`workbuddy_settings` / `workbuddy_call_stats`）、设置读写、`ServeHTTP` 全路径分派、模型前缀、调用计数落盘 |
| `upstream.go` | 上游协议层：协议常量、数据形状、HTTP 管线（`commonHeaders` / `backendHeaders` / `doEnvelope` / `doJSON` / `httpClientFor`）、**五个上游调用全部实现**、六个纯函数适配器、SSE 聚合 |
| `auth.go` | 扫码登录 start/poll、token 刷新、账号 CRUD / 启停 / 导出导入、脱敏视图、token 状态计算 |
| `models.go` | 模型目录缓存（10 分钟 TTL，失败回落上一份快照）、启停、前缀变更时 `openai_endpoints` 三列命名空间迁移 |
| `relay.go` | `/v1/models`、`/v1/chat/completions`、选号与失效即刷新、后台 `StartAutoRefresh` |
| `link.go` | 与 `openai_endpoints` 的接入/断开/状态 |

### 上游五个调用的实现要点

| 函数 | 实现 |
| --- | --- |
| `startLogin` | `POST /v2/plugin/auth/state`（body `{}`）→ `{state, authUrl}`；**按 state 新建独立 cookie jar** 存入 `loginStates` |
| `pollLogin` | `GET /v2/plugin/auth/token?state=`：业务码 `11217` / HTTP 4xx → pending（不是错误）；拿到 bearer 后再 `GET /v2/plugin/login/account` 补 uid/企业信息 |
| `refreshAccessToken` | `POST /v2/plugin/auth/token/refresh`，头 `X-Refresh-Token` / `X-Enterprise-Id` / `X-Auth-Refresh-Source`；HTTP 4xx 判定 refresh token 失效 |
| `fetchCatalog` | `GET /v3/config` + `Origin`/`Referer` + `X-User-Id: <固定 UUID>`；跳过 `maxInputTokens <= 0` 的条目 |
| `chatCompletions` | `POST /v2/chat/completions`；流式边读边 `Flush`，非流式 `aggregateCompletion` 聚合成整包；错误一定发生在写出响应之前 |

### 转发路径上的三个关键决定

1. **一律强制 `stream:true` 发上游**（上游对非流式回业务码 `11101`）；客户端是否流式只决定本层是
   「边读边转发」还是「聚合成整包」。
2. **中继面恒发标准 OpenAI SSE 分帧**（每个 chunk 一行 `data: <json>` 加空行分隔，流末补 `data: [DONE]`）。
   参考实现作为 CPA ABI 插件需要按入口协议区分分帧；本项目的中继面是**真实 HTTP 端点**，
   下游是模型网关（见 `internal/openai/relay.go` 对 `data: ` 前缀的判定），因此不需要那层区分。
3. **选号是确定性的**：优先第一个可用账号；若全都不可用但存在「未停用 + 有 refresh token + token 失效」
   的账号，则先刷新再用。另有后台 `StartAutoRefresh`（5 分钟周期）提前刷新即将过期的 token。

### 模型开关（停用/启用）的三层生效点

这个开关必须同时落在三处才算"有效"，缺一处就会出现"关了还能用"：

| 层 | 落点 | 作用 |
| --- | --- | --- |
| 插件设置 | `workbuddy_settings.disabledModels` | 单一事实来源，存**对外（带前缀）**模型名 |
| 网关联络 | `openai_endpoints.disabled_models`（由 `syncLinkedEndpointDisabledModels` 写入） | **网关就是按这一列拦请求的**：`internal/openai/relay.go` 的 `resolveEndpointModel` → `isModelDisabled(ep.DisabledModels, requested)`。只改插件设置，网关仍会把请求路由过来 |
| 转发闸 | `relay.go` 的 `serveChatCompletions` 前置判定 | 覆盖直连 `/api/workbuddy/v1`（绕过网关）的情况，返回 OpenAI 的 `model_not_found` |

注意 `openai_endpoints.models` 只是展示/刷新用的名单，**既不拦请求也不做白名单**（网关不读它做准入），
所以不能靠"把它从 models 里删掉"来实现停用。

前缀变更时，停用名单也要跟着换命名空间。前端是整对象 PUT，提交的是「新前缀 + 旧命名空间的名单」，
因此服务端做**双向归一化**（`stripAnyPrefix` 同时试旧、新前缀，按最长匹配优先，`remapNamespace` 再套新前缀），
保证"提交旧命名空间名单"与"提交新前缀名单"两种输入都归一化到同一结果且幂等。

### 前端

`WorkBuddyPlugin.jsx`：启用中继 / 接入模型网关 / 模型前缀 / 运行状态、账号表（扫码登录、启停、
刷新 token、改名、删除、导出导入）、模型表（批量启停、上下文、输出上限、图像支持）、接入信息条；
扫码登录对话框用 `qrcode` 本地生成二维码并 2 秒轮询。

### 验证

`go build ./...`、`go test ./internal/{workbuddy,manifest,server}`、
`npm run governance:check`（371 路由）、`npm run ui:governance`、`npx eslint` 全部通过。
单测覆盖：模板改写、思考档位、delta 清理、强制流式、SSE 分帧判定、`stripDataPrefix`、
`aggregateCompletion`（含 usage / tool_calls / reasoning_content / 兜底字段）、前缀迁移、token 状态。

### 尚未验证的部分

五个上游调用是**按参考仓库记录的协议事实实现**的，尚未用真实 CodeBuddy 账号跑通端到端。
首次联调建议顺序：① 扫码登录 → ② `/api/workbuddy/v1/models` 看目录 → ③ 直连
`POST /api/workbuddy/v1/chat/completions`（流式与非流式各一次）→ ④ 接入网关后经
`/v1/chat/completions` 转发。

## 10. 待确认的调优问题

以下不影响正确性，只影响行为取向；当前取的是「与参考实现一致」的默认值：

1. ~~**多账号调度**：现为「首个可用 / 首个可刷新」，无轮询与冷却。~~ **已定案**：
   采用「**站点时区今天已消耗 credit 最少**」选号（见 §14），并已补上
   **429/5xx 失败换号与账号冷却**（见 §14「失败换号与冷却」）。
2. **凭据存储位置**：现存在 `workbuddy_settings` 表的 JSON 里（单源最简）。是否改为独立凭据表或
   落盘 `data/workbuddy/*.json`（参考仓库做法）以便与设置解耦？
3. **模型前缀默认值**：现默认空串（沿用上游模型名）。是否默认 `wb-` 以免与其它端点重名？
4. **Claude Code 模板改写**：现无条件改写（同参考实现）。会轻度污染发往上游的 system 提示，
   网关若主要服务非 Claude Code 客户端，可做成开关。
5. **hy3/hy4 强制 `reasoning_effort=high`**：现无条件覆盖。是否做成可关闭项便于对比排障？

## 11. 上游可获取的账号 / 计费信息（实测结论）

2026-09-10 实测（匿名探测 + 一次真实目录拉取）。

| 信息 | 能否获取 | 来源 | 状态 |
| --- | --- | --- | --- |
| **模型倍率（credits）** | ✅ | `GET /v3/config` 的 `models[].credits`，形如 `"x0.79 credits"` | **已接入**：解析为 `ModelInfo.CreditsMultiplier`，在插件模型表「倍率」列展示。**不需要登录** |
| 模型能力元数据 | ✅ | 同接口：`maxInputTokens` / `maxOutputTokens` / `maxAllowedSize` / `supportsImages` / `supportsReasoning` / `supportsToolCall` / `onlyReasoning` / `vendor` / `descriptionZh` | **已接入**（图像类条目 `maxInputTokens <= 0` 会被跳过） |
| 账号剩余额度 / 已用积分 | ❌（CLI 侧无公开接口） | — | `/v2/plugin/*` 与 `/v3/config` 都不返回；实测探测 8 个候选额度路径（`/v2/plugin/user/credit`、`/v2/plugin/usage`、`/v2/plugin/quota`、`/v2/credit/balance` 等）**全部 404** |
| 单次请求 token 用量 | ✅ | `POST /v2/chat/completions` 的 SSE 流末 `usage` | 实测需 `stream_options.include_usage: true`（本插件已自动注入）；含 `prompt_cache_hit_tokens` / `prompt_cache_miss_tokens` |
| **单次请求实际扣费（credit）** | ✅ | 同上，`usage.credit` | 实测未命中约 0.45、命中约 0.13~0.21（GLM-5.3）。比「倍率」更直接；目前透传给网关但未落库 |
| 企业级额度 / 用量明细 | ✅ 但要走另一套 | 企业 OpenAPI（Bearer + `enterpriseId`）：`/api/v1/enterprises/{id}/openapi/usage/default-quota`、`/usage/members/detail`、`/usage/quota-cycle` | 需旗舰版/专享版企业管理员权限，与 CLI 用户 token 体系不同，不是同一个鉴权面 |

### 额度体系背景

- **Credit（积分）** 是 CodeBuddy 的统一用量单位；倍率（`credits`）就是「同一批 token 相对消耗几倍积分」。
- 额度按周期发放：个人 Free 100 积分/月、Team 成员 1000、企业默认 2000 积分/月；个人用量在
  CodeBuddy 官网「个人主页 → 用量」查看，CLI 侧没有对应接口。

### 本项目可以自己补的部分（不依赖上游接口）

- **每账号调用次数**：已有（`workbuddy_call_stats`）。
- **每账号 token 用量 + 按倍率加权的相对消耗**：可从网关自身的数据聚合 ——
  `openai_gateway_stats_hourly` 与调用日志都带 `endpointId`（= `workbuddy-internal`）与
  prompt/completion tokens，再乘各模型倍率即可得到「相对消耗」，用于找出吃额度的账号/模型。
- ⚠️ **不要把倍率写进 `openai_endpoints.pricing`**：那一列是「每百万 token 的**货币**单价」，
  而 credits 是相对倍数、不含货币单价，两者不可直接换算；硬写会让网关算出假金额。

### 实测数据（2026-09-10，28 个可服务对话的模型）

倍率区间 `x0.00` ~ `x1.62`，示例：`hy3` / `hy4-preview` = `x0.00`（不计费）、
`deepseek-v4.1-flash` = `x0.03`、`glm-5.3` / `glm-5.2` = `x0.79`、`kimi-k3-1` = `x1.62`。
已有一个默认跳过的联调测试守住这个字段：

```bash
cd backend-go && WORKBUDDY_LIVE=1 go test ./internal/workbuddy/ -run TestLiveFetchCatalog -v
```

## 12. 「缓存命中」统计为 0 的真正原因：字段名不匹配（已修）

> **本节纠正了一个我早先给出的错误结论。** 最初我根据"网关正则没匹配到 `cached_tokens`"推断
> "上游不上报缓存字段" —— 这个推断是错的。上游**确实上报**，只是命名不同。

### 上游实际返回什么（第三方可复现实测 + 腾讯官方协议文档）

- 端点 `POST https://copilot.tencent.com/v2/chat/completions`，参数 `stream: true` +
  **`stream_options.include_usage: true`**（非流式返回 400）。
- 读取字段：**`usage.prompt_cache_hit_tokens`**（命中）、`usage.prompt_cache_miss_tokens`（未命中）、
  **`usage.credit`**（本次扣费）。
  来源：《腾讯 Copilot 通道 GLM-5.3 缓存间歇性不命中· 技术调查日志》（2026-08-27，
  `cloud.tencent.com/developer/article/2732642`）。
- 腾讯官方《OpenAI Chat Completions 协议字段说明》另列 `usage.cache_read_tokens` /
  `usage.cache_write_tokens` 与 `usage.prompt_tokens_details.cached_tokens`
  （`cloud.tencent.com/document/product/1823/135872`）。

即 CodeBuddy 用的是 **DeepSeek 风格命名 `prompt_cache_hit_tokens`**。

### 网关统计不到的三个原因（均已修）

1. **字段名不匹配**：网关只认 `usage.prompt_tokens_details.cached_tokens`
   —— `internal/openai/service.go` 的 `cachedTokensRegex` 是字面量 `"cached_tokens"` 正则，
   `prompt_cache_hit_tokens` 它根本看不见 → 恒为 0。
2. **缺少 `stream_options.include_usage`**：网关转发时不带该参数（`internal/openai/` 里搜不到
   `stream_options`）。按实测的复现参数，这个端点的完整 usage（尤其缓存与 credit 字段）需要该开关。
3. **值的数据类型不匹配（改完前两条仍为 0 的原因）**：网关的正则是 `"cached_tokens"\s*:\s*(\d+)`，
   **只认不带引号的整数**。而实测上游把命中数发成**字符串**（`"prompt_cache_hit_tokens":"930560"`），
   若只做「改名字」的原样搬运，输出就是 `"cached_tokens":"930560"` —— 带引号，正则匹配不到。
   这个 bug 特别隐蔽：插件侧的容错解析（`jsonInt` 能解析字符串）依旧拿到 930560 并记进用量表，
   只有网关那一侧是 0，两边数据不一致。

   修复：`normalizeUsageCache` 用 `numericValue` **强制转成 JSON 数字**（整数给 `int64`，
   避免被序列化成 `9.3056e+05`；小数给 `float64`）；非数字值直接丢弃，不产出会被误读的字段。

### 怎么一眼确认是哪一类

`upstreamUsage` 里带 `types`（各字段的 JSON 类型）。若看到
`"prompt_cache_hit_tokens": "string"`，就是上面第 3 类问题 —— 这也是当时定位到它的关键线索：
**只看"插件统计有值、网关统计为 0"就足以断定问题在写下去的字节里**，而不是在上游有没有上报。

### 修复内容

- `forceStreamBody` 现在同时注入 `stream: true` 与 `stream_options.include_usage: true`
  （已有 `stream_options` 会**合并保留**，不整体覆盖）。
- `normalizeUsageCache` 把「命中」类字段统一归一到 `usage.prompt_tokens_details.cached_tokens`，
  覆盖 `cached_tokens`、`prompt_cache_hit_tokens`、`cache_read_tokens`、`cache_read_input_tokens`、
  `cache_hit_tokens`、`total_cached_tokens`；只归一命中语义，`*_write_*`（写入缓存）**不**计入命中率。
- 流式（chunk 清洗）与非流式（聚合）两条路径都过。

### 旁证：此前的 0 与「上游是否缓存」无关

`data/data.db` 的 `openai_gateway_analytics`：Vertex 端点 499 次调用命中 3324 万 / 5372 万 prompt（≈86%），
`workbuddy-internal` 16 次全 0。那 86% 来自 `gemini_upstream.go` 对 Gemini `total_cached_tokens` 的显式映射 ——
两个端点只差在**适配层有没有做字段映射**。看 0 就断言"上游没缓存"，是漏掉了一层。

### 两个额外发现

- **`usage.credit` 是本次请求的真实扣费**（实测 GLM-5.3 未命中 0.45~0.46、命中 0.13~0.21；
  DeepSeek V4 Flash 第二次命中后降到 0.01）。比「倍率」更直接，目前随 usage 透传但未落库，
  需要的话可以接进插件用量统计。
- **GLM 系缓存不稳定是服务端多副本不共享缓存所致**（实测命中率 4/10，且 HIT/MISS 近乎交替），
  客户端无法修复。同一实验还发现 `prompt_cache_key` 对 GLM 是**反效果**、会彻底关掉缓存 —— **不要加**。

### 自查方式

`GET /api/workbuddy/status` 的 `upstreamUsage{keys,sample,cacheReported,at}` 展示上游**最近一次** usage 的
真实字段名与原始样例（记录的是**未清洗**的 chunk）。前端「运行状态」行有
`上游已上报缓存命中` / `上游未上报缓存命中` 徽标。
修复后应看到 keys 中出现 `prompt_cache_hit_tokens`、`cacheReported=true`，
且网关看板里该端点的缓存命中率不再是 0。

## 13. 实际用量与扣费统计（usage.credit）

### 目标

把上游 usage 里的 tokens 与 **`usage.credit`（本次请求的真实扣费，实测字段）** 按
「**站点时区日期 × 账号 × 模型**」聚合，回答"谁在烧额度、哪个模型最贵"。

### 为什么落插件自己的表，而不是网关的 `openai_gateway_analytics`

- 那张表的维度是**端点 / 网关 key**，列结构属 openai 模块（单 Owner 文件），加列会跨域改动；
- "哪个插件账号在消耗"属于插件自己的域，落自己的表最干净，也不影响网关既有统计。

### 表与落盘策略

```sql
CREATE TABLE IF NOT EXISTS workbuddy_usage_daily (
    day TEXT NOT NULL,              -- 站点时区下的 YYYY-MM-DD
    account_id TEXT NOT NULL,
    model TEXT NOT NULL,
    requests INTEGER NOT NULL DEFAULT 0,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    cached_tokens INTEGER NOT NULL DEFAULT 0,
    credit REAL NOT NULL DEFAULT 0,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (day, account_id, model)
);
```

- 行数**有界**：天数 × 账号数 × 模型数，不会随请求量膨胀。
- **内存累加 + 每分钟落盘**（复用 `StartCallStatsFlush` 的同一 ticker），避免每请求一次写库；
  落库失败时把增量放回内存等待下轮，不丢数。
- 逐键 `ON CONFLICT ... DO UPDATE SET x = x + excluded.x` 累加，避免先读后写丢增量。

### 时区

日期归属**一律走站点时区**（CONTEXT.md 硬性规则）：`timeutil.LocationFromSettings`，
不在插件里直接引用 `time.Local` / `time.UTC` 做日期归属，兜底也用 `timeutil.LocationFromName("")`。
站点时区按 5 分钟缓存，避免每个请求都开库读设置，又能在改设置后很快生效。

### 接口

`GET /api/workbuddy/usage?days=7`（`days` 钳制 1~90）：

```jsonc
{
  "success": true, "days": 7, "since": "2026-09-04",
  "totals": { "requests": 3, "promptTokens": 350, "completionTokens": 15,
              "cachedTokens": 80, "credit": 0.63, "cacheHitRate": 0.2286 },
  "byAccount": [{ "accountId": "u1", "accountName": "…", "requests": 2, "credit": 0.53, "cacheHitRate": 0.267 }],
  "byModel":   [{ "model": "hy3", "requests": 2, "credit": 0.53 }],
  "daily":     [{ "day": "2026-09-10", "requests": 3, "credit": 0.63 }],
  "creditReported": true
}
```

- 查询前**先 flush**，保证刚发生的调用本次就能查到。
- 排序按扣费降序（其次调用数），直接回答"谁最贵"。
- `creditReported` 区分「上游没给 credit 字段」与「确实没花钱」：`usage.credit` 报 `0`（如 `hy3` 倍率 x0.00）
  与"字段缺失"是两件事，前端据此提示。

### 前端

插件卡片新增「用量与扣费」区块：范围选择（近 7/30/90 天）+ 四个合计格
（调用次数 / 输入词元（含缓存）/ 缓存命中率 / 扣费）+ 按账号与按模型两张表。
`credit` 常是 `0.03` 这种小数，展示按量级保留 2~4 位，避免固定两位都显示成 `0.00`。

### 验证

- 单测：`credit` 的「报 0 vs 未上报」区分、缓存字段提取优先级、日期桶按站点时区归属、
  空 usage 不虚增调用数、占位符兜底、`/usage` 聚合与排序、`days` 钳制。
- 端到端：`mock 上游 → 中继 → 下游字节 → 再查 /usage`，断言这次调用被正确计入
  （账号/模型/输入输出/缓存命中/credit 全对），证明「转发 → 记账」整条链路是通的。

## 14. 多账号选号策略：最少消耗优先

### 结论

**不是负载均衡（不是轮询）**，而是「**站点时区「今天」已消耗 credit 最少的可用账号**」。
消耗相同则取账号列表顺序靠前者 —— 保持确定性，便于排障与测试。

之所以不按"均分请求数"而是按"实际扣费"分摊：CodeBuddy 的计费单位是 credit，
不同模型倍率差几十倍（`deepseek-v4.1-flash` x0.03 vs `kimi-k3-1` x1.62），
按请求数均分照样会把额度烧得不均；按已消耗额度选号则天然把消耗拉平，也让每个账号更晚接近上限。

### 实现要点

- **权重只读内存，不查库**：选号在每个请求的热路径上，因此权重放在内存快照里
  （`creditDayUsed`），由后台每分钟的 ticker 刷新（`refreshCreditDaySnapshot`），
  再加上 `recordUsage` 的**即时累加** —— 否则"刚花掉的额度"要等一分钟才影响选号。
- **刷新前必须先落盘**（`flushUsage` → 再用 DB 的当日 SUM 重算），否则会把还没落盘的在途增量覆盖掉；
  即便如此仍对每个账号**取大合并**，防住"刷新与 recordUsage 并发"的窗口。
- **跨天自动清零**：权重按站点时区日期归属，`bumpCreditDay` / 刷新都会在发现日期变化时重置，
  避免把昨天的消耗算进今天。
- **快照未就绪时退化为「首个可用」**：统计缺失不影响可用性，只是暂时不均衡。
- **并发不做限制**（已确认）：不引入单账号在途请求上限，只依赖选号本身摊平。

### 失败换号与冷却（已实现）

上游返回**可重试错误**（HTTP 429/5xx、网络失败、SSE 流中途断流）时，不再直接 502 抛给调用方，
而是把该账号**标记冷却**（`cooldownUntil`，内存态，5 分钟）并**换下一个号重试**：

- 冷却中的账号不参与选号（`pickLeastConsumed` 与 `refreshFirstStaleAccount` 都会跳过），
  让配额/限流缓过去，避免同一账号被反复命中。
- 单次转发最多尝试 `maxAccountAttempts`（3）个账号，账号再多也不无限重试，防止拖垮延迟；
  全部失败或错误不可重试（4xx 等请求侧问题）时，把最后一次错误以 `upstream_error` 抛给调用方。
- 可重试判定通过 `*upstreamError` 类型化错误携带 `retryable` 标记；`chatCompletions`
  保证错误发生在写出任何下游字节之前，因此换号重试不会破坏已下发的响应。
- 400/401/403 等 4xx 不换号：那是请求或凭据侧问题，换号重发同样会失败（401 走既有 token 刷新路径）。

### 可观性

「用量与扣费」区块的范围选择里切到「**今天**」，按账号表里的「扣费」列**就是当前选号权重** ——
可以直接看出为什么这次走的是某个账号。

### 验证

- 单测：零消耗取列表序首个、消耗多者让位、同消耗回到列表序、跳过停用/过期/无 token 账号、
  全不可用返回 false、跨天清零、刷新不覆盖在途增量。
- 端到端：两个账号（各自 token）连发三次，断言送入上游的 `Authorization` 依次是
  `Bearer t1 → t2 → t1`（零消耗取首个 → t1 消耗后让位 → 拉平后回到列表序），
  并断言两次消耗都记在各自账号名下。**这才是"账号真的轮换了"的证据**，而不是只看选号函数。
