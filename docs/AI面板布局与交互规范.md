# AI面板布局与交互规范

最后更新：2026-08-19

AI 侧栏/全屏对话面板（`src/js/components/adminai/`）的布局与交互定案。凡涉及以下区域（消息区、折叠动画、输入框、头部按钮、品牌强调色）的改动，先对照本规范。

## 折叠动画（.askai-collapse）

消息卡片、推理展开块、工具步骤组共用一套 grid 折叠动画，三个坑必须避开：

1. **列轨道/行轨道必须显式 `minmax(0, …)`**：`fr` 只设上限、下限默认 `auto`（=内容 min-content）。
   - 列用 `grid-template-columns: minmax(0, 1fr)`，否则长 JSON/长路径（truncate/line-clamp 元素的 intrinsic 宽度）会把整条消息列撑出横向滚动；
   - 收起态用 `grid-template-rows: minmax(0, 0fr)`、展开态 `minmax(0, 1fr)`（两者可插值动画），否则收起态轨道缩不到 0，内容被裁后残留一条约 18px 的空白条。
2. **动画裁剪放在子元素层（`.askai-collapse > * { overflow: clip }`），不要放容器层**：容器 `overflow: clip` 会裁掉直接子元素外扩的 box-shadow（卡片 `ring-1` 边框整圈消失）。
3. **子级 clip 会误杀内部滚动区**：推理展开块（`.askai-reason-fade`，max-h 220px + overflow-y-auto）需要滚动，用高特异性恢复：`.askai-collapse > .askai-reason-fade { overflow-y: auto; overflow-x: clip; }`，不要为此把 clip 移回容器层。

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