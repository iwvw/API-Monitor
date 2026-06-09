# 验收记录

最后更新：2026-06-09

本文档记录当前可复核的静态扫描、构建和浏览器验证结果。历史长篇迁移记录已压缩到 [refactor-progress.md](./refactor-progress.md)。

## 验收规则

每次完成一个明确任务后至少记录：

- 日期。
- 任务。
- 修改范围。
- 运行的命令。
- 结果。
- 浏览器验证路由。
- Kumo-only 例外。
- 后续风险。

## 2026-06-09 Dashboard / 主机页 / 设置偏好 / 动效收口

任务：同步最近一轮控制台和主机页体验优化，修复容器运行问题与全站展开/收起动画回退，并更新文档状态。

修改范围：

- Dashboard：API 调用趋势使用缓存优先策略；趋势窗口为 30 天；新增宿主机性能监控；主机概览卡右侧显示小状态点。
- Server：紧凑表格视图优化列宽、负载、国旗位置、右键列菜单、到期剩余时长、流量/网速小框、Docker 展开区和 CPU/GPU/网络图表。
- Settings / Layout：侧栏折叠偏好接入后端用户设置；页面宽度和主题切换位于侧栏底部；暗色启动脚本前置到 `index.html`。
- Runtime：修复 `user_settings.theme_mode` 等设置字段兼容迁移；插件 ZIP 下载改用 Node 压缩能力，避免 Linux 容器内 `powershell: not found`。
- Motion：`AnimatedCollapse` 基于 Kumo `Collapsible` 和 Base UI `--collapsible-panel-height` 恢复高度/透明度过渡。

相关提交：

- `524f1f1 chore: polish dashboard and settings persistence`
- `430bd2f fix: restore collapsible transition animations`

运行命令：

```bash
npm run lint
npm run build
git diff --check
```

额外样式产物检查：

```powershell
$css = Get-ChildItem dist\assets -Filter *.css | Sort-Object LastWriteTime -Descending | Select-Object -First 1 | Get-Content -Raw
$css.Contains('--collapsible-panel-height')
$css.Contains('[data-open]')
$css.Contains('[data-closed]')
$css.Contains('[data-ending-style]')
$css.Contains('height,opacity')
```

结果：

- `npm run lint` 通过。
- `npm run build` 通过，仅保留 Vite chunk size warning。
- `git diff --check` 通过，仅提示 Windows 换行转换 warning。
- Collapsible 样式产物检查全部为 `True`。

浏览器验证路由：

- 本轮未启动或重启服务；未做完整登录后 browser smoke。
- 用户截图确认主机表格当前可渲染并展示展开行，动画修复通过代码路径与 CSS 产物验证。

Kumo-only 例外：

- `AnimatedCollapse` 是业务适配层，不是自写基础 UI。内部仍使用 Kumo `Collapsible.Root` / `Collapsible.Panel`。

后续风险：

- 主机页仍需真实浏览器交互回归：展开/收起动画体感、chart 展开卡顿、SSH、SFTP、Docker 更新、右键列菜单和移动端压缩布局。
- Agent 温度/功耗和 1.5s 指标刷新稳定性仍依赖真实 Agent 版本和网络状态。

## 2026-06-08 工具箱 PRD 前后端一次性收口

任务：根据用户最新要求覆盖文档旧进度，将双因子认证、音乐、可用性监测、文件柜、通知、系统设置的 PRD 前后端能力一次性补齐，并同步更新文档状态。

修改范围：

- Uptime：新增 JSON Query 与 Push adapters；新增 Push token 字段、公开 `/api/uptime/push/:token`、公开状态页、badge；前端新增 JSON Query / Push 类型配置；heartbeat 写入维护标记。
- Music：新增 `MusicCatalogService`，将通用目录调用和歌单详情补 tracks 逻辑从 router 抽出；修复 `music-api` 被动态路由先鉴权挂载的问题，确保公开音乐代理按设计生效。
- Notification：新增纯 `RuleEngine`，正式发送和 dry-run 都执行 conditions；修复全局限流返回 false 后仍继续发送的问题；新增规则引擎单测。
- Filebox：新增持久化 `filebox_settings`，后端强制最大文件大小和 MIME 策略；前端新增策略设置 tab；移除 `/settings` 内存 stub。
- System self-check：补齐 `filebox_settings`、`uptime push` 字段检查。
- 文档：更新 `refactor-progress.md` 与 `refactor-next.md`，旧“PRD 尚未收口”状态已被本轮完成状态覆盖。

运行命令：

```bash
npm run lint
npm run test
npm run build
rg --no-config -n '<button\b|<input\b|<select\b|<textarea\b|<table\b|<dialog\b' src/js -g '*.jsx' -g '*.js'
rg --no-config -n 'DialogContent|TabsList|TabsTrigger|@cloudflare/kumo/components/tabs' src/js -S
rg --no-config -n 'vue|pinia|chart\.js|createApp\(|new Vue|from .vue.|from .pinia.|Chart\.' src package.json package-lock.json -S
rg --no-config -n 'execCommand' src/js/pages src/js/components -S
git diff --check
```

结果：

- `npm run lint` 通过。
- `npm run test` 通过：15 个测试文件、190 个测试。
- `npm run build` 通过，仅保留 Vite chunk size warning。
- Kumo-only 原生控件扫描为 0。
- 旧 Tabs/Dialog API 扫描为 0。
- Vue/Pinia/Chart.js 旧依赖扫描为 0。
- `execCommand` 扫描为 0。
- `git diff --check` 通过；仅提示 Windows 换行转换 warning。

浏览器验证路由：

- 本轮未代填管理员密码进入应用内部；浏览器深度 smoke 仍需在用户授权管理员登录后执行。

Kumo-only 例外：

- 无新增前端 UI 例外。

后续风险：

- 仍需真实外部环境验证：NCM 登录/解锁/播放、Email/Telegram 投递、Uptime 真实目标与 Push、Filebox 大文件与 MIME 策略、数据库导入恢复。

## 2026-06-08 工具箱 PRD 底座与关键后端能力落地

任务：按现有 PRD 自主规划并推进第一批可落地实现，优先完成共享底座、安全/持久化迁移和 Uptime 后端核心能力。

修改范围：

- 扩展 `src/utils/secure-storage.js` 为 `SecureSecretStore` 兼容层，补齐 JSON 加密/解密和 TOTP/通知包装器。
- 新增 `AuditService`、`ToolboxEventBus`、`JobScheduler`、`SettingsRegistry`，并补齐 `settings_registry`、`toolbox_jobs` 核心表。
- TOTP：`totp_accounts.secret` 保持字段兼容但改为透明加密存储；新增 reveal 审计、事件发布、导入预览和 otplib 临时配置隔离。
- Filebox：新增 `filebox_entries` / `filebox_access_logs`，从 `metadata.json` 兼容迁移到 SQLite；新增访问日志、最大下载次数、阅后即焚下载后处理和 `DATA_DIR` 对齐。
- Uptime：新增纯状态机、probe registry、HTTP/Keyword/TCP/Ping fallback/DNS 检查；状态迁移写入 SQLite；新增 summary、clone、test、check-now、batch、状态页、公开状态页 API 和维护窗口 API；前端工作台补入状态页/维护窗口 tabs、Kumo Table 列表、默认状态页创建和快速维护窗口入口。
- Notification：订阅 `ToolboxEventBus`，可从统一事件入口触发已有规则引擎。
- Music：收紧音频代理域名 allowlist，避免 `includes()` 误放行伪造域名。
- System：新增数据库 migration self-check，并在设置注册表登记 system/totp 设置域。
- Logger：批量 flush timer 使用 `unref()`，避免一次性 node 脚本 require logger 后无法退出。
- 新增 Uptime 状态机/probe 单测，并扩展 secure-storage / response-builder 单测。

运行命令：

```bash
npm run test
npm run lint
npm run build
rg -n --pcre2 '<(?-i:button|select|input|textarea)\b' src/js/pages src/js/components -S
rg -n 'DialogContent|TabsList|TabsTrigger|@cloudflare/kumo/components/tabs' src -S
rg -n 'vue|pinia|chart\.js|createApp\(|new Vue|from .vue.|from .pinia.|Chart\.' src package.json package-lock.json -S
git diff --check
```

额外临时数据库烟测：

```bash
node -e "const db=require('./src/db/database'); db.initialize(); /* TOTP/Filebox/Uptime smoke */ db.close();"
node -e "const db=require('./src/db/database'); db.initialize(); /* Uptime status page/maintenance smoke */ db.close();"
```

结果：

- `npm run test` 通过：14 个测试文件、185 个测试。
- `npm run lint` 通过。
- `npm run build` 通过，仅保留 Vite chunk size warning。
- Kumo-only 原生控件扫描为 0。
- 旧 Tabs/Dialog API 扫描为 0。
- Vue/Pinia/Chart.js 旧依赖扫描为 0。
- `git diff --check` 通过，仅提示 Windows 换行转换 warning。
- 临时数据库烟测通过，覆盖 TOTP 加密读写、Filebox SQLite 文本分享、Uptime monitor/status page/maintenance。

浏览器验证路由：

- `http://127.0.0.1:5173/uptime` 可加载到安全登录页，页面标题为 `API Monitor`，浏览器控制台 error 为 0。
- 后登录 Uptime 状态页/维护窗口 tabs 未输入管理员密码做交互验证，当前以构建、单测和静态扫描覆盖。

Kumo-only 例外：

- 无新增前端 UI 例外。

后续风险：

- 本轮完成 PRD Phase 0/Phase 1 的关键底座和部分 Uptime 后端能力，不等同于六个工具箱模块的全部产品级 UI 完成。
- 仍需真实账号/外部环境验证：音乐播放源与解锁、通知实际投递、公开状态页路由体验、Docker/SSH/Agent/Cloudflare/PaaS 等。
- Uptime 状态页和维护窗口已具备轻量前端工作台入口；后续仍可继续补充高级编辑器、绑定监控数展示和公开状态页视觉定制。

## 2026-06-08 工具箱模块 PRD 与交接更新

任务：为双因子认证、音乐、可用性监测、文件柜、通知、系统重构补齐可交接的 PRD 和下一步执行入口。

修改范围：

- 新增 `docs/toolbox-modules-refactor-prd.md`，覆盖六个工具箱模块的前后端重构范围、架构、API、数据模型、迁移计划和验收标准。
- 新增 `docs/uptime-kuma-aligned-prd.md`，单独描述可用性监测对标 Uptime Kuma 的功能、状态机、通知联动、状态页和后端模型。
- 更新 `docs/refactor-next.md`，将工具箱模块整体重构列为最高优先级。
- 更新 `docs/refactor-progress.md`，标注工具箱模块当前仍是待产品级重构状态。

运行命令：

```bash
Select-String -Path .\docs\toolbox-modules-refactor-prd.md -Pattern '^#|^##|^###' -Encoding utf8
Select-String -Path .\docs\uptime-kuma-aligned-prd.md -Pattern '^#|^##|^###' -Encoding utf8
git status --short
```

结果：

- 两份 PRD 均存在，并包含可供下一轮 agent 直接执行的阶段计划与验收标准。
- 本轮为文档和交接更新，未修改运行代码。

后续风险：

- 未运行 `npm run lint` / `npm run build`，因为本轮没有代码改动。
- PRD 中的功能改造尚未实施，尤其是 TOTP secret 加密、文件柜 SQLite 迁移、通知事件总线、音乐服务拆分和 Uptime 状态机。

## 2026-06-08 前端样式收尾审查

任务：审查 `src/js/pages` 与 `src/js/components` 中剩余旧版前端样式，并按 Kumo-only 规范修复。

修改范围：

- `GlobalDialogHost.jsx` 接入 Kumo `DeleteResource`，并保留 `dialog.confirm(...)` 删除类文案的兼容迁移路径。
- `dialog.js` 新增 `dialog.deleteResource(options)`。
- 清理页面级 `shadow-*` / `hover:shadow` / `transition-shadow`。
- 移除旧 `quick-fade-in`、`motion-pop-in`、`app-collapse-panel` 自绘转场，折叠交互回到 Kumo `Collapsible`。
- 清理 Kumo `Button`、`Input`、`Select`、`Textarea` 上覆盖官方外观的 `bg-*`、`border-*`、`rounded*`、`focus:*` 等旧 class。
- 将 TOTP 分组列表迁移为 Kumo `Table`，并把少量硬编码 `bg-white` / `text-danger` 改为 Kumo token。

运行命令：

```bash
git diff --check
npm run lint
npm run build
rg --no-config -n '<button\\b|<input\\b|<select\\b|<textarea\\b|<table\\b|<dialog\\b' src/js -g '*.jsx' -g '*.js'
rg --no-config -n 'DialogContent|TabsList|TabsTrigger|@cloudflare/kumo/components/tabs' src/js -S
rg --no-config -n 'shadow-(xs|sm|md|lg|xl|2xl)|hover:shadow|transition-shadow|border-black/|bg-white|text-gray-|bg-gray-|quick-fade-in|motion-pop-in|app-collapse-panel|text-danger|bg-danger|border-danger' src/js src/css -g '*.jsx' -g '*.js' -g '*.css'
```

结果：

- `git diff --check` 通过。
- `npm run lint` 通过。
- `npm run build` 通过，仅保留 Vite chunk size warning。
- 原生 `<button>/<input>/<select>/<textarea>/<table>/<dialog>` 扫描为 0。
- 旧 Tabs/Dialog API 扫描为 0。
- 旧阴影、旧自绘转场、硬编码白底和旧危险色类扫描为 0。

后续风险：

- 未启动本地服务，也未做浏览器 smoke；本轮只做静态审查和 lint。
- 外部账号、SSH、Docker、Agent、Cloudflare、PaaS 等真实操作仍需真实环境验证。

## 2026-06-07 文档目录整理

任务：清理 `docs/`，把过时 Vue 文档更新为当前 React + Kumo 事实，并把 Kumo 上游参考快照移动到 `docs/reference/`。

修改范围：

- 新增 `docs/README.md`。
- 重写 `docs/CONTRIBUTING.md`。
- 重写 `docs/DESIGN.md`。
- 重写 `docs/FRONTEND_BEST_PRACTICES.md`。
- 重写 `docs/NEW_MODULE_GUIDE.md`。
- 重写 `docs/KUMO_MIGRATION_RULES.md`。
- 重写 `docs/refactor-progress.md`。
- 重写 `docs/refactor-next.md`。
- 重写 `docs/refactor-verification.md`。
- 移动 Kumo 参考快照到 `docs/reference/`。

已确认的当前源码事实：

- `npm run lint` 通过。
- `src/js/main.jsx` 是 React 入口。
- `src/js/pages/` 和 `src/js/components/` 中原生 `<button>/<select>/<input>/<textarea>` 静态扫描为 0。
- `src` 中 `DialogContent`、`TabsList`、`TabsTrigger`、`@cloudflare/kumo/components/tabs` 静态扫描为 0。
- `src`、`package.json`、`package-lock.json` 中 Vue、Pinia、Chart.js 依赖扫描为 0。
- `@cloudflare/kumo` 当前版本为 2.5.0。
- `DeleteResource` 当前从 `@cloudflare/kumo` 导出。
- `PageHeader` / `ResourceListPage` 当前为 block source，不是 barrel 运行时导出。

Kumo-only 例外：

- `AppPageHeader.jsx` 是顶栏紧凑过渡封装，组合 Kumo `Breadcrumbs` 与 `Tabs`；替换官方 block 前需适配。

后续风险：

- 本次仅整理文档，未修改运行代码。
- 删除确认已在 2026-06-08 通过全局弹窗宿主接入 `DeleteResource`。
- 仍需全路由 browser smoke。

## 2026-06-06 Kumo-only 收口记录

任务：完成剩余 Kumo-only 控件迁移，迁移 Uptime 图表，清理旧 Vue/原生前端，仅保留 React + Kumo 入口。

结果：

- `npm run build` 通过，仅保留 Vite chunk size warning。
- `src/js/pages` 与 `src/js/components` 中原生 `<button>/<select>/<input>/<textarea>` 静态扫描为 0。
- Chart.js / Vue / Pinia 扫描为 0。
- 旧 `TabsList` / `TabsTrigger` / `DialogContent` 扫描为 0。
- Kumo-only 例外：无新增例外。

浏览器验证记录：

- `/dns` 可渲染 Cloudflare 管理页。
- `/music` 不白屏。
- `/settings` 顶部/底部间距正常。
- `/server` 可渲染主机实例列表、展开区、Kumo charts 和相关 Dialog。

后续风险：

- 外部账号、SSH、Docker、Agent、Cloudflare、PaaS 等真实操作仍需真实环境验证。

## 复用命令

```bash
npm run lint
npm run build
rg -n --pcre2 '<(?-i:button|select|input|textarea)\b' src/js/pages src/js/components -S
rg -n 'DialogContent|TabsList|TabsTrigger|@cloudflare/kumo/components/tabs' src -S
rg -n 'vue|pinia|chart\.js|createApp\(|new Vue|from .vue.|from .pinia.|Chart\.' src package.json package-lock.json -S
```
