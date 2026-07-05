# API Monitor

API Monitor 是一个自托管的 API 管理、云资源管理与主机监控面板。

集中管理服务器、DNS、对象存储、PaaS、文件分享等。

## 功能概览

- 主机实例监控、实时指标、WebSocket 更新、终端与文件管理
- Docker、进程、网络质量、流量配额与告警
- Cloudflare、阿里云、腾讯云、Koyeb、Fly.io 等云服务管理
- OpenAI 兼容接口、模型调用记录与用量统计
- 可用性监测、公开状态页、自定义域名与首页快捷入口
- 文件中转、TOTP、备份、定时任务、通知模板与系统日志

## 快速部署

### Docker Compose

```yaml
services:
  api-monitor:
    image: iwvw/api-monitor:latest
    container_name: api-monitor
    ports:
      - "3000:3000"
    volumes:
      - ./data:/app/data
    environment:
      - ADMIN_PASSWORD=<CHANGE_ME>
      - JWT_SECRET=<CHANGE_ME_TO_A_LONG_RANDOM_STRING>
    restart: unless-stopped
```

### Docker CLI

```bash
docker run -d --name api-monitor \
  -p 3000:3000 \
  -v ./data:/app/data \
  -e ADMIN_PASSWORD=<CHANGE_ME> \
  -e JWT_SECRET=<CHANGE_ME_TO_A_LONG_RANDOM_STRING> \
  --restart unless-stopped \
  iwvw/api-monitor:latest
```

## 本地开发

```bash
npm install
npm run dev
```

常用命令：

```bash
npm run lint
npm run build
npm run backend-go:test
npm run backend-go:build
```

## 配置

可通过环境变量或 `.env` 配置。发布部署至少建议设置：

| 变量 | 说明 |
| --- | --- |
| `PORT` | 服务端口，默认 `3000` |
| `DATA_DIR` | 数据目录，默认 `./data` |
| `DB_NAME` | SQLite 数据库文件名，默认 `data.db` |
| `ADMIN_PASSWORD` | 初始化管理员密码，仅首次初始化使用 |
| `JWT_SECRET` | 会话密钥，建议使用长随机字符串 |
| `LOG_LEVEL` | 日志级别：`DEBUG`、`INFO`、`WARN`、`ERROR` |

不要把真实密码、Token、Cookie、私钥或云厂商凭证提交到仓库。

## 技术栈

- 后端：Go + SQLite
- 前端：React + Vite + Tailwind CSS + Kumo UI
- Agent：Rust
- 实时通信：Engine.IO / WebSocket

## 文档

- [文档索引](./docs/README.md)
- [开发指南](./docs/开发指南.md)
- [API 接口文档](./docs/API接口文档.md)
- [Kumo UI 规则](./docs/Kumo%20UI%20规则.md)

## 许可证

[MIT](./LICENSE)
