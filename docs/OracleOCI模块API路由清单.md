# Oracle OCI 模块 API 路由清单

最后更新：2026-09-08

## 1. 文档目的

本文档定义 Oracle OCI 模块首版和后续扩展的 API 路由清单，用于指导：

- Go manifest 路由注册
- `backend-go/internal/oracle/service.go` 的路径分发
- 前端 `OraclePage.jsx` 的请求契约
- 后端测试与接口文档补全

对应产品文档见 [Oracle OCI 主机管理模块 PRD](./prd/OracleOCI主机管理模块.md)，对应实现设计见 [Oracle OCI 模块技术设计文档](./OracleOCI模块技术设计文档.md)。

## 2. 通用约定

### Base Prefix

所有接口以：

```text
/api/oracle
```

为前缀。

### Auth

默认全部使用：

- `AuthSession`

即要求登录后访问。

### Response Mode

默认全部为：

- `ResponseJSON`

### 通用成功响应

```json
{
  "success": true,
  "data": {}
}
```

### 通用失败响应

```json
{
  "success": false,
  "error": "错误描述",
  "code": "ERROR_CODE"
}
```

## 3. Manifest 建议

首版最小注册可先只加一条 prefix：

```go
{Prefix: "/api/oracle", Module: "oracle", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "Oracle OCI compute management"}
```

如果后续希望更细粒度治理，可拆为多条 exact / pattern 路由。

## 4. 路由分组总览

### 账号

- `GET /api/oracle/accounts`
- `POST /api/oracle/accounts`
- `PUT /api/oracle/accounts/{id}`
- `DELETE /api/oracle/accounts/{id}`
- `POST /api/oracle/accounts/{id}/verify`

### 账号导出 / 导入（后续补充实现）

- `GET /api/oracle/export/accounts`
- `POST /api/oracle/import/accounts`

### 基础资源

- `GET /api/oracle/accounts/{id}/compartments`
- `GET /api/oracle/accounts/{id}/availability-domains`
- `GET /api/oracle/accounts/{id}/shapes`
- `GET /api/oracle/accounts/{id}/images`
- `GET /api/oracle/accounts/{id}/subnets`

### 实例

- `GET /api/oracle/accounts/{id}/instances`
- `GET /api/oracle/accounts/{id}/instances/{instanceId}`
- `POST /api/oracle/accounts/{id}/instances/{instanceId}/actions`
- `DELETE /api/oracle/accounts/{id}/instances/{instanceId}`
- `POST /api/oracle/accounts/{id}/instances`

### 成本概览（后续补充实现）

- `GET /api/oracle/accounts/{id}/cost`

### 附属资源

- `GET /api/oracle/accounts/{id}/instances/{instanceId}/vnic-attachments`
- `GET /api/oracle/accounts/{id}/instances/{instanceId}/boot-volume-attachments`
- `GET /api/oracle/accounts/{id}/instances/{instanceId}/volume-attachments`
- `GET /api/oracle/accounts/{id}/instances/{instanceId}/console-connections`
- `POST /api/oracle/accounts/{id}/instances/{instanceId}/console-connections`
- `DELETE /api/oracle/accounts/{id}/console-connections/{connectionId}`

### 可选

- `GET /api/oracle/accounts/{id}/work-requests/{workRequestId}`

## 5. 详细路由定义

## 5.1 账号管理

### `GET /api/oracle/accounts`

用途：

- 获取 Oracle 账号列表

前端用途：

- `账号管理` Tab 初始化
- 页面默认账号下拉

返回字段建议：

```json
{
  "success": true,
  "data": [
    {
      "id": 1,
      "name": "OCI Production",
      "tenancyOcidMasked": "ocid1.tenancy.oc1..***",
      "userOcidMasked": "ocid1.user.oc1..***",
      "fingerprint": "12:34:56:78:90:ab:cd:ef",
      "region": "us-phoenix-1",
      "defaultCompartmentId": "ocid1.compartment.oc1..***",
      "description": "生产账号",
      "lastVerifiedAt": "2026-07-12T10:00:00Z",
      "lastVerifyStatus": "success",
      "lastVerifyError": "",
      "createdAt": "2026-07-12T08:00:00Z"
    }
  ]
}
```

### `POST /api/oracle/accounts`

用途：

- 新增 OCI 账号

请求体：

```json
{
  "name": "OCI Production",
  "tenancyOcid": "ocid1.tenancy.oc1..xxx",
  "userOcid": "ocid1.user.oc1..xxx",
  "fingerprint": "12:34:56:78:90:ab:cd:ef",
  "region": "us-phoenix-1",
  "privateKeyPem": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----",
  "passphrase": "",
  "defaultCompartmentId": "ocid1.compartment.oc1..xxx",
  "description": "生产账号"
}
```

校验要求：

- `name` 必填
- `tenancyOcid` 必填
- `userOcid` 必填
- `fingerprint` 必填
- `region` 必填
- `privateKeyPem` 必填

### `PUT /api/oracle/accounts/{id}`

用途：

- 编辑 OCI 账号

请求体：

```json
{
  "name": "OCI Production",
  "tenancyOcid": "ocid1.tenancy.oc1..xxx",
  "userOcid": "ocid1.user.oc1..xxx",
  "fingerprint": "12:34:56:78:90:ab:cd:ef",
  "region": "us-phoenix-1",
  "privateKeyPem": "",
  "passphrase": "",
  "defaultCompartmentId": "ocid1.compartment.oc1..xxx",
  "description": "更新后的描述"
}
```

规则：

- 私钥留空表示不修改
- passphrase 留空表示不修改或清空，由实现明确约定

### `DELETE /api/oracle/accounts/{id}`

用途：

- 删除 OCI 账号

规则：

- 仅删除本地账号配置，不删除 OCI 云端资源
- 应写入操作日志

### `POST /api/oracle/accounts/{id}/verify`

用途：

- 验证账号配置是否可用

建议实现：

- 初始化 SDK client
- 读取 tenancy/compartment 相关只读资源
- 返回验证时间和简要结果

返回：

```json
{
  "success": true,
  "data": {
    "valid": true,
    "message": "账号验证成功",
    "verifiedAt": "2026-07-12T10:00:00Z"
  }
}
```

## 5.2 基础资源

### `GET /api/oracle/accounts/{id}/compartments`

用途：

- 获取可选 compartments

查询参数：

- `includeRoot=true|false`（可选）

返回字段建议：

- `id`
- `name`
- `description`
- `lifecycleState`
- `parentId`

### `GET /api/oracle/accounts/{id}/availability-domains`

用途：

- 获取指定 tenancy / region 下的 AD 列表

返回字段建议：

- `name`
- `compartmentId`

### `GET /api/oracle/accounts/{id}/shapes`

用途：

- 第二阶段启动实例表单使用

查询参数建议：

- `availabilityDomain`
- `compartmentId`
- `imageId`

### `GET /api/oracle/accounts/{id}/images`

用途：

- 第二阶段启动实例表单使用

查询参数建议：

- `compartmentId`
- `operatingSystem`
- `operatingSystemVersion`

### `GET /api/oracle/accounts/{id}/subnets`

用途：

- 第二阶段启动实例表单使用

查询参数建议：

- `compartmentId`
- `vcnId`

## 5.3 实例

### `GET /api/oracle/accounts/{id}/instances`

用途：

- 获取实例列表

查询参数建议：

- `compartmentId` 必填
- `availabilityDomain` 可选
- `state` 可选
- `keyword` 可选
- `limit` 可选
- `page` 或 `pageToken` 可选

返回字段建议：

```json
{
  "success": true,
  "data": {
    "items": [
      {
        "id": "ocid1.instance.oc1..xxx",
        "name": "oracle-arm-01",
        "state": "RUNNING",
        "shape": "VM.Standard.A1.Flex",
        "availabilityDomain": "AD-1",
        "faultDomain": "FAULT-DOMAIN-1",
        "region": "us-phoenix-1",
        "compartmentId": "ocid1.compartment.oc1..xxx",
        "timeCreated": "2026-07-12T08:00:00Z",
        "imageId": "ocid1.image.oc1..xxx",
        "primaryPublicIp": "203.0.113.10",
        "primaryPrivateIp": "10.0.0.12"
      }
    ],
    "nextPageToken": ""
  }
}
```

### `GET /api/oracle/accounts/{id}/instances/{instanceId}`

用途：

- 获取实例详情聚合数据

建议直接返回：

- 基础实例信息
- vnic 摘要
- boot volume 摘要
- block volume 摘要
- console connection 摘要

这样前端首次打开详情时只需一次请求。

### `POST /api/oracle/accounts/{id}/instances/{instanceId}/actions`

用途：

- 对实例执行生命周期动作

请求体：

```json
{
  "action": "START"
}
```

允许值建议：

- `START`
- `STOP`
- `SOFTSTOP`
- `RESET`
- `SOFTRESET`
- `REBOOTMIGRATE`

返回：

```json
{
  "success": true,
  "data": {
    "message": "实例操作已提交",
    "instanceId": "ocid1.instance.oc1..xxx",
    "action": "START"
  }
}
```

### `DELETE /api/oracle/accounts/{id}/instances/{instanceId}`

用途：

- 终止实例

查询参数建议：

- `preserveBootVolume=true|false`
- `preserveDataVolumes=true|false`

规则：

- 这是高风险接口
- 前端必须二次确认

### `POST /api/oracle/accounts/{id}/instances`

用途：

- 启动新实例

阶段：

- 第二阶段实现

请求体建议：

```json
{
  "compartmentId": "ocid1.compartment.oc1..xxx",
  "availabilityDomain": "AD-1",
  "displayName": "oracle-arm-01",
  "shape": "VM.Standard.A1.Flex",
  "imageId": "ocid1.image.oc1..xxx",
  "subnetId": "ocid1.subnet.oc1..xxx",
  "assignPublicIp": true,
  "sshAuthorizedKeys": "ssh-ed25519 AAAA..."
}
```

## 5.4 VNIC 与网络

### `GET /api/oracle/accounts/{id}/instances/{instanceId}/vnic-attachments`

用途：

- 获取实例的 VNIC attachments 和 VNIC 详情

返回字段建议：

- `attachmentId`
- `vnicId`
- `displayName`
- `privateIp`
- `publicIp`
- `subnetId`
- `isPrimary`
- `nicIndex`
- `state`

## 5.5 卷附加

### `GET /api/oracle/accounts/{id}/instances/{instanceId}/boot-volume-attachments`

用途：

- 获取引导卷附加列表

### `GET /api/oracle/accounts/{id}/instances/{instanceId}/volume-attachments`

用途：

- 获取数据卷附加列表

返回字段建议：

- `attachmentId`
- `volumeId`
- `device`
- `isReadOnly`
- `isShareable`
- `state`
- `timeCreated`

## 5.6 控制台连接

### `GET /api/oracle/accounts/{id}/instances/{instanceId}/console-connections`

用途：

- 查看实例控制台连接列表

### `POST /api/oracle/accounts/{id}/instances/{instanceId}/console-connections`

用途：

- 创建控制台连接

请求体：

```json
{}
```

首版通常不需要额外参数。

### `DELETE /api/oracle/accounts/{id}/console-connections/{connectionId}`

用途：

- 删除控制台连接

## 5.7 Work Request

### `GET /api/oracle/accounts/{id}/work-requests/{workRequestId}`

用途：

- 可选的异步状态查询接口

适用场景：

- 如果某些 OCI SDK 动作返回 work request id
- 前端需要轮询状态

第一阶段可不实现，留作文档保留项。

## 6. 错误码建议

- `ORACLE_ACCOUNT_NOT_FOUND`
- `ORACLE_VALIDATION_ERROR`
- `ORACLE_SDK_INIT_FAILED`
- `ORACLE_COMPARTMENT_REQUIRED`
- `ORACLE_INSTANCE_NOT_FOUND`
- `ORACLE_ACTION_INVALID`
- `ORACLE_CONSOLE_CONNECTION_NOT_FOUND`
- `ORACLE_API_ERROR`

## 7. 前端与接口映射

### `账号管理` Tab

- 加载账号列表：`GET /api/oracle/accounts`
- 新增账号：`POST /api/oracle/accounts`
- 编辑账号：`PUT /api/oracle/accounts/{id}`
- 删除账号：`DELETE /api/oracle/accounts/{id}`
- 验证账号：`POST /api/oracle/accounts/{id}/verify`

### `实例` Tab

- 加载 compartments：`GET /api/oracle/accounts/{id}/compartments`
- 加载实例列表：`GET /api/oracle/accounts/{id}/instances`
- 加载实例详情：`GET /api/oracle/accounts/{id}/instances/{instanceId}`
- 生命周期动作：`POST /api/oracle/accounts/{id}/instances/{instanceId}/actions`
- 终止实例：`DELETE /api/oracle/accounts/{id}/instances/{instanceId}`

### `网络` Tab

- 获取 VNIC：`GET /api/oracle/accounts/{id}/instances/{instanceId}/vnic-attachments`

### `卷` Tab

- 获取引导卷附加：`GET /api/oracle/accounts/{id}/instances/{instanceId}/boot-volume-attachments`
- 获取数据卷附加：`GET /api/oracle/accounts/{id}/instances/{instanceId}/volume-attachments`

### `控制台` Tab

- 获取控制台连接：`GET /api/oracle/accounts/{id}/instances/{instanceId}/console-connections`
- 创建控制台连接：`POST /api/oracle/accounts/{id}/instances/{instanceId}/console-connections`
- 删除控制台连接：`DELETE /api/oracle/accounts/{id}/console-connections/{connectionId}`

## 8. 实现优先级

### P0

- `/accounts`
- `/accounts/{id}/verify`
- `/accounts/{id}/compartments`
- `/accounts/{id}/instances`
- `/accounts/{id}/instances/{instanceId}`
- `/accounts/{id}/instances/{instanceId}/actions`
- `/accounts/{id}/instances/{instanceId}/vnic-attachments`
- `/accounts/{id}/instances/{instanceId}/boot-volume-attachments`
- `/accounts/{id}/instances/{instanceId}/volume-attachments`
- `/accounts/{id}/instances/{instanceId}/console-connections`
- `/accounts/{id}/console-connections/{connectionId}`

### P1

- `/accounts/{id}/availability-domains`
- `/accounts/{id}/shapes`
- `/accounts/{id}/images`
- `/accounts/{id}/subnets`
- `POST /accounts/{id}/instances`

### P2

- `/accounts/{id}/work-requests/{workRequestId}`

## 9. 文档后续同步点

实现完成后需要同步更新：

- [E:\Code\API-Monitor\docs\API接口文档.md](</E:/Code/API-Monitor/docs/API接口文档.md>)
- [E:\Code\API-Monitor\docs\README.md](</E:/Code/API-Monitor/docs/README.md>)
- 必要时更新 `docs/prd/OracleOCI主机管理模块.md`
