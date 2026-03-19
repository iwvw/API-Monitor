/**
 * DeepSeek 模块路由层
 * 提供账号管理 API + OpenAI 兼容的对话接口
 *
 * 路由挂载于 /api/deepseek
 */

const express = require('express');
const router = express.Router();
const storage = require('./storage');
const client = require('./deepseek-client');
const { parseSSEStream, collectStream } = require('./sse-parser');
const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('DS-Router');
const { getSession, getSessionById } = require('../../src/services/session');
const fs = require('fs');
const path = require('path');

// ==================== Session 缓存 (用于维系上下文对话) ====================
// 外部客户端可能将 reasoning_content 与 content 合并发回
// 因此使用子串包含匹配，同时持久化到数据库以支持重启后续接
const sessionCache = new Map();

function extractContentKey(content) {
    const trimmed = content.trim();
    if (trimmed.length < 10) return null;
    // 取中段 20~120 字符 (跳过开头可能被客户端截断的部分)
    return trimmed.slice(Math.min(20, Math.floor(trimmed.length / 4)), Math.min(120, trimmed.length));
}

function saveToSessionCache(content, sessionId, parentId) {
    if (!content || !sessionId) return;
    const key = extractContentKey(content);
    if (!key) return;
    // 写入内存缓存
    sessionCache.set(key, { sessionId, parentId });
    if (sessionCache.size > 500) {
        const firstKey = sessionCache.keys().next().value;
        sessionCache.delete(firstKey);
    }
    // 持久化到数据库
    storage.saveSessionCache(key, sessionId, parentId);
}

function findSessionIdByMessages(messages) {
    // 倒序查找最近的 assistant 回复
    for (let i = messages.length - 1; i >= 0; i--) {
        if (messages[i].role === 'assistant' && messages[i].content) {
            const incoming = messages[i].content;
            // 1. 先查内存缓存
            for (const [key, value] of sessionCache.entries()) {
                if (incoming.includes(key)) {
                    return value;
                }
            }
            // 2. 内存未命中，查数据库
            const dbResult = storage.findSessionCache(incoming);
            if (dbResult) {
                // 回填到内存
                const key = extractContentKey(incoming);
                if (key) sessionCache.set(key, dbResult);
                return dbResult;
            }
        }
    }
    return null;

}

// ==================== API Key 认证中间件 ====================

function requireApiKey(req, res, next) {
    // 0. 如果经过了 v1 网关的负载均衡分发，说明已经完成了全局鉴权，直接放行
    if (req.lb) return next();

    // 1. 检查 Session
    const session = getSession(req);
    if (session) return next();

    // 2. 检查 Authorization Header
    const authHeader = req.headers['authorization'];
    if (authHeader && authHeader.startsWith('Bearer ')) {
        const token = authHeader.substring(7);
        const sessionById = getSessionById(token);
        if (sessionById) return next();

        const apiKey = storage.getSetting('API_KEY');
        if (apiKey && token === apiKey) return next();
    }

    // 3. 检查 Query Param
    const queryKey = req.query.key;
    if (queryKey) {
        const apiKey = storage.getSetting('API_KEY');
        if (apiKey && queryKey === apiKey) return next();
    }

    res.status(401).json({
        error: { message: 'Invalid API Key', type: 'invalid_request_error', code: 'invalid_api_key' },
    });
}

// 管理接口需要 Session 认证
function requireAuth(req, res, next) {
    const session = getSession(req);
    if (session) return next();

    const authHeader = req.headers['authorization'];
    if (authHeader && authHeader.startsWith('Bearer ')) {
        const sessionById = getSessionById(authHeader.substring(7));
        if (sessionById) return next();
    }

    const pwd = req.headers['x-admin-password'];
    if (pwd) {
        // 简单密码验证（复用主应用的认证逻辑）
        try {
            const authService = require('../../src/services/auth');
            if (authService.verifyPassword && authService.verifyPassword(pwd)) return next();
        } catch (e) { }
    }

    res.status(401).json({ error: 'Unauthorized' });
}

// ==================== 账号管理 API ====================

router.get('/accounts', requireAuth, async (req, res) => {
    try {
        const accounts = storage.getAccounts();
        const accountsWithStatus = await Promise.all(
            accounts.map(async account => {
                let status = 'unknown';
                try {
                    if (account.token) {
                        status = 'online';
                    } else if (account.email || account.mobile) {
                        status = 'offline'; // 有凭证但未登录
                    }
                } catch (e) {
                    status = 'error';
                }
                return { ...account, status, password: '***' }; // 脱敏密码
            })
        );
        res.json(accountsWithStatus);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

router.post('/accounts', requireAuth, async (req, res) => {
    try {
        const { name, email, mobile, password, token } = req.body;
        if (!email && !mobile) {
            return res.status(400).json({ error: 'Missing email or mobile' });
        }
        if (!password && !token) {
            return res.status(400).json({ error: 'Missing password or token' });
        }

        const id = `ds_${Math.random().toString(36).slice(2, 7)}`;
        storage.addAccount({ id, name: name || '', email: email || '', mobile: mobile || '', password: password || '', token: token || '' });

        // 如果有密码但没有 token，尝试自动登录
        if (password && !token) {
            try {
                const newToken = await client.login({ email, mobile, password });
                storage.updateToken(id, newToken);
                logger.info(`账号 ${name || email || mobile} 自动登录成功`);
            } catch (loginErr) {
                logger.warn(`账号 ${name || email || mobile} 自动登录失败: ${loginErr.message}`);
            }
        }

        res.json({ message: 'Account added', id });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

router.put('/accounts/:id', requireAuth, (req, res) => {
    try {
        storage.updateAccount(req.params.id, req.body);
        res.json({ message: 'Account updated', id: req.params.id });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

router.delete('/accounts/:id', requireAuth, (req, res) => {
    try {
        storage.deleteAccount(req.params.id);
        res.json({ message: 'Account deleted' });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

router.post('/accounts/:id/toggle', requireAuth, (req, res) => {
    try {
        const result = storage.toggleAccount(req.params.id);
        res.json({ message: 'Account toggled', enable: result });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// 测试账号登录
router.post('/accounts/:id/test', requireAuth, async (req, res) => {
    try {
        const account = storage.getAccountById(req.params.id);
        if (!account) return res.status(404).json({ error: 'Account not found' });

        const token = await client.login(account);
        storage.updateToken(account.id, token);
        res.json({ success: true, message: 'Login successful' });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// 刷新所有账号
router.post('/accounts/refresh', requireAuth, async (req, res) => {
    try {
        const accounts = storage.getAccounts();
        let refreshed = 0, failed = 0;

        for (const account of accounts) {
            try {
                await client.refreshToken(account.id);
                refreshed++;
            } catch (e) {
                logger.warn(`刷新账号 ${account.name} 失败: ${e.message}`);
                failed++;
            }
        }

        res.json({ success: true, refreshed, failed });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// 导出账号
router.get('/accounts/export', requireAuth, (req, res) => {
    try {
        const accounts = storage.getAccounts();
        res.json({
            version: '1.0',
            type: 'deepseek-accounts',
            exportTime: new Date().toISOString(),
            accounts: accounts.map(a => ({
                name: a.name, email: a.email, mobile: a.mobile,
                password: a.password, token: a.token,
            })),
        });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// 导入账号
router.post('/accounts/import', requireAuth, async (req, res) => {
    try {
        const { accounts } = req.body;
        if (!Array.isArray(accounts)) return res.status(400).json({ error: 'Invalid format' });

        let imported = 0, skipped = 0;
        for (const acc of accounts) {
            if (!acc.email && !acc.mobile) { skipped++; continue; }
            try {
                const id = `ds_${Math.random().toString(36).slice(2, 7)}`;
                storage.addAccount({ id, ...acc });
                imported++;
            } catch (e) { skipped++; }
        }
        res.json({ success: true, imported, skipped });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// ==================== 设置管理 ====================

router.get('/settings', requireAuth, (req, res) => {
    try {
        res.json(storage.getSettings());
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

router.post('/settings', requireAuth, (req, res) => {
    try {
        for (const [key, value] of Object.entries(req.body)) {
            storage.updateSetting(key, String(value));
        }
        res.json({ message: 'Settings updated' });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// ==================== 统计概览 ====================

router.get('/stats', requireAuth, (req, res) => {
    try {
        const stats = storage.getStats();
        res.json(stats);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// ==================== 日志管理 ====================

router.get('/logs', requireAuth, (req, res) => {
    try {
        const limit = parseInt(req.query.limit) || 100;
        res.json(storage.getRecentLogs(limit));
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

router.get('/logs/:id', requireAuth, (req, res) => {
    try {
        const log = storage.getLogDetail(req.params.id);
        if (log && log.detail) {
            log.detail = JSON.parse(log.detail);
        }
        res.json(log);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

router.delete('/logs', requireAuth, (req, res) => {
    try {
        storage.clearLogs();
        res.json({ message: 'Logs cleared' });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// ==================== 模型相关 ====================

router.get('/models', (req, res) => {
    try {
        const matrixPath = path.join(__dirname, 'deepseek-models.json');
        const matrix = JSON.parse(fs.readFileSync(matrixPath, 'utf8'));
        const models = [];
        const now = Math.floor(Date.now() / 1000);

        for (const [baseId, config] of Object.entries(matrix)) {
            if (config.base) {
                models.push({ id: baseId, object: 'model', created: now, owned_by: 'deepseek' });
            }
            if (config.search) {
                models.push({ id: `${baseId}-search`, object: 'model', created: now, owned_by: 'deepseek' });
            }
        }

        res.json({ object: 'list', data: models });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// ==================== 模型重定向管理 ====================

router.get('/models/redirects', requireAuth, (req, res) => {
    try {
        const redirects = storage.getModelRedirects();
        res.json(redirects);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

router.post('/models/redirects', requireAuth, (req, res) => {
    const { sourceModel, targetModel } = req.body;
    if (!sourceModel || !targetModel) {
        return res.status(400).json({ error: 'Source and target models required' });
    }
    if (sourceModel === targetModel) {
        return res.status(400).json({ error: 'Cannot redirect to self' });
    }
    try {
        storage.addModelRedirect(sourceModel, targetModel);
        res.json({ success: true, sourceModel, targetModel });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

router.delete('/models/redirects/:sourceModel', requireAuth, (req, res) => {
    const { sourceModel } = req.params;
    try {
        storage.removeModelRedirect(sourceModel);
        res.json({ success: true, sourceModel });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// ==================== OpenAI 兼容文件接口 ====================

router.post('/v1/files', requireApiKey, async (req, res) => {
    try {
        if (!req.files || !req.files.file) {
            return res.status(400).json({ error: { message: 'No file uploaded', type: 'invalid_request_error' } });
        }

        const file = req.files.file;
        const purpose = req.body.purpose || 'fine-tune'; // OpenAI 兼容

        // 1. 选择账号
        const allAccounts = storage.getAccounts().filter(a => a.enable !== 0);
        if (allAccounts.length === 0) {
            return res.status(503).json({ error: { message: 'No enabled accounts available', type: 'service_unavailable' } });
        }
        const account = allAccounts[Math.floor(Math.random() * allAccounts.length)];

        // 2. 获取 Token
        const token = await client.getAccessToken(account.id);

        // 3. 创建临时 Session 用于上传
        const sessionId = await client.createSession(token);

        // 4. 读取文件并上传
        const fileBuffer = fs.readFileSync(file.tempFilePath);
        const fileId = await client.uploadFile(token, sessionId, fileBuffer, file.name);

        // 5. 记录缓存
        storage.saveFileCache(fileId, account.id, sessionId, file.name, file.size);

        // 6. 返回 OpenAI 兼容响应
        res.json({
            id: fileId,
            object: 'file',
            bytes: file.size,
            created_at: Math.floor(Date.now() / 1000),
            filename: file.name,
            purpose: purpose,
            status: 'processed',
        });

        // 清理临时文件
        try { fs.unlinkSync(file.tempFilePath); } catch (e) { }

    } catch (e) {
        logger.error(`File upload error: ${e.message}`);
        res.status(500).json({ error: { message: e.message, type: 'api_error' } });
    }
});

router.get('/v1/files', requireApiKey, (req, res) => {
    try {
        const files = storage.getAllFileCaches(100);
        res.json({
            object: 'list',
            data: files.map(f => ({
                id: f.file_id,
                object: 'file',
                bytes: f.file_size,
                created_at: Math.floor(new Date(f.created_at).getTime() / 1000),
                filename: f.file_name,
                purpose: 'fine-tune',
                status: 'processed',
            })),
        });
    } catch (e) {
        res.status(500).json({ error: { message: e.message } });
    }
});

// ==================== OpenAI 兼容对话接口 ====================

router.post(['/v1/chat/completions', '/chat/completions'], requireApiKey, async (req, res) => {
    const startTime = Date.now();
    try {
        let { model, messages, stream, file_ids } = req.body;

        // 处理模型重定向
        const redirects = storage.getModelRedirects();
        const redirect = redirects.find(r => r.source_model === model);
        if (redirect) model = redirect.target_model;

        // 如果 file_ids 是数组但为空，忽略它；如果是字符串，转为数组
        if (typeof file_ids === 'string') file_ids = [file_ids];
        const hasFiles = Array.isArray(file_ids) && file_ids.length > 0;

        // 获取可用账号
        let account;
        const allAccounts = storage.getAccounts().filter(a => a.enable !== 0);
        if (allAccounts.length === 0) {
            return res.status(503).json({ error: { message: 'No enabled accounts available', type: 'service_unavailable' } });
        }

        // 如果有文件，强制选择拥有该文件的账号
        if (hasFiles) {
            const fileCache = storage.getFileCache(file_ids[0]);
            if (fileCache) {
                account = allAccounts.find(a => a.id === fileCache.account_id);
                if (!account) {
                    return res.status(400).json({ error: { message: `Account for file ${file_ids[0]} not found or disabled`, type: 'invalid_request_error' } });
                }
                logger.info(`[文件对话] 强制使用账号: ${account.name || account.id} (文件 ID: ${file_ids[0]})`);
            }
        }

        // 如果没选定账号（无文件或文件缓存未命中），则随机选
        if (!account) {
            account = allAccounts[Math.floor(Math.random() * allAccounts.length)];
        }

        try {
            // 1. 获取 Token
            let token;
            try {
                token = await client.getAccessToken(account.id);
            } catch (tokenErr) {
                // Token 无效，尝试刷新
                if (tokenErr.message === 'TOKEN_INVALID') {
                    token = await client.refreshToken(account.id);
                } else {
                    throw tokenErr;
                }
            }

            // 2. 获取或创建 Session
            let sessionId;
            let parentId = null;

            const cached = findSessionIdByMessages(messages);
            if (cached) {
                sessionId = cached.sessionId;
                parentId = cached.parentId || null;
                logger.info(`[连续对话] 复用 session: ${sessionId}, parent: ${parentId}`);
            } else {
                // 如果是有文件的对话，且缓存中记录了上传时的 sessionId，可以尝试复用
                if (hasFiles) {
                    const fileCache = storage.getFileCache(file_ids[0]);
                    if (fileCache && fileCache.session_id) {
                        sessionId = fileCache.session_id;
                        logger.info(`[文件对话] 复用上传时的 session: ${sessionId}`);
                    }
                }

                if (!sessionId) {
                    try {
                        sessionId = await client.createSession(token);
                    } catch (sessionErr) {
                        if (sessionErr.message === 'TOKEN_INVALID') {
                            token = await client.refreshToken(account.id);
                            sessionId = await client.createSession(token);
                        } else {
                            throw sessionErr;
                        }
                    }
                }
            }

            // 3. 获取 PoW
            let powHeader;
            try {
                powHeader = await client.getPow(token);
            } catch (powErr) {
                if (powErr.message === 'TOKEN_INVALID') {
                    token = await client.refreshToken(account.id);
                    powHeader = await client.getPow(token);
                } else {
                    throw powErr;
                }
            }

            // 4. 构建请求体
            const payload = client.buildCompletionPayload(sessionId, messages, model, {
                parent_message_id: parentId,
                file_ids: file_ids || [],
            });

            // 5. 调用 Completion
            const dsResponse = await client.callCompletion(token, payload, powHeader);

            const isReasoner = model.includes('reasoner');

            if (stream) {
                // 流式输出
                res.setHeader('Content-Type', 'text/event-stream');
                res.setHeader('Cache-Control', 'no-cache');
                res.setHeader('Connection', 'keep-alive');
                res.setHeader('X-Accel-Buffering', 'no');

                const completionId = `chatcmpl-${Math.random().toString(36).slice(2)}`;
                const created = Math.floor(Date.now() / 1000);
                let fullContent = '';
                let fullReasoning = '';
                let firstTokenTime = null;
                let responseMessageId = null;

                parseSSEStream(
                    dsResponse,
                    isReasoner,
                    (type, text) => {
                        if (firstTokenTime === null) firstTokenTime = Date.now() - startTime;

                        if (type === 'thinking') {
                            fullReasoning += text;
                            const chunk = {
                                id: completionId, object: 'chat.completion.chunk', created, model,
                                choices: [{ index: 0, delta: { reasoning_content: text }, finish_reason: null }],
                            };
                            res.write(`data: ${JSON.stringify(chunk)}\n\n`);
                        } else {
                            fullContent += text;
                            const chunk = {
                                id: completionId, object: 'chat.completion.chunk', created, model,
                                choices: [{ index: 0, delta: { content: text }, finish_reason: null }],
                            };
                            res.write(`data: ${JSON.stringify(chunk)}\n\n`);
                        }
                        if (res.flush) res.flush();
                    },
                    () => {
                        // 发送结束标记
                        const doneChunk = {
                            id: completionId, object: 'chat.completion.chunk', created, model,
                            choices: [{ index: 0, delta: {}, finish_reason: 'stop' }],
                        };
                        res.write(`data: ${JSON.stringify(doneChunk)}\n\n`);
                        res.write('data: [DONE]\n\n');
                        res.end();

                        // 记录会话继承 (保存 message_id 作为下次的 parent_id)
                        saveToSessionCache(fullContent, sessionId, responseMessageId);

                        // 记录日志
                        storage.recordLog({
                            accountId: account.id, model, is_balanced: req.lb,
                            path: req.path, method: req.method, statusCode: 200,
                            durationMs: Date.now() - startTime, firstTokenTimeMs: firstTokenTime,
                            clientIp: req.ip, userAgent: req.get('user-agent'),
                            detail: {
                                model, type: 'stream',
                                messages: sanitizeMessages(messages),
                                response: { choices: [{ message: { role: 'assistant', content: fullContent, reasoning_content: fullReasoning } }] },
                            },
                        });
                    },
                    (err) => {
                        logger.error(`Stream error: ${err.message}`);
                        if (!res.headersSent) {
                            res.status(500).json({ error: { message: err.message } });
                        } else {
                            res.end();
                        }
                    },
                    (meta) => { if (meta.message_id) responseMessageId = meta.message_id; }
                );
            } else {
                // 非流式输出
                const result = await collectStream(dsResponse, isReasoner);
                const completionId = `chatcmpl-${Math.random().toString(36).slice(2)}`;
                const responseData = {
                    id: completionId,
                    object: 'chat.completion',
                    created: Math.floor(Date.now() / 1000),
                    model,
                    choices: [{
                        index: 0,
                        message: { role: 'assistant', content: result.content, reasoning_content: result.thinking },
                        finish_reason: 'stop',
                    }],
                    usage: { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 },
                };
                // 记录会话继承 (保存 message_id 作为下次的 parent_id)
                saveToSessionCache(result.content, sessionId, result.message_id);

                // 记录日志
                storage.recordLog({
                    accountId: account.id, model, is_balanced: req.lb,
                    path: req.path, method: req.method, statusCode: 200,
                    durationMs: Date.now() - startTime, firstTokenTimeMs: Date.now() - startTime,
                    clientIp: req.ip, userAgent: req.get('user-agent'),
                    detail: { model, messages: sanitizeMessages(messages), response: responseData },
                });

                res.json(responseData);
            }
        } catch (error) {
            logger.error(`[DS] Account ${account.name} failed: ${error.message}`);

            // 记录错误日志
            storage.recordLog({
                accountId: account.id, model, is_balanced: req.lb,
                path: req.path, method: req.method, statusCode: error.response?.status || 500,
                durationMs: Date.now() - startTime,
                clientIp: req.ip, userAgent: req.get('user-agent'),
                detail: { model, error: error.message },
            });

            if (!res.headersSent) {
                res.status(500).json({
                    error: { message: error.message, type: 'api_error' },
                });
            }
        }
    } catch (e) {
        logger.error(`Request error: ${e.message}`);
        if (!res.headersSent) {
            res.status(500).json({ error: { message: e.message } });
        }
    }
});

// ==================== 辅助函数 ====================

function sanitizeMessages(messages) {
    if (!messages) return [];
    return JSON.parse(JSON.stringify(messages)).map(msg => {
        if (Array.isArray(msg.content)) {
            msg.content.forEach(part => {
                if (part.type === 'image_url' && part.image_url?.url?.startsWith('data:')) {
                    part.image_url.url = `data:image/... [Base64 hidden, size=${part.image_url.url.length}]`;
                }
            });
        }
        return msg;
    });
}

module.exports = router;
