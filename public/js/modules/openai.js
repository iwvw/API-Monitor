/**
 * OpenAI API 模块
 * 负责 OpenAI API 相关功能
 */

export const openaiMethods = {
  switchToOpenai() {
          this.mainActiveTab = 'openai';
          if (this.openaiEndpoints.length === 0) {
            this.loadOpenaiEndpoints();
          }
        },

  showOpenaiToast(message, type = 'success') {
          this.openaiToast = { show: true, message, type };
          setTimeout(() => {
            this.openaiToast.show = false;
          }, 3000);
        },

  async loadOpenaiEndpoints() {
          this.openaiLoading = true;
          try {
            const response = await fetch('/api/openai/endpoints', {
              headers: this.getAuthHeaders()
            });
            const data = await response.json();
            if (Array.isArray(data)) {
              // 为每个端点添加 showKey 属性
              this.openaiEndpoints = data.map(ep => ({ ...ep, showKey: false }));
            } else if (data.error) {
              console.error('加载 OpenAI 端点失败:', data.error);
            }
          } catch (error) {
            console.error('加载 OpenAI 端点失败:', error);
          } finally {
            this.openaiLoading = false;
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
              headers: this.getAuthHeaders(),
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
          if (!confirm(`确定要删除端点 "${endpoint.name || endpoint.baseUrl}" 吗？`)) return;

          try {
            const response = await fetch(`/api/openai/endpoints/${endpoint.id}`, {
              method: 'DELETE',
              headers: this.getAuthHeaders()
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
            this.showOpenaiToast('正在验证...', 'success');
            const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
              method: 'POST',
              headers: this.getAuthHeaders()
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
          this.openaiRefreshing = true;
          try {
            const response = await fetch('/api/openai/endpoints/refresh', {
              method: 'POST',
              headers: this.getAuthHeaders()
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
            const response = await fetch('/api/openai/import', {
              method: 'POST',
              headers: this.getAuthHeaders(),
              body: JSON.stringify({ text: this.openaiBatchText })
            });

            const data = await response.json();
            if (data.success) {
              this.openaiBatchSuccess = `成功添加 ${data.imported || 0} 个端点`;
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
        }
};
