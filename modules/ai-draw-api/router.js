/**
 * AI Draw 模块 - API 路由
 */

const express = require('express');
const router = express.Router();
const { ProjectModel, ChatHistoryModel } = require('./models');
const aiDrawService = require('./service');
const { createLogger } = require('../../src/utils/logger');
const { requireAuth } = require('../../src/middleware/auth');

const logger = createLogger('AIDraw');

// 所有路由需要认证
router.use(requireAuth);

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
        const { title, engine_type, content } = req.body;
        const project = ProjectModel.create({
            title: title || `Untitled-${Date.now()}`,
            engine_type: engine_type || 'drawio',
            content: content || null,
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
        const { title, content, thumbnail, engine_type } = req.body;
        const project = ProjectModel.update(req.params.id, {
            title,
            content,
            thumbnail,
            engine_type,
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

/**
 * 获取可用的 AI Provider 列表
 */
router.get('/providers', (req, res) => {
    try {
        const { providerStorage } = require('../ai-chat-api/storage');
        const providers = providerStorage.getAll();

        // 不返回 API Key
        const safeProviders = providers.map(p => ({
            id: p.id,
            name: p.name,
            type: p.type,
            default_model: p.default_model,
            is_default: p.is_default,
        }));

        res.json({ success: true, data: safeProviders });
    } catch (error) {
        res.status(500).json({ success: false, error: error.message });
    }
});

module.exports = router;
