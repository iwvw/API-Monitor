# AI Agent 管理模块 PRD

最后更新：2026-09-17

> 变更说明（实现期调整，2026-09-19）：实例模型已从「实例归属单个用户」改为
> 「平台实例 + 用户授权」。实例由面板管理员统一登记，AI Agent 用户通过
> `aiagent_instance_grants` 被逐个授权可用实例，**默认不授权**。下文凡涉及
> 「实例归属/分配到用户名下/唯一约束 (user_id, server_id, port)」的描述均已
> 被此模型取代，详见 [ADR-0004](../adr/0004-AIAgent管理模块架构决策.md)。

## Problem Statement

API Monitor 目前管理的是基础设施与会话型服务（主机、云厂商、模型网关、DNS、订阅等），但没有一个模块用来管理「跑在各主机上的 AI 编码 Agent」。这类 Agent（OpenCode 已在使用，后续还有 Pi、Codex CLI、Claude Code 等）具有共同特征：以本地进程形式运行在开发者机器上，暴露一个本地 HTTP/SSE 服务端口，承载项目、会话、消息、任务等数据。

现状带来的问题：

1. 用户想在手机上继续电脑上未完成的 AI 编码工作，只能把本机端口通过 Cloudflare 等反向代理暴露到公网。这需要公网入口、逐端口配置，且把开发者本机直接暴露在公网，安全面过大。
2. 没有统一的「账号 + 实例」视图。多台机器（个人电脑、工作电脑）各自为政，切换靠手工改配置，没有一个入口能列出「我的机器」并一键切换。
3. 没有面向外部客户端（手机 App、另一台电脑上的客户端）的登录机制。现有 API Monitor 鉴权是「单管理员 session + API Key + Agent Key」，不适合「一个普通用户登录后只看到自己的实例」这种场景。
4. 第一次适配了 OpenCode 之后，如果按 OpenCode 专有实现写死，后续每接一个 Agent（Pi、Codex、Claude Code）都要重做一遍，维护成本线性增长。
5. 数据一致性依赖各客户端各自的本地存储（例如 OpenCode UI 的项目列表存在浏览器 localStorage），换设备、换网络就不同步。

用户需要的是：**一个通用的 AI Agent 管理模块**——管理员在面板里建用户；用户在任意客户端输入域名、用户名、密码即可长期登录；登录后看到自己名下的多台机器上的 Agent 实例并随时切换；所有访问都经面板中转，不需要暴露本机端口；并且这套能力要能平滑扩展到 OpenCode 之外的其它 AI Agent。

## Goals

1. 在 API Monitor 新增独立 `aiagent` 模块，作为「跑在各主机上的 AI Agent」的统一管理入口，位于前端侧边栏「API 服务 -> AI Agent」。
2. 提供模块自带的用户体系：管理员在面板创建/禁用用户、设置与重置密码；用户不注册、不依赖面板管理员账号。
3. 提供长期令牌登录：客户端填写域名 + 用户名 + 密码换取一个长期令牌，持久化后无需频繁登录；令牌可吊销、每设备独立。
4. 提供实例管理：管理员为用户名下的主机分配实例；用户可在已分配的主机上自助增删实例（指定 Provider、端口、名称）。把一台新主机首次纳入清单属于管理员动作。
5. 提供 Agent 原生流通道网关：通过主机 Agent 的主动出站长连接，把本机 Agent 服务的 HTTP + SSE 反向暴露给云端客户端，**不占用公网端口、不依赖 Cloudflare 反代**。
6. 以 Provider 抽象适配多种 AI Agent（OpenCode 为第一个 Provider），新增 Provider 不改变网关、用户、令牌、实例这些通用能力。
7. 实例在线状态以「Agent 进程真的在跑」为准（进程探测 + 端口探测），而不是仅看主机是否在线。
8. 保持与现有 Go Manifest 后端、React + Kumo 前端、SQLite 数据层、Agent（Rust）与模块治理方式一致。

## Non-Goals

1. 第一版不做用户自助注册（注册入口后续再评估），用户一律由管理员创建。
2. 不做多租户 RBAC、团队/组织、跨用户共享实例。
3. 不代理或计量模型调用。模型调用已经走模型网关，与本模块无关；本模块只转发 Agent 的本地 HTTP/SSE 服务数据。
4. 不做 Agent 进程的启动/停止/重启管理（第一版只做探测与转发；进程生命周期后续评估）。
5. 不做会话、消息、项目内容的服务端存储。数据的唯一事实来源始终是各机器上的 Agent 自身；本模块只持久化「用户、令牌、实例登记、实例级 UI 元数据」。
6. 不做公网直连入口（Cloudflare Tunnel / 公网端口）作为第一版通道；原生流通道之外的传输方式仅保留为未来备选。
7. 不包含具体 Agent 的业务功能复刻（例如不重写 OpenCode 的会话界面），客户端仍使用各自 Agent 的原生 UI。

## Target Users

1. 同时使用多台开发机（个人电脑 + 工作电脑）的开发者，希望手机上随时接续工作。
2. 不在同一局域网、也不想暴露公网端口的自托管用户。
3. 需要把 AI 编码 Agent 纳入统一面板管理、并期望未来接入多种 Agent 的运维者。
4. 希望有一个独立于面板管理员账号、可供普通使用者登录的账号体系的团队。

## Success Metrics

1. 管理员可在面板创建用户 `salen` 并设置密码。
2. 客户端输入域名 + 用户名 + 密码后登录成功，并拿到长期令牌；重启客户端无需重新登录。
3. 用户可添加两台及以上实例（个人电脑、工作电脑），在客户端一键切换。
4. 当且仅当目标机器上的 Agent 进程正在运行时，该实例显示在线；进程停止后状态在探测周期内变为离线。
5. 通过面板即可访问本机 Agent 的 HTTP + SSE 服务，且**没有开启任何公网端口、没有使用 Cloudflare 反向代理**。
6. 长耗时流式响应（模型生成中）能通过网关稳定传输，不出现整段延迟到生成结束才返回的现象。
7. 新增一个 Provider（例如 Pi）只需增加 Provider 定义与探测规则，网关/用户/令牌/实例代码无需修改。
8. 接入后不破坏 Go manifest、前端导航、路由治理与审计命令。

## Solution

新增独立模块 `aiagent`，中文入口名「AI Agent」，位于前端侧边栏「API 服务」分组。

1. 后端新增 `backend-go/internal/aiagent/`：模块用户、令牌、实例、实例元数据、访问日志，以及网关与 Agent 通道桥接。
2. 主机 Agent（`agent-rust/`）新增两项能力：`aiagent_probe_v1`（运行时进程探测）与 `aiagent_stream_v1`（HTTP/SSE 原生流数据通道），通过既有的 Engine.IO 长连接与一次性 token 反连机制工作。
3. 网关以 `ANY /api/aiagent/gw/{instanceId}/*` 形式暴露，鉴权用模块令牌，按归属校验后经 Agent 通道转发到目标机器的 `127.0.0.1:<port>`，响应逐块 flush 以支持 SSE。
4. 前端新增 `src/js/pages/aiagent/`：管理员面（用户管理、实例分配、令牌吊销）与用户面（我的实例、接入信息、实例元数据）。
5. Provider 以静态注册表 + 可选落库配置实现，第一版内置 `opencode`，预留 `pi`、`codex`、`claude-code`。
6. 客户端（如 OpenCode UI）以「服务器地址 + Bearer 令牌」接入模块网关，把实例当作一个可切换的服务器。

## Product Scope

### Phase 1：模块骨架与用户令牌体系

- `aiagent` 模块注册（后端 manifest/route_handlers/server.go、前端 store.js/MainLayout.jsx/Icons）
- 数据表：`aiagent_users`、`aiagent_tokens`、`aiagent_instances`
- 管理员面：用户增删改、重置密码、禁用/启用；实例分配
- 用户面：登录换取长期令牌、令牌吊销、列出我的实例
- 密码 bcrypt 存储、登录限流与失败锁定

### Phase 2：Agent 探测与实例在线状态

- Agent 新增 `aiagent_probe_v1` 能力与探测任务
- 进程探测（按 Provider 的进程名）+ 端口探测（本机回环端口监听）
- 实例列表返回在线/离线、Agent 版本、进程 PID、最后探测时间
- Provider 注册表：`opencode`/`pi`/`codex`/`claude-code` 的默认端口、进程名、探测规则

### Phase 3：原生流通道与网关

- Agent 新增 `aiagent_stream_v1` 能力与数据通道任务
- 云端 HTTP → 原生流字节管道 + SSE 透传（逐块 flush、写超时续期）
- 网关路由 `ANY /api/aiagent/gw/{instanceId}/*`，令牌 + 归属校验
- 一次性 token 反连（沿用既有终端流 broker 模式）

### Phase 4：客户端接入与实例元数据

- 实例级 UI 元数据读写（`aiagent_instance_meta`）
- 接入信息生成（客户端可用的网关地址、Provider 提示）
- 实例启停用（登记层面）、排序、备注
- 首个真实使用方 OpenCode UI 的账号登录与实例切换（在对应仓库实施，本模块提供接口）

### Phase 5：多 Provider 与使用观察（后续）

- 更多 Provider 的探测与接入参数（自定义进程名、自定义健康路径）
- 实例维度的只读使用观察（连接数、请求量、最后活跃时间），不含模型用量（模型用量属模型网关）
- 审计报表与访问日志检索

## User Stories

1. 作为管理员，我想在面板中创建用户并设置密码，以便把访问权限交给使用者而不共享管理员账号。
2. 作为管理员，我想重置用户密码或禁用用户，以便在凭证泄露时快速处置。
3. 作为管理员，我想把一个实例分配到某个用户名下，以便用户无需自己配置就能看到机器。
4. 作为使用者，我想用域名 + 用户名 + 密码登录，以便在任意客户端开始工作。
5. 作为使用者，我想登录后长期有效，不必每次打开客户端都输密码。
6. 作为使用者，我想在客户端里看到自己所有机器上的 Agent 实例，以便快速切换。
7. 作为使用者，我想在管理员已分配给的主机上自助添加实例（选 Provider、端口、起名字）。
8. 作为使用者，我想清楚知道某个实例当前是否在线、Agent 进程是否在运行。
9. 作为使用者，我想通过面板访问某台机器上的 Agent 服务，而不用在这台机器上开公网端口。
10. 作为使用者，我想在手机上继续电脑上未完成的会话，看到实时流式输出。
11. 作为使用者，我想让某台旧设备上的登录失效（吊销令牌），以降低风险。
12. 作为使用者，我想让不同客户端看到一致的实例列表与偏好，以便多端无缝切换。
13. 作为维护者，我想新增一个 AI Agent 类型时只改 Provider 定义与探测规则，避免动网关与账号代码。
14. 作为维护者，我想所有写操作都有审计日志，便于排查与追责。

## Functional Requirements

### 1. 模块入口与导航

- 模块 ID：`aiagent`
- 前端导航文案：`AI Agent`
- 分组位置：`API 服务`（`api-gateway`）
- 路由路径：`/aiagent`
- 页面目录：`src/js/pages/aiagent/`，入口 `src/js/pages/AiAgentPage.jsx`
- 命名注意：`AI Agent` 一词在接口文档页已被用作 API Key 类型标签（`src/js/pages/apidocs/constants.js`），本模块使用同一词组作为**模块名**，不影响 Key 类型展示；文档与 UI 文案需以「AI Agent 管理」明确语境。

### 2. 模块用户管理（管理员面，走面板 session 鉴权）

字段：

- `username`：登录名，唯一，仅允许字母数字与 `-` `_` `.`，长度 3-32
- `password`：管理员设置，最短 8 位，仅写入时使用，落库 bcrypt
- `displayName`：显示名，可选
- `disabled`：禁用位

规则：

- 用户名唯一，创建时校验；禁用后不可登录，已签发令牌立即失效（校验时检查用户状态）
- 密码只以 bcrypt 哈希落库（沿用 `internal/auth` 的 bcrypt 基线），任何接口不回显密码
- 用户删除前需检查其名下实例与令牌，删除需强确认（Kumo `DeleteResource`）
- 管理员不能通过接口读取用户明文密码，只能重置

### 3. 模块令牌（长期令牌）

- 登录：`POST /api/aiagent/auth/login`，入参 `username` + `password`，成功后签发一个长期令牌
- 令牌形态：随机 32 字节，十六进制或 base62；返回给客户端一次，服务端只存 SHA-256 哈希与展示用前缀
- 有效期：默认 180 天，可由配置调整；**不设刷新令牌**（决策见 ADR-0004）
- 持久化：客户端保存令牌并在每次请求携带；服务端按 `last_used_at` 记录活跃
- 吊销：管理员或用户可吊销单个令牌；吊销后立即失效
- 设备标签：登录时可带 `deviceLabel`（如「iPhone」「工作本」），仅用于展示
- 登录限流：按用户名 + IP 维度限流，连续失败达阈值后短时锁定

### 4. 实例管理

字段：

- `server_id`：关联主机（复用 `server_accounts` 中已装 Agent 的主机）
- `provider`：Agent 类型（`opencode` / `pi` / `codex` / `claude-code`）
- `label`：实例名称，如「家里台机」「工作本」
- `port`：Agent 本地服务端口，**每实例可配**，默认取 Provider 默认端口（OpenCode 默认 `4096`）
- `enabled`：启用位（禁用后网关拒绝转发）

授权关系：

- `aiagent_instance_grants(instance_id, user_id)`：用户可用的实例集合，多对多
- **默认不授权**：新建实例不自动开放，需在用户管理里逐个勾选

规则：

- 实例是平台共享资源，不归属用户；由面板管理员统一登记
- 用户只能看到并操作被授权的实例；管理员可看到全部
- 同一 `(server_id, provider)` 不允许重复登记（实例已是平台资源，不按用户区分）
- 端口第一版仅允许 Provider 默认端口（避免网关变成主机任意本地服务的转发器）
- **主机纳管**：把一台新主机首次纳入清单属于管理员动作；用户不可枚举或探测未被授权实例涉及的主机（跨租户隔离）
- 新增实例必须校验主机上的 Agent 在线（否则允许登记但提示离线）
- 实例在线判定：Agent 在线 **且** 探测返回该 Provider 进程正在运行（见第 6 节）

### 5. 实例元数据（多端一致）

- `GET/PUT /api/aiagent/instances/{id}/meta`，读写一份 JSON
- 用途：客户端把「项目钉选、项目排序、隐藏目录、上次打开的会话」等纯 UI 偏好存到账号级，保证多端一致
- 大小限制（如 64KB），服务端不解析内容，只做所有权校验与存取
- 决策：客户端本地 localStorage 在账号模式下让位于本接口

### 6. 实例在线状态与进程探测

- Agent 新增能力 `aiagent_probe_v1`
- 探测内容：本机回环端口是否监听、按 Provider 规则匹配的进程是否存在（进程名/PID）、可用时附带 Agent 版本
- 探测触发：模块按需触发（列表接口带缓存 TTL，如 15 秒），避免高频轮询
- 返回结构：`online`（进程在跑且在监听）、`portListening`、`processRunning`、`pid`、`startedAt`、`probedAt`
- 缓存与降级：Agent 离线时实例状态为「主机离线」；探测失败为「状态未知」，与「进程未运行」区分展示
- Provider 探测规则：
  - `opencode`：进程名匹配 `opencode`（含 `opencode.exe`），默认端口 `4096`
  - `pi`、`codex`、`claude-code`：第一版只登记规则占位（默认端口/进程名可配置），未实测前 UI 标注「未验证」

### 7. 原生流通道网关

- 路由：`ANY /api/aiagent/gw/{instanceId}/*`
- 鉴权：`Authorization: Bearer <模块令牌>`；SSE/WebSocket 这类无法自定义头的场景使用一次性短令牌（见第 8 节）
- 校验链：令牌有效 → 用户启用 → 实例存在且属于该用户 → 实例启用 → Provider 允许该路径 → Agent 在线
- 转发：云端为每个请求创建一条一次性数据通道，Agent 反连后把本机 `127.0.0.1:<port>` 的字节双向搬运；云端在通道之上使用 `http.Transport` + `httputil.ReverseProxy`，因此获得原生 HTTP 语义
- SSE：反向代理 `FlushInterval = -1`，响应头到达即返回、响应体逐块写出，禁止整体缓冲；写超时通过 `sseutil.RenewWriteDeadline` 续期
- 方法透传：GET/POST/PUT/PATCH/DELETE 全部支持；请求体与响应体（含流）原样透传
- 头部处理：过滤 hop-by-hop 头与 `Host`，剥离 `Authorization`（云端口令不下发到目标 Agent）
- 路径映射：网关前缀之后的路径原样拼到目标 base（Provider 决定 base，默认根路径）

### 8. 一次性短令牌（流式场景）

- `POST /api/aiagent/gw/{instanceId}/stream-token`（Bearer 鉴权）→ 返回 30 秒 TTL、一次性消费的短令牌
- 客户端在 `EventSource` / WebSocket URL 上携带 `?st=<短令牌>`
- 服务端在连接建立时原子校验并消费该令牌，之后连接持续有效
- 目的：避免长期令牌出现在 URL、访问日志、浏览器历史中

### 9. Provider 抽象

- Provider 注册信息：`id`、`label`、`defaultPort`、进程匹配规则、健康/探测方式、网关 base 路径、流式协议（SSE）
- 第一版内置 `opencode`；`pi`/`codex`/`claude-code` 以占位登记，可配置可用性开关
- 网关与账号体系不感知 Provider 细节，仅按 Provider 决定 base 路径与探测规则
- 新增 Provider 的改动面：Provider 定义 + 探测规则 + （如有）客户端接入提示；不含用户/令牌/实例/网关逻辑

### 10. 访问日志与审计

- 记录：登录（成功/失败）、令牌签发/吊销、用户增删改、实例增删改、网关转发（实例、方法、路径、状态码、耗时、字节数）
- 不记录：请求体/响应体内容、令牌明文、密码
- 保留期与清理沿用面板既有日志策略

### 11. Agent 侧能力

- `aiagent_probe_v1`：进程/端口探测任务
- `aiagent_stream_v1`：HTTP/SSE 数据通道任务
- 两能力随注册上报，服务端用 `requireAgentCapability` 做版本门禁；不支持时给出「请升级 Agent」的明确提示
- 复用既有一次性 token 反连模式；通道为字节管道，HTTP 语义交给标准库

## UX Requirements

1. 模块页面分两个视角：管理员面（用户/实例/令牌管理）与用户面（我的实例/接入信息）；由面板 session 与模块令牌区分，同一页面按权限呈现。
2. 实例列表以卡片或表格呈现：名称、Provider、主机、端口、在线状态（进程在跑/端口监听/离线/未知）、最后探测时间。
3. 在线状态用语义色：在线（成功）、进程未运行（警示）、主机离线（中性）、未知（次要）。
4. 每个实例提供「接入信息」弹层：客户端应填写的网关地址、Provider、端口、备注，可一键复制。
5. 高风险操作（删除用户、删除实例、吊销令牌）走 Kumo `DeleteResource` 确认。
6. 密码字段只在创建/重置时出现；列表只显示「已设置」。
7. 空态、加载态、错误态齐备；加载占位用 Kumo `SkeletonLine`/`Loader`。
8. 响应式：窄屏下实例卡片堆叠，管理员操作收进更多菜单。
9. 全中文文案，技术标识符（Provider 名、端口、路径）保留英文。

## 前端设计细节

### 1. 页面结构

- `src/js/pages/AiAgentPage.jsx`（壳）→ `src/js/pages/aiagent/AiAgentConsole.jsx`（主容器）
- 主容器用 Kumo `Tabs`：`我的实例` / `实例接入` /（管理员可见）`用户管理` / `访问日志`
- 子组件：`InstanceCard.jsx`、`InstanceDialog.jsx`、`AccessInfoDialog.jsx`、`UserTable.jsx`、`UserDialog.jsx`、`TokenTable.jsx`

### 2. 模块注册接线

- `src/js/store.js`：`MODULE_CONFIG.aiagent` + `MODULE_GROUPS` 的 `api-gateway.modules` 追加（顺序决定侧栏顺序），`DEFAULT_MODULE_VISIBILITY` 按需默认隐藏
- `src/js/components/MainLayout.jsx`：`lazy()` 导入 + `renderActivePage()` case + 视需要加入 `stickyHeaderScrollModule`
- `src/js/components/Icons.jsx`：`MODULE_ICON_MAP` 加图标（无品牌图标时用现有兜底）
- 设置页模块显隐由 `DEFAULT_MODULE_ORDER` 自动派生，无需改动

### 3. 语义列与组件约束

- 业务表走 `AppTable columns` 语义角色（primary/status/datetime/actions-*）
- 状态列用语义色 token，不硬编码颜色
- 复制长串用 `ClipboardText`
- 表单用 Kumo `Input`/`Select`/`Switch`/`Checkbox`/`Button`；不对原生表单元素做样式覆盖
- 遵守《Kumo UI 规则》与《前端布局约定》，例外登记到《重构验证与例外清单》

### 4. 数据流

- 管理员面接口走同源 session cookie（面板登录态）
- 用户面接口在客户端以 Bearer 令牌调用；面板内为便于演示与自测，提供「以当前用户身份预览」区块（可选）
- 网关地址由后端根据面板公开地址 + 实例 ID 生成，避免前端手工拼接

## 数据库设计

新增五张表，表名统一 `aiagent_` 前缀，全部幂等 `CREATE TABLE IF NOT EXISTS`。

### `aiagent_users`

```sql
CREATE TABLE IF NOT EXISTS aiagent_users (
  id            TEXT PRIMARY KEY,            -- usr_<random>
  username      TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,               -- bcrypt
  display_name  TEXT,
  disabled      INTEGER NOT NULL DEFAULT 0,
  created_at    TEXT NOT NULL,               -- RFC3339 UTC
  updated_at    TEXT NOT NULL,
  last_login_at TEXT
);
```

### `aiagent_tokens`

```sql
CREATE TABLE IF NOT EXISTS aiagent_tokens (
  id           TEXT PRIMARY KEY,             -- tok_<random>
  user_id      TEXT NOT NULL,
  token_hash   TEXT NOT NULL,                -- sha256(token)
  token_prefix TEXT NOT NULL,                -- 展示用前缀
  device_label TEXT,
  expires_at   TEXT NOT NULL,
  revoked_at   TEXT,
  last_used_at TEXT,
  last_ip      TEXT,
  created_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_aiagent_tokens_user    ON aiagent_tokens(user_id);
CREATE INDEX IF NOT EXISTS idx_aiagent_tokens_prefix  ON aiagent_tokens(token_prefix);
```

### `aiagent_instances`

```sql
CREATE TABLE IF NOT EXISTS aiagent_instances (
  id         TEXT PRIMARY KEY,               -- inst_<random>
  server_id  TEXT NOT NULL,                  -- 关联 server_accounts.id
  provider   TEXT NOT NULL,                  -- opencode|pi|codex|claude-code
  label      TEXT NOT NULL,
  port       INTEGER NOT NULL,
  enabled    INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_aiagent_instances_server ON aiagent_instances(server_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_aiagent_instances_unique
  ON aiagent_instances(server_id, provider);
```

### `aiagent_instance_grants`

```sql
CREATE TABLE IF NOT EXISTS aiagent_instance_grants (
  instance_id TEXT NOT NULL,
  user_id     TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  PRIMARY KEY (instance_id, user_id)
);
CREATE INDEX IF NOT EXISTS idx_aiagent_instance_grants_user ON aiagent_instance_grants(user_id);
```

### `aiagent_instance_meta`

```sql
CREATE TABLE IF NOT EXISTS aiagent_instance_meta (
  instance_id   TEXT PRIMARY KEY,
  user_id       TEXT NOT NULL,
  metadata_json TEXT NOT NULL DEFAULT '{}',
  updated_at    TEXT NOT NULL
);
```

### `aiagent_access_logs`

```sql
CREATE TABLE IF NOT EXISTS aiagent_access_logs (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id       TEXT,
  token_id      TEXT,
  instance_id   TEXT,
  action        TEXT NOT NULL,               -- login|logout|token.issue|token.revoke|gw|...
  result        TEXT NOT NULL,               -- ok|denied|error
  status_code   INTEGER,
  error_summary TEXT,
  ip            TEXT,
  user_agent    TEXT,
  created_at    TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_aiagent_access_logs_created ON aiagent_access_logs(created_at);
CREATE INDEX IF NOT EXISTS idx_aiagent_access_logs_user    ON aiagent_access_logs(user_id);
```

约定：

- 时间列一律 RFC3339 UTC 字符串写入；涉及日历边界的聚合（如日志按天）经 `internal/timeutil`
- 不建跨模块外键；`server_id` 仅存字符串，存在性由服务层校验
- 迁移用 `PRAGMA table_info` + `ALTER TABLE ADD COLUMN` 幂等处理

## 后端技术设计

### 1. 模块结构与接线

```text
backend-go/internal/aiagent/
├── service.go            # Service + New + open() + ServeHTTP 路径分发
├── schema.go             # 五张表幂等 DDL + 列迁移
├── user_store.go         # 模块用户 CRUD + bcrypt
├── token_store.go        # 令牌签发/校验/吊销 + 限流
├── instance_store.go     # 实例 CRUD + 归属校验
├── meta_store.go         # 实例元数据读写
├── providers.go          # Provider 注册表（opencode/pi/codex/claude-code）
├── gateway.go            # 网关：HTTP→帧桥 + SSE 透传 + 一次性短令牌
├── agent_bridge.go       # 与 serveragent 的探测/通道交互封装
├── access_log.go         # 访问日志落库
├── types.go              # Account/Payload/Normalized* 结构
└── service_test.go
```

接线：

- `manifest.go`：登记 `/api/aiagent`（`AuthSession` 前缀用于管理员面）+ 逐条子路由；用户面与网关路由使用 `AuthPublic` 并在 handler 内做模块令牌校验
- `server.go`：import、`Server` 字段、`newServer()` 实例化、`serveGoRoute()` case
- `route_handlers.go`：`moduleHandlers` 增加 `aiagent` 一行
- 分组登记按《新模块接入指南》的六处清单（manifest、route_descriptions、api_docs_catalog、route_aliases、route_contracts、分组两处）

### 2. 鉴权模型

- 管理员面：`AuthSession`，沿用面板登录态
- 用户面与网关：`AuthPublic` + handler 内 `Authorization: Bearer <模块令牌>` 校验（避免污染通用 session 分支）
- 一次性短令牌：内存 broker，30 秒 TTL、单次消费、绑定 instanceId 与 userId
- 令牌校验中间件：解析 Bearer → SHA-256 → 查 `aiagent_tokens` → 校验未吊销、未过期、用户启用 → 返回上下文（userID、tokenID）

### 3. 密码与令牌安全

- 密码 bcrypt（cost 与现有 `internal/auth` 一致），禁止明文落库与日志
- 令牌只存 SHA-256 哈希 + 展示前缀；明文仅在签发响应中出现一次
- 登录限流：按 `username` 与来源 IP 计数，失败达阈值锁定若干分钟
- 恒定时间比较，避免时序侧信道

### 4. 网关实现

- 前缀剥离后得到目标路径；按 Provider 组装目标 `http://127.0.0.1:<port><path>`
- 通过 Agent 数据通道发送请求帧；读取响应帧并逐块写入 `http.ResponseWriter`
- 强制 `FlushInterval = -1` 语义：每次写入后 `Flusher.Flush()`
- 写前 `sseutil.RenewWriteDeadline`，心跳按需注入
- 连接关闭、客户端断开、Agent 掉线三种情况都要正确终止并记录

### 5. Agent 桥接（复用 serveragent 能力）

- 探测：走 `serveragent` 的通用任务下发（新增 task type），同步等待结果
- 通道：复用 `terminal_stream.go` 的一次性 token broker 模式：先经控制通道下发「建立数据通道」任务（携带一次性 stream token），Agent 反连独立的 WebSocket，云端完成 HTTP↔帧 的双向桥接
- 通道：云端把数据通道包装成 net.Conn 交给 http.Transport，逐块转发；不自定义 HTTP 或帧协议

### 6. 错误处理

- 统一 `response.OK` / `response.Error`；错误码集中在 `types.go`
- 面向客户端的网关错误：401（令牌无效）、403（归属不符/实例禁用）、404（实例不存在）、409（主机离线）、502（Agent 通道异常）、504（超时）
- 面向管理员面的错误：参数校验、唯一冲突、删除前置检查

### 7. Provider 注册表

- `providers.go` 定义结构体切片，字段：`ID`、`Label`、`DefaultPort`、`ProcessMatch`（进程名正则/子串）、`BasePath`、`Streaming`（`sse`）
- 第一版：`opencode` 完整；`pi`/`codex`/`claude-code` 占位并带 `Verified: false`
- 网关与探测全部读注册表，不写死 Provider 分支

## Agent 侧设计（agent-rust）

### 1. 能力位

- `agent_capabilities()` 增加 `aiagent_probe_v1` 与 `aiagent_stream_v1`
- 服务端以 `requireAgentCapability` 门禁；缺失时提示升级 Agent

### 2. 探测任务（新增 task type）

- 入参：`port`、`provider`、`processMatch`
- 实现：本机回环端口监听检测 + 进程匹配（Windows 走进程枚举，Linux 走 `/proc` 或 `ps`）
- 出参：`portListening`、`processRunning`、`pid`、`startedAt`（可获取时）、`agentVersion`
- 只读、无副作用，超时短（如 5 秒）

### 3. 数据通道任务（新增 task type）

- 控制通道先下发「建立数据通道」任务，携带目标端口与一次性 `stream_id` + `stream_token`
- Agent 先连接本机 `127.0.0.1:<port>`（连不上即失败），再主动反连云端数据通道端点 `/ws/agent-port`（参照 `handle_pty_start_v2` 的模式）
- 连接建立后进入双向字节搬运：本地读取 → WebSocket 二进制帧；WebSocket 二进制帧 → 本地写入
- 长连接任务，不产出即时 TaskResult（云端以通道接入为准）
- 背压：本地→通道方向在通道不可写时结束；通道→本地方向由云端有界入站队列控制

## API Contract Draft

### 模块用户（管理员面，session）

- `GET /api/aiagent/users`
- `POST /api/aiagent/users`
- `PUT /api/aiagent/users/{id}`
- `POST /api/aiagent/users/{id}/reset-password`
- `DELETE /api/aiagent/users/{id}`

### 令牌（管理员面 + 用户面）

- `POST /api/aiagent/auth/login`（public，用户名 + 密码 → 长期令牌）
- `GET /api/aiagent/tokens`（当前用户名下令牌；管理员可查指定用户）
- `POST /api/aiagent/tokens/{id}/revoke`
- `POST /api/aiagent/auth/logout`

### 实例（用户面 / 管理员面）

- `GET /api/aiagent/instances`
- `POST /api/aiagent/instances`
- `PUT /api/aiagent/instances/{id}`
- `DELETE /api/aiagent/instances/{id}`
- `GET /api/aiagent/instances/{id}/status`（在线/进程探测，带缓存）
- `GET /api/aiagent/instances/{id}/access-info`（生成客户端接入信息）

### 实例元数据

- `GET /api/aiagent/instances/{id}/meta`
- `PUT /api/aiagent/instances/{id}/meta`

### Provider

- `GET /api/aiagent/providers`（返回注册表，含可用性与默认端口）

### 网关

- `ANY /api/aiagent/gw/{instanceId}/*`（Bearer 令牌）
- `POST /api/aiagent/gw/{instanceId}/stream-token`（换取一次性短令牌）

### 主机选择（复用）

- `GET /api/aiagent/servers`（可登记为实例的主机列表，带 Agent 在线与能力信息）

## Request / Response Requirements

成功：

```json
{
  "success": true,
  "data": {}
}
```

列表：

```json
{
  "success": true,
  "data": [],
  "total": 0,
  "offset": 0,
  "limit": 20
}
```

失败：

```json
{
  "success": false,
  "error": "错误描述",
  "code": "AIAGENT_ERROR"
}
```

额外要求：

- 令牌明文只在 `auth/login` 响应中出现一次，字段名 `token`，并附带 `expiresAt`
- 列表接口不回显密码、令牌哈希；令牌仅回显前缀与设备标签
- 网关接口透传目标响应状态码与 `Content-Type`，不改写为统一响应体（客户端需要原始协议）
- 时间字段统一 RFC3339

## Security Requirements

1. 管理员面接口一律 session 鉴权；用户面与网关一律模块令牌鉴权，二者不混用。
2. 密码 bcrypt 存储，禁止明文与日志输出；重置密码不返回明文。
3. 令牌只存哈希 + 前缀，长有效期可吊销；不在 URL 中携带长期令牌。
4. 归属校验严格：令牌 → 用户 → 实例；网关只允许转发到登记实例的本机端口，禁止任意主机/端口转发。
5. 网关过滤 hop-by-hop 头与 `Host`，避免请求走私与头部注入。
6. 登录限流与失败锁定；恒定时间比较。
7. 一次性短令牌 TTL 短、单次消费、绑定实例与用户。
8. 访问日志不含请求/响应体、令牌明文、密码。
9. 文档、示例与测试夹具不得写入真实密码、令牌、内部域名与本机绝对路径；示例用 `<PLACEHOLDER>` 与文档保留地址。
10. 全链路要求 HTTPS（面板侧），Agent 通道要求 WSS。

## Integration Decisions

1. **独立 `aiagent` 模块**，不并入 `server`（主机纳管）也不并入模型网关（模型调用）：职责分别为「AI Agent 的账号/实例/通道」「主机与 Agent 进程」「模型调用与计量」。
2. **模块自带用户体系**，不复用面板单管理员账号（决策见 ADR-0004）：面板登录只管管理，客户端登录换模块令牌。
3. **长期单一令牌**：不引入 access/refresh 双令牌（决策见 ADR-0004），以长有效期 + 吊销 + 限流控制风险。
4. **原生流通道**：不占用公网端口、不使用 Cloudflare 反代，通过主机 Agent 出站长连接与一次性 token 反连实现（决策见 ADR-0005）。
5. **Provider 抽象**：网关/账号/实例不感知具体 Agent，新增 Provider 的改动面最小化。
6. **在线口径**：以 Agent 进程运行 + 端口监听为准，而非仅主机在线。
7. **元数据只存 UI 偏好**：业务数据始终由各机器上的 Agent 自身持有，模块不做业务数据副本（避免一致性与合规问题）。
8. **客户端接入方式**：客户端把实例视为「一个服务器地址 + Bearer 令牌」，复用其既有的多服务器切换能力。

## Observability and Logging

记录以下行为到 `aiagent_access_logs` 与面板操作日志：

- 登录成功/失败、锁定触发
- 令牌签发、吊销、过期清理
- 用户创建/修改/重置密码/禁用/删除
- 实例创建/修改/删除/启停用
- 网关转发（实例、方法、路径、状态码、耗时、响应字节数）
- Agent 探测失败与通道异常

日志字段：用户 ID、令牌 ID、实例 ID、动作、结果、状态码、错误摘要、IP、UA、时间。不含敏感数据。

## Testing Decisions

### 后端测试

- schema 幂等初始化与列迁移
- 密码 bcrypt 往返、校验失败分支
- 令牌签发/校验/吊销/过期/用户禁用联动
- 登录限流与锁定
- 实例 CRUD、唯一约束、归属校验（跨用户越权必须失败）
- 元数据读写与大小限制
- Provider 注册表查询
- 一次性短令牌的原子消费与过期
- 网关：路径映射、头部过滤、SSE 逐块 flush（mock Agent 通道）、错误码
- 访问日志落库字段完整性

### Agent 测试（Rust）

- 探测任务：端口监听/未监听、进程存在/不存在、进程检测失败降级
- 数据通道：本地/通道双向字节搬运、边界情况（端口不可达、通道断开）回收

### 前端测试

- 模块注册与路由渲染
- 用户表单校验（用户名规则、密码长度）
- 实例表单校验（端口范围、Provider、主机必选）
- 状态展示（在线/进程未运行/离线/未知）
- 删除与吊销确认弹窗
- 空态/加载态/失败态

### 联调与验收

- `npm run governance:check`
- `node tools/backend-route-inventory.mjs`
- `npm run backend-go:test`
- `npm run db:audit`
- `npm run lint`、`npm test`、`npm run build`
- `npm run agent:build:windows`
- 浏览器 smoke：登录面板 → 创建用户 → 添加实例 → 查看在线状态 → 拉取接入信息 → 用客户端令牌访问网关

## Release Plan

### Milestone 1：模块骨架

- 注册 `aiagent` 模块（后端 manifest/route_handlers/server.go；前端 store.js/MainLayout.jsx）
- 五张表 schema + 列迁移
- 管理员面用户 CRUD + 密码重置

### Milestone 2：令牌与用户面

- 登录换长期令牌、令牌列表与吊销
- 登录限流与失败锁定
- 用户面实例列表（暂不接在线状态）

### Milestone 3：实例与探测

- 实例 CRUD、归属校验、主机选择
- Agent `aiagent_probe_v1` + 探测任务 + 实例状态接口与缓存
- 前端实例卡片与状态展示

### Milestone 4：原生流通道与网关

- Agent `aiagent_stream_v1` + 数据通道帧循环
- 云端 HTTP→帧桥 + SSE 透传 + 一次性短令牌
- 网关路由与错误码
- 端到端：手机/另一台电脑通过网关访问本机 Agent

### Milestone 5：元数据与接入信息

- 实例元数据接口
- 接入信息生成与复制
- 首个客户端接入（在对应仓库实施）

### Milestone 6：多 Provider 与审计完善

- Provider 注册表扩展与可用性开关
- 访问日志检索与基础统计
- 错误提示与边界完善

## Acceptance Criteria

1. 侧边栏「API 服务」分组下可见「AI Agent」模块入口。
2. 管理员可创建用户、设置密码、禁用用户、重置密码；密码不以明文落库或回显。
3. 客户端以域名 + 用户名 + 密码登录成功并获得长期令牌；重启后无需重新登录。
4. 令牌可吊销且吊销后立即失效；长有效期可配置。
5. 用户可添加多台实例并在客户端切换；跨用户访问被拒绝。
6. 实例在线状态以进程运行 + 端口监听为准；Agent 离线与进程未运行可区分展示。
7. 通过 `ANY /api/aiagent/gw/{instanceId}/*` 可访问目标机器上的 Agent HTTP 服务，且未开启任何公网端口、未使用 Cloudflare 反代。
8. SSE 流式响应逐块到达，不被整体缓冲。
9. 一次性短令牌可用于 `EventSource` 场景且 30 秒 TTL、单次消费。
10. Provider 注册表可见；新增 Provider 不需改动用户/令牌/实例/网关逻辑。
11. 实例元数据接口可供客户端存取 UI 偏好，多端一致。
12. 所有 `/api/aiagent` 路由进入 manifest 与 Go route 分发体系，分组/描述/schema 六处登记齐全。
13. 相关测试与治理命令通过（含 `db:audit`、`governance:check`、route inventory）。
14. 页面使用 Kumo 组件，中文 UI 完整，窄屏与暗色主题表现正常。

## Risks

1. 网关成为进入用户机器的唯一入口，鉴权或归属校验若出错影响极大——严格校验链 + 越权测试 + 一次性令牌缓解。
2. 长连接与 SSE 在中间层被缓冲或超时截断——`FlushInterval=-1` + 写超时续期 + 心跳 + 端到端流式测试缓解。
3. Agent 侧新增数据通道引入稳定性风险（内存、连接泄漏、背压）——帧循环限流、空闲回收、丢弃统计与上限保护缓解。
4. 长期单一令牌一旦泄露长期有效——可吊销 + 限流 + 每设备独立 + 短令牌用于 URL 场景缓解；未来可评估双令牌。
5. Provider 差异导致探测规则不准（进程名变动、端口自定义）——规则可配置 + 状态区分「未知」而非误报离线。
6. 主机 Agent 版本过旧不支持新能力——能力门禁 + 明确升级提示。
7. 元数据接口被滥用（存放大体积数据）——大小上限 + 所有权校验。
8. 模块命名与既有「AI Agent」Key 类型语义混淆——文档与 UI 文案明确语境，模块 ID 用 `aiagent`。

## Out of Scope

1. 用户自助注册、找回密码、邮箱/短信验证。
2. 多租户 RBAC、团队、跨用户共享实例。
3. Agent 进程的启动/停止/重启与软件安装管理。
4. 会话/消息/项目内容的服务端存储与备份。
5. 模型调用的代理与计量（归模型网关）。
6. 公网直连入口（Cloudflare Tunnel / 公网端口）方式。
7. 具体 Agent 的业务界面复刻。
8. 移动端原生 App（第一版以 Web/桌面客户端为主）。
9. 实例使用量的深度分析报表（Phase 5 只做基础观察）。

## Further Notes

1. 本模块与模型网关共同构成「AI 能力面」：模型网关管「调什么模型、花多少」，本模块管「在哪台机器上用哪个 Agent、怎么连上来」。
2. 与 `server` 模块的关系是「弱引用」：实例引用主机 ID，但主机纳管（Agent/SSH/Docker）仍归 `server` 模块。
3. OpenCode 是第一个 Provider；`pi`/`codex`/`claude-code` 以占位登记，实测后再标 `Verified`。
4. 原生流通道采用字节管道，HTTP/SSE 语义交给标准库；未来若需降低每请求连接开销，可在同一通道上叠加多路复用帧。
5. 首个真实使用方为 OpenCode UI（账号登录 + 实例切换），实施在其自身仓库，本模块只提供稳定接口。
6. 后续若需要「一键把实例导入某客户端」，可在 Phase 5 提供配置片段导出（不含明文令牌）。
