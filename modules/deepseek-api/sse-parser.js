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
    'response/search_status',
]);

const SKIP_CONTAINS = [
    'token_usage',
    'elapsed_secs',
    'pending_fragment',
    'conversation_mode',
    'fragments/-1/status',
    'fragments/-2/status',
    'fragments/-3/status',
];

// Unicode 上标数字映射
const SUPERSCRIPT_DIGITS = { '0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴', '5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹' };

/**
 * 将 [citation:N] 转换为带超链接的 Unicode 上标
 * 有 URL 时: [citation:8] → [⁽⁸⁾](url)
 * 无 URL 时: [citation:8] → ⁽⁸⁾
 */
function formatCitations(text, searchResults) {
    return text.replace(/\[citation:(\d+)\]/g, (_, num) => {
        const idx = parseInt(num, 10);
        const superNum = num.split('').map(d => SUPERSCRIPT_DIGITS[d] || d).join('');
        const sup = `⁽${superNum}⁾`;
        // 在已收集的搜索结果中查找对应来源 URL
        const ref = searchResults.find(r => (r.cite_index || r.index) === idx);
        const url = ref && (ref.url || ref.link || ref.href);
        return url ? `[${sup}](${url})` : sup;
    });
}

/**
 * 解析 DeepSeek SSE 流
 */
function parseSSEStream(response, isReasoner, onData, onEnd, onError, onMeta) {
    let buffer = '';
    response.setEncoding('utf8');

    // 包装 onData：自动转换 [citation:N] → [⁽ᴺ⁾](url)
    const originalOnData = onData;
    onData = (type, text) => {
        originalOnData(type, formatCitations(text, searchResults));
    };

    // 深度思考模式下从 thinking 开始，否则从 content 开始
    let currentType = isReasoner ? 'thinking' : 'content';
    let currentEvent = '';
    const searchResults = [];

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
                let handled = false;

                // --- 1. 初始 response 对象与业务错误检测 (逻辑增强) ---
                const bizData = data.v || {};
                const bizCode = bizData.code !== undefined ? bizData.code : (bizData.response?.code);
                const bizMsg = bizData.msg || (bizData.response?.msg);

                if (bizCode !== undefined && bizCode !== 0 && bizCode !== '0') {
                    // 特殊处理敏感词拦截
                    if (bizCode === 'content_filter' || (bizMsg && bizMsg.includes('content_filter'))) {
                        if (onMeta) onMeta({ status: 'content_filter' });
                    }
                    logger.warn(`[DeepSeek 业务错误] 代码: ${bizCode}, 信息: ${bizMsg}`);
                    onError(new Error(bizMsg || `DeepSeek Error (${bizCode})`));
                    return;
                }

                // --- 2. 递归解析逻辑 (核心引擎) ---
                const processObject = (obj) => {
                    const { p, v, o } = obj;

                    // A. 提取元数据 (状态、ID)
                    if (p && ['response/status', 'status', 'quasi_status'].some(path => p === path || p.endsWith('/' + path))) {
                        if (onMeta) onMeta({ status: v });
                        if (v === 'content_filter' && onMeta) onMeta({ status: 'content_filter' });
                    }
                    if (p && (p === 'response_message_id' || p === 'message_id' || p.endsWith('/message_id'))) {
                        if (onMeta) onMeta({ message_id: v });
                    }

                    // B. 提取搜索/参考资料 (增量收集)
                    if (p && (p.includes('results') || p.includes('search_results') || p.includes('references')) && Array.isArray(v)) {
                        v.forEach(item => {
                            if (item && !searchResults.some(r => (r.cite_index || r.index) === (item.cite_index || item.index))) {
                                searchResults.push(item);
                            }
                        });
                    }

                    // C. 提取文本片段 (多种路径兼容)
                    // 路径匹配逻辑：包含 content 或 response/fragments 的字符串值
                    if (p && typeof v === 'string' && v.length > 0) {
                        if (p.includes('content') || p.includes('response/fragments')) {
                            onData(currentType, v);
                            handled = true;
                        }
                    }

                    // D. 处理片段数组 (DeepSeek 核心格式)
                    if ((p === 'response/fragments' || p === 'fragments') && Array.isArray(v)) {
                        for (const frag of v) {
                            const fType = frag.type || '';
                            const fContent = frag.content || frag.v || '';
                            if (fType === 'RESPONSE' || fType === 'CONTENT' || fType === 'TEXT') {
                                currentType = 'content';
                                if (fContent) { onData('content', fContent); handled = true; }
                            } else if (fType === 'THINK') {
                                currentType = 'thinking';
                                if (fContent) { onData('thinking', fContent); handled = true; }
                            } else if (fType === 'SEARCH' && Array.isArray(frag.results || frag.search_results)) {
                                (frag.results || frag.search_results).forEach(r => {
                                    if (r && !searchResults.some(seen => (seen.cite_index || seen.index) === (r.cite_index || r.index))) {
                                        searchResults.push(r);
                                    }
                                });
                            }
                        }
                    }

                    // E. 特殊处理 message/response 嵌套
                    if (obj.message?.response) {
                        const resp = obj.message.response;
                        if (onMeta) {
                            if (resp.message_id) onMeta({ message_id: resp.message_id });
                            if (resp.status) onMeta({ status: resp.status });
                        }
                    }

                    // F. 兜底处理简单值 {"v": "text"} (无路径且无操作符)
                    if (!handled && v !== undefined && p === undefined && (o === undefined || o === 'APPEND')) {
                        if (typeof v === 'string' && v.length > 0) {
                            onData(currentType, v);
                            handled = true;
                        }
                    }

                    // G. 递归遍历子对象/数组
                    if (Array.isArray(v)) {
                        v.forEach(item => {
                            if (typeof item === 'object' && item !== null) processObject(item);
                        });
                    } else if (typeof v === 'object' && v !== null && !handled) {
                        processObject(v);
                    }
                };

                // 执行解析
                processObject(data);
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

        logger.debug(`[搜索] 流结束, 共收集 ${searchResults.length} 条搜索结果`);
        if (searchResults.length > 0) {
            logger.info(`[搜索引用] 共 ${searchResults.length} 条来源, 样本: ${JSON.stringify(searchResults[0])}`);
            let refText = '\n\n---\n**参考资料:**\n';
            const seen = new Set();
            searchResults.slice().sort((a, b) => (a.cite_index || a.index || 0) - (b.cite_index || b.index || 0)).forEach(item => {
                const idx = item.cite_index || item.index || 0;
                const url = item.url || item.link || item.href || '';
                if (!seen.has(idx) && url) {
                    seen.add(idx);
                    const title = item.title || item.site_name || item.name || url;
                    refText += `[${idx}] [${title}](${url})\n`;
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
