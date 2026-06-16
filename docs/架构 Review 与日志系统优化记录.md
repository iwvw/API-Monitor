# 架构 Review 与日志系统优化记录

更新时间：2026-06-16

## 结论

当前项目最适合继续走“模块化单体 + 深接口 + 内部事件 + 可观测运行时”的路线。暂不建议直接拆成微服务；当前主要问题是 Go 后端内部接缝还不够深，ServerAgent 和前端 ServerPage 的职责过宽，日志与错误追踪此前不够统一。

## 已修复

### 统一 Go 日志基础设施

- 新增 `backend-go/internal/applog`。
- 使用 Go `slog` 输出结构化 JSON 日志。
- 日志同时写入 stdout 和 `data/logs/app.log`。
- 支持基础文件轮转，默认 10 MB。
- 启动日志改为结构化日志。
- HTTP 请求统一记录：
  - `request_id`
  - `method`
  - `path`
  - `status`
  - `duration_ms`
  - `remote_addr`
  - `user_agent`
- 增加 panic recovery，避免单个请求 panic 直接穿透。
- 响应头统一返回 `X-Request-ID`。
- `response.Error` 返回体增加 `requestId`，方便前端错误和后端日志对应。

### 接入现有设置页日志读取

- 设置页 `/api/settings/app-log-file` 继续读取 `data/logs/app.log`。
- `/api/settings/clear-app-logs` 会清空当前日志文件。
- 测试环境使用自身 `DataDir`，避免全局日志路径污染测试。

### 替换高风险裸日志输出

- ServerAgent 连接、认证、断开、指标持久化失败改为结构化日志。
- Gemini CLI token 缓存失败、请求日志记录失败改为结构化日志。
- OpenAI 模块移除库代码中的 `log.Fatal`，避免随机数失败直接终止后端进程。

## 主要架构偏差

### 1. 路由清单和实际分发仍有重复认知

涉及文件：

- `backend-go/internal/manifest/manifest.go`
- `backend-go/internal/server/server.go`

问题：`manifest` 已经描述了路由归属、鉴权和响应类型，但 `server.go` 仍有大型 `switch`。新增或修改路由时需要维护两处认知。

建议：后续将 `manifest.Route` 绑定处理器注册表，让清单成为唯一入口。

### 2. ServerAgent 仍是过宽模块

涉及文件：

- `backend-go/internal/serveragent/*.go`
- `agent-rust/src/*.rs`
- `src/js/pages/ServerPage.jsx`

问题：连接注册、指标、任务、Docker、终端、文件、Agent 安装、数据库写入都聚在同一大模块中。实时链路 bug 很容易扩散。

建议：抽出 `AgentRuntime` 深模块，只暴露连接状态、发送任务、订阅指标、订阅进度等接口。

### 3. 前端 ServerPage 仍是巨页

涉及文件：

- `src/js/pages/ServerPage.jsx`
- `src/js/components/server/*`
- `src/js/modules/server-*.js`

问题：主机列表、指标图表、Docker、终端、SFTP、Agent 安装共享一个大状态面，容易引发全页重渲染和性能回归。

建议：按工作流拆成纵向切片：

- 主机列表切片
- 指标图表切片
- Docker 切片
- 终端切片
- 文件切片
- Agent 安装切片

### 4. 存储访问缺少稳定仓储接口

问题：多数模块直接打开 SQLite 并写 SQL。表结构、时间格式、事务策略容易散落。

建议：优先对高风险域建立仓储接口：

- 会话与登录尝试
- 主机指标历史
- Agent 连接状态
- 通知历史
- 操作日志

### 5. 内部事件仍以点对点调用为主

问题：通知、审计、实时广播、监控状态等副作用分散在业务调用里。

建议：先引入进程内事件总线，不急于引入 Redis/NATS。等事件模型稳定后再考虑外部队列适配器。

## 后续推荐顺序

1. 抽 `AgentRuntime`，把连接、任务、指标广播从业务路由中隔离。
2. 拆 `ServerPage.jsx`，先拆指标图表和 Docker 两个性能热点。
3. 将 `manifest` 升级为处理器注册入口，删除大分发重复认知。
4. 为主机指标、会话、通知历史补仓储接口。
5. 增加内部事件总线，统一通知、审计、实时广播。
6. 在设置页增加日志筛选能力：按 `request_id`、模块、级别、时间范围过滤。

## 开发规范建议

- 模块内部不要使用 `log.Fatal` 或直接退出进程。
- 后端错误必须可通过 `requestId` 对应到日志。
- 高风险后台 goroutine 必须记录失败日志。
- 新增模块需要明确：
  - 接口
  - 存储表
  - 鉴权方式
  - 事件
  - 日志字段
  - 测试入口
- 前端不要在高频实时数据路径触发全页状态更新。
