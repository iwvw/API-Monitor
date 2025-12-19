const express = require('express');
const router = express.Router();
const axios = require('axios');
const storage = require('./storage');
const client = require('./gemini-client');
const StreamProcessor = require('./utils/stream-processor');
const { requireAuth } = require('../../src/middleware/auth');

const streamProcessor = new StreamProcessor(client);

/**
 * API Key 认证中间件 (供外部客户端使用)
 */
const requireApiKey = async (req, res, next) => {
    const authHeader = req.headers.authorization;
    if (!authHeader || !authHeader.startsWith('Bearer ')) {
        return res.status(401).json({ error: { message: 'Unauthorized', type: 'invalid_request_error', code: '401' } });
    }

    const token = authHeader.substring(7);

    try {
        const settings = await storage.getSettings();
        const configuredKey = settings.API_KEY || '123456'; // 默认 123456 仅供兼容，建议设置

        if (token !== configuredKey) {
            return res.status(401).json({ error: { message: 'Invalid API Key', type: 'invalid_request_error', code: '401' } });
        }
        next();
    } catch (e) {
        console.error('API Key 验证出错:', e);
        res.status(500).json({ error: 'Auth Error' });
    }
};

/**
 * OpenAI 兼容的模型列表接口 - 返回基础模型和思考模型变体
 */
const path = require('path');
const fs = require('fs');

const MATRIX_FILE = path.join(__dirname, 'gemini-matrix.json');

// 默认矩阵配置（如果文件不存在）
const DEFAULT_MATRIX = {
    "gemini-2.5-pro": { base: true, maxThinking: true, noThinking: true, search: true, fakeStream: true, antiTrunc: true },
    "gemini-2.5-flash": { base: true, maxThinking: true, noThinking: true, search: true, fakeStream: true, antiTrunc: true },
    "gemini-3-pro-preview": { base: true, maxThinking: true, noThinking: true, search: true, fakeStream: true, antiTrunc: true },
    "gemini-3-flash-preview": { base: true, maxThinking: true, noThinking: true, search: true, fakeStream: true, antiTrunc: true }
};

// 辅助函数：读取矩阵配置
function getMatrixConfig() {
    try {
        if (fs.existsSync(MATRIX_FILE)) {
            return JSON.parse(fs.readFileSync(MATRIX_FILE, 'utf8'));
        }
    } catch (e) {
        console.error('Failed to read matrix file:', e);
    }
    return DEFAULT_MATRIX;
}

// 辅助函数：保存矩阵配置
function saveMatrixConfig(config) {
    try {
        fs.writeFileSync(MATRIX_FILE, JSON.stringify(config, null, 2), 'utf8');
        return true;
    } catch (e) {
        console.error('Failed to save matrix file:', e);
        return false;
    }
}

/**
 * 获取模型矩阵配置 (内部 API)
 */
router.get('/config/matrix', requireAuth, (req, res) => {
    res.json(getMatrixConfig());
});

/**
 * 更新模型矩阵配置 (内部 API)
 */
router.post('/config/matrix', requireAuth, (req, res) => {
    const newConfig = req.body;
    if (saveMatrixConfig(newConfig)) {
        res.json({ success: true });
    } else {
        res.status(500).json({ error: 'Failed to save configuration' });
    }
});

// 辅助函数：根据矩阵配置和禁用列表生成可用模型列表
function getAvailableModels(prefix) {
    const matrix = getMatrixConfig();
    const models = [];
    const disabledModels = storage.getDisabledModels();

    Object.keys(matrix).forEach(baseModelId => {
        const config = matrix[baseModelId];
        if (!config) return;

        // 1. 收集当前模型的基础可用变体 ID (不含前缀)
        const standardVariants = [];

        // A. 基础模型
        if (config.base) {
            standardVariants.push(baseModelId);
            // A.1 搜索变体
            if (config.search) {
                standardVariants.push(baseModelId + '-search');
            }
        }

        // B. 深度思考 (MaxThinking)
        if (config.maxThinking) {
            standardVariants.push(baseModelId + '-maxthinking');
            // B.1 搜索变体
            if (config.search) {
                standardVariants.push(baseModelId + '-maxthinking-search');
            }
        }

        // C. 快速思考 (NoThinking)
        if (config.noThinking) {
            standardVariants.push(baseModelId + '-nothinking');
            // C.1 搜索变体
            if (config.search) {
                standardVariants.push(baseModelId + '-nothinking-search');
            }
        }

        // 2. 为每个变体生成最终的模型对象 (应用全局前缀 + 功能前缀)
        standardVariants.forEach(variantId => {
            // 2.1 添加标准模型 (带全局前缀)
            const id1 = prefix + variantId;
            if (!disabledModels.includes(id1)) models.push(createModelObject(id1));

            // 2.2 添加假流式变体
            if (config.fakeStream) {
                const id2 = prefix + '假流式/' + variantId;
                if (!disabledModels.includes(id2)) models.push(createModelObject(id2));
            }

            // 2.3 添加抗截断变体
            if (config.antiTrunc) {
                const id3 = prefix + '流式抗截断/' + variantId;
                if (!disabledModels.includes(id3)) models.push(createModelObject(id3));
            }
        });
    });

    // 3. 注入重定向模型
    const redirects = storage.getModelRedirects();
    redirects.forEach(r => {
        // 仅当源模型不冲突时添加 (应用前缀后的源模型名)
        const sourceWithPrefix = prefix + r.source_model;
        if (!models.find(m => m.id === sourceWithPrefix) && !disabledModels.includes(sourceWithPrefix)) {
            models.push(createModelObject(sourceWithPrefix));
        }
    });
    
    return models;
}

/**
 * OpenAI 兼容的模型列表接口 - 基于矩阵配置动态生成
 */
router.get(['/v1/models', '/models'], requireApiKey, async (req, res) => {
    try {
        // 获取全局设置中的前缀
        const userSettingsService = require('../../src/services/userSettings');
        const globalSettings = userSettingsService.loadUserSettings();
        const prefix = (globalSettings.channelModelPrefix || {})['gemini-cli'] || '';

        const models = getAvailableModels(prefix);

        res.json({ object: 'list', data: models });
    } catch (e) {
        console.error('[GCLI] Failed to fetch matrix models:', e.message);
        res.status(500).json({ error: e.message });
    }
});

function createModelObject(id) {
    return {
        id: id,
        object: 'model',
        created: Math.floor(Date.now() / 1000),
        owned_by: 'google'
    };
}



// 全局渠道启用状态检查中间件
function requireChannelEnabled(req, res, next) {
    const userSettingsService = require('../../src/services/userSettings');
    const settings = userSettingsService.loadUserSettings();
    const channelEnabled = settings.channelEnabled || {};

    if (channelEnabled['gemini-cli'] === false) {
        return res.status(403).json({
            error: {
                message: 'Gemini CLI channel is disabled in global settings.',
                type: 'permission_error',
                code: 'channel_disabled'
            }
        });
    }
    next();
}

// 所有 /v1 (及为了兼容性直接暴露在根部的 chat/completions) 接口受全局启用状态控制
router.use(['/v1', '/chat/completions'], requireChannelEnabled);

// 账号冷却/避让逻辑缓存 (内存中)
// key: accountId:modelId -> resetTime
const accountCoolDowns = new Map(); 

/**
 * 检查账号是否处于冷却期
 */
function isAccountInCoolDown(accountId, model) {
    const key = `${accountId}:${model}`;
    const resetTime = accountCoolDowns.get(key);
    
    if (!resetTime) return false;

    if (resetTime > Date.now()) {
        return true;
    }

    // 已过期，移除
    accountCoolDowns.delete(key);
    return false;
}

/**
 * 获取账号所有受限的模型
 */
function getAccountCoolDowns(accountId) {
    const limitedModels = [];
    const now = Date.now();
    
    for (const [key, resetTime] of accountCoolDowns.entries()) {
        if (key.startsWith(`${accountId}:`)) {
            if (resetTime > now) {
                limitedModels.push({
                    model: key.split(':')[1],
                    resetTime,
                    remainingMs: resetTime - now
                });
            } else {
                accountCoolDowns.delete(key);
            }
        }
    }
    return limitedModels;
}

// ============== 管理接口 (需 Admin 权限) ==============
router.use(['/accounts', '/oauth/exchange', '/logs', '/settings', '/stats', '/quotas', '/models'], requireAuth);

/**
 * 获取额度信息
 */
router.get('/quotas', async (req, res) => {
    try {
        const { accountId } = req.query;
        if (!accountId) {
            return res.json({});
        }

        // 获取账号
        const accounts = storage.getAccounts();
        const account = accounts.find(a => a.id === accountId);
        if (!account) {
            return res.status(404).json({ error: 'Account not found' });
        }

        // 使用 client 获取模型列表和额度
        const quotas = await client.getQuotas(account);
        res.json(quotas);
    } catch (e) {
        console.error('获取额度失败:', e);
        res.status(500).json({ error: e.message });
    }
});

/**
 * 切换模型状态
 */
router.post('/models/status', async (req, res) => {
    try {
        const { modelId, enabled } = req.body;
        if (!modelId) {
            return res.status(400).json({ error: 'Model ID required' });
        }

        storage.setModelStatus(modelId, enabled);
        res.json({ success: true, modelId, enabled });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 获取模型重定向列表
 */
router.get('/models/redirects', async (req, res) => {
    try {
        const redirects = storage.getModelRedirects();
        res.json(redirects);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 添加模型重定向
 */
router.post('/models/redirects', async (req, res) => {
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

/**
 * 删除模型重定向
 */
router.delete('/models/redirects/:sourceModel', async (req, res) => {
    const { sourceModel } = req.params;
    try {
        storage.removeModelRedirect(sourceModel);
        res.json({ success: true, sourceModel });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

router.get('/accounts', async (req, res) => {
    try {
        const accounts = storage.getAccounts();

        // 尝试验证每个账号的 Token 状态
        const accountsWithStatus = await Promise.all(accounts.map(async (account) => {
            let status = 'online';
            
            // 获取该账号下所有被限流的模型列表
            const coolDowns = getAccountCoolDowns(account.id);

            try {
                // 仅当没被全局账号级限制（如果将来有）且目前是在线状态时验证
                await client.getAccessToken(account.id);
            } catch (e) {
                console.log(`Account ${account.name} validation failed:`, e.message);
                status = 'error';
            }

            return { ...account, status, coolDowns };
        }));

        res.json(accountsWithStatus);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 强制刷新所有账号状态和信息 (Email)
 */
router.post('/accounts/refresh', async (req, res) => {
    try {
        const accounts = storage.getAccounts();
        let refreshed = 0;
        let failed = 0;

        for (const account of accounts) {
            try {
                // 1. 获取 Token (这一步如果过期会自动刷新)
                const token = await client.getAccessToken(account.id);

                // 2. 强制获取最新 UserInfo 和 Project ID (如果缺失)
                const axiosConfig = await client.getAxiosConfig();
                let newEmail = account.email;
                let newProjectId = account.project_id;

                try {
                    const userRes = await axios.get('https://www.googleapis.com/oauth2/v2/userinfo', {
                        ...axiosConfig,
                        headers: { Authorization: `Bearer ${token}` }
                    });
                    if (userRes.data?.email) newEmail = userRes.data.email;
                } catch (infoErr) {
                    console.warn(`Failed to get email for ${account.name}: ${infoErr.message}`);
                }

                if (!newProjectId) {
                    try {
                        const projRes = await axios.get('https://cloudresourcemanager.googleapis.com/v1/projects', {
                            ...axiosConfig,
                            headers: { Authorization: `Bearer ${token}` }
                        });
                        const projects = projRes.data.projects || [];
                        if (projects.length > 0) newProjectId = projects[0].projectId;
                    } catch (projErr) {
                        // 忽略 Project ID 获取失败
                    }
                }

                if (newEmail !== account.email || newProjectId !== account.project_id) {
                    storage.updateAccount(account.id, {
                        ...account,
                        email: newEmail,
                        project_id: newProjectId
                    });

                    // 更新 token 记录
                    const tokenRecord = storage.getTokenByAccountId(account.id);
                    if (tokenRecord) {
                        storage.saveToken({
                            account_id: account.id,
                            ...tokenRecord,
                            email: newEmail,
                            project_id: newProjectId
                        });
                    }
                }

                refreshed++;
            } catch (e) {
                console.error(`Failed to refresh account ${account.name}:`, e.message);
                failed++;
            }
        }

        // 刷新完成后，重新获取最新账号列表返回
        const updatedAccounts = await Promise.all(storage.getAccounts().map(async (account) => {
            // 这里不再验证 Token 状态，因为刷新过程中已经验证/刷新过了，直接返回 online 即可（或者简单验证）
            // 为了响应速度，这里只返回静态数据，状态已经在刷新循环中处理了
            // 如果需要状态，可以简单标记
            return { ...account, status: 'online' };
        }));

        res.json({ success: true, refreshed, failed, accounts: updatedAccounts });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 账号管理 - 添加账号
 */
router.post('/accounts', async (req, res) => {
    try {
        const { name, email, client_id, client_secret, refresh_token, project_id } = req.body;
        if (!client_id || !client_secret || !refresh_token) {
            return res.status(400).json({ error: 'Missing OAuth credentials' });
        }
        const id = `acc_${Math.random().toString(36).slice(2, 7)}`;
        await storage.addAccount({
            id, name: name || 'Unnamed Account', email,
            client_id, client_secret, refresh_token, project_id
        });
        res.json({ message: 'Account added', id });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 账号管理 - 更新账号
 */
router.put('/accounts/:id', async (req, res) => {
    try {
        const { name, email, client_id, client_secret, refresh_token, project_id } = req.body;
        storage.updateAccount(req.params.id, {
            name, email, client_id, client_secret, refresh_token, project_id
        });
        res.json({ message: 'Account updated', id: req.params.id });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 手动获取邮箱
 */
router.post('/accounts/fetch-email', async (req, res) => {
    try {
        const { client_id, client_secret, refresh_token } = req.body;
        if (!client_id || !client_secret || !refresh_token) {
            return res.status(400).json({ error: 'Missing credentials' });
        }

        const axios = require('axios');

        // 刷新 Token 获取 Access Token
        const params = new URLSearchParams({
            client_id,
            client_secret,
            refresh_token,
            grant_type: 'refresh_token'
        });

        const tokenRes = await axios.post('https://oauth2.googleapis.com/token', params.toString(), {
            headers: { 'Content-Type': 'application/x-www-form-urlencoded' }
        });

        // 获取用户信息
        const userRes = await axios.get('https://www.googleapis.com/oauth2/v2/userinfo', {
            headers: { Authorization: `Bearer ${tokenRes.data.access_token}` }
        });

        res.json({ email: userRes.data.email });
    } catch (e) {
        console.error('Fetch email error:', e.response?.data || e.message);
        res.status(500).json({ error: e.response?.data?.error_description || e.message });
    }
});

/**
 * 账号管理 - 删除账号
 */
router.delete('/accounts/:id', async (req, res) => {
    try {
        storage.deleteAccount(req.params.id);
        res.json({ message: 'Account deleted' });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 账号管理 - 切换启用状态
 */
router.post('/accounts/:id/toggle', async (req, res) => {
    try {
        const result = storage.toggleAccount(req.params.id);
        res.json({ message: 'Account toggled', enable: result });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 日志管理 - 获取列表
 */
router.get('/logs', async (req, res) => {
    try {
        const limit = parseInt(req.query.limit) || 100;
        const offset = parseInt(req.query.offset) || 0;
        const logs = await storage.getLogs(limit, offset);
        res.json(logs);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 日志管理 - 获取详情
 */
router.get('/logs/:id', async (req, res) => {
    try {
        const log = await storage.getLogDetail(req.params.id);
        if (log && log.detail) {
            log.detail = JSON.parse(log.detail);
        }
        res.json(log);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 日志管理 - 清空日志
 */
router.delete('/logs', async (req, res) => {
    try {
        await storage.clearLogs();
        res.json({ message: 'Logs cleared' });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 设置管理 - 获取设置
 */
router.get('/settings', async (req, res) => {
    try {
        const settings = await storage.getSettings();
        res.json(settings);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 设置管理 - 更新设置
 */
router.post('/settings', async (req, res) => {
    try {
        for (const [key, value] of Object.entries(req.body)) {
            await storage.updateSetting(key, String(value));
        }
        res.json({ message: 'Settings updated' });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 概览统计
 */
router.get('/stats', async (req, res) => {
    try {
        const accounts = await storage.getAccounts();
        const logs = await storage.getLogs(10, 0);

        // 简单统计过去 24 小时调用量 (这里为了演示简单处理)
        const stats = {
            total_accounts: accounts.length,
            active_accounts: accounts.filter(a => a.enable !== 0).length,
            total_logs_count: logs.length, // 实际上应该是全部
            recent_logs: logs
        };
        res.json(stats);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

/**
 * 账号管理 - OAuth Token 交换
 */
router.post('/oauth/exchange', async (req, res) => {
    try {
        const { code, redirect_uri, client_id, client_secret, project_id: customProjectId } = req.body;
        if (!code || !redirect_uri || !client_id || !client_secret) {
            return res.status(400).json({ error: 'Missing code, redirect_uri, client_id, or client_secret' });
        }

        const axios = require('axios');
        const params = new URLSearchParams({
            code,
            client_id,
            client_secret,
            redirect_uri,
            grant_type: 'authorization_code'
        });
        const response = await axios.post('https://oauth2.googleapis.com/token', params.toString(), {
            headers: { 'Content-Type': 'application/x-www-form-urlencoded' }
        });

        let project_id = customProjectId || '';
        let email = null;

        // 获取用户邮箱
        try {
            const userRes = await axios.get('https://www.googleapis.com/oauth2/v2/userinfo', {
                headers: { Authorization: `Bearer ${response.data.access_token}` }
            });
            email = userRes.data.email;
        } catch (ue) {
            console.warn('Auto-discover email failed:', ue.message);
        }

        // 尝试自动发现 Project ID (可选)
        if (!project_id) {
            try {
                const projectsRes = await axios.get('https://cloudresourcemanager.googleapis.com/v1/projects', {
                    headers: { Authorization: `Bearer ${response.data.access_token}` }
                });
                if (projectsRes.data.projects && projectsRes.data.projects.length > 0) {
                    // 默认取第一个
                    project_id = projectsRes.data.projects[0].projectId;
                }
            } catch (pe) {
                console.warn('Auto-discover Project ID failed:', pe.message);
            }
        }

        res.json({
            ...response.data,
            project_id,
            email
        });
    } catch (e) {
        console.error('OAuth Exchange Error:', e.response?.data || e.message);
        res.status(e.response?.status || 500).json({
            error: e.response?.data?.error_description || e.message
        });
    }
});



/**
 * OpenAI 兼容的对话接口
 */
router.post(['/v1/chat/completions', '/chat/completions'], requireApiKey, async (req, res) => {
    const startTime = Date.now();
    try {
        let { model } = req.body;

        // 获取前缀
        const userSettingsService = require('../../src/services/userSettings');
        const globalSettings = userSettingsService.loadUserSettings();
        const prefix = (globalSettings.channelModelPrefix || {})['gemini-cli'] || '';

        // 注意：v1.js 的 dispatch 已经尝试剥离过一次前缀
        // 这里的 model 应当是剥离后的 inner model，或者是没匹配上前缀的完整 ID
        
        // 如果 model 依然带着前缀，剥离它（兼容直接调用该路由的情况）
        if (prefix && model.startsWith(prefix)) {
            model = model.substring(prefix.length);
        }

        // 处理模型重定向
        const redirects = storage.getModelRedirects();
        let redirect = redirects.find(r => r.source_model === model);
        if (redirect) {
            model = redirect.target_model;
        }

        // 最终的模型 ID (带前缀，用于验证和日志)
        const modelWithPrefix = prefix + model;
        
        // 验证模型是否有效（即在矩阵配置中存在且未被禁用）
        const availableModels = getAvailableModels(prefix);
        if (!availableModels.find(m => m.id === modelWithPrefix)) {
            const disabledModels = storage.getDisabledModels();
            if (disabledModels.includes(modelWithPrefix)) {
                return res.status(403).json({ error: { message: `Model '${modelWithPrefix}' is disabled`, type: 'permission_error', code: 'model_disabled' } });
            } else {
                // 如果在 GCLI 矩阵中完全找不到，可能不该由本渠道处理
                return res.status(404).json({ error: { message: `Model '${modelWithPrefix}' not found in Gemini CLI matrix`, type: 'invalid_request_error', code: 'model_not_found' } });
            }
        }
        
        // 更新请求中的模型名为剥离前缀后的名字，供 client 使用
        req.body.model = model; 

        // 获取所有启用账号
        let allAccounts = (await storage.getAccounts()).filter(a => a.enable !== 0);
        if (allAccounts.length === 0) {
            return res.status(503).json({ error: { message: 'No enabled accounts available', type: 'service_unavailable' } });
        }

        // 过滤掉处于冷却期的账号
        allAccounts = allAccounts.filter(a => !isAccountInCoolDown(a.id, model));
        if (allAccounts.length === 0) {
            return res.status(429).json({ 
                error: { 
                    message: 'All available Gemini accounts are currently rate-limited (429). Please try again later.', 
                    type: 'rate_limit_error',
                    code: '429'
                } 
            });
        }

        const strategy = globalSettings.load_balancing_strategy || 'random';
        const loadBalancer = require('../../src/utils/loadBalancer');

        // 智能重试逻辑
        let attemptedAccounts = new Set();
        let lastError = null;

        while (attemptedAccounts.size < allAccounts.length) {
            const availableAccounts = allAccounts.filter(a => !attemptedAccounts.has(a.id));
            if (availableAccounts.length === 0) break;

            const account = loadBalancer.getNextAccount('gemini-cli', availableAccounts, strategy);
            attemptedAccounts.add(account.id);

            try {
                if (req.body.stream) {
                    res.setHeader('Content-Type', 'text/event-stream');
                    res.setHeader('Cache-Control', 'no-cache');
                    res.setHeader('Connection', 'keep-alive');

                    const stream = streamProcessor.processStream(req.body, account.id);
                    for await (const chunk of stream) {
                        res.write(chunk);
                    }
                    res.end();

                    // 记录成功日志
                    await storage.addLog({
                        account_id: account.id,
                        model: modelWithPrefix,
                        is_balanced: req.lb,
                        request_path: req.path,
                        request_method: req.method,
                        status_code: 200,
                        duration_ms: Date.now() - startTime,
                        client_ip: req.ip,
                        user_agent: req.get('user-agent'),
                        detail: JSON.stringify({ request: req.body, type: 'stream' })
                    });
                    return; // 成功后退出
                } else {
                    const response = await client.generateContent(req.body, account.id);
                    const geminiData = response.data;

                    const candidate = geminiData.candidates?.[0];
                    const text = candidate?.content?.parts?.[0]?.text || '';
                    const reasoning = candidate?.content?.parts?.find(p => p.thought)?.text || '';

                    const responseData = {
                        id: `chatcmpl-${Math.random().toString(36).slice(2)}`,
                        object: 'chat.completion',
                        created: Math.floor(Date.now() / 1000),
                        model: modelWithPrefix, // 返回给用户带前缀的 ID
                        choices: [{
                            index: 0,
                            message: {
                                role: 'assistant',
                                content: text,
                                reasoning_content: reasoning
                            },
                            finish_reason: 'stop'
                        }],
                        usage: {
                            prompt_tokens: geminiData.usageMetadata?.promptTokenCount || 0,
                            completion_tokens: geminiData.usageMetadata?.candidatesTokenCount || 0,
                            total_tokens: geminiData.usageMetadata?.totalTokenCount || 0
                        }
                    };

                    // 记录成功日志
                    await storage.addLog({
                        account_id: account.id,
                        model: modelWithPrefix,
                        is_balanced: req.lb,
                        request_path: req.path,
                        request_method: req.method,
                        status_code: 200,
                        duration_ms: Date.now() - startTime,
                        client_ip: req.ip,
                        user_agent: req.get('user-agent'),
                        detail: JSON.stringify({ request: req.body, response: responseData })
                    });

                    return res.json(responseData); // 成功后退出
                }
            } catch (error) {
                console.warn(`[GCLI] Account ${account.name} failed, trying next... Error: ${error.message}`);
                lastError = error;

                // 处理 429 错误并提取重置时间
                if (error.response?.status === 429) {
                    const errorData = error.response.data;
                    // 支持多种 Google 错误格式中的重置时间字段
                    let resetTimeStr = errorData?.quotaInfo?.resetTime || 
                                     errorData?.error?.details?.[0]?.metadata?.quotaResetTimeStamp ||
                                     errorData?.error?.details?.[0]?.metadata?.resetTime;
                    
                    const key = `${account.id}:${model}`;
                    if (resetTimeStr) {
                        const resetTime = new Date(resetTimeStr).getTime();
                        if (!isNaN(resetTime)) {
                            console.log(`[GCLI] Account ${account.name} model ${model} rate limited until ${resetTimeStr}. Adding to cool-down.`);
                            accountCoolDowns.set(key, resetTime);
                        }
                    } else {
                        // 如果没找到明确重置时间，默认避让 1 分钟
                        accountCoolDowns.set(key, Date.now() + 60000);
                    }
                }

                // 记录错误日志
                await storage.addLog({
                    account_id: account.id,
                    model: modelWithPrefix,
                    is_balanced: req.lb,
                    request_path: req.path,
                    request_method: req.method,
                    status_code: error.response?.status || 500,
                    duration_ms: Date.now() - startTime,
                    client_ip: req.ip,
                    user_agent: req.get('user-agent'),
                    detail: JSON.stringify({ 
                        error: error.message, 
                        response_data: error.response?.data, // 仅记录 data 避免循环引用
                        body: req.body 
                    })
                });

                if (res.headersSent) {
                    if (req.body.stream) res.write(`data: ${JSON.stringify({ error: { message: 'Stream interrupted: ' + error.message } })}\n\n`);
                    return res.end();
                }
            }
        }

        // 所有账号都尝试过了
        res.status(lastError?.response?.status || 503).json({
            error: {
                message: `All Gemini accounts failed. Last error: ${lastError?.message}`,
                type: 'api_error'
            }
        });

    } catch (e) {
        console.error('Chat Completion General Error:', e.message);
        res.status(500).json({ error: { message: e.message, type: 'api_error' } });
    }
});

module.exports = router;
