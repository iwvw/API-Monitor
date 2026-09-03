# Google Cloud Platform（GCP）云资源管理模块 PRD

最后更新：2026-09-03

## Problem Statement

API Monitor 已覆盖 Cloudflare、阿里云、腾讯云、Koyeb、Fly.io、Oracle OCI、Microsoft 365 等云厂商，但缺少 Google Cloud Platform（GCP）支持。GCP 拥有 Compute Engine（VM）、Cloud Storage（对象存储）、Cloud Billing（费用）等广泛产品线，其 Service Account 凭证模式和 REST API 体系与已接入厂商有显著差异。

这带来几个问题：

1. 使用 GCP 的用户需要在 Google Cloud 控制台、gcloud CLI 和 API Monitor 之间切换，无法统一面板管理。
2. GCP 的 Service Account JSON 凭证、OAuth2 JWT token 交换、项目（project）维度的资源组织方式，与现有模块（OCID、AccessKey、API token）均不同，无法直接套用现有账号表单。
3. GCP 的 Compute Engine 操作是异步 Operation 模式，需要在交互语义上明确「指令已下发」与「资源已变更」的区别。
4. 用户无法在 API Monitor 中查看 GCP 项目、VM 实例、磁盘、防火墙、静态 IP、存储桶和费用信息，更无法执行启停、删除等操作。
5. 项目对云厂商凭证的存储策略不完全一致，GCP 模块需要从第一版开始建立 SA JSON 加密存储基线。

用户需要的是一个基于 GCP 官方 REST API 的、与现有云厂商模块风格一致、覆盖 GCP 主要功能、可继续扩展的 GCP 管理模块。

## Goals

1. 在 API Monitor 中新增独立 `gcp` 模块，作为 GCP 统一管理入口。
2. 使用 REST API + OAuth2 service account 实现（决策见 [ADR-0002](../adr/0002-GCP模块架构决策.md)），不引入官方 SDK 重型依赖。
3. 覆盖 GCP 主要功能：账号管理、项目浏览、Compute 实例全生命周期、磁盘、防火墙、静态 IP、Cloud Storage 存储桶、费用概览、Vertex AI 模型用量监控。
4. 保持与现有 Go Manifest 后端、React + Kumo 前端、SQLite 数据层和模块治理方式一致。
5. 凭证从第一版起采用 secure 加密存储，建立 GCP 侧安全基线。

## Non-Goals

1. 第一版不将 GCP 实例自动接入 `server` 模块的 Agent/SSH/Docker 体系（后续弱集成）。
2. 不做 GCP 全产品覆盖：Cloud SQL、GKE、BigQuery、Cloud Functions、Pub/Sub、Vertex AI 等 PaaS/托管服务的**管理与生命周期操作**不在范围；Phase 5 的 Vertex AI 模型**用量只读监控**（Cloud Monitoring 指标）是唯一例外，仅展示数据、不做创建/部署/删除。
3. 不做 IAM 角色/策略/服务账号管理（权限在 GCP 控制台配置，模块不代行）。
4. 不做 Terraform/IaC 编排、多租户 RBAC。
5. 不在浏览器内嵌 gcloud 终端或 Cloud Shell。

## Target Users

1. 使用 GCP Compute Engine 的个人用户和小团队运维者。
2. 希望在统一面板管理多云资产的自托管用户（已有阿里云/腾讯云/Oracle + 新增 GCP）。
3. 管理 GCP 免费层、e2-micro 实例、出海节点的管理员。
4. 需要快速查看 GCP 费用和存储桶的对象存储用户。

## Success Metrics

1. 用户可在 5 分钟内完成一个 GCP 账号接入并看到项目列表。
2. 用户可在 UI 中对 GCP 实例执行启动、停止、重启操作。
3. 用户可查看实例的公网/私网 IP、磁盘附加、防火墙规则。
4. 用户可看到当前月份费用概览。
5. 模块接入后不破坏 Go manifest、前端导航和审计命令。
6. SA JSON 凭证默认加密存储，列表不回显。

## Solution

新增独立 GCP 云厂商模块，模块 ID `gcp`，入口名称「Google Cloud」，位于前端侧边栏「云服务 -> 云厂商」分组。

1. 前端新增 `src/js/pages/GcpPage.jsx`，提供账号、项目、实例、磁盘、网络、存储、费用视图。
2. 后端新增 `backend-go/internal/gcp/`，REST 封装 + OAuth2 token 交换 + 按资源域拆分的 service 文件。
3. 路由统一 `/api/gcp/...` 前缀，登记 manifest.go。
4. 高风险操作（删除实例/删除磁盘/清空存储桶/删除防火墙）使用 Kumo DeleteResource 确认。
5. 所有 Compute 操作返回 Operation 对象，前端展示「指令已下发」并靠刷新确认。

## Product Scope

### Phase 1：账号接入与实例管理主链路

- GCP 账号（SA JSON）配置与验证
- 项目列表（Resource Manager）
- 实例列表（aggregatedList 全区域）、详情、搜索/筛选
- 实例生命周期动作：start / stop / reset / delete
- 实例磁盘附加查看
- 实例网络接口与公私网 IP 查看

### Phase 2：资源选择器与创建实例

- zone 列表、machineTypes 列表、images 列表（filter 支持 family 查询）
- subnet/网络选择
- 创建实例（基础参数：名称、zone、机型、镜像、磁盘、网络、boot disk size）
- 实例标签（setLabels）与元数据（setMetadata）编辑
- 防火墙规则查看
- 静态 IP（addresses）列表与查看

### Phase 3：存储与费用

- Cloud Storage 存储桶列表/详情/创建/删除
- 桶内对象浏览（前缀分页）、上传/下载/删除对象
- Cloud Billing 费用概览（当前月份费用曲线、按服务分类）
- billingAccounts 列表、项目计费信息查看

### Phase 4：与现有主机模块弱集成

- GCP 实例导入 server 模块表单草稿（主机名、IP、区域、备注预填）
- 一键生成 Agent 安装命令指引
- Cron/日报只读访问白名单（internalCronReadonlyPrefixes）

### Phase 5：模型用量监控（Vertex AI / Gemini）

- 通过 Cloud Monitoring time series 读取 Vertex AI 模型调用量指标
- 展示模型调用次数（invocation_count）、输入/输出 token、按模型/区域分组
- 用量曲线（按天聚合）+ 汇总卡片；只读查看，不做模型生命周期管理

## User Stories

1. 作为管理员，我想新增一个 GCP 账号（粘贴 SA JSON），以便在系统中管理 GCP 资源。
2. 作为管理员，我想验证账号是否有效，以便尽早发现 project、IAM 角色或密钥错误。
3. 作为管理员，我想看到该凭证可访问的项目列表，以便选择要管理的项目。
4. 作为管理员，我想设置默认项目，以便减少重复选择。
5. 作为管理员，我想查看项目下所有区域的实例列表，以便快速定位目标机器。
6. 作为管理员，我想按名称、状态、zone、IP 搜索实例，以便在实例多时快速筛选。
7. 作为管理员，我想看到实例的公网 IP 和私网 IP，以便连接和资产登记。
8. 作为管理员，我想查看实例机型、镜像、创建时间、电源状态，以便判断用途。
9. 作为管理员，我想对实例执行启动操作，以便恢复停机资源。
10. 作为管理员，我想对实例执行停止/重启操作，以便维护或节约资源。
11. 作为管理员，我想删除某个无用实例，以便减少费用，但执行前希望有强确认。
12. 作为管理员，我想查看实例的磁盘附加情况，以便排查磁盘问题。
13. 作为管理员，我想创建实例，以便快速扩容（Phase 2）。
14. 作为管理员，我想编辑实例标签，以便打上管理标记。
15. 作为管理员，我想查看项目的防火墙规则，以便了解网络边界。
16. 作为管理员，我想查看项目占用的静态 IP，以便清理闲置资源。
17. 作为管理员，我想浏览存储桶列表和桶内对象，以便管理对象存储资产。
18. 作为管理员，我想删除空存储桶，以便清理资源。
19. 作为管理员，我想看到当前月费用和按服务分类明细，以便控制支出。
20. 作为管理员，我想在失败时看到 GCP 返回的具体错误摘要，以便修正配置。
21. 作为管理员，我想让所有高风险操作写入操作日志，以便审计。
22. 作为管理员，我想 UI 与现有云厂商模块一致且全中文，以便降低学习成本。
23. 作为管理员，我想查看项目的 Vertex AI 模型调用量（次数与 token），以便判断模型使用成本与趋势（Phase 5）。
24. 作为管理员，我想按模型/区域筛选并看到近 30 天用量曲线，以便定位高消耗模型（Phase 5）。

## Functional Requirements

### 1. 模块入口与导航

- 模块 ID：`gcp`
- 前端导航文案：`Google Cloud`
- 分组位置：`云服务 -> 云厂商`
- 路由路径：`/gcp`
- 页面文件：`src/js/pages/GcpPage.jsx`

### 2. GCP 账号管理

支持 SA JSON 账号的新增、编辑、删除、验证、列表。字段：

- `name`：账号备注名
- `serviceAccountJson`：完整 SA JSON（粘贴或文件上传），验证必须包含 `client_email`、`private_key`、`token_uri`、`project_id`
- `defaultProjectId`（可选，默认取 JSON 内 `project_id`）
- `description`（可选）

规则：

- SA JSON 必须使用 `secure.SecureEncrypt` 加密存储，禁止明文。
- 列表页不回显 JSON 内容，展示 `client_email`、默认项目、创建时间、验证状态。
- 编辑时「不修改 JSON 则留空」。
- 验证：解析 JSON → JWT 签名 → token 交换 → 调用 `GET /v1/projects/{id}` 或 projects.list 确认可用，失败原因（JSON 格式错误/私钥损坏/权限不足/目标项目服务 API 未启用（`PERMISSION_DENIED ... has not been used in project ...`，提示预启用对应 API）/token endpoint 拒绝）分类展示。
- 存入 `gcp_accounts` 表（schema 见技术设计）。

### 3. 项目管理

- `GET projects.list`（Resource Manager）列出凭证可见的项目。
- 支持在账号内切换/保存默认项目。
- 展示：projectId、名称、生命周期状态、标签。
- 项目与资源（实例/磁盘/网络/存储桶）的查询路径为 `/api/gcp/accounts/{id}/projects/{projectId}/instances` 等，project 是资源的显式上下文。

### 4. 实例列表

- 使用 Compute Engine `instances.aggregatedList` 一次取全区域实例，返回标准化列表。
- 至少展示：名称、instanceId、状态（RUNNING/TERMINATED/STOPPING/PROVISIONING…）、机型、zone、公网 IP、私网 IP、创建时间、镜像/标签摘要。
- 支持按名称/状态/zone/IP 筛选（前端本地筛选为主，未配置过滤时后端可不传 filter）。
- 空态、加载态、失败态必须清晰。支持刷新。

### 5. 实例详情

- 基础信息：名称、ID、zone、机型、镜像、标签、状态、创建时间、删除保护状态。
- 网络接口：名称、子网、内网 IP、外部 IP（含 accessConfig 类型）、内外网层级（natIP 透明）。
- 磁盘附加：名称、类型、大小、模式（READ_WRITE/READ_ONLY）、来源快照/镜像、autoDelete、boot。
- 显示标签（labels）、元数据（metadata.items 摘要）、机器配置 CPU/RAM。
- 服务账号（attached service account）摘要。

### 6. 实例生命周期动作

Phase 1 支持：

- `start`
- `stop`
- `reset`
- `delete`（强确认：提示 boot disk 是否同时删除（delete local disk 选项））

规则：

- 所有动作返回 Operation；前端 toast「指令已下发」+ 列表刷新。
- `delete` 走 DeleteResource 确认，需勾选明确后果说明。
- 动作执行中状态（PROVISIONING/STOPPING/STAGING 等）由状态字段自然反映，不额外实现长轮询。

### 7. 磁盘（Phase 2 扩展，PRD 先定义范围）

- 项目磁盘列表（`disks.aggregatedList`）：名称、类型、大小、zone、状态、附加实例、快照来源。
- 磁盘创建（基础参数）、删除、快照（`disks.createSnapshot`）、扩容（`disks.resize`）。
- 删除与快照均需确认。

### 8. 防火墙规则（Phase 2）

- 项目防火墙列表（`firewalls.list`）：名称、方向（INGRESS/EGRESS）、优先级、动作（ALLOW/DENY）、网络、来源/目标、端口列表。
- 只读查看为主；创建/删除作为 Phase 2 可选增强（需确认）。

### 9. 静态 IP / 地址（Phase 2）

- `addresses.aggregatedList`：名称、类型（REGIONAL/GLOBAL）、区域、状态（IN_USE/RESERVED）、附加实例。
- 释放（delete）需确认，展示当前附加目标警告。

### 10. Cloud Storage 存储桶（Phase 3）

- 桶列表：名称、位置、存储类别、创建时间、统一版本控制状态。
- 桶详情：生命周期规则摘要、标签、权限摘要（IAM policy 只读）。
- 桶创建（名称、位置、存储类别、统一版本控制开关）。
- 桶删除（仅限空桶，DeleteResource 确认）。
- 对象浏览：前缀分页、大小、类型、最后修改时间；对象上传（小文件）、下载（签名 URL 或代理流）、删除（确认）。

### 11. 费用概览（Phase 3）

- billingAccounts 列表（`billingAccounts.list`）。
- 项目计费信息（`projects.getBillingInfo`）。
- 费用曲线：当前月按日费用（数据来自 Cloud Billing BigQuery 导出，非通用 REST；第一版以 billingAccounts、预算（Budgets API list）为可行 REST 能力，费用曲线列为后续增强，依赖用户配置 BigQuery 导出）。
- 预算列表与阈值展示（Budgets API `billingAccounts.budgets.list`）。
- 赠金/信用余额：**无公开 REST API 可读**（`billingAccounts.get` 只返回 name/open/displayName/currencyCode，v1 无 credits/balance 端点），控制台「赠金余额」仅 UI 可见，模块不承诺展示该项。成本预警可基于预算的 `creditFilteredSpend`（计入赠金抵扣后的净支出）实现。

### 12. 可观察性要求

- 记录：账号增删改、验证、项目切换、实例动作、磁盘动作、桶操作。
- 日志不包含 SA JSON 内容、私钥、token 值。
- 错误响应把 GCP error 原因转成「简短中文摘要 + 原始详情」。

### 13. 模型用量监控（Phase 5）

通过 Cloud Monitoring `timeSeries.list` 读取 Vertex AI 模型用量指标，只读展示，不做模型/Agent 生命周期管理。

指标（`metric.type`）：

- `aiplatform.googleapis.com/publisher/online_serving/model_invocation_count`：模型调用次数（resource `aiplatform.googleapis.com/PublisherModel`，按 `model_user_id` / `location` 分组）。
- 生成类模型（Gemini）输入/输出 token 指标（`.../request_count`、`input_token_count`、`output_token_count` 等）；第一版先展示调用次数 + 汇总，token 明细按实际可用指标补齐。

规则：

- 时间范围默认近 30 天，按天对齐（`alignmentPeriod=86400s`），支持前端切换范围。
- 前端展示：汇总卡片（总调用量/平均每日）+ `TimeseriesChart` 曲线 + 按模型/区域的明细列表。
- 账号验证失败分类补充「`roles/monitoring.viewer` 缺失 / `monitoring.googleapis.com` API 未启用」提示。
- 本小节只读，不做 Vertex 模型创建/部署/删除、不做 Agent Platform 管理。

## UX Requirements

1. 页面主体风格与 `OraclePage`、`AliyunPage` 保持一致。
2. 账号管理与资源管理同页 Tabs：`实例`、`磁盘`、`网络`、`存储`、`费用`、`账号管理`。
3. 资源视图全局头部：账号选择器 + 项目选择器 + 刷新按钮。
4. 项目选择器切换后，资源列表（实例/磁盘/网络/存储/费用）联动刷新。
5. 全部中文文案，Kumo 组件（Button/Table/Tabs/Dialog/DeleteResource/Toasty/Loader/Tooltip/Popover/Dropdown）。
6. 删除/终止动作使用 `dialog.deleteResource`。
7. SA JSON 输入区支持多行文本粘贴 + 可选文件上传。
8. 长字符串（instanceId、SA email、bucket 名、Operation name）支持复制（ClipboardText）。
9. 实例动作按钮组：启动/停止/重启/删除，按状态显隐（RUNNING 显示停止/重启，TERMINATED 显示启动/删除）。
10. 异步操作 toast 文案区分「已下发，等待 GCP 执行」与「操作失败」。
11. 空态、加载态、失败态、窄屏、暗色主题全覆盖（见《新模块接入指南》第 7 节）。

## Frontend Registration Template

新增 GCP 前端模块需对齐 oracle/aliyun 现有接线（`src/js/store.js`、`src/js/components/MainLayout.jsx`、`src/js/components/Icons.jsx`），具体改动如下：

### store.js

1. `MODULE_CONFIG` 在 `oracle` 条目之后追加：

```js
gcp: {
  name: 'Google Cloud',
  shortName: 'GCP',
  icon: 'fa-cloud',
  description: 'GCP 资源',
},
```

2. `MODULE_GROUPS` 中 `cloud-vendors.subgroups` 的 `modules` 数组加 `'gcp'`（当前 `['dns', 'aliyun', 'tencent', 'oracle', 'm365']` → 末尾追加）。

3. `DEFAULT_MODULE_ORDER` / `DEFAULT_MODULE_VISIBILITY` 由 `MODULE_GROUPS.flatMap(getGroupModuleIds)` 自动派生（见 `store.js` 约 326 行），**无需手改**——只要订阅了模块配置持久化逻辑即自动包含 `gcp`。若需默认隐藏，才手动加 `DEFAULT_MODULE_VISIBILITY.gcp = false;`。

### MainLayout.jsx

1. lazy import 区（约 34-49 行，oracle 附近）追加：

```js
const GcpPage = lazy(() => import('../pages/GcpPage.jsx'));
```

2. `renderActivePage()` switch 加 `case 'gcp': return <GcpPage />;`（与 oracle/aliyun 相邻）。
3. 若已有 sticky-header / 画布相关的模块白名单数组（按模块 id 分类渲染容器），需确认 `gcp` 归入哪一类，必要时补进对应数组。

### Icons.jsx

1. 品牌图标映射（`MODULE_ICON_MAP`，约 264-287 行）加一个 GCP 品牌图标映射；若暂无现成 GCP 图标资源，可先用 `Cloud`（与 `oracle`/`m365` 相同，属允许的 brand color 例外组），图标资源就绪后替换。

### 页面路由

- 路由路径 `/gcp`（与模块 ID 一致），页面文件 `src/js/pages/GcpPage.jsx`（规模大时按 `src/js/pages/gcp/` 目录拆分，同 OpenAIPage 模式）。

## Technical Design Principles

### 1. REST + OAuth2，不引入官方 SDK

见 [ADR-0002](../adr/0002-GCP模块架构决策.md)。REST 客户端封装：

- `client.go`：`gcpRequest(ctx, account, project, path, query, body)` 统一拼 URL、Bearer 头、超时（15s）、响应限 5MB、错误提取（GCP error 结构 `error.status/message/details`）。
- 环境变量 `GCP_API_BASE_URL` / `GCP_TOKEN_BASE_URL` 可覆盖，测试指向 mock server（同 koyeb `KOYEB_API_BASE_URL` 模式）。注意 Cloud Billing Budget API 是独立服务（`billingbudgets.googleapis.com`，非 `cloudbilling.googleapis.com`），预算类接口的 mock 覆盖需单独处理；模型用量走 Cloud Monitoring（`monitoring.googleapis.com`），同样需单独 base URL 覆盖（`GCP_MONITORING_API_BASE_URL`）。

### 2. 鉴权：SA JSON → JWT → access token

- `auth.go`：解析 SA JSON → `client_email` + `private_key`，构造 JWT（header RS256 + payload iss/sub/aud/scope/iat/exp，exp 1h）→ POST token endpoint → 缓存 access token 至 exp 前 5 分钟。
- scope 申请：统一申请完整 `https://www.googleapis.com/auth/cloud-platform`（写操作同 scope）。注意：实测 `cloud-platform.read-only` 无法通过 Compute `instances.aggregatedList` 等只读接口的鉴权（返回 `insufficient authentication scopes`），必须用完整 `cloud-platform`。
- token 缓存 key：account ID + scope。进程内存缓存（map + mutex），不落库。
- SA 最小权限建议（GCP 侧 IAM 配置，模块不代行）：目标项目授予 `roles/compute.admin` + `roles/viewer` + `roles/storage.admin`；结算账号层授予 `roles/billing.viewer`。注意 `roles/billing.viewer` 不支持绑定在项目资源上（GCP 报 `Role roles/billing.viewer is not supported for this resource`），费用概览所需的项目计费信息依赖结算账号层授权 + 项目 `viewer`；SA 无 `serviceusage.services.enable` 权限，目标项目需预启用 `cloudresourcemanager`/`compute`/`storage`/`billingbudgets` API。有 Organization 时可在组织/文件夹层一次授权继承到子项目，但游离在组织层级之外的项目（`projects describe` 的 parent 为空）必须项目级单独授权。

### 3. 模块走 Go Manifest 架构

- `manifest.go`：`{Prefix: "/api/gcp", Module: "gcp", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Google Cloud Platform accounts and resources"}`，登记在 `/api/oracle` 之后。
- `server.go`：import、字段、`newServer()` 实例化、`serveGoRoute()` case：
  - import 区加 `"github.com/iwvw/api-monitor/backend-go/internal/gcp"`；
  - Server 结构体加字段 `gcp *gcp.Service`；
  - `newServer()` 里 `gcp: gcp.New(cfg)`；
  - `serveGoRoute()` 加 `case "/api/gcp": s.gcp.ServeHTTP(w, r)`（同 `/api/oracle` 位点）。
- 无 Express sidecar、无独立微服务。

### 3.1 Service 结构、New 与 ServeHTTP（对齐 oracle 蓝图）

模块整体照抄 oracle 已验证的实现模板（`backend-go/internal/oracle/service.go`）：

- Service 结构体：

  ```go
  type Service struct {
      cfg     config.Config
      store   *database.Store
      schema  database.SchemaEnsurer
      clients clientFactory
  }
  ```

- `New(cfg config.Config)`：构造 Service、`store: database.New(cfg)`、`clients: clientFactory{}`；启动时用 `context.WithTimeout(ctx, 5*time.Second)` 做一次 `s.open()` 探活（成功即关连接），保证模块可用性在启动期暴露，然后返回 service。
- 路由分发：`ServeHTTP` 先 `strings.TrimPrefix(r.URL.Path, "/api/gcp")`，再 `strings.Trim(path, "/")`，空则保持空串，否则 `strings.Split(path, "/")` 按段落数 + 首段 + 方法做 switch-case 分发到各 handler；未知路径 `default: response.Error(w, http.StatusNotFound, ...)`。
- 每个 handler 内的 DB 访问模式：`s.open(r.Context())` → `defer db.Close()` → 查/写 → `response.OK(w, ...)` 或 `response.Error(w, http.StatusBadGateway, ...)`。

### 3.2 account_store 与安全脱敏约定（对齐 oracle 模板）

- `ensureSchema(ctx, db)`：幂等 `CREATE TABLE IF NOT EXISTS gcp_accounts` + `idx_gcp_accounts_created_at` / `idx_gcp_accounts_client_email` 索引（同 oracle schema.go 风格）。
- 写入库前的固定加工链：`cleanAccountPayload(payload)` 全字段 `strings.TrimSpace` → `validateAccountPayload(payload, requireJSON)` 校验 → 敏感字段 `secure.SecureEncrypt(...)` 加密 → 空值走 `nullEmpty(value)` 存 NULL。
- 列表响应一律 `safeAccount(account)` 脱敏：只回 id/name/client_email/默认项目/校验状态/时间戳 + `hasServiceAccountJson` 布尔，绝不回显 JSON 明文。
- 更新时「留空不覆盖」：字段为空则回填当前值（同 updateAccount 逻辑）。
- 响应统一走 `response` 包：`response.OK(w, data)` / `response.Error(w, status, msg)` / `decodeJSON(r, &struct)` / `writeResult(w, data, err)` 快捷函数。

### 4. SQLite Schema

#### `gcp_accounts`

- `id`
- `name`
- `client_email`（明文，用于列表展示与缓存 key）
- `default_project_id`（明文，用户设置的默认项目上下文；SA JSON 内的 `project_id` 仅解析校验用，不单独存列）
- `service_account_json_encrypted`
- `description`
- `last_verified_at`
- `last_verify_status`
- `last_verify_error`
- `created_at`
- `updated_at`

幂等 `CREATE TABLE IF NOT EXISTS gcp_accounts` + client_email 索引。实例/磁盘等资源数据实时查询，第一版不落库缓存。

### 5. 凭证加密

- `service_account_json_encrypted` 使用 `secure.SecureEncrypt`。
- 读取到 REST client 前临时解密，token 换取后即丢弃 JSON 明文（除保留必要字段内存缓存）。
- 列表接口走 `safeAccount` 脱敏（掩码 + hasServiceAccountJson 布尔）。

### 6. 模块内部按职责拆分

```text
backend-go/internal/gcp/
├── service.go          # Service + New + ServeHTTP 分发 + open()
├── schema.go           # gcp_accounts 幂等建表
├── account_store.go    # 账号 CRUD + secure 加密 + safeAccount
├── types.go            # Account / payload / Normalized* 结构
├── client.go           # gcpRequest REST 封装 + 分页处理
├── auth.go             # JWT 签名 + token 交换 + 内存缓存
├── compute_service.go  # instances/zones/machineTypes/images/labels
├── project_service.go  # Resource Manager projects
├── storage_service.go  # Cloud Storage 桶/对象（Phase 3）
├── billing_service.go  # billingAccounts/budgets（Phase 3）
├── monitoring_service.go # Cloud Monitoring 模型用量（Phase 5）
└── service_test.go
```

### 7. 归一化输出

- 所有列表/详情接口返回 Normalized 结构（驼峰小写），不生硬透传 GCP 原始 JSON 到前端。
- 详情接口可保留受控 `raw` 摘要字段用于调试。
- GCP 状态枚举原样保留（RUNNING/TERMINATED/STAGING…），前端做中文映射。

### 8. 参考实现与文档

- 后端蓝图：`backend-go/internal/oracle/`（提交 5a0f26bd 起稳定形态），本模块接线逐文件对齐它。
- 前端经验来源：`docs/Kumo UI 规则.md`（Kumo-only 组件白名单与 Table 用法）、`docs/前端开发最佳实践.md`、`docs/新模块接入指南.md`、`docs/重构验证与例外清单.md`（品牌色/文件输入等例外登记）。
- 前端接线故障排除：Kumo 组件白名单（Button/Input/Select/Tabs/Table/Dialog/Toasty/Checkbox/Switch/Sidebar/Loader/Tooltip/Popover/Dropdown/DeleteResource）；业务表走 `AppTable columns` 语义列角色（primary/status/datetime/actions-*）、可拖动列用 `Table.ResizeHandle`；删除确认走 `dialog.deleteResource`；长串复制用 `ClipboardText`。

## API Contract Draft

### 账号

- `GET /api/gcp/accounts`
- `POST /api/gcp/accounts`
- `PUT /api/gcp/accounts/{id}`
- `DELETE /api/gcp/accounts/{id}`
- `POST /api/gcp/accounts/{id}/verify`

### 项目

- `GET /api/gcp/accounts/{id}/projects`
- `PUT /api/gcp/accounts/{id}/default-project`

### 基础资源选择器（Phase 2）

- `GET /api/gcp/accounts/{id}/projects/{projectId}/zones`
- `GET /api/gcp/accounts/{id}/projects/{projectId}/machineTypes?zone=`
- `GET /api/gcp/accounts/{id}/projects/{projectId}/images?filter=`
- `GET /api/gcp/accounts/{id}/projects/{projectId}/subnetworks?region=`
- `GET /api/gcp/accounts/{id}/projects/{projectId}/firewalls`
- `GET /api/gcp/accounts/{id}/projects/{projectId}/addresses`

### 实例

- `GET /api/gcp/accounts/{id}/projects/{projectId}/instances`（aggregatedList）
- `GET /api/gcp/accounts/{id}/projects/{projectId}/instances/{instanceId}?zone=`（详情，zone 必填）
- `POST /api/gcp/accounts/{id}/projects/{projectId}/instances/{instanceId}/actions?zone=`（body：`action` = start/stop/reset）
- `DELETE /api/gcp/accounts/{id}/projects/{projectId}/instances/{instanceId}?zone=`（GCP `instances.delete` 仅删实例；磁盘是否随删由各盘 `autoDelete` 决定，前端删除确认中提示受影响磁盘）
- `POST /api/gcp/accounts/{id}/projects/{projectId}/instances`（Phase 2 创建，body 含 zone）
- `POST /api/gcp/accounts/{id}/projects/{projectId}/instances/{instanceId}/labels?zone=`（Phase 2）
- `GET /api/gcp/accounts/{id}/projects/{projectId}/operations/{operationName}`（Operation 状态查询，operationName 形如 `zones/{zone}/operations/...` 或 `global/operations/...`）

### 磁盘（Phase 2）

- `GET /api/gcp/accounts/{id}/projects/{projectId}/disks`（aggregatedList）
- `GET /api/gcp/accounts/{id}/projects/{projectId}/disks/{diskId}?zone=`
- `POST /api/gcp/accounts/{id}/projects/{projectId}/disks/{diskId}/resize?zone=`
- `POST /api/gcp/accounts/{id}/projects/{projectId}/disks/{diskId}/snapshot?zone=`
- `DELETE /api/gcp/accounts/{id}/projects/{projectId}/disks/{diskId}?zone=`

### 存储桶（Phase 3）

- `GET /api/gcp/accounts/{id}/projects/{projectId}/buckets`
- `GET /api/gcp/accounts/{id}/buckets/{bucket}/objects?prefix=`
- `POST /api/gcp/accounts/{id}/buckets`
- `DELETE /api/gcp/accounts/{id}/buckets/{bucket}`
- `POST /api/gcp/accounts/{id}/buckets/{bucket}/objects`（上传）
- `GET /api/gcp/accounts/{id}/buckets/{bucket}/objects/{object}/download-url`（签名 URL）
- `DELETE /api/gcp/accounts/{id}/buckets/{bucket}/objects/{object}`（删除对象）

### 费用（Phase 3）

- `GET /api/gcp/accounts/{id}/billing-accounts`
- `GET /api/gcp/accounts/{id}/projects/{projectId}/billing-info`
- `GET /api/gcp/accounts/{id}/billing-accounts/{billingAccountId}/budgets`

### 模型用量（Phase 5）

- `GET /api/gcp/accounts/{id}/projects/{projectId}/model-usage?days=30`（聚合调用量/token 汇总 + 按天序列 + 按模型/区域分组明细）
- `GET /api/gcp/accounts/{id}/projects/{projectId}/model-metrics?days=7&metric=invocation_count|input_token_count|output_token_count`（可选：单指标原始 time series 明细）

## Request / Response Requirements

统一遵循项目响应风格：

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
  "error": "错误描述",
  "code": "GCP_ERROR"
}
```

额外要求：

- 列表接口返回 Normalized 字段，不透传原始 GCP 对象。
- 列表需支持分页透传（`pageToken`/`nextPageToken`），内部聚合时下发给前端控制。

## Security Requirements

1. GCP 账号所有接口 session auth。
2. SA JSON 加密存储，日志不得打印私钥/JSON 明文/token。
3. 账号列表不回显 JSON。
4. 删除实例、删除磁盘、删除桶、清空对象等高风险操作需确认。
5. 关键动作写入现有操作日志体系。
6. 文档、示例和测试夹具不得写入真实 SA JSON、真实 project ID 网络信息。

## Integration Decisions

1. **不直接并入 `server` 模块**：云资源控制面 vs 主机纳管，职责不同（同 Oracle 决策）。
2. **前端独立 GcpPage**：复用现有云厂商模块范式，风险最小。
3. **后端独立 gcp service**：按资源域拆分，不污染 serveragent。
4. **与模型网关 Vertex AI 上游互不影响**：网关 Vertex 上游走 `x-goog-api-key`（API key），GCP 模块走 SA OAuth，凭证与模块分离；模型用量监控同样用 GCP 模块的 SA OAuth 读 Cloud Monitoring，与网关模型调用统计互相独立、不混用。
5. **未来「导入到主机模块」弱集成**：GCP 实例后续纳管 SSH/Agent，但不作为第一版前置。

## Observability and Logging

记录以下行为（操作日志体系）：

- 新增/编辑/删除账号、验证账号
- 切换默认项目
- 实例生命周期动作（start/stop/reset/delete）
- 磁盘删除/快照/扩容
- 桶创建/删除、对象上传/删除
- 模型用量查询（Phase 5，只读查询动作；不记录返回的指标明细）

日志字段：账号 ID、project ID、资源 ID、动作类型、结果、错误摘要。不含 SA JSON、私钥、token。

## Testing Decisions

### 后端测试

- schema 初始化幂等
- SA JSON 解析与校验（缺字段、坏 JSON、坏私钥）
- JWT 构造与签名（固定测试密钥对，验证 header/payload）
- token 交换 mock（成功/失败/过期刷新）
- gcpRequest 分页聚合
- 路由分发（accounts/projects/instances/actions 正确落到对应 handler）
- 实例归一化（raw GCP JSON → Normalized）
- 生命周期动作参数校验与错误分支
- 账号 CRUD 加解密往返
- 安全：safeAccount 不含敏感字段

### 前端测试

- 账号表单校验（JSON 格式、必填）
- 空态/加载态/失败态
- 实例列表筛选
- 动作确认弹窗
- Tabs 切换与项目联动刷新

### 联调与验收

- `npm run governance:check`
- `node tools/backend-route-inventory.mjs`
- `npm run backend-go:test`
- `npm run lint`、`npm test`
- 浏览器 smoke：登录 → 进入 GCP 页面 → 添加账号 → 查看项目/实例 → 执行动作 → 详情

## Release Plan

### Milestone 1：架构接入

- 前端注册 gcp 模块（store.js / MainLayout.jsx）
- 后端注册 /api/gcp（manifest.go / server.go）
- gcp_accounts schema + secure 加密

### Milestone 2：账号与项目

- 账号 CRUD + 验证（SA JSON → token → projects.list）
- 项目列表与默认项目切换
- 页面空态与账号管理

### Milestone 3：实例管理

- 实例列表（aggregatedList）
- 实例详情（磁盘/网络接口/标签）
- start/stop/reset/delete

### Milestone 4：资源选择器与创建

- zones/machineTypes/images/subnetworks 选择器
- 创建实例表单
- labels/metadata 编辑
- 防火墙规则查看、静态 IP 查看

### Milestone 5：存储与费用

- 桶列表/详情/创建/删除
- 对象浏览/上传/下载/删除
- billingAccounts/budgets/项目计费信息

### Milestone 6：体验完善

- 导入 server 模块草稿（弱集成）
- Cron/日报只读白名单
- 错误提示与操作日志完善
- 保存常用筛选（可选）

### Milestone 7：模型用量监控（Phase 5）

- monitoring base URL + monitoring_service.go（timeSeries.list 封装）
- 模型用量汇总/曲线/明细 API + 前端 Tab 区块
- SA 需补 `roles/monitoring.viewer` + 启用 monitoring API 的验证与提示

## Acceptance Criteria

1. 侧边栏可见「Google Cloud」模块入口，位于云厂商分组。
2. 可成功新增 GCP 账号（SA JSON）并验证通过。
3. 可见项目列表并切换默认项目。
4. 实例列表可见状态、机型、zone、公私网 IP。
5. 可对实例执行启动、停止、重启。
6. 可删除实例，操作前有强确认与 boot disk 处理说明。
7. 可查看实例磁盘附加与网络接口信息。
8. (Phase 2) 可创建实例、编辑标签。
9. (Phase 2) 可查看防火墙规则与静态 IP。
10. (Phase 3) 可查看存储桶列表与桶内对象，删除空桶。
11. (Phase 3) 可查看 billingAccounts、预算与项目计费信息。
12. (Phase 5) 可查看 Vertex AI 模型调用量汇总与近 30 天趋势曲线。
13. (Phase 5) 可按模型/区域查看用量明细，缺 monitoring 权限时给出明确提示。
14. SA JSON 不以明文持久化到数据库。
15. 所有 /api/gcp 接口进入 manifest 和 Go route 分发体系。
16. 页面使用 Kumo 组件，中文 UI 完整，样式无漂移。
17. 相关测试和治理命令通过。

## Risks

1. GCP REST 响应字段多、变化快，若直接透传原始 JSON 会难维护——统一归一化层缓解。
2. GCP 实例操作异步，前端误把请求成功当资源已变更——Operation 语义 + 状态刷新缓解。
3. SA JSON 权限过大会带来安全事故——依赖 GCP 侧最小 IAM 角色实践 + 文档指引。
4. 费用曲线依赖 BigQuery 导出（非通用 REST）——Phase 3 降级为 budgets + billing-info 概览。
5. 分页/聚合（aggregatedList per-zone items）处理复杂——client 层统一聚合缓解。
6. 沿用明文凭证存储模式会引入安全债务——第一版强制 secure 加密。
7. 把 GCP 做进 server 模块会混淆云资源管理与主机纳管边界——独立模块决策（ADR-0002）。
8. 模型用量依赖 Cloud Monitoring 时序指标与 `roles/monitoring.viewer`，SA 缺权限或监控数据延迟（分钟级）时显示为空——通过「指标为空/权限缺失」两类空态提示 + 前端说明缓解。

## Out of Scope

1. Cloud SQL、GKE、BigQuery、Functions、Pub/Sub 等 PaaS 管理与生命周期操作；Vertex AI/Agent Platform 仅允许 Phase 5 的只读用量监控，不做模型/Agent 部署管理。
2. 自动将 GCP 实例注册进 server_accounts。
3. IAM 角色/服务账号管理、组织/文件夹层级管理。
4. 浏览器内 gcloud/Cloud Shell 终端。
5. 多账号间细粒度协作权限。
6. BigQuery 费用导出深度分析（第一版只做 budgets/billing-info 概览）。

## Further Notes

1. GCP 模块与 Oracle 模块共同构成「REST 优先 + 凭证加密 + 资源归一化」新模板的实践（Oracle 是 SDK 优先变体）。
2. 模块落地后可反向推动 aliyun/tencent/koyeb 的凭证加密与 service 拆分治理。
3. 第一版优先把「看得到、控得住、报错清楚」做好，再扩展到创建实例、存储、费用。
4. 后续若需为 GCP 资源做告警（uptime 集成）或成本治理，可在本模块数据面之上演进，不影响现有架构。
