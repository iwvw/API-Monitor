# Oracle OCI 模块技术设计文档

最后更新：2026-07-12

## 1. 文档目的

本文档用于指导 API Monitor 中 Oracle OCI 主机管理模块的工程实现。它补充产品 PRD，聚焦以下内容：

1. 模块边界与职责划分
2. 前后端接入点
3. Go 后端模块结构
4. SQLite 数据设计
5. Oracle 官方 SDK 封装策略
6. 字段归一化与错误处理
7. 日志、安全与测试要求

对应产品文档见 [Oracle OCI 主机管理模块 PRD](../prd/OracleOCI主机管理模块.md)。

## 2. 设计目标

### 2.1 核心目标

- 新增独立的 `oracle` 云厂商模块。
- 后端使用 Oracle 官方 SDK 实现 OCI 实例相关资源管理。
- 第一阶段覆盖实例、VNIC、卷附加、控制台连接和高频生命周期动作。
- 与现有 React + Kumo、Go Manifest Backend、SQLite 架构保持一致。
- 从第一版开始使用现有 `secure` 能力加密保存私钥和口令。

### 2.2 设计约束

- 不新增独立服务或 sidecar。
- 不改动现有 `serveragent` 通信协议。
- 不将 OCI 实例直接并入现有 `server_accounts` 主机纳管模型。
- 不手写 OCI API 签名。
- 不把所有 OCI 响应对象原样透传给前端。

## 3. 模块定位与边界

### 3.1 模块定位

Oracle 模块是一个“云资源控制面”模块，不是“主机纳管执行面”模块。

它负责：

- OCI 账号管理
- OCI 资源读取
- OCI 实例生命周期控制
- OCI 实例网络与存储关联资源读取
- OCI 控制台连接管理

它不负责：

- SSH 登录
- Agent 安装与心跳
- Docker、SFTP、终端
- 主机指标采集

这些仍由现有 `server` / `serveragent` 模块负责。

### 3.2 和其他模块的关系

#### 与 `server` 模块

- 第一阶段无直接写入关系。
- 未来允许从 Oracle 实例生成“导入主机模块”的表单草稿。

#### 与 `settings` 模块

- 不新增全局设置项作为前置条件。
- 仅使用统一的用户认证和操作日志能力。

#### 与 `systemlogs` / `applog`

- 高风险操作和失败调用应进入系统日志/操作日志。

#### 与 `secure`

- Oracle 私钥、passphrase 必须通过 `backend-go/internal/secure` 加密。

## 4. 前端设计

## 4.1 模块注册

需要修改：

- [E:\Code\API-Monitor\src\js\store.js](</E:/Code/API-Monitor/src/js/store.js>)
- [E:\Code\API-Monitor\src\js\components\MainLayout.jsx](</E:/Code/API-Monitor/src/js/components/MainLayout.jsx>)

接入方式：

1. 在 `MODULE_CONFIG` 中新增 `oracle`
2. 将其放入 `云服务 -> 云厂商` 分组
3. 在 `MainLayout.jsx` 中懒加载 `OraclePage`
4. 在 `renderActivePage()` 中加入 `oracle` 分支

建议配置：

```js
oracle: {
  name: '甲骨文云',
  shortName: 'Oracle',
  icon: 'fa-cloud',
  description: 'Oracle Cloud Infrastructure 实例管理',
}
```

## 4.2 页面结构

页面文件：

- `src/js/pages/OraclePage.jsx`

建议首版结构参考 `AliyunPage.jsx` / `TencentPage.jsx`，采用单页 Tabs 模式：

- `instances`
- `network`
- `storage`
- `console`
- `accounts`

为了降低第一阶段复杂度，`network`、`storage`、`console` 三个 Tab 可以围绕当前选中的实例工作，而不是做全局资源页。

## 4.3 状态组织

建议页面内局部状态，不新增全局 Zustand slice。

首版页面内状态建议包括：

- `activeTab`
- `accounts`
- `selectedAccountId`
- `selectedCompartmentId`
- `instances`
- `selectedInstanceId`
- `instanceDetail`
- `vnicAttachments`
- `bootVolumeAttachments`
- `volumeAttachments`
- `consoleConnections`
- `loadingAccounts`
- `loadingInstances`
- `loadingDetail`
- `submittingAction`

原因：

- 现有云厂商页采用页面内自管理模式，最符合当前代码风格。
- OCI 模块首版没有跨页共享状态需求。

## 4.4 API 调用方式

短期沿用现有页面模式，可在页内使用 `getAuthHeaders()` 兼容当前请求习惯。

中期建议抽一个共享 helper，例如：

- `src/js/modules/authFetch.js`

但这不是 Oracle 首版的阻塞项。

## 4.5 UI 组件要求

- 表格：Kumo `Table`
- 弹窗：Kumo `Dialog`
- 按钮：Kumo `Button`
- 切换：Kumo `Tabs`
- 加载态：Kumo `SkeletonLine` / `Loader`
- 复制：Kumo `ClipboardText`
- 删除/终止确认：优先 `DeleteResource`

## 5. 后端设计

## 5.1 模块目录

新增目录：

```text
backend-go/internal/oracle/
├── service.go
├── types.go
├── schema.go
├── account_store.go
├── client_factory.go
├── compute_service.go
├── network_service.go
├── storage_service.go
├── console_service.go
├── normalize.go
└── service_test.go
```

### 文件职责

#### `service.go`

- 模块 HTTP 入口
- 路径拆分
- 参数校验
- 调用子服务
- 响应写回

#### `schema.go`

- `oracle_accounts` 等表的幂等建表
- 索引维护

#### `account_store.go`

- 账号 CRUD
- 账号字段加密/解密
- 列表安全视图

#### `client_factory.go`

- 将数据库中的账号配置转换为 OCI SDK client
- 管理 signer/config provider 初始化

#### `compute_service.go`

- 实例列表
- 实例详情
- 实例动作
- 启动实例（第二阶段）

#### `network_service.go`

- compartment 查询
- availability domain 查询
- VNIC attachment + VNIC 详情查询
- subnet 列表（第二阶段）

#### `storage_service.go`

- boot volume attachments
- block volume attachments

#### `console_service.go`

- console connections 查询 / 创建 / 删除

#### `normalize.go`

- 把 SDK 原始结构体转换成前端稳定字段

## 5.2 服务装配

需要修改：

- [E:\Code\API-Monitor\backend-go\internal\server\server.go](</E:/Code/API-Monitor/backend-go/internal/server/server.go>)

改动：

1. 引入 `internal/oracle`
2. 在 `Server` 结构体中新增 `oracle *oracle.Service`
3. 在 `NewServer()` 中实例化
4. 在 `serveGoRoute()` 中为 `/api/oracle` 分发

## 5.3 路由治理

需要修改：

- [E:\Code\API-Monitor\backend-go\internal\manifest\manifest.go](</E:/Code/API-Monitor/backend-go/internal/manifest/manifest.go>)

首版建议使用：

```go
{Prefix: "/api/oracle", Module: "oracle", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Oracle OCI compute management"}
```

Oracle 模块可先像 `aliyun` / `tencent` 一样用单 prefix 接管，模块内部自行解析子路径。

## 5.4 HTTP 路由风格

保持与现有云厂商 service 一致：

- service 作为 `http.Handler`
- 内部手工分发 path
- 不引入新的第三方路由器

这样可以最小化接入成本，并与现有 manifest 体系一致。

## 6. Oracle SDK 设计

## 6.1 SDK 选择原则

- 使用 Oracle 官方 SDK
- 不自己实现 REST 签名
- 不依赖 CLI 执行外部进程
- 不要求宿主机必须预装 OCI CLI

## 6.2 账号到 SDK Client 的转换

账号配置入库后，需要在请求阶段转换为 OCI SDK 所需的认证对象。

输入字段：

- tenancy OCID
- user OCID
- fingerprint
- region
- private key PEM
- passphrase

流程：

1. 从 DB 读取账号
2. 使用 `secure` 解密私钥和 passphrase
3. 在内存中构造 config provider / signer
4. 构造所需的 SDK clients

首版需要的 clients：

- Compute client
- VirtualNetwork client
- Blockstorage client

可选：

- Identity client（查询 availability domains / compartments）

## 6.3 Client 工厂策略

建议通过工厂函数集中创建 client，避免在多个 handler 里散落认证初始化逻辑。

示例职责：

- `newComputeClient(account OracleAccount) (...)`
- `newNetworkClient(account OracleAccount) (...)`
- `newBlockstorageClient(account OracleAccount) (...)`
- `newIdentityClient(account OracleAccount) (...)`

## 6.4 请求上下文与超时

所有 SDK 调用应继承 HTTP request context，并设置合理超时。

建议：

- 读操作：10~20 秒
- 写操作：20~30 秒

避免长时间阻塞 Go handler。

## 7. 数据设计

## 7.1 账号表

```sql
CREATE TABLE IF NOT EXISTS oracle_accounts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  tenancy_ocid TEXT NOT NULL,
  user_ocid TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  region TEXT NOT NULL,
  private_key_encrypted TEXT NOT NULL,
  passphrase_encrypted TEXT,
  default_compartment_id TEXT,
  description TEXT,
  last_verified_at DATETIME,
  last_verify_status TEXT,
  last_verify_error TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
)
```

建议索引：

- `idx_oracle_accounts_created_at`
- `idx_oracle_accounts_region`

## 7.2 可选视图偏好表

如果首版需要保存默认 compartment、默认实例视图，可追加：

```sql
CREATE TABLE IF NOT EXISTS oracle_saved_views (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  account_id INTEGER NOT NULL,
  name TEXT NOT NULL,
  compartment_id TEXT,
  filters_json TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
)
```

第一阶段不是必需项。

## 8. 资源查询与字段归一化

## 8.1 为什么要做归一化

OCI SDK 对象字段多、嵌套深、命名偏 SDK 风格。前端直接消费会带来几个问题：

1. 页面和 SDK 类型强耦合
2. 后续换字段或补充缓存时难以兼容
3. 阿里云 / 腾讯云 / Oracle 三个页面难以保持统一体验

因此后端必须输出“项目自定义稳定字段”。

## 8.2 实例列表归一化字段

建议输出：

- `id`
- `name`
- `state`
- `shape`
- `availabilityDomain`
- `faultDomain`
- `region`
- `timeCreated`
- `imageId`
- `primaryPublicIp`
- `primaryPrivateIp`
- `compartmentId`
- `isPrimaryVnicReady`
- `rawSummary`（可选）

## 8.3 实例详情归一化字段

建议输出：

- `id`
- `name`
- `state`
- `shape`
- `availabilityDomain`
- `faultDomain`
- `region`
- `timeCreated`
- `imageId`
- `metadata`
- `freeformTags`
- `definedTags`
- `launchMode`
- `agentConfig`
- `vnicSummary`
- `bootVolumeSummary`
- `blockVolumeSummary`

## 8.4 VNIC 归一化字段

- `attachmentId`
- `vnicId`
- `displayName`
- `subnetId`
- `privateIp`
- `publicIp`
- `hostnameLabel`
- `nicIndex`
- `isPrimary`
- `state`

## 8.5 卷附加归一化字段

- `attachmentId`
- `volumeId`
- `volumeType`
- `device`
- `isReadOnly`
- `isShareable`
- `state`
- `timeCreated`

## 8.6 控制台连接归一化字段

- `id`
- `instanceId`
- `state`
- `connectionString`
- `fingerprint`
- `timeCreated`

## 9. 实例详情的数据流

建议使用聚合式详情读取：

1. 读取实例详情
2. 查询该实例的 VNIC attachments
3. 对每个 attachment 获取 VNIC 详情
4. 查询 boot volume attachments
5. 查询 block volume attachments
6. 查询 console connections
7. 归一化并组合为一个详情 payload

优点：

- 前端只发一次详情请求即可拿到足够多的信息
- 便于后端隐藏 SDK 细节

缺点：

- 单次请求较重

折中方案：

- 列表与详情分开
- 详情页首次打开加载聚合详情
- 次级 tab 可按需独立刷新

## 10. 错误处理

## 10.1 分层策略

### 输入错误

- 400
- 示例：缺少 region、无效 accountId、无效 action

### 鉴权错误

- 401
- 由现有 session auth 统一处理

### 资源不存在

- 404
- 示例：账号不存在、实例不存在、console connection 不存在

### OCI 调用失败

- 502 或 500
- 返回统一错误结构
- 附带简短错误码

## 10.2 错误文案策略

后端应尽量将 Oracle SDK 错误整理为：

- 中文摘要
- 原始错误 message
- 可选错误码

例如：

```json
{
  "success": false,
  "error": "获取实例列表失败：账号无权访问该 compartment",
  "code": "OCI_NOT_AUTHORIZED"
}
```

## 11. 安全设计

1. 私钥和 passphrase 使用 `secure` 加密存储。
2. 响应中不返回私钥明文。
3. 操作日志中不记录敏感凭证。
4. 终止实例必须强确认。
5. 控制台连接字符串默认只在详情页展示，不写入长期缓存表。
6. 若未来支持启动实例，metadata 中涉及 SSH key 的字段也必须谨慎记录。

## 12. 日志与审计

建议记录以下事件：

- oracle.account.create
- oracle.account.update
- oracle.account.delete
- oracle.account.verify
- oracle.instance.list
- oracle.instance.action
- oracle.instance.terminate
- oracle.console_connection.create
- oracle.console_connection.delete

字段建议：

- `module`
- `account_id`
- `resource_id`
- `action`
- `status`
- `error_summary`
- `timestamp`

## 13. 测试策略

## 13.1 后端单测

- schema 幂等
- 账号加解密
- account store CRUD
- route dispatch
- action 参数校验
- 归一化函数
- 错误映射

## 13.2 SDK 集成测试

若真实云环境可用，可增加受控集成测试：

- 读取 compartments
- 读取实例列表
- 查询单实例详情

这些测试默认不进本地快速测试，应通过环境变量显式启用。

## 13.3 前端测试

- 账号表单
- 列表空态
- 动作确认
- 详情 tab
- 错误提示

## 14. 分阶段实施建议

### 第 1 阶段

- 路由接入
- Oracle 页面壳
- 账号 CRUD
- 账号验证

### 第 2 阶段

- compartments
- 实例列表
- 基础搜索与刷新

### 第 3 阶段

- 实例详情聚合
- 实例启动/停止/重启
- 终止实例

### 第 4 阶段

- VNIC / 卷 / 控制台连接

### 第 5 阶段

- 启动实例
- 导入到主机模块草稿
- 更丰富的筛选和缓存

## 15. 实现注意事项

1. 不要复制阿里云、腾讯云模块的明文凭证存储方式。
2. 不要把所有 OCI 逻辑塞进一个超大文件。
3. 不要在前端直接依赖 SDK 命名字段。
4. 不要在第一版尝试把 OCI 与 Agent/SSH/Docker 绑定到一起。
5. 不要先做复杂的创建实例流程再做基础实例读取。

## 16. 建议的首批代码改动点

- `src/js/store.js`
- `src/js/components/MainLayout.jsx`
- `src/js/pages/OraclePage.jsx`
- `backend-go/internal/oracle/*`
- `backend-go/internal/manifest/manifest.go`
- `backend-go/internal/server/server.go`
- `docs/architecture/API接口文档.md`（后续补充）
- `docs/README.md`
