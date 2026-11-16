# 邮箱验证码收件箱(Cloudflare Email Worker)

把发往你自有域名(已接入 Cloudflare Email Routing)的邮件解析出验证码与验证链接,
投递到 API Monitor 面板的通用收件箱,供自动登录、账号注册等消费者直接取用。

## 部署方式

有两种方式,任选其一:

### 方式一:面板一键部署(推荐)

在「DNS → 邮件」Tab 选择域名,点「部署收件箱 Worker」。面板会自动:
上传 Worker 脚本、注入回调地址与握手密钥、把该域名的 catch-all 指向 Worker,
并保留原转发目标(Worker 内部通过 `message.forward` 续转,邮件流不中断)。
列表会显示部署状态,可一键卸载(恢复原 catch-all 并删除 Worker)。

### 方式二:手动 wrangler 部署

```bash
cd docs/email-worker
npx wrangler login
npx wrangler secret put PANEL_BASE_URL      # 例如 https://api.example.com
npx wrangler secret put PANEL_WORKER_SECRET  # 与面板共享密钥一致
npx wrangler deploy
```

`wrangler deploy` 成功后,在 Cloudflare 后台 → Email → Routing 中把目标域名的
**catch-all**(或具体地址)动作设为 **Send to Worker** 并选择该 Worker。
注意规则只能指向已存在的 Worker,否则会以 `Workers Script Info not found` 失败。

## 面板侧密钥

面板回调校验用共享密钥,来源二选一:

- 环境变量 `EMAIL_INBOX_WORKER_SECRET`;
- 未配置时由加密根密钥派生(`secure.DeriveSecret("email-inbox")`)。

面板一键部署会自动使用同一来源注入 Worker,无需手工对齐。
校验走 `X-Worker-Secret` 头,常量时间比较;密钥不符直接 401,绝不接受伪造验证码。

## 面板收件箱接口

除 `ingest` 外全部为**会话鉴权**(浏览器登录后的 `sid` Cookie;Agent 走 MCP,见下)。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/emailcode/ingest` | Worker 投递入口(公开,`X-Worker-Secret` 校验) |
| GET | `/api/emailcode/domains` | 已部署收件箱、可用来收信的域名列表 |
| GET | `/api/emailcode/messages` | 列表;参数 `mailbox`、`domain`、`fromDomain`(支持后缀匹配)、`includeConsumed=1`、`limit`(≤200) |
| GET | `/api/emailcode/messages/{id}` | 单封详情(**含 `raw` MIME 原文**) |
| DELETE | `/api/emailcode/messages/{id}` | 删除单封 |
| POST | `/api/emailcode/messages/{id}/consume` | 标记单封已消费 |
| POST | `/api/emailcode/messages/batch` | **批量列表**:`{mailboxes:[...], domain?, fromDomain?, includeConsumed?, limit?}`,最多 50 个收件人 |
| POST | `/api/emailcode/messages/fetch` | **批量取详情**:`{ids:[...]}`,最多 50,返回含 `raw` 原文 |
| POST | `/api/emailcode/messages/consume` | **批量消费**:`{ids:[...], consumer?}`,最多 50 |
| POST | `/api/emailcode/wait` | 等待某收件人收到验证码(长轮询,**会消费**) |
| GET | `/api/emailcode/stats` | 近 N 天提取统计(`days`) |
| POST | `/api/emailcode/clear` | 清理(`onlyConsumed=1` 只删已消费) |

## 邮件原文与正文

每封邮件落库的字段(`Message`):

| 字段 | 含义 | 出现位置 |
| --- | --- | --- |
| `textBody` | 面板解析后的**正文纯文本**(≤64K runes) | 列表 + 详情 + 批量 |
| `code` | 启发式提取的**验证码** | 列表 + 详情 + 批量 |
| `link` | 提取到的**验证链接**(完整,不脱敏) | 列表 + 详情 + 批量 |
| `snippet` | 正文摘要 | 列表 + 详情 + 批量 |
| `subject` / `sender` / `fromDomain` / `receivedAt` | 元数据 | 列表 + 详情 + 批量 |
| `raw` | **MIME 原文**(≤256KB,按 UTF-8 边界截断) | **仅详情/批量取回**,列表不返回 |

要点:
- 原文只在 `GET /messages/{id}` 与 `POST /messages/fetch` 返回,列表为体积考虑**不带 raw**。
- Worker 侧投递原文上限 512KB;入库再截断到 256KB;`textBody` 上限 64K runes。
- 保留期默认 **7 天**,到期自动清理(含原文)。

## 批量操作(注册 / 取信)

- 收件地址可以任意:只要域名 catch-all 指向 Worker,`userN@yourdomain` 都会进收件箱;
  先 `GET /domains` 拿可用域名。
- 批量列:一次 `POST /messages/batch` 提交 ≤50 个收件人,拿到各自的最新邮件。
- 批量取原文:从列表拿到 `id` 后,`POST /messages/fetch` 一次取 ≤50 封(含 raw)。
- 批量消费:`POST /messages/consume` 一次标记 ≤50 封。
- 更大量:在 Agent 侧用 MCP 的 `run_batch`(单次 ≤20 个接口调用)分片,或循环。

## Agent / MCP 接入

Agent 不需要浏览器会话,用 **Agent Key** 走 MCP(Streamable HTTP / JSON-RPC 2.0):

- MCP 端点:`POST /api/ai/mcp`,请求头 `Authorization: Bearer <Agent Key>`。
- Agent Key / 权限模式在面板「API 文档 → AI 接入」查看与轮换;权限模式
  `minimal`(只读)/ `sensitive` / `standard`(默认,允许写入)/ `full`。
- 相关工具:
  - `call_api`:调任意内部接口(GET/POST/PUT/PATCH/DELETE + JSON body);
  - `run_batch`:一次 1–20 个接口调用(可 `serial`/`parallel`)。
  - 调用前用 `get_route` 看契约、`list_apis` / `find_api` 找接口。
- 约束:`call_api` 不能调流式/WebSocket、专用密钥鉴权路由与 AI 密钥管理路由;
  emailcode 属于普通会话接口,可正常调用(内部调用按会话等价放行)。

### 示例(curl)

**A. 会话直连**(浏览器登录后从 DevTools 复制 `sid` Cookie;或用 `POST /api/auth/login` 换 Cookie)

```bash
BASE=https://panel.example.com
COOKIE='sid=<SESSION_ID>'

# 可用收件域名
curl -s "$BASE/api/emailcode/domains" -H "Cookie: $COOKIE"

# 列表(含正文 textBody / code / link)
curl -s "$BASE/api/emailcode/messages?mailbox=user1@yourdomain.com&includeConsumed=1" -H "Cookie: $COOKIE"

# 单封详情(含 raw 原文)
curl -s "$BASE/api/emailcode/messages/123" -H "Cookie: $COOKIE"

# 批量列表(≤50 个收件人)
curl -s -X POST "$BASE/api/emailcode/messages/batch" -H "Cookie: $COOKIE" \
  -H 'Content-Type: application/json' \
  -d '{"mailboxes":["u1@yourdomain.com","u2@yourdomain.com"],"includeConsumed":true}'

# 批量取原文(≤50 个 id)
curl -s -X POST "$BASE/api/emailcode/messages/fetch" -H "Cookie: $COOKIE" \
  -H 'Content-Type: application/json' -d '{"ids":[123,124]}'

# 批量消费
curl -s -X POST "$BASE/api/emailcode/messages/consume" -H "Cookie: $COOKIE" \
  -H 'Content-Type: application/json' -d '{"ids":[123,124],"consumer":"agent"}'
```

**B. MCP(Agent Key,无需会话)**

```bash
BASE=https://panel.example.com
KEY='<AGENT_KEY>'

# 工具清单
curl -s "$BASE/api/ai/mcp" -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# call_api:批量列多个收件人
curl -s "$BASE/api/ai/mcp" -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"call_api","arguments":{
        "method":"POST","path":"/api/emailcode/messages/batch",
        "body":{"mailboxes":["u1@yourdomain.com","u2@yourdomain.com"],"includeConsumed":true}}}}'

# run_batch:并行拉多个收件人列表(单次 ≤20)
curl -s "$BASE/api/ai/mcp" -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"run_batch","arguments":{
        "mode":"parallel","operations":[
          {"name":"u1","method":"GET","path":"/api/emailcode/messages?mailbox=u1@yourdomain.com&includeConsumed=1"},
          {"name":"u2","method":"GET","path":"/api/emailcode/messages?mailbox=u2@yourdomain.com&includeConsumed=1"}
        ]}}}'
```

> 写操作(`consume`/`delete`/`clear`)在 `minimal` 模式会被拒绝;直接用 `POST` 前请确认权限模式。

## 自动链路

1. 消费者(如 PostHog 自动登录)发起流程并订阅收件箱等待验证码
2. 上游服务发验证码邮件到你的域名
3. Worker 收到邮件 → 原样搬运 MIME 原文 + 元数据 → `POST /api/emailcode/ingest`
4. 面板解析原文,提取验证码/链接/正文,入库并唤醒等待者;消费者取码、标记消费,继续完成后续步骤

## 约束

- catch-all 会接管该域名所有收件:配置了 `FORWARD_TO` 才会继续转发,
  否则邮件只进收件箱不再外发,务必确认续转目标。
- 验证码提取是启发式(6 位数字,优先匹配 code/verification 上下文,
  且绝不在 MIME 头里匹配以免误取事务 ID),非标准格式的邮件可能提取不到。
- 原文按 256KB 截断、正文按 64K runes 截断;保留期 7 天。
- 收件箱内容含敏感验证码与原文,接口按会话/Agent Key 鉴权,请定期清理。
