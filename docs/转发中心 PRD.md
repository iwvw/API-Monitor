# 转发中心（Forwarding Center）PRD

- 状态：草稿
- 日期：2026-08-24
- 作者：AI 辅助设计

---

## 目录

1. [概述](#1-概述)
2. [数据模型](#2-数据模型)
3. [传输方式详细设计](#3-传输方式详细设计)
4. [API 设计](#4-api-设计)
5. [前端设计](#5-前端设计)
6. [Agent 变更（Phase 2）](#6-agent-变更phase-2)
7. [TCP 中继器二进制设计](#7-tcp-中继器二进制设计)
8. [安全设计](#8-安全设计)
9. [错误处理与边界情况](#9-错误处理与边界情况)
10. [Phase 1 实现要点](#10-phase-1-实现要点)
11. [分阶段规划](#11-分阶段规划)
12. [测试计划](#12-测试计划)
13. [与现有架构的边界](#13-与现有架构的边界)
14. [风险与缓解](#14-风险与缓解)
15. [附录：参考实现](#15-附录参考实现)
16. [可视化连接画布设计](#16-可视化连接画布设计)
17. [高可用与故障转移设计](#17-高可用与故障转移设计)

---

## 1. 概述

### 1.1 是什么

转发中心是一个统一的管理模块，允许用户将任意内网 TCP/HTTP 服务通过多种传输方式暴露到公网或其他内网主机，实现「在任何安装了 Agent 的机器上，访问任意一台机器上的本地服务」的目标。

### 1.2 核心能力

- **统一管理**：在一个页面管理所有转发规则，无论底层传输方式是什么
- **多传输方式**：Cloudflare Tunnel、自建 TCP 中继、P2P 直连（分期实现）
- **多访问控制**：公开 / Token 认证 / 面板认证（分期实现）
- **零配置目标**：部署 Agent 后，面板上点几下即可完成转发规则创建

### 1.3 用户场景

| 场景 | 示例 | 推荐传输方式 |
|---|---|---|
| 本地 Web 开发调试 | 笔记本上跑 `localhost:3000`，需要在手机上访问 | CF Tunnel（HTTP） |
| 内网 SSH 访问 | 家里 NAS 的 SSH 端口，需要从公司访问 | TCP 中继 |
| 团队内共享服务 | 测试环境数据库（3306），团队成员需要直连 | TCP 中继 + Token |
| 点对点大文件传输 | 两台内网机器之间传文件，需要高速直连 | P2P（Phase 3） |
| 临时暴露 API 给第三方 | 本地开发的服务需要给合作方回调 | CF Tunnel + Token |

### 1.4 术语

| 术语 | 英文 | 定义 |
|---|---|---|
| 转发规则 | Forward | 一条将「源主机:端口」暴露到「目标地址」的配置记录 |
| 源主机 | Source Host | 服务实际运行所在的主机，必须安装 Agent |
| 传输方式 | Transport | 数据从入口到源主机的传输通道：cf_tunnel / tcp_relay / p2p |
| 访问控制 | Access Mode | 客户端连接时是否需要凭证：public / token / panel |
| 中继入口 | Relay Host | tcp_relay 模式下，有公网 IP 的主机，作为流量入口 |
| 反向隧道 | Reverse Tunnel | 源主机到中继入口之间的反向连接，由源主机主动发起 |
| 转发器 | Relay | 入口主机上运行的中继守护进程，listen 端口并桥接流量 |
| 访问地址 | Access URL | 客户端实际连接的地址，格式取决于传输方式 |
| 访问令牌 | Access Token | Token 模式下客户端连接时需提供的凭证 |

---

## 2. 数据模型

### 2.1 managed_forwards 表

```sql
CREATE TABLE IF NOT EXISTS managed_forwards (
    -- 主键
    id              TEXT PRIMARY KEY,                          -- 格式: fwd_<16位hex>

    -- 基本配置
    name            TEXT NOT NULL,                             -- 1-64字符，用户命名
    server_id       TEXT NOT NULL,                             -- 源主机 ID，关联 server_accounts.id
    local_host      TEXT NOT NULL DEFAULT '127.0.0.1',        -- 源地址，IPv4/IPv6/主机名
    local_port      INTEGER NOT NULL CHECK(local_port BETWEEN 1 AND 65535),  -- 源端口

    -- 协议
    protocol        TEXT NOT NULL DEFAULT 'tcp'                -- tcp / http / https
                    CHECK(protocol IN ('tcp','http','https')),

    -- 传输方式
    transport       TEXT NOT NULL                              -- cloudflare_tunnel / tcp_relay / p2p
                    CHECK(transport IN ('cloudflare_tunnel','tcp_relay','p2p')),

    -- === CF Tunnel 专用字段 ===
    tunnel_hostname TEXT,                                      -- 对外域名，如 cf-host001.example.com
    tunnel_path     TEXT,                                      -- ingress 路径，如 /fwd/fwd_xxxx

    -- === TCP 中继专用字段 ===
    relay_server_id TEXT,                                      -- 中继入口主机 ID
    remote_port     INTEGER,                                   -- 中继入口分配的端口
                    -- CHECK(remote_port BETWEEN 55655 AND 60655)

    -- === P2P 专用字段（预留） ===
    p2p_local_port  INTEGER,                                   -- P2P 模式下本地 listen 端口

    -- 访问控制
    access_mode     TEXT NOT NULL DEFAULT 'public'              -- public / token / panel
                    CHECK(access_mode IN ('public','token','panel')),
    access_token    TEXT,                                      -- token 模式：访问凭证，secure.SecureEncrypt 加密

    -- 状态
    desired_status  TEXT NOT NULL DEFAULT 'running'             -- running / stopped
                    CHECK(desired_status IN ('running','stopped')),
    apply_status    TEXT NOT NULL DEFAULT 'pending'             -- pending / deploying / running / stopped / failed / disconnected
                    CHECK(apply_status IN ('pending','deploying','running','stopped','failed','disconnected')),
    last_stage      TEXT NOT NULL DEFAULT '',
    last_error      TEXT NOT NULL DEFAULT '',
    connector_count INTEGER NOT NULL DEFAULT 0,                 -- 当前活跃连接数

    -- 审计字段
    created_at      TEXT DEFAULT (datetime('now')),
    updated_at      TEXT DEFAULT (datetime('now')),

    FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE,
    FOREIGN KEY (relay_server_id) REFERENCES server_accounts(id) ON DELETE SET NULL
);
```

### 2.2 字段详细说明

| 字段 | 校验规则 | 为空时 | 示例 |
|---|---|---|---|
| `id` | 自动生成，16 字节随机 hex | — | `fwd_3a1f5c8e9b2d047f` |
| `name` | 1-64 字符，不可为空，trim 前后空格 | — | `"调试 API v2"` |
| `server_id` | 必须存在于 `server_accounts` | — | `"host-001"` |
| `local_host` | IPv4/IPv6/主机名，不校验可达性 | `127.0.0.1` | `"0.0.0.0"`、`"::1"`、`"localhost"` |
| `local_port` | 1-65535，整数 | — | `5000` |
| `protocol` | 枚举值 | `tcp` | `"http"`、`"https"` |
| `transport` | 枚举值，创建后不可变更 | — | `"cloudflare_tunnel"` |
| `tunnel_hostname` | transport=cf_tunnel 时必填 | — | `"cf-host001.example.com"` |
| `tunnel_path` | transport=cf_tunnel 时必填，以 `/` 开头 | — | `"/fwd/fwd_3a1f"` |
| `relay_server_id` | transport=tcp_relay 时必填 | — | `"relay-001"` |
| `remote_port` | transport=tcp_relay 时必填，55655-60655 | — | `55655` |
| `access_mode` | 枚举值 | `public` | `"token"` |
| `access_token` | access_mode=token 时自动生成 32 字符 | 空 | 加密存储 |
| `desired_status` | 用户期望状态 | `running` | `"stopped"` |
| `apply_status` | 实际状态，后端维护 | `pending` | `"running"` |

### 2.3 业务约束

1. **transport 不可变更**：创建后传输方式不可改。用户如需更换传输方式，应删除重建。
2. **源主机唯一性检查**：同一台源主机上，`(local_host, local_port)` 组合唯一（不同传输方式也不允许重复，避免端口冲突）。
3. **中继端口唯一性**：同一台中继入口主机上，`remote_port` 唯一（由 `idx_managed_forwards_relay_port` 保证）。
4. **隧道路径唯一性**：同一个 CF 隧道域名上，`tunnel_path` 唯一（由 `idx_managed_forwards_tunnel_path` 保证）。
5. **级联删除**：删除源主机时，自动删除所有关联转发规则（ON DELETE CASCADE）。
6. **中继入口删除**：删除中继入口主机时，转发规则的中继字段置 NULL，apply_status 变为 `failed`，last_error 记录「中继主机已删除」。

### 2.4 索引

```sql
-- 按源主机查看转发规则
CREATE INDEX IF NOT EXISTS idx_managed_forwards_server
    ON managed_forwards(server_id, updated_at DESC);

-- 按传输方式筛选
CREATE INDEX IF NOT EXISTS idx_managed_forwards_transport
    ON managed_forwards(transport, apply_status);

-- 中继端口唯一性（部分索引，只对 tcp_relay 有效）
CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_forwards_relay_port
    ON managed_forwards(relay_server_id, remote_port)
    WHERE relay_server_id IS NOT NULL;

-- 隧道路径唯一性（部分索引，只对 cf_tunnel 有效）
CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_forwards_tunnel_path
    ON managed_forwards(tunnel_hostname, tunnel_path)
    WHERE tunnel_hostname IS NOT NULL;
```

### 2.5 迁移

```sql
-- 给 managed_proxy_tunnels 表加一个路径前缀字段
ALTER TABLE managed_proxy_tunnels
    ADD COLUMN tunnel_path_prefix TEXT NOT NULL DEFAULT '/fwd';
```

`tunnel_path_prefix` 用于区分该隧道上的转发规则路径与代理节点路径：
- 代理节点路径：`/api-monitor/<uuid>`
- 转发规则路径：`/fwd/<forward_id>`

### 2.6 状态机

```
                          ┌──────────┐
                          │  pending │  ← 创建后尚未部署
                          └────┬─────┘
                               │ deploy
                          ┌────▼──────┐
                          │ deploying │  ← 部署中
                          └────┬──────┘
                  ┌─────────────┼──────────────┐
                  │ 成功        │ 失败          │ 停止
             ┌────▼────┐  ┌────▼────┐  ┌─────▼──────┐
             │ running │  │ failed  │  │  stopped   │
             └────┬────┘  └────┬────┘  └─────┬──────┘
                  │ 重试       │ 重试         │ 启动
                  └────────────┘              │
                  │ 健康检查断开               │
             ┌────▼──────────┐                │
             │ disconnected  │────────────────┘
             └────┬──────────┘  (重新部署)
                  │ 自愈重放
                  └──→ deploying
```

**状态转换触发条件**：

| 转换 | 触发 | 由谁执行 |
|---|---|---|
| pending → deploying | 用户点「部署」或 deploy 接口 | 后端 handler |
| deploying → running | 部署成功（CF ingress 更新完成 / 中继端口分配 + 隧道建立） | 异步 task |
| deploying → failed | 部署失败（CF API 报错 / 端口冲突 / agent 离线） | 异步 task |
| running → disconnected | 健康循环检测到 CF 连接断开 / 中继隧道断开 | 健康循环 |
| running → stopped | 用户点「停止」 | 后端 handler |
| stopped → deploying | 用户点「启动」 | 后端 handler |
| disconnected → deploying | 自愈重放（指数退避） | 健康循环 |
| failed → deploying | 用户点「重试」 | 后端 handler |

### 2.7 响应结构体（Go）

```go
type ManagedForward struct {
    ID             string `json:"id"`
    Name           string `json:"name"`
    ServerID       string `json:"server_id"`
    ServerName     string `json:"server_name,omitempty"`  // JOIN 联表
    LocalHost      string `json:"local_host"`
    LocalPort      int    `json:"local_port"`
    Protocol       string `json:"protocol"`
    Transport      string `json:"transport"`

    // CF Tunnel
    TunnelHostname string `json:"tunnel_hostname,omitempty"`
    TunnelPath     string `json:"tunnel_path,omitempty"`

    // TCP Relay
    RelayServerID  string `json:"relay_server_id,omitempty"`
    RelayServerName string `json:"relay_server_name,omitempty"` // JOIN 联表
    RemotePort     int    `json:"remote_port,omitempty"`

    // 访问控制
    AccessMode     string `json:"access_mode"`
    AccessURL      string `json:"access_url"`       // 计算字段，非 DB 列
    HasToken       bool   `json:"has_token"`         // 前端显示用，后端不暴露 token 明文

    // 状态
    DesiredStatus  string `json:"desired_status"`
    ApplyStatus    string `json:"apply_status"`
    LastStage      string `json:"last_stage"`
    LastError      string `json:"last_error"`
    ConnectorCount int    `json:"connector_count"`

    // 时间
    CreatedAt      string `json:"created_at"`
    UpdatedAt      string `json:"updated_at"`
}
```

**AccessURL 计算规则**：

| transport | 格式 | 示例 |
|---|---|---|
| cloudflare_tunnel + http | `https://<tunnel_hostname><tunnel_path>` | `https://cf-host001.example.com/fwd/fwd_xxxx` |
| cloudflare_tunnel + tcp | `tcp://<tunnel_hostname>:443` | `tcp://cf-host001.example.com:443` |
| tcp_relay | `tcp://<relay_host>:<remote_port>` | `tcp://relay-001.example.com:55655` |
| p2p | `tcp://<p2p_address>:<p2p_local_port>` | `tcp://10.0.0.5:45678` |

---

## 3. 传输方式详细设计

### 3.1 Cloudflare Tunnel（Phase 1）

#### 3.1.1 原理

复用现有托管隧道基础设施。每台服务器已有 0 或 1 条 Cloudflare Named Tunnel（`managed_proxy_tunnels` 表）。cloudflared 在源主机上运行，通过出站 WSS 连接到 Cloudflare 边缘。在现有 tunnel ingress 配置中追加一条转发规则路径。

#### 3.1.2 数据流

```
客户端                             Cloudflare                    源主机
  │                                  │                            │
  │  GET https://cf-host.example.com │                            │
  │  /fwd/fwd_xxxx                   │                            │
  │ ────────────────────────────────>│                            │
  │                                  │  cloudflared 隧道          │
  │                                  │  (WSS 长连接)              │
  │                                  │ ──────────────────────────>│
  │                                  │                            │
  │                                  │  HTTP 到 tcp://127.0.0.1: │
  │                                  │  5000                      │
  │                                  │ ──────────────────────────>│
  │                                  │                            │
  │  <── 响应数据 ──────────────────│<── 响应数据 ──────────────│
```

#### 3.1.3 ingress 配置格式

```json
{
  "config": {
    "ingress": [
      {"hostname": "cf-host001.example.com", "path": "/api-monitor/uuid1", "service": "http://127.0.0.1:45654"},
      {"hostname": "cf-host001.example.com", "path": "/api-monitor/uuid2", "service": "http://127.0.0.1:45655"},
      {"hostname": "cf-host001.example.com", "path": "/fwd/fwd_3a1f", "service": "http://127.0.0.1:5000"},
      {"hostname": "cf-host001.example.com", "path": "/fwd/fwd_4b2e", "service": "tcp://127.0.0.1:22"},
      {"service": "http_status:404"}
    ]
  }
}
```

注意：`service` 字段根据 `protocol` 不同而变化：
- `protocol=http` → `http://127.0.0.1:<local_port>`
- `protocol=https` → `https://127.0.0.1:<local_port>`
- `protocol=tcp` → `tcp://127.0.0.1:<local_port>`

#### 3.1.4 实现要点

**创建转发规则**：
1. 校验源主机已部署 CF Tunnel（`managed_proxy_tunnels` 存在且 `apply_status='running'`）
2. 若无隧道，自动创建（复用 `runManagedTunnelDeploy`）
3. 生成 `tunnel_path = tunnel_path_prefix + "/" + forward_id`，如 `/fwd/fwd_3a1f`
4. 写入 `managed_forwards` 表
5. 调用 `syncTunnelIngress(serverID)` 重新部署 ingress 配置

**syncTunnelIngress 流程**：
1. 从 `managed_proxy_nodes` 读取所有 `access_mode='cloudflare_tunnel'` 的节点
2. 从 `managed_forwards` 读取所有 `transport='cloudflare_tunnel'` 且 `desired_status='running'` 的转发规则
3. 合并两者为一个 ingress 数组
4. `PUT /accounts/{id}/cfd_tunnel/{id}/configurations` 更新远程配置
5. 成功则 `apply_status='running'`，失败则 `apply_status='failed'`

**删除转发规则**：
1. 从 `managed_forwards` 删除记录
2. 调用 `syncTunnelIngress(serverID)` 移除 ingress 中的对应路径

**健康检查**：
- 复用现有 `startManagedTunnelHealthLoop`（每 5 分钟）
- CF 隧道健康即代表其上的所有转发规则健康
- 不需要为转发规则单独做健康检查

**限制**：
- 不支持 UDP 协议
- 依赖 Cloudflare 账号和服务可用性
- 免费计划吞吐有限（~100Mbps，每月 1GB 免费出站）
- 单隧道最多 50 条 ingress 规则（含兜底 404）
- TCP 协议的 TLS 由 Cloudflare 终止，非端到端加密

#### 3.1.5 错误场景

| 场景 | 表现 | 恢复 |
|---|---|---|
| 源主机 cloudflared 断开 | 转发规则不可用，隧道健康检查标 `disconnected` | 自愈重放（现有机制） |
| CF 账号 token 过期 | 部署失败，`last_error` 提示 token 过期 | 用户更新 CF 账号 |
| 超过 50 条 ingress 上限 | 部署失败，`last_error` 提示上限 | 用户删除不需要的规则 |
| 源主机离线 | 部署失败，`last_error` 提示 agent 离线 | 等待主机上线后重试 |

### 3.2 TCP 中继（Phase 2）

#### 3.2.1 架构总览

```
                    ┌──────────────────────────────────────────────────┐
                    │                    面板                           │
                    │  ┌────────────────────────────────────────────┐  │
                    │  │ 端口分配器 / 健康检查 / 规则管理            │  │
                    │  └────────────────────────────────────────────┘  │
                    └───────┬──────────────┬───────────────────────────┘
                            │ 控制信令      │ 控制信令
                    ┌───────▼──────┐ ┌──────▼──────────┐
                    │  入口主机     │ │  源主机          │
                    │  Agent +     │ │  Agent +        │
                    │  api-monitor-│ │  tcp_forwarder   │
                    │  relay       │ │  (内置隧道)      │
                    │              │ │                  │
 客户端 ──TCP:PORT──> TcpListener  │ │  TcpStream       │
                    │     │       │ │   ↑              │
                    │  copy_bidir │ │ copy_bidir       │
                    │     │       │ │   │              │
                    │  ┌──▼────┐  │ │ ┌─▼──────────┐   │
                    │  │ 隧道池  │  │ │ 隧道连接    │   │
                    │  └───────┘  │ │ └─────┬──────┘   │
                    └─────────────┘ └───────┼──────────┘
                                            │
                                   127.0.0.1:local_port
```

#### 3.2.2 核心组件

**`api-monitor-relay` 转发器**（独立 Go 二进制，~200 行）
- 功能极简：listen 指定端口，接受客户端连接，通过预先建立的反向隧道桥接
- 不处理应用层协议，纯 TCP 字节流转发
- 每端口一个 goroutine（`net.Listener`），每连接一个 goroutine（`io.Copy`）
- 通过 HTTP 接口上报健康状态（连接数、延迟）

**Agent 端 `tcp_forwarder` 模块**（`agent-rust/src/tcp_forwarder.rs`，~300 行）
- 收到面板 task type 53 的 `"install"` 命令
- 建立到入口主机的反向 TCP 连接（隧道连接）
- 隧道连接上有轻量协议头（forward_id 标识转发规则）
- 收到 `"remove"` 命令时断开隧道连接

#### 3.2.3 隧道协议

隧道连接建立后，源主机 Agent 发送一个 4 字节的 forward_id 长度 + forward_id 内容作为协议头：

```
源主机 → 入口主机:
  [4 字节 BE] forward_id 长度 N
  [N 字节]   forward_id 字符串

入口主机 → 源主机:
  [4 字节 BE] 状态码 (0=成功, 1=未知 forward_id)
```

入口主机收到后，查找该 forward_id 对应的转发规则，将后续所有字节流双向桥接到已连接的客户端。

#### 3.2.4 连接生命周期

```
  源主机 Agent                 入口主机                   客户端
      │                          │                        │
      │ 1. 面板下发 install      │                        │
      │<─────────────────────────│                        │
      │                          │                        │
      │ 2. 建立反向 TCP 连接     │                        │
      │ ────────TCP connect─────>│                        │
      │                          │                        │
      │ 3. 发送协议头(forward_id)│                        │
      │ ────────协议头──────────>│                        │
      │                          │                        │
      │ 4. 确认(状态码=0)        │                        │
      │ <────────确认────────────│                        │
      │                          │                        │
      │                          │ 5. 客户端连接          │
      │                          │<───TCP connect─────────│
      │                          │                        │
      │ 6. 双向桥接              │ 6. 双向桥接            │
      │ <═══════ 数据 ═════════>│ <════ 数据 ═══════════>│
      │                          │                        │
      │ 7. 客户端断开            │                        │
      │                          │<───TCP disconnect──────│
      │                          │                        │
      │ 8. 保持隧道连接          │                        │
      │ (等待下一个客户端)       │                        │
      │                          │                        │
      │ 9. 面板下发 remove       │                        │
      │<─────────────────────────│                        │
      │                          │                        │
      │ 10. 断开隧道连接         │                        │
      │ ────────TCP disconnect──>│                        │
```

**关键设计**：
- 隧道连接是**长连接**，在第一条转发规则创建时建立，最后一条删除时断开
- 隧道连接不因客户端断开而断开，保持等待下一个客户端
- 隧道连接断开后，Agent 自动重连（指数退避 1s/2s/4s/8s/.../30s）

#### 3.2.5 端口分配

```
区间: 55655 - 60655
分配规则:
  1. 面板查询 managed_forwards 中 relay_server_id=当前入口主机 的所有 remote_port
  2. 在 55655-60655 中找第一个未被占用的端口
  3. 暂存到 managed_forwards 表
  4. 通过 task 53 发给入口主机 Agent
  5. 入口主机 Agent 调用 bind-check（TcpListener::bind 测试）
  6. 若端口被占用，面板分配下一个，重复 4-5
  7. 最多尝试 10 次，全部失败则 deploy 失败
```

**端口释放**：
- 删除转发规则时，面板标记端口为可用
- 运行时端口释放由入口主机 Agent 管理（关闭 TcpListener）
- 不立即回收（防止 TIME_WAIT 状态端口被复用）

#### 3.2.6 防火墙

复用 `proxy_runtime.rs` 中的 `ensure_firewall_port()` 逻辑：
- 优先使用 `firewall-cmd`（firewalld）
- 回退到 `ufw`
- 回退到 `iptables`

```rust
fn ensure_firewall_port(port: u16, open: bool) -> Result<(), String> {
    if cfg!(target_os = "linux") {
        // firewalld
        if Command::new("firewall-cmd").arg("--state").status().is_ok() {
            let action = if open { "--add-port" } else { "--remove-port" };
            return systemctl_run(&format!("firewall-cmd --permanent {action}={port}/tcp --zone=public"));
        }
        // ufw
        if Command::new("ufw").arg("status").status().is_ok() {
            let action = if open { "allow" } else { "deny" };
            return systemctl_run(&format!("ufw {action} {port}/tcp"));
        }
    }
    Ok(())
}
```

#### 3.2.7 健康检查

入口主机 Agent 每 30 秒检测：
- TcpListener 是否仍在 listen
- 反向隧道连接是否存活（TCP keepalive）
- 上报 `connector_count`（当前活跃客户端连接数）

面板健康循环每 5 分钟检查：
- 入口主机 Agent 在线
- 隧道连接状态（通过 task 53 `"status"` 查询）
- 更新 `apply_status`（`running` / `disconnected`）

#### 3.2.8 限制

- 入口主机需要公网 IP 或端口映射
- 入口主机带宽决定转发吞吐上限
- 不支持 UDP（Phase 2 仅 TCP）
- 每转发规则一条隧道连接，N 条规则 = N 条长连接

### 3.3 P2P 直连（Phase 3，占位设计）

#### 3.3.1 原理

面板仅作为信令交换中心，NAT 打洞成功后数据直接在客户端和源主机之间传输，不经过任何中间节点。

#### 3.3.2 架构

```
                    ┌──────────┐
                    │  面板     │  ← 信令交换
                    │ (信令服务) │
                    └─────┬────┘
                          │
          ┌───────────────┼───────────────┐
          │ 交换 SDP/ICE  │               │
          │               │               │
    ┌─────▼──────┐  ┌─────▼──────┐
    │  客户端     │  │  源主机    │
    │  (任意)     │  │  Agent     │
    │             │  │            │
    │  ┌──────┐   │  │ ┌──────┐  │
    │  │ 打洞  │   │  │ │ 打洞  │  │
    │  │ 客户端│   │  │ │ 服务端│  │
    │  └──────┘   │  │ └──────┘  │
    └─────────────┘  └───────────┘
           │              │
           └── 直连 ──────┘
           (UDP/TCP, 不经过面板)
```

#### 3.3.3 实现策略

- 使用 STUN 协议（RFC 3489）进行 NAT 类型探测
- 支持 UDP 打洞 + TCP 打洞（UDP 为主）
- 打洞失败时自动回退到 TCP 中继（Phase 2）
- 使用 `tokio::net::UdpSocket` 实现 Agent 端打洞

**Phase 3 不在此 PRD 中详述，仅预留表结构和字段。**

---

## 4. API 设计

### 4.1 路由清单

所有路由前缀：`/api/server/forward`

| 方法 | 路径 | 描述 | 阶段 | 鉴权 |
|---|---|---|---|---|
| `GET` | `/api/server/forward` | 列出所有转发规则（支持分页/筛选/搜索） | P1 | session |
| `POST` | `/api/server/forward` | 创建转发规则 | P1 | session |
| `GET` | `/api/server/forward/{id}` | 读取单个转发规则详情 | P1 | session |
| `PUT` | `/api/server/forward/{id}` | 更新转发规则（仅允许更新部分字段） | P1 | session |
| `DELETE` | `/api/server/forward/{id}` | 删除转发规则 | P1 | session |
| `POST` | `/api/server/forward/{id}/deploy` | 部署/重部署 | P1 | session |
| `POST` | `/api/server/forward/{id}/stop` | 停止转发（desired_status=stopped） | P1 | session |
| `POST` | `/api/server/forward/{id}/start` | 启动转发（desired_status=running） | P1 | session |
| `GET` | `/api/server/forward/{id}/status` | 读取实时连接状态（连接数、延迟） | P2 | session |
| `GET` | `/api/server/forward/available-ports` | 列出入口主机可用端口 | P2 | session |
| `POST` | `/api/server/forward/preflight` | 创建前预检 | P1 | session |

### 4.2 通用响应格式

所有响应遵循系统现有格式：

```json
// 成功
{
    "success": true,
    "data": { ... }
}

// 列表
{
    "success": true,
    "data": [ ... ],
    "total": 42,
    "offset": 0,
    "limit": 20
}

// 错误
{
    "success": false,
    "error": "描述错误信息"
}
```

### 4.3 端点详解

#### 4.3.1 `GET /api/server/forward` — 列出转发规则

**查询参数**：

| 参数 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `offset` | int | 0 | 分页偏移 |
| `limit` | int | 20 | 每页数量（最大 100） |
| `server_id` | string | — | 按源主机筛选 |
| `transport` | string | — | 按传输方式筛选（cloudflare_tunnel/tcp_relay/p2p） |
| `apply_status` | string | — | 按状态筛选 |
| `search` | string | — | 模糊搜索 name |

**响应**：

```json
{
    "success": true,
    "data": [
        {
            "id": "fwd_3a1f5c8e9b2d047f",
            "name": "调试 API",
            "server_id": "host-001",
            "server_name": "开发机",
            "local_host": "127.0.0.1",
            "local_port": 5000,
            "protocol": "http",
            "transport": "cloudflare_tunnel",
            "tunnel_hostname": "cf-host001.example.com",
            "tunnel_path": "/fwd/fwd_3a1f5c8e9b2d047f",
            "access_mode": "public",
            "access_url": "https://cf-host001.example.com/fwd/fwd_3a1f5c8e9b2d047f",
            "desired_status": "running",
            "apply_status": "running",
            "last_stage": "completed",
            "last_error": "",
            "connector_count": 0,
            "created_at": "2026-08-24 10:00:00",
            "updated_at": "2026-08-24 10:00:00"
        }
    ],
    "total": 1,
    "offset": 0,
    "limit": 20
}
```

#### 4.3.2 `POST /api/server/forward` — 创建转发规则

**请求体**：

```json
{
    "name": "调试 API",                          // 必填，1-64 字符
    "server_id": "host-001",                     // 必填
    "local_host": "127.0.0.1",                   // 可选，默认 127.0.0.1
    "local_port": 5000,                          // 必填，1-65535
    "protocol": "http",                          // 可选，默认 tcp
    "transport": "cloudflare_tunnel",            // 必填，创建后不可改
    "relay_server_id": null,                     // tcp_relay 时必填
    "access_mode": "public"                      // 可选，默认 public
}
```

**校验规则**：

| 字段 | 校验 | 错误码 |
|---|---|---|
| `name` | 非空，trim 后 1-64 字符 | 400 |
| `server_id` | 必须存在于 server_accounts | 404 |
| `local_port` | 1-65535 整数 | 400 |
| `protocol` | 必须为 `tcp`/`http`/`https` | 400 |
| `transport` | 必须为 `cloudflare_tunnel`/`tcp_relay`/`p2p` | 400 |
| `relay_server_id` | transport=tcp_relay 时必填且存在于 server_accounts | 400 |
| `access_mode` | 必须为 `public`/`token`/`panel` | 400 |
| 唯一性 | `(server_id, local_host, local_port)` 不可重复 | 409 |

**响应**（201 Created）：

```json
{
    "success": true,
    "data": {
        "id": "fwd_3a1f5c8e9b2d047f",
        "name": "调试 API",
        "server_id": "host-001",
        "server_name": "开发机",
        "local_host": "127.0.0.1",
        "local_port": 5000,
        "protocol": "http",
        "transport": "cloudflare_tunnel",
        "tunnel_hostname": "cf-host001.example.com",
        "tunnel_path": "/fwd/fwd_3a1f5c8e9b2d047f",
        "access_mode": "public",
        "access_url": "https://cf-host001.example.com/fwd/fwd_3a1f5c8e9b2d047f",
        "desired_status": "running",
        "apply_status": "pending",
        "created_at": "2026-08-24 10:00:00",
        "updated_at": "2026-08-24 10:00:00"
    }
}
```

**注意**：创建后 `apply_status=pending`，需要调用 `deploy` 端点部署。

#### 4.3.3 `PUT /api/server/forward/{id}` — 更新转发规则

**可更新字段**：

| 字段 | 创建后是否可改 | 说明 |
|---|---|---|
| `name` | 是 | |
| `local_host` | 是 | 修改后需重新部署 |
| `local_port` | 是 | 修改后需重新部署 |
| `protocol` | 是 | 修改后需重新部署 |
| `relay_server_id` | 是 | 修改后需重新部署 |
| `access_mode` | 是 | 立即生效，无需重新部署 |
| `transport` | **否** | 创建后不可变更 |

**响应**（200 OK）：返回更新后的完整对象。

#### 4.3.4 `DELETE /api/server/forward/{id}` — 删除转发规则

**行为**：
- 如果 `apply_status=running`，需要先执行停止流程（断开隧道连接 / 移除 ingress）
- 然后删除数据库记录
- 如果源主机已离线，强制删除数据库记录（不等待 agent 响应）

**查询参数**：

| 参数 | 默认 | 说明 |
|---|---|---|
| `cascade` | `0` | 设为 `1` 时跳过确认，直接级联删除 |

**响应**（200 OK）：

```json
{
    "success": true,
    "data": {
        "message": "转发规则已删除"
    }
}
```

**冲突响应**（409 Conflict）：

```json
{
    "success": false,
    "error": "该转发规则仍有活跃连接（3 个），确认删除请使用 cascade=1",
    "data": {
        "connector_count": 3
    }
}
```

#### 4.3.5 `POST /api/server/forward/{id}/deploy` — 部署

**行为**：

根据 `transport` 执行不同部署逻辑：

**cloudflare_tunnel**：
1. 检查源主机 CF Tunnel 是否存在且 `apply_status=running`
2. 若无隧道，自动创建（复用 `runManagedTunnelDeploy`）
3. 设置 `apply_status=deploying`
4. 调用 `syncTunnelIngress(serverID)》 更新 ingress
5. 成功 → `apply_status=running`；失败 → `apply_status=failed`

**tcp_relay**：
1. 检查入口主机在线且 Agent 支持 `tcp_forwarder_v1`
2. 分配端口（55655-60655 查可用）
3. 设置 `apply_status=deploying`
4. 通过 task 53 发命令给入口主机 Agent：listen 端口
5. 通过 task 51 或 task 53 发命令给源主机 Agent：建立反向隧道
6. 成功 → `apply_status=running`；失败 → 回滚端口分配

**响应**（202 Accepted）：

```json
{
    "success": true,
    "data": {
        "task_id": "task_xxxx",
        "status": "deploying"
    }
}
```

#### 4.3.6 `POST /api/server/forward/{id}/stop` — 停止

**行为**：
- 设置 `desired_status=stopped`
- **cloudflare_tunnel**：从 ingress 中移除该路径，调用 `syncTunnelIngress`
- **tcp_relay**：断开反向隧道，释放端口
- 设置 `apply_status=stopped`

#### 4.3.7 `POST /api/server/forward/{id}/start` — 启动

**行为**：
- 设置 `desired_status=running`
- 调用 `deploy` 逻辑重新部署

#### 4.3.8 `POST /api/server/forward/preflight` — 预检

**请求体**：

```json
{
    "server_id": "host-001",
    "transport": "cloudflare_tunnel",
    "local_port": 5000,
    "relay_server_id": null
}
```

**校验项**：
- **cloudflare_tunnel**：源主机 CF 集成可用 / 隧道配额 / 路径无冲突
- **tcp_relay**：入口主机 Agent 在线 / 端口区间有余量 / 端口无冲突

**响应**：

```json
{
    "success": true,
    "data": {
        "passed": true,
        "checks": [
            {"name": "源主机在线", "passed": true},
            {"name": "CF Tunnel 可用", "passed": true},
            {"name": "路径无冲突", "passed": true}
        ],
        "suggestions": {
            "tunnel_hostname": "cf-host001.example.com",
            "tunnel_path": "/fwd/fwd_xxxx"
        }
    }
}
```

### 4.4 错误码汇总

| HTTP 状态码 | 含义 | 常见场景 |
|---|---|---|
| 200 | 成功 | GET/PUT/DELETE |
| 201 | 创建成功 | POST |
| 202 | 已接受（异步任务） | POST deploy |
| 400 | 请求参数错误 | 字段校验失败 |
| 404 | 资源不存在 | server_id 不存在 |
| 409 | 冲突 | 端口重复/唯一性约束/活跃连接 |
| 422 | 前置条件不满足 | CF 隧道未部署/Agent 离线 |
| 500 | 内部错误 | 数据库错误/未预期异常 |

---

## 5. 前端设计

### 5.1 页面位置

[现有主导航] → **转发中心**（Forwarding Center）

- 一级导航项，与「订阅管理」「主机实例」「Cloudflare」并列
- 在 `src/js/store.js` 中注册模块
- 页面文件：`src/js/pages/ForwardPage.jsx`
- 组件目录：`src/js/components/forward/`

### 5.2 Zustand Store 设计

```javascript
// src/js/store.js — 新增模块注册
{
    id: 'forward',
    name: '转发中心',
    icon: '↔',  // 或使用 Kumo 图标
    path: '/forward',
    component: ForwardPage,
    visible: true,
    order: 0,
}
```

```javascript
// src/js/components/forward/useForwardStore.js — 页面状态管理
import { create } from 'zustand';

const useForwardStore = create((set, get) => ({
    // 列表状态
    forwards: [],
    total: 0,
    loading: false,
    error: null,

    // 筛选/搜索
    filter: {
        server_id: '',
        transport: '',
        apply_status: '',
        search: '',
    },
    pagination: {
        offset: 0,
        limit: 20,
    },

    // 当前操作
    editingForward: null,  // 编辑中的转发规则
    showDialog: false,     // 创建/编辑弹窗
    dialogMode: 'create',  // 'create' | 'edit'

    // 部署状态
    deploying: new Set(),  // 正在部署的 forward id 集合

    // Actions
    loadForwards: async () => { /* ... */ },
    createForward: async (data) => { /* ... */ },
    updateForward: async (id, data) => { /* ... */ },
    deleteForward: async (id) => { /* ... */ },
    deployForward: async (id) => { /* ... */ },
    stopForward: async (id) => { /* ... */ },
    startForward: async (id) => { /* ... */ },
}));
```

### 5.3 页面状态

页面有四种状态，按优先级展示：

| 状态 | 条件 | 展示 |
|---|---|---|
| **Loading** | 初次加载，`loading=true` 且 `forwards=[]` | Kumo `<Loader>` 全屏居中 |
| **Empty** | 加载完成，`forwards=[]` 且无筛选条件 | 空态插图 + 「创建你的第一条转发规则」按钮 |
| **Empty (filtered)** | 加载完成，`forwards=[]` 但有筛选条件 | 「没有匹配的转发规则，试试调整筛选条件」 |
| **Error** | `error` 不为空 | Kumo `<Alert>` 错误提示 + 「重试」按钮 |
| **Populated** | `forwards` 有数据 | 表格列表 |

### 5.4 页面布局

```
┌─────────────────────────────────────────────────────────────────────┐
│  [转发中心]  [+ 创建转发规则]                                        │
│                                                                      │
│  ┌─ 筛选栏 ───────────────────────────────────────────────────────┐ │
│  │ 传输方式: [全部 ▼]  状态: [全部 ▼]  源主机: [全部 ▼]           │ │
│  │ 搜索: [___________________________]  [搜索]                     │ │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                      │
│  ┌─ 表格 ───────────────────────────────────────────────────────────┐ │
│  │ 规则名称 │ 源主机 │ 协议 │ 传输方式 │ 访问地址 │ 状态 │ 操作   │ │
│  ├──────────────────────────────────────────────────────────────────┤ │
│  │ 调试API  │ 开发机  │ HTTP │ CF Tunnel│ https:// │ ● 运行中 │ [...] │
│  │          │         │      │          │ cf-host..│             │       │
│  ├──────────────────────────────────────────────────────────────────┤ │
│  │ SSH调试  │ 开发机  │ TCP  │ TCP中继  │ tcp://.. │ ● 运行中 │ [...] │
│  │          │         │      │          │ :55655   │             │       │
│  ├──────────────────────────────────────────────────────────────────┤ │
│  │ 数据库   │ 测试机  │ TCP  │ CF Tunnel│ tcp://.. │ ● 已断开 │ [...] │
│  │          │         │      │          │          │             │       │
│  └──────────────────────────────────────────────────────────────────┘ │
│                                                                      │
│  每行操作菜单: [编辑] [部署] [停止/启动] [删除]                       │
│                                                                      │
│  ┌─ 分页 ──────────────────────────────────────────────────────────┐ │
│  │ 共 12 条    第 1/1 页    [<] [1] [>]                            │ │
│  └──────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────┘
```

### 5.5 创建/编辑弹窗

```
┌──────────────────────────────────────────────────┐
│  [创建转发规则]                                   │
│                                                   │
│  ┌─ 基本信息 ──────────────────────────────────┐ │
│  │ 规则名称: [___________________________]     │ │
│  │ 源主机:   [▼ 开发机 (Linux 10.0.0.5)]      │ │
│  │ 本地地址: [127.0.0.1]                       │ │
│  │ 本地端口: [5000]                             │ │
│  │ 协议:     [○ HTTP  ● TCP  ○ HTTPS]         │ │
│  └─────────────────────────────────────────────┘ │
│                                                   │
│  ┌─ 传输方式 ──────────────────────────────────┐ │
│  │ [● Cloudflare Tunnel]  [○ TCP 中继]  [○ P2P]│ │
│  │                                               │ │
│  │  ┌─ CF Tunnel 选项 ───────────────────────┐  │ │
│  │  │ 隧道状态: ● 已就绪 (cf-host001.example) │  │ │
│  │  │ 对外路径: /fwd/自动生成                  │  │ │
│  │  └─────────────────────────────────────────┘  │ │
│  └─────────────────────────────────────────────┘ │
│                                                   │
│  ┌─ 访问控制 ──────────────────────────────────┐ │
│  │ [● 公开(Public)]  [○ Token认证]  [○ 面板认证]│ │
│  └─────────────────────────────────────────────┘ │
│                                                   │
│  ┌─ 预检结果 ──────────────────────────────────┐ │
│  │ ✓ 源主机在线                                  │ │
│  │ ✓ CF Tunnel 已就绪                            │ │
│  │ ✓ 路径无冲突                                  │ │
│  └─────────────────────────────────────────────┘ │
│                                                   │
│  [取消]  [创建并部署]                               │
└──────────────────────────────────────────────────┘
```

**弹窗交互逻辑**：

| 交互 | 行为 |
|---|---|
| 选择传输方式 | 下方显示对应传输方式的选项面板 |
| 切换协议 | HTTP → 显示 access_url 为 https://；TCP → 显示 access_url 为 tcp:// |
| 选择源主机 | 自动检测该主机是否有 CF Tunnel（若无则显示「需要先部署隧道」提示） |
| 输入端口后 | 自动触发预检（debounce 500ms），显示预检结果 |
| 点「创建并部署」 | 先 POST 创建，成功后自动调用 POST deploy |
| 源主机离线 | 弹出警告「该主机当前离线，部署将在主机上线后执行」 |

### 5.6 行展开详情

点击行展开箭头，显示详细面板：

```
┌──────────────────────────────────────────────────────────────────┐
│ ┌─ 调试 API ─────────────────────────────────────────────────────┐ │
│ │ 规则 ID:  fwd_3a1f5c8e9b2d047f                              │ │
│ │ 访问地址: https://cf-host001.example.com/fwd/fwd_3a1f5c8e9b2d│ │
│ │ 创建时间: 2026-08-24 10:00:00                                │ │
│ │ 更新时间: 2026-08-24 12:30:00                                │ │
│ │                                                               │ │
│ │ ┌─ 状态历史 ────────────────────────────────────────────────┐ │ │
│ │ │ 12:30:00  running  - 健康检查通过                         │ │ │
│ │ │ 12:25:00  disconnected - Cloudflare 未检测到连接           │ │ │
│ │ │ 12:20:00  deploying - 自愈重放中                          │ │ │
│ │ │ 12:15:00  running  - 部署完成                              │ │ │
│ │ │ 10:00:00  pending  - 创建成功                              │ │ │
│ │ └────────────────────────────────────────────────────────────┘ │ │
│ │                                                               │ │
│ │ [编辑] [部署] [停止] [删除]                                    │ │
│ └───────────────────────────────────────────────────────────────┘ │
└──────────────────────────────────────────────────────────────────┘
```

### 5.7 操作确认弹窗

**删除确认**（使用 Kumo `<DeleteResource>`）：

```
┌──────────────────────────────────┐
│  删除转发规则「调试API」?         │
│                                   │
│  该操作将断开所有活跃连接，       │
│  并从 Cloudflare Tunnel 中移除   │
│  该路径。                         │
│                                   │
│  当前活跃连接: 0                  │
│                                   │
│  输入「调试API」确认删除:         │
│  [________________________]      │
│                                   │
│  [取消]  [确认删除]               │
└──────────────────────────────────┘
```

**停止确认**：

```
┌──────────────────────────────────┐
│  停止转发规则「调试API」?         │
│                                   │
│  停止后该服务将无法从公网访问。   │
│  当前活跃连接将断开。             │
│                                   │
│  [取消]  [确认停止]               │
└──────────────────────────────────┘
```

### 5.8 Toast 消息

| 操作 | 成功 | 失败 |
|---|---|---|
| 创建 | 「转发规则「调试API」已创建」 | 「创建失败：xxx」 |
| 部署 | 「转发规则「调试API」部署成功」 | 「部署失败：xxx」 |
| 更新 | 「转发规则「调试API」已更新」 | 「更新失败：xxx」 |
| 删除 | 「转发规则「调试API」已删除」 | 「删除失败：xxx」 |
| 停止 | 「转发规则「调试API」已停止」 | 「停止失败：xxx」 |
| 启动 | 「转发规则「调试API」已启动」 | 「启动失败：xxx」 |

### 5.9 组件树

```
ForwardPage
├── ForwardToolbar
│   ├── FilterSelect (传输方式)
│   ├── FilterSelect (状态)
│   ├── FilterSelect (源主机)
│   ├── SearchInput
│   └── CreateButton
├── ForwardTable
│   ├── ForwardRow (×N)
│   │   ├── ForwardStatusBadge
│   │   ├── ForwardAccessURL (可点击复制)
│   │   ├── RowActions (Dropdown)
│   │   │   ├── [编辑]
│   │   │   ├── [部署] / [重试]
│   │   │   ├── [停止] / [启动]
│   │   │   └── [删除]
│   │   └── ForwardDetailPanel (行展开)
│   │       └── StatusHistory
│   └── Pagination
├── ForwardDialog (创建/编辑)
│   ├── BasicInfoForm
│   ├── TransportSelector
│   │   ├── CFTunnelOptions
│   │   ├── TCPRelayOptions
│   │   └── P2POptions (disabled)
│   ├── AccessControlSelector
│   └── PreflightResult
├── DeleteConfirmDialog (Kumo DeleteResource)
└── Toast (Kumo Toasty)
```

### 5.10 数据流

```
用户操作                    API 调用                    Zustand 更新              UI 更新
────────                    ────────                    ────────────              ──────
页面加载                    GET /api/server/forward     set forwards, total      渲染表格
                                                                                
点「创建」                  —                           set showDialog=true,    打开弹窗
                                                         dialogMode='create'
点「创建并部署」            POST /api/server/forward    set forwards.push()     表格新增行
                            POST /api/server/           set deploying.add(id)   行显示「部署中」
                              forward/{id}/deploy          
                                                                                
部署完成(轮询)              GET /api/server/forward     set forwards.update()   行状态更新
                            (或 SSE 推送)                set deploying.delete(id)
                                                                                
点「删除」                  —                           set showDeleteConfirm   弹出确认框
确认删除                    DELETE /api/server/         set forwards.filter()   表格移除行
                              forward/{id}?cascade=1
                                                                                
点「停止」                  POST /api/server/           set forwards.update()   行状态更新
                              forward/{id}/stop
```

---

## 16. 可视化连接画布设计

### 16.1 概述

连接画布是一个**实时拓扑图**，以图形化方式展示所有转发规则的数据流路径。用户可以在画布上直观地看到：

- 源主机上有哪些服务正在被转发
- 流量经过什么传输方式（CF Tunnel / TCP 中继）
- 公网访问入口在哪里
- 哪些连接是健康的，哪些已断开
- 实时连接数

### 16.2 画布形态

```
┌─────────────────────────────────────────────────────────────────┐
│  转发中心  [列表视图] [● 画布视图]  [+ 创建转发规则]            │
│                                                                  │
│  ┌─────────────────────────────────────────────────────────────┐ │
│  │  [+ 缩放] [−]  [适应画布]  [显示: 全部]  [自动刷新 30s]    │ │
│  │                                                             │ │
│  │  ┌──────────┐         ┌────────────┐        ┌───────────┐  │ │
│  │  │ 开发机    │────HTTP─│ CF Tunnel  │──HTTPS─│ 公网用户   │  │ │
│  │  │ localhost:│         │ cf-host001 │        │           │  │ │
│  │  │  5000     │         │   .com     │        │ https://  │  │ │
│  │  │  ● 运行中  │         │  ● 已连接  │        │  .../fwd  │  │ │
│  │  │           │         │            │        │           │  │ │
│  │  │  localhost:│────TCP──│ 中继入口   │──TCP───│ SSH 客户端│  │ │
│  │  │  22       │         │  10.0.0.1  │        │           │  │ │
│  │  │  ● 运行中  │         │  :55655    │        │ ssh://... │  │ │
│  │  │           │         │  ● 3 连接  │        │           │  │ │
│  │  └──────────┘         └────────────┘        └───────────┘  │ │
│  │                                                             │ │
│  │                    ┌──────────────┐                         │ │
│  │  ┌──────────┐      │  P2P 直连    │     ┌───────────┐      │ │
│  │  │ 测试机    │──────│  (预留)      │─────│ 开发同事   │      │ │
│  │  │ localhost:│      │  未启用      │     │           │      │ │
│  │  │  3306    │      └──────────────┘     │           │      │ │
│  │  │  ● 已停止  │                          │           │      │ │
│  │  └──────────┘                          └───────────┘      │ │
│  │                                                             │ │
│  └─────────────────────────────────────────────────────────────┘ │
│                                                                  │
│  [图例: ● 运行中  ● 已断开  ● 已停止  ● 部署中  ● 连接数]     │
└─────────────────────────────────────────────────────────────────┘
```

### 16.3 节点类型

画布上有三种节点类型，每种用不同颜色和形状区分：

| 节点类型 | 形状 | 颜色 | 内容 |
|---|---|---|---|
| **源主机节点** | 圆角矩形（服务器图标） | 蓝（`#3B82F6`） | 主机名、IP、在线状态 |
| **服务节点** | 小圆角矩形（端口图标） | 绿（`#10B981`） | 端口号、协议、运行状态 |
| **传输方式节点** | 菱形（中继/隧道图标） | 橙（CF）/紫（中继） | 传输方式名、入口地址、连接数 |
| **访问目标节点** | 圆角矩形（地球图标） | 靛蓝（`#6366F1`） | 访问地址、访问方式 |

### 16.4 边（连接线）类型

| 边类型 | 线型 | 颜色 | 说明 |
|---|---|---|---|
| 活跃连接 | 实线 | 绿（`#10B981`） | 数据传输正常 |
| 已断开 | 虚线 | 红（`#EF4444`） | 连接中断 |
| 已停止 | 点线 | 灰（`#9CA3AF`） | 用户主动停止 |
| 部署中 | 动画虚线 | 蓝（`#3B82F6`） | 正在部署，有流动动画 |
| 高流量 | 粗实线 | 橙（`#F59E0B`） | 连接数 > 阈值 |

### 16.5 节点上的交互元素

**源主机节点**：
```
┌─────────────────────────┐
│  [💻] 开发机             │  ← 主机名
│  10.0.0.5               │  ← IP 地址
│  ● 在线                  │  ← 状态指示
│  ┌──────┐ ┌──────┐      │
│  │ :5000 │ │ :22  │      │  ← 服务端口块（可点击展开）
│  │ ●运行中│ │ ●运行中│    │
│  └──────┘ └──────┘      │
│  展开 [▼]               │  ← 展开按钮，点击显示该主机全部转发规则
└─────────────────────────┘
```

**传输方式节点**：
```
      ╱▔▔▔▔╲
     ╱  CF   ╲
    ╱  Tunnel ╲
    ╲  ● 已连接 ╱
     ╲ 3 连接 ╱
      ╲▁▁▁▁╱
```

**访问目标节点**：
```
┌─────────────────────────┐
│  [🌐] 公网用户           │
│  https://cf-host001     │  ← 完整访问 URL
│  .com/fwd/fwd_xxxx      │
│  访问方式: 公开          │  ← 访问控制模式
│  [复制链接]              │  ← 快捷操作
└─────────────────────────┘
```

### 16.6 交互模式

| 交互 | 触发 | 响应 |
|---|---|---|
| **拖拽节点** | 鼠标拖拽节点头部 | 节点跟随移动，连线自动更新 |
| **缩放** | 滚轮 / 工具条 +/- 按钮 | 画布缩放，范围 0.25x-4x |
| **平移** | 拖拽空白区域 / 鼠标中键 | 画布平移 |
| **适应画布** | 工具条按钮 | 自动计算缩放比例，使所有节点可见 |
| **点击节点** | 单击 | 节点高亮，显示信息浮窗 |
| **双击节点** | 双击 | 跳转到该转发规则的详情面板 |
| **悬停连线** | 鼠标悬停 | 显示连接路径信息（延迟、吞吐、连接数） |
| **框选** | Shift + 拖拽 | 框选多个节点，支持批量操作 |
| **右键菜单** | 右键节点 | 显示快捷操作：编辑、部署、停止、删除 |

### 16.7 信息浮窗（Tooltip / Popover）

**悬停源主机节点**：
```
┌──────────────────────────────────┐
│  开发机                          │
│  IP: 10.0.0.5                   │
│  Agent: ● 在线                  │
│  CF Tunnel: ● 已部署             │
│  转发规则: 2 条（1 运行中）      │
└──────────────────────────────────┘
```

**悬停服务节点**：
```
┌──────────────────────────────────┐
│  localhost:5000                  │
│  协议: HTTP                      │
│  传输方式: Cloudflare Tunnel     │
│  状态: ● 运行中                  │
│  访问地址: https://cf-host...    │
│  创建时间: 2026-08-24 10:00:00  │
│  [编辑] [停止] [删除]            │
└──────────────────────────────────┘
```

**悬停传输方式节点**：
```
┌──────────────────────────────────┐
│  Cloudflare Tunnel               │
│  入口: cf-host001.example.com    │
│  状态: ● 已连接                  │
│  活跃连接数: 3                   │
│  总连接数: 42                    │
│  最后健康检查: 30s 前            │
└──────────────────────────────────┘
```

**悬停连线**：
```
┌──────────────────────────────────┐
│  HTTP → CF Tunnel                │
│  状态: ● 运行中                  │
│  延迟: 12ms                      │
│  吞吐: 1.2 MB/s                  │
│  活跃连接: 3                     │
└──────────────────────────────────┘
```

### 16.8 布局算法

画布使用**分层布局（Layered Layout）**：

```
左列（源主机层）      中列（传输层）        右列（访问层）
┌──────────┐         ┌────────────┐        ┌───────────┐
│  源主机1  │─────────│ CF Tunnel  │────────│  公网     │
│  :5000   │         │            │        │           │
│  :22     │─────────│ 中继入口   │────────│  SSH 客户端│
└──────────┘         └────────────┘        └───────────┘
┌──────────┐         ┌────────────┐        ┌───────────┐
│  源主机2  │─────────│ P2P(预留)  │────────│  同事     │
│  :3306   │         └────────────┘        └───────────┘
└──────────┘
```

**布局规则**：
1. 所有源主机在左列，按主机名垂直排列
2. 同一主机的服务节点在主机节点内水平排列
3. 传输方式在中列，按类型分组
4. 访问目标在右列，按类型分组
5. 连线从源主机服务端口 → 传输方式 → 访问目标
6. 跨列连线使用贝塞尔曲线（`<path d="M... C..."/>`），避免重叠
7. 节点数量变化时重新计算布局（动画过渡）

### 16.9 实时更新

画布支持半实时刷新，不需要 WebSocket：

| 数据 | 刷新方式 | 间隔 |
|---|---|---|
| 转发规则列表 | 轮询 `GET /api/server/forward` | 30 秒 |
| 连接数 | 轮询同上（`connector_count` 字段） | 30 秒 |
| 状态变化 | 轮询对比上次状态，触发动画 | 30 秒 |
| 节点位置 | 仅布局变化时重新计算 | 拖拽/增删时 |

**状态变化动画**：
- 运行中 → 已断开：连线渐变为红色虚线，节点闪烁 2 次
- 部署中 → 运行中：连线从蓝色动画虚线渐变为绿色实线
- 新增规则：节点从透明淡入，连线从无到有展开

### 16.10 实现方案

**零新增依赖**：复用现有 `SchedulerPage.jsx` 的 `WorkflowCanvas` 模式——纯 SVG + React 定位。

**核心组件**：

```jsx
// src/js/components/forward/ForwardCanvas.jsx
//
// 结构：
// <ForwardCanvas>
//   <canvas ref={containerRef}>
//     <svg>  ← 连线层（贝塞尔曲线）
//       <path d="M..." className="edge running" />
//       <path d="M..." className="edge disconnected" />
//     </svg>
//     <div>  ← 节点层（绝对定位）
//       <SourceHostNode host={host} />
//       <TransportNode transport={transport} />
//       <AccessNode access={access} />
//     </div>
//   </canvas>
//   <CanvasControls />  ← 缩放/平移/适应按钮
//   <CanvasLegend />    ← 图例
// </ForwardCanvas>
```

**组件分解**：

| 组件 | 文件 | 职责 |
|---|---|---|
| `ForwardCanvas` | `forward/ForwardCanvas.jsx` | 画布容器：缩放/平移/布局计算 |
| `SourceHostNode` | `forward/canvas/SourceHostNode.jsx` | 源主机节点（含服务端口块） |
| `TransportNode` | `forward/canvas/TransportNode.jsx` | 传输方式节点（菱形/图标） |
| `AccessNode` | `forward/canvas/AccessNode.jsx` | 访问目标节点 |
| `CanvasEdge` | `forward/canvas/CanvasEdge.jsx` | 连线（SVG path + 动画） |
| `CanvasControls` | `forward/canvas/CanvasControls.jsx` | 缩放/平移/适应按钮 |
| `CanvasLegend` | `forward/canvas/CanvasLegend.jsx` | 图例 |
| `NodeTooltip` | `forward/canvas/NodeTooltip.jsx` | 节点信息浮窗 |
| `useCanvasLayout` | `forward/canvas/useCanvasLayout.js` | 布局计算 Hook |
| `useCanvasInteraction` | `forward/canvas/useCanvasInteraction.js` | 拖拽/缩放/平移 Hook |

**复用现有代码模式**：

| 功能 | 参考来源 | 复用方式 |
|---|---|---|
| 缩放/平移 | `SchedulerPage.jsx` L641-695 | 直接复用滚轮缩放 + 鸟瞰图逻辑 |
| 贝塞尔曲线连线 | `SchedulerPage.jsx` L555-580 | 复用 `buildWorkflowCanvasLayout` 的 S 形曲线算法 |
| 节点绝对定位 | `SchedulerPage.jsx` L582-749 | 复用 `<div style={{position:'absolute', left, top}}>` 模式 |
| 状态着色 | `SchedulerPage.jsx` 节点状态类名 | 复用 `.node-running` / `.node-failed` 等 CSS class |
| 拖拽平移 | `SchedulerPage.jsx` L693-695 | 复用 `useDraggableScroll` |

### 16.11 数据转换

从 API 返回的 `ManagedForward[]` 到画布 `nodes` / `edges` 的转换逻辑：

```javascript
// useCanvasLayout.js — 核心转换函数

function buildCanvasGraph(forwards, hosts) {
    const nodes = [];
    const edges = [];

    // 1. 按源主机分组
    const hostGroups = groupBy(forwards, 'server_id');

    // 2. 为每个源主机创建主机节点
    for (const [serverId, fwds] of Object.entries(hostGroups)) {
        const host = hosts.find(h => h.id === serverId);
        const hostNode = {
            id: `host-${serverId}`,
            type: 'source-host',
            x: HOST_COLUMN_X,
            y: auto, // 由布局算法计算
            data: {
                name: host?.name || serverId,
                ip: host?.host,
                online: host?.online,
                forwards: fwds,
            }
        };
        nodes.push(hostNode);

        // 3. 为每个转发规则创建边
        for (const fwd of fwds) {
            const transportNodeId = `transport-${fwd.id}`;
            const accessNodeId = `access-${fwd.id}`;

            // 传输方式节点
            nodes.push({
                id: transportNodeId,
                type: 'transport',
                x: TRANSPORT_COLUMN_X,
                y: auto,
                data: {
                    transport: fwd.transport,
                    status: fwd.apply_status,
                    connectorCount: fwd.connector_count,
                    hostname: fwd.tunnel_hostname,
                    port: fwd.remote_port,
                }
            });

            // 访问目标节点
            nodes.push({
                id: accessNodeId,
                type: 'access',
                x: ACCESS_COLUMN_X,
                y: auto,
                data: {
                    url: fwd.access_url,
                    mode: fwd.access_mode,
                    protocol: fwd.protocol,
                }
            });

            // 服务 → 传输 连线
            edges.push({
                id: `edge-${fwd.id}-svc-trans`,
                source: `host-${serverId}`,
                target: transportNodeId,
                label: fwd.protocol,
                status: fwd.apply_status,
                connectorCount: fwd.connector_count,
            });

            // 传输 → 访问 连线
            edges.push({
                id: `edge-${fwd.id}-trans-acc`,
                source: transportNodeId,
                target: accessNodeId,
                label: fwd.transport === 'cloudflare_tunnel' ? 'HTTPS' : 'TCP',
                status: fwd.apply_status,
            });
        }
    }

    // 4. 计算 Y 坐标（分层垂直排列，避免重叠）
    return layoutNodes(nodes, edges);
}
```

### 16.12 与列表视图的切换

```
┌─────────────────────────────────────┐
│  转发中心  [● 列表视图] [画布视图]   │
│                                      │
│  默认显示列表视图，方便管理操作       │
│  点击「画布视图」切换到拓扑图         │
│  切换时保留当前筛选条件               │
│  状态存储在 Zustand 中                │
│  （viewMode: 'table' | 'canvas'）    │
└─────────────────────────────────────┘
```

### 16.13 空状态和加载状态

**空状态**（无转发规则时）：
```
┌─────────────────────────────────────┐
│                                     │
│         [画布图标 - 灰色]            │
│      还没有转发规则                  │
│  创建一条转发规则，这里将有拓扑图     │
│                                     │
│         [+ 创建第一条转发规则]       │
│                                     │
└─────────────────────────────────────┘
```

**加载状态**：
```
┌─────────────────────────────────────┐
│                                     │
│           [Kumo Loader]             │
│         正在加载拓扑...              │
│                                     │
└─────────────────────────────────────┘
```

### 16.14 实现阶段

| 阶段 | 内容 | 文件 |
|---|---|---|
| P1.1 | 基础画布容器（缩放/平移/空状态） | `ForwardCanvas.jsx`、`useCanvasInteraction.js` |
| P1.2 | 布局算法 + 数据转换 | `useCanvasLayout.js` |
| P1.3 | 节点渲染（源主机节点 + 传输方式节点 + 访问目标节点） | `SourceHostNode.jsx`、`TransportNode.jsx`、`AccessNode.jsx` |
| P1.4 | 连线渲染（SVG 贝塞尔曲线 + 状态着色） | `CanvasEdge.jsx` |
| P1.5 | 交互（拖拽节点/悬停浮窗/双击跳转） | `NodeTooltip.jsx`、`useCanvasInteraction.js` |
| P1.6 | 视图切换 + 自动刷新 + 动画 | ForwardPage 集成 |
| P2 | 实时连接数更新（轮询） | 复用现有轮询逻辑 |
| P3 | 状态变化动画（连线渐变/闪烁） | CSS transition + animation |

### 6.1 新增 task type

```rust
// agent-rust/src/protocol.rs
const EVENT_DASHBOARD_TASK: &str = "dashboard:task";

// 现有:
// 50 = proxy.runtime
// 51 = cloudflared
// 52 = self_uninstall

// 新增:
// 53 = tcp_forwarder
```

### 6.2 新增模块 `src/tcp_forwarder.rs`

```rust
// ============ 数据结构 ============

/// 面板下发的任务 payload
#[derive(Debug, Deserialize)]
struct TcpForwarderRequest {
    operation: String,           // "install" | "remove" | "status"
    forward_id: String,          // 转发规则 ID
    relay_host: String,          // 入口主机地址
    relay_port: u16,             // 入口主机端口
    local_host: String,          // 本地服务地址
    local_port: u16,             // 本地服务端口
}

/// 状态上报
#[derive(Debug, Serialize)]
struct TcpForwarderStatus {
    forward_id: String,
    connected: bool,             // 隧道是否建立
    connector_count: usize,      // 当前活跃客户端连接数
    uptime_seconds: u64,         // 隧道已存活时间
}

// ============ 实现 ============

/// 入口函数，由 task 分发器调用
pub fn reconcile(raw: &str) -> Result<String, String> {
    let request: TcpForwarderRequest = serde_json::from_str(raw)
        .map_err(|e| format!("invalid tcp_forwarder request: {e}"))?;
    match request.operation.trim().to_lowercase().as_str() {
        "install" => install(&request),
        "remove" => remove(&request.forward_id),
        "status" => status(&request.forward_id),
        _ => Err(format!("unknown operation: {}", request.operation)),
    }
}

/// 建立反向隧道到入口主机
fn install(request: &TcpForwarderRequest) -> Result<String, String> {
    // 1. 建立到入口主机的 TCP 连接
    let addr = format!("{}:{}", request.relay_host, request.relay_port);
    let mut stream = TcpStream::connect(&addr)
        .map_err(|e| format!("connect to relay {addr}: {e}"))?;

    // 2. 发送协议头 (forward_id 长度 + forward_id)
    let id_bytes = request.forward_id.as_bytes();
    let len_bytes = (id_bytes.len() as u32).to_be_bytes();
    stream.write_all(&len_bytes)
        .map_err(|e| format!("send forward_id header: {e}"))?;
    stream.write_all(id_bytes)
        .map_err(|e| format!("send forward_id: {e}"))?;

    // 3. 读取确认
    let mut ack = [0u8; 4];
    stream.read_exact(&mut ack)
        .map_err(|e| format!("read relay ack: {e}"))?;
    let status = u32::from_be_bytes(ack);
    if status != 0 {
        return Err(format!("relay rejected forward_id: status={status}"));
    }

    // 4. 注册到全局隧道管理器
    TUNNEL_MANAGER.register(request.forward_id.clone(), stream);

    Ok(serde_json::json!({"status": "connected", "forward_id": request.forward_id}).to_string())
}

/// 断开反向隧道
fn remove(forward_id: &str) -> Result<String, String> {
    TUNNEL_MANAGER.unregister(forward_id);
    Ok(serde_json::json!({"status": "removed", "forward_id": forward_id}).to_string())
}

/// 查询状态
fn status(forward_id: &str) -> Result<String, String> {
    let state = TUNNEL_MANAGER.status(forward_id);
    Ok(serde_json::to_string(&state).unwrap_or_default())
}
```

### 6.3 全局隧道管理器

```rust
// agent-rust/src/tcp_forwarder.rs

use std::collections::HashMap;
use std::sync::Mutex;
use tokio::io::{copy_bidirectional, AsyncReadExt, AsyncWriteExt};
use tokio::net::TcpStream;
use tokio::sync::oneshot;

lazy_static! {
    static ref TUNNEL_MANAGER: TunnelManager = TunnelManager::new();
}

struct TunnelState {
    forward_id: String,
    stream: TcpStream,              // 到入口主机的隧道连接
    connector_count: Arc<AtomicUsize>,
    started_at: Instant,
    shutdown_tx: Option<oneshot::Sender<()>>,
}

struct TunnelManager {
    tunnels: Mutex<HashMap<String, Arc<Mutex<TunnelState>>>>,
}

impl TunnelManager {
    fn new() -> Self {
        TunnelManager { tunnels: Mutex::new(HashMap::new()) }
    }

    fn register(&self, forward_id: String, stream: TcpStream) {
        let (shutdown_tx, shutdown_rx) = oneshot::channel();
        let state = Arc::new(Mutex::new(TunnelState {
            forward_id: forward_id.clone(),
            stream,
            connector_count: Arc::new(AtomicUsize::new(0)),
            started_at: Instant::now(),
            shutdown_tx: Some(shutdown_tx),
        }));

        // 启动心跳保活任务
        let state_clone = state.clone();
        tokio::spawn(async move {
            let mut interval = tokio::time::interval(Duration::from_secs(30));
            loop {
                tokio::select! {
                    _ = interval.tick() => {
                        // 发送心跳包 (空数据)
                        let mut stream = state_clone.lock().unwrap().stream.try_clone().unwrap();
                        if stream.write_all(b"\x00\x00\x00\x00").await.is_err() {
                            break;
                        }
                    }
                    _ = &mut tokio::time::timeout(Duration::from_secs(60), async {
                        let _ = shutdown_rx.await;
                    }) => {
                        break;
                    }
                }
            }
        });

        self.tunnels.lock().unwrap().insert(forward_id, state);
    }

    fn unregister(&self, forward_id: &str) {
        if let Some(state) = self.tunnels.lock().unwrap().remove(forward_id) {
            if let Some(tx) = state.lock().unwrap().shutdown_tx.take() {
                let _ = tx.send(());
            }
        }
    }

    fn status(&self, forward_id: &str) -> TcpForwarderStatus {
        let tunnels = self.tunnels.lock().unwrap();
        if let Some(state) = tunnels.get(forward_id) {
            let s = state.lock().unwrap();
            TcpForwarderStatus {
                forward_id: forward_id.to_string(),
                connected: true,
                connector_count: s.connector_count.load(Ordering::Relaxed),
                uptime_seconds: s.started_at.elapsed().as_secs(),
            }
        } else {
            TcpForwarderStatus {
                forward_id: forward_id.to_string(),
                connected: false,
                connector_count: 0,
                uptime_seconds: 0,
            }
        }
    }
}
```

### 6.4 入口主机侧 Agent 新增逻辑

入口主机 Agent 需要：
1. 收到 task 53 `"install"` 时，在指定端口启动 `TcpListener`
2. 接受客户端连接后，等待源主机建立反向隧道
3. 将客户端连接和反向隧道进行 `copy_bidirectional` 桥接

```rust
// 入口主机侧的 listen 逻辑
async fn listen_and_bridge(relay_port: u16, forward_id: String) -> Result<(), String> {
    let addr = format!("0.0.0.0:{relay_port}");
    let listener = TcpListener::bind(&addr)
        .map_err(|e| format!("bind {addr}: {e}"))?;

    // 等待源主机建立反向隧道
    let tunnel_stream = wait_for_tunnel(forward_id).await?;

    loop {
        let (client_stream, _) = listener.accept().await
            .map_err(|e| format!("accept: {e}"))?;

        let tunnel = tunnel_stream.try_clone()
            .map_err(|e| format!("clone tunnel: {e}"))?;

        tokio::spawn(async move {
            let _ = copy_bidirectional(&mut client_stream, &mut tunnel).await;
        });
    }
}
```

### 6.5 能力声明

```rust
// agent-rust/src/main.rs
fn agent_capabilities() -> HashMap<String, bool> {
    let mut caps = HashMap::new();
    // ... 现有能力 ...
    caps.insert("tcp_forwarder_v1".to_string(), true);
    caps
}
```

### 6.6 task 分发器

```rust
// agent-rust/src/main.rs — EVENT_DASHBOARD_TASK handler
tokio::spawn(async move {
    match task.task_type {
        1  => { /* COMMAND */ }
        50 => proxy_runtime::reconcile(&task.data),
        51 => cloudflared::reconcile(&task.data),
        52 => schedule_self_uninstall(),
        53 => tcp_forwarder::reconcile(&task.data),  // <-- 新增
        _ => error!("不支持的任务类型"),
    }
});
```

---

## 7. TCP 中继器二进制设计

### 7.1 概述

`api-monitor-relay` 是一个极简的 Go TCP 中继守护进程，运行在入口主机上。它由 Agent 下载并管理（复用 `proxy_runtime.rs` 的二进制生命周期模式）。

### 7.2 完整代码结构

```go
// cmd/api-monitor-relay/main.go
package main

import (
    "encoding/binary"
    "encoding/json"
    "flag"
    "io"
    "log"
    "net"
    "net/http"
    "os"
    "os/signal"
    "sync"
    "sync/atomic"
    "syscall"
)

var (
    listenAddr = flag.String("listen", ":8080", "管理 API 监听地址")
    configFile = flag.String("config", "/etc/api-monitor-relay/config.json", "配置文件路径")
)

type RelayConfig struct {
    Forwards []ForwardConfig `json:"forwards"`
}

type ForwardConfig struct {
    ID          string `json:"id"`
    ListenPort  int    `json:"listen_port"`
    TunnelAddr  string `json:"tunnel_addr"`  // 源主机反向连接地址(由面板协调)
}

type RelayServer struct {
    mu       sync.Mutex
    forwards map[string]*ForwardHandler
    stats    RelayStats
}

type RelayStats struct {
    TotalConnections atomic.Int64
    ActiveConnections atomic.Int64
}

type ForwardHandler struct {
    config    ForwardConfig
    listener  net.Listener
    tunnel    net.Conn
    stats     *RelayStats
    closeCh   chan struct{}
}

func (h *ForwardHandler) serve() {
    for {
        conn, err := h.listener.Accept()
        if err != nil {
            select {
            case <-h.closeCh:
                return
            default:
                log.Printf("accept error: %v", err)
                continue
            }
        }
        h.stats.TotalConnections.Add(1)
        h.stats.ActiveConnections.Add(1)
        go h.handleConn(conn)
    }
}

func (h *ForwardHandler) handleConn(client net.Conn) {
    defer client.Close()
    defer h.stats.ActiveConnections.Add(-1)

    // 等待隧道连接就绪（或使用已有隧道）
    tunnel := h.getTunnel()
    if tunnel == nil {
        return
    }

    // 双向桥接
    var wg sync.WaitGroup
    wg.Add(2)
    go func() {
        defer wg.Done()
        io.Copy(tunnel, client)
    }()
    go func() {
        defer wg.Done()
        io.Copy(client, tunnel)
    }()
    wg.Wait()
}

func (h *ForwardHandler) getTunnel() net.Conn {
    h.mu.Lock()
    defer h.mu.Unlock()
    return h.tunnel
}

func (h *ForwardHandler) setTunnel(conn net.Conn) {
    h.mu.Lock()
    defer h.mu.Unlock()
    if h.tunnel != nil {
        h.tunnel.Close()
    }
    h.tunnel = conn
}

func main() {
    flag.Parse()

    // 读配置
    data, err := os.ReadFile(*configFile)
    if err != nil {
        log.Fatalf("read config: %v", err)
    }
    var config RelayConfig
    json.Unmarshal(data, &config)

    srv := &RelayServer{
        forwards: make(map[string]*ForwardHandler),
    }

    // 启动每个转发规则的 listener
    for _, fwd := range config.Forwards {
        handler := &ForwardHandler{
            config:  fwd,
            closeCh: make(chan struct{}),
            stats:   &srv.stats,
        }
        listener, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", fwd.ListenPort))
        if err != nil {
            log.Printf("listen port %d: %v", fwd.ListenPort, err)
            continue
        }
        handler.listener = listener
        srv.forwards[fwd.ID] = handler
        go handler.serve()
    }

    // 管理 API
    http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        json.NewEncoder(w).Encode(map[string]interface{}{
            "status": "running",
            "forwards": len(srv.forwards),
            "active_connections": srv.stats.ActiveConnections.Load(),
            "total_connections": srv.stats.TotalConnections.Load(),
        })
    })

    // 信号处理
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    go func() {
        <-sigCh
        for _, h := range srv.forwards {
            close(h.closeCh)
            h.listener.Close()
        }
        os.Exit(0)
    }()

    log.Printf("api-monitor-relay started, listen=%s", *listenAddr)
    log.Fatal(http.ListenAndServe(*listenAddr, nil))
}
```

### 7.3 构建与部署

```makefile
# 构建
build-relay:
    cd cmd/api-monitor-relay && GOOS=linux GOARCH=amd64 go build -o api-monitor-relay
    sha256sum api-monitor-relay > api-monitor-relay.sha256

# 发布
release-relay:
    # 上传到面板可访问的存储
    cp api-monitor-relay /var/www/relay/
```

部署方式：与 cloudflared 相同的模式（`cloudflaredTaskPayload` 风格）：
- 面板下发 task 给入口主机 Agent
- Agent 下载二进制、SHA-256 校验、写配置、启动 systemd 服务
- 健康检查：`systemctl is-active api-monitor-relay`

---

## 8. 安全设计

### 8.1 Token 认证（Phase 3）

**Token 生成**：
- 使用 `crypto/rand` 生成 32 字节随机数
- Base62 编码为 43 字符的 token（`am_` 前缀，如 `am_3f8a...`）
- 存入数据库时使用 `secure.SecureEncrypt` 加密

**Token 验证**：
- 客户端连接时在 TCP 连接建立后首先发送 token
- 格式：`[4 字节 BE token 长度] + [token 明文]` 或 HTTP 头 `Authorization: Bearer am_xxx`
- 入口主机收到后，向面板验证（或本地缓存 + 定期刷新）

**Token 撤销**：
- 用户在前端点「重置 token」→ 面板生成新 token → 加密入库
- 旧 token 立即失效

### 8.2 面板认证代理（Phase 3）

**原理**：面板作为反向代理，对客户端进行会话认证后再转发到目标服务。

**数据流**：

```
客户端 ──HTTPS──> 面板(反向代理) ──隧道──> 源主机
```

**限制**：面板成为流量中转，性能受限于面板所在服务器带宽。**此模式仅适用于低流量管理场景。**

### 8.3 加密传输

- **Cloudflare Tunnel**：TLS 由 Cloudflare 边缘终止，cloudflared 到源主机之间无额外加密（HTTP 明文到 localhost）
- **TCP 中继**：隧道连接无内置加密（信任内网），如有需要可在上层应用使用 TLS
- **Token**：使用 `secure.SecureEncrypt`（AES-256-GCM）加密存储

### 8.4 端口隔离

- 代理节点端口：45654-55654
- 中继转发端口：55655-60655
- **禁止混用**，防止订阅者误连接到转发服务，或转发客户端误连到代理节点

---

## 9. 错误处理与边界情况

### 9.1 错误场景矩阵

| 场景 | 检测 | 表现 | 恢复 |
|---|---|---|---|
| 源主机 Agent 离线 | 部署时 `registry.Get` 失败 | `apply_status=failed`，`last_error="agent offline"` | 主机上线后手动重试 |
| 源主机 cloudflared 断开 | 健康循环检测到 CF 连接数为 0 | `apply_status=disconnected` | 自愈重放（现有机制） |
| 入口主机 Agent 离线 | 部署时 `registry.Get` 失败 | `apply_status=failed` | 主机上线后手动重试 |
| CF Token 过期 | CF API 返回 401 | `apply_status=failed` | 用户更新 CF 账号 |
| 端口冲突 | bind-check 失败 | 自动换端口（最多 10 次） | 全部失败时 `apply_status=failed` |
| 超过 50 条 ingress 上限 | 部署时检查 `len(ingress)≥49` | 提前返回错误，不调用 CF API | 用户删除不需要的规则 |
| 中继端口全满 | 55655-60655 全部被占用 | `apply_status=failed` | 用户删除不需要的规则 |
| 隧道连接断开 | TCP keepalive + 健康检查 | `apply_status=disconnected` | Agent 自动重连（指数退避） |
| CF 服务中断 | CF API 返回 5xx | `apply_status=failed` | 等待 CF 恢复后重试 |
| 源主机被删除 | 级联删除触发 | 转发规则自动删除 | — |
| 入口主机被删除 | 级联 SET NULL 触发 | `apply_status=failed`，`relay_server_id=NULL` | 用户更新入口主机 |
| 并发部署同一转发规则 | `createExclusiveProxyTask` 检查 | 返回 409 Conflict | 等待当前任务完成 |
| 转发规则名称重复 | 应用程序校验 | 返回 400 "name already exists" | 用户修改名称 |

### 9.2 重试策略

| 操作 | 重试时机 | 重试次数 | 间隔 |
|---|---|---|---|
| 部署（deploy） | 用户手动触发 | 不限 | — |
| 自愈重放（disconnected） | 健康循环（每 5 分钟） | 3 次 | 指数退避 5/10/15 分钟 |
| 隧道连接断开 | Agent 自动重连 | 不限 | 指数退避 1s→30s |
| CF API 调用失败 | 部署任务内部重试 | 3 次 | 3 秒间隔 |

### 9.3 回滚策略

**部署失败时回滚**：

| 传输方式 | 回滚内容 |
|---|---|
| cloudflare_tunnel | 从 ingress 中移除本次新增的路径；若本次创建了新隧道，级联删除隧道 |
| tcp_relay | 释放已分配的端口；通知入口主机关闭 listener；通知源主机断开隧道 |

---

## 10. Phase 1 实现要点

### 10.1 新增文件

| 文件 | 内容 | 风险等级 |
|---|---|---|
| `backend-go/internal/serveragent/managed_forwards.go` | 转发规则 CRUD + 部署 + 生命周期 + 健康检查 | 低（新文件） |
| `backend-go/internal/serveragent/managed_forwards_test.go` | 单元测试 | 低（新文件） |
| `src/js/pages/ForwardPage.jsx` | 转发中心主页面 | 低（新文件） |
| `src/js/components/forward/ForwardTable.jsx` | 转发规则列表 | 低（新文件） |
| `src/js/components/forward/ForwardRow.jsx` | 单行 | 低（新文件） |
| `src/js/components/forward/ForwardDialog.jsx` | 创建/编辑弹窗 | 低（新文件） |
| `src/js/components/forward/ForwardStatusBadge.jsx` | 状态徽章 | 低（新文件） |
| `src/js/components/forward/useForwardStore.js` | Zustand store | 低（新文件） |

### 10.2 修改文件

| 文件 | 改动 | 风险等级 |
|---|---|---|
| `backend-go/internal/serveragent/service.go` | 注册路由 + 表 DDL + 迁移 | 中（高风险文件，聚焦改动） |
| `backend-go/internal/serveragent/managed_tunnels.go` | `loadTunnelIngress` 追加读取 `managed_forwards` | 中 |
| `backend-go/internal/manifest/manifest.go` | 注册新路由权限 | 中（高风险文件） |
| `backend-go/internal/server/server.go` | 路由分发 | 低 |
| `backend-go/internal/system/route_descriptions.go` | 接口文档 | 低 |
| `backend-go/internal/system/route_contracts.go` | 接口契约 | 低 |
| `src/js/store.js` | 注册转发中心模块 | 中（高风险文件） |

### 10.3 函数签名

```go
// managed_forwards.go

// 路由分发
func (s *Service) handleManagedForwardRoutes(w http.ResponseWriter, r *http.Request, db *sql.DB, subparts []string)

// 列表
func (s *Service) listManagedForwards(w http.ResponseWriter, r *http.Request, db *sql.DB)

// 创建
func (s *Service) createManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB)

// 读取
func (s *Service) getManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB, id string)

// 更新
func (s *Service) updateManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB, id string)

// 删除
func (s *Service) deleteManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB, id string)

// 部署
func (s *Service) deployManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB, id string)

// 停止
func (s *Service) stopManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB, id string)

// 启动
func (s *Service) startManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB, id string)

// 预检
func (s *Service) preflightManagedForward(w http.ResponseWriter, r *http.Request, db *sql.DB)

// 计算 access_url
func buildAccessURL(fwd *ManagedForward) string

// 同步 CF Tunnel ingress（合并代理节点 + 转发规则）
func syncTunnelIngress(ctx context.Context, db *sql.DB, serverID string) error
```

### 10.4 不变的文件

| 文件 | 原因 |
|---|---|
| `agent-rust/src/cloudflared.rs` | Phase 1 不改 Agent |
| `agent-rust/src/proxy_runtime.rs` | Phase 1 不改 Agent |
| `agent-rust/src/main.rs` | Phase 1 不改 Agent |
| `src/js/pages/SubscriptionPage.jsx` | 转发中心独立页面，不混入订阅管理 |
| `src/js/pages/ServerPage.jsx` | 不碰高风险文件 |
| `backend-go/internal/serveragent/managed_proxy_nodes.go` | 不碰代理节点逻辑 |
| `backend-go/internal/serveragent/server_ops.go` | 不碰 agent 任务下发逻辑 |

---

## 11. 分阶段规划

### Phase 1：Cloudflare Tunnel 转发（预计 3-5 天）

| 步骤 | 内容 | 文件 | 验证 |
|---|---|---|---|
| 1.1 | 数据库表 + 迁移 | `service.go` | `go test ./internal/serveragent/` |
| 1.2 | 后端 CRUD（list/create/get/update/delete） | `managed_forwards.go` | 单元测试 |
| 1.3 | 后端部署/停止/启动 + 预检 | `managed_forwards.go` | 单元测试 |
| 1.4 | 修改 `loadTunnelIngress` 合并转发规则 | `managed_tunnels.go` | 手动测试 ingress 输出 |
| 1.5 | 路由注册 + manifest + server | `service.go` / `manifest.go` / `server.go` | `npm run governance:check` |
| 1.6 | 前端 ForwardPage 表格 + 创建弹窗 | `ForwardPage.jsx` / `ForwardDialog.jsx` | `npm run lint` |
| 1.7 | 前端部署/停止/删除流程 | `ForwardRow.jsx` / `useForwardStore.js` | 手动测试 |
| 1.8 | 前端状态轮询 | `useForwardStore.js` | 手动测试 |
| 1.9 | 接口文档 + 契约 | `route_descriptions.go` / `route_contracts.go` | `node tools/backend-route-inventory.mjs` |

**交付物**：用户可在面板上创建 HTTP/TCP 转发规则，通过 CF 域名访问内网服务。

### Phase 2：TCP 中继（预计 10-14 天）

| 步骤 | 内容 | 文件 | 验证 |
|---|---|---|---|
| 2.1 | 编写 `api-monitor-relay` Go 二进制 | `cmd/api-monitor-relay/main.go` | 独立测试（2 台机器对连） |
| 2.2 | Agent 端 `tcp_forwarder.rs` | `agent-rust/src/tcp_forwarder.rs` | `cargo check` + 单元测试 |
| 2.3 | Agent 端 task 分发器 | `agent-rust/src/main.rs` | `cargo check` |
| 2.4 | Agent 端能力声明 | `agent-rust/src/main.rs` | `cargo check` |
| 2.5 | 后端入口主机管理 + 端口分配 | `managed_forwards.go` | 单元测试 |
| 2.6 | 后端 deploy 逻辑（tcp_relay 分支） | `managed_forwards.go` | 集成测试 |
| 2.7 | 前端支持 tcp_relay 传输方式 | `ForwardDialog.jsx` | 手动测试 |
| 2.8 | 转发器二进制管理（安装/升级/健康检查） | 复用 `proxy_runtime.rs` 模式 | 集成测试 |
| 2.9 | 防火墙规则 + 端口 bind-check | 复用 `proxy_runtime.rs` | 集成测试 |
| 2.10 | 健康检查 + 自动重连 | `managed_forwards.go` + `tcp_forwarder.rs` | 故障注入测试 |

**交付物**：用户可选择入口主机，将任意 TCP 服务通过中继暴露。

### Phase 3：P2P + 访问控制（预计 7-10 天）

| 步骤 | 内容 | 验证 |
|---|---|---|
| 3.1 | STUN NAT 探测 | 单元测试 |
| 3.2 | 信令交换 | 集成测试 |
| 3.3 | 打洞失败回退到 TCP 中继 | 故障注入 |
| 3.4 | Token 生成/验证/存储 | 安全测试 |
| 3.5 | Token 认证中间件（入口主机侧） | 集成测试 |
| 3.6 | 面板认证代理 | 集成测试 |
| 3.7 | 前端 Token 显示/重置 | 手动测试 |
| 3.8 | 访问控制选择器 | 手动测试 |

**交付物**：支持 P2P 直连 + 认证访问的完整转发中心。

---

## 12. 测试计划

### 12.1 单元测试

| 测试 | 文件 | 覆盖 |
|---|---|---|
| CRUD 操作 | `managed_forwards_test.go` | 创建/读取/更新/删除/列表 |
| 校验规则 | `managed_forwards_test.go` | 名称长度/端口范围/协议枚举/传输方式校验 |
| 唯一性约束 | `managed_forwards_test.go` | 端口唯一/路径唯一/主机端口组合唯一 |
| ingress 合并 | `managed_tunnels_test.go` | `loadTunnelIngress` 追加转发规则 |
| access_url 计算 | `managed_forwards_test.go` | CF Tunnel / TCP 中继的 URL 格式 |
| 状态转换 | `managed_forwards_test.go` | pending→deploying→running / running→stopped |
| 预检 | `managed_forwards_test.go` | 通过/失败场景 |
| 级联删除 | `managed_forwards_test.go` | 源主机删除后转发规则自动删除 |
| 错误响应 | `managed_forwards_test.go` | 404/400/409/422 状态码 |
| 前端 store | `useForwardStore.test.js` | load/create/update/delete 状态管理 |

### 12.2 集成测试

| 测试 | 场景 | 工具 |
|---|---|---|
| CF Tunnel 部署 | 创建转发规则 → deploy → 检查 ingress 配置 | 手动检查 CF API 响应 |
| CF Tunnel 删除 | 删除转发规则 → 检查 ingress 中路径已移除 | 手动检查 CF API 响应 |
| 并发部署 | 同时部署 2 条转发规则 → 检查 ingress 包含两条路径 | 手动检查 |
| 端口分配 | 连续创建 10 条 tcp_relay 规则 → 检查端口递增 | 单元测试 |
| Agent 在线/离线 | 源主机离线时部署 → 检查错误响应 | 单元测试 |
| 前端全流程 | 创建 → 部署 → 状态更新 → 停止 → 删除 | 手动测试 |

### 12.3 故障注入测试

| 故障 | 预期行为 | 验证方式 |
|---|---|---|
| CF API 超时 | 部署失败，重试 3 次后标 `failed` | mock CF API 返回超时 |
| 端口被占用 | 自动尝试下一个端口，全部失败则标 `failed` | mock bind-check 返回占用 |
| Agent 部署中断连 | 异步 task 超时后标 `failed` | 手动断开 agent 连接 |
| 入口主机离线 | 部署返回错误，提示用户 | 单元测试 |
| 并发操作同一规则 | 第二个请求返回 409 | 单元测试 |

---

## 13. 与现有架构的边界

| 边界 | 规则 | 违反后果 |
|---|---|---|
| 端口区间 | 代理节点：45654-55654；**中继转发：55655-60655** | 订阅者误连到转发服务 / 端口冲突 |
| 流量记账 | 转发流量**不进入**订阅账本，不参与 `traffic_reporting` | 财务数据错误 |
| 代理协议 | 转发中心**不实现** VLESS/Hysteria2 等代理协议 | 架构污染，违反 ADR-0001 |
| 运行时 | 转发中心**不管理** sing-box；TCP 中继器是独立二进制 | 功能耦合，升级困难 |
| 数据通道 | 控制信号走 Engine.IO；**数据走独立 TCP 连接** | 控制通道拥塞，Agent 断连风险 |
| 前端组件 | 使用 Kumo 组件，遵循现有设计模式 | 样式不一致，跳过代码审查 |
| 路由 manifest | 所有新路由必须在 `manifest.go` 注册 | 路由治理失效 |
| 高风险文件 | `service.go` / `manifest.go` / `server.go` / `store.js` 需聚焦改动 | 触发 CONTEXT.md 风险规则 |

---

## 14. 风险与缓解

| 风险 | 概率 | 影响 | 缓解 |
|---|---|---|---|
| 入口主机带宽不足 | 中 | 转发延迟高/丢包 | 面板显示带宽监控；用户选择合适入口；支持多入口主机 |
| 转发器进程崩溃 | 低 | 连接中断 | Agent 健康检查自动重启（复用 systemd 模式）；重启后自动重建隧道 |
| 端口冲突 | 低 | 部署失败 | 面板分配 + agent bind-check 双保险；冲突时自动换端口 |
| 未授权访问 | 中 | 服务泄露 | 访问控制（Phase 3 补 Token/面板认证）；默认 `public` 但前端提示风险 |
| UDP 不可用 | 高 | 部分服务无法转发 | Phase 1/2 明确标注仅支持 TCP/HTTP；UDP 留待 P2P 阶段 |
| Cloudflare 服务中断 | 低 | CF 隧道不可用 | 用户可切换传输方式到 TCP 中继；中止时不影响面板其余功能 |
| Agent 同时管理多个运行时 | 低 | 资源竞争 | 每个运行时独立 systemd 服务；资源隔离 |
| 转发规则达到 50 条上限 | 低 | 无法创建新规则 | 部署前检查；提示用户清理或使用 TCP 中继 |
| 面板重启后状态不一致 | 中 | `deploying` 状态卡住 | 启动时重新加载所有 `deploying` 状态；标记为 `failed` 并提示用户重试 |
| 隧道数据被中间人窃听 | 低 | 数据泄露 | TCP 中继隧道仅在内网使用；公网场景建议使用 CF Tunnel（TLS）或上层加密 |

---

## 15. 附录：参考实现

### 15.1 类似项目对比

| 项目 | 相似点 | 差异 |
|---|---|---|
| **frp** | 反向隧道 + 端口映射 | 独立二进制，不集成到面板；需要单独配置 frps/frpc |
| **ngrok** | Cloudflare Tunnel 类似 | 商业服务，不开放源码；自建需要 ngrokd |
| **bore** | 极简 TCP 隧道 | CLI 工具，无管理面板；无持久化 |
| **rathole** | Rust 实现的 TCP 反向隧道 | 类似 frp，但更轻量；可参考其协议设计 |
| **Cloudflare Tunnel** | 已集成 | 仅 HTTP/HTTPS 代理，目前未用 TCP 转发能力 |

### 15.2 参考代码模式

| 模式 | 来源文件 | 用途 |
|---|---|---|
| 二进制生命周期管理 | `agent-rust/src/proxy_runtime.rs:319-426` | TCP 转发器下载 + 校验 |
| systemd 服务管理 | `agent-rust/src/proxy_runtime.rs:460-473` | 转发器守护进程管理 |
| 防火墙规则 | `agent-rust/src/proxy_runtime.rs:476-507` | 入口主机端口开放 |
| 端口 bind-check | `agent-rust/src/proxy_runtime.rs:560-592` | 端口可用性预检 |
| 异步任务部署 | `backend-go/internal/serveragent/managed_tunnels.go:175-349` | Phase 1 deploy 异步模式 |
| 健康检查循环 | `backend-go/internal/serveragent/managed_tunnels.go:356-393` | 转发规则健康检查 |
| 自愈重放 | `backend-go/internal/serveragent/managed_tunnels.go:439-509` | disconnected 自动重试 |
| 前端状态徽章 | `src/js/pages/SubscriptionPage.jsx:43-49` | 转发规则状态展示 |
| CF Tunnel ingress 管理 | `backend-go/internal/cloudflare/managed_tunnel.go:134-156` | ingress 配置 API 调用 |

---

## 17. 高可用与故障转移设计

### 17.1 概述

当一台源主机或其上的服务失效时，系统自动将流量切换到另一台健康的主机，保证服务的持续可访问。**访问入口不变**（URL 不变），后端目标自动切换。

### 17.2 核心概念

| 术语 | 定义 |
|---|---|
| **服务组（Service Group）** | 一组运行相同服务的机器，共享同一个 `local_port` 和 `protocol` |
| **主节点（Primary）** | 当前流量指向的主机 |
| **备用节点（Standby）** | 服务组中其他运行相同服务的主机，主节点故障时接管 |
| **服务健康检查** | 对源主机 `local_host:local_port` 做 TCP 拨号检测服务是否存活 |
| **故障转移（Failover）** | 主节点健康检查失败后，自动切换到备用节点 |
| **回切（Fallback）** | 主节点恢复后，自动将流量切回（可选） |

### 17.3 数据模型变更

#### 17.3.1 managed_forwards 表新增字段

```sql
ALTER TABLE managed_forwards ADD COLUMN group_id TEXT;          -- 服务组 ID，同一组可互相接管
ALTER TABLE managed_forwards ADD COLUMN health_check_enabled INTEGER NOT NULL DEFAULT 0;  -- 是否启用健康检查
ALTER TABLE managed_forwards ADD COLUMN health_check_interval INTEGER NOT NULL DEFAULT 30; -- 健康检查间隔秒
ALTER TABLE managed_forwards ADD COLUMN health_check_timeout INTEGER NOT NULL DEFAULT 5;   -- 健康检查超时秒
ALTER TABLE managed_forwards ADD COLUMN health_check_unhealthy_threshold INTEGER NOT NULL DEFAULT 3;  -- 连续失败次数，达到后触发切换
ALTER TABLE managed_forwards ADD COLUMN health_check_healthy_threshold INTEGER NOT NULL DEFAULT 2;    -- 连续成功次数，达到后恢复
ALTER TABLE managed_forwards ADD COLUMN failover_enabled INTEGER NOT NULL DEFAULT 0;  -- 是否启用自动故障转移
ALTER TABLE managed_forwards ADD COLUMN failover_current_server_id TEXT;               -- 当前实际服务的主机 ID
ALTER TABLE managed_forwards ADD COLUMN failover_switched_at TEXT;                     -- 最近一次切换时间
ALTER TABLE managed_forwards ADD COLUMN failover_reason TEXT;                          -- 最近一次切换原因
```

#### 17.3.2 新增 managed_forward_targets 表

```sql
CREATE TABLE IF NOT EXISTS managed_forward_targets (
    id              TEXT PRIMARY KEY,
    forward_id      TEXT NOT NULL,              -- 关联转发规则
    server_id       TEXT NOT NULL,              -- 目标主机 ID
    priority        INTEGER NOT NULL DEFAULT 0, -- 优先级（0=最高，数字越大优先级越低）
    role            TEXT NOT NULL DEFAULT 'standby',  -- primary / standby / backup
    health_status   TEXT NOT NULL DEFAULT 'unknown',  -- unknown / healthy / unhealthy / offline
    last_checked_at TEXT,
    last_error      TEXT NOT NULL DEFAULT '',
    created_at      TEXT DEFAULT (datetime('now')),
    updated_at      TEXT DEFAULT (datetime('now')),
    FOREIGN KEY (forward_id) REFERENCES managed_forwards(id) ON DELETE CASCADE,
    FOREIGN KEY (server_id) REFERENCES server_accounts(id) ON DELETE CASCADE,
    UNIQUE(forward_id, server_id)
);

CREATE INDEX IF NOT EXISTS idx_forward_targets_forward ON managed_forward_targets(forward_id, priority);
```

### 17.4 健康检查机制

#### 17.4.1 检查类型

| 类型 | 方法 | 检测内容 | 适用场景 |
|---|---|---|---|
| **TCP 拨号** | `net.DialTimeout(host:port, timeout)` | 服务端口是否可连接 | 所有 TCP 协议 |
| **HTTP 探活** | `GET http://host:port/health` 或配置的路径 | HTTP 响应码 2xx/3xx | HTTP/HTTPS 协议 |
| **Agent 心跳** | Engine.IO 长连接状态 | Agent 是否在线 | 所有转发规则（基础） |

#### 17.4.2 检查流程

```
                                 ┌─────────────────────┐
                                 │  健康检查循环         │
                                 │  (每 30s 遍历所有     │
                                 │   启用了 health_check │
                                 │   的转发规则)         │
                                 └──────┬──────────────┘
                                        │
                          ┌─────────────▼─────────────┐
                          │  获取 failover_current    │
                          │  _server_id              │
                          └─────────────┬─────────────┘
                                        │
                          ┌─────────────▼─────────────┐
                          │  TCP 拨号 local_host:port │
                          │  超时: health_check_timeout │
                          └─────────────┬─────────────┘
                                        │
                    ┌───────────────────┼───────────────────┐
                    │ 成功              │ 失败              │
              ┌─────▼─────┐      ┌──────▼──────┐
              │ 连续成功+1 │      │ 连续失败+1  │
              │ 失败数=0   │      │ 成功数=0    │
              └─────┬─────┘      └──────┬──────┘
                    │                   │
              ┌─────▼─────┐      ┌──────▼──────┐
              │ >= healthy │      │ >= unhealthy│
              │ threshold? │      │ threshold?  │
              └─────┬─────┘      └──────┬──────┘
                    │ 否                │ 否
                    ▼                   ▼
              保持当前状态         继续计数
                    │ 是                │ 是
                    ▼                   ▼
              标记 healthy        触发故障转移
              (如已转移则回切)     (切换到下一台健康主机)
```

#### 17.4.3 健康检查在面板中的位置

健康检查作为独立的后台循环，与现有 `startManagedTunnelHealthLoop` 并列：

```go
// backend-go/internal/serveragent/service.go — 启动
go func() {
    defer s.backgroundWG.Done()
    s.startForwardHealthLoop(backgroundCtx)
}()
```

```go
// 循环间隔：30 秒
func (s *Service) startForwardHealthLoop(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done(): return
        case <-ticker.C:
            s.checkForwardHealth(ctx)
        }
    }
}
```

### 17.5 故障转移流程

#### 17.5.1 完整时序

```
主节点故障               面板检测到故障         面板切换目标         备用节点接管
    │                       │                    │                    │
    │ 服务崩溃              │                    │                    │
    │───进程退出───         │                    │                    │
    │                       │                    │                    │
    │                       │ TCP 拨号失败(×3)   │                    │
    │                       │<─── 30s 循环 ────  │                    │
    │                       │                    │                    │
    │                       │ 标记主节点 unhealthy │                    │
    │                       │ 查找备用节点        │                    │
    │                       │ 从 managed_forward_ │                    │
    │                       │ targets 中选优先级  │                    │
    │                       │ 最高的 healthy 节点 │                    │
    │                       │                    │                    │
    │                       │ 更新 failover_current│                   │
    │                       │ _server_id          │                    │
    │                       │                    │                    │
    │                       │ CF Tunnel 模式:    │                    │
    │                       │ 更新 ingress 配置   │                    │
    │                       │ (service 指向备用   │                    │
    │                       │ 主机的 local_port)  │                    │
    │                       │                    │                    │
    │                       │ TCP 中继模式:      │                    │
    │                       │ 通知源主机断开隧道   │                    │
    │                       │ 通知备用主机建立隧道  │                    │
    │                       │                    │<─── 建立隧道 ────  │
    │                       │                    │                    │
    │                       │ 记录切换日志        │                    │
    │                       │ 发送通知            │                    │
    │                       │                    │ 客户端连接成功      │
    │                       │                    │<─── 新连接 ──────  │
    │                       │                    │                    │
    │ 主节点恢复             │                    │                    │
    │───进程启动───          │                    │                    │
    │                       │ TCP 拨号成功(×2)   │                    │
    │                       │<─── 60s ─────────  │                    │
    │                       │                    │                    │
    │                       │ (可选回切)          │                    │
    │                       │ 更新 ingress 指向   │                    │
    │                       │ 主节点，通知备用     │                    │
    │                       │ 断开隧道            │                    │
    │<─── 重新接管 ─────────│                    │                    │
```

#### 17.5.2 切换策略

| 策略 | 说明 | 默认 |
|---|---|---|
| **自动切换** | 健康检查连续失败 `unhealthy_threshold` 次后自动切换 | 启用 |
| **自动回切** | 主节点恢复后自动切回 | 关闭（防止颠簸） |
| **手动切换** | 用户可在面板上手动触发切换 | 始终可用 |
| **手动回切** | 用户手动将流量切回主节点 | 始终可用 |

**自动回切关闭的原因**：防止主节点不稳定时反复切换（flapping）。如果主节点频繁启停，每次切换都会导致短暂中断（TCP 连接断开）。推荐用户确认主节点稳定后手动回切。

#### 17.5.3 备用节点选择算法

```
1. 从 managed_forward_targets 中筛选 forward_id=当前转发规则 的记录
2. 按 priority 升序排列
3. 排除 failover_current_server_id（当前正在服务的）
4. 排除 health_status!='healthy' 的
5. 取第一条（优先级最高且健康的）
6. 如果没有健康备用节点，发送告警通知，不切换
7. 优先级相同的，按随机顺序选择（负载均衡）
```

### 17.6 各传输方式下的故障转移实现

#### 17.6.1 Cloudflare Tunnel

```
切换前:  ingress rules →
  {"hostname":"cf-host","path":"/fwd/fwd_xxx","service":"tcp://10.0.0.5:5000"}
切换后:  ingress rules →
  {"hostname":"cf-host","path":"/fwd/fwd_xxx","service":"tcp://10.0.0.6:5000"}
```

**实现**：
- 调用 `syncTunnelIngress`，将 ingress 中的 `service` 地址从原主机 IP 改为备用主机 IP
- 访问 URL 不变（`https://cf-host.example.com/fwd/fwd_xxx`）
- 已建立的 TCP 连接会断开，新连接立即指向备用主机
- HTTP 服务：客户端重连即恢复
- 切换时间：~1-2 秒（CF API 调用 + DNS 传播）

**回切**：同理，改回 ingress 中的 `service` 地址。

#### 17.6.2 TCP 中继

```
切换前:  源主机 A(10.0.0.5:5000) ← 反向隧道 → 入口主机(:55655)
切换后:  源主机 B(10.0.0.6:5000) ← 反向隧道 → 入口主机(:55655)
```

**实现**：
1. 面板通知源主机 A 断开反向隧道（task 53 `"remove"`）
2. 面板通知源主机 B 建立反向隧道（task 53 `"install"`）
3. 入口主机上的 TcpListener 端口不变（`:55655`）
4. 入口主机收到新隧道连接后，更新桥接目标
5. 客户端连接保持：**入口端口不变，客户端无感知**

**切换时间**：~3-5 秒（任务下发 + 隧道建立 + TCP 握手）

#### 17.6.3 P2P（Phase 3）

P2P 模式下故障转移不支持（打洞是端到端的，无法中间切换）。如果主节点故障，P2P 连接断开，需等待新的打洞协商。但可回退到 TCP 中继：

```
P2P 失败 → 回退到 TCP 中继 → 使用备用节点
```

### 17.7 前端交互设计

#### 17.7.1 创建弹窗中的高可用配置

```
┌──────────────────────────────────────────────────┐
│  [创建转发规则]                                   │
│                                                  │
│  ┌─ 基本信息 ──────────────────────────────────┐ │
│  │ ...                                          │ │
│  └─────────────────────────────────────────────┘ │
│                                                  │
│  ┌─ 高可用配置 ────────────────────────────────┐ │
│  │ [● 启用健康检查]  [○ 不启用]                │ │
│  │                                              │ │
│  │ 检查间隔: [30] 秒    超时: [5] 秒           │ │
│  │ 连续失败 [3] 次后切换                        │ │
│  │ 连续成功 [2] 次后恢复                        │ │
│  │                                              │ │
│  │ [● 启用自动故障转移]                         │ │
│  │ [○ 自动回切]  (默认关闭，防止颠簸)           │ │
│  │                                              │ │
│  │ 备用主机:                                    │ │
│  │ ┌─ 列表 ──────────────────────────────────┐ │ │
│  │ │ 优先级 │ 主机      │ 状态     │ 操作    │ │ │
│  │ │ 1      │ 测试机    │ ● 在线   │ [删除] │ │ │
│  │ │        │ (10.0.0.6)│          │        │ │ │
│  │ │ 2      │ 开发机2   │ ● 在线   │ [删除] │ │ │
│  │ │        │ (10.0.0.7)│          │        │ │ │
│  │ │        │ [+ 添加备用主机]      │        │ │ │
│  │ └────────────────────────────────────────┘ │ │
│  └─────────────────────────────────────────────┘ │
│                                                  │
│  [取消]  [创建]                                   │
└──────────────────────────────────────────────────┘
```

#### 17.7.2 列表视图中的故障状态

```
│ 规则名称 │ 源主机 │ 协议 │ 传输方式 │ 访问地址 │ 状态 │ 故障转移 │ 操作 │
│──────────┼────────┼──────┼──────────┼──────────┼──────┼──────────┼──────┤
│ 调试API  │ 开发机  │ HTTP │ CF Tunnel│ https:// │ ● 运 │ ● 已切换 │ [...]│
│          │ (主)    │      │          │          │ 行中 │  → 测试机│      │
│          │         │      │          │          │      │ 12:30:00 │      │
```

**故障转移状态列**：

| 状态 | 显示 | 说明 |
|---|---|---|
| 未启用 | 灰 `未启用` | 未配置高可用 |
| 正常 | 绿 `● 正常` | 主节点健康，未发生切换 |
| 已切换 | 橙 `● 已切换 → 测试机 12:30` | 已切换到备用节点 |
| 无可用备用 | 红 `● 主节点故障 无备用` | 主节点故障，但无健康备用节点可切换 |
| 切换中 | 蓝 `● 切换中...` | 正在执行切换 |

#### 17.7.3 详情面板中的故障转移信息

行展开详情面板新增故障转移卡片：

```
┌─ 故障转移 ──────────────────────────────────────────────┐
│  状态: ● 已切换（12:30:00）                              │
│  原因: 开发机 localhost:5000 TCP 连接超时（连续 3 次失败）│
│  当前服务: 测试机 (10.0.0.6)                             │
│                                                          │
│  ┌─ 目标主机健康状态 ─────────────────────────────────┐ │
│  │ 主机      │ 优先级 │ 角色    │ 健康状态 │ 延迟    │ │
│  │──────────┼────────┼─────────┼─────────┼─────────│ │
│  │ 开发机    │ 0      │ primary │ ● 恢复中 │ 2ms     │ │
│  │ 测试机    │ 1      │ standby │ ● 健康   │ 3ms     │ │
│  │ 开发机2   │ 2      │ backup  │ ○ 离线   │ -       │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  [手动切换: ▼]  [回切到主节点]                           │
└──────────────────────────────────────────────────────────┘
```

#### 17.7.4 画布视图中的故障转移展示

```
┌──────────┐         ┌────────────┐        ┌───────────┐
│ 开发机    │         │ CF Tunnel  │        │ 公网用户   │
│ (主)      │─X── ───│   (active)  │────────│           │
│ :5000    │  断开   │            │        │           │
│ ● 故障    │         │            │        │ URL 不变  │
│           │         │            │        │           │
│ 测试机    │─────────│   (切换后)  │        │           │
│ (备)      │  实线   │            │        │           │
│ :5000    │ 绿色    │            │        │           │
│ ● 运行中  │         │            │        │           │
└──────────┘         └────────────┘        └───────────┘
```

画布上的变化：
- 主节点与服务之间连线变为**红色虚线**（已断开），加 `✕` 标记
- 备用节点与服务之间连线变为**绿色实线**（已接管），加动画流动效果
- 传输方式节点上显示 `(已切换)` 标记

#### 17.7.5 通知

故障转移事件触发系统通知：

| 事件 | 通知内容 | 通知方式 |
|---|---|---|
| 切换成功 | 「转发规则「调试API」已从 开发机 切换到 测试机」 | 站内通知 + 可选 Telegram/邮件 |
| 无可用备用 | 「转发规则「调试API」主节点故障，且无可用备用主机」 | 站内通知 + 高优先级推送 |
| 主节点恢复 | 「转发规则「调试API」主节点 开发机 已恢复」 | 站内通知 |
| 切换失败 | 「转发规则「调试API」故障转移失败：备用节点 测试机 连接失败」 | 站内通知 + 高优先级推送 |

### 17.8 后端实现

#### 17.8.1 新增文件

| 文件 | 内容 | 风险等级 |
|---|---|---|
| `backend-go/internal/serveragent/forward_health.go` | 健康检查循环 + TCP 拨号 | 低（新文件） |
| `backend-go/internal/serveragent/forward_failover.go` | 故障转移逻辑 + 备用节点选择 | 低（新文件） |
| `backend-go/internal/serveragent/forward_health_test.go` | 健康检查测试 | 低（新文件） |
| `backend-go/internal/serveragent/forward_failover_test.go` | 故障转移测试 | 低（新文件） |

#### 17.8.2 核心函数

```go
// forward_health.go

// 健康检查循环（每 30 秒）
func (s *Service) startForwardHealthLoop(ctx context.Context)

// 单次健康检查（遍历所有启用了 health_check 的转发规则）
func (s *Service) checkForwardHealth(ctx context.Context)

// 对单个目标主机执行 TCP 拨号检查
func (s *Service) checkTargetHealth(ctx context.Context, target *ManagedForwardTarget) (bool, error)

// 更新目标主机的健康状态
func (s *Service) updateTargetHealth(ctx context.Context, targetID string, healthy bool, err error)
```

```go
// forward_failover.go

// 执行故障转移（切换到备用节点）
func (s *Service) executeFailover(ctx context.Context, forwardID string, reason string) error

// 选择备用节点（按优先级 + 健康状态排序）
func (s *Service) selectFailoverTarget(ctx context.Context, forwardID string) (*ManagedForwardTarget, error)

// 执行回切
func (s *Service) executeFallback(ctx context.Context, forwardID string) error

// 更新转发规则的目标主机（CF Tunnel 更新 ingress / TCP 中继重连隧道）
func (s *Service) updateForwardTarget(ctx context.Context, forwardID, newServerID string) error
```

#### 17.8.3 修改文件

| 文件 | 改动 | 风险等级 |
|---|---|---|
| `backend-go/internal/serveragent/service.go` | 注册健康检查循环 + 表 DDL + 迁移 | 中 |
| `backend-go/internal/serveragent/managed_forwards.go` | deploy/stop 逻辑中处理 failover 字段 | 中 |
| `backend-go/internal/serveragent/managed_tunnels.go` | `syncTunnelIngress` 支持动态切换目标地址 | 中 |

### 17.9 切换时间线

```
t0:  主节点服务崩溃
t0+3s: 第一次健康检查失败（连续失败=1）
t0+33s: 第二次健康检查失败（连续失败=2）
t0+63s: 第三次健康检查失败（连续失败=3 >= threshold=3）
        → 触发故障转移
t0+63s: 面板选择备用节点
t0+63s: 更新 ingress 配置 / 通知备用节点建立隧道
t0+64s: CF API 返回成功 / 备用节点隧道建立
t0+65s: 新客户端连接到达备用节点 ✅

总切换时间: ~65 秒（可配置）

优化建议:
- 健康检查间隔从 30s 改为 10s → 切换时间缩短到 ~25s
- 使用 Agent 进程崩溃事件（Engine.IO 断连）作为触发信号 → 立即触发，无需等待 3 次轮询
```

### 17.10 颠簸保护

**Flapping Prevention**：防止主节点在恢复和故障之间反复切换。

| 机制 | 实现 |
|---|---|
| 回切默认关闭 | 主节点恢复后不回切，标记为 healthy 但保持备用 |
| 切换冷却 | 同一转发规则 5 分钟内最多切换 1 次 |
| 指数退避 | 如果切换后 5 分钟内新主节点也故障，下次切换间隔翻倍（10 分钟→20 分钟→上限 60 分钟） |
| 手动干预 | 颠簸超过 3 次后，自动切换暂停，仅发送告警通知，等待用户手动处理 |

### 17.11 实施阶段

| 阶段 | 内容 | 文件 |
|---|---|---|
| P1.1 | 数据库迁移（managed_forwards 加 failover 字段 + managed_forward_targets 表） | `service.go` |
| P1.2 | 健康检查循环（TCP 拨号 + 状态更新） | `forward_health.go` |
| P1.3 | 故障转移逻辑（备用节点选择 + ingress 更新 + 隧道切换） | `forward_failover.go` |
| P1.4 | 前端高可用配置 UI（创建弹窗扩展 + 备用主机管理） | `ForwardDialog.jsx` |
| P1.5 | 前端故障状态展示（列表列 + 详情卡片 + 画布连线） | `ForwardRow.jsx`、`ForwardCanvas.jsx` |
| P1.6 | 通知集成 | 复用现有通知通道 |
| P1.7 | 颠簸保护 + 冷却机制 | `forward_failover.go` |

### 17.12 风险与限制

| 风险 | 说明 | 缓解 |
|---|---|---|
| 服务必须是有状态的复制 | 备用主机需要运行相同的服务，且数据一致 | 转发中心只负责流量切换，不负责数据同步 |
| TCP 连接中断 | 切换时已有 TCP 连接断开 | 客户端需实现重连逻辑；HTTP 无状态服务无影响 |
| 健康检查误判 | 网络抖动导致误判为故障 | 健康检查阈值可配置（默认连续 3 次失败才切换） |
| 并发切换同一转发规则 | 两个健康检查循环同时触发切换 | 使用 `createExclusiveProxyTask` 互斥锁 |
| 备用主机也故障 | 所有备用节点都不可用 | 标记为无可用备用，持续告警，直到管理员介入 |