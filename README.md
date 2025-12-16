<h1 align="center">
  <img src="public/logo.png" width="32" height="32" style="vertical-align: text-bottom;"> API Monitor
</h1>

一个统一的 API 管理面板，支持多种云服务和 API 的集中管理与监控。

![](https://img.shields.io/badge/Node.js-18+-green.svg)
![](https://img.shields.io/badge/License-MIT-blue.svg)
![](https://img.shields.io/badge/Vue.js-3-brightgreen.svg)
![](https://img.shields.io/badge/Express-4.18-lightgrey.svg)

## ✨ 功能特性

### 🎯 已支持的服务

#### Zeabur 监控
- 💰 **实时余额监控** - 显示每月免费额度剩余
- 💸 **项目费用追踪** - 每个项目的实时费用统计
- ✏️ **项目快速改名** - 点击铅笔图标即可重命名项目
- 🌐 **域名显示** - 显示项目的所有域名，点击直接访问
- 🐳 **服务状态监控** - 显示所有服务的运行状态和资源配置
- 👥 **多账号支持** - 同时管理多个 Zeabur 账号
- 🔄 **自动刷新** - 每 30 秒自动更新数据
- ⏸️ **服务控制** - 暂停、启动、重启服务
- 📋 **查看日志** - 实时查看服务运行日志

#### Cloudflare DNS 管理
- 🔑 **多账号管理** - 支持添加多个 Cloudflare API Token
- 🌍 **域名列表** - 查看所有托管的域名
- 📝 **DNS 记录管理** - 添加、编辑、删除 DNS 记录
- ⚡ **快速编辑** - 双击名称或内容列直接编辑
- 🔄 **快速切换** - 一键切换 A/AAAA/CNAME 记录的目标地址
- 📋 **模板管理** - 保存常用 DNS 配置为模板，快速应用
- 🟠 **代理状态** - 轻松开启/关闭 Cloudflare 代理（橙云）

#### OpenAI API（预览版）
- 🤖 **模型列表** - 查看所有可用的 AI 模型
- ✅ **可用性检测** - 实时检测 API 端点状态
- 🔑 **多端点管理** - 支持管理多个 OpenAI 兼容端点

### 🚀 即将支持
- 更多云服务平台监控（AWS、Azure、阿里云等）
- 更多 DNS 服务商支持（DNSPod、阿里云 DNS 等）
- 域名注册商管理
- SSL 证书监控
- 服务器性能监控
- 告警通知系统

## 📁 项目结构

```
api-monitor/
├── src/                        # 源代码目录
│   ├── middleware/             # 中间件
│   │   ├── auth.js            # 认证中间件
│   │   └── cors.js            # CORS 配置
│   ├── services/              # 业务逻辑服务
│   │   ├── config.js          # 配置管理
│   │   └── session.js         # 会话管理
│   ├── routes/                # 路由模块
│   │   ├── index.js           # 路由汇总
│   │   ├── auth.js            # 认证路由
│   │   └── health.js          # 健康检查
│   └── utils/                 # 工具函数
│       └── cookie.js          # Cookie 解析
├── modules/                   # 功能模块
│   ├── zeabur-api/            # Zeabur 监控模块
│   │   ├── router.js          # 路由
│   │   ├── zeabur-api.js      # API 封装
│   │   └── storage.js         # 数据存储
│   ├── cloudflare-dns/        # Cloudflare DNS 模块
│   │   ├── router.js          # 路由
│   │   ├── cloudflare-api.js  # API 封装
│   │   └── storage.js         # 数据存储
│   └── openai-api/           # OpenAI API 模块
│       ├── router.js          # 路由
│       ├── openai-api.js      # API 封装
│       └── storage.js         # 数据存储
├── config/                    # 配置文件目录（运行时生成）
│   ├── password.json          # 管理员密码
│   ├── sessions.json          # 会话数据
│   ├── zb-accounts.json       # Zeabur 账号
│   ├── cf-accounts.json       # Cloudflare 账号
│   └── openai-endpoints.json  # OpenAI 端点
├── public/                    # 前端静态文件
│   ├── index.html            # Vue.js 单页应用
│   └── logo.png              # 网站图标
├── .github/workflows/         # CI/CD 配置
│   └── docker-publish.yml    # Docker 自动构建
├── server.js                  # Express 服务器入口
├── package.json              # 项目依赖
├── Dockerfile                # Docker 镜像构建
├── docker-compose.yml        # Docker Compose 配置
├── zbpack.json               # Zeabur 部署配置
├── DEPLOY.md                 # 部署指南
├── SESSION_AUTH.md           # 认证说明
├── STRUCTURE.md              # 架构说明
└── README.md                 # 项目说明
```

> 💡 详细的架构说明请查看 [STRUCTURE.md](./STRUCTURE.md)

## 📦 快速开始

### 前置要求

- Node.js 18+
- 至少一个支持的服务账号（Zeabur / Cloudflare）

### 本地部署

```bash
# 1. 克隆项目
git clone https://github.com/iwvw/api-monitor.git
cd api-monitor

# 2. 安装依赖
npm install

# 3. 启动服务（开发模式，支持热重载）
npm run dev

# 或直接启动
npm start

# 4. 访问应用
# 打开浏览器访问：http://localhost:3000
```

### 云平台部署

#### Zeabur 部署（推荐）

详细部署步骤请查看 [DEPLOY.md](./DEPLOY.md)

#### 其他平台

本项目支持部署到任何支持 Node.js 的平台：
- Vercel
- Railway
- Render
- Heroku
- 阿里云 / 腾讯云

## 🐳 Docker 部署

### 使用 Docker 运行

```bash
docker run -d --name api-monitor \
  -p 3000:3000 \
  -e NODE_ENV=production \
  -e ADMIN_PASSWORD="your_secure_password" \
  -v $(pwd)/data:/app/config \
  ghcr.io/iwvw/api-monitor:latest
```

### 使用 Docker Compose（推荐）

```yaml
version: '3.8'

services:
  api-monitor:
    image: ghcr.io/iwvw/api-monitor:latest
    container_name: api-monitor
    ports:
      - "3000:3000"
    environment:
      - NODE_ENV=production
      - PORT=3000
      - ADMIN_PASSWORD=your_secure_password
    volumes:
      - ./data:/app/config
    restart: unless-stopped
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3000/"]
      interval: 30s
      timeout: 10s
      retries: 3
```

启动：

```bash
docker-compose up -d
```

## 📖 使用说明

### 首次使用

1. 访问应用后，首次使用需要设置管理员密码（至少 6 位）
2. 设置完成后，使用密码登录
3. 根据需要切换不同的功能标签页

### 添加 Zeabur 账号

1. 点击 **"Zeabur 监控"** 标签页
2. 点击 **"⚙️ 管理账号"**
3. 输入账号名称和 API Token
4. 点击 **"➕ 添加到列表"**

**获取 Zeabur API Token：**
1. 登录 [Zeabur 控制台](https://zeabur.com)
2. 点击右上角头像 → **Settings**
3. 找到 **Developer** 或 **API Keys** 选项
4. 点击 **Create Token**

### 添加 Cloudflare 账号

1. 点击 **"CF DNS 管理"** 标签页
2. 切换到 **"账号管理"** 子标签
3. 点击 **"添加账号"**
4. 输入账号名称和 API Token

**获取 Cloudflare API Token：**
1. 登录 [Cloudflare Dashboard](https://dash.cloudflare.com)
2. 点击右上角头像 → **My Profile**
3. 选择 **API Tokens** 标签
4. 点击 **Create Token**
5. 选择 **Edit zone DNS** 模板或自定义权限

## 🔧 环境变量

| 变量名 | 说明 | 默认值 |
|--------|------|--------|
| `PORT` | 服务端口 | `3000` |
| `NODE_ENV` | 运行环境 | `development` |
| `ADMIN_PASSWORD` | 管理员密码 | - |
| `CONFIG_DIR` | 配置文件目录 | `./config` |

## 🔒 安全说明

### 认证机制
- 🔐 **Session 认证** - 基于服务器内存的会话管理
- 🍪 **HttpOnly Cookie** - 防止 XSS 攻击
- ⏰ **会话过期** - 2 天自动过期，需重新登录
- 🔑 **密码保护** - 管理员密码至少 6 位

> 💡 详细的认证说明请查看 [SESSION_AUTH.md](./SESSION_AUTH.md)

### API Token 安全
- ✅ Token 存储在服务器配置文件中
- ✅ 输入时自动打码显示
- ✅ 不会暴露在前端代码或浏览器中
- ✅ 支持多账号隔离管理

### 重要提示

⚠️ **请勿将以下文件提交到 Git：**
- `config/` 目录下的所有配置文件
- `.env` 文件
- 任何包含敏感信息的文件

⚠️ **生产环境建议：**
- 设置强密码（建议 12 位以上）
- 定期更换 API Token
- 启用 HTTPS（设置 `NODE_ENV=production`）
- 定期备份配置文件
- 限制服务器访问 IP

## 🔄 API 端点

### 认证相关
| 方法 | 端点 | 说明 |
|------|------|------|
| `POST` | `/api/login` | 用户登录 |
| `POST` | `/api/logout` | 用户登出 |
| `GET` | `/api/session` | 检查会话状态 |
| `GET` | `/api/check-password` | 检查是否已设置密码 |
| `POST` | `/api/set-password` | 设置管理员密码 |

### Zeabur 监控
| 方法 | 端点 | 说明 |
|------|------|------|
| `POST` | `/api/temp-accounts` | 获取账号信息 |
| `POST` | `/api/temp-projects` | 获取项目信息 |
| `POST` | `/api/validate-account` | 验证账号 |
| `GET` | `/api/server-accounts` | 获取服务器存储的账号 |
| `POST` | `/api/server-accounts` | 保存账号到服务器 |
| `POST` | `/api/project/rename` | 重命名项目 |
| `POST` | `/api/service/pause` | 暂停服务 |
| `POST` | `/api/service/restart` | 重启服务 |
| `POST` | `/api/service/logs` | 获取服务日志 |

### Cloudflare DNS
| 方法 | 端点 | 说明 |
|------|------|------|
| `GET` | `/api/cf-dns/accounts` | 获取账号列表 |
| `POST` | `/api/cf-dns/accounts` | 添加账号 |
| `DELETE` | `/api/cf-dns/accounts/:id` | 删除账号 |
| `GET` | `/api/cf-dns/accounts/:id/zones` | 获取域名列表 |
| `GET` | `/api/cf-dns/accounts/:id/zones/:zoneId/records` | 获取 DNS 记录 |
| `POST` | `/api/cf-dns/accounts/:id/zones/:zoneId/records` | 添加 DNS 记录 |
| `PUT` | `/api/cf-dns/accounts/:id/zones/:zoneId/records/:recordId` | 更新 DNS 记录 |
| `DELETE` | `/api/cf-dns/accounts/:id/zones/:zoneId/records/:recordId` | 删除 DNS 记录 |
| `POST` | `/api/cf-dns/accounts/:id/zones/:zoneId/switch` | 快速切换记录 |
| `GET` | `/api/cf-dns/templates` | 获取模板列表 |
| `POST` | `/api/cf-dns/templates` | 添加模板 |

### OpenAI API
| 方法 | 端点 | 说明 |
|------|------|------|
| `GET` | `/api/openai/endpoints` | 获取端点列表 |
| `POST` | `/api/openai/endpoints` | 添加端点 |
| `DELETE` | `/api/openai/endpoints/:id` | 删除端点 |
| `POST` | `/api/openai/endpoints/:id/models` | 获取模型列表 |
| `POST` | `/api/openai/endpoints/:id/check` | 检查可用性 |

## 🛠️ 技术栈

- **后端**：Node.js + Express
- **前端**：Vue.js 3 (CDN)
- **容器**：Docker + Docker Compose
- **CI/CD**：GitHub Actions

## 🤝 贡献指南

欢迎提交 Issue 和 Pull Request！

### 如何贡献

1. **Fork 本仓库**
2. **创建特性分支** (`git checkout -b feature/AmazingFeature`)
3. **提交更改** (`git commit -m 'Add some AmazingFeature'`)
4. **推送到分支** (`git push origin feature/AmazingFeature`)
5. **提交 Pull Request**

### 添加新服务支持

如果您希望添加对新服务的支持：

1. 在 `modules/` 目录下创建新的模块目录
2. 创建以下文件：
   - `router.js` - 路由定义
   - `xxx-api.js` - API 封装
   - `storage.js` - 数据存储
3. 在 `src/routes/index.js` 中注册模块路由
4. 更新前端 `public/index.html` 添加 UI
5. 提交 Pull Request

### 代码规范

- 使用 ES6+ 语法
- 保持代码简洁清晰
- 添加必要的注释
- 遵循现有的代码风格

## 📄 许可证

MIT License - 自由使用和修改

详见 [LICENSE](./LICENSE) 文件

## 🙏 致谢

感谢所有贡献者和使用者的支持！

Made with ❤️ [iwvw](https://github.com/iwvw)
❤️ [jiujiu532](https://github.com/jiujiu532)