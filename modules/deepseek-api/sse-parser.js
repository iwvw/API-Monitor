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

                // --- 1. 初始 response 对象与业务错误检测 ---
                if (data.v && typeof data.v === 'object') {
                    const bizCode = data.v.code || data.v.response?.code;
                    const bizMsg = data.v.msg || data.v.response?.msg;
                    
                    if (bizCode !== undefined && bizCode !== 0 && bizCode !== '0') {
                        logger.warn(`[DeepSeek 业务错误] 代码: ${bizCode}, 信息: ${bizMsg}`);
                        onError(new Error(bizMsg || `DeepSeek Error (${bizCode})`));
                        return;
                    }

                    if (data.v.response) {
                        const resp = data.v.response;
                        if (onMeta) {
                            if (resp.message_id) onMeta({ message_id: resp.message_id });
                            if (resp.status) onMeta({ status: resp.status });
                            if (resp.auto_continue) onMeta({ status: 'AUTO_CONTINUE' });
                        }
                        if (resp.fragments && Array.isArray(resp.fragments)) {
                            for (const frag of resp.fragments) {
                                const fType = frag.type;
                                const fContent = frag.content || frag.v || ''; 
                                if (fContent) {
                                    if (fType === 'THINK') {
                                        onData('thinking', fContent);
                                        handled = true;
                                    } else if (['RESPONSE', 'CONTENT', 'TEXT', 'ANSWER'].includes(fType)) {
                                        onData('content', fContent);
                                        handled = true;
                                    } else if (fType === 'SEARCH') {
                                        const results = frag.results || frag.search_results || frag.references || [];
                                        if (Array.isArray(results)) {
                                            logger.debug(`[搜索] 初始 SEARCH fragment 收集到 ${results.length} 条结果`);
                                            results.forEach(r => searchResults.push(r));
                                        }
                                    }
                                }
                            }
                        }
                        if (handled) continue;
                    }
                }

                // --- 2. 识别路径并动态确定类型 ---
                if (data.p && typeof data.p === 'string') {
                    // 状态与指令提取 (用于自动续写)
                    if (['response/status', 'status', 'quasi_status'].includes(data.p)) {
                        if (onMeta) onMeta({ status: data.v });
                    }
                    if (['response_message_id', 'message_id'].includes(data.p)) {
                        if (onMeta) onMeta({ message_id: data.v });
                    }

                    // 匹配 response/fragments/(-1 或 \d+)/content 或类似的路径
                    const contentMatch = data.p.match(/response\/fragments\/(-?\d+)\/content/);
                    if (contentMatch) {
                        if (typeof data.v === 'string') {
                            onData(currentType, data.v);
                            handled = true;
                        }
                        if (handled) continue;
                    }
                    
                    // 状态路径匹配 (片段级状态)
                    if (data.p.match(/response\/fragments\/(-?\d+)\/status/)) {
                        if (onMeta) onMeta({ status: data.v });
                    }

                    // 搜索结果 (覆盖各种可能的路径)
                    if (data.p.match(/response\/fragments\/(-?\d+)\/(results|search_results|references)/) && Array.isArray(data.v)) {
                         logger.debug(`[搜索] 增量路径 ${data.p} 收集到 ${data.v.length} 条结果`);
                         data.v.forEach(item => searchResults.push(item));
                         continue;
                    }

                    // 搜索结果也可能出现在 response/search_results 等路径
                    if ((data.p === 'response/search_results' || data.p === 'search_results') && Array.isArray(data.v)) {
                         logger.debug(`[搜索] 顶层路径 ${data.p} 收集到 ${data.v.length} 条结果`);
                         data.v.forEach(item => searchResults.push(item));
                         continue;
                    }
                }

                // 处理嵌套在 message 里的 response (部分样本中存在)
                if (data.message?.response) {
                    const resp = data.message.response;
                    if (onMeta) {
                        if (resp.message_id) onMeta({ message_id: resp.message_id });
                        if (resp.status) onMeta({ status: resp.status });
                    }
                }

                // --- 3. 兼容 fragments APPEND (片段切换/新增) ---
                if (data.p === 'response/fragments' && (data.o === 'APPEND' || data.o === 'SET') && Array.isArray(data.v)) {
                    for (const frag of data.v) {
                        const fType = frag.type;
                        const fContent = frag.content || frag.v || '';
                        if (fType === 'RESPONSE' || fType === 'CONTENT' || fType === 'TEXT') {
                            currentType = 'content';
                            if (fContent) onData('content', fContent);
                            handled = true;
                        } else if (fType === 'THINK') {
                            currentType = 'thinking';
                            if (fContent) onData('thinking', fContent);
                            handled = true;
                        }
                    }
                    if (handled) continue;
                }

                // --- 4. 简单内容片段 {"v":"文本"} (无 p 字段) ---
                if (data.v !== undefined && data.p === undefined && (data.o === undefined || data.o === 'APPEND')) {
                    if (typeof data.v === 'string' && data.v.length > 0) {
                        onData(currentType, data.v);
                        handled = true;
                    }
                    if (handled) continue;
                }

                // --- 5. 兜底带路径的文本 (SET/APPEND/BATCH) ---
                if (data.p && typeof data.v === 'string') {
                    if (data.p.includes('content') || data.p.includes('response/fragments')) {
                        onData(currentType, data.v);
                        handled = true;
                    }
                }

                if (!handled && typeof data.v === 'string' && data.v.length > 0 && !shouldSkip(data.p || '')) {
                    // 最后的兜底：如果没处理但看起来像文本，且不在跳过列表中，也尝试收集
                    // logger.debug(`[Low-Confidence] Collected suspect data: path=${data.p}, val=${data.v}`);
                    onData(currentType, data.v);
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
