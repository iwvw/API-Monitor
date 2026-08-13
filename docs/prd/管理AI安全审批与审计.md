# 管理 AI 安全、审批与审计 PRD

最后更新：2026-08-13

> 本文件是「管理 AI」四份子 PRD 之一，对应安全权限模型、写操作批准流程、审计扩展与总体数据模型。配套子 PRD：
>
> - [管理 AI 核心引擎与数据层](./管理AI核心引擎与数据层.md)（后端 Run Loop）
> - [Web Ask AI 侧栏](./管理AIWeb侧栏前端.md)（前端交互）
> - [频道接入框架与 Telegram](./管理AI频道接入与Telegram.md)（渠道）
> - **[共通实现约定](./管理AI共通实现约定.md)**：实现本 PRD 前必须先读，内含所有源码接线片段。

## Problem Statement

让一个 AI 能调用系统 API（438 条路由，很多是写操作）需要一套严格的安全模型，否则 AI 等于一个"天然管理员后门"。Cloudflare Agent Lee 的写操作强制审批、OpenClaw 的配对/allowlist/沙箱、AWS Q 的"操作前确认"——行业共识是：**管理 AI 必须只读优先，写操作经人类逐个批准**。

当前系统已有：
- `system_config` 写开关 `ai_agent_write_enabled`（`ai_access.go L228`）
- `callAPIFromAI` 的路径黑名单（`/api/ai/` 递归禁止、密钥管理接口禁止、L33-38）
- 方法白名单（GET/POST/PUT/PATCH/DELETE，L109-116）
- `ai_access_audit` 审计表（L114-126）
- route manifest 的 auth modes（`AuthSession` / `AuthAgent` / `AuthPublic`）

但缺少：**会话级身份传导**、**写操作逐个批准状态机**、**渠道身份绑定**、**跨渠道审计统一视图**。此外，现有 `ai_access_audit` 表只记录"外部 MCP 调用"，管理 AI 的审计需要更细粒度（按会话、按执行 run、按单个工具调用）。

## Solution

### 总体数据模型

所有新表位于 `backend-go/internal/adminai/` 模块，通过 `ensureSchema` + `sync.Once` 在首次启动时创建（现有模式：`backend-go/internal/database/schema_ensurer.go` 等 20 处）。

```sql
-- 会话（PRD-01）
CREATE TABLE IF NOT EXISTS admin_ai_sessions (
    id TEXT PRIMARY KEY,                      -- 前缀 "aas_"
    source TEXT NOT NULL,                     -- "web" | "channel:<channelId>"
    channel_ref TEXT,                         -- 渠道 id（当 source=channel 时）
    title TEXT NOT NULL DEFAULT '',
    model TEXT,                               -- 模型标识（"endpointId/modelName"）
    write_enabled INTEGER NOT NULL DEFAULT 0, -- 创建时快照
    identity_json TEXT,                       -- 身份/权限上下文快照
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    last_activity_at TEXT NOT NULL
);
CREATE INDEX idx_admin_ai_sessions_source ON admin_ai_sessions(source);
CREATE INDEX idx_admin_ai_sessions_activity ON admin_ai_sessions(last_activity_at DESC);

-- 消息（PRD-01）
CREATE TABLE IF NOT EXISTS admin_ai_messages (
    id TEXT PRIMARY KEY,                      -- 前缀 "aam_"
    session_id TEXT NOT NULL REFERENCES admin_ai_sessions(id) ON DELETE CASCADE,
    role TEXT NOT NULL,                       -- "user" | "assistant" | "tool"
    content TEXT NOT NULL,
    tool_call_meta TEXT,                      -- JSON: {toolName, input, output, status}
    created_at TEXT NOT NULL
);
CREATE INDEX idx_admin_ai_messages_session ON admin_ai_messages(session_id, created_at);

-- 执行记录（审计）
CREATE TABLE IF NOT EXISTS admin_ai_executions (
    id TEXT PRIMARY KEY,                      -- 前缀 "aae_"
    session_id TEXT NOT NULL REFERENCES admin_ai_sessions(id) ON DELETE CASCADE,
    source TEXT NOT NULL,
    status TEXT NOT NULL,                     -- "running" | "completed" | "cancelled" | "error" | "timeout"
    tool_calls_count INTEGER NOT NULL DEFAULT 0,
    llm_model TEXT,
    llm_prompt_tokens INTEGER DEFAULT 0,
    llm_completion_tokens INTEGER DEFAULT 0,
    started_at TEXT NOT NULL,
    finished_at TEXT,
    error TEXT
);
CREATE INDEX idx_admin_ai_executions_session ON admin_ai_executions(session_id);

-- 工具调用明细（审计）
CREATE TABLE IF NOT EXISTS admin_ai_tool_calls (
    id TEXT PRIMARY KEY,
    execution_id TEXT NOT NULL REFERENCES admin_ai_executions(id) ON DELETE CASCADE,
    tool_name TEXT NOT NULL,
    input_json TEXT NOT NULL,
    output_summary TEXT,                      -- 截断摘要
    status TEXT NOT NULL,                     -- "running" | "success" | "error" | "blocked" | "approved"
    blocked_by_approval TEXT,                 -- 对应的 approval id（如果被写操作拦截）
    started_at TEXT NOT NULL,
    finished_at TEXT
);
CREATE INDEX idx_admin_ai_tool_calls_exec ON admin_ai_tool_calls(execution_id);

-- 频道配置（PRD-03）
CREATE TABLE IF NOT EXISTS admin_ai_channels (
    id TEXT PRIMARY KEY,
    type TEXT NOT NULL CHECK(type IN ('telegram')),  -- v1 仅 telegram
    name TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    config_encrypted TEXT NOT NULL,           -- secure.EncryptJSON
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- 频道用户绑定
CREATE TABLE IF NOT EXISTS admin_ai_channel_bindings (
    id TEXT PRIMARY KEY,
    channel_id TEXT NOT NULL REFERENCES admin_ai_channels(id) ON DELETE CASCADE,
    channel_user_id TEXT NOT NULL,            -- Telegram 数字 user id
    channel_username TEXT,
    panel_user_id TEXT,                       -- 绑定的面板用户（NULL = 未绑定，仅 allowlist 时可用）
    role TEXT NOT NULL DEFAULT 'admin',       -- 预留
    created_at TEXT NOT NULL,
    UNIQUE(channel_id, channel_user_id)
);

-- 写操作审批表
CREATE TABLE IF NOT EXISTS admin_ai_approvals (
    id TEXT PRIMARY KEY,                      -- 前缀 "aaa_"
    session_id TEXT NOT NULL REFERENCES admin_ai_sessions(id) ON DELETE CASCADE,
    tool_call_id TEXT REFERENCES admin_ai_tool_calls(id),
    status TEXT NOT NULL DEFAULT 'pending',   -- "pending" | "approved" | "rejected" | "expired" | "cancelled"
    plan_summary TEXT NOT NULL,               -- 人类可读的计划摘要
    method TEXT NOT NULL,                     -- HTTP 方法
    path TEXT NOT NULL,                       -- API 路径
    body_snapshot TEXT,                       -- 请求体快照（截断）
    requested_by TEXT,                        -- "web" | "channel:<channelId>"
    approved_by TEXT,                         -- 批准人标识
    expires_at TEXT NOT NULL,                 -- 30 分钟 TTL
    created_at TEXT NOT NULL,
    resolved_at TEXT
);
CREATE INDEX idx_admin_ai_approvals_session ON admin_ai_approvals(session_id);
CREATE INDEX idx_admin_ai_approvals_status ON admin_ai_approvals(status);

-- 扩展现有 ai_access_audit 表（新增 channel 列，确保SQLite column migration）
-- 通过 ensureSQLiteColumn 加列，不改现有行
-- ALTER TABLE ai_access_audit ADD COLUMN channel TEXT;
```

### 权限模型

**身份来源**：

| 场景 | 身份来源 | 写权限 |
| --- | --- | --- |
| Web 侧栏 | 当前登录 session 用户（`AuthSession`） | 全局 `ai_agent_write_enabled` + 会话快照 |
| 频道（Telegram） | `admin_ai_channel_bindings` 映射到面板用户 | 同上 + 频道级 `writeEnabled` 开关 |
| 未绑定频道用户 | 不允许（allowlist 拒绝） | — |

**执行身份**：

- Web 侧：引擎从 `http.Request` 提取当前登录用户，传给引擎作为 `Identity`。
- 频道：频道层查 `admin_ai_channel_bindings` 获取 `panelUserId`，传 `Identity{Source: "channel:<id>", UserID: "<panelUserId>"}`。
- 引擎把 `Identity` 传给 `callAPIFromAI`，引擎不直接持有用户身份，身份由上层传入。

**权限判定**：

- 读操作（GET）始终允许（受工具目录白名单约束，manifest 中 `AuthPublic` 路由不可通过 AI 调用）。
- 写操作（POST/PUT/PATCH/DELETE）需同时满足：
  1. 全局 `ai_agent_write_enabled` = true（`system_config` 键）
  2. 会话创建时快照的 `write_enabled` = true
  3. 渠道级（如有）`writeEnabled` = true
  4. 未命中路径黑名单（`/api/ai/`、密钥管理接口）
  5. 路由 `Owner` 为 `Go`、`ResponseMode` 非 Stream/WebSocket
  6. **审批通过**（`admin_ai_approvals` 记录 `status=approved`）

### 写操作审批流程

1. 引擎调用工具时，若方法为写操作且满足以上 1-5 条件，不直接执行，而是生成 `approval_required` 事件 + 创建 `admin_ai_approvals` 记录（`status=pending`，`expires_at=30min`）。
2. 引擎暂停该工具调用，等待审批。
3. 审批人通过 Web 侧栏批准卡或频道内联按钮（v2）批准/拒绝。
4. 批准后：引擎继续执行工具调用，返回结果。
5. 拒绝后：引擎标记 `tool_call.status=blocked`，返回拒绝消息给用户。
6. 超时：`expires_at` 后自动标记 `expired`，引擎收到 `approval_expired` 事件后通知用户。
7. 工具调用结束后，`tool_call` 记录最终状态。

**Web 批准 API**（`POST /api/admin-ai/approvals/:id/resolve` `{action: "approve"|"reject"}`）：
- 鉴权：当前登录用户必须与审批记录所属会话的 `Identity` 匹配（或为管理员）；v1 简化：任何登录管理员可批准属于同一来源（web）的审批。
- 渠道批准：v2 实现，通过 Telegram inline 按钮 callback data。

### 审计扩展

- `admin_ai_executions`：每次引擎执行一条记录，记录 LLM 用量（复用网关统计，此处冗余记录便于独立审计）。
- `admin_ai_tool_calls`：每次工具调用明细，含输入输出摘要。
- 现有 `ai_access_audit` 表加 `channel` 列（`ensureSQLiteColumn` 加列），标记渠道来源，与现有外部 MCP 审计统一查询。
- 现有审计 UI 扩展筛选条件：执行来源（web/channel）和关联会话。

### 系统配置

新增 `system_config` 键（沿用现有模式，`ai_access.go` 的 `ai_agent_write_enabled` 模式）：

| 键 | 类型 | 默认 | 说明 |
| --- | --- | --- | --- |
| `admin_ai_enabled` | bool | `true` | 管理 AI 总开关 |
| `admin_ai_default_model` | text | 空 | 默认推理模型（endpointId/modelName） |
| `admin_ai_write_enabled` | bool | `false` | 写操作全局开关（与现有 `ai_agent_write_enabled` 共存，统一检查） |
| `admin_ai_tool_call_limit` | int | `12` | 单轮最大工具调用次数 |
| `admin_ai_timeout_seconds` | int | `300` | 单轮执行超时 |
| `admin_ai_context_window` | int | `40000` | 上下文窗口 token 上限（超过截断早消息） |

所有配置在面板「AI 接入」设置页中展示（现有 `/api/ai-access` 页面扩展）。

### 管理面路由

- `POST /api/admin-ai/approvals/:id/resolve` `{action: "approve"|"reject"}` — 批准/拒绝
- `GET /api/admin-ai/approvals/:id` — 读取审批详情
- `GET /api/admin-ai/approvals` — 分页查询待办/历史审批
- `GET /api/admin-ai/audit` — 审计查询（tool_calls + executions 合并视图）
- `GET /api/admin-ai/settings` — 管理 AI 配置（system_config 包裹）
- `PUT /api/admin-ai/settings` — 更新配置

## Implementation Decisions

- 审批表 `admin_ai_approvals` 独立于引擎状态机：引擎发出 `approval_required` 事件后挂起，不阻塞其它会话。
- 审批超时由后台 goroutine 定时清理（`time.AfterFunc` with `expiresAt`），或懒加载在查询时检查。
- 会话 `identity_json` 快照：在创建时序列化一份 Identity，后续不跟踪用户权限变更；用户重新授权后应新建会话。
- 不要为审批表加外键到 `tool_call_id` 的级联删除——审批独立于工具调用，即使工具调用记录被清理，审批记录仍保留。
- 审计表 `admin_ai_executions` 和 `admin_ai_tool_calls` 量可能很大，提供定时清理策略（默认保留 90 天，在 `admin_ai_settings` 中可配）。
- 环境变量：`ADMIN_AI_DEFAULT_MODEL`（可选，oral 等效于 `admin_ai_default_model`）。

## Testing Decisions

- 单测覆盖：权限判定的 6 个条件全部独立覆盖（mock route manifest、mock system_config）。
- 审批流程状态机测试：pending → approve → execute → complete；pending → reject → blocked；pending → expire → expired。
- 并发场景：同一会话多个审批请求不会互相干扰。
- 审计写入：mock 引擎执行，验证 executions 和 tool_calls 记录正确。
- 验证命令：`npm run backend-go:test`、`go test ./backend-go/internal/adminai/...`。

## Release Plan

1. 数据模型建表（所有 7 张新表 + 现有 ai_access_audit 加列）。
2. 权限判定逻辑（6 条件）与身份传导。
3. 审批状态机 + 管理面路由（approve/reject）。
4. 审批超时清理 + 审计写入。
5. 设置页扩展（系统配置管理）。
6. 审计查询 API + 全链路联调。

## Acceptance Criteria

- Web 侧栏（PRD-02）中，发出一条"帮我关掉 xx 功能"的指令，AI 展示计划与批准卡，点批准后执行，拒绝后不执行。
- 未开启写操作全局开关时，写操作调用被拒绝且给出明确错误。
- 未绑定频道的 Telegram 用户发送消息，被 ignore 或回复"未授权"。
- 审计表可以查询到每次引擎执行（含 LLM 用量）与每次工具调用明细。
- 审批超时 30 分钟后自动标记 expired，不再执行。
- 所有新表通过 `ensureSchema` 创建，不加外部迁移框架。
- 现有 `ai_access_audit` 表 `channel` 列通过 `ensureSQLiteColumn` 加列，不破坏现有数据。
- 全部 lint/build/test 通过。

## Out of Scope

- v1 不做角色细分（所有管理员权限一致，无"只读管理员"角色）。
- v1 不做多用户审批（只有当前管理员可批准自己的会话）。
- v1 不做渠道内联按钮批准（Telegram 写操作统一走"回面板审批"或默认拒绝）。
- 不做审计日志的长期归档策略（90 天清理，不设自动导出）。
- 不做引擎沙箱或代码隔离（工具调用经 `callAPIFromAI` 已经是进程内隔离，不执行任意代码）。