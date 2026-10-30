# 会话交接（HANDOFF）

> 交接时间：2026-10-30 17:30 (Asia/Shanghai)
> 交接范围：本次会话的全部工作

---

## 一、本次会话做了什么（按时间顺序）

### 阶段 1：OCC 同步层修复 → 已发布 v2.0.49 ✅

**起因**：用户报告「前端信息不刷新，只有 F5 才正常，例如对话标题、有的回复」。

**做了什么**：

| 编号 | 问题 | 处理 |
|---|---|---|
| A | agent 跑完后折叠块不自动收起 | ✅ 已修并**运行时验证** |
| B | 侧栏项目行识别不到运行状态 | ✅ 代码已改，**运行时未验证** |
| T1 | 侧栏未打开的会话丢事件后无法自愈 | ✅ 已修，机理成立但**端到端未复现** |
| T2 | `onSessionStatus` 缺 directory | ✅ 已修（同 B） |

**验证状态（重要）**：
- `tsc -b` / `eslint` / `vitest run`（2196 passed）/ `vite build` 全绿
- CI 三条全绿，Release 15 个产物上传
- **A 有真实环境证据**：`Working for 5s→12s → Worked for 12s`，未刷新
- **B/T1 只有单测覆盖，没有端到端证据**

**放弃的两个方案**（详见 `docs/design/数据同步层设计.md` §18.6）：
1. 放宽 seq 缺口丢弃条件 → 会破坏 4 条刻意设计的测试，已回滚
2. `onReconnected` cooldown 延后补发 → 写不出能区分修复与否的测试（假阳性），放弃

**产出**：
```
8eb6dd0c chore: bump version to 2.0.49
fd470aa7 docs: 记录第三批同步修复（T1–T3）与两项未采纳方案
b2bc9d0c fix(sync): 未打开的会话丢事件后不再只能靠刷新恢复
```
已推送到 `origin/main`，tag `v2.0.49` 已推。

---

### 阶段 2：Gemini 网关问题排查 → **未完成** ⚠️

**起因**：用户报告 `gemini-3.8-flash` 模型「总是中断」「卡住」「504」「502」。

**最终结论**：**问题未解决**。根因锁定但修复未落地。

**关键数据（这是本次会话最有价值的产出）**：

```
new-api 中转（同模型）：15 次测试全部 1.4–7.8 秒，reasoning=0
我们的网关（Vertex）：  2s ~ 141s，ttfb≈latency，reasoning 有值

同一个 prompt(1227 token)：一次 2.1s，一次 134s —— 差 64 倍
```

**已确证的 3 个缺陷**：

| 缺陷 | 位置 | 置信度 |
|---|---|---|
| A. `reasoning_effort=none` 必现 400 | `vertex_upstream.go:569` | 高（有对照 + 实测 400） |
| B. 直连路径无首字节超时 | `relay_loop.go:503` | 高（配置 + 代码双证） |
| C. `includeThoughts` 强制开启 | `vertex_upstream.go:569` | 中（对照成立但非唯一原因） |

**服务器改动（已生效）**：
- 文件：`/opt/1panel/www/sites/dsukhub.com/proxy/root.conf`（主机 `39cd3303`）
- 加了 `proxy_read_timeout 86400` / `proxy_send_timeout 86400` / `proxy_buffering off`
- 已备份 `root.conf.bak-20261030-164111`，`nginx -t` 通过，已 reload
- **效果：504 消失，但「卡住」没解决**

---

## 二、仓库当前状态（接手前必须核对）

### OCC (`/opt/code/OCC`)

```
分支：main
HEAD：8eb6dd0c chore: bump version to 2.0.49
工作区：干净
远端：已同步，tag v2.0.49 已推
```

**无需任何操作。**

### API-Monitor (`/opt/code/API-Monitor`)

```
分支：dev
HEAD：9e7a0bc3 docs: Gemini 网关问题排查交接
工作区：干净
状态：dev 领先 origin/dev 1 个提交（就是这份交接文档）
origin/main 在 043b26b1 (v2.1.1)，与 dev 是不同分支线
```

**未推送的 Gemini 相关提交（9 个，在 dev 上）**：

```
699df620 fix(antigravity): prompt_tokens 补回缓存命中部分，命中率不再恒为 100%
91c53503 fix(antigravity): 流式响应补 X-Accel-Buffering / 写超时延长 / SSE ping 保活
63da53f4 fix(antigravity): 读出上下文字段，用量面板缓存不再恒为 0
0ae97454 test(antigravity): 补纯 thinking 上游流空响应边界回归
88f4b8b0 fix(antigravity): 纯 thinking 收尾不再被空响应守卫误杀
61463e9e test(antigravity): 补齐「零输出但正常收尾」的边界回归
31a13d13 fix(antigravity): 上游空响应/坏工具调用不再伪装成成功，错误如实透出
c331d41c fix(agent): 补齐 OpenAI 工具调用转发链路与 PowerShell 脚本提取
aeba6bc9 feat(agent): v2 /context 轻量投影，首屏控制面大响应
```

**注意**：其中 `91c53503`（X-Accel-Buffering）我后来发现**不是 504 的真正原因**（全局本来就有 `proxy_buffering off`，错误是 `reading response header`）。改动本身无害（其它插件都有），但提交信息里的因果描述不准确。

**已被回滚的改动**：我曾改过 `vertex_upstream.go`（includeThoughts / none 分支）和 `relay_loop.go`（waitForFirst），**这两个改动现在不在代码里**。接手后若要修缺陷 A/B/C，需要重新写。

---

## 三、待办事项（按优先级）

### P0：必须用户决策后才能动

**1. `includeThoughts` 默认值**

| 选项 | 效果 | 代价 |
|---|---|---|
| 改为 `false`（对齐 new-api） | 速度稳定，1.4–7.8s | 客户端看不到 reasoning 内容 |
| 保留 `true` + 强制 `thinkingLevel: medium` | 我实测 3.0–4.4s，也可行 | 仍需每次请求带 level |

**用户尚未选择。**

### P1：确定收益，可直接做

**2. 修 `reasoning_effort=none` 的 400**

- 位置：`vertex_upstream.go:569` 附近
- 做法：`none` 时**整个删除 thinkingConfig**（抄 CLIProxyAPI `apply.go` 的 ModeNone 分支）
- 可写单测，可验证

### P2：需要先做诊断

**3. 定位 `bytes=0 + 200 + 60s` 那 17 条请求**

- 现象：日志有记录，但 `openai_gateway_analytics` 表**无对应行**
- 可能路径：middleware / relayLoop 不记账分支 / `clientCancelled` 静默 return
- 建议：在 `proxyChatCompletions` 每个 return 点加诊断日志，用户复现一次即可定位

**4. 查清旧 Vertex 凭据为什么能用**

- 数据库 `api_key = 099b73d6...:9a73...:7f7a...`
- 直连 Google 返回 401 `API keys are not supported`
- 但网关返回 200 → **请求没到 Google，或走了别的通道**
- 完全未查清

### P3：低优先级

**5. 修正 `91c53503` 的提交信息**（因果描述不准），或保留但知情

---

## 四、环境与凭据

### 主机

| 用途 | serverId | 说明 |
|---|---|---|
| 工作主机 | `3bf52f49-1713-443c-8f72-0188e40f7f76` | Windows `DESKTOP-0BSJMGE`，opencode serve :4096 |
| 网关主机 | `39cd3303-fa37-4910-91da-65c395d1639b` | Linux ARM `ubuntu`，Docker |

### 关键路径（网关主机）

```
网关容器：iwvw/api-monitor:dev  (端口 3000)
数据库：  /opt/1panel/docker/compose/api-monitor/data/data.db
网关日志：/opt/1panel/docker/compose/api-monitor/data/logs/app.log   (JSON, UTC)
反代配置：/opt/1panel/www/sites/dsukhub.com/proxy/root.conf
反代日志：/opt/1panel/www/sites/dsukhub.com/log/error.log
```

### 凭据（敏感信息脱敏）

```
我们网关：sk-am-avg5lq_***
Vertex：  AQ.***[REDACTED]***
new-api： sk-egGATbLvk***
```

### 测试脚本（本机 `/tmp/opencode/`，未进仓库）

```
newapi-test.py      new-api 中转基线
newapi-cmp.py       new-api 不同 reasoning_effort 对比
vx-local.py         直连 Vertex 认证测试
vx-cmp.py           直连 Vertex 三种 thinkingConfig 对比（核心对照实验）
thinking-probe.mjs  经我们网关测 reasoning_effort
```

---

## 五、踩过的坑（血泪教训，务必看）

### 1. 不要把「代码可疑」当成「这是根因」

本次会话我至少 5 次犯了同一个错：

| 错误结论 | 打脸证据 |
|---|---|
| 504 是缺 `X-Accel-Buffering` | 全局本来就有 `proxy_buffering off` |
| `MALFORMED_FUNCTION_CALL` 是主因 | 全天 0 次 |
| 空流导致 `200+0字节` | 那些请求在 analytics 表无记录 |
| `thinkingConfig` 缺 level 是根因 | 直连 A 组（什么都不设）也 62.6s |
| 旧复合 key 可用 | 直连 401 |

**正确做法**：做**对照实验**（同输入、不同配置/不同网关），而不是读代码猜。

### 2. 测试设计错误会伪装成「发现 bug」

- `max_tokens=100` 时 reasoning 模型把额度全用在思考上 → `finish=length` + 无正文，我一度当成「上游返回空流」
- 修正：`max_tokens=2000` 并统计 `reasoning` 字段

### 3. DOM 启发式选择器不可靠（OCC 阶段）

连续 7 轮验证失败，原因各异：
- 抓到外层容器、文本截断
- 侧栏收起时项目行不渲染
- 猜的项目名不存在
- 正则匹配整页文本 → 误命中消息正文（假阳性）

**教训**：先断言前置条件，再用「文本是否相等」这种客观判据。

### 4. 时区

网关日志是 **UTC**，用户界面是 **+8**。对齐时间必须换算，否则会查错时段。

### 5. 用户主机的 opencode 与网关是两回事

`dsukhub.com/v1` 是**模型网关**，不是 opencode 服务器。用 Playwright 把 OCC 指向它当服务器会 401。

---

## 六、用户沟通偏好

从本次会话观察：

- 要求**实证**，反复追问「确定问题在这儿吗」「你自己在这个对话用gemini」
- 不喜欢空泛结论，要具体数据
- 明确说过「就仿照 CLIProxyAPI 的成熟设计吧」——**倾向复用成熟实现，不要自创**
- 说过「你可以直接发 mermaid」
- 说过「不用，我现在切换到 gemini」——会自己动手验证

**建议**：接手后先复述你理解的问题，给出可验证的实验设计，再动手改代码。

---

## 七、一句话总结

**OCC 的同步层修复已完成并发布 v2.0.49（A 有验证，B/T1 只有单测）。Gemini 网关问题排查到根因但未修复，卡在「includeThoughts 默认值」需要用户决策。服务器 504 已消除，但「卡住」仍在。**
