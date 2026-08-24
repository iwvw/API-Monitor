# API Monitor

API Monitor 是一个自托管的 API 管理、云资源管理与主机监控面板。

集中管理服务器、DNS、对象存储、PaaS、文件分享等各种分散服务。

## 功能概览

- 主机实例监控、实时指标、WebSocket 更新、终端与文件管理
- Docker、进程、网络质量、流量配额与告警
- Cloudflare、阿里云、腾讯云、Koyeb、Fly.io 等云服务管理
- OpenAI 兼容接口、模型调用记录与用量统计
- 可用性监测、公开状态页、自定义域名与首页快捷入口
- 文件中转、TOTP、备份、定时任务、通知模板与系统日志
- 还有很多待定功能>>>>>>

![image](https://image.dooo.ng/t/2026/07/23/6a61fba454686.webp)

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
      - APP_ENV=production
      - SECURE_COOKIES=true
      - ADMIN_PASSWORD=<CHANGE_ME>
      - JWT_SECRET=<CHANGE_ME_TO_A_LONG_RANDOM_STRING>
      - ENCRYPTION_KEY=<CHANGE_ME_TO_ANOTHER_LONG_RANDOM_STRING>
    restart: unless-stopped
```

### Docker CLI

```bash
docker run -d --name api-monitor \
  -p 3000:3000 \
  -v ./data:/app/data \
  -e APP_ENV=production \
  -e SECURE_COOKIES=true \
  -e ADMIN_PASSWORD=<CHANGE_ME> \
  -e JWT_SECRET=<CHANGE_ME_TO_A_LONG_RANDOM_STRING> \
  -e ENCRYPTION_KEY=<CHANGE_ME_TO_ANOTHER_LONG_RANDOM_STRING> \
  --restart unless-stopped \
  iwvw/api-monitor:latest
```

生产模式必须通过 HTTPS 反向代理访问；`Secure` 会话 Cookie 不会在普通 HTTP 页面中生效。本地开发保持默认的 `APP_ENV=development`，可直接使用 `http://localhost:5173`。

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

可通过环境变量或 `.env` 文件配置。下表标注了必选与可选变量；未设置时使用默认值。

### 生产必填

| 变量 | 必选 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `ADMIN_PASSWORD` | 首次部署必选 | - | 初始化管理员密码，仅首次初始化 / 设置密码时使用；一旦设置不可通过环境变量之外的方式修改 |
| `JWT_SECRET` | 生产必选 | - | 启动安全校验项，`APP_ENV=production` 时要求至少 32 字符的长随机串 |
| `ENCRYPTION_KEY` | 生产必选 | - | 敏感凭据（API 密钥、托管凭据等）AES 加密主密钥，生产要求至少 32 字符；变更会导致已加密数据无法解密，请妥善保管 |

### 核心配置（均可选）

| 变量 | 必选 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `PORT` | 可选 | `3000` | 服务端口（`GO_PORT` 优先于 `PORT`） |
| `GO_PORT` | 可选 | - | 服务端口覆盖值，优先于 `PORT` |
| `GO_HOST` | 可选 | `0.0.0.0` | 监听地址 |
| `APP_ENV` | 可选 | `development` | `development` 或 `production`；生产模式启用更严格的安全默认值（未设置时回退 `NODE_ENV`） |
| `NODE_ENV` | 可选 | `development` | `APP_ENV` 未设置时的回退值 |
| `DATA_DIR` | 可选 | `./data` | 数据目录（SQLite、备份、上传等） |
| `DB_NAME` | 可选 | `data.db` | SQLite 数据库文件名，位于 `DATA_DIR` 下 |
| `DIST_DIR` | 可选 | `./dist` | 前端构建产物目录 |
| `PUBLIC_DIR` | 可选 | `./public` | 静态资源目录 |
| `NODE_LEGACY_URL` | 可选 | - | 旧 Node sidecar 后端地址（迁移期兼容用，当前版本无需设置） |
| `ADMIN_AI_DEFAULT_MODEL` | 可选 | - | 管理 AI 默认模型名，留空则面板内手动选择 |
| `GATEWAY_BODY_MAX_MB` | 可选 | `16` | 模型网关转发入口可接受的请求体上限（MB），小内存主机可调小 |

### 安全与网络（均可选）

| 变量 | 必选 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `SECURE_COOKIES` | 可选 | 生产 `true`，开发 `false` | 会话 Cookie 是否仅通过 HTTPS 发送 |
| `ALLOW_LOCAL_SHELL_TASKS` | 可选 | 非生产 `true` | 是否允许后台任务直接执行本机 Shell；生产默认关闭 |
| `TRUSTED_PROXY_CIDRS` | 可选 | - | 允许提供真实客户端 IP 的反向代理 IP/CIDR 列表，逗号分隔 |
| `CORS_ALLOWED_ORIGINS` | 可选 | - | 允许跨域访问 API 的 Origin 白名单，逗号分隔 |
| `DEMO_MODE` | 可选 | `false` | 设为 `true` 启用演示模式（禁止修改密码等写操作） |

### 云厂商 API 端点覆盖（均可选，默认直连官方）

| 变量 | 默认值 |
| --- | --- |
| `CLOUDFLARE_API_BASE_URL` | `https://api.cloudflare.com` |
| `ALIYUN_API_BASE_URL` / `ALIYUN_DNS_ENDPOINT` / `ALIYUN_ECS_ENDPOINT` / `ALIYUN_SWAS_ENDPOINT` / `ALIYUN_CMS_ENDPOINT` | 阿里云整体 API 基址 / 各产品官方端点 |
| `TENCENT_API_BASE_URL` / `TENCENT_DNSPOD_ENDPOINT` / `TENCENT_CVM_ENDPOINT` / `TENCENT_LIGHTHOUSE_ENDPOINT` / `TENCENT_MONITOR_ENDPOINT` | 腾讯云整体 API 基址 / 各产品官方端点 |
| `FLY_GRAPHQL_URL` / `FLY_MACHINES_URL` / `FLY_LOGS_URL` | Fly.io 官方 API 端点 |
| `KOYEB_API_BASE_URL` | `https://app.koyeb.com` |
| `M365_GRAPH_BASE_URL` / `M365_LOGIN_BASE_URL` | Microsoft 365 官方端点 |

通常无需设置；仅在内网隔离 / 自定义网关 / 镜像代理场景覆盖使用。

### 进阶调优（均可选，默认即可用）

| 变量 | 必选 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `API_MONITOR_MEMORY_GUARD` | 可选 | 默认开启 | 内存守卫总开关，设为 `false`/`off` 关闭 |
| `API_MONITOR_MEMORY_LIMIT_MB` | 可选 | 未设置时取 cgroup 内存限制的 70% | 内存使用上限（MB），超过触发主动 GC |
| `API_MONITOR_MEMORY_GC_TRIGGER_RATIO` | 可选 | `0.85` | 内存使用达到上限的比例（`0 < x <= 1`）时触发 GC |
| `API_MONITOR_MEMORY_CHECK_SECONDS` | 可选 | `15` | 内存检查周期（秒） |
| `API_MONITOR_UPTIME_HEARTBEAT_RETENTION_DAYS` | 可选 | - | 可用性监测心跳历史保留天数 |
| `API_MONITOR_AGENT_PRESENCE_MODE` | 可选 | - | Agent 在线状态判定模式 |
| `API_MONITOR_AGENT_OFFLINE_AFTER_MS` / `API_MONITOR_AGENT_SUSPECT_AFTER_MS` / `API_MONITOR_AGENT_STARTUP_GRACE_MS` / `API_MONITOR_AGENT_RECOVERY_SAMPLES` | 可选 | - | Agent 在线状态判定调参 |
| `API_MONITOR_AGENT_METRICS_PERSIST_INTERVAL_MS` | 可选 | - | Agent 指标持久化间隔（毫秒） |
| `API_MONITOR_AGENT_NETWORK_QUALITY_PERSIST_INTERVAL_MS` | 可选 | - | Agent 网络质量采集持久化间隔（毫秒） |
| `API_MONITOR_PPROF` | 可选 | `0` | 设为 `1` 开启 pprof 性能剖析（仅排查性能问题时使用） |

### 开发工具脚本（可选）

| 变量 | 必选 | 说明 |
| --- | --- | --- |
| `API_MONITOR_BASE_URL` | 可选 | 后端冒烟与巡检脚本默认地址（默认 `http://127.0.0.1:3000`） |
| `VIRTUAL_AGENT_URL` / `VIRTUAL_AGENT_SERVER_ID` / `VIRTUAL_AGENT_KEY` | 可选 | 虚拟 Agent 联调参数 |

### Agent（Rust，由安装脚本自动写入，通常无需手动设置）

| 变量 | 必选 | 说明 |
| --- | --- | --- |
| `API_MONITOR_SERVER` | Agent 必填 | 后端服务地址 |
| `API_MONITOR_KEY` | Agent 必填 | Agent 配对密钥 |
| `API_MONITOR_SERVER_ID` | 可选 | 所属主机实例 ID |
| `API_MONITOR_SING_BOX_BIN` | 可选 | sing-box 运行时二进制路径（托管代理用） |
| `API_MONITOR_DOCKER_REGISTRY_MIRRORS` | 可选 | Docker Hub 镜像加速地址，逗号分隔 |
| `API_MONITOR_TRAFFIC_REPORT_SECS` | 可选 | 流量上报间隔（秒，默认 `300`，最小 `60`） |

## 技术栈

- 后端：Go + SQLite
- 前端：React + Vite + Tailwind CSS + Kumo UI
- Agent：Rust
- 实时通信：Engine.IO / WebSocket

## 文档

- [文档索引](./docs/README.md)
- [开发指南](./docs/开发指南.md)
- [API 接口文档](./docs/API接口文档.md)
- [安全加固与扫描计划](./docs/安全加固与扫描计划.md)
- [Kumo UI 规则](./docs/Kumo%20UI%20规则.md)

## 许可证

[MIT](./LICENSE)
