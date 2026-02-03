/**
 * AI Draw 模块 - 前端逻辑
 * 
 * 支持 Mermaid (原生集成) 和 Draw.io (embed 嵌入)
 */

import mermaid from 'mermaid';
import { renderMarkdown, showToast } from './utils.js';

// 初始化 Mermaid
try {
    mermaid.initialize({
        startOnLoad: false,
        theme: 'dark',
        securityLevel: 'loose',
        flowchart: { useMaxWidth: true, htmlLabels: true },
        sequence: { useMaxWidth: true },
        fontFamily: 'inherit',
    });
} catch (e) {
    console.error('[AI Draw] Mermaid 初始化失败:', e);
}

/**
 * AI Draw 模块数据
 */
export const aiDrawData = {
    aiDrawProjects: [],
    aiDrawCurrentProject: null,
    aiDrawLoading: false,
    aiDrawSaving: false,
    aiDrawShowCreateMenu: false,
    aiDrawShowChat: false,
    aiDrawCurrentTab: 'projects',
    aiDrawSearchQuery: '',

    // Mermaid 编辑器状态
    aiDrawMermaidCode: '',
    aiDrawMermaidSvg: '',
    aiDrawMermaidError: null,

    // Draw.io 状态
    aiDrawDrawioReady: false,
    aiDrawMessageListenerBound: false,

    // AI 聊天状态
    aiDrawChatMessages: [],
    aiDrawChatInput: '',
    aiDrawChatLoading: false,
};

/**
 * AI Draw 模块计算属性
 */
export const aiDrawComputed = {
    /**
     * 获取过滤后的项目列表
     */
    filteredAiDrawProjects() {
        if (!this.aiDrawSearchQuery) {
            return this.aiDrawProjects;
        }
        const query = this.aiDrawSearchQuery.toLowerCase();
        return this.aiDrawProjects.filter(p =>
            (p.title || '').toLowerCase().includes(query) ||
            (p.engine_type || '').toLowerCase().includes(query)
        );
    },
};

/**
 * AI Draw 模块方法
 */
export const aiDrawMethods = {
    /**
     * 模块初始化
     */
    async aiDrawInit() {
        console.log('[AI Draw] 初始化模块');
        await this.aiDrawLoadProjects();
    },

    /**
     * 加载项目列表
     */
    async aiDrawLoadProjects() {
        this.aiDrawLoading = true;
        try {
            const res = await fetch('/api/ai-draw/projects');
            if (res.ok) {
                const data = await res.json();
                this.aiDrawProjects = data.data || [];
            }
        } catch (e) {
            console.error('[AI Draw] 加载项目失败:', e);
        } finally {
            this.aiDrawLoading = false;
        }
    },

    /**
     * 创建新项目
     */
    async aiDrawCreateProject(engineType) {
        this.aiDrawShowCreateMenu = false;
        const tempId = Date.now();

        try {
            // Optimistic UI update (optional, but good for responsiveness)
            // But here we wait for server to get real ID

            showToast('正在创建项目...', 'info');

            const res = await fetch('/api/ai-draw/projects', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    title: '未命名图表',
                    engine_type: engineType,
                    content: engineType === 'mermaid' ? this.aiDrawGetDefaultMermaid() : '',
                }),
            });

            const data = await res.json();

            if (res.ok) {
                this.aiDrawProjects.unshift(data.data);
                showToast('项目创建成功', 'success');

                // 确保视图更新后再打开
                this.$nextTick(() => {
                    this.aiDrawOpenProject(data.data);
                });
            } else {
                throw new Error(data.error || '服务器返回错误');
            }
        } catch (e) {
            console.error('[AI Draw] 创建项目失败:', e);
            showToast('创建失败: ' + e.message, 'error');
        }
    },

    /**
     * 打开项目
     */
    async aiDrawOpenProject(project) {
        this.aiDrawCurrentProject = project;
        this.aiDrawCurrentTab = 'editor';

        if (project.engine_type === 'mermaid') {
            this.aiDrawMermaidCode = project.content || this.aiDrawGetDefaultMermaid();
            // 确保 DOM 已经更新且可见
            requestAnimationFrame(() => {
                this.$nextTick(() => this.aiDrawRenderMermaid());
            });
        }

        // 加载聊天历史
        await this.aiDrawLoadChatHistory(project.id);
    },

    /**
     * 获取默认 Mermaid 代码
     */
    aiDrawGetDefaultMermaid() {
        return `flowchart TD
    A[开始] --> B{判断条件}
    B -->|是| C[执行操作]
    B -->|否| D[其他操作]
    C --> E[结束]
    D --> E`;
    },

    /**
     * 渲染 Mermaid 图表
     */
    async aiDrawRenderMermaid() {
        if (!this.aiDrawMermaidCode.trim()) {
            this.aiDrawMermaidSvg = '';
            this.aiDrawMermaidError = null;
            return;
        }

        try {
            const id = 'mermaid-' + Date.now();
            const { svg } = await mermaid.render(id, this.aiDrawMermaidCode);
            this.aiDrawMermaidSvg = svg;
            this.aiDrawMermaidError = null;
        } catch (e) {
            this.aiDrawMermaidError = e.message || '语法错误';
            console.warn('[Mermaid] 渲染错误:', e);
        }
    },

    /**
     * 格式化 Mermaid 代码
     */
    aiDrawFormatMermaid() {
        // 简单的格式化：统一缩进
        const lines = this.aiDrawMermaidCode.split('\n');
        let depth = 0;
        const formatted = lines.map(line => {
            const trimmed = line.trim();
            if (!trimmed) return '';

            // 减少缩进的关键词
            if (/^end\b/i.test(trimmed)) depth = Math.max(0, depth - 1);

            const result = '    '.repeat(depth) + trimmed;

            // 增加缩进的关键词
            if (/^(subgraph|loop|alt|opt|par|critical|break)\b/i.test(trimmed)) depth++;

            return result;
        });

        this.aiDrawMermaidCode = formatted.join('\n');
        this.aiDrawRenderMermaid();
    },

    /**
     * 重置缩放
     */
    aiDrawZoomReset() {
        const preview = this.$refs.mermaidPreview;
        if (preview) {
            preview.scrollTo({ top: 0, left: 0, behavior: 'smooth' });
        }
    },

    /**
     * Draw.io 嵌入 URL
     */
    get aiDrawDrawioUrl() {
        // 使用官方 embed 服务
        const params = new URLSearchParams({
            embed: '1',
            ui: 'dark',
            spin: '1',
            proto: 'json',
            saveAndExit: '0',
            noSaveBtn: '0',
        });
        return `https://embed.diagrams.net/?${params.toString()}`;
    },

    /**
     * Draw.io iframe 加载完成
     */
    aiDrawOnDrawioLoad() {
        console.log('[Draw.io] iframe 加载完成');
        this.aiDrawDrawioReady = true;

        // 监听来自 Draw.io 的消息 (防止重复绑定)
        if (!this.aiDrawMessageListenerBound) {
            window.addEventListener('message', this.aiDrawHandleDrawioMessage);
            this.aiDrawMessageListenerBound = true;
        }

        // 如果有已保存的内容，加载它
        if (this.aiDrawCurrentProject?.content) {
            // 给一点延迟让 draw.io 内部完全初始化
            setTimeout(() => {
                this.aiDrawLoadDrawioContent(this.aiDrawCurrentProject.content);
            }, 500);
        }
    },

    /**
     * 处理 Draw.io 消息
     */
    aiDrawHandleDrawioMessage(event) {
        if (!event.data || typeof event.data !== 'string') return;

        try {
            const msg = JSON.parse(event.data);

            switch (msg.event) {
                case 'init':
                    // Draw.io 就绪，加载内容
                    if (this.aiDrawCurrentProject?.content) {
                        this.aiDrawLoadDrawioContent(this.aiDrawCurrentProject.content);
                    }
                    break;

                case 'save':
                case 'autosave':
                    // 保存内容
                    if (this.aiDrawCurrentProject) {
                        this.aiDrawCurrentProject.content = msg.xml;
                    }
                    break;

                case 'export':
                    // 导出完成
                    console.log('[Draw.io] 导出:', msg);
                    break;
            }
        } catch (e) {
            // 忽略非 JSON 消息
        }
    },

    /**
     * 加载 Draw.io 内容
     */
    aiDrawLoadDrawioContent(xml) {
        const iframe = this.$refs.drawioFrame;
        if (!iframe?.contentWindow) return;

        iframe.contentWindow.postMessage(JSON.stringify({
            action: 'load',
            xml: xml,
        }), '*');
    },

    /**
     * 保存项目
     */
    async aiDrawSaveProject() {
        if (!this.aiDrawCurrentProject) return;

        this.aiDrawSaving = true;
        try {
            let content = '';

            if (this.aiDrawCurrentProject.engine_type === 'mermaid') {
                content = this.aiDrawMermaidCode;
            } else if (this.aiDrawCurrentProject.engine_type === 'drawio') {
                // 请求 Draw.io 导出
                const iframe = this.$refs.drawioFrame;
                if (iframe?.contentWindow) {
                    iframe.contentWindow.postMessage(JSON.stringify({ action: 'export', format: 'xml' }), '*');
                }
                content = this.aiDrawCurrentProject.content || '';
            }

            const res = await fetch(`/api/ai-draw/projects/${this.aiDrawCurrentProject.id}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ content }),
            });

            if (res.ok) {
                const { data } = await res.json();
                // 更新本地数据
                const idx = this.aiDrawProjects.findIndex(p => p.id === data.id);
                if (idx >= 0) this.aiDrawProjects[idx] = data;
                this.aiDrawCurrentProject = data;

                console.log('[AI Draw] 保存成功');
            }
        } catch (e) {
            console.error('[AI Draw] 保存失败:', e);
        } finally {
            this.aiDrawSaving = false;
        }
    },

    /**
     * 更新项目标题
     */
    async aiDrawUpdateTitle(title) {
        if (!this.aiDrawCurrentProject || title === this.aiDrawCurrentProject.title) return;

        try {
            const res = await fetch(`/api/ai-draw/projects/${this.aiDrawCurrentProject.id}`, {
                method: 'PUT',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ title }),
            });

            if (res.ok) {
                const { data } = await res.json();
                this.aiDrawCurrentProject.title = data.title;
                const idx = this.aiDrawProjects.findIndex(p => p.id === data.id);
                if (idx >= 0) this.aiDrawProjects[idx].title = data.title;
            }
        } catch (e) {
            console.error('[AI Draw] 更新标题失败:', e);
        }
    },

    /**
     * 删除项目
     */
    async aiDrawDeleteProject(id) {
        if (!confirm('确定要删除此项目吗？')) return;

        try {
            const res = await fetch(`/api/ai-draw/projects/${id}`, { method: 'DELETE' });
            if (res.ok) {
                this.aiDrawProjects = this.aiDrawProjects.filter(p => p.id !== id);
                if (this.aiDrawCurrentProject?.id === id) {
                    this.aiDrawCurrentProject = null;
                }
            }
        } catch (e) {
            console.error('[AI Draw] 删除失败:', e);
        }
    },

    /**
     * 导出图表
     */
    aiDrawExport() {
        if (!this.aiDrawCurrentProject) return;

        if (this.aiDrawCurrentProject.engine_type === 'mermaid') {
            // 导出 SVG
            const svgBlob = new Blob([this.aiDrawMermaidSvg], { type: 'image/svg+xml' });
            const url = URL.createObjectURL(svgBlob);
            const a = document.createElement('a');
            a.href = url;
            a.download = `${this.aiDrawCurrentProject.title || 'diagram'}.svg`;
            a.click();
            URL.revokeObjectURL(url);
        } else {
            // 请求 Draw.io 导出
            const iframe = this.$refs.drawioFrame;
            if (iframe?.contentWindow) {
                iframe.contentWindow.postMessage(JSON.stringify({
                    action: 'export',
                    format: 'svg',
                }), '*');
            }
        }
    },

    /**
     * 加载聊天历史
     */
    async aiDrawLoadChatHistory(projectId) {
        try {
            const res = await fetch(`/api/ai-draw/projects/${projectId}/chat`);
            if (res.ok) {
                const { data } = await res.json();
                this.aiDrawChatMessages = (data || []).map(m => ({
                    role: m.role,
                    content: m.content,
                }));
            }
        } catch (e) {
            console.error('[AI Draw] 加载聊天历史失败:', e);
        }
    },

    /**
     * 发送 AI 消息
     */
    async aiDrawSendMessage(text) {
        const content = text || this.aiDrawChatInput.trim();
        if (!content) return;

        this.aiDrawChatInput = '';
        this.aiDrawChatMessages.push({ role: 'user', content });
        this.aiDrawChatLoading = true;

        // 滚动到底部
        this.$nextTick(() => {
            const container = this.$refs.aiDrawChatContainer;
            if (container) container.scrollTop = container.scrollHeight;
        });

        try {
            const engineType = this.aiDrawCurrentProject?.engine_type || 'mermaid';

            const res = await fetch('/api/ai-draw/chat', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    message: content,
                    engine_type: engineType,
                    project_id: this.aiDrawCurrentProject?.id,
                }),
            });

            if (res.ok) {
                const { data } = await res.json();
                this.aiDrawChatMessages.push({ role: 'assistant', content: data.content });
            } else {
                const err = await res.json();
                this.aiDrawChatMessages.push({ role: 'assistant', content: `错误: ${err.message || '请求失败'}` });
            }
        } catch (e) {
            this.aiDrawChatMessages.push({ role: 'assistant', content: `网络错误: ${e.message}` });
        } finally {
            this.aiDrawChatLoading = false;
            this.$nextTick(() => {
                const container = this.$refs.aiDrawChatContainer;
                if (container) container.scrollTop = container.scrollHeight;
            });
        }
    },

    /**
     * 将 AI 生成的代码应用到编辑器
     */
    aiDrawApplyCode(content) {
        // 提取代码块中的 Mermaid 代码
        const codeMatch = content.match(/```(?:mermaid)?\s*([\s\S]*?)```/);
        if (codeMatch) {
            this.aiDrawMermaidCode = codeMatch[1].trim();
        } else {
            // 尝试直接使用内容
            this.aiDrawMermaidCode = content.trim();
        }
        this.aiDrawRenderMermaid();
    },

    /**
     * 渲染 Markdown (用于聊天消息)
     */
    aiDrawRenderMarkdown(text) {
        return renderMarkdown(text);
    },

    /**
     * 获取引擎显示名称
     */
    aiDrawGetEngineName(type) {
        const names = {
            mermaid: 'Mermaid',
            drawio: 'Draw.io',
            excalidraw: 'Excalidraw',
        };
        return names[type] || type;
    },

    /**
     * 格式化日期
     */
    aiDrawFormatDate(dateStr) {
        if (!dateStr) return '';
        const date = new Date(dateStr);
        const now = new Date();
        const diff = now - date;

        if (diff < 60000) return '刚刚';
        if (diff < 3600000) return `${Math.floor(diff / 60000)} 分钟前`;
        if (diff < 86400000) return `${Math.floor(diff / 3600000)} 小时前`;
        if (diff < 604800000) return `${Math.floor(diff / 86400000)} 天前`;

        return date.toLocaleDateString('zh-CN');
    },
};
