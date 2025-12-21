# 模块开发模板使用指南

本目录提供了一个标准化的模块开发模板，用于快速扩展项目功能。

## 快速开始

### 1. 后端部分 (Backend)

1.  **复制模板**: 复制 `modules/_template` 文件夹并重命名为你的模块名 (例如 `my-service-api`)。
2.  **配置数据库**: 
    *   修改 `schema.sql`，定义你的表结构。
    *   在 `src/db/database.js` 中添加新表的初始化逻辑（或者手动在 SQLite 中运行 SQL）。
3.  **定义模型**:
    *   修改 `models.js`，将 `{{module_name}}` 替换为实际表名。
    *   在 `src/db/models/index.js` 中导出你的新模型。
4.  **实现存储层**: 修改 `storage.js` 中的逻辑。
5.  **实现 API 服务**: 在 `service.js` 中实现与外部第三方 API 的交互。
6.  **注册路由**:
    *   修改 `router.js` 中的路由定义。
    *   在 `src/routes/index.js` 的 `moduleRouteMap` 中添加你的模块路由映射。

### 2. 前端部分 (Frontend)

1.  **样式**: 
    *   复制 `src/css/template.css` 为 `src/css/your-module.css`。
    *   在 `src/index.html` 中引入该 CSS 文件。
2.  **模板**:
    *   复制 `src/templates/template.html` 为 `src/templates/your-module.html`。
    *   在 `src/index.html` 的 templates 区域引入该文件（或通过 `template-loader.js` 加载）。
3.  **逻辑**:
    *   复制 `src/js/modules/template.js` 为 `src/js/modules/your-module.js`。
    *   在 `src/js/main.js` 中导入并混入 (mixin) 你的模块方法。
4.  **导航**:
    *   在 `src/index.html` 的侧边栏导航中添加指向你模块的链接。

## 关键变量替换

在模板文件中，请全局替换以下占位符：

*   `{{module_name}}`: 模块名称 (小写，下划线分隔)，如 `my_service`。
*   `{{ModuleName}}`: 模块名称 (大驼峰)，如 `MyService`。
*   `{{module_prefix}}`: 数据库 ID 前缀，如 `ms`。
*   `{{MODULE_ENV_VAR}}`: 环境变量名称，如 `MY_SERVICE_TOKEN`。
*   `{{module_title}}`: UI 显示的中文标题。

## 集成检查清单

- [ ] `schema.sql` 已执行 (数据库表已创建)
- [ ] `src/db/models/index.js` 已导出新模型
- [ ] `src/routes/index.js` 已注册路由映射
- [ ] `src/js/main.js` 已混入前端方法
- [ ] `src/index.html` 已添加导航项、CSS 和 HTML 模板
