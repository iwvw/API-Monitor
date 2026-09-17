# ADR-0005：AI Agent 原生流通道架构决策

- 状态：已接受
- 日期：2026-09-17

## 决策

`aiagent` 模块的网关（`ANY /api/aiagent/gw/{instanceId}/*`）不依赖任何公网入口：既不占用中继的公开端口，也不使用 Cloudflare 反向代理。转发通过**主机 Agent 主动出站的原生流通道**完成——云端把 HTTP 请求编码为多路复用帧，经 Agent 的反连 WebSocket 送达目标机器的 `127.0.0.1:<port>`，响应（含 SSE 流）按帧回传并逐块写出。

模块归属与账号体系见 [ADR-0004](./0004-AIAgent管理模块架构决策.md)，需求见 [AI Agent 管理模块 PRD](../prd/AIAgent管理模块PRD.md)。

### 1. 问题：为什么不能用现成的公网入口方案

把本机 Agent 服务暴露给远端客户端，项目内已有三类现成机制，但都不满足需求：

1. **转发中心 `tcp_relay` + `access_mode=panel`**：能力完整，SSE 可用（走 `httputil.NewSingleHostReverseProxy`）。代价是需要一台中继入口主机并占用一个公网端口（区间 55655-60655），且流量经中继主机中转。需求明确「不用把本地端口通过 Cloudflare 等反代出去」，此方案仍属于「公网入口」范式。
2. **Cloudflare Named Tunnel**：需要域名与 CF 侧代管；`panel` 模式在 CF 隧道分支是**同步缓冲式** HTTP 代理（走 Agent 的 `http_proxy` 操作），不适合 SSE。
3. **P2P 打洞**：仍需要 STUN 与保底通道，且打洞成功率受网络环境影响，不是确定性方案。

需求是「不受网络影响、不暴露公网端口、直接连我们的服务器」，因此需要一个**由 Agent 主动出站、云端终止、全程不落地公网端口**的通道。

### 2. 决策：Agent 原生流通道

通道拓扑：

```text
客户端 ──HTTPS(Bearer)──> API Monitor 云端 ──WSS(出站)──> 主机 Agent ──TCP──> 127.0.0.1:<port>
```

1. 主机 Agent 已通过 Engine.IO 长连接常驻云端（`backend-go/internal/serveragent/engineio.go`），控制面复用该连接。
2. 数据面由 Agent **主动反向连接**一条独立的 WebSocket 数据通道，云端在该通道上做 HTTP ↔ 帧 的转换。
3. 全程不需要公网端口、不需要 DNS、不需要反向代理；Agent 只要求出站 443。

### 3. 连接建立：复用一次性 token 反连模式

沿用既有终端流 broker 的模式（`backend-go/internal/serveragent/terminal_stream.go`）：

1. 云端在收到网关请求后，创建一次性 stream（随机 `stream_id` + `stream_token`，TTL 30 秒、单次消费）。
2. 经控制通道向 Agent 下发「建立数据通道」任务，携带 `stream_id` 与 `stream_token`。
3. Agent 主动反连云端数据通道端点并在握手时提交 token。
4. 云端原子校验（绑定 serverID + token + 未消费 + 未过期）后完成对接，之后进入帧循环。

决策理由：不改动既有控制通道语义，不新造认证机制，一次性凭证不常驻、不落库。

### 4. 通道语义：一次请求一条字节管道

数据通道是一条**字节管道**，而不是在通道上再实现一层 HTTP：

1. 云端每收到一个网关请求，就创建一条一次性 stream（`stream_id` + `stream_token`），经控制通道下发任务给 Agent。
2. Agent 主动反连 `/ws/agent-port` 并在握手时提交 token；云端原子校验后完成对接。
3. 通道两端是原始字节：Agent 把本机 `127.0.0.1:<port>` 的 TCP 字节读出来按 WebSocket 二进制帧回传；云端把请求体按帧下发。
4. 云端在通道之上提供 `net.Conn` 门面（`agentPortConn`），交给 `http.Transport` 使用，从而获得**原生 HTTP 语义**：SSE 流式、chunked、大 body、头部语义全部自动成立，不需要自定义 HTTP 解析或帧类型扩展。
5. 并发由「每请求一条通道」实现：多个请求各自持有独立通道，互不阻塞；通道为一次性凭证、短 TTL、单次消费。

采用字节管道而非「HTTP over 自定义帧」的理由：HTTP 解析、分块、SSE 边界与背压都是易错点，交给标准库既更简单也更可靠；帧协议（DATA/CLOSE/KEEPALIVE）仅作为未来的可选扩展保留，不进入第一版关键路径。

### 4.1 反代与流式

1. 云端用 `httputil.ReverseProxy`，`FlushInterval = -1`，保证逐块写出。
2. 响应头到达即向上游返回，响应体经 `io.Pipe` 流式转发，**不等待完整响应**。
3. 写前通过 `sseutil.RenewWriteDeadline` 续期，避免 `http.Server.WriteTimeout` 掐断长连接。
4. 终止条件：客户端断开、Agent 掉线、目标连接关闭三种情况都回收通道并记录结果。

### 5. Agent 侧新增能力与任务

1. 能力位：`agent_capabilities()` 增加 `aiagent_stream_v1`（数据通道）与 `aiagent_probe_v1`（运行时探测）。
2. 任务类型：在 `match task.task_type`（`agent-rust/src/main.rs`）新增两个类型（55 数据通道、56 探测，避开已用 1-54）。
3. 数据通道任务（55）：Agent 先连接本机 `127.0.0.1:<port>`（连不上即失败，避免浪费一次性凭证），再反连 `/ws/agent-port`；随后进入双向字节搬运：本地读取 → WebSocket 二进制帧，WebSocket 二进制帧 → 本地写入。任务为长连接，不产出即时 TaskResult（云端以通道接入为准）。
4. 探测任务（56）：只读，回环端口监听检测 + 进程匹配，短超时（5 秒）。
5. 能力门禁：服务端以 `requireAgentCapability` 校验，缺失时返回「请升级 Agent」的明确提示，而非静默失败。

### 6. 背压、回收与稳定性

1. 出站数据复用 Agent 既有的分级队列，避免大流量挤占控制面心跳。
2. 云端入站队列有界（容量固定），消费方落后时断开通道并计数，不做无界内存堆积。
3. 通道 TTL 短、单次消费；`closeForServer` 在 Agent 控制连接断开时统一回收该主机的全部在途通道。
4. 数据通道断开时，云端使该主机的在途请求快速失败，避免客户端长时间挂起。

### 7. 备选方案对比

| 方案 | 是否暴露公网端口 | SSE 支持 | Agent 改动 | 结论 |
| --- | --- | --- | --- | --- |
| 转发中心 tcp_relay + panel | 是（占中继端口 55655-60655） | 支持 | 无 | 不采用：仍是公网入口，违背需求 |
| Cloudflare Named Tunnel | 是（CF 域名） | panel 分支为缓冲式，不支持 | 无 | 不采用 |
| P2P 打洞 + tcp_relay 保底 | 保底需公网端口 | 依赖保底 | 有 | 不采用：非确定性 |
| Agent 原生流通道（本决策） | 否 | 支持（逐块 flush） | 有 | **采用** |

### 8. 边界与未来

1. 数据传输过程中，云端只做字节转发，不解析 Agent 的业务协议；路径与头部处理仅限必要的过滤。
2. 本通道与转发中心、P2P 并存但互不依赖；转发中心仍服务其它场景（如非 Agent 的端口转发）。
3. 若未来需要降低每请求的连接开销，可在同一通道上叠加多路复用帧（复用中继的 `[1B type][2B conn_id][4B len]` 格式）；网关接口与客户端契约保持不变。
4. 若需要直连体验（例如低延迟本地网络优先），可在同一实例上叠加 P2P/直连作为可选路径。

## 后果

- 优点：不暴露任何公网端口、不依赖 Cloudflare、不需要中继主机；复用既有一次性 token 反连模式，新增代码面小；通道是字节管道，对 SSE 是原生支持而非例外处理；跨网络（4G/5G/公司网）只需出站 443 即可用。
- 缺点/代价：需要改动 Rust Agent 并重新发布（已有自更新能力，发布不是障碍）；每个 HTTP 请求占用一条通道与一次反连握手，连接开销高于多路复用方案；云端承担全部转发流量（应用侧数据，量级可控）；面板成为单点，面板不可用时客户端无法访问（会话数据仍在本地 Agent，不丢失）。
- 风险与缓解：
  - 长连接与 SSE 被中间层缓冲或截断：逐块 flush + 写超时续期，并做端到端流式验收。
  - Agent 侧连接/内存泄漏与背压：入站队列有界、通道 TTL、控制连接断开时统一回收。
  - 高频请求下的连接开销：第一版以正确性与简单性优先；若出现性能瓶颈，按第 8 节第 3 条叠加多路复用帧。
  - 主机 Agent 版本过旧：能力位门禁 + 升级提示。

## 来源

参考资料（项目内既有实现）：

- 一次性 token 流 broker：`backend-go/internal/serveragent/terminal_stream.go`
- Agent 控制通道与任务下发：`backend-go/internal/serveragent/engineio.go`、`agent_tasks.go`、`tasks.go`
- 本机端口桥接与复用帧参考：`backend-go/cmd/api-monitor-relay/main.go`、`agent-rust/src/tcp_forwarder.rs`（第一版未采用其帧层，仅作未来多路复用的参考）
- Agent 独立流通道（V2）实现要点：`agent-rust/src/main.rs` 的 `handle_pty_start_v2`
- SSE 写超时续期工具：`backend-go/internal/sseutil/sseutil.go`
- 既有 SSE 范式：`backend-go/internal/serveragent/tasks.go`、`backend-go/internal/adminai/stream.go`
- 流式反代参考：`backend-go/internal/serveragent/managed_forward_reconcile.go`
- 配套决策：[ADR-0004：AI Agent 管理模块架构决策](./0004-AIAgent管理模块架构决策.md)
- 配套需求：[AI Agent 管理模块 PRD](../prd/AIAgent管理模块PRD.md)
