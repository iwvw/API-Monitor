/**
 * AI Draw 模块 - 业务服务
 * 
 * 复用 ai-chat-api 的 LLM Provider 能力进行 AI 绘图对话
 */

const { createLogger } = require('../../src/utils/logger');
const { providerStorage } = require('../ai-chat-api/storage');
const llmService = require('../ai-chat-api/service');

const logger = createLogger('AIDraw');

/**
 * 系统提示词 - 针对不同绘图引擎
 */
const SYSTEM_PROMPTS = {
    mermaid: `你是一个专业的 Mermaid 图表生成助手。用户会描述他们想要的图表，你需要生成对应的 Mermaid 代码。

规则：
1. 只返回 Mermaid 代码，不要包含 \`\`\`mermaid 标记
2. 支持的图表类型：flowchart, sequenceDiagram, classDiagram, stateDiagram, erDiagram, gantt, pie, mindmap, timeline
3. 使用中文标签
4. 代码必须语法正确

示例输出格式：
flowchart TD
    A[开始] --> B{判断}
    B -->|是| C[执行]
    B -->|否| D[结束]`,

    excalidraw: `你是一个 Excalidraw 图表生成助手。用户会描述他们想要的图表，你需要生成 Excalidraw 的 JSON 元素数组。

规则：
1. 返回有效的 JSON 数组，包含 Excalidraw 元素
2. 每个元素必须包含：id, type, x, y, width, height, strokeColor, backgroundColor
3. 支持的类型：rectangle, ellipse, diamond, arrow, line, text
4. 使用合理的布局和间距

示例输出格式：
[{"id":"1","type":"rectangle","x":100,"y":100,"width":120,"height":60,"strokeColor":"#1e1e1e","backgroundColor":"#a5d8ff"}]`,

    drawio: `你是一个 Draw.io 图表生成助手。用户会描述他们想要的图表，你需要生成 Draw.io 的 XML 格式内容。

规则：
1. 返回有效的 mxGraphModel XML
2. 使用清晰的布局
3. 包含适当的样式

如果用户描述不清晰，请询问更多细节。`,
};

/**
 * AI Draw 服务类
 */
class AIDrawService {
    /**
     * 获取默认 Provider
     */
    getDefaultProvider() {
        const providers = providerStorage.getAll();
        return providers.find(p => p.is_default) || providers[0] || null;
    }

    /**
     * AI 聊天生成图表
     */
    async chat(messages, options = {}) {
        const { engineType = 'drawio', providerId, model } = options;

        // 获取 Provider
        let provider;
        if (providerId) {
            provider = providerStorage.getById(providerId);
        } else {
            provider = this.getDefaultProvider();
        }

        if (!provider) {
            throw new Error('未配置 AI Provider，请先在 AI Chat 模块中添加 Provider');
        }

        // 构建消息上下文
        const systemPrompt = SYSTEM_PROMPTS[engineType] || SYSTEM_PROMPTS.drawio;
        const contextMessages = [
            { role: 'system', content: systemPrompt },
            ...messages,
        ];

        logger.info(`[Chat] 使用 Provider: ${provider.name}, 引擎: ${engineType}`);

        try {
            const result = await llmService.chat(provider, contextMessages, {
                model: model || provider.default_model
            });
            return {
                content: result.content,
                usage: result.usage,
            };
        } catch (error) {
            logger.error('[Chat] AI 调用失败:', error.message);
            throw error;
        }
    }

    /**
     * 流式 AI 聊天
     */
    async *chatStream(messages, options = {}) {
        const { engineType = 'drawio', providerId, model } = options;

        let provider;
        if (providerId) {
            provider = providerStorage.getById(providerId);
        } else {
            provider = this.getDefaultProvider();
        }

        if (!provider) {
            throw new Error('未配置 AI Provider');
        }

        const systemPrompt = SYSTEM_PROMPTS[engineType] || SYSTEM_PROMPTS.drawio;
        const contextMessages = [
            { role: 'system', content: systemPrompt },
            ...messages,
        ];

        logger.info(`[Stream] 使用 Provider: ${provider.name}, 引擎: ${engineType}`);

        for await (const chunk of llmService.chatStream(provider, contextMessages, {
            model: model || provider.default_model,
        })) {
            yield chunk;
        }
    }

    /**
     * 解析 URL 内容 (简化版，不依赖外部库)
     */
    async parseUrl(url) {
        logger.info(`[ParseURL] 解析: ${url}`);

        try {
            const response = await fetch(url, {
                headers: {
                    'User-Agent': 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36',
                },
            });

            if (!response.ok) {
                throw new Error(`HTTP ${response.status}`);
            }

            const html = await response.text();

            // 简单提取标题
            const titleMatch = html.match(/<title[^>]*>([^<]*)<\/title>/i);
            const title = titleMatch ? titleMatch[1].trim() : 'Untitled';

            // 移除脚本和样式标签，提取文本内容
            let content = html
                .replace(/<script[^>]*>[\s\S]*?<\/script>/gi, '')
                .replace(/<style[^>]*>[\s\S]*?<\/style>/gi, '')
                .replace(/<[^>]+>/g, ' ')
                .replace(/\s+/g, ' ')
                .trim()
                .substring(0, 10000);

            return {
                title,
                content,
                url,
            };
        } catch (error) {
            logger.error('[ParseURL] 解析失败:', error.message);
            throw new Error(`URL 解析失败: ${error.message}`);
        }
    }
}

module.exports = new AIDrawService();
