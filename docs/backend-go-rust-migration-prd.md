# Go Main + Rust Agent 后端迁移 PRD

最后更新：2026-06-14

本文档基于当前仓库后端扫描结果，定义 API Monitor 从 Node.js 后端迁移到 **Go 主程序 + Rust Agent** 的产品与工程要求。目标不是逐行翻译现有代码，而是在保持前端接口兼容和数据安全的前提下，逐步让 Go 接管主后端，让 Rust Agent 继续承接主机侧能力，最终退出 Node 运行时。

## 当前决策

- 主程序目标运行时：Go。
- Agent 目标运行时：Rust，沿用并增强当前 Rust Agent。
- 迁移策略：Go main 先作为兼容壳启动，未迁移模块临时反代到 Node legacy sidecar；模块迁完一个，下线一个反代。
- 前端策略：保持现有 React + Kumo 前端接口不变，前端不因后端语言迁移而重构。
- 数据策略：继续使用 SQLite，迁移期不得让 Go 和 Node 无约束地同时写同一批表。
- 音乐模块决策：`music-api` 不纳入 Go/Rust 迁移目标；目标架构中退役音乐模块，后续单独清理前端入口、后端路由、数据库残留和 Node 音乐相关依赖。

## 扫描摘要

当前后端由 Express 主程序、核心路由、业务模块、WebSocket/SSE 通道、SQLite schema、定时任务和 Rust Agent 组成。

粗略扫描结果：

- 后端 route 声明约 496 个。
- 业务模块目录 16 个，不含模板模块。
- 最大模块是主机模块，约 8k 行，包含账号、凭据、Agent、SSH、SFTP、Docker、终端、指标、网络质量和快速命令。
- SQLite 显式 schema 至少覆盖 63 张表；另有多处 JS 内兼容迁移和 `ALTER TABLE`。
- 长驻通道包括 `/ws/logs`、`/ws/metrics`、`/ws/ssh`、Socket.IO `/agent` 和 `/metrics`、任务 SSE。
- Rust Agent 已覆盖指标采集、Docker、本机文件管理、PTY、任务执行、任务进度和 Socket.IO 协议，不是空白重写对象。

## Problem Statement

当前 Node.js 后端在功能持续扩张后变得偏重，主要问题是：

- 运行时和依赖体积偏大，生产依赖中混有前端库、云 SDK、音乐代理、SSH、文件上传、图表/文档类依赖等。
- 后端模块数量多，Express route 和模块初始化分散，迁移或排障时需要在主程序、路由汇总、模块 router、storage、schema 和后台任务之间来回跳转。
- 主机工作台能力复杂，Node 同时承担控制面和数据面，SSH/SFTP/Docker/终端/Agent/指标混在一个高风险模块里。
- SQLite schema 与历史兼容迁移分散，切换运行时时如果没有统一 migration/self-check，旧数据升级风险高。
- 前端已基本完成 React + Kumo 收口，后端资源占用和部署形态成为下一阶段主要优化点。

用户希望降低资源占用、简化部署，并让后端架构更适合长期维护。

## Solution

将 API Monitor 后端演进为：

```text
React/Kumo Frontend
        |
Go Main Backend
  - HTTP JSON routes
  - auth/session/2FA
  - SQLite migration and storage
  - WebSocket/SSE hub
  - module control plane
  - cloud/API/provider adapters
        |
Rust Agent
  - host metrics
  - PTY terminal
  - file operations
  - Docker operations
  - task execution and progress

Temporary during migration:
Node legacy sidecar for modules not yet migrated
```

Go main 必须从第一天保持当前前端依赖的接口形状。迁移期间，Go main 根据模块迁移状态决定：

- 已迁移模块：Go 本地处理。
- 未迁移模块：反代到 Node legacy sidecar。
- 已退役模块：不在目标架构中实现；音乐模块属于此类。

Rust Agent 不重写为 Go。主机侧能力继续由 Rust Agent 承担，Go main 负责认证、连接管理、任务编排、审计、结果持久化和前端事件分发。

## User Stories

1. As an administrator, I want the main backend to use less memory, so that the dashboard can run reliably in small containers.
2. As an administrator, I want the Docker image to be smaller, so that deployment and upgrade are faster.
3. As an administrator, I want the existing frontend to keep working, so that backend migration does not force another UI rewrite.
4. As an administrator, I want existing SQLite data to migrate safely, so that accounts, settings, logs, monitors, tokens, and history are not lost.
5. As an administrator, I want a visible migration self-check, so that I can know whether my database is ready for the Go backend.
6. As an administrator, I want old Node modules to keep working during migration, so that the system remains usable before every module is rewritten.
7. As an administrator, I want music to be removed from the target backend, so that unsupported or heavy music dependencies do not block migration.
8. As a user, I want login, logout, password setup, password change, session restore, and 2FA to behave the same, so that security flows remain familiar.
9. As a user, I want module visibility, ordering, theme, page width, and sidebar settings to persist, so that the shell experience is unchanged.
10. As a user, I want dashboards to show the same server, uptime, API, TOTP, filebox, and cloud summaries, so that no overview capability is lost.
11. As a user, I want TOTP/HOTP accounts, groups, import/export, code generation, reveal audit, and browser extension download to keep working.
12. As a user, I want Filebox public share links, passwords, expiry, download limits, burn-after-reading, access logs, and cleanup jobs to keep working.
13. As a user, I want Uptime monitors, status pages, badges, push tokens, maintenance windows, incidents, heartbeats, and notifications to keep working.
14. As a user, I want notification channels, alert rules, event catalog, templates, dry-run, retry, maintenance suppression, and history to keep working.
15. As a user, I want OpenAI-compatible endpoint management and chat completions to preserve the current request/response behavior.
16. As a user, I want Gemini CLI and Qwen proxy routes to keep streaming responses correctly, so that AI clients do not break.
17. As a user, I want Cloudflare account, DNS, Workers, Pages, R2, Tunnel, SSL, analytics, templates, import, and export flows to keep working.
18. As a user, I want Aliyun and Tencent cloud DNS and compute controls to keep working.
19. As a user, I want Koyeb and Fly.io account, app, service, log, instance, metric, redeploy, rename, and update flows to keep working.
20. As a user, I want OpenList and cron task management to keep working in the self-hosted module.
21. As a server operator, I want Rust Agent connections to survive Go main migration, so that host telemetry and terminal features remain stable.
22. As a server operator, I want SSH/SFTP fallback behavior to be explicit, so that I know when the system uses Agent versus direct SSH.
23. As a server operator, I want Docker actions, update checks, logs, stats, images, networks, volumes, and compose actions to remain auditable.
24. As a server operator, I want dangerous commands and destructive Docker operations to keep confirmation and audit behavior.
25. As a developer, I want each migrated module to expose a small, testable interface, so that business logic is not buried inside HTTP handlers.
26. As a developer, I want provider calls behind adapters, so that Cloudflare/Aliyun/Tencent/Koyeb/Fly/Qwen/Gemini behavior can be tested without real accounts.
27. As a developer, I want response and error formats to be consistent, so that the frontend does not need module-specific parsing fixes.
28. As a developer, I want route authorization to be generated from a route manifest, so that public, authenticated, API-key, Agent, WebSocket, and SSE routes do not drift.
29. As a developer, I want migration tests using copied SQLite fixtures, so that old user databases are protected before the Go backend becomes default.
30. As a maintainer, I want Node sidecar usage to be visible, so that we know exactly which modules still block Node retirement.
31. As a maintainer, I want production builds to exclude retired music dependencies, so that the resource target is not undermined by unused packages.
32. As a maintainer, I want a rollback path during each migration wave, so that a broken Go module can be routed back to Node while preserving data.

## Module Inventory And Target Matrix

The route counts below are approximate static counts from router declarations. They are useful for sizing, not as a formal API spec.

| Module | Current route prefix | Visible capability | Approx routes | SQLite footprint | Target | Risk | Priority |
| --- | --- | --- | ---: | --- | --- | --- | --- |
| Core shell | `/`, static assets, uploads | SPA serving, uploads, security headers, compression, fallback routes | 20 | Core config/session/settings/log tables | Go main | Medium | Wave 1 |
| Auth | `/api/auth`, `/api` legacy auth fallback | password setup/login/logout/session/2FA/change password | 11 | `system_config`, `sessions`, `login_attempts`, `user_settings` | Go main | High | Wave 1 |
| Health | `/health` | health check | 1 | none | Go main | Low | Wave 1 |
| Settings/System/Logs | `/api/settings`, `/api/system`, `/api/logs` | settings, DB import/export, log settings, operation logs, host metrics | 24+ | core tables plus migration self-check | Go main | High | Wave 1 |
| OpenAI API gateway | `/api/openai`, `/v1` | endpoint management, model refresh, health, chat completions, logs/stats | 17 plus `/v1` | `openai_endpoints`, `openai_health_history`, chat/session tables if present | Go main with provider adapters | High | Wave 4 |
| Gemini CLI | `/api/gemini-cli`, `/v1` compatibility | OAuth accounts, tokens, project matrix, logs, model checks, streaming proxy | 35 | 4 Gemini tables plus compatibility columns | Go main; helper binaries may remain external adapters | Very high | Wave 4 |
| Qwen | `/api/qwen` | accounts, settings, matrix, logs, model redirects, streaming chat/images | 16 | 5 Qwen tables | Go main with HTTP/SSE adapter | High | Wave 4 |
| Server | `/api/server`, `/api/server/agent`, `/ws/ssh`, Socket.IO `/agent` | host accounts, credentials, Agent, SSH/SFTP, Docker, terminal, metrics, network quality, snippets | 71+ | 9 server tables | Go control plane + Rust Agent data plane | Very high | Wave 5 |
| Cloudflare | `/api/cloudflare` | accounts, zones, DNS, templates, cache purge, SSL, analytics, Workers, Pages, R2, Tunnels | 67 | 4 Cloudflare tables | Go main with Cloudflare adapter | Very high | Wave 3 |
| Aliyun | `/api/aliyun` | accounts, DNS, ECS/SWAS/firewall, metrics | 26 | 2 Aliyun tables | Go main with Aliyun adapter | Medium | Wave 3 |
| Tencent | `/api/tencent` | accounts, DNSPod, CVM, Lighthouse | 16 | 2 Tencent tables | Go main with Tencent adapter | Medium | Wave 3 |
| Koyeb | `/api/koyeb` | accounts, data, service/app actions, logs, instances, metrics, usage | 17 | 1 Koyeb table | Go main with HTTP adapter | Medium | Wave 3 |
| Fly.io | `/api/flyio` | accounts, apps, machines, events, config, logs, image updates | 15 | 1 Fly table | Go main with HTTP adapter | Medium | Wave 3 |
| OpenList | `/api/openlist` | accounts, fs list/get/search, storages, settings | 11 | 3 OpenList tables | Go main with HTTP adapter | Medium | Wave 2 |
| Cron | `/api/cron` | scheduled HTTP tasks and logs | 7 | 2 cron tables | Go scheduler module | Medium | Wave 2 |
| TOTP | `/api/totp` | accounts, groups, codes, verify, HOTP increment, import/export, extension download | 21 | 2 TOTP tables | Go main | Medium | Wave 2 |
| Filebox | `/api/filebox` | public retrieve/download/verify, shares, settings, logs, cleanup | 16 | 3 Filebox tables | Go main | High | Wave 2 |
| Uptime | `/api/uptime` | monitors, probes, states, status pages, badges, push, maintenance, import/export | 27 | 11 Uptime tables | Go worker + Go route module | High | Wave 2 |
| Notification | `/api/notification` | channels, rules, event catalog, templates, dry-run, history, suppression | 22 | 6 Notification tables | Go worker + Go route module | High | Wave 2 |
| Music | `/api/music` | NCM proxy/player/unblock/auth | 36 | `music_settings` compatibility table | Retired; no migration | N/A | Not migrated |

## Backend Interfaces To Preserve

### HTTP JSON Interface

Most frontend calls expect JSON responses using `success`, `data`, and `error` fields. The Go backend must preserve this envelope unless a route already returns a special shape such as streaming data, file download, proxy response, badge, or OpenAI-compatible payload.

### Authentication Interface

The migration must preserve:

- Initial password setup.
- Password login and logout.
- Session cookie behavior.
- Session restore.
- Password change.
- System 2FA setup, enable, disable, and status.
- API key behavior for OpenAI-compatible `/v1` routes.
- Agent key behavior for public Agent push/install routes.

### Route Authorization Interface

Routes must be classified into:

- Public health/static routes.
- Public filebox share routes.
- Public uptime status page, badge, and push routes.
- Public or semi-public AI-compatible routes guarded by API key.
- Authenticated dashboard routes.
- Agent-authenticated routes.
- WebSocket/SSE routes.

This classification should live in a route manifest or equivalent Go module, not be scattered across ad hoc route registration.

### SQLite Storage Interface

The Go backend must be able to:

- Open the existing SQLite database.
- Run idempotent migrations.
- Perform migration self-check before serving traffic.
- Preserve encrypted secrets and legacy encrypted values.
- Import/export database backups.
- Run vacuum/analyze/log cleanup.
- Avoid concurrent Node/Go writes to the same module tables during sidecar migration.

### Secret Storage Interface

The Go backend must preserve compatibility with existing encrypted values for:

- Admin/session secrets where applicable.
- Cloud provider tokens.
- TOTP secrets.
- Notification credentials.
- AI provider tokens.
- Agent keys and server credentials.

If encryption format changes, Go must support read-old/write-new migration with explicit self-check reporting.

### Streaming Interface

The Go backend must preserve:

- SSE chat completions for OpenAI/Gemini/Qwen flows.
- Task SSE for server task progress.
- WebSocket logs.
- WebSocket metrics.
- WebSocket terminal bridge.
- Socket.IO Agent compatibility, at least until the Rust Agent protocol is replaced or stabilized behind a Go adapter.

### Rust Agent Interface

The current Rust Agent speaks a Socket.IO-like protocol with events for auth, state, host info, dashboard tasks, PTY input/resize, PTY data, task result, and task progress. The target Go main must either:

- Implement compatible Socket.IO server behavior, or
- Introduce a new Agent protocol while keeping a compatibility adapter for existing deployed agents.

The PRD assumes compatibility first.

## Implementation Decisions

### 1. Build Go Main As A Compatibility Shell First

The first Go deliverable is not a migrated module. It is a main backend shell that can:

- Load config and environment variables.
- Serve health and static assets.
- Open SQLite read-only for self-check, then read/write after migration mode is enabled.
- Authenticate sessions.
- Reverse proxy unmigrated module prefixes to Node.
- Emit structured logs.
- Expose a migration status route.

This creates the seam where module ownership can move from Node to Go without changing the frontend.

### 2. Introduce A Route Manifest

Create a route manifest that records for every prefix:

- Public/auth/API-key/Agent auth requirement.
- Go-owned or Node-proxied owner.
- JSON/file/stream/WebSocket/SSE response mode.
- Module name.
- Data ownership during migration.

This prevents the current dynamic route rules from being re-created as scattered Go conditionals.

### 3. Migrate Storage Before Business Logic

Before a module is rewritten, Go must have:

- The schema definition.
- Idempotent migrations.
- A table ownership flag.
- Fixture tests from existing databases.
- Compatibility reads for legacy columns.

The storage layer is the riskiest shared surface. It should be deep: callers should not know schema quirks, legacy defaults, encrypted value formats, or old column migrations.

### 4. Split Server Module Into Control Plane And Data Plane

Do not move all `server-api` behavior into Go as one large module.

Target split:

- Go server control plane: accounts, credentials, snippets, task registry, audits, route handling, Agent connection registry, metrics persistence.
- Rust Agent data plane: host metrics, Docker, file operations, PTY terminal, command execution, update tasks.
- Optional Go direct-SSH adapter: only for fallback where no Agent is available; this must be a separate adapter, not the default path.

### 5. Retire Music Instead Of Migrating It

The target backend must not implement the music module. The retirement work should include:

- Hide/remove module metadata and navigation entry.
- Stop mounting `/api/music`.
- Remove music-specific runtime dependencies from production packages.
- Decide whether to keep, archive, or drop `music_settings`.
- Add release notes explaining removal.

This cleanup can happen before or during Go migration, but it should not block Go main shell work.

### 6. Use Provider Adapters For Cloud And AI Modules

Cloud and AI modules should not embed HTTP/SDK calls directly in route handlers. Go should define provider adapters for:

- Cloudflare.
- Aliyun.
- Tencent.
- Koyeb.
- Fly.io.
- OpenAI-compatible endpoints.
- Gemini CLI / Google OAuth / Antigravity requester.
- Qwen.
- OpenList.

Tests should run against fake adapters. Real-account validation stays manual.

### 7. Preserve Node Sidecar Only As A Migration Tool

Node sidecar is temporary. It should have:

- Explicit module allowlist.
- Health check.
- Clear logs showing proxied routes.
- No ownership of tables once a module is marked Go-owned.
- A final removal checklist.

### 8. Keep The Existing Frontend Contract Stable

The frontend should continue using existing paths during migration. If a route needs cleanup, create compatibility aliases and deprecate them later after Node retirement.

## Migration Waves

### Wave 0: Contract Freeze And Baseline

- Generate a formal route inventory from the current Node app.
- Capture representative JSON responses for key endpoints.
- Capture SQLite schema and legacy migration behavior.
- Measure current Node idle RSS, Docker image size, startup time, and low-memory mode behavior.
- Record all public/auth/API-key/Agent route classifications.
- Mark music as retired in target architecture.

### Wave 1: Go Main Shell And Core

- Implement Go main startup, config, logging, static assets, health, security headers, CORS, compression rules.
- Implement auth/session/password/2FA.
- Implement settings/system/log routes.
- Implement DB backup/import preview/import commit/vacuum/analyze/self-check.
- Implement Node legacy sidecar reverse proxy.
- Keep frontend unchanged.

### Wave 2: Toolbox And Local Utility Modules

- TOTP.
- Filebox.
- Uptime.
- Notification.
- Cron.
- OpenList.

These modules have strong local domain logic and measurable tests. Uptime and notification should establish the Go worker/event scheduler pattern.

### Wave 3: Cloud And PaaS Modules

- Cloudflare.
- Aliyun.
- Tencent.
- Koyeb.
- Fly.io.

Provider adapters and fake-provider tests should be mandatory before real-account validation.

### Wave 4: API Gateway And AI Proxy Modules

- OpenAI endpoint manager and `/v1` compatibility.
- Qwen account/proxy/log/model redirect flows.
- Gemini CLI account/proxy/log/model check flows.

Streaming compatibility is the key risk. Gemini may temporarily retain helper binaries or a sidecar adapter if reproducing behavior in Go would delay Node retirement.

### Wave 5: Server Workbench

- Server accounts and credentials.
- Agent connection registry.
- Metrics persistence and broadcast.
- Task registry and task SSE.
- Quick commands and history.
- Docker control via Rust Agent.
- File management via Rust Agent.
- Terminal via Rust Agent PTY.
- Optional direct SSH/SFTP fallback adapter.

This is the highest-risk wave and should not be first.

### Wave 6: Node Retirement

- Remove Node sidecar.
- Remove retired music dependencies.
- Remove migrated Node module runtime dependencies.
- Update Dockerfile and docker-compose.
- Update docs and verification records.
- Keep frontend build tooling as Node/Vite unless a separate frontend build decision is made.

## Testing Decisions

### Test External Behavior

Tests should cross module interfaces the same way callers do. Avoid tests that depend on internal handler layout. Useful test surfaces:

- HTTP route contract tests.
- Storage module tests with SQLite fixtures.
- Migration self-check tests.
- Provider adapter tests with fake clients.
- Worker/scheduler tests with controlled clocks.
- Agent protocol tests with fake Agent clients.
- Streaming tests for SSE/WebSocket payload ordering.

### Contract Tests

Before migrating a module, capture:

- Status codes.
- Response envelopes.
- Error messages and error codes where present.
- Auth requirements.
- Query/body parameter behavior.
- Pagination/default values.
- Import/export payload shapes.

Go must pass the same contract tests before a route prefix is switched from Node to Go.

### Database Tests

Use copied fixture databases representing:

- Fresh install.
- Current latest schema.
- Older schema with missing compatibility columns.
- Data with encrypted secrets.
- Data with large logs/history tables.

Every migration must be idempotent.

### Security Tests

Cover:

- Public route allowlist.
- Authenticated route protection.
- API key routes.
- Agent key routes.
- 2FA enable/disable.
- Filebox public password verification.
- Dangerous server/Docker operations requiring confirmation/audit.
- Secret redaction in logs and export paths.

### Resource Tests

Record and compare:

- Go main idle RSS after Node retirement.
- Go main startup time.
- Docker image size.
- Rust Agent idle RSS.
- Uptime worker memory over long-running checks.
- WebSocket/Agent reconnect behavior.

Suggested target: Go main should be materially smaller than current Node runtime in low-memory deployments; exact threshold should be set after Wave 0 measurement.

## Out Of Scope

- Migrating or preserving the music module.
- Rewriting the React/Kumo frontend.
- Replacing SQLite with another database.
- Adding multi-user RBAC.
- Expanding provider features beyond current behavior.
- Rewriting the Rust Agent from scratch.
- Removing Node before route contracts and DB migration tests are in place.
- Changing the public OpenAI-compatible API shape except where a compatibility alias is kept.

## Further Notes

The recommended first implementation task is **Wave 0 + Wave 1 shell**, not a business module. The shell creates the migration seam and gives every later module a safe place to land.

Highest-risk areas:

- `server-api` because it combines Agent, SSH/SFTP, Docker, terminal, metrics, tasks, and credentials.
- `cloudflare-api` because it has the largest provider surface.
- `gemini-cli-api` because it combines OAuth, streaming, proxy behavior, helper binaries, and compatibility-sensitive API flows.
- SQLite migrations because old user data must survive the runtime switch.

Fastest likely wins:

- Removing music from the target runtime.
- Moving production dependency installation away from frontend-only packages.
- Go main shell with Node sidecar proxy.
- Core auth/settings/system/logs migration.
- TOTP/Filebox/Uptime/Notification tests and Go ports.
