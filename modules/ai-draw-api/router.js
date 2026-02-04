/**
 * AI Draw 模块 - API 路由
 * 
 * 独立的 Provider 管理和项目管理
 */

const express = require('express');
const router = express.Router();
const { DrawProviderModel, ProjectModel, ChatHistoryModel } = require('./models');
const aiDrawService = require('./service');
const { createLogger } = require('../../src/utils/logger');
const { requireAuth } = require('../../src/middleware/auth');

const logger = createLogger('AIDraw');

// 所有路由需要认证
router.use(requireAuth);

// ==================== Provider API ====================

/**
 * 获取所有 Provider (不返回 API Key)
 */
router.get('/providers', (req, res) => {
    try {
        const providers = DrawProviderModel.getAll();
        const safeProviders = providers.map(p => ({
            id: p.id,
            name: p.name,
            source_type: p.source_type,
            base_url: p.base_url,
            default_model: p.default_model,
            internal_provider_id: p.internal_provider_id,
            enabled: p.enabled,
            is_default: p.is_default,
            sort_order: p.sort_order,
            created_at: p.created_at,
            updated_at: p.updated_at,
            // 不返回 api_key
        }));
        res.json({ success: true, data: safeProviders });
    } catch (error) {
        logger.error('获取 Provider 列表失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取内部可用的 Provider 列表（来自 ai-chat-api）
 */
router.get('/providers/internal', (req, res) => {
    try {
        const providers = aiDrawService.getInternalProviders();
        res.json({ success: true, data: providers });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取单个 Provider
 */
router.get('/providers/:id', (req, res) => {
    try {
        const provider = DrawProviderModel.getById(req.params.id);
        if (!provider) {
            return res.status(404).json({ success: false, error: 'Provider 不存在' });
        }
        // 不返回 api_key
        const { api_key, ...safeProvider } = provider;
        res.json({ success: true, data: safeProvider });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 创建 Provider
 */
router.post('/providers', (req, res) => {
    try {
        const { name, source_type, base_url, api_key, default_model, internal_provider_id, enabled, is_default } = req.body;

        if (!name) {
            return res.status(400).json({ success: false, error: '名称不能为空' });
        }

        if (source_type === 'external' && (!base_url || !api_key)) {
            return res.status(400).json({ success: false, error: '外部来源需要填写 API 地址和密钥' });
        }

        if (source_type === 'internal' && !internal_provider_id) {
            return res.status(400).json({ success: false, error: '内部来源需要选择 Provider' });
        }

        const provider = DrawProviderModel.create({
            name,
            source_type: source_type || 'external',
            base_url,
            api_key,
            default_model,
            internal_provider_id,
            enabled: enabled !== false,
            is_default: Boolean(is_default),
        });

        logger.info(`Provider 已创建: ${provider.id} (${provider.name})`);

        // 不返回 api_key
        const { api_key: _, ...safeProvider } = provider;
        res.json({ success: true, data: safeProvider });
    } catch (error) {
        logger.error('创建 Provider 失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 更新 Provider
 */
router.put('/providers/:id', (req, res) => {
    try {
        const { name, source_type, base_url, api_key, default_model, internal_provider_id, enabled, is_default, sort_order } = req.body;

        const provider = DrawProviderModel.update(req.params.id, {
            name,
            source_type,
            base_url,
            api_key,
            default_model,
            internal_provider_id,
            enabled,
            is_default,
            sort_order,
        });

        if (!provider) {
            return res.status(404).json({ success: false, error: 'Provider 不存在' });
        }

        // 不返回 api_key
        const { api_key: _, ...safeProvider } = provider;
        res.json({ success: true, data: safeProvider });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 删除 Provider
 */
router.delete('/providers/:id', (req, res) => {
    try {
        const deleted = DrawProviderModel.delete(req.params.id);
        if (!deleted) {
            return res.status(404).json({ success: false, error: 'Provider 不存在' });
        }
        res.json({ success: true });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 设置默认 Provider
 */
router.post('/providers/:id/set-default', (req, res) => {
    try {
        const provider = DrawProviderModel.setDefault(req.params.id);
        if (!provider) {
            return res.status(404).json({ success: false, error: 'Provider 不存在' });
        }
        const { api_key: _, ...safeProvider } = provider;
        res.json({ success: true, data: safeProvider });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 测试 Provider 连接
 */
router.post('/providers/:id/test', async (req, res) => {
    try {
        const provider = DrawProviderModel.getById(req.params.id);
        if (!provider) {
            return res.status(404).json({ success: false, error: 'Provider 不存在' });
        }

        const result = await aiDrawService.testProvider(provider);
        res.json({ success: true, data: result });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 项目 API ====================

/**
 * 获取所有项目
 */
router.get('/projects', (req, res) => {
    try {
        const limit = parseInt(req.query.limit) || 50;
        const projects = ProjectModel.getAll(limit);
        res.json({ success: true, data: projects });
    } catch (error) {
        logger.error('获取项目列表失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 获取单个项目
 */
router.get('/projects/:id', (req, res) => {
    try {
        const project = ProjectModel.getById(req.params.id);
        if (!project) {
            return res.status(404).json({ success: false, error: '项目不存在' });
        }
        res.json({ success: true, data: project });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 创建项目
 */
router.post('/projects', (req, res) => {
    try {
        const { title, engine_type, content, provider_id } = req.body;
        const project = ProjectModel.create({
            title: title || `Untitled-${Date.now()}`,
            engine_type: engine_type || 'drawio',
            content: content || null,
            provider_id: provider_id || null,
        });
        logger.info(`项目已创建: ${project.id} (${project.engine_type})`);
        res.json({ success: true, data: project });
    } catch (error) {
        logger.error('创建项目失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 更新项目
 */
router.put('/projects/:id', (req, res) => {
    try {
        const { title, content, thumbnail, engine_type, provider_id } = req.body;
        const project = ProjectModel.update(req.params.id, {
            title,
            content,
            thumbnail,
            engine_type,
            provider_id,
        });
        if (!project) {
            return res.status(404).json({ success: false, error: '项目不存在' });
        }
        res.json({ success: true, data: project });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * 删除项目
 */
router.delete('/projects/:id', (req, res) => {
    try {
        const deleted = ProjectModel.delete(req.params.id);
        if (!deleted) {
            return res.status(404).json({ success: false, error: '项目不存在' });
        }
        // 同时删除聊天历史
        ChatHistoryModel.clearByProject(req.params.id);
        res.json({ success: true });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 聊天 API ====================

/**
 * 获取项目聊天历史
 */
router.get('/projects/:id/chat', (req, res) => {
    try {
        const messages = ChatHistoryModel.getByProject(req.params.id);
        res.json({ success: true, data: messages });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * AI 对话 (同步)
 */
router.post('/chat', async (req, res) => {
    try {
        const { project_id, message, engine_type, provider_id, model } = req.body;

        if (!message) {
            return res.status(400).json({ success: false, error: '消息不能为空' });
        }

        // 获取历史消息作为上下文
        let contextMessages = [];
        if (project_id) {
            const history = ChatHistoryModel.getByProject(project_id, 20);
            contextMessages = history.map(m => ({ role: m.role, content: m.content }));
        }

        // 添加当前消息
        contextMessages.push({ role: 'user', content: message });

        // 调用 AI
        const result = await aiDrawService.chat(contextMessages, {
            engineType: engine_type,
            providerId: provider_id,
            model,
        });

        // 保存聊天历史
        if (project_id) {
            ChatHistoryModel.add(project_id, 'user', message);
            ChatHistoryModel.add(project_id, 'assistant', result.content);
        }

        res.json({
            success: true,
            data: {
                content: result.content,
                usage: result.usage,
            },
        });
    } catch (error) {
        logger.error('AI 对话失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * AI 对话 (流式 SSE)
 */
router.post('/chat/stream', async (req, res) => {
    const { project_id, message, engine_type, provider_id, model } = req.body;

    if (!message) {
        return res.status(400).json({ success: false, error: '消息不能为空' });
    }

    // 设置 SSE 响应头
    res.setHeader('Content-Type', 'text/event-stream');
    res.setHeader('Cache-Control', 'no-cache');
    res.setHeader('Connection', 'keep-alive');
    res.setHeader('X-Accel-Buffering', 'no');

    try {
        // 构建上下文
        let contextMessages = [];
        if (project_id) {
            const history = ChatHistoryModel.getByProject(project_id, 20);
            contextMessages = history.map(m => ({ role: m.role, content: m.content }));
        }
        contextMessages.push({ role: 'user', content: message });

        // 保存用户消息
        if (project_id) {
            ChatHistoryModel.add(project_id, 'user', message);
        }

        // 流式调用
        let fullContent = '';
        for await (const chunk of aiDrawService.chatStream(contextMessages, {
            engineType: engine_type,
            providerId: provider_id,
            model,
        })) {
            fullContent += chunk;
            res.write(`data: ${JSON.stringify({ type: 'chunk', data: chunk })}\n\n`);
        }

        // 保存助手回复
        if (project_id) {
            ChatHistoryModel.add(project_id, 'assistant', fullContent);
        }

        res.write(`data: ${JSON.stringify({ type: 'done', data: { content: fullContent } })}\n\n`);
        res.end();
    } catch (error) {
        logger.error('流式对话失败:', error.message);
        res.write(`data: ${JSON.stringify({ type: 'error', data: error.message })}\n\n`);
        res.end();
    }
});

/**
 * 清空项目聊天历史
 */
router.delete('/projects/:id/chat', (req, res) => {
    try {
        const count = ChatHistoryModel.clearByProject(req.params.id);
        res.json({ success: true, deleted: count });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 工具 API ====================

/**
 * 解析 URL
 */
router.post('/parse-url', async (req, res) => {
    try {
        const { url } = req.body;
        if (!url) {
            return res.status(400).json({ success: false, error: 'URL 不能为空' });
        }

        const result = await aiDrawService.parseUrl(url);
        res.json({ success: true, data: result });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

module.exports = router;
