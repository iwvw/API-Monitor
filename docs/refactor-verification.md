# 验收记录

最后更新：2026-06-07

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
- 删除确认迁移到 `DeleteResource` 尚未执行。
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
