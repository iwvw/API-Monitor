# React + Kumo 重构状态

最后更新：2026-06-09

本文档记录当前事实，不再作为旧 Vue 迁移流水账。后续执行项见 [refactor-next.md](./refactor-next.md)，验证结果见 [refactor-verification.md](./refactor-verification.md)。

## 当前目标

API Monitor 前端收口到：

- React 19
- Zustand
- `@cloudflare/kumo` 2.5.0
- Tailwind CSS v4
- ECharts + Kumo Charts

旧 Vue、Pinia、模板加载器、Chart.js 和旧原生 CSS 不再是当前前端基线。

## 当前主页面

| 模块 | 页面文件 | 状态 | 说明 |
|------|----------|------|------|
| 仪表盘 | `src/js/pages/DashboardPage.jsx` | 当前页面 | 系统概览、30 天 API 趋势缓存、宿主机性能监控、主机状态点 |
| 主机实例 | `src/js/pages/ServerPage.jsx` | 当前页面 | 主机列表、紧凑表格、国家/国旗、Agent、SSH/SFTP、Docker、历史趋势 |
| 双因子认证 | `src/js/pages/TotpPage.jsx` | PRD 已实现 | TOTP/HOTP、分组、导入预览/提交、加密备份、reveal 审计 |
| 文件柜 | `src/js/pages/FileboxPage.jsx` | PRD 已实现 | 上传、文本分享、取件码、访问密码、下载限制、清理、访问日志、策略设置 |
| 可用性监测 | `src/js/pages/UptimePage.jsx` | PRD 已实现 | HTTP/Keyword/JSON Query/TCP/Ping/DNS/Push、状态机、状态页、维护窗口、导入导出 |
| 通知 | `src/js/pages/NotificationPage.jsx` | PRD 已实现 | 渠道、规则、事件 catalog、模板预览、dry-run、条件引擎、历史 |
| OpenAI | `src/js/pages/OpenAIPage.jsx` | 当前页面 | 端点、模型、聊天、人设、历史 |
| GCLI | `src/js/pages/GeminiCliPage.jsx` | 当前页面 | Gemini CLI 账号、矩阵、日志 |
| 通义千问 | `src/js/pages/QwenPage.jsx` | 当前页面 | Qwen 代理、凭证、矩阵、日志 |
| PaaS | `src/js/pages/PaasPage.jsx` | 当前页面 | Koyeb / Fly.io 监控与操作 |
| Cloudflare | `src/js/pages/DnsPage.jsx` | 当前页面 | 账号、域名、DNS、Workers、Pages、R2、Tunnels |
| 阿里云 | `src/js/pages/AliyunPage.jsx` | 当前页面 | DNS / ECS |
| 腾讯云 | `src/js/pages/TencentPage.jsx` | 当前页面 | DNS / CVM |
| 自建服务 | `src/js/pages/SelfHPage.jsx` | 当前页面 | OpenList、文件、定时任务 |
| 音乐 | `src/js/pages/MusicPage.jsx` | PRD 已实现 | 网易云音乐登录、搜索、歌单、播放、队列、歌词、代理与 service 拆分 |
| 系统设置 | `src/js/pages/SettingsPage.jsx` | PRD 已实现 | 模块、主题、安全、日志、数据库维护、导入 preview/commit |

## 工具箱模块重构状态

双因子认证、音乐、可用性监测、文件柜、通知、系统设置已经按当前 PRD 完成前后端收口。文档中的旧阶段进度不再限制当前状态；后续只保留真实外部环境验证、浏览器 smoke、长期维护扫描。

详细 PRD：

- 工具箱整体：[toolbox-modules-refactor-prd.md](./toolbox-modules-refactor-prd.md)
- 可用性监测专项：[uptime-kuma-aligned-prd.md](./uptime-kuma-aligned-prd.md)

当前判断：

- TOTP：secret 透明加密存储、显式 reveal 审计、导入 preview/commit、加密备份导入导出、HOTP 递增和 otplib 配置隔离已落地。
- 音乐：Cookie 后端加密保存；auth、NCM client、catalog、unblock、audio proxy 已拆出 service；代理 allowlist 已收紧；前端 Audio 生命周期已清理。
- Uptime：纯状态机、probe registry、状态持久化、HTTP/Keyword/JSON Query/TCP/Ping/DNS/Push adapters、公开状态页、badge、维护窗口、导入导出和前端 tabs 已落地。
- 文件柜：metadata 已从 JSON 迁移到 SQLite；访问日志、访问密码、下载次数限制、阅后即焚、清理任务、持久化策略设置和前端策略页已落地。
- 通知：已接入 `ToolboxEventBus`；事件 catalog、模板预览、规则 dry-run、条件引擎、限流阻断、批量/重试和维护抑制已落地。
- 系统设置：`SettingsRegistry`、migration self-check、数据库导入 preview/commit、备份/压缩/分析和设置页 UX 已落地。

## 壳层与导航

- `MainLayout.jsx` 使用 Kumo `Sidebar`。
- URL path 与当前模块同步，支持浏览器 back/forward。
- 模块显隐和顺序从后端用户设置加载。
- 页面宽度支持 `标准 / 宽屏 / 全宽` 三档，并持久化到 `app_page_width_mode`。
- 侧栏折叠状态持久化到后端用户设置，并继续保留本地即时响应。
- 暗色主题启动脚本在 `src/index.html` head 内提前执行，减少启动闪白。
- 顶栏当前使用 `AppPageHeader` 组合 Kumo `Breadcrumbs` 与 Kumo `Tabs`，用于满足紧凑高度和 450px 断点。

## Kumo-only 状态

已确认的当前基线：

- `src/js/pages` 与 `src/js/components` 中原生 `<button>/<select>/<input>/<textarea>/<table>/<dialog>` 静态扫描为 0。
- 旧 `TabsList` / `TabsTrigger` / `@cloudflare/kumo/components/tabs` 写法已清零。
- React 源码不再依赖 Vue、Pinia、Chart.js。
- Toast helper 已接入 Kumo Toasty。
- 全局删除确认已通过 `GlobalDialogHost.jsx` 接入 Kumo `DeleteResource`，显式删除/移除/销毁类 confirm 会自动走 DeleteResource。
- 图表优先使用 Kumo `TimeseriesChart`、`Meter`、`ChartPalette`。
- 复制命令和 token 的场景已开始使用 `ClipboardText`。
- 展开/收起统一使用 `AnimatedCollapse`，该组件基于 Kumo `Collapsible` 与 Base UI 高度变量恢复高度/透明度过渡。
- `execCommand` 扫描为 0。

## 2026-06-09 最新收口

- Dashboard：API 调用趋势优先使用缓存；趋势窗口调整为 30 天；新增宿主机性能监控卡片；主机概览卡新增右侧小状态点，按主机在线/异常/离线显示。
- Server：紧凑表格视图完成哪吒风格信息密度优化；名称列收窄；位置列承载国旗；右键列显示/隐藏；到期时间用于剩余时长；负载精简为一个数值；主机详情、Docker 子面板和图表展开恢复动画。
- Charts：CPU/GPU 图表接入温度曲线；小尺寸图表减少轴标签；主机历史数据进入页面后保留并实时刷新；Agent 更新后前端节奏问题需继续真实环境观察。
- Settings / Shell：侧栏折叠偏好云端持久化；页面宽度/主题切换放入侧栏底部；系统设置文案和二级 tabs 尺寸按当前规范收口。
- Runtime：`user_settings.theme_mode` 与侧栏字段做了兼容迁移；插件 ZIP 下载改为 Node 压缩实现，不再依赖容器内 PowerShell。
- Motion：`AnimatedCollapse` 重新接入 Kumo `Collapsible.Panel` 的 data 状态与 `--collapsible-panel-height`，恢复列表展开/收起动效。

## 已废弃或移除

- 独立 `ai-chat` 主导航模块。
- 独立 Antigravity 页面模块。
- 旧 Vue 模板加载方案。
- Chart.js 图表基线。
- 本地 `ModuleTabs` 包装组件。

注意：`GeminiCliPage.jsx` 仍有 `Google Antigravity` OAuth 文案，这是共享 Client 凭证说明，不代表独立模块恢复。

## 当前待收口点

1. 真实账号/真实环境验证仍需进行：音乐播放源、解锁代理、Email/Telegram 投递、Uptime 真实目标、Agent/SSH/Docker/Cloudflare/PaaS、插件 ZIP 下载等。
2. 全路由自动 browser smoke 尚未建立；当前只完成构建、单测和静态扫描。
3. 官方 `PageHeader` block 是否安装并替换本地紧凑 `AppPageHeader`，仍是设计系统层面的后续决策。
4. 移动端和窄屏布局需要持续视觉回归，尤其是主机紧凑表格、展开区图表、终端和 Docker 表格。
5. 硬编码颜色、局部 class override、旧动画类和阴影类仍需周期性扫描。

## 参考资料

- Kumo 规则：[KUMO_MIGRATION_RULES.md](./KUMO_MIGRATION_RULES.md)
- 前端实践：[FRONTEND_BEST_PRACTICES.md](./FRONTEND_BEST_PRACTICES.md)
- 组件注册表：[reference/kumo-component-registry.md](./reference/kumo-component-registry.md)
