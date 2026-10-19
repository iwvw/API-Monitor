# 模型网关插件 - Qoder 方案

最后更新：2026-10-02

本文档记录 Qoder 插件（`qoder.com` → OpenAI 兼容 API）的上游协议事实、实现结构与运维要点。
插件把 Qoder 设备登录账号封装成 OpenAI 兼容上游，接入模型网关端点列表，并支持每日自动签到与余额检测。

## 1. 定位与来源

- 上游：Qoder，**分国内版与国际版两套独立区域**（见 §2.5）。
- 协议来源：开源反代 **qoder2api**（`jyao0708/qoder2api`，国际版）、
  **qoder2api-worker**（`nostalgia296/qoder2api-worker`，国内版）与
  **qoder2api-hub**（`shuishuipingan/qoder2api-hub`，双区域）记录的设备登录、
  加密 SSE 与控制面接口。
- 实现：`backend-go/internal/qoder/`，前端 `src/js/pages/openai/plugins/QoderPlugin.jsx`。
- 与同类插件（lobsterai/workbuddy/geminicli）目录结构、表命名、link 语义保持一致。

## 2. 目录结构

```
backend-go/internal/qoder/
├── service.go    Settings/Account、建表、选号、调用计数、ServeHTTP 入口、站点时区
├── upstream.go   设备登录、身份加密与签名、加密 SSE 对话、auth JSON 解析、token 刷新
├── auth.go       账号脱敏、CRUD、选号与失败冷却、导入导出、签到/余额入口
├── models.go     模型目录对外视图、逐模型启停、停用名单别名归一
├── catalog.go    模型目录动态发现（签名 GET model/list + 按区域缓存 + 实时倍率解析）
├── offpeak.go    倍率裁决与错峰促销规则（上游 promotion 驱动 + 静态兜底）
├── relay.go      中继面：请求转发、换号重试、流式透传
├── billing.go    余额查询与每日签到（openapi 控制面）
├── link.go       网关端点接入（接入/断开/状态）
├── usage.go      用量统计（站点时区 × 账号 × 模型）
└── scheduler.go  每日签到调度（站点时区 cron + TZ watcher）
```

## 3. 上游协议事实

### 3.1 设备登录（`upstream.go`）

1. 生成 `nonce`（32 位 hex）+ PKCE `verifier`/`challenge`（S256），构造引导 URL：
   `https://qoder.com/device/selectAccounts?nonce=&challenge=&challenge_method=S256&client_id=...`
2. 轮询 `GET https://openapi.qoder.sh/api/v1/deviceToken/poll?nonce=&verifier=&challenge_method=S256`：
   - HTTP 404 表示用户尚未完成授权，继续轮询；
   - 200 且 `token` 非空表示授权成功，返回 `{token, user_id, refresh_token, expires_at}`。
3. 用 `Bearer {token}` 拉取 `GET /api/v1/userinfo`、`GET /api/v3/user/status`（校验白名单）、
   `GET /api/v2/user/plan`，组装账号（`securityOauthToken` = token）。

### 3.2 对话（`upstream.go`）

Qoder 对话走**加密 SSE**，不是标准 Bearer：

- 身份加密：`name/aid/uid/yx_uid/organization_*/user_type/security_oauth_token/refresh_token`
  用随机 `tempKey`（16 字节 hex）做 AES-CBC 加密得 `info`；`tempKey` 用内置 RSA 公钥
  PKCS1v15 加密得 `cosyKey`。
- 请求头 `Authorization: Bearer COSY.{payloadB64}.{sig}`：
  - `payloadB64` = base64(`{cosyVersion, ideVersion, info, requestId, version}`)；
  - `sig` = `md5(payloadB64 + "\n" + cosyKey + "\n" + cosyDate + "\n" + body + "\n" + path)`。
- body 经**自定义字母表 base64 变换**（`legacyEncode`），POST 到
  `https://api3.qoder.sh/algo/api/v2/service/pro/sse/agent_chat_generation`。
- 响应每行 `data: {"body":"<openai chunk>"}`；`body` 里是标准 OpenAI 流式 chunk。
  聚合时优先取 `choices[].delta`，兼容 `choices[].message`；tool_calls 按 index 合并。

### 3.3 余额与签到（`billing.go`）

控制面接口**仅需** `Bearer {deviceToken}` + `Cosy-ClientType: 10`，无需 COSY 签名。

| 用途 | 方法与路径 | 关键语义 |
| --- | --- | --- |
| 余额 | `GET {openapi}/api/v2/quota/usage` | `userQuota.{total,used,remaining,unit}`；`isQuotaExceeded`/`limitExceeded` 判耗尽 |
| 活动列表 | `GET {openapi}/sash/api/v1/me/campaigns` | 找 `actionType=CLAIM_BENEFIT` 且 `claimStatus=CLAIMABLE` 的行 |
| 领取 | `POST {openapi}/sash/api/v1/me/campaigns/{campaignId}/claim` | 空 body；成功 `status=GRANTED`，重复 `replayed=true` |

- 每日 100 Credits 活动：领取窗口每天 10:00（次日 10:00 前）开放，每账号每窗口一次。
- 幂等：已领取返回 `CLAIMED`/`replayed=true`，按 `already` 处理而非失败。
- 刷新 token：`POST {openapi}/api/v1/deviceToken/refresh`，body `{refresh_token}`，
  返回 `{token, refresh_token, expires_in}`。

### 3.4 模型目录（`models.go` / `catalog.go`）

模型目录分**动态发现**与**静态兜底**两层：

- **动态发现**（`catalog.go`）：用已登录账号的 COSY 签名会话调
  `GET /algo/api/v2/model/list?Encode=1`（区域基址：国内版 `gateway.qoder.com.cn`、
  国际版 `api1.qoder.sh`；GET 无 body，签名 body 段为空、path 剥去 `/algo` 前缀），
  按分组返回，只取 `chat` 分组。条目 `display_name` 作对外 id、`key` 作上游短 key。
  按区域缓存（`modelCacheTTL` 10 分钟），`StartModelRefresh` 后台定期回源，
  `handleModels` 读取时按 TTL 惰性回源；回源失败保留上一份快照，首次成功前回退静态表。
  这是对外 id 与路由的**权威来源**，能跟上上游增删/改名（参考实现 `cli-proxy/qoder`）。
- **静态兜底**（`cnStaticModels`/`intlStaticModels`）：仅在动态目录首次拉取成功前使用，
  取自官方客户端目录缓存 `catalog-v6` 实测记录，并提供动态目录不带的元数据
  （厂商标识），按上游短 key 回填到动态条目上：

- 国内版 14 个：`Auto`/`Qwen3.8-Max`/`Qwen3.8-Flash`/`Qwen3.7-Max`/`Qwen3.7-Plus`/
  `Qwen3.7-Flash`/`DeepSeek-V4-Pro`/`DeepSeek-Flash`/`GLM-5.3`/`GLM-5.3-Flash`/
  `GLM-5.2`/`Kimi-K3`/`Kimi-K2.8-Preview`/`MiniMax-M2.7`。
- 国际版 17 个：额外有 `Ultimate`/`Performance`/`Efficient`/`Sonus`/`Cantus`，
  无 `GLM-5.2`/`Qwen3.7-Flash`。

**模型标识分两个命名空间（不可混用）**：`ModelInfo.ID` 是**官方模型名**（如
`Qwen3.8-Max`），作为对外模型名——中继 `/v1/models`、网关端点 `models` 列、停用名单
都用它，与官方桌面版选择器一致（参考实现 `qoder2api-hub` 约定）；`ModelInfo.Key`
是**上游短 key**（如 `qmodel_38max`），只在构造上游请求时写入 `model_config.key`
/ `x-model-key`。转发时经 `lookupModelKey` / `normalizeModelKey` 把对外名（或短 key）
解析回上游 key。

参考 workbuddy 的设计：`mergeCatalogs` 把两区域目录合并成一份对外视图（按对外
官方名去重，国内版打底、国际版独有项追加），每个模型标记型号级可用区域 `regions`
与 id 级 `idRegions`，仅国际版提供的标 `internationalOnly`。

**模型列表按实际账号区域过滤**（`catalog`）：只存在国内版账号时仅返回国内版模型，
只有国际版账号时仅返回国际版模型，两区域都有才返回合并视图（带区域标记）；
无账号时默认国内版。因此前端模型表不会把两区域盲目混在一起。

前端模型表：第一行显示官方模型名（对外 id），第二行显示上游短 key；按 `regions`
显示「通用/国内/国际」徽标，并展示实时倍率列（表头带「实时 / 兜底」徽标，
悬浮说明含标准价、折后价、窗口时区与来源；免费模型显示「免费」）。选号按
`accountServesModel` 过滤：账号所属区域不提供目标模型时跳过（官方名与短 key 均可命中）。

**停用采用白名单语义（关键）**：`isModelEnabled` 只放行**当前目录内**且未被停用的
模型名，目录外的任意别名一律拒绝。停用判定经 `disabledMatchTokens` 做**别名闭包归一**
（`modelAliasClosure` 在「静态表 + 动态目录」的 id↔key 连通图上展开）：即使上游把
`DeepSeek-Flash` 改名为 `DeepSeek-V4.1-Flash`（沿用短 key `dfmodel`），按旧名停用
依然生效，杜绝改名/别名绕过停用。网关端点 `models` 列与中继 `/v1/models` 都写
**启用中的**模型名（`enabledModelIDs`），从路由索引源头收敛。

**停用名单迁移**：模型对外 id 由短 key 改为官方名后，加载/保存设置时经
`migrateDisabledModels` 把旧名单里的短 key 归一化为官方名（保持前缀），避免用户
已停用的模型「复活」；启用某模型时（`setModelsEnabled`）用同一别名闭包清掉指向
该模型的旧名/短 key 残留。

### 3.5 倍率与错峰折扣（`catalog.go` / `offpeak.go`）

**倍率全部来自上游实时目录，不再硬编码。** 实测（2026-10-20，国内版账号）
`GET /algo/api/v2/model/list?Encode=1` 的 `chat` 分组逐项带：

| 字段 | 含义 | 备注 |
| --- | --- | --- |
| `price_factor` | 当前标价倍率 | **0 是合法值**（免费模型），必须用指针解析 |
| `original_price_factor` | 原价倍率 | 限时免费模型给出「现价 0 / 原价 0.1」 |
| `is_free` | 上游标记免费 | 与 `price_factor == 0` 取或 |
| `promotion` | 促销规则 | `active`/`rule_id`/`discount_factor`/`before_promotion_price_factor`/`window_start`/`window_end`/`timezone`/`badge`/`link_url` |

解析回填优先级：**上游实时 `price_factor` → 静态表 → 未解析（前端显示「—」）**，
并下发 `priceSource`（`live` / `static`），前端倍率列表头据此显示「实时 / 兜底」徽标。

**关键坑：`price_factor` 与 `promotion` 会互相矛盾。** 实测错峰窗口内
`qmodel_38max` 的 `price_factor` 已是折后值 `0.2`（= 0.5×0.4），而 `qmodel` 的
`price_factor` 仍是原价 `0.1`（官方文档与 promo badge 都写明折后应为 `0.04`）。
因此 `ModelInfo.MultiplierAt` 的裁决顺序是：

1. 实时值已等于 `标准×折扣` → 上游已折价，直接用；
2. 在窗口内且规则未关停 → 按 `标准×折扣` 计算（覆盖 `qmodel` 这种漏写折扣的情况）；
3. 其余取实时值；上游没给实时值才退回静态标准倍率。

促销窗口按上游 `timezone` 判定（实测 `Asia/Shanghai 22:00–08:00`，支持跨午夜），
`offpeak.go` 显式引入 `time/tzdata`，不依赖宿主是否安装时区库。
`staticPromotions` 只在**上游没下发 promotion** 时兜底，保证首次回源成功前展示与历史一致：

| 模型 | 标准倍率 | 错峰倍率 | 窗口 |
| --- | --- | --- | --- |
| Qwen3.8-Max (`qmodel_38max`) | 0.5x | 0.2x（4 折） | Asia/Shanghai 22:00–08:00 |
| Qwen3.7-Plus (`qmodel`) | 0.1x | 0.04x（4 折） | Asia/Shanghai 22:00–08:00 |

注意：错峰只改倍率，不改模型质量；消耗仍计入套餐额度（非赠送）。

**2026-10-20 实测与旧静态表的差异**（说明为什么必须实时拉取）：

| 模型 | 旧静态表 | 上游实际 |
| --- | --- | --- |
| DeepSeek-Flash (`dfmodel`) | 0.10 | **0.00**（`is_free`，原价 0.10） |
| MiniMax (`mmodel`) | 0.20 / MiniMax-M2.7 | **0.10** / MiniMax-M3.1-Flash |
| Qwen3.8-Max (`qmodel_38max`) | 0.50 | 窗口内 0.20 |

**不要把倍率写进 `openai_endpoints.pricing`**：那一列是「每百万 token 的**货币**单价」，
`price_factor` 是相对倍数、不含货币，硬写会让网关算出假金额。

### 3.5 区域（`region.go`）

Qoder 分国内版与国际版，两套域名、client_id、登录参数完全独立：

| 用途 | 国内版 (cn，默认) | 国际版 (intl) |
| --- | --- | --- |
| 官网 | `https://qoder.cn` | `https://qoder.com` |
| 登录引导 | `https://qoder.cn/device/selectAccounts` | `https://qoder.com/device/selectAccounts` |
| client_id | `732aef47-9cf2-46a2-95fe-4cebb5d0d1fa` | `e883ade2-e6e3-4d6d-adf7-f92ceff5fdcb` |
| nonce | 带横线 UUID | 32 位 hex |
| openapi | `https://openapi.qoder.com.cn` | `https://openapi.qoder.sh` |
| 对话网关 | `https://gateway.qoder.com.cn` | `https://api3.qoder.sh` |
| UA | `QoderWork/1.1.64` | `Qoder/1.1.64` |

`Account.Region` 决定该账号走哪一套；空值按国内版处理。所有 URL 经
`openAPIBaseFor(region)` / `loginBaseFor(region)` / `gatewayFor(region)` /
`chatStreamURLFor(region)` 派生。

**签到差异（关键）**：国内版活动列表**不需要真实机器身份**，多账号可各自领取；
国际版服务端按 `Cosy-Machine*` 真实身份过滤设备定向活动，缺身份时每日行被静默隐藏
（实测：无机器头只返回 VIEW_DETAILS 行；带真实 runtime-info 身份才出现每日行）。
因此本插件国内版开箱可用，国际版签到在未接入真实机器身份前可能只返回详情类活动。

## 4. 每日签到 / 余额检测（对齐 workbuddy）

- 调度：`scheduler.go` 用 `cron.New(cron.WithLocation(站点时区))` + 每分钟 TZ watcher 重建，
  签到时刻 `5 9,21 * * *`（站点时区 9 点与 21 点各一次），遵循 CONTEXT.md 时区硬规则。
- 开关：`Settings.AutoCheckin`（`*bool`，缺省视为开启，显式 false 才关闭）。
- 每次签到成功后回写账号 `LastCheckinAt` 与 `Credits`（余额），失败原因写回 `LastError`。
- 账号范围：全部「未停用」账号；`disabled` 同时摘出转发与签到（与 workbuddy 语义一致）。
- 前端账号表「签到」列显示最近签到时刻，「积分」列 Popover 展开额度明细（剩余/已用/总额/账号类型）。

## 5. 管理面接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET/PUT | `/api/qoder/settings` | 插件设置（账号已脱敏） |
| GET | `/api/qoder/status` | 运行状态（可用账号、冷却数、模型数） |
| GET | `/api/qoder/models` | 模型目录与启停状态 |
| POST | `/api/qoder/models/toggle/{id}`、`/models/toggle-batch` | 单/批量启停 |
| POST | `/api/qoder/checkin` | 全部账号立即签到 |
| POST | `/api/qoder/login/start`、`/login/poll` | 设备登录发起与轮询 |
| GET/POST | `/api/qoder/accounts`、`/accounts/{id}/...` | 账号列表、toggle、test、checkin、quota、编辑、删除 |
| GET/POST | `/api/qoder/accounts/export`、`/import` | 账号导入导出（强制真实会话） |
| GET/POST/DELETE | `/api/qoder/link` | 网关端点接入/状态/断开 |
| POST | `/api/qoder/v1/chat/completions`、`GET /v1/models` | 中继面（仅本机回环） |

## 6. 验证

- 单测 `qoder_test.go`：加密 SSE 信封解析（含直接 chunk 兼容）、模型 key 归一化、
  自定义字母表变换、过期时间归一化、选号策略归一化、签到领取与幂等、余额解析、token 刷新、
  区域归一化 / 域名自证 / 区域配置；动态目录解析（`parseUpstreamCatalog`，display_name
  作 id / key 作短 key / 实时 price_factor 优先 / 免费 0 值保留 / 静态兜底回填）、
  签名 GET 目录（`fetchUpstreamCatalog`）、回源失败回退静态兜底、停用白名单语义与改名后的
  别名闭包归一；倍率裁决（`MultiplierAt`：上游已折价 / 未折价 / 促销结束 / 无实时值）、
  促销窗口按时区与跨午夜判定、错峰状态聚合。
- 接线自检：`npm run governance:check`、`npm run ui:governance`、`node tools/three-side-governance-check.mjs`。
- 上线前建议：真实账号跑通「登录 → 中继转发 → 签到 → 余额」全链路。
