# AI Draw 模块集成说明

## 概述

AI Draw 模块是从 `ai-draw-nexus` 项目集成到 API Monitor 的绘图模块，采用「微前端」架构设计。

## 模块结构

```
api-monitor/
├── modules/
│   └── ai-draw-api/                # 后端 API 模块
│       ├── router.js               # Express 路由
│       ├── service.js              # 业务逻辑服务
│       ├── models.js               # 数据模型 (ProjectModel, ChatHistoryModel, DrawProviderModel)
│       └── schema.sql              # 数据库建表语句
├── src/
│   ├── js/
│   │   └── modules/
│   │       └── ai-draw.js          # 前端模块
│   ├── css/
│   │   └── ai-draw.css             # 模块样式
│   └── templates/
│       └── ai-draw.html            # HTML 模板
└── public/
    └── ai-draw/                    # [需部署] AI Draw 前端应用
        ├── index.html
        └── assets/
```

## API 端点

### Provider 管理 (独立配置)
- `GET /api/ai-draw/providers` - 获取所有 Provider
- `GET /api/ai-draw/providers/internal` - 获取内部可用的 Provider (来自 AI Chat 模块)
- `GET /api/ai-draw/providers/:id` - 获取单个 Provider
- `POST /api/ai-draw/providers` - 创建 Provider
- `PUT /api/ai-draw/providers/:id` - 更新 Provider
- `DELETE /api/ai-draw/providers/:id` - 删除 Provider
- `POST /api/ai-draw/providers/:id/set-default` - 设置默认 Provider
- `POST /api/ai-draw/providers/:id/test` - 测试 Provider 连接

### 项目管理
- `GET /api/ai-draw/projects` - 获取项目列表
- `GET /api/ai-draw/projects/:id` - 获取单个项目
- `POST /api/ai-draw/projects` - 创建项目
- `PUT /api/ai-draw/projects/:id` - 更新项目
- `DELETE /api/ai-draw/projects/:id` - 删除项目

### AI 对话
- `POST /api/ai-draw/chat` - 同步 AI 对话
- `POST /api/ai-draw/chat/stream` - 流式 AI 对话 (SSE)
- `GET /api/ai-draw/projects/:id/chat` - 获取项目聊天历史
- `DELETE /api/ai-draw/projects/:id/chat` - 清空聊天历史

### 工具
- `POST /api/ai-draw/parse-url` - 解析 URL 内容

## Provider 配置

### 独立 Provider 架构

AI Draw 模块拥有**独立的 Provider 配置系统**，支持两种来源类型：

#### 1. 外部来源 (external)
直接配置 API 地址和密钥，完全独立于其他模块。

```json
{
  "name": "OpenAI",
  "source_type": "external",
  "base_url": "https://api.openai.com/v1",
  "api_key": "sk-...",
  "default_model": "gpt-4o",
  "enabled": true,
  "is_default": true
}
```

#### 2. 内部来源 (internal)
引用 AI Chat 模块已配置的 Provider，共享 API 配置但可覆盖默认模型。

```json
{
  "name": "复用 Claude",
  "source_type": "internal",
  "internal_provider_id": "provider_xxx",
  "default_model": "claude-3-opus-20240229",
  "enabled": true
}
```

### 配置界面

在 AI Draw 模块的 **设置** 标签页中可以：
- 添加/编辑/删除 Provider
- 设置默认 Provider
- 测试 Provider 连接
- 在外部和内部来源之间切换

## 功能特性

### 支持的绘图引擎
1. **Mermaid** - 代码驱动的图表（流程图、时序图、类图等）
2. **Draw.io** - 专业流程图工具

### AI 增强功能
- 自然语言生成图表
- 上下文对话优化
- URL 内容解析作为输入

## 数据库表

```sql
-- Provider 表 (独立配置)
CREATE TABLE ai_draw_providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    source_type TEXT NOT NULL CHECK(source_type IN ('internal', 'external')),
    -- 外部来源配置
    base_url TEXT,
    api_key TEXT,
    default_model TEXT,
    -- 内部来源配置
    internal_provider_id TEXT,
    -- 通用配置
    enabled INTEGER DEFAULT 1,
    is_default INTEGER DEFAULT 0,
    sort_order INTEGER DEFAULT 0,
    created_at DATETIME,
    updated_at DATETIME
);

-- 项目表
CREATE TABLE ai_draw_projects (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT 'Untitled',
    engine_type TEXT NOT NULL,
    content TEXT,
    thumbnail TEXT,
    provider_id TEXT,  -- 关联 Provider
    created_at DATETIME,
    updated_at DATETIME
);

-- 聊天历史表
CREATE TABLE ai_draw_chat_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL,
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at DATETIME
);
```

## 注意事项

1. **Provider 独立配置**：AI Draw 拥有自己的 Provider 配置表，不再强依赖 AI Chat 模块
2. **内部引用可选**：可以选择引用 AI Chat 的 Provider，也可以完全独立配置
3. **前端独立部署**：React 前端作为独立应用部署，通过 iframe 嵌入
4. **跨框架通信**：使用 `postMessage` 实现 Vue 父页面与 React iframe 通信

## 后续开发

- [x] 独立的 Provider 配置系统
- [x] 内部/外部来源切换
- [x] Provider 测试连接功能
- [ ] 完成 ai-draw-nexus 适配 embed 模式
- [ ] 实现 postMessage 双向通信协议
- [ ] 添加 WebSocket 协作支持
