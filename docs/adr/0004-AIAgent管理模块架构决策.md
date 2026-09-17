# ADR-0004：AI Agent 管理模块架构决策

- 状态：已接受
- 日期：2026-09-17

## 决策

API Monitor 新增独立的 `aiagent` 模块，中文入口名「AI Agent」，注册在前端侧边栏「API 服务」（`api-gateway`）分组。模块用于管理跑在各主机上的 AI 编码 Agent（首个 Provider 为 OpenCode，后续扩展 Pi、Codex CLI、Claude Code 等），提供**模块自带的用户体系、长期令牌登录、实例登记、实例在线探测与原生流通道网关**。模块遵循云厂商模块已验证的多文件分层模板，全部接口进入 Go Manifest 路由治理体系。配套需求见 [AI Agent 管理模块 PRD](../prd/AIAgent管理模块PRD.md)，通道实现见 [ADR-0005](./0005-AIAgent原生流通道架构决策.md)。

### 1. 独立模块，不并入 server 也不并入模型网关

`aiagent` 与相邻模块的边界：

1. `server` 模块负责主机纳管（Agent 安装、SSH、Docker、指标）；`aiagent` 只**引用**主机 ID，不接管主机生命周期。
2. 模型网关（`openai`）负责模型调用的代理与计量；`aiagent` 只转发 Agent 进程自身的本地 HTTP/SSE 服务数据，不接触模型调用。
3. 三者职责分别为「在哪台机器上用哪个 Agent、怎么连上来」「主机与 Agent 进程」「调什么模型、花多少」，不重叠。

模块 ID 用 `aiagent`（路由 `/api/aiagent`、包名 `internal/aiagent`）。中文名沿用需求方指定的「AI Agent」。注意 `AI Agent` 一词在接口文档页已作为 API Key 类型标签使用（`src/js/pages/apidocs/constants.js` 的 `aka_` 类型），二者语境不同：前者是模块/导航名，后者是密钥类型；文档与 UI 文案以「AI Agent 管理」明确语境，模块 ID 保持 `aiagent` 以避免与既有标识符冲突。

### 2. 模块自带用户体系，不复用面板单管理员账号

现状是单管理员模型：登录只校验一个密码（`backend-go/internal/auth/service_test.go` 中登录请求体仅含 `password` 与 `totpToken`），密码 bcrypt 存于 `user_settings` 体系，没有多用户表。本模块**不复用**该体系，理由：

1. 需求是面向使用者的普通账号（例如 `salen`），由管理员在面板创建并设置密码；与「面板管理员」是两个安全域，混用会让使用者获得面板管理能力。
2. 管理面与使用面的鉴权强度与生命周期不同：管理员会话是短时 session + 可选 2FA；使用者是长期令牌 + 可吊销。
3. 独立用户表使「禁用用户 = 其所有令牌立即失效」这类处置逻辑清晰可控，不牵动面板安全基线。

因此：**管理员面**（用户增删改、重置密码、实例分配、令牌吊销）走面板现有 `AuthSession`；**用户面与网关**走模块自己的令牌，在 handler 内校验，不进入通用 session 分支。

### 3. 长期单一令牌，不引入 access/refresh 双令牌

决策：登录成功签发**一个长期令牌**（默认 180 天，可配置），不设刷新令牌。

1. 需求明确为「持久化，不用经常登录」，双令牌的价值主要在短时 access 降低泄露窗口，但会引入刷新失败、并发刷新、时钟偏差等一系列客户端复杂度。
2. 风险改由其它维度控制：令牌可吊销、每设备独立、登录限流与失败锁定、URL 场景改用一次性短令牌（见第 6 节）、用户禁用即全失效。
3. 服务端只存 SHA-256 哈希与展示前缀，明文仅在签发响应出现一次；比较使用恒定时间实现。

未来若确有需要，可在不改变客户端契约的前提下引入双令牌（令牌结构预留类型字段）。

### 4. 实例模型：主机会员 + Provider + 端口

实例是「一台已装 Agent 的主机 + 一种 AI Agent 类型 + 一个本地端口 + 一个归属用户」。

1. 归属由 `user_id` 决定，用户只能看到与操作自己的实例；管理员可看全部。
2. 唯一约束 `(user_id, server_id, port)`，避免重复登记。
3. **端口每实例可配**，默认取 Provider 默认端口（OpenCode 为 `4096`），因为实际部署端口可能被占用或自定义。
4. 用户可自助添加自己的机器，管理员也可代为分配，两种入口写同一张表。

补充（实现期安全加固）：为实现跨租户隔离，**把一台新主机首次纳入清单属于管理员动作**。普通用户只能在管理员已分配给其账号的主机上增删实例；`handleServers` 与实例创建/探测都按调用方过滤，避免任意登录用户枚举全舰主机并探测其端口状态。

### 5. Provider 抽象：网关与账号体系不感知具体 Agent

`providers.go` 维护静态注册表，字段含 `ID`、`Label`、`DefaultPort`、进程匹配规则、网关 base 路径、流式协议（SSE）。第一版内置 `opencode` 并标记已实测；`pi`、`codex`、`claude-code` 以占位登记（默认端口与进程规则可配、`Verified=false`）。

1. 网关只按 Provider 决定目标 base 路径与探测规则，不含 Provider 分支逻辑。
2. 新增 Provider 的改动面限定为：注册表条目 + 探测规则 + 客户端接入提示；不触碰用户、令牌、实例、网关。
3. 不用「每 Provider 一个模块」的方案：AI Agent 的管理语义（用户、实例、通道）完全一致，拆模块会导致大量重复代码与分散的导航入口。

### 6. 令牌校验：Bearer + 一次性短令牌

- 常规请求：`Authorization: Bearer <长期令牌>`。
- 流式场景（`EventSource` 与 WebSocket 无法自定义请求头）：先用长期令牌调用 `POST /api/aiagent/gw/{instanceId}/stream-token` 换取一次性短令牌，再以 `?st=` 携带。短令牌 TTL 30 秒、单次消费、绑定 instanceId 与 userId。
- 复用现有终端流 broker 的做法（`backend-go/internal/serveragent/terminal_stream.go` 的 `create`/`consume` 模式），不新造一次性凭证机制。

### 7. 在线口径：Agent 进程运行 + 端口监听

实例状态以「Agent 进程真的在跑」为准，而不是仅看主机是否在线。

1. Agent 新增能力 `aiagent_probe_v1`，按 Provider 规则做进程匹配（进程名/PID）与回环端口监听检测。
2. 结果三态区分：`online`（进程在跑且在监听）、`主进程未运行`、`主机离线`、`状态未知`（探测失败），避免把探测失败误报为离线。
3. 列表接口带缓存 TTL（约 15 秒），避免高频触发探测。

### 8. 元数据只存 UI 偏好，业务数据不做服务端副本

`aiagent_instance_meta` 只存客户端 UI 偏好（项目钉选、排序、隐藏目录、上次打开的会话等），业务数据（项目、会话、消息）的唯一事实来源始终是各机器上的 Agent 自身。

理由：业务数据落库会带来一致性（Agent 与副本不一致）、存储与合规、以及「Agent 已删除但副本还在」等问题，且与本项目「SQLite 单进程、低依赖」的定位冲突。多端一致性问题通过「单一事实来源 + 轻量偏好共享」解决，而不是复制业务数据。

### 9. 模块结构：照抄云厂商多文件分层模板

```text
backend-go/internal/aiagent/
├── service.go            # Service + New + open() + ServeHTTP 路径分发
├── schema.go             # 五张表幂等 DDL + 列迁移
├── user_store.go         # 模块用户 CRUD + bcrypt
├── token_store.go        # 令牌签发/校验/吊销 + 限流
├── instance_store.go     # 实例 CRUD + 归属校验
├── meta_store.go         # 实例元数据读写
├── providers.go          # Provider 注册表
├── gateway.go            # 网关：HTTP→帧桥 + SSE 透传 + 一次性短令牌
├── agent_bridge.go       # 与 serveragent 的探测/通道交互封装
├── access_log.go         # 访问日志落库
├── types.go              # Account/Payload/Normalized* 结构
└── service_test.go
```

规模增长时按域继续拆分，禁止把逻辑堆进超大 `service.go`。

### 10. 路由与鉴权：Manifest 登记 + 两类鉴权并存

- `manifest.go` 登记 `/api/aiagent` 前缀与逐条子路由；管理员面 `AuthSession`，用户面与网关 `AuthPublic`（在 handler 内做模块令牌校验）。
- `server.go` 按新模块标准四处改动（import、Server 字段、`newServer()` 实例化、`serveGoRoute()` case）。
- `route_handlers.go` 的 `moduleHandlers` 增加一行。
- 按《新模块接入指南》完成六处接口目录登记（manifest、route_descriptions、api_docs_catalog、route_aliases、route_contracts、分组两处）。

### 11. 前端：独立 AiAgentPage，注册进 api-gateway 分组

- `src/js/pages/AiAgentPage.jsx` + `src/js/pages/aiagent/` 目录。
- `store.js` 的 `MODULE_CONFIG` 与 `MODULE_GROUPS`（`api-gateway.modules`）增加条目。
- `MainLayout.jsx` 的 lazy import 与 `renderActivePage()` case；`Icons.jsx` 加图标映射。
- UI 全部 Kumo 组件、中文文案；删除与吊销走 `DeleteResource` 确认。

### 12. 不做多租户 RBAC

模块不建立角色、团队、共享实例等权限模型。用户之间完全隔离，管理员是全权。这与需求「仅自己使用、管理员建用户」一致，也与云厂商模块「不做多租户 RBAC」的既有决策一致。

## 后果

- 优点：一个用户一个入口即可管理跨多台机器的多个 AI Agent；不暴露公网端口；用户体系与面板安全域隔离；Provider 抽象使后续接入成本低；复用既有 Agent 通道与 broker 机制，新增协议面小。
- 缺点/代价：新增一套用户与令牌体系，长期令牌的泄露窗口较大；不做业务数据副本意味着客户端必须在目标 Agent 在线时才能访问；Provider 差异需要逐一手工验证探测规则。
- 风险与缓解：
  - 网关成为进入用户机器的唯一入口：严格校验链（令牌 → 用户 → 实例 → Provider 路径），越权用例进测试。
  - 长期令牌泄露：可吊销 + 每设备独立 + 登录限流 + URL 场景一次性短令牌。
  - 探测规则不准：规则可配置，状态保留「未知」而非误报离线。
  - 主机 Agent 版本过旧：能力门禁 + 明确升级提示。
  - 模块命名与既有 AI Agent Key 类型混淆：文档与 UI 文案明确语境，模块 ID 固定 `aiagent`。

## 来源

参考资料（项目内既有实现，均已验证）：

- Agent 与 Engine.IO 长连接：`backend-go/internal/serveragent/engineio.go`
- Agent 任务下发与能力门禁：`backend-go/internal/serveragent/agent_tasks.go`、`tasks.go`、`agent-rust/src/main.rs` 的 `agent_capabilities()`
- 一次性 token 流 broker：`backend-go/internal/serveragent/terminal_stream.go`
- 模块自带密码（bcrypt）范式：`backend-go/internal/filebox/service.go`
- 长期令牌 / Scope / 前缀范式：`backend-go/internal/apikeys/manager.go`
- 多文件分层与 schema 规范：`backend-go/internal/managedproxy/schema.go`、`backend-go/internal/subscription/schema.go`
- 模块接线与新模块清单：`docs/guides/新模块接入指南.md`、`docs/standards/云厂商模块开发指南.md`
- 前端模块注册：`src/js/store.js`、`src/js/components/MainLayout.jsx`
- 配套需求文档：[AI Agent 管理模块 PRD](../prd/AIAgent管理模块PRD.md)
- 通道实现决策：[ADR-0005：AI Agent 原生流通道架构决策](./0005-AIAgent原生流通道架构决策.md)
