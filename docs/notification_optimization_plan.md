# 通知系统优化方案

## 1. 现状问题分析

### 1.1 无限重试漏洞 (Infinite Retry Bug)
- **原因**：`enqueue()` 总是创建新的历史记录，而 `startRetryProcessor()` 在重试时并未更新原有记录的状态，而是通过 `enqueue` 创建了 `retry_count` 为 0 的新记录。
- **后果**：原有失败记录永远保持 `failed` 状态且重试次数不增加，导致每次轮询都会被重新拾取，造成无限重试和数据库膨胀。

### 1.2 "Pending" 通知丢失
- **原因**：通知在内存队列中处理，若程序崩溃，内存队列丢失。
- **后果**：数据库中标记为 `pending` 的记录在重启后不会被重新加载到内存队列，导致通知“遗失”。

### 1.3 队列串行阻塞
- **原因**：`startQueueProcessor()` 使用 `while` 循环配合 `await` 串行处理。
- **后果**：一个缓慢的渠道（如 SMTP 超时）会阻塞所有其他通知（如 Telegram）。

### 1.4 邮件渠道 HTML 注入
- **原因**：`email.js` 直接将文本插入 HTML 且无转义。
- **后果**：若告警详细信息包含 HTML 特殊字符，可能破坏邮件布局或产生安全隐患。

## 2. 优化方案

### 2.1 修复重试逻辑
- 修改 `service.js` 的 `enqueue` 方法，支持传入已有的 `log_id`。
- 若存在 `log_id`，则不再创建新记录。
- `startRetryProcessor` 在将任务加入队列前，先调用 `storage.history.updateStatus(log.id, 'retrying')`，触发重试计数增加。

### 2.2 实现重启恢复
- 在通知服务 `init()` 时，从数据库加载所有 `status` 为 `pending` 或 `retrying` 的记录，重新加入发送队列。

### 2.3 简单并发处理
- 将 `send` 逻辑稍作解耦，允许一定程度的并发处理（例如，每个渠道独立处理，或限制总并发数）。

### 2.4 邮件 HTML 转义
- 在 `email.js` 中引入 HTML 转义逻辑，确保消息内容安全。

## 3. 实施步骤

1. 修改 `modules/notification-api/service.js`：
   - 重构 `enqueue`。
   - 完善 `startRetryProcessor`。
   - 在 `init` 中增加加载历史逻辑。
2. 修改 `modules/notification-api/channels/email.js`：
   - 增加内容转义。
3. (可选) 修改 `modules/notification-api/models.js` 或 `storage.js` 以支持更方便的加载逻辑。
