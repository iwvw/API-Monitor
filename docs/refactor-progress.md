# UI 重构进度追踪

## 目标
将所有模块从旧版 Vue 3 + FontAwesome + 原生 CSS 方案，完整迁移至 **React 19 + @cloudflare/kumo v2.5.0 + Tailwind CSS v4**，视觉风格对齐 Cloudflare Dashboard。

## 技术规范

### 设计 Token（Kumo 变量）
| 用途 | Token |
|------|-------|
| 页面背景 | `bg-kumo-canvas` |
| 卡片/面板背景 | `bg-kumo-base` |
| 凹陷区域背景 | `bg-kumo-recessed` |
| 表单控件背景 | `bg-kumo-control` |
| 主要文字 | `text-kumo-strong` |
| 普通文字 | `text-kumo-default` |
| 次要文字 | `text-kumo-subtle` |
| 边框 | `border-kumo-line` |
| 品牌色 | `text-kumo-brand` / `bg-kumo-brand` |
| 成功 | `text-kumo-success` / `bg-kumo-success/10` |
| 危险 | `text-kumo-danger` / `bg-kumo-danger/10` |
| 警告 | `text-kumo-warning` / `bg-kumo-warning/10` |
| 信息 | `text-kumo-info` / `bg-kumo-info-tint` |

### 组件导入规范
```js
// 推荐（tree-shaking）
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
// 或
import { Button, Input } from "@cloudflare/kumo";
```

### 页面文件路径规范
```
src/js/pages/<ModuleName>Page.jsx
```

### 主要交互模式
- 列表页：顶部操作栏（筛选 + 搜索 + 主操作按钮）+ 卡片/表格主体
- 弹窗：使用 `useState` 控制本地显示状态，与 `Dialog` 原语或自定义 modal 配合
- 状态 badge：`text-xs font-semibold px-2 py-0.5 rounded border`
- 数据卡片：`bg-kumo-base border border-kumo-line rounded-lg p-5 shadow-sm`
- 操作按钮组：`flex items-center gap-2`

### Toast 通知
```js
import { toast } from '../modules/toast.js';
toast.success('操作成功');
toast.error('操作失败');
toast.warning('注意...');
toast.info('提示...');
```

---

## 模块重构状态

### 已完成 ✅
| 模块 | 文件 | 完成日期 | 说明 |
|------|------|----------|------|
| MainLayout | `components/MainLayout.jsx` | 2026-06-05 | 侧边栏 + 顶栏，支持折叠动画 |
| AuthPage | `pages/AuthPage.jsx` | 2026-06-05 | 登录页 |
| DashboardPage | `pages/DashboardPage.jsx` | 2026-06-05 | 统计卡片 + API 趋势图 + 服务列表 |
| ServerPage | `pages/ServerPage.jsx` | 2026-06-05 | 主机实例 + 历史趋势 + Docker + SSH 终端 |

### Wave 1 — 工具类模块
| 模块 | 文件 | 状态 | 说明 |
|------|------|------|------|
| TOTP (2FA) | `pages/TotpPage.jsx` | ✅ 完成 | 验证码列表 + 分组 + 设置 |
| Filebox (文件柜) | `pages/FileboxPage.jsx` | ✅ 完成 | 文件列表 + 上传 + 分享链接 |
| Uptime (监测) | `pages/UptimePage.jsx` | ✅ 完成 | 监测列表 + 添加 + 统计报表 |
| Notification (通知) | `pages/NotificationPage.jsx` | ✅ 完成 | 渠道 + 告警规则 + 历史 + 配置 |

### Wave 2 — API 网关
| 模块 | 文件 | 状态 | 说明 |
|------|------|------|------|
| OpenAI | `pages/OpenAIPage.jsx` | ⬜ 待开始 | API Key 管理 + 对话测试 |
| GeminiCLI | `pages/GeminiCliPage.jsx` | ⬜ 待开始 | API 代理配置 + 日志统计 |
| Qwen (通义) | `pages/QwenPage.jsx` | ⬜ 待开始 | 通义千问 API 代理 |
| Antigravity | `pages/AntigravityPage.jsx` | ⬜ 待开始 | Antigravity 代理服务 |

### Wave 3 — 基础设施
| 模块 | 文件 | 状态 | 说明 |
|------|------|------|------|
| PaaS | `pages/PaasPage.jsx` | ⬜ 待开始 | Koyeb + Fly.io 应用监控 |
| Cloudflare DNS | `pages/DnsPage.jsx` | ⬜ 待开始 | DNS 区域 + Records + Workers + Pages |
| 阿里云 | `pages/AliyunPage.jsx` | ⬜ 待开始 | DNS + ECS 管理 |
| 腾讯云 | `pages/TencentPage.jsx` | ⬜ 待开始 | DNS + CVM 管理 |

### Wave 4 — 特殊模块
| 模块 | 文件 | 状态 | 说明 |
|------|------|------|------|
| Self-H (自建服务) | `pages/SelfHPage.jsx` | ⬜ 待开始 | 自建服务状态面板 |
| AI Chat | `pages/AiChatPage.jsx` | ⬜ 待开始 | AI 对话助手（Markdown + 流式输出） |
| Music | `pages/MusicPage.jsx` | ⬜ 待开始 | 网易云音乐播放器 |

---

## MainLayout 路由注册

每完成一个模块，需在 `MainLayout.jsx` 的 `renderActivePage()` switch 中添加对应 case：

```jsx
case 'totp':     return <TotpPage />;
case 'filebox':  return <FileboxPage />;
// ...
```

---

## Git 提交记录

| Hash | 模块 | 日期 |
|------|------|------|
| 1ae0849 | MainLayout 侧边栏动画修复 | 2026-06-05 |
| 16eea3a | TotpPage | 2026-06-05 |
| (待填充) | FileboxPage | - |
| (待填充) | UptimePage | - |
| (待填充) | NotificationPage | - |

---

## 接手须知（给下一位助手）

1. **进度文件**：本文件（`docs/refactor-progress.md`）是唯一的进度来源，以此为准
2. **旧模板**：`src/templates/*.html` + `src/css/*.css` + `src/js/modules/*.js` 是旧实现，重构时参考业务逻辑但不复制代码风格
3. **MainLayout 路由**：`src/js/components/MainLayout.jsx` 的 `renderActivePage()` 方法，需要为每个完成的模块注册 case
4. **API 端点**：所有后端接口不变，路径均为 `/api/<module>/...`，可在旧 `src/js/modules/*.js` 中查阅
5. **Token 规范**：严格使用 kumo token，禁止硬编码颜色值
6. **Git 提交**：每完成一个模块做一次 commit，并更新本文件的"已完成"表格和"Git 提交记录"
