# API-Monitor 模块开发指南

本指南帮助你快速创建符合项目规范的新模块。

## 🚀 快速开始

### 方法一：使用自动脚本

```bash
cd .templates
node create-module.js <模块名>

# 示例
node create-module.js weather-api
```

### 方法二：手动复制模板

1. 复制 `backend/` 模板到 `modules/<模块名>/`
2. 复制 `frontend/module.template.css` 到 `public/css/<模块名>.css`
3. 复制 `frontend/module.template.js` 到 `public/js/modules/<模块名>.js`
4. 替换所有占位符

---

## 📁 目录结构

新模块完成后的文件结构：

```
modules/<module-name>/
├── router.js          # API 路由
├── storage.js         # 数据存储
├── service.js         # 业务逻辑（可选）
└── schema.sql         # 数据库表结构

public/
├── css/<module-name>.css    # 模块样式
└── js/modules/<module-name>.js  # 前端逻辑
```

---

## 🔧 配置步骤

### 1. 注册 API 路由

在 `server.js` 中添加：

```javascript
const myModuleRouter = require('./modules/my-module/router');
app.use('/api/my-module', myModuleRouter);
```

### 2. 添加数据模型

在 `src/db/models.js` 中创建模型类：

```javascript
class MyModuleItem {
  static findAll() { /* ... */ }
  static findById(id) { /* ... */ }
  static create(data) { /* ... */ }
  static update(id, data) { /* ... */ }
  static delete(id) { /* ... */ }
}
module.exports = { MyModuleItem, /* 其他模型 */ };
```

### 3. 更新数据库 Schema

将 `schema.template.sql` 内容添加到 `src/db/schema.sql`

### 4. 引入前端 CSS

在 `public/index.html` 的 `<head>` 中添加：

```html
<link rel="stylesheet" href="css/my-module.css">
```

### 5. 注册前端模块

在 `public/js/main.js` 中：

```javascript
import { myModuleMethods } from './modules/my-module.js';

// 在 methods 中合并
methods: {
  ...myModuleMethods,
  // ... 其他方法
}
```

### 6. 更新 Store

在 `public/js/store.js` 中添加模块状态：

```javascript
export const store = Alpine.reactive({
  // ... 现有属性
  
  // 新模块
  myModuleItems: [],
  myModuleLoading: false,
  myModuleSelectedItems: [],
});
```

### 7. 添加导航标签

在 `public/index.html` 中添加主标签：

```html
<button class="main-tab" 
        :class="{ active: mainActiveTab === 'myModule' }"
        @click="switchToMyModule()">
  <i class="fas fa-cube"></i>
  <span>我的模块</span>
</button>
```

---

## 📝 命名规范

| 场景 | 格式 | 示例 |
|------|------|------|
| 目录/文件名 | kebab-case | `weather-api/` |
| API 路由 | kebab-case | `/api/weather-api` |
| 数据库表 | snake_case | `weather_api_items` |
| JS 变量 | camelCase | `weatherApiItems` |
| CSS 类名 | kebab-case | `.weather-api-card` |

---

## 🎨 设计规范

### 颜色

使用 CSS 变量定义模块主题色：

```css
:root {
  --my-module-primary: #6366f1;
  --my-module-primary-dark: #4f46e5;
}
```

### 响应式

所有组件必须支持移动端，断点：
- 桌面：> 768px
- 平板：481px - 768px
- 手机：≤ 480px

---

## 🔌 API 规范

### 响应格式

**成功：**
```json
{
  "success": true,
  "item": { ... }
}
```

**列表：**
```json
[
  { "id": "xxx", "name": "..." },
  ...
]
```

**错误：**
```json
{
  "error": "错误信息"
}
```

---

## ❓ 常见问题

### 模块不显示？
- 检查 `server.js` 是否注册了路由
- 检查 `index.html` 是否引入了 CSS
- 检查 `main.js` 是否导入了模块方法

### 数据库表不存在？
- 确认将 schema.sql 内容添加到 `src/db/schema.sql`
- 删除 `data/api-monitor.db` 重新启动（会重建数据库）

### 样式不生效？
- 确认 CSS 文件路径正确
- 检查类名是否使用了正确的模块前缀
