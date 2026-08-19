---
description: OCR 审查全部待提交代码并修复异常，按域分类提交推送 dev，等待 CI 通过后部署 Fly.io
agent: build
---

执行完整的"审查 → 修复 → 提交推送 → 部署"流程，严格遵循 CONTEXT.md 中的约定。用户输入：$ARGUMENTS

## 阶段 1：盘点工作区

1. 运行 `git status` 和 `git log --oneline -5`，确认当前分支（必须是 `dev`，否则先切到 `dev`）。
2. 确认工作区状态：未提交改动（含未跟踪文件）+ 已提交未推送的提交。
3. 若工作区存在其他任务留下的未提交更改（不属于本次代码的），不要混入：只处理你的目标文件，必要时先与用户确认，绝不触碰他人未完成的工作。

## 阶段 2：OCR 审查（delegation 模式）

1. 加载 `open-code-review-delegate` skill，按 skill 工作流执行：
   - `ocr delegate preview --format json`（workspace 模式）审查未提交改动；若有已提交未推送的提交，对每个提交用 `ocr delegate preview -c <hash>` 审查。
   - 对每个 reviewable 文件执行 `ocr delegate rule` 取规则、`git diff` 取变更，进行逐文件审查。
   - 输出必须包含 total_files / reviewed_files / skipped_files / coverage_rate，任何文件不得无声跳过。
2. 审查范围 = 本次将要提交的全部代码（stage 的、未 stage 的、未跟踪的、未推送的提交）。

## 阶段 3：修复异常

1. 修复所有 **Critical/High** 问题（bug、安全、数据丢失风险），修复后必须重新审查验证。
2. **Medium** 问题：可自动修复的直接修；需要人工判断的列出来询问用户。
3. **Low** 风格类：除非 trivial 否则跳过。
4. 修复后用 `npm run audit:fast`（或必要的 lint/单测）验证代码仍通过检查。

## 阶段 4：分类提交

按项目领域分组提交（参考 CONTEXT.md 的 natural parallelism）：
- `backend-go/`（Go 后端）
- `src/js/`（前端）
- `agent-rust/`（Rust Agent）
- `docs/` + 配置 + 工具脚本（各自独立或合并为一组）

每个组单独 `git add` + `git commit`，提交信息遵循 `git log` 现有风格（简洁、中文或英文按惯例）。检查确认没有遗漏、没有混入无关文件、更没有把密钥/敏感数据提交进去。

## 阶段 5：推送

1. 推送前再次 `git status`，确保工作区干净（自己刚提交完）。
2. `git push origin dev`。若推送被拒绝（远端有新提交），先 `git pull --rebase` 再推。

## 阶段 6：等待 CI 并部署

1. 用 `gh run watch` 监听最新推送触发的 Actions 运行，直到全部通过。
2. 若 CI 失败：查看失败日志、修复问题、重新提交推送，再回到阶段 6。
3. 全部通过后执行 Fly.io 部署约定：
   - 在 `E:\文件\flyio\api-monitor` 目录（fly.toml 所在处，app `apimnt`）执行 `flyctl deploy`。
   - 部署成功确认后访问 `https://apimnt.fly.dev/` 验证；出现 DNS AAAA 记录传播警告属正常现象。

## 完成标准

- OCR 覆盖率 100%，Critical/High 全部修复
- 工作区干净，提交按域分类、无无关文件
- dev 已推送，Actions 全部通过
- Fly.io 已重新部署并验证可访问