<p align="center">
  <img src="./src/logo.svg" width="120" height="120" alt="API Monitor Logo">
</p>

<h1 align="center">API Monitor</h1>

<p align="center">
  <a href="https://github.com/iwvw/api-monitor/blob/main/LICENSE"><img src="https://img.shields.io/github/license/iwvw/api-monitor" alt="License"></a>
  <a href="https://nodejs.org/"><img src="https://img.shields.io/badge/Node.js-20+-green.svg" alt="Node.js"></a>
  <a href="https://www.sqlite.org/"><img src="https://img.shields.io/badge/Storage-SQLite3-orange.svg" alt="Storage"></a>
  <a href="https://hub.docker.com/r/iwvw/api-monitor"><img src="https://img.shields.io/docker/pulls/iwvw/api-monitor.svg" alt="Docker Pulls"></a>
  <a href="https://github.com/iwvw/api-monitor/actions"><img src="https://img.shields.io/github/actions/workflow/status/iwvw/api-monitor/docker-publish.yml" alt="Build Status"></a>
  <img src="https://img.shields.io/badge/Platform-AMD64%20%7C%20ARM64-blue.svg" alt="Platforms">
  <img src="https://stone.professorlee.work/api/stone/iwvw/API-Monitor" alt="Stone Badge" width="200" />

</p>

---

**一个全能型的 API 管理与服务器监控面板**。
主机、实时 终端、Docker、云服务集成，包括 Cloudflare、OpenAI、Koyeb。

支持Antigravity / Gemini 的模型转 API 调用，有完善的额度使用统计、日志记录、模型列表获取、全链路耗时统计。

[🔵 Docker Hub](https://hub.docker.com/r/iwvw/api-monitor)

> [!WARNING]
> 请勿在演示环境中输入真实的敏感数据

## 📦 快速开始
 
### 1. Docker 部署 (推荐)

**方式一：Docker Compose (最简)**

```yaml
version: '3.8'
services:
  api-monitor:
    image: iwvw/api-monitor:latest
    container_name: api-monitor
    ports:
      - "3000:3000"
    volumes:
      - ./data:/app/data
    restart: unless-stopped
```

**方式二：Docker CLI**

```bash
docker run -d --name api-monitor \
  -p 3000:3000 \
  -v $(pwd)/data:/app/data \
  --restart unless-stopped \
  iwvw/api-monitor:latest
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
| `JWT_SECRET` | (随机) | **强烈建议设置**。用于加密会话 Token |
| `DATA_DIR` | `/app/data` | 数据持久化目录 (数据库与日志存放路径) |
| `DB_NAME` | `data.db` | 数据库文件名 |
| `LOG_LEVEL` | `INFO` | 日志级别 (`DEBUG`, `INFO`, `WARN`, `ERROR`) |
| `LOG_RETENTION_DAYS` | `7` | 本地日志文件保留天数 |
| `TRUST_PROXY` | `false` | 若部署在反代后 (如 Nginx/CF)，建议设为 `true` |
| `LOW_MEMORY_MODE` | `1` (Docker) | 小内存容器优化开关，延迟/禁用非关键重依赖 |
| `LAZY_MODULE_ROUTES` | `1` (Docker) | 按首次请求加载非后台模块路由，降低启动 RSS |
| `GEOIP_LOOKUP` | `0` (Docker/Fly) | 是否启用 GeoIP 国家识别；开启会加载较大的 `geoip-lite` 数据库 |
| `JSON_BODY_LIMIT` | `5mb` (Docker) | JSON 请求体上限，小内存容器建议保持较低 |
| `UPLOAD_MAX_FILE_SIZE_MB` | `50` (Docker) | 单文件上传上限；文件使用临时文件落盘以降低内存峰值 |
| `NODE_OPTIONS` | `--max-old-space-size=128` (Docker) | Node.js 堆内存上限，适合 200MB 级容器 |
| `VITE_USE_CDN` | `true` | 是否启用 CDN 加载静态资源 (构建时生效) |
| `VITE_CDN_PROVIDER`| `npmmirror` | CDN 节点选择 (`npmmirror`, `jsdelivr`, `unpkg`, `bootcdn`) |

---

## 📁 目录结构

```
api-monitor/
├── server.js              # 应用入口
├── src/                   # 核心源码
│   ├── js/modules/        # 前端业务模块
│   ├── db/                # 数据库层
│   ├── middleware/        # Express 中间件
│   ├── routes/            # API 路由
│   ├── services/          # 业务服务
│   └── utils/             # 工具函数
├── modules/               # 可插拔业务模块
│   ├── server-api/        # 服务器/SSH/Docker
│   ├── cloudflare-api/    # Cloudflare DNS
│   ├── antigravity-api/   # Antigravity Agent
│   ├── music-api/         # 网易云音乐代理
│   └── ...                # 更多模块
├── data/                  # 持久化目录 (挂载点)
└── dist/                  # 生产构建产物
```

详细架构说明 → [docs/DESIGN.md](./docs/DESIGN.md)

---

## 🧩 模块开发指南

本项目采用插件化架构，您可以轻松扩展新功能。详细的开发步骤和规范请参考：

👉 **[模块开发模板使用指南](./modules/_template/README.md)**

---

## 📄 许可证

本项目基于 [MIT](LICENSE) 协议开源。

**Made with ❤️ by [iwvw](https://github.com/iwvw) & [jiujiu532](https://github.com/jiujiu532)**


