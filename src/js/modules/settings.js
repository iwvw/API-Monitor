/**
 * 设置管理模块
 * 负责系统设置、模块配置和数据导入导出
 */

import { store } from '../store.js';

export const settingsMethods = {
  // 从后端加载所有设置
  async loadModuleSettings() {
    try {
      const response = await fetch('/api/settings', {
        headers: this.getAuthHeaders(),
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

          // 应用 PaaS 刷新间隔
          if (settings.zeaburRefreshInterval) {
            this.zeaburRefreshInterval = settings.zeaburRefreshInterval;
            this.zeaburRefreshIntervalSec = settings.zeaburRefreshInterval / 1000;
            if (
              this.mainActiveTab === 'paas' &&
              this.paasCurrentPlatform === 'zeabur' &&
              !this.dataRefreshPaused
            ) {
              this.startAutoRefresh();
            }
          }
          if (settings.koyebRefreshInterval) {
            this.koyebRefreshInterval = settings.koyebRefreshInterval;
            this.koyebRefreshIntervalSec = settings.koyebRefreshInterval / 1000;
          }
          if (settings.flyRefreshInterval) {
            this.flyRefreshInterval = settings.flyRefreshInterval;
            this.flyRefreshIntervalSec = settings.flyRefreshInterval / 1000;
          }

          // 应用模块设置
          const validModules = [
            'dashboard',
            'openai',
            'antigravity',
            'gemini-cli',
            'paas',
            'dns',
            'aliyun',
            'self-h',
            'server',
            'totp',
            'music',
            'uptime',
          ];
          if (settings.moduleVisibility) {
            const filtered = {};
            validModules.forEach(m => {
              filtered[m] = settings.moduleVisibility[m] !== false;
            });
            this.moduleVisibility = filtered;
          }
          if (settings.moduleOrder) {
            // 保留已保存的有效模块顺序
            const savedOrder = settings.moduleOrder.filter(m => validModules.includes(m));
            // 找出所有缺失的模块（新增或之前未保存的）
            const missingModules = validModules.filter(m => !savedOrder.includes(m));
            // 合并：已保存顺序 + 缺失模块追加到末尾
            this.moduleOrder = [...savedOrder, ...missingModules];
          }

          if (settings.channelEnabled) Object.assign(this.channelEnabled, settings.channelEnabled);
          if (settings.channelModelPrefix)
            Object.assign(this.channelModelPrefix, settings.channelModelPrefix);
          if (settings.load_balancing_strategy)
            this.agSettingsForm.load_balancing_strategy = settings.load_balancing_strategy;
          if (settings.serverIpDisplayMode) this.serverIpDisplayMode = settings.serverIpDisplayMode;
          if (settings.vibrationEnabled !== undefined)
            this.vibrationEnabled = settings.vibrationEnabled;

          if (settings.totpSettings) Object.assign(this.totpSettings, settings.totpSettings);
          if (settings.navLayout) this.navLayout = settings.navLayout;
          if (settings.agentDownloadUrl !== undefined)
            this.agentDownloadUrl = settings.agentDownloadUrl;
          if (settings.publicApiUrl !== undefined)
            this.publicApiUrl = settings.publicApiUrl;

          this.activateFirstVisibleModule();
          return true;
        }
      }
      return false;
    } catch (error) {
      console.error('加载用户设置失败:', error);
      return false;
    }
  },

  // 基础常用逻辑
  handleSettingsEsc() {
    this.showSettingsModal = false;
  },

  // 切换模块可见性
  toggleModuleVisibility(module) {
    this.moduleVisibility[module] = !this.moduleVisibility[module];
  },

  setAllModulesVisibility(visible) {
    Object.keys(this.moduleVisibility).forEach(k => {
      this.moduleVisibility[k] = visible;
    });
    // dashboard 强制可见
    this.moduleVisibility.dashboard = true;
  },

  // 模块排序拖拽处理
  handleDragStart(event, index) {
    this.draggedIndex = index;
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', index);
  },

  handleDragEnd() {
    this.draggedIndex = null;
  },

  handleDragOver(event, index) {
    event.preventDefault();
    if (this.draggedIndex === null || this.draggedIndex === index) return;
  },

  handleDrop(event, index) {
    event.preventDefault();
    if (this.draggedIndex === null || this.draggedIndex === index) return;

    const items = [...this.moduleOrder];
    const [removed] = items.splice(this.draggedIndex, 1);
    items.splice(index, 0, removed);
    this.moduleOrder = items;
    this.draggedIndex = null;
  },

  // 触摸拖拽处理 (移动端)
  handleTouchStart(event, index) {
    this.draggedIndex = index;
  },

  handleTouchMove(event, index) {
    // 可选：添加触觉反馈
  },

  handleTouchEnd(event, index) {
    if (this.draggedIndex === null || this.draggedIndex === index) {
      this.draggedIndex = null;
      return;
    }

    const items = [...this.moduleOrder];
    const [removed] = items.splice(this.draggedIndex, 1);
    items.splice(index, 0, removed);
    this.moduleOrder = items;
    this.draggedIndex = null;
  },

  // 修改密码
  async changePassword() {
    if (!this.newPassword || this.newPassword.length < 6) {
      this.passwordError = '密码至少需要6位';
      return;
    }
    if (this.newPassword !== this.confirmPassword) {
      this.passwordError = '两次输入密码不一致';
      return;
    }

    try {
      const response = await fetch('/api/auth/change-password', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({ password: this.newPassword }),
      });
      const result = await response.json();
      if (result.success) {
        this.passwordSuccess = '密码修改成功，下次登录生效';
        this.passwordError = '';
        this.newPassword = '';
        this.confirmPassword = '';
      } else {
        this.passwordError = result.msg || '修改失败';
      }
    } catch (e) {
      this.passwordError = '请求出错: ' + e.message;
    }
  },

  // 统一保存全局 API 设置 (我们之前重点修复的核心逻辑)
  async saveGlobalApiSettings() {
    console.log('[Settings] 保存全局配置...');
    this.antigravitySaving = true;
    try {
      const apiKey = this.agSettingsForm.API_KEY;
      const proxy = this.agSettingsForm.PROXY;
      const isMaskedKey = apiKey && /^\.+$/.test(apiKey);

      const payload = { PROXY: proxy || '' };
      if (!isMaskedKey && apiKey !== undefined) {
        payload.API_KEY = apiKey;
      }

      // 1. 同步保存到 Antigravity 和 Gemini CLI
      await fetch('/api/antigravity/settings', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(payload),
      });
      await fetch('/api/gemini-cli/settings', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(payload),
      });

      // 2. 保存其余全局选项
      await this.saveUserSettingsToServer();
      this.showGlobalToast('全局配置已保存', 'success');
    } catch (error) {
      this.showGlobalToast('保存失败: ' + error.message, 'error');
    } finally {
      this.antigravitySaving = false;
    }
  },

  // 保存所有设置到后端
  async saveUserSettingsToServer() {
    if (!this.isAuthenticated) return false;
    this.antigravitySaving = true;
    try {
      const settings = {
        customCss: this.customCss,
        moduleVisibility: this.moduleVisibility,
        channelEnabled: this.channelEnabled,
        channelModelPrefix: this.channelModelPrefix,
        moduleOrder: this.moduleOrder,
        load_balancing_strategy: this.agSettingsForm.load_balancing_strategy,
        serverIpDisplayMode: this.serverIpDisplayMode,
        vibrationEnabled: this.vibrationEnabled,
        navLayout: this.navLayout,
        totpSettings: this.totpSettings,
        agentDownloadUrl: this.agentDownloadUrl,
        publicApiUrl: this.publicApiUrl,
        // 刷新间隔设置
        zeaburRefreshInterval: this.zeaburRefreshInterval,
        koyebRefreshInterval: this.koyebRefreshInterval,
        flyRefreshInterval: this.flyRefreshInterval,
      };

      const response = await fetch('/api/settings', {
        method: 'POST',
        headers: this.getAuthHeaders(),
        body: JSON.stringify(settings),
      });
      return response.ok;
    } catch (error) {
      console.error('[Settings] 保存设置失败:', error);
      return false;
    } finally {
      this.antigravitySaving = false;
    }
  },

  // 渠道开关处理
  toggleChannelEnabled(channel) {
    if (!this.channelEnabled) this.channelEnabled = {};
    this.channelEnabled[channel] = !this.channelEnabled[channel];
    this.saveUserSettingsToServer();
  },

  // 数据库运维操作
  async handleClearLogs() {
    const confirmed = await store.showConfirm({
      title: '确认清空日志',
      message: '这将删除所有历史日志记录，此操作不可逆。确认继续？',
      icon: 'fa-trash-alt',
      confirmText: '确认清空',
      confirmClass: 'btn-danger',
    });
    if (!confirmed) return;
    try {
      await fetch('/api/settings/clear-logs', { method: 'POST', headers: store.getAuthHeaders() });
      this.fetchDbStats();
      this.showGlobalToast('日志已清空', 'success');
    } catch (e) {
      this.showGlobalToast('清空失败', 'error');
    }
  },

  async handleVacuumDb() {
    this.vacuuming = true;
    try {
      await fetch('/api/settings/vacuum-database', {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });
      this.fetchDbStats();
      this.showGlobalToast('数据库已压缩', 'success');
    } catch (e) {
      this.showGlobalToast('压缩失败', 'error');
    } finally {
      this.vacuuming = false;
    }
  },

  async saveLogSettings() {
    try {
      await fetch('/api/settings/log-settings', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(this.logSettings),
      });
      this.showGlobalToast('自动清理配置已保存', 'success');
    } catch (e) { }
  },

  async enforceLogLimits() {
    this.logLimitsEnforcing = true;
    try {
      await fetch('/api/settings/enforce-log-limits', {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });
      this.fetchDbStats();
      this.showGlobalToast('清理执行成功', 'success');
    } catch (e) {
    } finally {
      this.logLimitsEnforcing = false;
    }
  },

  // 加载统计信息
  async fetchDbStats() {
    try {
      const response = await fetch('/api/settings/database-stats', {
        headers: store.getAuthHeaders(),
      });
      const result = await response.json();
      if (result.success) this.dbStats = result.data;
    } catch (e) { }
  },

  async fetchLogSettings() {
    try {
      const response = await fetch('/api/settings/log-settings', {
        headers: store.getAuthHeaders(),
      });
      const result = await response.json();
      if (result.success) this.logSettings = result.data;
    } catch (e) { }
  },

  // 导数逻辑
  exportAllData() {
    window.location.href = `/api/settings/export-database?admin_password=${encodeURIComponent(store.loginPassword)}`;
  },

  importAllData() {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.db';
    input.onchange = async e => {
      const file = e.target.files[0];
      if (!file) return;

      const formData = new FormData();
      formData.append('database', file);

      try {
        const response = await fetch('/api/settings/import-database', {
          method: 'POST',
          headers: { 'x-admin-password': store.loginPassword },
          body: formData,
        });
        const result = await response.json();
        if (result.success) {
          alert('数据导入成功，应用将自动重启');
          window.location.reload();
        } else {
          alert('导入失败: ' + (result.error || result.msg || '未知错误'));
        }
      } catch (err) {
        alert('导入出错');
      }
    };
    input.click();
  },

  // 样视管理
  applyCustomCss() {
    let style = document.getElementById('custom-css-dynamic');
    if (!style) {
      style = document.createElement('style');
      style.id = 'custom-css-dynamic';
      document.head.appendChild(style);
    }
    style.textContent = this.customCss;
  },

  async saveCustomCss() {
    this.applyCustomCss();
    await this.saveUserSettingsToServer();
    this.customCssSuccess = '样式已保存';
    setTimeout(() => (this.customCssSuccess = ''), 3000);
  },

  resetCustomCss() {
    if (confirm('确认重置自定义样式吗？')) {
      this.customCss = '';
      this.applyCustomCss();
      this.saveUserSettingsToServer();
    }
  },

  // 激活第一个可见项（仅当当前 tab 不可见时）
  activateFirstVisibleModule() {
    if (store.singlePageMode) return;
    // 如果当前 tab (默认为 dashboard) 是可见的，则不切换
    if (this.moduleVisibility[this.mainActiveTab]) return;
    // 否则找到第一个可见的模块
    const first = this.moduleOrder.find(m => this.moduleVisibility[m]);
    if (first) this.mainActiveTab = first;
  },

  // Helper 方法补全 (从 html 中引用到的)
  getModuleIcon(m) {
    return store.getModuleIcon ? store.getModuleIcon(m) : 'fa-cube';
  },
  getModuleName(m) {
    return store.getModuleName ? store.getModuleName(m) : m;
  },
  formatFileSize(s) {
    return s > 1024 * 1024 ? (s / (1024 * 1024)).toFixed(2) + 'MB' : (s / 1024).toFixed(2) + 'KB';
  },
  getLogIcon(l) {
    return l === 'ERROR' ? 'fa-exclamation-circle' : 'fa-info-circle';
  },
  formatMessage(m) {
    return m;
  },

  // 如果 main.js 引用了 saveSettings 作为别名
  saveSettings() {
    return this.saveUserSettingsToServer();
  },
};
