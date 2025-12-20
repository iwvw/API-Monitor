/**
 * NextChat 模块 - API 路由
 * 提供会话管理和聊天功能的 REST API
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
const { createLogger } = require('../../src/utils/logger');

const logger = createLogger('NextChat');

// 初始化数据库
try {
    storage.initDatabase();
    logger.info('NextChat 数据库初始化成功');
} catch (error) {
    logger.error('NextChat 数据库初始化失败:', error.message);
}

// ==================== 会话 API ====================

/**
 * GET /sessions - 获取所有会话
 */
router.get('/sessions', (req, res) => {
    try {
        const sessions = storage.getAllSessions();
        res.json({ success: true, data: sessions });
    } catch (error) {
        logger.error('获取会话列表失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * GET /sessions/:id - 获取单个会话
 */
router.get('/sessions/:id', (req, res) => {
    try {
        const session = storage.getSession(req.params.id);
        if (!session) {
            return res.status(404).json({ success: false, error: '会话不存在' });
        }
        res.json({ success: true, data: session });
    } catch (error) {
        logger.error('获取会话失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * POST /sessions - 创建新会话
 */
router.post('/sessions', (req, res) => {
    try {
        const session = storage.createSession(req.body);
        logger.success(`创建会话: ${session.id}`);
        res.json({ success: true, data: session });
    } catch (error) {
        logger.error('创建会话失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * PUT /sessions/:id - 更新会话
 */
router.put('/sessions/:id', (req, res) => {
    try {
        const session = storage.updateSession(req.params.id, req.body);
        if (!session) {
            return res.status(404).json({ success: false, error: '会话不存在' });
        }
        res.json({ success: true, data: session });
    } catch (error) {
        logger.error('更新会话失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * DELETE /sessions/:id - 删除会话
 */
router.delete('/sessions/:id', (req, res) => {
    try {
        const deleted = storage.deleteSession(req.params.id);
        if (!deleted) {
            return res.status(404).json({ success: false, error: '会话不存在' });
        }
        logger.success(`删除会话: ${req.params.id}`);
        res.json({ success: true, message: '会话已删除' });
    } catch (error) {
        logger.error('删除会话失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * DELETE /sessions - 清空所有会话
 */
router.delete('/sessions', (req, res) => {
    try {
        const count = storage.clearAllSessions();
        logger.success(`清空所有会话: ${count} 个`);
        res.json({ success: true, message: `已删除 ${count} 个会话` });
    } catch (error) {
        logger.error('清空会话失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 消息 API ====================

/**
 * GET /sessions/:id/messages - 获取会话的所有消息
 */
router.get('/sessions/:id/messages', (req, res) => {
    try {
        const session = storage.getSession(req.params.id);
        if (!session) {
            return res.status(404).json({ success: false, error: '会话不存在' });
        }
        const messages = storage.getMessages(req.params.id);
        res.json({ success: true, data: messages });
    } catch (error) {
        logger.error('获取消息列表失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * POST /sessions/:id/messages - 添加消息到会话
 */
router.post('/sessions/:id/messages', (req, res) => {
    try {
        const session = storage.getSession(req.params.id);
        if (!session) {
            return res.status(404).json({ success: false, error: '会话不存在' });
        }
        const message = storage.addMessage(req.params.id, req.body);
        res.json({ success: true, data: message });
    } catch (error) {
        logger.error('添加消息失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * DELETE /messages/:id - 删除消息
 */
router.delete('/messages/:id', (req, res) => {
    try {
        const deleted = storage.deleteMessage(req.params.id);
        if (!deleted) {
            return res.status(404).json({ success: false, error: '消息不存在' });
        }
        res.json({ success: true, message: '消息已删除' });
    } catch (error) {
        logger.error('删除消息失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * DELETE /sessions/:id/messages - 清空会话的所有消息
 */
router.delete('/sessions/:id/messages', (req, res) => {
    try {
        const count = storage.clearMessages(req.params.id);
        logger.success(`清空会话消息: ${req.params.id}, ${count} 条`);
        res.json({ success: true, message: `已删除 ${count} 条消息` });
    } catch (error) {
        logger.error('清空消息失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 聊天 API ====================

/**
 * POST /chat - 发送消息并获取回复
 * 使用现有的 Antigravity 或 Gemini CLI 后端
 */
router.post('/chat', async (req, res) => {
    try {
        const { session_id, content, model, stream = true } = req.body;

        if (!session_id || !content) {
            return res.status(400).json({
                success: false,
                error: '缺少必要参数: session_id, content'
            });
        }

        // 获取会话
        const session = storage.getSession(session_id);
        if (!session) {
            return res.status(404).json({ success: false, error: '会话不存在' });
        }

        // 保存用户消息
        const userMessage = storage.addMessage(session_id, {
            role: 'user',
            content: content
        });

        // 获取历史消息作为上下文
        const messages = storage.getMessages(session_id);

        // 构建请求体
        const chatMessages = messages.map(m => ({
            role: m.role,
            content: m.content
        }));

        // 如果有系统提示，添加到开头
        if (session.system_prompt) {
            chatMessages.unshift({
                role: 'system',
                content: session.system_prompt
            });
        }

        const useModel = model || session.model || 'gemini-2.5-flash';

        // 确定使用哪个后端
        // Gemini 模型使用 Gemini CLI，其他使用 Antigravity
        const isGeminiModel = useModel.toLowerCase().includes('gemini');

        if (stream) {
            // 流式响应
            res.setHeader('Content-Type', 'text/event-stream');
            res.setHeader('Cache-Control', 'no-cache');
            res.setHeader('Connection', 'keep-alive');

            let fullResponse = '';

            try {
                // 使用内部 API 调用
                const http = require('http');
                const apiPath = isGeminiModel ? '/api/gemini-cli-api/chat' : '/api/antigravity/chat';

                const requestBody = JSON.stringify({
                    model: useModel,
                    messages: chatMessages,
                    stream: true
                });

                const options = {
                    hostname: '127.0.0.1',
                    port: process.env.PORT || 3000,
                    path: apiPath,
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'Content-Length': Buffer.byteLength(requestBody)
                    }
                };

                const proxyReq = http.request(options, (proxyRes) => {
                    proxyRes.on('data', (chunk) => {
                        const text = chunk.toString();
                        res.write(text);

                        // 解析 SSE 数据
                        const lines = text.split('\n');
                        for (const line of lines) {
                            if (line.startsWith('data: ')) {
                                try {
                                    const data = JSON.parse(line.slice(6));
                                    if (data.choices && data.choices[0].delta && data.choices[0].delta.content) {
                                        fullResponse += data.choices[0].delta.content;
                                    }
                                } catch (e) {
                                    // 忽略解析错误
                                }
                            }
                        }
                    });

                    proxyRes.on('end', () => {
                        // 保存助手回复
                        if (fullResponse) {
                            storage.addMessage(session_id, {
                                role: 'assistant',
                                content: fullResponse,
                                model: useModel
                            });
                        }
                        res.write('data: [DONE]\n\n');
                        res.end();
                    });
                });

                proxyReq.on('error', (error) => {
                    logger.error('代理请求失败:', error.message);
                    res.write(`data: ${JSON.stringify({ error: error.message })}\n\n`);
                    res.end();
                });

                proxyReq.write(requestBody);
                proxyReq.end();

            } catch (error) {
                logger.error('聊天失败:', error.message);
                res.write(`data: ${JSON.stringify({ error: error.message })}\n\n`);
                res.end();
            }
        } else {
            // 非流式响应
            try {
                const http = require('http');
                const apiPath = isGeminiModel ? '/api/gemini-cli-api/chat' : '/api/antigravity/chat';

                const requestBody = JSON.stringify({
                    model: useModel,
                    messages: chatMessages,
                    stream: false
                });

                const options = {
                    hostname: '127.0.0.1',
                    port: process.env.PORT || 3000,
                    path: apiPath,
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json',
                        'Content-Length': Buffer.byteLength(requestBody)
                    }
                };

                const proxyReq = http.request(options, (proxyRes) => {
                    let data = '';
                    proxyRes.on('data', chunk => data += chunk);
                    proxyRes.on('end', () => {
                        try {
                            const response = JSON.parse(data);
                            const assistantContent = response.choices?.[0]?.message?.content || '';

                            // 保存助手回复
                            const assistantMessage = storage.addMessage(session_id, {
                                role: 'assistant',
                                content: assistantContent,
                                model: useModel
                            });

                            res.json({
                                success: true,
                                data: {
                                    user_message: userMessage,
                                    assistant_message: assistantMessage,
                                    response: response
                                }
                            });
                        } catch (e) {
                            res.status(500).json({ success: false, error: '解析响应失败' });
                        }
                    });
                });

                proxyReq.on('error', (error) => {
                    res.status(500).json({ success: false, error: error.message });
                });

                proxyReq.write(requestBody);
                proxyReq.end();

            } catch (error) {
                logger.error('聊天失败:', error.message);
                res.status(500).json({ success: false, error: error.message });
            }
        }
    } catch (error) {
        logger.error('聊天 API 错误:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

// ==================== 统计 API ====================

/**
 * GET /stats - 获取统计信息
 */
router.get('/stats', (req, res) => {
    try {
        const stats = storage.getStats();
        res.json({ success: true, data: stats });
    } catch (error) {
        logger.error('获取统计信息失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

/**
 * GET /models - 获取可用模型列表
 */
router.get('/models', async (req, res) => {
    try {
        // 聚合 Antigravity 和 Gemini CLI 的模型列表
        const models = [];

        // Gemini 模型
        const geminiModels = [
            'gemini-2.5-flash',
            'gemini-2.5-pro',
            'gemini-2.0-flash',
            'gemini-1.5-pro',
            'gemini-1.5-flash'
        ];

        geminiModels.forEach(name => {
            models.push({
                id: name,
                name: name,
                provider: 'gemini'
            });
        });

        // OpenAI 兼容模型 (通过 Antigravity)
        const openaiModels = [
            'gpt-4o',
            'gpt-4o-mini',
            'gpt-4-turbo',
            'claude-3-5-sonnet',
            'claude-3-opus'
        ];

        openaiModels.forEach(name => {
            models.push({
                id: name,
                name: name,
                provider: 'antigravity'
            });
        });

        res.json({ success: true, data: models });
    } catch (error) {
        logger.error('获取模型列表失败:', error.message);
        res.status(500).json({ success: false, error: error.message });
    }
});

module.exports = router;
