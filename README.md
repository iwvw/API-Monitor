# 🚀 API Monitor Dashboard

[![License](https://img.shields.io/github/license/iwvw/api-monitor)](LICENSE)
[![Node.js](https://img.shields.io/badge/Node.js-18+-green.svg)](https://nodejs.org/)
[![Storage](https://img.shields.io/badge/Storage-SQLite3-orange.svg)](https://www.sqlite.org/)

**一个全能型的 API 管理与服务器监控面板**。
它不仅能帮您集中管理 Zeabur、Koyeb、Cloudflare、OpenAI 等多种云服务，还提供了强大的主机管理、实时 SSH 终端及 Docker 容器监控功能。

[🔴 在线演示 (Demo)](https://api-monitor.zeabur.app/)

> [!WARNING]
> 请勿在演示环境中输入真实的敏感数据（如 API Key、服务器密码等）。演示数据将**定期自动清空**。

---

## ✨ 核心特性

### 🖥️ 基础设施管理
- **主机监控**：实时可视化 CPU、内存、磁盘及系统负载数据。
- **SSH Web 终端**：全功能交互式终端，支持多会话切换与断线重连。
- **Docker 管理**：一键控制容器启停、重启，查看实时运行状态。
- **健康拨测**：定时检测主机连通性及响应时间，生成历史趋势图。
- **隐私保护**：支持主机 IP 自动脱敏/隐藏，适合公开演示或共享屏幕。

### ☁️ 云服务集成
- **Zeabur**：多账号余额监控、项目费用追踪、服务生命周期管理。
- **Koyeb**：
  - 支持多账号管理与组织切换。
  - 服务/应用生命周期控制（暂停/重启/重新部署）。
  - 实时日志流查看、实例状态监控及资源用量统计。
- **Cloudflare DNS**：多账号域名管理、DNS 记录快速增删改、代理模式切换。
- **AI 模型 API**：
  - **OpenAI / Antigravity / Gemini**：多端点可用性检测。
  - 实时配额查询、模型列表获取、全链路耗时统计。

### 🛠️ 架构与安全
- **全链路追踪**：引入 **Trace ID**，从 HTTP 请求到数据库审计日志实现全生命周期追踪。
- **结构化日志**：基于 Node.js `AsyncLocalStorage` 的高性能异步 JSON 日志系统。
- **自动脱敏**：智能识别并打码日志及数据库中的 Token、密码、Key 等敏感信息。
- **持久化存储**：采用 SQLite，支持千万级日志存量与自动保留策略（按天/按量清理）。

---

## 📦 快速开始

### 1. Docker 部署 (推荐)

**方式一：Docker Compose (最简)**

```bash
# 1. 下载配置文件
curl -O https://raw.githubusercontent.com/iwvw/api-monitor/main/docker-compose.yml

# 2. 启动服务
docker compose pull && docker compose up -d
```

**方式二：Docker CLI**

```bash
docker run -d --name api-monitor \
  -p 3000:3000 \
  -v $(pwd)/data:/app/data \
  --restart unless-stopped \
  ghcr.io/iwvw/api-monitor:latest
```

### 2. 本地开发

```bash
# 克隆仓库
git clone https://github.com/iwvw/api-monitor.git
cd api-monitor

# 安装依赖
npm install

# 启动开发模式 (热重载: 前端 Vite + 后端 Express)
npm run dev
```

如需仅运行生产环境模式：
```bash
npm run build && npm start
```

---

## 🔒 环境变量配置

支持通过 `.env` 文件或 Docker 环境变量进行配置。可参考根目录下的 `.env.example`。

| 变量名 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `PORT` | `3000` | 服务运行端口 |
| `NODE_ENV` | `production` | 运行环境 (`development` / `production`) |
| `ADMIN_PASSWORD` | - | **初始管理员密码**（首次启动时生效，也可在界面设置） |
| `JWT_SECRET` | (随机) | **强烈建议设置**。用于加密会话 Token，固定此值可防止重启后用户掉线。 |
| `LOG_LEVEL` | `INFO` | 日志级别 (`DEBUG`, `INFO`, `WARN`, `ERROR`) |
| `LOG_RETENTION_DAYS` | `7` | 本地日志文件保留天数 |
| `TRUST_PROXY` | `false` | 若部署在反代后 (如 Nginx/Cloudflare)，建议设为 `true` 以获取真实 IP |

---

## 📁 目录结构

```text
api-monitor/
├── src/                    # 前端源码 & 后端核心
│   ├── index.html          # 前端入口文件
│   ├── css/                # 模块化样式表
│   ├── js/                 # 前端核心逻辑 (Vue 3/Vanilla)
│   ├── templates/          # HTML 模板片段
│   ├── db/                 # 数据库模型 (SQLite) & ORM
│   ├── middleware/         # 中间件 (Auth, Logger, CORS)
│   ├── routes/             # API 路由定义
│   ├── services/           # 业务服务层
│   └── utils/              # 通用工具函数
├── modules/                # 业务功能模块 (插件化架构)
│   ├── _template/          # 模块开发模板
│   ├── server-management/  # 主机/Docker/SSH 管理
│   ├── antigravity-api/    # Antigravity 客户端集成
│   ├── cloudflare-dns/     # Cloudflare DNS 管理
│   ├── gemini-cli-api/     # Gemini CLI 适配器
│   ├── koyeb-api/          # Koyeb 平台集成
│   ├── openai-api/         # OpenAI 接口监控
│   └── zeabur-api/         # Zeabur 平台集成
├── data/                   # 持久化数据 (git忽略)
├── dist/                   # 前端构建产物
├── server.js               # 后端启动入口
├── vite.config.js          # Vite 构建配置
├── Dockerfile              # Docker 构建文件
└── docker-compose.yml      # 容器编排配置
```

---

## 🧩 模块开发指南

本项目采用插件化架构，您可以轻松扩展新功能。详细的开发步骤和规范请参考：

👉 **[模块开发模板使用指南](./modules/_template/README.md)**

---

## 📄 许可证

本项目基于 [MIT](LICENSE) 协议开源。

**Made with ❤️ by [iwvw](https://github.com/iwvw) & [jiujiu532](https://github.com/jiujiu532)**