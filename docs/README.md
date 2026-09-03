# API Monitor 文档

- [安全加固与扫描计划](./安全加固与扫描计划.md)

本目录只保留当前仍有维护价值的文档。历史迁移记录、一次性测试报告和包含本机路径的诊断材料已移除。

## 核心文档

- [开发指南](./开发指南.md)
- [API 接口文档](./API接口文档.md)
- [项目架构与技术详解](./项目架构与技术详解.md)
- [设计文档](./设计文档.md)
- [Oracle OCI 模块技术设计文档](./OracleOCI模块技术设计文档.md)
- [Oracle OCI 模块 API 路由清单](./OracleOCI模块API路由清单.md)
- [目录结构说明](./目录结构说明.md)
- [贡献指南](./贡献指南.md)
- [1Panel 快捷控制接口文档](./onepanel接口文档.md)

## 开发规范

- [前端开发最佳实践](./前端开发最佳实践.md)
- [Kumo UI 规则](./Kumo%20UI%20规则.md)
- [AI面板布局与交互规范](./AI面板布局与交互规范.md)
- [重构验证与例外清单](./重构验证与例外清单.md)
- [新模块接入指南](./新模块接入指南.md)
- [Go 后端启动指南](./GO后端启动指南.md)
- [待办任务闭环流程](./待办任务闭环流程.md)

### 前端布局约定

- Kumo `Tabs` 自带外层 ring、内部横向滚动和 active indicator，不要再额外包 `overflow-x-auto`、`overflow-hidden` 或 `p-px/px-px` 容器，否则容易裁掉四周描边并造成 tab 切换时宽度抖动。
- 页面级容器默认保持自然布局和 `overflow: visible`；只有表格、日志、终端、文件列表这类真实滚动区域才放 `overflow-auto`。
- 不要用手动 1px padding 修补 ring 被裁的问题。先检查祖先容器的 `overflow-hidden`、固定 `h-full/min-h-0/flex-1` 工作区和额外滚动壳，优先移除错误的裁切边界。
- 需要让主内容区填满视口时，优先使用父级 `min-h-full flex-1` 和子级 `min-h-0 flex-1` 的 flex/grid 传递，不要硬编码 `calc(100dvh - 9rem)` 这类高度。外层负责分配剩余空间，真实列表、表格或终端面板再在内部滚动。
- 视口型页面的底部与两侧 gutter 由 `MainLayout.jsx` 统一负责；大屏按 `lg:px-6` / `pb-6` 对齐时，页面内部不要再额外补一层页面级 `pb-*`，否则底部会比左右更厚。
- `PageStack` 默认适合自然文档页。不要为了“看起来更满”就把页面强行改成 viewport 工作区；不适合内部滚动的页面，直接让整页滚动，只要底部间距仍由主布局统一提供即可。
- 只有 Cloudflare、API 网关、API 接口文档这类桌面端确实需要固定工具栏、双栏面板或表格内部滚动的页面，才使用无底部 padding 的 viewport 变体，并把 `h-full / min-h-0 / flex-1` 链路传到底层滚动容器。
- 不要把“页面是否属于视口工作区”的判断散落在页面内部和硬编码高度里。应由 `MainLayout.jsx` 统一声明页面走 `document`、`viewport` 或“移动端整页滚动 + 桌面端内部滚动”的响应式工作区模式，页面内部只负责具体分栏和表格/列表滚动。
- 桌面端需要表格或双栏面板内部滚动时，优先让外层 `<main>` 在对应断点改为 `overflow-hidden`，再让页面根、分栏容器、表格壳依次提供 `min-h-0 flex-1`；不要只给最里层表格写 `height: 100%` 或 `overflow: auto`，否则高度链一断就会退回整页滚动。
- 修这类高度问题时必须同时检查 loading 分支和正常分支是否共用同一页根布局；只修主分支、漏掉 skeleton/loading 容器，会造成首屏和加载后高度行为不一致。
- 同一个模块的不同 tab 应尽量复用同一种页根布局，不要在“页面自然滚动”和“内部 workspace 视口”两种模式之间来回切换，否则会引入滚动条出现/消失、可用宽度重算和左右位移。
- 如果某个控件看起来比 Cloudflare 官方更“贴边”或更“薄”，先排查页面根字号、祖先裁切容器和滚动壳，再怀疑 Kumo 组件本身；这类问题通常是宿主布局造成的，不是 Tabs、LayerCard 或 Button 自己少了一层边框。
- `grid` 默认会拉伸同一行的卡片高度。内容型卡片、右侧操作栏、事件目录、导入执行面板等不需要等高时，在父级使用 `items-start`，必要时给子卡片加 `self-start`，不要用固定高度或多余 padding 填补底部空白。
- 卡片内部如果出现大块空白，优先检查外层 `grid items-stretch`、卡片自身 `h-full/min-h-*`、`flex-1`、`justify-between` 和 `bodyClassName` 是否把内容区撑满；只有确实承载图表、表格、终端或列表视口时才保留等高布局。
- 响应式卡片列表优先使用明确断点，例如 `grid-cols-1 sm:grid-cols-2 xl:grid-cols-4`。不要用过窄的固定 `minmax()` 上限造成大屏列数不足，也不要让占位卡片和真实卡片使用不同的高度模型。
- 预览型画布只需要“看个大概”时，不要重新计算节点布局。保留节点原始相对位置，先按包围盒归一化，再整体缩放；外框容器保持 `overflow-visible`，只把裁切放在内部 viewport，避免边框或 ring 被裁。
- 登录、初始化和密钥输入必须逐个核对具体输入框，不要只按页面关键字替换。管理员密码、新密码、确认密码使用 `type="password"`；2FA 验证码仍使用 `type="text"` + `inputMode="numeric"`。
- 将自绘按钮、占位操作、状态标签等替换为 Kumo 组件时，保留语义和交互状态：按钮用 `Button`，状态用 `Badge/StatusBadge`，可复制文本用 `ClipboardText`，面板优先用 `SectionCard/LayerCard`。替换后要检查 hover、focus、disabled、loading 和无数据状态是否仍然完整。
- 导入/导出、批量操作、自动刷新等配置区应保持紧凑一致：相关操作尽量合并到同一控制组，避免同一模块在“自然页面滚动”和“内部工作区视口”之间切换。
- 整页滚动页面（Servers、订阅分发、模型网关、OpenAI、DNS、Oracle、阿里云、腾讯云、M365 等）统一由 `app-main-panel` 承担页面滚动，`main` 使用 `flex-1 min-w-0 overflow-x-clip`（不要 `overflow-y-auto`，否则 tab 无法滑入覆盖面包屑）。面包屑（`z-20`）与 tab 栏（`z-30`）在同一滚动流，页面滚动时 tab 自动吸顶覆盖面包屑，效果等同 Cloudflare 官方。
- 模块默认走“整页滚动 + tab 吸附”模式；只有日志、Draw.io、提示词库这类真正需要填满视口并在内容内部滚动的页面才归入 viewport 工作区。表格/双栏页面不要再额外做“表格内部滚动、页面不滚”的混合模式——统一整页滚动，表格、双栏面板自然高，随页面一起滚。
- 模块级 tab 栏统一用 `stickyTabsBaseClass`（`flex items-center`，无 `flex-wrap`），不要用 `PageToolbar`（基类自带 `flex-col/flex-wrap/pb`，叠加 sticky 会换行并使 tab 栏高度异常）。tab 栏与右侧“更多”`按钮同行且不换行。
- tab 栏右侧的操作一律收进 `TabBarOverflowActions`：宽屏（`md`）内联展示带文字 Kumo 按钮，窄屏收起为一个与 tab 同高（`h-9 w-9 !rounded-lg`）的「更多」按钮，点击弹出官方 `DropdownMenu` 列表；每一项带图标与文字，禁止自绘列表。Select 类操作折叠进 `DropdownMenu.Sub` 子菜单，用 `RadioItem` 勾选。
- tab 栏右侧的搜索框用 `ResponsiveSearchInput`：宽屏展示输入框，窄屏折叠为搜索图标 Popover（内部输入 + 可选搜索按钮）。所有搜索框在移动端都应折叠为搜索按钮，不要占整行宽度。
- 双列表格布局（如 DNS 域名+记录、模型网关端点+模型）依赖祖先 `container-type` 容器与 `@container` 断点切换两列。改造页面时不要把容器类（如 `dns-workspace`）一并删掉，否则双列退回单列。两列都保持自然高、一起滚动，某列内容较短时直接变短，不强行拉平。
- 双列内容高度不均且需要短列常驻可视时（如系统设置·审计/安全/数据库的「左配置 + 右列表」）：父级 `grid items-start gap-4 cq-xl:grid-cols-[minmax(22rem,0.9fr)_minmax(0,1.1fr)]`，左侧配置卡（或多卡分组容器）加 `cq-xl:sticky top-[calc(var(--app-header-height)+0.5rem)] z-20`（吸顶时贴在页面吸顶 tab 栏下方）；`cq-xl:` 前缀保证吸顶只在双列断点生效——窄屏单列布局回到普通文档流，避免移动端滚动时卡片悬空。列内保持自然高，不额外引入表格内部滚动的混合模式。
- 双列中某一侧需要「sticky + 限高内部滚动」（如 API 接口文档的右侧接口详情：`cq-xl:sticky cq-xl:top-[70px] cq-xl:max-h-[calc(100vh-82px)] cq-xl:overflow-y-auto cq-xl:self-start`）时，另一侧（如接口目录）必须加 `self-start`，否则会被行高拉伸、内容短时底部留白；目录树自身保持自然高随页滚动。
- 表格/列表底部不留额外间隙：PageStack 使用 `viewport`（`pb-0`）变体 + 内容自然高度，不要再用 `h-full/flex-1` 撑满或内部 `overflow-auto` 制造填满视口的假象；底部被 content 自身内容决定，多余空白来自额外的底部 padding 或少显 `flex-1`。
- 所有页面内容区四边距统一为 12px（`--app-canvas-gutter-x/top/bottom`），由 `MainLayout.jsx` 统一负责；viewport 工作区分支与普通滚动分支都使用同一套 gutter 变量，保证仪表盘、系统日志、Draw.io 等页面四边一致。页面内部不要额外叠加 `px-px/pt-px` 这类 padding 造成内外累计不均匀。
- 改页面模板时必须同步检查 import（如 `stickyTabsBaseClass`、`TabBarOverflowActions`、`ResponsiveSearchInput`）；漏 import 会导致对应模块整页崩溃（`X is not defined`）。改动共享组件/常量（例如 `MODULE_TABS_PROPS`）会影响所有模块，需全局评估后再提交。修改后立即跑一次 `vite build` 兜底，能发现变量名笔误（如 `viewportWorkspace` 与 `viewportWorkspaceModule`）导致的构建失败。
- 实现折叠/展开的 grid 容器，轨道必须写 `minmax(0, …)`（`fr` 下限默认 auto=内容 min-content）：列不归零会被长 JSON/长路径撑出横向滚动，行不归零收起后残留空白条。动画裁剪放子元素层（容器 `overflow: clip` 会裁掉子元素外扩的 ring 边框）；需要内部滚动的子元素用高特异性规则恢复 `overflow-y: auto`，不要挪 clip。
- `overflow-hidden` 容器可被程序性横向滚动产生 `scrollLeft` 漂移（内容被 translate 撑宽时尤其容易），整体偏移排查优先查滚动容器，改用 `overflow-clip` 彻底禁止。
- 站点品牌色是橙色（`--color-brand: #dc7d40`，见 [AI面板布局与交互规范](./AI面板布局与交互规范.md)）；`kumo-brand` 是 Kumo 库默认蓝紫，不是站点品牌色，品牌视觉（入口按钮、hover 边框）用 `brand` 不要用 `kumo-brand`。

## 归档（历史 / 一次性文档）

以下文档为历史巡检、审查、交接或迁移记录，仅作回溯参考，不视为现行标准，也不转为 GitHub Issue：

- `接口巡检报告-本地.md` / `接口巡检报告-生产.md`（一次性全量接口巡检）
- `生产环境流程审查.md`（一次性审查）
- `修复审查清单.md` / `Bug审计与修复交接文档.md`（一次性交接）
- `kumo-design-违规审计清单.md`（一次性审计）
- `Go后端迁移状态.md`（迁移历史）
- `handoff-zcode-ask-ai-sidebar.md`（交接）

活跃开发工作改由 GitHub Issues + `backlog`/`in-progress`/`done` 标签跟踪，见下方「开发规范」与 `CONTEXT.md` 协作约定。

## 参考资料

- [PRD](./prd/)
- [Draw.io 图编辑工具模块 PRD](./prd/Drawio图编辑工具模块.md)
- [文档编辑器重构 PRD](./prd/文档编辑器重构.md)
- [Oracle OCI 主机管理模块 PRD](./prd/OracleOCI主机管理模块.md)
- [提示词库模块 PRD](./prd/提示词库模块.md)
- [Kumo 参考资料](./reference/)

## 文档命名与内容规范

### 文件名

- 文档使用简体中文命名，文件名取自 H1 标题并移除空格，例如 `Oracle OCI 模块 API 路由清单` 对应 `OracleOCI模块API路由清单.md`。
- 专有名词缩写保留原文大小写，如 `OCI`、`API`、`PRD`、`Kumo`、`Go`。
- `reference/` 下的 Kumo 快照由上游自动生成并随 Kumo 升级刷新，保留上游英文名，不参与汉化。
- 文档链接使用相对路径；文件名含空格时使用 `%20` 编码，例如 `[Kumo UI 规则](./Kumo%20UI%20规则.md)`。

### 内容

- 正文使用简体中文；代码、命令、API 路径、组件名等技术标识符保留英文原样。
- 首行 H1 标题与文件名保持一致。
- 维护型文档在标题下方标注 `最后更新：YYYY-MM-DD`。
- PRD 统一使用英文小节结构：`Problem Statement` / `Solution` / `User Stories` / `Functional Requirements` / `API Contract` / `Acceptance Criteria` / `Out of Scope`。
- 已废弃、历史或一次性内容必须显式标注，避免被误认为现行标准。
- 遵循下方文档安全约定。

## 文档安全约定

- 不写入真实密码、Token、Cookie、私钥、会话 ID 或云厂商凭证。
- 示例密钥统一使用 `<PLACEHOLDER>` 形式。
- 示例 IP 优先使用文档保留地址，例如 `203.0.113.10`。
- 本机绝对路径、临时目录、个人用户名和内部域名不要写入文档。
