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
function parseSSEStream(stream, thinkingEnabled, onChunk, onDone, onError, onMetadata) {
    let buffer = '';
    // 深度思考模式下从 thinking 开始，否则从 content 开始
    let currentType = thinkingEnabled ? 'thinking' : 'content';
    let currentEvent = '';
    let searchResults = [];

    stream.on('data', (chunk) => {
        buffer += chunk.toString();

        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
            const trimmed = line.trim();

            if (!trimmed) {
                currentEvent = '';
                continue;
            }

            // 事件类型
            if (trimmed.startsWith('event: ')) {
                currentEvent = trimmed.slice(7).trim();
                continue;
            }

            if (!trimmed.startsWith('data: ')) continue;
            const dataStr = trimmed.slice(6).trim();
            if (!dataStr || dataStr === '[DONE]' || dataStr === '{}') continue;

            // 跳过非内容事件
            if (currentEvent === 'title' || currentEvent === 'update_session' ||
                currentEvent === 'close' || currentEvent === 'finish' ||
                currentEvent === 'ready' || currentEvent === 'finish_session') {
                continue;
            }

            try {
                const data = JSON.parse(dataStr);

                // --- 格式1: 初始 response 对象 {"v":{"response":{...}}} ---
                if (data.v && typeof data.v === 'object' && data.v.response) {
                    const resp = data.v.response;
                    if (onMetadata && resp.message_id) {
                        onMetadata({ message_id: resp.message_id });
                    }
                    if (resp.fragments && Array.isArray(resp.fragments)) {
                        for (const frag of resp.fragments) {
                            if (frag.type === 'THINK' && frag.content) {
                                currentType = 'thinking';
                                onChunk('thinking', frag.content);
                            } else if (frag.type === 'RESPONSE' && frag.content) {
                                currentType = 'content';
                                onChunk('content', frag.content);
                            } else if (frag.type === 'SEARCH' && Array.isArray(frag.results)) {
                                frag.results.forEach(r => searchResults.push(r));
                            }
                        }
                    }
                    continue;
                }

                // --- 格式2: fragments APPEND (思考→正文切换信号) ---
                if (data.p === 'response/fragments' && data.o === 'APPEND' && Array.isArray(data.v)) {
                    for (const frag of data.v) {
                        if (frag.type === 'RESPONSE') {
                            currentType = 'content';
                            if (frag.content) {
                                onChunk('content', frag.content);
                            }
                        } else if (frag.type === 'THINK') {
                            currentType = 'thinking';
                            if (frag.content) {
                                onChunk('thinking', frag.content);
                            }
                        }
                    }
                    continue;
                }

                // --- 格式3: 简单内容片段 {"v":"文本"} (无 p 字段) ---
                if (data.v !== undefined && data.p === undefined && data.o === undefined) {
                    if (typeof data.v === 'string' && data.v.length > 0) {
                        onChunk(currentType, data.v);
                    }
                    continue;
                }

                // --- 格式4: 带路径的内容 APPEND ---
                if (data.p && data.o === 'APPEND' && typeof data.v === 'string') {
                    if (data.p.includes('content')) {
                        onChunk(currentType, data.v);
                    }
                    continue;
                }

                // --- 格式5: BATCH 操作 ---
                if (data.p && data.o === 'BATCH' && Array.isArray(data.v)) {
                    // 通常是 token_usage / quasi_status 等元数据，跳过
                    continue;
                }

                // --- 格式6: 搜索结果单独下发 ---
                if (data.p && typeof data.p === 'string' && data.p.endsWith('/results') && Array.isArray(data.v)) {
                    if (data.v.length > 0 && data.v[0].cite_index !== undefined) {
                        data.v.forEach(item => searchResults.push(item));
                    }
                    continue;
                }

                // 其它带路径的 SET 操作，大多是状态更新，跳过
                if (data.p !== undefined) {
                    if (shouldSkip(data.p)) continue;
                }

            } catch (e) {
                // 忽略解析错误
            }
        }
    });

    stream.on('end', () => {
        // 处理残余 buffer
        if (buffer.trim()) {
            const lines = buffer.split('\n');
            for (const line of lines) {
                const trimmed = line.trim();
                if (!trimmed.startsWith('data: ')) continue;
                const dataStr = trimmed.slice(6).trim();
                if (!dataStr || dataStr === '[DONE]' || dataStr === '{}') continue;
                try {
                    const data = JSON.parse(dataStr);
                    if (data.v !== undefined && data.p === undefined && typeof data.v === 'string' && data.v.length > 0) {
                        onChunk(currentType, data.v);
                    }
                } catch (_) { }
            }
        }

        if (searchResults.length > 0) {
            let refText = '\n\n---\n**参考资料:**\n';
            const seen = new Set();
            searchResults
                .slice()
                .sort((a, b) => (a.cite_index || 0) - (b.cite_index || 0))
                .forEach(item => {
                    const idx = item.cite_index || 0;
                    if (!seen.has(idx)) {
                        seen.add(idx);
                        const title = item.title || item.site_name || item.url;
                        refText += `[${idx}] [${title}](${item.url})\n`;
                    }
                });
            onChunk('content', refText);
        }

        onDone();
    });

    stream.on('error', (err) => {
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
