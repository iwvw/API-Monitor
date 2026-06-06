# Kumo-only React 重构后续执行计划

本文档是后续 agent 执行 UI 重构的工作入口。目标是把 API-Monitor 从旧 Vue 3 + FontAwesome + 原生 CSS 彻底收口到 React 19 + `E:\Code\kumo` / `@cloudflare/kumo` + Tailwind CSS v4。

## 硬性原则

1. 不新增自写 UI 组件。页面组件、业务容器、hook、数据适配函数可以保留，但 Button、Input、Select、Tabs、Table、Dialog、Toast、Checkbox、Switch、Sidebar、Loader 等 UI 组件必须优先直接使用 Kumo。
2. 不再新增本项目自定义组件库。已有 `ModuleTabs` 这类 UI 包装组件要逐步替换为直接使用 Kumo 组件。
3. 不硬编码视觉颜色。除第三方品牌标识、图表语义色等明确例外外，颜色必须使用 Kumo token。
4. 不复制旧 Vue/原生 CSS 的样式方案。旧实现只作为业务逻辑和 API 行为参考。
5. 每个 agent 一次只处理一个明确任务，完成后构建、浏览器验证、更新文档，再提交。

## 当前判断

按页面数量，迁移已经超过一半；按可交付质量和功能对齐程度，整体仍约在 50%-60%。主要缺口是：

- Cloudflare DNS 仍是半完成，Records 等内部细节没有完全对齐旧系统。
- 表格列宽拖动是部分完成，不是全站完成。
- Kumo 组件使用不够一致，仍有原生按钮、select、自绘样式和包装组件。
- `ai-chat` 删除状态需要核准，当前 Git 索引和工作区存在不一致。
- 文档里有 `[HEAD]` 伪记录，不能当作真实提交历史。
- 旧 Vue 文件、临时脚本、diff 文件需要决定删除或归档。

## 阶段 0：冻结与盘点

目标：建立可信基线，避免后续 agent 在混乱 Git 状态上继续修改。

执行项：

1. 读取 `docs/refactor-progress.md`、本文档、`package.json`、`src/js/components/MainLayout.jsx`。
2. 记录 `git status --short`，区分 staged、unstaged、deleted、added、untracked。
3. 生成真实状态表：
   - 已迁移页面
   - 半完成页面
   - React 已移除但旧 Vue 仍残留的模块
   - 文档说完成但代码未完全完成的项目
   - 代码完成但文档未更新的项目
4. 处理或明确记录这些高风险状态：
   - `src/js/pages/AiChatPage.jsx` 的 `AD` 状态
   - `ai-chat` 前后端残留
   - `tmp_*`、`old_diff*`、`fix-switches*.js`
   - `docs/refactor-progress.md` 的 `[HEAD]` 伪记录

验收标准：

- 每个 Git 变更都有归属说明。
- 文档只记录事实，不把未提交工作写成已完成历史。
- `npm run build` 可通过。

建议提交：

```bash
docs(refactor): reconcile migration status and next work plan
```

## 阶段 1：Kumo 基线对齐

目标：确认 `E:\Code\kumo` / `@cloudflare/kumo` 是唯一 UI 基准。

执行项：

1. 审查 `E:\Code\kumo` 的 Button、Input、Select、Tabs、Table、Dialog、Toast、Checkbox、Switch、Sidebar、Loader、Autocomplete 用法。
2. 更新 `docs/KUMO_MIGRATION_RULES.md`。
3. 扫描本项目中的自绘 UI：
   - 原生 `<button>` 用作 tabs/actions
   - 原生 `<select>`
   - 自绘 modal
   - 自绘 switch/checkbox
   - 本地 UI 包装组件
   - 硬编码颜色
4. 建立例外清单。例外只能是 Kumo 缺失且业务必须保留的能力，并且必须写入文档。

验收标准：

- 后续 agent 有明确 Kumo-only 规则。
- 不新增自写 UI 组件。
- 已有自写 UI 包装被列入替换计划。

建议提交：

```bash
docs(ui): define Kumo-only migration rules
```

## 阶段 2：路由与壳层收口

目标：保证 React 应用骨架稳定。

执行项：

1. 审查 `src/js/components/MainLayout.jsx`。
2. 确认每个 React 页面都有 import、sidebar entry、route key、URL path mapping、render case。
3. 移除已废弃模块入口，特别是独立 `ai-chat`。
4. 逐个验证直接访问：
   - `/dashboard`
   - `/server`
   - `/totp`
   - `/filebox`
   - `/uptime`
   - `/notification`
   - `/openai`
   - `/gemini-cli`
   - `/qwen`
   - `/antigravity`
   - `/paas`
   - `/dns`
   - `/aliyun`
   - `/tencent`
   - `/self-h`
   - `/music`
   - `/settings`
5. 验证 browser history、back、forward、sidebar active 状态。

验收标准：

- `npm run build` 通过。
- 全部路由无白屏。
- 控制台无 React runtime error。
- Sidebar 高亮和 URL 同步正确。

建议提交：

```bash
fix(ui): stabilize React shell routing and navigation
```

## 阶段 3：旧系统功能差异审计

目标：逐模块对比旧 Vue 实现，避免迁移丢功能。

每个模块记录：

| 模块 | 旧入口 | React 页面 | API 是否一致 | 缺失功能 | 优先级 |
|------|--------|------------|--------------|----------|--------|

重点模块：

- Server：主机列表、后台管理、Agent quick install、Docker、SSH/SFTP、历史趋势。
- Cloudflare DNS：Accounts、Zones、DNS Records、Workers、Pages、R2、Tunnels。
- OpenAI：endpoint、model、chat、persona、history、Autocomplete。
- Gemini/Qwen/Antigravity：accounts、model matrix、logs、quota、toggle 状态。
- Filebox/Music/TOTP/Uptime/Notification：CRUD、搜索、筛选、loading、error、empty state。

验收标准：

- 每个模块缺失项明确。
- 不再用粗粒度“完成/半完成”代替功能核查。

建议提交：

```bash
docs(refactor): audit Vue to React feature parity
```

## 阶段 4：补完半完成模块

优先级 1：`DnsPage.jsx`。

执行项：

1. 补 DNS Records 详情视图。
2. 支持 zone 选择后查看 records。
3. 支持新增、编辑、删除 DNS record。
4. 迁移旧 Quick Switch 逻辑。
5. Workers、Pages、R2、Tunnels 对齐旧系统已有能力。
6. Accounts 管理使用 Kumo Dialog/Form。
7. 所有数据密集型表格使用 Kumo Table，并支持 Kumo ResizeHandle。
8. Loading 使用 Kumo Loader/Skeleton。
9. Empty/error/refresh 状态统一。

验收标准：

- Cloudflare DNS 不再标记“半完成”。
- 旧系统核心 Cloudflare 功能不丢。
- `/dns` 浏览器验证通过。

建议提交：

```bash
feat(ui): complete Cloudflare DNS React migration
```

优先级 2：表格拖动全站补齐。

当前已有拖动柄的页面：

- OpenAI
- Gemini CLI
- Qwen
- Antigravity
- Server
- Aliyun
- Tencent
- SelfH

仍需复核或补齐：

- DnsPage
- MusicPage
- PaasPage
- TotpPage

验收标准：

- 所有数据密集型表格支持列宽拖动。
- 简单短表格若不支持拖动，必须在文档中记录理由。

建议提交：

```bash
feat(ui): add resizable columns to remaining dense tables
```

## 阶段 5：全站 Kumo 组件收敛

目标：消除多个 agent 分头修改造成的 UI 漂移。

执行项：

1. 替换原生 tab/action button 为 Kumo Button 或 Kumo Tabs。
2. 替换原生 select 为 Kumo Select。
3. 替换自绘 modal 为 Kumo Dialog。
4. 替换自绘 checkbox/switch 为 Kumo Checkbox/Switch。
5. 替换自写 UI 包装组件为直接使用 Kumo。
6. 统一 Button、Table、Dialog、Toast、Tabs、Sidebar、Loader 密度。
7. 清理硬编码颜色。

验收标准：

- 不新增自写 UI 组件。
- `ModuleTabs` 等包装组件被删除或只剩非 UI 业务逻辑。
- 自绘 switch/checkbox/modal/tab 不再存在。
- 页面密度、间距、表格样式一致。

建议提交：

```bash
refactor(ui): normalize pages against Kumo components
```

## 阶段 6：删除旧 Vue 残留

目标：React 迁移完成后，旧 UI 不再参与系统。

执行项：

1. 确认是否彻底废弃旧 Vue 入口。
2. 删除或归档：
   - `src/js/main.js`
   - `src/templates/*.html`
   - 旧 `src/css/*.css`
   - 旧 `src/js/modules/*.js`
   - `src/js/template-loader.js`
   - 旧 store
3. 清理独立 `ai-chat`：
   - frontend route
   - backend modules
   - db schema
   - imports
   - sidebar metadata
   - CSS
   - templates
4. 清理临时文件：
   - `tmp_index.html`
   - `tmp_openai_fix.js`
   - `old_diff.txt`
   - `old_diff_utf8.txt`
   - `fix-switches*.js`

验收标准：

- 独立 `ai-chat` 模块无残留。
- 旧 Vue 入口不再被生产构建引用。
- `npm run build` 通过。

建议提交：

```bash
refactor(ui): remove legacy Vue frontend remnants
```

## 阶段 7：自动化验证

目标：让 agent 自己完成验收。

执行项：

1. 建立浏览器 smoke 验证脚本。
2. 自动访问全部主路由。
3. 检查：
   - 无白屏
   - 无 console error
   - 主内容存在
   - sidebar active 正确
   - 关键按钮可点击
4. 数据依赖页面必须在无账号、API 失败、空数据时有清晰状态。
5. 结果写入 `docs/refactor-verification.md`。

验收标准：

- 一条命令完成前端验收。
- 每次提交前必须跑 build 和 route smoke。

建议提交：

```bash
test/ui): add route smoke verification for React migration
```

## 阶段 8：最终交付

目标：系统进入可维护状态。

执行项：

1. 更新 `README.md` 运行方式。
2. 更新 `docs/NEW_MODULE_GUIDE.md`，新模块默认 React + Kumo。
3. 更新 `docs/FRONTEND_BEST_PRACTICES.md`。
4. 删除或归档过期迁移文档。
5. 最终全量验收：
   - build
   - route smoke
   - browser console
   - desktop/mobile
   - dark/light/auto theme
   - login/logout
   - core API workflows

最终验收标准：

- 所有主模块不再依赖旧 Vue UI。
- 全部主路由可访问。
- 无运行时白屏。
- 无明显样式漂移。
- 文档、代码、Git 历史一致。

建议提交：

```bash
chore(refactor): finalize Kumo React migration
```

## Agent 执行规则

每个 agent 必须按以下流程执行：

1. 先读 `docs/refactor-progress.md`、本文档、`docs/KUMO_MIGRATION_RULES.md`、当前目标页面、旧 Vue 对应模块。
2. 一次只处理一个明确任务。
3. 修改前记录缺口。
4. 修改后运行 `npm run build`。
5. 对相关路由做浏览器验证。
6. 更新文档。
7. 一个 commit 对应一个任务。
8. 不留下临时脚本、tmp 文件、staged/unstaged 混合状态、文档已完成但代码未完成的记录。

## 最新收口备注（2026-06-06）

- Cloudflare React 页面已完成主要后端接口覆盖和 Kumo `Table` 验证，下一步优先补自动化 smoke，而不是继续手工点检同一页面。
- Dialog 迁移重点已完成：React 页面旧 `asChild` 写法已清零，Cloudflare 关闭后空节点问题已修。
- 页面底部间距策略已调整：由 `MainLayout` 统一控制，普通页面不要再补 `pb-20`；只有固定底部播放器等真实遮挡场景才保留额外避让。
- 全站中文化已做第一轮可见文本收敛，后续应通过浏览器文本扫描逐路由补漏；旧 Vue 文件若确认废弃，应在阶段 6 统一删除或归档。
