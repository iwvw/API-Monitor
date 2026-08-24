# Handoff: Ask AI 侧栏复刻 + 管理 AI PRD-04/03（ZCode 会话交接）

生成时间：2026-08-14
来源会话工作区：`C:\Users\DSUK\.zcode\workspace\default`（本文件已迁移至项目内）
项目工作目录：`E:\Code\API-Monitor`（分支 dev）

## 一、任务背景

1. **Cloudflare Ask AI 侧栏调研**（已完成）：官方实机抓取结构/样式/交互，产出
   `docs/cloudflare-ask-ai-sidebar-reference.yaml`（颜色 token 实测值、各元素 class、审批 UI、响应式、全屏模式全记录）。
   结论：官方 = React + `@cloudflare/kumo` 组件库 + Tailwind，非手绘。
2. **侧栏复刻**（已完成，未提交）：把 `src/js/components/adminai/*` 重写为 Cloudflare 风格。
3. **PRD-04 安全审批与审计**（已完成，未提交）+ **PRD-03 频道接入 Telegram**（已完成，未提交）。

## 二、已完成（截至本交接）

### dev 分支已推送
- `1d8f356` feat(adminai): 管理 AI 模块 Round 1（引擎/SSE/Web 侧栏/审批）+ Cloudflare 侧栏调研参考；openai 模型池与若干页面调整
  （含 40 文件：adminai 后端 3 文件、前端 4 组件 + AdminAIPage、5 份 PRD 文档、YAML 参考、openai 重构等）

### 未提交改动（等用户审阅单持有者文件后提交）
**单持有者文件（待审）**：
| 文件 | 改动 |
|---|---|
| `backend-go/internal/manifest/manifest.go` | +9 条路由：/approvals、/audit、/channels/{id}/start\|stop\|status、/channel-bindings、/channel-bindings/{id} |
| `backend-go/internal/server/server.go` | newServer 加 `StartBackground()`+`SetupChannels()`；Shutdown 加 `StopBackground()`+`StopAllChannels()`；serveGoRoute case +5 路径 |
| `backend-go/internal/system/route_contracts.go` | +7 条 admin-ai 契约 + settings 新键（另含 openai/routing 契约补登） |
| `src/js/components/MainLayout.jsx` | Ask AI 按钮 Bot→Sparkle（模型网关图标）；主画布 `md:mr-[var(--askai-sidebar-w,0px)]` 压缩联动 |

**非单持有者改动**：
- 前端：`AskAiPanel.jsx`（自绘浮层 450px/z-1150/translateX、Esc 关闭、拖宽手柄、58px header、点阵背景、空状态云+建议卡、隐私条、多行 textarea 自动增高 256px、Send→Stop、Expand 全屏、CSS 变量 --askai-sidebar-w）、`MessageList.jsx`（用户 bg-kumo-fill 右对齐/助手 bg-kumo-base ring、推理折叠、思考过程折叠、代理标签、反馈按钮）、`ApprovalCard.jsx`（4 按钮+N 处更改+请求更改 0/1000）、`ToolCallCard.jsx`（→ Running 行）、`app.css`（cloud-orb 动画）、`askAiEvents` 事件解析
- 后端：`backend-go/internal/adminai/` 下新增 `approvals.go`（列表/详情/30min TTL/后台清理 goroutine/多键 settings）、`audit.go`（executions+tool_calls 合并视图）、`channels.go`（channels/bindings CRUD + start/stop/status + 入站→RunLoop→出站）、`channel/channel.go`（Channel 接口+Registry）、`channel/telegram.go`（长轮询/getMe/allowlist/@提及/4096 分片/编辑静默）；`service.go`（channels/bindings 表 + ai_access_audit.channel 列 ensureSQLiteColumn 表缺失时静默跳过 + 路由分发）；`engine.go`（审批 TTL 30min、SSE 补 reasoning + approval method/path/bodySnapshot）；`approvals_test.go`（8 单测全绿）

### 关键修复（含在未提交改动中）
- **response.OK 包装坑**：后端统一 `{success, data:{...}}`，前端解析必须 `(data.data || data).xxx`——AskAiPanel 三处（loadSessions/loadMessages/创建会话）已修，否则 `sessions.find is not a function`。

## 三、下一步（待办）

1. **用户审阅单持有者文件**（上表 4 文件）→ 通过后 `git add -A && commit && push origin dev`
2. 部署：按惯例 `flyctl deploy apimnt`（若用户要求发版）
3. Telegram 频道管理前端页（渠道列表/绑定管理 UI，对应 channels.go API）
4. `AdminAIPage.jsx` 设置页完善（settings 多键表单：总开关/默认模型/写开关/工具上限/超时/上下文窗口/审计保留）
5. 可选：审计查询前端页（/api/admin-ai/audit）、AI 接入设置页扩展（PRD-04 管理面）

## 四、技术要点（勿丢）

- **SSE 事件名**：meta / reasoning / delta / tool_start / tool_result / approval_required / error / done；`tool_result`（前端已改对）
- **审批流**：写操作 → approval_required 事件 + `admin_ai_approvals` pending（expires_at=30min）→ 前端批准卡 → POST /approvals/{id}/resolve {action:approve|reject} → 引擎通道唤醒
- **频道授权**：`admin_ai_channel_bindings`（channel_id+channel_user_id→panel_user_id），authorize 闭包注入；Telegram 写操作默认只读（全局写开关未开时被拒）
- **前端样式对齐**：kumo-fill=oklch(0.922 0 0) 用户气泡 / kumo-base=白 助手气泡 / kumo-overlay=oklch(0.9875 0 0) 侧栏底 / 文本 #313131；框选 `rounded-xl px-4 py-3 text-sm`；推理 12px neutral-400
- **侧栏交互规格**：450px 固定（可拖 320~800）、Esc 关闭、Enter 换行 Ctrl+Enter 发送、<768px 全屏覆盖、Expand 全屏（Collapse to sidebar 收回）、打开时主内容压缩（--askai-sidebar-w 变量）
- **adminai Service 生命周期**：StartBackground()（审批清理）/ StopBackground() / SetupChannels() / StopAllChannels()，server.go 已接

## 五、验证命令

```bash
npm run backend-go:test   # 后端全量（adminai 8 单测在内）
npm run governance:check
npm run lint
npm run build             # vite build
```

## 六、安全注意

- 不暴露/提交 ENCRYPTION_KEY、JWT_SECRET、ADMIN_PASSWORD
- Telegram `botToken` 必须 `secure.EncryptJSON` 加密存储（已实现）
- 不删除 data/、backup/、.env、node_modules/
- `.playwright-cli/` 已加入 .gitignore（本地日志）