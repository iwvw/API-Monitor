# 参考快照

最后更新：2026-11-05

本目录存放上游或生成型资料，不代表项目当前状态本身。项目状态请优先阅读 `docs/README.md` 和 `docs/standards/Kumo UI 规则.md`。

## 文件

- `kumo-component-registry.md`：从 `node_modules/@cloudflare/kumo/ai/component-registry.json` 整理出的 Markdown 快照。
- `kumo-component-registry.json`：Kumo 组件注册表原始 JSON 快照。
- `kumo-dialog.md`：Kumo Dialog 文档快照。

## 当前重要结论

- 当前 Kumo 包版本：`@cloudflare/kumo` 2.17.0。
- 组件注册表已与 2.17.0 对齐：54 个组件 + 3 个 block（`DeleteResource`、`PageHeader`、`ResourceListPage`），共 9 个分类。
- `DeleteResource` 可从 `@cloudflare/kumo` 导出。
- `PageHeader` 和 `ResourceListPage` 当前是 block source；使用前需要通过 Kumo CLI 安装或复制 block source，不要直接从 barrel 导入。
- Chart 相关能力包括 `TimeseriesChart`、`Meter`、`ChartPalette`，图表应使用 Kumo palette、`loading` 和必要的 `tooltipBoundary`。
- `LayerDialog` 的主操作按钮是 `LayerDialog.Action`；`LayerDialog.Actions.Primary` 自 2.15.0 起只是弃用别名。
- `LayerDialog.Separator`（2.17 新增）可在 `LayerDialog.Body` 内做整宽分节。

## 快照与当前版本的已知差异

无。快照已由 2.17.0 的 `ai/component-registry.json` 重新生成，`kumo-component-registry.md` 与官方
`scripts/component-registry/markdown-generator.ts` 的输出保持一致。

2.16 与 2.17 期间补齐的能力（本次刷新覆盖）：

- `ClipboardText.masked`（2.17 新增，默认隐藏敏感值，点按才显示）
- `LayerDialog.Separator`（2.17 新增，Body 内的整宽分隔线）
- `TimeseriesChart.xAxisHideLabelOverlap`（2.16 新增，默认 `true`）
- `DatePicker` 视觉重写（2.17）：日期格 32px、列宽 36px、箭头为无边框方形按钮、双月并排、
  `captionLayout="dropdown"` 的年月下拉也有正式样式；改用 Kumo token，Kumo 自带的 `.rdp-*`
  布局规则被删除（`kumo.css` 只剩月份切换动画关键帧），但 react-day-picker 仍会输出 `.rdp-*` 类名。
- 2.11–2.15 期间补齐的能力：`Badge.icon`、`Pagination.hasNextPage`、`Select` 的 12 个 positioner 锚定 props、
  `ButtonGroup`、`InlineCopyText`、`TagInput`、`Slider`、`GlobeMap`，以及 `LayerDialog.Action` 的
  `menu`/`menuLabel`（拆分按钮）、`Radio.Group` 的 `segmented`、`Checkbox.Group` 的 `card` 外观。

## 升级 2.17 时确认过的项目侧影响

- `src/css/app.css` 里针对 `.rdp-button_previous` / `.rdp-button_next` / `.rdp-caption_label` /
  `.rdp-dropdown` 的 30 行微调已随这次升级删除。原因是它按 2.16 的「1.625rem 箭头 + 2.5rem
  caption」硬对齐，而 2.17 已把箭头改成 32px 无边框方形按钮（`!important` 会把它改回旧尺寸），
  且 `.rdp-caption_label` / `.rdp-dropdown` 的隐藏规则也不再需要（下拉由 `components.Dropdown`
  换成 Kumo `Select`，不再渲染 DayPicker 原生下拉与回显标签）。
- `DatePicker` 在 2.17 仍把 `classNames` 合并到内部默认类之上，`components.Dropdown` /
  `DropdownNav` 覆盖继续生效（已在浏览器里实测：年月两个 Select 正常渲染、双月 range 布局正常）。
- `LayerDialog.Body` 顶部裁剪兜底规则依赖的 `base-ui-disable-scrollbar` 类名在 2.17 仍存在
  （现在是 Kumo 的 `no-scrollbar` 与 Base UI 的 `base-ui-disable-scrollbar` 同时挂在滚动视口上），
  `px-4.5 pt-0.5 pb-4.5` 内容层也仍是原样，因此该规则继续命中，无需改动。


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
