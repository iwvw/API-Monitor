# 工具箱模块整体重构 PRD

最后更新：2026-06-08

目标读者：另一个 Codex 对话中的实现者。本文档需要作为交接材料使用，因此会同时写清楚产品目标、现状入口、前后端边界、数据迁移、API 草案、测试策略和分阶段执行顺序。

范围：工具箱中的双因子认证、音乐、可用性监测、文件柜、通知、系统设置。它们现在都能看到雏形，但很多能力是草草接上的，前端大多是单文件页面，后端也有不少“router 即业务层”或“JSON 文件即数据库”的临时实现。重构目标不是只修样式，而是把这些模块提升成可长期维护、功能闭环、Kumo 规范一致的产品化模块。

相关基线文档：

- `docs/KUMO_MIGRATION_RULES.md`
- `docs/FRONTEND_BEST_PRACTICES.md`
- `docs/uptime-kuma-aligned-prd.md`

## Problem Statement

工具箱模块目前存在同一类结构性问题：

- 前端页面过大：`TotpPage`、`MusicPage`、`NotificationPage`、`SettingsPage`、`UptimePage` 都是大型单文件组件，业务状态、表单状态、请求逻辑、渲染逻辑、工具函数混在一起。
- 后端边界薄：部分模块直接在 router 中完成第三方请求、Cookie 合并、文件写入、业务判断、错误处理，缺少可测试的 service/repository/domain 层。
- 数据持久化不统一：TOTP 使用 SQLite 但 secret 明文字段；Filebox 使用 JSON metadata；Music 设置表在核心迁移里临时创建；通知和系统设置有表但缺统一配置域和迁移计划。
- 安全边界不足：TOTP secret 可通过 `showSecret=true` 取出；Filebox 公开下载缺访问策略；Music 音频代理存在 SSRF 风险收敛空间；系统数据库导入导出是高危操作但确认、审计、恢复流程不完整。
- 交互不够稳定：很多删除仍是普通 confirm；列表、表格、上传、播放器、扫码、通知历史、日志查看缺少统一 loading/error/empty 状态；移动端压缩布局不够一致。
- Kumo 使用不彻底：已经大量引入 Kumo，但仍有自绘进度条、卡片、拖拽区、复制逻辑、tabs/按钮组合和本地动画样式需要清理。
- 模块间缺事件总线：Uptime、Server、系统日志、数据库、文件柜、音乐登录状态等事件没有统一发送到通知和审计系统。
- 可观测性和测试不足：缺少纯函数状态机、适配器单测、端到端 smoke、数据库迁移测试、失败重试测试。

用户目标：这些模块要重构为真正可用的工具工作台。体验要紧凑、可靠、Kumo-only，后端要可维护、可测试、可迁移，另一个 Codex 可以按 PRD 分阶段实现。

## Solution

重构为一套“工具箱平台层 + 各模块深模块”的架构：

- 工具箱平台层提供统一页面 shell、Kumo 密度规则、API client、错误处理、DeleteResource、审计、设置域、事件总线、后台任务、加密密钥存储、数据迁移工具。
- 每个工具模块内部拆为 repository、service/domain、adapter、router、frontend hooks/components。
- 前端从大页面拆成页面入口、workbench、list/table、detail pane、editor dialog、settings panel、hooks、API client。
- 后端从 router 中移出业务逻辑，router 只做参数解析、鉴权、调用 service、返回统一响应。
- 所有破坏性操作走 Kumo `DeleteResource` 或明确的高危操作确认流程。
- 所有基础 UI 遵守 Kumo：Button 默认 `size="sm"`，同排 Button/Input/Select 高度一致，内部 tabs 使用 `size="sm"`，表格默认不换行，图表使用 Kumo `TimeseriesChart` 和 `Meter`。
- 数据迁移必须兼容当前 SQLite 数据和已有 JSON 文件，并提供可重复执行的 migration。

## Current Code Map

这些是当前实现者需要先看的入口：

- 双因子认证前端：`src/js/pages/TotpPage.jsx`，约 1799 行。
- 双因子认证后端：`modules/totp-api/router.js`、`storage.js`、`models.js`、`schema.sql`、`totp-service.js`。
- 音乐前端：`src/js/pages/MusicPage.jsx`，约 1894 行。
- 音乐后端：`modules/music-api/router.js`，约 851 行，业务逻辑大量在 router 中。
- 可用性监测前端：`src/js/pages/UptimePage.jsx`，约 1164 行。
- 可用性监测后端：`modules/uptime-api/router.js`、`storage.js`、`monitor-service.js`、`schema.sql`。
- 文件柜前端：`src/js/pages/FileboxPage.jsx`，约 833 行。
- 文件柜后端：`modules/filebox-api/router.js`、`service.js`，metadata 当前在 `data/filebox/metadata.json`。
- 通知前端：`src/js/pages/NotificationPage.jsx`，约 1390 行。
- 通知后端：`modules/notification-api/router.js`、`service.js`、`storage.js`、`schema.sql`、`channels/email.js`、`channels/telegram.js`。
- 系统设置前端：`src/js/pages/SettingsPage.jsx`，约 1231 行。
- 系统后端：`src/routes/settings.js`、`src/routes/system.js`、`src/services/userSettings.js`、`src/db/schema.sql`、`src/db/database.js`、`src/db/models/System.js`。

当前值得注意的问题：

- `src/routes/settings.js` 中 `module.exports = router` 出现在文件中段，后面仍继续注册路由，虽然运行上可能可用，但结构非常容易误导维护者。
- `modules/totp-api/schema.sql` 的 `totp_accounts.secret` 是明文字段，需要迁移为加密存储。
- `modules/filebox-api/service.js` 使用同步文件读写 JSON metadata，缺并发安全、分页、审计、原子删除。
- `modules/music-api/router.js` 同时处理 NCM API 代理、Cookie 合并、解锁、音频代理、二维码登录、登录状态，应该拆成多个 service。
- `modules/notification-api/service.js` 已经有队列、批量、重试、熔断、维护、抖动检测，但都在一个类中，难以单测。
- `src/js/pages/FileboxPage.jsx`、`TotpPage.jsx` 仍有手写复制 fallback、`document.execCommand`、本地 history 和业务状态混用。
- `UptimePage` 的列表按 monitor 单独拉 `history/uptime`，列表规模变大时请求碎片化。

## Goals

1. 工具箱模块全部形成清晰的前后端模块边界。
2. 所有工具页面符合 Kumo-only 规范，不新增基础 UI 自绘组件。
3. 每个模块都有可靠的数据模型、迁移策略和安全策略。
4. 每个模块都有可测试的 service/domain 层，不把核心逻辑留在 router 或 React 组件里。
5. 工具箱模块共用设置、审计、通知、后台任务、密钥加密、错误响应、实时事件能力。
6. 用户能在紧凑高密度界面中完成主要工作，不需要在一堆松散卡片和表单中来回找。
7. 移动端核心信息尽量一行或两行显示，详情可以压缩但不能丢关键操作。
8. 文件、密钥、Cookie、数据库导入等高风险能力必须有明确确认、审计和恢复路径。
9. 重构后的代码可以由多个 Codex 对话分模块并行推进。

## Non-goals

- 不在第一阶段一次性增加所有可能的第三方通知渠道。
- 不在第一阶段把音乐模块做成完整流媒体平台，只保证搜索、登录、播放、歌单、歌词、队列、解锁和缓存可靠。
- 不在第一阶段把 Filebox 做成多存储后端网盘，但数据模型要预留 storage driver。
- 不在第一阶段实现多用户权限矩阵，先沿用当前管理员认证，但 API 和数据模型不要堵死多用户。
- 不复制 Uptime Kuma、FileCodeBox、网易云客户端等项目的视觉风格，视觉仍遵守 Kumo。

## Product Principles

- 先稳定后丰富：修复功能闭环、数据可靠性、安全边界，再加高级玩法。
- 前端不藏复杂度：复杂流程用分段 tabs、紧凑表格、详情面板、明确状态，而不是堆长表单。
- 后端用深模块：每个模块有少量稳定接口，内部封装复杂逻辑，便于单测和替换。
- 所有 secret 都要经过加密存储，不在普通 GET 中返回明文。
- 所有删除都使用 `DeleteResource`；导入覆盖、清空日志、恢复数据库等使用高危操作确认。
- 所有列表页支持空态、加载态、错误态、刷新、筛选、搜索和移动端压缩。
- 所有表格默认 `whitespace-nowrap`，长内容用 `truncate`、tooltip、`ClipboardText` 或详情弹窗。
- 图表使用 Kumo Chart，百分比和用量使用 Kumo `Meter`。

## User Stories

1. 作为管理员，我想在工具箱里看到所有工具模块的健康状态，所以我能知道哪些功能可用、哪些配置缺失。
2. 作为管理员，我想工具箱每个页面都有一致的顶部导航、筛选和操作区，所以我不需要重新学习每个模块。
3. 作为管理员，我想所有删除操作都有标准确认，所以我不会误删密钥、文件、通知规则或监测项。
4. 作为管理员，我想所有设置保存后立刻持久化并可恢复，所以刷新或重启后不会丢配置。
5. 作为管理员，我想所有敏感配置加密保存，所以数据库泄露时不会直接暴露 secret、Cookie、SMTP 密码、Telegram token。
6. 作为管理员，我想所有模块的重大操作进入审计日志，所以我能追溯谁改了配置、谁下载了文件、谁导出了密钥。

### 双因子认证

7. 作为管理员，我想安全保存 TOTP/HOTP 账号，所以我可以把它作为可信的 2FA 管理工具。
8. 作为管理员，我想通过摄像头、上传图片、粘贴 otpauth URI、手动录入导入账号，所以不同场景都能快速录入。
9. 作为管理员，我想导入时预览重复项和错误项，所以不会重复导入同一个账号。
10. 作为管理员，我想分组、搜索、排序和批量管理账号，所以大量 2FA 项也能管理。
11. 作为管理员，我想验证码默认隐藏，必要时短暂显示，所以旁观风险更低。
12. 作为管理员，我想复制验证码时有明确反馈，所以不会误以为复制失败。
13. 作为管理员，我想导出加密备份，所以迁移浏览器或设备时不需要明文暴露所有 secret。
14. 作为管理员，我想 HOTP 计数器递增有确认和历史，所以不会误消耗计数。
15. 作为管理员，我想浏览器扩展同步配置可控，所以扩展不会拿到过期或错误的连接信息。

### 音乐

16. 作为用户，我想快速搜索歌曲、歌手、专辑、歌单，所以可以把音乐模块当作可用播放器。
17. 作为用户，我想扫码登录后能稳定获取每日推荐、我的歌单和喜欢列表，所以登录不是摆设。
18. 作为用户，我想播放失败时自动尝试质量降级或解锁源，所以不会频繁遇到不可播放。
19. 作为用户，我想播放器有稳定队列、上一首、下一首、循环、随机、音量和进度控制，所以基础播放体验完整。
20. 作为用户，我想歌词跟随播放滚动，点击歌词可以跳转，所以沉浸播放可用。
21. 作为用户，我想移动端底部播放器紧凑且不遮挡内容，所以手机上也能用。
22. 作为管理员，我想音乐 Cookie 只在后端加密保存，所以前端 localStorage 不保存敏感登录态。
23. 作为管理员，我想音频代理有严格白名单和超时，所以不会变成任意 URL 代理。

### 可用性监测

24. 作为管理员，我想可用性监测对标 Uptime Kuma 的核心体验，所以监测、告警、维护和状态页能形成闭环。
25. 作为管理员，我想新增 HTTP、Keyword、JSON Query、TCP、Ping、DNS、Push 等监测，所以覆盖常见服务。
26. 作为管理员，我想每个监测有详情面板、心跳条、延迟图、SLA、Incident 时间线，所以历史可解释。
27. 作为管理员，我想状态变更经过确认次数和恢复确认，所以短抖动不会刷屏。
28. 作为管理员，我想维护窗口能抑制通知并展示在状态页，所以计划维护不会误报。
29. 作为公开用户，我想访问状态页无需登录，所以可以自助查看服务健康状态。

### 文件柜

30. 作为管理员，我想上传文件或文本并生成取件码，所以可以临时分享内容。
31. 作为取件人，我想输入取件码后看到清晰 metadata，再决定下载或复制文本，所以不会盲目下载。
32. 作为管理员，我想设置过期时间、下载次数、阅后即焚和可选密码，所以分享边界可控。
33. 作为管理员，我想上传进度、速度、剩余时间稳定显示，所以大文件上传不焦虑。
34. 作为管理员，我想取消上传并清理临时文件，所以失败上传不会留下垃圾。
35. 作为管理员，我想服务端历史分页、筛选和删除，所以能管理所有分享。
36. 作为管理员，我想过期文件自动清理并可审计，所以磁盘不会无限增长。

### 通知

37. 作为管理员，我想配置通知渠道、规则、模板、静默、维护和备用渠道，所以告警能可靠送达。
38. 作为管理员，我想通知规则从统一事件目录选择事件，所以不会手动填错事件类型。
39. 作为管理员，我想测试渠道时看到连接过程和错误原因，所以能快速修配置。
40. 作为管理员，我想通知历史能按状态、规则、渠道、时间筛选，所以能排查发送问题。
41. 作为管理员，我想告警聚合、重试和熔断可观测，所以系统不会在故障风暴时失控。
42. 作为维护者，我想通知规则引擎是纯模块，所以条件、模板、抑制、抖动检测都能单测。

### 系统

43. 作为管理员，我想系统设置分成清晰域：外观、模块、安全、数据库、日志、API、关于，所以设置不是一团。
44. 作为管理员，我想修改主题、页面宽度、模块显示后立即保存并同步到云端/数据库，所以刷新后状态一致。
45. 作为管理员，我想安全修改密码和 2FA 登录设置，所以退出和刷新不会绕过登录态。
46. 作为管理员，我想数据库导入前自动备份、验证 schema、展示风险确认，所以不会误导入坏库。
47. 作为管理员，我想日志查看、下载、清理、限制策略清晰，所以日志不会撑爆数据库或文件。
48. 作为管理员，我想看到宿主机性能与进程指标，所以判断当前实例是否健康。

## Cross-module Architecture Requirements

### Backend Layering

每个模块最终应遵守以下层次：

1. Router
   - 只负责鉴权、参数解析、调用 service、统一响应。
   - 不直接读写文件、数据库、第三方 API、Cookie 合并或业务状态。
2. Service
   - 编排业务流程，调用 repository、domain、adapter、job、event bus。
   - 对外暴露稳定接口。
3. Repository
   - 封装 SQLite 读写、分页、事务、迁移兼容。
   - 不做复杂业务判断。
4. Domain
   - 放纯逻辑：状态机、规则匹配、OTP URI 解析、文件生命周期、模板渲染、播放器数据归一化。
   - 必须能用 Vitest 单测。
5. Adapter
   - 第三方服务或协议封装：NCM API、Unblock、SMTP、Telegram、HTTP probe、DNS probe、storage driver。
6. Jobs
   - 后台清理、重试、聚合、状态检查、过期文件清理、rollup。
7. Events
   - 所有模块重要事件发布到统一事件总线，供通知、审计、实时 UI 消费。

### Shared Services

建议新增或强化这些平台能力：

- `SecureSecretStore`
  - 加密保存 TOTP secret、Music Cookie、SMTP 密码、Telegram token、Push token。
  - 支持 `encryptJson`、`decryptJson`、`maskSecret`、`rotateKey` 预留。
- `AuditService`
  - 标准记录：actor、module、action、resourceType、resourceId、summary、metadata、ip、userAgent、traceId。
  - 删除、导入、导出、下载、密钥 reveal、数据库恢复必须写审计。
- `ToolboxEventBus`
  - 标准事件：`module.resource.created`、`module.resource.updated`、`module.resource.deleted`、`module.job.failed`、`module.security.revealed`、`uptime.monitor.down` 等。
  - 通知模块从 event bus 消费，不再依赖各模块散落调用。
- `JobScheduler`
  - 支持 named job、interval、jitter、lock、last_run、next_run、error、manual run。
  - 用于 Uptime check、Filebox cleanup、Notification retry、log cleanup、music cache cleanup。
- `ApiResponse`
  - 统一 `{ success, data, error, code, requestId }`。
  - 第三方 API 原始返回需要 adapter 归一化，不把不可控结构直接透给前端。
- `SettingsRegistry`
  - 模块注册自己的 setting domain、默认值、schema、脱敏策略。
  - 系统设置页按 registry 渲染，不再把所有设置硬编码在单页面里。

### Frontend Layering

每个页面拆分为：

- `Page`：只负责页面壳和 tab 状态。
- `Workbench`：组合列表、详情、工具栏、弹窗。
- `ApiClient`：封装 fetch/axios、鉴权 headers、错误标准化。
- `hooks`：列表缓存、详情缓存、表单状态、实时事件。
- `components`：业务组件，基础 UI 仍必须来自 Kumo。
- `dialogs`：编辑、删除、导入、导出、高危确认。
- `utils`：纯格式化、数据归一化。

建议组件命名示例：

- `TotpWorkbench`、`TotpAccountList`、`TotpAccountEditor`、`TotpImportDialog`。
- `MusicWorkbench`、`MusicPlayerProvider`、`MusicQueuePanel`、`MusicLoginDialog`。
- `FileboxWorkbench`、`FileDropZone`、`ShareResultPanel`、`FileboxHistoryTable`。
- `NotificationWorkbench`、`ChannelEditorDialog`、`RuleBuilderDialog`、`DeliveryHistoryTable`。
- `SystemSettingsWorkbench`、`SettingsSection`、`DangerZonePanel`、`DatabaseMaintenancePanel`。

## Module Requirements

### 1. 双因子认证

#### Product Scope

MVP 必须支持：

- TOTP/HOTP 账号 CRUD。
- 分组 CRUD、排序、按分组筛选。
- 搜索 issuer/account。
- 当前验证码、剩余时间、复制、短暂 reveal。
- 摄像头扫码、上传二维码、粘贴 URI、手动录入。
- 批量导入 otpauth URI。
- 加密导出备份。
- HOTP 计数器递增和验证。
- 浏览器扩展下载/配置同步保留，但要安全化。

#### Backend

- `secret` 迁移为加密字段，例如 `secret_encrypted`，保留旧字段迁移后清空或废弃。
- `GET /accounts` 永远不返回 secret。
- `GET /accounts/:id?showSecret=true` 要废弃，替换为 `POST /accounts/:id/reveal-secret`，要求管理员当前密码或短期 reauth token，并写审计。
- 生成 codes 可以由后端执行，但要加 rate limit，避免扩展或页面高频刷。
- `generateAllCodes` 不应修改全局 `authenticator.options` 造成跨账号参数污染；改为每次使用局部 options。
- 导入要返回 preview：有效、重复、冲突、错误，而不是直接导入。
- 导出支持两类：明文 otpauth URI 需要高危确认；加密备份为默认。
- 扩展 ZIP 压缩不要在请求中拼 PowerShell 字符串；改为启动时或构建时生成，或使用 Node zip 库。
- 所有账号/分组修改写审计。

#### Data Model

建议表：

- `totp_accounts`
  - `id`、`otp_type`、`issuer`、`account`、`secret_encrypted`、`algorithm`、`digits`、`period`、`counter`、`group_id`、`icon`、`color`、`sort_order`、`created_at`、`updated_at`、`last_used_at`。
- `totp_groups`
  - 保留现有字段，新增 `updated_at`。
- `totp_audit_events`
  - 可并入全局 `operation_logs`，但需要记录 reveal/export/increment。
- `totp_import_batches`
  - 可选，用于记录批量导入结果。

#### API Contract

- `GET /api/totp/summary`
- `GET /api/totp/accounts?q=&groupId=&type=`
- `POST /api/totp/accounts`
- `GET /api/totp/accounts/:id`
- `PUT /api/totp/accounts/:id`
- `DELETE /api/totp/accounts/:id`
- `POST /api/totp/accounts/:id/code`
- `POST /api/totp/accounts/:id/increment`
- `POST /api/totp/accounts/:id/reveal-secret`
- `POST /api/totp/import/preview`
- `POST /api/totp/import/commit`
- `POST /api/totp/export`
- `GET /api/totp/groups`
- `POST /api/totp/groups`
- `PUT /api/totp/groups/:id`
- `DELETE /api/totp/groups/:id`
- `PUT /api/totp/order`
- `GET /api/totp/extension/package`
- `POST /api/totp/extension/sync-token`

#### Frontend

- 主视图：左侧分组/筛选，右侧账号网格或表格，窄屏转为单列。
- 账号卡片必须固定高度，验证码、剩余时间、平台、账号对齐。
- 验证码默认隐藏时，卡片仍保持尺寸稳定。
- 导入流程使用 Dialog + Tabs：扫码、上传、URI、手动。
- 删除账号/分组用 `DeleteResource`。
- 复制验证码/URI/扩展地址用 `ClipboardText` 或 Kumo tooltip button，不写手工 `execCommand` fallback。
- 高级设置移到系统设置中的 TOTP domain，页面只保留当前工作流必要设置。

### 2. 音乐

#### Product Scope

MVP 必须支持：

- 搜索歌曲、歌单、歌手。
- 热门歌单、歌单详情、我的歌单、每日推荐。
- 扫码登录、登录状态、退出。
- 播放、暂停、上一首、下一首、进度、音量、循环、随机。
- 歌词和翻译歌词。
- 播放失败降级：原始 URL、质量降级、解锁源、错误提示。
- 队列管理：追加、替换、移除、清空、当前播放。
- 小播放器和全屏播放器。

#### Backend

- 拆出 `MusicAuthService`：Cookie 加密保存、合并、登录状态、退出。
- 拆出 `NcmClient`：封装 `@neteasecloudmusicapienhanced/api`，统一错误和超时。
- 拆出 `MusicUnblockService`：解锁源、超时、失败原因、源可用性。
- 拆出 `AudioProxyService`：Range 转发、白名单、DNS/IP 校验、超时、最大重定向、响应头过滤。
- 拆出 `MusicCatalogService`：搜索、歌单、歌词、推荐、数据归一化和短期缓存。
- Cookie 不返回给前端；前端只拿登录用户摘要。
- 音频 URL 不长期缓存敏感参数；可对代理 URL 做短期 token。
- `allowedDomains.some(host.includes(domain))` 需要收紧为后缀匹配，并防止 `evilkuwo.cn` 这种绕过。
- 二维码轮询要有过期状态和取消机制。

#### Data Model

建议表：

- `music_settings`
  - `key`、`value_encrypted`、`updated_at`，保留旧 `value` 迁移。
- `music_cache`
  - `cache_key`、`payload_json`、`expires_at`、`created_at`。
- `music_play_history`
  - `song_id`、`song_json`、`played_at`、`duration`、`source`。
- `music_favorites`
  - 可选，用于本地收藏。
- `music_proxy_tokens`
  - 可选，短期代理 token。

#### API Contract

- `GET /api/music/health`
- `GET /api/music/auth/status`
- `POST /api/music/auth/logout`
- `POST /api/music/auth/qr/start`
- `GET /api/music/auth/qr/:key/status`
- `GET /api/music/search?q=&type=&limit=&offset=`
- `GET /api/music/suggest?q=`
- `GET /api/music/song/:id`
- `GET /api/music/song/:id/url?quality=`
- `GET /api/music/song/:id/lyrics`
- `POST /api/music/song/:id/unblock`
- `GET /api/music/playlists/hot`
- `GET /api/music/playlists/:id`
- `GET /api/music/me/playlists`
- `GET /api/music/me/recommend/songs`
- `GET /api/music/audio/proxy/:token`

#### Frontend

- 抽出 `useMusicPlayer` 或 `MusicPlayerProvider`，集中管理 Audio 实例和事件监听。
- Audio 实例生命周期必须有 cleanup，避免页面多次进入后重复监听。
- 搜索和歌单数据由 hooks 管理，避免直接在页面里散落 fetch。
- 队列独立组件，支持右键或行操作。
- 播放器底部固定时，页面根容器只留真实遮挡空间。
- 表格列不换行，歌曲名/专辑/歌手 truncate。
- 移动端两行内显示歌曲、歌手、时长、播放按钮。
- 登录 Dialog 显示二维码、状态、刷新、取消；轮询在关闭时停止。

### 3. 可用性监测

可用性监测已有单独 PRD：`docs/uptime-kuma-aligned-prd.md`。本总 PRD 只列纳入工具箱整体重构时必须对齐的要点。

MVP 要求：

- 监测工作台、聚合列表 API、详情面板。
- HTTP(s)、Keyword、JSON Query、TCP、Ping、DNS、Push adapters。
- SQLite 状态持久化，移除 JSON state 文件依赖。
- 状态机纯模块，支持 down/up 确认、retry interval、resend interval、maintenance、paused。
- 通知通过统一事件总线进入通知模块。
- 状态页和维护窗口作为 P1/P2，但数据模型第一阶段预留。
- 前端图表用 Kumo `TimeseriesChart`，loading 使用 Kumo chart loading，tooltipBoundary 设置正确。

与工具箱整体的关系：

- Uptime 的事件类型必须注册到 `ToolboxEventBus`。
- Uptime 的维护窗口应与通知模块维护计划统一数据模型或有桥接层。
- Uptime 页面应使用和其他工具页一致的 module tabs、toolbar、DeleteResource、table density。

### 4. 文件柜

#### Product Scope

MVP 必须支持：

- 文本分享和文件分享。
- 取件码获取 metadata。
- 下载文件或复制文本。
- 过期时间、阅后即焚、下载次数限制。
- 上传进度、速度、剩余时间、取消。
- 服务端历史：分页、搜索、筛选、删除、刷新。
- 分享结果：取件码、下载链接、二维码、复制。
- 过期清理任务。

P1：

- 可选访问密码。
- 可选最大文件大小和允许 MIME 类型设置。
- 下载限速或单 IP 限制。
- 文件预览：图片、文本、PDF metadata。
- 存储 driver 抽象：local filesystem 首先实现。

#### Backend

- metadata 迁移到 SQLite，文件仍可留本地磁盘。
- 添加事务：创建分享时先写 pending，文件保存成功后标记 active；失败时清理临时文件。
- 阅后即焚和下载次数限制必须原子处理，不能并发下载绕过。
- 清理任务删除过期记录和物理文件，记录删除结果。
- 公开取件和下载 API 返回脱敏数据，不泄露服务器路径。
- 下载应支持 Range，便于大文件和浏览器恢复。
- 上传限制在后端强制执行，不只依赖前端 100MB。
- 每次 retrieve/download/delete 写审计。

#### Data Model

建议表：

- `filebox_shares`
  - `id`、`code`、`type`、`status`、`original_name`、`stored_name`、`storage_driver`、`storage_path`、`mime_type`、`size`、`text_encrypted`、`created_at`、`expires_at`、`burn_after_reading`、`max_downloads`、`download_count`、`password_hash`、`created_by`、`last_accessed_at`。
- `filebox_access_logs`
  - `share_id`、`code`、`action`、`ip`、`user_agent`、`success`、`error`、`created_at`。
- `filebox_settings`
  - `max_file_size`、`allowed_mime_types`、`default_expiry_hours`、`public_upload_enabled`。

#### API Contract

- `POST /api/filebox/shares`
- `GET /api/filebox/shares?status=&q=&limit=&cursor=`
- `GET /api/filebox/shares/:id`
- `DELETE /api/filebox/shares/:id`
- `GET /api/filebox/public/:code`
- `POST /api/filebox/public/:code/verify`
- `GET /api/filebox/public/:code/download`
- `POST /api/filebox/jobs/cleanup`
- `GET /api/filebox/settings`
- `PUT /api/filebox/settings`

兼容层：

- 保留 `/api/filebox/retrieve/:code` 和 `/api/filebox/download/:code`，内部转到新 service，避免旧链接立即失效。

#### Frontend

- 分享、取件、历史三个 tabs 保留，但压缩布局。
- 上传区可以是业务组件，但基础控件使用 Kumo，边框和颜色用 Kumo token。
- 上传进度使用 Kumo `Meter`，不要自绘百分比条。
- 结果复制使用 `ClipboardText`。
- 服务端历史使用 Kumo `Table`，不换行，支持分页。
- 删除使用 `DeleteResource`。
- 本地历史不作为真数据源，只作为最近复制/访问的便捷列表；必须和服务端历史明确区分。

### 5. 通知

#### Product Scope

MVP 必须支持：

- Email 和 Telegram 渠道 CRUD。
- 渠道连接测试。
- 规则 CRUD。
- 事件源选择：Uptime、Server、System、Filebox、Music、TOTP。
- 条件、静默、重复阈值、时间窗口、备用渠道。
- 模板标题和内容。
- 通知历史：状态、渠道、规则、错误、重试次数。
- 全局配置：重试、批量、限流、base URL。

P1：

- 更多渠道 adapter：Webhook、Discord、Slack、Bark、Server 酱等。
- 通知模板变量预览。
- 规则 dry-run。
- 告警分组和升级策略。
- 维护窗口统一 UI。

#### Backend

- 拆出纯 `RuleEngine`：条件、时间窗口、静默、重复阈值、抖动检测。
- 拆出 `TemplateRenderer`：变量替换、缺失变量提示、预览。
- 拆出 `DeliveryQueue`：持久化 pending/retrying、并发、重试、熔断、批量。
- 拆出 `ChannelRegistry`：Email、Telegram 是 adapter，不写死在 service 里。
- `checkRateLimit()` 当前返回 false 但调用方仍继续发送，需要重构为明确阻断/降级行为。
- 批量通知当前合并后复用第一条 log_id，历史表达不清，应生成 batch log 并关联子记录。
- 配置解密逻辑不能靠 `startsWith('u2f')` 猜测；需要字段版本或统一加密封装。
- 维护计划从通知模块孤立表升级为共享 maintenance domain，至少能被 Uptime 和 Server 使用。

#### Data Model

保留并演进：

- `notification_channels`
  - 新增 `config_encrypted`、`config_version`、`last_test_status`、`last_test_at`。
- `alert_rules`
  - 新增 `event_schema_version`、`condition_json`、`template_json`、`route_json`。
- `notification_history`
  - 新增 `event_id`、`batch_id`、`duration_ms`、`provider_message_id`。
- `alert_state_tracking`
  - 保留，用纯 RuleEngine 管理。
- `notification_global_config`
  - 拆分或规范 JSON 配置。
- `maintenance_windows`
  - 建议迁为全局模块表，供 notification/uptime/server 共用。

#### API Contract

- `GET /api/notification/summary`
- `GET /api/notification/events/catalog`
- `GET /api/notification/channels`
- `POST /api/notification/channels`
- `PUT /api/notification/channels/:id`
- `DELETE /api/notification/channels/:id`
- `POST /api/notification/channels/:id/test`
- `GET /api/notification/rules`
- `POST /api/notification/rules`
- `PUT /api/notification/rules/:id`
- `DELETE /api/notification/rules/:id`
- `POST /api/notification/rules/:id/dry-run`
- `POST /api/notification/rules/:id/enable`
- `POST /api/notification/rules/:id/disable`
- `GET /api/notification/history?status=&channelId=&ruleId=&q=&limit=&cursor=`
- `POST /api/notification/history/:id/retry`
- `DELETE /api/notification/history`
- `GET /api/notification/config`
- `PUT /api/notification/config`

#### Frontend

- 主 tabs：渠道、规则、历史、维护、设置。
- 渠道用表格或紧凑卡片，但字段对齐，不用松散大卡。
- 规则编辑器分段：事件、条件、路由、模板、抑制。
- 条件和事件类型不让用户手填字符串，从事件 catalog 选择。
- 模板编辑提供变量列表和预览。
- 历史用 Kumo `Table`，状态 badge、错误 tooltip、重试按钮。
- 删除渠道/规则用 `DeleteResource`。
- 清空历史是高危操作确认，不用 DeleteResource 也要明确二次确认。

### 6. 系统

#### Product Scope

MVP 必须支持：

- 外观：主题、页面宽度、导航样式。
- 模块：显示隐藏、排序。
- 安全：密码修改、2FA 登录开关、会话退出。
- 数据库：统计、分析、备份、导入、压缩、清理。
- 日志：系统日志、操作日志、原始日志、保留策略、清理。
- API：全局 API key/proxy/system instruction、模块配置入口。
- 关于：版本、依赖、运行环境。
- 宿主机指标：CPU、内存、磁盘、进程、运行时间。

#### Backend

- `settings.js` 拆域：
  - `settings-router`
  - `database-maintenance-router`
  - `log-router`
  - `security-settings-router`
  - `appearance-settings-router`
- `userSettings` 改为 schema-driven settings registry，避免字段不断在多个地方手写映射。
- 数据库导入流程：
  - 上传到 temp。
  - 校验 SQLite。
  - 校验必要表和 schema version。
  - 备份当前 DB。
  - 切换维护锁。
  - 替换文件。
  - 重新初始化。
  - 失败时自动尝试恢复备份。
- 数据库导出不应在下载回调里直接删除临时文件而无兜底任务；应有 temp cleanup job。
- 日志读取应分页/游标，不每次读整文件。
- 高危操作全部写审计。
- `system/host-metrics` 可以保留，但指标 service 要抽出，方便 Dashboard 复用。

#### Data Model

建议：

- `system_config`
  - 保留 key/value，但 value 支持 `type`、`is_secret`、`schema`、`updated_by`。
- `user_settings`
  - 短期保留单例，但新增迁移必须在 schema、model、database migration 三处一致。
- `settings_domains`
  - 可选，记录模块设置域版本。
- `system_jobs`
  - 记录 backup/import/log cleanup 等任务。
- `operation_logs`
  - 增强审计字段。
- `login_attempts`
  - 如果已有在 model 中动态创建，纳入 schema/migration 正式管理。

#### API Contract

- `GET /api/settings`
- `PATCH /api/settings/:domain`
- `GET /api/settings/domains`
- `GET /api/settings/database/stats`
- `GET /api/settings/database/analysis`
- `POST /api/settings/database/backup`
- `POST /api/settings/database/import/preview`
- `POST /api/settings/database/import/commit`
- `POST /api/settings/database/vacuum`
- `GET /api/settings/logs/system?cursor=&limit=`
- `GET /api/settings/logs/operation?cursor=&limit=`
- `PUT /api/settings/logs/policy`
- `POST /api/settings/logs/cleanup`
- `GET /api/system/host-metrics`
- `GET /api/system/health`

#### Frontend

- 设置页用官方/项目规范 tabs，内部 tabs 使用 `size="sm"`。
- 每个设置域都是独立 section，不在一个组件里写所有表单。
- 保存按钮统一小号，和输入框同高。
- 高危操作放 Danger Zone，使用明显但不过度的 Kumo destructive 样式。
- 数据库表格不换行，右侧操作对齐。
- 日志 viewer 使用虚拟化或分页，不在页面一次渲染大量文本。
- 移动端每个设置行最多两行：标题/说明一列，控件一列，窄屏纵向但对齐。

## Frontend Global Requirements

- 不新增基础 UI 自绘组件。
- 不使用原生 `<button>`、`<select>`、`<input>`、`<textarea>` 来模拟 Kumo 组件。
- 所有 icon-only Button 必须 `shape="square"` 或 `shape="circle"`，并有 `aria-label`。
- 同排 Button/Input/Select 高度一致。
- 表格内容默认不换行。
- 页面主容器不要额外撑宽或制造右侧空白。
- 图表和 Meter 使用 Kumo 标准。
- 空态、错误态、loading 态每个模块必须有。
- 动画克制，优先 Kumo 自带行为；展开收起不能卡顿。
- 移动端核心信息一行或两行，展开详情也要压缩。

## Backend Global Requirements

- 所有模块 API 统一鉴权，不再在每个页面手写 `localStorage admin_password` 的细节；前端 API client 负责。
- 所有模块 router 统一错误处理。
- 所有模块参数用 zod 或同等 schema 校验。
- 所有模块 migration 可重复执行。
- 所有 JSON 字段读写集中处理，不在页面和 router 反复 `JSON.parse`。
- 所有 secret 加密，所有公开 API 脱敏。
- 所有后台任务可观察：lastRun、nextRun、status、error。
- 所有高危操作写审计。

## Implementation Decisions

- 保留现有路由路径作为兼容层，但新实现内部走 service。
- 第一阶段不追求拆目录完美，先把核心逻辑从页面和 router 抽出。
- Filebox metadata 必须从 JSON 迁移到 SQLite。
- TOTP secret 必须加密迁移。
- Music Cookie 必须只在后端加密保存，前端只存非敏感用户摘要。
- Notification 是全局通知中心，不让 Uptime/Server 等模块各自实现通知策略。
- System settings 是全局设置中心，但模块设置由各模块注册 domain。
- Uptime 的细化实现以 `docs/uptime-kuma-aligned-prd.md` 为准。

## Testing Decisions

测试优先覆盖外部行为，不测试私有实现细节。

必须补的单测：

- TOTP
  - URI parse/generate。
  - TOTP/HOTP code generation 不受账号参数互相污染。
  - secret 加密迁移。
  - import preview duplicate detection。
- Music
  - Cookie merge。
  - URL allowlist。
  - audio proxy header filtering。
  - song URL fallback decision。
  - lyric parse。
- Filebox
  - code generation uniqueness。
  - expiry。
  - burn-after-reading atomic behavior。
  - download count limit。
  - metadata migration from JSON。
- Notification
  - RuleEngine condition/time/suppression。
  - TemplateRenderer。
  - Retry/backoff/rate-limit。
  - batch history behavior。
  - maintenance suppression。
- System
  - settings normalization。
  - migration adds missing columns。
  - database import preview rejects invalid DB。
  - log policy normalization。
- Uptime
  - 见 `docs/uptime-kuma-aligned-prd.md`。

集成测试：

- API CRUD smoke for each module。
- Auth required endpoints reject missing password/session。
- Public Filebox retrieve/download works without exposing path。
- Notification channel test can be mocked。
- Music NCM client can be mocked。

前端 smoke：

- `/totp` 不白屏，能打开导入 dialog。
- `/music` 不白屏，能搜索，能打开登录 dialog。
- `/uptime` 不白屏，列表空态和新增 dialog 可用。
- `/filebox` 不白屏，分享/取件/history tabs 可用。
- `/notification` 不白屏，渠道/规则/history/settings tabs 可用。
- `/settings` 不白屏，所有主 tabs 可切换。
- 桌面和窄屏各测一次。

验证命令：

- `npm run lint`
- `npm run build`
- `npm run test`
- Kumo-only 静态扫描见 `docs/KUMO_MIGRATION_RULES.md`。

## Migration Plan

### Phase 0: Foundation

- 建立 shared API client。
- 建立统一 error response 和 zod validation 约定。
- 建立 `SecureSecretStore`。
- 建立 `AuditService`。
- 建立 `ToolboxEventBus`。
- 建立 `JobScheduler` 最小版本。
- 写迁移测试骨架。

### Phase 1: Security and Persistence

- TOTP secret 加密迁移。
- Filebox metadata JSON 迁移 SQLite。
- Music Cookie 字段统一加密。
- Notification config 加密版本化。
- System settings migration 对齐 schema/model/database。

### Phase 2: Backend Service Extraction

- 拆 TOTP service/repository/domain。
- 拆 Filebox repository/service/storage driver。
- 拆 Music auth/client/catalog/proxy/unblock。
- 拆 Notification rule/queue/channel/template。
- 拆 Settings 域路由和 service。
- Uptime 按单独 PRD 先拆 state machine/repository/adapter registry。

### Phase 3: Frontend Workbench Refactor

- 每个页面拆 API client、hooks、业务组件。
- 所有删除迁移到 `DeleteResource`。
- 表格和列表统一不换行、紧凑布局。
- 复制文本迁移到 `ClipboardText`。
- 进度条迁移到 `Meter`。
- 移动端布局压缩。

### Phase 4: Product Completion

- TOTP 加密导出、import preview。
- Filebox 访问密码、download limit、cleanup job UI。
- Music 队列管理、登录状态、错误恢复、歌词体验。
- Notification event catalog、dry-run、模板预览。
- System 数据库 import preview/commit。
- Uptime 状态页和维护窗口。

### Phase 5: Observability and Polish

- 每个后台 job 可观察。
- 每个模块 summary 卡片。
- 审计日志串联所有模块。
- 端到端 smoke。
- 文档更新。

## Acceptance Criteria

整体：

- 所有模块主要功能能正常使用，不只是页面可打开。
- `npm run lint`、`npm run build`、相关测试通过。
- 所有基础 UI 符合 Kumo-only 静态扫描。
- 所有高危操作有确认和审计。
- 所有 secret 不再明文通过普通 API 返回。
- 所有服务端数据有 SQLite 持久化或明确缓存策略。
- 所有页面移动端无明显溢出、重叠、右侧空白。

双因子认证：

- 旧账号无损迁移，secret 加密保存。
- 导入、扫码、手动录入、复制、HOTP 递增可用。
- 导出默认加密，高危明文导出需确认。

音乐：

- 登录、搜索、播放、歌单、歌词、队列可用。
- 播放失败有 fallback 和明确提示。
- 后端 Cookie 加密，代理安全收敛。

可用性监测：

- 监测工作台和核心 adapters 可用。
- 状态机、心跳、Incident、通知联动稳定。
- 详见单独 Uptime PRD。

文件柜：

- 文件/文本分享、取件、下载、历史、删除、过期清理可用。
- metadata 在 SQLite。
- 公开 API 不泄露路径。

通知：

- 渠道、规则、历史、重试、批量、熔断、维护策略可用且可观测。
- 事件 catalog 与其他模块联动。

系统：

- 设置保存持久化。
- 数据库备份/导入/压缩/分析安全可用。
- 日志查看/清理/保留策略可用。
- 登录安全退出后刷新不保持登录状态。

## Risks

- TOTP secret 迁移是最高风险，必须先备份数据库并有回滚路径。
- Filebox JSON 迁移要处理文件缺失、metadata 损坏、重复 code、过期记录。
- Music 依赖第三方 API 和解锁服务，必须把不可用状态变成可解释错误。
- Notification 当前功能多但集中，拆分时容易改变行为，需要先补测试。
- System 数据库导入属于高危操作，必须分 preview 和 commit。
- Uptime 重构范围大，应按已有单项 PRD 分阶段推进。

## Open Questions

1. Filebox 是否允许匿名上传？当前代码分享接口需要 auth，但注释里曾提到 FileCodeBox 式匿名上传。建议 MVP 默认只允许管理员上传，公开取件。
2. TOTP 是否需要多设备同步？当前只做本地数据库。建议先做加密导出导入。
3. Music 是否需要本地收藏和播放历史长期保存？建议 P1。
4. Notification 是否优先增加 Webhook 渠道？建议 P1，因为可以覆盖很多第三方。
5. System 设置是否需要多 profile？建议不进 MVP。

## Handoff Notes For Next Codex

推荐接手顺序：

1. 先做 Phase 0 shared foundation，尤其 `SecureSecretStore`、`AuditService`、API client。
2. 再做 TOTP secret 加密迁移和 Filebox SQLite 迁移，因为这是数据安全和可靠性的底座。
3. 然后拆 Notification rule engine，因为 Uptime、Server、System 后续都要依赖它。
4. Music 可以并行做，重点是先拆后端 service 和前端 audio provider。
5. Uptime 按 `docs/uptime-kuma-aligned-prd.md` 单独推进，但事件和通知接口要和本 PRD 对齐。

不要一上来只改样式。这个重构的核心是：数据可靠、安全边界、业务闭环、Kumo 一致性。
