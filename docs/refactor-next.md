# 后续执行计划

最后更新：2026-06-08

本文档只保留当前真正需要继续推进的事项。工具箱 PRD 的前后端实现已经按本轮要求完成；历史阶段计划已归并到 [refactor-progress.md](./refactor-progress.md)，本轮验收记录见 [refactor-verification.md](./refactor-verification.md)。

## 最高优先级

### 1. 真实账号/真实环境回归

当前状态：双因子认证、音乐、可用性监测、文件柜、通知、系统设置的 PRD 前后端闭环已经落地。仍需用真实外部条件验证以下流程，因为它们依赖账号、网络、第三方服务或远端 Agent，不能只靠静态扫描和单测证明：

- 音乐：真实 NCM 登录、搜索、播放源、解锁源、歌词和代理播放。
- 通知：真实 Email / Telegram 渠道投递、规则条件、限流、重试、批量与维护窗口抑制。
- Uptime：HTTP/Keyword/JSON Query/TCP/Ping/DNS/Push 的真实目标检测、公开状态页、badge 和告警链路。
- Filebox：大文件上传、MIME 限制、访问密码、下载次数、阅后即焚、过期清理和公开取件。
- TOTP：旧数据迁移、加密备份导出/导入、HOTP 递增、reveal 审计。
- System：数据库备份、导入 preview/commit、压缩、分析、日志清理和安全退出。

验收命令：

```bash
npm run test
npm run lint
npm run build
```

### 2. 全路由 browser smoke

建立自动或半自动检查，覆盖：

- `/dashboard`
- `/server`
- `/totp`
- `/filebox`
- `/uptime`
- `/notification`
- `/openai`
- `/gemini-cli`
- `/qwen`
- `/paas`
- `/dns`
- `/aliyun`
- `/tencent`
- `/self-h`
- `/music`
- `/settings`

检查项：

- 页面不白屏。
- 侧栏高亮正确。
- 顶栏不换行。
- 控制台无 React runtime error。
- 主要弹窗可打开关闭。
- 空状态可读。
- 移动端不重叠。

说明：当前浏览器会先进入安全登录页；没有管理员密码授权时，不在 browser smoke 中代填密码。

### 3. 删除确认文案精确化回归

当前状态：`dialog.deleteResource(options)` 与 `GlobalDialogHost.jsx` 的 Kumo `DeleteResource` 通路已落地；明确包含删除、移除、销毁、永久删除的旧 `dialog.confirm(...)` 会自动走 DeleteResource。

后续建议：

1. 对高频资源删除场景逐步改为显式 `dialog.deleteResource({ resourceType, resourceName })`，提升输入确认文案的准确性。
2. 重启、清缓存、重新部署、导入覆盖、清空日志等继续使用普通 confirm。
3. 做一次真实浏览器回归，确认中文资源名、批量删除和无资源名删除的交互都可接受。

## 中优先级

### 4. PageHeader block 决策落地

当前判断：`PageHeader` 是 Kumo block source，不是 `@cloudflare/kumo` barrel 导出。不能直接 import。

可选路径：

- 保持 `AppPageHeader`：适合当前顶栏紧凑高度，风险最低。
- 安装/复制官方 PageHeader block：适合后续页面级 header 统一，但需要适配边框、间距、`size="sm"` tabs 和 450px 断点。

### 5. Kumo-only 周期扫描

持续扫描：

```bash
rg -n --pcre2 '<(?-i:button|select|input|textarea)\b' src/js/pages src/js/components -S
rg -n 'DialogContent|TabsList|TabsTrigger|@cloudflare/kumo/components/tabs' src -S
rg -n 'vue|pinia|chart\.js|createApp\(|new Vue|from .vue.|from .pinia.|Chart\.' src package.json package-lock.json -S
rg -n 'execCommand' src/js/pages src/js/components -S
```

### 6. 移动端与图表压缩

重点页面：

- `ServerPage.jsx`：展开卡片、CPU/GPU 温度、Docker 表格。
- `DashboardPage.jsx`：宿主机性能卡片、API 调用趋势。
- `MusicPage.jsx`：播放器、封面、搜索结果、歌词。
- `DnsPage.jsx`：Cloudflare 多表格。
- `UptimePage.jsx`：监测卡片、图表、状态页、维护窗口。

要求：

- 一行或两行展示主要信息。
- 对齐稳定。
- 小图表轴标签不溢出。
- tooltip 不越界。

## 低优先级

### 7. Kumo 注册表刷新

当 `@cloudflare/kumo` 版本升级后，刷新：

- `docs/reference/kumo-component-registry.md`
- `docs/reference/kumo-component-registry.json`
- 相关规则文档中的导出能力说明。

特别关注：

- `PageHeader` 是否变成运行时导出。
- `DeleteResource` API 是否变化。
- Button/Input/Select/Tabs 尺寸规范是否变化。
- Chart loading、tooltipBoundary、palette API 是否变化。

## Agent 执行规则

1. 修改前读对应页面和相关文档。
2. 使用 Kumo 优先，不新增自写基础 UI 组件。
3. 不回退用户或其他 agent 的未提交改动。
4. 修改后运行必要验证。
5. 更新 `refactor-verification.md`。
6. 不留下临时文件。
