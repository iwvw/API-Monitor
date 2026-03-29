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
const DS_DELETE_SESSION_URL = 'https://chat.deepseek.com/api/v0/chat_session/delete';
const DS_DELETE_ALL_SESSIONS_URL = 'https://chat.deepseek.com/api/v0/chat_session/delete_all';

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
function isTokenInvalid(status, code, msg, bizCode, bizMsg) {
    if (status === 401 || status === 403) return true;
    if (code === 40001 || code === 40002 || code === 40003) return true;
    if (bizCode === 40001 || bizCode === 40002 || bizCode === 40003) return true;
    const m = ((msg || '') + ' ' + (bizMsg || '')).toLowerCase();
    return m.includes('token') || m.includes('unauthorized') ||
        m.includes('expired') || m.includes('not login') ||
        m.includes('login required') || m.includes('invalid jwt');
}

/**
 * 判断是否应尝试刷新 Token（更精确的判断）
 * 对 HTTP 200 但 biz_code 异常的情况做认证相关性检测
 */
function shouldAttemptRefresh(status, code, bizCode, msg, bizMsg) {
    if (isTokenInvalid(status, code, msg, bizCode, bizMsg)) return true;
    // HTTP 200/code=0 但 biz_code 非零时，检查是否是认证相关的失败
    if (status === 200 && code === 0 && bizCode !== 0) {
        const combined = ((msg || '') + ' ' + (bizMsg || '')).toLowerCase();
        const authKeywords = [
            'auth', 'authorization', 'credential', 'expired',
            'invalid jwt', 'jwt', 'login', 'not login',
            'session expired', 'token', 'unauthorized',
            '登录', '未登录', '认证', '凭证', '会话过期', '令牌',
        ];
        return authKeywords.some(kw => combined.includes(kw));
    }
    return false;
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

// ==================== 消息格式化 (移植自 ds2api/internal/prompt/messages.go) ====================

// Markdown 图片模式 - 移除 ! 前缀防止 DeepSeek 渲染异常
const MARKDOWN_IMAGE_RE = /!\[([^\]]*)\]\(([^)]+)\)/g;

/**
 * 标准化消息内容，支持字符串、数组和其他格式
 * 移植自 ds2api NormalizeContent()
 */
function normalizeContent(content) {
    if (!content) return '';
    if (typeof content === 'string') return content;
    if (Array.isArray(content)) {
        const parts = [];
        for (const item of content) {
            if (!item || typeof item !== 'object') continue;
            const type = (item.type || '').toLowerCase().trim();
            // 支持 text / output_text / input_text 类型
            if (type === 'text' || type === 'output_text' || type === 'input_text') {
                if (item.text) parts.push(item.text);
                else if (item.content) parts.push(item.content);
            }
        }
        return parts.join('\n');
    }
    return JSON.stringify(content);
}

/**
 * 使用 DeepSeek 特殊标记格式化消息
 * 移植自 ds2api MessagesPrepare()
 *
 * 核心改进：使用 DeepSeek 原生的特殊 token 标记来构建 prompt
 * 这对 R1 深度思考的上下文理解有显著提升
 */
function messagesPrepare(messages) {
    // 1. 预处理：标准化每条消息
    const processed = messages.map(m => ({
        role: m.role || 'user',
        text: normalizeContent(m.content),
    }));

    if (processed.length === 0) return '';

    // 2. 合并连续相同角色的消息
    const merged = [];
    for (const msg of processed) {
        if (merged.length > 0 && merged[merged.length - 1].role === msg.role) {
            merged[merged.length - 1].text += '\n\n' + msg.text;
        } else {
            merged.push({ ...msg });
        }
    }

    // 3. 使用 DeepSeek 特殊标记格式化
    const parts = [];
    for (let i = 0; i < merged.length; i++) {
        const m = merged[i];
        switch (m.role) {
            case 'assistant':
                parts.push(`<｜Assistant｜>${m.text}<｜end▁of▁sentence｜>`);
                break;
            case 'tool':
                if (i > 0) {
                    parts.push(`<｜Tool｜>${m.text}`);
                } else {
                    parts.push(m.text);
                }
                break;
            case 'system':
                // 清晰的 system 边界能显著改善 R1 和 V3 的上下文理解
                if (m.text.trim()) {
                    parts.push(`<system_instructions>\n${m.text.trim()}\n</system_instructions>\n\n`);
                }
                break;
            case 'user':
                // 始终为 user 消息添加标记，R1 推理在显式标记用户回合时效果最佳
                parts.push(`<｜User｜>${m.text}`);
                break;
            default:
                parts.push(m.text);
                break;
        }
    }

    // 4. 移除 Markdown 图片的 ! 前缀
    return parts.join('').replace(MARKDOWN_IMAGE_RE, '[$1]($2)');
}

/**
 * 构建 DeepSeek Completion 请求体
 * 使用增强的消息格式化（DeepSeek 特殊标记）
 */
function buildCompletionPayload(sessionId, messages, model, options = {}) {
    // 确定是否启用思考模式
    const isReasoner = model.includes('reasoner');
    const isSearch = model.includes('search');

    // 使用增强的格式化器构建 prompt
    const prompt = messagesPrepare(messages);

    const payload = {
        chat_session_id: sessionId,
        prompt: prompt,
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

// ==================== 会话清理 (移植自 ds2api/internal/deepseek/client_session_delete.go) ====================

/**
 * 删除单个 DeepSeek 会话
 */
async function deleteSession(token, sessionId) {
    if (!sessionId) return;
    try {
        const headers = { ...BASE_HEADERS, authorization: `Bearer ${token}` };
        await postJSON(DS_DELETE_SESSION_URL, headers, { chat_session_id: sessionId });
    } catch (e) {
        logger.warn(`删除会话失败: ${e.message}`);
    }
}

/**
 * 删除所有 DeepSeek 会话
 */
async function deleteAllSessions(token) {
    try {
        const headers = { ...BASE_HEADERS, authorization: `Bearer ${token}` };
        await postJSON(DS_DELETE_ALL_SESSIONS_URL, headers, {});
    } catch (e) {
        logger.warn(`删除所有会话失败: ${e.message}`);
    }
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
    deleteSession,
    deleteAllSessions,
    normalizeContent,
    BASE_HEADERS,
};
