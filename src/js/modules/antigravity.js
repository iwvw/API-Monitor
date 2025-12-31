/**
 * Antigravity API 模块
 */

import { store } from '../store.js';
import { toast } from './toast.js';

export const antigravityMethods = {
  switchToAntigravity() {
    store.mainActiveTab = 'antigravity';
    store.antigravityCurrentTab = 'quotas';
    this.loadAntigravityAccounts();
    this.loadAntigravityStats();
    this.loadAntigravityCheckHistory(); // 加载检测历史
    this.loadAntigravityAutoCheckSettings(); // 加载定时检测设置
  },

  async loadAntigravityAccounts() {
    store.antigravityLoading = true;
    try {
      const response = await fetch('/api/antigravity/accounts', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        store.antigravityAccounts = data;

        // 如果未选中账号，默认选中第一个在线账号
        if (!store.antigravityQuotaSelectedAccountId && data.length > 0) {
          const firstOnline = data.find(acc => acc.status === 'online');
          store.antigravityQuotaSelectedAccountId = firstOnline ? firstOnline.id : data[0].id;
          // 加载选中账号的额度
          this.loadAntigravityQuotas();
        }

        // 为每个账号加载简要额度信息
        this.loadAllAccountQuotas();

        if (store.mainActiveTab === 'antigravity' && store.antigravityCurrentTab === 'accounts') {
          toast.success('账号列表已刷新');
        }
      }
    } catch (error) {
      console.error('加载 Antigravity 账号失败:', error);
      toast.error('加载账号失败');
    } finally {
      store.antigravityLoading = false;
    }
  },

  /**
   * 为所有账号加载简要额度信息
   */
  async loadAllAccountQuotas() {
    const KEY_MODELS = [
      'gemini-3-pro-high',
      'gemini-3-flash',
      'gemini-3-pro-image',
      'claude-sonnet-4-5',
    ];

    for (const account of store.antigravityAccounts) {
      if (account.status !== 'online') continue;

      try {
        const response = await fetch(`/api/antigravity/accounts/${account.id}/quotas`, {
          headers: store.getAuthHeaders(),
        });
        if (response.ok) {
          const quotaData = await response.json();
          // 后端返回的是分组数据：{ groupId: { models: [{ id, remaining }] } }
          // 需要遍历所有分组找到目标模型
          const quotas = {};

          // 遍历所有分组
          for (const groupId of Object.keys(quotaData)) {
            const group = quotaData[groupId];
            if (!group.models) continue;

            // 遍历分组内的模型
            for (const model of group.models) {
              if (KEY_MODELS.includes(model.id)) {
                quotas[model.id] = {
                  percent: model.remaining || 0,
                };
              }
            }
          }

          // Vue 响应式更新
          account.quotas = quotas;
        }
      } catch (error) {
        console.error(`加载账号 ${account.name} 额度失败:`, error);
      }
    }
  },

  openAddAntigravityManualModal() {
    this.antigravityManualForm = {
      name: '',
      accessToken: '',
      refreshToken: '',
      projectId: '',
      expiresIn: 3599,
    };
    this.antigravityManualFormError = '';
    this.showAntigravityManualModal = true;
  },

  openAddAntigravityAccountModal() {
    this.antigravityEditingAccount = null;
    this.antigravityAccountForm = {
      name: '',
      email: '',
      password: '',
    };
    this.antigravityAccountFormError = '';
    this.showAntigravityAccountModal = true;
  },

  editAntigravityAccount(account) {
    this.antigravityEditingAccount = account;
    this.antigravityAccountForm = {
      name: account.name || '',
      email: account.email || '',
      password: account.password || '',
    };
    this.antigravityAccountFormError = '';
    this.showAntigravityAccountModal = true;
  },

  async saveAntigravityAccount() {
    if (!this.antigravityAccountForm.name) {
      this.antigravityAccountFormError = '请填写账号名称';
      return;
    }

    store.antigravitySaving = true;
    try {
      const url = this.antigravityEditingAccount
        ? `/api/antigravity/accounts/${this.antigravityEditingAccount.id}`
        : '/api/antigravity/accounts';

      const response = await fetch(url, {
        method: this.antigravityEditingAccount ? 'PUT' : 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(this.antigravityAccountForm),
      });

      if (response.ok) {
        toast.success(this.antigravityEditingAccount ? '账号已更新' : '账号已添加');
        this.showAntigravityAccountModal = false;
        this.loadAntigravityAccounts();
      } else {
        const data = await response.json();
        this.antigravityAccountFormError = data.error || '保存失败';
      }
    } catch (error) {
      this.antigravityAccountFormError = '保存失败: ' + error.message;
    } finally {
      this.antigravitySaving = false;
    }
  },

  async deleteAntigravityAccount(account) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除账号 "${account.name}" 吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(`/api/antigravity/accounts/${account.id}`, {
        method: 'DELETE',
        headers: store.getAuthHeaders(),
      });

      if (response.ok) {
        toast.success('账号已删除');
        this.loadAntigravityAccounts();
        this.loadAntigravityStats(); // Refresh stats
      } else {
        toast.error('删除失败');
      }
    } catch (error) {
      toast.error('删除失败: ' + error.message);
    }
  },

  // 手动添加账号
  async saveAntigravityManualAccount() {
    if (!this.antigravityManualForm.accessToken || !this.antigravityManualForm.refreshToken) {
      this.antigravityManualFormError = 'Access Token 和 Refresh Token 均为必填项';
      return;
    }

    store.antigravitySaving = true;
    try {
      const response = await fetch('/api/antigravity/accounts/manual', {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(this.antigravityManualForm),
      });

      const data = await response.json();
      if (response.ok) {
        toast.success('账号添加成功');
        this.showAntigravityManualModal = false;
        this.loadAntigravityAccounts();
        this.loadAntigravityStats();
        // Reset form
        this.antigravityManualForm = {
          name: '',
          accessToken: '',
          refreshToken: '',
          projectId: '',
          expiresIn: 3599,
        };
      } else {
        this.antigravityManualFormError = data.error || '添加失败';
      }
    } catch (error) {
      this.antigravityManualFormError = '添加失败: ' + error.message;
    } finally {
      store.antigravitySaving = false;
    }
  },

  // 加载统计
  async loadAntigravityStats() {
    try {
      const response = await fetch('/api/antigravity/stats', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      store.antigravityStats = data;
    } catch (error) {
      console.error('Failed to load stats:', error);
    }
  },

  async toggleAntigravityAccount(account) {
    try {
      const response = await fetch(`/api/antigravity/accounts/${account.id}`, {
        method: 'PUT',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ enable: !account.enable }),
      });

      if (response.ok) {
        toast.success(account.enable ? '账号已停用' : '账号已启用');
        this.loadAntigravityAccounts();
      }
    } catch (error) {
      toast.error('切换状态失败');
    }
  },

  // OAuth 逻辑
  async openGoogleAuthUrl() {
    try {
      const response = await fetch('/api/antigravity/oauth/url', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (data.url) {
        window.open(data.url, '_blank');
      }
    } catch (error) {
      toast.error('获取授权链接失败');
    }
  },

  async parseAgOauthUrl() {
    if (!this.agOauthUrl) return;
    store.antigravityLoading = true;
    try {
      const response = await fetch('/api/antigravity/oauth/parse-url', {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          url: this.agOauthUrl,
          customProjectId: this.agCustomProjectId,
          allowRandomProjectId: this.agAllowRandomProjectId,
        }),
      });

      const data = await response.json();
      if (response.ok) {
        toast.success('账号授权成功');
        this.agOauthUrl = '';
        store.antigravityCurrentTab = 'accounts';
        this.loadAntigravityAccounts();
      } else {
        toast.error(data.error || '解析失败');
      }
    } catch (error) {
      toast.error('解析失败: ' + error.message);
    } finally {
      store.antigravityLoading = false;
    }
  },

  async refreshAntigravityProjectId(account) {
    try {
      const response = await fetch(`/api/antigravity/accounts/${account.id}/refresh-project-id`, {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (response.ok) {
        toast.success(`项目 ID 已更新: ${data.projectId}`);
        this.loadAntigravityAccounts();
      } else {
        toast.error(data.error || '更新失败');
      }
    } catch (error) {
      toast.error('更新失败: ' + error.message);
    }
  },

  // 刷新所有凭证
  async refreshAllAgAccounts() {
    store.agRefreshingAll = true;
    toast.info('正在刷新所有凭证及邮箱信息...');
    try {
      const response = await fetch('/api/antigravity/accounts/refresh-all', {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (response.ok) {
        const s =
          data.success_count !== undefined
            ? data.success_count
            : typeof data.refreshed === 'number'
              ? data.refreshed
              : 0;
        const f =
          data.fail_count !== undefined
            ? data.fail_count
            : typeof data.failed === 'number'
              ? data.failed
              : 0;
        toast.success(`同步完成: 成功 ${s}, 失败 ${f}`);

        if (Array.isArray(data.accounts)) {
          store.antigravityAccounts = data.accounts;
        } else {
          this.loadAntigravityAccounts();
        }
        this.loadAntigravityStats();
      } else {
        toast.error(data.error || '刷新失败');
      }
    } catch (error) {
      toast.error('刷新失败: ' + error.message);
    } finally {
      store.agRefreshingAll = false;
    }
  },

  // 切换 Antigravity 子标签页
  switchAntigravityTab(tabName) {
    store.antigravityCurrentTab = tabName;

    // 根据不同标签页加载对应数据
    if (tabName === 'quotas') {
      // 如果还没加载过账号，先加载
      if (!this.antigravityAccounts || this.antigravityAccounts.length === 0) {
        this.loadAntigravityAccounts().then(() => {
          // 如果没有选中账号，默认选中第一个启用的
          if (
            !this.antigravityQuotaSelectedAccountId &&
            this.antigravityAccounts &&
            this.antigravityAccounts.length > 0
          ) {
            const enabled = this.antigravityAccounts.filter(a => a.enable);
            this.antigravityQuotaSelectedAccountId =
              enabled.length > 0 ? enabled[0].id : this.antigravityAccounts[0].id;
          }
          this.loadAntigravityQuotas();
        });
      } else {
        // 如果已有账号但没选中
        if (
          !this.antigravityQuotaSelectedAccountId &&
          this.antigravityAccounts &&
          this.antigravityAccounts.length > 0
        ) {
          const enabled = this.antigravityAccounts.filter(a => a.enable);
          this.antigravityQuotaSelectedAccountId =
            enabled.length > 0 ? enabled[0].id : this.antigravityAccounts[0].id;
        }
        this.loadAntigravityQuotas();
      }
    } else if (tabName === 'matrix') {
      // 矩阵列表依赖于额度中的模型列表，因此需要确保额度数据已加载
      if (!store.antigravityQuotas || Object.keys(store.antigravityQuotas).length === 0) {
        this.loadAntigravityQuotas().then(() => {
          this.loadAntigravityMatrix();
        });
      } else {
        this.loadAntigravityMatrix();
      }
    } else if (tabName === 'settings') {
      this.loadAntigravitySettings();
    } else if (tabName === 'logs') {
      this.loadAntigravityLogs();
    } else if (tabName === 'accounts') {
      // 加载账号列表和检测历史
      this.loadAntigravityAccounts();
      this.loadAntigravityCheckHistory();
    } else {
      // 切出额度页，停止轮询
      this.stopAntigravityQuotaPolling();
    }
  },

  // 启动/安排下一次额度刷新
  scheduleNextQuotaLoad() {
    this.stopAntigravityQuotaPolling(); // 先清除旧的

    // 只有当前在 Antigravity 模块且在 quotas 标签页时才安排下次刷新
    if (this.mainActiveTab === 'antigravity' && this.antigravityCurrentTab === 'quotas') {
      this.antigravityQuotaTimer = setTimeout(() => {
        // 如果当前不可见，则不刷新，但重新安排下一次检查
        if (document.visibilityState !== 'visible') {
          this.scheduleNextQuotaLoad();
          return;
        }
        this.loadAntigravityQuotas(true); // true 表示自动刷新
      }, 30000); // 30秒刷新一次
    }
  },

  stopAntigravityQuotaPolling() {
    if (this.antigravityQuotaTimer) {
      clearTimeout(this.antigravityQuotaTimer);
      this.antigravityQuotaTimer = null;
    }
  },

  // 额度查看
  async loadAntigravityQuotas(isAutoRefresh = false) {
    // 如果不是自动刷新，显示 loading 状态
    if (!isAutoRefresh) {
      store.antigravityQuotaLoading = true;
    }

    try {
      let url = '/api/antigravity/quotas';
      // 如果选中了特定账号，使用特定账号的 API
      if (store.antigravityQuotaSelectedAccountId) {
        url = `/api/antigravity/accounts/${store.antigravityQuotaSelectedAccountId}/quotas`;
      }

      const res = await fetch(url, {
        headers: store.getAuthHeaders(),
      });
      const data = await res.json();
      store.antigravityQuotas = data;
      store.antigravityQuotasLastUpdated = new Date().toLocaleString('zh-CN', {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      });

      if (!isAutoRefresh) {
        toast.success('刷新成功');
      }
    } catch (error) {
      console.error('加载额度失败:', error);
      if (!isAutoRefresh) {
        toast.error('加载额度失败');
      }
    } finally {
      if (!isAutoRefresh) {
        store.antigravityQuotaLoading = false;
      }
      // 无论成功失败，都安排下一次刷新
      this.scheduleNextQuotaLoad();
    }
  },

  getAgQuotaColor(percent) {
    if (percent > 40) return 'var(--ag-success)';
    if (percent > 10) return 'var(--ag-warning)';
    return 'var(--ag-error)';
  },

  getLogStatusClass(statusCode) {
    if (statusCode >= 200 && statusCode < 300) return 'status-2xx';
    if (statusCode >= 300 && statusCode < 400) return 'status-3xx';
    if (statusCode >= 400 && statusCode < 500) return 'status-4xx';
    if (statusCode >= 500) return 'status-5xx';
    return '';
  },

  // 格式化重置时间显示 (前端本地时区)
  formatDisplayDate(isoTime) {
    if (!isoTime) return '无';
    try {
      const date = new Date(isoTime);
      if (isNaN(date.getTime())) return isoTime;

      return date
        .toLocaleString('zh-CN', {
          month: '2-digit',
          day: '2-digit',
          hour: '2-digit',
          minute: '2-digit',
          hour12: false,
        })
        .replace(/\//g, '-');
    } catch (e) {
      return isoTime;
    }
  },

  // 将重置时间转换为倒计时格式 (自动适配时区)
  formatResetCountdown(isoTime, nowVal) {
    if (!isoTime) return '无';
    try {
      const resetDate = new Date(isoTime);
      if (isNaN(resetDate.getTime())) return '无';

      const now = new Date();
      const diffMs = resetDate - now;

      if (diffMs <= 0) return '已重置';

      const totalMinutes = Math.floor(diffMs / (1000 * 60));
      const totalHours = Math.floor(totalMinutes / 60);
      const remainMinutes = totalMinutes % 60;

      if (totalHours >= 24) {
        const days = Math.floor(totalHours / 24);
        const remainHours = totalHours % 24;
        return `${days}天${remainHours}时`;
      }

      if (totalHours > 0) {
        return `${totalHours}时${remainMinutes}分`;
      }

      return `${remainMinutes}分`;
    } catch (e) {
      return '无';
    }
  },

  // 日志管理
  async loadAntigravityLogs() {
    store.antigravityLoading = true;
    try {
      const response = await fetch('/api/antigravity/logs', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      store.antigravityLogs = data.logs || [];
      if (store.mainActiveTab === 'antigravity' && store.antigravityCurrentTab === 'logs') {
        toast.success('调用日志已更新');
      }
    } catch (error) {
      toast.error('加载日志失败');
    } finally {
      store.antigravityLoading = false;
    }
  },

  // 获取过滤后的 Antigravity 日志
  getFilteredAntigravityLogs() {
    let logs = store.antigravityLogs || [];

    if (store.antigravityLogFilterAccount) {
      logs = logs.filter(log => log.accountId === store.antigravityLogFilterAccount);
    }

    if (store.antigravityLogFilterModel) {
      logs = logs.filter(log => log.model === store.antigravityLogFilterModel);
    }

    return logs;
  },

  // 获取日志中所有出现的模型列表（用于筛选下拉框）
  getAntigravityLogModels() {
    const models = new Set();
    (store.antigravityLogs || []).forEach(log => {
      if (log.model) models.add(log.model);
    });
    return Array.from(models).sort();
  },

  async clearAntigravityLogs() {
    const confirmed = await store.showConfirm({
      title: '确认清空',
      message: '确定要清空所有调用日志吗？',
      icon: 'fa-trash',
      confirmText: '清空',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch('/api/antigravity/logs/clear', {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('日志已清空');
        this.loadAntigravityLogs();
      } else {
        toast.error('清空失败');
      }
    } catch (error) {
      toast.error('请求失败: ' + error.message);
    }
  },

  async viewAntigravityLogDetail(log) {
    try {
      const response = await fetch(`/api/antigravity/logs/${log.id}`, {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();

      if (data.log) {
        store.agLogDetailShowRaw = false;
        this.antigravityLogDetail = data.log;
        this.showAntigravityLogDetailModal = true;
      } else {
        toast.error('日志详情获取失败');
      }
    } catch (error) {
      console.error('获取日志详情失败:', error);
      toast.error('获取日志详情失败: ' + error.message);
    }
  },

  // 设置管理
  async loadAntigravitySettings() {
    store.antigravityLoading = true;
    try {
      const response = await fetch('/api/antigravity/settings', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      store.antigravitySettings = data;

      // 转换为表单对象，预初始化所有可能的 Key
      const form = {
        DEFAULT_TEMPERATURE: '',
        DEFAULT_TOP_P: '',
        DEFAULT_TOP_K: '',
        DEFAULT_MAX_TOKENS: '',
        MAX_IMAGES: '',
        IMAGE_BASE_URL: '',
        CREDENTIAL_MAX_USAGE_PER_HOUR: '',
        TIMEOUT: '',
        REQUEST_LOG_RETENTION_DAYS: '',
        API_KEY: '',
        PROXY: '',
      };
      // 后端返回的是 { key: value } 对象，直接合并
      if (data && typeof data === 'object' && !Array.isArray(data)) {
        Object.keys(data).forEach(key => {
          if (key in form || key === 'API_KEY' || key === 'PROXY') {
            form[key] = data[key];
          }
        });
      } else if (Array.isArray(data)) {
        // 兼容旧格式（数组）
        data.forEach(s => {
          form[s.key] = s.value;
        });
      }
      // 使用 Object.assign 而非直接赋值，以保留已有的全局设置（如负载均衡策略）
      Object.assign(this.agSettingsForm, form);

      // 后台异步加载重定向配置，不阻塞主设置加载
      this.loadModelRedirects();

      if (store.mainActiveTab === 'antigravity' && store.antigravityCurrentTab === 'settings') {
        toast.success('模块配置已同步');
      }
    } catch (error) {
      toast.error('加载设置失败');
    } finally {
      store.antigravityLoading = false;
    }
  },

  // 批量保存所有设置
  async saveAllAgSettings() {
    this.antigravitySaving = true;
    try {
      const keys = Object.keys(this.agSettingsForm);
      let saved = 0;
      for (const key of keys) {
        const value = this.agSettingsForm[key];
        // 允许保存空字符串，以支持清空设置（如 PROXY）
        // 只跳过 undefined 值
        if (value !== undefined) {
          await fetch('/api/antigravity/settings', {
            method: 'POST',
            headers: {
              ...store.getAuthHeaders(),
              'Content-Type': 'application/json',
            },
            body: JSON.stringify({ key, value: String(value) }),
          });
          saved++;
        }
      }
      toast.success(`已保存 ${saved} 项设置`);
    } catch (error) {
      toast.error('保存失败: ' + error.message);
    } finally {
      this.antigravitySaving = false;
    }
  },

  antigravitySettingsByGroup(group) {
    if (!Array.isArray(this.antigravitySettings)) return [];
    return this.antigravitySettings.filter(s => s.category === group);
  },

  getAgSettingLabel(key) {
    const labels = {
      // 'API_KEY': 'API 访问密钥 (v1/*)',
      CREDENTIAL_MAX_USAGE_PER_HOUR: '凭证每小时调用上限',
      REQUEST_LOG_RETENTION_DAYS: '日志保留天数',
      PORT: '服务监听端口',
      HOST: '服务监听地址',
      API_URL: '流式接口 URL',
      API_MODELS_URL: '模型列表 URL',
      API_NO_STREAM_URL: '非流式接口 URL',
      API_HOST: 'API Host 头',
      API_USER_AGENT: 'User-Agent',
      PROXY: 'HTTP 代理',
      TIMEOUT: '请求超时 (ms)',
      USE_NATIVE_AXIOS: '使用原生 Axios',
    };
    return labels[key] || key;
  },

  getAgSettingDefault(key) {
    const defaults = {
      CREDENTIAL_MAX_USAGE_PER_HOUR: '20',
      RETRY_STATUS_CODES: '429,500',
      RETRY_MAX_ATTEMPTS: '3',
      MAX_IMAGES: '10',
      MAX_REQUEST_SIZE: '50mb',
      PORT: '8045',
    };
    return defaults[key] || '-';
  },

  // isAgSettingSensitive(key) {
  //     return ['PANEL_PASSWORD', 'API_KEY', 'GOOGLE_CLIENT_SECRET'].includes(key);
  // },

  async saveAgSetting(s) {
    try {
      const response = await fetch('/api/antigravity/settings', {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ key: s.key, value: s.value }),
      });
      if (response.ok) {
        this.showGlobalToast(`设置已保存: ${this.getAgSettingLabel(s.key)}`);
      }
    } catch (error) {
      toast.error('保存失败');
    }
  },

  getAccountName(accountId) {
    const acc = this.antigravityAccounts.find(a => a.id === accountId);
    return acc ? acc.name : accountId || 'System';
  },

  // 视图切换
  toggleAntigravityQuotaView() {
    if (!store.antigravityQuotaViewMode) {
      store.antigravityQuotaViewMode = 'grouped';
    } else {
      store.antigravityQuotaViewMode =
        store.antigravityQuotaViewMode === 'list' ? 'grouped' : 'list';
    }
  },

  getAllAntigravityModels() {
    if (!this.antigravityQuotas) return [];

    let allModels = [];

    // 明确的分组顺序
    const groupOrder = ['图像生成', 'claude_gpt', 'tab_completion', 'gemini', 'others'];

    // 按照固定顺序遍历分组
    groupOrder.forEach(groupId => {
      const group = this.antigravityQuotas[groupId];
      if (group && group.models && Array.isArray(group.models)) {
        // 给模型加上分组图标，方便识别
        const modelsWithIcon = group.models.map(m => ({
          ...m,
          groupIcon: group.icon,
          groupName: group.name,
        }));
        allModels = allModels.concat(modelsWithIcon);
      }
    });

    return allModels;
  },

  async toggleModelStatus(model, event) {
    const enabled = event.target.checked;
    const modelId = model.id;
    let foundModel = null;

    // Optimistic update: 修改源数据以触发视图更新
    // 遍历所有分组找到该模型
    Object.values(this.antigravityQuotas).forEach(group => {
      if (group.models) {
        const target = group.models.find(m => m.id === modelId);
        if (target) {
          foundModel = target;
          // 如果属性不存在，Vue 2可能需要 $set，但通常加载后属性都在
          if (target.enabled === undefined) this.$set(target, 'enabled', true);
          target.enabled = enabled;
        }
      }
    });

    // 同时也修改传入的临时对象，以防万一
    model.enabled = enabled;

    try {
      const response = await fetch(`/api/antigravity/models/${modelId}/status`, {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ enabled }),
      });

      if (response.ok) {
        toast.success(`模型 ${modelId} 已${enabled ? '启用' : '禁用'}`);
      } else {
        // Revert
        if (foundModel) foundModel.enabled = !enabled;
        model.enabled = !enabled;
        event.target.checked = !enabled;
        toast.error('状态更新失败');
      }
    } catch (error) {
      // Revert
      if (foundModel) foundModel.enabled = !enabled;
      model.enabled = !enabled;
      event.target.checked = !enabled;
      toast.error('请求失败: ' + error.message);
    }
  },

  // 模型重定向管理
  async loadModelRedirects() {
    try {
      const response = await fetch('/api/antigravity/models/redirects', {
        headers: store.getAuthHeaders(),
      });
      store.antigravityModelRedirects = await response.json();
    } catch (error) {
      toast.error('加载重定向配置失败');
    }
  },

  async addModelRedirect(sourceModel, targetModel) {
    if (!sourceModel || !targetModel) return;

    try {
      // 如果是编辑模式，且修改了源模型名称（主键变了），则需要先删除旧的
      if (store.agEditingRedirectSource && store.agEditingRedirectSource !== sourceModel) {
        await fetch(
          `/api/antigravity/models/redirects/${encodeURIComponent(store.agEditingRedirectSource)}`,
          {
            method: 'DELETE',
            headers: store.getAuthHeaders(),
          }
        );
      }

      const response = await fetch('/api/antigravity/models/redirects', {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ sourceModel, targetModel }),
      });

      if (response.ok) {
        toast.success('重定向规则已保存');
        // 清空输入框和编辑状态
        store.newRedirectSource = '';
        store.newRedirectTarget = '';
        store.agEditingRedirectSource = null;
        await this.loadModelRedirects();
        return true;
      } else {
        const data = await response.json();
        toast.error('保存失败: ' + (data.error || '未知错误'));
        return false;
      }
    } catch (error) {
      toast.error('请求失败: ' + error.message);
      return false;
    }
  },

  editModelRedirect(r) {
    store.newRedirectSource = r.source_model;
    store.newRedirectTarget = r.target_model;
    store.agEditingRedirectSource = r.source_model;
    // 滚动到输入框
    const el = document.getElementById('ag-redirect-inputs');
    if (el) {
      el.scrollIntoView({ behavior: 'smooth', block: 'center' });
      // 简单的高亮效果
      el.style.boxShadow = '0 0 10px var(--ag-primary)';
      setTimeout(() => {
        el.style.boxShadow = 'none';
      }, 1000);
    }
  },

  async removeModelRedirect(sourceModel) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除 ${sourceModel} 的重定向规则吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(
        `/api/antigravity/models/redirects/${encodeURIComponent(sourceModel)}`,
        {
          method: 'DELETE',
          headers: store.getAuthHeaders(),
        }
      );

      if (response.ok) {
        toast.success('删除成功');
        await this.loadModelRedirects();
      } else {
        toast.error('删除失败');
      }
    } catch (error) {
      toast.error('请求失败: ' + error.message);
    }
  },

  // 模型矩阵管理
  async loadAntigravityMatrix() {
    store.antigravityLoading = true;
    try {
      const response = await fetch('/api/antigravity/config/matrix', {
        headers: store.getAuthHeaders(),
      });
      store.antigravityMatrix = await response.json();
      if (store.mainActiveTab === 'antigravity' && store.antigravityCurrentTab === 'matrix') {
        toast.success('模型矩阵已加载');
      }
    } catch (error) {
      console.error('Failed to load matrix:', error);
      toast.error('加载矩阵配置失败');
    } finally {
      store.antigravityLoading = false;
    }
  },

  async saveAntigravityMatrix() {
    store.antigravityLoading = true;
    try {
      const response = await fetch('/api/antigravity/config/matrix', {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(store.antigravityMatrix),
      });

      if (response.ok) {
        toast.success('矩阵配置已保存');
      } else {
        toast.error('保存失败');
      }
    } catch (error) {
      toast.error('保存请求失败: ' + error.message);
    } finally {
      store.antigravityLoading = false;
    }
  },

  getAntigravityMatrixList() {
    if (!store.antigravityMatrix) return [];

    // 我们希望矩阵列表包含当前在“额度状态”中出现且已启用的所有内部模型
    const allInternalModels = this.getAllAntigravityModels()
      .filter(m => m.enabled !== false) // 仅包含在额度状态中启用的模型
      .map(m => m.id);

    // 确保矩阵中有这些内部模型的条目
    allInternalModels.forEach(id => {
      if (!store.antigravityMatrix[id]) {
        // 如果是新发现的模型，给个默认禁用状态
        store.antigravityMatrix[id] = { base: false, fakeStream: false, antiTrunc: false };
      }
    });

    // 转换为数组用于渲染，并根据内部 ID 排序
    return Object.keys(store.antigravityMatrix)
      .filter(id => allInternalModels.includes(id)) // 仅显示在额度状态中启用的模型
      .sort()
      .map(id => ({
        id,
        ...store.antigravityMatrix[id],
      }));
  },

  toggleAgMatrixItem(modelId, field) {
    if (!store.antigravityMatrix[modelId]) {
      store.antigravityMatrix[modelId] = { base: false, fakeStream: false, antiTrunc: false };
    }
    store.antigravityMatrix[modelId][field] = !store.antigravityMatrix[modelId][field];
  },

  isAgMatrixColumnAllChecked(field) {
    const list = this.getAntigravityMatrixList();
    if (list.length === 0) return false;
    return list.every(item => item[field]);
  },

  toggleAgMatrixColumn(field) {
    const list = this.getAntigravityMatrixList();
    const allChecked = this.isAgMatrixColumnAllChecked(field);

    list.forEach(item => {
      store.antigravityMatrix[item.id][field] = !allChecked;
    });
  },

  toggleAgMatrixRow(modelId) {
    if (!store.antigravityMatrix[modelId]) return;

    const row = store.antigravityMatrix[modelId];
    // 逻辑：如果当前行有任何一项是 true，则全部设为 false；否则全部设为 true
    const hasAnyOn = row.base || row.fakeStream || row.antiTrunc;
    const newState = !hasAnyOn;

    row.base = newState;
    row.fakeStream = newState;
    row.antiTrunc = newState;
  },

  // 导出账号
  async exportAntigravityAccounts() {
    try {
      const response = await fetch('/api/antigravity/accounts/export', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();

      if (data.error) {
        toast.error('导出失败: ' + data.error);
        return;
      }

      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `antigravity-accounts-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);

      toast.success(`已导出 ${data.accounts?.length || 0} 个账号`);
    } catch (error) {
      toast.error('导出失败: ' + error.message);
    }
  },

  // 导入账号
  async importAntigravityAccountsFromFile() {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json';

    input.onchange = async e => {
      const file = e.target.files[0];
      if (!file) return;

      try {
        const text = await file.text();
        const data = JSON.parse(text);

        if (!data.accounts || !Array.isArray(data.accounts)) {
          toast.error('无效的文件格式');
          return;
        }

        store.antigravityLoading = true;
        toast.info(`正在导入 ${data.accounts.length} 个账号，请稍候...`);

        const response = await fetch('/api/antigravity/accounts/import', {
          method: 'POST',
          headers: {
            ...store.getAuthHeaders(),
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ accounts: data.accounts }),
        });

        const result = await response.json();
        if (result.success) {
          toast.success(
            `导入成功: ${result.imported} 个账号${result.skipped > 0 ? `，跳过 ${result.skipped} 个` : ''}`
          );
          this.loadAntigravityAccounts();
        } else {
          toast.error('导入失败: ' + (result.error || '未知错误'));
        }
      } catch (error) {
        toast.error('导入失败: ' + error.message);
      } finally {
        store.antigravityLoading = false;
      }
    };

    input.click();
  },

  // ========== 模型检测历史功能 ==========

  /**
   * 执行模型健康检测
   */
  async runAntigravityModelCheck() {
    store.antigravityChecking = true;
    toast.info('正在检测模型健康状态...');

    // 立即刷新一次，显示 "Waiting..." 状态
    this.loadAntigravityCheckHistory();

    // 开启轮询，实现实时刷新表格
    const pollInterval = setInterval(() => {
      console.log('[Antigravity] Polling check history...', store.antigravityChecking);
      if (store.antigravityChecking) {
        this.loadAntigravityCheckHistory();
      } else {
        console.log('[Antigravity] Polling stopped');
        clearInterval(pollInterval);
      }
    }, 2000);

    try {
      const response = await fetch('/api/antigravity/accounts/check', {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (response.ok && data.success) {
        toast.success(`检测完成: ${data.totalAccounts} 账号, ${data.totalModels} 模型`);
      } else {
        toast.error(data.error || '检测失败');
      }
    } catch (error) {
      toast.error('检测请求失败: ' + error.message);
    } finally {
      store.antigravityChecking = false;
      clearInterval(pollInterval);
      this.loadAntigravityCheckHistory(); // 确保最后刷新一次
    }
  },

  /**
   * 切换定时检测开关
   */
  toggleAntigravityAutoCheck() {
    if (store.antigravityAutoCheck) {
      // 开启定时检测
      this.startAntigravityAutoCheck();
      toast.success(
        `已开启定时检测 (每 ${Math.round(store.antigravityAutoCheckInterval / 60000)} 分钟)`
      );
    } else {
      // 关闭定时检测
      this.stopAntigravityAutoCheck();
      toast.info('已关闭定时检测');
    }
    // 保存设置到后端
    this.saveAntigravityAutoCheckSettings();
  },

  /**
   * 重启定时检测 (间隔变化时)
   */
  restartAntigravityAutoCheck() {
    if (store.antigravityAutoCheck) {
      this.stopAntigravityAutoCheck();
      this.startAntigravityAutoCheck();
      toast.success(
        `定时检测间隔已更新为 ${Math.round(store.antigravityAutoCheckInterval / 60000)} 分钟`
      );
    }
    // 保存设置到后端
    this.saveAntigravityAutoCheckSettings();
  },

  /**
   * 启动定时检测
   * 基于上次执行时间计算剩余等待时间，而不是每次重启都从头开始
   */
  startAntigravityAutoCheck() {
    this.stopAntigravityAutoCheck(); // 确保没有重复定时器

    const interval = Number(store.antigravityAutoCheckInterval);
    const lastRun = store.antigravityAutoCheckLastRun || 0;
    const now = Date.now();
    const elapsed = now - lastRun;

    // 计算剩余等待时间
    let delay = interval - elapsed;
    if (delay < 0 || lastRun === 0) {
      // 如果已经超过间隔或从未运行过，立即执行
      delay = 0;
    }

    console.log(
      `[Antigravity] 定时检测启动: 间隔=${interval / 60000}分钟, 上次=${lastRun ? new Date(lastRun).toLocaleTimeString() : '从未'}, 剩余等待=${Math.round(delay / 60000)}分钟`
    );

    // 首先等待剩余时间
    store.antigravityAutoCheckTimerId = setTimeout(() => {
      // 执行首次检测
      if (!store.antigravityChecking) {
        console.log('[Antigravity] 定时检测触发 (首次/延迟)');
        this.runAntigravityModelCheck();
        store.antigravityAutoCheckLastRun = Date.now();
        this.saveAntigravityAutoCheckSettings();
      }

      // 然后开始正常的间隔循环
      store.antigravityAutoCheckTimerId = setInterval(() => {
        if (!store.antigravityChecking) {
          console.log('[Antigravity] 定时检测触发');
          this.runAntigravityModelCheck();
          store.antigravityAutoCheckLastRun = Date.now();
          this.saveAntigravityAutoCheckSettings();
        }
      }, interval);
    }, delay);
  },

  /**
   * 停止定时检测
   */
  stopAntigravityAutoCheck() {
    if (store.antigravityAutoCheckTimerId) {
      clearInterval(store.antigravityAutoCheckTimerId);
      store.antigravityAutoCheckTimerId = null;
    }
  },

  /**
   * 加载定时检测设置
   */
  async loadAntigravityAutoCheckSettings() {
    try {
      const response = await fetch('/api/antigravity/settings', {
        headers: store.getAuthHeaders(),
      });
      const settings = await response.json();

      // 从设置中恢复定时检测状态
      if (settings.autoCheckEnabled !== undefined) {
        store.antigravityAutoCheck =
          settings.autoCheckEnabled === '1' || settings.autoCheckEnabled === true;
      }
      if (settings.autoCheckInterval !== undefined) {
        store.antigravityAutoCheckInterval = parseInt(settings.autoCheckInterval) || 3600000;
      }
      // 加载禁用模型列表
      if (settings.disabledCheckModels) {
        try {
          store.antigravityDisabledCheckModels = JSON.parse(settings.disabledCheckModels) || [];
        } catch (e) {
          store.antigravityDisabledCheckModels = [];
        }
      }
      // 加载上次执行时间
      if (settings.autoCheckLastRun !== undefined) {
        store.antigravityAutoCheckLastRun = parseInt(settings.autoCheckLastRun) || 0;
      }

      // 如果设置为开启，启动定时器
      if (store.antigravityAutoCheck) {
        this.startAntigravityAutoCheck();
      }
    } catch (error) {
      console.error('加载定时检测设置失败:', error);
    }
  },

  /**
   * 保存定时检测设置
   */
  async saveAntigravityAutoCheckSettings() {
    try {
      await fetch('/api/antigravity/settings', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...store.getAuthHeaders(),
        },
        body: JSON.stringify({
          autoCheckEnabled: store.antigravityAutoCheck ? '1' : '0',
          autoCheckInterval: String(store.antigravityAutoCheckInterval),
          autoCheckLastRun: String(store.antigravityAutoCheckLastRun || 0),
          disabledCheckModels: JSON.stringify(store.antigravityDisabledCheckModels),
        }),
      });
    } catch (error) {
      console.error('保存定时检测设置失败:', error);
    }
  },

  /**
   * 切换模型检测开关
   */
  toggleAntigravityCheckModel(modelId) {
    const idx = store.antigravityDisabledCheckModels.indexOf(modelId);
    if (idx >= 0) {
      // 已禁用，启用它
      store.antigravityDisabledCheckModels.splice(idx, 1);
    } else {
      // 未禁用，禁用它
      store.antigravityDisabledCheckModels.push(modelId);
    }
    // 保存到设置
    this.saveAntigravityAutoCheckSettings();
  },

  /**
   * 加载模型检测历史
   */
  async loadAntigravityCheckHistory() {
    try {
      const response = await fetch('/api/antigravity/models/check-history', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      store.antigravityCheckHistory = data;
    } catch (error) {
      console.error('加载模型检测历史失败:', error);
    }
  },

  /**
   * 清空模型检测历史
   */
  async clearAntigravityCheckHistory() {
    const confirmed = await store.showConfirm({
      title: '确认清空',
      message: '确定要清空所有模型检测历史吗？',
      icon: 'fa-trash',
      confirmText: '清空',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch('/api/antigravity/models/check-history/clear', {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('检测历史已清空');
        store.antigravityCheckHistory = { models: [], times: [], matrix: {} };
      } else {
        toast.error('清空失败');
      }
    } catch (error) {
      toast.error('请求失败: ' + error.message);
    }
  },

  /**
   * 格式化检测时间显示
   */
  formatCheckTime(timestamp) {
    if (!timestamp) return '-';
    const date = new Date(timestamp * 1000);
    return date.toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    });
  },

  /**
   * 获取检测徽章的 CSS 类
   * @param {Object} checkData - 检测数据 {status, passedAccounts, error_log}
   * @param {number} accountIndex - 账号序号 (1-based)
   * @returns {string} CSS 类名
   */
  getCheckBadgeClass(checkData, accountIndex) {
    if (!checkData) return 'check-badge-unknown';

    // 检测中状态
    if (checkData.error_log === 'Waiting...' || checkData.error_log === 'Checking...') {
      return 'check-badge-unknown';
    }

    // 检查是否通过
    const passedList = (checkData.passedAccounts || '').split(',').filter(s => s);
    if (passedList.includes(String(accountIndex))) {
      return 'check-badge-success';
    }

    // 检查错误日志中是否有内容（说明检测完成）
    const errorLog = checkData.error_log || '';
    const checkComplete =
      errorLog.length > 0 && errorLog !== 'Waiting...' && errorLog !== 'Checking...';

    // 只有检测明确完成且当前账号不在通过列表中，才显示失败
    // 检测完成的标志：status 为 ok 或 error，且有错误日志
    if (checkComplete && (checkData.status === 'ok' || checkData.status === 'error')) {
      return 'check-badge-error';
    }

    // 其他情况视为未检测（可能正在检测该账号）
    return 'check-badge-unknown';
  },

  /**
   * 获取检测徽章的标题提示
   */
  getCheckBadgeTitle(checkData, accountIndex) {
    if (!checkData) return '未检测';

    if (checkData.error_log === 'Waiting...' || checkData.error_log === 'Checking...') {
      return `账号 #${accountIndex} 检测中`;
    }

    const passedList = (checkData.passedAccounts || '').split(',').filter(s => s);
    if (passedList.includes(String(accountIndex))) {
      return `账号 #${accountIndex} 通过`;
    }

    if (passedList.length > 0 || checkData.status === 'error') {
      return `账号 #${accountIndex} 失败`;
    }

    return `账号 #${accountIndex} 未检测`;
  },

  /**
   * 获取账号配额显示数据
   */
  getAccountQuotaDisplay(quotas) {
    const DISPLAY_MAP = {
      'gemini-3-pro-high': 'G3 Pro',
      'gemini-3-flash': 'G3 Flash',
      'gemini-3-pro-image': 'G3 Image',
      'claude-sonnet-4-5': 'Claude 4.5',
    };

    const result = [];
    for (const [modelId, label] of Object.entries(DISPLAY_MAP)) {
      if (quotas[modelId]) {
        result.push({
          key: modelId,
          label: label,
          percent: quotas[modelId].percent || 0,
        });
      } else {
        result.push({
          key: modelId,
          label: label,
          percent: 0,
        });
      }
    }
    return result;
  },

  /**
   * 获取配额进度条的背景色
   */
  getQuotaBarColor(percent) {
    if (percent >= 50) return 'rgba(16, 185, 129, 0.15)';
    if (percent >= 20) return 'rgba(245, 158, 11, 0.15)';
    return 'rgba(239, 68, 68, 0.15)';
  },

  /**
   * 获取配额填充进度条的颜色
   */
  getQuotaFillColor(percent) {
    if (percent >= 50) return '#10b981';
    if (percent >= 20) return '#f59e0b';
    return '#ef4444';
  },

  /**
   * 获取配额百分比文字颜色
   */
  getQuotaTextColor(percent) {
    if (percent >= 50) return '#10b981';
    if (percent >= 20) return '#f59e0b';
    return '#ef4444';
  },
};
