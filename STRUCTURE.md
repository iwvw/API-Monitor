# 🏗️ API Monitor 项目架构说明

本文档详细介绍 API Monitor 的项目结构、代码组织和架构设计。

## 📋 目录

- [项目结构](#项目结构)
- [架构设计](#架构设计)
- [模块说明](#模块说明)
- [代码优化](#代码优化)
- [扩展指南](#扩展指南)

---

## 📁 项目结构

```
api-monitor/
├── src/                        # 源代码目录
│   ├── middleware/             # 中间件
│   │   ├── auth.js            # 认证中间件
│   │   └── cors.js            # CORS 配置
│   ├── services/              # 业务逻辑服务
│   │   ├── config.js          # 配置管理（密码）
│   │   └── session.js         # 会话管理
│   ├── routes/                # 路由模块
│   │   ├── index.js           # 路由汇总
│   │   ├── auth.js            # 认证相关路由
│   │   └── health.js          # 健康检查路由
│   └── utils/                 # 工具函数
│       └── cookie.js          # Cookie 解析
├── modules/                   # 功能模块
│   ├── zeabur-api/            # Zeabur API 管理
│   │   ├── router.js          # 路由
│   │   ├── zeabur-api.js      # API 封装
│   │   └── storage.js         # 数据存储
│   ├── cloudflare-dns/        # Cloudflare DNS 管理
│   │   ├── router.js          # 路由
│   │   ├── cloudflare-api.js  # API 封装
│   │   └── storage.js         # 数据存储
│   └── openai-api/           # OpenAI API 管理
│       ├── router.js          # 路由
│       ├── openai-api.js      # API 封装
│       └── storage.js         # 数据存储
├── config/                    # 配置文件目录
│   ├── zb-accounts.json       # Zeabur 账号配置
│   ├── sessions.json          # 会话数据
│   ├── password.json          # 管理员密码
│   ├── cf-accounts.json       # Cloudflare 账号
│   └── openai-endpoints.json  # OpenAI 端点
├── public/                    # 前端静态文件
│   ├── index.html            # Vue.js 单页应用
│   └── logo.png              # 网站图标
├── server.js                  # 精简的入口文件（76 行）
├── server.js.backup           # 原始文件备份（1128 行）
├── package.json               # 项目依赖
├── Dockerfile                 # Docker 镜像
└── docker-compose.yml         # Docker Compose 配置
```

---

## 🏛️ 架构设计

### 分层架构

```
┌─────────────────────────────────────────┐
│           前端 (Vue.js SPA)             │
│         public/index.html               │
└─────────────────────────────────────────┘
                    ↓ HTTP/HTTPS
┌─────────────────────────────────────────┐
│          路由层 (Routes)                │
│    src/routes/ + modules/*/router.js    │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│         中间件层 (Middleware)           │
│    认证、CORS、错误处理                 │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│          服务层 (Services)              │
│    业务逻辑、会话管理、配置管理         │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│         数据层 (Storage)                │
│    JSON 文件存储 (config/)              │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│        外部 API (Third-party)           │
│  Zeabur / Cloudflare / OpenAI          │
└─────────────────────────────────────────┘
```

### 模块化设计

#### 核心模块

| 模块 | 路径 | 职责 |
|------|------|------|
| **认证中间件** | `src/middleware/auth.js` | Session/密码验证 |
| **CORS 配置** | `src/middleware/cors.js` | 跨域请求处理 |
| **配置管理** | `src/services/config.js` | 配置文件读写 |
| **会话管理** | `src/services/session.js` | Session 生命周期 |
| **认证路由** | `src/routes/auth.js` | 登录/登出接口 |
| **健康检查** | `src/routes/health.js` | 系统状态监控 |

#### 功能模块

| 模块 | 路径 | 功能 |
|------|------|------|
| **Zeabur API** | `modules/zeabur-api/` | 账号、项目、服务管理 |
| **Cloudflare DNS** | `modules/cloudflare-dns/` | 域名、DNS 记录管理 |
| **OpenAI API** | `modules/openai-api/` | 端点、模型管理 |

---

## 📦 模块说明

### 1. 源代码目录 (`src/`)

#### 中间件层 (`src/middleware/`)

**auth.js** - 认证中间件
```javascript
// 功能：
// - 验证 Session Cookie
// - 验证 x-admin-password 头
// - 保护需要认证的路由
```

**cors.js** - CORS 配置
```javascript
// 功能：
// - 配置跨域请求
// - 允许携带 credentials
// - 设置允许的请求方法和头
```

#### 服务层 (`src/services/`)

**config.js** - 配置管理
```javascript
// 功能：
// - 读写密码配置
// - 管理配置文件路径
// - 确保配置目录存在
```

**session.js** - 会话管理
```javascript
// 功能：
// - 创建会话（生成随机 ID）
// - 验证会话（检查有效期）
// - 销毁会话（登出）
// - 清理过期会话
```

#### 路由层 (`src/routes/`)

**index.js** - 路由汇总
```javascript
// 功能：
// - 注册所有路由
// - 统一路由管理
// - 模块化路由加载
```

**auth.js** - 认证路由
```javascript
// 端点：
// - POST /api/login
// - POST /api/logout
// - GET /api/session
// - GET /api/check-password
// - POST /api/set-password
```

**health.js** - 健康检查
```javascript
// 端点：
// - GET /health
// - GET /api/health
```

#### 工具层 (`src/utils/`)

**cookie.js** - Cookie 工具
```javascript
// 功能：
// - 解析 Cookie 字符串
// - 提取特定 Cookie 值
```

### 2. 功能模块 (`modules/`)

每个功能模块包含三个核心文件：

#### 模块结构

```
modules/
└── example-module/
    ├── router.js          # 路由定义
    ├── example-api.js     # API 封装
    └── storage.js         # 数据存储
```

#### Zeabur API 模块

**router.js** - 路由定义
```javascript
// 端点：
// - POST /api/temp-accounts
// - POST /api/temp-projects
// - GET /api/server-accounts
// - POST /api/server-accounts
// - POST /api/project/rename
// - POST /api/service/pause
// - POST /api/service/restart
// - POST /api/service/logs
```

**zeabur-api.js** - API 封装
```javascript
// 功能：
// - 封装 Zeabur GraphQL API
// - 获取账号信息
// - 管理项目和服务
```

**storage.js** - 数据存储
```javascript
// 功能：
// - 读写账号配置
// - 管理 zb-accounts.json
```

#### Cloudflare DNS 模块

**router.js** - 路由定义
```javascript
// 端点：
// - GET /api/cf-dns/accounts
// - POST /api/cf-dns/accounts
// - DELETE /api/cf-dns/accounts/:id
// - GET /api/cf-dns/accounts/:id/zones
// - GET /api/cf-dns/accounts/:id/zones/:zoneId/records
// - POST /api/cf-dns/accounts/:id/zones/:zoneId/records
// - PUT /api/cf-dns/accounts/:id/zones/:zoneId/records/:recordId
// - DELETE /api/cf-dns/accounts/:id/zones/:zoneId/records/:recordId
```

**cloudflare-api.js** - API 封装
```javascript
// 功能：
// - 封装 Cloudflare REST API
// - 管理域名和 DNS 记录
// - 处理代理状态
```

**storage.js** - 数据存储
```javascript
// 功能：
// - 读写账号配置
// - 管理 cf-accounts.json
// - 模板管理
```

#### OpenAI API 模块

**router.js** - 路由定义
```javascript
// 端点：
// - GET /api/openai/endpoints
// - POST /api/openai/endpoints
// - DELETE /api/openai/endpoints/:id
// - POST /api/openai/endpoints/:id/models
// - POST /api/openai/endpoints/:id/check
```

**openai-api.js** - API 封装
```javascript
// 功能：
// - 封装 OpenAI API
// - 获取模型列表
// - 检查端点可用性
```

**storage.js** - 数据存储
```javascript
// 功能：
// - 读写端点配置
// - 管理 openai-endpoints.json
```

### 3. 配置目录 (`config/`)

运行时生成的配置文件：

| 文件 | 说明 | 格式 |
|------|------|------|
| `password.json` | 管理员密码（加密） | `{ "password": "..." }` |
| `sessions.json` | 会话数据 | `{ "sessionId": {...} }` |
| `zb-accounts.json` | Zeabur 账号 | `[{ "name": "...", "token": "..." }]` |
| `cf-accounts.json` | Cloudflare 账号 | `[{ "name": "...", "token": "..." }]` |
| `openai-endpoints.json` | OpenAI 端点 | `[{ "name": "...", "url": "...", "key": "..." }]` |

---

## 🔄 代码优化

### 优化前后对比

| 指标 | 优化前 | 优化后 | 改进 |
|------|--------|--------|------|
| **主文件行数** | 1128 行 | 76 行 | ↓ 93% |
| **模块数量** | 1 个文件 | 15+ 个模块 | 模块化 |
| **职责分离** | 混杂 | 清晰 | ✅ |
| **可维护性** | 低 | 高 | ✅ |
| **可测试性** | 困难 | 容易 | ✅ |
| **可扩展性** | 困难 | 容易 | ✅ |

### 主要改进

#### 1. 代码模块化

**之前**: 所有逻辑都在 `server.js` 中（1128 行）

**现在**: 按职责分离到不同模块
- ✅ 中间件层独立
- ✅ 服务层独立
- ✅ 路由层独立
- ✅ 工具层独立
- ✅ 功能模块独立

#### 2. 主文件精简

**server.js** 从 1128 行精简到 76 行，只负责：
1. 加载环境变量
2. 创建 Express 应用
3. 应用中间件
4. 注册路由
5. 启动服务器

#### 3. 职责清晰

每个模块都有明确的单一职责，符合 SOLID 原则。

#### 4. 易于维护和扩展

- **添加新功能**: 在对应模块中添加
- **修改逻辑**: 只需修改相关模块
- **测试**: 每个模块可独立测试
- **复用**: 服务层代码可复用

---

## 🚀 扩展指南

### 添加新功能模块

#### 步骤 1：创建模块目录

```bash
mkdir -p modules/new-service
cd modules/new-service
```

#### 步骤 2：创建模块文件

**1. router.js** - 路由定义

```javascript
const express = require('express');
const router = express.Router();
const { requireAuth } = require('../../src/middleware/auth');
const storage = require('./storage');
const api = require('./new-service-api');

// 获取账号列表
router.get('/accounts', requireAuth, async (req, res) => {
  try {
    const accounts = storage.getAccounts();
    res.json({ success: true, accounts });
  } catch (error) {
    res.status(500).json({ success: false, message: error.message });
  }
});

// 添加账号
router.post('/accounts', requireAuth, async (req, res) => {
  try {
    const { name, token } = req.body;
    const account = storage.addAccount({ name, token });
    res.json({ success: true, account });
  } catch (error) {
    res.status(500).json({ success: false, message: error.message });
  }
});

module.exports = router;
```

**2. new-service-api.js** - API 封装

```javascript
// 封装第三方 API 调用
class NewServiceAPI {
  constructor(token) {
    this.token = token;
    this.baseURL = 'https://api.example.com';
  }

  async getData() {
    const response = await fetch(`${this.baseURL}/data`, {
      headers: {
        'Authorization': `Bearer ${this.token}`
      }
    });
    return await response.json();
  }
}

module.exports = NewServiceAPI;
```

**3. storage.js** - 数据存储

```javascript
const fs = require('fs');
const path = require('path');

const CONFIG_DIR = process.env.CONFIG_DIR || './config';
const ACCOUNTS_FILE = path.join(CONFIG_DIR, 'new-service-accounts.json');

// 确保配置目录存在
if (!fs.existsSync(CONFIG_DIR)) {
  fs.mkdirSync(CONFIG_DIR, { recursive: true });
}

function getAccounts() {
  if (!fs.existsSync(ACCOUNTS_FILE)) {
    return [];
  }
  const data = fs.readFileSync(ACCOUNTS_FILE, 'utf8');
  return JSON.parse(data);
}

function saveAccounts(accounts) {
  fs.writeFileSync(ACCOUNTS_FILE, JSON.stringify(accounts, null, 2));
}

function addAccount(account) {
  const accounts = getAccounts();
  account.id = Date.now().toString();
  accounts.push(account);
  saveAccounts(accounts);
  return account;
}

module.exports = {
  getAccounts,
  saveAccounts,
  addAccount
};
```

#### 步骤 3：注册路由

在 `src/routes/index.js` 中注册：

```javascript
const newServiceRouter = require('../../modules/new-service/router');

function registerRoutes(app) {
  // ... 其他路由

  // 注册新服务路由
  app.use('/api/new-service', newServiceRouter);
}

module.exports = registerRoutes;
```

#### 步骤 4：更新前端

在 `public/index.html` 中添加 UI 组件。

### 添加新路由

在 `src/routes/` 创建新路由文件：

```javascript
const express = require('express');
const router = express.Router();

router.get('/example', (req, res) => {
  res.json({ message: 'Example route' });
});

module.exports = router;
```

在 `src/routes/index.js` 中注册：

```javascript
const exampleRouter = require('./example');
app.use('/api/example', exampleRouter);
```

### 添加新服务

在 `src/services/` 创建新服务：

```javascript
// src/services/cache.js
class CacheService {
  constructor() {
    this.cache = new Map();
  }

  get(key) {
    return this.cache.get(key);
  }

  set(key, value, ttl = 3600000) {
    this.cache.set(key, value);
    setTimeout(() => this.cache.delete(key), ttl);
  }
}

module.exports = new CacheService();
```

在路由中使用：

```javascript
const cache = require('../services/cache');

router.get('/data', async (req, res) => {
  const cached = cache.get('data');
  if (cached) {
    return res.json(cached);
  }

  const data = await fetchData();
  cache.set('data', data);
  res.json(data);
});
```

### 添加新中间件

在 `src/middleware/` 创建中间件：

```javascript
// src/middleware/rateLimit.js
const rateLimit = new Map();

function rateLimitMiddleware(req, res, next) {
  const ip = req.ip;
  const now = Date.now();
  const limit = 100; // 每分钟 100 次
  const window = 60000; // 1 分钟

  if (!rateLimit.has(ip)) {
    rateLimit.set(ip, { count: 1, resetTime: now + window });
    return next();
  }

  const record = rateLimit.get(ip);
  if (now > record.resetTime) {
    record.count = 1;
    record.resetTime = now + window;
    return next();
  }

  if (record.count >= limit) {
    return res.status(429).json({ error: 'Too many requests' });
  }

  record.count++;
  next();
}

module.exports = rateLimitMiddleware;
```

在 `server.js` 中应用：

```javascript
const rateLimitMiddleware = require('./src/middleware/rateLimit');
app.use(rateLimitMiddleware);
```

---

## 📝 开发规范

### 代码风格

- 使用 ES6+ 语法
- 使用 async/await 处理异步
- 统一使用 2 空格缩进
- 添加必要的注释

### 错误处理

```javascript
router.post('/api/example', async (req, res) => {
  try {
    // 业务逻辑
    const result = await doSomething();
    res.json({ success: true, data: result });
  } catch (error) {
    console.error('Error:', error);
    res.status(500).json({
      success: false,
      message: error.message
    });
  }
});
```

### 安全建议

1. **输入验证** - 验证所有用户输入
2. **错误信息** - 不要暴露敏感信息
3. **认证保护** - 使用 `requireAuth` 中间件
4. **HTTPS** - 生产环境启用 HTTPS
5. **依赖更新** - 定期更新依赖包

---

## 🧪 测试

### 单元测试示例

```javascript
// tests/services/session.test.js
const sessionService = require('../src/services/session');

describe('Session Service', () => {
  test('should create session', () => {
    const session = sessionService.createSession('password123');
    expect(session).toHaveProperty('sessionId');
    expect(session).toHaveProperty('createdAt');
  });

  test('should validate session', () => {
    const session = sessionService.createSession('password123');
    const isValid = sessionService.validateSession(session.sessionId);
    expect(isValid).toBe(true);
  });
});
```

### API 测试示例

```javascript
// tests/api/auth.test.js
const request = require('supertest');
const app = require('../server');

describe('Auth API', () => {
  test('POST /api/login', async () => {
    const response = await request(app)
      .post('/api/login')
      .send({ password: 'test_password' });

    expect(response.status).toBe(200);
    expect(response.body).toHaveProperty('success');
  });
});
```

---

## 🔗 相关文档

- [README.md](./README.md) - 项目总览
- [DEPLOY.md](./DEPLOY.md) - 部署指南
- [SESSION_AUTH.md](./SESSION_AUTH.md) - 认证说明

---

## 📚 技术栈

### 后端

- **Node.js** - JavaScript 运行时
- **Express** - Web 框架
- **CORS** - 跨域支持

### 前端

- **Vue.js 3** - 渐进式框架（CDN）
- **Fetch API** - HTTP 请求
- **LocalStorage** - 本地存储

### 部署

- **Docker** - 容器化
- **Zeabur** - 云平台
- **GitHub Actions** - CI/CD

---

## 💡 最佳实践

### 1. 模块化开发

- 每个功能独立成模块
- 模块间低耦合
- 接口清晰明确

### 2. 错误处理

- 统一错误格式
- 记录错误日志
- 友好的错误提示

### 3. 安全性

- 使用 HTTPS
- 验证所有输入
- 保护敏感数据
- 定期更新依赖

### 4. 性能优化

- 使用缓存
- 减少 API 调用
- 压缩响应数据
- 异步处理

### 5. 可维护性

- 清晰的代码结构
- 完善的文档
- 统一的代码风格
- 充分的测试覆盖

---

## 🎯 未来规划

### 短期目标

- [ ] 添加单元测试
- [ ] 完善错误处理
- [ ] 优化性能
- [ ] 添加日志系统

### 中期目标

- [ ] 支持数据库存储
- [ ] 添加缓存层（Redis）
- [ ] 实现多用户支持
- [ ] 添加权限管理

### 长期目标

- [ ] 微服务架构
- [ ] 消息队列
- [ ] 分布式部署
- [ ] 监控告警系统

---

Made with ❤️ by the API Monitor Team
