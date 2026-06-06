# React + Kumo 重构状态

最后更新：2026-06-07

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
| 仪表盘 | `src/js/pages/DashboardPage.jsx` | 当前页面 | 系统概览、API 调用趋势、宿主机性能监控 |
| 主机实例 | `src/js/pages/ServerPage.jsx` | 当前页面 | 主机列表、Agent、SSH/SFTP、Docker、历史趋势 |
| 双因子认证 | `src/js/pages/TotpPage.jsx` | 当前页面 | TOTP 账号、分组、设置 |
| 文件柜 | `src/js/pages/FileboxPage.jsx` | 当前页面 | 上传、分享、取件码、历史 |
| 可用性监测 | `src/js/pages/UptimePage.jsx` | 当前页面 | 监测目标、状态、趋势 |
| 通知 | `src/js/pages/NotificationPage.jsx` | 当前页面 | 渠道、规则、历史 |
| OpenAI | `src/js/pages/OpenAIPage.jsx` | 当前页面 | 端点、模型、聊天、人设、历史 |
| GCLI | `src/js/pages/GeminiCliPage.jsx` | 当前页面 | Gemini CLI 账号、矩阵、日志 |
| 通义千问 | `src/js/pages/QwenPage.jsx` | 当前页面 | Qwen 代理、凭证、矩阵、日志 |
| PaaS | `src/js/pages/PaasPage.jsx` | 当前页面 | Koyeb / Fly.io 监控与操作 |
| Cloudflare | `src/js/pages/DnsPage.jsx` | 当前页面 | 账号、域名、DNS、Workers、Pages、R2、Tunnels |
| 阿里云 | `src/js/pages/AliyunPage.jsx` | 当前页面 | DNS / ECS |
| 腾讯云 | `src/js/pages/TencentPage.jsx` | 当前页面 | DNS / CVM |
| 自建服务 | `src/js/pages/SelfHPage.jsx` | 当前页面 | OpenList、文件、定时任务 |
| 音乐 | `src/js/pages/MusicPage.jsx` | 当前页面 | 网易云音乐搜索、播放、收藏、歌词 |
| 系统设置 | `src/js/pages/SettingsPage.jsx` | 当前页面 | 模块、主题、安全、日志、数据库维护 |

## 壳层与导航

- `MainLayout.jsx` 使用 Kumo `Sidebar`。
- URL path 与当前模块同步，支持浏览器 back/forward。
- 模块显隐和顺序从后端用户设置加载。
- 页面宽度支持 `标准 / 宽屏 / 全宽` 三档，并持久化到 `app_page_width_mode`。
- 顶栏当前使用 `AppPageHeader` 组合 Kumo `Breadcrumbs` 与 Kumo `Tabs`，用于满足紧凑高度和 450px 断点。

## Kumo-only 状态

已确认的当前基线：

- `src/js/pages` 与 `src/js/components` 中原生 `<button>/<select>/<input>/<textarea>` 静态扫描为 0。
- 旧 `TabsList` / `TabsTrigger` / `@cloudflare/kumo/components/tabs` 写法已清零。
- React 源码不再依赖 Vue、Pinia、Chart.js。
- Toast helper 已接入 Kumo Toasty。
- 图表优先使用 Kumo `TimeseriesChart`、`Meter`、`ChartPalette`。
- 复制命令和 token 的场景已开始使用 `ClipboardText`。

## 已废弃或移除

- 独立 `ai-chat` 主导航模块。
- 独立 Antigravity 页面模块。
- 旧 Vue 模板加载方案。
- Chart.js 图表基线。
- 本地 `ModuleTabs` 包装组件。

注意：`GeminiCliPage.jsx` 仍有 `Google Antigravity` OAuth 文案，这是共享 Client 凭证说明，不代表独立模块恢复。

## 当前待收口点

1. 删除确认迁移到 Kumo `DeleteResource`。
2. 官方 `PageHeader` block 是否安装并替换本地紧凑 `AppPageHeader`，需在适配后执行，不能直接 barrel import。
3. 全路由自动 browser smoke 尚未建立。
4. Cloudflare、PaaS、Server、Music 等依赖真实账号/Agent 的流程仍需真实环境验证。
5. 移动端和窄屏布局需要持续做视觉回归，尤其是展开卡片、图表轴标签和顶部栏。
6. 硬编码颜色、局部 class override 仍需周期性扫描。

## 参考资料

- Kumo 规则：[KUMO_MIGRATION_RULES.md](./KUMO_MIGRATION_RULES.md)
- 前端实践：[FRONTEND_BEST_PRACTICES.md](./FRONTEND_BEST_PRACTICES.md)
- 组件注册表：[reference/kumo-component-registry.md](./reference/kumo-component-registry.md)
