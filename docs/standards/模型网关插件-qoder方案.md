# 模型网关插件 - Qoder 方案

最后更新：2026-11-15

本文档记录 Qoder 插件（`qoder.cn` / `qoder.com` → OpenAI 兼容 API）的上游协议事实、实现结构与运维要点。
插件把 Qoder 设备登录账号封装成 OpenAI 兼容上游，接入模型网关端点列表，并支持每日自动签到（含国际版）、
连签统计、token 主动续期与真实设备指纹配置。

## 1. 定位与来源

- 上游：Qoder，**分国内版与国际版两套独立区域**（见 §3.5）。
- 协议来源：开源反代 **qoder2api**（`Zhengyuuuui/qoder2api`，本次改造主要依据）、
  `jyao0708/qoder2api`、**qoder2api-worker**（`nostalgia296/qoder2api-worker`，国内版）与
  **qoder2api-hub**（`shuishuipingan/qoder2api-hub`，双区域）记录的设备登录、
  加密 SSE 与控制面接口。
- 实现：`backend-go/internal/qoder/`，前端 `src/js/pages/modelhub/plugins/QoderPlugin.jsx`。
- 与同类插件（lobsterai/workbuddy）目录结构、表命名、link 语义保持一致。

## 2. 目录结构

```
backend-go/internal/qoder/
├── service.go           Settings/Account、建表、选号、调用计数、ServeHTTP 入口、站点时区
├── identity.go          设备指纹与稳定设备身份（派生 / 真实指纹 → 新旧推理协议）
├── upstream.go          设备登录、身份加密与签名、加密 SSE 对话、auth JSON 解析、token 刷新
├── auth.go              账号脱敏、CRUD、选号与失败冷却、导入导出、签到/余额入口、token 续期
├── models.go            模型目录对外视图、逐模型启停、停用名单别名归一
├── catalog.go           模型目录动态发现（签名 GET model/list + 按区域缓存 + 实时倍率解析）
├── offpeak.go           倍率裁决与错峰促销规则（上游 promotion 驱动 + 静态兜底）
├── thinking.go          思考能力解析（thinking_config → 档位/可关闭）与强度归一
├── params.go            OpenAI 请求参数映射（reasoning_effort / context_window）与上下文校验
├── relay.go             中继面：请求转发、换号重试、流式透传、token 后台续期
├── billing.go           余额查询与每日签到（openapi 控制面，双区域）
├── checkin_helpers.go   签到纯逻辑：请求头、活动筛选、窗口日期、连签统计、时刻规范化
├── checkin_history.go   签到历史持久化（SQLite）与日期工具
├── link.go              网关端点接入（接入/断开/状态）
├── usage.go             用量统计（站点时区 × 账号 × 模型）
└── scheduler.go         每日签到调度（站点时区 cron + TZ watcher + 错峰 + 漏签重试）
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

**⚠️ 业务错误藏在 HTTP 200 的 SSE 信封里（关键坑）**

上游出错时不改 HTTP 状态码，而是在 SSE 首行返回：

```
data:{"headers":{"Content-Type":["application/json"]},
     "body":"{\"code\":\"110\",\"message\":\"Billing daily count exceeded\"}",
     "statusCodeValue":403,"statusCode":"FORBIDDEN"}
```

即 **HTTP 200 + `statusCodeValue: 403` + 内层业务码**，并且**不会再有 `[DONE]`**。
旧实现只解析 `body` 里的 `choices`，这类载荷因没有 `choices` 被当空 chunk 丢弃，
于是流一直等不到结束标记 —— 客户端表现为**超时**（实测干等 120s），
把「当日额度用尽」误报成「网络超时/限流」。

`parseLegacyEnvelope` + `envelopeError` 现在会识别并立即中止（实测 860ms 返回）：

| 判据 | 说明 |
| --- | --- |
| `statusCodeValue >= 400` | HTTP 层状态 |
| `statusCode` ∈ FORBIDDEN / UNAUTHORIZED / TOO_MANY_REQUESTS | 语义状态 |
| `body` 内层 `code` 非 `0`/`200` | 业务码（110 = 当日额度用尽） |

注意 `body` 内层若带 `choices` 则一定不是错误，不会误判。

因为这时 200 响应头与部分数据可能已写出，`relay.go` 无法改状态码，
改用 `writeStreamError` 补一条 `data: {"error":...}` + `[DONE]`，
并对额度类错误追加「该账号额度已用尽，可换用其它账号或等待额度重置」。

### 3.2.1 池级限流与恢复时刻（`poollimit.go`）

**code=110 是账号池级限制，换号无效。** 2026-10-20 实测：

- 三个账号积分分别 1700 / 1600 / 1600，`quota/usage` 里 `isQuotaExceeded=false`；
- 但三个账号 × 2 个模型 = **6/6 全部返回 code=110**。

所以「Billing daily count exceeded」不是积分耗尽，而是**每日调用次数**上限，且按账号池判定。

**换号重试仍保留**（万一将来是单账号限流），但加了两层优化：

1. **预读首事件**（`peekFirstEvent`）：上游把错误塞在 HTTP 200 的 SSE 首行，
   响应头此时还是干净的。先读首事件判断，错误则换号（客户端完全无感），
   正常才写出响应头。`peekedStream` 把预读字节回放，保证不丢内容。
2. **池级短路**：全部账号都因额度失败 → 记录 `poolLimited` → 冷却期内直接返回 429，
   不再依次试满所有账号。实测同一请求 **869ms → 17ms（快 50 倍）**。
   任一账号成功即清除该状态。

**恢复时刻是推断值，不是上游下发。** 三个来源都不给重置时间：

| 来源 | 有无重置时间 |
| --- | --- |
| SSE 错误信封 | ❌ 仅 `body` / `headers` / `statusCode` / `statusCodeValue` 4 个字段 |
| `quota/usage` | ❌ 只有远期 `expiresAt`（253402214400000） |
| `campaigns` | ✅ 间接：`startAt`/`endAt` 日窗口 + 文案「每日 10:00（UTC+8）刷新」 |

签到窗口实测 `2026-10-19 10:00` → `2026-10-20 09:59`，正好 UTC+8 的 10:00。
但签到窗口与计费限流是两套机制，**不能确定完全一致**，因此 `inferPoolResetAt`
只把它当「下一个 UTC+8 10:00」的推断值，并在接口与 UI 上明确标注 `inferred=true`。

⚠️ **推断值必须配硬上限**（`poolLimitMaxDuration` = 30 分钟）：推断偏晚时
（例如真实重置在 09:00 而我们按 10:00 算）若直接用推断时刻做锁定，最坏会锁到
近一整天——**整个账号池对所有模型都不可用**。故 `poolLimitWindow` 取
「推断的重置时刻」与「now + 30 分钟」中的**较早者**：推断值只兜底，硬上限保证
即使推断错了也只多锁半小时，到期自动失效并重新探测（最多浪费一次换号重试）。
UI 上因此显示「最迟 X 重试」而不是「预计 X 恢复」。

**额度类错误的判定要精确**（`quotaExhausted`）：只认「billing / daily count /
daily request / quota / insufficient / credit」这几类明确信号，并**排除**
「rate limit / too many requests」。速率限制一旦被误判成「每日次数用尽」，
账号会被冷却 30 分钟（而非 60 秒），还可能把账号池拖进池级限流——代价远大于
漏判（漏判最多多试几个账号）。

**预读首事件的已知取舍**：响应头的写出时机被推迟到第一个 SSE 事件到达。正常
对话里上游几乎立即发第一个 chunk，感知不到；若上游迟迟不发首事件，客户端在拿到
响应头前会一直等待。保留预读是因为它修掉的是更严重的问题（错误藏在 200 里 →
干等 120s 超时）。

账号级冷却：额度类错误设 30 分钟（`quotaCooldownDuration`），其余 60 秒；
`AccountView.cooldownSeconds` 下发剩余秒数供前端展示。

### 3.3 余额与签到（`billing.go` / `checkin_helpers.go` / `checkin_history.go`）
控制面接口**仅需** `Bearer {deviceToken}` + `Cosy-ClientType: 10`，无需 COSY 签名。

| 用途 | 方法与路径 | 关键语义 |
| --- | --- | --- |
| 余额 | `GET {openapi}/api/v2/quota/usage` | `userQuota.{total,used,remaining,unit}`；`addOnQuota` 收签到额度；`dedicatedResourcePackages` 是活动奖励包（SOTA 专属积分）；`isQuotaExceeded`/`limitExceeded` 判耗尽 |
| 活动列表 | `GET {openapi}/sash/api/v1/me/campaigns` | 找 `actionType=CLAIM_BENEFIT` 且 `claimStatus=CLAIMABLE` 的行；`startAt`/`endAt` 是日窗口 |
| 领取 | `POST {openapi}/sash/api/v1/me/campaigns/{campaignId}/claim` | **空 body**（无 `{}`）；成功 `status=CLAIMED` + `grantId`，重复 `replayed=true` |

- 每日 100 Credits 活动：领取窗口每天 10:00（次日 09:59 前）开放，每账号每窗口一次。
- 幂等：已领取返回 `CLAIMED`/`replayed=true`，按 `already` 处理而非失败。
- 刷新 token：`POST {openapi}/api/v1/deviceToken/refresh`，body `{refresh_token}`，
  返回 `{token, refresh_token, expires_in}`。
- **不要走 `daily-check-in/claim`**：国内已 `DISABLED` 却对未领取日恒返回 409（会被误判成
  「已领取」而漏领积分），国际版直接 404。`campaigns` 是唯一权威领取路径。
- `daily-check-in/status` 只作**只读统计**参考：国内 streak 恒 0、国际版 404，
  所以连签天数以本地记账为准（见 §4）。
- 专属资源包**不并入**通用额度合计：SOTA 积分通常限 Sonus 使用，混进通用余额会算出假可用额度。

**国际版签到必须携带真实设备指纹（关键）**：社区 A/B 实验（qoder2api issue #4）证明
缺少 `cosy-machinetoken` + `cosy-machinetype` 时服务端**不下发** `CLAIM_BENEFIT` 活动，
表现为「暂无可领取权益」（`no_campaign`）；补上后活动立即出现、移除任一字段又消失。
自造指纹无效（服务端校验签发来源），必须用官方客户端的真实指纹。

| 区域 | 指纹要求 |
| --- | --- |
| `cn`（`openapi.qoder.com.cn`） | **不要发**机器头。实测发自造指纹反而拿不到活动 |
| `intl`（`openapi.qoder.sh`） | **首次领取前必须携带**真实指纹 |

**懒下发机制**：国际版账号在官方客户端**首次领取一次后**，服务端转为「懒下发」——
之后即使不带指纹，`campaigns` 也能查到该活动。

**判定顺序必须「先查询，再判断指纹」（2026-11-15 真实账号实测校准）**：
本机国际版账号不带指纹调用 `campaigns` 依然返回了
`CLAIM_BENEFIT / CLAIMED`（100 Credits，窗口当天→次日），说明该账号已走过官方客户端、
处于懒下发状态。因此**不能**在「国际版 + 无指纹」时直接返回 `need_fingerprint`——
那会把「今天已领过」误报成「缺指纹」。正确顺序：

1. 先请求 `campaigns`（无论有无指纹）；
2. 有 `CLAIMABLE` → 领取；
3. 有 `CLAIM_BENEFIT` 但都是 `CLAIMED` → `already`（并补记窗口日期）；
4. 完全没有 `CLAIM_BENEFIT` 且国际版无指纹 → `need_fingerprint`（这才是真缺指纹）；
5. 其余 → `none`。

`checkinExtraHeaders` 保证未配置真实指纹的账号**不发**机器头（国内版永远如此），
只有配了真实指纹的账号带机器头。

**怎么拿到真实指纹（本机可提取，已验证）**：Qoder 桌面端把设备指纹交给阿里 umid
组件生成，组件随客户端一起装在本机，直接运行即可，无需抓包：

- `%USERPROFILE%\.qoder\.bin\umid-*\runtime-info.exe`（国际版）/
  `%USERPROFILE%\.qoder-cn\.bin\umid-*\runtime-info.exe`（国内版）
  → 输出 `{"machineToken":"…","machineType":"…","machineCode":"…"}`
- `machineId`：`%APPDATA%\com.qoder.app.stable\auth.machine-id`（国际版）/
  `%APPDATA%\com.qodercn.app.stable\auth.machine-id`（国内版）
- `machineOs`：`${arch}_win32`，如 `x86_64_win32`（客户端同款拼法）
- `hostname`：本机主机名；`version`：该区域客户端版本（实测国际 0.4.3 / 国内 0.4.8）

封装成脚本：`pwsh -File tools/qoder-fingerprint.ps1 -Pretty`（`-Region cn` 取国内版），
输出可直接粘进账号编辑弹窗的指纹输入框。实测本机两套客户端输出的 machineToken 相同，
machineId 不同。

> **指纹与设备绑定**：换机器 / 重装系统后指纹会变，等于上游眼中的新设备。
> `cosy-version` 也按区域真实客户端版本发送（不是固定值），指纹里显式给了 `version`
> 时以指纹为准。

> ⚠️ **签到请求也必须走账号代理出口**：本插件统一经 `httpClientFor(acc.ID)` 出网，
> 与对话共用同一出口，避免「对话走代理、签到直连」的双出口画像。

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
悬浮说明含标准价、折后价、窗口时区与来源；免费模型显示「免费」），以及
上下文档位、思考档位两列（悬浮展开全部可选值），模型名旁标「NEW」「视觉」徽标。
选号按 `accountServesModel` 过滤：账号所属区域不提供目标模型时跳过
（官方名与短 key 均可命中）。

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

**不要把倍率写进 `modelhub_endpoints.pricing`**：那一列是「每百万 token 的**货币**单价」，
`price_factor` 是相对倍数、不含货币，硬写会让网关算出假金额。

### 3.6 思考强度与上下文（`thinking.go` / `params.go`）

上游目录里还有两类能力元数据，已接入并映射到 OpenAI 语义：

**思考强度（`thinking_config`）**

| 上游字段 | 语义 |
| --- | --- |
| `thinking_config.disabled` 存在 | 允许关闭思考 |
| `thinking_config.enabled.efforts` | 发布的档位（对象键），如 `{xhigh,low,medium}` |
| `thinking_config.enabled.efforts.*.is_default` | 默认档位 |
| 无 `thinking_config` | 无思考能力描述（如 `Auto`、`MiniMax`），只有 `is_reasoning` |

请求侧映射：调用方的 `reasoning_effort`（也接受 `reasoning.effort` / `effort`）
写进上游 `parameters.reasoning_effort`；开关写进 `model_config.is_reasoning`
与 `chat_context.extra.modelConfig.is_reasoning`（两处必须同步）。
`reasoning_effort: "none"` / `thinking.type: "disabled"` / `reasoning.enabled: false`
表示关闭——仅当上游 `disabled` 存在时真的关闭，否则保持默认（不假装关掉）。

**档位归一向上取**：模型只发布 `high/max` 时请求 `low` → 取 `high`；请求 `xhigh`
（Qwen 系）→ 截到该模型最高档。为什么向上：静默降级成「不开思考」会明显损失质量。

**调用方没指定档位时不猜默认档**：上游用 `is_default` 标了默认档就照用；没标就
**不写任何 `reasoning_effort`**，交给上游用自己的默认。猜最高档会平白拉高思考
开销与延迟（属于行为漂移），因此 `DefaultEffort` 在上游未标记时保持为空。

2026-10-20 实测各模型档位：

| 模型 | 档位 | 默认 |
| --- | --- | --- |
| Qwen3.8-Max / Qwen3.8-Flash | low / medium / xhigh | medium |
| Qwen3.7-Max / Qwen3.7-Plus | 仅开关（无档位） | — |
| DeepSeek-V4-Pro / GLM-5.2 / GLM-5.3-Flash | high / max | max |
| DeepSeek-Flash / GLM-5.3 / Kimi-K2.8-Preview / Kimi-K3 | low / high / max | max |
| Auto / MiniMax-M3.1-Flash / Qwen3.7-Flash | 无 `thinking_config` | — |

**上下文档位（`context_config`）**

上游发布 `{"1M":{"token_count":1000000},"200K":{"token_count":200000,"is_default":true}}`，
键名（1M/200K）不可靠，以 `token_count` 为准；解析后升序存入
`ModelInfo.ContextWindows`。转发前估算输入 token，超限直接返回 400
`context_length_exceeded`，不再绕一圈等上游报错（换号重试还会白耗额度）。
请求可用 `context_window` / `max_input_tokens` 指定较小档位；未指定时按该模型
最大档位判。

⚠️ **估算必须用纯字符启发式，不要用 tiktoken**：实测 `go-tiktoken` 在本项目
调用方式下耗时超线性——20000 字符 350ms、40000 字符 1.8s，一条普通对话就会让
每次请求多等一秒以上（曾用 `CountPromptTokens` 实现，5MB 请求直接挂死到超时）。
现在用 `util.EstimateTokens`（ASCII 约 4 字符/token、CJK 约 1.3），5MB 请求
122ms 内返回 400。`qoder_test.go` 的 `TestUsageTokensForCheckIsFast` 守住这条约束。

⚠️ **不要用 `max_input_tokens` 判上限**：上游这个字段自相矛盾（`Qwen3.8-Max` 给
180000，`context_config` 却列出 1M 档），仅在无 `context_config` 时作兜底。

**其余能力字段**（`is_vl` / `is_new` / `is_default` / `icon` / `is_reasoning`）已随
`/api/qoder/models` 下发，前端据此打「视觉」「NEW」徽标。`is_default` 当前只有
`Auto` 为 true。

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
| cosy-version（新版协议） | `0.4.8` | `0.4.3` |

`Account.Region` 决定该账号走哪一套；空值按国内版处理。所有 URL 经
`openAPIBaseFor(region)` / `loginBaseFor(region)` / `gatewayFor(region)` /
`chatStreamURLFor(region)` 派生。

**设备身份与「新旧推理协议」（`identity.go`）**

上游按 `(cosy-clienttype, cosy-version)` 决定下发哪套模型清单：

| 协议 | 触发条件 | 头 | 模型清单 |
| --- | --- | --- | --- |
| 旧 | 账号未配置真实指纹 | `clienttype=5` / `version=0.1.43`（签名会话）/ UA `Go-http-client/2.0` | 国内 12 / 国际 13 |
| 新 | 账号配置了真实指纹 | `clienttype=10` / `version=<区域客户端版本>` / UA `undici` + 真实 machine* + `cosy-business-product: app` / `cosy-scene: app` / `cosy-data-policy: agree` | 国际额外多 Sonus(`smodel`) / Cantus(`cmodel`) / Fast(`fast`) |

`cosy-version` **不是固定值**：实测（2026-11-15 本机客户端日志）国际版发 `0.4.3`、
国内版发 `0.4.8`，都是 `clienttype=10`。因此 `regionConfig.clientVersion` 按区域记录
真实客户端版本，指纹里显式给了 `version` 时优先用指纹里的值。

- **未配置指纹时按账号稳定派生**（`deriveMachineID` = md5 UUIDv4 形态、
  `deriveMachineType` = 18 位 hex、`deriveMachineToken` = sha512 base64url 43 位），
  同一账号跨请求、跨重启结果一致。**不要随机化**：随机机器头等于「同一账号在多台设备上活动」，
  是最容易被关联的特征之一。
- 派生指纹只能用于**旧协议**：自造指纹在推理侧会被上游拒绝（`RESOURCE_LIMIT`），
  国际版签到的活动也不下发。只有真实指纹才切新版协议。
- 签到与对话/目录**共用同一份身份**：`identityForAccount` 是唯一入口，
  避免「签到一套设备、推理另一套设备」。

**签到区域差异（关键）**：国内版活动列表**不需要真实机器身份**，且**不能**发自造指纹
（发了反而拿不到活动）；国际版服务端按 `Cosy-Machine*` 真实身份过滤设备定向活动，
缺身份时每日行被静默隐藏（实测：无机器头只返回 VIEW_DETAILS 行；带真实身份才出现每日行）。
实现见 §3.3。

## 4. 每日签到 / 余额检测（对齐 workbuddy）

- 调度：`scheduler.go` 用 `cron.New(cron.WithLocation(站点时区))` + 每分钟 TZ watcher，
  站点时区或签到时刻变化时重建调度器并重新挂载任务，遵循 CONTEXT.md 时区硬规则。
- 时刻可配：`Settings.AutoCheckinTimes`（`HH:MM` 列表，留空默认 `["10:00"]`），每个时刻一条
  cron 表达式；设置页保存后即时生效（watcher 检测到 spec 变化即重建）。
- 开关：`Settings.AutoCheckin`（`*bool`，缺省视为开启，显式 false 才关闭）。
- **账号错峰（防风控）**：签到是非对话类高频行为，多账号同秒批量签到是很明显的自动化特征，
  会导致活动被上游隐藏（账号看不到权益）。因此：
  - 账号间强制最小间隔 `CheckinGapMS`（默认 1000ms）+ 随机抖动 `CheckinJitterMS`（默认 500ms，
    用 `crypto/rand`，伪随机序列可被重建等于没有抖动）；
  - 每个账号在时刻点附近有 `[-window, +window]` 的固定偏移（`CheckinStaggerS` 默认 60s），
    偏移由账号 ID 的 sha256 稳定派生——**必须跨天稳定**，否则同一账号的行为画像逐日漂移。
- **漏签重试**：签到时段触发后若出现 `none`（活动尚未下发）或请求失败，不标记当日完成，
  由 `retryPendingSlots` 每分钟补跑直到该时段 + 1 小时；`need_fingerprint` 属配置问题，
  不占用重试额度。
- 每次签到成功后回写账号 `LastCheckinAt`、`Credits`（余额）与本地连签统计；失败原因写回 `LastError`。
  只有 `success` / `already` 才更新「最近签到时刻」，`none` / `need_fingerprint` 不算签到成功。
- 账号范围：全部「未停用」账号；`disabled` 同时摘出转发与签到（与 workbuddy 语义一致）。
- **连续签到的记账规则**（`checkin_history.go`）：窗口是 10:00 → 次日 09:59（UTC+8），
  按活动 `startAt` 的**窗口日期**记账而非自然日，避免 10:00 前点击把当天误记为已签、连签虚高。
  上游 legacy 统计恒 0/404，故连签天数、累计天数、累计积分全部本地计算（表 `qoder_checkin_history`）。
  「今天未领」时从昨天往前数，当天未领不算断链。
- 前端账号表：「签到」列显示最近签到时刻，「连签」列显示连续/累计天数，「积分」Popover 展开额度明细
  （剩余/已用/总额/账号类型 + 专属资源包），token 列额外显示「指纹 / 缺指纹」「需重授权」。

## 5. Token 续期与账号功能（`relay.go` / `auth.go`）

Qoder 的 access token 与 refresh token 都有有效期，双 token 同时过期后账号会静默失效
（额度显示为空、请求 401）。插件用 `deviceToken/refresh` 在过期前主动续期：

- 后台：`StartAutoRefresh` 每 5 分钟扫一次，账号 token 剩余寿命不足 `refreshWindowDays`（3 天）
  就用 `refresh_token` 换新 token 与新 refresh_token。开关 `AutoRefreshToken`（缺省开启）。
- 手动：`POST /api/qoder/accounts/{id}/refresh` 单账号续期、`POST /api/qoder/refresh` 全部账号续期，
  前端账号表每个账号有续期按钮、表头工具条有「续期 token」。
- 状态可见：账号视图下发 `tokenAgeDays`（由 `tokenIssuedAt` 计算，无签发时刻为 -1）、
  `lastRefreshAt`、`needsReauth`（无 token，或 token 已过期且无 refresh token）。
- 重新授权保留指纹：`finishLogin` 在账号已存在时**保留原有的 `checkinFingerprint`**。
  同一账号重新授权 = 同一台设备，抹掉指纹会让上游当成换设备，国际版签到失效、
  Sonus/Cantus/Fast 三档模型从清单消失。

## 6. 管理面接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET/PUT | `/api/qoder/settings` | 插件设置（账号已脱敏） |
| GET | `/api/qoder/status` | 运行状态（可用账号、冷却数、模型数） |
| GET | `/api/qoder/models` | 模型目录与启停状态 |
| POST | `/api/qoder/models/toggle/{id}`、`/models/toggle-batch` | 单/批量启停 |
| POST | `/api/qoder/checkin` | 全部账号立即签到 |
| POST | `/api/qoder/refresh` | 全部账号立即续期 token |
| POST | `/api/qoder/login/start`、`/login/poll` | 设备登录发起与轮询 |
| GET | `/api/qoder/accounts` | 账号列表（脱敏视图，含连签统计与指纹状态） |
| PUT/DELETE | `/api/qoder/accounts/{id}` | 编辑（备注名 / 区域 / 设备指纹）与删除 |
| POST | `/api/qoder/accounts/{id}/toggle`、`/test`、`/checkin`、`/refresh` | 启停、连通性测试、单账号签到、单账号续期 |
| GET | `/api/qoder/accounts/{id}/quota`、`/checkin-history` | 额度明细、签到历史（最近 60 条 + 统计） |
| GET/POST | `/api/qoder/accounts/export`、`/import` | 账号导入导出（强制真实会话） |
| GET/POST/DELETE | `/api/qoder/link` | 网关端点接入/状态/断开 |
| POST | `/api/qoder/v1/chat/completions`、`GET /v1/models` | 中继面（仅本机回环） |

**账号编辑的指纹语义**：`checkinFingerprint` 字段缺省表示不修改；`clearFingerprint: true`
显式清空（JS 端很难区分「缺省」与「显式 null」，因此用独立布尔，避免误清）。
下发视图只给 `fingerprint: true/false`，**不回显指纹内容**。

## 7. 验证

- 单测 `qoder_test.go`：加密 SSE 信封解析（含直接 chunk 兼容）、模型 key 归一化、
  自定义字母表变换、过期时间归一化、选号策略归一化、签到领取与幂等、余额解析、token 刷新、
  区域归一化 / 域名自证 / 区域配置；动态目录解析（`parseUpstreamCatalog`，display_name
  作 id / key 作短 key / 实时 price_factor 优先 / 免费 0 值保留 / 静态兜底回填）、
  签名 GET 目录（`fetchUpstreamCatalog`）、回源失败回退静态兜底、停用白名单语义与改名后的
  别名闭包归一；倍率裁决（`MultiplierAt`：上游已折价 / 未折价 / 促销结束 / 无实时值）、
  促销窗口按时区与跨午夜判定、错峰状态聚合；思考能力解析（三形态）、档位归一向上取、
  请求参数映射（`reasoning_effort` 各种写法 / 关闭思考 / 输出上限）、上下文超限拦截。
- 单测 `checkin_helpers_test.go`：设备身份稳定性与派生形态（UUID / 18 位 machinetype /
  43 字符 machinetoken）、真实指纹切换新版协议（版本优先取区域客户端版本、指纹显式值优先）、
  签到请求头按区域差异（CN 不发机器头 / INT 缺指纹不发机器头 / 带指纹发全量头）、
  `need_fingerprint` 明确返回、懒下发（CLAIMED 无指纹报 already 而非缺指纹）、
  连签统计（含今天未领不断链）、历史幂等与 0 值不覆盖、签到时刻规范化、cron 表达式生成、
  错峰偏移稳定且有界、窗口日期提示、`dedicatedResourcePackages` 解析与不并入合计、`needsReauth`。
- **真实账号实测**（`live_probe_test.go`，默认跳过，需 `QODER_LIVE_TOKEN` 等环境变量）：
  2026-11-15 用本机国内版与国际版账号验证：
  - 国内版**不带指纹**即可拿到 `CLAIM_BENEFIT / CLAIMABLE`（100 Credits，窗口当天→次日）；
  - 国际版**不带指纹**也能拿到 `CLAIM_BENEFIT`，但状态是 `CLAIMED`（懒下发，官方客户端当天已领），
    因此必须按「先查询再判断指纹」的顺序，否则会误报缺指纹；
  - 余额接口 `quota/usage` 解析正常（`remaining=100 limit=100 unit=credits`），
    该账号无 `dedicatedResourcePackages`（解析器对空数组返回空列表）。
- 指纹提取脚本实测：`pwsh -File tools/qoder-fingerprint.ps1` 在本机输出
  `machineToken/machineType/machineCode` 与 `machine_id`，两套客户端 machineToken 一致、
  machineId 不同。
- 接线自检：`npm run governance:check`、`npm run ui:governance`、`node tools/three-side-governance-check.mjs`。
- 上线前建议：真实账号跑通「登录 → 中继转发 → 签到 → 余额」全链路；国际版另需验证
  「配置指纹后活动出现」与「Sonus/Cantus/Fast 进入模型清单」。
