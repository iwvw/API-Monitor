# 管理 AI 模块 — 共通实现约定

最后更新：2026-08-13

> 本文件是子代理实现「管理AI」四份子 PRD 时必须遵守的接线约定。所有代码块直接从仓库源码摘录，子代理应**直接复制**而非重新查找。
>
> 配套子 PRD：[01-核心引擎](./管理AI核心引擎与数据层.md) · [02-Web侧栏](./管理AIWeb侧栏前端.md) · [03-频道与Telegram](./管理AI频道接入与Telegram.md) · [04-安全审批](./管理AI安全审批与审计.md)
>
> 红线：manifest.go · server.go · store.js · MainLayout.jsx 为单持有者文件，改后必须逐行审阅。

---

## 目录

1. [Go 模块骨架](#1-go-模块骨架)
2. [manifest.go 路由登记](#2-manifestgo-路由登记)
3. [server.go 接线（三处）](#3-servergo-接线三处)
4. [system_config 读写模式](#4-system_config-读写模式)
5. [secure 加密/解密](#5-secure-加密解密)
6. [notification Telegram 复用](#6-notification-telegram-复用)
7. [callAPIFromAI 工具桥](#7-callapifromai-工具桥)
8. [OpenAI 网关 LLM 复用](#8-openai-网关-llm-复用)
9. [ensureSchema 与 ensureSQLiteColumn](#9-ensureschema-与-ensuresqlitecolumn)
10. [randomID 生成](#10-randomid-生成)
11. [store.js 模块注册](#11-storejs-模块注册)
12. [MainLayout.jsx 接线](#12-mainlayoutjsx-接线)
13. [Kumo 组件 import 路径](#13-kumo-组件-import-路径)
14. [config 环境变量读取](#14-config-环境变量读取)
15. [前端 SSE 接入模式](#15-前端-sse-接入模式)

---

## 1. Go 模块骨架

### 目录结构

```
backend-go/internal/adminai/
  channel/
    channel.go       # Channel 接口定义
    telegram.go      # Telegram 实现
  service.go         # 主 Service（New / ServeHTTP / ensureSchema / open）
```

### Service 初始化（`service.go`）

```go
package adminai

import (
    "context"
    "database/sql"
    "net/http"
    "sync"
    "time"

    "github.com/iwvw/api-monitor/backend-go/internal/config"
    "github.com/iwvw/api-monitor/backend-go/internal/database"
    "github.com/iwvw/api-monitor/backend-go/internal/response"
)

type Service struct {
    cfg        config.Config
    store      *database.DB
    schemaOnce sync.Once
    schemaErr  error
}

func New(cfg config.Config) *Service {
    return &Service{
        cfg:   cfg,
        store: database.New(cfg),
    }
}

func (s *Service) open(ctx context.Context) (*sql.DB, error) {
    db, err := s.store.Open(ctx)
    if err != nil {
        return nil, err
    }
    s.schemaOnce.Do(func() {
        s.schemaErr = s.ensureSchema(ctx, db)
    })
    if s.schemaErr != nil {
        db.Close()
        return nil, s.schemaErr
    }
    return db, nil
}
```

### ServeHTTP 模式（凡不在 `serveSystemControlRoute` 中的路由均走此模式）

参考 `notification/service.go:149`：

```go
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // 统一用 "/api/admin-ai" 前缀，由 server.go switch 分发后此处去掉前缀
    path := strings.TrimPrefix(r.URL.Path, "/api/admin-ai")
    // 按 path 和方法路由
    switch {
    case path == "/sessions" && r.Method == http.MethodGet:
        s.listSessions(w, r)
    // ...
    }
}
```

---

## 2. manifest.go 路由登记

文件：`backend-go/internal/manifest/manifest.go:72-461`，`buildRoutes()` 返回 `[]Route`。

每个路由条目格式：

```go
{Prefix: "/api/admin-ai", Module: "admin-ai", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "管理 AI 助手总入口", MatchMode: MatchPrefix},
{Prefix: "/api/admin-ai/sessions", Module: "admin-ai", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "管理 AI 会话列表/创建", MatchMode: MatchExact},
{Prefix: "/api/admin-ai/sessions/{id}", Module: "admin-ai", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "管理 AI 会话更新/删除", MatchMode: MatchPattern},
{Prefix: "/api/admin-ai/messages", Module: "admin-ai", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "管理 AI 消息发送/历史", MatchMode: MatchExact},
{Prefix: "/api/admin-ai/channels", Module: "admin-ai", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "管理 AI 频道配置", MatchMode: MatchExact},
{Prefix: "/api/admin-ai/channels/{id}", Module: "admin-ai", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "管理 AI 频道更新/删除", MatchMode: MatchPattern},
{Prefix: "/api/admin-ai/approvals/{id}", Module: "admin-ai", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "管理 AI 审批详情/操作", MatchMode: MatchPattern},
{Prefix: "/api/admin-ai/settings", Module: "admin-ai", Owner: OwnerGo, Auth: AuthSession, ResponseMode: ResponseJSON, Description: "管理 AI 系统设置", MatchMode: MatchExact},
```

**注意**：`MatchPrefix` 用于前缀路由（`/api/admin-ai` 匹配所有以它开头的），`MatchExact` 用于精确路径，`MatchPattern` 用于含 `{id}` 参数的路由。

---

## 3. server.go 接线（三处）

文件：`backend-go/internal/server/server.go`

### 3a. import 加行

```go
"github.com/iwvw/api-monitor/backend-go/internal/adminai"
```

### 3b. Server struct 加字段（L45-75）

```go
adminai *adminai.Service
```

### 3c. newServer 初始化（L96-165 区域）

```go
adminaiService := adminai.New(cfg)
```

添加进 `&Server{...}` 构造体：

```go
adminai: adminaiService,
```

### 3d. serveGoRoute switch 加 case（L393-492）

```go
case "/api/admin-ai", "/api/admin-ai/sessions", "/api/admin-ai/sessions/{id}", "/api/admin-ai/messages", "/api/admin-ai/channels", "/api/admin-ai/channels/{id}", "/api/admin-ai/channel-bindings", "/api/admin-ai/channel-bindings/{id}", "/api/admin-ai/approvals/{id}", "/api/admin-ai/settings":
    s.adminai.ServeHTTP(w, r)
```

### 3e. Shutdown 加 stop（L167-199 区域）

```go
if s.adminai != nil {
    // 如 adminai 有 goroutine 需关闭，加 Stop 方法
}
```

---

## 4. system_config 读写模式

文件：`backend-go/internal/system/ai_access.go:248-290`

### 读（bool 型）

```go
const adminAIWriteEnabledKey = "admin_ai_write_enabled"

func (s *Service) getAdminAIWriteEnabled(ctx context.Context, db *sql.DB) (bool, error) {
    var value string
    err := db.QueryRowContext(ctx, "SELECT value FROM system_config WHERE key = ?", adminAIWriteEnabledKey).Scan(&value)
    if err == sql.ErrNoRows {
        return false, nil
    }
    if err != nil {
        return false, err
    }
    return value == "true" || value == "1", nil
}
```

### 写（任意类型）

```go
func (s *Service) setAdminAIWriteEnabled(r *http.Request) (map[string]interface{}, error) {
    var req struct {
        WriteEnabled bool `json:"writeEnabled"`
    }
    // ... parse body
    db, err := s.open(r.Context())
    // ... check err
    now := time.Now().UTC().Format(time.RFC3339)
    value := "false"
    if req.WriteEnabled { value = "true" }
    _, err = db.ExecContext(r.Context(),
        `INSERT OR REPLACE INTO system_config (key, value, description, updated_at) VALUES (?, ?, ?, ?)`,
        adminAIWriteEnabledKey, value, "管理 AI 写操作开关", now)
    // ... check err, log audit
    // 参考 ai_access.go:286-296
}
```

---

## 5. secure 加密/解密

文件：`backend-go/internal/secure/secure.go`

```go
import "github.com/iwvw/api-monitor/backend-go/internal/secure"
```

### 加密存储（频道配置等敏感 JSON）

```go
encrypted, err := secure.EncryptJSON(configMap)
// encrypted 存入 DB 的 TEXT 列
```

### 解密读取

```go
var config map[string]interface{}
err := secure.DecryptJSON(dbValue, &config)
```

生产环境需 `ENCRYPTION_KEY` 环境变量。

---

## 6. notification Telegram 复用

文件：`backend-go/internal/notification/service.go:1644-1691`

### 发送消息

```go
// sendTelegram(ctx, cfg, title, message)
// cfg 为 map[string]interface{}，需含 bot_token 和 chat_id
// 内部调用 callTelegram，走 sendMessage API
result, err := s.notify.sendTelegram(ctx, cfg, title, message)
```

### 编辑消息

```go
err := s.notify.editTelegram(ctx, cfg, chatID, messageID, title, newMessage)
```

### adminai 模块获取 notify 引用

通过 `Server` 传参注入（signature 后续定）：

```go
type TelegramSender interface {
    Send(ctx context.Context, cfg map[string]interface{}, title, message string) (notification.DeliveryResult, error)
    Edit(ctx context.Context, cfg map[string]interface{}, chatID string, messageID int64, title, message string) error
}
```

---

## 7. callAPIFromAI 工具桥

文件：`backend-go/internal/server/ai_caller.go:18-107`

### 签名

```go
func (s *Server) callAPIFromAI(ctx context.Context, call systemmetrics.AICallRequest) (systemmetrics.AICallResponse, error)
```

### 输入（AICallRequest）

```go
type AICallRequest struct {
    Method  string            `json:"method"`
    Path    string            `json:"path"`
    Headers map[string]string `json:"headers"`
    Body    json.RawMessage   `json:"body"`
}
```

### 输出（AICallResponse）

```go
type AICallResponse struct {
    StatusCode int                 `json:"statusCode"`
    Headers    map[string][]string `json:"headers"`
    Body       interface{}         `json:"body,omitempty"`
    Raw        string              `json:"raw,omitempty"`
}
```

### 内部安全策略（已实现，无需重写）

- 方法白名单：GET/POST/PUT/PATCH/DELETE
- 路径黑名单：`/api/ai/`（递归禁止）、`/api/system/ai-access/key`、`/api/ai-access/key`
- 写操作检查：`isWriteAIMethod(method)` → `AIAgentWriteAllowed(ctx)`
- 1MB body 上限
- 仅允许 `OwnerGo` 路由
- 拒绝 Stream/WebSocket 响应模式
- `X-AI-Agent: api-monitor` 头标记

### adminai 调用方式

`AICaller` 函数类型已在 `systemmetrics.AICaller` 中定义（`ai_access.go:80`），通过 `SetAICaller` 注入：

```go
// server.go newServer 已有：
systemService.SetAICaller(server.callAPIFromAI)

// adminai 中可通过 systemService 的 AICaller 字段调用
// 或通过 Server 直接传引用
```

---

## 8. OpenAI 网关 LLM 复用

文件：`backend-go/internal/openai/service.go`

### 选点逻辑

```go
// selectEndpointCandidates 返回按延迟加权排序的候选端点
// 定义在 openai/service.go:3938-4051
func (s *Service) selectEndpointCandidates(ctx context.Context, model string) ([]Candidate, error)
```

### 统一转发

```go
// relayLoop 承担重试/限流/代理池/熔断
// 定义在 openai/service.go:4099
func (s *Service) relayLoop(ctx context.Context, req Request, candidates []Candidate) (Response, error)
```

### 复用策略

adminai 模块不直接 import openai 包（避免循环依赖），而是：

1. **最简单路径**：用内部 gateway key 调 `POST /v1/chat/completions`，走网关完整链路（选点+代理+流式）
2. **需要进程内复用**：通过 `Server.openai` 暴露方法，或注册 `InferenceClient` 接口

v1 推荐方案 1：内部构造 HTTP 请求打网关，不走外部网络。

---

## 9. ensureSchema 与 ensureSQLiteColumn

### ensureSchema 模式

参考 `openai/service.go:443-565`：

```go
func (s *Service) ensureSchema(ctx context.Context, db *sql.DB) error {
    statements := []string{
        `CREATE TABLE IF NOT EXISTS admin_ai_sessions (
            id TEXT PRIMARY KEY,
            ...,
            created_at TEXT NOT NULL
        )`,
        `CREATE TABLE IF NOT EXISTS admin_ai_messages (
            id TEXT PRIMARY KEY,
            session_id TEXT NOT NULL REFERENCES admin_ai_sessions(id) ON DELETE CASCADE,
            ...
        )`,
        `CREATE INDEX IF NOT EXISTS idx_admin_ai_messages_session ON admin_ai_messages(session_id, created_at)`,
    }
    for _, stmt := range statements {
        if _, err := db.ExecContext(ctx, stmt); err != nil {
            return err
        }
    }
    return nil
}
```

### ensureSQLiteColumn 加列

文件：`openai/service.go:645-665`：

```go
func ensureSQLiteColumn(ctx context.Context, db *sql.DB, table, column, definition string) error {
    rows, err := db.QueryContext(ctx, "PRAGMA table_info("+table+")")
    if err != nil { return err }
    defer rows.Close()
    for rows.Next() {
        var cid int
        var name, columnType string
        var notNull, primaryKey int
        var defaultValue interface{}
        if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
            return err
        }
        if name == column { return nil }
    }
    _, err = db.ExecContext(ctx, "ALTER TABLE "+table+" ADD COLUMN "+column+" "+definition)
    return err
}
```

### 扩展现有 ai_access_audit 表

在 `ensureSchema` 末尾调用：

```go
if err := ensureSQLiteColumn(ctx, db, "ai_access_audit", "channel", "TEXT"); err != nil {
    return err
}
```

---

## 10. randomID 生成

### 按 notification 模式（推荐：`notif_1723123456000_ab12cd34`）

```go
func randomID(prefix string) (string, error) {
    buf := make([]byte, 8)
    if _, err := rand.Read(buf); err != nil {
        return "", err
    }
    return fmt.Sprintf("%s_%d_%s", prefix, time.Now().UnixMilli(), hex.EncodeToString(buf)), nil
}
```

### 或按 ai_access 模式（纯 token：`aik_` + 32 字节 hex）

```go
func randomToken(prefix string, byteLen int) (string, error) {
    buf := make([]byte, byteLen)
    if _, err := rand.Read(buf); err != nil {
        return "", err
    }
    return prefix + hex.EncodeToString(buf), nil
}
```

### 前缀约定

| 表 | 前缀 |
| --- | --- |
| `admin_ai_sessions` | `aas_` |
| `admin_ai_messages` | `aam_` |
| `admin_ai_executions` | `aae_` |
| `admin_ai_tool_calls` | `aatc_` |
| `admin_ai_channels` | `aac_` |
| `admin_ai_channel_bindings` | `aacb_` |
| `admin_ai_approvals` | `aaa_` |

---

## 11. store.js 模块注册

文件：`src/js/store.js:13-141`

### MODULE_CONFIG 加条目

```js
adminai: {
    name: '管理 AI',
    shortName: 'AI',
    icon: 'fa-robot',
    description: '管理 AI 助手',
},
```

### MODULE_GROUPS 加分组

放在 `api-gateway` 组内（L190-194）：

```js
{
    id: 'api-gateway',
    name: 'API 接口',
    icon: 'fa-file-code',
    modules: ['apidocs', 'adminai'],
},
```

### 默认可见性

`DEFAULT_MODULE_VISIBILITY`（L320-322 附近）和 `DEFAULT_MODULE_ORDER` 中加 `adminai`。

---

## 12. MainLayout.jsx 接线

文件：`src/js/components/MainLayout.jsx`

### MODULE_PATHS 加条目（L131-137 区域）

```js
adminai: 'adminai',
```

### renderActivePage switch 加 case（L613-673 区域）

```jsx
case 'adminai':
    return <AdminAIPage />;
```

### 顶部右上角入口按钮

参照 `SidebarStyleSwitchItems`（L754-757）在同一 header 区域加 Ask AI 开关按钮：

```jsx
<button
    onClick={() => store.setShowAskAI(!store.showAskAI)}
    className="..."
    aria-label="Ask AI"
>
    <BotIcon className="h-5 w-5" />
</button>
```

---

## 13. Kumo 组件 import 路径

| 组件 | import 路径 |
| --- | --- |
| Button | `@cloudflare/kumo/components/button` |
| Input | `@cloudflare/kumo/components/input` |
| Select | `@cloudflare/kumo/components/select` |
| Dialog | `@cloudflare/kumo/components/dialog` |
| Sidebar | `@cloudflare/kumo/components/sidebar` |
| Tabs | `@cloudflare/kumo` |
| Banner | `@cloudflare/kumo` |
| Tooltip | `@cloudflare/kumo` |
| Loader | `@cloudflare/kumo` |
| Badge | `@cloudflare/kumo` |
| Empty | `@cloudflare/kumo` |
| Switch | `@cloudflare/kumo/components/switch` |
| Checkbox | `@cloudflare/kumo/components/checkbox` |
| Dropdown | `@cloudflare/kumo` |
| Popover | `@cloudflare/kumo` |
| Table | `@cloudflare/kumo` |
| Toasty | `@cloudflare/kumo` |
| DeleteResource | `@cloudflare/kumo` |
| TimeseriesChart | `@cloudflare/kumo` |
| Meter | `@cloudflare/kumo` |
| ChartPalette | `@cloudflare/kumo` |

---

## 14. config 环境变量读取

文件：`backend-go/internal/config/config.go`

```go
type Config struct {
    // ...
    AdminAIDefaultModel string // 新增
    // ...
}

func Load() Config {
    // ...
    return Config{
        // ...
        AdminAIDefaultModel: envString("ADMIN_AI_DEFAULT_MODEL", ""),
        // ...
    }
}
```

辅助函数：

```go
func envString(key, def string) string
func envInt(key string, def int) int
func envBool(key string, def bool) bool
func envList(key, sep string) []string
```

---

## 15. 前端 SSE 接入模式

参考 `src/js/pages/GitHubPage.jsx` 的 EventSource 用法：

```js
const eventSource = useRef(null);

const startStream = (runId) => {
    eventSource.current = new EventSource(`/api/admin-ai/messages/stream?runId=${runId}`);
    eventSource.current.addEventListener('delta', (e) => {
        const data = JSON.parse(e.data);
        // 追加文本到当前消息
    });
    eventSource.current.addEventListener('tool_start', (e) => {
        const data = JSON.parse(e.data);
        // 显示工具调用开始
    });
    eventSource.current.addEventListener('done', (e) => {
        eventSource.current?.close();
    });
    eventSource.current.addEventListener('error', () => {
        // 断线处理
    });
};

const stopStream = () => {
    eventSource.current?.close();
    eventSource.current = null;
};

// 清理
useEffect(() => () => stopStream(), []);
```

**注意**：侧栏关闭时调用 `stopStream()`，避免无用连接。

---

## 附录：不得修改的文件清单

以下文件每次仅允许**最小改动**（一行或一个条目），且改后必须由主智能体逐行审阅：

| 文件 | 允许改动 |
| --- | --- |
| `backend-go/internal/manifest/manifest.go` | 加路由条目（~10 行） |
| `backend-go/internal/server/server.go` | import + struct 字段 + newServer 构造 + serveGoRoute case + Shutdown |
| `src/js/store.js` | MODULE_CONFIG 条目 + MODULE_GROUPS 条目 + 全局 showAskAI 状态 |
| `src/js/components/MainLayout.jsx` | MODULE_PATHS + renderActivePage case + 右上角入口按钮 |
| `backend-go/internal/config/config.go` | 加 1-2 个环境变量字段 |

所有其他文件（`backend-go/internal/adminai/`、`src/js/components/adminai/`、`src/js/pages/AdminAIPage.jsx`）均为**新增**，可自由创建。

---

## 附录：验证命令

```bash
# 后端
npm run backend-go:test
npm run governance:check
npm run lint -- --quiet

# 前端
npm run lint
npm run test
npm run build
```