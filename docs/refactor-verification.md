# 重构验收记录

## 2026-06-06 Kumo-only 完全收口

- 任务：完成剩余 Kumo-only 控件迁移，迁移 Uptime 图表，清理旧 Vue/原生前端，仅保留 React + Kumo 入口。
- 修改范围：
  - `src/js/pages/UptimePage.jsx`：从 Chart.js canvas 生命周期迁移到 Kumo `TimeseriesChart` + ECharts。
  - `src/js/pages/OpenAIPage.jsx`、`GeminiCliPage.jsx`、`QwenPage.jsx`、`PaasPage.jsx`、`NotificationPage.jsx`、`TotpPage.jsx`、`ServerPage.jsx`：剩余原生 `button/select/input/textarea` 全部替换为 Kumo `Button/Input/Textarea/Select` 或 Kumo Button 分段控件。
  - 删除旧 Vue 前端入口与资产：`src/js/main.js`、`src/js/template-loader.js`、`src/templates/`、旧 `src/js/modules/*.js`、旧 `src/js/stores/`、未引用旧 composables、旧 `src/css/*.css`。
  - 保留 React 运行时 helper：`src/js/modules/dialog.js`、`toast.js`、`utils.js`、`kumoTabs.js` 和 `src/js/composables/useTableResize.js`。
  - 移除 `chart.js`、`vue`、`pinia` 依赖；React 入口显式加载 `@xterm/xterm/css/xterm.css`。
- 静态扫描：
  - `rg -n --pcre2 '<(?-i:button|select|input|textarea)\b' src/js/pages src/js/components -S`：0 命中。
  - `rg -n "chart\.js|new Chart|Chart\." src package.json package-lock.json -S`：0 命中。
  - `rg -n "\b(vue|pinia|chart\.js)\b|from 'vue'|createApp\(|new Vue|window\.vueApp" package.json package-lock.json src -S`：0 命中。
  - 旧入口扫描仅保留 `src/index.html` 的 `/js/main.jsx`。
- 构建结果：`npm run build` 通过；仅保留 Vite chunk size warning。
- Kumo-only 例外：无新增例外。
- 浏览器验证：本段仅记录静态与构建验收；全路由 browser smoke 仍建议在最终发布前单独执行。

## 2026-06-06 SettingsPage 接口对接验证

- 任务：补齐系统设置页与后端/旧前端设置接口的对接，并让 React 导航读取模块显隐和顺序。
- 修改范围：
  - `src/js/pages/SettingsPage.jsx`
  - `src/js/store.js`
  - `src/js/components/MainLayout.jsx`
  - `src/js/pages/PaasPage.jsx`
  - `src/js/pages/TotpPage.jsx`
  - `docs/refactor-progress.md`
- 构建结果：`npm run build` 通过；仅保留 Vite chunk size warning。
- 静态扫描：
  - `SettingsPage.jsx` 中 `<button>/<select>/<input>/<textarea>` 为 0。
  - `DialogContent`、`TabsList`、`TabsTrigger`、`@cloudflare/kumo/components/tabs` 为 0。
  - React 导航/设置扫描未发现已删除模块；仅 `GeminiCliPage.jsx` 保留 GCLI OAuth 文案里的 `Google Antigravity`。
- Kumo-only 例外：无新增例外。
- 后续风险：仍需浏览器 smoke 确认 `/settings` 各 tab 的接口返回状态，以及 `/music` 白屏修复在当前 dev server 中已生效。

本文档记录 Kumo-only React 重构期间的构建、浏览器、路由和例外验证结果。

## 验收规则

每次 agent 完成一个明确任务后，至少记录：

- 日期
- 任务
- 修改范围
- `npm run build` 结果
- 浏览器验证路由
- console error 结果
- Kumo-only 例外
- 后续风险

## 记录

### 2026-06-06 基线检查

- 任务：读取 `docs/refactor-progress.md` 并评估当前迁移进度。
- 修改范围：文档计划与基线记录。
- 构建结果：`npm run build` 通过。
- 浏览器验证：尚未执行全路由浏览器 smoke。
- 已知风险：
  - 当前 Git 工作区已收回为未暂存状态，但仍有大量未提交重构改动。
  - Cloudflare DNS 仍标记为半完成。
  - 表格列宽拖动为部分完成。
  - 独立 `ai-chat` 删除已在当前工作区发生，仍需独立提交。
- Kumo-only 例外：暂未确认必须例外。

### 2026-06-06 Phase 0 收口

- 任务：建立无人值守执行基线。
- 修改范围：
  - 新增 `docs/refactor-next.md`
  - 新增 `docs/KUMO_MIGRATION_RULES.md`
  - 更新 `docs/refactor-progress.md`
  - 删除未跟踪临时文件 `fix-switches*.js`、`old_diff*.txt`、`tmp_index.html`、`tmp_openai_fix.js`
  - 删除未被引用的本地 UI 包装组件 `src/js/components/ModuleTabs.jsx`
- 构建结果：`npm run build` 通过；仅有 Vite chunk size 警告。
- 浏览器验证：待路由收口后执行。
- console error：待浏览器 smoke 执行后记录。
- Kumo-only 例外：无新增例外；本地 `ModuleTabs` 包装已删除。
- 后续风险：
  - 工作区仍有多页 React 迁移和 ai-chat 删除改动，需要分任务提交。
  - 旧 Vue 文件仍保留为参考，后续阶段统一删除或归档。

### 2026-06-06 DnsPage Kumo-only 控件收敛

- 任务：将 `DnsPage.jsx` 的明显原生 UI 控件替换为 Kumo 组件。
- 修改范围：
  - `src/js/pages/DnsPage.jsx`
  - `src/js/composables/useTableResize.js`
- 构建结果：`npm run build` 通过；仅有 Vite chunk size 警告。
- 浏览器验证：待全路由 smoke 阶段执行。
- console error：待浏览器 smoke 执行后记录。
- Kumo-only 例外：无新增例外。
- 后续风险：
  - `DnsPage.jsx` 仍需补完 DNS Records 详情、新增、编辑、删除和 Quick Switch 旧逻辑。
  - 其他页面仍有大量原生 `<button>/<select>/<input>/<textarea>` 需要后续收敛。

### 2026-06-06 小页面 Kumo-only 控件收敛

- 任务：收敛低风险页面中的原生表单和按钮。
- 修改范围：
  - `src/js/pages/AuthPage.jsx`
  - `src/js/pages/DashboardPage.jsx`
  - `src/js/pages/SettingsPage.jsx`
- 构建结果：`npm run build` 通过；仅有 Vite chunk size 警告。
- 静态扫描：上述 3 个页面中 `<button>`、`<select>`、`<input>`、`<textarea>` 已清零。
- 浏览器验证：待全路由 smoke 阶段执行。
- console error：待浏览器 smoke 执行后记录。
- Kumo-only 例外：无新增例外。
- 后续风险：`SettingsPage` 已从自绘侧边 tab 切换为 Kumo `Tabs`，后续浏览器 smoke 需确认窄屏下标签滚动表现。

### 2026-06-06 云厂商页面 Kumo-only 控件收敛

- 任务：收敛阿里云、腾讯云页面中的原生 tab、select、按钮和表单输入。
- 修改范围：
  - `src/js/pages/AliyunPage.jsx`
  - `src/js/pages/TencentPage.jsx`
- 构建结果：`npm run build` 通过；仅有 Vite chunk size 警告。
- 静态扫描：上述 2 个页面中 `<button>`、`<select>`、`<input>`、`<textarea>` 已清零。
- 浏览器验证：待全路由 smoke 阶段执行。
- console error：待浏览器 smoke 执行后记录。
- Kumo-only 例外：无新增例外。
- 后续风险：两个页面的云资源操作仍依赖真实账号/API，后续 smoke 以空状态和控制台错误检查为主。

### 2026-06-06 UptimePage Kumo-only 控件收敛

- 任务：收敛 Uptime 监测页面中的原生导航、过滤、搜索和表单输入。
- 修改范围：`src/js/pages/UptimePage.jsx`
- 构建结果：`npm run build` 通过；仅有 Vite chunk size 警告。
- 静态扫描：`UptimePage.jsx` 中 `<button>`、`<select>`、`<input>`、`<textarea>` 已清零。
- 浏览器验证：待全路由 smoke 阶段执行。
- console error：待浏览器 smoke 执行后记录。
- Kumo-only 例外：无新增例外。
- 后续风险：Chart.js 图表和 socket 心跳逻辑未改动，后续 smoke 需确认切换到添加/统计标签不会产生控制台错误。

### 2026-06-06 FileboxPage Kumo-only 控件收敛

- 任务：收敛文件柜页面中的原生 tab、取件码输入、文件输入、文本域、有效期选择和历史操作按钮。
- 修改范围：`src/js/pages/FileboxPage.jsx`
- 构建结果：`npm run build` 通过；仅有 Vite chunk size 警告。
- 静态扫描：`FileboxPage.jsx` 中 JSX `<button>`、`<select>`、`<input>`、`<textarea>` 已清零。
- 浏览器验证：待全路由 smoke 阶段执行。
- console error：待浏览器 smoke 执行后记录。
- Kumo-only 例外：二维码生成参数仍保留黑白色值，这是 QRCode 输出规范色，不属于 UI 主题色。
- 后续风险：Kumo `Input type="file"` 作为隐藏文件选择入口使用，后续 smoke 需确认点击拖拽区域仍能打开系统文件选择器。

### 2026-06-06 SelfHPage Kumo-only 控件收敛

- 任务：收敛自托管页面中的 OpenList 控件、文件操作按钮、右键菜单和定时任务表单。
- 修改范围：`src/js/pages/SelfHPage.jsx`
- 构建结果：`npm run build` 通过；仅有 Vite chunk size 警告。
- 静态扫描：`SelfHPage.jsx` 中 JSX `<button>`、`<select>`、`<input>`、`<textarea>` 与 `appearance-none` 已清零。
- 浏览器验证：待全路由 smoke 阶段执行。
- console error：待浏览器 smoke 执行后记录。
- Kumo-only 例外：无新增例外。
- 后续风险：OpenList 文件操作和 cron 执行依赖真实后端数据，后续 smoke 需重点检查空状态、弹窗打开、定时任务表单切换和右键菜单交互。

### 2026-06-06 页面宽度与 Tabs 统一

- 任务：统一不同模块页面宽度，并统一标签页到 Kumo `Tabs` 顶层导入和 `tabs` 数组写法。
- 修改范围：
  - `src/js/components/MainLayout.jsx`
  - `src/js/store.js`
  - `src/js/pages/DnsPage.jsx`
  - `src/js/pages/AliyunPage.jsx`
  - `src/js/pages/TencentPage.jsx`
  - `src/js/pages/SelfHPage.jsx`
  - `src/js/pages/ServerPage.jsx`
  - `src/js/pages/SettingsPage.jsx`
  - `src/js/pages/FileboxPage.jsx`
  - `src/js/pages/UptimePage.jsx`
  - `src/js/pages/NotificationPage.jsx`
  - `src/js/pages/MusicPage.jsx`
- 构建结果：`npm run build` 通过；仅有 Vite chunk size 警告。
- 静态扫描：
  - `@cloudflare/kumo/components/tabs` 已清零。
  - `TabsList` / `TabsTrigger` 已清零。
  - 页面根容器 `max-w-7xl mx-auto` / `max-w-4xl mx-auto` 已清零。
- 浏览器验证：待全路由 smoke 阶段执行。
- console error：待浏览器 smoke 执行后记录。
- Kumo-only 例外：无新增例外。
- 后续风险：顶部栏宽度切换在极窄移动端需 smoke 确认是否需要隐藏或折叠；`selectedValue` 仅适合 uncontrolled 初始值，受控业务标签页继续使用 `value/onValueChange`。

### 2026-06-06 Cloudflare / Dialog / 间距 / 中文化验证

- 构建：`npm run build` 通过；仅有 Vite chunk size 警告。
- 静态扫描：
  - React 源码中 `Dialog.Close asChild`、`DialogContent`、`TabsList`、`TabsTrigger`、`@cloudflare/kumo/components/tabs` 已清零。
  - 普通 React 页面根容器 `pb-20/pb-16/pb-24` 已清零；Music 保留固定播放器避让。
- 浏览器验证：
  - `/dns` 可渲染真实 Cloudflare 域名列表；页面文本未出现 AntiG / Antigravity 导航入口。
  - 顶部宽度切换 Tabs 为 sm 尺寸，高约 `24px`；Cloudflare 业务 Tabs 为正常尺寸，高约 `34px`。
  - Cloudflare `Table.ResizeHandle` 存在，拖动后首列从 `260px` 变为 `320px`。
  - Cloudflare “添加域名” Dialog 可打开，关闭后无残留 `role="dialog"` 节点。
  - `/music` 不白屏，未出现 `DialogContent is not defined`。
  - `/settings` 顶部/底部内容间距均为 `30px`，未出现 `Public API URL` 和 AntiG 文案。
- 控制台：
  - `/dns` console error：0。
  - `/music` console error：0。
  - `/settings` console error：0。
- 仍需后续扩大验证：
  - 全主路由自动 smoke 尚未落地。
  - 旧 Vue 参考文件和 `src/js/modules/*.js` 仍可能包含英文或 Antigravity 文案，但当前 React 入口不依赖旧 UI。

### 2026-06-06 ServerPage 主机实例展开与 Agent 功能补齐

- 任务：对照旧前端补齐主机实例列表展开/收起、Agent 部署/批量部署/升级入口，并将图表迁移到 Kumo Charts / ECharts。
- 修改范围：
  - `src/js/pages/ServerPage.jsx`
  - `package.json`
  - `package-lock.json`
- 构建结果：`npm run build` 通过；仅保留 Vite chunk size warning。
- 浏览器验证：
  - `/server` 可正常渲染主机实例列表，控制台 error 为 0。
  - 顶部工具条包含升级 Agent、批量部署、刷新、导出、导入、探测、新增主机。
  - IP 显示模式切换已恢复为 Kumo `Tabs`：明文 / 打码 / 隐藏。
  - 主机展开区可渲染系统/负载、CPU/内存 Kumo `TimeseriesChart`、GPU、网络和 Docker 面板。
  - Kumo `Dialog` 验证通过：升级 Agent、批量部署 Agent、导入主机、单机部署 Agent 均可打开并关闭。
- Kumo-only 例外：无新增例外；新增控件使用 Kumo `Button` / `Tabs` / `Dialog` / `Checkbox` / `Input` / `Textarea` / `Meter` / `TimeseriesChart`。
- 后续风险：
  - 单机/批量 Agent 的实际安装、卸载、升级动作依赖真实 SSH/Agent 环境，本次只做非破坏性打开与状态验证。
  - `ServerPage.jsx` 仍有历史管理/终端区域的原生表单控件，未纳入本次主机列表展开补齐范围。
