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
│       ├── models.js               # 数据模型
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
- `GET /api/ai-draw/providers` - 获取可用 AI Provider

## 前端集成

AI Draw 采用 iframe 嵌入方式，需要将 `ai-draw-nexus` 项目构建后部署：

```bash
# 在 ai-draw-nexus 目录中
cd ai-draw-nexus
pnpm install
pnpm build

# 将构建产物复制到 API Monitor
cp -r dist/* ../api-monitor/public/ai-draw/
```

## 功能特性

### 支持的绘图引擎
1. **Mermaid** - 代码驱动的图表（流程图、时序图、类图等）
2. **Excalidraw** - 手绘风格白板
3. **Draw.io** - 专业流程图工具

### AI 增强功能
- 自然语言生成图表
- 上下文对话优化
- URL 内容解析作为输入

### Provider 复用
AI Draw 复用 AI Chat 模块的 LLM Provider 配置，无需单独配置。

## 数据库表

```sql
-- 项目表
CREATE TABLE ai_draw_projects (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT 'Untitled',
    engine_type TEXT NOT NULL,
    content TEXT,
    thumbnail TEXT,
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

1. **依赖 AI Chat 模块**：AI Draw 需要 AI Chat 模块提供 LLM Provider
2. **前端独立部署**：React 前端作为独立应用部署，通过 iframe 嵌入
3. **跨框架通信**：使用 `postMessage` 实现 Vue 父页面与 React iframe 通信

## 后续开发

- [ ] 完成 ai-draw-nexus 适配 embed 模式
- [ ] 实现 postMessage 双向通信协议
- [ ] 添加 WebSocket 协作支持
