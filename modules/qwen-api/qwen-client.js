/**
 * Qwen HTTP 客户端 (api-monitor 移植版)
 * 核心逻辑参考 qwen2API (Python) 实现
 * 
 * 提供：消息格式转换、会话管理、SSE 请求分发
 */

const axios = require('axios');
const { createLogger } = require('../../src/utils/logger');
const storage = require('./storage');
const logger = createLogger('Qwen-Client');

// === Qwen 网页版 API 常量 ===
const QWEN_BASE = 'https://chat.qwen.ai';
const URLS = {
    CONVERSATION: `${QWEN_BASE}/api/v1/chat/completions`,
    FILES: `${QWEN_BASE}/api/v1/files`,
    MODELS: `${QWEN_BASE}/api/v1/models`,
};

// 模拟真实浏览器的 Headers (对应原项目的 camoufox 特征)
const BASE_HEADERS = {
    'Authority': 'chat.qwen.ai',
    'Accept': 'application/json, text/plain, */*',
    'Accept-Language': 'zh-CN,zh;q=0.9,en;q=0.8',
    'Content-Type': 'application/json',
    'Origin': QWEN_BASE,
    'Referer': `${QWEN_BASE}/`,
    'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36',
    'Sec-Ch-Ua': '"Not_A Brand";v="24", "Chromium";v="130", "Google Chrome";v="130"',
    'Sec-Ch-Ua-Mobile': '?0',
    'Sec-Ch-Ua-Platform': '"Windows"',
    'Sec-Fetch-Dest': 'empty',
    'Sec-Fetch-Mode': 'cors',
    'Sec-Fetch-Site': 'same-origin',
    'X-Requested-With': 'XMLHttpRequest',
};

/**
 * 将 OpenAI 格式的消息转换为 Qwen Payload
 */
function buildChatPayload(messages, model, options = {}) {
    // 基础模型转换
    const isReasoner = model.includes('reasoner') || model.includes('o1') || model.includes('r1');
    
    // Qwen 网页版消息结构处理
    const qwenMessages = messages.map(msg => ({
        role: msg.role === 'system' ? 'user' : msg.role,
        content: msg.content
    }));

    const payload = {
        model: 'qwen-max', // 强制指定为网页版最高性能模型
        messages: qwenMessages,
        stream: options.stream || false,
        incremental_output: true, // 增量输出 (网页版特有)
        plugins: options.plugins || [], // 工具调用支持
    };

    if (options.session_id) {
        payload.session_id = options.session_id;
    }

    return payload;
}

/**
 * 调用 Qwen 对话接口
 */
async function callChat(token, payload, isStream = false) {
    const headers = {
        ...BASE_HEADERS,
        'Authorization': `Bearer ${token}`,
    };

    try {
        const response = await axios.post(URLS.CONVERSATION, payload, {
            headers: headers,
            responseType: isStream ? 'stream' : 'json',
            validateStatus: false,
            timeout: 60000,
        });

        if (response.status !== 200) {
            let errorMsg = 'Unknown error';
            if (!isStream) {
                errorMsg = JSON.stringify(response.data);
            }
            throw new Error(`Qwen upstream error (${response.status}): ${errorMsg}`);
        }

        return response;
    } catch (error) {
        logger.error(`Qwen call error: ${error.message}`);
        throw error;
    }
}

/**
 * 刷新 Token 逻辑 (Placeholder - Qwen 通常需要重新从 Cookie/存储中更新)
 */
async function refreshToken(accountId) {
    const account = storage.getAccountById(accountId);
    if (!account) throw new Error('Account not found');
    
    // Qwen2API 原版逻辑通常是通过 Camoufox 重新获取
    // 这里建议手动更新 Token，或在 router 中触发手动登录测试
    logger.info(`Refresh token requested for ${account.name}`);
    return account.token;
}

module.exports = {
    buildChatPayload,
    callChat,
    refreshToken,
    BASE_HEADERS,
    URLS
};
