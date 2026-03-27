/**
 * 简易 Token 估算工具
 * 用于在网页反代中模拟 OpenAI 的 usage 统计
 */

/**
 * 估算文本的 Token 数量
 * 规则：
 * 1. 中文字符（包括标点）约为 2 tokens
 * 2. 英文单词（按空格切分）约为 1.3 tokens
 * 3. 其他非空白字符约为 1 token
 */
function estimateTokens(text) {
    if (!text || typeof text !== 'string') return 0;

    let count = 0;
    
    // 1. 处理中文、日文、韩文（CJK）字符
    const cjkMatches = text.match(/[\u4e00-\u9fa5\u3040-\u30ff\uac00-\ud7af\uff01-\uffee]/g);
    if (cjkMatches) {
        count += cjkMatches.length * 2;
    }

    // 2. 移除 CJK 字符后处理英文/数字
    const remaining = text.replace(/[\u4e00-\u9fa5\u3040-\u30ff\uac00-\ud7af\uff01-\uffee]/g, ' ');
    const words = remaining.trim().split(/\s+/);
    
    for (const word of words) {
        if (word.length === 0) continue;
        // 简单模拟：长度超过 4 的单词按 [长/3] 计，短单词计 1
        if (word.length > 4) {
            count += Math.ceil(word.length / 3) * 1.3;
        } else {
            count += 1.3;
        }
    }

    return Math.ceil(count);
}

/**
 * 估算消息列表的总 Token 数
 */
function estimateMessagesTokens(messages) {
    if (!Array.isArray(messages)) return 0;
    
    let total = 0;
    for (const msg of messages) {
        // 角色名开销
        total += 4; 
        
        if (typeof msg.content === 'string') {
            total += estimateTokens(msg.content);
        } else if (Array.isArray(msg.content)) {
            for (const part of msg.content) {
                if (part.type === 'text') {
                    total += estimateTokens(part.text);
                } else if (part.type === 'image_url') {
                    // 图片在 OpenAI 中通常固定计费（如 85-1105 tokens）
                    // 网页版作为附件，这里统一模拟计为 500
                    total += 500;
                }
            }
        }
    }
    // API 响应的基本开销
    total += 3; 
    return total;
}

module.exports = {
    estimateTokens,
    estimateMessagesTokens
};
