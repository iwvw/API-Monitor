# Kumo-only UI 规则

最后更新：2026-09-05

本文档是 API Monitor 前端 UI 的硬约束。当前项目以 `@cloudflare/kumo` 2.13.1 为唯一设计系统基线。

## 总规则

1. 页面可以写业务布局、hook 和数据适配，但基础 UI 必须优先使用 Kumo。
2. 不新增自写 Button、Input、Select、Tabs、Table、Dialog、Toast、Checkbox、Switch、Sidebar、Loader、Badge、Tooltip、Popover、Dropdown 等组件。
3. 不新增通用 UI 包装组件；确需保留的本地组件必须是业务组合或过渡封装，并在待办中记录。
4. 不用 `<button>` 模拟 Tabs，不用 `<select>` 模拟 Select，不用 div + CSS 模拟 Dialog。
5. 不硬编码主题色，使用 Kumo token。
6. 旧 Vue/原生 CSS 只可作为业务逻辑参考，不作为样式来源。

## 组件优先级

| 需求 | 必须优先使用 |
|------|--------------|
| 操作按钮 | Kumo `Button` |
| 文本输入 | Kumo `Input` |
| 多行输入 | Kumo `Textarea` |
| 下拉选择 | Kumo `Select`，隐藏标签使用 `aria-label` |
| 标签切换 | Kumo `Tabs` |
| 表格 | Kumo `Table` |
| 表格拖动 | Kumo `Table.ResizeHandle` |
| 弹窗 | Kumo `Dialog` |
| 删除确认 | Kumo `DeleteResource` |
| 通知 | Kumo `Toasty` / `createKumoToastManager` |
| 开关 | Kumo `Switch` |
| 勾选 | Kumo `Checkbox` |
| 侧边栏 | Kumo `Sidebar` |
| 加载 | Kumo `SkeletonLine` / `Loader` / Chart `loading` |
| 搜索建议 | Kumo `Autocomplete` |
| 可复制文本 | Kumo `ClipboardText` |
| 百分比/用量 | Kumo `Meter` |
| 时间序列图 | Kumo `TimeseriesChart` + `ChartPalette` |
| 展开收起 | `AnimatedCollapse`，内部必须基于 Kumo `Collapsible` |

## 密度与尺寸

- 全站按钮默认 `size="sm"`。
- Toolbar、筛选行、内部层级 Tabs 使用 `size="sm"`。
- Button、Input、Select 同排时必须高度一致。
- 图标按钮使用 `shape="square"` 或 `shape="circle"`，并提供 `aria-label`。
- 卡片边框统一 `border border-kumo-line`，必要时用语义 token 强调。
- 表格内容默认一行显示，长文本用 truncation、tooltip、ClipboardText 或详情弹窗处理。

## Toolbar（已记录的刻意决策）

官方文档（kumo-ui.com/components/toolbar）已将 `Toolbar` 的 `size` prop 标记为废弃（2.10.0 起，
未来 major 版本移除）。**本项目刻意保留 `size="sm"`，不迁移**：

- 原因：`Toolbar.Button` / `Toolbar.Input` / `Toolbar.Link` / `Toolbar.InputGroup` 的尺寸由 Toolbar
  上下文强制注入（其 Props 已 Omit `size`，无法单独设置），官方在 2.13.1 没有任何「非废弃」的紧凑
  路径。项目 17 处导出/导入工具栏全部使用这些子部件，移除 `size="sm"` 会把行内控件从 sm 撑到 base，
  违反本页「Toolbar 使用 size=sm」的密度规则。
- 处置：维持 `size="sm"`；lint 若拦截该废弃项，走白名单放行并引用本条。
- 迁移预案（Kumo 移除 `size` 后）：将 `Toolbar.Button` 替换为 `Button size="sm"`、`Toolbar.Input`
  替换为 `Input size="sm"`，并手工补充 Kumo 的分段样式
  （`rounded-none border-0 bg-transparent shadow-none ring-0 focus-within:z-2 focus:z-2` +
  Toolbar 容器的 `[&>*:not(:first-child)]:border-l` 分段边线），或整行改为普通紧凑按钮组。
  迁移时逐处核对工具栏视觉密度。

## 表格布局

- 业务表格继续以 Kumo `Table` 为基础，并通过应用组合层声明语义列角色。
- 不使用纯百分比列宽。选择、开关、状态、时间、数值和操作列使用有边界的固定宽度；宽表应声明多个 `primary/content` 弹性列共同分配剩余空间，避免单列在宽屏下过度拉伸。
- 固定布局必须提供语义 `columns` 或有效 `colgroup`，不得依赖浏览器平均分列。
- 表体默认垂直居中；多行描述列可以显式顶部对齐。操作、选择、状态和控制列始终垂直居中。
- 数值右对齐，状态、控制和操作居中，主内容与时间左对齐。
- 三个文字操作必须同排时使用 `actions-xl`（280–400px）并禁止按钮组换行，避免操作列拉高整行。
- 移动端保留最小可读宽度，溢出限制在表格框架内部；只有存在详情或主列替代信息时才允许隐藏辅助列。
- 可拖动表格继续使用 Kumo `Table.ResizeHandle`，并遵守语义角色的最小和最大宽度。
- 完整设计与迁移计划见 [语义化表格布局与移动端适配 PRD](./prd/语义化表格布局与移动端适配.md)。

## DeleteResource

当前 `@cloudflare/kumo` 已导出 `DeleteResource`：

```jsx
import { DeleteResource } from '@cloudflare/kumo';

<DeleteResource
  open={open}
  onOpenChange={setOpen}
  resourceType="Zone"
  resourceName="example.com"
  onDelete={handleDelete}
  isDeleting={isDeleting}
  size="sm"
  errorMessage={errorMessage}
/>
```

资源删除、账号删除、对象删除、批量删除等破坏性删除确认应迁移到 `DeleteResource`。重启、刷新、清缓存、导入覆盖、重新部署等非删除动作可以继续使用普通 confirm。

## PageHeader

当前包里 `PageHeader` 和 `ResourceListPage` 是 block source，不是 barrel 运行时导出。不要直接：

```js
import { PageHeader } from '@cloudflare/kumo';
```

当前决定：

- 不直接从 Kumo barrel 替换本地顶栏封装。
- 保留 `src/js/components/AppPageHeader.jsx` 作为紧凑过渡组件。
- 若要使用官方 block，应通过 Kumo CLI 安装或复制 `blocks-source/page-header`，再适配本项目顶栏：避免双边框、保持 450px 断点、内部 Tabs 使用 `size="sm"`。

## Chart

- 使用 `TimeseriesChart`，不要回退到 Chart.js。
- 加载时传 `loading`，让 Kumo 显示骨架动画。
- 卡片或滚动容器内传 `tooltipBoundary`。
- 语义数据使用 `ChartPalette.semantic(...)`。
- 无语义分类数据使用 categorical palette。
- 小尺寸图表降低轴标签密度，避免 label 溢出。

## Collapsible

当前全站展开/收起统一走 `src/js/components/AnimatedCollapse.jsx`：

- 继续使用 Kumo `Collapsible.Root` / `Collapsible.Panel`。
- 高度动画使用 Base UI 暴露的 `--collapsible-panel-height`，并基于 `data-open`、`data-closed`、`data-starting-style`、`data-ending-style` 设置状态。
- 不新增旧式 `max-height` 魔法数动画，不恢复 `quick-fade-in`、`motion-pop-in`、`app-collapse-panel`。
- 含 chart 的展开内容应延迟渲染或使用 Kumo `loading`，减少展开卡顿。

## 当前静态基线

以下扫描在 2026-06-09 的工作区用于判断是否回潮：

```bash
npm run ui:governance
rg -n --pcre2 '<(?-i:button|select|input|textarea)\b' src/js/pages src/js/components -S
rg -n 'DialogContent|TabsList|TabsTrigger|@cloudflare/kumo/components/tabs' src -S
rg -n 'quick-fade-in|motion-pop-in|app-collapse-panel|transition-shadow|hover:shadow|shadow-(xs|sm|md|lg|xl|2xl)' src/js src/css -S
```

预期结果：0 命中。若出现命中，必须解释是合法例外还是需要迁移。

## 例外规则

如果 Kumo 缺少某项能力，必须：

1. 先确认当前 `@cloudflare/kumo` 包和注册表确实没有对应组件/API。
2. 在 [重构验证与例外清单](./重构验证与例外清单.md) 记录例外原因。
3. 使用最小语义实现，不做视觉组件封装。
4. 后续一旦 Kumo 提供能力，迁回 Kumo。
