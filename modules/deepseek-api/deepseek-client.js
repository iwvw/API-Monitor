/**
 * DeepSeek HTTP 客户端
 * 移植自 ds2api/internal/deepseek/
 *
 * 核心功能：登录、创建会话、获取 PoW、调用 Completion (SSE)
 */

const https = require('https');
const http = require('http');
const axios = require('axios');
const FormData = require('form-data');
const { createLogger } = require('../../src/utils/logger');
const { getSolver } = require('./pow-solver');
const storage = require('./storage');
const logger = createLogger('DS-Client');

// ==================== 常量 ====================

const DS_LOGIN_URL = 'https://chat.deepseek.com/api/v0/users/login';
const DS_SESSION_URL = 'https://chat.deepseek.com/api/v0/chat_session/create';
const DS_POW_URL = 'https://chat.deepseek.com/api/v0/chat/create_pow_challenge';
const DS_COMPLETION_URL = 'https://chat.deepseek.com/api/v0/chat/completion';
const DS_UPLOAD_URL = 'https://chat.deepseek.com/api/v0/chat/upload_file';

const BASE_HEADERS = {
    'Host': 'chat.deepseek.com',
    'User-Agent': 'DeepSeek/1.6.11 Android/35',
    'Accept': 'application/json',
    'Content-Type': 'application/json',
    'x-client-platform': 'android',
    'x-client-version': '1.6.11',
    'x-client-locale': 'zh_CN',
    'accept-charset': 'UTF-8',
};

// Token 内存缓存
const tokenCache = new Map(); // accountId -> { token, expiresAt }

// ==================== HTTP 辅助 ====================

/**
 * 发送 JSON POST 请求
 */
function postJSON(url, headers, body) {
    return new Promise((resolve, reject) => {
        const urlObj = new URL(url);
        const options = {
            hostname: urlObj.hostname,
            port: urlObj.port || 443,
            path: urlObj.pathname + urlObj.search,
            method: 'POST',
            headers: { ...headers },
        };
        const payload = JSON.stringify(body);
        options.headers['Content-Length'] = Buffer.byteLength(payload);

        const client = urlObj.protocol === 'https:' ? https : http;
        const req = client.request(options, (res) => {
            let data = '';
            res.on('data', chunk => { data += chunk; });
            res.on('end', () => {
                try {
                    resolve({ status: res.statusCode, data: JSON.parse(data) });
                } catch (e) {
                    resolve({ status: res.statusCode, data: { raw: data } });
                }
            });
        });

        req.on('error', reject);
        req.write(payload);
        req.end();
    });
}

/**
 * 发送流式 POST 请求（返回原始 response 对象）
 */
function streamPost(url, headers, body) {
    return new Promise((resolve, reject) => {
        const urlObj = new URL(url);
        const options = {
            hostname: urlObj.hostname,
            port: urlObj.port || 443,
            path: urlObj.pathname + urlObj.search,
            method: 'POST',
            headers: { ...headers },
        };
        const payload = JSON.stringify(body);
        options.headers['Content-Length'] = Buffer.byteLength(payload);

        const client = urlObj.protocol === 'https:' ? https : http;
        const req = client.request(options, (res) => {
            resolve(res);
        });

        req.on('error', reject);
        req.write(payload);
        req.end();
    });
}

// ==================== 认证 ====================

/**
 * 登录 DeepSeek 账号
 * @param {Object} account - { email, mobile, password }
 * @returns {string} token
 */
async function login(account) {
    const payload = {
        password: (account.password || '').trim(),
        device_id: 'deepseek_to_api',
        os: 'android',
    };

    if (account.email && account.email.trim()) {
        payload.email = account.email.trim();
    } else if (account.mobile && account.mobile.trim()) {
        const { mobile, areaCode } = normalizeMobile(account.mobile.trim());
        payload.mobile = mobile;
        if (areaCode) payload.area_code = areaCode;
    } else {
        throw new Error('Missing email/mobile');
    }

    const resp = await postJSON(DS_LOGIN_URL, BASE_HEADERS, payload);
    const data = resp.data;

    if (data.code !== 0) {
        throw new Error(`Login failed: ${data.msg || JSON.stringify(data)}`);
    }

    const bizData = data.data?.biz_data;
    const token = bizData?.user?.token;

    if (!token || !token.trim()) {
        const bizMsg = data.data?.biz_msg;
        throw new Error(`Login failed: ${bizMsg || 'missing token'}`);
    }

    return token;
}

/**
 * 标准化手机号
 */
function normalizeMobile(raw) {
    const digits = raw.replace(/\D/g, '');
    if (!digits) return { mobile: raw, areaCode: null };

    // 中国手机号 +86 前缀处理
    if ((raw.startsWith('+') || digits.startsWith('86')) && digits.startsWith('86') && digits.length === 13) {
        return { mobile: digits.slice(2), areaCode: null };
    }

    return { mobile: digits, areaCode: null };
}

/**
 * 创建对话 Session
 */
async function createSession(token) {
    const headers = { ...BASE_HEADERS, authorization: `Bearer ${token}` };
    const resp = await postJSON(DS_SESSION_URL, headers, { agent: 'chat' });

    if (resp.status === 200 && resp.data.code === 0) {
        const sessionId = resp.data.data?.biz_data?.id;
        if (sessionId) return sessionId;
    }

    // 检查 token 是否失效
    if (isTokenInvalid(resp.status, resp.data.code, resp.data.msg)) {
        throw new Error('TOKEN_INVALID');
    }

    throw new Error(`Create session failed: ${resp.data.msg || 'unknown'}`);
}

/**
 * 获取 PoW 挑战并计算答案
 */
async function getPow(token) {
    const headers = { ...BASE_HEADERS, authorization: `Bearer ${token}` };
    const resp = await postJSON(DS_POW_URL, headers, { target_path: '/api/v0/chat/completion' });

    if (resp.status === 200 && resp.data.code === 0) {
        const challenge = resp.data.data?.biz_data?.challenge;
        if (!challenge) throw new Error('Empty PoW challenge');

        const solver = getSolver();
        const answer = await solver.compute(challenge);
        const { PowSolver } = require('./pow-solver');
        return PowSolver.buildPowHeader(challenge, answer);
    }

    if (isTokenInvalid(resp.status, resp.data.code, resp.data.msg)) {
        throw new Error('TOKEN_INVALID');
    }

    throw new Error(`Get PoW failed: ${resp.data.msg || 'unknown'}`);
}

/**
 * 调用 Completion API（流式）
 * @returns {IncomingMessage} 原始 HTTP 响应流
 */
async function callCompletion(token, payload, powHeader) {
    const headers = {
        ...BASE_HEADERS,
        authorization: `Bearer ${token}`,
        'x-ds-pow-response': powHeader,
    };

    const resp = await streamPost(DS_COMPLETION_URL, headers, payload);

    if (resp.statusCode !== 200) {
        // 读取错误响应
        let errBody = '';
        for await (const chunk of resp) {
            errBody += chunk;
        }
        throw new Error(`Completion failed (${resp.statusCode}): ${errBody.slice(0, 500)}`);
    }

    return resp;
}

/**
 * 上传文件到 DeepSeek
 * @param {string} token
 * @param {string} sessionId
 * @param {Buffer} fileBuffer
 * @param {string} fileName
 * @returns {string} fileId
 */
async function uploadFile(token, sessionId, fileBuffer, fileName) {
    const form = new FormData();
    form.append('file', fileBuffer, { filename: fileName });
    form.append('chat_session_id', sessionId);

    try {
        const resp = await axios.post(DS_UPLOAD_URL, form, {
            headers: {
                ...BASE_HEADERS,
                ...form.getHeaders(),
                authorization: `Bearer ${token}`,
            },
        });

        if (resp.status === 200 && resp.data.code === 0) {
            const fileId = resp.data.data?.biz_data?.id;
            if (fileId) return fileId;
        }

        throw new Error(`Upload failed: ${resp.data.msg || 'unknown'}`);
    } catch (err) {
        if (err.response && isTokenInvalid(err.response.status, err.response.data?.code, err.response.data?.msg)) {
            throw new Error('TOKEN_INVALID');
        }
        throw err;
    }
}

/**
 * 检查 token 是否失效
 */
function isTokenInvalid(status, code, msg) {
    if (status === 401 || status === 403) return true;
    if (code === 40001 || code === 40002 || code === 40003) return true;
    const m = (msg || '').toLowerCase();
    return m.includes('token') || m.includes('unauthorized');
}

// ==================== 账号令牌管理 ====================

/**
 * 获取可用的 Access Token（自动登录/缓存）
 */
async function getAccessToken(accountId) {
    // 1. 检查内存缓存
    const cached = tokenCache.get(accountId);
    if (cached && cached.token) {
        return cached.token;
    }

    // 2. 检查数据库中的 token
    const account = storage.getAccountById(accountId);
    if (!account) throw new Error(`Account not found: ${accountId}`);

    if (account.token && account.token.trim()) {
        tokenCache.set(accountId, { token: account.token });
        return account.token;
    }

    // 3. 需要登录获取新 token
    logger.info(`账号 ${account.name || accountId} 需要登录获取 Token...`);
    const token = await login(account);

    // 保存到数据库和缓存
    storage.updateToken(accountId, token);
    tokenCache.set(accountId, { token });
    logger.info(`账号 ${account.name || accountId} 登录成功`);

    return token;
}

/**
 * 刷新 Token（重新登录）
 */
async function refreshToken(accountId) {
    const account = storage.getAccountById(accountId);
    if (!account) throw new Error(`Account not found: ${accountId}`);

    logger.info(`正在刷新账号 ${account.name || accountId} 的 Token...`);
    tokenCache.delete(accountId);

    const token = await login(account);
    storage.updateToken(accountId, token);
    tokenCache.set(accountId, { token });
    logger.info(`账号 ${account.name || accountId} Token 已刷新`);

    return token;
}

/**
 * 构建 DeepSeek Completion 请求体
 */
function buildCompletionPayload(sessionId, messages, model, options = {}) {
    const settings = storage.getSettings();

    // 确定是否启用思考模式
    const isReasoner = model.includes('reasoner');
    const isSearch = model.includes('search');

    // 构建提示词
    const prompt = messages.map(m => {
        if (typeof m.content === 'string') return m.content;
        if (Array.isArray(m.content)) {
            return m.content.map(p => p.text || '').join('\n');
        }
        return '';
    }).join('\n\n');

    // 最后一条用户消息作为 prompt
    const lastUserMsg = [...messages].reverse().find(m => m.role === 'user');
    const userPrompt = lastUserMsg
        ? (typeof lastUserMsg.content === 'string' ? lastUserMsg.content : JSON.stringify(lastUserMsg.content))
        : '';

    const payload = {
        chat_session_id: sessionId,
        prompt: userPrompt,
        ref_file_ids: options.file_ids || [],
        thinking_enabled: isReasoner,
        search_enabled: isSearch,
    };

    // 连续对话：传入上一条消息的 ID 作为 parent
    if (options.parent_message_id) {
        payload.parent_message_id = options.parent_message_id;
    }

    return payload;
}

module.exports = {
    login,
    createSession,
    getPow,
    callCompletion,
    getAccessToken,
    refreshToken,
    buildCompletionPayload,
    uploadFile,
    BASE_HEADERS,
};
