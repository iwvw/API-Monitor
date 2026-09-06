# Huawei Cloud（华为云）云资源管理模块 PRD

最后更新：2026-09-06

## Problem Statement

API Monitor 已覆盖 Cloudflare、阿里云、腾讯云、Oracle OCI、Google Cloud、Koyeb、Fly.io 等云厂商，但缺少华为云（Huawei Cloud）支持。华为云拥有 ECS（弹性云服务器）、Flexus L 轻量应用服务器、云解析 DNS、VPC/EIP、OBS 对象存储、BSS 费用等广泛产品线，其 AK/SK 凭证模式和 REST API 体系（`SDK-HMAC-SHA256` 签名）与已接入厂商有显著差异。

这带来几个问题：

1. 使用华为云的用户需要在华为云控制台、CLI 和 API Monitor 之间切换，无法统一面板管理。
2. 华为云的 AK/SK 凭证、region/project 项目维度资源组织方式、国内站/国际站域名差异，与现有模块（OCID、SA JSON、API token）均不同，无法直接套用现有账号表单。
3. 华为云 Flexus L 轻量应用服务器没有独立 API，是「云主机 + 弹性公网IP + 云硬盘 + 云备份 + 主机安全」的组合服务（列表走配置审计 RMS、云主机操作走 ECS），模块需要处理这种组合语义。
4. OBS 对象存储使用独立的签名协议（非 `SDK-HMAC-SHA256`），是华为云接入中唯一的签名分叉点。
5. 用户无法在 API Monitor 中查看华为云项目、ECS/Flexus 实例、公网 IP、DNS 记录、存储桶和费用信息，更无法执行启停、删除等操作。
6. 项目对云厂商凭证的存储策略不完全一致，华为云模块需要从第一版开始建立 AK/SK 加密存储基线。

用户需要的是一个基于华为云官方 REST API 的、与现有云厂商模块风格一致、覆盖华为云主要功能、可继续扩展的华为云管理模块。

## Goals

1. 在 API Monitor 中新增独立 `huawei` 模块，作为华为云统一管理入口。
2. 使用 REST API + AK/SK 自研签名实现（决策见 [ADR-0003](../adr/0003-华为云模块架构决策.md)），不引入官方 SDK 重型依赖。
3. 覆盖华为云主要功能：账号管理、项目/区域、ECS 实例全生命周期、Flexus L 实例、云解析 DNS、EIP/网络、OBS 存储、费用概览。
4. 保持与现有 Go Manifest 后端、React + Kumo 前端、SQLite 数据层和模块治理方式一致。
5. 凭证从第一版起采用 secure 加密存储，建立华为云侧安全基线。

## Non-Goals

1. 第一版不将华为云实例自动接入 `server` 模块的 Agent/SSH/Docker 体系（后续弱集成）。
2. 不做华为云全产品覆盖：CCE/K8s、RDS、FunctionGraph、CDN、ModelArts 等 PaaS/托管服务的**管理与生命周期操作**不在范围。
3. 不做 IAM 用户/权限/企业项目（EP）管理（权限在华为云侧配置，模块不代行）。
4. 不做 Terraform/IaC 编排、多租户 RBAC。
5. Flexus L 的**续订/退订**不在第一版范围（费用敏感操作），流量包剩余量只读展示可纳入费用 Tab。
6. 不做华为云 CLI（CloudShell）内嵌终端。
7. 不做成本分摊/账单深度分析（第一版只做月度账单概览与资源包使用量）。

## Target Users

1. 使用华为云 ECS/Flexus L 的个人用户和小团队运维者。
2. 希望在统一面板管理多云资产的自托管用户（已有阿里云/腾讯云/Oracle/GCP + 新增华为云）。
3. 管理华为云国内站/国际站节点的管理员。
4. 需要快速查看华为云 DNS、EIP 和 OBS 对象存储的用户。

## Success Metrics

1. 用户可在 5 分钟内完成一个华为云账号接入并看到项目列表。
2. 用户可在 UI 中对 ECS/Flexus 实例执行启动、停止、重启操作。
3. 用户可查看实例的公网/私网 IP、规格、区域、订单信息。
4. 用户可浏览云解析 zone 与记录集、EIP 列表、OBS 桶与对象。
5. 模块接入后不破坏 Go manifest、前端导航和审计命令。
6. SK 凭证默认加密存储，列表不回显。

## Solution

新增独立华为云云厂商模块，模块 ID `huawei`，入口名称「华为云」，位于前端侧边栏「云服务 -> 云厂商」分组。

1. 前端新增 `src/js/pages/HuaweiPage.jsx`，提供账号、项目、实例、Flexus L、DNS、网络、存储、费用视图。
2. 后端新增 `backend-go/internal/huawei/`，REST 封装 + AK/SK 签名 + 按资源域拆分的 service 文件。
3. 路由统一 `/api/huawei/...` 前缀，登记 manifest.go。
4. 高风险操作（删除实例/删除 DNS 记录/删除桶/删除对象/解绑 EIP）使用 `dialog.deleteResource` / Kumo `DeleteResource` 确认。
5. 异步任务（创建实例等返回 `job_id`）前端展示「指令已下发」并靠刷新/任务查询确认。

## Product Scope

### Phase 1：账号接入与实例管理主链路

- 华为云账号（AK/SK + 站点）配置与验证
- 项目列表（IAM `/v3/projects`，自动发现 region + project_id）
- ECS 实例列表（全区域）、详情、搜索/筛选
- ECS 实例生命周期动作：批量启动 / 停止 / 重启 / 删除
- 实例公网/私网 IP、规格、计费模式查看
- Flexus L 实例列表（RMS `all-resources`，`type=hcss.l-instance`）
- Flexus L 云主机详情与启停/重启（复用 ECS API）

### Phase 2：资源选择器与网络

- ECS 规格（flavor）、镜像列表（创建实例选择器）
- 创建实例（基础参数：名称、region、规格、镜像、密码/密钥、磁盘大小）
- 重置密码、修改实例名称
- 弹性公网 IP 列表、绑定/解绑
- VPC 与安全组只读查看

### Phase 3：DNS 与存储

- 云解析 zone 列表/详情/创建/删除
- 记录集（recordset）列表/创建/修改/删除
- OBS 桶列表/详情/创建/删除
- OBS 桶内对象浏览（前缀分页）、上传/下载/删除对象

### Phase 4：费用与体验完善

- BSS 月度账单概览（当月费用、按服务分类）
- Flexus L 流量包剩余量只读展示
- 实例导入 server 模块表单草稿（弱集成）
- Cron/日报只读访问白名单（internalCronReadonlyPrefixes）

## User Stories

1. 作为管理员，我想新增一个华为云账号（填写 AK/SK 并选择站点），以便在系统中管理华为云资源。
2. 作为管理员，我想验证账号是否有效，以便尽早发现 AK/SK、权限或站点选择错误。
3. 作为管理员，我想看到该凭证可访问的项目列表，以便选择要管理的区域。
4. 作为管理员，我想设置默认区域/默认项目，以便减少重复选择。
5. 作为管理员，我想查看项目下所有区域的 ECS 实例列表，以便快速定位目标机器。
6. 作为管理员，我想按名称、状态、区域、IP 搜索实例，以便在实例多时快速筛选。
7. 作为管理员，我想看到实例的公网 IP 和私网 IP，以便连接和资产登记。
8. 作为管理员，我想查看实例规格、计费模式、订单号，以便判断用途和成本。
9. 作为管理员，我想对实例执行启动/停止/重启操作，以便维护或节约资源。
10. 作为管理员，我想删除某个无用实例，以便减少费用，但执行前希望有强确认。
11. 作为管理员，我想查看 Flexus L 实例列表和套餐内资源（云主机、流量包），以便管理轻量服务器。
12. 作为管理员，我想对 Flexus L 的云主机执行启停/重启，以便恢复或维护服务。
13. 作为管理员，我想查看云解析 zone 与记录集，以便管理域名解析。
14. 作为管理员，我想新增/修改/删除 DNS 记录，以便调整解析（Phase 3）。
15. 作为管理员，我想查看弹性公网 IP 列表与绑定状态，以便清理闲置资源（Phase 2）。
16. 作为管理员，我想浏览 OBS 桶与桶内对象，以便管理对象存储资产（Phase 3）。
17. 作为管理员，我想看到当月费用和按服务分类明细，以便控制支出（Phase 4）。
18. 作为管理员，我想在失败时看到华为云返回的具体错误摘要，以便修正配置。
19. 作为管理员，我想让所有高风险操作写入操作日志，以便审计。
20. 作为管理员，我想 UI 与现有云厂商模块一致且全中文，以便降低学习成本。

## Functional Requirements

### 1. 模块入口与导航

- 模块 ID：`huawei`
- 前端导航文案：`华为云`
- 分组位置：`云服务 -> 云厂商`
- 路由路径：`/huawei`
- 页面文件：`src/js/pages/HuaweiPage.jsx`

### 2. 华为云账号管理

支持 AK/SK 账号的新增、编辑、删除、验证、列表。字段：

- `name`：账号备注名
- `site`：站点枚举 `cn`（国内站，默认）/ `intl`（国际站），决定 API 域名后缀 `.myhuaweicloud.cn` / `.myhuaweicloud.com`
- `accessKeyId`：Access Key ID（明文存储，列表脱敏展示）
- `secretAccessKey`：Secret Access Key（`secure.SecureEncrypt` 加密存储，永不明文回显）
- `defaultRegion`（可选）：默认区域（如 `cn-north-4`）
- `defaultProjectId`（可选）：默认项目 ID（验证时自动发现或用户指定）
- `description`（可选）

规则：

- SK 必须使用 `secure.SecureEncrypt` 加密存储，禁止明文。
- 列表页不回显 SK，AK 做部分掩码（如 `HPUA***TS6Q`），展示名称、站点、默认区域、默认项目、创建时间、验证状态。
- 编辑时「不修改 SK 则留空」。
- 验证：AK/SK 签名调用 `GET /v3/projects`（站点对应 IAM 域名）→ 返回项目列表即成功；失败原因（AK/SK 错误、站点选择错误、无项目权限）分类展示。
- 验证成功后自动把发现的第一个项目写为默认项目（若未设置），并刷新 `last_verified_at`。
- 存入 `huawei_accounts` 表（schema 见「数据库设计」）。

### 3. 项目/区域管理

- `GET /v3/projects`（IAM）列出凭证可见的项目（region 名称 + project_id + domain_id）。
- 支持在账号内切换/保存默认区域与默认项目。
- 展示：region 名（如 `cn-north-4`）、project_id、所属 domain_id。
- 资源查询路径统一携带 project_id：`/api/huawei/accounts/{id}/projects/{projectId}/instances` 等。

### 4. ECS 实例列表

- 使用 `GET /v1/{project_id}/cloudservers/detail` 拉取实例，返回标准化列表。
- 至少展示：名称、ID、状态（ACTIVE/SHUTOFF/REBOOT/…）、规格、区域、公网 IP、私网 IP、计费模式（postPaid/prePaid）、创建时间。
- 支持按名称/状态/区域/IP 筛选（前端本地筛选为主）。
- 空态、加载态、失败态必须清晰。支持刷新。

### 5. ECS 实例详情

- 基础信息：名称、ID、规格（vcpu/内存/规格编码）、镜像、计费模式、区域、创建时间、订单号（包年包月）。
- 网络接口：VPC、子网、内网 IP、弹性公网 IP（含带宽）。
- 磁盘信息：系统盘/数据盘大小、类型。
- 安全组摘要、密钥对名称。

### 6. ECS 实例生命周期动作

Phase 1 支持（均为批量接口语义，单实例也走批量）：

- 批量启动：`POST /v1/{project_id}/cloudservers/action`，body `{"os-start":{"servers":[{"id":"..."}]}}`
- 批量停止：`POST .../action`，body `{"os-stop":{"servers":[...]}}`
- 批量重启：`POST .../action`，body `{"reboot":{"servers":[...],"type":"SOFT|HARD"}}`
- 删除：`DELETE /v1/{project_id}/cloudservers/{server_id}?delete_publicip=true&delete_volume=TRUE`

规则：

- 启停/重启返回同步结果；前端 toast 成功/失败 + 列表刷新。
- `delete` 走 DeleteResource 确认，需勾选说明（是否随删公网 IP 与云硬盘）。
- 动作执行中状态（REBOOT/STOPPING 等）由状态字段自然反映，不额外实现长轮询。

### 7. Flexus L 实例

Flexus L 是组合服务（云主机 + EIP + 云硬盘 + 云备份 + 主机安全），无独立 API：

- **实例列表**：`GET /v1/resource-manager/domains/{domain_id}/all-resources?type=hcss.l-instance&region_id=...&marker=...&limit=200`（配置审计 RMS，`rms.myhuaweicloud.cn` 全局端点），返回套餐 ID、名称、区域、规格编码、订单号、计费模式、套餐内资源（云主机 `physical_resource_id`、流量包）。
- **云主机操作**：从套餐 `properties.resources` 提取 `logical_resource_type == "huaweicloudinternal_ecs_instance"` 的 `physical_resource_id`，复用 ECS API（详情/启停/重启/重置密码）。
- 展示：套餐名、套餐 ID、区域、规格编码（如 `hf.large.1.40g.30m.linux`）、计费模式（Flexus 仅包年包月 prePaid）、订单号、创建时间、云主机状态。
- Flexus L 与普通 ECS 在同一「计算实例」Tab 内用子标签区分，或独立 Flexus L Tab（见前端设计）。

### 8. 云解析 DNS（Phase 3）

- Zone 列表/详情：`GET /v2/zones`、`GET /v2/zones/{zone_id}`；创建 `POST /v2/zones`；删除 `DELETE /v2/zones/{zone_id}`。
- 记录集：`GET /v2/zones/{zone_id}/recordsets`（支持 `marker/limit` 分页）；创建 `POST /v2/zones/{zone_id}/recordsets`；修改 `PUT /v2/zones/{zone_id}/recordsets/{recordset_id}`；删除 `DELETE /v2/zones/{zone_id}/recordsets/{recordset_id}`。
- Zone 至少展示：名称、类型（public/private）、状态、记录数、创建时间。
- 记录集展示：名称、类型（A/AAAA/CNAME/MX/TXT/…）、TTL、记录值列表、权重。
- 删除 zone 与删除记录集均需确认。

### 9. 弹性公网 IP / 网络（Phase 2）

- EIP 列表：`GET /v1/{project_id}/publicips`；绑定 `PUT /v1/{project_id}/publicips/{id}/associate-instance`；解绑 `PUT /v1/{project_id}/publicips/{id}/disassociate-instance`。
- VPC 列表：`GET /v1/{project_id}/vpcs`（只读）。
- 安全组：`GET /v1/{project_id}/security-groups`（只读）。
- EIP 至少展示：公网 IP、绑定状态（FREE/ELB/ECS）、带宽、区域。
- 解绑/删除 EIP 需确认。

### 10. OBS 对象存储（Phase 3）

- 桶列表：`GET https://obs.{region}.myhuaweicloud.cn/`（XML，列出该 region 桶）。
- 桶对象浏览：`GET https://{bucket}.obs.{region}.myhuaweicloud.cn/?prefix=...&max-keys=1000`。
- 桶创建：`PUT https://obs.{region}.myhuaweicloud.cn/{bucket}`；桶删除：`DELETE ...`（仅空桶，确认）。
- 对象上传（PUT 流式）、下载（代理流或签名 URL）、删除（确认）。
- OBS 使用独立签名器（`obs_auth.go`），字段结构与 ECS/DNS 归一化规则一致。

### 11. 费用概览（Phase 4，可选）

- 月度账单：BSS `bss.myhuaweicloud.cn`（域名已确认可达，具体接口路径以官方 API 文档校准后实现）。
- Flexus L 流量包剩余量：`POST /v2/bills/free-resources/usages`（BSS，路径以官方文档校准）。
- 展示：当月总费用、按服务分类、Flexus 流量包剩余量与有效期。

### 12. 可观察性要求

- 记录：账号增删改、验证、项目切换、实例动作、DNS 变更、桶操作、EIP 绑定。
- 日志不包含 SK 明文、AK/SK 签名中间值。
- 错误响应把华为云 error 原因转成「简短中文摘要 + 原始详情」。

## UX Requirements

1. 页面主体风格与 `GcpPage`、`OraclePage` 保持一致。
2. 账号管理与资源管理同页 Tabs：`计算实例`、`Flexus L`、`域名解析`、`网络`、`存储`、`费用`、`账号管理`。
3. 资源视图全局头部：账号选择器 + 区域/项目选择器 + 刷新按钮。
4. 项目选择器切换后，资源列表联动刷新。
5. 全部中文文案，Kumo 组件（Button/Input/Select/Tabs/Table/Dialog/DeleteResource/Toasty/Loader/Tooltip/Popover/Dropdown）。
6. 删除/终止动作使用 `dialog.deleteResource`（迁 Kumo `DeleteResource`）。
7. AK/SK 输入区：AK 单行输入 + SK 密码框输入（粘贴支持），站点用 Select（国内站/国际站）。
8. 长字符串（实例 ID、project_id、桶名、记录集 ID）支持复制（ClipboardText）。
9. 实例动作按钮组：启动/停止/重启/删除，按状态显隐（ACTIVE 显示停止/重启，SHUTOFF 显示启动/删除）。
10. 异步/批量操作 toast 文案区分「已下发」与「操作失败」。
11. 空态、加载态、失败态、窄屏、暗色主题全覆盖（见《新模块接入指南》第 7 节）。

## 前端设计细节

本节吸收 GcpPage/OraclePage 已落地的实现经验（`src/js/pages/GcpPage.jsx`、`src/js/pages/OraclePage.jsx`、`src/js/modules/tableLayout.js`、`docs/语义化表格布局与移动端适配.md`），华为云页面直接复用以下模式，不另起炉灶。

### 1. 页面外层结构与 Tab 栏

- 根容器用 `<PageStack>`，内为 sticky 顶栏 + 各 Tab 条件渲染块。
- 顶栏模式（参照 GcpPage.jsx:1395-1412）：

```jsx
<div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
  <Tabs {...MODULE_TABS_PROPS} value={activeTab} onValueChange={setActiveTab} tabs={tabs} />
  {activeTab !== 'accounts' && (
    <div className="flex items-center gap-2">
      <Select alignItemWithTrigger size="sm" aria-label="华为云账号" value={selectedAccountId} options={...} />
      <Select alignItemWithTrigger size="sm" aria-label="区域/项目" value={selectedProjectId} options={...} />
      <Button type="button" size="sm" variant="secondary" onClick={refresh}><RefreshCw className="h-4 w-4" /></Button>
    </div>
  )}
</div>
```

- `MODULE_TABS_PROPS` 来自 `src/js/modules/kumoTabs.js`（`variant: 'segmented'` + 响应式 className）。
- Tabs 数组每项 `label` 为 `<span className="inline-flex items-center gap-1.5"><Icon className="h-3.5 w-3.5" />中文</span>`，账号管理放最后。
- 切换账号时重置项目/资源状态（参照 GcpPage 切换账号逻辑）。

### 2. Tab 与内容布局

- 默认 Tab：`计算实例`。
- Tab 清单（含图标）：`compute` 计算实例 / `flexus` Flexus L / `dns` 域名解析 / `network` 网络 / `storage` 存储 / `billing` 费用 / `accounts` 账号管理。
- 计算实例、Flexus L 采用「列表 + 详情」双栏 master-detail（参照 OraclePage.jsx:999-1128）：`<div className="grid min-h-0 gap-4 cq-xl:grid-cols-[minmax(0,1.45fr)_minmax(360px,0.75fr)]">`，左 `SectionCard title="实例列表"`，右 `SectionCard title="实例详情"`（`DetailGrid`，`divide-y divide-kumo-line/80` + `grid grid-cols-[108px_minmax(0,1fr)] px-4 py-2.5`）。
- DNS、网络、存储用「工具列 + 表格」单栏布局。
- 内容块间距统一 `flex flex-col gap-3`（页面级）与 `gap-4`（SectionCard 间），双栏网格 `gap-4`，弹窗表单 `flex flex-col gap-3`。

### 3. 表格设计（AppTable 语义列）

- 业务表一律用 `AppTable`（`src/js/components/ui/AppPrimitives.jsx` 的 `AppTable columns` 组合层），不新增基础 Table 实现。
- columns 数组声明语义列角色（`resolveTableColumns` 自动解析 colgroup/minWidth/对齐），角色取值见 `TABLE_COLUMN_ROLES`（tableLayout.js）。
- 实例表列定义样例（参照 GcpPage INSTANCE_TABLE_COLUMNS）：

```jsx
const INSTANCE_TABLE_COLUMNS = [
  { id: 'name', role: 'primary', width: 200, minWidth: 160 },
  { id: 'status', role: 'status' },
  { id: 'publicIp', role: 'identifier', width: 180, minWidth: 150 },
  { id: 'flavor', role: 'meta', width: 160 },
  { id: 'region', role: 'meta', grow: 1, minWidth: 140 },
  { id: 'createdAt', role: 'datetime' },
  { id: 'actions', role: 'actions-lg', width: 150 },
];
```

- Flexus L 表列：`name` primary / `status` status / `region` meta / `specCode` meta / `orderId` identifier / `createdAt` datetime / `actions` actions-md。
- DNS zone 表列：`name` primary / `type` type / `records` count / `status` status / `createdAt` datetime / `actions` actions-md。
- EIP 表列：`publicIp` primary / `status` status / `bandwidth` number / `region` meta / `createdAt` datetime / `actions` actions-md。
- OBS 桶表列：`name` primary / `region` meta / `storageClass` type / `createdAt` datetime / `actions` actions-md。
- 规则：每表至少一个弹性列（primary/content/identifier）；固定工具列（checkbox/操作列）不参与剩余空间放大；宽表不得由单个主列吞掉全部余量。
- 表格渲染模式（参照 GcpPage:1431-1494）：loading 时骨架表格（`DataTableFrame variant="embedded" density="dense"` + AppTable 填 3 行 `SkeletonLine`）；数据空时 `EmptyState card={false} icon={X}`；数据表固定 `className="min-h-0 flex-1 overflow-auto rounded-none border-0 scrollbar-thin"`，`Table.Header variant="compact"`。
- 主名称列用 ghost Button（`className="block h-auto max-w-full truncate px-1.5 py-0.5 font-semibold text-kumo-strong"`）点击开详情/抽屉。
- 行选中用 `Table.Row variant="selected"`（master-detail 联动）。

### 4. 选择器与表单

- 账号/区域选择器用 Kumo `Select size="sm" alignItemWithTrigger`，带 `aria-label`（无可见 label 时）。
- 账号选项 `accounts.map(a => ({value: String(a.id), label: a.name}))`，项目选项 `projects.map(p => ({value: p.projectId, label: p.name || p.projectId}))`。
- 账号表单弹窗（新增/编辑）：`grid gap-3 cq-md:grid-cols-2` + `Input label=...`，站点用 `Select`，SK 用 `Input type="password"`；编辑时 SK 留空表示不更换。
- 弹窗固定 `className="@container !w-[min(38rem,calc(100vw-2rem))] p-6"`（同 GcpPage Dialog 样式）。
- 详情抽屉复用 Dialog（`KeyValueGrid` 展示基础信息/网络接口/磁盘/安全组）。

### 5. 删除确认

- 删除账号/实例/DNS 记录/桶/对象/EIP 全部走 `dialog.deleteResource`（`src/js/modules/dialog.js`）：

```jsx
const ok = await dialog.deleteResource({
  title: '删除实例',
  message: `确定删除实例「${server.name}」吗？${includeIp ? '将同时释放公网 IP。' : ''}${includeDisk ? '将同时删除云硬盘。' : ''}`,
  confirmLabel: '删除实例',
});
if (!ok) return;
```

- 实例删除弹窗需展示随删选项（`Switch`：同时释放公网 IP / 同时删除云硬盘），默认勾选安全项。
- 后续按《Kumo UI 规则》逐步迁向 Kumo `DeleteResource`（`resourceType/resourceName/onDelete/isDeleting`）。

### 6. 空态 / 加载 / 失败态

- 加载：表格骨架（`SkeletonLine` 行）或 `Loader`，不用本地自绘 loader。
- 空态：`EmptyState card={false} icon={X} title description className="min-h-64"`；账号表空态也可用行内占位（`Table.Row > Table.Cell colSpan={n} className="py-10 text-center text-kumo-subtle"`）。
- 失败：统一 `toast.error(error.message || '...')`；账号未选/未配置时给出引导文案。
- 窄屏：内容区仅在表格框架内横向滚动（页面级禁止横滚），操作列垂直居中，触摸目标不小于 44px。

### 7. 移动端与暗色主题

- 移动端遵循《语义化表格布局与移动端适配.md》：essential 列始终显示，supporting 列平板显示，optional 列可隐藏但必须有详情/复制入口。
- 暗色主题：全部用 Kumo token（`bg-kumo-*`/`text-kumo-*`/`border-kumo-*`），不硬编码颜色；品牌图标例外登记到《重构验证与例外清单.md》。

### 8. 接线改动（Frontend Registration Template）

参照 GCP PRD 的接线清单，华为云需改 4 个文件：

- `src/js/store.js`：`MODULE_CONFIG` 加 `huawei: { name: '华为云', shortName: 'Huawei', icon: 'fa-cloud', description: '华为云资源' }`；`MODULE_GROUPS` 的 `cloud-vendors.subgroups.modules` 数组末尾追加 `'huawei'`。
- `src/js/components/MainLayout.jsx`：lazy import `const HuaweiPage = lazy(() => import('../pages/HuaweiPage.jsx'));`；`renderActivePage()` 加 `case 'huawei': return <HuaweiPage />;`；`stickyHeaderScrollModule` 数组补 `'huawei'`。
- `src/js/components/Icons.jsx`：`MODULE_ICON_MAP` 加 `huawei: Cloud`（先用 Cloud 兜底，simple-icon 就绪后替换；品牌色属允许例外组）。
- 路由路径 `/huawei`，页面文件 `src/js/pages/HuaweiPage.jsx`（规模大时拆 `src/js/pages/huawei/` 目录）。

## 数据库设计

### `huawei_accounts` 表

幂等建表语句（schema.go，参照 gcp_accounts/oracle_accounts 模式）：

```sql
CREATE TABLE IF NOT EXISTS huawei_accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    site TEXT NOT NULL DEFAULT 'cn',
    access_key_id TEXT NOT NULL,
    secret_access_key_encrypted TEXT NOT NULL,
    domain_id TEXT,
    default_region TEXT,
    default_project_id TEXT,
    description TEXT,
    last_verified_at DATETIME,
    last_verify_status TEXT,
    last_verify_error TEXT,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_huawei_accounts_created_at ON huawei_accounts(created_at);
CREATE INDEX IF NOT EXISTS idx_huawei_accounts_access_key_id ON huawei_accounts(access_key_id);
```

字段说明：

| 字段 | 类型 | 说明 |
|---|---|---|
| `id` | INTEGER PK | 自增主键 |
| `name` | TEXT NOT NULL | 账号备注名 |
| `site` | TEXT NOT NULL DEFAULT 'cn' | 站点：`cn` 国内站 / `intl` 国际站，驱动域名后缀 |
| `access_key_id` | TEXT NOT NULL | AK，明文存储（账号标识），列表脱敏展示 |
| `secret_access_key_encrypted` | TEXT NOT NULL | SK，`secure.SecureEncrypt` 加密存储，永不明文回显 |
| `domain_id` | TEXT | IAM 账号域 ID（验证时从 projects 返回的 domain_id 提取） |
| `default_region` | TEXT | 默认区域，如 `cn-north-4` |
| `default_project_id` | TEXT | 默认项目 ID，验证自动发现或用户指定 |
| `description` | TEXT | 备注 |
| `last_verified_at` | DATETIME | 最近验证时间 |
| `last_verify_status` | TEXT | 最近验证状态（success/failed/never） |
| `last_verify_error` | TEXT | 最近验证错误摘要（不含凭证明文） |
| `created_at` | DATETIME | 创建时间，默认 CURRENT_TIMESTAMP |
| `updated_at` | DATETIME | 更新时间，默认 CURRENT_TIMESTAMP |

设计要点：

- 索引覆盖 `created_at` + 业务键 `access_key_id`（同 oracle 的 region 索引模式）。
- 资源数据（实例/DNS/EIP/桶）实时查询，第一版不落库缓存（同 GCP）。
- 域名后缀由 `site` 推导，不单独存域名。

## 后端技术设计

### 1. Service 结构、New 与 ServeHTTP（对齐 GCP/Oracle 蓝图）

```go
type Service struct {
    cfg     config.Config
    store   *database.Store
    schema  database.SchemaEnsurer
    http    *http.Client
    signers *signerCache   // 进程内 AK/SK 签名器缓存（可选）
}
```

- `New(cfg config.Config)`：构造 Service、`store: database.New(cfg)`；启动时 5s 超时 `open()` 预检建表（失败仅 warn，同 GCP）。
- `ServeHTTP`：`strings.TrimPrefix(r.URL.Path, "/api/huawei")` → Trim → Split 按段落数 + 首段 + Method switch 分发（OBS 对象名含 `/` 时用 `EscapedPath` 逐段 `url.PathUnescape`，同 GCP）。
- handler 骨架：`accountForRequest`（解析 ID → open DB → getAccount，`sql.ErrNoRows` 返回 404）→ `clientForAccount`（解密 SK 构造带签名的 client）→ domain 方法 → `writeResult`。

### 2. account_store 与安全脱敏

- `ensureSchema` 幂等建表（DDL 见上）。
- 写入库前固定加工链：`cleanAccountPayload`（TrimSpace 各字段）→ `validateAccountPayload`（必填校验）→ SK `secure.SecureEncrypt(...)` 加密 → 空值走 `nullEmpty` 存 NULL。
- 列表响应一律 `safeAccount(account)` 脱敏：只回 id/name/site/掩码 AK/domain_id/default_region/default_project_id/验证状态/时间戳 + `hasSecretAccessKey` 布尔，绝不回显 SK。
- 更新时「留空不覆盖」：SK 留空则回填当前值（同 updateAccount 逻辑）。
- 响应统一走 `response` 包：`response.OK(w, data)` / `response.Error(w, status, msg)` / `writeResult(w, data, err)`。

### 3. client.go：REST 封装

- base URL 映射表按 site + region 推导：

```go
iam:  https://iam.myhuaweicloud.{suffix}            // 全局
ecs:  https://ecs.{region}.myhuaweicloud.{suffix}
dns:  https://dns.{region}.myhuaweicloud.{suffix}
vpc:  https://vpc.{region}.myhuaweicloud.{suffix}
rms:  https://rms.myhuaweicloud.{suffix}            // 全局（实测无 region 前缀可用）
bss:  https://bss.myhuaweicloud.{suffix}            // 全局
obs:  https://obs.{region}.myhuaweicloud.{suffix}   // 独立签名
```

- 支持 `HUAWEI_<API>_API_BASE_URL` 环境变量逐端点覆盖（测试指向 mock server，同 GCP）。
- `huaweiRequest(ctx, site, region, service, path, query, body)`：拼 URL → AK/SK 签名（`auth.go`）→ 超时 30s → 响应用 `io.LimitReader` 限流（8MB）→ 非 2xx 解析华为云错误体 `{error_code, error_msg}`，转「中文摘要 + 原始 error_msg」。
- 分页：列表接口按服务透传 `marker/limit` 或 `offset/limit`，不做强制聚合（RMS 用 marker，DNS 用 marker，ECS 一次返回全量 + count）。

### 4. auth.go：SDK-HMAC-SHA256 签名

按 ADR-0003 第 8 节实现：

- canonicalURI：路径逐段 escape（保留 `A-Za-z0-9_-~.`，其余 `%XX` 大写），末尾无 `/` 则补 `/`（**仅签名用**）。
- canonicalQueryString：key 排序、值 escape，`key=value&...`。
- canonicalHeaders：`host:{host}\nx-sdk-date:{date}\n`（host 小写无端口）。
- signedHeaders：`host;x-sdk-date`。
- contentHash：JSON body 取 `sha256(body)`；空 body 为 `sha256("")`；非 application/json 时为 `UNSIGNED-PAYLOAD`。
- stringToSign：`SDK-HMAC-SHA256\n{x-sdk-date}\nsha256(canonicalRequest)`。
- signature：`HMAC-SHA256(SK, stringToSign)`（直接用 SK，无派生链）。
- Header：`Authorization: SDK-HMAC-SHA256 Access={AK}, SignedHeaders=host;x-sdk-date, Signature={sig}`。

### 5. obs_auth.go：OBS 独立签名

Phase 3 实现。OBS 用独立签名协议（AK/SK 参与 + 内容哈希 + `Date`/`x-obs-date`），实现时对照官方 `huaweicloud-sdk-go-obs` 校准。

### 6. 模块内部按职责拆分

```text
backend-go/internal/huawei/
├── service.go            # Service + New + ServeHTTP 分发 + open()
├── schema.go             # huawei_accounts 幂等建表
├── account_store.go      # 账号 CRUD + secure 加密 + safeAccount
├── types.go              # Account / payload / Normalized* 结构
├── client.go             # huaweiRequest REST 封装 + base URL 映射 + 分页
├── auth.go               # AK/SK SDK-HMAC-SHA256 签名
├── obs_auth.go           # OBS 独立签名器（Phase 3）
├── compute_service.go    # ECS 实例 + 生命周期动作
├── flexus_service.go     # Flexus L（RMS + ECS 复用）
├── dns_service.go        # 云解析 zone/recordset
├── network_service.go    # EIP/VPC/安全组
├── storage_service.go    # OBS 桶/对象（Phase 3）
├── billing_service.go    # BSS 费用（Phase 4 可选）
└── service_test.go
```

### 7. 归一化输出

- 所有列表/详情接口返回 Normalized 结构（驼峰小写），不生硬透传华为云原始 JSON。
- 详情接口可保留受控 `raw` 摘要字段用于调试。
- 华为云状态枚举原样保留（ACTIVE/SHUTOFF/REBOOT/…），前端做中文映射。

### 8. 参考实现与文档

- 后端蓝图：`backend-go/internal/gcp/`（REST + token 模式，最近实践）、`backend-go/internal/oracle/`（多字段加密 + 脱敏模式）、`backend-go/internal/aliyun/`（自研 RPC 签名）。
- 前端经验来源：`docs/Kumo UI 规则.md`、`docs/前端开发最佳实践.md`、`docs/新模块接入指南.md`、`docs/语义化表格布局与移动端适配.md`、`docs/重构验证与例外清单.md`。

## API Contract Draft

### 账号

- `GET /api/huawei/accounts`
- `POST /api/huawei/accounts`
- `PUT /api/huawei/accounts/{id}`
- `DELETE /api/huawei/accounts/{id}`
- `POST /api/huawei/accounts/{id}/verify`

### 项目/区域

- `GET /api/huawei/accounts/{id}/projects`
- `PUT /api/huawei/accounts/{id}/defaults`（body：`defaultRegion` / `defaultProjectId`）

### ECS 实例

- `GET /api/huawei/accounts/{id}/projects/{projectId}/instances`
- `GET /api/huawei/accounts/{id}/projects/{projectId}/instances/{serverId}`
- `POST /api/huawei/accounts/{id}/projects/{projectId}/instances/actions`（body：`action` = start/stop/reboot，`serverIds` 数组，批量语义）
- `DELETE /api/huawei/accounts/{id}/projects/{projectId}/instances/{serverId}?deletePublicIp=&deleteVolume=`
- `PUT /api/huawei/accounts/{id}/projects/{projectId}/instances/{serverId}`（改名，Phase 2）
- `POST /api/huawei/accounts/{id}/projects/{projectId}/instances/{serverId}/reset-password`（Phase 2）
- `POST /api/huawei/accounts/{id}/projects/{projectId}/instances`（创建，Phase 2）

### Flexus L

- `GET /api/huawei/accounts/{id}/flexus-instances`（RMS `all-resources`，`type=hcss.l-instance`，可选 `region`、`marker`）
- `POST /api/huawei/accounts/{id}/flexus-instances/{instanceId}/actions`（body：`action` = start/stop/reboot，内部转 ECS）
- `DELETE /api/huawei/accounts/{id}/flexus-instances/{instanceId}`（删除 Flexus L 套餐，强确认，Phase 2）

### DNS（Phase 3）

- `GET /api/huawei/accounts/{id}/projects/{projectId}/dns/zones`
- `GET /api/huawei/accounts/{id}/projects/{projectId}/dns/zones/{zoneId}`
- `POST /api/huawei/accounts/{id}/projects/{projectId}/dns/zones`
- `DELETE /api/huawei/accounts/{id}/projects/{projectId}/dns/zones/{zoneId}`
- `GET /api/huawei/accounts/{id}/projects/{projectId}/dns/zones/{zoneId}/recordsets`
- `POST /api/huawei/accounts/{id}/projects/{projectId}/dns/zones/{zoneId}/recordsets`
- `PUT /api/huawei/accounts/{id}/projects/{projectId}/dns/zones/{zoneId}/recordsets/{recordsetId}`
- `DELETE /api/huawei/accounts/{id}/projects/{projectId}/dns/zones/{zoneId}/recordsets/{recordsetId}`

### 网络（Phase 2）

- `GET /api/huawei/accounts/{id}/projects/{projectId}/eips`
- `PUT /api/huawei/accounts/{id}/projects/{projectId}/eips/{eipId}/associate`
- `PUT /api/huawei/accounts/{id}/projects/{projectId}/eips/{eipId}/disassociate`
- `GET /api/huawei/accounts/{id}/projects/{projectId}/vpcs`
- `GET /api/huawei/accounts/{id}/projects/{projectId}/security-groups`

### 存储（Phase 3）

- `GET /api/huawei/accounts/{id}/projects/{projectId}/buckets`
- `GET /api/huawei/accounts/{id}/projects/{projectId}/buckets/{bucket}/objects?prefix=`
- `POST /api/huawei/accounts/{id}/projects/{projectId}/buckets`
- `DELETE /api/huawei/accounts/{id}/projects/{projectId}/buckets/{bucket}`
- `POST /api/huawei/accounts/{id}/projects/{projectId}/buckets/{bucket}/objects`（上传）
- `DELETE /api/huawei/accounts/{id}/projects/{projectId}/buckets/{bucket}/objects/{object}`（删除）
- `GET /api/huawei/accounts/{id}/projects/{projectId}/buckets/{bucket}/objects/{object}/download-url`（签名 URL，Phase 3 可选）

### 费用（Phase 4，可选）

- `GET /api/huawei/accounts/{id}/billing/overview`（当月费用概览）
- `GET /api/huawei/accounts/{id}/billing/free-resources`（资源包/流量包剩余量）

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
  "code": "HUAWEI_ERROR"
}
```

额外要求：

- 列表接口返回 Normalized 字段，不透传原始华为云对象。
- 列表分页参数（marker/limit 或 offset/limit）由后端封装，前端传 page/marker 语义即可。

## Security Requirements

1. 华为云账号所有接口 session auth。
2. SK 加密存储，日志不得打印 SK 明文/AK 完整值/签名中间值。
3. 账号列表不回显 SK，AK 掩码展示。
4. 删除实例、删除 DNS 记录、删除桶、删除对象、解绑 EIP 等高风险操作需确认。
5. 关键动作写入现有操作日志体系。
6. 文档、示例和测试夹具不得写入真实 AK/SK、真实 project_id 网络信息。

## Integration Decisions

1. **不直接并入 `server` 模块**：云资源控制面 vs 主机纳管，职责不同（同 Oracle/GCP 决策）。
2. **前端独立 HuaweiPage**：复用现有云厂商模块范式，风险最小。
3. **后端独立 huawei service**：按资源域拆分，不污染 serveragent。
4. **Flexus L 复用 ECS API**：套餐列表走 RMS，云主机操作走 ECS，不引入额外服务抽象。
5. **站点驱动域名**：账号 `site` 字段驱动 base URL 后缀，避免硬编码国内站。
6. **未来「导入到主机模块」弱集成**：华为云实例后续纳管 SSH/Agent，不作为第一版前置。

## Observability and Logging

记录以下行为（操作日志体系）：

- 新增/编辑/删除账号、验证账号
- 切换默认区域/项目
- 实例生命周期动作（start/stop/reboot/delete）
- DNS zone/recordset 变更
- 桶创建/删除、对象上传/删除
- EIP 绑定/解绑

日志字段：账号 ID、project ID、资源 ID、动作类型、结果、错误摘要。不含 SK、AK 完整值。

## Testing Decisions

### 后端测试

- schema 初始化幂等
- 签名器：固定 AK/SK/时间戳构造的 canonicalRequest 与官方 signer 结果一致（可抓官方 signer 输出做 fixture）
- AK/SK 校验（缺字段）
- huaweiRequest 错误提取与限流
- 路由分发（accounts/projects/instances/actions 正确落到对应 handler）
- 实例/Flexus 归一化（raw JSON → Normalized）
- 生命周期动作参数校验与错误分支
- 账号 CRUD 加解密往返
- 安全：safeAccount 不含敏感字段

### 前端测试

- 账号表单校验（AK/SK 必填、站点选择）
- 空态/加载态/失败态
- 实例列表筛选
- 动作确认弹窗（含随删 IP/磁盘选项）
- Tabs 切换与项目联动刷新

### 联调与验收

- `npm run governance:check`
- `node tools/backend-route-inventory.mjs`
- `npm run backend-go:test`
- `npm run lint`、`npm test`
- 浏览器 smoke：登录 → 进入华为云页面 → 添加账号 → 查看项目/实例 → 执行动作 → 详情

## Release Plan

### Milestone 1：架构接入

- 前端注册 huawei 模块（store.js / MainLayout.jsx / Icons.jsx）
- 后端注册 /api/huawei（manifest.go / server.go）
- huawei_accounts schema + SK 加密

### Milestone 2：账号与项目

- 账号 CRUD + 验证（AK/SK → projects）
- 项目列表与默认区域/项目切换
- 页面空态与账号管理

### Milestone 3：实例管理

- ECS 实例列表/详情
- 批量启停/重启/删除
- Flexus L 实例列表与云主机联动

### Milestone 4：网络与创建

- EIP 列表/绑定/解绑、VPC/安全组只读
- 创建实例、重置密码、改名

### Milestone 5：DNS 与存储

- 云解析 zone/recordset CRUD
- OBS 桶/对象（独立签名器）

### Milestone 6：费用与体验完善

- BSS 月度账单概览 + Flexus 流量包剩余量
- 导入 server 模块草稿（弱集成）
- 错误提示与操作日志完善

## Acceptance Criteria

1. 侧边栏可见「华为云」模块入口，位于云厂商分组。
2. 可成功新增华为云账号（AK/SK + 站点）并验证通过，项目列表可见。
3. 可见项目列表并切换默认区域/项目。
4. ECS 实例列表可见状态、规格、区域、公私网 IP。
5. 可对 ECS 实例执行启动、停止、重启。
6. 可删除实例，操作前有强确认与随删 IP/磁盘说明。
7. Flexus L 列表可见套餐信息，云主机可启停/重启。
8. (Phase 2) 可创建实例、重置密码、改名，可查看 EIP 并绑定/解绑。
9. (Phase 3) 可查看 DNS zone 与记录集并做 CRUD。
10. (Phase 3) 可查看 OBS 桶与桶内对象，删除空桶。
11. (Phase 4) 可查看当月费用概览与 Flexus 流量包剩余量。
12. SK 不以明文持久化到数据库，列表不回显。
13. 所有 /api/huawei 接口进入 manifest 和 Go route 分发体系。
14. 页面使用 Kumo 组件，中文 UI 完整，样式无漂移。
15. 相关测试和治理命令通过。

## Risks

1. 华为云 REST 响应字段多、变化快，直接透传难维护——统一归一化层缓解。
2. ECS 启停/重启返回同步结果但实际生效有延迟，前端误把请求成功当资源已变更——刷新观察 + 状态字段自然反映缓解。
3. AK/SK 权限过大会带来安全事故——依赖华为云侧最小权限（如 `ECS FullAccess` + 对应只读）实践 + 文档指引。
4. BSS 费用接口路径与权限不确定——Phase 4 按官方文档校准，未校准前不开放写接口。
5. OBS 独立签名协议是唯一分叉点——单独 obs_auth.go + 官方 SDK 参考校准。
6. 国内站/国际站域名与产品可用性差异——站点字段驱动 base URL，文档标注差异。
7. Flexus L 组合资源列表（RMS all-resources）在不同账号下可能缺失 RMS 服务开通——验证与错误提示补充「开通 RMS/配置审计」引导。

## Out of Scope

1. CCE/K8s、RDS、FunctionGraph、CDN、ModelArts 等 PaaS 管理与生命周期操作。
2. 自动将华为云实例注册进 server_accounts。
3. IAM 用户/权限、企业项目（EP）管理。
4. Flexus L 续订/退订操作。
5. 浏览器内 CloudShell 终端。
6. 多账号间细粒度协作权限。
7. BSS 账单深度分析/成本分摊（第一版只做概览）。

## Further Notes

1. 华为云模块与 GCP/Oracle 模块共同构成「REST 优先 + 凭证加密 + 资源归一化」模板的实践（GCP 是 OAuth2 变体、华为云是 AK/SK 签名变体、Oracle 是 SDK 变体）。
2. Flexus L 接入经验（组合服务 = RMS 列表 + ECS 操作）可反向指导未来接入其他轻量服务器产品线。
3. 第一版优先把「看得到、控得住、报错清楚」做好，再扩展到创建实例、存储、费用。
4. 签名器与 OBS 签名器实现完成后，可沉淀为项目级 `internal/huaweisign/` 独立包以便单测与复用。
5. 后续若需为华为云资源做告警或成本治理，可在本模块数据面之上演进，不影响现有架构。
