# 资产管理模块 PRD

最后更新：2026-09-22

状态：P0 已实现（登记册 + 实体/虚拟视图 + 总览 + 到期派生 + 成本分组；P1 纳管与告警待实现）

## Problem Statement

API Monitor 目前管理着大量可被视作「资产」的对象：主机与服务器实例、云厂商账号与云资源、域名、SSL 证书、订阅与套餐、许可证、模型网关 API Key、集中访问密钥、托管代理节点、TOTP 账号等。但这些对象全部按业务模块分散存放，各自有各自的列表、字段口径与到期展示方式，缺少一个统一的资产视角。

这带来几个具体问题：

1. 用户无法在一个地方看到「我到底有哪些资产、哪些快到期、每年花多少钱」。
2. 到期字段口径不统一：`server_accounts.expires_at` 是 DATETIME，`subscription_subscriptions.expire_at` 是 TEXT，`uptime_monitor_states.ssl_expiry` 又是另一种语义，跨模块汇总必须先归一。
3. 到期判断逻辑重复且分散：`ServerPage.jsx` 在前端算剩余天数，`subscription/summary.go` 在后端算 expired，`uptime/ssl.go` 又单独算 `daysLeft`。
4. 面板管不到的资产（网络设备、存储、终端、线下服务器、SaaS 账号）完全没有登记入口。
5. 成本信息仅零星存在（`openai_gateway_stats_hourly.cost`、华为云费用概览），没有统一口径。
6. 到期告警能力存在但未覆盖资产维度：`notification` 模块已支持 `ssl_expiry` 等事件，却没有统一的资产到期事件。

用户真正需要的不是「把各模块列表再抄一遍」，而是一个既能独立登记台账、又能把已有对象纳管进来统一看到期与成本的资产中心。

## Goals

1. 新增独立模块 `assets`，作为统一的资产管理入口。
2. 提供混合模型：既支持手工登记（面板管不到的资产），也支持纳管已有模块对象（引用而非复制数据）。
3. 统一到期字段口径与判断逻辑，跨模块到期汇总只在一个地方发生。
4. 提供实体资产与虚拟资产两条视图，共享同一张表与同一套 UI。
5. 引入成本维度（金额、币种、计费周期）与月均折算，为后续成本汇总打基础。
6. 复用现有 `notification` 与 `cronjobs` 能力实现资产到期告警，不新建告警体系。
7. 完全遵循现行 Go Manifest 路由治理、SQLite 幂等建表、Kumo UI 与站点时区约定。

## Non-Goals

1. 第一版不做自动发现与全量扫描：不会把 160 张业务表里的对象自动倒入资产清单，纳管必须显式确认。
2. 第一版不替换或改造任何已有模块的数据表，纳管通过引用与快照实现，不侵入来源模块。
3. 第一版不做折旧、摊销、财务对账等会计级能力。
4. 第一版不做多币种自动汇率换算，不做跨币种合计（除非用户显式配置基准币种与汇率）。
5. 第一版不做资产与工单、采购、库存的联动。
6. 第一版不提供 QR 标签打印与移动端扫码盘点。
7. 第一版不把机器网卡流量、托管节点流量、订阅用户用量或外部导入节点流量合并进资产统计（遵循 CONTEXT.md 硬性规则）。

## Target Users

1. 自托管用户与个人运维者，希望统一掌握服务器、域名、证书、订阅的到期与成本。
2. 小团队管理员，需要一份可维护的硬件与软件资产台账。
3. 已使用 API Monitor 管理多云与订阅，希望有跨模块汇总视图的用户。
4. 需要为续费、证书更换、许可证到期做提前规划的管理员。

## Success Metrics

1. 用户可以在 2 分钟内手工登记一条资产并看到到期倒计时。
2. 用户可以在总览页一眼看到即将到期与已过期资产的计数与清单。
3. 用户可以将已有服务器、订阅、云账号、SSL 证书纳管为资产，且来源数据可刷新。
4. 到期判断在所有视图中口径一致，均基于站点时区。
5. 成本信息按币种正确分组，月均折算口径在 UI 上有明确说明。
6. 资产到期可通过现有通知渠道收到告警。
7. 模块接入后不破坏现有 manifest 治理、前端导航与审计命令。

## Solution

新增独立模块，模块 ID 为 `assets`，入口名称「资产管理」，位于前端侧边栏新增的顶层分组「资产」中。

核心设计是**两层资产模型 + 快照式纳管**：

1. **登记资产**（`origin=manual`）：用户手工录入，面板管不到的对象，如网络设备、存储、终端、线下服务器、SaaS 账号。
2. **纳管资产**（`origin=linked`）：指向已有模块对象的引用，通过 `source_module` + `source_ref_id` 标识来源。纳管时写入一份快照（到期、状态、成本），由定时任务按来源刷新。

纳管采用快照而非读取时实时跨模块解析，理由是：各来源模块字段口径不一致（三种到期格式并存），实时解析会让列表查询退化为多次跨模块调用并形成强耦合。快照方案的代价是刷新间隔内存在滞后，可接受。

前端采用 Tabs 分区：总览 / 实体资产 / 虚拟资产 / 纳管来源。后端新增 `backend-go/internal/assets/`，路由前缀 `/api/assets`，鉴权 `AuthSession`。

## Product Scope

### Phase 1（P0）：登记册与到期视图

- `assets` 表建立，字段一次到位（预留纳管字段）。
- 手工登记资产的增删改查。
- 实体资产与虚拟资产两条列表视图。
- 总览：计数卡 + 到期分桶 + 最近到期表。
- 到期筛选（即将到期 / 已过期）。
- 完整的空态、加载态、失败态、窄屏与暗色适配。

### Phase 2（P1）：纳管与告警

- 纳管来源候选列表与批量纳管。
- 纳管行快照刷新（手动 + 定时）。
- 到期告警：新增 `asset_expiry` 通知事件，接入 `cronjobs` 日扫。
- 成本字段与按币种分组的成本卡。
- 每资产可覆盖的告警阈值。

### Phase 3（P2）：生命周期与汇总

- `asset_events` 生命周期事件（续费 / 变更 / 退役 / 来源失效）。
- 来源失效标记为 `orphan` 的处理流程。
- 成本趋势图与聚合仪表盘。
- 导出与 QR 标签（可选）。

## User Stories

1. 作为管理员，我想手工登记一台没有接入面板的物理服务器，以便统一掌握它的到期时间。
2. 作为管理员，我想登记网络设备、存储与终端，以便形成完整硬件台账。
3. 作为管理员，我想把已有的服务器账号纳管为资产，以便不必重复录入。
4. 作为管理员，我想把订阅与套餐纳管为资产，以便统一看到期。
5. 作为管理员，我想把域名与 SSL 证书纳管为资产，以便在证书过期前收到提醒。
6. 作为管理员，我想把云厂商账号与 API Key 纳管为资产，以便管理凭据到期。
7. 作为管理员，我想在总览页看到各类资产的计数，以便快速了解资产规模。
8. 作为管理员，我想看到即将到期与已过期的资产数量与清单，以便安排续费。
9. 作为管理员，我想按类型、状态、提供方、标签筛选资产，以便在资产较多时快速定位。
10. 作为管理员，我想看到每条资产的剩余天数与状态色调，以便判断紧急程度。
11. 作为管理员，我想记录资产的成本金额、币种与计费周期，以便了解经常性支出。
12. 作为管理员，我想按币种看到月均成本，以便不因汇率误算总额。
13. 作为管理员，我想为每条资产设置是否自动续费，以便自动续费的资产不再重复提醒。
14. 作为管理员，我想为重要资产单独设置告警阈值，以便提前更多天收到提醒。
15. 作为管理员，我想让纳管资产能从来源模块刷新数据，以便来源变更后资产信息同步。
16. 作为管理员，我想在来源对象被删除后仍保留资产记录，以便不丢失台账。
17. 作为管理员，我想给资产打标签与写备注，以便按项目或用途归类。
18. 作为管理员，我想让所有资产写操作进入操作日志，以便审计。
19. 作为管理员，我想整个模块 UI 与现有模块一致且全中文，以便降低学习成本。
20. 作为管理员，我想后续能查看资产的生命周期事件，以便回溯续费与变更历史。

## Functional Requirements

### 1. 模块入口与导航

- 模块 ID：`assets`
- 前端导航文案：`资产管理`，短名 `资产`
- 分组：新增顶层分组「资产」（group id `assets`），排在「仪表盘」之后
- 路由路径：`/assets`
- 页面目录：`src/js/pages/assets/`

### 2. 资产数据模型

资产分实体与虚拟两类，用同一张表承载。

实体类 `asset_type`：`server`（服务器/主机）、`network`（网络设备）、`storage`（存储）、`terminal`（终端）、`other_hw`（其他硬件）。

虚拟类 `asset_type`：`cloud_instance`（云资源实例）、`domain`（域名）、`ssl_cert`（SSL 证书）、`subscription`（订阅/套餐）、`license`（许可证）、`api_key`（API Key/凭据）、`proxy_node`（代理节点）、`saas`（SaaS）。

字段清单见 Technical Design Principles 的数据模型小节。

### 3. 手工登记资产

- 支持新增、编辑、删除、查看。
- 必填字段：`name`、`category`、`asset_type`。
- 分类决定可选类型：选实体类时只能选实体类型，选虚拟类时只能选虚拟类型。
- 删除使用 `dialog.deleteResource` 确认流程，不允许裸用 `confirm()`。
- 编辑与删除写操作进入操作日志。

### 4. 实体资产视图

- 表格展示实体类资产。
- 列：名称（含类型徽标）、提供方、状态、到期日、剩余天数、成本、标签、操作。
- 实体资产额外支持 `serial_no`（序列号）、`model`（型号）、`location`（位置）。
- 支持按类型、状态、提供方、标签筛选，支持名称搜索。

### 5. 虚拟资产视图

- 表格展示虚拟类资产，列定义与实体视图一致。
- 纳管行额外展示来源模块标记与同步时间。
- 支持相同的筛选与搜索能力。

### 6. 到期管理

- 状态由 `expire_at` 与站点时区实时派生：`expired`（已过期）、`expiring`（阈值内）、`active`（正常）、`retired`（已退役）、`orphan`（来源失效）、`unknown`（无到期信息）。
- 默认告警阈值：30 / 14 / 7 / 1 天。支持每资产通过 `warn_days_json` 覆盖。
- `auto_renew=1` 的资产不产生到期告警。
- 到期筛选参数 `expiring_within=N` 返回 N 天内到期（含已过期）的资产。
- 日期归属一律经 `internal/timeutil` 取站点时区，禁止在业务代码中直接使用 `time.Local` / `time.UTC`。

### 7. 成本管理

- 字段：`cost_amount`（金额）、`cost_currency`（币种）、`cost_cycle`（周期）。
- 周期枚举：`monthly`、`quarterly`、`yearly`、`one_time`、`usage`。
- 列表按原周期展示；汇总时折算为月均：`monthly` 直接取，`quarterly/3`，`yearly/12`，`one_time` 不计入经常性成本并单列，`usage` 不计入。
- 默认按币种分组展示，不做跨币种合计。
- 可选：用户在设置中配置基准币种与手工汇率后，才提供合计。
- 月均折算口径必须在 UI 上明确说明。

### 8. 总览

- 计数卡：实体资产数、虚拟资产数、即将到期数、已过期数。
- 到期分桶：已过期 / 7 天内 / 30 天内 / 正常 / 不续费，每段可点击跳转到列表并带对应筛选。
- 成本卡：按币种分组的月均成本，`one_time` 单列。
- 最近到期表：Top 10，按 `expire_at` 升序。

### 9. 纳管机制（P1）

- 提供「待纳管候选」接口，按来源模块分组列出尚未纳管的对象。
- 用户多选确认后批量纳管，不做自动导入。
- 去重键为 `(source_module, source_ref_id)` 唯一，重复纳管时提示已存在并跳转。
- 纳管行可手动刷新，也可由定时任务批量刷新。
- 来源对象被删除时，资产行不级联删除，标记为 `orphan` 并记录事件，由用户决定保留或清除。

### 10. 纳管来源覆盖清单（P1）

| 来源表 | 映射类型 | 可取字段 |
| --- | --- | --- |
| `server_accounts` | `server` / `cloud_instance` | `expires_at`、`tags`、`description` |
| `subscription_subscriptions` | `subscription` | `expire_at`、`cycle_*`、`total_bytes` |
| `cf_accounts` 及各云 `*_accounts` | `cloud_instance` / `api_key` | `expires_on` |
| `aliyun_domains` / `tencent_domains` | `domain` | 到期时间（刷新时拉取） |
| `uptime_monitor_states` | `ssl_cert` | `ssl_expiry`（直接读表，不重复握手探测） |
| `m365_accounts` 许可证 | `license` | 许可证数量与到期 |
| `openai_gateway_keys` / `api_access_keys` / `aiagent_tokens` | `api_key` | `expires_at` |
| `managed_proxy_nodes` | `proxy_node` | 节点状态与端口 |

### 11. 到期告警（P1）

- 新增 `notification` 事件 `asset_expiry`。
- 由 `cronjobs` 日扫任务触发，必须 `cron.New(cron.WithLocation(站点时区))` 并带 TZ watcher。
- 按资产的告警阈值分级提醒，`auto_renew=1` 跳过。
- 同一资产同一阈值在同一周期内只提醒一次，避免重复轰炸。

### 12. 生命周期事件（P2）

- `asset_events` 记录：`renewed`、`updated`、`retired`、`source_lost`、`source_restored`。
- 详情页以时间线展示。
- 事件写入由后端自动完成，不由前端手工创建。

## UX Requirements

1. 页面主体风格与 `DockerHubPage`、`SubscriptionPage` 保持一致。
2. 顶部使用 Tabs 分区：`总览`、`实体资产`、`虚拟资产`、`纳管来源`。
3. 列表使用 `AppTable` + 语义列角色，列定义集中在 `constants.js`。
4. 筛选栏使用 `ResponsiveSearchInput` + `Select`，窄屏用 `TabBarOverflowActions` 收纳。
5. 新建/编辑使用 `LayerDialog`，结构必须满足一个 `Title` + 一个 `Body` + 可选 `Description` + `Actions`，`Actions` 只含一个 `Actions.Primary`。
6. 日期选择使用现成的 `DateField` / `DateTimeField`，不使用原生 `input type="date"`。
7. 删除使用 `dialog.deleteResource`。
8. 剩余天数用状态色调分级：正常 `info`、30 天内 `warning`、7 天内 `danger`、已过期 `danger`。
9. 颜色只用语义 token（`text-kumo-strong`、`text-kumo-subtle`、`bg-kumo-recessed`、`kumo-success/warning/danger`），不写死色值。
10. 时间展示统一使用 `modules/utils.js` 的 `formatDateTime`，它已跟随全局展示时区。
11. 所有可见文案使用简体中文，不使用中文引号包裹缩写词。
12. 每个视图覆盖加载中、空数据、加载失败、保存中、窄屏、暗色六种状态。

## Technical Design Principles

### 1. 数据模型

主表 `assets`：

- `id`：主键
- `origin`：`manual` | `linked`
- `category`：`physical` | `virtual`
- `asset_type`：类型枚举（见 Functional Requirements 第 2 节）
- `name`：名称
- `provider`：提供方
- `owner`：负责人
- `location`：位置
- `serial_no`：序列号（实体）
- `model`：型号（实体）
- `status`：`active` | `expiring` | `expired` | `retired` | `orphan` | `unknown`
- `source_module`：纳管来源模块标识（`linked` 时填）
- `source_ref_id`：纳管来源对象 ID（`linked` 时填）
- `source_synced_at`：上次刷新时间
- `acquire_date`：购置日期
- `expire_at`：到期时刻，统一存储为 UTC RFC3339 TEXT
- `warn_days_json`：覆盖全局阈值的天数数组，空则用默认
- `auto_renew`：是否自动续费
- `cost_amount`：成本金额
- `cost_currency`：币种
- `cost_cycle`：计费周期
- `tags_json`：标签数组
- `metadata_json`：扩展字段容器
- `remark`：备注
- `created_at` / `updated_at`

辅助表：

- `asset_links`（P2）：纳管去重键 `(source_module, source_ref_id)` 唯一约束。
- `asset_events`（P2）：生命周期事件。
- `asset_settings`（P1）：单行设置表，存基准币种、汇率、全局告警阈值。

到期时刻统一为 UTC RFC3339 TEXT 是刻意设计：现有三种到期口径在纳管时全部归一，展示与日期归属统一交给前端与 `timeutil`。

### 2. 后端模块结构

```text
backend-go/internal/assets/
├── service.go            Service 定义、New()、ServeHTTP 路径分发
├── handlers.go           薄 handler，调 response.OK/Error
├── store.go              SQL 读写集中处
├── schema.go             幂等建表 DDL
├── expire.go             到期派生与状态判定
├── cost.go               月均折算与币种分组
├── sources.go            纳管来源解析与刷新（P1）
├── helpers.go            decodeJSON、分页、类型转换
└── service_test.go
```

### 3. 路由注册

- 路由前缀 `/api/assets`，鉴权 `AuthSession`，响应 `json`。
- 在 `backend-go/internal/manifest/manifest.go` 登记前缀与每条叶子子路由。
- 在 `backend-go/internal/server/route_handlers.go` 的 `moduleHandlers` 注册 `assets` 分发。
- 在 `backend-go/internal/server/server.go` 完成 import、字段与实例化接线。
- 只登记前缀不登记叶子路由会被标记为 `prefixRoute`，视为未完成接入。
- 写接口必须在 `backend-go/internal/system/route_contracts.go` 登记请求契约，否则 `backend-go:test` 的契约覆盖测试会失败。

### 4. API 目录登记

按现行约定完成 API 目录六处登记，保证接口可被 AI 目录召回：

- `route_descriptions.go`：中文描述
- `api_docs_catalog.go`：逐条登记，写接口必须显式声明方法
- `route_aliases.go`：模块名与关键资源口语别名
- `route_contracts.go`：写接口请求体契约
- `system/service.go` 的 `routeGroup()`：分组归属
- 前端 `ApiDocsPage.jsx` 的分组映射

### 5. 数据库约定

- 表名带 `asset_` 前缀，避免与现有表冲突（注意避开无关的 `drawio_assets`）。
- 使用 `CREATE TABLE IF NOT EXISTS` + `CREATE INDEX IF NOT EXISTS` 幂等建表，加列前先用 `PRAGMA table_info` 判断。
- SQLite 周期 WAL 维护只做 PASSIVE，禁止在自动路径使用 TRUNCATE/RESTART。
- 新表必须能被 `npm run db:audit`（`cmd/schema-audit`）识别。

### 6. 时区约定

- 站点时区唯一来源为 `user_settings.time_zone`，经 `internal/timeutil` 读取。
- 绝对时刻写库保持 UTC RFC3339：`time.Now().UTC().Format(time.RFC3339)`。
- 日期归属（哪天算到期、按今天的桶）一律经 `LocationFromSettings`。
- 由 `tools/tz-governance-check.mjs` 把守。

### 7. 前端结构

```text
src/js/pages/assets/
├── AssetsPage.jsx         容器：Tab、筛选、加载、Dialog 编排
├── tabs.jsx               Tab 定义数组
├── constants.js           类型/状态/周期枚举与列定义
├── api.js                 请求封装与路径常量
├── utils.js               到期计算、成本折算、状态派生、格式化
├── OverviewPanel.jsx      总览
├── AssetTable.jsx         资产表（实体/虚拟共用）
├── AssetFormDialog.jsx    新建/编辑
├── AssetDetailDialog.jsx  详情与刷新
├── CostSummaryCard.jsx    成本卡
└── SourcePickerDialog.jsx 纳管来源选择（P1）
```

请求统一走 `src/js/modules/apiClient.js` 的 `request`，返回值取 `result.data ?? result`。

### 8. 前端接线

新增模块四处接线：

1. `src/js/store.js`：`MODULE_CONFIG` 加配置，`MODULE_GROUPS` 加顶层分组。
2. `src/js/components/MainLayout.jsx`：lazy import 与 `renderActivePage()` 分支。
3. `src/js/components/Icons.jsx`：`MODULE_ICON_MAP` 加图标。
4. 页面目录 `src/js/pages/assets/`。

`src/js/store.js`、`src/js/components/MainLayout.jsx`、`backend-go/internal/manifest/manifest.go`、`backend-go/internal/server/server.go` 均为 CONTEXT.md 列出的单一归属高风险文件，实施时需独占。

## API Contract Draft

### 资产

- `GET /api/assets`：列表，支持 `category`、`asset_type`、`status`、`provider`、`tag`、`q`、`expiring_within`、`limit`、`offset`
- `POST /api/assets`：新建
- `GET /api/assets/{id}`：详情
- `PUT /api/assets/{id}`：更新
- `DELETE /api/assets/{id}`：删除

### 汇总

- `GET /api/assets/overview`：按类别/类型/状态的计数、到期分桶、按币种成本
- `GET /api/assets/expiring`：即将到期清单

### 纳管（P1）

- `GET /api/assets/candidates`：待纳管候选，按来源模块分组
- `POST /api/assets/links`：批量纳管
- `POST /api/assets/{id}/refresh`：刷新单个纳管行

### 设置（P1）

- `GET /api/assets/settings`：读取基准币种、汇率、全局阈值
- `PUT /api/assets/settings`：更新设置

## Request / Response Requirements

统一遵循项目现有响应风格：

成功：

```json
{
  "success": true,
  "data": {}
}
```

失败：

```json
{
  "success": false,
  "error": "错误描述"
}
```

额外要求：

- 列表接口返回归一化字段，不透传来源模块原始结构。
- 分页统一使用 `limit` / `offset`，默认 `limit=100`。
- 写接口请求体必须登记到 `route_contracts.go`。

## Security Requirements

1. 所有 `/api/assets` 接口要求 session auth。
2. 资产不存储任何明文凭据；纳管 API Key 类资产只存引用与元数据，不复制密钥内容。
3. 日志与错误信息不打印来源对象的敏感字段。
4. 删除与批量纳管等高风险操作需明确确认。
5. 关键写操作写入现有操作日志体系。
6. 文档与测试夹具不得写入真实凭据、真实 IP 或本机绝对路径。

## Integration Decisions

1. **独立模块而非并入现有模块**
   原因：资产横跨云厂商、DevOps、订阅、工具箱多块，放入任何单一现有模块都会造成职责混乱。

2. **快照式纳管而非实时跨模块解析**
   原因：各来源到期口径不一，实时解析会造成多次跨模块调用与强耦合；快照方案与主流开源资产清单工具（CloudQuery 模式）一致。

3. **显式纳管而非自动发现**
   原因：自动导入 160 张业务表的对象会产生不可控噪声，显式确认保证清单可信。

4. **复用 notification 与 cronjobs**
   原因：不新建告警体系，避免重复实现与配置分裂。

5. **来源失效保留记录**
   原因：资产台账属于用户数据，来源删除不应导致用户数据丢失。

## Observability and Logging

模块至少记录以下行为：

- 新增 / 编辑 / 删除资产
- 批量纳管与单个纳管
- 纳管行刷新及其结果
- 到期告警触发

日志要求：

- 记录资产 ID、来源模块、动作类型、结果状态、错误摘要
- 不记录来源对象的敏感字段
- 刷新失败时记录可诊断信息

## Testing Decisions

### 后端测试

- schema 幂等初始化
- 资产 CRUD 与字段校验
- 分类与类型约束
- 到期派生与状态判定（含站点时区）
- 成本月均折算与币种分组
- 纳管去重
- 来源失效标记
- 路由分发与请求契约

### 前端测试

- 表单校验
- 空态 / 加载态 / 失败态渲染
- 列表筛选与搜索
- 删除确认
- 状态色调分级

### 验收命令

```bash
npm run governance:check
node tools/backend-route-inventory.mjs
npm run backend-go:test
npm run ui:governance
npm run lint
npm test
```

## Release Plan

### Milestone 1：架构接入

- 前端注册 `assets` 模块与分组
- 后端注册 `/api/assets`
- 建立 SQLite schema
- 完成 API 目录六处登记

### Milestone 2：登记册与视图（P0）

- 资产 CRUD
- 实体 / 虚拟两条列表
- 总览计数与到期分桶
- 到期筛选

### Milestone 3：纳管与成本（P1）

- 纳管候选与批量纳管
- 快照刷新
- 成本字段与成本卡
- 设置表

### Milestone 4：告警与生命周期（P1/P2）

- `asset_expiry` 通知事件
- `cronjobs` 日扫任务
- `asset_events` 生命周期
- 来源失效处理流程

### Milestone 5：汇总与增强（P2）

- 成本趋势图
- 聚合仪表盘
- 导出与 QR 标签

## Acceptance Criteria

1. 侧边栏中可以看到「资产」分组与「资产管理」入口。
2. 用户可以手工登记一条资产，并在列表与总览中看到它。
3. 实体资产与虚拟资产视图各自正确分类展示。
4. 用户可以按类型、状态、提供方、标签筛选，并按名称搜索。
5. 到期状态与剩余天数在所有视图中口径一致，且基于站点时区。
6. 总览正确显示计数、到期分桶与最近到期清单。
7. 成本按币种分组展示，月均折算口径有明确说明。
8. 用户可以把已有服务器、订阅、云账号、SSL 证书纳管为资产。
9. 纳管行可以刷新，且刷新后来源数据同步。
10. 来源对象删除后资产记录保留并标记为 `orphan`。
11. 到期告警可通过现有通知渠道送达，`auto_renew` 资产不重复提醒。
12. 所有 `/api/assets` 接口进入 manifest 与 Go 路由分发体系，并完成 API 目录登记。
13. 页面全部使用 Kumo 组件，中文 UI 完整，无明显样式漂移。
14. 相关治理命令、后端测试与前端 lint 全部通过。

## Risks

1. 纳管来源跨 8 类模块，字段口径差异大，若归一化不彻底会导致刷新结果不一致。
2. 快照刷新存在滞后，用户可能误以为列表为实时数据，需在 UI 上标注同步时间。
3. 多币种成本若无明确口径，容易产生错误总额，需坚持按币种分组。
4. 到期派生若前后端各算一遍，容易再次产生口径分裂，需明确以后端为准、前端只做展示分级。
5. `store.js` 与 `MainLayout.jsx` 为单一归属高风险文件，多窗口协作时需独占，否则易冲突。
6. 若把流量类指标混入资产统计，会违反 CONTEXT.md 的流量口径硬性规则。

## Out of Scope

1. 自动发现与全量扫描导入。
2. 改造任何已有模块的数据表。
3. 折旧、摊销与财务对账。
4. 自动汇率换算与跨币种合计（除非用户显式配置）。
5. 工单、采购与库存联动。
6. QR 标签打印与移动端扫码盘点。
7. 资产与流量的任何合并统计。

## Further Notes

1. 本模块最有机会成为项目内「跨模块聚合」的范式样本：引用 + 快照 + 统一到期口径，后续其他汇总类需求可复用。
2. 到期口径统一后，可反向推动 `ServerPage.jsx`、`subscription/summary.go`、`uptime/ssl.go` 的到期计算逐步收敛到统一实现。
3. 建议 P0 严格控制在登记册范围，先把「看得到、筛得动、报得准」做好，再进入纳管与告警。
