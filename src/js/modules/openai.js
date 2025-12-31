/**
 * OpenAI API 模块
 * 负责 OpenAI API 相关功能
 */

import { store } from '../store.js';
import { toast } from './toast.js';

// 缓存 key 常量（定义在模块级别，避免 Vue 警告）
const OPENAI_CACHE_KEY = 'openai_endpoints_cache';

export const openaiMethods = {
  // 从本地缓存加载端点数据（立即显示）
  loadFromOpenaiCache() {
    try {
      const cached = localStorage.getItem(OPENAI_CACHE_KEY);
      if (cached) {
        const data = JSON.parse(cached);
        if (data && Array.isArray(data.endpoints)) {
          store.openaiEndpoints = data.endpoints.map(ep => ({
            ...ep,
            showKey: false,
            refreshing: false,
          }));
          return true;
        }
      }
    } catch (e) {
      console.warn('加载 OpenAI 缓存失败:', e);
    }
    return false;
  },

  // 保存端点数据到本地缓存
  saveToOpenaiCache(endpoints) {
    try {
      localStorage.setItem(
        OPENAI_CACHE_KEY,
        JSON.stringify({
          endpoints,
          timestamp: Date.now(),
        })
      );
    } catch (e) {
      console.warn('保存 OpenAI 缓存失败:', e);
    }
  },

  switchToOpenai() {
    store.mainActiveTab = 'openai';
    if (store.openaiEndpoints.length === 0) {
      // 优先加载缓存
      this.loadFromOpenaiCache();
      // 后台刷新最新数据
      this.loadOpenaiEndpoints(true);
    }
  },

  showOpenaiToast(message, type = 'success') {
    toast[type](message);
  },

  async loadOpenaiEndpoints(silent = false) {
    if (!silent) store.openaiLoading = true;
    try {
      // 1. 加载端点列表（用于账号管理展示）
      const epResponse = await fetch('/api/openai/endpoints', {
        headers: store.getAuthHeaders(),
      });
      const epData = await epResponse.json();
      if (Array.isArray(epData)) {
        // 保持当前的展开状态
        const expandedIds = { ...this.openaiExpandedEndpoints };

        store.openaiEndpoints = epData.map(ep => ({
          ...ep,
          showKey: false,
          refreshing: false,
        }));

        // 保存到本地缓存
        this.saveToOpenaiCache(epData);
      }

      // 2. 从聚合接口加载全渠道模型列表 (HChat 使用)
      const modelsResponse = await fetch('/v1/models', {
        headers: store.getAuthHeaders(),
      });
      const modelsData = await modelsResponse.json();

      if (modelsData && Array.isArray(modelsData.data)) {
        // 存储包含渠道信息的完整对象
        store.openaiAllModels = modelsData.data.sort((a, b) => {
          // 先按渠道排序，再按名称排序
          if (a.owned_by !== b.owned_by) return a.owned_by.localeCompare(b.owned_by);
          return a.id.localeCompare(b.id);
        });

        // 智能初始化模型
        if (store.openaiAllModels.length > 0) {
          if (
            !store.openaiChatModel ||
            !store.openaiAllModels.find(m => m.id === store.openaiChatModel)
          ) {
            store.openaiChatModel = store.openaiAllModels[0].id;
          }
        }
      }

      if (!silent && store.mainActiveTab === 'openai' && store.openaiCurrentTab === 'endpoints') {
        toast.success('端点及模型列表已刷新');
      }
    } catch (error) {
      console.error('加载模型列表失败:', error);
    } finally {
      if (!silent) store.openaiLoading = false;
    }
  },

  // 移除旧的本地过滤方法，改用聚合数据
  updateOpenaiAllModels(explicitRefresh = false) {
    this.loadOpenaiEndpoints(true); // 内部调用仍然是静默加载
    if (explicitRefresh) {
      toast.success('HChat 模型列表已刷新');
    }
  },

  async sendOpenaiChatMessage() {
    if (!store.openaiChatMessageInput.trim() || store.openaiChatLoading) return;

    const userContent = store.openaiChatMessageInput;
    store.openaiChatMessageInput = '';

    // 添加用户消息
    store.openaiChatMessages.push({
      role: 'user',
      content: userContent,
    });

    this.scrollToBottom();

    store.openaiChatLoading = true;

    try {
      const messages = [
        { role: 'system', content: store.openaiChatSystemPrompt },
        ...store.openaiChatMessages,
      ];

      // 显式指定完整路径，防止丢失前缀
      const response = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          model: store.openaiChatModel,
          messages: messages,
          stream: true,
          ...store.openaiChatSettings,
        }),
      });

      if (!response.ok) {
        let errorMessage = `HTTP 错误 ${response.status}`;
        try {
          const errData = await response.json();
          // 智能提取 OpenAI 格式或通用格式的错误消息
          errorMessage = errData.error?.message || errData.message || JSON.stringify(errData);
        } catch (e) {
          // 保持默认 HTTP 错误
        }
        throw new Error(errorMessage);
      }

      // 处理流式响应
      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      const assistantMsg = { role: 'assistant', content: '', reasoning: '' };
      store.openaiChatMessages.push(assistantMsg);

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        const chunk = decoder.decode(value, { stream: true });
        const lines = chunk.split('\n');

        for (const line of lines) {
          const trimmedLine = line.trim();
          if (trimmedLine.startsWith('data: ')) {
            const dataStr = trimmedLine.slice(6);
            if (dataStr === '[DONE]') break;

            try {
              const data = JSON.parse(dataStr);
              const delta = data.choices?.[0]?.delta;

              if (delta) {
                // 处理思考内容 (Reasoning / Thinking)
                if (delta.reasoning_content) {
                  assistantMsg.reasoning += delta.reasoning_content;
                }
                // 处理标准内容
                if (delta.content) {
                  assistantMsg.content += delta.content;
                }
                this.scrollToBottom();
              }
            } catch (e) {
              // 忽略解析错误
            }
          }
        }
      }
    } catch (error) {
      console.error('AI 对话失败:', error);

      // 核心修复：确保 error 是字符串，防止显示 [object Object]
      const displayError =
        error.message || (typeof error === 'string' ? error : JSON.stringify(error));

      this.showOpenaiToast('对话失败: ' + displayError, 'error');
      store.openaiChatMessages.push({
        role: 'assistant', // 改为 assistant 角色以保持 UI 一致
        content: '❌ **错误**: ' + displayError,
      });
    } finally {
      store.openaiChatLoading = false;
      this.scrollToBottom();
    }
  },

  clearOpenaiChat() {
    store.openaiChatMessages = [];
  },

  stopOpenaiChat() {
    store.openaiChatLoading = false;
    // 这里如果需要中断 Fetch，可以使用 AbortController，暂先简单重置状态
  },

  scrollToBottom() {
    setTimeout(() => {
      const el = document.getElementById('openai-chat-messages');
      if (el) {
        el.scrollTop = el.scrollHeight;
      }
      // 触发代码高亮
      document.querySelectorAll('pre code').forEach(block => {
        if (!block.dataset.highlighted) {
          hljs.highlightElement(block);
          block.dataset.highlighted = 'true';
        }
      });
    }, 50);
  },

  openAddOpenaiEndpointModal() {
    this.openaiEditingEndpoint = null;
    this.openaiEndpointForm = { name: '', baseUrl: '', apiKey: '', notes: '' };
    this.openaiEndpointFormError = '';
    this.showOpenaiEndpointModal = true;
  },

  editOpenaiEndpoint(endpoint) {
    this.openaiEditingEndpoint = endpoint;
    this.openaiEndpointForm = {
      name: endpoint.name || '',
      baseUrl: endpoint.baseUrl || '',
      apiKey: endpoint.apiKey || '',
      notes: endpoint.notes || '',
    };
    this.openaiEndpointFormError = '';
    this.showOpenaiEndpointModal = true;
  },

  async saveOpenaiEndpoint() {
    if (!this.openaiEndpointForm.baseUrl || !this.openaiEndpointForm.apiKey) {
      this.openaiEndpointFormError = '请填写 API 地址和 API Key';
      return;
    }

    this.openaiSaving = true;
    this.openaiEndpointFormError = '';

    try {
      const url = this.openaiEditingEndpoint
        ? `/api/openai/endpoints/${this.openaiEditingEndpoint.id}`
        : '/api/openai/endpoints';

      const response = await fetch(url, {
        method: this.openaiEditingEndpoint ? 'PUT' : 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(this.openaiEndpointForm),
      });

      const data = await response.json();
      if (response.ok && (data.success || data.endpoint || data.id)) {
        // 根据验证结果显示不同的提示
        if (this.openaiEditingEndpoint) {
          this.showOpenaiToast('端点已更新', 'success');
        } else if (data.verification && data.verification.valid) {
          const modelsCount = data.endpoint?.models?.length || 0;
          this.showOpenaiToast(`端点已添加，验证成功！找到 ${modelsCount} 个模型`, 'success');
        } else if (data.verification && !data.verification.valid) {
          this.showOpenaiToast('端点已添加，但 API 验证失败', 'error');
        } else {
          this.showOpenaiToast('端点已添加', 'success');
        }
        this.showOpenaiEndpointModal = false;
        await this.loadOpenaiEndpoints(); // 加载端点列表
        this.updateOpenaiAllModels(); // 立即更新 HChat 可用模型列表
      } else {
        this.openaiEndpointFormError = data.error || '保存失败';
      }
    } catch (error) {
      this.openaiEndpointFormError = '保存失败: ' + error.message;
    } finally {
      this.openaiSaving = false;
    }
  },

  async deleteOpenaiEndpoint(endpoint) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除端点 "${endpoint.name || endpoint.baseUrl}" 吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}`, {
        method: 'DELETE',
        headers: store.getAuthHeaders(),
      });

      const data = await response.json();
      if (response.ok && data.success) {
        this.showOpenaiToast('端点已删除', 'success');
        await this.loadOpenaiEndpoints();
      } else {
        this.showOpenaiToast('删除失败: ' + (data.error || '未知错误'), 'error');
      }
    } catch (error) {
      this.showOpenaiToast('删除失败: ' + error.message, 'error');
    }
  },

  async verifyOpenaiEndpoint(endpoint) {
    try {
      toast.info('正在验证...');
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });

      const data = await response.json();
      if (data.valid) {
        this.showOpenaiToast(`验证成功！找到 ${data.modelsCount || 0} 个模型`, 'success');
        await this.loadOpenaiEndpoints();
      } else {
        this.showOpenaiToast('验证失败: ' + (data.error || 'API Key 无效'), 'error');
      }
    } catch (error) {
      this.showOpenaiToast('验证失败: ' + error.message, 'error');
    }
  },

  async refreshEndpointModels(endpoint) {
    if (endpoint.refreshing) return;

    endpoint.refreshing = true;
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });

      const data = await response.json();
      if (data.valid) {
        this.showOpenaiToast(`${endpoint.name || '端点'} 模型列表已更新`, 'success');
        // 重新加载端点列表以获取新模型 (静默模式，不显示加载动画)
        await this.loadOpenaiEndpoints(true);
        // 如果是展开状态，确保它保持展开
      } else {
        this.showOpenaiToast('刷新失败: ' + (data.error || 'API Key 无效'), 'error');
      }
    } catch (error) {
      this.showOpenaiToast('刷新失败: ' + error.message, 'error');
    } finally {
      endpoint.refreshing = false;
    }
  },

  async toggleOpenaiEndpoint(endpoint) {
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/toggle`, {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ enabled: endpoint.enabled }),
      });

      const data = await response.json();
      if (data.success) {
        this.showOpenaiToast(endpoint.enabled ? '端点已启用' : '端点已禁用', 'success');
        // 刷新模型列表，因为禁用端点会影响可用模型
        this.updateOpenaiAllModels();
      } else {
        this.showOpenaiToast('操作失败: ' + (data.error || '未知错误'), 'error');
        // 恢复 UI 状态
        endpoint.enabled = !endpoint.enabled;
      }
    } catch (error) {
      this.showOpenaiToast('操作失败: ' + error.message, 'error');
      endpoint.enabled = !endpoint.enabled;
    }
  },

  async refreshAllOpenaiEndpoints() {
    store.openaiRefreshing = true;
    try {
      const response = await fetch('/api/openai/endpoints/refresh', {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });

      const data = await response.json();
      if (data.success) {
        const successCount = data.results?.filter(r => r.success).length || 0;
        this.showOpenaiToast(`刷新完成！已更新 ${successCount} 个启用端点`, 'success');
        await this.loadOpenaiEndpoints();
      } else {
        this.showOpenaiToast('刷新失败: ' + (data.error || '未知错误'), 'error');
      }
    } catch (error) {
      this.showOpenaiToast('刷新失败: ' + error.message, 'error');
    } finally {
      this.openaiRefreshing = false;
    }
  },

  async batchAddOpenaiEndpoints() {
    this.openaiBatchError = '';
    this.openaiBatchSuccess = '';

    if (!this.openaiBatchText.trim()) {
      this.openaiBatchError = '请输入端点信息';
      return;
    }

    this.openaiAdding = true;

    try {
      // 尝试解析为 JSON
      let endpoints = null;
      try {
        const parsed = JSON.parse(this.openaiBatchText);
        if (Array.isArray(parsed)) {
          endpoints = parsed;
        }
      } catch (e) {
        // 不是 JSON，使用文本格式
      }

      const response = await fetch('/api/openai/batch-add', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(endpoints ? { endpoints } : { text: this.openaiBatchText }),
      });

      const data = await response.json();
      if (data.success) {
        this.openaiBatchSuccess = `成功添加 ${data.added || 0} 个端点`;
        this.openaiBatchText = '';
        await this.loadOpenaiEndpoints();
        setTimeout(() => {
          this.openaiBatchSuccess = '';
        }, 3000);
      } else {
        this.openaiBatchError = data.error || '添加失败';
      }
    } catch (error) {
      this.openaiBatchError = '添加失败: ' + error.message;
    } finally {
      this.openaiAdding = false;
    }
  },

  toggleOpenaiModels(endpointId) {
    this.openaiExpandedEndpoints[endpointId] = !this.openaiExpandedEndpoints[endpointId];
  },

  isOpenaiEndpointExpanded(endpointId) {
    return !!this.openaiExpandedEndpoints[endpointId];
  },

  getModelName(model) {
    if (typeof model === 'string') {
      return model;
    }
    if (model && typeof model === 'object') {
      return model.id || model.name || 'unknown';
    }
    return 'unknown';
  },

  maskApiKey(apiKey) {
    if (!apiKey) return '';
    if (apiKey.length <= 8) return '****';
    return apiKey.substring(0, 4) + '****' + apiKey.substring(apiKey.length - 4);
  },

  async copyToClipboard(text) {
    try {
      await navigator.clipboard.writeText(text);
      this.showOpenaiToast('已复制到剪贴板', 'success');
    } catch (error) {
      // 降级方案
      const textarea = document.createElement('textarea');
      textarea.value = text;
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand('copy');
      document.body.removeChild(textarea);
      this.showOpenaiToast('已复制到剪贴板', 'success');
    }
  },

  // 导出所有端点
  async exportOpenaiEndpoints() {
    try {
      if (store.openaiEndpoints.length === 0) {
        toast.warning('没有可导出的端点');
        return;
      }

      const exportData = {
        version: '1.0',
        exportTime: new Date().toISOString(),
        endpoints: store.openaiEndpoints.map(ep => ({
          name: ep.name,
          baseUrl: ep.baseUrl,
          apiKey: ep.apiKey,
          notes: ep.notes,
        })),
      };

      const dataStr = JSON.stringify(exportData, null, 2);
      const dataBlob = new Blob([dataStr], { type: 'application/json' });
      const url = URL.createObjectURL(dataBlob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `openai-endpoints-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);

      this.showOpenaiToast('端点导出成功', 'success');
    } catch (error) {
      this.showOpenaiToast('导出失败: ' + error.message, 'error');
    }
  },

  // 从文件导入端点
  async importOpenaiEndpointsFromFile() {
    const confirmed = await store.showConfirm({
      title: '确认导入',
      message: '导入端点将添加到现有端点列表中，是否继续？',
      icon: 'fa-exclamation-triangle',
      confirmText: '确定导入',
      confirmClass: 'btn-primary',
    });

    if (!confirmed) return;

    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json';
    input.onchange = async event => {
      const file = event.target.files[0];
      if (!file) return;

      const reader = new FileReader();
      reader.onload = async e => {
        try {
          const importedData = JSON.parse(e.target.result);

          // 验证数据格式
          if (!importedData.version || !importedData.endpoints) {
            this.showOpenaiToast('无效的备份文件格式', 'error');
            return;
          }

          // 导入端点
          const response = await fetch('/api/openai/import', {
            method: 'POST',
            headers: store.getAuthHeaders(),
            body: JSON.stringify({ endpoints: importedData.endpoints }),
          });

          const data = await response.json();
          if (data.success) {
            let message = `成功导入 ${data.imported || 0} 个端点`;
            if (data.skipped > 0) {
              message += `，跳过 ${data.skipped} 个重复端点`;
            }
            this.showOpenaiToast(message, 'success');
            await this.loadOpenaiEndpoints();
          } else {
            this.showOpenaiToast('导入失败: ' + (data.error || '未知错误'), 'error');
          }
        } catch (error) {
          this.showOpenaiToast('导入失败: ' + error.message, 'error');
        }
      };
      reader.readAsText(file);
    };
    input.click();
  },
};
