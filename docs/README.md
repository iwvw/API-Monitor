# API Monitor 文档索引

最后更新：2026-06-09

本文档目录记录当前项目事实。API Monitor 现在以 **Express + React 19 + Zustand + @cloudflare/kumo 2.5.0 + Tailwind CSS v4 + SQLite** 为主线；旧 Vue 前端、旧模板加载器、Pinia 和 Chart.js 已不再作为当前前端方案。

## 建议阅读顺序

1. [DESIGN.md](./DESIGN.md)：当前架构、模块边界、数据流和前端壳层。
2. [KUMO_MIGRATION_RULES.md](./KUMO_MIGRATION_RULES.md)：Kumo-only UI 硬约束。
3. [FRONTEND_BEST_PRACTICES.md](./FRONTEND_BEST_PRACTICES.md)：React + Kumo 页面实现细节。
4. [NEW_MODULE_GUIDE.md](./NEW_MODULE_GUIDE.md)：新增模块接入流程。
5. [CONTRIBUTING.md](./CONTRIBUTING.md)：本地开发、验证和提交规范。
6. [refactor-progress.md](./refactor-progress.md)：当前重构状态。
7. [refactor-next.md](./refactor-next.md)：后续待办与执行规则。
8. [refactor-verification.md](./refactor-verification.md)：静态扫描、构建和浏览器验收记录。
9. [termix-reference-analysis.md](./termix-reference-analysis.md)：Termix 终端、SFTP、Docker、资源监控参考分析。

## 参考快照

`docs/reference/` 存放上游或生成型参考资料，不作为项目状态文档直接阅读：

- [reference/kumo-component-registry.md](./reference/kumo-component-registry.md)：`@cloudflare/kumo` 组件注册表 Markdown 快照。
- [reference/kumo-component-registry.json](./reference/kumo-component-registry.json)：同一注册表的 JSON 快照。
- [reference/kumo-dialog.md](./reference/kumo-dialog.md)：Kumo Dialog 文档快照。

## 当前源码基线

- React 入口：`src/js/main.jsx`。
- 应用壳层：`src/js/components/MainLayout.jsx`。
- 页面目录：`src/js/pages/`。
- 全局状态：`src/js/store.js`。
- 核心 API 路由：`src/routes/`。
- 业务模块：`modules/*-api/`。
- 样式入口：`src/css/app.css`。

当前主页面包括：Dashboard、Server、TOTP、Filebox、Uptime、Notification、OpenAI、Gemini CLI、Qwen、PaaS、Cloudflare、Aliyun、Tencent、Self-H、Music、Settings。

## 重要状态

- 独立 `ai-chat` 与独立 Antigravity 前端模块不再是当前主导航模块。
- GCLI 页面中出现的 `Google Antigravity` 文案来自共享 OAuth Client 凭证说明，不代表独立 Antigravity 模块恢复。
- Kumo `DeleteResource` 已由当前包导出，可用于删除确认迁移。
- Kumo `PageHeader` / `ResourceListPage` 在当前包中属于 block source，不应直接从 `@cloudflare/kumo` barrel 导入。
- `src/js/components/AppPageHeader.jsx` 是当前顶栏的紧凑临时封装；若替换为官方 PageHeader block，应先安装/复制 block 并适配顶栏高度、边框和 `size="sm"` 标签页要求。
- `src/js/components/AnimatedCollapse.jsx` 是当前全站展开/收起适配层，基于 Kumo `Collapsible` 和 Base UI 高度变量恢复过渡动画。
- 侧栏折叠偏好、页面宽度偏好、主题偏好等前端显示设置已接入后端用户设置，启动阶段通过 `src/index.html` 内联主题脚本避免暗色模式闪白。
- 插件 ZIP 下载不再依赖 PowerShell，后端使用 Node 侧压缩能力，适配 Linux 容器运行环境。

## 常用验证

```bash
npm run lint
npm run build
rg -n --pcre2 '<(?-i:button|select|input|textarea)\b' src/js/pages src/js/components -S
rg -n 'vue|pinia|chart\.js|createApp\(|new Vue|from .vue.|from .pinia.|Chart\.' src package.json package-lock.json -S
rg -n 'DialogContent|TabsList|TabsTrigger|@cloudflare/kumo/components/tabs' src -S
```
