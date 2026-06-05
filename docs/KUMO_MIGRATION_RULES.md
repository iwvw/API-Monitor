# Kumo-only UI 迁移规则

本文档是 API-Monitor 后续 UI 改造的硬约束。目标是让系统完全对齐 `E:\Code\kumo` / `@cloudflare/kumo`，不再维护本项目自己的 UI 组件体系。

## 总规则

1. 页面可以是 React component，业务逻辑可以抽 hook，但 UI 基础组件必须使用 Kumo。
2. 不新增自写 Button、Input、Select、Tabs、Table、Dialog、Toast、Checkbox、Switch、Sidebar、Loader、Badge、Tooltip、Popover、Dropdown 等组件。
3. 不新增本地 UI 包装组件。已有包装组件要逐步替换为直接使用 Kumo。
4. 不写自绘 switch、checkbox、modal、tab、toast、table header、resize handle。
5. 不硬编码主题颜色。优先使用 `bg-kumo-*`、`text-kumo-*`、`border-kumo-*`。
6. 旧 Vue/原生 CSS 只作为业务逻辑参考，不作为样式来源。

## 允许保留的代码类型

- 页面组件，例如 `OpenAIPage.jsx`、`DnsPage.jsx`。
- 业务 hook，例如请求数据、状态同步、表格列宽状态。
- 数据转换函数。
- API client/helper。
- 页面内必要布局代码，但布局必须由 Kumo 组件和 Kumo token 驱动。

## 禁止新增的代码类型

- 本项目自己的通用 UI 组件。
- 用 `<button>` 模拟 Tabs。
- 用 `<select>` 模拟 Kumo Select。
- 用 div + CSS 模拟 Dialog。
- 用 input + CSS 模拟 Switch/Checkbox。
- 用自定义 DOM 直接管理 Toast。
- 用本地 CSS 大面积覆盖 Kumo 视觉。

## Kumo 组件优先级

| 需求 | 必须优先使用 |
|------|--------------|
| 操作按钮 | Kumo `Button` |
| 文本输入 | Kumo `Input` |
| 多行输入 | Kumo `Textarea` |
| 下拉选择 | Kumo `Select`，隐藏标签时使用 `aria-label`，不要使用已弃用的 `hideLabel` |
| 标签切换 | Kumo `Tabs` |
| 表格 | Kumo `Table` |
| 表格拖动 | Kumo `Table.ResizeHandle` |
| 弹窗 | Kumo `Dialog` |
| 通知 | Kumo `Toasty` / Kumo toast manager |
| 开关 | Kumo `Switch` |
| 勾选 | Kumo `Checkbox` |
| 侧边栏 | Kumo `Sidebar` |
| 加载 | Kumo `SkeletonLine` / Loader |
| 搜索建议 | Kumo `Autocomplete` |

## 当前必须替换的已知项

- `src/js/components/ModuleTabs.jsx`：替换为页面内直接使用 Kumo `Tabs`。
- `DnsPage.jsx` 顶部 tab 原生按钮：替换为 Kumo `Tabs`。
- `DnsPage.jsx` 账号选择原生 `select`：替换为 Kumo `Select`。
- 所有自绘或原生 action button：复核是否可替换为 Kumo `Button`。
- 所有数据密集型表格：复核是否使用 Kumo `Table` 和 `Table.ResizeHandle`。

## 例外规则

如果 Kumo 缺少某项能力，agent 必须：

1. 先在 `E:\Code\kumo` 中确认确实没有对应组件或 API。
2. 在 `docs/refactor-verification.md` 记录例外原因。
3. 使用最小原生语义实现，不做视觉组件封装。
4. 后续一旦 Kumo 提供能力，必须替换。

## 验收命令

每次 UI 修改至少运行：

```bash
npm run build
```

涉及路由、弹窗、表格、主题、导航时，还必须做浏览器验证，并把结果写入 `docs/refactor-verification.md`。
