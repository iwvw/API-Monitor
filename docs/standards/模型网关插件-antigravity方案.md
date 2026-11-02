# 模型网关插件：Antigravity 方案

## 目标

把 Google Antigravity（Cloud Code Assist 上的 Claude / Gemini 订阅额度）转成
Anthropic Messages 兼容 API，供网关端点和兼容中继使用。

## 参考实现

[router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 的
antigravity provider（v8）：

| 能力 | 上游位置 |
| --- | --- |
| 客户端版本发现与 UA | `internal/misc/antigravity_version.go` |
| OAuth / 项目发现 | `internal/auth/antigravity/{auth,constants}.go` |
| 执行与请求构建 | `internal/runtime/executor/antigravity_executor*.go` |
| Claude↔Antigravity 翻译 | `internal/translator/antigravity/claude/*` |
| 工具 schema 清洗 | `internal/runtime/executor/antigravity_executor_request.go` |
| 模型目录与额度 | `internal/registry/models/models.json`（`antigravity` 段） |

本项目把上述能力落成 `backend-go/internal/antigravity/`（插件外壳）+
`backend-go/internal/antigravity/engine/pkg/antigravity/`（协议引擎）。

## 客户端指纹（重要）

Cloud Code **拒绝 2.23.0 以下的客户端访问新模型**，所以 UA 版本不能写死。

- `version.go` 后台每 3 小时从官方 Hub 的 auto-updater manifest
  （`antigravity-hub-auto-updater-*.run.app/manifest/latest-arm64-mac.yml`）拉最新版本，
  缓存 6 小时；拉取失败时保留上次成功值，缺失才回退 `2.24.0`。
- UA 形态与官方一致，changelist 只对已知发版套用：

  ```
  antigravity/hub/<version> (aidev_client; os_type=darwin; arch=arm64; cl=<changelist>)
  ```

- `onboardUser` 用长控制面 UA（追加 `google-api-nodejs-client/10.3.0`）+
  `X-Goog-Api-Client: gl-node/22.21.1`；生成类请求用短 UA。
- `ANTIGRAVITY_USER_AGENT_VERSION` 可覆盖版本（排障用），不设置则跟随 Hub。

## 端点

`BaseURLs` 顺序为 **daily → prod**。消费级凭据默认落在 daily，且
`retrieveUserQuotaSummary`、`fetchUserInfo`、`setUserSettings` 只在 daily 可用。
生成请求固定用 `ForwardBaseURLs()[0]`（daily）；**不再按套餐切 prod** ——
同一账号在不同端点可能拿到互不兼容的模型授权。

## 请求构建

- `loadCodeAssist` 只带 `metadata.ideType=ANTIGRAVITY`，不再发 ide 版本/名称。
- `onboardUser` 用 `tier_id` + `metadata{ide_type, ide_version, ide_name}`
  （旧实现的 `tierId`/`platform`/`pluginType` 被上游忽略）。
- 工具声明 schema 走 `CleanJSONSchema`：展开 `$ref`、合并 `allOf`、剥离
  Gemini v1internal 不支持的字段。
- 工具调用 ID 必须是 `^[a-zA-Z0-9_-]+$` 且请求内唯一。网关侧 OpenAI 入口常用
  `call_xxx|fc_yyy` 形式，因此 `SanitizeClaudeToolID` + `toolIDMapper` 负责清洗
  与去重，`functionCall`/`functionResponse` 共用同一映射保证配对。
- `contents` 首尾归一化（`turns.go`）：
  - 先丢弃尾部「纯 thought 的 model turn」（上游 #6530 会 400）；
  - Gemini 系再补空 user turn（带 `forceEmptyText`，确保真发 `{"text":""}`）；
  - Claude 系不补空 turn —— 其适配层拒绝空文本 part；
  - 尾轮是带 `functionResponse` 的 model turn 时保留（上游靠它继续生成）。

## 额度

- `maxOutputTokens` 按 `models.go` 的 per-model 上限收口（`claude-*-5-5-high`
  为 128000，Gemini 4 Argon 为 1048576），未知模型回落到历史默认值。
- `thinking.budget_tokens` 按模型 `[min, max]` 裁剪；低于最小值时整体移除
  `thinkingConfig`（上游对此直接 400，不会自动纠正）。
- `max_tokens` 始终保持在 `budget_tokens` 之上，且不越过模型上限。

## 已知取舍

- 静态目录只是 `fetchAvailableModels` 不可达时的兜底；上游新增模型后需要同步
  `models.go` 与 `claude_types.go`，否则新模型的额度上限会落到默认值。
- 不在本项目实现上游的 reasoning-replay 缓存与 CAQS 签名包装：那部分依赖
  CLIProxyAPI 自身的会话存储，超出插件职责。
