# 管理 AI 频道接入框架与 Telegram PRD

最后更新：2026-08-13

> 本文件是「管理 AI」四份子 PRD 之一，对应频道接入抽象层与 Telegram 频道实现。配套子 PRD：
>
> - [管理 AI 核心引擎与数据层](./管理AI核心引擎与数据层.md)（后端 Run Loop）
> - [Web Ask AI 侧栏](./管理AIWeb侧栏前端.md)（前端交互）
> - [安全、审批与审计](./管理AI安全审批与审计.md)（权限闭环）
> - **[共通实现约定](./管理AI共通实现约定.md)**：实现本 PRD 前必须先读，内含所有源码接线片段。

## Problem Statement

参考 OpenClaw 的频道模型（Channels）：一个管理 AI 不应只在 Web 面板里可用，还应能接入用户日常使用的消息平台（Telegram、Discord、Slack 等），在其中以 Bot 身份对话，并能广播/回复。

当前系统已有通知中心（`notification_channels` 表，支持 Telegram 出站推送），但它是单向的——只会推告警消息，不会收用户消息、不会理解上下文、不会做流式回答。管理 AI 的频道需要双向：入站解析用户消息 → 触发引擎执行 → 出站流式推送回答。

需要先做频道抽象层，把「入站→执行→出站」的通用管道定义清楚，再以 Telegram 作为首个实现目标。

## Solution

### 频道抽象层

在 `backend-go/internal/adminai/channel/` 定义 `Channel` 接口：

```go
type Channel interface {
    ID() string                         // 唯一标识，如 "telegram"
    Start(ctx context.Context) error    // 启动（长轮询/webhook监听）
    Stop(ctx context.Context) error     // 停止
    Send(ctx context.Context, to string, msg OutboundMessage) (string, error) // 发送消息
    Edit(ctx context.Context, to, id string, msg OutboundMessage) error       // 编辑已有消息
    Status() ChannelStatus              // 运行状态
}
```

入站消息统一为 `InboundEnvelope`：

```go
type InboundEnvelope struct {
    ChannelID string            // "telegram"
    ChatID    string            // 发送者/群组的唯一键
    UserID    string            // 发送者用户ID
    Username  string            // 显示名
    Text      string            // 消息文本（纯文本，去除了格式标记）
    ChatType  string            // "dm" | "group" | "topic"
    IsMention bool              // 是否在群组中被@
    Raw       json.RawMessage   // 原始消息（渠道扩展用）
}
```

`OutboundMessage` 支持多段：

```go
type OutboundMessage struct {
    Text    string            // 主文本（支持 HTML/Markdown 子集）
    Blocks  []OutboundBlock   // 结构化块：代码块、按钮、图片、错误
    Stream  bool              // 是否流式编辑（Telegram 编辑预览）
}
```

### 引擎对接

- 引擎不感知具体渠道，只消费 `InboundEnvelope` 与 `Identity`（见安全 PRD）。
- 频道层收到入站消息 → 组装 `Identity`（从渠道用户ID映射面板用户/角色）→ 调引擎 execute（返回事件流）→ 频道层把事件流转化渠道出站格式（如 Telegram 编辑预览）。
- 每个渠道独立 goroutine 运行，`Start()` 注册到引擎的 `ChannelRegistry`，通过 `admin_ai_channels` 表配置。

### 配置管理

`admin_ai_channels` 表（定义见数据模型 PRD-04）：
- 渠道类型（`telegram` / 预留 `discord` / `slack` 等）。
- 渠道特定配置 JSON（Telegram 的 bot_token, allowlist, dmPolicy, groupPolicy 等），`secure.EncryptJSON` 加密存储（复用 `notification_channels` 模式）。
- 启用/禁用、关联提示词（可选）、来源映射（`channel:<id>` 用于会话定位）。

## Telegram 频道实现

### 连接方式

- **长轮询优先**（类似 OpenClaw 的 grammY runner）：`getUpdates` 轮询，同一 token 只允许一个活跃 poller（服务端 guarantee，避免 409 冲突）。
- Webhook 模式可选：`setWebhook` + 本地监听 + 代理规约（`/telegram-webhook` 路径，`telegramWHSecret` 验证）。
- 出站复用 `notification.sendTelegram`（`backend-go/internal/notification/service.go L1644`）与 `editTelegram`（L1670）的底层 HTTP 客户端（`telegramHTTPClient` L1817），不做重复封装。

### 入站消息处理

- 识别 DM 与群组消息：`message.chat.type`。
- 群组只响应提及（`@bot_username` 或 `requireMention` 配置）；DM 全部响应（受 `dmPolicy` 约束）。
- 消息文本提取 `message.text`，保留 `message_id` 用于回复。
- 多人 DM 按群组会话处理（OpenClaw 模式）。
- 非文本消息（图片/语音/文件）暂不处理（v1 只做纯文本，标记 `[media:unsupported]` 占位）。

### 出站与流式

- 非流式回答：`sendMessage`（HTML parse_mode，`disable_web_page_preview` 可配）。
- 流式回答：`sendMessage` 发送临时消息 → `editMessageText` 逐编辑推送（如 OpenClaw 的 streaming mode `partial`）。最终收尾时定稿。
- 消息分片：Telegram 单条 4096 字符上限；超长消息按段落分割为多条（`textChunkLimit` 可配）。
- 代码块：`<pre>` / `<code>` 标签，Telegram 原生 HTML 渲染。

### 权限与配对

- DM 策略 `dmPolicy`：`allowlist`（推荐，v1 默认）| `open`（仅信任网络/公共测试）。`pairing`（OpenClaw 模式，v2）。
- 入站用户 ID 到面板用户映射表 `admin_ai_channel_bindings`（见 PRD-04）：`channelId + channelUserId → panelUserId`。v1 支持手动绑定（管理员在面板设置「Telegram 用户 → 面板管理员」），v2 支持 `/pair` 命令动态配对。
- 写操作安全：Telegram 频道默认只读（`writeEnabled: false`），需要在面板设置中主动开启 + 绑定 Telegram 用户后才允许写操作。写操作审批在渠道侧通过 inline 按钮（v2）。
- 群组 `requireMention` 可配，默认 `true`；`allowFrom` 白名单。

### 配置示例（存入 `admin_ai_channels` config JSON）

```json
{
  "botToken": "encrypted:...",
  "dmPolicy": "allowlist",
  "allowFrom": ["123456789"],
  "groupPolicy": "allowlist",
  "groups": {
    "-1001234567890": { "requireMention": true }
  },
  "textChunkLimit": 4000,
  "streaming": { "mode": "partial", "preview": { "toolProgress": true } }
}
```

### 管理面路由（manifest 登记 `admin-ai` module）

- `GET /api/admin-ai/channels`：渠道列表与状态。
- `POST /api/admin-ai/channels`：新增渠道配置（type + config 加密）。
- `PUT /api/admin-ai/channels/:id`：更新渠道配置。
- `DELETE /api/admin-ai/channels/:id`：删除渠道配置（停止 bot）。
- `POST /api/admin-ai/channels/:id/start`：手动启动。
- `POST /api/admin-ai/channels/:id/stop`：手动停止。
- `GET /api/admin-ai/channels/:id/status`：运行时状态（在线/离线/错误）。
- `GET /api/admin-ai/channel-bindings`：绑定列表。
- `POST /api/admin-ai/channel-bindings`：新增绑定（手动）。
- `DELETE /api/admin-ai/channel-bindings/:id`：删除绑定。

## Implementation Decisions

- 新文件 `backend-go/internal/adminai/channel/telegram.go` 实现 `Channel` 接口。
- 不引入 grammY 库：用标准库 `net/http` + `encoding/json` 直接调 Bot API（现有 `notification.callTelegram` 已封装）；避免新增 node_modules 级别的依赖。
- 邀请码配对（`/pair` 命令）在 v2 实现；v1 只做手动绑定 + allowlist。
- 流式编辑的"消息不可重复编辑"错误用 `editTelegram` 相同的 `message is not modified` 静默处理（L1687）。
- 渠道配置的 `bot_token` 加密存储复用 `secure.EncryptJSON`（与 `notification_channels` 一致）。
- 启动时（`Start()`) 做 `getMe` 校验 token 有效性与身份；持续失败在面板展示状态错误。
- 删除渠道时自动 `stop()` + 清理轮询 goroutine。

## Testing Decisions

- `Channel` 接口 mocks：用 mock 实现测试引擎→渠道的对接。
- Telegram 层：mock Bot API 的 `getUpdates`/`sendMessage`/`editMessageText` 端点，覆盖长轮询、消息分片、流式编辑、404 错误处理。
- 绑定映射单测：`channelUserId → panelUserId` 解析。
- 验证命令：`npm run backend-go:test`、`go test ./backend-go/internal/adminai/...`。

## Release Plan

1. `Channel` 接口定义 + `ChannelRegistry` + 空实现骨架。
2. `admin_ai_channels` 表 + 管理面路由 CRUD。
3. Telegram 长轮询入站 + 引擎对接（只读，不流式）。
4. Telegram 出站（sendMessage 复用）+ 流式编辑。
5. 权限映射：绑定表 + 手动绑定 UI + allowlist 执行。
6. `channel-bindings` 管理面路由 + 面板渠道管理页。

## Acceptance Criteria

- 在面板配置一个 Telegram bot token 后，启动 bot 并在 DM 中收到"已就绪"消息。
- 向 bot 发送消息，3 秒内收到 AI 的回答（基于面板真实数据）。
- 群组中添加 bot 并 @提及，bot 回复；未提及时不回复。
- 未在 allowlist 中的用户发 DM 被忽略（或回复"未授权"）。
- 配置变更后重启 bot 应用新配置。
- 删除渠道时 bot 停止轮询，不再响应/发送。
- 所有可见 UI 中文；lint/build/test 通过。

## Out of Scope

- v1 不做自动配对流程（`/pair` 命令），只做手动绑定。
- v1 不做 Telegram WebApp / Mini App / /dashboard 仪表盘集成。
- v1 不做 Telegram 富媒体（图片/语音/文件）入站处理。
- v1 不做 Telegram inline 按钮写操作批准（只使用 "只读" 或 "回面板审批"）。
- v1 不做 Discord / Slack 等第二渠道（仅预留接口）。
- 不做渠道级 RAG 或独立记忆（所有渠道共享引擎会话，PRD-01 已定义渠道隔离）。