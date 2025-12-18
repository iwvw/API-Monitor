const axios = require('axios');
const AntigravityRequester = require('./antigravity-requester');
const storage = require('./storage');
const path = require('path');
const fs = require('fs');

// 默认配置 (保留作为 fallback)
const DEFAULT_CONFIG = {
    CLIENT_ID: '1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com',
    CLIENT_SECRET: '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',
    API_URL: 'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:streamGenerateContent?alt=sse',
    MODELS_URL: 'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels',
    NO_STREAM_URL: 'https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:generateContent',
    API_HOST: 'daily-cloudcode-pa.sandbox.googleapis.com',
    USER_AGENT: 'antigravity/1.11.3 windows/amd64'
};

let requester = null;

/**
 * 获取当前配置
 */
function getConfig() {
    const settings = storage.getSettings();
    // 将设置数组转换为对象如果需要，或者假设 getSettings 返回的是 Key-Value 对象
    // 根据 router.js 中 getSettings 的实现，它似乎返回的是个对象 map
    // 让我们做个简单的映射以防万一

    // 如果 settings 是数组 (key-value pair)，转对象
    let configMap = {};
    if (Array.isArray(settings)) {
        settings.forEach(s => configMap[s.key] = s.value);
    } else {
        configMap = settings || {};
    }

    return {
        CLIENT_ID: configMap.GOOGLE_CLIENT_ID || DEFAULT_CONFIG.CLIENT_ID,
        CLIENT_SECRET: configMap.GOOGLE_CLIENT_SECRET || DEFAULT_CONFIG.CLIENT_SECRET,
        API_URL: configMap.API_URL || DEFAULT_CONFIG.API_URL,
        MODELS_URL: configMap.API_MODELS_URL || DEFAULT_CONFIG.MODELS_URL,
        NO_STREAM_URL: configMap.API_NO_STREAM_URL || DEFAULT_CONFIG.NO_STREAM_URL,
        API_HOST: configMap.API_HOST || DEFAULT_CONFIG.API_HOST,
        USER_AGENT: configMap.API_USER_AGENT || DEFAULT_CONFIG.USER_AGENT,
        PROXY: configMap.PROXY || '',
        TIMEOUT: parseInt(configMap.TIMEOUT) || 30000
    };
}

/**
 * 初始化或获取 Requester
 */
function getRequester() {
    // 这里我们不做单例缓存，或者每次调用前重新检查配置是否变更？
    // 为了支持热重载，我们最好让 requester 实例能更新配置，或者每次只需确保 binPath 正确
    // AntigravityRequester 主要负责 spawn 进程，配置大多在请求时传入
    if (!requester) {
        requester = new AntigravityRequester({
            binPath: path.join(__dirname, 'bin')
        });
    }
    return requester;
}

/**
 * 刷新 Token
 */
async function refreshToken(account, token) {
    const config = getConfig();

    const body = new URLSearchParams({
        client_id: config.CLIENT_ID,
        client_secret: config.CLIENT_SECRET,
        grant_type: 'refresh_token',
        refresh_token: token.refresh_token
    });

    try {
        const response = await axios({
            method: 'POST',
            url: 'https://oauth2.googleapis.com/token',
            headers: {
                'Host': 'oauth2.googleapis.com',
                'User-Agent': 'Go-http-client/1.1',
                'Content-Type': 'application/x-www-form-urlencoded'
            },
            data: body.toString(),
            timeout: config.TIMEOUT,
            // 如果有代理，Axios 需要额外配置，这里暂时略过 proxy agent 的复杂配置，假设 oauth 请求直连或系统代理
            // 若需支持代理，可引入 https-proxy-agent
        });

        const newTokenData = {
            accountId: account.id,
            accessToken: response.data.access_token,
            refreshToken: token.refresh_token, // 保持原有的 refresh_token
            expiresIn: response.data.expires_in,
            timestamp: Date.now(),
            projectId: token.project_id,
            email: token.email,
            userId: token.user_id,
            userEmail: token.user_email
        };

        storage.saveToken(newTokenData);
        storage.updateAccount(account.id, { status: 'online' });
        return newTokenData;
    } catch (error) {
        console.error(`刷新 Token 失败 (${account.name}):`, error.response?.data || error.message);
        if (error.response?.status === 400 || error.response?.status === 401) {
            storage.disableToken(account.id);
            storage.updateAccount(account.id, { status: 'error' });
        }
        throw error;
    }
}

/**
 * 获取有效的 Token（如果过期会自动刷新）
 */
async function getValidToken(accountId) {
    const account = storage.getAccountById(accountId);
    if (!account || !account.enable) return null;

    let token = storage.getTokenByAccountId(accountId);
    if (!token) return null;

    // 检查是否过期 (提前5分钟刷新)
    const expiresAt = token.timestamp + (token.expires_in * 1000);
    if (Date.now() >= expiresAt - 300000) {
        try {
            const newToken = await refreshToken(account, token);
            return newToken.accessToken;
        } catch (e) {
            return null;
        }
    }

    return token.access_token;
}

/**
 * 构建请求头
 */
function buildHeaders(accessToken) {
    const config = getConfig();
    return {
        'Host': config.API_HOST,
        'User-Agent': config.USER_AGENT,
        'Authorization': `Bearer ${accessToken}`,
        'Content-Type': 'application/json',
        'Accept-Encoding': 'gzip'
    };
}

/**
 * 转换上游模型 ID
 */
function mapModels(data) {
    if (!data || !data.models) return [];
    return Object.keys(data.models).sort().map(id => ({
        id,
        object: 'model',
        created: Math.floor(Date.now() / 1000),
        owned_by: 'google'
    }));
}
/**
 * 将 OpenAI 格式的请求转换为 Antigravity API 格式
 * Antigravity 使用特殊的包装结构: {project, requestId, request: {...}, model, userAgent}
 */
function convertOpenAIToAntigravityRequest(openaiRequest, token) {
    const { model, messages, temperature, max_tokens, top_p, top_k, stop, tools } = openaiRequest;

    // 转换 messages 到 contents
    const contents = [];
    let systemText = '';

    for (const msg of messages || []) {
        if (msg.role === 'system') {
            systemText = typeof msg.content === 'string' ? msg.content : JSON.stringify(msg.content);
        } else if (msg.role === 'user') {
            const parts = [];
            if (typeof msg.content === 'string') {
                parts.push({ text: msg.content });
            } else if (Array.isArray(msg.content)) {
                for (const item of msg.content) {
                    if (item.type === 'text') {
                        parts.push({ text: item.text });
                    } else if (item.type === 'image_url') {
                        const imageUrl = item.image_url?.url || '';
                        const match = imageUrl.match(/^data:image\/(\w+);base64,(.+)$/);
                        if (match) {
                            parts.push({
                                inlineData: { mimeType: `image/${match[1]}`, data: match[2] }
                            });
                        }
                    }
                }
            }
            if (parts.length > 0) {
                contents.push({ role: 'user', parts });
            }
        } else if (msg.role === 'assistant') {
            const parts = [];
            if (typeof msg.content === 'string' && msg.content.trim()) {
                parts.push({ text: msg.content });
            }
            if (msg.tool_calls && msg.tool_calls.length > 0) {
                for (const tc of msg.tool_calls) {
                    let args = {};
                    try {
                        args = typeof tc.function.arguments === 'string'
                            ? JSON.parse(tc.function.arguments)
                            : tc.function.arguments || {};
                    } catch (e) { }
                    parts.push({
                        functionCall: { id: tc.id, name: tc.function.name, args }
                    });
                }
            }
            if (parts.length > 0) {
                contents.push({ role: 'model', parts });
            }
        } else if (msg.role === 'tool') {
            contents.push({
                role: 'user',
                parts: [{
                    functionResponse: {
                        id: msg.tool_call_id,
                        name: msg.name || '',
                        response: { output: msg.content }
                    }
                }]
            });
        }
    }

    // 检测是否启用思维链
    const enableThinking = model.endsWith('-thinking') ||
        model === 'gemini-2.5-pro' ||
        model.startsWith('gemini-3-pro-') ||
        model === 'rev19-uic3-1p';

    // 构建 generationConfig
    const generationConfig = {
        topP: top_p ?? 0.85,
        topK: top_k ?? 50,
        temperature: temperature ?? 1,
        candidateCount: 1,
        maxOutputTokens: max_tokens ?? 8096,
        stopSequences: stop ? (Array.isArray(stop) ? stop : [stop]) : [],
        thinkingConfig: {
            includeThoughts: enableThinking,
            thinkingBudget: enableThinking ? 1024 : 0
        }
    };

    // Claude thinking 模型需要删除 topP 参数
    if (enableThinking && model.includes('claude')) {
        delete generationConfig.topP;
    }

    // 转换 tools 格式
    const antigravityTools = (tools && tools.length > 0) ? tools.map(tool => ({
        functionDeclarations: [{
            name: tool.function?.name,
            description: tool.function?.description,
            parameters: tool.function?.parameters
        }]
    })).filter(t => t.functionDeclarations[0].name) : [];

    // 构建 request 对象
    // sessionId 是 Antigravity API 所需的关键字段
    const sessionId = String(-Math.floor(Math.random() * 9e18));

    const request = {
        contents,
        systemInstruction: {
            role: 'user',
            parts: [{ text: systemText || '' }]
        },
        generationConfig,
        sessionId
    };

    // 只有当有工具时才添加 tools 和 toolConfig
    if (antigravityTools.length > 0) {
        request.tools = antigravityTools;
        request.toolConfig = {
            functionCallingConfig: { mode: 'VALIDATED' }
        };
    }

    // 构建 Antigravity 请求体
    return {
        project: token?.project_id || '',
        requestId: `req-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
        request,
        model: model,
        userAgent: 'antigravity'
    };
}

/**
 * 列出可用模型
 */
async function listModels(accountId) {
    const token = await getValidToken(accountId);
    if (!token) throw new Error('No valid token available');

    const config = getConfig();
    const headers = buildHeaders(token);
    const req = getRequester();

    const response = await req.antigravity_fetch(config.MODELS_URL, {
        method: 'POST',
        headers,
        body: JSON.stringify({}),
        proxy: config.PROXY,
        timeout: config.TIMEOUT
    });

    if (response.status !== 200) {
        const text = await response.text();
        throw new Error(`Failed to list models: ${text}`);
    }

    const data = await response.json();
    return {
        object: 'list',
        data: mapModels(data)
    };
}

/**
 * 处理流式响应片段
 */
function parseAndEmitStreamChunk(line, callback) {
    if (!line.startsWith('data: ')) return null;

    try {
        const data = JSON.parse(line.slice(6));
        const parts = data.response?.candidates?.[0]?.content?.parts;

        let result = {
            usage: data.response?.usageMetadata,
            done: !!data.response?.candidates?.[0]?.finishReason
        };

        if (parts) {
            for (const part of parts) {
                if (part.thought === true) {
                    callback({ type: 'thinking', content: part.text });
                } else if (part.text !== undefined) {
                    callback({ type: 'text', content: part.text });
                } else if (part.functionCall) {
                    callback({ type: 'tool_calls', tool_calls: [part.functionCall] });
                }
            }
        }

        return result;
    } catch (e) {
        return null;
    }
}

/**
 * 发送聊天补全请求 (流式)
 */
async function chatCompletionsStream(accountId, requestBody, callback) {
    const account = storage.getAccountById(accountId);
    if (!account || !account.enable) throw new Error('Account not found or disabled');

    let tokenObj = storage.getTokenByAccountId(accountId);
    if (!tokenObj) throw new Error('No valid token available');

    // 检查是否过期并刷新
    const expiresAt = tokenObj.timestamp + (tokenObj.expires_in * 1000);
    if (Date.now() >= expiresAt - 300000) {
        try {
            const newToken = await refreshToken(account, tokenObj);
            tokenObj = { ...tokenObj, access_token: newToken.accessToken, project_id: newToken.projectId || tokenObj.project_id };
        } catch (e) {
            throw new Error('Token refresh failed');
        }
    }

    const accessToken = tokenObj.access_token;

    const config = getConfig();
    const headers = buildHeaders(accessToken);
    const req = getRequester();

    const startTime = Date.now();
    let statusCode = 200;
    let errorText = '';

    // 将 OpenAI 格式转换为 Antigravity API 格式
    const antigravityRequest = convertOpenAIToAntigravityRequest(requestBody, tokenObj);

    try {
        const stream = req.antigravity_fetchStream(config.API_URL, {
            method: 'POST',
            headers,
            body: JSON.stringify(antigravityRequest),
            proxy: config.PROXY,
            timeout: config.TIMEOUT
        });

        let buffer = '';
        await new Promise((resolve, reject) => {
            stream
                .onStart(({ status }) => { statusCode = status; })
                .onData((chunk) => {
                    if (statusCode !== 200) {
                        errorText += chunk;
                        return;
                    }
                    buffer += chunk;
                    const lines = buffer.split('\n');
                    buffer = lines.pop();
                    lines.forEach(line => parseAndEmitStreamChunk(line, callback));
                })
                .onEnd(() => {
                    if (statusCode !== 200) {
                        reject(new Error(`API Error ${statusCode}: ${errorText}`));
                    } else {
                        resolve();
                    }
                })
                .onError(reject);
        });

        storage.recordLog({
            accountId,
            path: '/v1/chat/completions',
            method: 'POST',
            statusCode,
            durationMs: Date.now() - startTime,
            detail: { model: requestBody.model, messageCount: requestBody.messages?.length || 0 }
        });
    } catch (error) {
        storage.recordLog({
            accountId,
            path: '/v1/chat/completions',
            method: 'POST',
            statusCode: statusCode || 500,
            durationMs: Date.now() - startTime
        });
        throw error;
    }
}

/**
 * 获取额度并进行模型分组
 */
async function listQuotas(accountId) {
    const token = await getValidToken(accountId);
    if (!token) throw new Error('No valid token available');

    const config = getConfig();
    const headers = buildHeaders(token);
    const req = getRequester();

    const response = await req.antigravity_fetch(config.MODELS_URL, {
        method: 'POST',
        headers,
        body: JSON.stringify({}),
        proxy: config.PROXY,
        timeout: config.TIMEOUT
    });

    if (response.status !== 200) {
        const text = await response.text();
        throw new Error(`Failed to fetch quotas: ${text}`);
    }

    const data = await response.json();
    const models = data.models || {};

    // 定义分组规则
    const groups = [
        {
            id: 'banana_pro',
            name: 'Banana_Pro',
            description: 'Gemini Pro图像生成模型',
            icon: '🍌',
            patterns: ['gemini-3-pro-image']
        },
        {
            id: 'claude_gpt',
            name: 'Claude/GPT',
            description: 'Claude和GPT模型共享额度',
            icon: '🧠',
            patterns: ['claude-', 'gpt-', 'o1-', 'o3-']
        },
        {
            id: 'tab_completion',
            name: 'Tab补全',
            description: 'Tab补全模型',
            icon: '📝',
            patterns: ['chat_']
        },
        {
            id: 'gemini',
            name: 'Gemini',
            description: 'Gemini模型',
            icon: '💎',
            patterns: ['gemini-2.5-', 'gemini-2.0-', 'gemini-3-', 'rev19-uic3-1p']
        }
    ];

    const result = {};
    const processedModels = new Set();

    const formatDate = (dateInput) => {
        if (!dateInput) return null;
        try {
            const date = new Date(dateInput);
            if (isNaN(date.getTime())) return null;
            // 返回 ISO 时间戳，前端计算倒计时
            return date.toISOString();
        } catch (e) {
            return null;
        }
    };

    groups.forEach(group => {
        const groupModels = [];
        let minRemaining = 100;
        let latestReset = null;

        Object.entries(models).forEach(([id, info]) => {
            // 如果模型已经被处理过，则跳过
            if (processedModels.has(id)) return;

            const isMatch = group.patterns.some(p => id.toLowerCase().includes(p.toLowerCase()));
            if (isMatch) {
                processedModels.add(id);

                let modelRem = 100;
                let modelResetTime = null;

                if (info.quotaInfo) {
                    let remVal = null;
                    if (info.quotaInfo.remainingFraction !== undefined && info.quotaInfo.remainingFraction !== null) {
                        remVal = Number(info.quotaInfo.remainingFraction) * 100;
                    } else if (info.quotaInfo.remaining !== undefined && info.quotaInfo.remaining !== null) {
                        remVal = Number(info.quotaInfo.remaining);
                    }

                    if (remVal !== null && !isNaN(remVal)) {
                        modelRem = remVal;
                        minRemaining = Math.min(minRemaining, modelRem);
                    }

                    if (info.quotaInfo.resetTime) {
                        modelResetTime = info.quotaInfo.resetTime;
                        if (!latestReset || info.quotaInfo.resetTime > latestReset) {
                            latestReset = info.quotaInfo.resetTime;
                        }
                    }
                }

                groupModels.push({
                    id: id,
                    remaining: Math.round(modelRem),
                    resetTime: formatDate(modelResetTime) || '永不重置'
                });
            }
        });

        if (groupModels.length > 0) {
            // Sort models by ID to ensure consistent order
            groupModels.sort((a, b) => a.id.localeCompare(b.id));

            result[group.id] = {
                name: group.name,
                description: group.description,
                icon: group.icon,
                models: groupModels,
                remaining: Math.round(minRemaining),
                resetTime: formatDate(latestReset) || '01-01 08:00'
            };
        }
    });

    // 其他模型
    const others = [];
    Object.entries(models).forEach(([id, info]) => {
        if (!processedModels.has(id)) {
            let rem = 100;
            if (info.quotaInfo) {
                if (info.quotaInfo.remainingFraction !== undefined && info.quotaInfo.remainingFraction !== null) {
                    rem = Number(info.quotaInfo.remainingFraction) * 100;
                } else if (info.quotaInfo.remaining !== undefined && info.quotaInfo.remaining !== null) {
                    rem = Number(info.quotaInfo.remaining);
                }
            }
            if (isNaN(rem)) rem = 100;

            others.push({
                id,
                remaining: Math.round(rem),
                resetTime: formatDate(info.quotaInfo?.resetTime) || '12-18 12:19'
            });
        }
    });

    if (others.length > 0) {
        // Sort others by ID to ensure consistent order
        others.sort((a, b) => a.id.localeCompare(b.id));

        result['others'] = {
            name: '其他模型',
            description: '未分组模型单独计费',
            icon: '📋',
            models: others
        };
    }

    return result;
}

module.exports = {
    getValidToken,
    listModels,
    listQuotas,
    chatCompletionsStream,
    getRequester
};

