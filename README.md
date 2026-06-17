<p align="center">
  <img src="./src/logo.svg" width="120" height="120" alt="API Monitor Logo">
</p>

<h1 align="center">API Monitor</h1>

<p align="center">
  <a href="https://github.com/iwvw/api-monitor/blob/main/LICENSE"><img src="https://img.shields.io/github/license/iwvw/api-monitor" alt="License"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.23+-00ADD8.svg" alt="Go"></a>
  <a href="https://react.dev/"><img src="https://img.shields.io/badge/React-19+-61DAFB.svg" alt="React"></a>
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

# 前端开发
npm install
npm run dev          # 启动 Vite 开发服务器 (http://localhost:5173)

# 后端开发 (Go)
cd backend-go
go build -o api-monitor.exe ./cmd/api-monitor
./api-monitor.exe    # 启动 Go 后端 (http://localhost:3000)
```

**快速启动脚本 (Windows)**

```bash
# 使用启动脚本（自动构建 + 启动）
.\scripts\start.bat
```

如需构建生产版本：

```bash
npm run build        # 构建前端到 dist/
cd backend-go && go build -o api-monitor.exe ./cmd/api-monitor
```

---

## 🔒 环境变量配置

支持通过 `.env` 文件或 Docker 环境变量进行配置。可参考根目录下的 `.env.example`。

| 变量名 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `PORT` | `3000` | 服务运行端口 |
| `DATA_DIR` | `./data` | 数据持久化目录 (数据库与日志存放路径) |
| `DB_NAME` | `data.db` | 数据库文件名 |
| `LOG_LEVEL` | `INFO` | 日志级别 (`DEBUG`, `INFO`, `WARN`, `ERROR`) |
| `JWT_SECRET` | (随机) | **强烈建议设置**。用于加密会话 Token |
| `ADMIN_PASSWORD` | - | **初始管理员密码**（首次启动时生效） |

---

## 📁 目录结构

```
api-monitor/
├── backend-go/            # Go 后端服务
│   ├── cmd/api-monitor/   # 主程序入口
│   └── internal/          # 内部包（路由、服务、数据库等）
├── agent-rust/            # Rust Agent 客户端
│   └── src/               # Agent 源码
├── src/                   # 前端源码
│   ├── js/                # React 组件与页面
│   └── css/               # 样式文件
├── docs/                  # 项目文档
├── scripts/               # 脚本工具
├── data/                  # 数据目录（运行时生成，已忽略）
├── dist/                  # 前端构建产物（已忽略）
└── vite.config.mjs        # Vite 配置
```

详细说明 → [docs/目录结构说明.md](./docs/目录结构说明.md)

---

## 🏗️ 技术栈

**后端**: Go 1.23+ + SQLite3 + Engine.IO
**前端**: React 19 + Vite 7 + Tailwind CSS 4 + Zustand
**Agent**: Rust + Tokio + Engine.IO Client
**存储**: SQLite3
**实时通信**: Engine.IO / WebSocket

---

## 📚 文档

- [项目架构与技术详解](./docs/项目架构与技术详解.md) - 完整技术架构
- [开发指南](./docs/开发指南.md) - 开发环境与开发流程
- [API 接口文档](./docs/API接口文档.md) - REST API 完整参考
- [目录结构说明](./docs/目录结构说明.md) - 目录结构详解
- [系统问题诊断报告](./docs/系统问题诊断报告.md) - 问题诊断与解决方案

更多文档请查看 [docs/](./docs/) 目录。

---

## 📄 许可证

本项目基于 [MIT](LICENSE) 协议开源。

**Made with ❤️ by [iwvw](https://github.com/iwvw) & [jiujiu532](https://github.com/jiujiu532)**


