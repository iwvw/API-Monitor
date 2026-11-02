# 模型网关插件：Gemini CLI 方案

最后更新：2026-09-13
状态：**已实现**（上游协议层已实现；`upstreamImplemented = true`）

本文档给出「模型网关 → 插件中心」`geminicli` 插件的定位、上游协议事实、后端模块、
接口契约、接线点、前端、登记与卸载步骤。实现事实来源：
CLIProxyAPI 的 gemini-cli 实现（`internal/auth/gemini`、
`internal/runtime/executor/gemini_cli_executor.go`，commit `dd49a520`）。

---

## 1. 目标

把 **Google Gemini CLI**（Cloud Code Assist，`cloudcode-pa.googleapis.com` 的
`v1internal` 协议）封装成模型网关的一个 OpenAI 兼容上游：

- 插件内完成 gemini-cli 的 **OAuth 登录**与 **access token 自动刷新**，凭据落库；
- 模型目录为**静态目录**（7 个，来自 CLIProxyAPI 的 `models.json` gemini-cli 段），
  随代码发版更新；
- 对外暴露 `POST /api/geminicli/v1/chat/completions`（流式 + 非流式），
  按 workbuddy/ds2api 同样的方式**接入网关端点列表**，由网关统一计费/日志/路由；
- 前端在「模型网关 → 插件」页提供一张插件卡片：启用开关、OAuth 登录、账号列表、
  模型启停、网关接入、用量摘要。

与 **antigravity 插件**的关系：二者都访问 Google 的 Code Assist 体系，但使用
**不同的 OAuth client**（gemini-cli 自己的 CLI client），**额度桶相互独立**。
同一份 Google 账号可以在两个插件下各登一次，互不影响配额归属。

## 2. 移植策略

参考实现是 CLIProxyAPI 的内置 gemini-cli executor（Go）。本项目把其中的
**OAuth + Cloud Code Assist 协议事实原生实现**，不引入外部运行时。

| 参考实现 | 本项目对应 |
| --- | --- |
| CLIProxyAPI 内部 executor 注册 | 本项目 `Service` 直接实现 `http.Handler` |
| executor 的方法分派 | `ServeHTTP` 路径分派 |
| 凭据文件存储 | `geminicli_settings` 表内的账号数组（支持多账号） |
| 插件配置加载 | `manifest.go` 路由登记 + `server.go` 组装接线 |
| executor 自有模型结构 | 本项目自有 `ModelInfo` / `Account` / `AccountView` |

参照物是项目内的 **WorkBuddy 插件**（同为「账号池 → OpenAI 兼容」形态），
目录结构、表命名、link 语义、前缀语义、调用计数与用量口径全部对齐，
便于后续维护与统一卸载。

## 3. 上游协议事实清单

以下事实直接决定实现细节，**照抄不可改动**：

| 事实 | 说明 |
| --- | --- |
| OAuth client | gemini-cli 固定 CLI client（公开常量，非用户凭据）。**仓库不内置该凭据**：部署方需通过环境变量 `GEMINI_CLI_OAUTH_CLIENT_ID` / `GEMINI_CLI_OAUTH_CLIENT_SECRET` 提供，缺失时登录返回明确错误。取值来自官方 gemini-cli 安装包内的客户端常量 |
| Scopes | `https://www.googleapis.com/auth/cloud-platform`、`userinfo.email`、`userinfo.profile` |
| 授权端点 | `https://accounts.google.com/o/oauth2/v2/auth`，`response_type=code` + **PKCE S256** + `access_type=offline` + `prompt=consent` |
| Token 端点 | `https://oauth2.googleapis.com/token`（authorization_code 与 refresh_token 两种 grant） |
| 本地回调 | `DefaultCallbackPort = 8085`，路径 `/oauth2callback`，redirect 用 `http://localhost:8085/oauth2callback`（与 CLIProxyAPI 一致；授权码绑定该值，换成 127.0.0.1 会 `invalid_grant`）。**监听失败不是致命错误**：部署在远程服务器时浏览器到不了服务器的回环地址，此时降级为「粘贴回调地址」模式 |
| 登录会话 TTL | `loginTTL = 10` 分钟；过期或完成后关闭本地监听，释放端口。授权码**一次性消费**：取走后即标记，换 token 失败则丢弃会话，避免 poll 反复撞 `invalid_grant` |
| 回调兜底 | 远程部署场景：授权后浏览器跳到 `http://localhost:8085/oauth2callback?...` 打不开，用户把地址栏整条 URL 粘贴回面板（`POST /login/callback`），后端用会话里保存的 `code_verifier` 换 token |
| 用户信息 | `https://www.googleapis.com/oauth2/v1/userinfo?alt=json`（注意是 v1） |
| Code Assist 主机 | `https://cloudcode-pa.googleapis.com`，API 版本 `v1internal` |
| UA / Api-Client | `User-Agent: GeminiCLI/0.34.0/<model> (<GOOS>; <GOARCH>; terminal)`；`X-Goog-Api-Client: google-genai-sdk/1.41.0 gl-node/v22.19.0` |
| project_id 获取 | 主路径对齐 CatieCli：① 用 Cloud Resource Manager 列用户 GCP 项目（`filter=lifecycleState:ACTIVE`，优先含 `default`，否则第一个），选中后调 `serviceusage.googleapis.com` 启用 `cloudaicompanion` / `geminicloudassist` 服务；② 列不到项目时回退 CLIProxyAPI 的 `loadCodeAssist` / `onboardUser`（metadata 三字段、tier 取 `allowedTiers` 的 isDefault，缺省 `legacy-tier`）；③ 仍无则用兜底项目 `aicode-consumers`（登录时可手动填项目 ID 覆盖）。之所以以 Resource Manager 为主：不少个人账号的 `loadCodeAssist` 不返回项目，但其 GCP 项目在 Resource Manager 里可见 |
| 非流式对话 | `v1internal:generateContent`，请求体 `{"project","model","request":{...}}`，响应需从外层 `response` 字段解包 |
| 流式对话 | `v1internal:streamGenerateContent?alt=sse`，SSE 数据行外层同样需解包 |
| 用量字段 | 内层 `usageMetadata`：`promptTokenCount` / `toolUsePromptTokenCount` / `candidatesTokenCount` / `thoughtsTokenCount` / `cachedContentTokenCount` |

## 4. 后端模块

```
backend-go/internal/geminicli/
├── service.go     Service / Settings / New / 建表 / 设置读写 / ServeHTTP 分派 / 调用计数
├── upstream.go    ★ 上游协议层：OAuth 常量、PKCE、账号类型、loadCodeAssist/onboardUser/generate
├── auth.go        OAuth start/poll、token 刷新、账号 CRUD / 启用停用 / 选号 / 冷却
├── models.go      静态模型目录、启停（单模型与批量）
├── relay.go       /api/geminicli/v1：模型列表 + chat/completions（流式/非流式）
├── usage.go       实际用量（站点时区日聚合 × 账号 × 模型）、usage 接口
└── link.go        与网关端点列表的接入/断开/状态（openai_endpoints）
```

### 4.1 Settings（持久化于 `geminicli_settings.data`）

```jsonc
{
  "enabled": false,              // 总开关；关闭时上游调用全部拒绝
  "modelPrefix": "",             // 对外模型名前缀（如 "gcli-"），转发时剥离
  "proxyPoolId": "",             // 复用「代理池」插件的出口
  "disabledModels": [],          // 不对外提供的模型
  "accounts": [                  // OAuth 登录产生的凭据（含 token，仅服务端可见）
    {
      "id": "<email 或 projectID>", "email": "...", "nickname": "...", "projectId": "...",
      "accessToken": "...", "refreshToken": "...", "expiresAt": 1780000000,
      "disabled": false, "createdAt": "...", "lastRefreshAt": "...", "lastError": ""
    }
  ]
}
```

`Settings` 的 `Accounts` 字段**永不直接下发**给前端；对外一律经 `toAccountView()`
脱敏（不下发任何 token）。

### 4.2 数据表

| 表 | 用途 |
| --- | --- |
| `geminicli_settings` | 单行（`id = 1`）JSON 设置，含账号凭据 |
| `geminicli_call_stats` | 每账号累计调用次数（`identifier` 主键，逐条 UPSERT 累加） |
| `geminicli_usage_daily` | 实际用量，按「站点时区日期 × 账号 × 模型」聚合（主键 `day, account_id, model`） |

`openai_endpoints` 表复用既有结构（`plugin_id = 'geminicli'`），不新增列。

## 5. HTTP 接口契约

### 5.1 管理面 `/api/geminicli/*`（会话鉴权，`AuthSession`）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/geminicli/settings` | 读取插件设置（账号已脱敏） |
| PUT / POST | `/api/geminicli/settings` | 保存设置并生效（`accounts` / `disabledModels` 由后端持有，回传被忽略） |
| GET | `/api/geminicli/status` | `{enabled, upstreamReady, accountCount, availableCount, modelCount, linkBaseUrl}` |
| POST | `/api/geminicli/test` | 连通性自检 |
| GET | `/api/geminicli/models` | 模型目录（带前缀）与逐项 `enabled` |
| POST | `/api/geminicli/models/toggle/{id}` | 单模型启停（body `{enabled}`） |
| POST | `/api/geminicli/models/toggle-batch` | 批量启停（body `{enabled, models[]}`） |
| POST | `/api/geminicli/login/start` | 发起 OAuth → `{state, url, expiresAt}` |
| POST | `/api/geminicli/login/poll` | 轮询登录态 → `{status: pending\|success, account?}` |
| POST | `/api/geminicli/login/callback` | 粘贴回调 URL/授权码完成登录（body `{state, url}`）→ `{status:"success", account?}` |
| GET | `/api/geminicli/accounts` | 账号列表（脱敏 + 调用次数 + token 状态 + 可用性） |
| POST | `/api/geminicli/accounts/{id}/toggle` | 启用/停用（body `{disabled}`） |
| PUT | `/api/geminicli/accounts/{id}` | 修改备注名（body `{nickname}`） |
| POST | `/api/geminicli/accounts/{id}/refresh` | 立即刷新 access token |
| POST | `/api/geminicli/accounts/{id}/test` | 账号连通性探针 |
| DELETE | `/api/geminicli/accounts/{id}` | 删除账号 |
| GET | `/api/geminicli/usage?days=N` | 按账号/模型/日期聚合的实际用量（1~90 天，默认 7） |
| GET / POST / DELETE | `/api/geminicli/link` | 接入状态 / 接入 / 断开网关端点 |

> 说明：`/api/geminicli/models/toggle`（无路径参数）在当前后端实现里会因缺少模型 ID
> 返回 400；**单模型启停请走 `/models/toggle/{id}`**，与前端实现一致。

### 5.2 中继面 `/api/geminicli/v1/*`（`AuthInternal`，仅本机回环）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/geminicli/v1/models` | OpenAI 风格模型列表（带前缀、过滤停用项） |
| POST | `/api/geminicli/v1/chat/completions` | 对话转发（`stream` 决定流式/非流式） |

## 6. 路由与接线点

### 6.1 `backend-go/internal/manifest/manifest.go`

```go
{Prefix: "/api/geminicli", Module: "geminicli", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Gemini CLI 插件（Google Gemini CLI 反代）管理"},
{Prefix: "/api/geminicli/v1", Module: "geminicli-compatible", Owner: OwnerGo, Auth: AuthInternal, ResponseMode: ResponseStream, Description: "Gemini CLI 插件 OpenAI 兼容中继（仅本机内部网关调用）"},
```

### 6.2 `backend-go/internal/server/server.go`

- import `"github.com/iwvw/api-monitor/backend-go/internal/geminicli"`；
- 结构体字段 `geminicli *geminicli.Service`，构造 `geminicli: geminicli.New(cfg)`；
- 注入 `server.geminicli.SetProxyPoolSelector(server.proxypool)`；
- 启动 `server.geminicli.StartCallStatsFlush(warmupCtx)`；
- 转发 case `/api/geminicli`, `/api/geminicli/v1` → `s.geminicli.ServeHTTP`。

## 7. 前端

### 7.1 文件与注册

- 新增 `src/js/pages/openai/plugins/GeminiCliPlugin.jsx`；
- `src/js/pages/openai/OpenAIPluginsPanel.jsx` 的 `PLUGINS` 追加 `id: 'geminicli'` 条目
  （位于 `antigravity` 之后、`workbuddy` 之前），图标为组件内导出的内联 SVG `GeminiCliBrand`。

### 7.2 组件结构（单列 SectionCard 堆叠）

- **Gemini CLI**：启用中继、接入模型网关、模型前缀、运行状态（可用账号/账号总数/模型数）。
- **账号**：OAuth 登录按钮、账号表（启用开关 / 昵称或 email / projectId / 调用次数 /
  token 状态徽标 / 可用性 / 编辑 / 删除）。**不展示任何 token**。
- **OAuth 登录对话框**：`login/start` 拿 `url` 后 `window.open` 打开新标签，同时展示可点击
  链接；打开期间按 2 秒间隔 `login/poll`，`status === 'success'` 时刷新账号与状态并关闭。
  对话框另有「粘贴回调地址」输入框：把授权后地址栏里的完整链接（或授权码）贴入，
  调 `login/callback` 直接完成登录，覆盖远程部署时本地监听收不到回调的情况。
  还有「Google Cloud 项目 ID」输入框（可选）：账号没有自动分配 Cloud Code 项目时
  （`onboardUser` 返回 `cloudaicompanionProject: {}`），填一个已启用 Cloud Code/Gemini
  的 GCP 项目 ID 后重新发起授权即可。
- **模型**：id、上下文、输出上限、启用开关、全部启用/停用、表头全选。
- **用量**：近 N 天总量卡片 + 按账号 / 按模型表。

## 8. 登记与卸载

### 8.1 `docs/plugins/registry.json`

在 `plugins` 对象里新增 `geminicli` 条目，字段结构与 `workbuddy` 一致：
`name` / `version` / `status` / `kind` / `summary` / `backendDirs` / `manifest` /
`serverWiring` / `dbTables` / `dbColumnAdds` / `frontend` / `goDeps` /
`systemContracts` / `docs` / `uninstallSteps`。

### 8.2 卸载步骤

见 `docs/plugins/registry.json` 的 `plugins.geminicli.uninstallSteps`，要点：

1. `rm backend-go/internal/geminicli/`；
2. `manifest.go` 删除两个前缀行；
3. `server.go` 删除 import、字段、构造、注入、`StartCallStatsFlush`、转发 case、鉴权分支；
4. `DROP TABLE geminicli_usage_daily; DROP TABLE geminicli_call_stats; DROP TABLE geminicli_settings;`
5. `DELETE FROM openai_endpoints WHERE id = 'geminicli-internal';`
6. 删除前端组件并在 `OpenAIPluginsPanel.jsx` 移除注册条目；
7. 删除本文档。

## 9. 多账号与选号

选号目标：优先未停用、token 未过期、**不在冷却期**的账号；在候选里取
**累计调用次数最少者**（`callDisplay` 消耗均衡）。候选为空时退化为可用账号（忽略冷却）。

- `pickAccount(exclude)` 逐次排除已尝试账号，供转发路径换号重试；
- `inCooldown` / `setCooldown` / `clearCooldown` 维护纯内存冷却表
  （`cooldownUntil`，重启即清空），上游可重试失败后写入，转发成功时清除。

## 10. 限额与限流现状

- **账号冷却（已实现）**：上游返回可重试错误（429/5xx/网络/流中途断）时，把该账号写入
  60 秒冷却并换号重试；单次转发最多尝试 `len(accounts)+1` 次。
- **模型级限流（本插件目前未做）**：workbuddy 插件维护了「账号 × 模型」维度的限流簿，
  geminicli **尚未实现**这一层，只做账号级冷却。上游若按模型限流，当前表现为该账号
  整体冷却 60 秒，其余模型也会短暂让位。后续如需对齐 workbuddy，可新增独立的模型限流簿。
- **额度和账号可用性**：`AccountView.available` 由 `accountAvailable` 计算
  （未停用 + 有 access token + token 未过期）。

## 11. 用量与计费口径

- 只在收到上游 `usageMetadata` 时按「站点时区日期 × 账号 × 模型」记账；
  prompt = `promptTokenCount + toolUsePromptTokenCount`，
  completion = `candidatesTokenCount + thoughtsTokenCount`，
  cached = `cachedContentTokenCount`。
- 日期桶统一走 `internal/timeutil`（`LocationFromSettings` / `LocationFromName`），
  符合 CONTEXT.md 的时区规则。
- **无计费信息**：Gemini CLI 的 `usageMetadata` 不含货币或积分字段，因此本插件
  不做扣费换算，前端只展示调用次数与词元用量。

## 12. 验证方式

- 后端单测：`cd backend-go && go test ./internal/geminicli/...`；
- 前端静态检查：`npm run lint`、`npm run ui:governance`；
- 路由治理：`npm run governance:check`（`manifest.go` 两条前缀须与 `server.go` 接线一致）；
- 文档链接：`npm run docs:check`；
- 前端构建：`npm run build`。
- 端到端（需真实 Google 账号）：`login/start` → 浏览器授权 → `login/poll` 返回 `success`
  且账号出现在列表；启用插件并接入网关后，经网关 `/v1/chat/completions` 转发一次，
  确认 `usage` 表与页面「用量」区块出现记录。
- 远程部署（粘贴模式）：服务器上 8085 不可回环时，`login/callback` 用粘贴的 URL
  换取 token 并落库，账号出现在列表。

## 13. 尚未验证的部分

- 真实 Google 账号的 OAuth 回调与 `project_id` 获取依赖 8085 端口可监听；远程部署时
  改用 `login/callback` 粘贴模式。两者均需人工用真实账号验证，CI 无法覆盖。
- 模型级限流、多账号并发摊平的实际效果未做压测。
