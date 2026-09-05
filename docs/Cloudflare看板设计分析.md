# Cloudflare 官方看板（Analytics Tiles）设计分析

- 分析日期：2026-09-05
- 分析对象：Cloudflare 帐户主页（dash.cloudflare.com/home）Analytics 区块，卡片可拖拽、可缩放、容器自适应
- 分析方法：页面 DOM 反推 + react-grid-layout 公开 API 对照 + 与本站（API-Monitor）技术栈对照

## 0. 结论摘要

1. **底层库是 react-grid-layout**（STRML 开源库，或其 fork）：DOM 中 `react-grid-layout`、`react-grid-item react-draggable cssTransforms react-resizable` 是该库输出类名的实锤。
2. **卡片壳 = kumo 设计 token**：`bg-kumo-base ring-kumo-line hover:ring-kumo-fill` 等，与本站安装的 `@cloudflare/kumo@2.13.1` 同款。
3. **图表 = echarts**：卡片 DOM 带 `_echarts_instance_` 属性，canvas 渲染。
4. **「流畅」的三个来源**：
   - 拖拽用 **transform（GPU）定位**，全程不触发布局重排；
   - 网格容器高度用 **min-height + 200ms 过渡动画**，增删卡/换行时平滑；
   - 卡片内部是 **`flex-1 min-h-0` 自适应链** + echarts ResizeObserver，缩放时图表即时收缩不溢出。
5. **本站落地成本很低**：echarts、kumo 都已具备，唯一缺的是 react-grid-layout 一个依赖 + 布局持久化端点。

## 1. 页面分层结构

```
section.group/analytics            ← 整个看板是一个 section
├── DashboardHeader                ← 区块级工具栏
│   ├── h3 "Analytics"（text-lg font-semibold）
│   └── 按钮组（gap-2）
│       ├── Popover：时间范围（"过去 24 小时"）——节级筛选，驱动所有 tile
│       ├── DropdownMenu：+ 添加指标（size-9 圆形图标钮，aria-label="添加指标"）
│       └── Button：刷新所有图块（size-9 圆形图标钮）
└── .tile-grid-root                ← 网格容器（w-full mx-auto，max-width 1000px）
    ├── 高度包装层（relative + transition-[min-height] duration-200）
    └── .react-grid-layout（绝对定位子项）
        └── .react-grid-item.react-draggable.cssTransforms.react-resizable
            ├── TileFrame（relative w-full h-full）
            └── DashboardChartShell（卡片本体）
```

要点：时间范围在**区块级**（Popover），添加/刷新也是区块级；卡片本身不再有工具栏，只有标题 + 内容 + 角落缩放柄。

## 2. 网格机制（react-grid-layout）

DOM 可反推出的参数：

- 容器 `max-width: 1000px`，该宽度下排 **2 列**，首卡 494×241：`494 = (1000 − 12 间距) / 2`。
- 高度公式（RGL 标准）：`itemHeight = rowHeight × rowSpan + marginY × (rowSpan − 1)`；`241 ≈ 110 × 2 + 21`，即 rowHeight≈110、margin≈[12, 21]。
- 所有子项 `position: absolute` + `transform: translate(x, y)` —— **transform 定位**是拖拽流畅的核心前提。
- 容器高度由内容行数决定，行数变化时靠外层 `transition-[min-height] duration-200` 补动画（`transition-timing-function: ease`，`motion-reduce:transition-none` 尊重系统减弱动效）。

响应式是 RGL 内置能力：`WidthProvider` 测量容器宽度 + 断点布局（cols per breakpoint），窄屏自动降列数。

## 3. 卡片解剖（可复刻的 CSS 配方）

网格项（绝对定位，外层）：`relative w-full h-full` —— 定位交给网格，卡片填满自己的格子。

卡片壳（DashboardChartShell）：

```
flex h-full min-h-0 flex-col overflow-hidden rounded-lg
bg-kumo-base ring ring-kumo-line transition-colors hover:ring-kumo-fill
```

- `h-full min-h-0` + 内部 `flex-1 min-h-0` 链：**卡片内容随格子缩放即时自适应**，这是「自适应大小」的关键，缺 `min-h-0` 内容会把卡片撑破。
- `hover:ring-kumo-fill`：整卡悬停有极轻的交互暗示，不突兀。
- 卡内无 padding，靠子元素各自控制：

| 区块 | 配方 |
|---|---|
| 标题行 | `flex items-start gap-3 px-4 pt-3 pb-0.5`；标题 `truncate text-xs font-medium text-kumo-subtle`（带 title 属性，超长省略） |
| 主体 | `min-h-0 flex-1 overflow-hidden p-0` |
| 数值行 | `flex items-baseline gap-2`：大数 `text-2xl font-semibold leading-tight text-kumo-default`（59.67k）+ 环比 `text-sm font-medium text-kumo-success`（↗ 75.1%） |
| 图表区 | `-mx-4 mt-1.5 min-h-10 flex-1 relative overflow-hidden`，内嵌 echarts（canvas 填满剩余高度；241px 卡内图高 173px） |

细节：`-mx-4` 负外边距让图表**出血到卡片左右边缘**（标题行还留 px-4 内边距），视觉上图表比文字更宽，层次分明。

## 4. 流畅感来源（对照清单）

用户感知的「非常流畅」由四件事叠加：

1. **拖拽 transform 化**：RGL `useCSSTransforms`（默认开）——拖动中只改 `transform: translate3d`，GPU 合成层上移动，不重排、不重绘布局；松手后才落回绝对坐标。
2. **碰撞重排带动画**：拖过其他卡片时，被挤开的卡片也是 transform 过渡移动（CSS transition），不是瞬跳。
3. **容器高度动画**：外层 `min-height` 200ms 过渡，加卡/删卡/布局变化时整块区域高度丝滑过渡。
4. **内容即时收缩**：`flex-1 min-h-0` 链 + echarts 监听容器 resize，拖拽/缩放过程中图表实时重算画布尺寸。

## 5. 本站（API-Monitor）现状对照

| 项 | Cloudflare 官方 | 本站现状 | 差距 |
|---|---|---|---|
| 图表 | echarts | echarts ^6.1.0 ✅ | 无 |
| 设计系统 | kumo | @cloudflare/kumo 2.13.1 ✅ | 无 |
| 网格/拖拽/缩放 | react-grid-layout | 无（DashboardPage.jsx 1342 行静态 CSS Grid + cq-* 断点） | 需加依赖 |
| 卡片壳 | DashboardChartShell | AppCard / SectionCard / DashboardOverviewCard（同款 kumo token） | 直接复用样式 |
| 布局持久化 | 服务端保存 | 后端 settings Service 已有 ServeHTTP 模式（`/api/settings` 同层），可挂 layout 保存端点 | 需新增 |

本站 DashboardPage 现有的 `DashboardOverviewCard`、`MiniMeter`、`SectionCard` 都是同一套 kumo 视觉语言，卡片外观可直接迁移；`min-h-0 flex-1` 链在 SectionCard bodyClassName 中已有先例（`flex min-h-0 flex-1 flex-col p-2.5`），说明卡片内部自适应模式的基建已在。

## 6. 落地建议（后续实施参考）

- **依赖**：`react-grid-layout`（MIT，Cloudflare 生产同款）；卡片组件用现有 AppCard/DashboardOverviewCard 改造出 `TileFrame`。
- **卡片清单**：把现有统计卡（主机、服务器、PaaS、DNS、可用率、文件柜、调度、API 趋势）抽成可增删的 tile，区块级时间范围筛选与卡内图表联动。
- **布局持久化**：`{i, x, y, w, h}` JSON 数组存后端（settings 服务同级），`onLayoutChange` 防抖保存；重启/刷新不丢。
- **平滑细节**：容器包一层 `transition-[min-height] duration-200 ease`；echarts 实例挂 ResizeObserver；拖拽期 hover 效果压到最轻避免闪烁。
