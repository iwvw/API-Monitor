# AI面板布局与交互规范

最后更新：2026-09-20

AI 侧栏/全屏对话面板（`src/js/components/adminai/`）的布局与交互定案。凡涉及以下区域（消息区、折叠动画、输入框、头部按钮、品牌强调色、动效时长）的改动，先对照本规范。

## 消息动作与输入区

### 助手消息 footer（`MessageActions`）

助手消息在终态（非 pending、非流式）显示 footer 动作：复制整条回复、重新生成。复制内容取该消息所有 `text` part 拼接（不含推理与工具结果）；重新生成复用 `handleEditResend` 的截断重发路径，只是把入口从「用户消息」换成「助手消息往前找最近一条 user 消息」。流式期间两个动作都禁用（后端活跃 run 会 409）。

### 滚动到底部

消息列表容器是 `relative`，列表本体是内部滚动区，`atBottom` 由 `onScroll` 维护；不在底部时浮出「回到最新」圆形按钮（绝对定位在列表底部居中）。

### 输入区

- **思考强度**：`admin_ai_reasoning_effort`（设置页「基础设置」），取值 `low|medium|high`，留空表示不携带 `reasoning_effort`、保持上游默认。厂商差异（`max`→`high`、`thinking`/`effort` 别名、Claude 的 budget 语义）由 `/v1` 网关的 `normalizeReasoningEffort` / `requestEnablesReasoning` 统一处理，面板侧不做分支。非法值在写入时（`clampAISetting`）与请求构造时（`normalizeReasoningEffort`）各收敛一次，都不会透传给上游。
- **`@` 资源菜单**：子序列模糊匹配（`fuzzyMatch`，前缀与连续命中加权）+ 匹配高亮 + 上下键/Enter 键盘导航。菜单与输入框是兄弟节点，keydown 不冒泡到菜单容器，因此在**捕获阶段**挂 document 监听（capture 先于 target 冒泡）。
- **`/` 斜杠命令**：仅行首 `/` 且未输入空格时触发，命令只做本地动作（切模式/新对话/停止），不写入消息正文。命令清单在 `SlashCommandMenu.jsx` 的 `SLASH_COMMANDS`，新增命令必须对应已有能力，不引入新的后端语义。
- **模型选择器**（`ModelPicker.jsx`）：选项来自 `/api/openai/models`，空选项时不渲染。选择结果通过 `POST /api/admin-ai/messages` 的 `model` 字段传递，只影响随后发送的消息；空值表示用管理设置里的 `admin_ai_default_model`。
- **Enter 冲突**：`handleTextareaKeyDown` 在 `atMenuOpen || slashMenuOpen` 时直接返回，把 Enter 让给菜单键盘导航。

## 原语分层

对话界面按「行布局 / 气泡外观 / 内联 trace」三层拆分，避免单组件同时承担对齐、外观、状态与动作。新增原子组件放 `src/js/components/adminai/primitives/`，只放 Kumo 覆盖不到的业务组合。

- `MessageRow`（`primitives/MessageBubble.jsx`）—— 只管对齐（`align: start|end`）与槽位编排（`header` 状态条 / `children` 正文 / `footer` 动作）。对应 shadcn/ui `Message` 的职责。
- `MessageBubble`（同文件）—— 只管气泡外观。`variant: user|assistant|error` 决定配色，`shape: card|chat` 决定圆角与内边距，`streaming` 追加品牌色 ring。对应 shadcn/ui `Bubble` 的职责；**不要**在气泡里塞状态徽章或动作按钮。
  - `card`（助手正文）：`w-full rounded-xl px-4 py-3`
  - `chat`（用户气泡/编辑框）：`min-w-0 max-w-full rounded-2xl rounded-tr-md px-4 py-2.5`，宽度随内容
- `TracePill` / `TraceChevron` / `TraceTypingDots`（`primitives/TracePill.jsx`）—— 内联 trace 胶囊（推理、工具步骤组）与助手消息头三处共用同一视觉。`emphasis` 区分消息级外壳（实线边框）与内联 trace（半透明边框）。
- `StatusDot`（`primitives/StatusDot.jsx`）—— 工具/步骤状态环（running=品牌 spinner / success=绿勾 / failed=红叉）。尺寸走静态类名映射：Tailwind 无法从 `h-${n}` 拼接串生成 CSS，会静默失效。

这三处胶囊此前各自复制了一串 Tailwind 类，边框透明度已经漂移，统一收敛到 `TracePill`。新增同类折叠行必须复用，不要再复制类名。

### 弹层原语选择

会话下拉（Tabs + 会话列表 + 底部按钮）用 Kumo `Popover`，**不要**用 `DropdownMenu`：内含可聚焦控件时，菜单的 `menuitem` 语义与 roving tabindex 会破坏内部控件的键盘导航。`DropdownMenu` 只用于纯动作菜单（审批卡的次级动作、模型选择器）。`CommandPalette` 是模态 Dialog，不适用于锚定在输入框上方的内联弹层。

### 审批卡

主操作「仅此次」独占一行，其余动作（允许此对话 / 请求更改 / 拒绝）收进 `DropdownMenu`。「允许此对话」的二次确认走菜单项的两步点击（切到确认态再点一次），**不再**用 `setTimeout` 5 秒静默复位——那种写法键盘不可靠且不可测。过期判断用 `expiresAt` 算 `remainingMs`，不要拿倒计时文案字符串比较。

## 流式阶段文案

消息头的运行中文案由 `AskAiPanel/phaseLabel.js` 的 `streamPhaseLabel(parts)` 按**最后一个 part 的类型**推导，不再固定显示「正在回复…」：末段是工具调用/结果 → 正在执行工具，末段是推理 → 正在思考，末段是审批 → 等待审批，否则正在回复。外部 run（`live.phase`）优先用后端给的 phase。

**不显示耗时**：parts 数据模型没有 per-part 时间戳，任何「思考 N 秒」都是编造精度。只有历史行有 `createdAt`，不足以为时间线分段计时。若将来要做，先给 part 加时间戳再改文案。

## 动效 token

过渡时长与缓动一律引用 token，禁止在 `adminai/**` 与 `app.css` 的 `askai-*` 规则里裸写毫秒数或 `cubic-bezier(...)`。

- 源变量定义在 `app.css` 的 `:root`：`--motion-duration-{quick,base,medium,slow,slower}`（150/200/250/300/350ms）、`--motion-ease-{soft,snappy,out,panel}`。
- `@theme inline` 把 `--transition-duration-*` / `--ease-*` 映射到上述源变量，供 Tailwind 生成 `duration-quick|base|medium|slow|slower` 与 `ease-soft|snappy|panel` 工具类；手写 CSS 用 `var(--motion-duration-base)` / `var(--motion-ease-soft)`。
- 为什么源变量放 `:root` 而不是直接写在 `@theme`：`@theme inline` 会把取值内联进工具类且不落盘变量定义，`var(--duration-base)` 会悬空失效。`@theme` 只做映射，`var(--motion-*)` 才始终有值。
- 周期性循环动画（含 `infinite`，如 `askai-caret-blink`、`askai-live-pulse`）的时长是循环周期而非运动时长，不纳入 token；`animation-delay` 是错峰偏移量，同样不纳入。
- 把守脚本 `tools/motion-governance-check.mjs`（含在 `governance:check`）：`adminai/**` 与 `askai-*` 规则硬失败，其余区域只输出 warning 供增量迁移。

## 折叠动画（AnimatedCollapse）

消息卡片、推理展开块、工具步骤组共用 `src/js/components/AnimatedCollapse.jsx`（Kumo `Collapsible.Panel` 封装，高度 0↔auto 过渡 + `prefers-reduced-motion` 降级）。**不要**再自绘折叠容器，`app.css` 里旧的 `.askai-collapse` grid-rows 方案已删除。

历史坑位记录（自绘方案遗留，仅供理解为什么改走 Kumo）：旧实现依赖 `grid-template-rows: minmax(0, 0fr/1fr)` 插值，必须显式给列轨道 `minmax(0, 1fr)` 才能防长 JSON/长路径撑宽；容器层 `overflow: clip` 会裁掉子元素外扩的 box-shadow（卡片 `ring-1` 整圈消失），所以裁剪必须放子元素层；而子级 clip 又会误杀推理块的内部滚动区。这三个坑都是自绘 grid 方案的固有代价，换 `AnimatedCollapse` 后不再适用。

## 宽度与截断

- **工具步骤行**（`ToolCallCard.jsx`）：label/path/结果摘要统一 `min-w-0 line-clamp-1 break-all` + `title` 原文。注意 `-webkit-line-clamp` 元素在 Chrome 的内在尺寸（intrinsic sizing）贡献的是 max-content，**不负责消膨胀**——宽度收敛靠折叠容器轨道归零，clamp 只做视觉单行省略。
- **用户气泡/编辑框**：宽度上限 `max-w-full`（匹配助手回复卡宽度；曾用 85% 被用户嫌窄）。编辑框另加 `min-w-[10rem]` 防短文本被压窄导致「取消/发送」按钮行溢出重叠（16rem 会被嫌宽）。
- **工具步骤与正文间距**：定案 12px（卡片 `gap-2` + 正文 `mt-1`）；16px 会被嫌「有空位」。

## 滚动与偏移

- 侧栏「对话 ⇄ 管理」滑动容器用 `overflow-clip` 而非 `overflow-hidden`：hidden 允许程序性/惯性横向滚动，隐藏视图 `translate-x-8` 会把容器 scrollWidth 撑大、scrollLeft 漂移 32px，导致整个会话视图左移（面板右侧空出一条）。clip 彻底禁止滚动，scrollLeft 恒为 0。

## 头部按钮与输入框交互

- 右上角关闭按钮按上下文：管理视图打开时 =「关闭设置」（`setManageOpen(false)`，侧栏保持）；对话视图时 =「关闭侧栏」。Esc 键优先级一致。
- 设置按钮位于头部右上角（展开/关闭之间；全屏在「收回到侧栏」旁），不在输入框工具行内。
- 输入框底部工具行：左 = 行为 Tabs（询问/代理）+ 外部 run 指示器，右 = 发送/停止，左右组 `shrink-0`/`min-w-0` 防挤压。
- **外部 run 指示器**：其他通道（BOT/定时任务/API）正在跑当前会话时显示在工具行（模式切换右侧），脉冲点 + 状态文案 + 停止按钮；滑入动画（`askai-external-run-in`）+ 阶段切换文案淡入；**不显示会话标题名**（用户明确要求）；run 结束自动消失。

## 品牌色

- 站点品牌色 = **橙色系**：`--color-brand: #dc7d40`（登录页品牌区同款橙棕），光斑为 `#fb923c`。定义于 `app.css` 的 `@theme`。
- `kumo-brand`（oklch 260° 蓝紫）只是 Kumo 库默认强调色，**不是站点品牌色**；涉及品牌视觉（入口按钮、hover 边框、强调图形）一律用 `brand`。
- 已统一范围：Ask AI 入口按钮（打开态实底 `bg-brand ring-brand` + 白色图标）、全站卡片/按钮 `hover:border-brand/*`、仪表盘整套强调色、**公开页全套**（`pages/Public*` 7 个页面 + `components/public/PublicPageIconPicker`，共 38 处：左侧列表选中态/卡片高亮/地图节点/图标选择/进度条 tone 等，含 1 处蓝色硬编码阴影 `rgba(59,130,246,0.08)` → 品牌橙）、**主程序全量品牌强调**（35 个文件 332 处：`text/bg/border/hover/ring/stroke` 等前缀统一 `kumo-brand` → `brand`；`to-kumo-brand-hover` 渐变 → `to-brand-hover`，配套新增 `--color-brand-hover: #c96a33`）。
- **保留 `kumo-brand` 的地方 = 焦点态**（`focus:*`/`focus-visible:*`/`has-[…:focus]` 的 ring/border/text，共 15 处）：焦点环是键盘交互指示的库默认语义，非品牌视觉，勿改。
- 全站仍大量用 `kumo-brand` 做普通强调色（图标、链接、焦点环），属库默认行为，与品牌色共存，勿混用语义。