# 贡献指南

感谢你参与 API Monitor。当前项目已经收口到 React + Kumo 前端和 Express + SQLite 后端，贡献时请优先遵守现有模块边界与 Kumo-only UI 规则。

## 环境要求

- Node.js 20 LTS 或更高版本。
- npm 9 或更高版本。
- Rust 1.75 或更高版本，仅在维护 `agent-rust/` 时需要。
- SQLite 使用内置 `better-sqlite3`，本地开发不需要额外数据库服务。

## 本地开发

```bash
git clone https://github.com/iwvw/api-monitor.git
cd api-monitor
npm install
npm run dev
```

开发模式会同时启动后端 Express 和前端 Vite。生产模式可使用：

```bash
npm run build
npm start
```

## 常用命令

```bash
npm run lint
npm run lint:fix
npm run build
npm run test
npm run format:check
```

前端样式或交互改动至少运行 `npm run lint` 与 `npm run build`。涉及路由、弹窗、表格、图表、导航、移动端布局时，还应做浏览器验证并更新 [refactor-verification.md](./refactor-verification.md)。

## 代码规范

- 前端基础 UI 必须优先使用 `@cloudflare/kumo`。
- 不新增自绘 Button、Input、Select、Tabs、Table、Dialog、Toast、Checkbox、Switch、Sidebar、Loader 等 UI 组件。
- 不硬编码主题色，使用 `bg-kumo-*`、`text-kumo-*`、`border-kumo-*`、`ring-kumo-*` 等 token。
- 按钮默认使用 `size="sm"`；与 Input、Select、Tabs 同排时保持高度一致。
- 删除确认优先迁移到 Kumo `DeleteResource`。
- 表格内容默认不换行，数据密集表格优先支持 `Table.ResizeHandle`。
- 图表使用 Kumo Chart 系列，颜色使用 `ChartPalette`，加载期使用 `loading` 骨架。

详细 UI 约束见 [KUMO_MIGRATION_RULES.md](./KUMO_MIGRATION_RULES.md)。

## 项目结构

```text
api-monitor/
├── server.js              # Express 入口
├── src/
│   ├── js/                # React 前端
│   │   ├── components/    # 壳层与少量业务辅助组件
│   │   ├── pages/         # 主页面
│   │   ├── modules/       # 前端运行时 helper
│   │   └── store.js       # Zustand 全局状态
│   ├── css/app.css        # Tailwind/Kumo 样式入口
│   ├── db/                # SQLite 初始化与模型
│   ├── routes/            # 核心 API 路由
│   ├── services/          # 核心服务
│   └── utils/             # 工具函数
├── modules/               # 可插拔业务模块
├── agent-rust/            # Agent 源码
├── docs/                  # 项目文档
└── test/                  # 测试
```

## 提交规范

使用 Conventional Commits：

```text
feat(server): add host metric endpoint
fix(ui): align compact toolbar controls
docs(kumo): update component registry notes
```

常用类型：

- `feat`：新功能。
- `fix`：修复问题。
- `docs`：文档更新。
- `refactor`：重构。
- `perf`：性能优化。
- `test`：测试。
- `chore`：工具、依赖或构建变更。

## PR 检查清单

- [ ] 已运行必要的 lint/build/test。
- [ ] UI 改动符合 Kumo-only 规则。
- [ ] 相关文档已更新。
- [ ] 未回退或覆盖他人的未提交改动。
- [ ] 没有留下临时脚本、调试输出或无关文件。
