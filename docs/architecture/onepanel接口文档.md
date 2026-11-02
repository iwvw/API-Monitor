# 1Panel 快捷控制接口文档

最后更新：2026-08-13

通过 API Monitor 的 onepanel 模块，可对安装了 1Panel 的服务器进行面板状态查询、网站/容器/应用/SSL/备份/数据库等全生命周期管理。

## 架构

```
Agent (MCP/call_api) → API Monitor 后端 (/api/onepanel/*) → Agent shell 通道 → 签名 curl → 1Panel REST API
```

后端不直接连接 1Panel，而是通过 API Monitor Agent 在内网执行签名请求到 `127.0.0.1:8888`。

## 响应语义

- **HTTP 状态码**反映「通道是否可达」：`5xx` = 连接失败 / Agent 离线 / 参数未通过校验；`2xx` = 请求已到达 1Panel。
- **业务结果在内层**：1Panel 返回体 `{"code":200,"message":"success","data":...}` 被透传进外层 `data`。即使操作被 1Panel 拒绝（如对已停止容器再 stop），HTTP 仍为 200，需检查内层 `data.code == 200` 才代表业务成功。
- 通用代理接口的文字说明见末尾「通用代理执行」。

## 准备工作

1. 在目标服务器上启用 1Panel API 接口并获取 ApiKey：

```sql
sqlite3 /opt/1panel/db/core.db
UPDATE settings SET value='Enable' WHERE key='ApiInterfaceStatus';
UPDATE settings SET value='<随机32位hex>' WHERE key='ApiKey';
UPDATE settings SET value='0.0.0.0/0\n::/0' WHERE key='IpWhiteList';
.quit
systemctl restart 1panel-core
```

2. 在 API Monitor 中注册连接：

```json
POST /api/onepanel/config
{
  "serverId": "<API Monitor 服务器 ID>",
  "apiKey": "<上一步设置的 ApiKey>",
  "baseUrl": "https://127.0.0.1:8888"   // 可选，默认值
}
```

## 配置管理

### 列出所有连接

```
GET /api/onepanel/config
```

返回连接列表，apiKey 字段脱敏为 `hasKey: true/false`。

### 新增连接

```
POST /api/onepanel/config
Body: { "serverId": "string", "apiKey": "string", "baseUrl": "string (可选)" }
```

### 更新连接

```
PUT /api/onepanel/config/{serverId}
Body: { "apiKey": "string", "baseUrl": "string" }
```

### 删除连接

```
DELETE /api/onepanel/config/{serverId}
```

## 面板与总览

### 面板连通性检查

```
GET /api/onepanel/{serverId}/health
```

检查 1Panel 是否可达，代理到 `/dashboard/base/no/no`。

### 聚合总览

```
GET /api/onepanel/{serverId}/overview
```

一次返回面板状态、网站列表、OpenResty 状态三合一。

### 实时资源监控

```
GET /api/onepanel/{serverId}/dashboard/current
```

返回 CPU 百分比、内存/交换/磁盘使用、IO、网络、Top 进程等。

### 面板升级检查

```
GET /api/onepanel/{serverId}/upgrade/check
```

返回可升级版本信息。

### 面板升级

```
POST /api/onepanel/{serverId}/upgrade
Body: { "version": "x.y.z" }
```

升级到指定版本。

## 网站管理

### 列出网站

```
GET /api/onepanel/{serverId}/websites
```

### 创建网站

```
POST /api/onepanel/{serverId}/websites
Body: {
  "type": "proxy|static",
  "alias": "<站点域名，如 example.com>",
  "proxy": "127.0.0.1:<服务端口>",
  "domains": [{ "domain": "<站点域名>", "port": 80 }],
  "enableSSL": false,
  "webSiteGroupID": 1
}
```

### 获取网站详情

```
GET /api/onepanel/{serverId}/websites/{id}
```

### 删除网站

```
DELETE /api/onepanel/{serverId}/websites/{id}
```

### 网站启停操作

```
POST /api/onepanel/{serverId}/websites/{id}/operate
Body: { "id": 10, "operate": "start|stop|restart" }
```

### 更新反代配置

```
POST /api/onepanel/{serverId}/websites/{id}/proxy
Body: {
  "id": 10,
  "name": "root",
  "operate": "update",
  "proxyHost": "127.0.0.1",
  "proxyPass": "http://127.0.0.1:9000",
  "match": "^~ /"
}
```
对应 1Panel `POST /websites/proxies/update`（`request.WebsiteProxyConfig`）。

### 配置 HTTPS

```
POST /api/onepanel/{serverId}/websites/{id}/https
Body: {
  "enable": true,
  "type": "existed|auto|manual",
  "httpConfig": "HTTPSOnly|HTTPAlso|HTTPToHTTPS",
  "websiteSSLId": 2
}
```

### 更新 nginx 配置

```
POST /api/onepanel/{serverId}/websites/{id}/nginx
Body: { "operate": "add|update|delete", "params": {...} }
```

## 应用管理

### 查询应用市场

```
GET /api/onepanel/{serverId}/apps
```

返回可安装的应用列表。

### 安装应用

```
POST /api/onepanel/{serverId}/apps/install
Body: {
  "appDetailId": 123,
  "name": "myapp",
  "version": "8.0",
  "dockerCompose": "自定义 compose 内容（可选）",
  "editCompose": false,
  "params": { ... }
}
```
必填 `appDetailId`（应用详细 ID，从「查询应用市场」接口的返回中获得）与 `name`（应用实例名）。

### 查看已安装应用

```
GET /api/onepanel/{serverId}/apps/installed
```

### 操作已安装应用

```
POST /api/onepanel/{serverId}/apps/installed/{appInstallId}/op
Body: { "id": 1, "operate": "start|stop|restart" }
```

## 容器管理

### 列出容器

```
GET /api/onepanel/{serverId}/containers
```

请求体（POST 搜索）：
```json
{ "page": 1, "pageSize": 50, "state": "running", "orderBy": "name", "order": "ascending" }
```

### 批量操作容器

```
POST /api/onepanel/{serverId}/containers/operate
Body: { "names": ["container1", "container2"], "operation": "start|stop|restart|kill|pause|unpause|remove" }
```

### 容器日志

```
GET /api/onepanel/{serverId}/containers/{name}/logs
```

### 创建 Compose

```
POST /api/onepanel/{serverId}/containers/compose
Body: { "name": "myapp", "compose": { ... } }
```

## SSL 证书

### 列出 SSL 证书

```
GET /api/onepanel/{serverId}/ssl
```

### 申请 SSL 证书（ACME）

```
POST /api/onepanel/{serverId}/ssl/obtain
Body: { "ID": 1, "nameservers": [...], "skipDNSCheck": false }
```

### 创建 ACME 账号

```
POST /api/onepanel/{serverId}/acme
Body: {
  "type": "letsencrypt",
  "email": "admin@example.com",
  "keyType": "EC256",
  "caDirURL": "<自定义 CA 目录 URL，type=custom 时必填>"
}
```
必填 `type`（letsencrypt/zerossl/buypass/google/custom）、`email`、`keyType`（EC256/EC384/RSA2048/RSA3072/RSA4096/RSA8192）。

## OpenResty

### 读取状态

```
GET /api/onepanel/{serverId}/openresty/status
```

返回连接数、请求数、活跃连接等。

### 重载配置

```
POST /api/onepanel/{serverId}/openresty/reload
```

校验 nginx 配置并重载（`docker exec openresty nginx -t && docker exec openresty nginx -s reload`）。

## 备份

### 立即备份

```
POST /api/onepanel/{serverId}/backup
Body: { "type": "app|website|mysql|mariadb|redis|postgresql|mongodb", "name": "存储账号", "detailName": "备份对象" }
```

### 备份记录

```
GET /api/onepanel/{serverId}/backups/records
```

### 备份存储账号

```
GET /api/onepanel/{serverId}/backups/options
```

## 数据库

### 列出/创建数据库

```
GET /api/onepanel/{serverId}/databases
POST /api/onepanel/{serverId}/databases
Body: { "type": "mysql|mariadb|postgresql|redis|mongodb", "name": "dbname", "format": "utf8mb4", "username": "user", "password": "pass" }
```

### 修改密码

```
POST /api/onepanel/{serverId}/databases/{id}/password
Body: { "password": "newpass", "database": "dbname" }
```

### 删除数据库

```
DELETE /api/onepanel/{serverId}/databases/{id}
```

## 运行环境

### 列出/创建运行环境

```
GET /api/onepanel/{serverId}/runtimes
POST /api/onepanel/{serverId}/runtimes
Body: { "type": "php|python|nodejs", "name": "myenv", "version": "8.2" }
```

## 定时任务

### 列出/创建定时任务

```
GET /api/onepanel/{serverId}/cronjobs
POST /api/onepanel/{serverId}/cronjobs
Body: { "type": "shell|backup", "name": "每日备份", "spec": "0 3 * * *", "script": "..." }
```

## 内置 API 目录（兜底）

### 读取官方 API 目录

```
GET /api/onepanel/spec
```

返回内置的 717 个 1Panel v2 官方 API 端点清单（method / path / summary），**无需 serverId**。

### 通用代理执行

```
POST /api/onepanel/{serverId}/proxy
Body: { "method": "GET|POST|PUT|DELETE", "path": "/websites/list", "body": {} }
```

当快捷操作未覆盖你的目标时，先用 `spec` 查目录，再用 `proxy` 手动执行任意 1Panel API。

## 典型工作流

### 安装应用 → 建站 → 反代 → HTTPS

```
1. GET  /api/onepanel/{serverId}/apps                    ← 查可安装应用
2. POST /api/onepanel/{serverId}/apps/install             ← 安装（compose）
3. POST /api/onepanel/{serverId}/websites                 ← 创建网站（type=proxy, proxy=127.0.0.1:PORT）
4. POST /api/onepanel/{serverId}/ssl/obtain               ← 申请 SSL
5. POST /api/onepanel/{serverId}/websites/{id}/https      ← 启用 HTTPS
```

## 对比

| 方式 | 适用场景 | 接口数 |
|---|---|---|
| 快捷操作 | 高频管理：启停网站/容器/重载/备份/升级 | 38 个 |
| 通用代理 | 覆盖全部 717 个 1Panel API | 1 个 `proxy` |
| 先查目录再用 proxy | 发现 + 手动执行 | `spec` + `proxy` |