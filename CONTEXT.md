# API Monitor AI 上下文

最后更新：2026-07-22

这是 AI 维护者在改动 API Monitor 之前必须先读的第一个文件。它记录了当前架构、不可协商的规则、高风险文件，以及安全的维护命令。

## 当前架构

- 前端：React 19、Vite 8、Tailwind CSS 4、Zustand，以及 `@cloudflare/kumo` 2.13.2。
- UI 体系：基础控件与图表只用 Kumo。本地组件应当是业务组合组件或收窄的过渡包装。
- 后端：`backend-go/` 中的 Go 单进程后端，路由由 `backend-go/internal/manifest/manifest.go` 统一治理。
- 持久化：SQLite 仍是唯一的持久化存储。除非有明确的产品决策，否则不要替换它。
- Agent：`agent-rust/` 中的 Rust Agent，通过 Go 后端兼容 Engine.IO/Socket.IO 的服务接入。
- 托管代理运行时：仅 Linux 的 Agent 能力，负责收敛固定的 sing-box 运行时与按订阅划分的用户。控制面不安装、不管理 Xray。见 `docs/adr/0001-托管代理运行时.md`。
- 订阅计费：Agent 读取仅回环的 sing-box V2Ray Stats 计数器，持久化基线，并向 Go 订阅账本发送幂等增量。导入的外部节点永远不会计入该账本。
- 运行时数据：data、backup、uploads、secrets 以及本地环境文件被有意忽略，必须加以保护。

日常开发应默认 Go 后端掌握当前的路由面。Node 边车时代的 Express/模块文档属于历史资料，除非某个现行文件明确表示仍然适用。

## 高风险文件

- `src/js/pages/ServerPage.jsx`：大页面，包含终端、Docker、SFTP、指标以及破坏性的服务器操作。
- `src/js/pages/DnsPage.jsx`：Cloudflare 大型界面，包含 DNS、Workers、Pages、R2、隧道以及媒体预览。
- `src/js/pages/OpenAIPage.jsx`：OpenAI 兼容网关的配置、日志与模型路由。
- `src/js/components/MainLayout.jsx`：应用外壳、模块路由、侧边栏、页面宽度以及主题集成。
- `src/js/store.js`：模块注册表、可见性/排序设置、鉴权请求头以及全局 UI 状态。
- `backend-go/internal/server/server.go`：HTTP 分发、静态文件服务、清单路由、中间件以及实时入口。
- `backend-go/internal/manifest/manifest.go`：后端路由归属与鉴权模式的事实来源。
- `backend-go/internal/serveragent/service.go`：Rust Agent 协议、实时指标、终端、Docker 以及主机操作。
- `backend-go/internal/database` 以及设置/数据库维护文件：SQLite 生命周期与真实数据安全。

只有在有明确理由时才改动这些文件。避免在其中进行大范围格式化或顺手重写。

## 不可协商的规则

- 只用 Kumo：在适用场景下使用 Kumo 的 `Button`、`Input`、`Select`、`Tabs`、`Table`、`Dialog`、`DeleteResource`、`Toasty`、`Checkbox`、`Switch`、`Sidebar`、`Loader`、`Tooltip`、`Popover`、`DropdownMenu`、`TimeseriesChart`、`Meter` 以及 `ChartPalette`。
- 弹窗统一用 kumo 原生 `LayerDialog`（`@cloudflare/kumo/components/layer-dialog`），不要再用自研包装。`LayerDialog.Content` 的直接子元素只允许一个 `Title`、一个 `Body`，加可选的 `Description` 和 `Actions`；`Actions` 只能含一个 `Actions.Primary`。违反会在运行时抛错，`npm run ui:governance` 已把守。
- `LayerDialog` 的 `Content`/`Title`/`Description`/`Body` 只解构自己的固定字段，写在它们上面的 `className`/`ref` 会被静默丢弃（不报错但也不生效，例如 `Body` 上的 `gap-3` 从未起作用）。需要额外类名或 ref 时，放进 slot 内部自己的元素上；`npm run ui:governance` 已把守。
- 底部有多个并列业务按钮、或内容是依赖确定高度的画布/终端/编辑器时，保留 kumo 基础 `Dialog`，不要强行套 `LayerDialog`。
- `LayerDialog.Body` 的滚动内容层顶部无内边距（仅 `px-4.5 pb-4.5`，窄屏 `px-4 pb-4`），而外层 ScrollAreaViewport 是裁剪容器、Kumo Button/Input 的描边用外扩 `ring`；Body 首个子元素若自带 ring，上边框会被裁掉 1px。`src/css/app.css` 中的 `[data-drawer-content] div.base-ui-disable-scrollbar > div[role='presentation'][class~='px-4.5'][class~='pb-4.5']` 规则已统一补顶部内边距兜底。该规则依赖 Kumo 内部类名（`base-ui-disable-scrollbar`、`px-4.5 pb-4.5`），升级 Kumo 需重新确认选择器仍命中且不再需要兜底。已排查 Kumo 其余裁剪容器（Dialog/DropdownMenu/InputGroup/Collapsible/LayerCard/Sidebar/Toast），均无此问题。
- 破坏性删除确认应逐步迁移到 `dialog.deleteResource` / Kumo `DeleteResource`。非删除类确认可以使用普通确认流程。
- 每一处后端路由改动都必须体现在 Go 路由清单中，并通过路由治理检查。
- 默认不要删除或重写 `.env`、`data/`、`backup/`、`backend-go/data/`、`backend-go/internal/server/data/`、`node_modules/` 或 `public/`。
- 除非明确要求，不要替换 SQLite、拆分微服务或进行大规模架构重写。
- 绝不把机器网卡流量、托管节点原始流量、订阅用户用量或导入的外部节点流量合并成一个总量。
- 托管内部代理节点使用面板分配的端口，范围含端点 `45654-55654`；绝不要假定为 443 端口。分配在每个服务器内唯一，应用前由 Agent 做端口占用检查，且不改变导入的外部节点端口。
- 当前性能可接受时不要主动做性能优化。优先考虑治理、清晰度以及低风险的局部改进。
- 除非明确要求，不要回退现有的未提交用户改动或 AI 改动。
- SQLite 周期 WAL 维护只做 PASSIVE（`wal_checkpoint(PASSIVE)`）。禁止在自动路径
  使用 TRUNCATE/RESTART 重置型 checkpoint：面板有常驻轮询读者，modernc 驱动下它们在
  读者存续时会无视 busy_timeout 与 ctx 无限阻塞，是周期性 `database is locked`
  风暴的根因。主动截断/回收磁盘只走设置页「数据库压缩」或 GitHub 历史清理等用户动作。
- 全站时区统一由设置控制（`user_settings.time_zone`，唯一的时区控制点），业务一律经
  `internal/timeutil`（`LocationFromSettings`/`LocationFromName`/`ReadTimeZone`）取站点时区；
  禁止在业务代码中直接使用 `time.Local`/`time.UTC` 做日期归属（「几点执行」「星期几」
  「月/周期日界」「按『今天』的日期桶/文件名」）。绝对时刻（instant）写库/日志保持
  UTC/RFC3339（`time.Now().UTC().Format(time.RFC3339)` 正确，前端负责显示时区）。
  新增调度器必须 `cron.New(cron.WithLocation(站点时区))` 并带 TZ watcher（参考
  `internal/cronjobs`）；不改用 `cron.New()` 裸调用。已对齐：cronjobs、backup、
  uptime 维护窗口、notification、openai analytics、system API 日报、订阅计费/用量周期
  （`subscriptionledger.CycleWindow`/`planCycleWindow`）。CI 由 `tools/tz-governance-check.mjs`
  （含在 `governance:check`）把守，新增功能默认遵守。
- 过渡时长与缓动统一走 motion token：源变量 `--motion-duration-*`/`--motion-ease-*` 定义在
  `app.css` 的 `:root`，`@theme inline` 仅映射为 Tailwind `duration-*`/`ease-*` 工具类
  （`@theme inline` 会内联取值且不落盘变量定义，源变量必须放 `:root`，否则 `var()` 悬空）。
  禁止在 `src/js/components/adminai/**` 与 `app.css` 的 `askai-*` 规则里裸写毫秒数或
  `cubic-bezier(...)`；循环动画周期（`infinite`）与 `animation-delay` 不在此列。
  `tools/motion-governance-check.mjs`（含在 `governance:check`）把守，其余区域先以 warning
  增量迁移。

## AI 维护命令

快速本地审计：

```bash
npm run audit:fast
```

完整审计，包含 Go 测试、路由清单，以及针对运行中的 Go 后端的后端冒烟测试：

```bash
npm run audit:full
```

仅生成可清理工作区报告：

```bash
npm run clean:check
```

只删除受清理允许清单保护的可再生缓存/构建产物：

```bash
npm run clean:workspace
```

核心检查：

```bash
npm run governance:check
npm run ui:governance
npm run lint
npm test
npm run backend-go:test
node tools/backend-route-inventory.mjs
```

后端冒烟测试需要位于 `API_MONITOR_BASE_URL` 或 `http://127.0.0.1:3000` 的 Go 后端：

```bash
npm run backend-go:smoke
```

## 清理策略

可安全清理的可再生目标：

- `.cache/`
- `.tmp/`
- `dist/`
- `backend-go/tmp/`
- `backend-go/api-monitor.exe`
- `agent-rust/target/`
- 被 `tools/workspace-cleanup.mjs` 匹配到的被忽略的临时 trace/测试文件

始终保留：

- `.env`
- `data/`
- `backup/`
- `backend-go/data/`
- `backend-go/internal/server/data/`
- `node_modules/`
- `public/`

`public/` 虽被忽略，但仍可能被静态服务或部署流程使用。删除或重写它之前必须明确确认。

## UI 例外清单

允许的 UI 例外记录在 `docs/standards/重构验证与例外清单.md`。后续审计在把硬编码颜色或文件输入模式标记为回归之前，应先参考该文件。

当前已知例外分组：

- `BrandIcon` 中的品牌色。
- 二维码的深/浅色以及二维码图片背景。
- 为保证 xterm 可读性的终端回退色。
- 媒体预览的黑/白背景。
- 遗留的 ECharts 颜色，仅在改动相关图表时迁移。
- 浏览器文件选择器所需时隐藏的原生文件输入。

## 重构顺序

对于超大文件，只有在相关改动足以支撑时才拆分。按以下顺序进行：

1. 常量与纯函数。
2. Hooks。
3. 弹窗。
4. 表格与面板。
5. 页面容器。

每一步都要保持可独立验证。

## 多窗口 Agent 协作

适用于多个 Agent 窗口同时针对本仓库运行不同任务的情况。核心原则：冲突只允许发生在受控、串行的合并时刻，绝不发生在共享工作区。

### 任务跟踪（GitHub Issues）

- 所有进行中的开发工作都以 GitHub Issues 形式跟踪在 `iwvw/API-Monitor` 上，使用 `backlog`/`in-progress`/`done` 标签。每个任务对应一个 issue；敏感细节保留在仓库文件中，并在 issue 正文里引用它们。
- 开始工作前：找到或创建该任务的 issue，置为 `in-progress`，并在 issue 正文中注明文件域与分支。绝不触碰另一个进行中 issue/任务持有的文件。
- 完成后，关闭该 issue（或置为 `done`），并注明所使用的验证命令。具体闭环流程（进度检查、完成判定、标签/关闭命令、常见坑）见 `docs/guides/待办任务闭环流程.md`。
- `docs/archive/多Agent协作登记.md` 已不再作为协作入口，仅作历史记录保留。

### 文件归属（第一道防线）

- 归属以进行中的任务为单位（见上方任务跟踪）：每个任务在其 issue 正文中声明自己的文件域。
- 硬性规则：同一时刻一个文件最多由一个进行中的任务持有。
- 单一归属文件（同一时刻只允许一个任务修改）：`src/js/pages/ServerPage.jsx`、`src/js/pages/DnsPage.jsx`、`src/js/pages/OpenAIPage.jsx`、`src/js/components/MainLayout.jsx`、`src/js/store.js`、`backend-go/internal/server/server.go`、`backend-go/internal/manifest/manifest.go`、`backend-go/internal/serveragent/service.go`。
- 天然可并行：backend-go/（Go）、src/js/（前端）、agent-rust/（Rust）是彼此独立的域。

### 提交节奏（保持工作区干净）

- 只要完成一个可独立验证的步骤就立即提交；不要累积未提交的改动。
- 开始前检查 `git status`；如果他人有未提交的改动，先协调或等其提交，不要在其之上继续叠加。
- **工作区仍有未提交更改（任何任务留下）时，禁止推送（git push）**：只可提交自己域的改动，推送必须等到工作区干净、且经确认无他人未提交内容后进行。误推送他人未完成的工作会造成跨窗口污染。
- 遵循 `git log` 中现有的提交信息风格。

### 单一集成者

- 由一个指定的窗口执行最终集成：合并、完整审计（`npm run audit:full`）以及发布。其他窗口只提交改动，不合并。
- 对于大型跨模块工作，每个 Agent 在自己的 `agent/<task>` 分支上工作，仅在集成窗口合并回 `dev`。

### 共享资源互斥

- 3000 端口 / Go 后端进程：只允许一个窗口启动它；其他窗口只读代码或改用其他端口。
- SQLite（`data/`、`backend-go/data/`）：同一时刻只允许一个任务执行迁移或备份脚本。
- 慢命令（`npm run audit:full`）：串行执行以避免相互干扰。
- `dist/`、`node_modules/`、`.cache/`：可并发写入但可再生；冲突无害。
