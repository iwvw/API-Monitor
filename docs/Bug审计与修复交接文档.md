# 全仓 Bug 审计与修复交接文档

日期：2026-08-23
背景：2026-08-22 全仓复审（8 个只读子代理分域审计 + 人工逐行复核高危项）确认 08-18 的 13 项高危修复全部在位无回归，同时新发现 5 高危 + 约 15 中危 + 12 低危。随后按「全部修复」要求实施，本文档交接当前进度。

**2026-08-23 更新：A 组（notification 抑制/重试/限流）+ B 组（前端 3 项）已完成并提交。**

---

## 一、修复总览

| 域 | 项数 | 状态 |
|---|---|---|
| openai 网关 | 9（含高危 3） | ✅ 完成 + 回归测试 |
| 数据库/设置/备份/配置 | 6 | ✅ 完成 + 回归测试 |
| adminai | 5（含高危 1） | ✅ 完成 + 回归测试 |
| serveragent/system | 4（含高危 1） | ✅ 完成 + 回归测试 |
| uptime / cronjobs | 4 | ✅ 完成 + 回归测试 |
| 订阅/云集成/github/filebox/drawio | 7 | ✅ 完成 + 回归测试 |
| cronjobs 时区热更新 | 1 | ✅ 完成 + 回归测试 |
| 前端 | 7 | ✅ 全部完成 + vitest 用例 |
| notification 抑制/静默/重试/限流 | 1（中危） | ✅ 已实施（见第六节 A 组更新） |

验证基线：`go build ./...`、`go vet ./...` 零输出；`go test ./internal/... -count=1` 全绿；前端 `eslint` 干净、vitest 42 文件 401 用例全过。

## 二、高危五项（全部已修）

1. **Engine.IO WebSocket 无读上限（匿名 OOM DoS）**
   `serveragent/engineio.go`：两条拿到 conn 的路径（新建会话 / sid 重升级）统一 `conn.SetReadLimit(maxEngineIOPostBytes)`（64MB，与 polling POST 上限对齐）。测试 `TestEngineIOWebSocketRejectsOversizedFrame`。
2. **网关把 200 成功回复误判限流（吞回复/冻出口/重复计费）**
   `openai/proxypool.go isRateLimitResponse`：关键词分支仅在「状态码≥400」或「任意状态码但正文为 JSON 且含非空顶层 `error` 成员」时生效（新增 `jsonBodyHasTopLevelError`），纯文本 200 正文提及 "rate limit" 不再命中。测试 `TestIsRateLimitResponseIgnoresSuccessBody`。
3. **cron AI 任务 policy=allow 绕过「写操作全局开关」硬底线**
   `adminai/engine.go executeCallAPITool`：allow 分支置 autoApprove 前先 `getWriteEnabled`，false 返回「写操作未启用」；readonly 拒绝与普通会话审批流不变。测试 `TestCronAllowPolicyRespectsWriteSwitch` 等 3 个。
4. **/v1/messages 全端点失败返回 nil error → 客户端收 200 空 body**
   `openai/anthropic.go:631` 改为 `return status, nil, fmt.Errorf("%s", msg)`。测试 `TestAnthropicMessagesAllEndpointsFailedReturnsError`（变异验证：旧代码确实复现 200 空 body）。
5. **网关密钥端点白名单 failover 绕过**
   恢复候选级过滤：`gateway_keys.go` 新增 `filterCandidatesByKeyIdentity`（同步换算 chosenIndex、保持亲和排序）+ `recordDisallowedEndpoints`/`rejectDisallowedEndpoints`（403、Kind=blocked，与 enforceGatewayKeyLimits 同口径）；接入 chat/responses/messages 三入口。测试 `TestGatewayKeyEndpointWhitelistNoFailoverBypass`。

## 三、中危修复明细

### openai（8 项）
- sessionBindings 加容量上限 1024 + TTL 30min + 达限重建式清理（`trimSessionBindings`，测试 `TestTrimSessionBindings`）
- /v1/messages 请求体对齐 16MB MaxBytesReader，超限 413
- 上游非流式响应体统一走 `readUpstreamBodyLimited`（64MB LimitReader+1 检测超限报错）
- egress IP 探测移出全局锁（手写 singleflight）+ 失败负缓存 60s（16 并发仅 1 次探测的测试）
- 端点导入扩展 key 改为整串 JSON 数组 `SecureEncrypt`（与读取端对称），导出→导入→读取 round-trip 测试
- 端点更新验证条件改 `keyChanged || BaseURL 实际变化`；验证失败保留旧 models（两个测试）
- failover 链失败候选响应体统一 Close，消除连接泄漏

### 数据库/设置/备份（4 项）
- VACUUM 与换库互斥 TOCTOU：互斥下沉为 `database.SwapMutex()`（新文件 `database/swap.go`），settings 与 backup 共用；`enqueueVacuumTask` TryLock 失败返 409，`runVacuum` 全程持锁
- 导入回滚两分支补 ResetPool + sidecar 清理；`database.go` schemaErr 改失败不缓存可重试（`schemaMu/schemaDone` 替代 once）
- **方向调整（重要发现）**：Windows 下常驻句柄缓存物理连接，ResetPool 关不掉，O_TRUNC 就地重写会被残留锁卡 30s——新增 `database.PrepareSwapFile`（integrity_check + 预转 WAL + 清 sidecar），入库文件先转 WAL 后 journal PRAGMA 变 no-op（实测 30s BUSY → 3ms）
- backup restoreFromZip 全套防护（持 SwapMutex、前后 ResetPool、PrepareSwapFile、清 -wal/-shm/-journal、integrity_check、提示重启）
- 云上传改流式（os.Open 作 body、Stat 定 ContentLength、S3 SigV4 先流式算 SHA256 再 Seek 回头发）
- dotenv 剥 UTF-8 BOM + scanner.Buffer 1MB + scanner.Err 处理；日志清理 cutoff 改空格格式（时间戳列）/date 列单独口径

### adminai（4 项）
- audit 分页 offset 越界 clamp 返回空页（原 `merged[offset:end]` panic）
- Telegram channel Stop 幂等（close 后置 nil + stopChannelInstance 后 registry.Unregister）
- 审批 INSERT 走 execBusyRetry，失败清理等待通道并返回错误（触发器模拟落库失败的测试）
- 辅助 LLM 统一多候选回退：新增 `callLLMPlainWithFallback`（复用 splitModelList），标题/简报/记忆提炼三链路接入

### serveragent/system（1 中危）
- 匿名 WS 订阅 /metrics 与 root 命名空间泄露主机标识：调查确认 PublicStatusPage（root）与 PublicServerStatusPage（/metrics）匿名消费且只用 country_code 等字段 → 选**广播侧剥离敏感字段**方案：`metrics_hub.go sanitizeBroadcastPayload` 递归剥 hostname/ip/isp/org/asn，原始数据不可变（两个测试）

### uptime/cronjobs/notification 相关（3 项）
- uptime recordPush 补 lockMonitor per-monitor 互斥（并发不重复 incident 测试）
- cronjobs 取消传播：节点循环前查 run 状态、cancelled 即中止；最终 UPDATE 加 `AND status='running'` 保护，取消后不再发完成通知
- cronjobs 时区热更新：ReloadAll 内检测站点时区与调度器 Location 不同即重建（Stop→New(WithLocation)→Start 重挂全部 entry）；因无跨包设置变更回调，另加每分钟轻量看护 `startTZWatcher`。测试 `tz_reload_test.go`

### 订阅/云集成（6 项）
- **profile 型订阅内部节点记账链路补全**：ledger 四处查询补 profile 分支（COALESCE 周期/配额、pf.enabled 校验、entitlement、Current() 探测、ActiveCredentialsForNode 凭据下发）；NodeLibrary 新增 `InternalNodeIDs` 字段经 replacePlanNodeRelations 以 profile id 写入（explicit 清单）；create/updateProfile 变更后 EnqueueNodes reconcile
- github cleanupHistory cutoff 按表区分格式（五张 CURRENT_TIMESTAMP 表用空格格式；action_runs 保持 RFC3339），边界日测试
- filebox 分享码 5→8 字符 + 拒绝采样无偏随机；下载计数改条件原子 UPDATE（`downloads = downloads + 1 WHERE ... downloads < max_downloads` 按 RowsAffected 判定），burn 同样条件化
- drawio getVersion 补 docID 归属校验 404
- aliyun SWAS 补翻页 + 区域失败 Warn；tencent DNSPod 两接口翻页（`listAllDnspodPages`）；cloudflare listZones 翻页循环

## 四、低危修复明细

- AI Agent Key 校验改 subtle.ConstantTimeCompare（system/ai_access.go，五路径测试）
- v2 tasks SSE 流每写前 RenewWriteDeadline、写失败退出循环（tasks.go/server_ops.go）

## 五、已核实无需修复（避免后人重复排查）

- memguard 包无问题；main.go 无优雅关机但生产无挂起路径
- engineio 门禁、免密钥安装路由、uploads LFI、模板死循环渲染、uptime check 锁主体、SSE 五流续期等 08-18 修复全部在位
- docker_policy 单字符串 extraArgs 形态理论可绕，但现版 agent-rust 对任何非空 extraArgs 整体拒绝，仅旧版 Agent 有暴露面（未修，纵深缺口记录在案）
- subscription entitlement/凭据查询中 plan 分支与 profile 分支语义已对齐（'all'/‘explicit’ 同构）

## 六、未完成项与交接更新

### ✅ A. notification 抑制/静默/重试/限流 —— 已于 2026-08-23 实施完成

**原现状**：DryRun/Trigger 只评估时间窗/条件/维护窗；`suppression`(repeat_count/silence_minutes)、`quiet_until`、alert_state_tracking 表、max_retry_times/global_rate_limit_per_hr 只有 schema/API/前端配置界面，零评估逻辑。

**落地实现**（全部按下方原方案照做，新增 `internal/notification/suppression.go` + `suppression_test.go`）：
1. 纯函数 `shouldSendSuppression(state, now, repeatCount, silenceMinutes)`、`quietUntilActive`、线程安全 `hourlyRateLimiter`（UTC 小时桶，本窗口首次拒绝标记）
2. Trigger 接线：quiet_until → continue；lifecycle resolve 跳过抑制；fingerprint + `evaluateSuppression`（读 alert_state_tracking 判定 + UPSERT 累计 consecutive_failures，DB 错误 fail-open）；渠道发送前 rateLimiter.Allow(GlobalRateLimitPerHr)；任一渠道成功后 `recordSuppressionSent`（last_notified_at=now、consecutive_failures=1）
3. 重试：`sendWithRetry`（按 MaxRetryTimes，单次等待钳制 ≤5s，ctx.Done 提前退出），失败落 `retry_count`，重试成功后也回写
4. 测试：表测 + 集成（发-抑-发 / 静默窗内抑 / quiet_until 跳过 / 限流封顶 / 重试落库 / resolve 不被抑制）

### ✅ B. 前端剩余 3 项 —— 已于 2026-08-23 实施完成

1. `BackupPage.jsx`：save() 包 try/catch + toast.error；exportConfig 导出 toast 补「包含云存储密钥，请注意保管」
2. `pwa.js`：controllerchange 兜底 setTimeout 存句柄 `controllerVersionFallbackTimeout`，收到有效版本应答后 clearTimeout
3. `adminAiMessages.js`：tool_result 无 toolCallId 时回退匹配同名 RUNNING 无 id tool_call（无 toolName 取第一个 RUNNING 无 id，新增 `updateFirstPart`）；vitest 用例 +3

按以下分组提交：backend-go/（59 文件）、src/js/（8 文件）、docs/（本文档）。2026-08-23 已推 dev；vitest 用例总数升至 401。

### C. 待产品决策（不改动）

- 全局 `admin_ai_auto_approve=true` 时同样跳过 getWriteEnabled（adminai 修复代理附带发现）：原语义即「完全批准=直接执行」，与 cron allow 不同源，是否也纳入写开关硬底线需拍板
- EnableAutoEscalation 字段语义未定义，保持现状
- proxyClients 无界、auth 反代 IP 锁定等 08-18 保留项见 docs/修复审查清单.md 第六节

## 七、验证命令（提交前复跑）

```
cd backend-go && go build ./... && go vet ./... && go test ./internal/... -count=1 -timeout 900s
npm run lint && npm test -- --run
```
最近一次全绿记录：2026-08-23（go 全包 ok；eslint 干净；vitest 42 文件 401 用例通过）。全量并行偶发单包超时属环境负载，单包复跑即绿。

## 八、注意事项

1. **已提交**：本轮全部改动已按 backend-go / src/js / docs 分组提交并推送 dev（含新文件 database/swap.go、swap_test.go、vacuum_swap_test.go、cronjobs/tz_reload_test.go、notification/suppression.go、suppression_test.go）
2. 仓库 CRLF 检出导致 `gofmt -l` 对大量文件误报，只需保证自己 touched 文件内容级格式
3. 多个并行会话协作时曾出现半成品互相踩编译的情况，最终以上述全量验证为准
4. 08-22 审计完整问题清单（含每个疑点的排除理由）在会话报告与记忆 full-repo-bug-audit-2026-08-22 中；08-18 轮次见 docs/修复审查清单.md
