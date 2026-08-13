# 管理 AI Web 侧栏前端 PRD

最后更新：2026-08-13

> 本文件是「管理 AI」四份子 PRD 之一，对应 Web 侧栏（前端交互）。配套子 PRD：
>
> - [管理 AI 核心引擎与数据层](./管理AI核心引擎与数据层.md)（后端 Run Loop）
> - [频道接入框架与 Telegram](./管理AI频道接入与Telegram.md)（渠道）
> - [安全、审批与审计](./管理AI安全审批与审计.md)（权限闭环）
> - **[共通实现约定](./管理AI共通实现约定.md)**：实现本 PRD 前必须先读，内含所有源码接线片段。

## Problem Statement

后端引擎（PRD-01）会提供会话与流式能力，但用户需要一个真正"点开就能聊天"的入口。当前面板没有内置 AI 对话 UI；把用户引导去配置外部 MCP 客户端不符合产品心智。参照 Cloudflare Dashboard 的 "Ask AI" 侧栏（Agent Lee）与 OpenClaw 的 Control UI / WebChat：对话入口应该常驻在面板右上角，一个滑出式侧栏，随时可以问。

同时，管理 AI 不是普通聊天：回答会携带工具调用痕迹、待批准计划、图表等高信息密度内容。侧栏必须能渲染这些结构化内容，而不是纯文本气泡。

## Solution

在 `MainLayout` 顶层新增一个**全局 Ask AI 侧栏**（`AskAiPanel`），覆盖所有模块页面（不依赖当前正在浏览的页面路由）：

- 入口：顶部栏右侧的"对话"图标按钮（常驻）。
- 形态：右侧滑出式浮层面板，桌面 380px，移动端全屏覆盖；`panel open` 时给主内容保留/遮罩（选留半透明遮罩 + Esc/遮罩点击关闭）。
- 技术：非 Kumo `Sidebar`（那是导航布局组件），而是自建 `fixed inset-y-0 right-0 z-50` 浮层 + 动画；内部元素 100% 复用 Kumo 组件。
- 数据流：发起消息 → 调 `POST /api/admin-ai/messages` 拿 runId → EventSource 订阅 `/api/admin-ai/messages/stream` 流式渲染；清理沿用 `abortController`/关闭时断开。

## User Stories

1. 作为管理员，我想在任意页面右上角点开/收起 AI 侧栏，以便快速提问不打断当前工作。
2. 作为管理员，我想看到 AI 逐字流式输出，以便获得即时反馈。
3. 作为管理员，我想看到 AI 正在调用哪个接口、等待什么，以便理解它在做什么。
4. 作为管理员，我想在侧栏里看到会话历史并回切到旧会话，以便复查之前的结论。
5. 作为管理员，我想新建/删除会话，以便清理聊天记录。
6. 作为管理员，当 AI 需要一个写操作批准时，我想直接在面板里看到计划并批准/拒绝，以便不用离开页面。
7. 作为管理员，我想让 AI 回答中的代码块/命令可一键复制，以便快速使用。
8. 作为管理员，我想看到错误时给出可重试入口，以便对话被我打断后恢复。
9. 作为管理员，当对话出错或超时时，我要看到明确的中文提示而不是白屏。

## Functional Requirements

### 侧栏容器

- `AskAiPanel` 挂载在 `MainLayout` 顶层（`AppPageHeader` 区域右侧加入口按钮，参考 `SidebarStyleSwitchItems` L754-757 所在 header 区的实现方式）。
- 开合状态存 `store.js`（全局，不随路由丢失）。
- 桌面宽度 380px，高度 100vh，右侧滑出 + 200ms 过渡；移动端（`<sm`）全宽覆盖。
- 打开时主内容区加半透明遮罩（`bg-black/30`），点击遮罩关闭；Esc 关闭。
- 无障碍：面板焦点陷阱 + `role="dialog"` + aria-label。
- 关闭时终止进行中的 EventSource，恢复连接在下次打开时重建。

### 聊天视图

一个双向消息列表：

- 用户消息：右侧气泡；助手消息：左侧内容块（不做气泡框，用分隔线区分，避免大文本挤压）。
- 消息类型（由后端事件协议渲染）：
  - `text`：普通文本，支持简单 Markdown 渲染（加粗/行内代码/链接）；不引入完整 md 库，只做白名单子集（复用现有轻量渲染工具，若有）。
  - `code block`：用现有 `CodeEditor` 只读态（`QuickCommandBar.jsx:19` 先例）或等宽 pre + 复制按钮；行号可选。
  - `tool_call`：紧凑卡片，展示工具名 + 参数摘要 + 状态（运行中/成功/失败），失败显示原因；可用 Kumo `Banner`/`Tooltip`。
  - `approval`：批准卡（见下）；`error`：Kumo `Banner` tone=注意 + "重试"按钮；`done`：可折叠"用量/耗时"说明。
- 自动滚动到最新消息；用户上滚时不强制下拉。
- 输入区底部：Kumo `Input`/`Textarea`（多行）+ 发送 `Button` + 取消（进行中时显示）。

### 会话管理

- 侧栏头部提供会话下拉/切换器：显示「当前会话」+ 历史会话列表 + "新建会话" + "删除会话"。
- 删除会话走 `DELETE /api/admin-ai/sessions/:id`，删除前 Kumo `DeleteResource` 二次确认（遵循 CONTEXT.md 破坏性确认规则）。
- 会话历史懒加载：进入面板加载最近 N 条；滚动到顶加载更早。

### 写操作批准卡

- 当后端推送 `approval_required`：渲染计划正文（动作摘要、目标资源、参数，均来自审批快照）+ 两个主操作：批准 / 拒绝；展示 `expiresAt` 倒计时。
- 批准后展示执行结果（复用 `tool_result`）与状态徽标。
- 对应后端 API 见 PRD-04（`POST /api/admin-ai/approvals/:id/resolve` 等）。
- 批准卡是本功能一期唯一与安全 PRD 强耦合的 UI；非破坏性确认仍用普通 confirm 流。

### 连接与错误处理

- SSE 断线（无心跳/网络错误）：显示"连接已断开"，保留已渲染消息，提供"重试"/"继续"。
- 后端 429/限流/超时：显示中文错误 + 推荐稍后重试。
- 会话被并发执行占用时（后端拒绝）显示明确提示，引导等待或取消前一执行。

## Implementation Decisions

- 组件摆放：新建 `src/js/components/adminai/AskAiPanel.jsx` + 内部小件（`MessageList`、`ApprovalCard`、`ToolCallCard` 等），不塞进 `MainLayout` 内部代码。
- 状态存 `src/js/store.js`（全局 open/close + currentSessionId + running runId）。
- 事件解析：写 `src/js/modules/adminAiEvents.js`，把 SSE 事件行解析成 `Message[]` 结构（含 `content`/`blocks`）。
- 样式：遵循 Kumo token；不使用硬编码颜色（对照 `docs/重构验证与例外清单.md`）。
- 动画用现有 `AnimatedCollapse` 或纯 CSS transition；不新增动画库。
- 流式文本保留在内存中，结束时一次性 Persist（后端已在服务端持久化，前端不重复写库）。

## API Contract Draft

- 对应 PRD-01：`POST /api/admin-ai/messages`、`GET /api/admin-ai/sessions`、`GET /api/admin-ai/sessions/:id/messages`、`DELETE /api/admin-ai/sessions/:id`、`GET /api/admin-ai/cancel`。
- 对应 PRD-04：`POST /api/admin-ai/approvals/:id/resolve`（body `{action: 'approve'|'reject'}`）、`GET /api/admin-ai/approvals/:id`。

## Testing Decisions

- 组件测试（vitest + Testing Library）：侧栏开合、消息渲染各 block、批准卡交互、会话切换、断线重连提示。
- SSE 解析器单测（事件行 → Message 结构）。
- 浏览器烟测：打开面板 → 提问 → 流式输出 → 工具卡出现 → 批准一幕 → 关闭再开恢复连接。
- 验证命令：`npm run lint -- --quiet`、`npm run test`、`npm run build`；Kumo 基线无新增违规。

## Release Plan

1. 侧栏容器 + 开合状态 + 遮罩。
2. 消息列表 + 普通文本/代码块渲染 + 自动滚动。
3. SSE 接入 + 流式渲染 + 断线处理。
4. 工具调用卡 + 会话管理（切换/新建/删除）。
5. 批准卡（与 PRD-04 联调）。
6. 移动端适配 + 无障碍 + 全量验收。

## Acceptance Criteria

- 任意页面右上角可打开/关闭侧栏，状态跨路由保持。
- 提问后能实时看到流式文本，且不阻塞页面其它操作。
- AI 调用工具时显示工具卡；写操作展示批准卡并可批准/拒绝。
- 支持会话新建、切换、删除（带二次确认）。
- 断线/出错有中文提示与恢复路径。
- 移动端为全屏覆盖，可正常使用。
- 全部可见 UI 为中文；Kumo 静态基线无新增违规；lint/build/单测通过。

## Out of Scope

- 不做侧栏内的富文档编辑/音视频。
- 不做会话标题自动生成/重命名（v1 用时间戳标题）。
- 不做多窗口同步（同一用户浏览器多标签只各自维护 state，服务端以 sessionId 为准）。
- 不做 Markdown 完整排版引擎（仅白名单子集，超纲内容降级为代码块）。