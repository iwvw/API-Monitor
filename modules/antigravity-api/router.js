const express = require('express');
const storage = require('./storage');
const client = require('./antigravity-client');
const { v4: uuidv4 } = require('uuid');
const axios = require('axios');
const crypto = require('crypto');
const { requireAuth } = require('../../src/middleware/auth');
const { getSession, getSessionById } = require('../../src/services/session');

const router = express.Router();

// 内存中的 OAuth State
const OAUTH_STATE = crypto.randomUUID();
const OAUTH_REDIRECT_URI = 'http://localhost:8045/oauth-callback';

/**
 * API Key 认证中间件
 * 允许:
 * 1. 有效的 Admin Session
 * 2. Visualization Header "Authorization: Bearer <API_KEY>"
 */
function requireApiAuth(req, res, next) {
    // 1. 检查 Session
    const session = getSession(req);
    if (session) return next();

    // 2. 检查 Authorization Header (API Key)
    const authHeader = req.headers['authorization'];
    if (authHeader && authHeader.startsWith('Bearer ')) {
        const token = authHeader.substring(7);

        // 尝试作为 Session ID
        const sessionById = getSessionById(token);
        if (sessionById) return next();

        // 尝试作为 API Key
        const configuredApiKey = storage.getSetting('API_KEY');
        if (configuredApiKey && token === configuredApiKey) {
            return next();
        }
    }

    // 3. 检查 Query Param (compat)
    const queryKey = req.query.key;
    if (queryKey) {
        const configuredApiKey = storage.getSetting('API_KEY');
        if (configuredApiKey && queryKey === configuredApiKey) {
            return next();
        }
    }

    res.status(401).json({ error: { message: 'Invalid API Key or Session', type: 'invalid_request_error', code: 'invalid_api_key' } });
}

// ============== 管理接口 (需 Admin 权限) ==============

// 所有管理接口使用 requireAuth
router.use(['/accounts', '/settings', '/logs', '/oauth', '/stats', '/quotas'], requireAuth);

// 全局渠道启用状态检查中间件
function requireChannelEnabled(req, res, next) {
    const userSettingsService = require('../../src/services/userSettings');
    const settings = userSettingsService.loadUserSettings();
    const channelEnabled = settings.channelEnabled || {};

    if (channelEnabled['antigravity'] === false) {
        return res.status(403).json({
            error: {
                message: 'Antigravity channel is disabled in global settings.',
                type: 'permission_error',
                code: 'channel_disabled'
            }
        });
    }
    next();
}

// 所有 /v1 接口受全局启用状态控制
router.use('/v1', requireChannelEnabled);


// OAuth 配置 (与 client.js 保持一致)
const GOOGLE_CLIENT_ID = '1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com';
const GOOGLE_CLIENT_SECRET = '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';
const SCOPES = [
    'https://www.googleapis.com/auth/cloud-platform',
    'https://www.googleapis.com/auth/userinfo.email',
    'https://www.googleapis.com/auth/userinfo.profile',
    'https://www.googleapis.com/auth/cclog',
    'https://www.googleapis.com/auth/experimentsandconfigs'
];

// 获取所有账号
router.get('/accounts', (req, res) => {
    try {
        const accounts = storage.getAccounts();
        res.json(accounts);
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 刷新所有凭证
router.post('/accounts/refresh-all', async (req, res) => {
    try {
        const results = await client.refreshAllAccounts();
        res.json({
            success: true,
            total: results.total || 0,
            success_count: results.success || 0,
            fail_count: results.fail || 0
        });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 手动添加/更新账号属性 (非 OAuth 流程)
router.put('/accounts/:id', (req, res) => {
    try {
        const { enable, name, email } = req.body;
        const accountId = req.params.id;

        // 如果是切换启用状态
        if (enable !== undefined) {
            storage.updateAccount(accountId, { enable: enable ? 1 : 0 });
        } else {
            storage.updateAccount(accountId, { name, email });
        }

        res.json({ success: true });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 删除账号
router.delete('/accounts/:id', (req, res) => {
    try {
        storage.deleteAccount(req.params.id);
        res.json({ success: true });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 注入模型启用状态
function injectModelStatus(quotas) {
    if (!quotas) return quotas;
    const configs = storage.getModelConfigs();

    // 遍历所有分组
    Object.values(quotas).forEach(group => {
        if (group.models && Array.isArray(group.models)) {
            group.models.forEach(model => {
                // 如果 configs 中没有记录，默认为 true
                const isEnabled = configs[model.id] !== undefined ? configs[model.id] : true;
                model.enabled = isEnabled;
            });
        }
    });
    return quotas;
}

// 获取所有账号的额度汇总
router.get('/quotas', async (req, res) => {
    try {
        const accounts = storage.getAccounts();
        const enabledAccounts = accounts.filter(a => a.enable);

        if (enabledAccounts.length === 0) {
            return res.json({});
        }

        // 目前简单取第一个有效账号的额度，或者汇总所有账号
        // 由于 Antigravity 通常是共享模型配额，取一个即可，
        // 这里为了准确，尝试取第一个有 Token 的账号
        let quotas = {};
        for (const account of enabledAccounts) {
            try {
                quotas = await client.listQuotas(account.id);
                if (Object.keys(quotas).length > 0) break;
            } catch (e) {
                console.error(`Fetch quotas failed for ${account.name}:`, e.message);
            }
        }

        res.json(injectModelStatus(quotas));
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// 获取设置
router.get('/settings', (req, res) => {
    try {
        // storage.getSettings() 返回的是 { key: value } 对象
        const settingsMap = storage.getSettings();

        const allSettings = [
            // 1. 安全配置
            { key: 'API_KEY', value: settingsMap['API_KEY'] || '', category: 'auth', description: '用于保护 /v1/* 端点的访问密钥' },

            // 2. 服务与网络
            { key: 'PORT', value: settingsMap['PORT'] || '8045', category: 'network', description: '服务监听端口' },
            { key: 'HOST', value: settingsMap['HOST'] || '0.0.0.0', category: 'network', description: '服务监听地址' },
            { key: 'PROXY', value: settingsMap['PROXY'] || '', category: 'network', description: 'HTTP 代理服务器地址' },
            { key: 'TIMEOUT', value: settingsMap['TIMEOUT'] || '180000', category: 'network', description: '请求超时时间 (ms)' },
            { key: 'USE_NATIVE_AXIOS', value: settingsMap['USE_NATIVE_AXIOS'] || 'false', category: 'network', description: '是否使用原生 Axios (不通过 Go 客户端)' },

            // 3. API 端点配置
            { key: 'API_URL', value: settingsMap['API_URL'] || 'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:streamGenerateContent?alt=sse', category: 'params', description: '流式接口 URL' },
            { key: 'API_MODELS_URL', value: settingsMap['API_MODELS_URL'] || 'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels', category: 'params', description: '模型列表 URL' },
            { key: 'API_NO_STREAM_URL', value: settingsMap['API_NO_STREAM_URL'] || 'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:generateContent', category: 'params', description: '非流式接口 URL' },
            { key: 'API_HOST', value: settingsMap['API_HOST'] || 'daily-cloudcode-pa.sandbox.googleapis.com', category: 'params', description: 'API Host 头' },
            { key: 'API_USER_AGENT', value: settingsMap['API_USER_AGENT'] || 'antigravity/1.11.3 windows/amd64', category: 'params', description: 'API 请求 User-Agent' },

            // 4. 其他原有配置
            { key: 'CREDENTIAL_MAX_USAGE_PER_HOUR', value: settingsMap['CREDENTIAL_MAX_USAGE_PER_HOUR'] || '20', category: 'quota', description: '每小时凭证最大使用次数' },
            { key: 'REQUEST_LOG_RETENTION_DAYS', value: settingsMap['REQUEST_LOG_RETENTION_DAYS'] || '7', category: 'logs', description: '日志保留天数' }
        ];

        res.json(allSettings);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// 更新设置
router.post('/settings', (req, res) => {
    const { key, value } = req.body;
    if (!key) return res.status(400).json({ error: 'Key is required' });
    try {
        storage.updateSetting(key, value);
        res.json({ success: true });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// 切换模型启用状态
router.post('/models/:id/status', (req, res) => {
    const modelId = req.params.id;
    const { enabled } = req.body;

    try {
        if (enabled === undefined) {
            return res.status(400).json({ error: 'Enabled status required' });
        }
        storage.updateModelStatus(modelId, enabled);
        res.json({ success: true, modelId, enabled });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// 获取模型重定向列表
router.get('/models/redirects', requireApiAuth, (req, res) => {
    try {
        const redirects = storage.getModelRedirects();
        res.json(redirects);
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// 添加模型重定向
router.post('/models/redirects', requireApiAuth, (req, res) => {
    const { sourceModel, targetModel } = req.body;
    if (!sourceModel || !targetModel) {
        return res.status(400).json({ error: 'Source and target models required' });
    }

    // 防止循环重定向
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

// 删除模型重定向
router.delete('/models/redirects/:sourceModel', requireApiAuth, (req, res) => {
    const { sourceModel } = req.params;
    try {
        storage.removeModelRedirect(sourceModel);
        res.json({ success: true, sourceModel });
    } catch (e) {
        res.status(500).json({ error: e.message });
    }
});

// 获取 Google OAuth URL
router.get('/oauth/url', (req, res) => {
    // 动态获取当前请求的 Origin
    // const protocol = req.headers['x-forwarded-proto'] || req.protocol;
    // const host = req.headers['x-forwarded-host'] || req.get('host');
    // const redirectUri = `${protocol}://${host}/oauth-callback`;

    const params = new URLSearchParams({
        access_type: 'offline',
        client_id: GOOGLE_CLIENT_ID,
        prompt: 'consent',
        redirect_uri: OAUTH_REDIRECT_URI,
        response_type: 'code',
        scope: SCOPES.join(' '),
        state: OAUTH_STATE
    });
    const url = `https://accounts.google.com/o/oauth2/v2/auth?${params.toString()}`;
    res.json({ url });
});

// 处理解析回调 URL
router.post('/oauth/parse-url', async (req, res) => {
    let { url, replaceId, customProjectId, allowRandomProjectId } = req.body;
    if (!url) return res.status(400).json({ error: 'URL is required' });

    try {
        // 尝试作为完整 URL 解析，如果用户只粘贴了查询字符串，则添加一个虚拟主机使其可解析
        const parsedUrl = new URL(url.includes('://') ? url : `http://localhost?${url}`);
        const code = parsedUrl.searchParams.get('code') || url.trim(); // 如果没有 'code' 参数，则尝试将整个 URL 视为 code
        const state = parsedUrl.searchParams.get('state');

        if (!code || code.length < 10) {
            console.error('Invalid authorization code or URL:', url);
            return res.status(400).json({ error: '无效的授权 Code 或 URL' });
        }

        // 校验 state 避免 CSRF（可选，但原程序有校验）
        if (state && state !== OAUTH_STATE) {
            console.warn('OAuth state mismatch:', { received: state, expected: OAUTH_STATE, expectedGlobal: OAUTH_STATE });
            // 为了提高兼容性，暂时打印警告但不强制拦截（如果用户刷新了页面，OAUTH_STATE 可能会变）
        } else if (!state) {
            console.warn('OAuth state missing from callback URL.');
        }


        // 已经回归固定回调策略
        // const protocol = req.headers['x-forwarded-proto'] || req.protocol;
        // const host = req.headers['x-forwarded-host'] || req.get('host');
        // const redirectUri = `${protocol}://${host}/oauth-callback`;

        // 构造交换参数
        const params = new URLSearchParams({
            code,
            client_id: GOOGLE_CLIENT_ID,
            redirect_uri: OAUTH_REDIRECT_URI,
            grant_type: 'authorization_code'
        });
        if (GOOGLE_CLIENT_SECRET) {
            params.append('client_secret', GOOGLE_CLIENT_SECRET);
        }

        // 交换 Token (增加原程序中使用的 User-Agent 等关键 Header)
        let tokenData;
        try {
            console.log('Exchanging code for token...', { code: code.substring(0, 5) + '...', redirectUri: OAUTH_REDIRECT_URI });
            const tokenRes = await axios({
                method: 'POST',
                url: 'https://oauth2.googleapis.com/token',
                headers: {
                    'Host': 'oauth2.googleapis.com',
                    'User-Agent': 'Go-http-client/1.1',
                    'Content-Type': 'application/x-www-form-urlencoded',
                    'Accept-Encoding': 'gzip'
                },
                data: params.toString(),
                timeout: 30000 // 默认 30s 超时
            });
            tokenData = tokenRes.data;
        } catch (tokenErr) {
            const errorBody = tokenErr.response?.data;
            console.error('Code exchange failed:', errorBody || tokenErr.message);
            const errMsg = errorBody?.error_description || errorBody?.error || tokenErr.message;
            return res.status(400).json({
                error: `Google 授权交换失败: ${errMsg}`,
                details: errorBody,
                hint: `请确保您在点击“获取授权链接”生成 URL 后，授权后转跳的 8045 端口 URL 被完整复制回来。Google 仅认可 8045 端口作为重定向地址。`
            });
        }
        let projectId = customProjectId || null;
        let email = null;

        // 获取用户信息和项目 ID
        try {
            const userRes = await axios.get('https://www.googleapis.com/oauth2/v2/userinfo', {
                headers: { Authorization: `Bearer ${tokenData.access_token}` }
            });
            email = userRes.data.email;

            if (!projectId) {
                const projRes = await axios.get('https://cloudresourcemanager.googleapis.com/v1/projects', {
                    headers: { Authorization: `Bearer ${tokenData.access_token}` }
                });
                const projects = projRes.data.projects || [];
                if (projects.length > 0) {
                    projectId = projects[0].projectId;
                }
            }
        } catch (e) {
            console.error('Failed to fetch additional info:', e.message);
        }

        if (!projectId && allowRandomProjectId) {
            projectId = `antigravity-${Math.random().toString(36).substring(2, 10)}`;
        }

        if (!projectId) {
            return res.status(400).json({ error: '无法自动获取项目 ID，请手动输入或勾选允许随机 ID', code: 'PROJECT_ID_MISSING' });
        }

        // 保存账号和 Token
        let accountId = replaceId;
        if (!accountId) {
            const acc = storage.addAccount({
                name: email || 'Google Account',
                email: email,
                enable: true
            });
            accountId = acc.id;
        }

        storage.saveToken({
            accountId,
            accessToken: tokenData.access_token,
            refreshToken: tokenData.refresh_token,
            expiresIn: tokenData.expires_in,
            timestamp: Date.now(),
            projectId,
            email: email,
            userEmail: email
        });

        // 成功添加后更新状态为 online
        storage.updateAccount(accountId, { status: 'online' });

        res.json({ success: true, accountId });
    } catch (error) {
        console.error('OAuth parse failed:', error.response?.data || error.message);
        res.status(500).json({ error: error.message });
    }
});


// 批量刷新账号
router.post('/accounts/refresh-all', async (req, res) => {
    const accounts = storage.getAccounts().filter(a => a.enable);
    let refreshed = 0;
    let failed = 0;

    for (const acc of accounts) {
        try {
            const accessToken = await client.getValidToken(acc.id);
            if (!accessToken) throw new Error('Failed to get token');

            // 自动刷新邮箱信息和项目 ID

            // 1. 刷新邮箱
            let newEmail = null;
            try {
                // 尝试 v2 接口
                let userRes;
                try {
                    userRes = await axios.get('https://www.googleapis.com/oauth2/v2/userinfo', {
                        headers: {
                            Authorization: `Bearer ${accessToken}`,
                            'User-Agent': 'antigravity/1.11.3'
                        },
                        timeout: 5000
                    });
                } catch (v2Err) {
                    // 如果 v2 失败，尝试 v3
                    console.warn(`UserInfo v2 failed for ${acc.id}, trying v3:`, v2Err.message);
                    userRes = await axios.get('https://www.googleapis.com/oauth2/v3/userinfo', {
                        headers: {
                            Authorization: `Bearer ${accessToken}`,
                            'User-Agent': 'antigravity/1.11.3'
                        },
                        timeout: 5000
                    });
                }

                if (userRes.data && userRes.data.email) {
                    newEmail = userRes.data.email;
                    if (newEmail !== acc.email) {
                        storage.updateAccount(acc.id, { email: newEmail });

                        // 记录成功的更新 (Info level)
                        storage.recordLog({
                            accountId: acc.id,
                            path: 'refresh-email',
                            method: 'INTERNAL',
                            statusCode: 200,
                            durationMs: 0,
                            detail: { message: 'Email updated', oldEmail: acc.email, newEmail }
                        });
                    }
                } else {
                    storage.recordLog({
                        accountId: acc.id,
                        path: 'refresh-email',
                        method: 'INTERNAL',
                        statusCode: 200,
                        durationMs: 0,
                        detail: { warning: 'No email field in response', data: userRes.data }
                    });
                }
            } catch (userInfoError) {
                const status = userInfoError.response?.status || 0;
                const errorDetail = userInfoError.response?.data || userInfoError.message;
                console.warn(`Failed to refresh user info for ${acc.id}:`, userInfoError.message);

                // 记录错误到日志
                storage.recordLog({
                    accountId: acc.id,
                    path: 'refresh-email',
                    method: 'INTERNAL',
                    statusCode: status,
                    durationMs: 0,
                    detail: { error: 'Failed to fetch userinfo', detail: errorDetail }
                });
            }

            // 2. 刷新项目 ID (如果缺失) 并更新 Token
            try {
                const currentToken = storage.getTokenByAccountId(acc.id);
                if (currentToken) {
                    let newProjectId = currentToken.project_id;

                    if (!newProjectId) {
                        try {
                            const projRes = await axios.get('https://cloudresourcemanager.googleapis.com/v1/projects', {
                                headers: { Authorization: `Bearer ${accessToken}` },
                                timeout: 10000
                            });
                            const projects = projRes.data.projects || [];
                            if (projects.length > 0) {
                                newProjectId = projects[0].projectId;
                            }
                        } catch (projErr) {
                            // 忽略 Project ID 获取失败
                        }
                    }

                    // 构造符合 saveToken 期望的驼峰命名对象
                    const tokenData = {
                        accountId: currentToken.account_id,
                        accessToken: currentToken.access_token,
                        refreshToken: currentToken.refresh_token,
                        expiresIn: currentToken.expires_in,
                        timestamp: currentToken.timestamp,
                        projectId: newProjectId,
                        email: newEmail || currentToken.email,
                        userId: currentToken.user_id,
                        userEmail: newEmail || currentToken.user_email
                    };
                    storage.saveToken(tokenData);
                }
            } catch (tokenErr) {
                console.warn(`Failed to update token info for ${acc.id}:`, tokenErr.message);
            }

            storage.updateAccount(acc.id, { status: 'online' });
            refreshed++;
        } catch (e) {
            storage.updateAccount(acc.id, { status: 'error' });
            failed++;
        }
    }

    // 获取最新列表返回
    const updatedAccounts = storage.getAccounts();
    res.json({ success: true, refreshed, failed, total: accounts.length, accounts: updatedAccounts });
});

// 手动添加账号 (Access Token + Refresh Token)
router.post('/accounts/manual', async (req, res) => {
    try {
        const { name, accessToken, refreshToken, projectId, expiresIn } = req.body;

        if (!accessToken || !refreshToken) {
            return res.status(400).json({ error: 'Access Token and Refresh Token are required' });
        }

        let pId = projectId;
        let email = '';

        // 尝试获取用户信息
        try {
            const userRes = await axios.get('https://www.googleapis.com/oauth2/v2/userinfo', {
                headers: { Authorization: `Bearer ${accessToken}` }
            });
            email = userRes.data.email;

            if (!pId) {
                const projRes = await axios.get('https://cloudresourcemanager.googleapis.com/v1/projects', {
                    headers: { Authorization: `Bearer ${accessToken}` }
                });
                const projects = projRes.data.projects || [];
                if (projects.length > 0) {
                    pId = projects[0].projectId;
                }
            }
        } catch (e) {
            console.warn('Manual add: failed to fetch user info or project:', e.message);
        }

        // 如果获取失败，允许为空或者生成随机ID if not critical
        if (!pId) {
            // 尝试从 accessToken 解析？ 通常不行，只能随机或者报错
            // 这里允许用户手动填入，如果没填入且获取不到，则给个默认值
            pId = `antigravity-manual-${Math.random().toString(36).substring(2, 8)}`;
        }

        const acc = storage.addAccount({
            name: name || email || 'Manual Account',
            email: email,
            enable: true
        });

        storage.saveToken({
            accountId: acc.id,
            accessToken,
            refreshToken,
            expiresIn: expiresIn || 3599,
            timestamp: Date.now(),
            projectId: pId,
            email,
            userEmail: email
        });

        storage.updateAccount(acc.id, { status: 'online' });

        res.json({ success: true, accountId: acc.id });

    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 获取统计信息
router.get('/stats', (req, res) => {
    try {
        const accounts = storage.getAccounts();
        const tokens = storage.getTokens(); // 假设有 getTokens() 或直接读表
        // 由于 storage.js 只有 getTokens(), 我们需要去 storage.js 确认有没有暴露
        // 如果没有，我们暂且只统计 account
        const total = accounts.length;
        const enabled = accounts.filter(a => a.enable).length;
        const online = accounts.filter(a => a.status === 'online').length;

        res.json({ total, enabled, online, disabled: total - enabled });
    } catch (error) {
        res.status(500).json({ error: error.message });
    }
});

// 刷新单个账号项目 ID
router.post('/accounts/:id/refresh-project-id', async (req, res) => {
    try {
        const accessToken = await client.getValidToken(req.params.id);
        if (!accessToken) throw new Error('Failed to get access token');

        const projRes = await axios.get('https://cloudresourcemanager.googleapis.com/v1/projects', {
            headers: { Authorization: `Bearer ${accessToken}` }
        });
        const projects = projRes.data.projects || [];
        if (projects.length === 0) throw new Error('No projects found');

        const projectId = projects[0].projectId;
        const currentToken = storage.getTokenByAccountId(req.params.id);
        storage.saveToken({ ...currentToken, accountId: req.params.id, accessToken, projectId });

        storage.updateAccount(req.params.id, { status: 'online' });

        res.json({ success: true, projectId });
    } catch (error) {
        storage.updateAccount(req.params.id, { status: 'error' });
        res.status(500).json({ error: error.message });
    }
});

// 查询账号额度
router.get('/accounts/:id/quotas', async (req, res) => {
    try {
        const accountId = req.params.id;
        // 验证账号是否存在且启用
        const account = storage.getAccountById(accountId);
        if (!account) {
            return res.status(404).json({ error: 'Account not found' });
        }

        const quotas = await client.listQuotas(accountId);
        res.json(injectModelStatus(quotas));
    } catch (error) {
        console.error(`Fetch quotas failed for account ${req.params.id}:`, error.message);
        res.status(500).json({ error: error.message });
    }
});

/**
 * ==================== 日志与设置 ====================
 */

// 获取调用日志
router.get('/logs', (req, res) => {
    const limit = parseInt(req.query.limit) || 100;
    const logs = storage.getRecentLogs(limit);
    res.json({ logs });
});

// 获取日志详情
router.get('/logs/:id', (req, res) => {
    const log = storage.getLogDetail(req.params.id);
    if (log) res.json({ log });
    else res.status(404).json({ error: 'Log not found' });
});

// 清空日志
router.post('/logs/clear', (req, res) => {
    storage.clearLogs();
    res.json({ success: true });
});



/**
 * ==================== OpenAI 兼容接口 ====================
 */

// 列出模型
router.get('/v1/models', requireApiAuth, async (req, res) => {
    const startTime = Date.now();
    let statusCode = 200;

    try {
        // 默认使用第一个启用且有效的账号获取模型列表
        const accounts = storage.getAccounts().filter(a => a.enable);
        if (accounts.length === 0) {
            statusCode = 503;
            storage.recordLog({
                accountId: null,
                path: '/v1/models',
                method: 'GET',
                statusCode,
                durationMs: Date.now() - startTime,
                clientIp: req.ip || req.connection?.remoteAddress,
                userAgent: req.headers['user-agent']
            });
            return res.status(503).json({ error: 'No enabled accounts available' });
        }

        const data = await client.listModels(accounts[0].id);

        // 过滤被禁用的模型，并添加重定向模型
        if (data && data.data) {
            const configs = storage.getModelConfigs();
            data.data = data.data.filter(m => {
                return configs[m.id] !== undefined ? configs[m.id] : true;
            });

            // 注入重定向模型并收集目标模型
            const redirects = storage.getModelRedirects();
            const targetModels = new Set();

            redirects.forEach(r => {
                targetModels.add(r.target_model);
                // 仅当源模型不冲突时添加
                if (!data.data.find(m => m.id === r.source_model)) {
                    data.data.push({
                        id: r.source_model,
                        object: 'model',
                        created: Date.now(),
                        owned_by: 'system-redirect',
                        permission: [],
                        root: r.source_model,
                        parent: null
                    });
                }
            });

            // 移除被重命名的原始模型（即重定向的目标模型）
            data.data = data.data.filter(m => !targetModels.has(m.id));
        }

        storage.recordLog({
            accountId: accounts[0].id,
            path: '/v1/models',
            method: 'GET',
            statusCode: 200,
            durationMs: Date.now() - startTime,
            clientIp: req.ip || req.connection?.remoteAddress,
            userAgent: req.headers['user-agent'],
            detail: { modelCount: data?.data?.length || 0 }
        });

        res.json(data);
    } catch (error) {
        statusCode = 500;
        storage.recordLog({
            accountId: null,
            path: '/v1/models',
            method: 'GET',
            statusCode,
            durationMs: Date.now() - startTime,
            clientIp: req.ip || req.connection?.remoteAddress,
            userAgent: req.headers['user-agent'],
            detail: { error: error.message }
        });
        res.status(500).json({ error: error.message });
    }
});

// 聊天补全
router.post('/v1/chat/completions', requireApiAuth, async (req, res) => {
    try {
        let { model, messages, stream } = req.body;

        // 处理模型重定向
        const redirects = storage.getModelRedirects();
        const redirect = redirects.find(r => r.source_model === model);
        if (redirect) {
            console.log(`[Redirect] Redirecting model ${model} to ${redirect.target_model}`);
            model = redirect.target_model;
            // 更新请求体中的 model，确保后续逻辑使用重定向后的模型
            req.body.model = model;
        }

        // 检查模型是否被禁用
        if (!storage.isModelEnabled(model)) {
            return res.status(403).json({
                error: {
                    message: `Model '${model}' is disabled by administrator.`,
                    type: 'permission_error',
                    code: 'model_disabled'
                }
            });
        }

        // 策略：负载均衡
        const accounts = storage.getAccounts().filter(a => a.enable);
        if (accounts.length === 0) {
            return res.status(503).json({ error: 'No enabled accounts available' });
        }

        const settings = require('../../src/services/userSettings').loadUserSettings();
        const strategy = settings.load_balancing_strategy || 'random';

        const loadBalancer = require('../../src/utils/loadBalancer');
        const account = loadBalancer.getNextAccount('antigravity', accounts, strategy);

        if (stream) {
            res.setHeader('Content-Type', 'text/event-stream');
            res.setHeader('Cache-Control', 'no-cache');
            res.setHeader('Connection', 'keep-alive');

            const id = `chatcmpl-${uuidv4()}`;
            const created = Math.floor(Date.now() / 1000);

            await client.chatCompletionsStream(account.id, req.body, (event) => {
                if (event.type === 'text') {
                    const chunk = {
                        id, object: 'chat.completion.chunk', created, model,
                        choices: [{ index: 0, delta: { content: event.content }, finish_reason: null }]
                    };
                    res.write(`data: ${JSON.stringify(chunk)}\n\n`);
                } else if (event.type === 'thinking') {
                    const chunk = {
                        id, object: 'chat.completion.chunk', created, model,
                        choices: [{ index: 0, delta: { reasoning_content: event.content }, finish_reason: null }]
                    };
                    res.write(`data: ${JSON.stringify(chunk)}\n\n`);
                } else if (event.type === 'tool_calls') {
                    const chunk = {
                        id, object: 'chat.completion.chunk', created, model,
                        choices: [{
                            index: 0,
                            delta: {
                                tool_calls: event.tool_calls.map((tc, idx) => ({
                                    index: idx,
                                    id: tc.id,
                                    type: 'function',
                                    function: { name: tc.name, arguments: JSON.stringify(tc.args) }
                                }))
                            },
                            finish_reason: null
                        }]
                    };
                    res.write(`data: ${JSON.stringify(chunk)}\n\n`);
                } else if (event.type === 'signature') {
                    // 透传签名（可选，某些客户端可能需要这个来维持长对话）
                    const chunk = {
                        id, object: 'chat.completion.chunk', created, model,
                        choices: [{ index: 0, delta: { signature: event.content }, finish_reason: null }]
                    };
                    res.write(`data: ${JSON.stringify(chunk)}\n\n`);
                }
            });

            res.write(`data: [DONE]\n\n`);
            res.end();
        } else {
            // 目前简写，非流式也可以通过流式聚合实现
            let fullContent = '';
            let usage = null;
            await client.chatCompletionsStream(account.id, req.body, (event) => {
                if (event.type === 'text') {
                    fullContent += event.content;
                }
            });

            res.json({
                id: `chatcmpl-${uuidv4()}`,
                object: 'chat.completion',
                created: Math.floor(Date.now() / 1000),
                model,
                choices: [{
                    index: 0,
                    message: { role: 'assistant', content: fullContent },
                    finish_reason: 'stop'
                }]
            });
        }
    } catch (error) {
        if (!res.headersSent) {
            res.status(500).json({ error: error.message });
        } else {
            console.error('Error during streaming:', error);
            res.end();
        }
    }
});

module.exports = router;
