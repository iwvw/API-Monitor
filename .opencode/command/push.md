---
description: 无人值守：OCR 审查并修复，全部未提交改动按域提交推送 dev，等 CI 通过后按 SSH-Docker 约定部署生产
agent: build
---

执行完整的"审查 → 修复 → 提交推送 → 部署"流程，严格遵循 CONTEXT.md 中的约定。用户输入：$ARGUMENTS

**总原则：全程无人值守，绝不向用户提问。** 遇到任何疑问、选项分歧、归属不明的改动，一律采用推荐/默认设置（保守、低风险）直接处理：分支非 `dev` 就直接切到 `dev`；归属不明或无法自动分组的文件并入最接近的域组或独立「chore」提交；可独立验证的步骤立即独立提交，不积压。除非命令 _本身_ 因凭据/权限制约而无法继续，否则不问。

## 阶段 1：盘点工作区

1. 运行 `git status` 和 `git log --oneline -5`，确认当前分支（必须是 `dev`，否则先切到 `dev`）。
2. 确认工作区状态：未提交改动（含未跟踪文件）+ 已提交未推送的提交。
3. **工作区所有未提交改动（含其他窗口/任务留下的、不属于本次代码的文件）一律纳入本次提交推送范围**，不得遗漏、不得视为「他人工作」而跳过；提交信息按各文件实际内容描述。

## 阶段 2：OCR 审查（delegation 模式）

1. 加载 `open-code-review-delegate` skill，按 skill 工作流执行：
   - `ocr delegate preview --format json`（workspace 模式）审查未提交改动；若有已提交未推送的提交，对每个提交用 `ocr delegate preview -c <hash>` 审查。
   - 对每个 reviewable 文件执行 `ocr delegate rule` 取规则、`git diff` 取变更，进行逐文件审查。
   - 输出必须包含 total_files / reviewed_files / skipped_files / coverage_rate，任何文件不得无声跳过。
2. 审查范围 = 本次将要提交的全部代码（stage 的、未 stage 的、未跟踪的、未推送的提交）。

## 阶段 3：修复异常

1. 修复所有 **Critical/High** 问题（bug、安全、数据丢失风险），修复后必须重新审查验证。
2. **Medium** 问题：可自动修复的直接修；需要人工判断的按推荐/默认设置（保守、低风险）处理，不询问用户。
3. **Low** 风格类：除非 trivial 否则跳过。
4. 修复后用 `npm run audit:fast`（或必要的 lint/单测）验证代码仍通过检查。

## 阶段 4：分类提交

按项目领域分组提交（参考 CONTEXT.md 的 natural parallelism）：
- `backend-go/`（Go 后端）
- `src/js/`（前端）
- `agent-rust/`（Rust Agent）
- `docs/` + 配置 + 工具脚本（各自独立或合并为一组）

**工作区所有未提交改动（含其他任务/窗口留下的、不属于本次代码的文件）必须全部纳入提交**，不得遗漏、不得视为「他人工作」而跳过；提交信息按各文件实际内容描述。

每个组单独 `git add` + `git commit`，提交信息遵循 `git log` 现有风格（简洁、中文或英文按惯例）。检查确认没有遗漏、没有混入无关文件、更没有把密钥/敏感数据提交进去。

## 阶段 5：推送

1. 推送前再次 `git status`，确保工作区干净（自己刚提交完）。
2. `git push origin dev`。若推送被拒绝（远端有新提交），先 `git pull --rebase` 再推。

## 阶段 6：等待 CI 并部署

1. 用 `gh run watch` 监听最新推送触发的 Actions 运行，直到全部通过。
2. 若 CI 失败：查看失败日志、修复问题、重新提交推送，再回到阶段 6。
3. 全部通过后执行 SSH-Docker 部署约定：用新构建的 `iwvw/api-monitor:dev` 镜像更新生产服务器上的 Docker 容器 `api-monitor`。连接信息（服务器/凭据/主机键指纹）与完整命令见本机 `~/.config/opencode/instructions.md`「SSH-Docker 部署约定（API-Monitor）」，**禁止把任何凭据写入本仓库**。

## 阶段 7：关闭本次改动对应的 GitHub Issues

若本次改动对应了已跟踪的 task issue（`backlog`/`in-progress`/`done` 标签），部署通过后逐个闭环。**遵循 `docs/待办任务闭环流程.md` 第 3 步的标准三步：先加 `done` 标签、再移除 `in-progress`/`backlog`、最后关闭附完成说明。**

**常见坑（必须避免）：只 `gh issue close` 而不换标签，会导致已关闭的 issue 仍挂着 `backlog`/`in-progress`。**

```powershell
# 1) 加 done 标签
gh issue edit <编号> --repo iwvw/API-Monitor --add-label done

# 2) 移除 in-progress / backlog（只留 done）
gh issue edit <编号> --repo iwvw/API-Monitor --remove-label in-progress
gh issue edit <编号> --repo iwvw/API-Monitor --remove-label backlog

# 3) 关闭，comment 写清实现要点 + 涉及 commit + 验证命令
gh issue close <编号> --repo iwvw/API-Monitor -r completed -c "实现要点与涉及 commit；验证：npm run audit:fast、npm run lint、go test ./...、cargo build；CI Security Baseline + CI/CD Pipeline 均 success；生产容器已更新为 iwvw/api-monitor:dev"

# 批量用 PowerShell 循环（本机是 pwsh，勿用 bash 的 for ...; do）
foreach ($n in 41,42,43,44,45,46) {
  gh issue edit $n --repo iwvw/API-Monitor --add-label done
  gh issue edit $n --repo iwvw/API-Monitor --remove-label in-progress
  gh issue edit $n --repo iwvw/API-Monitor --remove-label backlog
  gh issue close $n --repo iwvw/API-Monitor -r completed -c "..."
}
```

注意：`gh issue close` 没有 `--label` 参数，标签必须用 `gh issue edit` 单独管理。批量操作若遇 `Post ... EOF` 网络抖动导致单个 issue 未更新，需回查该 issue 标签并补处理。

## 完成标准

- OCR 覆盖率 100%，Critical/High 全部修复
- 工作区干净，提交按域分类、无无关文件
- dev 已推送，Actions 全部通过
- 生产容器已按 SSH-Docker 约定更新并验证可访问
- 本次改动对应的 issue 已闭环（标签为 `done`、非 `backlog`/`in-progress`，且已关闭附验证命令）