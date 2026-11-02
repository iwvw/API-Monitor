# 模型网关插件：LobsterAI 方案

最后更新：2026-09-15
状态：**已实现**（上游协议层已补全；`upstreamImplemented = true`）

本文给出「模型网关 → 插件中心」新增 `lobsterai` 插件的接口设计、文件清单、接线点、登记与卸载步骤。
实现事实来源：[xinxinshuhao-create/lobsterai2api](https://github.com/xinxinshuhao-create/lobsterai2api)
（Go 反代）与配套部署文章 [qianling.pw/lobsterai2api](https://qianling.pw/lobsterai2api/)。

---

## 1. 目标

把**网易有道 LobsterAI（龙虾）**封装成模型网关的一个 OpenAI 兼容上游：

- 插件内完成 LobsterAI **OAuth 登录**（本地回环回调 + 远程粘贴回调两种路径）与
  **access token 自动刷新**，凭据落库；
- 模型目录**动态**来自上游 `GET /api/models/available`（失败回落静态表）；
- 额度与批次到期明细来自 `GET /api/user/profile-summary`；
- **每日自动签到**（`/api/client-activities`，+100 积分/号/天），站点时区 9 点与 21 点各一次；
- 对外暴露 `POST /api/lobsterai/v1/chat/completions`（流式 + 非流式），
  按同类插件的方式**接入网关端点列表**，由网关统一计费/日志/路由；
- 前端在「模型网关 → 插件」页提供一张插件卡片：启用开关、账号登录、账号表、积分明细、
  模型启停、用量统计、网关接入。

## 2. 移植策略

参考仓库是独立的 Go HTTP 服务（`lobsterai2api`，默认 `:8367`），本项目是**内嵌 Go 进程**架构，
`server.go` 直接组装各插件 Service，不存在外部插件宿主，因此**不托管外部进程**，改为按其记录的
上游协议事实**原生实现**（与 workbuddy/geminicli/ds2api 一致）。

| 参考仓库 | 本项目对应 |
| --- | --- |
| `cmd/server` HTTP 服务 | `Service` 直接实现 `http.Handler` |
| `internal/auth` 账号文件（`auths/lobsterai-<uid>.json`） | `lobsterai_settings` 表内的账号数组（支持多账号） |
| `internal/pool` 内存账号池 + `state.json` | `lobsterai_settings.accounts` + 内存冷却/调用计数 |
| `internal/upstream` 协议层 | `backend-go/internal/lobsterai/upstream.go` |
| `internal/scheduler` 定时器 | `backend-go/internal/lobsterai/scheduler.go`（cron + 站点时区 watcher） |
| `cmd/login` 本地回调登录 | `startLogin` / `startCallbackListener` / `completeLoginByPaste` |
| 外部 `checkin.py` 脚本 | 插件内建 `DailyCheckin` + 调度器（无需外部脚本） |

## 3. 上游协议事实清单（来自参考仓库与部署文章）

以下事实直接决定实现细节：

| 事实 | 说明 |
| --- | --- |
| 上游基址 | `https://lobsterai-server.youdao.com`（环境变量 `LOBSTERAI_UPSTREAM_BASE` 可覆盖） |
| 登录门户 | `https://lobsterai.youdao.com`（环境变量 `LOBSTERAI_LOGIN_PORTAL` 可覆盖） |
| 登录 URL | `{portal}/portal#/login?source=electron&redirect_uri=http://127.0.0.1:{port}/auth/callback&state={state}` |
| 本地回调 | 回调地址必须为 `http://127.0.0.1:{随机端口}/auth/callback`（登录页校验）；远程部署时粘贴回调链接 |
| 换取 token | `POST /api/auth/exchange`，body `{authCode, firstKeyfrom, latestKeyfrom, uuid, version}` |
| 刷新 token | `POST /api/auth/refresh`，body 为 keyfrom 载荷 + `refreshToken` |
| 会话终止 | 错误码 `40100` / `40101` 表示 refresh 被拒，需重新登录 |
| 对话转发 | `POST /api/proxy/v1/chat/completions`，头 `Authorization: Bearer <accessToken>`、`X-LobsterAI-Client-Capabilities: kimi-k3-agentic-v1`、`X-LobsterAI-Client-Version` |
| 强制流式 | 上游只支持流式，`stream:false` 会返回 500；转发前一律强制 `stream:true` |
| 模型目录 | `GET /api/models/available`，Bearer accessToken，返回 `modelId/modelName/provider/costMultiplier` |
| 额度明细 | `GET /api/user/profile-summary` 的 `totalCreditsRemaining` 与 `creditItems[]`（含 `expiresAt`）；`/api/user/quota` 只含 freeCredits，不用 |
| 每日签到 | `GET /api/client-activities/slot` → `GET /api/client-activities/{code}/context` → `POST /api/client-activities/{code}/actions/check_in`（body `{configRevision, idempotencyKey, payload}`） |
| 客户端版本 | 签到与 UA 需 `clientVersion`；官方更新接口 `api-overmind.youdao.com/.../lobsterai/prod/update` 可取，环境变量 `LOBSTERAI_CLIENT_VERSION` 可覆盖 |
| 积分规则 | 注册赠 300（14 天）、邀请赠 300（365 天）、每日签到赠 100（30 天） |
| 错误信封 | `{code, msg|message, data}`，`code != 0` 为业务错误；余额/限流文案按关键词归类 |

## 4. 后端模块

```
backend-go/internal/lobsterai/
├── service.go    Service / Settings / New / 建表 / 设置读写 / ServeHTTP 分派 / 调用计数 / 站点时区
├── upstream.go   ★ 上游协议层：常量、HTTP 客户端、clientVersion、登录、token、对话 SSE 聚合、模型目录、额度、签到
├── auth.go       脱敏视图、token 状态、登录入口、账号 CRUD、选号与冷却、额度/签到账号级操作
├── models.go     模型目录（动态缓存 + 静态回退）、启停（含批量）、前缀命名空间迁移
├── relay.go      /api/lobsterai/v1：模型列表 + chat/completions（流式/非流式，失败换号）
├── usage.go      实际用量（站点时区日聚合、缓存命中归一）、usage 接口
├── scheduler.go  每日签到调度（cron.WithLocation 站点时区 + TZ watcher）
└── link.go       与网关端点列表的接入/断开/状态（openai_endpoints）
```

### 4.1 Settings（持久化于 `lobsterai_settings.data`）

```jsonc
{
  "enabled": true,               // 总开关；关闭时上游调用全部拒绝
  "modelPrefix": "",             // 对外模型名前缀（如 "lobster-"），转发时剥离
  "proxyPoolId": "",             // 复用「代理池」插件的出口
  "disabledModels": [],          // 不对外提供的模型（对外名，含前缀）
  "autoCheckin": true,           // 每日自动签到开关
  "accounts": [                  // 登录产生的凭据（含 token，仅服务端可见）
    {
      "id": "<uid>", "nickname": "...", "userId": "...", "uuid": "...",
      "firstKeyfrom": "...", "latestKeyfrom": "...",
      "accessToken": "...", "refreshToken": "...",
      "expiresAt": 1780000000, "credits": 832.35,
      "disabled": false, "createdAt": "...", "lastRefreshAt": "...",
      "lastCheckinAt": "...", "lastError": ""
    }
  ]
}
```

`Settings` 的 `Accounts` 字段**永不直接下发**给前端；对外一律经 `toAccountView()` 脱敏（不下发任何 token）。

### 4.2 数据表

| 表 | 用途 |
| --- | --- |
| `lobsterai_settings` | 单行（`id = 1`）JSON 设置，含账号凭据 |
| `lobsterai_call_stats` | 每账号累计调用次数（`identifier` 主键，逐条 UPSERT 累加） |
| `lobsterai_usage_daily` | 实际用量，按「站点时区日期 × 账号 × 模型」聚合（主键 `day, account_id, model`） |

`openai_endpoints` 表复用既有结构（`plugin_id = 'lobsterai'`），不新增列。

## 5. HTTP 接口契约

### 5.1 管理面 `/api/lobsterai/*`（会话鉴权，`AuthSession`）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/lobsterai/settings` | 读取插件设置（账号已脱敏） |
| PUT | `/api/lobsterai/settings` | 保存设置并生效 |
| GET | `/api/lobsterai/status` | `{enabled, upstreamReady, accountCount, availableCount, coolingCount, modelCount, autoCheckin, linkBaseUrl}` |
| POST | `/api/lobsterai/test` | 连通性自检 |
| POST | `/api/lobsterai/login/start` | 发起登录 → `{state, url, expiresAt}` |
| POST | `/api/lobsterai/login/poll` | 轮询登录态 → `{status: pending\|success, account?}` |
| POST | `/api/lobsterai/login/callback` | 用粘贴的回调链接/授权码完成登录 |
| GET | `/api/lobsterai/accounts` | 账号列表（脱敏 + 调用次数 + token 状态） |
| DELETE | `/api/lobsterai/accounts/{id}` | 删除账号 |
| PUT | `/api/lobsterai/accounts/{id}` | 改备注名 |
| POST | `/api/lobsterai/accounts/{id}/toggle` | 启用/停用（停用即摘出转发与签到） |
| POST | `/api/lobsterai/accounts/{id}/refresh` | 立即刷新 token |
| POST | `/api/lobsterai/accounts/{id}/test` | 单账号连通性测试（额度查询探针） |
| POST | `/api/lobsterai/accounts/{id}/checkin` | 单账号立即签到 |
| GET | `/api/lobsterai/accounts/{id}/credits` | 单账号积分与批次到期明细 |
| POST | `/api/lobsterai/checkin` | 全部账号立即签到 |
| GET | `/api/lobsterai/models` | `{success, upstreamReady, models:[{id, name, provider, costMultiplier, enabled}]}`（`?refresh=1` 强制回源） |
| POST | `/api/lobsterai/models/toggle/{id}` | 单模型启停 |
| POST | `/api/lobsterai/models/toggle-batch` | 批量启停 |
| GET | `/api/lobsterai/usage` | 用量统计（`?days=1..90`） |
| GET | `/api/lobsterai/link` | 网关接入状态 |
| POST | `/api/lobsterai/link` | 接入网关端点列表 |
| DELETE | `/api/lobsterai/link` | 断开并从端点列表移除 |

### 5.2 中继面 `/api/lobsterai/v1/*`（`AuthInternal`，仅本机回环）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/lobsterai/v1/models` | 带前缀的对外模型列表（网关校验/刷新端点模型时调用） |
| POST | `/api/lobsterai/v1/chat/completions` | OpenAI 兼容对话（流式 + 非流式） |

网关接入后，`openai_endpoints` 里会出现一条固定端点：

- `id = "lobsterai-internal"`
- `name = "LobsterAI"`
- `base_url = "http://127.0.0.1:<PORT>/api/lobsterai/v1"`
- `api_key = "sk-lobsterai-internal"`（内部固定密钥，仅回环注入）
- `plugin_id = "lobsterai"`

## 6. 路由与接线点

### 6.1 `backend-go/internal/manifest/manifest.go`

在插件区块（`/api/geminicli` 之后）追加两条：

```go
{Prefix: "/api/lobsterai", Module: "lobsterai", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "LobsterAI 插件（网易有道龙虾转 OpenAI 兼容 API）管理"},
{Prefix: "/api/lobsterai/v1", Module: "lobsterai-compatible", Owner: OwnerGo, Auth: AuthInternal, ResponseMode: ResponseStream, Description: "LobsterAI 插件 OpenAI 兼容中继（仅本机内部网关调用）"},
```

### 6.2 `backend-go/internal/server/server.go`

| 位置 | 改动 |
| --- | --- |
| import 块 | `"github.com/iwvw/api-monitor/backend-go/internal/lobsterai"` |
| `Server` 结构 | `lobsterai *lobsterai.Service` |
| `newServer` 构造 | `lobsterai: lobsterai.New(cfg),` |
| 注入 | `server.lobsterai.SetProxyPoolSelector(server.proxypool)` |
| 后台任务 | `server.lobsterai.StartCallStatsFlush(warmupCtx)`、`server.lobsterai.StartCheckinScheduler(warmupCtx)` |
| `serveGoRoute` | `case "/api/lobsterai", "/api/lobsterai/v1": s.lobsterai.ServeHTTP(w, r)` |

> 中继面用 `AuthInternal`（仅回环），因此无需改 `AuthAPIKey` 的兼容模块白名单。

## 7. 前端

### 7.1 文件与注册

- 新增 `src/js/pages/openai/plugins/LobsterAIPlugin.jsx`
- `src/js/pages/openai/OpenAIPluginsPanel.jsx` 的 `PLUGINS` 数组追加：

```js
{ id: 'lobsterai', name: 'LobsterAI', description: '网易有道 LobsterAI（龙虾）转 API。', icon: LobsterAIBrand, detail: LobsterAIPlugin }
```

### 7.2 组件结构（单列 SectionCard 堆叠）

1. **LobsterAI**（配置）：启用中继、接入模型网关、每日自动签到、模型前缀、运行状态。
2. **账号**：`Table`（启用 / 账号 / 积分 / 调用 / token / 操作），顶部「全部签到」+「账号登录」。
   积分可点击查看批次明细；操作列含签到、刷新 token、编辑、删除。
3. **模型**：`Table`（启用 / 模型 / 来源 / 倍率），顶部「刷新目录」+ 批量启停。
4. **用量**：范围选择 + 四个合计格 + 按账号/按模型两张表。

对话框：账号登录（登录链接 + 粘贴回调 + 轮询状态）、编辑备注名、积分明细。

样式全部走 §「样式红线」：Kumo 组件 + `SectionCard`/`FieldRow`、无硬编码颜色、说明文字走 `title` 悬浮、
`getAuthHeaders()` 统一请求头、`toast` 统一提示、写操作后回读验证。

## 8. 登记与卸载

### 8.1 `docs/plugins/registry.json`

已追加 `lobsterai` 条目（`kind: "engine-plugin"`），记录后端目录、manifest 行、server 接线、
表、前端文件与注册点、`goDeps`（`github.com/robfig/cron/v3`，项目内已存在）、卸载步骤。

### 8.2 卸载步骤

见 `registry.json` 的 `uninstallSteps`。要点：删包 → 删 manifest 两行 → 删 server 接线 →
`DROP TABLE lobsterai_usage_daily / lobsterai_call_stats / lobsterai_settings` →
`DELETE FROM openai_endpoints WHERE id = 'lobsterai-internal'` → 删前端组件与注册条目 → 删本文档。

## 9. 实现状态与验证

### 后端（全部实现）

| 文件 | 内容 |
| --- | --- |
| `service.go` | Service/Settings、建表、设置读写、`ServeHTTP` 全路径分派、调用计数、站点时区缓存 |
| `upstream.go` | 上游协议层：clientVersion、登录（回环 + 粘贴）、exchange/refresh、强制流式对话与 SSE 聚合、模型目录、额度明细、每日签到、错误分类 |
| `auth.go` | 脱敏视图、token 状态、登录入口、账号 CRUD、选号（调用次数均衡）与失败冷却、额度/签到账号级操作 |
| `models.go` | 动态模型目录缓存（1 小时 TTL，失败回落静态表）、启停、前缀变更时端点命名空间迁移 |
| `relay.go` | `/v1/models`、`/v1/chat/completions`（流式透传 + 非流式聚合，失败换号，usage 记账） |
| `usage.go` | 用量按站点时区日聚合、缓存命中字段归一（`prompt_cache_hit_tokens` 等）、usage 接口 |
| `scheduler.go` | 每日签到调度（`cron.WithLocation(站点时区)` + 每分钟 TZ watcher） |
| `link.go` | 与 `openai_endpoints` 的接入/断开/状态 |

### 转发路径上的三个关键决定

1. **一律强制 `stream:true` 发上游**（上游对非流式返回 500）；客户端是否流式只决定本层是
   「边读边转发」还是「聚合成整包」。
2. **中继面恒发标准 OpenAI SSE 分帧**，流末补 `data: [DONE]`。
3. **失败换号**：429/5xx/网络失败/余额/限流 → 账号冷却（60s）并换下一个号；
   最多尝试「账号数 + 1」次，全部失败才把最后一次错误以 `upstream_error` 抛出。
   错误一定发生在写出任何下游字节之前，因此换号重试不破坏已下发的响应。

### 验证

- `go build ./...`、`go vet ./internal/lobsterai/ ./internal/server/ ./internal/manifest/` 通过。
- `go test ./internal/{lobsterai,manifest,server}` 通过。
- `npm run governance:check`（375 路由）、`npm run ui:governance`、`npx eslint` 通过。
- 单测覆盖：强制流式、SSE 聚合（content / usage / tool_calls / message 兜底）、错误分类、
  回调解析、前缀命名空间迁移、缓存字段提取、token 状态、日期桶按站点时区归属、签到默认开关等。

### 尚未验证的部分

上游调用是**按参考仓库记录的协议事实实现**的，尚未用真实 LobsterAI 账号跑通端到端。
首次联调建议顺序：① 账号登录 → ② `/api/lobsterai/models?refresh=1` 看目录 →
③ 直连 `POST /api/lobsterai/v1/chat/completions`（流式与非流式各一次）→
④ 接入网关后经 `/v1/chat/completions` 转发 → ⑤ 手动签到看积分变化。

## 10. 待确认的调优问题

1. **clientVersion 来源**：现优先环境变量 `LOBSTERAI_CLIENT_VERSION`，其次官方更新接口（缓存 1 小时），
   最后回退 `0.1.0`。若上游对版本有门槛，建议部署时显式配置。
2. **签到幂等**：`idempotencyKey` 用随机 UUID，同一天重复触发时依赖上游 `claimedToday` 判定；
   若上游对幂等键有更强约束，可改为按「账号 + 日期」派生。
3. **凭据存储位置**：现存在 `lobsterai_settings` 表的 JSON 里（单源最简）。
4. **选号策略**：现按累计调用次数最少选号（与 geminicli 一致）；若积分差异大，
   可改为「剩余积分最多优先」（参考仓库做法）。
5. **模型前缀默认值**：现默认空串（沿用上游模型名），避免与其它端点重名可设为 `lobster-`。
