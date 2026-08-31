# kumo-design 规范违规审计清单

生成日期：2026-08-31
范围：`src/js` 全部 186 个源文件（*.jsx / *.js）
对照基线：kumo-design 15 条规则 + `docs/重构验证与例外清单.md`（现有 31 条例外**均不豁免**以下违规）

> 说明：本清单为只读审计结果，未修改任何代码。所有违规均不在例外清单覆盖范围内。

---

## 汇总

| 规则 | 违规数 | 严重度 | 可批量迁移 |
|------|--------|--------|-----------|
| font-weight（禁用 font-bold） | 212 | 低 | ✅ 机械 |
| shadow-borders（border+shadow） | 25 | 低 | ✅ 机械 |
| hover-color-transitions（transition-colors） | 32（+13 transition-all） | 低 | ✅ 机械 |
| content-text-size（正文 16px+） | 约 8 | 中 | ⚠️ 需语义判断 |
| font-tracking（tracking-*） | 约 12 | 低 | ⚠️ 需语义判断 |
| layer-card-nesting（LayerCard 嵌套） | 2 | 中 | ⚠️ 需重构 |
| heading-case（uppercase 标题） | 0 | - | - |
| dialog-rendering（条件渲染 Dialog） | 0 | - | - |
| collapse-content-size / sticky-borders | 0 | - | - |
| concentric-border-radius | 待视觉抽查 | - | - |

**总计：约 290+ 处**

---

## 一、font-weight（禁用 font-bold）— 212 处

规则：标题用 `font-semibold`，行内加粗用 `font-medium`，禁用 `font-bold`。

### 分文件命中

| 文件 | 命中数 | 行号 |
|------|-------|------|
| `pages/ServerPage.jsx` | 64 | 398, 458, 532, 591, 618, 654, 660, 689, 695, 751, 760, 804, 836, 891, 988, 5964, 6053, 6077, 6102, 6519, 7856, 8091, 8095, 8495, 8503, 8575, 8832, 9068, 9162, 9237, 9325, 9363, 9367, 9371, 9375, 9401, 9511, 9701, 9831, 9946, 10102, 10648, 10663, 10807, 10880, 11023, 11217, 11249, 11263, 11374, 11384, 11418, 11424, 11436, 11454, 11628, 11778, 11802, 11921, 11943, 11954, 11958, 12061, 12138 |
| `pages/PaasPage.jsx` | 32 | 2972, 2980, 2991, 3035, 3043, 3054, 3098, 3106, 3111, 3115, 3121, 3125, 3130, 3140, 3166, 3220, 3226, 3230, 3252, 3303, 3351, 3358, 3362, 3373, 3378, 3382, 3388, 3402, 3421, 3442, 3472, 3581 |
| `pages/ApiDocsPage.jsx` | 14 | 744, 772, 959, 1030, 1079, 1275, 1286, 1358, 1382, 1484, 1487, 1709, 1882, 2095 |
| `pages/PublicServerStatusPage.jsx` | 10 | 275, 355, 364, 384, 411, 421, 463, 473, 681, 729 |
| `pages/SubscriptionPage.jsx` | 9 | 2115, 2185, 2275, 2470, 2728, 2775, 2788, 2799, 2848 |
| `pages/NotificationPage.jsx` | 9 | 961, 1088, 1091, 1168, 1329, 1506, 1745, 1925, 1926 |
| `pages/UptimePage.jsx` | 8 | 232, 257, 1495, 1535, 1541, 1762, 2075, 2183 |
| `pages/PublicStatusPage.jsx` | 8 | 374, 397, 410, 433, 467, 473, 523, 527 |
| `pages/DashboardPage.jsx` | 8 | 267, 371, 1179, 1199, 1219, 1239, 1259, 1279 |
| `pages/PublicGitHubPage.jsx` | 7 | 1547, 1655, 1660, 1725, 2143, 2178, 2188 |
| `pages/SettingsPage.jsx` | 5 | 1201, 1389, 1413, 1908, 2221 |
| `pages/TotpPage.jsx` | 4 | 2042, 2081, 2687, 2758 |
| `components/server/SftpPanel.jsx` | 4 | 410, 433, 446, 459 |
| `pages/SchedulerPage.jsx` | 4 | 1614, 1826, 2069, 2131 |
| `pages/GitHubPage.jsx` | 4 | 1659, 2482, 2611, 2735 |
| `pages/OraclePage.jsx` | 3 | 1419, 1552, 1674 |
| `components/MainLayout.jsx` | 2 | 124, 703 |
| `components/server/QuickCommandBar.jsx` | 2 | 419, 464 |
| `pages/AuthPage.jsx` | 2 | 317, 343 |
| `pages/DnsPage.jsx` | 2 | 2106, 2417 |
| `modules/flowUnits.js` | 1 | 1 |
| `pages/VoidRoomPage.jsx` | 1 | 741 |
| `components/ui/AppPrimitives.jsx` | 1 | 23 |
| `components/github/GitHubPublicPagesPanel.jsx` | 1 | 374 |
| `pages/PublicSubscriptionInfoPage.jsx` | 1 | 87 |
| `pages/PublicSharePage.jsx` | 1 | 154 |
| `pages/FileboxPage.jsx` | 1 | 835 |
| `pages/PublicM365RegisterPage.jsx` | 1 | 315 |
| `pages/AdminAIPage.jsx` | 1 | 12 |
| `components/adminai/MessageList.jsx` | 1 | 236 |
| `components/adminai/AskAiPanel.jsx` | 1 | 1448 |

### 迁移规则
- **标题类 → `font-semibold`**：`<h1>/<h2>/<h3>/<h4>`、`Dialog.Title`、`text-base/text-lg` 区块标题
- **行内加粗/数值/标签类 → `font-medium`**：`<label>`、统计数值 span/div、badge、小字标签
- 低风险、机械替换，无业务逻辑影响

---

## 二、shadow-borders（border+shadow）— 25 处

规则：禁用「边框+投影」组合，用 `ring ring-kumo-line` 代替。`shadow-none` 显式禁用阴影不算违规。

| 文件 | 行号 | 内容 |
|------|------|------|
| `pages/GitHubPage.jsx` | 1283 | spotlighted 分支 `border-brand/60 shadow-lg shadow-brand/10 ring-2 ring-brand/20` |
| `pages/GitHubPage.jsx` | 1518 | `border border-kumo-line ... shadow-sm` |
| `pages/GitHubPage.jsx` | 1639 | `border border-kumo-line ... shadow-sm` |
| `pages/PublicGitHubPage.jsx` | 1157 | spotlighted 分支 `border-brand/60 shadow-lg ...` |
| `pages/PublicGitHubPage.jsx` | 1392 | `border border-kumo-line ... shadow-sm` |
| `pages/PublicGitHubPage.jsx` | 1513 | `border border-kumo-line ... shadow-sm` |
| `pages/PublicGitHubPage.jsx` | 1532 | `border border-kumo-line ... shadow-sm` |
| `pages/PublicGitHubPage.jsx` | 1754-1756 | `border ... shadow-[0_0_0_1px_rgba(...)]`（选中态） |
| `pages/PublicGitHubPage.jsx` | 1798 | `border ... lg:hover:shadow-lg` |
| `pages/PublicGitHubPage.jsx` | 1805 | `border border-brand/30 ... shadow-sm` |
| `pages/DnsPage.jsx` | 2684 | 选中态 `border-brand/70 ... shadow-sm` |
| `pages/SchedulerPage.jsx` | 710 | `border ... shadow-sm` |
| `pages/SchedulerPage.jsx` | 813 | `border border-kumo-line ... shadow-sm` |
| `pages/NotificationPage.jsx` | 948 | `hover:border-brand/50 hover:shadow-sm` |
| `pages/NotificationPage.jsx` | 1070 | `hover:border-brand/50 hover:shadow-sm` |
| `pages/NotificationPage.jsx` | 1315 | `hover:border-brand/40 hover:shadow-sm` |
| `pages/ServerPage.jsx` | 10644 | `border-l border-kumo-line ... shadow-[-12px_0_24px_-24px_rgba(...)]` |
| `components/forward/ForwardCanvas.jsx` | 484 | `border-kumo-brand ... shadow-md` |
| `components/forward/ForwardCanvas.jsx` | 485 | `border-kumo-line ... shadow-sm` |
| `components/forward/ForwardCanvas.jsx` | 565 | `border ... shadow-md ring-1 ring-kumo-brand/10` |
| `components/forward/ForwardCanvas.jsx` | 680 | `border ... shadow-lg` |
| `components/adminai/AskAiPanel.jsx` | 102 | `border ... hover:shadow-[0_0_12px_-2px_var(--color-kumo-shadow-drop)]` |
| `components/adminai/AskAiPanel.jsx` | 1437 | `border-[3px] border-brand/80 ... shadow-2xl` |
| `components/public/PublicPageIconPicker.jsx` | 151 | `border border-kumo-line ... shadow-sm` |
| `components/server/ServerLocationMap.jsx` | 247 | `border border-kumo-line ... shadow-sm` |

> 注：`ServerLocationMap.jsx:154` 地图数据点内联 `box-shadow` 等效 ring 描边，不计入。

---

## 三、hover-color-transitions（transition-colors）— 32 处（核心）

规则：hover 颜色变化必须即时，禁止 `transition-colors` 颜色过渡。

### 核心 32 处

| 文件 | 行号 | 内容 |
|------|------|------|
| `ApiDocsPage.jsx` | 1432 | 选择卡片 `transition-colors` + `hover:bg-kumo-recessed/50` |
| `ApiDocsPage.jsx` | 1961 | Button `transition-colors`（variant 颜色切换） |
| `DashboardPage.jsx` | 80 | `SERVICE_TOOL_ITEM_CLASS` `transition-colors hover:border-brand/60 hover:bg-kumo-base` |
| `DashboardPage.jsx` | 384 | `transition-colors group-hover:text-kumo-strong` |
| `DashboardPage.jsx` | 1179 | `transition-colors group-hover:text-brand` |
| `DashboardPage.jsx` | 1199 | `transition-colors group-hover:text-brand` |
| `DashboardPage.jsx` | 1219 | `transition-colors group-hover:text-brand` |
| `DashboardPage.jsx` | 1239 | `transition-colors group-hover:text-brand` |
| `DashboardPage.jsx` | 1259 | `transition-colors group-hover:text-brand` |
| `DashboardPage.jsx` | 1279 | `transition-colors group-hover:text-brand` |
| `SettingsPage.jsx` | 2141 | `transition-colors hover:border-brand/50` |
| `SettingsPage.jsx` | 2150 | `transition-colors hover:border-brand/50` |
| `SettingsPage.jsx` | 2171 | `transition-colors hover:border-brand/50` |
| `SettingsPage.jsx` | 2183 | `transition-colors hover:border-brand/50` |
| `SettingsPage.jsx` | 2192 | `transition-colors hover:border-brand/50` |
| `SettingsPage.jsx` | 2201 | `transition-colors hover:border-brand/50` |
| `SettingsPage.jsx` | 2212 | `transition-colors hover:text-brand hover:underline` |
| `SchedulerPage.jsx` | 710 | `transition-colors` + `hover:border-brand/50` |
| `UptimePage.jsx` | 1471 | `transition-colors hover:bg-kumo-recessed/25` |
| `TotpPage.jsx` | 1631 | `transition-colors hover:border-brand` |
| `AskAiPanel.jsx` | 105 | `transition-colors duration-200 group-hover:bg-brand/10` |
| `AskAiPanel.jsx` | 106 | `transition-colors duration-200 group-hover:text-brand` |
| `AskAiPanel.jsx` | 109 | `transition-colors group-hover:text-kumo-default` |
| `AskAiPanel.jsx` | 165 | `transition-colors` + `group-hover:text-kumo-default` |
| `AskAiPanel.jsx` | 424 | `transition-colors` + `hover:bg-kumo-tint hover:text-kumo-default` |
| `AdminConsole.jsx` | 305 | 选择卡片 `transition-colors` + `hover:bg-kumo-recessed/50` |
| `MainLayout.jsx` | 896 | `transition-colors hover:text-kumo-strong` |
| `MainLayout.jsx` | 905 | `transition-colors hover:text-kumo-strong` |
| `PublicPageIconPicker.jsx` | 121 | `transition-colors` + `hover:bg-kumo-recessed/35` |
| `PublicPageIconPicker.jsx` | 138 | `transition-colors` + `hover:bg-kumo-recessed/35` |
| `PublicPageIconPicker.jsx` | 287 | `transition-colors hover:border-brand/45` |
| `AppPrimitives.jsx` | 318 | `transition-colors hover:border-brand/60` |

### 附带：`transition` / `transition-all` / `transition-[...]` 中的 hover 颜色过渡（13 处，未计入核心）

| 文件 | 行号 | 内容 |
|------|------|------|
| `DnsPage.jsx` | 2684 | `transition` + `hover:border-brand/50 hover:bg-kumo-recessed/40` |
| `DrawioPage.jsx` | 482 | `transition` + `hover:bg-kumo-recessed/20` |
| `DrawioPage.jsx` | 747 | `transition` + `hover:border-brand/35 hover:bg-kumo-recessed/40` |
| `DrawioPage.jsx` | 758 | `transition` + `hover:border-brand/35 hover:bg-kumo-recessed/40` |
| `NotificationPage.jsx` | 948 | `transition-all duration-200 hover:border-brand/50` |
| `NotificationPage.jsx` | 1070 | `transition-all duration-200 hover:border-brand/50` |
| `NotificationPage.jsx` | 1315 | `transition-all duration-200 hover:border-brand/40` |
| `M365Page.jsx` | 1900 | `transition` + `hover:border-brand/40 hover:bg-kumo-base` |
| `M365Page.jsx` | 2020 | `transition group-hover:border-brand/50 group-hover:text-brand` |
| `ServerPage.jsx` | 8475 | `transition-all` + `hover:border-kumo-interact` |
| `MessageList.jsx` | 732 | `transition-all` + `hover:bg-kumo-tint hover:text-kumo-default` |
| `AskAiPanel.jsx` | 102 | `transition-all` + `hover:border-brand/40 hover:bg-kumo-base` |
| `PublicGitHubPage.jsx` | 1798 | `transition-[...,background-color]` + `hover:bg-kumo-base/50` |

> 未计为违规：selected/active 状态切换、focus 态、非颜色过渡（transform/opacity/width/height）。

---

## 四、content-text-size（正文 16px+）— 约 8 处

规则：正文/数据必须 14px，16px+ 仅限标题。以下均为统计数值/数据（非标题）：

| 文件 | 行号 | 内容 |
|------|------|------|
| `ApiDocsPage.jsx` | 744 | 摘要卡 `{value}` 数据，`text-base …cq-sm:text-lg` |
| `OpenAIPage.jsx` | 2074, 2126, 2182, 2310, 2331, 2356 | 网关请求统计数值，`text-lg…cq-xl:text-2xl` |
| `ServerPage.jsx` | 11802 | 升级批量数量 `{…items?.length}`，`text-lg` |
| `SchedulerPage.jsx` | 1614 | 汇总统计 `{value}`，`text-base…cq-sm:text-lg` |
| `OraclePage.jsx` | 1931 | 成本金额 `{formatCurrency}`，`text-2xl`（边界：醒目统计数字） |
| `TotpPage.jsx` | 1697 | OTP 码值，`text-[16px]…cq-sm:text-[20px]` |
| `AuthPage.jsx` | 742 | OTP 输入框，`text-lg`（边界：安全码输入） |

> 合法：`AskAiPanel.jsx:88`(h3)、`AdminAIPage.jsx:12`(h1)、`BackupPage.jsx:228`(h1)、`PublicGitHubPage.jsx:2178`(h1)、`PublicServerStatusPage.jsx:729`(h1)、`PublicPromptPage.jsx:32`(h1)、`MainLayout.jsx:378`(品牌 wordmark)、`ServerPage.jsx:7969`(图标)、`TotpPage.jsx:98`(品牌 mark 图标)。

---

## 五、font-tracking（tracking-*）— 约 12 处

规则：禁用 `tracking-*` 改变字符间距。绝大多数用于小字号 uppercase 标签字距优化：

| 文件 | 行号 | 内容 |
|------|------|------|
| `AuthPage.jsx` | 742 | `tracking-widest`（OTP 码输入，最突出） |
| `SettingsPage.jsx` | 1757, 1761 | `tracking-wider`（小标签） |
| `PublicGitHubPage.jsx` | 1546 | `tracking-[0.08em]`（小标签） |
| `UptimePage.jsx` | 232 | `tracking-wider`（小标签） |
| `SubscriptionPage.jsx` | 2728, 2775, 2788, 2799 | `tracking-wide`（小节标题） |
| `AskAiPanel.jsx` | 1501, 1768 | `tracking-wide`（分组标签） |
| `DocumentOutline.jsx` | 25 | `tracking-wider`（大纲标题） |

> `TotpPage.jsx:1697` 的 `tracking-normal` 为默认值，不算违规。

---

## 六、layer-card-nesting（LayerCard 嵌套）— 2 处

规则：禁止 `<LayerCard>` 嵌套 `<LayerCard>`。（`LayerCard.Secondary/Primary` 是子部件，正常。）

| 文件 | 行号 | 说明 |
|------|------|------|
| `pages/GitHubPage.jsx` | 2415 / 2457 / 2469 | 外层 LayerCard → Primary → 内层 LayerCard（仓库卡片，map 内） |
| `pages/GitHubPage.jsx` | 2778 / 2793 / 2809 | 外层 LayerCard → Primary → 内层 LayerCard（Token 卡片，map 内） |

---

## 七、合规项（无违规）

- **heading-case**：所有 `uppercase` 均为小字号标签/badge，非标题 ✅
- **dialog-rendering**：全部使用 `open` prop，无条件渲染 ✅
- **collapse-content-size / sticky-borders**：AnimatedCollapse 保持内容尺寸；sticky 均有 border-b ✅
- **icon-alignment / related-text-spacing / text-spacing / inline-monospace-size**：未见明显问题 ✅

---

## 八、待视觉抽查

- **concentric-border-radius**：纯静态难以判定同心圆角，建议浏览器抽查 `FormCard`（rounded-xl/内层 rounded-lg 图标）、`SectionCard`。

---

## 修复优先级建议

1. **font-bold（212 处）**：机械、低风险、可批量，优先。
2. **border+shadow（25 处）**：机械替换为 ring，低风险。
3. **transition-colors（32+13 处）**：移除颜色过渡类，低风险。
4. **LayerCard 嵌套（2 处）**：需重构，中风险。
5. **content-text-size（约 8 处）**：需语义判断（统计大数字是否保留视觉强调）。

> 注意：ServerPage、DnsPage、OpenAIPage 等为 CONTEXT.md 标记的单所有者高风险文件，大面积改动前需遵守多 Agent 协作约定（文件持有登记、分支、串行审计）。
