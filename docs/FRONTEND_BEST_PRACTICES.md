# 前端开发最佳实践

最后更新：2026-06-09

本文档只描述当前 React + Kumo 前端。旧 Vue、Teleport、模板加载器、`showToast` mixin 等做法已不再适用。

## 页面结构

- 页面组件放在 `src/js/pages/<ModuleName>Page.jsx`。
- 应用壳层由 `src/js/components/MainLayout.jsx` 负责，页面不要自行设置全局宽度。
- 页面根容器优先使用 `flex min-w-0 flex-col gap-4` 一类紧凑布局。
- 固定底部播放器等真实遮挡场景才额外留底部空间。

## Kumo 组件

- Button、Input、Select、Tabs、Table、Dialog、Toast、Checkbox、Switch、Sidebar、Loader、Tooltip、Popover、Dropdown 等基础 UI 必须使用 Kumo。
- 按钮默认 `size="sm"`。
- Button、Input、Select 同排时使用相同尺寸，避免高度错位。
- 内部层级标签栏、工具栏标签和筛选标签使用 `Tabs size="sm"`。
- 页面或模块主标签可使用默认高度，但仍使用 Kumo `Tabs`。
- 图标按钮使用 `shape="square"` 或 `shape="circle"`，并提供 `aria-label` 和必要的 `title`。

## 弹窗与删除确认

- 普通弹窗使用 Kumo `Dialog`。
- 全局 alert/confirm/prompt 通过 `src/js/modules/dialog.js` 和 `GlobalDialogHost.jsx`。
- 删除资源确认应迁移到 Kumo `DeleteResource`，并传入 `resourceType`、`resourceName`、`onDelete`、`isDeleting`、`errorMessage`。
- 非删除类确认，例如重启、清空缓存、导入覆盖、重新部署，可以继续使用普通 confirm。

## Toast

使用 `src/js/modules/toast.js`：

```js
import { toast } from '../modules/toast.js';

toast.success('操作成功');
toast.error('操作失败');
toast.warning('请检查配置');
toast.info('状态已刷新');
```

该 helper 已接入 Kumo `Toasty` 管理器。

## 表格

- 使用 Kumo `Table`。
- 数据密集表格默认不换行：`whitespace-nowrap`。
- 内容过长使用 `truncate`、`title`、`ClipboardText` 或详情弹窗，不让单元格撑爆布局。
- 需要列宽控制时使用 `Table.ResizeHandle` 和 `useTableResize.js`。
- 行级管理动作可以支持双击行任意位置进入管理，但按钮、复选框、链接等交互元素要阻止冒泡。

## 图表

- 使用 Kumo Chart 系列，优先 `TimeseriesChart` 和 `Meter`。
- 数据加载期间传 `loading`，让 Kumo 显示正弦波骨架，图表画布在加载完成前隐藏。
- 图表位于滚动容器或卡片内时传 `tooltipBoundary`，限制 tooltip 不越界。
- 使用 `ChartPalette.semantic(...)` 表示状态、告警、错误等有语义的数据。
- 使用 categorical palette 表示无天然好坏的多序列数据。
- 小尺寸图表要减少轴标签密度、缩短单位、控制 legend 和 tooltip 体积。

## 复制文本

需要展示并复制命令、token、URL、对象 key 时，优先使用 Kumo `ClipboardText`：

```jsx
<ClipboardText
  size="sm"
  text="npx @cloudflare/kumo help"
  tooltip={{ text: '复制', copiedText: '已复制', side: 'top' }}
/>
```

敏感值展示掩码时使用 `textToCopy` 保存真实复制值。

## 响应式布局

- 窄屏优先一行或两行内完成主要信息。
- 保持左右对齐，不让同一组指标在不同卡片中跳动。
- 展开后的卡片也要压缩密度，避免把移动端变成纵向长墙。
- 固定格式元素要设置稳定尺寸，例如 toolbar、icon button、meter、cover、chart 容器。
- 不按 viewport width 缩放字体。

## 展开与动效

- 列表展开、卡片详情、Docker 子面板、说明面板等统一使用 `src/js/components/AnimatedCollapse.jsx`。
- `AnimatedCollapse` 必须继续包裹 Kumo `Collapsible`，并保留 `data-open/data-closed` 与 `--collapsible-panel-height` 驱动的高度过渡。
- 含 chart 的展开区应配合 `DeferredRender` 或 Kumo chart `loading`，避免展开动画期间立即渲染重型画布。
- 不恢复旧 `quick-fade-in`、`motion-pop-in`、`app-collapse-panel` 这类自绘动画类。
- 动效要短、稳、可被 `prefers-reduced-motion` 关闭。

## 颜色与边框

- 使用 Kumo token：`bg-kumo-*`、`text-kumo-*`、`border-kumo-*`、`ring-kumo-*`。
- 卡片边框统一使用 `border border-kumo-line`，需要强调时加透明语义边框；不要额外叠页面级 `shadow-*` 或硬编码灰色。
- 百分比进度条使用 Kumo `Meter` 或 Kumo token 驱动的边框，不使用自绘彩条替代设计系统。

## PageHeader

当前 `@cloudflare/kumo` 包中 `PageHeader` 是 block source，不是 barrel 运行时导出。不要写：

```js
import { PageHeader } from '@cloudflare/kumo';
```

当前顶栏保留 `AppPageHeader` 作为紧凑过渡封装。若要替换，应先用 Kumo CLI 安装或复制官方 block，再适配：

- 顶栏已有外层 `border-b`，避免双边框。
- 顶栏标签页必须 `size="sm"`。
- 移动端断点使用 `450px` 起步，避免顶部换行。

## 验证命令

```bash
npm run lint
npm run build
rg -n --pcre2 '<(?-i:button|select|input|textarea)\b' src/js/pages src/js/components -S
rg -n 'DialogContent|TabsList|TabsTrigger|@cloudflare/kumo/components/tabs' src -S
rg -n 'vue|pinia|chart\.js|createApp\(|new Vue|from .vue.|from .pinia.|Chart\.' src package.json package-lock.json -S
rg -n 'quick-fade-in|motion-pop-in|app-collapse-panel|transition-shadow|hover:shadow|shadow-(xs|sm|md|lg|xl|2xl)' src/js src/css -S
```
