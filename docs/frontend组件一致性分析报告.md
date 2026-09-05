# API Monitor 前端组件一致性 + Bug 分析报告

- 生成日期：2026-09-05
- 分析范围：`src/js/` 全部 186 个源文件（pages / components / hooks / modules，排除 `*.test.js`、`src/css`）
- 设计系统基线：`@cloudflare/kumo@2.13.1`（本机实际安装版本）
- 官方文档基线：https://kumo-ui.com/（41 组件 + 6 图表 + 3 blocks 全部核对）
- 结论性质：只读审计，未修改任何代码。所有 file:line 均经人工核对真实行号。

> 供后续 Agent 使用：每个问题都给出了文件定位、代码片段、影响与修复建议。修复前请遵守
> CONTEXT.md 的多 Agent 文件持有约定（ServerPage.jsx / DnsPage.jsx / OpenAIPage.jsx / MainLayout.jsx 等单所有者文件）。

> 修复状态（2026-09-05）：P1/P2/P8、GcpPage size、aria-label×6、Text heading3、markdown.js href、
> formatFileSize 边界、transition-colors×3、parseInt radix×10、AskAiPanel 问候语时区 均已修复并通过
> `npm run lint` / `npm test`（408 通过）/ `npm run ui:governance`。治理工具 `public` 目录盲区已修复
> （`skippedDirs` 移除 `public`，PublicPageIconPicker 3 处自绘按钮登记白名单，并顺手将图标删除
> `dialog.confirm` 迁移到 `dialog.deleteResource` 消除新暴露的告警）；CONTEXT.md / docs/Kumo UI 规则.md
> 版本已更新为 2.13.1。**P3/P4 已修复**：10 处 `useStore()` 无 selector 全量订阅全部改为精确 selector
> （单字段用 `useStore(s => s.x)`，多字段用 `useShallow`），ServerPage 每 15s 回写全局 store 的死同步已移除
> （已确认 `s.serverList` 无任何消费者）。**Toolbar size 决策已定**：刻意保留 `size="sm"`（
> `Toolbar.Button/Input` 尺寸由上下文强制注入，官方 2.13.1 无非废弃紧凑路径），决策与迁移预案已写入
> `docs/Kumo UI 规则.md` 新增「Toolbar（已记录的刻意决策）」节。尚未修复：P5 图表轴时区、
> P6/P9/P10 其余位置、自绘组件替代（§3.1）。验证备注：`npm run ui:governance` 当前整体失败仅因
> 另一个并发 Agent 窗口新增的未跟踪文件 `src/js/pages/BookmarksPage.jsx`（4 处违规，非本报告范围、
> 非本次改动所致）；本报告涉及文件均通过治理扫描。

---

## 0. 执行摘要

### 总体结论

代码质量整体很高：ESLint 全绿、无条件 hook、无 render 内 setState、定时器/事件监听清理基本齐备、
`dangerouslySetInnerHTML` 全部经 DOMPurify、无高危崩溃或数据破坏类 bug。发现的问题集中在四类：

| 类别 | 数量 | 最高严重度 |
|------|------|-----------|
| 显性/隐性逻辑 Bug | 11 | 中 |
| 自绘组件替代官方 Kumo | 约 22 处 | 中 |
| Kumo 用法不一致（尺寸/aria/弃用 API） | 30+ 处 | 中 |
| 治理/文档基线问题 | 4 | 低 |

### 最优先修复的三件事

1. **PublicStatusPage 可用率 0% 显示错误**（数据正确性，直接影响公开状态页可信度）
2. **TotpPage 扫码 50ms 延迟无取消守卫**（摄像头资源泄漏 + 状态错位）
3. **`useStore()` 无 selector 全量订阅 + ServerPage 15s 轮询写全局 store**（大规模页面性能隐患）

---

## 1. 基线核对

### 1.1 版本与文档不一致

| 项目 | 现状 | 建议 |
|------|------|------|
| CONTEXT.md | 写 `@cloudflare/kumo 2.10.0` | 实际安装 2.13.1，需更新 |
| CONTEXT.md Kumo 组件清单 | 含 `TimeseriesChart`、`ChartPalette` 等 | 2.13.1 已新增更多组件，见 §3.5 建议 |

### 1.2 官方弃用 API 在项目中的使用情况

| 官方弃用项（依据 kumo-ui.com） | 项目中现状 | 处置 |
|------|------|------|
| `Toolbar` 的 `size` prop（2.10.0 起弃用，应省略取默认 base） | **17 处**全部 `size="sm"`：BackupPage:240、DrawioPage:702、GitHubPage:2490、DnsPage:2380/3003/3062、OpenAIPage:1262、OraclePage:1348、M365Page:1857、AliyunPage:438、PaasPage:2888、DS2APIPlugin:451、AntigravityPlugin:452、TencentPage:455、TotpPage:1991、UptimePage:2256、ServerPage:7689/10256 | ⚠️ 与项目「密度」规则冲突。需项目决策：跟随官方去掉 size（工具栏变高），或记录为有意保留 |
| `Text` 的 `heading1/2/3` 变体（弃用，改用 `heading` + `as`） | GitHubPage:1287 `variant="heading3"` | 机械迁移 |
| `Table.CheckCell` 的 `onValueChange`（弃用，改用 `onCheckedChange`） | 无违规（DnsPage:2482/2855 已用 onCheckedChange） | 无需处理 |
| `MenuBar`（已弃用） | 无使用 | 无需处理 |

---

## 2. 显性 / 隐性 Bug 清单

按严重度排序。`file:line` 均已核对。

### P1. PublicStatusPage 实时可用率 `||` 吞掉 0 值（数据正确性）

- 位置：`src/js/pages/PublicStatusPage.jsx:298-299`（另见 :526）
- 代码：
  ```js
  uptime24h: beat.uptime24h || monitor.uptime24h,
  uptime30d: beat.uptime30d || monitor.uptime30d,
  ```
- 问题：可用率 0~100，**0 是合法值**（全挂时可用率 0）。`0 || old` 永远落到旧值；
  若旧值 undefined，随后 `formatUptimePercent(undefined)`（:90 默认 fallback 是字符串 `'100'`）→ **真实 0% 显示为 100%**。
  :526 `monitor.uptime30d ? ... : '--'` 同样把 0 显示为 `--`。
- 影响：公开状态页在真实全故障场景下仍显示 100% 可用，数据不可信。
- 修复：改用 `??`（`beat.uptime24h ?? monitor.uptime24h`），并让 `formatUptimePercent` 对 undefined 与真实 0 区分（0 应显示 0，不落 fallback '100'）。

### P2. TotpPage 扫码 50ms 延迟启动无取消守卫（摄像头泄漏 + 状态错位）

- 位置：`src/js/pages/TotpPage.jsx:1093-1141`（配套 `stopQrScan` :1144-1154、弹窗关闭回调 :2111）
- 代码：
  ```js
  setIsScanning(true);
  setTimeout(async () => {          // 句柄未保存，无法取消
    const html5QrCode = new window.Html5Qrcode('qr-reader');
    scannerRef.current = html5QrCode;
    await html5QrCode.start(...);
  }, 50);
  ```
- 问题：用户在 50ms 窗口内关闭弹窗/点停止时 `scannerRef.current` 仍为 null，`stopQrScan` 空跑；
  50ms 后定时器照样创建实例并启动摄像头。此时 `isScanning=false` 但摄像头在录，UI 与真实状态错位；
  再次点「开启摄像头」会创建第二个实例覆盖 ref，第一个实例永远无法 `stop()`（摄像头常驻、重复解码）。
- 修复：定时器句柄存入 ref；`stopQrScan` / 组件卸载时 `clearTimeout` + 加「已取消」标志；启动前先停旧实例。

### P3. `useStore()` 无 selector 全量订阅（性能）

- 位置：`App.jsx:96`、`MainLayout.jsx:448`、`ServerPage.jsx:2473`（另见 SettingsPage:153、AuthPage:363、DashboardPage:395、OpenAIPage:583、PaasPage:88、GitHubPage:1759、DnsPage:336）
- 问题：Zustand `useStore()` 不带 selector 会订阅整个 store 对象，任意 `setState`（切 tab、主题、Dashboard 30s 一次的 `appProcessUptimeSeconds`、ServerPage 15s 轮询）都触发这些巨型组件整树重渲染。
- 修复：改为精确 selector（`useStore(s => s.theme)`）或用 `useShallow` 组合标量。ServerPage 是 11K 行页面，收益最大。

### P4. ServerPage 把本地 serverList 全量回写全局 store

- 位置：`src/js/pages/ServerPage.jsx:2968`
- 代码：`useEffect(() => { useStore.setState({ serverList }); }, [serverList]);`
- 问题：serverList 引用每次轮询（15s）都变，制造全局 store 变更风暴，连带 P3 中所有 `useStore()` 消费者重渲染；同时制造 ServerPage 自身的循环渲染（写 store → 读 store → 重渲染）。
- 修复：确认是否有组件消费 `s.serverList`；若无应删除该同步，或改为仅在需要时局部更新。

### P5. 站点时区约定被绕过（业务归属 + 图表轴）

- 位置：`AskAiPanel.jsx:71-72`（问候语 `new Date().getHours()`，「几点」业务归属）；
  图表轴/时间标签用浏览器本地时区：`PublicStatusPage.jsx:99-102`（`formatChartTime` 用 `getHours`）、
  `DnsPage.jsx:248-250`、`UptimePage.jsx:70`、`DashboardPage.jsx:917`、`GcpPage.jsx:219`、`ServerPage.jsx:1216/1876`
- 影响：违反 CONTEXT.md「站点时区唯一控制点」约定。问候语是「几点」归属判断（问候语严重度中），图表轴属显示层（低）。
- 修复：问候语走 `getDisplayTimeZone()`/后端显示字段；轴标签统一 `formatDateTime`（utils.js 已支持 displayTimeZone）。

### P6. ServerPage 流量周期校验用浏览器本地时区解析无时区日期

- 位置：`src/js/pages/ServerPage.jsx:4142-4143`
  ```js
  const cycleStart = new Date(`${serverForm.trafficCycleStart}T00:00:00`).getTime();
  const cycleEnd   = new Date(`${serverForm.trafficCycleEnd}T23:59:59`).getTime();
  ```
- 影响：站点时区与浏览器时区跨日界线时，`cycleEnd < cycleStart` 前端校验可能误判。仅影响前端校验（后端裁决），低危。

### P7. markdown.js 图片预览锚点被 DOMPurify 剥离 href（功能退化）

- 位置：`src/js/modules/markdown.js:46`
  ```js
  return `<div class="msg-image-container"><a href="javascript:void(0)" class="img-preview-trigger"><img .../></a></div>`;
  ```
- 问题：`ALLOWED_URI_REGEXP`（:17-18）显式拦截 `javascript:`，DOMPurify 会剥离该 href → 锚点无 href；
  且全项目找不到 `img-preview-trigger` 的点击消费方（无 CSS、无事件委托），预览点击实际不可用（图片本身正常内联显示）。
- 修复：删除 `href="javascript:void(0)"` 或改用 `href="#"` + onClick preventDefault，并补上实际预览行为或移除残留锚点。

### P8. SubscriptionPage 弹窗 effect 内 fetch 回调无取消守卫

- 位置：`src/js/pages/SubscriptionPage.jsx:1052-1070`
- 问题：请求在途时卸载/关闭弹窗，回调仍 `setCloudflareAccounts(...)`（卸载后写入 + 过期响应覆盖）。
- 修复：加 `let cancelled` 标志或 AbortController（项目大部分 async 已有此模式，此处遗漏）。

### P9. parseInt 缺 radix（23 处）

- 位置：ServerPage:3500/3501/3520/3521/5377/8527/8536/8544/8553/11046/12180/12199、UptimePage:2012/2058/2068/2089/2175、NotificationPage:1469/1478/1569/1840/1855
- 影响：`"08"` 旧引擎按八进制等边界行为。当前输入基本是数字串，实际风险低，机械可修。

### P10. utils.js formatFileSize 对 0~1 / 负值输出 `undefined` 单位

- 位置：`src/js/modules/utils.js:127-133`
- 影响：`formatFileSize(0.5)` → `"0.5 undefined"`。当前调用场景均为非负整数（有 `bytes===0` 前置），边缘低危。

### P11. 提示项：骨架屏等静态列表 `key={index}`

- 位置：PublicStatusPage:213（30 格固定心跳）、ServerPage:9124、DnsPage:2134/2399/2670、OraclePage:1857、OpenAIPage:1973、M365Page:147/173 等
- 说明：全部为静态骨架/固定 `<col>` 表，无内部状态，index key 安全。仅记录备查，若将来加动画/展开态需改。

### 未发现问题的类别（给后续维护者的信心）

- 定时器/事件监听：所有 `setInterval/setTimeout/requestAnimationFrame` 均有 cleanup；window/document 监听均配对移除
- 无嵌套交互元素（button 内嵌 button 等）、无受控/非受控切换警告
- 所有 `dangerouslySetInnerHTML` 均经 `renderMarkdown`（marked + DOMPurify + URI 白名单）或 shiki 转义
- 无 store 对象直接 push/splice 修改、无 render 期间 setState
- echarts/xterm/socket/ResizeObserver/URL.revokeObjectURL 均正确 dispose

---

## 3. 组件一致性与官方替代建议

### 3.1 自绘组件重复实现官方 Kumo 组件

#### 3.1.1 自绘 badge / chip（→ Kumo `Badge`）

| 位置 | 说明 |
|------|------|
| `components/adminai/MessageList.jsx:531` | `rounded-full bg-kumo-tint px-1.5 py-0.5 text-[10px]` 状态徽章「已停止」 |
| `components/adminai/MessageList.jsx:534` | 同上「出错了」（`bg-kumo-danger/10 ... text-kumo-danger`） |
| `components/adminai/AskAiPanel.jsx:1408` | 写权限徽章（`rounded-full bg-kumo-success/10`） |
| `components/adminai/ToolCallCard.jsx:347/375` | 数量计数 chip（`rounded-full bg-kumo-base ... text-[10px]`） |
| `pages/SchedulerPage.jsx:793` | 画布浮动计数标签 |
| `components/forward/ForwardCanvas.jsx:363` | 用 Kumo Button 强覆盖样式模拟 chip（主机过滤标签） |

建议：`Badge variant="..."` 替代（Badge 已支持 `variant` 语义色 + `appearance="dot"`，见 kumo-ui.com/components/badge）。

#### 3.1.2 自绘 meter / 进度条（→ Kumo `Meter`，百分比/用量场景官方明确用 Meter）

| 位置 | 说明 |
|------|------|
| `pages/openai/plugins/AntigravityPlugin.jsx:591-596` | 额度占比条（`h-1.5 w-full ... rounded-full` 手写轨道+填充） |
| `pages/SettingsPage.jsx:1915-1919` | 数据库分段占用条（多段需保留手绘或重构） |
| `pages/OpenAIPage.jsx:2740-2747` | 模型用量占比条（用了 ChartPalette，需保留配色） |
| `pages/ServerPage.jsx:486-491` | 迷你指标条组件 |

注：ServerPage:674/6013/9089/9266 的 `trackClassName="!h-1 ..."` 是 **Kumo Meter 的自定义轨道类，合规**，不要误改。

#### 3.1.3 Empty 空状态双实现（→ Kumo `Empty`）

- `src/js/components/ui/AppPrimitives.jsx:594` 的 `EmptyState`（icon + title + description + action，**76 处调用**）
- `src/js/components/adminai/AskAiPanel.jsx:68` 的本地 `EmptyState`（云朵动画 + 问候，业务组合，合理但同名冲突）
- 同名组件两套实现，且 AppPrimitives 的 EmptyState 功能等价于官方 `Empty`（官方 props：`title/icon/description/contents/commandLine/size`）。
- 建议：AppPrimitives 的 EmptyState 迁移到 Kumo `Empty`（`action` → `contents`，`card` → 外层自己套 LayerCard/Table.Cell），
  消除重复；AskAiPanel 的本地 EmptyState 建议改名（如 `AskAiEmptyState`）避免与全局混淆。

#### 3.1.4 自绘状态 pill（→ Kumo `Badge`）

- `src/js/components/ui/AppPrimitives.jsx:582-592` `getStatusPillClass` / `getHttpStatusPillClass`
  返回手写 `bg-*-10 text-*-* border-*` pill 类，被 UptimePage/TencentPage/OraclePage/OpenAIPage/ApiDocsPage/AliyunPage/GcpPage/M365Page/PaasPage 共 9 个页面使用。
- 项目已有合规的 `StatusBadge`（:563，包装 Kumo Badge）。建议自绘 pill 统一走 `StatusBadge` / Kumo Badge。

### 3.2 Kumo 用法不一致

#### 3.2.1 GcpPage 尺寸体系偏离（全站 Select/Input 默认 sm）

- **Select `size="base"` ×5**：`GcpPage.jsx:1959/1966/1973/1986/2033`（创建实例/存储桶表单）
- **Input `size="md"` ×8**：`GcpPage.jsx:1884/1932/1936/1955/1981/2008/2025/2029`（`md` 不是官方合法 size）
- 全站其余 60+ 处 Select 均为 `size="sm"`；GcpPage 同页其它 Input 也是 sm（如 :1669）→ 页内混用。
- 建议：统一 `size="sm"`（`md` 直接非法，需改）。

#### 3.2.2 图标按钮缺 aria-label（官方要求 icon-only Button 必须 aria-label）

| 位置 | 语义 |
|------|------|
| `components/server/SftpPanel.jsx:317` | 返回上级目录 |
| `components/forward/ForwardDialog.jsx:691` | 添加备用目标 |
| `pages/ApiDocsPage.jsx:2068` | 刷新密钥监控 |
| `pages/OraclePage.jsx:1103/1104/1105` | 启动 / 停止 / 调整规格 |
| `pages/ApiDocsPage.jsx:1974`、`OpenAIPage.jsx:4269` | Popover 内日期触发器 |

#### 3.2.3 transition-colors hover 颜色过渡（官方禁）

- `pages/DnsPage.jsx:2102`、`pages/SettingsPage.jsx:1281`、`pages/TotpPage.jsx:2598`、
  `components/MainLayout.jsx:861`、`components/forward/ForwardCanvas.jsx:363`（改 Badge 时一并处理）

#### 3.2.4 大面积 CTA 尺寸

- `AuthPage.jsx:688` `Button size="base"`（认证页主 CTA，页内 256 行返回按钮为 sm，页内混用）
- `PublicSharePage.jsx:358` `Button size="lg"`（公开页下载主 CTA）
- 低严重度，疑似刻意设计，不建议机械改。

### 3.3 官方有更佳替代、值得利用的组件（当前未用或可替代）

依据 https://kumo-ui.com/ 官方文档：

| 官方组件/能力 | 官方推荐用法 | 项目现状与建议 |
|------|------|------|
| `DropdownMenu.LinkItem` | 导航项必须用 `LinkItem`（不要 `Item` + href） | 建议迁移项目中 DropdownMenu 内的链接项 |
| `Dialog.Root role="alertdialog"` | 破坏性确认官方路径是 alertdialog（或 DeleteResource），非普通 dialog | 检查破坏性删除是否都走 `dialog.deleteResource`（见下） |
| `DeleteResource` | 删除确认 block：需输入资源名 | 项目已逐步迁移，符合官方方向；继续推进 |
| `TimeseriesChart` | 时间序列数据直接用 TimeseriesChart，`Chart` 仅用于饼图等自定义 | 项目 UptimePage 仍留有遗留 ECharts 直接 setOption 的图表（见 docs/重构验证与例外清单），触碰时迁移 |
| `SkeletonLine` vs `Loader` | 内容占位用 SkeletonLine，操作进行中用 Loader；图表 loading 用骨架 | 项目已基本正确，未见误用 |
| `TooltipProvider` | 官方要求在应用根部只包一次，做 tooltip 延迟分组 | **main.jsx 未包**。功能不受影响（tooltip 无需 Provider 也能用），但建议补上获得统一延迟行为 |
| `Toasty` | 官方 Provider + `useKumoToastManager()` | main.jsx 已正确包 `Toasty`（`@cloudflare/kumo/components/toast`）✅ |
| `Combobox` | 值必须来自预置列表时用 Combobox（Autocomplete 是自由文本） | 若存在「只能从列表选」但用 Autocomplete 的场景可迁移（本次未发现明确违规） |
| `PageHeader`（block） | 官方页面头部组合 block | 项目保留本地 `AppPageHeader.jsx` 是已记录的决定，继续保留 |
| `Meter` | 官方无独立 Progress，进度/占比统一用 Meter | 见 3.1.2 |
| `Badge` 红色语义 | 官方推荐语义收敛到 `error`（`destructive` 仍在类型中但不推荐） | 项目大量 `variant="error"`，符合；`teal/orange/blue/purple/neutral/outline` 均为官方合法 token 变体 ✅ |

### 3.4 治理工具盲区与宽泛豁免

1. **`tools/ui-governance-check.mjs:45` 的 `skippedDirs` 含 `public`**：导致 `src/js/components/public/`
   目录（PublicPageIconPicker.jsx、PublicOverviewStats.jsx）从未被治理扫描覆盖，检查器与实际白名单不一致。
   建议改为精确路径或去掉 `public`。
2. **宽泛豁免**：`GitHubPage/PublicGitHubPage` 的 `value.startsWith('#')` 全豁免、TotpPage 的 `#hex` 全豁免覆盖面过大，
   新增 hex 时不易被发现，建议收窄到具体 class/行。
3. **`md` 不是合法 Input size**：现有扫描器不校验 Kumo prop 合法性，GcpPage 的 `size="md"` 漏检。

### 3.5 本地组件评估

| 组件 | 评估 | 结论 |
|------|------|------|
| `components/ui/AppPrimitives.jsx` | AppCard/SectionCard/InsetPanel 是 LayerCard 业务封装；AppTable/DataTableFrame 是语义表格组合层；FieldRow/PageStack/PageToolbar 是布局常量 | 合规保留。EmptyState、getStatusPillClass 建议迁到官方（见 3.1） |
| `components/AnimatedCollapse.jsx` | 基于 Kumo Collapsible 的过渡封装，符合项目规则 | 合规保留 |
| `components/ui/FormCard.jsx` | LayerCard 封装 | 合规保留 |
| `components/AppPageHeader.jsx` | 紧凑过渡组件，官方 PageHeader 是 block 不直接导入 | 已记录决定，保留 |
| `components/SiteFontTimeseriesChart.jsx` | TimeseriesChart 站点字体封装 | 合规保留 |
| `components/GlobalDialogHost.jsx` | 全局 dialog 宿主 | 合规保留 |
| `components/ui/BrandIcon.jsx` / `Icons.jsx` / `CountryFlag.jsx` | 品牌图标业务组件 | 合规保留 |

### 3.6 一致性正面确认（无违规）

- 原生控件白名单外违规 = 0（21 处原生控件全部命中例外）
- 硬编码颜色白名单外违规 = 0（85 处命中全部有真实豁免理由）
- `Table layout="fixed"` 37 处全部带 `<colgroup>` 或语义列
- Textarea/Input 无单行误用；`font-bold` 0 命中；`tracking-*` 0 命中
- 无手写 modal/switch/tabs/dropdown/toast/tooltip（`fixed inset-0`、`w-9 h-5`、`-mb-px` 等模式零匹配）

---

## 4. 修复批次建议（面向后续 Agent 的可执行清单）

按「低风险机械可批量 → 需语义判断」排序。每批完成后跑 `npm run ui:governance && npm run lint && npm test`。

### 批次 1：数据正确性（优先，独立）
- [ ] `PublicStatusPage.jsx:298-299` `||` → `??`，`formatUptimePercent` 区分 undefined 与 0（:90-97）
- [ ] `PublicStatusPage.jsx:526` 0 值不显示 `--`

### 批次 2：资源/生命周期
- [ ] `TotpPage.jsx:1093-1141` 定时器句柄 + 取消标志 + 卸载清理
- [ ] `SubscriptionPage.jsx:1052-1070` cancelled 标志 / AbortController

### 批次 3：Kumo 一致性（机械）
- [ ] GcpPage 13 处 `size="base"/"md"` 统一 `sm`
- [ ] 7 处图标按钮补 `aria-label`（§3.2.2）
- [ ] 5 处 `transition-colors` 清理（§3.2.3）
- [ ] 自绘 badge 7 处 → Kumo `Badge`（§3.1.1）
- [ ] 自绘状态 pill → `StatusBadge`（§3.1.4）
- [ ] `GitHubPage.jsx:1287` `variant="heading3"` → `heading` + `as`

### 批次 4：官方替代（需决策/重构）
- [ ] `EmptyState` → Kumo `Empty`（76 处调用，需分页逐步迁）；AskAiPanel 本地 EmptyState 改名
- [ ] 自绘进度条 4 处 → Meter（分段/多色需手工）
- [ ] `Toolbar size="sm"` 17 处：项目决策（跟随官方去 size / 记录保留）
- [ ] DropdownMenu 链接项 → `LinkItem`

### 批次 5：性能（需测量后再动）
- [ ] `useStore()` 无 selector 的 10 处 → 精确 selector / useShallow
- [ ] `ServerPage.jsx:2968` serverList 回写全局 store 的评估与移除

### 批次 6：工具与文档
- [ ] `ui-governance-check.mjs:45` 去掉 `public` 目录跳过
- [ ] 收窄 GitHubPage/TotpPage 的 #hex 宽泛豁免
- [ ] CONTEXT.md kumo 版本 2.10.0 → 2.13.1
- [ ] `main.jsx` 补 `TooltipProvider`（低优先）

---

## 5. 附录

### 验证命令
```bash
npm run ui:governance   # 设计系统治理（白名单扫描）
npm run lint            # ESLint
npm test                # 单测（含 markdown.test 图片锚点断言）
```

### 相关既有文档
- `docs/Kumo UI 规则.md` —— Kumo-only 硬约束
- `docs/重构验证与例外清单.md` —— 允许的 UI 例外白名单
- `docs/kumo-design-违规审计清单.md` —— 2026-08-31 kumo-design 15 条规则审计（font-bold/border+shadow/transition-colors 等，与本文 3.2 互补，行号可能因改动偏移）
- `docs/reference/kumo-component-registry.md` —— 本机包组件注册表
- `docs/Bug审计与修复交接文档.md` —— 既有 bug 交接记录（新问题补充时同步）
