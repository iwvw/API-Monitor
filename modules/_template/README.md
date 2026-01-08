# Antigravity 模块开发与集成规范指南

本文档规定了新模块接入系统的标准化流程与代码规范，确保全站 UI/UX 一致性及系统架构的整洁。

---

## 🚀 1. 挂载与注册 (Mounting & Registration)

### 1.1 后端路由挂载

在 `src/routes/index.js` 中，将模块路由添加到 `moduleRouteMap`：

```javascript
const moduleRouteMap = {
  'notification-api': '/api/notification', // 键名为前端模块ID, 值为后端路径前缀
  // ...
};
```

### 1.2 前端基础配置

在 `src/js/stores/app.js` 中配置图标与基本信息：

```javascript
export const MODULE_CONFIG = {
  notification: {
    name: '通知管理',
    icon: 'fas fa-bell',
    desc: '统一告警与通知中心'
  },
  // ...
};
```

### 1.3 模块可见性 (白名单)

**核心步骤！** 在 `src/js/modules/settings.js` 的 `validModules` 数组中手动添加模块 ID，否则模块在“用户设置”中会被过滤，导致前端不显示。

```javascript
const validModules = [
  'dashboard', 'uptime', 'notification', // 必须包含模块名
  // ...
];
```

---

## 💻 2. 前端显示与方法 (Frontend Methods)

### 2.1 Vue 方法集成 (Vue Mixin)

在 `src/js/main.js` 中导入并混入数据和方法：

```javascript
import { notificationData, notificationMethods } from './modules/notification.js';

const app = Vue.createApp({
  data() {
    return {
      ...notificationData, // 混入数据
      // ...
    };
  },
  methods: {
    ...notificationMethods, // 混入方法
    // ...
  }
});
```

### 2.2 HTML 模板加载

1. **容器定义**: 在 `src/index.html` 的主内容区域添加：

   ```html
   <div id="template-notification" v-show="mainActiveTab === 'notification'"></div>
   ```

2. **路由映射**: 在 `src/js/template-loader.js` 的 `templateMap` 中指定文件：

   ```javascript
   const templateMap = {
     'notification.html': '#template-notification',
   };
   ```

---

## 🎨 3. UI 标准化定义 (Standard UI)

### 3.1 主题配色 (Theming)

在 `src/css/styles.css` 中定义主题变量，所有子组件将自动继承配色：

```css
.theme-notification {
  --current-primary: #f59e0b; /* 主色调 */
  --current-dark: #d97706;    /* 悬停/深色态 */
  --current-rgb: 245, 158, 11; /* 阴影/半透明色 */
}
/* 定义子标签页激活态渐变 (必须) */
.theme-notification .tab-btn.active {
  background: linear-gradient(135deg, var(--current-primary), var(--current-dark)) !important;
  box-shadow: 0 2px 8px rgba(var(--current-rgb), 0.3);
}
```

### 3.2 子标签栏 (SecTabs)

统一使用 `sec-tabs` 类名。

```html
<div class="sec-tabs">
  <button class="tab-btn" :class="{ active: currentSubTab === 'tab1' }" @click="...">
    <i class="fas fa-xxx"></i> 标题
  </button>
</div>
```

### 3.3 模态框标准化 (Modals)

1. **外部放置**: 模态框 HTML 必须放在 `.tab-content` 主容器之外。
2. **主题声明**: 在 `modal-overlay` 上添加模块主题类名。
3. **结构规范**:

```html
<!-- 模态框必须在 overlay 层声明 theme 类 -->
<div v-if="showModal" class="modal-overlay theme-notification" @click.self="showModal = false">
  <div class="modal"> <!-- 大尺寸可用 .modal-lg -->
    <div class="modal-header">
      <h3><i class="fas fa-xxx"></i> 标题</h3>
      <button class="modal-close" @click="..."><i class="fas fa-times"></i></button>
    </div>
    <div class="modal-body">...内容...</div>
    <div class="modal-footer">
      <button class="btn btn-secondary" @click="...">取消</button>
      <button class="btn btn-primary" @click="...">确定</button>
    </div>
  </div>
</div>
```

### 3.4 开关切换器 (AG-Switch)

不再使用原生复选框，统一使用以下结构：

```html
<label class="ag-switch">
  <input type="checkbox" v-model="...">
  <div class="ag-switch-track">
    <div class="ag-switch-knob"></div>
  </div>
  <span class="ag-switch-label">说明文字</span>
</label>
```

---

## ✅ 集成检查清单 (Checklist)

- [ ] 后端 `router.js` 定义完毕并注册到 `src/routes/index.js`。
- [ ] 前端 `validModules` 已加入白名单。
- [ ] `main.js` 已导入并混入 `{module}Data` 和 `{module}Methods`。
- [ ] `index.html` 占位符 ID 与 `template-loader.js` 映射一致。
- [ ] 所有的模态框均在容器外层且绑定了 `theme-xxx` 类。
- [ ] `styles.css` 中已定义模块专属渐变激活态样式。
