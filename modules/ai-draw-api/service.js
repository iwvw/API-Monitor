/**
 * AI Draw 模块 - 业务服务
 * 
 * 独立的 LLM Provider 管理，支持：
 * - external: 自行配置的外部 API
 * - internal: 复用 ai-chat-api 的 Provider
 */

const { createLogger } = require('../../src/utils/logger');
const { DrawProviderModel } = require('./models');

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
     * 获取 Provider 的实际配置（处理内部/外部来源）
     * @param {Object} provider - Provider 记录
     * @returns {Object} - 包含 base_url, api_key, default_model 的配置
     */
    async resolveProviderConfig(provider) {
        if (!provider) return null;

        if (provider.source_type === 'internal') {
            // 内部来源：从 ai-chat-api 获取配置
            try {
                const { providerStorage } = require('../ai-chat-api/storage');
                const internalProvider = providerStorage.getById(provider.internal_provider_id);
                if (!internalProvider) {
                    logger.warn(`内部 Provider ${provider.internal_provider_id} 不存在`);
                    return null;
                }
                return {
                    base_url: internalProvider.base_url,
                    api_key: internalProvider.api_key,
                    default_model: provider.default_model || internalProvider.default_model,
                };
            } catch (e) {
                logger.error('获取内部 Provider 失败:', e.message);
                return null;
            }
        } else {
            // 外部来源：直接使用配置
            return {
                base_url: provider.base_url,
                api_key: provider.api_key,
                default_model: provider.default_model,
            };
        }
    }

    /**
     * 获取默认 Provider
     */
    getDefaultProvider() {
        return DrawProviderModel.getDefault();
    }

    /**
     * 获取所有 Provider
     */
    getAllProviders() {
        return DrawProviderModel.getAll();
    }

    /**
     * 获取启用的 Provider
     */
    getEnabledProviders() {
        return DrawProviderModel.getEnabled();
    }

    /**
     * 获取内部可用的 Provider 列表（来自 ai-chat-api）
     */
    getInternalProviders() {
        try {
            const { providerStorage } = require('../ai-chat-api/storage');
            const providers = providerStorage.getEnabled();
            return providers.map(p => ({
                id: p.id,
                name: p.name,
                type: p.type,
                base_url: p.base_url,
                default_model: p.default_model,
            }));
        } catch (e) {
            logger.warn('获取内部 Provider 列表失败:', e.message);
            return [];
        }
    }

    /**
     * AI 聊天生成图表
     */
    async chat(messages, options = {}) {
        const { engineType = 'drawio', providerId, model } = options;

        // 获取 Provider
        let provider;
        if (providerId) {
            provider = DrawProviderModel.getById(providerId);
        } else {
            provider = this.getDefaultProvider();
        }

        if (!provider) {
            throw new Error('未配置 AI Provider，请先在设置中添加 Provider');
        }

        // 解析实际配置
        const config = await this.resolveProviderConfig(provider);
        if (!config) {
            throw new Error(`Provider "${provider.name}" 配置无效`);
        }

        // 构建消息上下文
        const systemPrompt = SYSTEM_PROMPTS[engineType] || SYSTEM_PROMPTS.drawio;
        const contextMessages = [
            { role: 'system', content: systemPrompt },
            ...messages,
        ];

        const useModel = model || config.default_model || 'gpt-3.5-turbo';
        logger.info(`[Chat] 使用 Provider: ${provider.name}, 模型: ${useModel}, 引擎: ${engineType}`);

        try {
            const result = await this._callLLM(config, contextMessages, useModel);
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
            provider = DrawProviderModel.getById(providerId);
        } else {
            provider = this.getDefaultProvider();
        }

        if (!provider) {
            throw new Error('未配置 AI Provider');
        }

        const config = await this.resolveProviderConfig(provider);
        if (!config) {
            throw new Error(`Provider "${provider.name}" 配置无效`);
        }

        const systemPrompt = SYSTEM_PROMPTS[engineType] || SYSTEM_PROMPTS.drawio;
        const contextMessages = [
            { role: 'system', content: systemPrompt },
            ...messages,
        ];

        const useModel = model || config.default_model || 'gpt-3.5-turbo';
        logger.info(`[Stream] 使用 Provider: ${provider.name}, 模型: ${useModel}, 引擎: ${engineType}`);

        for await (const chunk of this._callLLMStream(config, contextMessages, useModel)) {
            yield chunk;
        }
    }

    /**
     * 调用 LLM API (同步)
     */
    async _callLLM(config, messages, model) {
        const url = `${config.base_url}/chat/completions`;

        const response = await fetch(url, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${config.api_key}`,
            },
            body: JSON.stringify({
                model,
                messages,
                temperature: 0.7,
                max_tokens: 4096,
                stream: false,
            }),
        });

        if (!response.ok) {
            const error = await response.json().catch(() => ({}));
            throw new Error(error.error?.message || `API 请求失败: ${response.status}`);
        }

        const data = await response.json();
        return {
            content: data.choices?.[0]?.message?.content || '',
            usage: data.usage || {},
        };
    }

    /**
     * 调用 LLM API (流式)
     */
    async *_callLLMStream(config, messages, model) {
        const url = `${config.base_url}/chat/completions`;

        const response = await fetch(url, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                'Authorization': `Bearer ${config.api_key}`,
            },
            body: JSON.stringify({
                model,
                messages,
                temperature: 0.7,
                max_tokens: 4096,
                stream: true,
            }),
        });

        if (!response.ok) {
            const error = await response.json().catch(() => ({}));
            throw new Error(error.error?.message || `API 请求失败: ${response.status}`);
        }

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        try {
            while (true) {
                const { done, value } = await reader.read();
                if (done) break;

                buffer += decoder.decode(value, { stream: true });
                const lines = buffer.split('\n');
                buffer = lines.pop() || '';

                for (const line of lines) {
                    const trimmed = line.trim();
                    if (!trimmed || !trimmed.startsWith('data: ')) continue;

                    const data = trimmed.slice(6);
                    if (data === '[DONE]') return;

                    try {
                        const json = JSON.parse(data);
                        const content = json.choices?.[0]?.delta?.content;
                        if (content) {
                            yield content;
                        }
                    } catch (e) {
                        // 忽略解析错误
                    }
                }
            }
        } finally {
            reader.releaseLock();
        }
    }

    /**
     * 测试 Provider 连接
     */
    async testProvider(provider) {
        const config = await this.resolveProviderConfig(provider);
        if (!config) {
            return { success: false, error: '配置无效' };
        }

        try {
            const result = await this._callLLM(config, [{ role: 'user', content: 'Hi' }], config.default_model || 'gpt-3.5-turbo');
            return { success: true, response: result.content.slice(0, 100) };
        } catch (e) {
            return { success: false, error: e.message };
        }
    }

    /**
     * 解析 URL 内容 (简化版)
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
