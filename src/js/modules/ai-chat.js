/**
 * AI Chat 模块 - 前端业务逻辑
 */

import { renderMarkdown } from './utils.js';


/**
 * AI Chat 模块数据
 */
export const aiChatData = {
    // Provider 相关
    aiChatProviders: [],
    aiChatCurrentProviderId: '',
    aiChatCurrentModel: '',
    aiChatCurrentProviderModels: [],

    // 对话相关
    aiChatConversations: [],
    aiChatCurrentConversation: null,
    aiChatMessages: [],

    // 输入相关
    aiChatInputMessage: '',
    aiChatIsStreaming: false,
    aiChatStreamingContent: '',
    aiChatAbortController: null,

    // UI 状态
    aiChatSidebarOpen: false,
    aiChatShowProviderModal: false,
    aiChatEditingProvider: null,
    aiChatProviderForm: {
        id: '',
        name: '',
        type: 'openai',
        base_url: 'https://api.openai.com/v1',
        api_key: '',
        default_model: 'gpt-4o',
    },
};

/**
 * AI Chat 模块计算属性
 */
export const aiChatComputed = {};

/**
 * AI Chat 模块方法
 */
export const aiChatMethods = {
    /**
     * 初始化 AI Chat 模块
     */
    async aiChatInit() {
        console.log('[AIChat] 初始化模块');
        await this.aiChatFetchProviders();
        await this.aiChatFetchConversations();
    },

    /**
     * 获取所有 Provider
     */
    async aiChatFetchProviders() {
        try {
            const res = await fetch('/api/ai-chat/providers');
            const data = await res.json();
            if (data.success) {
                this.aiChatProviders = data.data;
                // 默认选择第一个
                if (this.aiChatProviders.length > 0 && !this.aiChatCurrentProviderId) {
                    this.aiChatCurrentProviderId = this.aiChatProviders[0].id;
                    this.aiChatCurrentModel = this.aiChatProviders[0].default_model;
                    this.aiChatFetchModels(this.aiChatCurrentProviderId);
                }
            }
        } catch (error) {
            console.error('[AIChat] 获取 Provider 失败:', error);
        }
    },

    /**
     * 获取 Provider 模型列表
     */
    async aiChatFetchModels(providerId) {
        try {
            const res = await fetch(`/api/ai-chat/providers/${providerId}/models`);
            const data = await res.json();
            if (data.success) {
                this.aiChatCurrentProviderModels = data.data;
                // 如果当前模型不在列表中，使用默认模型
                const provider = this.aiChatProviders.find(p => p.id === providerId);
                if (provider && !this.aiChatCurrentProviderModels.find(m => m.id === this.aiChatCurrentModel)) {
                    this.aiChatCurrentModel = provider.default_model;
                }
            }
        } catch (error) {
            console.error('[AIChat] 获取模型列表失败:', error);
            this.aiChatCurrentProviderModels = [];
        }
    },

    /**
     * Provider 切换处理
     */
    aiChatOnProviderChange() {
        if (this.aiChatCurrentProviderId) {
            const provider = this.aiChatProviders.find(p => p.id === this.aiChatCurrentProviderId);
            this.aiChatCurrentModel = provider?.default_model || '';
            this.aiChatFetchModels(this.aiChatCurrentProviderId);
        } else {
            this.aiChatCurrentModel = '';
            this.aiChatCurrentProviderModels = [];
        }
    },

    /**
     * 获取所有对话
     */
    async aiChatFetchConversations() {
        try {
            const res = await fetch('/api/ai-chat/conversations');
            const data = await res.json();
            if (data.success) {
                this.aiChatConversations = data.data;
            }
        } catch (error) {
            console.error('[AIChat] 获取对话列表失败:', error);
        }
    },

    /**
     * 创建新对话
     */
    async aiChatNewConversation() {
        try {
            const res = await fetch('/api/ai-chat/conversations', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    provider_id: this.aiChatCurrentProviderId,
                    model: this.aiChatCurrentModel,
                }),
            });
            const data = await res.json();
            if (data.success) {
                this.aiChatConversations.unshift(data.data);
                this.aiChatSelectConversation(data.data);
            }
        } catch (error) {
            console.error('[AIChat] 创建对话失败:', error);
        }
    },

    /**
     * 选择对话
     */
    async aiChatSelectConversation(conv) {
        this.aiChatCurrentConversation = conv;
        this.aiChatMessages = [];
        this.aiChatSidebarOpen = false;

        // 加载消息
        try {
            const res = await fetch(`/api/ai-chat/conversations/${conv.id}/messages`);
            const data = await res.json();
            if (data.success) {
                this.aiChatMessages = data.data;
                this.$nextTick(() => this.aiChatScrollToBottom());
            }
        } catch (error) {
            console.error('[AIChat] 加载消息失败:', error);
        }
    },

    /**
     * 删除对话
     */
    async aiChatDeleteConversation(convId) {
        if (!confirm('确定删除这个对话吗？')) return;

        try {
            const res = await fetch(`/api/ai-chat/conversations/${convId}`, { method: 'DELETE' });
            const data = await res.json();
            if (data.success) {
                this.aiChatConversations = this.aiChatConversations.filter(c => c.id !== convId);
                if (this.aiChatCurrentConversation?.id === convId) {
                    this.aiChatCurrentConversation = null;
                    this.aiChatMessages = [];
                }
            }
        } catch (error) {
            console.error('[AIChat] 删除对话失败:', error);
        }
    },

    /**
     * 发送消息
     */
    async aiChatSendMessage() {
        const message = this.aiChatInputMessage.trim();
        if (!message || !this.aiChatCurrentProviderId) return;

        // 如果没有当前对话，先创建一个
        if (!this.aiChatCurrentConversation) {
            await this.aiChatNewConversation();
        }

        this.aiChatInputMessage = '';
        this.aiChatIsStreaming = true;
        this.aiChatStreamingContent = '';

        // 创建 AbortController 用于取消请求
        this.aiChatAbortController = new AbortController();

        try {
            const res = await fetch('/api/ai-chat/chat/stream', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    conversation_id: this.aiChatCurrentConversation.id,
                    provider_id: this.aiChatCurrentProviderId,
                    model: this.aiChatCurrentModel,
                    message,
                }),
                signal: this.aiChatAbortController.signal,
            });

            const reader = res.body.getReader();
            const decoder = new TextDecoder();

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;

                const text = decoder.decode(value, { stream: true });
                const lines = text.split('\n');

                for (const line of lines) {
                    if (!line.startsWith('data: ')) continue;
                    const data = line.slice(6);
                    if (!data) continue;

                    try {
                        const json = JSON.parse(data);

                        switch (json.type) {
                            case 'user_message':
                                this.aiChatMessages.push(json.data);
                                break;
                            case 'chunk':
                                this.aiChatStreamingContent += json.data;
                                this.aiChatScrollToBottom();
                                break;
                            case 'done':
                                this.aiChatMessages.push({
                                    id: Date.now().toString(),
                                    role: 'assistant',
                                    content: json.data.content,
                                });
                                break;
                            case 'error':
                                console.error('[AIChat] 流式响应错误:', json.data);
                                this.$toast?.error?.('AI 响应失败: ' + json.data);
                                break;
                        }
                    } catch (e) {
                        // 忽略解析错误
                    }
                }
            }
        } catch (error) {
            if (error.name !== 'AbortError') {
                console.error('[AIChat] 发送消息失败:', error);
                this.$toast?.error?.('发送失败: ' + error.message);
            }
        } finally {
            this.aiChatIsStreaming = false;
            this.aiChatStreamingContent = '';
            this.aiChatAbortController = null;

            // 刷新对话列表 (标题可能已更新)
            await this.aiChatFetchConversations();
        }
    },

    /**
     * 停止流式响应
     */
    aiChatStopStreaming() {
        if (this.aiChatAbortController) {
            this.aiChatAbortController.abort();
        }
    },

    /**
     * 滚动到底部
     */
    aiChatScrollToBottom() {
        this.$nextTick(() => {
            const container = this.$refs.aiChatMessagesContainer;
            if (container) {
                container.scrollTop = container.scrollHeight;
            }
        });
    },

    /**
     * 渲染 Markdown
     */
    aiChatRenderMarkdown(content) {
        if (!content) return '';
        try {
            return renderMarkdown(content);
        } catch (e) {
            return content;
        }
    },

    // ========== Provider 管理 ==========

    /**
     * 编辑 Provider
     */
    aiChatEditProvider(provider) {
        this.aiChatEditingProvider = provider;
        this.aiChatProviderForm = {
            id: provider.id,
            name: provider.name,
            type: provider.type,
            base_url: provider.base_url,
            api_key: '', // 不显示原有密钥
            default_model: provider.default_model,
        };
    },

    /**
     * 重置 Provider 表单
     */
    aiChatResetProviderForm() {
        this.aiChatEditingProvider = null;
        this.aiChatProviderForm = {
            id: '',
            name: '',
            type: 'openai',
            base_url: 'https://api.openai.com/v1',
            api_key: '',
            default_model: 'gpt-4o',
        };
    },

    /**
     * 保存 Provider
     */
    async aiChatSaveProvider() {
        const form = this.aiChatProviderForm;
        if (!form.name) {
            this.$toast?.warning?.('请输入 Provider 名称');
            return;
        }

        try {
            const payload = { ...form };
            // 如果是编辑模式且没有输入新密钥，不发送 api_key
            if (this.aiChatEditingProvider && !payload.api_key) {
                delete payload.api_key;
            }

            const res = await fetch('/api/ai-chat/providers', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify(payload),
            });

            const data = await res.json();
            if (data.success) {
                this.$toast?.success?.(this.aiChatEditingProvider ? 'Provider 已更新' : 'Provider 已添加');
                this.aiChatResetProviderForm();
                await this.aiChatFetchProviders();
            } else {
                this.$toast?.error?.(data.error || '保存失败');
            }
        } catch (error) {
            console.error('[AIChat] 保存 Provider 失败:', error);
            this.$toast?.error?.('保存失败: ' + error.message);
        }
    },

    /**
     * 删除 Provider
     */
    async aiChatDeleteProvider(providerId) {
        if (!confirm('确定删除这个 Provider 吗？')) return;

        try {
            const res = await fetch(`/api/ai-chat/providers/${providerId}`, { method: 'DELETE' });
            const data = await res.json();
            if (data.success) {
                this.$toast?.success?.('Provider 已删除');
                await this.aiChatFetchProviders();
            }
        } catch (error) {
            console.error('[AIChat] 删除 Provider 失败:', error);
        }
    },
};

export default {
    data: aiChatData,
    computed: aiChatComputed,
    methods: aiChatMethods,
};
