---
description: 无人值守：OCR 审查并修复，全部未提交改动按域提交推送 dev；ARM 本机改动直接 local 构建部署，非 ARM 改动等 CI 通过后在 ARM 拉取构建部署（Agent 统一走 release 升级）
agent: build
---

执行完整的"审查 → 修复 → 提交推送 → 部署"流程，严格遵循 CONTEXT.md 约定。用户输入：$ARGUMENTS

**总原则：全程无人值守，绝不向用户提问。** 有疑问、选项分歧、归属不明时一律取推荐/默认设置（保守、低风险）：分支非 `dev` 就切过去；无法归组的文件并入最接近的域组或单独「chore」提交；可独立验证的步骤立即提交，不积压。仅当命令_本身_受凭据/权限阻塞时才停。

## 阶段 1：盘点工作区

1. `git status` + `git log --oneline -5`，确认在 `dev` 分支（否则先切）。
2. 盘点范围 = 未提交改动（含未跟踪文件）+ 已提交未推送的提交。
3. **所有未提交改动一律纳入本次提交，包括其他窗口/任务留下的文件**；不得遗漏、不得以「他人工作」为由跳过；提交信息按各文件实际内容描述。
4. 判定本次是否涉及 Agent 改动（`agent-rust/**` 的 `src`/`vendor`/`Cargo.toml`/`Cargo.lock`），涉及则记录目标版本 = `agent-rust/Cargo.toml` 的 `[package].version`，供阶段 6.5 使用。

## 阶段 2：OCR 审查（delegation 模式）

加载 `open-code-review-delegate` skill 并按其工作流执行：

1. `ocr delegate preview --format json`（workspace 模式）审查未提交改动；有已提交未推送的提交时，对每个提交跑 `ocr delegate preview -c <hash>`。
2. 逐文件用 `ocr delegate rule` 取规则、`git diff` 取变更后审查。
3. 输出必须含 total_files / reviewed_files / skipped_files / coverage_rate，不得无声跳过任何文件。

审查范围 = 本次将要提交的全部代码（stage 的、未 stage 的、未跟踪的、未推送的提交）。

## 阶段 3：修复异常

1. **Critical/High**（bug、安全、数据丢失）：全部修复，修完重新审查验证。
2. **Medium**：能自动修的直接修；需人工判断的取推荐/默认设置，不询问。
3. **Low**（风格类）：除非 trivial 否则跳过。
4. 修完跑 `npm run audit:fast`（或必要的 lint/单测）确认仍通过。

## 阶段 4：分类提交

按 CONTEXT.md 的 natural parallelism 分组：`backend-go/`、`src/js/`、`agent-rust/`、`docs/`+配置+工具脚本（各自独立或合并）。

每组单独 `git add` + `git commit`，信息遵循 `git log` 现有风格。提交前确认：无遗漏、无混入无关文件、无密钥/敏感数据。

## 阶段 5：推送

1. 推送前 `git status` 确认工作区干净。
2. `git push origin dev`；被拒则 `git pull --rebase` 后重推。

## 阶段 6：等待 CI 并部署

1. **判定路径**：本机是 ARM 生产主机（`uname -m` = `aarch64` 且 `api-monitor` 容器跑在本机）走 6a，否则走 6b。
2. **6a｜ARM 本机改动**：跳过 CI 等待，直接构建部署（阶段 4/5 的提交推送照常做，否则代码没进仓库）：
   ```bash
   cd /opt/code/API-Monitor && export PATH="$PATH:$HOME/.cargo/bin:/usr/local/go/bin" && ./tools/local-image.sh
   cd /opt/1panel/docker/compose/api-monitor && docker compose up -d
   ```
3. **6b｜其它主机改动**：先 `gh run watch` 等 CI 全绿（`CI/CD Pipeline`、`Security Baseline`，涉及 `agent-rust/**` 时另有 `Agent Release`），失败则修复重推；然后在 **am MCP** 上对「ARM」主机执行（先调 `/api/server/accounts` 按 name 查 serverId 并确认在线）：
   ```bash
   cd /opt/code/API-Monitor && git pull --rebase origin dev && \
   export PATH="$PATH:$HOME/.cargo/bin:/usr/local/go/bin" && ./tools/local-image.sh && \
   cd /opt/1panel/docker/compose/api-monitor && docker compose up -d
   ```
   超过 MCP 等待上限返回 504 时用 `wait=false` 异步执行并轮询 `agent_task_result`。
4. **Agent 不打进镜像**：`agent-rust/**` 一律走阶段 6.5（`local-image.sh` 顺带编的本机 Agent 仅供镜像内调试）。
5. **部署后校验**（两条路径相同）：`docker ps` 为 `Up ... (healthy)`；`docker inspect api-monitor --format '{{.Image}}'` 必须等于 `docker images iwvw/api-monitor:local --format '{{.ID}}'`（`local` 是可变标签，只有 ID 可靠）；`restarts=0`；日志无 `SECURITY WARNING` 与 panic。
6. **环境变量**（写进 compose 的 `environment`，**不要写进本仓库**）：`ENCRYPTION_KEY` 生产必须设且 ≥32 字符（缺失时 `APP_ENV=production` 直接启动失败）；可选 `API_MONITOR_STRICT_ENCRYPTION=1`、`API_MONITOR_INTERNAL_CRON_TOKEN`+`API_MONITOR_INTERNAL_CRON_STRICT=true`、`MODELHUB_GATEWAY_ALLOW_LOCAL_WITHOUT_KEY`（默认关闭）。

## 阶段 6.5：Agent 版本升级（仅当涉及 agent-rust/）

Agent 版本已按 CONTEXT.md 递增，release（built-agents）会产出新二进制。生产面板部署完成后**必须把所有主机 Agent 升到新版本**，否则旧版本永远升不上去。全程经 am MCP，禁止把凭据写入本仓库。

1. 判定：对本次各提交 `git diff-tree --no-commit-id --name-only -r <hash>`，任一含 `agent-rust/` 即视为涉及；目标版本取阶段 1 记录值。
2. GET `/api/server/accounts` 收集每台主机的 `id`、`agent_version`、`is_online`。
3. POST `/api/server/agent/batch-upgrade?protocol=<http|https>`，一次覆盖全部在线主机：
   ```json
   {"serverIds":[<全部在线主机 id>],"protocol":"<http|https>","base_url":"<面板公网 base url>","concurrency":16,"force_ssh":false,"fallback_ssh":false}
   ```
   用返回的 `payload.data.id` 轮询 GET `/api/server/agent/batch/{id}` 至 `succeeded`；`failed`/部分失败时查详情并重试失败主机。
4. 再 GET `/api/server/accounts` 逐一核对**每台**主机 `agent_version` 等于目标版本。当时离线的主机等其上线补升；**任何一台未升级都算未完成**，不得提前关闭 issue。

## 阶段 7：关闭对应 GitHub Issues

本次改动对应了已跟踪 issue（`backlog`/`in-progress`/`done` 标签）时，部署通过后逐个闭环。遵循 `docs/guides/待办任务闭环流程.md` 第 3 步：**先加 `done`、再移除 `in-progress`/`backlog`、最后关闭并附完成说明**。

```bash
n=41   # 对每个 issue 重复
gh issue edit "$n" --repo iwvw/API-Monitor --add-label done
gh issue edit "$n" --repo iwvw/API-Monitor --remove-label in-progress --remove-label backlog
gh issue close "$n" --repo iwvw/API-Monitor -r completed -c "实现要点 + 涉及 commit；验证：npm run audit:fast、npm run lint、go test ./...、cargo build；生产容器已更新为 iwvw/api-monitor:local 并 healthy"
```

**坑**：`gh issue close` 没有 `--label`，标签只能用 `gh issue edit` 管理；只 close 不换标签会让已关闭 issue 仍挂 `backlog`/`in-progress`。遇到 `Post ... EOF` 网络抖动要回查该 issue 标签并补处理。

## 完成标准

- OCR 覆盖率 100%，Critical/High 全部修复
- 工作区干净，提交按域分类、无无关文件
- `dev` 已推送，Actions 全绿（ARM 本机改动也需推送，只是不必等 CI 再部署）
- 生产容器为 `iwvw/api-monitor:local`、`healthy`，且容器镜像 ID 与本机最新 `local` 镜像 ID 一致
- 涉及 Agent 改动时：所有主机 `agent_version` 已核对一致
- 对应 issue 已闭环（标签仅 `done` 且已关闭）
