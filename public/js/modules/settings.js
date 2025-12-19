/**
 * 设置管理模块
 * 负责系统设置、模块配置和数据导入导出
 */

export const settingsMethods = {
  // 从后端加载所有设置
  async loadUserSettings() {
    try {
      const response = await fetch('/api/settings', {
        headers: this.getAuthHeaders()
      });

      // 顺便加载数据库统计信息
      this.fetchDbStats();
      this.fetchLogSettings(); // 加载日志设置

      if (response.ok) {
        const result = await response.json();
        if (result.success && result.data) {
          const settings = result.data;

          // 应用自定义CSS
          if (settings.customCss) {
            this.customCss = settings.customCss;
            this.applyCustomCss();
          }

          // 应用 Zeabur 刷新间隔
          if (settings.zeaburRefreshInterval) {
            this.zeaburRefreshInterval = settings.zeaburRefreshInterval;
            // 如果 Zeabur 模块已激活，重启自动刷新
            if (this.mainActiveTab === 'zeabur' && !this.dataRefreshPaused) {
              this.startAutoRefresh();
            }
          }

          // 应用模块设置
          if (settings.moduleVisibility) {
            this.moduleVisibility = settings.moduleVisibility;
          }
          if (settings.moduleOrder) {
            this.moduleOrder = settings.moduleOrder;
          }
          if (settings.channelEnabled) {
            this.channelEnabled = settings.channelEnabled;
          }
          if (settings.channelModelPrefix) {
            this.channelModelPrefix = settings.channelModelPrefix;
          }
          if (settings.load_balancing_strategy) {
            this.agSettingsForm.load_balancing_strategy = settings.load_balancing_strategy;
          }
          if (settings.serverIpDisplayMode) {
            this.serverIpDisplayMode = settings.serverIpDisplayMode;
          }

          this.activateFirstVisibleModule();
          return true;
        }
      }

      // 如果后端没有设置，尝试从localStorage加载（向后兼容）
      this.loadCustomCssFromLocal();
      this.loadModuleSettingsFromLocal();
      return false;
    } catch (error) {
      console.error('加载用户设置失败:', error);
      // 降级到localStorage
      this.loadCustomCssFromLocal();
      this.loadModuleSettingsFromLocal();
      return false;
    }
  },

  // 从localStorage加载自定义CSS（向后兼容）
  loadCustomCssFromLocal() {
    const savedCss = localStorage.getItem('custom_css');
    if (savedCss) {
      this.customCss = savedCss;
      this.applyCustomCss();
    }
  },

  // 从localStorage加载模块设置(向后兼容)
  loadModuleSettingsFromLocal() {
    const savedVisibility = localStorage.getItem('module_visibility');
    const savedOrder = localStorage.getItem('module_order');

    const availableModules = ['openai', 'antigravity', 'gemini-cli', 'zeabur', 'dns', 'server'];

    if (savedVisibility) {
      const saved = JSON.parse(savedVisibility);
      availableModules.forEach(module => {
        if (!(module in saved)) {
          saved[module] = true;
        }
      });
      this.moduleVisibility = saved;
    }

    // 简单的 channelEnabled 向后兼容 (默认为 true)
    if (!this.channelEnabled) {
      this.channelEnabled = { antigravity: true, 'gemini-cli': true };
    }
    // channelModelPrefix 向后兼容
    if (!this.channelModelPrefix) {
      this.channelModelPrefix = { antigravity: '', 'gemini-cli': '' };
    }

    if (savedOrder) {
      const saved = JSON.parse(savedOrder);
      availableModules.forEach(module => {
        if (!saved.includes(module)) {
          saved.push(module);
        }
      });
      this.moduleOrder = saved.filter(m => availableModules.includes(m));
    }
    this.activateFirstVisibleModule();
  },

  // 激活第一个可见的模块
  activateFirstVisibleModule() {
    // 如果当前选中的模块不可见，或者我们想默认选中第一个
    // 这里简单的策略是：总是尝试切换到排序后的第一个可见模块
    // 这样用户登录时就会看到他们配置的第一个模块
    if (this.moduleOrder && this.moduleOrder.length > 0) {
      const firstVisible = this.moduleOrder.find(m => this.moduleVisibility[m]);
      if (firstVisible) {
        this.mainActiveTab = firstVisible;
      }
    }
  },

  // 保存所有设置到后端
  async saveUserSettingsToServer() {
    try {
      const settings = {
        customCss: this.customCss,
        zeaburRefreshInterval: this.zeaburRefreshInterval,
        moduleVisibility: this.moduleVisibility,
        channelEnabled: this.channelEnabled,
        channelModelPrefix: this.channelModelPrefix,
        moduleOrder: this.moduleOrder,
        load_balancing_strategy: this.agSettingsForm.load_balancing_strategy,
        serverIpDisplayMode: this.serverIpDisplayMode
      };

      const response = await fetch('/api/settings', {
        method: 'POST',
        headers: this.getAuthHeaders(),
        body: JSON.stringify(settings)
      });

      if (response.ok) {
        const result = await response.json();
        return result.success;
      }
      return false;
    } catch (error) {
      console.error('保存用户设置失败:', error);
      return false;
    }
  },

  // 应用自定义 CSS
  applyCustomCss() {
    const styleElement = document.getElementById('custom-css');
    if (styleElement) {
      styleElement.textContent = this.customCss;
    }
  },

  // 保存自定义 CSS
  async saveCustomCss() {
    try {
      // 先保存到localStorage（向后兼容）
      localStorage.setItem('custom_css', this.customCss);
      this.applyCustomCss();

      // 保存到后端
      const success = await this.saveUserSettingsToServer();

      if (success) {
        this.customCssSuccess = '自定义 CSS 已保存到主机';
      } else {
        this.customCssSuccess = '自定义 CSS 已保存到本地';
      }
      this.customCssError = '';

      setTimeout(() => {
        this.customCssSuccess = '';
      }, 3000);
    } catch (error) {
      this.customCssError = '保存失败: ' + error.message;
      this.customCssSuccess = '';
    }
  },

  // 重置自定义 CSS
  async resetCustomCss() {
    this.customCss = '';
    localStorage.removeItem('custom_css');
    this.applyCustomCss();

    // 保存到后端
    await this.saveUserSettingsToServer();

    this.customCssSuccess = '自定义 CSS 已重置';
    this.customCssError = '';
    setTimeout(() => {
      this.customCssSuccess = '';
    }, 3000);
  },

  // 加载模块设置（已废弃，使用 loadUserSettings 代替）
  async loadModuleSettings() {
    // 从后端加载所有设置
    await this.loadUserSettings();

    // 定义所有可用模块
    const availableModules = ['zeabur', 'dns', 'openai', 'server', 'antigravity', 'gemini-cli'];

    // 确保所有模块都有配置
    availableModules.forEach(module => {
      if (!(module in this.moduleVisibility)) {
        this.moduleVisibility[module] = true;
      }
    });

    // 确保模块顺序包含所有模块
    availableModules.forEach(module => {
      if (!this.moduleOrder.includes(module)) {
        this.moduleOrder.push(module);
      }
    });

    // 确保至少有一个模块可见，并切换到第一个可见模块
    const hasVisibleModule = Object.values(this.moduleVisibility).some(v => v);
    if (!hasVisibleModule) {
      this.moduleVisibility[this.moduleOrder[0]] = true;
    }

    // 切换到第一个可见的模块
    const firstVisibleModule = this.moduleOrder.find(m => this.moduleVisibility[m]);
    if (firstVisibleModule) {
      this.mainActiveTab = firstVisibleModule;
    }

    // 保存更新后的设置
    await this.saveModuleSettings();
  },

  // 保存模块设置
  async saveModuleSettings() {
    // 保存到localStorage（向后兼容）
    localStorage.setItem('module_visibility', JSON.stringify(this.moduleVisibility));
    localStorage.setItem('module_order', JSON.stringify(this.moduleOrder));
    // channelEnabled 通常不需要存 localStorage，因为主要依赖后端配置，但为了保持一致也可以存
    localStorage.setItem('channel_enabled', JSON.stringify(this.channelEnabled));

    // 保存到后端
    await this.saveUserSettingsToServer();
  },

  // 切换模块可见性
  toggleModuleVisibility(module) {
    this.moduleVisibility[module] = !this.moduleVisibility[module];

    // 确保至少有一个模块可见
    const hasVisibleModule = Object.values(this.moduleVisibility).some(v => v);
    if (!hasVisibleModule) {
      this.moduleVisibility[module] = true;
      this.showGlobalToast('至少需要显示一个模块', 'warning');
      return;
    }

    // 如果隐藏的是当前模块，切换到第一个可见模块
    if (!this.moduleVisibility[module] && this.mainActiveTab === module) {
      const firstVisibleModule = this.moduleOrder.find(m => this.moduleVisibility[m]);
      if (firstVisibleModule) {
        this.mainActiveTab = firstVisibleModule;
      }
    }

    this.saveModuleSettings();
    this.showGlobalToast(`${this.getModuleName(module)} 模块已${this.moduleVisibility[module] ? '显示' : '隐藏'}`, 'success');
  },

  // 切换渠道启用状态 (不影响 UI 可见性)
  toggleChannelEnabled(channel) {
    if (!this.channelEnabled) this.channelEnabled = {};
    this.channelEnabled[channel] = !this.channelEnabled[channel];

    this.saveModuleSettings(); // 复用保存逻辑
    this.showGlobalToast(`${this.getModuleName(channel)} 渠道已${this.channelEnabled[channel] ? '启用' : '禁用'}`, 'success');
  },

  // 获取模块名称
  getModuleName(module) {
    const names = {
      zeabur: 'Zeabur',
      dns: 'CF DNS',
      openai: 'OpenAPI',
      server: 'Hosts',
      antigravity: 'Antigravity',
      'gemini-cli': 'GCLI'
    };
    return names[module] || module;
  },

  // 获取模块图标
  getModuleIcon(module) {
    const icons = {
      zeabur: 'fa-rocket',
      dns: 'fa-cloud',
      openai: 'fa-robot',
      server: 'fa-server',
      antigravity: 'fa-atom',
      'gemini-cli': 'fa-terminal'
    };
    return icons[module] || 'fa-cube';
  },

  // 拖拽开始
  handleDragStart(event, index) {
    this.draggedIndex = index;
    event.target.classList.add('dragging');
    // 防止选中文本
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/html', event.target.innerHTML);
  },

  // 拖拽结束
  handleDragEnd(event) {
    event.target.classList.remove('dragging');
    // 移除所有拖拽over样式
    document.querySelectorAll('.draggable-module-item').forEach(item => {
      item.classList.remove('drag-over');
    });
  },

  // 拖拽经过
  handleDragOver(event, index) {
    event.preventDefault(); // 允许放下
    if (this.draggedIndex === null || this.draggedIndex === index) return;

    // 获取拖拽項和目标项
    const list = this.moduleOrder;
    const item = list[this.draggedIndex];

    // 实时交换数组中的位置
    list.splice(this.draggedIndex, 1);
    list.splice(index, 0, item);

    // 更新被拖拽项的索引
    this.draggedIndex = index;

    // 无需手动添加 drag-over 样式，因为列表已经实时更新重绘
  },

  // 拖拽放下
  async handleDrop(event, dropIndex) {
    event.preventDefault();
    this.draggedIndex = null;

    // 移除所有可能的拖拽样式
    document.querySelectorAll('.draggable-module-item').forEach(item => {
      item.classList.remove('dragging');
    });

    // 自动保存设置（当前 moduleOrder 已经是最新顺序）
    await this.saveModuleSettings();
  },

  // 保存设置
  async saveSettings() {
    await this.saveModuleSettings();
    this.showGlobalToast('设置已保存到主机', 'success');
    this.showSettingsModal = false;
  },

  // 保存 Zeabur 设置
  async saveZeaburSettings() {
    // 确保也保存到 localStorage，保持一致性
    localStorage.setItem('zeabur_refresh_interval', this.zeaburRefreshInterval);

    const success = await this.saveUserSettingsToServer();
    if (success) {
      this.showGlobalToast('Zeabur 模块配置已保存', 'success');
    } else {
      this.showGlobalToast('保存失败', 'error');
    }
  },

  // 导出全部数据（数据库文件）
  async exportAllData() {
    try {
      this.showGlobalToast('正在导出数据库...', 'info');

      // 使用 fetch 下载，支持认证头
      const response = await fetch('/api/settings/export-database', {
        method: 'GET',
        headers: this.getAuthHeaders()
      });

      if (!response.ok) {
        throw new Error('导出失败: ' + response.statusText);
      }

      // 获取文件 blob
      const blob = await response.blob();

      // 创建下载链接
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `api-monitor-backup-${new Date().toISOString().slice(0, 19).replace(/:/g, '-')}.db`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);

      this.showGlobalToast('数据库导出成功！文件包含所有数据', 'success');
    } catch (error) {
      this.showGlobalToast('导出失败: ' + error.message, 'error');
    }
  },

  // 导入全部数据（数据库文件）
  async importAllData() {
    const confirmed = await this.showConfirm({
      title: '确认导入数据库',
      message: '导入数据库将完全覆盖当前所有数据，原数据库会自动备份。是否继续？',
      icon: 'fa-exclamation-triangle',
      confirmText: '确定导入',
      confirmClass: 'btn-warning'
    });

    if (!confirmed) return;

    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.db';
    input.onchange = async (event) => {
      const file = event.target.files[0];
      if (!file) return;

      // 验证文件类型
      if (!file.name.endsWith('.db')) {
        this.showGlobalToast('请选择 .db 数据库文件', 'error');
        return;
      }

      try {
        this.showGlobalToast('正在导入数据库，请稍候...', 'info');

        // 创建 FormData
        const formData = new FormData();
        formData.append('database', file);

        // 上传数据库文件
        const response = await fetch('/api/settings/import-database', {
          method: 'POST',
          headers: {
            'X-Session-ID': localStorage.getItem('session_id')
          },
          body: formData
        });

        const result = await response.json();

        if (result.success) {
          this.showGlobalToast('数据库导入成功！页面将在3秒后刷新', 'success');

          // 3秒后刷新页面以加载新数据
          setTimeout(() => {
            window.location.reload();
          }, 3000);
        } else {
          this.showGlobalToast('导入失败: ' + result.error, 'error');
        }
      } catch (error) {
        this.showGlobalToast('导入失败: ' + error.message, 'error');
      }
    };

    input.click();
  },

  // 旧版JSON导入（保留用于兼容）
  async importAllDataLegacy() {
    const confirmed = await this.showConfirm({
      title: '确认导入',
      message: '导入数据将覆盖当前所有配置，是否继续？',
      icon: 'fa-exclamation-triangle',
      confirmText: '确定导入',
      confirmClass: 'btn-warning'
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
          if (!importedData.version) {
            this.showGlobalToast('无效的备份文件格式', 'error');
            return;
          }

          // 导入 Zeabur 数据
          if (importedData.zeabur) {
            if (importedData.zeabur.accounts) {
              this.managedAccounts = importedData.zeabur.accounts;
              await this.saveManagedAccounts();
            }
            if (importedData.zeabur.projectCosts) {
              this.projectCosts = importedData.zeabur.projectCosts;
              localStorage.setItem('zeabur_project_costs', JSON.stringify(this.projectCosts));
            }
          }

          // 导入 DNS 数据
          if (importedData.dns) {
            if (importedData.dns.accounts) {
              // 通过API导入DNS账号
              for (const account of importedData.dns.accounts) {
                await fetch('/api/cf-dns/accounts', {
                  method: 'POST',
                  headers: this.getAuthHeaders(),
                  body: JSON.stringify(account)
                });
              }
              await this.loadDnsAccounts();
            }
            if (importedData.dns.templates) {
              // 通过API导入DNS模板
              for (const template of importedData.dns.templates) {
                await fetch('/api/cf-dns/templates', {
                  method: 'POST',
                  headers: this.getAuthHeaders(),
                  body: JSON.stringify(template)
                });
              }
              await this.loadDnsTemplates();
            }
          }

          // 导入 OpenAI 数据
          if (importedData.openai && importedData.openai.endpoints) {
            await fetch('/api/openai/import', {
              method: 'POST',
              headers: this.getAuthHeaders(),
              body: JSON.stringify({ endpoints: importedData.openai.endpoints })
            });
            await this.loadOpenaiEndpoints();
          }

          // 导入设置
          if (importedData.settings) {
            if (importedData.settings.moduleVisibility) {
              this.moduleVisibility = importedData.settings.moduleVisibility;
            }
            if (importedData.settings.moduleOrder) {
              this.moduleOrder = importedData.settings.moduleOrder;
            }
            if (importedData.settings.channelEnabled) {
              this.channelEnabled = importedData.settings.channelEnabled;
            }
            this.saveModuleSettings();
          }

          this.showGlobalToast('数据导入成功', 'success');
          await this.fetchData();
        } catch (error) {
          this.showGlobalToast('导入失败: ' + error.message, 'error');
        }
      };
      reader.readAsText(file);
    };
    input.click();
  },

  // 获取数据库统计
  async fetchDbStats() {
    try {
      const response = await fetch('/api/settings/database-stats', {
        headers: this.getAuthHeaders()
      });
      if (response.ok) {
        const result = await response.json();
        if (result.success) {
          this.dbStats = result.data;
        }
      }
    } catch (error) {
      console.error('获取数据库统计失败:', error);
    }
  },

  // 压缩数据库
  async handleVacuumDb() {
    try {
      this.showGlobalToast('正在压缩数据库...', 'info');
      const response = await fetch('/api/settings/vacuum-database', {
        method: 'POST',
        headers: this.getAuthHeaders()
      });
      const result = await response.json();

      if (result.success) {
        this.showGlobalToast('数据库压缩成功', 'success');
        await this.fetchDbStats(); // 刷新统计
      } else {
        this.showGlobalToast('操作失败: ' + result.error, 'error');
      }
    } catch (error) {
      this.showGlobalToast('请求失败: ' + error.message, 'error');
    }
  },

  // 清理日志
  async handleClearLogs() {
    const confirmed = await this.showConfirm({
      title: '确认清理日志',
      message: '确定要清空所有操作日志吗？此操作不可恢复。',
      icon: 'fa-trash-alt',
      confirmText: '确定清理',
      confirmClass: 'btn-danger'
    });

    if (!confirmed) return;

    try {
      this.showGlobalToast('正在清理日志...', 'info');
      const response = await fetch('/api/settings/clear-logs', {
        method: 'POST',
        headers: this.getAuthHeaders()
      });
      const result = await response.json();

      if (result.success) {
        this.showGlobalToast(result.message, 'success');
        await this.fetchDbStats(); // 刷新统计
      } else {
        this.showGlobalToast('操作失败: ' + result.error, 'error');
      }
    } catch (error) {
      this.showGlobalToast('请求失败: ' + error.message, 'error');
    }
  },

  // 获取日志保留设置
  async fetchLogSettings() {
    try {
      const response = await fetch('/api/settings/log-settings', {
        headers: this.getAuthHeaders()
      });
      if (response.ok) {
        const result = await response.json();
        if (result.success) {
          this.logSettings = { ...result.data };
        }
      }
    } catch (error) {
      console.error('获取日志设置失败:', error);
    }
  },

  // 保存日志保留设置
  async saveLogSettings() {
    try {
      this.logSettingsSaving = true;
      const response = await fetch('/api/settings/log-settings', {
        method: 'POST',
        headers: this.getAuthHeaders(),
        body: JSON.stringify(this.logSettings)
      });
      const result = await response.json();

      if (result.success) {
        this.showGlobalToast('日志保留策略已保存', 'success');
      } else {
        this.showGlobalToast('保存失败: ' + result.error, 'error');
      }
    } catch (error) {
      this.showGlobalToast('请求失败: ' + error.message, 'error');
    } finally {
      this.logSettingsSaving = false;
    }
  },

  // 立即执行日志清理
  async enforceLogLimits() {
    try {
      this.logLimitsEnforcing = true;
      this.showGlobalToast('正在执行日志清理策略...', 'info');

      // 使用当前输入的值执行清理
      const response = await fetch('/api/settings/enforce-log-limits', {
        method: 'POST',
        headers: this.getAuthHeaders(),
        body: JSON.stringify(this.logSettings)
      });

      const result = await response.json();

      if (result.success) {
        this.showGlobalToast(result.message, 'success');
        await this.fetchDbStats(); // 刷新统计
      } else {
        this.showGlobalToast('操作失败: ' + result.error, 'error');
      }
    } catch (error) {
      this.showGlobalToast('请求失败: ' + error.message, 'error');
    } finally {
      this.logLimitsEnforcing = false;
    }
  },

  // 获取系统审计日志
  async fetchSystemLogs() {
    try {
      this.systemLogsLoading = true;
      const response = await fetch('/api/settings/operation-logs', {
        headers: this.getAuthHeaders()
      });
      const result = await response.json();
      if (result.success) {
        this.systemLogs = result.data;
      }
    } catch (error) {
      console.error('获取审计日志失败:', error);
    } finally {
      this.systemLogsLoading = false;
    }
  },

  // 翻译操作类型
  translateOpType(type) {
    const types = {
      create: '新增',
      update: '修改',
      delete: '删除',
      login: '登录',
      logout: '登出',
      system: '系统'
    };
    return types[type] || type;
  },

  // 查看日志详情
  viewLogDetail(log) {
    let detailStr = '';
    try {
      const details = typeof log.details === 'string' ? JSON.parse(log.details) : log.details;
      detailStr = JSON.stringify(details, null, 2);
    } catch (e) {
      detailStr = log.details;
    }

    this.showInfo({
      title: '日志详情 - ' + this.translateOpType(log.operation_type),
      message: `<pre style="background: var(--bg-tertiary); padding: 10px; border-radius: 6px; font-size: 12px; max-height: 400px; overflow-y: auto;">${detailStr}</pre>`,
      icon: 'fa-info-circle'
    });
  },

  // 切换日志流
  toggleLogStream() {
    if (this.logStreamEnabled) {
      this.startLogStream();
    } else {
      this.stopLogStream();
    }
  },

  // 开始日志流
  startLogStream() {
    if (this.logStreamInterval) return;

    // 初始加载
    this.fetchRecentLogMessages();

    // 定时轮询 (如果没有 WebSocket)
    this.logStreamInterval = setInterval(() => {
      this.fetchRecentLogMessages();
    }, 3000);
  },

  // 停止日志流
  stopLogStream() {
    if (this.logStreamInterval) {
      clearInterval(this.logStreamInterval);
      this.logStreamInterval = null;
    }
  },

  // 获取最近的系统运行日志
  async fetchRecentLogMessages() {
    try {
      const response = await fetch('/api/settings/sys-logs', {
        headers: this.getAuthHeaders()
      });
      const result = await response.json();
      if (result.success && result.data) {
        // 简单的追加去重逻辑 (实际应用中可能需要更复杂的 traceId 或 id 检查)
        const currentMessages = [...this.systemLogMessages];
        result.data.forEach(newMsg => {
          const exists = currentMessages.some(m => m.time === newMsg.time && m.message === newMsg.message);
          if (!exists) {
            currentMessages.push(newMsg);
          }
        });

        // 保持最后 100 条
        this.systemLogMessages = currentMessages.slice(-100);

        // 自动滚动到底部
        this.$nextTick(() => {
          const container = document.querySelector('.log-stream-container');
          if (container) {
            container.scrollTop = container.scrollHeight;
          }
        });
      }
    } catch (error) {
      console.error('获取实时日志失败:', error);
    }
  },

  // 保存全局 API 设置 (统一保存到 Antigravity 和 Gemini CLI)
  async saveGlobalApiSettings() {
    this.antigravitySaving = true;
    try {
      const apiKey = this.agSettingsForm.API_KEY;
      const proxy = this.agSettingsForm.PROXY;

      // 1. 保存到 Antigravity
      const agUpdates = [
        { key: 'API_KEY', value: apiKey },
        { key: 'PROXY', value: proxy }
      ];

      for (const update of agUpdates) {
        await fetch('/api/antigravity/settings', {
          method: 'POST',
          headers: {
            ...this.getAuthHeaders(),
            'Content-Type': 'application/json'
          },
          body: JSON.stringify(update)
        });
      }

      // 2. 保存到 Gemini CLI
      const gcliUpdates = {
        API_KEY: apiKey,
        PROXY: proxy
      };

      // 同步到本地表单对象
      if (this.geminiCliSettingsForm) {
        this.geminiCliSettingsForm.API_KEY = apiKey;
        this.geminiCliSettingsForm.PROXY = proxy;
      }

      await fetch('/api/gemini-cli-api/settings', {
        method: 'POST',
        headers: {
          ...this.getAuthHeaders(),
          'Content-Type': 'application/json'
        },
        body: JSON.stringify(gcliUpdates)
      });

      // 3. 保存 User Settings (Load Balancing Strategy)
      await this.saveUserSettingsToServer();

      this.showGlobalToast('全局 API 及网络配置已同步应用', 'success');
    } catch (error) {
      console.error('保存全局设置失败:', error);
      this.showGlobalToast('保存失败: ' + error.message, 'error');
    } finally {
      this.antigravitySaving = false;
    }
  }
};
