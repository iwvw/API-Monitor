# UI 重构进度追踪

## 2026-06-06 SettingsPage 后端接口对接

- `src/js/pages/SettingsPage.jsx` 已按旧前端设置页和后端接口重写：
  - 对接 `/api/settings` 完整读写：模块显隐/顺序、渠道开关、模型前缀、负载均衡、主机地址显示、振动、导航布局、TOTP 偏好、PaaS 刷新间隔、Public API URL、Agent 下载 URL、自定义 CSS。
  - 对接 `/api/gemini-cli/settings` 与 `/api/qwen/settings`：API_KEY、PROXY、SYSTEM_INSTRUCTION 的全局同步。
  - 对接 `/api/auth/change-password` 和 `/api/auth/2fa/*`。
  - 对接数据库维护接口：导出、导入、统计、分析、VACUUM、清理日志、清理聊天消息。
  - 对接日志接口：日志保留设置、系统日志、审计日志、app.log 读取/清空、立即执行日志限制。
- `src/js/store.js` 新增 `normalizeUserSettings`、`loadUserSettings`、`applyUserSettings`，前端导航使用后端模块顺序和可见性；渠道和模块均按白名单过滤，不恢复已删除模块。
- `src/js/components/MainLayout.jsx` 已在登录后加载用户设置，并按模块设置过滤侧栏；系统设置入口固定保留。
- `src/js/pages/PaasPage.jsx` 与 `src/js/pages/TotpPage.jsx` 的局部设置保存改为 `PATCH /api/settings`，避免局部保存覆盖完整用户设置。
- `SettingsPage.jsx` 静态扫描已确认无原生 `<button>/<select>/<input>/<textarea>`，表格使用 Kumo `Table`。

> 后续执行计划见 `docs/refactor-next.md`。Kumo-only 硬性规则见 `docs/KUMO_MIGRATION_RULES.md`。验收记录见 `docs/refactor-verification.md`。
> 最新约束：不新增自写 UI 组件，已有本地 UI 包装组件也要逐步替换为直接使用 Kumo。

## 目标
将所有模块从旧版 Vue 3 + FontAwesome + 原生 CSS 方案，完整迁移至 **React 19 + @cloudflare/kumo v2.5.0 + Tailwind CSS v4**，视觉风格对齐 Cloudflare Dashboard。

## 技术规范

### 设计 Token（Kumo 变量）
| 用途 | Token |
|------|-------|
| 页面背景 | `bg-kumo-canvas` |
| 卡片/面板背景 | `bg-kumo-base` |
| 凹陷区域背景 | `bg-kumo-recessed` |
| 表单控件背景 | `bg-kumo-control` |
| 主要文字 | `text-kumo-strong` |
| 普通文字 | `text-kumo-default` |
| 次要文字 | `text-kumo-subtle` |
| 边框 | `border-kumo-line` |
| 品牌色 | `text-kumo-brand` / `bg-kumo-brand` |
| 成功 | `text-kumo-success` / `bg-kumo-success/10` |
| 危险 | `text-kumo-danger` / `bg-kumo-danger/10` |
| 警告 | `text-kumo-warning` / `bg-kumo-warning/10` |
| 信息 | `text-kumo-info` / `bg-kumo-info-tint` |

### 组件导入规范
```js
// 推荐（tree-shaking）
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
// 或
import { Button, Input } from "@cloudflare/kumo";
```

### 页面文件路径规范
```
src/js/pages/<ModuleName>Page.jsx
```

### 主要交互模式
- 列表页：顶部操作栏（筛选 + 搜索 + 主操作按钮）+ 卡片/表格主体
- 弹窗：使用 `useState` 控制本地显示状态，并使用 Kumo `Dialog`
- 状态 badge：`text-xs font-semibold px-2 py-0.5 rounded border`
- 数据卡片：`bg-kumo-base border border-kumo-line rounded-lg p-5 shadow-sm`
- 操作按钮组：`flex items-center gap-2`

### Toast 通知
```js
import { toast } from '../modules/toast.js';
toast.success('操作成功');
toast.error('操作失败');
toast.warning('注意...');
toast.info('提示...');
```

---

## 模块重构状态

### 已完成 ✅
| 模块 | 文件 | 完成日期 | 说明 |
|------|------|----------|------|
| MainLayout | `components/MainLayout.jsx` | 2026-06-05 | 侧边栏 + 顶栏，支持折叠动画 |
| AuthPage | `pages/AuthPage.jsx` | 2026-06-05 | 登录页 |
| DashboardPage | `pages/DashboardPage.jsx` | 2026-06-05 | 统计卡片 + API 趋势图 + 服务列表 |
| ServerPage | `pages/ServerPage.jsx` | 2026-06-05 | 主机实例 + 历史趋势 + Docker + SSH 终端 |

### Wave 1 — 工具类模块
| 模块 | 文件 | 状态 | 说明 |
|------|------|------|------|
| TOTP (2FA) | `pages/TotpPage.jsx` | ✅ 完成 | 验证码列表 + 分组 + 设置 |
| Filebox (文件柜) | `pages/FileboxPage.jsx` | ✅ 完成 | 文件列表 + 上传 + 分享链接 |
| Uptime (监测) | pages/UptimePage.jsx | ✅ 完成 | 监测列表 + 添加 + 统计报表 |
| Notification (通知) | pages/NotificationPage.jsx | ✅ 完成 | 渠道 + 告警规则 + 历史 + 配置 |

### Wave 2 — API 网关
| 模块 | 文件 | 状态 | 说明 |
|------|------|------|------|
| OpenAI | `pages/OpenAIPage.jsx` | ✅ 完成 | API Key 管理 + 对话测试 |
| GeminiCLI | `pages/GeminiCliPage.jsx` | ✅ 完成 | API 代理配置 + 日志统计 |
| Qwen (通义) | `pages/QwenPage.jsx` | ✅ 完成 | 通义千问 API 代理 |
| Antigravity | `pages/AntigravityPage.jsx` | ✅ 完成 | Antigravity 代理服务 |

### Wave 3 — 基础设施
| 模块 | 文件 | 状态 | 说明 |
|------|------|------|------|
| PaaS | `pages/PaasPage.jsx` | ✅ 完成 | Koyeb + Fly.io 应用监控 |
| Cloudflare DNS | `pages/DnsPage.jsx` | ⬜ 半完成 内部详细功能未完成 | DNS 区域 + Records + Workers + Pages |
| 阿里云 | `pages/AliyunPage.jsx` | ✅ 完成 | DNS + ECS 管理 |
| 腾讯云 | `pages/TencentPage.jsx` | ✅ 完成 | DNS + CVM 管理 |

### Wave 4 — 特殊模块
| 模块 | 文件 | 状态 | 说明 |
|------|------|------|------|
| Self-H (自建服务) | `pages/SelfHPage.jsx` | ✅ 完成 | 自建服务状态面板，已集成 |
| Music | `pages/MusicPage.jsx` | ✅ 完成 | 网易云音乐播放器，支持 Autocomplete 搜索建议 |

---

## MainLayout 路由注册

每完成一个模块，需在 `MainLayout.jsx` 的 `renderActivePage()` switch 中添加对应 case：

```jsx
case 'totp':     return <TotpPage />;
case 'filebox':  return <FileboxPage />;
case 'self-h':   return <SelfHPage />;
// ...
```

---

## 2026-06-05 Runtime Verification Notes

- Fixed NotificationPage runtime blank screen caused by JSX evaluating template placeholders such as `{{variable}}`.
- Fixed ServerPage render-phase store update warning by syncing `serverList` to Zustand from an effect.
- Added URL/history synchronization in `MainLayout.jsx`: direct paths such as `/server`, `/notification`, `/openai`, `/gemini-cli`, and `/qwen` now select the matching module, and sidebar navigation pushes browser history.
- Restored ServerPage add-host Agent path in the add host modal. The Agent tab calls `POST /api/server/agent/quick-install` and shows Linux/Windows install commands using Kumo `Tabs`, `Input`, `Select`, `Textarea`, and `Button`.
- Fixed API gateway blank screens:
  - `OpenAIPage.jsx`: replaced missing `Settings` icon reference with `SettingsIcon`.
  - `GeminiCliPage.jsx`: added missing `Cpu` icon import.
  - `QwenPage.jsx`: added missing `RefreshCw` icon import.
- Added missing `Clock` icon to `src/js/components/Icons.jsx` to resolve runtime errors in `SelfHPage`.
- Integrated `SelfHPage` into `MainLayout`.
- Fixed Kumo `Checkbox` accessibility warnings across all pages by providing `label` or `aria-label`.
- Browser verification passed for all integrated routes with zero console errors.
- **Module Removal**: 独立 `ai-chat` 模块已在当前工作区移除，包含 React 入口、后端模块、数据库 schema 和路由；该变更仍需独立提交。旧 Vue 参考文件中的 OpenAI chat 命名不属于独立 `ai-chat` 模块。
- **Autocomplete Integration**: 在 `MusicPage` 搜索栏和 `OpenAIPage` 模型选择器中集成了官方 `Autocomplete` 组件。支持异步建议获取和分组显示（如已收藏/所有模型）。

## 2026-06-05 Kumo Component Compliance Notes

- Replaced the custom DOM/CSS toast manager with Kumo `Toasty` and `createKumoToastManager`; existing `toast.success/error/warning/info` calls remain compatible.
- Updated Gemini CLI and Qwen Model Matrix controls to use Kumo `Table` root and Kumo `Checkbox`; verified checkbox centers align with their table cell centers.
- Normalized ServerPage top action buttons (`probe all` and `add host`) and sub-tab buttons to a fixed 32px height to prevent squeezed button rendering and small header shifts when switching tabs.
- Moved theme switching out of the sidebar footer into Settings > Appearance, using Kumo `Select` with explicit `auto`, `light`, and `dark` modes.
- **Global Spacing**: Applied balanced symmetric padding (`p-4 lg:p-8`) in `MainLayout` to ensure consistent top and bottom gaps across all modules. 已为 `MusicPage` 增加 `pb-32`，其余页面维持 `pb-20` 以确保滚动到底部时不贴边。
- Refactored `MainLayout` sidebar against Kumo Sidebar Basic/Usage docs: `Sidebar.Provider`, `Sidebar.Header`, `Sidebar.Content`, `Sidebar.Group`, `Sidebar.GroupLabel`, `Sidebar.Menu`, `Sidebar.MenuButton`, `Sidebar.Footer`, and `Sidebar.Trigger`. Removed custom collapsible group navigation, `Sidebar.MenuSub`, `Sidebar.Rail`, `resizable`, and `peekable` after layout regressions.

## Git 提交记录

| Hash | 模块 | 日期 |
|------|------|------|
| ef995ec | React 模块运行时白屏与路由历史修复 | 2026-06-05 |
| d8ecd4f | Wave 2 路由与图标集成 | 2026-06-05 |
| bc623b2 | AntigravityPage 页面实现 | 2026-06-05 |
| eca17ab | QwenPage 页面实现 | 2026-06-05 |
| d2c223c | GeminiCliPage 页面实现 | 2026-06-05 |
| 5306ffa | OpenAIPage 页面实现 | 2026-06-05 |
| 1ae0849 | MainLayout 侧边栏动画修复 | 2026-06-05 |
| 16eea3a | TotpPage 页面实现 | 2026-06-05 |
| 9be6579 | TotpPage 刷新与跳动修复 | 2026-06-05 |
| 113addb | FileboxPage 页面实现 | 2026-06-05 |
| 2831052 | 核心基础及 Auth, Dashboard, Server 迁移 | 2026-06-05 |
| 1f6c2cb | 侧边栏折叠修复及 2FA 紧凑化调整 | 2026-06-05 |
| 885886a | 侧边栏折叠按钮固定（左侧对齐）调整 | 2026-06-05 |
| d3d7233 | UptimePage & NotificationPage 页面实现 | 2026-06-05 |

---

## 接手须知（给下一位助手）

1. **进度文件**：本文件记录已知事实；后续执行计划以 `docs/refactor-next.md` 为准，Kumo-only 规则以 `docs/KUMO_MIGRATION_RULES.md` 为准
2. **旧模板**：`src/templates/*.html` + `src/css/*.css` + `src/js/modules/*.js` 是旧实现，重构时参考业务逻辑但不复制代码风格
3. **MainLayout 路由**：`src/js/components/MainLayout.jsx` 的 `renderActivePage()` 方法，需要为每个完成的模块注册 case
4. **API 端点**：所有后端接口不变，路径均为 `/api/<module>/...`，可在旧 `src/js/modules/*.js` 中查阅
5. **Token 规范**：严格使用 kumo token，禁止硬编码颜色值
6. **Git 提交**：每完成一个模块做一次 commit，并更新本文件的"已完成"表格和"Git 提交记录"

## 实时问题记录

每修复一个问题，在后面标注修复结果
由助手自行决定修复顺序和修复方法，无需询问用户

1. 全站按钮尺寸略大，可能造成布局高度异常，切换时发生位移 ✅ 已修复
2. 缺少设置页面和进入按钮 ✅ 已修复
3. Toast尺寸过大，需要优化 ✅ 已修复
4. 组件间空隙有点大，可更紧凑些 ✅ 已修复
5. 开关按钮样式仍旧为自绘，同时扫描全局自绘组件，需要更改为kumo组件 ✅ 已修复
    - 已核实 OpenAI、Gemini CLI、Qwen、Antigravity、TOTP、Filebox、Notification 等页面均已切换为 Kumo `Switch` / `Checkbox`
6. 许多表格样式的内容宽度不合理，要加入拖动柄来自定义 部分完成
    - 已有拖动柄：OpenAI、Gemini CLI、Qwen、Server、Aliyun、Tencent、Self-H、Cloudflare
    - Cloudflare 表格拖动已在浏览器验证，首列可从 `260px` 拖到 `320px`
    - 仍需复核或补齐：Music、PaaS、TOTP
7. 所有页面底部没有留空隙，导致元素底部贴边 ✅ 已修复
    - `MainLayout` 主内容区域统一 `p-4 lg:p-8`，顶部与底部间距一致
    - 普通页面根容器已移除额外 `pb-20`，避免底部大于顶部；Music 保留播放器避让留白
8. 主机实例管理功能缺失，没有完全对接原有后台管理的后端，主机列表和后台管理要分开，和原前端逻辑保持一致 ✅ 已修复
    - 主机实例的组件要和其它页面保持一致，例如 flex flex-wrap items-center justify-between border-b border-kumo-line pb-3 gap-4
9. 所有页面加入骨架屏 ✅ 已修复
    - Music、Server、OpenAI 等数据加载较慢的页面均已集成 `SkeletonLine`
10. 阿里云和腾讯云的图标是一样的 ✅ 已修复
11. 系统控制台再优化一下，紧凑点 ✅ 已修复
12. 多个 agent 分头修改导致前端样式风格不统一：按钮高度、间距、表格、弹窗、toast、switch/checkbox 等需要按 Kumo 官方组件和统一密度规范收敛
    - 主题切换已收敛到设置页，侧栏不再保留独立切换按钮；设置页使用 Kumo `Select`，支持跟随系统/浅色/深色
    - API 网关子标签必须直接使用 Kumo `Tabs`，不再使用本地 `ModuleTabs` 包装组件
    - 侧栏已按 Kumo Sidebar 官方 Basic 结构重构：`GroupLabel` 只作为组标题，模块入口全部使用同级 `Sidebar.MenuButton`，移除重复组标题、二级子菜单和边缘 `Sidebar.Rail`
    - AntiG / Antigravity 前端入口已移除；GCLI 保留，GCLI 内部 OAuth 的 `Google Antigravity` 提示按依赖事实保留
13. 不同模块页面宽度不统一，且无法调节 ✅ 已修复
    - `MainLayout` 已接管统一页面宽度，支持 `标准 / 宽屏 / 全宽` 三档 Kumo `Tabs` 切换并持久化到 localStorage
    - 已移除主要页面根容器的局部 `max-w-7xl/max-w-4xl mx-auto` 限制，避免模块自行锁死宽度
14. 所有标签页需要统一为 Kumo `Tabs` 写法 ✅ 已修复基础规范
    - 已统一 `Tabs` 从 `@cloudflare/kumo` 顶层导入
    - 已清除 `TabsList` / `TabsTrigger` 旧结构，统一使用 `variant="segmented"` + `tabs={[...]}` 数组写法
    - 受控标签页继续使用 `value/onValueChange`；Kumo `selectedValue` 仅作为 uncontrolled 初始值使用

## 2026-06-06 Phase 0 Baseline

- Git 索引已收回为未暂存状态，避免 `MM/AM/AD` 混合状态导致误提交。
- 已删除未跟踪临时文件：`fix-switches*.js`、`old_diff*.txt`、`tmp_index.html`、`tmp_openai_fix.js`。
- 已删除未被引用的本地 UI 包装组件 `src/js/components/ModuleTabs.jsx`，后续直接使用 Kumo `Tabs`。
- React 构建入口为 `src/index.html` 中的 `/js/main.jsx`；旧 `src/js/main.js` 未被当前 Vite 入口引用，暂作为旧实现参考处理。
- 当前主页面文件存在：Dashboard、Server、TOTP、Filebox、Uptime、Notification、OpenAI、Gemini CLI、Qwen、PaaS、DNS、Aliyun、Tencent、Settings、Self-H、Music。
- Cloudflare DNS 仍是优先补完模块。

## 2026-06-06 Kumo-only 收敛记录

- 已核准 `E:\Code\kumo` / `@cloudflare/kumo` v2.5.0 的实际组件导出：`Button`、`Input`、`Select`、`Tabs`、`Table.ResizeHandle`、`Dialog`、`Checkbox`、`Switch`、`Toasty`、`Autocomplete`、`Sidebar` 均可用。
- Kumo `Select.hideLabel` 已被上游标记 deprecated；后续隐藏标签统一使用 `aria-label`。
- 全站 Kumo `Tabs` 基础规范已统一：
  - 从 `@cloudflare/kumo` 顶层导入 `Tabs`
  - 默认使用 `variant="segmented"` 和 `tabs` 数组声明标签
  - 需要同步 React/Zustand 状态的页面使用 `value/onValueChange`，不使用只作初始值的 `selectedValue`
- 全站页面宽度基础规范已统一：
  - `MainLayout` 提供 `标准 / 宽屏 / 全宽` 三档页面宽度
  - 宽度偏好写入 `app_page_width_mode`
  - 模块页面根容器不再自行设置 `max-w-7xl/max-w-4xl mx-auto`
- `DnsPage.jsx` 已完成第一轮 Kumo-only 控件收敛：
  - 顶部原生 tab button 改为 Kumo `Tabs`
  - 账号选择原生 `select` 改为 Kumo `Select`
  - 账号弹窗原生 `input` 改为 Kumo `Input`
  - 账号操作原生 icon button 改为 Kumo `Button`
  - Zones、Tunnels、Accounts 表格接入 Kumo `Table.ResizeHandle`
- `AuthPage.jsx`、`DashboardPage.jsx`、`SettingsPage.jsx` 已完成 Kumo-only 控件收敛：
  - 登录/首次设置密码表单改用 Kumo `Input` 和 `Button`
  - 控制台刷新动作改用 Kumo `Button`
  - 设置页自绘 tab button 改用 Kumo `Tabs`
  - 设置页密码输入改用 Kumo `Input`
  - 上述 3 个页面的 `<button>`、`<select>`、`<input>`、`<textarea>` 静态扫描已清零
- `AliyunPage.jsx`、`TencentPage.jsx` 已完成 Kumo-only 控件收敛：
  - 顶部自绘 tab button 改为 Kumo `Tabs`
  - 云账号选择原生 `select` 改为 Kumo `Select`
  - 实例操作与账号操作图标按钮改为 Kumo `Button`
  - 账号弹窗输入改为 Kumo `Input` / `Textarea`
  - 上述 2 个页面的 `<button>`、`<select>`、`<input>`、`<textarea>` 静态扫描已清零
- `UptimePage.jsx` 已完成 Kumo-only 控件收敛：
  - 顶部导航、监测类型选择改为 Kumo `Tabs`
  - 搜索和新增/编辑表单输入改为 Kumo `Input`
  - 状态过滤、刷新动作改为 Kumo `Button`
  - 该页面 `<button>`、`<select>`、`<input>`、`<textarea>` 静态扫描已清零
- `FileboxPage.jsx` 已完成 Kumo-only 控件收敛：
  - 顶部导航和分享类型切换改为 Kumo `Tabs`
  - 取件码、隐藏文件选择输入改为 Kumo `Input`
  - 分享文本改为 Kumo `Textarea`
  - 有效期选择改为 Kumo `Select`
  - 历史操作与上传取消动作改为 Kumo `Button`
  - 该页面 JSX `<button>`、`<select>`、`<input>`、`<textarea>` 静态扫描已清零
- `SelfHPage.jsx` 已完成 Kumo-only 控件收敛：
  - 顶部子导航、文件布局切换和定时任务类型切换改为 Kumo `Tabs`
  - OpenList 实例选择、定时任务周期/周几/每月日期选择改为 Kumo `Select`
  - OpenList 实例表单、图片预览尺寸、定时任务名称/cron/时间输入改为 Kumo `Input`
  - 定时任务命令/URL 改为 Kumo `Textarea`
  - 文件面包屑、README 关闭、定时任务操作、右键菜单动作改为 Kumo `Button`
  - 该页面 JSX `<button>`、`<select>`、`<input>`、`<textarea>` 和 `appearance-none` 静态扫描已清零

## 2026-06-06 Cloudflare / Dialog / 间距 / 中文化收尾

- `DnsPage.jsx` 已扩展为 Cloudflare 综合管理页，覆盖账号、域名与 DNS、Workers、Pages、R2、Tunnel、DNS 模板等后端接口入口。
- Cloudflare 表格按 Kumo `Table` 标准实现，并接入 `Table.ResizeHandle`；浏览器验证首列表格列宽可从 `260px` 拖到 `320px`。
- `useTableResize.js` 已支持 mouse/touch 拖动，拖动期间设置 `col-resize` 和禁用文本选择，结束后清理监听。
- React 页面中的 `Dialog.Close asChild` 已迁移为 Kumo `Dialog.Close render={(props) => ...}` 写法。
- `DnsPage.jsx` 通用 Dialog 改为仅在 `modal.type` 存在时挂载，避免关闭后残留不可见但可接收指针事件的空 Dialog 节点。
- `MainLayout` 主内容内层从 `h-full` 改为 `min-h-full`；普通页面移除根容器额外 `pb-20`，Settings 浏览器测得顶部/底部间距同为 `30px`。
- 可见中文化已补一轮：导航、总览卡片说明、设置页公网 API 字段、PaaS/通知/OpenAI/GCLI/Qwen 等可见令牌和参数文案已改中文。
- AntiG / Antigravity 模块入口已从 React 导航和页面中移除；GCLI 依赖的 requester 与 OAuth 文案继续保留。
