# 可用性监测重构 PRD

最后更新：2026-06-08

目标：将 API Monitor 的可用性监测模块重构为接近 Uptime Kuma 体验和能力的监测工作台，同时保持当前 React + Kumo、SQLite、Socket.IO、通知系统和模块化后端架构。

参考资料：

- Uptime Kuma README: https://github.com/louislam/uptime-kuma
- Uptime Kuma Status Page Wiki: https://github.com/louislam/uptime-kuma/wiki/Status-Page
- Uptime Kuma Maintenance Wiki: https://github.com/louislam/uptime-kuma/wiki/Maintenance

## Problem Statement

当前 `uptime` 模块已经有基础监测能力，但仍偏粗糙：

- 监测类型窄：后端实际只覆盖 HTTP、TCP、Ping-like；前端出现 Keyword、DNS 字段但后端未完整实现。
- 体验偏“列表 + 表单”：缺少 Uptime Kuma 式监测工作台、详情面板、事件时间线、快速测试、克隆、暂停、批量操作、状态页。
- 状态与历史不够产品化：心跳、Incident、可用率已存在，但没有统一的状态摘要、SLA 报表、证书信息、维护期展示和公共状态页。
- 告警联动不够一体化：通知系统已有规则、维护计划、静默、批量、重试能力，但 uptime 页面没有形成“监测配置 -> 通知策略 -> 维护窗口 -> 状态页”的闭环。
- 后端可扩展性不足：检查逻辑写在单个 service 里，新增 Docker、Push、DNS、JSON Query、WebSocket 等类型会继续堆积复杂度。
- 实时体验不稳定：当前前端会按 monitor 分别拉 history/rate，列表多时请求碎片化；Socket 只推 heartbeat，缺少状态变更、incident、maintenance、certificate 等事件。

用户目标是让功能和体验对标 Uptime Kuma，但界面必须符合 API Monitor 的 Kumo 规范，而不是复刻 Uptime Kuma 的 Bootstrap/Vue 视觉。

## Solution

重构为一个“监测工作台 + 检查引擎 + 状态页 + 通知联动”的完整模块：

- 前端采用 Kumo 组件构建高密度工作台：左侧监测列表/分组，右侧详情面板，底部/侧边承载事件、维护、通知、证书和历史图表。
- 后端拆成深模块：Probe Adapter、Scheduler、State Machine、Heartbeat/Incident Storage、Status Page Service、Maintenance Service、Notification Bridge、Realtime Gateway。
- 数据库从简单三张表扩展为可演进模型，保留现有 `uptime_monitors`、`uptime_heartbeats`、`uptime_incidents` 的迁移兼容。
- 先做 MVP 能力：HTTP(s)、Keyword、JSON Query、TCP、Ping、DNS、Push、状态页、维护窗口、通知联动、SLA 报表。
- P1/P2 再补 Docker Container、WebSocket、Steam Game Server、代理、Prometheus/badge、导入导出、多探针 Agent。

## Benchmark Summary

Uptime Kuma 当前公开能力包括 HTTP(s)、TCP、Keyword、JSON Query、WebSocket、Ping、DNS Record、Push、Steam Game Server、Docker Containers 等监测类型，提供响应式实时 UI、90+ 通知服务、20 秒级间隔、多个状态页、状态页域名映射、Ping 图表、证书信息、代理和 2FA。状态页面向公开用户，会缓存并定时刷新；维护窗口用于在时间范围内暂停通知并在状态页展示维护信息。

API Monitor 不需要一次性复制所有通知渠道，但需要在体验模型上对齐：监测状态清楚、配置流顺手、告警可信、历史可解释、状态页可公开。

## Goals

1. 让用户在一个页面内完成新增、测试、编辑、暂停、删除、克隆、分组、批量操作和查看历史。
2. 让每个监测项有稳定、可解释的状态：`up`、`down`、`pending`、`maintenance`、`paused`、`unknown`。
3. 支持多种主流监测类型，且新增类型不需要改动核心状态机。
4. 提供可靠的状态变更通知，只在确认故障/恢复时触发，不因抖动刷屏。
5. 支持多个公开状态页，可配置 slug、分组、监测项、品牌、维护横幅、历史展示。
6. 提供维护窗口，支持手动、一次性、Cron、按周/按月/间隔循环。
7. 提供可用率、延迟、Incident、证书到期、MTTR/MTBF 等报表。
8. 对窄屏和移动端友好：一行或两行显示核心状态，详情可压缩但不丢失关键操作。
9. 全部基础 UI 使用 Kumo：Button/Input/Select/Tabs/Table/Dialog/DeleteResource/TimeseriesChart/Meter/SkeletonLine/Tooltip 等。

## Non-goals

- 不在第一阶段实现 Uptime Kuma 的全部 90+ 通知服务；先复用现有 Email/Telegram，并保留通知渠道扩展接口。
- 不直接复刻 Uptime Kuma 的 UI 样式；本项目继续遵守 Kumo 视觉规范。
- 不在第一阶段实现分布式多地域探针，但数据模型要预留 probe/agent 字段。
- 不在第一阶段提供完整第三方 API 兼容层。
- 不在公开状态页暴露管理接口或敏感配置。

## Current State

当前代码中的基础能力：

- `modules/uptime-api/schema.sql` 已有 `uptime_monitors`、`uptime_heartbeats`、`uptime_incidents`。
- `modules/uptime-api/monitor-service.js` 已有状态机、定时检查、Socket 心跳推送、down/up 通知触发。
- `modules/uptime-api/storage.js` 已有监测 CRUD、心跳保存、Incident 创建/恢复、可用率计算。
- `src/js/pages/UptimePage.jsx` 已有列表、筛选、搜索、展开详情、Kumo TimeseriesChart、批量删除、添加/编辑表单。
- `modules/notification-api` 已有渠道、规则、历史、静默、维护计划、批量、重试、熔断、漂移渠道能力。

主要缺口：

- 前端表单字段与后端能力不一致，例如 DNS/Keyword 前端已露出但检查逻辑不完整。
- Ping 是 TCP fallback，不是真实 ICMP 或明确的 TCP ping。
- HTTP accepted status code 没有真正解析和判断，只是 `validateStatus = true`。
- 没有 JSON Query、WebSocket、Push、Docker 等监测适配器。
- 状态机状态持久化在 JSON 文件，应该进入 SQLite。
- 缺少状态页、维护页、证书信息、报表页、监测分组。
- 每个监测项单独请求 history/uptime，列表多时开销大。
- 缺少检查任务调度、并发限制、抖动、手动重试、即时测试的统一模型。

## User Stories

1. 作为管理员，我想在一个工作台中看到所有监测项的状态，所以我可以快速判断当前系统是否健康。
2. 作为管理员，我想按状态筛选监测项，所以我可以立即定位故障或等待确认的服务。
3. 作为管理员，我想按标签、分组、类型搜索监测项，所以我可以管理大量服务。
4. 作为管理员，我想新增 HTTP(s) 监测，所以我可以监控网页或 API 是否可访问。
5. 作为管理员，我想为 HTTP 监测配置 method、headers、body、认证、超时、重定向、TLS 策略，所以我可以监控真实业务接口。
6. 作为管理员，我想配置可接受状态码范围，所以 204、301 或自定义成功码不会误报。
7. 作为管理员，我想配置网页关键字监测，所以我可以确认页面内容不是错误页。
8. 作为管理员，我想配置 JSON Query 监测，所以我可以判断接口响应中的业务字段是否符合预期。
9. 作为管理员，我想配置 TCP 端口监测，所以我可以确认数据库、SSH、Redis 等端口可连接。
10. 作为管理员，我想配置 Ping 监测，所以我可以确认主机网络可达。
11. 作为管理员，我想配置 DNS Record 监测，所以我可以确认域名解析记录正确。
12. 作为管理员，我想配置 Push 监测，所以 cron、备份任务、离线任务可以主动上报存活。
13. 作为管理员，我想配置 WebSocket 监测，所以我可以确认实时接口可握手。
14. 作为管理员，我想监控 Docker 容器状态，所以我可以知道关键容器是否退出或 unhealthy。
15. 作为管理员，我想新增监测前点击“测试”，所以我可以确认配置有效再保存。
16. 作为管理员，我想克隆一个监测项，所以我可以快速创建相似目标。
17. 作为管理员，我想暂停单个或批量监测项，所以维护或迁移时不会触发告警。
18. 作为管理员，我想看到最近 N 次心跳条，所以我可以一眼判断稳定性。
19. 作为管理员，我想看到延迟趋势图，所以我可以发现性能劣化。
20. 作为管理员，我想看到 24h、7d、30d、90d 可用率，所以我可以评估 SLA。
21. 作为管理员，我想看到 Incident 时间线，所以我可以追溯每次故障开始、恢复和持续时间。
22. 作为管理员，我想看到故障原因和最近错误消息，所以我可以快速定位问题。
23. 作为管理员，我想看到证书到期时间和证书链摘要，所以我可以提前续期。
24. 作为管理员，我想配置证书到期提醒阈值，所以我可以在到期前收到通知。
25. 作为管理员，我想为每个监测项选择通知渠道，所以不同服务可以通知不同群组。
26. 作为管理员，我想配置故障确认次数和恢复确认次数，所以短暂抖动不会告警。
27. 作为管理员，我想配置重复提醒间隔，所以长时间故障可以定期提醒但不刷屏。
28. 作为管理员，我想配置维护窗口，所以计划维护不会触发故障通知。
29. 作为管理员，我想维护窗口显示在状态页，所以外部用户知道服务正在维护。
30. 作为管理员，我想维护窗口支持一次性、手动、Cron、按周、按月、间隔循环，所以我可以覆盖不同运维场景。
31. 作为管理员，我想公开状态页，所以用户可以自助查看服务状态。
32. 作为管理员，我想创建多个状态页，所以不同产品或客户可以看到不同服务集合。
33. 作为管理员，我想设置状态页 slug 和自定义域名，所以可以对外提供品牌化入口。
34. 作为管理员，我想在状态页分组服务，所以公开页面更清晰。
35. 作为管理员，我想选择状态页是否显示历史、延迟和 uptime，所以公开信息可控。
36. 作为公开用户，我想看到整体状态、服务分组、当前事件和维护公告，所以我不用登录就能了解服务状态。
37. 作为公开用户，我想状态页自动刷新，所以我能看到接近实时的状态变化。
38. 作为管理员，我想导入/导出监测配置，所以迁移部署更容易。
39. 作为管理员，我想批量删除、批量暂停、批量分组、批量修改通知渠道，所以大量监测项维护不痛苦。
40. 作为管理员，我想查看通知发送历史，所以我可以确认告警是否送达。
41. 作为管理员，我想从 Incident 跳转到通知记录，所以我可以串联故障和告警。
42. 作为管理员，我想在移动端压缩查看监测状态，所以手机排障时不需要横向来回找。
43. 作为管理员，我想所有表格一行显示并可横向滚动，所以数据密集页面不会换行变乱。
44. 作为维护者，我想每种监测类型是独立适配器，所以新增类型不会破坏现有类型。
45. 作为维护者，我想状态机可以单元测试，所以告警抖动、恢复、维护期逻辑可信。
46. 作为维护者，我想调度器有并发和超时保护，所以大量监测项不会拖垮进程。
47. 作为维护者，我想历史数据有保留策略和聚合表，所以长期运行不会让 SQLite 膨胀。
48. 作为维护者，我想公开状态页走只读 API 和缓存，所以不泄露后台敏感信息。

## Functional Requirements

### 1. 监测工作台

- 页面首屏是实际工作台，不做营销式介绍。
- 顶部使用现有 `AppPageHeader` + Kumo `Tabs`，主 tabs 建议：`监测项`、`状态页`、`维护窗口`、`事件`、`统计`、`设置`。
- `监测项` 采用左右布局：
  - 左侧：分组/标签/状态筛选、搜索、新建按钮、监测列表。
  - 右侧：选中监测详情；窄屏时详情进入抽屉或折叠区域。
- 列表项内容：
  - 状态色条、名称、类型图标、URL/host、标签、最近延迟、可用率、最近心跳条。
  - 行级操作：暂停/恢复、编辑、克隆、测试、删除。
  - 支持右键菜单：编辑、克隆、暂停、维护、复制 Push URL、删除。
- 详情内容：
  - 状态摘要卡：当前状态、持续时间、最近错误、下次检查时间。
  - 延迟趋势：Kumo `TimeseriesChart`，loading 使用 Kumo chart loading。
  - 心跳条：最近 60/120 次，tooltip 显示时间、状态、延迟、错误。
  - SLA 指标：24h、7d、30d、90d、MTTR、MTBF、Incident 数。
  - Incident 时间线：打开/恢复/维护关联/通知发送。
  - 证书信息：域名、issuer、subject、valid from/to、剩余天数、SAN 摘要。
  - 原始配置只读摘要：类型、间隔、超时、重试、通知渠道。

### 2. 监测类型

MVP 必须支持：

- HTTP(s)：method、URL、headers、body、basic/bearer auth、accepted status codes、follow redirects、ignore TLS、proxy、timeout。
- HTTP(s) Keyword：复用 HTTP 请求，增加 keyword、匹配模式 contains/not contains/regex。
- HTTP(s) JSON Query：JSONPath 或 JMESPath 风格查询、比较操作、期望值。
- TCP Port：hostname、port、timeout。
- Ping：优先系统 ping 命令；无法执行时明确显示 TCP fallback，不伪装成 ICMP。
- DNS Record：server、record type、hostname、expected value、解析耗时。
- Push：生成 tokenized URL，支持 grace period、heartbeat payload、最近 push 时间。

P1 支持：

- WebSocket：URL、headers、握手成功、可选发送/期待消息。
- Docker Container：通过本机 Docker socket 或已有 Agent 返回容器状态，支持 running/healthy/image changed。
- SSL Certificate 独立监测：证书到期、链错误、域名不匹配。

P2 支持：

- Steam Game Server。
- SMTP/IMAP/Redis/MQTT/gRPC 等协议型适配器。
- 多探针/多地域检查。

### 3. 状态机与告警

- 状态：`up`、`down`、`pending_down`、`pending_up`、`maintenance`、`paused`、`unknown`。
- 每次检查生成 heartbeat，但只有状态确认变迁时生成 incident/notification。
- `confirm_count` 分为 `downConfirmCount` 和 `upConfirmCount`，支持不同阈值。
- 支持 `retry_interval`：失败后短间隔重试，确认失败后恢复正常 interval。
- 支持 `resend_interval`：故障持续超过指定时间后重复提醒。
- 维护期内检查仍可执行，但告警被抑制，状态页显示维护。
- 暂停状态不执行检查，也不计入可用率；维护状态是否计入 SLA 由配置决定。
- 状态必须持久化到 SQLite，服务重启后恢复上一状态，不依赖 JSON 文件。

### 4. 状态页

- 支持多个状态页。
- 每个状态页有 slug、标题、描述、logo、主题、公开开关、显示选项。
- 支持按 domain 映射状态页；若没有 domain 映射则使用 `/status/:slug`。
- 支持服务分组和排序。
- 支持展示维护公告、当前事件、历史事件。
- 公开 API 只返回脱敏数据，不返回 headers/body/auth/proxy/token。
- 状态页允许缓存 60-300 秒，默认 300 秒；后台工作台仍实时。
- 提供 badge endpoint：`/api/uptime/public/badge/:monitorId` 或 status page badge。

### 5. 维护窗口

- 支持目标：全局、监测项、分组、状态页。
- 支持策略：手动启停、单次时间窗、Cron、间隔循环、按周、按月。
- 支持 timezone。
- 支持 title、description，description 可支持 Markdown。
- 维护期内：
  - 通知服务跳过对应事件。
  - 工作台显示维护标签。
  - 状态页显示维护 banner。
  - heartbeat 标记 `maintenance = true`。

### 6. 通知联动

- 复用 `notification-api` 的 channel/rule/history/config。
- Uptime 页面支持快速选择启用渠道；高级规则仍可跳转通知模块管理。
- Uptime 自动生成默认规则：
  - `uptime/down` critical 或 warning。
  - `uptime/up` info。
  - `uptime/cert_expiring` warning。
  - `uptime/push_missing` warning。
- 通知 payload 标准化：
  - monitorId、monitorName、type、target、status、error、ping、duration、incidentId、statusPageUrl。
- 支持通知模板变量预览。
- 从 monitor detail 可查看相关通知历史。

### 7. 数据统计和报表

- 每个监测项提供 24h/7d/30d/90d uptime。
- 提供全局统计：总数、正常、故障、维护、暂停、平均响应、当前 incident。
- 提供延迟百分位：p50/p95/p99。
- 提供 incident 报表：次数、总故障时长、平均恢复时间、最长故障。
- 提供按标签/分组统计。
- 提供数据保留策略：
  - raw heartbeats 默认保留 30 天。
  - daily rollup 长期保留。
  - incident 永久保留，除非用户手动清理。

### 8. 导入导出与批量操作

- 支持导出 monitors、status pages、maintenance windows。
- 支持导入 JSON，导入前预览新增/更新/冲突。
- 批量操作：暂停、恢复、删除、分组、通知渠道、维护窗口、检查频率。
- 删除确认使用 Kumo `DeleteResource`。

## Frontend Requirements

### Kumo 规范

- 基础 UI 全部使用 Kumo：`Button`、`Input`、`Select`、`Textarea`、`Tabs`、`Table`、`Dialog`、`DeleteResource`、`Checkbox`、`Switch`、`Tooltip`、`TimeseriesChart`、`Meter`、`SkeletonLine`。
- 按钮默认 `size="sm"`。
- 同排 Button/Input/Select 高度一致。
- 数据密集表格不换行，支持横向滚动和列宽控制。
- 图表必须使用 Kumo `TimeseriesChart`，loading 使用 Kumo chart loading。
- 不使用页面级自绘阴影和旧动画；展开收起使用 Kumo `Collapsible` 或现有 `AnimatedCollapse` 的官方行为。

### 页面结构

- `UptimeWorkbench`：页面 shell，管理 tab、筛选、布局。
- `MonitorListPane`：列表、搜索、分组、批量选择。
- `MonitorDetailPane`：状态摘要、图表、心跳、Incident、证书、通知历史。
- `MonitorEditorDialog`：新建/编辑/克隆，按类型渲染表单段。
- `MonitorTypeFields`：每种监测类型的字段组件。
- `StatusPageManager`：状态页列表、编辑器、预览入口。
- `MaintenanceManager`：维护窗口列表和编辑器。
- `IncidentCenter`：全局事件中心。
- `UptimeStatsView`：报表和趋势。

### 状态管理

- 建议抽出 `useUptimeStore` 或模块级 hooks，集中处理：
  - monitors 列表和 detail cache。
  - realtime events。
  - filters/search/selection。
  - optimistic update。
  - request de-duplication。
- 列表接口返回足够摘要，避免每个 monitor 单独拉 history/rate。
- 展开详情时再拉细节，但使用缓存和后台刷新。

### 移动端要求

- 列表项两行内展示核心信息：状态、名称、目标、延迟、可用率、心跳条。
- 详情面板在移动端使用单列压缩布局。
- 状态页编辑器避免多列挤压，使用分段 tabs 或 accordion。
- 图表轴标签在小尺寸下减少 tick，单位缩短。

## Backend Requirements

### 深模块划分

1. `MonitorRepository`
   - 负责 monitors、tags、groups、states、heartbeats、incidents、rollups 的读写。
   - 隐藏 SQLite schema 细节，向 service 暴露稳定接口。

2. `ProbeAdapterRegistry`
   - 根据 monitor type 获取 adapter。
   - adapter 统一接口：

```js
async function check(monitor, context) {
  return {
    ok: true,
    status: 'up',
    latencyMs: 123,
    message: 'OK',
    details: {},
    observedAt: new Date().toISOString(),
  };
}
```

3. `ProbeAdapters`
   - `HttpProbe`
   - `KeywordProbe`
   - `JsonQueryProbe`
   - `TcpProbe`
   - `PingProbe`
   - `DnsProbe`
   - `PushProbe`
   - `WebSocketProbe`
   - `DockerProbe`
   - `CertificateProbe`

4. `MonitorScheduler`
   - 管理 next run、interval、jitter、retry interval、concurrency。
   - 支持 start/stop/restart/testNow。
   - 服务重启后从 DB 恢复 active monitors。

5. `MonitorStateMachine`
   - 纯函数/可单测。
   - 输入 previousState、checkResult、monitorConfig、maintenanceContext。
   - 输出 nextState、incidentAction、notificationAction。

6. `IncidentService`
   - 创建/恢复/确认 incident。
   - 计算 duration、MTTR、MTBF。

7. `MaintenanceService`
   - 判断 monitor/status page 当前是否在维护期。
   - 计算下一次维护。
   - 与 notification service 对接。

8. `StatusPageService`
   - 生成公开状态页模型。
   - 处理 slug/domain 路由。
   - 执行缓存和脱敏。

9. `UptimeNotificationBridge`
   - 把 state machine 输出转为 notification event。
   - 统一 payload 和模板变量。

10. `UptimeRealtimeGateway`
   - Socket.IO 事件发布。
   - 事件：heartbeat、monitor:state、incident:opened、incident:resolved、monitor:updated、maintenance:changed。

### 数据库演进

建议新增/调整表：

- `uptime_monitors`
  - 保留现有字段，新增：`description`、`group_id`、`parent_id`、`weight`、`retry_interval`、`resend_interval`、`up_confirm_count`、`down_confirm_count`、`proxy_id`、`probe_id`、`config_json`、`auth_json_encrypted`、`last_checked_at`、`next_check_at`。
- `uptime_monitor_states`
  - `monitor_id`、`state`、`fail_count`、`recover_count`、`active_incident_id`、`last_transition_at`、`last_error`、`last_ping`、`updated_at`。
- `uptime_heartbeats`
  - 新增：`state`、`duration_ms`、`status_code`、`error_code`、`details_json`、`maintenance`、`probe_id`。
- `uptime_incidents`
  - 新增：`status`、`severity`、`acknowledged_at`、`acknowledged_by`、`maintenance_id`、`resolved_reason`。
- `uptime_groups`
  - 分组名称、排序、颜色。
- `uptime_monitor_tags`
  - monitor/tag 多对多。
- `uptime_daily_stats`
  - monitor_id、date、uptime、avg/p95/p99 latency、up/down/maintenance counts、incident duration。
- `uptime_status_pages`
  - slug、domain、title、description、theme、public、cache_seconds、config_json。
- `uptime_status_page_groups`
  - status_page_id、name、order_index。
- `uptime_status_page_monitors`
  - status_page_id、group_id、monitor_id、order_index、display_name。
- `uptime_maintenance_windows`
  - title、description、strategy、timezone、start_at、end_at、cron、recurrence_json、active。
- `uptime_maintenance_targets`
  - maintenance_id、target_type、target_id。
- `uptime_push_tokens`
  - monitor_id、token_hash、last_push_at、grace_seconds、enabled。
- `uptime_certificates`
  - monitor_id、subject、issuer、valid_from、valid_to、san_json、fingerprint、checked_at。
- `uptime_probes`
  - 预留本地/agent/remote probe。

迁移要求：

- 现有 `uptime_monitors` 数据必须无损迁移。
- `confirm_count` 迁移为 `down_confirm_count` 和 `up_confirm_count`。
- `notification_channels` 迁移为 monitor 默认通知渠道，并继续兼容通知规则。
- 原 JSON 状态文件迁移到 `uptime_monitor_states` 后停止写 JSON。

### API Contract

管理 API：

- `GET /api/uptime/summary`
- `GET /api/uptime/monitors?status=&type=&tag=&group=&q=`
- `POST /api/uptime/monitors`
- `GET /api/uptime/monitors/:id`
- `PUT /api/uptime/monitors/:id`
- `DELETE /api/uptime/monitors/:id`
- `POST /api/uptime/monitors/:id/clone`
- `POST /api/uptime/monitors/:id/toggle`
- `POST /api/uptime/monitors/:id/test`
- `POST /api/uptime/monitors/:id/check-now`
- `GET /api/uptime/monitors/:id/heartbeats?range=&limit=`
- `GET /api/uptime/monitors/:id/incidents?limit=&cursor=`
- `GET /api/uptime/monitors/:id/stats?range=24h|7d|30d|90d`
- `GET /api/uptime/incidents?status=&range=&monitorId=`
- `POST /api/uptime/batch`
- `GET /api/uptime/groups`
- `POST /api/uptime/groups`
- `PUT /api/uptime/groups/:id`
- `DELETE /api/uptime/groups/:id`
- `GET /api/uptime/status-pages`
- `POST /api/uptime/status-pages`
- `PUT /api/uptime/status-pages/:id`
- `DELETE /api/uptime/status-pages/:id`
- `GET /api/uptime/maintenance`
- `POST /api/uptime/maintenance`
- `PUT /api/uptime/maintenance/:id`
- `DELETE /api/uptime/maintenance/:id`
- `POST /api/uptime/import`
- `GET /api/uptime/export`

公开 API：

- `GET /status/:slug`
- `GET /api/uptime/public/status-pages/:slug`
- `GET /api/uptime/public/status-pages/by-domain`
- `GET /api/uptime/public/badge/:monitorId`
- `POST /api/uptime/push/:token`

Realtime events：

- `uptime:heartbeat`
- `uptime:monitor-state`
- `uptime:incident-opened`
- `uptime:incident-resolved`
- `uptime:monitor-updated`
- `uptime:maintenance-changed`
- `uptime:summary`

### Security

- HTTP monitor headers/body/auth 中的敏感字段需要加密保存或按字段脱敏。
- Push token 只保存 hash，创建时只显示一次原 token。
- 公开状态页 API 必须脱敏。
- 监测 URL 存在 SSRF 风险：
  - 默认允许本机管理员使用，但应提供全局安全选项。
  - 可选 deny private IP、deny localhost、allowlist domains。
  - DNS rebinding 需要在请求前后校验最终 IP。
- Docker socket 访问必须显式启用，并提示权限风险。
- 状态页自定义 HTML/iframe 不进入 MVP。

## Implementation Decisions

- 保留 `modules/uptime-api` 作为模块边界，但内部拆分 service。
- 不把检查逻辑继续写在单个 `monitor-service.js` 中。
- 状态机必须是纯逻辑模块，便于单元测试。
- 调度器不依赖 `setInterval` per monitor 的简单模型，应支持并发、jitter、retry interval 和服务重启恢复。
- 前端从单文件 `UptimePage.jsx` 拆出业务组件和 hooks。
- 状态页是同一 React app 的 public route，但使用独立数据入口和无管理操作的只读组件。
- 通知模块继续作为统一通知中心，Uptime 只提供事件和默认规则。
- 监测类型配置使用 `config_json` 承载类型私有字段，常用字段保留列索引用于查询和排序。
- 可用率计算以 incident duration 为主，heartbeat rollup 为辅；暂停不计入，维护是否计入由配置决定。

## Testing Decisions

测试优先覆盖外部行为，不测试内部实现细节。

必须测试：

- `MonitorStateMachine`
  - 连续失败进入 down。
  - 中途成功回到 up。
  - down 后恢复确认。
  - flapping 不重复通知。
  - maintenance 抑制通知。
  - paused 不生成检查动作。
- `ProbeAdapters`
  - HTTP status range。
  - keyword contains/not contains/regex。
  - JSON query 比较。
  - TCP 成功、超时、拒绝连接。
  - DNS expected value。
  - Push token 和 grace period。
- `MonitorScheduler`
  - interval、retry interval、jitter、concurrency。
  - restart 后恢复 active monitors。
- `IncidentService`
  - open/resolve/duration。
  - open incident 去重。
- `StatusPageService`
  - slug/domain lookup。
  - public payload 脱敏。
  - maintenance banner。
  - cache 行为。
- `NotificationBridge`
  - down/up/cert/push_missing payload。
  - maintenance skip。
- API integration
  - CRUD、test-now、batch、status page public、push endpoint。
- Frontend
  - monitor workbench 渲染。
  - 新建不同类型 monitor 的字段切换。
  - 详情图表 loading。
  - 移动端布局不重叠。
  - 状态页 public route 可访问。

建议测试工具：

- Vitest 单元测试纯模块。
- 本地 fake HTTP/TCP/DNS server 做 adapter integration。
- Playwright 或 Browser 插件做 `/uptime`、`/status/:slug` smoke。

## Acceptance Criteria

MVP 完成标准：

- 至少支持 HTTP(s)、Keyword、JSON Query、TCP、Ping、DNS、Push。
- 新工作台能在 100 个监测项下流畅操作，列表加载只发聚合请求。
- 新增/编辑/测试/暂停/删除/克隆/批量操作可用。
- 监测状态机重启后不丢状态。
- Down/Up 通知只在确认状态变迁时发送。
- 维护窗口能抑制通知并显示在状态页。
- 多状态页可创建、编辑、公开访问。
- 每个监测项有延迟图、心跳条、Incident 时间线、SLA 指标。
- `npm run lint`、`npm run build`、相关测试通过。
- 前端静态扫描仍满足 Kumo-only 规则。

## Milestones

### Phase 0: Foundation

- 设计并迁移 schema。
- 拆出 repository、state machine、adapter registry、scheduler。
- 保持现有 API 兼容。
- 添加状态机和 storage 测试。

### Phase 1: Core Monitor MVP

- 实现 HTTP、Keyword、JSON Query、TCP、Ping、DNS、Push adapters。
- 实现 test-now/check-now。
- 实现聚合列表 API 和详情 API。
- 前端重构工作台、详情面板和监测编辑器。

### Phase 2: Notification and Maintenance

- Uptime 默认通知规则。
- Monitor detail 中展示通知历史。
- 维护窗口管理。
- 维护状态联动工作台和通知。

### Phase 3: Status Pages

- 多状态页 schema/API。
- Public status route。
- 分组、排序、维护 banner、incident 展示。
- Badge endpoint。

### Phase 4: Reports and Retention

- Daily rollup。
- SLA/MTTR/MTBF 报表。
- 数据保留设置。
- 导入导出。

### Phase 5: Advanced Monitors

- WebSocket。
- Docker Container。
- Certificate monitor。
- Proxy。
- 多 probe/Agent 预留落地。

## Out of Scope

- 第一阶段不追求和 Uptime Kuma 完全一致的全部 monitor type。
- 第一阶段不实现 90+ 通知渠道。
- 第一阶段不做公开状态页自定义 HTML。
- 第一阶段不做多用户权限矩阵；沿用当前管理认证。
- 第一阶段不做跨地域主动探针，但 schema 和 API 预留。

## Further Notes

- 当前 `NotificationPage` 已经有 maintenance schema，但 UI 能力较弱；Uptime 重构可以先在 uptime 模块内提供维护窗口入口，再逐步统一到通知模块。
- 当前 `UptimePage.jsx` 存在较多文案编码显示异常的风险，重构时建议顺手重写该页面文案，避免继续在旧单文件上修补。
- Docker 监测建议优先复用已有 Agent 能力，避免要求 API Monitor 主进程直接访问远端 Docker socket。
- Ping 在 Windows/Linux 权限差异明显，必须在 UI 中标明“ICMP”或“TCP fallback”，不要让用户误解。
- 状态页缓存策略应与后台实时工作台分离，避免公开页面频繁打 DB。
