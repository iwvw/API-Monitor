# 后续执行计划

最后更新：2026-06-07

本文档只保留当前真正需要继续推进的事项。历史阶段计划已归并到 [refactor-progress.md](./refactor-progress.md)。

## 最高优先级

### 1. 删除确认迁移到 DeleteResource

目标：破坏性删除确认使用 Kumo `DeleteResource`，普通 confirm 只保留给非删除动作。

建议步骤：

1. 在 `src/js/modules/dialog.js` 增加 `dialog.deleteResource(options)`。
2. 在 `GlobalDialogHost.jsx` 渲染 Kumo `DeleteResource`。
3. 让取消行为返回 `false`，确认行为返回 `true`。
4. 优先替换资源删除场景：
   - Cloudflare 账号、Zone、DNS Record、Worker、Pages、R2、Tunnel。
   - Server 主机、凭据。
   - PaaS 账号、应用。
   - Uptime 监测目标。
   - Notification 渠道、规则。
   - Filebox 分享。
   - TOTP 账号、分组。
5. 重启、清缓存、重新部署、导入覆盖、清空日志等继续使用普通 confirm。

验收：

```bash
npm run lint
npm run build
rg -n "dialog\\.confirm\\(" src/js/pages src/js/components -S
```

### 2. PageHeader block 决策落地

当前判断：`PageHeader` 是 Kumo block source，不是 `@cloudflare/kumo` barrel 导出。不能直接 import。

可选路径：

- 保持 `AppPageHeader`：适合当前顶栏紧凑高度，风险最低。
- 安装/复制官方 PageHeader block：适合后续页面级 header 统一，但需要适配边框、间距、`size="sm"` tabs 和 450px 断点。

若决定替换：

1. 使用 Kumo CLI 安装 `PageHeader`，或复制当前包内 `dist/blocks-source/page-header` 到项目。
2. 转成项目 JS/JSX 约定。
3. 确保顶栏不出现双 `border-b`。
4. 确保顶部在 450px 以上不换行。
5. 保持内部层级 Tabs 为 `size="sm"`。

验收：

```bash
npm run build
```

并做 `/dashboard`、`/server`、`/settings` 的桌面和窄屏浏览器验证。

## 中优先级

### 3. 全路由 browser smoke

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

### 4. Kumo-only 周期扫描

持续扫描：

```bash
rg -n --pcre2 '<(?-i:button|select|input|textarea)\b' src/js/pages src/js/components -S
rg -n 'DialogContent|TabsList|TabsTrigger|@cloudflare/kumo/components/tabs' src -S
rg -n '#[0-9a-fA-F]{3,8}|rgba?\(|hsla?\(' src/js src/css -S
```

硬编码颜色若用于 QRCode、第三方品牌标识、canvas 或协议输出，需要记录例外。

### 5. 移动端与图表压缩

重点页面：

- `ServerPage.jsx`：展开卡片、CPU/GPU 温度、Docker 表格。
- `DashboardPage.jsx`：宿主机性能卡片、API 调用趋势。
- `MusicPage.jsx`：播放器、封面、搜索结果、歌词。
- `DnsPage.jsx`：Cloudflare 多表格。

要求：

- 一行或两行展示主要信息。
- 对齐稳定。
- 小图表轴标签不溢出。
- tooltip 不越界。

## 低优先级

### 6. 真实账号/环境流程验证

这些流程需要真实外部环境，不应只靠静态检查：

- Docker 容器、镜像、网络、卷操作。
- SSH/SFTP。
- Agent 快速安装、升级、卸载。
- Cloudflare Workers / Pages / R2 / Tunnels。
- Koyeb / Fly.io 批量操作。
- 音乐播放源、解锁代理。

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

1. 每次只处理一个明确任务。
2. 修改前读对应页面和相关文档。
3. 使用 Kumo 优先，不新增自写 UI 组件。
4. 不回退用户或其他 agent 的未提交改动。
5. 修改后运行必要验证。
6. 更新 `refactor-verification.md`。
7. 不留下临时文件。
