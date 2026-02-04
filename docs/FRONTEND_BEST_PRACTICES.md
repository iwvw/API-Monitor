# 前端开发最佳实践

本文档记录项目开发中积累的最佳实践和常见陷阱，供开发者参考。

---

## 🎯 模态框 (Modal) 最佳实践

### **问题：模态框位置偏移或无法居中**

**症状**：模态框使用 `position: fixed` 但无法正确居中，显示在奇怪的位置。

**原因**：在 CSS 中，如果父元素有以下任一属性，会创建新的"包含块"(containing block)，导致 `position: fixed` 相对于父元素定位而非视口：
- `transform` (非 `none`)
- `filter` (非 `none`)
- `perspective` (非 `none`)
- `will-change: transform` / `filter` / `perspective`
- `contain: paint` / `layout` / `strict` / `content`

**解决方案**：使用 Vue 的 `<Teleport>` 组件将模态框渲染到 `<body>` 顶层：

```html
<!-- ❌ 错误：模态框嵌套在可能有 transform 的父元素内 -->
<div class="tab-content">
    <div v-if="showModal" class="modal-overlay">
        <!-- 模态框内容 -->
    </div>
</div>

<!-- ✅ 正确：使用 Teleport 渲染到 body -->
<div class="tab-content">
    <Teleport to="body">
        <div v-if="showModal" class="modal-overlay">
            <!-- 模态框内容 -->
        </div>
    </Teleport>
</div>
```

### **模态框必需属性**

```html
<div v-if="showModal" 
     class="modal-overlay" 
     @click.self="showModal = false"
     @keydown.esc="showModal = false"
     tabindex="-1">
    <div class="modal">
        <!-- 内容 -->
    </div>
</div>
```

- `@click.self`: 点击遮罩层关闭（不包括模态框内部点击）
- `@keydown.esc`: ESC 键关闭
- `tabindex="-1"`: 允许接收键盘事件

---

## 🔔 Toast 通知规范

### **正确用法**

项目使用 `showToast` 方法而非 `$toast`：

```javascript
// ❌ 错误
this.$toast.success('操作成功');
this.$toast.error('操作失败');

// ✅ 正确
this.showToast('操作成功', 'success');
this.showToast('操作失败', 'error');
this.showToast('请注意', 'warning');
this.showToast('提示信息', 'info');
```

---

## 📦 模块集成检查清单

新模块集成时，确保以下事项：

- [ ] 在 `store.js` 或 `app.js` 中注册模块配置
- [ ] 在 `template-loader.js` 中添加模板加载
- [ ] 在 `main.js` 中导入并合并 data/methods/computed
- [ ] CSS 文件在 `styles.css` 中 `@import` 引入
- [ ] 模态框使用 `<Teleport to="body">` 确保正确定位

---

## 🔧 常见陷阱

| 问题 | 原因 | 解决方案 |
|------|------|----------|
| 模态框位置错误 | 父元素有 `transform` | 使用 `<Teleport to="body">` |
| Toast 报错 `Cannot read properties of undefined` | 使用了 `$toast` 而非 `showToast` | 改用 `this.showToast(msg, type)` |
| 删除操作只清本地不删服务器 | API 调用缺失 | 记得调用后端 DELETE API |
| 日志不规范 | 使用 `console.error` | 使用 `createLogger` 统一日志 |
