# AGENTS.md

API Monitor —— 自托管 API / 云资源 / 主机监控面板。前端 React+Vite，后端 Go+SQLite，Agent 用 Rust。

**先读 `CONTEXT.md`（仓库根目录，会自动加载）。** 它记录当前架构、不可协商规则、高风险文件清单与协作约定；本文件只补充「改了会踩坑」的落地要点，与 CONTEXT.md 冲突时以 CONTEXT.md 为准。

## 命令

- 装依赖：`npm install`；起前端+后端：`npm run dev`（Vite + `go run`，前端 `http://localhost:5173`）。
- 本机打生产镜像（不走 CI 全量编译）：`./tools/local-image.sh` → `iwvw/api-monitor:local`，以已发布镜像为基底只覆盖本机编译的 Go/前端/Agent 产物，约 10 秒。配套 `docker-compose.local.yml` 起在 3001 端口。Agent 静态编译需 `apt install musl-tools` + `rustup target add aarch64-unknown-linux-musl`。
- 前端单测：`npm test`（vitest，`test/`、`src/js/modules/**`、`src/js/components/**`、`src/js/pages/**` 下 `*.test.js`，node 环境）。单个文件：`npx vitest run test/<x>.test.js`。
- Go 单测：`npm run backend-go:test`（= `cd backend-go && go test ./...`）。单包/单测：`cd backend-go && go test ./internal/<pkg>/ -run <Name> -v`。CI 用 `-p 1` 串行（并发包会在 Linux 撞 TempDir 句柄，非真失败，会自动重跑一次）。
- 快速审计：`npm run audit:fast`（`icons:generate` → `governance:check` → `ui:governance` → `kumo:registry:check` → `docs:check` → `lint` → `test`）。完整审计：`npm run audit:full`（另含 Go 测试、路由清单、后端冒烟，需运行中的 Go 后端在 `API_MONITOR_BASE_URL` 或 `http://127.0.0.1:3000`）。
- 安卓客户端 `mobile-android/`（Kotlin + Compose 骨架）吃同一 Go 后端接口，但**不受 Kumo-only 与 `ui:governance` 约束**（那套规则只作用于 `src/js`）；不参与 `audit:fast`/`audit:full`，构建需本地 Android SDK（`npm run android:assemble` / `android:test`）。
- `governance:check` = 三侧治理 + 时区治理 + motion 治理 + Agent 版本治理，四个独立脚本（`tools/*-governance*.mjs`）。改路由/`agent-rust`/时间/动画前先看对应脚本的判定。
- `npm run backend-go:dev` 由 `tools/dev-backend.mjs` 跨平台启动(Windows/macOS/Linux 同一条命令):先读根 `.env` → `.env.local` 注入尚未设置的变量,再在 `backend-go/` 下执行 `go run ./cmd/api-monitor`。Go 侧 `internal/config/dotenv.go` 也会加载 `.env`(进程环境优先),但**不读** `.env.local`。

## 路由是「两处」不是一处

`backend-go/internal/manifest/manifest.go` 登记路由归属/鉴权（路由事实来源），但**分发按 `Module` 查表**——注册表在 `backend-go/internal/server/route_handlers.go`（`moduleHandlers`），不再有 `server.go` 的路径 `case`。

新增后端路由必须：① 在 `manifest.go` 加行；② 在 `route_handlers.go` 的 `moduleHandlers` 为对应 `Module` 登记 handler（同一 `Module` 可复用已有行）。漏任一处 `npm run governance:check` 会失败。前端 `src/js/**` 里出现的 `/api/*` 路径字面量也必须被 manifest 覆盖，否则治理检查报错。

## Kumo-only UI（违反即回归）

- 只用 `@cloudflare/kumo` 组件；弹窗统一用 kumo 原生 `LayerDialog`（`@cloudflare/kumo/components/layer-dialog`），不要再写自研包装。
- `LayerDialog.Content` 的直接子元素只允许一个 `Title`、一个 `Body`，加可选的 `Description` 和 `Actions`；`Actions` 只能含一个 `LayerDialog.Action`（2.15 起；`Actions.Primary` 只是弃用别名）。违反**运行时抛错**。
- `LayerDialog` 的 `Content`/`Title`/`Description`/`Body` 会**静默丢弃**写在它们上的 `className`/`ref`（不报错也不生效）。要加类名或 ref，放到 slot 内部自己的元素上。
- 底部多个并列业务按钮、或内容是依赖确定高度的画布/终端/编辑器时，用 kumo 基础 `Dialog`，别硬套 `LayerDialog`。
- 破坏性删除确认迁移到 Kumo `DeleteResource`（`dialog.deleteResource`）。
- `npm run ui:governance` 把守；允许的例外见 `docs/standards/重构验证与例外清单.md`，审计报错前先查它。

## 硬性规则（改前必看）

- **时区**：唯一控制点是 `user_settings.time_zone`，业务一律经 `internal/timeutil` 取站点时区；禁止 `time.Local`/`time.UTC` 做日期归属。新增调度器必须 `cron.New(cron.WithLocation(站点时区))` + TZ watcher（参考 `internal/cronjobs`）。
- **SQLite**：周期 WAL 维护**只做 `wal_checkpoint(PASSIVE)`**；禁止自动路径用 TRUNCATE/RESTART（常驻轮询读者 + modernc 驱动会无限阻塞，是 `database is locked` 风暴根因）。主动压缩只走设置页「数据库压缩」等用户动作。
- **流量不可合并**：机器网卡流量、托管节点原始流量、订阅用户用量、导入的外部节点流量，绝不合并成一个总量。
- **托管内部代理端口**：面板分配，范围含端点 `45654-55654`，**绝不是 443**；不改导入的外部节点端口。
- **持久化**：SQLite 是唯一持久化存储，不要替换。当前性能可接受时不要主动做性能优化。
- **Agent 版本绑定**：改动 `agent-rust/src/**`、`vendor/**`、`Cargo.toml`、`Cargo.lock` 任一处，必须同步递增 `[package].version`（`Cargo.toml` 与 `Cargo.lock` 一致）。只改代码不 bump 会导致主机永远升不上新二进制。`tools/agent-version-governance.mjs` 把守。
- 默认不要删除或重写 `.env`、`data/`、`backup/`、`backend-go/data/`、`backend-go/internal/server/data/`、`node_modules/`、`public/`。

## 高风险文件（单文件改动，改前确认无其他任务持有）

`src/js/pages/ServerPage.jsx`、`src/js/pages/DnsPage.jsx`、`src/js/pages/OpenAIPage.jsx`、`src/js/components/MainLayout.jsx`、`src/js/store.js`、`backend-go/internal/server/server.go`、`backend-go/internal/manifest/manifest.go`、`backend-go/internal/serveragent/service.go`。都有明确理由才动，不要顺手大范围格式化或重写。超大文件拆分按 CONTEXT.md「重构顺序」：常量/纯函数 → hooks → 弹窗 → 表格/面板 → 页面容器，每步独立可验证。

## 版本与发布

- `package.json` 的 `version` 是**唯一**正式版本来源；Git tag/Release 加 `v` 前缀（`v2.0.1`）。不要在 `dev` 手动改版本或以 tag 代替源码版本。
- 发布链路全自动:合并 `dev`→`main` → `Bump Main Version` 工作流递增 → Release 工作流建 tag、多架构 Docker 镜像,全绿后才把草稿转公开。发失败保留草稿。
- **Agent 与镜像解耦**:Agent 二进制不再打进镜像。`agent-release.yml` 单独构建三平台并发布到**本仓库**的 release(仓库为 public,可匿名直链;dev 用滚动 tag `built-agents`,正式发布用 `agent-v<Cargo.toml 版本>`;产物按「三平台一组」上传,含 `.sha256`)。镜像构建期注入 `AGENT_DOWNLOAD_URL` 指向该 release,主机安装与 Agent 自升级都走匿名直链;离线/内网部署可用 `--build-arg INCLUDE_AGENTS=true` 把三平台产物(预置在 `agent-rust/` 下)重新打进镜像。
- `dev` 分支推送触发 `ci-cd.yml`:前端 lint/test/build + Go 测试(按包分 2 片并行)+ 多架构镜像,镜像 tag `dev`。发布 Agent 用默认 `GITHUB_TOKEN`(`contents: write`),不需要额外 secret。

## 多窗口 / 多 Agent 协作

- 进行中的工作跟踪在 GitHub Issues（`iwvw/API-Monitor`，`backlog`/`in-progress`/`done` 标签）；每个任务在 issue 正文声明文件域。开始前找/建 issue 并置 `in-progress`，不碰其他 `in-progress` 任务持有的文件。
- 提交节奏：完成一个可独立验证的步骤就立即提交，不要攒。改前查 `git status`，别人有未提交改动时先协调。
- **工作区有未提交改动（任何任务留下的）时禁止 `git push`**；只提交自己域，推送等干净且确认无他人未提交内容后再做。
- 单一集成者负责合并、`npm run audit:full` 与发布；其他窗口只提交。慢命令（`audit:full`）、3000 端口/Go 进程、SQLite 迁移脚本需串行/互斥。

## 清理

可安全清理（可再生）：`.cache/`、`.tmp/`、`dist/`、`backend-go/tmp/`、`backend-go/api-monitor.exe`、`agent-rust/target/`。用 `npm run clean:check` 出报告、`npm run clean:workspace` 按允许清单删除。`public/` 虽被忽略但可能被静态服务/部署使用，删除或重写前必须明确确认。
