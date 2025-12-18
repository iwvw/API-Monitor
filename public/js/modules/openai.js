/**
 * OpenAI API 模块
 * 负责 OpenAI API 相关功能
 */

import { store } from '../store.js';
import { toast } from './toast.js';

export const openaiMethods = {
  switchToOpenai() {
    store.mainActiveTab = 'openai';
    if (store.openaiEndpoints.length === 0) {
      this.loadOpenaiEndpoints();
    }
  },

  showOpenaiToast(message, type = 'success') {
    toast[type](message);
  },

  async loadOpenaiEndpoints() {
    store.openaiLoading = true;
    try {
      const response = await fetch('/api/openai/endpoints', {
        headers: store.getAuthHeaders()
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        // 为每个端点添加 showKey 属性
        store.openaiEndpoints = data.map(ep => ({ ...ep, showKey: false }));
      } else if (data.error) {
        console.error('加载 OpenAI 端点失败:', data.error);
      }
    } catch (error) {
      console.error('加载 OpenAI 端点失败:', error);
    } finally {
      store.openaiLoading = false;
    }
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
      notes: endpoint.notes || ''
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
        body: JSON.stringify(this.openaiEndpointForm)
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
        await this.loadOpenaiEndpoints();
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
      confirmClass: 'btn-danger'
    });

    if (!confirmed) return;

    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}`, {
        method: 'DELETE',
        headers: store.getAuthHeaders()
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
        headers: store.getAuthHeaders()
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

  async refreshAllOpenaiEndpoints() {
    store.openaiRefreshing = true;
    try {
      const response = await fetch('/api/openai/endpoints/refresh', {
        method: 'POST',
        headers: store.getAuthHeaders()
      });

      const data = await response.json();
      if (data.success) {
        this.showOpenaiToast(`刷新完成！成功: ${data.results?.filter(r => r.success).length || 0}`, 'success');
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
        body: JSON.stringify(endpoints ? { endpoints } : { text: this.openaiBatchText })
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
          notes: ep.notes
        }))
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
      confirmClass: 'btn-primary'
    });

    if (!confirmed) return;

    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json';
    input.onchange = async (event) => {
      const file = event.target.files[0];
      if (!file) return;

      const reader = new FileReader();
      reader.onload = async (e) => {
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
            body: JSON.stringify({ endpoints: importedData.endpoints })
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
  }
};
