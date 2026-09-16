# 邮箱验证码收件箱（Cloudflare Email Worker）

把发往你自有域名（已接入 Cloudflare Email Routing）的邮件解析出验证码与验证链接，
投递到 API Monitor 面板的通用收件箱，供自动登录、账号注册等消费者直接取用。

## 部署方式

有两种方式，任选其一：

### 方式一：面板一键部署（推荐）

在「DNS → 邮件」Tab 选择域名，点「部署收件箱 Worker」。面板会自动：
上传 Worker 脚本、注入回调地址与握手密钥、把该域名的 catch-all 指向 Worker，
并保留原转发目标（Worker 内部通过 `message.forward` 续转，邮件流不中断）。
列表会显示部署状态，可一键卸载（恢复原 catch-all 并删除 Worker）。

### 方式二：手动 wrangler 部署

```bash
cd docs/email-worker
npx wrangler login
npx wrangler secret put PANEL_BASE_URL      # 例如 https://api.example.com
npx wrangler secret put PANEL_WORKER_SECRET  # 与面板共享密钥一致
npx wrangler deploy
```

`wrangler deploy` 成功后，在 Cloudflare 后台 → Email → Routing 中把目标域名的
**catch-all**（或具体地址）动作设为 **Send to Worker** 并选择该 Worker。
注意规则只能指向已存在的 Worker，否则会以 `Workers Script Info not found` 失败。

## 面板侧密钥

面板回调校验用共享密钥，来源二选一：

- 环境变量 `EMAIL_INBOX_WORKER_SECRET`；
- 未配置时由加密根密钥派生（`secure.DeriveSecret("email-inbox")`）。

面板一键部署会自动使用同一来源注入 Worker，无需手工对齐。
校验走 `X-Worker-Secret` 头，常量时间比较；密钥不符直接 401，绝不接受伪造验证码。

## 面板收件箱接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/emailcode/ingest` | Worker 投递入口（公开，密钥校验） |
| GET | `/api/emailcode/messages` | 列表（`mailbox`/`includeConsumed`/`limit`） |
| POST | `/api/emailcode/wait` | 等待某收件人收到验证码（长轮询） |
| POST | `/api/emailcode/messages/{id}/consume` | 标记已消费 |
| DELETE | `/api/emailcode/messages/{id}` | 删除单条 |
| GET | `/api/emailcode/messages` | 另有清理能力由面板操作 |

## 自动链路

1. 消费者（如 PostHog 自动登录）发起流程并订阅收件箱等待验证码
2. 上游服务发验证码邮件到你的域名
3. Worker 收到邮件 → 解析正文提取验证码/链接/元数据 → `POST /api/emailcode/ingest`
4. 收件箱入库并唤醒等待者；消费者取码、标记消费，继续完成后续步骤

## 约束

- catch-all 会接管该域名所有收件：配置了 `FORWARD_TO` 才会继续转发，
  否则邮件只进收件箱不再外发，务必确认续转目标。
- 验证码提取是启发式（6 位数字，优先匹配 code/verification 上下文，
  且绝不在 MIME 头里匹配以免误取事务 ID），非标准格式的邮件可能提取不到。
- 收件箱内容含敏感验证码，接口按会话鉴权，请定期清理。
