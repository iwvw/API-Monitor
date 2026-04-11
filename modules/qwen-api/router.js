const express = require('express');
const router = express.Router();
const axios = require('axios');
const storage = require('./storage');
const { v4: uuidv4 } = require('uuid');
const logger = {
    info: (...args) => console.log('[QwenAPI]', ...args),
    error: (...args) => console.error('[QwenAPI]', ...args)
};

// 工具函数：提取 Bearer Token
function extractBearerToken(token) {
    if (!token) return '';
    if (token.includes('token=')) {
        const match = token.match(/token=([^;]+)/);
        return match ? match[1] : token;
    }
    return token;
}

// 模拟账号在线探测
async function checkAccountStatus(token) {
    const bearer = extractBearerToken(token);
    try {
        const res = await axios.get('https://chat.qwen.ai/api/models', {
            headers: {
                'Authorization': `Bearer ${bearer}`,
                'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
                'Referer': 'https://chat.qwen.ai/',
            },
            timeout: 5000
        });
        return res.status === 200 ? 'online' : 'offline';
    } catch (e) {
        return 'offline';
    }
}

// ==================== 管理后台接口 ====================

router.get('/stats', (req, res) => {
    res.json(storage.getStats());
});

router.get('/matrix', (req, res) => {
    res.json(storage.getMatrix());
});

router.put('/matrix/:id', (req, res) => {
    res.json(storage.updateMatrixItem(req.params.id, req.body));
});

router.post('/sync-models', async (req, res) => {
    try {
        const result = await storage.syncModelsFromOfficial();
        res.json(result);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

router.get('/accounts', async (req, res) => {
    const accounts = storage.getAccounts();
    res.json(accounts);
    setTimeout(async () => {
        for (const acc of accounts) {
            const status = await checkAccountStatus(acc.token);
            if (status !== acc.status) storage.updateAccount(acc.id, { status });
        }
    }, 100);
});

router.post('/accounts', (req, res) => {
    let name = req.body.name;
    const token = req.body.token;

    if (!name && token) {
        const bearer = extractBearerToken(token);
        try {
            const parts = bearer.split('.');
            if (parts.length >= 2) {
                const payload = JSON.parse(Buffer.from(parts[1], 'base64').toString('utf8'));
                name = payload.nickname || payload.username || payload.email || payload.sub || payload.userId || '自动解析账号';
            }
        } catch (e) {
            name = '未命名凭证';
        }
    }

    const account = { id: uuidv4(), ...req.body, name: name || '未命名凭证' };
    storage.addAccount(account);
    res.json({ success: true, id: account.id });
});

router.delete('/accounts/:id', (req, res) => {
    storage.deleteAccount(req.params.id);
    res.json({ success: true });
});

router.get('/settings', (req, res) => {
    res.json({
        API_KEY: storage.getSetting('API_KEY') || '',
        SYSTEM_INSTRUCTION: storage.getSetting('SYSTEM_INSTRUCTION') || ''
    });
});

router.post('/settings', (req, res) => {
    const { API_KEY, SYSTEM_INSTRUCTION } = req.body;
    storage.updateSetting('API_KEY', API_KEY);
    storage.updateSetting('SYSTEM_INSTRUCTION', SYSTEM_INSTRUCTION);
    res.json({ success: true });
});

router.get('/logs', (req, res) => {
    const db = require('../../src/db/database').getDatabase();
    try {
        const logs = db.prepare(`
            SELECT l.*, a.name AS account_name 
            FROM qwen_logs l 
            LEFT JOIN qwen_accounts a ON l.account_id = a.id 
            ORDER BY l.created_at DESC 
            LIMIT 200
        `).all();
        res.json(logs);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

router.delete('/logs', (req, res) => {
    // ... (保持原样)
});

// ==================== 模型重定向 (别名) ====================
router.get('/models/redirects', (req, res) => {
    res.json(storage.getModelRedirects());
});

router.post('/models/redirects', (req, res) => {
    const { sourceModel, targetModel } = req.body;
    if (!sourceModel || !targetModel) return res.status(400).json({ error: 'Missing parameters' });
    try {
        res.json(storage.saveModelRedirect(sourceModel, targetModel));
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

router.delete('/models/redirects/:source', (req, res) => {
    try {
        res.json(storage.deleteModelRedirect(req.params.source));
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// ==================== OpenAI 兼容聊天接口 ====================
router.post('/v1/chat/completions', async (req, res) => {
    let { model, messages, stream } = req.body;
    const ts = Math.floor(Date.now() / 1000);
    const startTime = Date.now();
    
    // 应用模型重定向逻辑
    const redirects = storage.getModelRedirects();
    const redirection = redirects.find(r => r.source_model === model);
    if (redirection) {
        logger.info(`模型重定向: ${model} -> ${redirection.target_model}`);
        model = redirection.target_model;
    }

    const targetModel = model;

    // 构建注入式上下文
    let contextPrompt = '';
    if (messages.length > 1) {
        contextPrompt = messages.slice(0, -1).map(m => {
            const roleName = m.role === 'assistant' ? 'Assistant' : 'User';
            const content = typeof m.content === 'string' ? m.content : JSON.stringify(m.content);
            return `${roleName}: ${content}`;
        }).join('\n\n') + '\n\n---\n\n';
    }

    const lastMsg = messages[messages.length - 1];
    const currentContent = typeof lastMsg.content === 'string' ? lastMsg.content : JSON.stringify(lastMsg.content);
    const finalContent = contextPrompt + currentContent;

    // 智慧意图识别：检测是否需要激活绘画模式
    const isImageRequest = currentContent.includes('画') || currentContent.includes('生成图片') || currentContent.includes('image');
    const subType = isImageRequest ? 't2i' : 't2t';

    const qwenMsg = {
        fid: uuidv4(), parentId: null, childrenIds: [uuidv4()],
        role: 'user', content: finalContent, user_action: 'chat',
        timestamp: ts, models: [targetModel], chat_type: subType,
        feature_config: {
            thinking_enabled: true, output_schema: 'phase', research_mode: 'normal',
            auto_thinking: true, thinking_mode: 'Auto', thinking_format: 'summary',
            auto_search: false, code_interpreter: false, function_calling: false, 
            plugins_enabled: true,
            image_generation: isImageRequest, 
            default_aspect_ratio: '1:1',
        },
        extra: { meta: { subChatType: subType, mode: isImageRequest ? 'image_generation' : undefined } },
        sub_chat_type: subType, parent_id: null,
    };

    const accounts = storage.getAccounts().filter(a => a.enable !== 0 && a.status !== 'invalid');
    if (accounts.length === 0) return res.status(503).json({ error: 'No valid accounts' });
    const account = accounts[Math.floor(Math.random() * accounts.length)];
    const bearerToken = extractBearerToken(account.token);
    const commonHeaders = {
        'Authorization': `Bearer ${bearerToken}`,
        'Content-Type': 'application/json',
        'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36',
    };

    let chatId = null;
    try {
        const createRes = await axios.post('https://chat.qwen.ai/api/v2/chats/new', {
            title: `chat_${ts}`, models: [targetModel], chat_mode: 'normal', chat_type: 't2t', timestamp: ts,
        }, { headers: commonHeaders, timeout: 15000 });

        chatId = createRes.data?.data?.id;
        if (!chatId) throw new Error('创建会话失败');

        const response = await axios({
            method: 'post',
            url: `https://chat.qwen.ai/api/v2/chat/completions?chat_id=${chatId}`,
            headers: { ...commonHeaders, 'Accept': 'text/event-stream' },
            data: {
                stream: true, version: '2.1', incremental_output: true,
                chat_id: chatId, chat_mode: 'normal', model: targetModel,
                parent_id: null, messages: [qwenMsg], timestamp: ts,
            },
            responseType: 'stream',
            timeout: 180000,
        });

        if (stream) {
            res.setHeader('Content-Type', 'text/event-stream');
            res.setHeader('X-Accel-Buffering', 'no');
            res.setHeader('Cache-Control', 'no-cache');
            res.setHeader('Connection', 'keep-alive');
            res.setHeader('Transfer-Encoding', 'chunked');
            res.flushHeaders(); // 立即发送头部
        }

        let fullContent = '';
        let fullReasoning = '';
        let buffer = '';
        let firstTokenTimeMs = null;
        const responseId = `chatcmpl-${uuidv4()}`;

        response.data.on('data', chunk => {
            buffer += chunk.toString();
            const lines = buffer.split('\n');
            buffer = lines.pop();

            for (let line of lines) {
                line = line.trim();
                if (!line.startsWith('data:')) continue;
                const dataStr = line.substring(5).trim();
                if (dataStr === '[DONE]') continue;
                
                try {
                    // 调试：打印原始流数据
                    if (dataStr.includes('extra') || dataStr.includes('wanx')) {
                        logger.info('探测到潜在图片数据:', dataStr);
                    }

                    const data = JSON.parse(dataStr);
                    const delta = data.choices?.[0]?.delta || {};
                    const phase = delta.phase || data.phase || 'answer';
                    
                    // 深度思维链解析 (支持 Qwen 3.6 thinking_summary)
                    const isThinking = phase.includes('think') || phase.includes('thought');
                    let text = delta.reasoning_content || delta.content || data.content || data.text || '';
                    
                    // 提取隐藏在 extra 中的总结性思考
                    if (delta.extra?.summary_thought?.content) {
                        const thoughtItems = delta.extra.summary_thought.content;
                        text += Array.isArray(thoughtItems) ? thoughtItems.join('') : thoughtItems;
                    }

                    // 图片链接智能防御：如果 content 本身就是官网图片链接，自动 Markdown 化
                    if (text.startsWith('https://cdn.qwenlm.ai/output/') && !text.includes('![')) {
                        text = `\n\n![Generated Image](${text})\n\n`;
                    }

                    if (text && !firstTokenTimeMs) {
                        firstTokenTimeMs = Date.now() - startTime;
                    }

                    if (isThinking) fullReasoning += text;
                    else fullContent += text;

                    // 图片生图链接捕获 (Wanx 增强支持)
                    const wanxUrl = data.extra?.wanx_image_url || data.extra?.tool_result?.[0]?.image;
                    if (wanxUrl) {
                        const imgMd = `\n\n![Generated Image](${wanxUrl})`;
                        if (!fullContent.includes(wanxUrl)) {
                            fullContent += imgMd;
                            if (stream) {
                                res.write(`data: ${JSON.stringify({
                                    id: responseId, object: 'chat.completion.chunk', created: ts,
                                    model: targetModel, choices: [{ index: 0, delta: { content: imgMd }, finish_reason: null }]
                                })}\n\n`);
                            }
                        }
                    }

                    if (stream && text) {
                        const outDelta = isThinking ? { reasoning_content: text } : { content: text };
                        res.write(`data: ${JSON.stringify({
                            id: responseId, object: 'chat.completion.chunk', created: ts,
                            model: targetModel, choices: [{ index: 0, delta: outDelta, finish_reason: null }]
                        })}\n\n`);
                    }
                } catch (e) {}
            }
        });

        response.data.on('end', () => {
            const duration = Date.now() - startTime;
            const tokens = Math.ceil((finalContent.length + fullContent.length) * 1.5);
            
            // 全量入库：包含上下文和首字耗时
            storage.addLog({
                account_id: account.id, model: targetModel, prompt: currentContent,
                response: fullContent, tokens, duration, status: 'success',
                first_token_time_ms: firstTokenTimeMs,
                messages: messages // 存入完整上下文数组
            });
            storage.updateAccount(account.id, { status: 'online' });

            if (stream) {
                res.write(`data: ${JSON.stringify({
                    id: responseId, object: 'chat.completion.chunk', created: ts,
                    model: targetModel, choices: [{ index: 0, delta: {}, finish_reason: 'stop' }]
                })}\n\n`);
                res.write('data: [DONE]\n\n');
                res.end();
            } else {
                res.json({
                    id: responseId, object: 'chat.completion', created: ts, model: targetModel,
                    choices: [{ index: 0, message: { role: 'assistant', content: fullContent, reasoning_content: fullReasoning }, finish_reason: 'stop' }]
                });
            }
            if (chatId) axios.delete(`https://chat.qwen.ai/api/v2/chats/${chatId}`, { headers: commonHeaders, timeout: 5000 }).catch(() => {});
        });

    } catch (e) {
        const duration = Date.now() - startTime;
        storage.addLog({
            account_id: account?.id, model: targetModel, prompt: currentContent,
            response: '', tokens: 0, duration, status: 'failed', error: e.message,
            messages: messages
        });
        if (account?.id) storage.updateAccount(account.id, { status: 'offline' });
        if (!res.headersSent) res.status(500).json({ error: e.message });
        if (chatId) axios.delete(`https://chat.qwen.ai/api/v2/chats/${chatId}`, { headers: commonHeaders, timeout: 5000 }).catch(() => {});
    }
});

// 图片生成逻辑同步升级
router.post('/v1/images/generations', async (req, res) => {
    const { prompt, model = 'qwen3.6-plus', size = '1024x1024' } = req.body;
    const startTime = Date.now();
    const accounts = storage.getAccounts().filter(a => a.enable !== 0 && a.status !== 'invalid');
    if (accounts.length === 0) return res.status(503).json({ error: 'No valid accounts' });

    const account = accounts[Math.floor(Math.random() * accounts.length)];
    const bearerToken = extractBearerToken(account.token);
    const ts = Math.floor(Date.now() / 1000);
    const commonHeaders = { 'Authorization': `Bearer ${bearerToken}`, 'Content-Type': 'application/json', 'User-Agent': 'Mozilla/5.0' };

    let chatId = null;
    try {
        const createRes = await axios.post('https://chat.qwen.ai/api/v2/chats/new', {
            title: `t2i_${ts}`, models: [model], chat_mode: 'normal', chat_type: 't2i', timestamp: ts,
        }, { headers: commonHeaders });
        chatId = createRes.data?.data?.id;
        if (!chatId) throw new Error('创建会话失败');

        const response = await axios({
            method: 'post',
            url: `https://chat.qwen.ai/api/v2/chat/completions?chat_id=${chatId}`,
            headers: { ...commonHeaders, 'Accept': 'text/event-stream' },
            data: {
                stream: true, version: '2.1', chat_id: chatId, model: model,
                messages: [{
                    fid: uuidv4(), role: 'user', content: prompt, user_action: 'chat',
                    feature_config: { image_generation: true, plugins_enabled: true, default_aspect_ratio: size === '1024x1024' ? '1:1' : '16:9' },
                    extra: { meta: { subChatType: 't2i', mode: 'image_generation' } },
                    sub_chat_type: 't2i',
                }],
            },
            responseType: 'stream',
        });

        let imageUrl = null;
        let buffer = '';
        response.data.on('data', chunk => {
            buffer += chunk.toString();
            const lines = buffer.split('\n');
            buffer = lines.pop();
            for (let line of lines) {
                line = line.trim(); if (!line.startsWith('data:')) continue;
                try {
                    const data = JSON.parse(line.substring(5).trim());
                    const delta = data.choices?.[0]?.delta || data;
                    const content = delta.content || delta.text || '';
                    const extra = delta.extra || {};
                    if (content.startsWith('http')) imageUrl = content;
                    else if (extra.wanx_image_url) imageUrl = extra.wanx_image_url;
                    else if (extra.tool_result?.[0]?.image) imageUrl = extra.tool_result[0].image;
                } catch (e) {}
            }
        });

        response.data.on('end', () => {
            const duration = Date.now() - startTime;
            storage.addLog({
                account_id: account.id, model: model, prompt: prompt,
                response: imageUrl || '', tokens: 0, duration, status: imageUrl ? 'success' : 'failed',
                messages: [{ role: 'user', content: prompt }]
            });
            if (imageUrl) storage.updateAccount(account.id, { status: 'online' });
            if (imageUrl) res.json({ created: ts, data: [{ url: imageUrl }] });
            else res.status(500).json({ error: 'Failed to extract image' });
            if (chatId) axios.delete(`https://chat.qwen.ai/api/v2/chats/${chatId}`, { headers: commonHeaders, timeout: 5000 }).catch(() => {});
        });

    } catch (e) {
        const duration = Date.now() - startTime;
        storage.addLog({ account_id: account?.id, model: model, prompt: prompt, response: '', tokens: 0, duration, status: 'failed', error: e.message });
        res.status(500).json({ error: e.message });
        if (chatId) axios.delete(`https://chat.qwen.ai/api/v2/chats/${chatId}`, { headers: commonHeaders, timeout: 5000 }).catch(() => {});
    }
});

module.exports = router;
