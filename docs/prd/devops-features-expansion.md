# 开发者运维看板功能扩展 PRD

最后更新：2026-06-30

## Problem Statement

当前 API Monitor 已具备基础的主机监控、PaaS云服务聚合、定时任务和可用性监测。但对于核心开发者和自建服务运维者，系统的深度管控和数据防护能力仍有几处显著空白：
1. **Docker 容器黑盒**：用户虽能登录主机执行 shell，但缺乏直观的容器列表、状态指示、重启控制和日志浏览，难以快速排查容器化服务的故障。
2. **SSL 证书到期遗忘**：可用性监测只检查 HTTP 状态码，无法在 HTTPS 证书临近过期时进行可视化倒计时和提早预警。
3. **OpenAI 代理透传黑盒**：缺少对网关中转请求的流量、延迟、模型消耗占比及令牌（Token）的统计分析与可视化图表。
4. **数据备份空白**：系统的 SQLite 数据库、定时任务数据及文件柜数据缺乏自动化的多云（OSS/COS/R2）云端打包备份，存在单点丢失隐患。
5. **多端日志分散**：各个服务器和运行容器的日志分散，不方便统一检索与实时流式过滤。

## Solution

在系统内增加与优化五个核心能力模块，均采用 Kumo 标准 UI 规范实现：
1. **Docker 容器管理**（集成于 `ServerPage`）：显示容器列表、健康度、端口，支持启动/停止/重启/查看实时日志。
2. **SSL 证书监控**（集成于 `UptimePage`）：自动解析 HTTPS 域名证书，提供临期倒计时与告警通知。
3. **OpenAI 网关分析**（集成于 `OpenAIPage`）：提供时序图表展示请求量、延迟变化及 Token 消费统计。
4. **多云自动备份中心**（新增工具箱子页面）：支持定时打包本地 SQLite 与文件柜，增量同步到阿里云 OSS、腾讯云 COS、或 S3 兼容的 Cloudflare R2。
5. **统一日志查看器**（新增工具箱子页面）：允许集中查阅并流式搜索系统运行日志与对接节点的日志流。

---

## User Stories

### 1. Docker 容器管理
1. 作为管理员，我想在主机详情中查看该主机上所有 Docker 容器的状态，以便了解服务是否正常运行。
2. 作为管理员，我想对单个容器进行一键启动、停止、重启，以便在容器挂掉时快速恢复。
3. 作为管理员，我想直接在浏览器中查看容器的最新 100 行实时日志，以便排查报错。

### 2. SSL 证书监控
4. 作为管理员，我想在可用性监测（Uptime）中看到每个 HTTPS 站点的证书剩余有效天数，以便及时续期。
5. 作为管理员，我想在证书有效期不足 15 天和 7 天时收到系统通知，以免网站访问中断。

### 3. OpenAI 网关流量分析
6. 作为管理员，我想查看每日 OpenAI 代理的请求次数、平均响应时间与错误率，以便了解网关的整体负载。
7. 作为管理员，我想查看各端点及模型的 Token 消费分布，以便进行成本核算与分配。
8. 作为管理员，我想检索历史请求记录（包含时间、状态码、延迟和模型名），以便定位特定异常请求。

### 4. 多云自动备份中心
9. 作为管理员，我想配置阿里云 OSS、腾讯云 COS 或 Cloudflare R2 的连接凭证，以便指定备份存储。
10. 作为管理员，我想设定每日自动备份时间，将系统 SQLite 数据库与文件柜压缩上传，以便防范数据丢失。
11. 作为管理员，我想在备份中心查看历史备份任务记录，并支持一键下载历史备份文件。

### 5. 统一日志查看器
12. 作为管理员，我想在单一页面查看系统各模块的最新的日志流，以便快速了解整体运行状况。
13. 作为管理员，我想使用正则表达式对日志流进行关键字过滤与高亮，以便从海量日志中筛选关键报错。

---

## Functional Requirements

### Docker 容器管理 (Server Docker Tab)
- 接口对接：通过在 `ServerPage` 原有的 SSH 会话连接上执行 `docker ps -a --format ...` 命令进行轻量级获取，无需在远端安装独立的代理守护进程。
- 提供容器状态标识（运行中/已暂停/已退出）、端口映射、创建时间展示。
- 动作按钮：启动 (`docker start`)、停止 (`docker stop`)、重启 (`docker restart`)。
- 实时日志查看：执行 `docker logs --tail 100` 获取并流式展示。

### SSL 证书到期监测 (Uptime Expiry Panel)
- 可用性探测线程在进行 HTTP 校验时，握手阶段自动提取客户端证书链，并计算 `NotAfter` 时间差。
- 在 `UptimePage` 的表格和详情卡片中增加“证书状态”与“剩余天数”列。
- 在系统定时检测中，若发现证书天数小于设置的警戒线（默认 15 天），则自动向 `NotificationPage` 模块发送告警日志并触发通知通道。

### OpenAI 网关分析 (OpenAI Analytics View)
- 后端中转服务在处理 `/v1/chat/completions` 等请求后，将流量指标（状态码、延迟、Prompt Token、Completion Token、端点 ID、模型名）保存到 SQLite 本地表中（不保存敏感请求体）。
- 前端在 `OpenAIPage` 下增加“网关分析”子 Tab：
  - **折线图**：展示每日请求数与耗时变动。
  - **分类饼图**：展示各模型 Token 的消耗比例。
  - **日志表格**：分页展示最近 100 条请求的请求日志（字段包括：时间、端点、模型、状态、耗时、Token数）。

### 多云自动备份中心 (Backup Page)
- 页面入口：`工具箱` -> `备份中心` (`/tools/backup`)。
- 配置项：
  - 存储渠道：本地目录 / 阿里云 OSS / 腾讯云 COS / S3 兼容服务。
  - 凭证信息：Endpoint、Bucket、AccessKeyID、AccessKeySecret。
  - 自动备份计划：Cron 表达式。
- 动作支持：立即备份、下载备份、删除备份、手动恢复。
- 备份内容：SQLite 数据库 (`data/api-monitor.db`)、文件柜存储目录。

### 统一日志查看器 (Log Viewer Page)
- 页面入口：`系统` -> `系统日志` (`/system/logs`)。
- 后端实现：拦截读取系统 `api-monitor.log`。
- 前端组件：一个高性能的终端样式的日志控制台：
  - 实时滚动刷新（可暂停）。
  - 支持按日志级别（DEBUG/INFO/WARN/ERROR）筛选。
  - 支持关键字正则检索，匹配项变黄高亮。

---

## API Contract Draft

### Docker
- `GET /api/servers/:id/docker/containers` - 获取容器列表
- `POST /api/servers/:id/docker/containers/:containerId/action` - 触发动作 (start/stop/restart)
- `GET /api/servers/:id/docker/containers/:containerId/logs` - 获取容器最新日志

### SSL
- `GET /api/uptime/ssl-status` - 获取所有监控域名的证书天数列表

### OpenAI Analytics
- `GET /api/openai/analytics/summary` - 获取分析汇总指标
- `GET /api/openai/analytics/charts` - 获取趋势图表数据
- `GET /api/openai/analytics/logs` - 获取分页请求日志

### Backup
- `GET /api/backup/configs` - 获取备份配置
- `POST /api/backup/configs` - 保存备份配置
- `GET /api/backup/records` - 查看备份历史记录
- `POST /api/backup/run` - 手动触发备份
- `DELETE /api/backup/records/:id` - 删除备份记录

### Logs
- `GET /api/system/logs/stream` - 查看最新日志流
- `GET /api/system/logs/download` - 下载完整系统日志

---

## Release & Implementation Plan

为保证主干分支始终可正常编译和部署，将按以下切面分布迭代：
1. **第一阶段：OpenAI 流量网关分析** - 数据打点落库与前端可视化图表 Tab。
2. **第二阶段：SSL 证书监控** - 探针链解析与可用性看板联动。
3. **第三阶段：主机 Docker 可视化** - 基于现有 SSH 通道的远端容器状态指令映射。
4. **第四阶段：多云自动备份中心** - 打包压缩算法及 OSS/COS/S3 API 数据同步接口。
5. **第五阶段：日志查看器与全功能集成** - 系统控制台流式对接与整体回归测试。
