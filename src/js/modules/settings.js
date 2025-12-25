/**
 * 设置管理模块
 * 负责系统设置、模块配置和数据导入导出
 */

import { store, MODULE_CONFIG } from '../store.js';

export const settingsMethods = {
  // 从后端加载所有设置
  async loadUserSettings() {
    try {
      const response = await fetch('/api/settings', {
        headers: store.getAuthHeaders()
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
            // 如果 PaaS 模块已激活且当前平台正是 Zeabur，重启自动刷新
            if (this.mainActiveTab === 'paas' && this.paasCurrentPlatform === 'zeabur' && !this.dataRefreshPaused) {
              this.startAutoRefresh();
            }
          }

          // 应用模块设置 (过滤掉已废弃的模块)
          const validModules = ['openai', 'antigravity', 'gemini-cli', 'paas', 'dns', 'self-h', 'server', 'totp'];
          if (settings.moduleVisibility) {
            // 过滤掉不再支持的模块
            const filtered = {};
            validModules.forEach(m => {
              filtered[m] = settings.moduleVisibility[m] !== false;
            });
            this.moduleVisibility = filtered;
          }
          if (settings.moduleOrder) {
            // 过滤掉不再支持的模块
            this.moduleOrder = settings.moduleOrder.filter(m => validModules.includes(m));
            // 确保所有有效模块都在列表中
            validModules.forEach(m => {
              if (!this.moduleOrder.includes(m)) {
                this.moduleOrder.push(m);
              }
            });
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
          if (settings.vibrationEnabled !== undefined) {
            this.vibrationEnabled = settings.vibrationEnabled;
          }
          if (settings.totpSettings) {
            Object.assign(store.totpSettings, settings.totpSettings);
          }
          if (settings.navLayout) {
            // 如果之前是侧边栏，或者没有设置，现在统一切换到底栏风格
            store.navLayout = (settings.navLayout === 'sidebar' || !settings.navLayout) ? 'bottom' : settings.navLayout;
          }
          if (settings.agentDownloadUrl !== undefined) {
            store.agentDownloadUrl = settings.agentDownloadUrl;
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

    const availableModules = ['openai', 'antigravity', 'gemini-cli', 'paas', 'dns', 'self-h', 'server', 'totp'];

    if (savedVisibility) {
      const saved = JSON.parse(savedVisibility);
      // 只保留有效模块，过滤掉已废弃的模块
      const filtered = {};
      availableModules.forEach(module => {
        filtered[module] = saved[module] !== false;
      });
      this.moduleVisibility = filtered;
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
    this.antigravitySaving = true; // 复用该变量控制全局 Loading
    try {
      const settings = {
        customCss: this.customCss,
        zeaburRefreshInterval: this.zeaburRefreshInterval,
        moduleVisibility: this.moduleVisibility,
        channelEnabled: this.channelEnabled,
        channelModelPrefix: this.channelModelPrefix,
        moduleOrder: this.moduleOrder,
        load_balancing_strategy: this.agSettingsForm.load_balancing_strategy,
        serverIpDisplayMode: this.serverIpDisplayMode,
        vibrationEnabled: this.vibrationEnabled,
        navLayout: store.navLayout,
        totpSettings: store.totpSettings,
        agentDownloadUrl: store.agentDownloadUrl
      };

      const response = await fetch('/api/settings', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(settings)
      });

      if (response.ok) {
        const result = await response.json();
        if (result.success) {
          this.showGlobalToast('系统设置已应用并保存', 'success');
          // 可选：如果布局发生变化，可以强制刷新下某些视图
          return true;
        }
      }
      this.showGlobalToast('设置保存失败，请检查连接', 'error');
      return false;
    } catch (error) {
      console.error('保存用户设置失败:', error);
      this.showGlobalToast('保存出错: ' + error.message, 'error');
      return false;
    } finally {
      this.antigravitySaving = false;
    }
  },

  // 统一保存全局 API 设置 (Antigravity/通道等)
  async saveGlobalApiSettings() {
    try {
      // 1. 保存 Antigravity 模块内部设置 (API_KEY, PROXY 等)
      await this.saveAllAgSettings();
      // 2. 保存全局用户设置 (负载均衡策略, 渠道开关, 前缀等)
      await this.saveUserSettingsToServer();
    } catch (e) {
      console.error('保存全局 API 设置失败:', e);
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
    const availableModules = ['paas', 'dns', 'openai', 'server', 'antigravity', 'gemini-cli', 'self-h', 'totp'];

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
  },

  // 一键设置所有模块可见性
  setAllModulesVisibility(visible) {
    this.moduleOrder.forEach((module, index) => {
      // 如果是隐藏全部，保留排序中的第一个模块可见以防界面空白
      if (!visible && index === 0) {
        this.moduleVisibility[module] = true;
      } else {
        this.moduleVisibility[module] = visible;
      }
    });

    // 如果隐藏的是当前模块且当前模块现在不可见，切换到第一个可见模块
    if (!this.moduleVisibility[this.mainActiveTab]) {
      const firstVisible = this.moduleOrder.find(m => this.moduleVisibility[m]);
      if (firstVisible) {
        this.mainActiveTab = firstVisible;
      }
    }

    this.saveModuleSettings();
  },

  // 切换渠道启用状态 (不影响 UI 可见性)
  toggleChannelEnabled(channel) {
    if (!this.channelEnabled) this.channelEnabled = {};
    this.channelEnabled[channel] = !this.channelEnabled[channel];

    this.saveModuleSettings(); // 复用保存逻辑
  },

  // 获取模块名称 (使用统一配置，设置页面使用简短名称)
  getModuleName(module) {
    const config = MODULE_CONFIG[module];
    return config ? config.shortName : module;
  },

  // 获取模块图标 (使用统一配置)
  getModuleIcon(module) {
    const config = MODULE_CONFIG[module];
    return config ? config.icon : 'fa-cube';
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

  // ========== 触摸拖拽支持 ==========
  // 触摸开始
  handleTouchStart(event, index) {
    this.touchStartIndex = index;
    this.touchStartY = event.touches[0].clientY;
    this.touchCurrentY = this.touchStartY;

    // 添加拖拽样式
    const target = event.currentTarget;
    target.classList.add('dragging');
  },

  // 触摸移动
  handleTouchMove(event, index) {
    if (this.touchStartIndex === null || this.touchStartIndex === undefined) return;

    this.touchCurrentY = event.touches[0].clientY;

    // 计算移动的 Y 方向距离
    const deltaY = this.touchCurrentY - this.touchStartY;

    // 如果移动距离过小，认为是点击而不是拖拽
    if (Math.abs(deltaY) < 10) return;

    // 确定是拖拽，阻止默认滚动行为
    event.preventDefault();

    // 基于移动距离计算目标位置
    const items = document.querySelectorAll('.draggable-module-item');
    const itemHeight = items[0]?.offsetHeight || 50;
    const moveSteps = Math.round(deltaY / (itemHeight + 8)); // 8 是 gap

    let targetIndex = this.touchStartIndex + moveSteps;
    targetIndex = Math.max(0, Math.min(targetIndex, this.moduleOrder.length - 1));

    // 如果目标位置变化了，交换顺序
    if (targetIndex !== this.touchStartIndex) {
      const list = this.moduleOrder;
      const item = list[this.touchStartIndex];
      list.splice(this.touchStartIndex, 1);
      list.splice(targetIndex, 0, item);
      // 更新开始位置
      this.touchStartIndex = targetIndex;
      this.touchStartY = this.touchCurrentY;
    }
  },

  // 触摸结束
  async handleTouchEnd(event, index) {
    // 移除拖拽样式
    document.querySelectorAll('.draggable-module-item').forEach(item => {
      item.classList.remove('dragging');
    });

    // 重置状态
    this.touchStartIndex = null;
    this.touchStartY = null;
    this.touchCurrentY = null;

    // 静默保存设置到后端
    this.saveModuleSettings();
  },

  // 保存设置
  async saveSettings() {
    await this.saveModuleSettings();
    this.showGlobalToast('排序与可见性已保存', 'success', 3000, true);
  },

  // 处理设置面板的 ESC 键
  handleSettingsEsc(event) {
    // 如果日志查看器正在显示，不关闭设置面板
    if (this.logViewer && this.logViewer.visible) {
      return;
    }
    // 如果有其他模态框打开，不关闭设置面板
    if (this.customDialog && this.customDialog.show) {
      return;
    }
    // 关闭设置面板
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
    // 1. 创建隐藏的文件输入框
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.db';
    input.style.position = 'absolute';
    input.style.opacity = '0';
    input.style.pointerEvents = 'none';
    document.body.appendChild(input);

    // 2. 监听文件选择
    input.onchange = async (event) => {
      this.showGlobalToast('已选择文件，正在准备...', 'info');
      const file = event.target.files[0];

      // 触发后立即清理 DOM
      if (document.body.contains(input)) {
        document.body.removeChild(input);
      }

      if (!file) {
        this.showGlobalToast('未选择文件', 'warning');
        return;
      }

      // 验证文件类型
      if (!file.name.endsWith('.db')) {
        this.showGlobalToast('文件格式错误，请选择 .db 文件', 'error');
        return;
      }

      this.showGlobalToast('请在弹出的对话框中确认', 'info');

      // 3. 用户二次确认
      // 注意：这里由于对话框组件限制，message 只能传字符串，<br> 会被转义，先改回纯文本
      const confirmed = await this.showConfirm({
        title: '确认导入数据库',
        message: `确定要导入文件 ${file.name} 吗？这会覆盖当前所有数据并自动备份。`,
        icon: 'fa-exclamation-triangle',
        confirmText: '开始导入',
        confirmClass: 'btn-warning'
      });

      if (!confirmed) {
        this.showGlobalToast('已取消导入', 'info');
        return;
      }

      try {
        this.showGlobalToast('正在上传并恢复数据库，请勿关闭页面...', 'info');

        // 4. 构建上传数据
        const formData = new FormData();
        formData.append('database', file);

        // 获取认证头
        const authHeaders = this.getAuthHeaders();
        const headers = {};
        if (authHeaders['x-admin-password']) {
          headers['x-admin-password'] = authHeaders['x-admin-password'];
        }

        // 5. 发送请求
        const response = await fetch('/api/settings/import-database', {
          method: 'POST',
          headers: headers,
          body: formData
        });

        if (!response.ok) {
          const errorData = await response.json().catch(() => ({ error: '网络错误 ' + response.status }));
          throw new Error(errorData.error || '导入失败 (' + response.status + ')');
        }

        const result = await response.json();

        if (result.success) {
          this.showGlobalToast('数据库导入成功！页面将在3秒后自动刷新', 'success');
          // 延迟刷新
          setTimeout(() => {
            window.location.reload();
          }, 3000);
        } else {
          this.showGlobalToast('导入失败: ' + result.error, 'error');
        }
      } catch (error) {
        console.error('Database import error:', error);
        this.showGlobalToast('操作失败: ' + error.message, 'error');
      }
    };

    // 3. 立即触发点击
    input.click();

    // 兜底清理：如果用户取消了选择框，且 input 还在 body 里（某些浏览器行为不同），1分钟后清理
    setTimeout(() => {
      if (document.body.contains(input)) {
        document.body.removeChild(input);
      }
    }, 60000);
  },

  // 旧版JSON导入（保留用于兼容）
  async importAllDataLegacy() {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json';
    input.style.display = 'none';
    document.body.appendChild(input);

    input.onchange = async (event) => {
      const file = event.target.files[0];
      // 触发后立即移除元素
      document.body.removeChild(input);
      if (!file) return;

      const confirmed = await this.showConfirm({
        title: '确认导入',
        message: '导入数据将覆盖当前所有配置，是否继续？',
        icon: 'fa-exclamation-triangle',
        confirmText: '确定导入',
        confirmClass: 'btn-warning'
      });

      if (!confirmed) return;

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
        headers: store.getAuthHeaders()
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
        headers: store.getAuthHeaders()
      });
      if (response.ok) {
        const result = await response.json();
        if (result.success) {
          this.logSettings = { ...result.data };
          // 同步保存文件信息
          if (result.fileInfo) {
            this.logFileInfo = result.fileInfo;
          }
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
        // 更新文件信息
        if (result.fileInfo) {
          this.logFileInfo = result.fileInfo;
        }
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
  // 全局 API 设置相关方法
  // (保留 saveGlobalApiSettings 等)

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

      this.showGlobalToast('全局 API 及网络配置已同步应用', 'success', 3000, true);
    } catch (error) {
      console.error('保存全局设置失败:', error);
      this.showGlobalToast('保存失败: ' + error.message, 'error');
    } finally {
      this.antigravitySaving = false;
    }
  }
};
