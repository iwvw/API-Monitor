# 参考快照

最后更新：2026-10-23

本目录存放上游或生成型资料，不代表项目当前状态本身。项目状态请优先阅读 `docs/README.md` 和 `docs/standards/Kumo UI 规则.md`。

## 文件

- `kumo-component-registry.md`：从 `node_modules/@cloudflare/kumo/ai/component-registry.json` 整理出的 Markdown 快照。
- `kumo-component-registry.json`：Kumo 组件注册表原始 JSON 快照。
- `kumo-dialog.md`：Kumo Dialog 文档快照。

## 当前重要结论

- 当前 Kumo 包版本：`@cloudflare/kumo` 2.15.0。
- 组件注册表已与 2.15.0 对齐：54 个组件 + 3 个 block（`DeleteResource`、`PageHeader`、`ResourceListPage`），共 9 个分类。
- `DeleteResource` 可从 `@cloudflare/kumo` 导出。
- `PageHeader` 和 `ResourceListPage` 当前是 block source；使用前需要通过 Kumo CLI 安装或复制 block source，不要直接从 barrel 导入。
- Chart 相关能力包括 `TimeseriesChart`、`Meter`、`ChartPalette`，图表应使用 Kumo palette、`loading` 和必要的 `tooltipBoundary`。
- `LayerDialog` 的主操作按钮是 `LayerDialog.Action`；`LayerDialog.Actions.Primary` 自 2.15.0 起只是弃用别名。

## 快照与当前版本的已知差异

无。快照已由 2.15.0 的 `ai/component-registry.json` 重新生成，`kumo-component-registry.md` 与官方
`scripts/component-registry/markdown-generator.ts` 的输出保持一致。

2.11–2.15 期间补齐的能力（此前文档里记为「升级刷新时应补齐」的漂移项已随本次刷新覆盖）：

- `Badge.icon`（2.11 新增，filled 变体支持图标）
- `Pagination.hasNextPage`（2.13 新增，未知总数分页）
- `Select` 的 12 个 positioner 锚定 props（`side`/`sideOffset`/`align`/`alignOffset`/`alignItemWithTrigger` 等，2.13 新增）
- `ButtonGroup`、`InlineCopyText`、`TagInput`（2.14 新增）
- `Slider`、`GlobeMap`（2.15 新增）
- `LayerDialog` 进阶用法：`LayerDialog.Action` 的 `menu`/`menuLabel`（拆分按钮）、`Radio.Group` 的 `segmented` 外观、`Checkbox.Group` 的 `card` 外观

## 刷新时机

升级 `@cloudflare/kumo` 后刷新本目录，并同步更新：

- `docs/standards/Kumo UI 规则.md`
- `docs/standards/前端布局约定.md`

刷新方式：`npm run kumo:registry`（= `node tools/kumo-registry-snapshot.mjs`）。脚本会：

1. 把 `node_modules/@cloudflare/kumo/ai/component-registry.json` 原样复制为
   `docs/reference/kumo-component-registry.json`（逐字节一致，方便直接 diff 上游产物）。
2. 按上游 `scripts/component-registry/markdown-generator.ts` 的 `generateAIContext()` 逻辑重新生成
   `kumo-component-registry.md`。上游是 TS，仓库没有 TS 运行时，因此 `tools/kumo-registry-snapshot.mjs`
   复刻了同一套规则；升级 kumo 后先跑一次，若 `git diff` 出现预期之外的格式变化，说明上游生成规则变了，
   需要同步这个脚本。

`npm run kumo:registry:check` 只比对不写盘，已接入 `npm run audit:fast`，快照过期会直接失败。
