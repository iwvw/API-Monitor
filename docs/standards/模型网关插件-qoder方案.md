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
├── models.go     静态模型目录、逐模型启停
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

### 3.4 模型目录（`models.go`）

Qoder 无公开动态模型目录接口（`/algo/api/v2/model/list` 需 COSY 签名），目录以
**按区域各自维护的静态表**为准，取自官方客户端目录缓存 `catalog-v6`
（HKDF-SHA256(uid) + AES-256-GCM 解密）实测记录，含 `price_factor` 倍率：

- 国内版 14 个：`auto`/`qmodel_38max`(Qwen3.8-Max)/`qfmodel`(Qwen3.8-Flash)/
  `qmodel_latest`(Qwen3.7-Max)/`qmodel`(Qwen3.7-Plus)/`q37fmodel`(Qwen3.7-Flash)/
  `dmodel`(DeepSeek-V4-Pro)/`dfmodel`(DeepSeek-Flash)/`gmodel`(GLM-5.3)/
  `gfmodel`(GLM-5.3-Flash)/`gm51model`(GLM-5.2)/`kmodel_latest`(Kimi-K3)/
  `kmodel`(Kimi-K2.8-Preview)/`mmodel`(MiniMax-M2.7)。
- 国际版 17 个：额外有 `ultimate`/`performance`/`efficient`/`smodel`(Sonus)/
  `cmodel`(Cantus)，无 `gm51model`/`q37fmodel`。

参考 workbuddy 的设计：`mergeCatalogs` 把两区域目录合并成一份对外视图（按 id 去重，
国内版打底、国际版独有 id 追加），每个模型标记型号级可用区域 `regions` 与 id 级
`idRegions`，仅国际版提供的标 `internationalOnly`。

**模型列表按实际账号区域过滤**（`catalog`）：只存在国内版账号时仅返回国内版模型，
只有国际版账号时仅返回国际版模型，两区域都有才返回合并视图（带区域标记）；
无账号时默认国内版。因此前端模型表不会把两区域盲目混在一起。

前端模型表：第一行显示模型名，第二行显示模型标识（id）；按 `regions` 显示
「通用/国内/国际」徽标，并展示 `price_factor` 倍率列。选号按 `accountServesModel`
过滤：账号所属区域不提供目标模型时跳过。模型 key 直接透传给上游
`model_config.key` / `x-model-key`。

### 3.6 错峰折扣（`offpeak.go`）

官方错峰规则（`docs.qoder.com/events/offpeakrate`）：错峰窗口以 **UTC** 为准，
每日 14:00–00:00 UTC（本地北京时间 22:00–08:00）；常规时段 00:00–14:00 UTC。
仅两个模型享折扣：

| 模型 | 标准倍率 | 错峰倍率 |
| --- | --- | --- |
| Qwen3.8-Max (`qmodel_38max`) | 0.5x | 0.2x（6 折） |
| Qwen3.7-Plus (`qmodel`) | 0.1x | 0.04x（6 折） |

`effectiveMultiplier` 按当前时刻计算实际生效倍率，`/api/qoder/models` 下发
`effectiveMultiplier` / `offPeak` / `hasOffPeak` 字段；前端倍率列显示生效倍率，
折扣中额外标「低峰」徽标。其余模型全程标准倍率。注意：错峰只改倍率，不改模型质量；
消耗仍计入套餐额度（非赠送）。

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
  区域归一化 / 域名自证 / 区域配置。
- 接线自检：`npm run governance:check`、`npm run ui:governance`、`node tools/three-side-governance-check.mjs`。
- 上线前建议：真实账号跑通「登录 → 中继转发 → 签到 → 余额」全链路。
