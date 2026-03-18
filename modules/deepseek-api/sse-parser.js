/**
 * DeepSeek SSE 响应解析器
 * 基于实际 DeepSeek API 响应格式实现
 *
 * 深度思考模式 SSE 格式：
 *   1. 初始消息: {"v":{"response":{...fragments:[{type:"THINK",content:"初始思考"}]...}}}
 *   2. 思考片段: {"v":"思考文本"} (无 p 字段)
 *   3. 切换回复: {"p":"response/fragments","o":"APPEND","v":[{type:"RESPONSE",content:"回复"}]}
 *   4. 回复片段: {"v":"回复文本"}
 *
 * 普通模式 SSE 格式：
 *   data: {"v":"文本片段"}
 *   data: {"p":"path","o":"SET|APPEND|BATCH","v":value}
 *   event: finish_session / close / title
 */

const { createLogger } = require('../../src/utils/logger');
const logger = createLogger('DS-SSE');

// 需要跳过的状态路径
const SKIP_PATHS = new Set([
    'quasi_status',
    'response/status',
]);

const SKIP_CONTAINS = [
    'token_usage',
    'elapsed_secs',
    'pending_fragment',
    'conversation_mode',
];

/**
 * 解析 DeepSeek SSE 流
 */
function parseSSEStream(response, isReasoner, onData, onEnd, onError, onMeta) {
    let buffer = '';
    response.setEncoding('utf8');

    // 深度思考模式下从 thinking 开始，否则从 content 开始
    let currentType = isReasoner ? 'thinking' : 'content';
    let currentEvent = '';
    let searchResults = [];

    response.on('data', (chunk) => {
        buffer += chunk;
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
            const trimmed = line.trim();
            if (!trimmed) {
                currentEvent = '';
                continue;
            }

            if (trimmed.startsWith('event: ')) {
                currentEvent = trimmed.slice(7).trim();
                continue;
            }

            if (!trimmed.startsWith('data: ')) continue;
            const dataStr = trimmed.slice(6).trim();
            if (!dataStr || dataStr === '[DONE]' || dataStr === '{}') continue;

            if (['title', 'update_session', 'close', 'finish', 'ready', 'finish_session'].includes(currentEvent)) {
                continue;
            }

            try {
                const data = JSON.parse(dataStr);

                // --- 1. 初始 response 对象 ---
                if (data.v && typeof data.v === 'object' && data.v.response) {
                    const resp = data.v.response;
                    if (onMeta && resp.message_id) {
                        onMeta({ message_id: resp.message_id });
                    }
                    if (resp.fragments && Array.isArray(resp.fragments)) {
                        for (const frag of resp.fragments) {
                            if (frag.type === 'THINK' && frag.content) {
                                onData('thinking', frag.content);
                            } else if (frag.type === 'RESPONSE' && frag.content) {
                                onData('content', frag.content);
                            } else if (frag.type === 'SEARCH' && Array.isArray(frag.results)) {
                                frag.results.forEach(r => searchResults.push(r));
                            }
                        }
                    }
                    continue;
                }

                // --- 2. 识别路径并动态确定类型 ---
                if (data.p && typeof data.p === 'string') {
                    const contentMatch = data.p.match(/response\/fragments\/(\d+)\/content/);
                    if (contentMatch) {
                        const index = parseInt(contentMatch[1]);
                        const type = (index === 0 && isReasoner) ? 'thinking' : 'content';
                        
                        if ((data.o === 'APPEND' || data.o === 'SET') && typeof data.v === 'string') {
                            onData(type, data.v);
                            currentType = type;
                        }
                        continue;
                    }

                    if (data.p.match(/response\/fragments\/\d+\/results/) && Array.isArray(data.v)) {
                         data.v.forEach(item => searchResults.push(item));
                         continue;
                    }
                }

                // --- 3. 兼容 fragments APPEND ---
                if (data.p === 'response/fragments' && data.o === 'APPEND' && Array.isArray(data.v)) {
                    for (const frag of data.v) {
                        if (frag.type === 'RESPONSE') {
                            currentType = 'content';
                            if (frag.content) onData('content', frag.content);
                        } else if (frag.type === 'THINK') {
                            currentType = 'thinking';
                            if (frag.content) onData('thinking', frag.content);
                        }
                    }
                    continue;
                }

                // --- 4. 简单内容片段 {"v":"文本"} ---
                if (data.v !== undefined && data.p === undefined && data.o === undefined) {
                    if (typeof data.v === 'string' && data.v.length > 0) {
                        onData(currentType, data.v);
                    }
                    continue;
                }

                // --- 5. 兜底带路径的文本 APPEND ---
                if (data.p && (data.o === 'APPEND' || data.o === 'SET') && typeof data.v === 'string') {
                    if (data.p.includes('content') || data.p.includes('response/fragments')) {
                        onData(currentType, data.v);
                    }
                    continue;
                }
            } catch (e) {
                // Ignore parse errors
            }
        }
    });

    response.on('end', () => {
        if (buffer.trim()) {
            const lines = buffer.split('\n');
            for (const line of lines) {
                const trimmed = line.trim();
                if (!trimmed.startsWith('data: ')) continue;
                const dataStr = trimmed.slice(6).trim();
                try {
                    const data = JSON.parse(dataStr);
                    if (data.v !== undefined && data.p === undefined && typeof data.v === 'string') {
                        onData(currentType, data.v);
                    }
                } catch (_) { }
            }
        }

        if (searchResults.length > 0) {
            let refText = '\n\n---\n**参考资料:**\n';
            const seen = new Set();
            searchResults.slice().sort((a, b) => (a.cite_index || 0) - (b.cite_index || 0)).forEach(item => {
                const idx = item.cite_index || 0;
                if (!seen.has(idx)) {
                    seen.add(idx);
                    const title = item.title || item.site_name || item.url;
                    refText += `[${idx}] [${title}](${item.url})\n`;
                }
            });
            onData('content', refText);
        }
        onEnd();
    });

    response.on('error', (err) => {
        onError(err);
    });
}

/**
 * 收集完整的 SSE 流并返回结果
 */
function collectStream(stream, thinkingEnabled) {
    return new Promise((resolve, reject) => {
        let thinking = '';
        let content = '';
        let messageId = null;

        parseSSEStream(
            stream,
            thinkingEnabled,
            (type, text) => {
                if (type === 'thinking') {
                    thinking += text;
                } else {
                    content += text;
                }
            },
            () => resolve({ thinking, content, message_id: messageId }),
            (err) => reject(err),
            (meta) => { if (meta.message_id) messageId = meta.message_id; }
        );
    });
}

function shouldSkip(eventPath) {
    if (SKIP_PATHS.has(eventPath)) return true;
    for (const pattern of SKIP_CONTAINS) {
        if (eventPath.includes(pattern)) return true;
    }
    return false;
}

module.exports = { parseSSEStream, collectStream };
