# API Monitor 重构指南

## 问题分析

原 `public/index.html` 文件过大（6706行），包含了所有的 HTML、CSS 和 JavaScript 代码，不利于维护。

## 重构方案

### 1. CSS 模块化

已将 CSS 拆分为以下独立文件：

- `public/css/styles.css` - 基础样式、变量、通用组件
- `public/css/projects.css` - 项目和服务相关样式
- `public/css/modals.css` - 模态框和表单样式
- `public/css/dns.css` - DNS 管理相关样式
- `public/css/tables.css` - 表格样式
- `public/css/tabs.css` - 标签页样式
- `public/css/settings.css` - 设置侧边栏样式
- `public/css/logs.css` - 日志和 OpenAI 相关样式

### 2. JavaScript 模块化（待完成）

由于 JavaScript 代码量巨大（约3000行），建议进一步拆分为：

```
public/js/
├── app.js              # 主应用入口
├── auth.js             # 认证相关
├── zeabur.js           # Zeabur 监控功能
├── dns.js              # DNS 管理功能
├── openai.js           # OpenAI API 管理功能
├── utils.js            # 工具函数
└── api.js              # API 请求封装
```

### 3. HTML 模板优化（待完成）

可以考虑使用 Vue 的单文件组件（SFC）或将模板拆分为多个组件文件。

## 实施步骤

### 步骤 1: 备份原文件

```bash
cp public/index.html public/index.html.backup
```

### 步骤 2: 更新 HTML 文件

在 `public/index.html` 的 `<head>` 部分，将原有的 `<style>` 标签替换为：

```html
<!-- Custom Styles -->
<link rel="stylesheet" href="/css/styles.css">
<link rel="stylesheet" href="/css/projects.css">
<link rel="stylesheet" href="/css/modals.css">
<link rel="stylesheet" href="/css/dns.css">
<link rel="stylesheet" href="/css/tables.css">
<link rel="stylesheet" href="/css/tabs.css">
<link rel="stylesheet" href="/css/settings.css">
<link rel="stylesheet" href="/css/logs.css">
```

### 步骤 3: 提取 JavaScript（需要手动完成）

1. 从 `index.html` 中复制所有 `<script>` 标签内的 JavaScript 代码
2. 创建 `public/js/app.js` 文件
3. 将代码粘贴到 `app.js` 中
4. 在 HTML 中引用：

```html
<!-- Application Script -->
<script src="/js/app.js"></script>
```

### 步骤 4: 测试

1. 启动应用服务器
2. 访问应用，确保所有功能正常
3. 检查浏览器控制台是否有错误
4. 测试所有主要功能：
   - 登录/认证
   - Zeabur 监控
   - DNS 管理
   - OpenAI API 管理
   - 设置功能

## 进一步优化建议

### 1. 使用构建工具

考虑引入 Vite 或 Webpack 进行模块打包：

```bash
npm init vite@latest
# 选择 Vue + JavaScript
```

### 2. 组件化

将大型 Vue 应用拆分为多个组件：

```
src/
├── components/
│   ├── Auth/
│   │   ├── LoginModal.vue
│   │   └── SetPasswordModal.vue
│   ├── Zeabur/
│   │   ├── AccountCard.vue
│   │   ├── ProjectCard.vue
│   │   └── ServiceItem.vue
│   ├── DNS/
│   │   ├── ZoneList.vue
│   │   ├── RecordTable.vue
│   │   └── RecordModal.vue
│   └── OpenAI/
│       ├── EndpointList.vue
│       └── EndpointModal.vue
├── views/
│   ├── ZeaburView.vue
│   ├── DNSView.vue
│   └── OpenAIView.vue
├── App.vue
└── main.js
```

### 3. 状态管理

对于复杂的状态管理，考虑使用 Pinia：

```bash
npm install pinia
```

```javascript
// stores/auth.js
import { defineStore } from 'pinia'

export const useAuthStore = defineStore('auth', {
  state: () => ({
    isAuthenticated: false,
    loginPassword: ''
  }),
  actions: {
    async login(password) {
      // 登录逻辑
    }
  }
})
```

### 4. TypeScript

为了更好的类型安全，可以迁移到 TypeScript：

```bash
npm install --save-dev typescript @types/node
```

## 注意事项

1. **渐进式重构**：不要一次性重构所有代码，建议分模块逐步进行
2. **保持备份**：每次重构前都要备份原文件
3. **充分测试**：每完成一个模块的重构，都要进行完整的功能测试
4. **版本控制**：使用 Git 管理代码变更，方便回滚

## 当前状态

✅ CSS 已完全模块化（8个文件）
⏳ JavaScript 待提取（需要手动完成）
⏳ HTML 模板待优化
⏳ 组件化待实施

## 下一步行动

1. 手动提取 JavaScript 代码到 `public/js/app.js`
2. 更新 `public/index.html`，移除内联的 `<style>` 和 `<script>` 标签
3. 测试所有功能
4. 考虑引入构建工具进行进一步优化

## 文件大小对比

- 原 `index.html`: 6706 行
- 重构后预期:
  - `index.html`: ~500 行（仅HTML模板）
  - CSS 文件总计: ~1500 行（分8个文件）
  - JS 文件: ~3000 行（可进一步拆分）

## 维护性提升

- ✅ 代码职责分离（HTML/CSS/JS）
- ✅ 样式模块化，易于查找和修改
- ✅ 支持代码复用
- ✅ 便于团队协作
- ✅ 更好的可读性

## 参考资源

- [Vue.js 官方文档](https://vuejs.org/)
- [Vite 构建工具](https://vitejs.dev/)
- [Pinia 状态管理](https://pinia.vuejs.org/)
