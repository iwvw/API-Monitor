/**
 * NextChat 模块 - Vue 方法
 * 聊天界面和会话管理的核心逻辑
 */

export const nextchatMethods = {
    // ========== 会话管理 ==========

    /**
     * 加载所有会话
     */
    async loadSessions() {
        try {
            this.nextchat.isLoading = true;
            const response = await fetch('/api/nextchat/sessions', {
                headers: this.getAuthHeaders()
            });
            const result = await response.json();

            if (result.success) {
                this.nextchat.sessions = result.data;

                // 如果没有选中的会话，选择第一个
                if (!this.nextchat.currentSessionId && this.nextchat.sessions.length > 0) {
                    this.selectSession(this.nextchat.sessions[0].id);
                }
            }
        } catch (error) {
            console.error('加载会话失败:', error);
            this.showGlobalToast('加载会话失败', 'error');
        } finally {
            this.nextchat.isLoading = false;
        }
    },

    /**
     * 选择会话
     */
    async selectSession(sessionId) {
        if (this.nextchat.currentSessionId === sessionId) return;

        this.nextchat.currentSessionId = sessionId;
        this.nextchat.messages = [];

        // 加载消息
        await this.loadMessages(sessionId);

        // 更新模型
        const session = this.nextchat.sessions.find(s => s.id === sessionId);
        if (session && session.model) {
            this.nextchat.selectedModel = session.model;
        }
    },

    /**
     * 创建新会话
     */
    async createNewSession() {
        try {
            const response = await fetch('/api/nextchat/sessions', {
                method: 'POST',
                headers: this.getAuthHeaders(),
                body: JSON.stringify({
                    topic: '新对话',
                    model: this.nextchat.selectedModel
                })
            });

            const result = await response.json();

            if (result.success) {
                this.nextchat.sessions.unshift(result.data);
                this.selectSession(result.data.id);
                this.showGlobalToast('创建新对话成功', 'success');
            }
        } catch (error) {
            console.error('创建会话失败:', error);
            this.showGlobalToast('创建会话失败', 'error');
        }
    },

    /**
     * 删除会话
     */
    async deleteSession(sessionId, event) {
        if (event) {
            event.stopPropagation();
        }

        const confirmed = await this.showConfirm({
            title: '确认删除',
            message: '确定要删除这个对话吗？',
            icon: 'fa-trash-alt',
            confirmText: '删除',
            confirmClass: 'btn-danger'
        });

        if (!confirmed) return;

        try {
            const response = await fetch(`/api/nextchat/sessions/${sessionId}`, {
                method: 'DELETE',
                headers: this.getAuthHeaders()
            });

            const result = await response.json();

            if (result.success) {
                // 从列表中移除
                this.nextchat.sessions = this.nextchat.sessions.filter(s => s.id !== sessionId);

                // 如果删除的是当前会话，选择下一个
                if (this.nextchat.currentSessionId === sessionId) {
                    if (this.nextchat.sessions.length > 0) {
                        this.selectSession(this.nextchat.sessions[0].id);
                    } else {
                        this.nextchat.currentSessionId = null;
                        this.nextchat.messages = [];
                    }
                }

                this.showGlobalToast('删除成功', 'success');
            }
        } catch (error) {
            console.error('删除会话失败:', error);
            this.showGlobalToast('删除失败', 'error');
        }
    },

    // ========== 消息管理 ==========

    /**
     * 加载会话消息
     */
    async loadMessages(sessionId) {
        try {
            const response = await fetch(`/api/nextchat/sessions/${sessionId}/messages`, {
                headers: this.getAuthHeaders()
            });
            const result = await response.json();

            if (result.success) {
                this.nextchat.messages = result.data;

                // 滚动到底部
                this.$nextTick(() => {
                    this.scrollToBottom();
                });
            }
        } catch (error) {
            console.error('加载消息失败:', error);
        }
    },

    /**
     * 发送消息
     */
    async sendMessage() {
        const content = this.nextchat.inputText.trim();
        if (!content || this.nextchat.isStreaming) return;

        // 如果没有会话，先创建一个
        if (!this.nextchat.currentSessionId) {
            await this.createNewSession();
        }

        // 添加用户消息到界面
        const userMessage = {
            id: 'temp_user_' + Date.now(),
            role: 'user',
            content: content,
            created_at: new Date().toISOString()
        };
        this.nextchat.messages.push(userMessage);

        // 清空输入框
        this.nextchat.inputText = '';

        // 添加助手消息占位
        const assistantMessage = {
            id: 'temp_assistant_' + Date.now(),
            role: 'assistant',
            content: '',
            created_at: new Date().toISOString(),
            isStreaming: true
        };
        this.nextchat.messages.push(assistantMessage);

        // 滚动到底部
        this.$nextTick(() => {
            this.scrollToBottom();
        });

        // 开始流式请求
        this.nextchat.isStreaming = true;

        try {
            const response = await fetch('/api/nextchat/chat', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    session_id: this.nextchat.currentSessionId,
                    content: content,
                    model: this.nextchat.selectedModel,
                    stream: true
                })
            });

            const reader = response.body.getReader();
            const decoder = new TextDecoder();
            let fullContent = '';

            while (true) {
                const { done, value } = await reader.read();
                if (done) break;

                const chunk = decoder.decode(value);
                const lines = chunk.split('\n');

                for (const line of lines) {
                    if (line.startsWith('data: ')) {
                        const data = line.slice(6);
                        if (data === '[DONE]') continue;

                        try {
                            const parsed = JSON.parse(data);
                            if (parsed.choices && parsed.choices[0].delta && parsed.choices[0].delta.content) {
                                fullContent += parsed.choices[0].delta.content;

                                // 更新界面
                                const lastMessage = this.nextchat.messages[this.nextchat.messages.length - 1];
                                if (lastMessage && lastMessage.role === 'assistant') {
                                    lastMessage.content = fullContent;
                                }

                                // 滚动到底部
                                this.$nextTick(() => {
                                    this.scrollToBottom();
                                });
                            }

                            if (parsed.error) {
                                throw new Error(parsed.error);
                            }
                        } catch (e) {
                            // 忽略解析错误
                            if (e.message && !e.message.includes('JSON')) {
                                console.error('解析错误:', e);
                            }
                        }
                    }
                }
            }

            // 完成流式响应
            const lastMessage = this.nextchat.messages[this.nextchat.messages.length - 1];
            if (lastMessage) {
                lastMessage.isStreaming = false;
            }

            // 更新会话标题（如果是第一条消息）
            if (this.nextchat.messages.length === 2) {
                this.updateSessionTopic(content);
            }

            // 刷新会话列表
            await this.loadSessions();

        } catch (error) {
            console.error('发送消息失败:', error);
            this.showGlobalToast('发送失败: ' + error.message, 'error');

            // 移除失败的消息
            this.nextchat.messages = this.nextchat.messages.filter(m => !m.id.startsWith('temp_'));
        } finally {
            this.nextchat.isStreaming = false;
        }
    },

    /**
     * 更新会话标题
     */
    async updateSessionTopic(firstMessage) {
        // 截取前 20 个字符作为标题
        const topic = firstMessage.length > 20 ? firstMessage.slice(0, 20) + '...' : firstMessage;

        try {
            await fetch(`/api/nextchat/sessions/${this.nextchat.currentSessionId}`, {
                method: 'PUT',
                headers: this.getAuthHeaders(),
                body: JSON.stringify({ topic })
            });
        } catch (error) {
            console.error('更新标题失败:', error);
        }
    },

    // ========== 模型切换 ==========

    /**
     * 切换模型
     */
    async changeModel() {
        if (!this.nextchat.currentSessionId) return;

        try {
            await fetch(`/api/nextchat/sessions/${this.nextchat.currentSessionId}`, {
                method: 'PUT',
                headers: this.getAuthHeaders(),
                body: JSON.stringify({ model: this.nextchat.selectedModel })
            });
        } catch (error) {
            console.error('切换模型失败:', error);
        }
    },

    // ========== UI 辅助 ==========

    /**
     * 滚动到底部
     */
    scrollToBottom() {
        const container = document.querySelector('.nextchat-messages');
        if (container) {
            container.scrollTop = container.scrollHeight;
        }
    },

    /**
     * 处理回车发送
     */
    handleKeyDown(event) {
        if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault();
            this.sendMessage();
        }
    },

    /**
     * 自动调整输入框高度
     */
    autoResize(event) {
        const textarea = event.target;
        textarea.style.height = 'auto';
        textarea.style.height = Math.min(textarea.scrollHeight, 200) + 'px';
    },

    /**
     * 渲染 Markdown
     */
    renderMarkdown(content) {
        if (!content) return '';

        // 简单的 Markdown 渲染
        let html = content
            // 代码块
            .replace(/```(\w*)\n([\s\S]*?)```/g, '<pre><code class="language-$1">$2</code></pre>')
            // 行内代码
            .replace(/`([^`]+)`/g, '<code>$1</code>')
            // 粗体
            .replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
            // 斜体
            .replace(/\*([^*]+)\*/g, '<em>$1</em>')
            // 换行
            .replace(/\n/g, '<br>');

        return html;
    },

    /**
     * 获取当前会话信息
     */
    getCurrentSession() {
        if (!this.nextchat.currentSessionId) return null;
        return this.nextchat.sessions.find(s => s.id === this.nextchat.currentSessionId);
    },

    /**
     * 初始化 NextChat 模块
     */
    async initNextChat() {
        console.log('[NextChat] 初始化...');
        await this.loadSessions();
        console.log('[NextChat] 初始化完成');
    },

    /**
     * 切换到 OpenAPI 聊天子页面
     */
    switchToOpenaiChat() {
        this.openaiCurrentTab = 'chat';
        this.initNextChat();
    }
};
