# API Monitor 性能、依赖与安全需求

> 来源：2026-10-27 生产压测（ARM 主机 4 核 / 23GB，`iwvw/api-monitor:dev`）
> 仓库：`iwvw-per/API-Monitor`，基线分支 `dev`，基线提交 `84e8eaac`

**需求索引**

| 编号 | 主题 | 优先级 |
|---|---|---|
| 需求一 | 静态资源预压缩（gzip / brotli） | P0 |
| 需求二 | 鉴权结果短 TTL 缓存 | P1 |
| 需求三 | 前端 / 后端依赖升级 | P2 |
| 需求四 | **Go 工具链版本治理（CVE-2026-111943）** | **P0** |


## 压测基线数据

压测环境：ARM 主机 `64.181.246.5`（Oracle 圣何塞，4 核 aarch64，Ubuntu 24.04），
容器无 CPU/内存限制，`wrk` 4 线程，本机回环，每档 20 秒。

| 场景 | 并发 | RPS | P50 | P90 | P99 | 容器 CPU |
|---|---|---|---|---|---|---|
| `/health` | 200 | 14706 | 13.13ms | 26.48ms | 46.22ms | ~2% |
| 静态 `/assets/index-BjJR-GUH.css`（486KB） | 50 | 3782 | 11.79ms | 23.98ms | 39.58ms | **215%** |
| `/api/aiagent/servers`（401） | 400 | 9611 | 40.68ms | 72.12ms | 117.56ms | **204%** |

两条结论驱动下面的需求：

1. 静态资源峰值只有 3782 RPS，且从 50 并发就到顶；容器 CPU 打满 2 核。
   瓶颈是「每次请求实时 gzip 486KB」，不是磁盘、不是网络、不是内存。
2. 鉴权路径比纯框架路径吞吐低 35%（14706 → 9611），每个请求都在查 SQLite。

---

## 需求一：静态资源预压缩（gzip / brotli）

### 现状

- 前端产物在镜像内 `/app/dist`（`Dockerfile` 第 159 行 `COPY --from=frontend-builder /app/dist ./dist`），**没有 bind mount**，宿主机无法直接访问。
- 静态文件由 Go 直接服务：`backend-go/internal/server/server.go` 的 `serveStatic` → `tryServeFile` → `http.ServeFile`。
- 压缩由全局中间件 `backend-go/internal/httpcompress/middleware.go` 完成，`gzip.BestSpeed`（level 1），**每个请求都实时压缩**。
- 实测：`/assets/index-BjJR-GUH.css` 未压缩 486153 字节，gzip 后 102031 字节，压缩比 4.8:1。
- 磁盘分布（`/app/dist` 共约 245MB）：
  - `vendor/` 150.9MB、`agent/` 50.6MB、`assets/` 25.3MB、`fonts/` 18.1MB
  - `assets/` 内 850 个文件，其中 js 675 个约 94.9MB、css 13 个约 0.99MB、svg 2323 个约 4.7MB、woff2 331 个约 19.7MB

### 目标

把「每次请求实时压缩」改成「构建期压缩一次、运行期直接发送」，消除静态资源的 CPU 热点。

### 实施要求

1. **构建期预压缩**
   - 在 `Dockerfile` 的 `frontend-builder` 阶段，`npm run build` 之后、`COPY --from` 之前，对 `dist/` 下可压缩类型生成同名 `.gz` 和 `.br` 文件。
   - 只处理文本类型：`.js .mjs .css .html .svg .json .webmanifest .map .txt .xml .wasm`。
   - **跳过**已经压缩过的类型：`.woff2 .woff .png .jpg .jpeg .gif .webp .avif .ico .zip .gz .br .mp4`。
   - **跳过** `dist/agent/` 目录（内含 Agent 二进制及其 `.sha256`，是下载产物，不该被压缩替换）。
   - 小于 1KB 的文件跳过（gzip 头尾开销约 20B，压缩后可能更大）。
   - 建议 gzip 用 `-9`（构建期一次性成本，换取最小体积），brotli 用 `-q 11 -w 24`。
   - 注意 `.gz` / `.br` 的 mtime 要保留源文件的值，便于 `If-Modified-Since` 正确工作。

2. **运行期直接发送预压缩文件**
   - 在 `serveStatic` 路径（或 `httpcompress.Middleware` 内）优先查找 `<file>.br` / `<file>.gz`：
     - 请求 `Accept-Encoding` 含 `br` → 优先 `.br`
     - 否则含 `gzip` → 用 `.gz`
     - 都没有 → 原始文件
   - 命中预压缩文件时，直接 `http.ServeContent` 发送，并设置：
     - `Content-Encoding: br` 或 `gzip`
     - `Content-Type` 按**原始扩展名**推断（不能按 `.br` 推断）
     - `Vary: Accept-Encoding`
     - 保留原有 `Cache-Control`（`assets/` 是 `public, max-age=31536000, immutable`，见 `server.go` 的 `setStaticCacheHeaders`）
   - 命中预压缩文件时，**必须绕过 `httpcompress.Middleware` 的二次压缩**。当前中间件靠 `Content-Encoding != ""` 判断已编码并跳过（`middleware.go` 第 59 行），确认这条路径成立即可，不要重复压缩。
   - 预压缩文件不存在时，回退到现有的实时 gzip 行为，保证开发环境（无预压缩步骤）不受影响。

3. **禁止事项**
   - 不要在运行时启动时压缩（那只是把成本从请求期挪到启动期，且会拖慢启动）。
   - 不要改动 `/api/*` 的压缩行为，API 响应仍然走实时 gzip（JSON 体积小且变化频繁，预压缩无意义）。

### 验收标准

- `curl -H 'Accept-Encoding: gzip' -D- https://dsukhub.com/assets/<某 js>` 返回 `Content-Encoding: gzip`，且 `Content-Length` 等于 `.gz` 文件大小。
- `curl -H 'Accept-Encoding: br'` 返回 `Content-Encoding: br`。
- `curl` 不带 `Accept-Encoding` 返回原始大小且无 `Content-Encoding`。
- 镜像体积增幅不超过 60%（预压缩产物约为源文件 25~30%）。
- 重复压测同场景：**3782 RPS → 目标 ≥ 12000 RPS**，容器 CPU 从 215% 降到 100% 以下。
- 前端页面加载无异常（`index.html`、JS chunk、CSS、字体、SVG 图标、PWA manifest 均正常）。

---

## 需求二：鉴权结果短 TTL 缓存

### 现状

两条鉴权路径，**每个请求都查一次 SQLite**：

1. **模块用户 Bearer Token**（OpenCodeUI 客户端、MCP 调用）
   - `backend-go/internal/aiagent/service.go` `resolveAuth` → `store.go` `authenticateToken`
   - 每次执行 `SELECT t.id, t.user_id, ... FROM aiagent_tokens t JOIN aiagent_users u ... WHERE t.token_hash = ?`
   - token 是**不透明随机串**，鉴权时用 `sha256(token)` 比对（`store.go` `hashToken`）
   - `last_used_at` 已有 1 分钟节流写（`shouldTouchToken` / `tokenTouchInterval`）

2. **面板管理员 Session**（浏览器 cookie `sid`）
   - `backend-go/internal/auth/service.go` `IsAuthenticated` → `validateRequestSession` → `validateSession`
   - 每次执行 `SELECT ... FROM sessions WHERE session_id = ?`
   - `last_accessed_at` 已有 1 分钟节流写

当前 `authorizeGoRoute`（`server.go` 第 535 行）对 aiagent 路由直接放行，由模块内部 `resolveAuth` 完整处理；其余 `AuthSession` 路由走 `s.auth.IsAuthenticated`。

### 目标

在进程内存里缓存鉴权结果，把重复请求的 SQLite 查询降到接近零，同时保证撤权能及时生效。

### 实施要求

1. **缓存键**
   - 模块 token：`sha256(plain token)` 的十六进制串（**不要缓存明文 token**，避免内存泄漏时泄露凭据）。
   - 管理员 session：`sid` 的 `sha256` 摘要。
   - 两类缓存分开，键空间不要混用。

2. **缓存内容**
   - 模块 token：`authContext{UserID, TokenID, Username}` + 过期时间戳。
   - 管理员 session：`sessionRecord` 的判定结果（是否有效）+ 过期时间戳。

3. **TTL 建议**
   - 默认 **30 秒**，可通过环境变量覆盖，例如 `AUTH_CACHE_TTL=30s`；设为 `0` 或负数时关闭缓存（回退到当前行为）。
   - 缓存条目的绝对过期时间不得晚于「token / session 自身的 expires_at」，两者取更早者。

4. **必须失效的场景（安全红线）**

   以下操作必须在**同一次请求内**主动清除对应缓存条目，不能等 TTL 自然过期：

   | 场景 | 需要失效的键 | 现有代码位置 |
   |---|---|---|
   | 撤销单个 token | 该 token | `store.go` `revokeToken` |
   | 撤销某用户全部 token | 该用户全部 token | `store.go` `revokeUserTokens` |
   | 用户被禁用 | 该用户全部 token | 用户更新逻辑 |
   | 用户被删除 | 该用户全部 token | 用户删除逻辑 |
   | 登出 | 该 sid | `auth/service.go` `destroySession` |
   | 修改管理员密码 | 全部 session | 密码修改逻辑 |
   | session 被置 `is_active = 0` | 该 sid | `validateSession` 内已有的 UPDATE 分支 |

   如果做不到精确失效，就按用户维度整组失效（宁可多失效，不可漏失效）。

5. **实现约束**
   - 用 `sync.Mutex` + `map` 即可，参考现有 `internal/gcp/auth.go` 的 `tokenCache` 写法（`get` / `set` / `invalidateAccount`）。**不要引入新依赖**。
   - 必须有容量上限（建议 10000 条）和惰性清理，防止恶意大量无效 token 打满内存。
   - 缓存未命中时的查询失败（DB 错误）**不要缓存**，避免把瞬时故障固化成 30 秒的持续失败。
   - 缓存命中的请求仍然要更新 `last_used_at` / `last_accessed_at` 的节流逻辑（或至少在缓存条目里带上上次 touch 时间），否则面板上「最后活跃时间」会失真。

6. **明确不做的事**
   - 不缓存 401 结果。无效凭据每次都要真实回源，避免攻击者用缓存探测 token 有效性。
   - 不改变 `IsAdmin` 的判定逻辑，只缓存结果。

### 验收标准

- 单元测试：同一 token 连续 100 次请求，SQLite 查询次数为 1（用现有测试里的 mock/计数方式验证）。
- 单元测试：撤销 token 后**立即**请求返回 401（不等待 TTL）。
- 单元测试：登出后立即请求返回 401。
- 单元测试：用户被禁用后立即请求返回 401。
- 单元测试：`AUTH_CACHE_TTL=0` 时行为与改造前完全一致（每次都查库）。
- 单元测试：无效 token 连续请求 100 次，每次都回源（不缓存 401）。
- 重复压测同场景：**9611 RPS → 目标 ≥ 13000 RPS**。
- 面板「最后活跃时间」显示正常，没有因为缓存而停止更新。

---

## 需求三：依赖升级

### 现状（2026-10-27 检查结果）

**前端（package.json）**

| 包 | 当前 | 最新 | 备注 |
|---|---|---|---|
| `react` / `react-dom` | `^19.2.8` | 19.3.1 | minor |
| `vite` | `^8.2.2` | 8.3.5 | minor |
| `vitest` | `^5.0.3` | 5.0.5 | patch |
| `@vitest/coverage-v8` | `^5.0.3` | 5.0.5 | patch |
| `@vitejs/plugin-react` | `^6.1.0` | 6.1.2 | patch |
| `zustand` | `^5.0.15` | 5.0.16 | patch |
| `@cloudflare/kumo` | `^2.15.0` | 2.16.0 | minor |
| `axios` | `^1.20.0` | 1.21.0 | minor |
| `marked` | `^18.0.10` | 18.1.2 | minor |
| `shiki` | `^4.4.3` | 4.5.0 | minor |
| `eslint` | `^10.9.0` | 10.13.0 | minor |
| `prettier` | `^3.9.6` | 3.9.10 | patch |
| `katex` | `^0.19.0` | 0.19.1 | patch |
| `react-grid-layout` | `2.2.4`（**锁定，无 `^`**） | 2.3.0 | minor，见下方注意 |

**后端（go.mod）**

`go 1.26.10`，直接依赖 20 个。核心包当前版本：

| 模块 | 当前 |
|---|---|
| `github.com/go-chi/chi/v5` | v5.3.2 |
| `github.com/gorilla/websocket` | v1.5.3 |
| `golang.org/x/crypto` | v0.58.0 |
| `golang.org/x/net` | v0.61.0 |
| `golang.org/x/sync` | v0.24.0 |
| `golang.org/x/text` | v0.43.0 |
| `modernc.org/sqlite` | v1.60.1 |
| `github.com/klauspost/compress` | v1.20.1 |
| `github.com/andybalholm/brotli` | v1.2.6 |
| `github.com/shirou/gopsutil/v4` | v4.26.9 |
| `github.com/go-webauthn/webauthn` | v0.18.2 |
| `github.com/oracle/oci-go-sdk/v65` | v65.126.2 |
| `github.com/router-for-me/CLIProxyAPI/v6` | v6.10.9 |
| `github.com/sardanioss/httpcloak` | v1.7.2 |

> 注：本地 `go list -u -m all` 因 `proxy.golang.org` 网络不可达未能取到最新版本号，
> 需要在 CI 或能访问 Go proxy 的环境里跑一次补全。

### 实施要求

1. **升级策略**
   - 前端：在 `dev` 分支上单独一个 PR，只做依赖升级 + `package-lock.json` 更新，**不夹带功能改动**。
   - 优先 patch 和 minor；本次清单里没有 major 跨版本，全部是 patch/minor，风险可控。
   - `react-grid-layout` 当前是**精确锁定 `2.2.4`**（没有 `^`），说明历史上踩过坑。升级到 `2.3.0` 前请先确认变更日志，或暂时不动，只升级其他包。
   - **Go 工具链见下方需求四，有单独的强制要求。**

2. **必须跑通的验证**
   - `npm ci`（不是 `npm install`）能装成功，`package-lock.json` 无冲突。
   - `npm run build` 成功，`dist/index.html` 和 `dist/assets` 存在。
   - `npm run lint` 无新增告警。
   - `npm test`（vitest）全绿。
   - 后端 `go build ./...`、`go test ./...` 全绿。
   - Docker 镜像能构建成功（含 arm64）。

3. **安全相关依赖单独对待**
   - `golang.org/x/crypto`、`golang.org/x/net`、`dompurify`、`axios` 属于安全敏感包，有任何 patch 版本都应当尽快跟进。
   - 仓库已有 `.gitleaks.toml` 和 `RUSTSEC` 例外记录（见提交 `84e8eaac`），升级后请复核这些例外是否还需要保留。

4. **禁止事项**
   - 不要在升级 PR 里顺手改代码风格或重构。
   - 不要为了「消除告警」而升级到 major 版本（例如 `typescript` 最新已是 7.x，但项目当前未直接依赖，不要贸然引入）。
   - 不要跳过 `package-lock.json` 的提交。

### 验收标准

- 依赖升级 PR 的 CI（含 arm64 镜像构建）全绿。
- 升级前后分别跑一次本文档开头的压测，性能无回退（RPS 波动 ±5% 以内）。
- 前端主要页面人工回归：登录、面板首页、AI Agent 页、模型网关页、书签页、设置页。

---

## 需求四：Go 工具链版本治理（安全修复强制项）

### 现状核查结论

**主构建已修复，但存在一处未覆盖的漏洞版本。**

| 位置 | 当前值 | 是否受 CVE-2026-111943 影响 |
|---|---|---|
| `backend-go/go.mod` `go` 指令 | `1.26.10` | ✅ 已修复 |
| `Dockerfile` 第 54 行 `golang:` 镜像 | `golang:1.26.10-alpine3.24` | ✅ 已修复 |
| CI（`ci-cd.yml` / `release.yml` / `security.yml` / `backend-race.yml`） | 读 `go-version-file: backend-go/go.mod` | ✅ 随 go.mod 自动修复 |
| **`.github/workflows/managed-runtime.yml` 第 21 行** | **`GO_VERSION: 1.26.4`** | ❌ **仍受影响** |

修复提交：`c0dbd1d1` "fix(security): 升级 Go 1.26.10 与 rustls-webpki/webpki-roots，修掉 Security Baseline 失败"，
该提交把 Dockerfile 从 `1.26.9` 升到 `1.26.10`，`go.mod` 同步到 `1.26.10`。

### CVE-2026-111943 事实边界（已核实）

- **编号**：CVE-2026-111943 / GO-2026-6809 / Go issue #82345
- **披露**：2026-10-19 随 Go **1.27.3** 与 **1.26.10** 发布
- **受影响版本**：`< 1.26.10`，以及 `>= 1.27.0-0 < 1.27.3`
- **已修复版本**：`1.26.10`、`1.27.3` 及更高
- **位置**：标准库 `crypto/x509`，符号 `Certificate.Verify`
- **性质**：eMudhra/emSign 中间 CA 的 P-384 私钥因熵缺陷可被数学恢复；
  Go 在 `crypto/x509` 中加入 SPKI SHA-256 黑名单，`Verify` 会过滤含这些中间证书的链
- **利用方式**：中间人攻击。攻击者需要特定网络位置 + 使用已泄露中间 CA 签发的证书
- **评分**：Red Hat CVSS 3.1 = 6.8（AV:N/AC:H/PR:N/UI:R/C:H/I:H/A:N）；Snyk 7.6 high

### 生产环境实测（2026-10-27）

ARM 生产容器 `64.181.246.5` 实测：

```
GET /health  →  "goVersion": "go1.26.10"
二进制字符串  →  go1.26.10
二进制符号    →  crypto/x509.containsDistrustedSPKIHash   ← 该 CVE 修复引入的函数，确认已生效
```

**结论：当前生产二进制确实包含该 CVE 的修复，不是靠版本号推断的。**

> 注：镜像内 `ca-certificates-20260611-r0`（Alpine 3.24.1）**不包含** emSign 根移除。
> 但 Go 的修复是标准库层面的黑名单（`containsDistrustedSPKIHash`），不依赖系统 CA 包，
> 所以该包版本不影响本 CVE 的防护效果。若后续有其他依赖系统 CA 的漏洞，仍需单独跟进 Alpine 包。

### 需要修的问题

1. **`.github/workflows/managed-runtime.yml` 的 `GO_VERSION: 1.26.4` 必须升级**
   - 该 workflow 用这个版本编译 **sing-box** 托管代理运行时，产物发布为
     `managed-runtime-v1.13.14`，由 `backend-go/internal/serveragent/proxy_runtime_catalog.go`
     引用下载地址与 SHA256，部署到用户主机上运行。
   - sing-box 是 VPN/代理客户端，**大量使用 TLS 出站连接和证书校验**，
     正是 `crypto/x509` 漏洞的直接命中场景。
   - 该 workflow 只在 `workflow_dispatch` 或自身文件变更时触发，
     **不会随 `dev` 分支的 Go 升级自动重建**，所以一直停留在 1.26.4。
   - `govulncheck` job 只扫描 `backend-go/`，**覆盖不到这个独立构建**。

2. **升级该 workflow 时的连带影响（重要）**
   - 换 Go 版本会改变 sing-box 二进制的字节，**现有 SHA256 固定值必然失配**：
     - `094568655c7324de08bbeb108df886950f76b4dc164e1e5bc08e64219f1af727`（amd64）
     - `fd19a07696593307c14133ae0ebed2e089523424b3f2ab2ea74aa206661f7131`（arm64）
   - 因此不能只改 `GO_VERSION`，必须同时：
     - 提升 `SING_BOX_VERSION` / release tag（如 `managed-runtime-v1.13.15`），否则无法发布（release 不可变）
     - 更新 `managed-runtime.yml` 里的 `sha256sum --check` 期望值
     - 同步更新 `proxy_runtime_catalog.go` 的 `AMD64URL/ARM64URL/AMD64SHA256/ARM64SHA256`
   - 这是一个**跨 workflow + 代码 + 已部署节点**的联动变更，必须作为独立 PR 并明确回滚方案。

3. **安全基线存在盲区**
   - `govulncheck` 只覆盖 `backend-go`，未覆盖 `managed-runtime.yml` 的独立构建。
   - 建议补一条 CI 检查：扫描 `.github/workflows/*.yml` 中所有硬编码的 `GO_VERSION` /
     `go-version`，低于 `backend-go/go.mod` 声明的版本时报错。

### 实施要求

1. **立即**：把 `managed-runtime.yml` 的 `GO_VERSION` 从 `1.26.4` 升到 `>= 1.26.10`，
   按上面第 2 点完成版本号、SHA256、catalog 的联动更新。
2. **建立机制**：新增 CI 检查，确保仓库内所有 Go 版本固定点不低于 `go.mod`。
3. **维持惯例**：本次升级已在 `c0dbd1d1` 建立了「Go 补丁版本升级 + 安全基线」的做法，后续保持：
   每个 Go 安全公告发布后 7 天内完成 go.mod + Dockerfile 升级。
4. **不要做的事**：
   - 不要只改 `GO_VERSION` 而不更新 SHA256 和 catalog，会导致节点拉取校验失败。
   - 不要为了「版本看起来统一」而把 `managed-runtime.yml` 改成读 `go.mod`
     （两者构建目标不同，但至少要保证安全补丁同步）。

### 验收标准

- `managed-runtime.yml` 的 `GO_VERSION` ≥ `1.26.10`。
- 该 workflow 能成功跑通并发布新 release，`sha256sum --check` 通过。
- `proxy_runtime_catalog.go` 的 URL 与 SHA256 与实际产物一致（用 `curl` 下载后 `sha256sum` 复核）。
- 对已部署的 sing-box 节点执行一次运行时更新，确认新二进制可正常启动并完成 TLS 出站连接。
- 新增的 CI 版本一致性检查能在「GO_VERSION 低于 go.mod」时正确失败。

---

## 优先级建议

| 优先级 | 需求 | 理由 |
|---|---|---|
| **P0** | **需求四 Go 工具链治理** | **安全修复，`managed-runtime.yml` 仍在 1.26.4（受 CVE-2026-111943 影响），且该产物是 TLS 客户端** |
| P0 | 需求一 静态预压缩 | 当前静态资源 CPU 热点明确，改造收益最大（3 倍+），且不涉及安全逻辑 |
| P1 | 需求二 鉴权缓存 | 收益约 35%，但涉及安全红线，必须有完整的失效测试 |
| P2 | 需求三 依赖升级 | 长期健康度，不紧急，但应定期做 |

## 附：本次压测的方法与脚本

压测命令（在目标主机上执行）：

```bash
wrk -t4 -c<并发> -d20s --latency http://127.0.0.1:3000<路径>
```

三个测试路径：

- `/health` —— 纯框架开销
- `/assets/index-BjJR-GUH.css` —— 静态资源 486KB
- `/api/aiagent/servers` —— 走完整鉴权中间件（返回 401）

建议把上述压测固化成仓库里的 `tools/bench.sh`，便于每次性能相关改动后回归。
