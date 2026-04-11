# API Monitor 新模块集成避坑指南 (v1.0)

本指南总结了在集成 **Qwen2API** 模块过程中的核心技术细节与实战教训，旨在确保后续模块（如 Anthropic、Grok 等）能以最快速度对齐全局设计原则并实现功能闭环。

---

## 一、 架构契约 (Architecture Contract)

### 1. 双端白名单机制 (必做)
框架具备强大的“自愈”功能，会自动剔除配置中不存在的模块。
- **后端注册**：必须在 `src/services/userSettings.js` 的 `validModules` 列表中添加模块 ID。
- **前端注册**：必须在 `src/js/modules/settings.js` 的 `VALID_MODULES` 中添加相同 ID。
- **后果**：漏掉任何一个，模块在刷新页面后都会在导航栏消失。

### 2. 路由豁免 (Proxy 模块特有)
如果你的模块是 API 代理（转发到 OpenAI 格式），必须在 `src/routes/index.js` 的 `moduleRouteMap` 注册对应的转发前缀，并将其加入认证豁免（如果需要外部直接调用）。

---

## 二、 设计原则对齐 (The CSS Variable Secret)

### 1. 变量驱动，拒绝 Hardcode
**教训**：不要在模块 `css` 里写 `background: #006eff`。
**正确做法**：
1. 在 `src/css/styles.css` 的 `.theme-xxx` 类名下注册三个变量：
   - `--current-primary`: 品牌主色
   - `--current-dark`: 悬浮/深色态
   - `--current-rgb`: 用于生成带透明度的光影效果
2. 全局所有的 `.btn-primary`, `.active`, `.pulse-dot` 都会自动同步变色。

### 2. 镜像复刻，CSS 脱脂
**原则**：相似功能的模块（如 Proxy 类型）前端应该完全一样。
- **做法**：直接在 HTML 模板里复用 `ds-stat-card`, `ds-stat-cards` 等成熟模块的类名。
- **优势**：确保了全站布局比例、阴影、毛玻璃质感的像素级统一。

---

## 三、 布局陷阱 (Layout Pitfalls)

### 1. Modal 越狱层级 (Critical)
**教训**：Modal 卸在 Tab 容器内部会导致定位坍塌。
- **原理**：一旦父容器设置了 `transform` (动画相关) 或特定的 `filter`，内部元素的 `position: fixed` 会相对于父级而非视口定位。
- **修复**：Modal 必须定义在 `templates/xxx.html` 的**最外层**节点，与 `.tab-content` 平级。

---

## 四、 交互与数据逻辑

### 1. 禁用法：直接访问 Window
**教训**：在 Vue 模板中严禁写 `{{ window.location.origin }}`，这会导致渲染函数崩溃引发白屏。
- **方案**：所有全局变量通过 `src/js/store.js` 进行响应式分发。

### 2. 后端数据闭环
**教训**：前端仪表盘显示 0 数据是因为缺少“异步落库”逻辑。
- **要求**：在代理模块的 `router.js` 转发请求成功后，必须立即执行一条 `database.prepare(...).run()` 写入 `xxx_logs`。
- **路由要求**：必须暴露 `/stats`（合计数据）和 `/matrix`（模型配置）接口供前端调用。

---

## 五、 模块集成清单 (Checklist)

- [ ] `userSettings.js` 允许模块显示
- [ ] `settings.js` 前端白名单同步
- [ ] `styles.css` 注册品牌色变量
- [ ] `store.js` 注册模块专用响应式状态 (Stats/Matrix)
- [ ] `main.js` 混入模块 Methods 并配置 TemplateLoader
- [ ] 后端实现 `/stats` 接口并完成 `Log` 落库逻辑
- [ ] Modal HTML 结构脱离内容区嵌套

---
*记录日期：2026-04-11*
*最后维护：Antigravity AI (Advanced Agentic Coding)*
