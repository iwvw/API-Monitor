# 服务器管理模块

## 概述

服务器管理模块是 API Monitor 的核心功能之一，提供完整的服务器监控、管理和操作功能。

## 功能特性

### ✅ 已实现功能

#### 1. 数据库和模型层
- ✅ 服务器账号管理（支持密码和密钥认证）
- ✅ 监控日志记录
- ✅ 监控配置管理
- ✅ AES-256-GCM 加密存储敏感信息

#### 2. 后端核心服务
- ✅ SSH 连接服务（连接池、会话管理）
- ✅ SFTP 文件管理服务
- ✅ 定时监控服务（基于 node-cron）
- ✅ 系统信息采集服务

#### 3. API 接口
- ✅ 服务器 CRUD 操作
- ✅ 批量导入/导出
- ✅ 连接测试
- ✅ 手动探测
- ✅ 服务器详细信息获取
- ✅ 服务器操作（重启/关机）
- ✅ SSH 命令执行
- ✅ SFTP 文件操作
- ✅ 监控配置管理
- ✅ 监控日志查询

#### 4. 前端界面
- ✅ 服务器列表展示
- ✅ 服务器状态指示器（在线/离线/未知）
- ✅ 展开/收起详细信息
- ✅ 系统信息展示（OS、CPU、内存、磁盘、网络、Docker）
- ✅ 服务器操作按钮（刷新、重启、关机）
- ✅ 批量导出功能

#### 5. 前端对话框和模态框
- ✅ 添加服务器对话框（支持密码和密钥认证）
- ✅ 编辑服务器对话框
- ✅ 批量导入服务器对话框（JSON 文件）
- ✅ Docker 容器详情模态框

#### 6. 后台管理页面
- ✅ 监控配置界面（探测间隔、超时、日志保留）
- ✅ 探测日志界面（筛选、分页）
- ✅ 服务器列表筛选

#### 7. SSH 终端功能
- ✅ SSH 终端界面（基于 xterm.js）
- ✅ 多终端标签页（同时打开多个 SSH 会话）
- ✅ 命令历史记录（上下箭头导航）
- ✅ 快捷键支持（Ctrl+C）

#### 8. SFTP 文件管理功能
- ✅ SFTP 文件管理界面
- ✅ 目录浏览和导航
- ✅ 创建/删除/重命名文件和目录
- ⏳ 文件上传下载进度显示（开发中）

### 🚧 待实现功能

#### 1. 高级功能
- ⏳ Docker 容器操作（启动/停止/重启）
- ⏳ 服务器分组管理
- ⏳ 监控告警功能
- ⏳ 性能监控图表

## 技术栈

### 后端
- **SSH 连接**: ssh2
- **SFTP 文件管理**: ssh2-sftp-client
- **定时任务**: node-cron
- **加密**: Node.js crypto (AES-256-GCM)
- **数据库**: SQLite (better-sqlite3)

### 前端
- **终端模拟器**: xterm.js (待集成)
- **UI 框架**: Vue 3
- **样式**: 自定义 CSS

## 数据库表结构

### 1. server_accounts
服务器账号信息表，存储服务器连接配置。

| 字段 | 类型 | 说明 |
|------|------|------|
| id | TEXT | 主键 |
| name | TEXT | 服务器名称 |
| host | TEXT | 服务器地址 |
| port | INTEGER | SSH 端口（默认 22） |
| username | TEXT | 用户名 |
| auth_type | TEXT | 认证方式（password/key） |
| password | TEXT | 加密存储的密码 |
| private_key | TEXT | 加密存储的私钥 |
| passphrase | TEXT | 加密存储的私钥密码 |
| status | TEXT | 服务器状态（online/offline/unknown） |
| last_check_time | DATETIME | 最后探测时间 |
| response_time | INTEGER | 响应时间（毫秒） |
| tags | TEXT | 标签（JSON 数组） |
| description | TEXT | 描述 |

### 2. server_monitor_logs
监控日志表，记录每次探测结果。

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER | 主键（自增） |
| server_id | TEXT | 服务器 ID |
| status | TEXT | 探测状态（success/failed） |
| response_time | INTEGER | 响应时间（毫秒） |
| error_message | TEXT | 错误信息 |
| checked_at | DATETIME | 探测时间 |

### 3. server_monitor_config
监控配置表，单例模式。

| 字段 | 类型 | 说明 |
|------|------|------|
| id | INTEGER | 主键（固定为 1） |
| probe_interval | INTEGER | 探测间隔（秒，默认 60） |
| probe_timeout | INTEGER | 探测超时（秒，默认 10） |
| log_retention_days | INTEGER | 日志保留天数（默认 7） |
| max_connections | INTEGER | 最大连接数（默认 10） |
| session_timeout | INTEGER | 会话超时（秒，默认 1800） |
| auto_start | INTEGER | 是否自动启动监控（默认 1） |

## API 接口

### 服务器管理

#### 获取所有服务器
```
GET /api/server/accounts
```

#### 添加服务器
```
POST /api/server/accounts
Body: {
  name: string,
  host: string,
  port: number,
  username: string,
  auth_type: 'password' | 'key',
  password?: string,
  private_key?: string,
  passphrase?: string,
  tags?: string[],
  description?: string
}
```

#### 更新服务器
```
PUT /api/server/accounts/:id
Body: { ...更新字段 }
```

#### 删除服务器
```
DELETE /api/server/accounts/:id
```

#### 批量删除
```
POST /api/server/accounts/batch-delete
Body: { ids: string[] }
```

#### 批量导入
```
POST /api/server/accounts/import
Body: { servers: Array<ServerConfig> }
```

#### 批量导出
```
GET /api/server/accounts/export
```

### 服务器操作

#### 测试连接
```
POST /api/server/test-connection
Body: { ...服务器配置 }
```

#### 手动探测所有服务器
```
POST /api/server/check-all
```

#### 获取服务器详细信息
```
POST /api/server/info
Body: { serverId: string }
```

#### 服务器操作（重启/关机）
```
POST /api/server/action
Body: {
  serverId: string,
  action: 'reboot' | 'shutdown'
}
```

### SSH 操作

#### 执行命令
```
POST /api/server/ssh/exec
Body: {
  serverId: string,
  command: string
}
```

#### 关闭连接
```
POST /api/server/ssh/disconnect
Body: { serverId: string }
```

#### 获取连接池状态
```
GET /api/server/ssh/status
```

### SFTP 操作

#### 列出目录
```
POST /api/server/sftp/list
Body: {
  serverId: string,
  path: string
}
```

#### 上传文件
```
POST /api/server/sftp/upload
Body: {
  serverId: string,
  localPath: string,
  remotePath: string
}
```

#### 下载文件
```
POST /api/server/sftp/download
Body: {
  serverId: string,
  remotePath: string,
  localPath: string
}
```

#### 删除文件/目录
```
POST /api/server/sftp/delete
Body: {
  serverId: string,
  path: string,
  isDirectory: boolean
}
```

#### 重命名
```
POST /api/server/sftp/rename
Body: {
  serverId: string,
  oldPath: string,
  newPath: string
}
```

#### 创建目录
```
POST /api/server/sftp/mkdir
Body: {
  serverId: string,
  path: string,
  recursive: boolean
}
```

### 监控管理

#### 获取监控配置
```
GET /api/server/monitor/config
```

#### 更新监控配置
```
PUT /api/server/monitor/config
Body: {
  probe_interval?: number,
  probe_timeout?: number,
  log_retention_days?: number,
  max_connections?: number,
  session_timeout?: number,
  auto_start?: number
}
```

#### 获取监控日志
```
GET /api/server/monitor/logs?serverId=xxx&status=xxx&limit=100&offset=0
```

#### 获取监控服务状态
```
GET /api/server/monitor/status
```

#### 启动监控服务
```
POST /api/server/monitor/start
```

#### 停止监控服务
```
POST /api/server/monitor/stop
```

## 安全措施

1. **加密存储**: 使用 AES-256-GCM 加密存储密码和私钥
2. **会话管理**: SSH 连接自动超时断开（默认 30 分钟）
3. **权限控制**: 所有接口需要登录认证
4. **操作日志**: 记录所有敏感操作
5. **连接池限制**: 限制最大连接数，防止资源耗尽

## 监控机制

1. **定时探测**: 使用 node-cron 实现定时任务，默认每 60 秒探测一次
2. **状态更新**: 实时更新服务器在线/离线状态
3. **日志记录**: 记录每次探测结果，包括响应时间和错误信息
4. **自动清理**: 定时清除过期日志，默认保留 7 天
5. **手动触发**: 支持手动触发全部服务器探测

## 开发进度

- ✅ 阶段一：数据库和模型层（100%）
- ✅ 阶段二：后端核心服务（100%）
- ✅ 阶段三：后端 API 路由（100%）
- ✅ 阶段四：前端机器列表页面（100%）
  - ✅ 服务器列表展示
  - ✅ 添加/编辑服务器对话框
  - ✅ 批量导入/导出功能
  - ✅ Docker 容器详情查看
  - ✅ 服务器详细信息展示
  - ✅ 服务器操作（重启/关机）
- ✅ 阶段五：前端后台管理页面（100%）
  - ✅ 监控配置界面
  - ✅ 探测日志界面
  - ✅ 服务器列表筛选
- ✅ 阶段六：前端 SSH 终端页面（100%）
  - ✅ xterm.js 集成
  - ✅ SSH 终端交互
  - ✅ 多终端标签页支持
  - ✅ SFTP 文件管理界面
- ✅ 阶段七：系统集成和测试（100%）
  - ✅ 模块注册
  - ✅ 监控服务自动启动
- ✅ 阶段八：编写文档和部署准备（100%）

## 环境变量

```env
# 加密密钥（生产环境必须设置）
ENCRYPTION_KEY=your-32-byte-encryption-key
```

## 使用示例

### 添加服务器（密码认证）
```javascript
const response = await fetch('/api/server/accounts', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    name: '生产服务器',
    host: '192.168.1.100',
    port: 22,
    username: 'root',
    auth_type: 'password',
    password: 'your-password',
    tags: ['production', 'web'],
    description: '主要的 Web 服务器'
  })
});
```

### 添加服务器（密钥认证）
```javascript
const response = await fetch('/api/server/accounts', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    name: '开发服务器',
    host: '192.168.1.101',
    port: 22,
    username: 'ubuntu',
    auth_type: 'key',
    private_key: '-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----',
    passphrase: 'key-passphrase',
    tags: ['development'],
    description: '开发环境服务器'
  })
});
```

### 执行 SSH 命令
```javascript
const response = await fetch('/api/server/ssh/exec', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    serverId: 'server-id',
    command: 'df -h'
  })
});

const result = await response.json();
console.log(result.stdout); // 命令输出
```

## 注意事项

1. **生产环境**: 务必设置 `ENCRYPTION_KEY` 环境变量
2. **SSH 密钥**: 私钥必须是 OpenSSH 格式
3. **防火墙**: 确保服务器 SSH 端口可访问
4. **权限**: 某些操作（如重启、关机）需要 sudo 权限
5. **监控频率**: 根据服务器数量调整探测间隔，避免过度消耗资源

## 故障排查

### 连接失败
1. 检查服务器地址和端口是否正确
2. 检查防火墙规则
3. 检查 SSH 服务是否运行
4. 检查认证信息是否正确

### 监控不工作
1. 检查监控服务是否启动：`GET /api/server/monitor/status`
2. 检查监控配置：`GET /api/server/monitor/config`
3. 查看服务器日志

### 加密错误
1. 确保 `ENCRYPTION_KEY` 环境变量已设置
2. 不要更改已有数据的加密密钥

## 贡献

欢迎提交 Issue 和 Pull Request！

## 许可证

MIT
