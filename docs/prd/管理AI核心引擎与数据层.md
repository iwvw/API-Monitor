# 管理 AI 核心引擎与数据层 PRD

最后更新：2026-08-13

> 本文件是「管理 AI」四份子 PRD 之一，对应后端核心引擎与数据层，是整个功能的技术地基。配套子 PRD：
>
> - [Web Ask AI 侧栏](./管理AIWeb侧栏前端.md)（前端交互）
> - [频道接入框架与 Telegram](./管理AI频道接入与Telegram.md)（渠道）
> - [安全、审批与审计](./管理AI安全审批与审计.md)（权限闭环）
> - **[共通实现约定](./管理AI共通实现约定.md)**：实现本 PRD 前必须先读，内含所有源码接线片段。

## Problem Statement

API Monitor 已经具备两套"AI 能操作系统"的资产，但彼此断层：

1. **AI 接入面**（`/api/ai/mcp` + `/api/ai/manifest`，`backend-go/internal/system/ai_access.go`）已经能通过 MCP 把 438 条 Go 路由暴露给外部客户端，有写操作总开关 `ai_agent_write_enabled` 和审计表 `ai_access_audit`；但它要求使用者自带 AI 客户端、懂 MCP 配置，门槛高，且没有"对话会话"概念。
2. **OpenAI 兼容模型网关**（`backend-go/internal/openai/`）拥有端点到上游 LLM 的完整转发链（选点、密钥解密、延迟加权、代理池、流式回写），但只对外提供 `/v1` 推理，不为面板内部业务提供"直接调用自家网关做推理"的进程内能力。

目前没有一个组件能把"用户在面板里问一个问题 → 触发 LLM 推理 → 推理过程中按需调用 system 内部路由 → 流式把结果推给前端/外部渠道"串起来。管理 AI 需要的是一套**会话驱动的推理执行引擎**，而不是又一层转发代理。

## Solution

新增 Go 模块 `backend-go/internal/adminai/`，作为管理 AI 的后端核心。它提供：

- **会话与消息存储**：`admin_ai_sessions` / `admin_ai_messages`，可同时服务于"Web 侧栏"与"外部频道"两类会话（异构，一个会话只归属一种来源）。
- **推理执行引擎（Run Loop）**：给定会话与用户消息，驱动 LLM（复用模型网关选点逻辑）按 agentic loop 执行 —— 工具发现 → 工具调用 → 结果回填 → 最终回答，支持流式推送。
- **工具调用桥**：进程内调用经由 `Server.callAPIFromAI`（`backend-go/internal/server/ai_caller.go`）再造，但以"当前会话身份 + 会话级写权限"执行，而不是单一 Agent Key（详见安全 PRD）。
- **事件分发**：把引擎产生的流式事件（增量文本、工具开始/结束、计划待批准、错误）推送给出处：Web 走 SSE，渠道走频道出站（PRD-03）。
- **能力暴露**：把引擎能力封装为 `/api/admin-ai/*` 管理面路由与（可选的）内部 MCP tool 扩展，保持与现有 `/api/ai/*` 的职责边界。

引擎不感知具体 UI / 具体渠道：它只消费"来源 + 会话 id + 消息 + 身份/权限上下文"，产出事件流。

## User Stories

1. 作为管理员，我想在 Web 侧栏发起一次对话，系统把我的消息交给 LLM 并流式返回回答，以便获得即时反馈。
2. 作为管理员，我想让 AI 在回答途中调用系统接口（如查日志、查 DNS 记录），以便直接基于真实数据作答。
3. 作为管理员，我想每一次"打算修改系统状态"的动作都先在对话中展示计划并等我批准，以便防止误操作。
4. 作为管理员，我想把我的会话按来源区分保存（Web / Telegram / 未来渠道），以便各端互不串扰。
5. 作为管理员，我想从任意端点继续我的会话，以便跨端有延续（渠道平滑迁移时可关闭）。
6. 作为管理员，我想看到 AI 每一步在做什么（调用了哪个接口、正在等什么），以便理解决策过程。

## Functional Requirements

### 会话模型

- 会话来源类型：`web`（面板侧栏）、`channel:<channelId>`（外部渠道，见 PRD-03）。
- 会话字段：id、来源类型、渠道引用（可空）、标题、所属模型（LLM endpoint+model）、只读/可写策略快照、创建/更新时间、最后活动时间；无独立"用户"字段，权限由当前执行身份动态判定（见安全 PRD）。
- 同一 Web 用户只维护一个"当前活跃会话"；历史会话按 createdAt 排序展示，支持删除（CASCADE 删除消息）。
- 渠道会话由渠道唯一键定位（如 `telegram:<chatId>`），一对一定位（见 PRD-03）。
- 会话默认**不跨会话记忆**（v1 不做 recall），与 Cloudflare Agent Lee 一致；上下文窗口只包含当前会话消息 + 单条消息的工具结果回填。

### 消息模型

- 消息角色：`user` / `assistant` / `tool`（工具结果回填，不直接对用户呈现，仅作引擎上下文）。
- 消息字段：session_id、role、content（正文）、tool call 元数据（tool name / input）、created_at。
- assistant 消息支持「流式累积」：引擎分批更新同一消息的 content（SSE 每个事件携带同一 message id）。
- 消息体大小上限 64KB；超长工具结果在入上下文前截断并标记「已截断」。

### 推理执行引擎

引擎输入：

```
{
  source: "web" | "channel:<channelId>",
  sessionId: string,        // 不存在则先建会话
  prompt: string,           // 用户本条消息
  identity: {...},          // 执行身份与权限上下文（见安全 PRD）
  model: { endpointHint?, model? },  // 可选，缺省用系统默认
}
```

引擎流程（可中断、可流式）：

1. 载入/创建会话，把 prompt 写入 `admin_ai_messages`。
2. 解析当前系统写权限与工具目录（由 manifest 生成，按会话身份过滤）。
3. 调 LLM（`chat/completions` 风格），把系统提示 + 工具定义 + 消息历史发给模型。
4. 若模型返回 tool_calls：逐工具执行（见下方工具桥），写入 tool 消息回填，回到第 3 步。
5. 若模型返回文本：流式累积为 assistant 消息，事件推送。
6. 完成或用户取消：终止循环，持久化最终消息。

引擎约束：

- 每次对话的写开关只在会话创建 / 渠道绑定时解析并快照，执行期间不变。
- 单轮最多 N 次工具调用（默认 12），超限强制结束并提示。
- 单轮总时长上限（默认 5 分钟），超时取消。
- 引擎不负责审批 UI；写操作在调用时通过"待批准"事件挂起（见安全 PRD 的审批待办模型）。
- 引擎无独立 HTTP 客户端，LLM 调用一律走内部网关客户端（见 LLM 接入）。

### LLM 接入

- 复用 `backend-go/internal/openai/service.go` 的选点逻辑 `selectEndpointCandidates`（L3938）与统一 `relayLoop`（L4099），在 `adminai` 模块内封装 `InferenceClient`：
  - 输入：`{ model?, endpointHint?, messages, tools }`；缺省走网关默认模型。
  - 输出：非流式调用返回完整文本；流式调用返回事件回调（增量块 / 工具调用片段）。
- 发送到上游的请求体直接构造 OpenAI `chat/completions` 接口；对 Anthropic 类端点（`/v1/messages`）由网关侧适配，引擎不感知厂商差异。
- LLM 调用也记入网关用量（`openai` 的 analytics/summary 统计路径），复用现有计费与日志。
- 无可用端点 / 端点全部失败：引擎返回明确中文错误事件，不静默。

### 工具目录与调用桥

- 工具目录由现有 AI 路由目录 `list_apis`/`get_route`（`ai_access.go` L468-565）提供，按 manifest module/group 组织；对外暴露时只包含会话身份允许访问的读写路由（见安全 PRD）。
- 实际执行走 `Server.callAPIFromAI`（`ai_caller.go` L18-107）的语义：方法白名单、`/api/ai/*` 递归禁止、密钥管理接口禁止、写操作需写开关。
- 关键差异：管理 AI 调用时 `X-AI-Agent` 头与审计打点沿用现有通道；身份来自会话（安全 PRD 统一记录）。

### 流式事件协议

- 事件经一个 `EventSink` 接口下推，事件类型（全部带 `messageId`）：

| 事件 | 字段 | 语义 |
| --- | --- | --- |
| `meta` | sessionId、model、writeEnabled | 会话元信息 |
| `delta` | text | 增量文本 |
| `tool_start` | toolName、args | 工具开始 |
| `tool_result` | toolName、status、summary | 工具结束（结果摘要） |
| `approval_required` | approvalId、plan 摘要、expiresAt | 写操作待批准 |
| `approval_expired` | approvalId | 批准超时 |
| `error` | message、可重试 | 失败 |
| `done` | messageId、usage | 完成 |

- Web 侧由 `/api/admin-ai/messages` 的 SSE 通道下发；保持心跳（每 15s `: ping`）防断连。

### 管理面路由（manifest 登记 `admin-ai` module，Owner=go，全局以 `session` 鉴权）

- `GET /api/admin-ai/sessions`：当前用户会话列表（按 last_activity 倒序）与 metadata。
- `POST /api/admin-ai/sessions`：新建会话（可带 title/model）。
- `DELETE /api/admin-ai/sessions/:id`：删除会话及其消息（CASCADE）。
- `GET /api/admin-ai/sessions/:id/messages`：拉取会话历史（游标分页）。
- `POST /api/admin-ai/messages`：发起一次推理执行（body 见 User Stories），响应 `202`，SSE 挂 `/messages/stream` 或本请求内流式返回（二选一，见 Implementation Decisions）。
- `GET /api/admin-ai/messages/stream?sessionId=...`：订阅当前执行的事件流。
- `POST /api/admin-ai/cancel`：取消当前用户/会话的执行。
- 渠道相关管理面路由见 PRD-03。

## Implementation Decisions

- 新模块目录 `backend-go/internal/adminai/`，独立 `ensureSchema` + `schemaOnce`（同现有 20 处 schema 惯例）；manifest.go 登记 `admin-ai` module。
- 内部 `InferenceClient` 与 `callAPIFromAI` 复用；不重复实现 HTTP 客户端与端点选择。
- 引擎状态机可测试：把「决策是否继续/何时结束」抽成纯函数，方便单测。
- 流式采用 SSE：抄 `openai/analytics.go` 的 `text/event-stream` 模式；Web 侧用 EventSource（或 fetch 流式读取）。
- 一个执行对应一个上下文/队列，`sessionId` + 来源作为并发键；同一会话同时只允许一个活跃执行，后续请求直接返回「已有执行进行中」错误。
- 审计：每次引擎执行与每次工具调用都写 `admin_ai_executions` 与 `admin_ai_tool_calls` 明细（见安全 PRD）。

## API Contract Draft

- `GET /api/admin-ai/sessions` → `{ sessions: [{id, source, title, model, createdAt, lastActivityAt, messageCount}] }`
- `POST /api/admin-ai/sessions` `{ title?, model? }` → `{ id, ... }`
- `DELETE /api/admin-ai/sessions/{id}` → `{ ok: true }`
- `GET /api/admin-ai/sessions/{id}/messages?cursor=...&limit=50` → `{ items: [...], nextCursor }`
- `POST /api/admin-ai/messages` `{ sessionId, prompt, model? }` → `{ sessionId, runId }`
- `GET /api/admin-ai/messages/stream?runId=...` → `text/event-stream`（事件协议见上）
- `POST /api/admin-ai/cancel` `{ runId }` → `{ cancelled: true }`

## Testing Decisions

- 后端单测覆盖：引擎状态机（继续/结束/超界）、工具调用桥（只读/写开关/黑名单）、会话与消息 CRUD、LLM 响应解析（tool_calls 分支、流式 chunk 合并）。
- API 测试：会话 lifecycle、消息分页游标、SSE 事件序列（不含真实 LLM，mock `InferenceClient`）。
- 并发测试：同一会话并发执行被拒绝。
- 验证命令：`npm run backend-go:test`、`npm run governance:check`、`npm run lint -- --quiet`。

## Release Plan

1. schema 与模型（session/message/execution 表）。
2. `InferenceClient`（复用网关选点 + relayLoop）。
3. 引擎状态机 + 工具桥接线（先只读工具、无审批）。
4. SSE 事件协议 + 管理面路由 + cancel。
5. 审计写入（executions / tool_calls）。
6. 联调：Web 侧栏（PRD-02）与渠道层（PRD-03）各跑通一条只读链路。

每个纵切面保持主分支可构建、可测试。

## Acceptance Criteria

- 通过 `/api/admin-ai/messages` + SSE 可以完成一次"提问 → LLM 流式回答"。
- AI 在回答中能调用一个只读内部接口（如查询系统日志）并基于其真实结果回答。
- AI 触发的所有工具调用都出现在审计明细中。
- 写操作在写开关关闭时被拒绝并返回明确错误。
- 同一会话并发执行被拒绝且不丢消息。
- `.env`/依赖零新增；全部现有测试通过。

## Out of Scope

- 不实现跨会话记忆 / RAG / 向量检索（v1）。
- 不做 LLM 自托管训练或微调。
- 不做多用户会话权限细分（v1 为管理员全局会话）。
- 不做工具调用沙箱（工具只允许 manifest 内的 Go 路由，不执行任意代码）。
- 不负责审批 UI 与渠道写审批（见 PRD-03/04）。