# 参考快照

最后更新：2026-09-08

本目录存放上游或生成型资料，不代表项目当前状态本身。项目状态请优先阅读 `docs/README.md` 和 `docs/standards/Kumo UI 规则.md`。

## 文件

- `kumo-component-registry.md`：从 `node_modules/@cloudflare/kumo/ai/component-registry.json` 整理出的 Markdown 快照。
- `kumo-component-registry.json`：Kumo 组件注册表原始 JSON 快照。
- `kumo-dialog.md`：Kumo Dialog 文档快照。

## 当前重要结论

- 当前 Kumo 包版本：`@cloudflare/kumo` 2.13.2。
- `DeleteResource` 可从 `@cloudflare/kumo` 导出。
- `PageHeader` 和 `ResourceListPage` 当前是 block source；使用前需要通过 Kumo CLI 安装或复制 block source，不要直接从 barrel 导入。
- Chart 相关能力包括 `TimeseriesChart`、`Meter`、`ChartPalette`，图表应使用 Kumo palette、`loading` 和必要的 `tooltipBoundary`。

## 快照与当前版本的已知差异

`kumo-component-registry` 快照生成于 2.10.0（8/12），当前实际 2.13.2。组件清单仍 100% 对齐（48/48），但有以下 props 漂移，升级刷新时应补齐：

- `Badge.icon`（2.11 新增）
- `Pagination.hasNextPage`
- `Select` 的 12 个 positioner 锚定 props（2.13 新增）

## 刷新时机

升级 `@cloudflare/kumo` 后刷新本目录，并同步更新：

- `docs/standards/Kumo UI 规则.md`
- `docs/standards/前端布局约定.md`
