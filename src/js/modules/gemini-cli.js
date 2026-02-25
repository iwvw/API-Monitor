/**
 * Gemini CLI API 模块
 */

import { store } from '../store.js';
import { toast } from './toast.js';

export const geminiCliMethods = {
  switchToGeminiCli() {
    store.mainActiveTab = 'gemini-cli';
    if (!store.geminiCliCurrentTab) {
      store.geminiCliCurrentTab = 'models';
    }

    if (store.geminiCliCurrentTab === 'models') {
      this.loadGeminiCliMatrix();
    } else if (store.geminiCliCurrentTab === 'accounts') {
      this.loadGeminiCliAccounts();
    }

    // 加载检测历史和定时设置
    this.loadGeminiCliCheckHistory();
    this.loadGeminiCliAutoCheckSettings();
  },

  // 切换 Gemini CLI 子标签页
  switchGeminiCliTab(tabName) {
    store.geminiCliCurrentTab = tabName;
    if (tabName === 'models') {
      this.loadGeminiCliMatrix();
    } else if (tabName === 'logs') {
      this.loadGeminiCliLogs();
    } else if (tabName === 'settings') {
      this.loadGeminiCliSettings();
      this.loadGeminiCliModelRedirects();
    } else if (tabName === 'accounts') {
      this.loadGeminiCliAccounts();
      this.loadGeminiCliCheckHistory(); // 自动加载检测历史
      this.loadGeminiCliQuotas(); // 自动加载额度信息
    }
  },

  async initGeminiCli() {
    // 确保默认标签页设置
    if (!store.geminiCliCurrentTab) {
      store.geminiCliCurrentTab = 'models';
    }

    // 始终加载矩阵配置（这是首页，必须加载）
    await this.loadGeminiCliMatrix();

    // 后台加载账号列表
    this.loadGeminiCliAccounts();

    // 后台加载检测历史
    this.loadGeminiCliCheckHistory();

    // 加载定时检测设置并启动定时器（如果用户开启了定时检测）
    this.loadGeminiCliAutoCheckSettings();

    // 启动账号列表自动刷新 (用于更新冷却倒计时)
    if (this.gcliAccountTimer) clearInterval(this.gcliAccountTimer);
    this.gcliAccountTimer = setInterval(() => {
      if (store.mainActiveTab === 'gemini-cli' && store.geminiCliCurrentTab === 'accounts') {
        this.loadGeminiCliAccounts();
        this.loadGeminiCliCheckHistory(); // 自动刷新检测历史
      }
    }, 10000);
  },

  // 获取所有模型列表
  getAllGeminiCliModels() {
    const models = [];
    const modelDataMap = store.geminiCliModels || {};

    for (const [modelId, modelData] of Object.entries(modelDataMap)) {
      models.push({
        id: modelId,
        groupIcon: this.getGeminiCliModelGroupIcon(modelId),
        groupName: this.getGeminiCliModelGroupName(modelId),
        remaining: modelData.remaining || 0,
        resetTime: modelData.resetTime || '-',
        enabled: modelData.enabled,
        description: this.getGeminiCliModelDescription(modelId),
      });
    }

    return models.sort((a, b) => {
      // 按 ID 排序，但也考虑分组
      return a.id.localeCompare(b.id);
    });
  },

  // 获取模型分组图标
  getGeminiCliModelGroupIcon(modelId) {
    if (modelId.includes('image') || modelId.includes('vision')) return '🖼️';
    if (modelId.includes('pro')) return '🤖';
    if (modelId.includes('flash')) return '🚀';
    if (modelId.includes('ultra')) return '💎';
    return '🤖';
  },

  // 获取模型分组名称
  getGeminiCliModelGroupName(modelId) {
    if (modelId.includes('image') || modelId.includes('vision')) return '图像生成';
    if (modelId.includes('pro')) return 'Pro 系列';
    if (modelId.includes('flash')) return 'Flash 系列';
    if (modelId.includes('ultra')) return 'Ultra 系列';
    return 'Gemini';
  },

  // 获取模型描述
  getGeminiCliModelDescription(modelId) {
    if (modelId.includes('pro')) return '适用于复杂推理任务';
    if (modelId.includes('flash')) return '快速且经济高效';
    if (modelId.includes('vision') || modelId.includes('image')) return '多模态视觉能力';
    return '通用语言模型';
  },

  // 获取额度进度条颜色
  getGeminiCliQuotaColor(percent) {
    if (percent > 80) return '#ef4444'; // 危险
    if (percent > 50) return '#f59e0b'; // 警告
    return '#10b981'; // 正常
  },

  // 获取日志状态码对应的 CSS 类
  getGcliStatusClass(code) {
    if (!code) return 'ag-status-unknown';
    if (code >= 200 && code < 300) return 'ag-status-success';
    if (code === 429) return 'ag-status-warning';
    if (code >= 400) return 'ag-status-danger';
    return 'ag-status-unknown';
  },

  // 格式化冻结状态的 tooltip 文本
  formatCoolDownTitle(coolDowns) {
    if (!coolDowns || coolDowns.length === 0) return '';
    return coolDowns
      .map(c => {
        const time = new Date(c.resetTime).toLocaleTimeString();
        return `${c.model} 至 ${time}`;
      })
      .join(', ');
  },

  // 获取过滤后的 Gemini CLI 日志
  getFilteredGeminiCliLogs() {
    let logs = store.geminiCliLogs || [];

    if (store.geminiCliLogFilterAccount) {
      logs = logs.filter(log => log.accountId === store.geminiCliLogFilterAccount);
    }

    if (store.geminiCliLogFilterModel) {
      logs = logs.filter(log => log.model === store.geminiCliLogFilterModel);
    }

    return logs;
  },

  // 获取日志中所有出现的模型列表（用于筛选下拉框）
  getGeminiCliLogModels() {
    const models = new Set();
    (store.geminiCliLogs || []).forEach(log => {
      if (log.model) models.add(log.model);
    });
    return Array.from(models).sort();
  },

  // 加载模型矩阵配置
  async loadGeminiCliMatrix() {
    store.geminiCliModelLoading = true;
    try {
      const response = await fetch('/api/gemini-cli/config/matrix', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      store.geminiCliMatrix = data;
      // 只有在非初始化情况下才提示？或者始终提示？
      // 用户要求“刷新结果给出反馈”，这里加上成功提示
      if (store.mainActiveTab === 'gemini-cli' && store.geminiCliCurrentTab === 'models') {
        toast.success('矩阵配置已刷新');
      }
    } catch (error) {
      console.error('加载模型矩阵失败:', error);
      toast.error('加载配置失败');
    } finally {
      store.geminiCliModelLoading = false;
    }
  },

  // 保存模型矩阵配置
  async saveGeminiCliMatrix() {
    try {
      const response = await fetch('/api/gemini-cli/config/matrix', {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(store.geminiCliMatrix),
      });

      if (response.ok) {
        toast.success('配置已保存 (API 模型列表已更新)');
      } else {
        toast.error('保存失败');
      }
    } catch (error) {
      toast.error('保存失败: ' + error.message);
    }
  },

  // 切换矩阵中的单个开关
  toggleMatrixItem(modelId, field) {
    if (!store.geminiCliMatrix[modelId]) return;
    store.geminiCliMatrix[modelId][field] = !store.geminiCliMatrix[modelId][field];
    this.saveGeminiCliMatrix();
  },

  // 检查某列是否全选
  isMatrixColumnAllChecked(field) {
    if (!store.geminiCliMatrix) return false;
    const keys = Object.keys(store.geminiCliMatrix);
    if (keys.length === 0) return false;
    return keys.every(key => store.geminiCliMatrix[key][field]);
  },

  // 切换整列开关
  toggleMatrixColumn(field) {
    if (!store.geminiCliMatrix) return;
    const isAllChecked = this.isMatrixColumnAllChecked(field);
    const newValue = !isAllChecked;

    Object.keys(store.geminiCliMatrix).forEach(key => {
      store.geminiCliMatrix[key][field] = newValue;
    });
    this.saveGeminiCliMatrix();
  },

  toggleGeminiCliMatrixRow(modelId) {
    if (!store.geminiCliMatrix[modelId]) return;

    const row = store.geminiCliMatrix[modelId];
    // 逻辑：如果当前行有任何一项是 true，则全部设为 false；否则全部设为 true
    const fields = ['base', 'maxThinking', 'noThinking', 'search', 'fakeStream', 'antiTrunc'];
    const hasAnyOn = fields.some(f => row[f]);
    const newState = !hasAnyOn;

    fields.forEach(f => {
      if (row[f] !== undefined) row[f] = newState;
    });

    this.saveGeminiCliMatrix();
  },

  // 获取有序的矩阵数据列表
  getGeminiCliMatrixList() {
    if (!store.geminiCliMatrix) return [];
    // 定义核心模型的显示顺序
    const order = [
      'gemini-2.5-pro',
      'gemini-2.5-flash',
      'gemini-3.1-pro-preview',
      'gemini-3-pro-preview',
      'gemini-3-flash-preview',
    ];

    // 也可以包含其他扩展模型
    const allKeys = Object.keys(store.geminiCliMatrix);
    const sortedKeys = allKeys.sort((a, b) => {
      const idxA = order.indexOf(a);
      const idxB = order.indexOf(b);
      if (idxA !== -1 && idxB !== -1) return idxA - idxB;
      if (idxA !== -1) return -1;
      if (idxB !== -1) return 1;
      return a.localeCompare(b);
    });

    return sortedKeys.map(key => ({
      id: key,
      ...store.geminiCliMatrix[key],
    }));
  },

  async loadGeminiCliStats() {
    try {
      const response = await fetch('/api/gemini-cli/stats', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      store.geminiCliStats = data;
    } catch (error) {
      console.error('加载 Gemini CLI 统计失败:', error);
    }
  },

  async loadGeminiCliLogs() {
    store.geminiCliLoading = true;
    try {
      const response = await fetch('/api/gemini-cli/logs', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        store.geminiCliLogs = data;
        if (store.mainActiveTab === 'gemini-cli' && store.geminiCliCurrentTab === 'logs') {
          toast.success('调用日志已更新');
        }
      }
    } catch (error) {
      toast.error('加载日志失败');
    } finally {
      store.geminiCliLoading = false;
    }
  },

  async viewGeminiCliLogDetail(log) {
    try {
      const response = await fetch(`/api/gemini-cli/logs/${log.id}`, {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();

      // 标准化数据结构以适配 Antigravity 模板 (与 Antigravity 格式一致)
      if (data) {
        // 1. 顶层字段映射（数据库字段 -> 驼峰命名）
        // getLogDetail 返回的是数据库原始格式，需要映射
        data.timestamp = data.timestamp || data.created_at;
        data.durationMs = data.durationMs || data.duration_ms;
        data.statusCode = data.statusCode || data.status_code;
        data.accountId = data.accountId || data.account_id;
        data.path = data.path || data.request_path || '/v1/chat/completions';
        data.method = data.method || data.request_method || 'POST';
        data.clientIp = data.clientIp || data.client_ip;
        data.userAgent = data.userAgent || data.user_agent;

        // 2. Detail 对象标准
        if (data.detail) {
          // Case A: 已经是 OpenAI 格式 (直接透传的请求)
          // 需要将 detail.request.messages 提升为 detail.messages 以匹配模板
          if (data.detail.request && data.detail.request.messages && !data.detail.messages) {
            data.detail.messages = data.detail.request.messages;
          }

          // Case B: Google 格式 (contents) -> OpenAI 格式 (messages)
          if (data.detail.request && data.detail.request.contents && !data.detail.messages) {
            data.detail.messages = data.detail.request.contents.map(c => ({
              role: c.role === 'model' ? 'assistant' : c.role,
              content: c.parts ? c.parts.map(p => p.text).join('') : '',
            }));
          }

          // 处理 Response: candidates -> choices
          // 如果是流式请求 (type: stream)，可能没有完整的 response 对象，或者 response 是空
          if (
            data.detail.response &&
            data.detail.response.candidates &&
            !data.detail.response.choices
          ) {
            data.detail.response.choices = data.detail.response.candidates.map(c => ({
              message: {
                role: 'assistant',
                content:
                  c.content && c.content.parts ? c.content.parts.map(p => p.text).join('') : '',
                reasoning_content: null,
              },
            }));
          }
        }
      }

      store.gcliLogDetailShowRaw = false;
      store.geminiCliLogDetail = data;
      store.showGeminiCliLogDetailModal = true;
    } catch (error) {
      toast.error('加载日志详情失败');
      console.error(error);
    }
  },

  async clearGeminiCliLogs() {
    const confirmed = await store.showConfirm({
      title: '确认清空',
      message: '确定要清空所有 Gemini CLI 调用日志吗？',
      icon: 'fa-trash',
      confirmText: '清空',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch('/api/gemini-cli/logs', {
        method: 'DELETE',
        headers: store.getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('日志已清空');
        store.geminiCliLogs = [];
      }
    } catch (error) {
      toast.error('清空失败');
    }
  },

  async loadGeminiCliSettings() {
    store.geminiCliLoading = true;
    try {
      const response = await fetch('/api/gemini-cli/settings', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      store.geminiCliSettings = data;
      this.geminiCliSettingsForm = { ...data };
      if (store.mainActiveTab === 'gemini-cli' && store.geminiCliCurrentTab === 'settings') {
        toast.success('模块设置已从服务器同步');
      }
    } catch (error) {
      toast.error('加载设置失败');
    } finally {
      store.geminiCliLoading = false;
    }
  },

  async saveGeminiCliSettings() {
    store.geminiCliSaving = true;
    try {
      const response = await fetch('/api/gemini-cli/settings', {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(this.geminiCliSettingsForm),
      });

      if (response.ok) {
        toast.success('设置已保存');
        await this.loadGeminiCliSettings();
      } else {
        toast.error('保存设置失败');
      }
    } catch (error) {
      toast.error('保存设置失败: ' + error.message);
    } finally {
      store.geminiCliSaving = false;
    }
  },

  async loadGeminiCliAccounts() {
    store.geminiCliLoading = true;
    try {
      const response = await fetch('/api/gemini-cli/accounts', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        store.geminiCliAccounts = data;
        // 加载完账号后刷新一遍统计
        this.loadGeminiCliStats();

        if (store.mainActiveTab === 'gemini-cli' && store.geminiCliCurrentTab === 'accounts') {
          toast.success('账号列表已刷新');
        }
      }
    } catch (error) {
      console.error('加载 Gemini CLI 账号失败:', error);
      toast.error('加载账号失败');
    } finally {
      store.geminiCliLoading = false;
    }
  },

  async refreshGeminiCliAccounts() {
    store.geminiCliLoading = true;
    toast.info('正在刷新所有账号及邮箱信息...');
    try {
      const response = await fetch('/api/gemini-cli/accounts/refresh', {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (response.ok) {
        toast.success(`刷新完成: 成功 ${data.refreshed}, 失败 ${data.failed}`);

        // 如果后端返回了最新的账号列表，直接更新 store
        if (Array.isArray(data.accounts)) {
          store.geminiCliAccounts = data.accounts;
          // 同时刷新统计
          this.loadGeminiCliStats();
        } else {
          // 降级回退
          await this.loadGeminiCliAccounts();
        }
      } else {
        toast.error(data.error || '刷新失败');
      }
    } catch (error) {
      toast.error('刷新请求失败: ' + error.message);
    } finally {
      store.geminiCliLoading = false;
    }
  },

  openAddGeminiCliAccountModal() {
    this.geminiCliEditingAccount = null;
    this.geminiCliAccountForm = {
      name: '',
      client_id: '',
      client_secret: '',
      refresh_token: '',
      project_id: '',
    };
    this.geminiCliAccountFormError = '';
    store.showGeminiCliAccountModal = true;
  },

  async saveGeminiCliAccount() {
    if (
      !this.geminiCliAccountForm.name ||
      !this.geminiCliAccountForm.client_id ||
      !this.geminiCliAccountForm.refresh_token
    ) {
      this.geminiCliAccountFormError = '请填写必填项';
      return;
    }

    store.geminiCliSaving = true;
    try {
      const isEditing = store.geminiCliEditingAccount !== null;
      const url = isEditing
        ? `/api/gemini-cli/accounts/${store.geminiCliEditingAccount.id}`
        : '/api/gemini-cli/accounts';

      const response = await fetch(url, {
        method: isEditing ? 'PUT' : 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(this.geminiCliAccountForm),
      });

      if (response.ok) {
        toast.success(isEditing ? '账号已更新' : '账号已保存');
        store.showGeminiCliAccountModal = false;
        store.geminiCliEditingAccount = null;
        this.loadGeminiCliAccounts();
      } else {
        const data = await response.json();
        this.geminiCliAccountFormError = data.error || '保存失败';
      }
    } catch (error) {
      this.geminiCliAccountFormError = '保存失败: ' + error.message;
    } finally {
      store.geminiCliSaving = false;
    }
  },

  // 手动获取邮箱
  async fetchGeminiCliEmail() {
    if (
      !this.geminiCliAccountForm.client_id ||
      !this.geminiCliAccountForm.client_secret ||
      !this.geminiCliAccountForm.refresh_token
    ) {
      toast.error('请先填写 Client ID、Client Secret 和 Refresh Token');
      return;
    }

    store.geminiCliLoading = true;
    try {
      const response = await fetch('/api/gemini-cli/accounts/fetch-email', {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          client_id: this.geminiCliAccountForm.client_id,
          client_secret: this.geminiCliAccountForm.client_secret,
          refresh_token: this.geminiCliAccountForm.refresh_token,
        }),
      });

      const result = await response.json();
      if (response.ok && result.email) {
        this.geminiCliAccountForm.email = result.email;
        toast.success(`已获取邮箱: ${result.email}`);
      } else {
        toast.error(result.error || '获取邮箱失败');
      }
    } catch (error) {
      toast.error('获取邮箱失败: ' + error.message);
    } finally {
      store.geminiCliLoading = false;
    }
  },

  async deleteGeminiCliAccount(account) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除账号 "${account.name}" 吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(`/api/gemini-cli/accounts/${account.id}`, {
        method: 'DELETE',
        headers: store.getAuthHeaders(),
      });

      if (response.ok) {
        toast.success('账号已删除');
        this.loadGeminiCliAccounts();
      } else {
        toast.error('删除失败');
      }
    } catch (error) {
      toast.error('删除失败: ' + error.message);
    }
  },

  openGeminiCliAuthUrl() {
    const clientId = store.geminiCliCustomClientId;
    const redirectUri = encodeURIComponent(store.geminiCliOAuthRedirectUri);
    const scope = encodeURIComponent(
      'https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile'
    );
    const state = `111_${Math.random().toString(36).slice(2)}`;

    const url = `https://accounts.google.com/o/oauth2/auth?client_id=${clientId}&redirect_uri=${redirectUri}&scope=${scope}&response_type=code&access_type=offline&prompt=consent&include_granted_scopes=true&state=${state}`;

    window.open(url, '_blank');
    store.geminiCliOAuthUrl = url;
  },

  async parseGeminiCliOauthUrl() {
    if (!store.geminiCliOauthReturnUrl) {
      toast.error('请先粘贴回调 URL');
      return;
    }

    let url;
    try {
      url = new URL(store.geminiCliOauthReturnUrl);
    } catch (e) {
      toast.error('无效的 URL 格式');
      return;
    }

    const code = url.searchParams.get('code');
    if (!code) {
      toast.error('URL 中未找到授权码 (code)');
      return;
    }

    store.geminiCliLoading = true;
    try {
      const response = await fetch('/api/gemini-cli/oauth/exchange', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify({
          code,
          redirect_uri: store.geminiCliOAuthRedirectUri,
          client_id: store.geminiCliCustomClientId,
          client_secret: store.geminiCliCustomClientSecret,
          project_id: store.geminiCliCustomProjectId || undefined,
        }),
      });

      const result = await response.json();
      if (response.ok) {
        // 自动填充表单并保存
        this.geminiCliAccountForm = {
          name: `Gemini Project ${result.project_id || 'Auto'}`,
          email: result.email || '',
          client_id: store.geminiCliCustomClientId,
          client_secret: store.geminiCliCustomClientSecret,
          refresh_token: result.refresh_token,
          project_id: result.project_id,
        };

        // 执行保存
        await this.saveGeminiCliAccount();
        store.showGeminiCliOAuthExpand = false;
        store.geminiCliOauthReturnUrl = '';
        store.geminiCliCustomProjectId = '';
        toast.success('OAuth 认证成功并已保存账号');
      } else {
        toast.error(result.error || '交换 Token 失败');
      }
    } catch (error) {
      console.error('OAuth 交换失败:', error);
      toast.error('请求失败: ' + error.message);
    } finally {
      store.geminiCliLoading = false;
    }
  },

  // 编辑账号
  editGeminiCliAccount(account) {
    store.geminiCliEditingAccount = account;
    this.geminiCliAccountForm = {
      name: account.name || '',
      client_id: account.client_id || '',
      client_secret: account.client_secret || '',
      refresh_token: account.refresh_token || '',
      project_id: account.project_id || '',
    };
    this.geminiCliAccountFormError = '';
    store.showGeminiCliAccountModal = true;
  },

  // 切换账号启用状态
  async toggleGeminiCliAccount(account) {
    try {
      const response = await fetch(`/api/gemini-cli/accounts/${account.id}/toggle`, {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });

      if (response.ok) {
        toast.success(account.enable ? '账号已禁用' : '账号已启用');
        this.loadGeminiCliAccounts();
      } else {
        toast.error('操作失败');
      }
    } catch (error) {
      toast.error('操作失败: ' + error.message);
    }
  },

  // Model Redirect Management
  async loadGeminiCliModelRedirects() {
    try {
      const response = await fetch('/api/gemini-cli/models/redirects', {
        headers: store.getAuthHeaders(),
      });
      store.geminiCliModelRedirects = await response.json();
    } catch (error) {
      toast.error('加载重定向配置失败');
    }
  },

  async addGeminiCliModelRedirect(sourceModel, targetModel) {
    if (!sourceModel || !targetModel) {
      toast.error('请填写源模型和目标模型');
      return;
    }
    try {
      // 如果是编辑模式，且修改了源模型名称（主键变了），则需要先删除旧的
      if (store.gcliEditingRedirectSource && store.gcliEditingRedirectSource !== sourceModel) {
        await fetch(
          `/api/gemini-cli/models/redirects/${encodeURIComponent(store.gcliEditingRedirectSource)}`,
          {
            method: 'DELETE',
            headers: store.getAuthHeaders(),
          }
        );
      }

      const response = await fetch('/api/gemini-cli/models/redirects', {
        method: 'POST',
        headers: {
          ...store.getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ sourceModel, targetModel }),
      });

      if (response.ok) {
        toast.success('操作成功');
        store.newGeminiCliRedirectSource = '';
        store.newGeminiCliRedirectTarget = '';
        store.gcliEditingRedirectSource = null;
        await this.loadGeminiCliModelRedirects();
        return true;
      } else {
        const data = await response.json();
        toast.error('操作失败: ' + (data.error || '未知错误'));
        return false;
      }
    } catch (error) {
      toast.error('请求失败: ' + error.message);
      return false;
    }
  },

  editGeminiCliModelRedirect(r) {
    store.newGeminiCliRedirectSource = r.source_model;
    store.newGeminiCliRedirectTarget = r.target_model;
    store.gcliEditingRedirectSource = r.source_model;
  },

  async removeGeminiCliModelRedirect(sourceModel) {
    const confirmed = await store.showConfirm({
      title: '确认删除',
      message: `确定要删除 ${sourceModel} 的重定向吗？`,
      icon: 'fa-trash',
      confirmText: '删除',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch(
        `/api/gemini-cli/models/redirects/${encodeURIComponent(sourceModel)}`,
        {
          method: 'DELETE',
          headers: store.getAuthHeaders(),
        }
      );

      if (response.ok) {
        toast.success('删除成功');
        await this.loadGeminiCliModelRedirects();
      } else {
        toast.error('删除失败');
      }
    } catch (error) {
      toast.error('请求失败: ' + error.message);
    }
  },

  // 导出账号
  async exportGeminiCliAccounts() {
    try {
      const response = await fetch('/api/gemini-cli/accounts/export', {
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
      a.download = `gemini-cli-accounts-${new Date().toISOString().slice(0, 10)}.json`;
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
  async importGeminiCliAccountsFromFile() {
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

        store.geminiCliLoading = true;
        const response = await fetch('/api/gemini-cli/accounts/import', {
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
          this.loadGeminiCliAccounts();
        } else {
          toast.error('导入失败: ' + (result.error || '未知错误'));
        }
      } catch (error) {
        toast.error('导入失败: ' + error.message);
      } finally {
        store.geminiCliLoading = false;
      }
    };

    input.click();
  },

  // 执行模型健康检测 (旧版，保留或删除)
  async checkGeminiCliAccounts() {
    store.geminiCliCheckLoading = true;
    toast.info('正在检测模型健康状态...');
    try {
      const response = await fetch('/api/gemini-cli/accounts/check', {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (response.ok) {
        toast.success('检测完成');
        await this.loadGeminiCliCheckHistory();
      } else {
        toast.error(data.error || '检测失败');
      }
    } catch (error) {
      toast.error('检测请求失败: ' + error.message);
    } finally {
      store.geminiCliCheckLoading = false;
    }
  },

  // ========== 模型检测历史功能 ==========

  /**
   * 执行模型健康检测
   */
  async runGeminiCliModelCheck() {
    store.geminiCliChecking = true;
    toast.info('正在检测模型健康状态..');

    // 开启轮询，实现实时刷新表格
    const pollInterval = setInterval(() => {
      if (store.geminiCliChecking) {
        this.loadGeminiCliCheckHistory();
      } else {
        clearInterval(pollInterval);
      }
    }, 2000);

    try {
      const response = await fetch('/api/gemini-cli/accounts/check', {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (response.ok && data.success) {
        toast.success('检测完成');
      } else {
        toast.error(data.error || '检测失败');
      }
    } catch (error) {
      toast.error('检测请求失败: ' + error.message);
    } finally {
      store.geminiCliChecking = false;
      clearInterval(pollInterval);
      this.loadGeminiCliCheckHistory(); // 确保最后刷新一次
    }
  },

  /**
   * 切换定时检测开关
   * 后端完全控制定时器，前端只负责修改设置
   */
  toggleGeminiCliAutoCheck() {
    // 保存设置到后端，后端会自动重启定时器
    this.saveGeminiCliAutoCheckSettings();
    if (store.geminiCliAutoCheck) {
      toast.success(
        `已开启定时检测 (每 ${Math.round(store.geminiCliAutoCheckInterval / 60000)} 分钟)`
      );
    } else {
      toast.info('已关闭定时检测');
    }
  },

  /**
   * 重启定时检测 (间隔变化时)
   * 后端完全控制定时器，前端只负责修改设置
   */
  restartGeminiCliAutoCheck() {
    // 保存设置到后端，后端会自动重启定时器
    this.saveGeminiCliAutoCheckSettings();
    if (store.geminiCliAutoCheck) {
      toast.success(
        `定时检测间隔已更新为 ${Math.round(store.geminiCliAutoCheckInterval / 60000)} 分钟`
      );
    }
  },

  /**
   * 加载定时检测设置
   */
  async loadGeminiCliAutoCheckSettings() {
    try {
      const response = await fetch('/api/gemini-cli/settings', {
        headers: store.getAuthHeaders(),
      });
      const settings = await response.json();

      if (settings.autoCheckEnabled !== undefined) {
        store.geminiCliAutoCheck =
          settings.autoCheckEnabled === '1' || settings.autoCheckEnabled === true;
      }
      if (settings.autoCheckInterval !== undefined) {
        store.geminiCliAutoCheckInterval = parseInt(settings.autoCheckInterval) || 3600000;
      }
      // 加载禁用模型列表
      if (settings.disabledCheckModels) {
        try {
          store.geminiCliDisabledCheckModels = JSON.parse(settings.disabledCheckModels) || [];
        } catch (e) {
          store.geminiCliDisabledCheckModels = [];
        }
      }

      // 后端定时器状态已包含在 _autoCheckStatus 中
      if (settings._autoCheckStatus) {
        store.geminiCliAutoCheckStatus = settings._autoCheckStatus;
      }

      console.log('[Gemini CLI] 定时检测设置:', {
        enabled: store.geminiCliAutoCheck,
        interval: store.geminiCliAutoCheckInterval,
        intervalMinutes: Math.round(store.geminiCliAutoCheckInterval / 60000),
        backendRunning: settings._autoCheckStatus?.running,
      });
    } catch (error) {
      console.error('加载 Gemini CLI 定时检测设置失败:', error);
    }
  },

  /**
   * 保存定时检测设置
   * 后端会在收到设置后自动重启定时器
   */
  async saveGeminiCliAutoCheckSettings() {
    try {
      await fetch('/api/gemini-cli/settings', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...store.getAuthHeaders(),
        },
        body: JSON.stringify({
          autoCheckEnabled: store.geminiCliAutoCheck ? '1' : '0',
          autoCheckInterval: String(store.geminiCliAutoCheckInterval),
          disabledCheckModels: JSON.stringify(store.geminiCliDisabledCheckModels),
        }),
      });
      // 保存后刷新设置以获取最新的后端状态
      this.loadGeminiCliAutoCheckSettings();
    } catch (error) {
      console.error('保存 Gemini CLI 定时检测设置失败:', error);
    }
  },

  /**
   * 切换模型检测开关
   */
  toggleGeminiCliCheckModel(modelId) {
    const idx = store.geminiCliDisabledCheckModels.indexOf(modelId);
    if (idx >= 0) {
      store.geminiCliDisabledCheckModels.splice(idx, 1);
    } else {
      store.geminiCliDisabledCheckModels.push(modelId);
    }
    this.saveGeminiCliAutoCheckSettings();
  },

  /**
   * 加载模型检测历史
   */
  async loadGeminiCliCheckHistory() {
    try {
      const response = await fetch('/api/gemini-cli/models/check-history', {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      store.geminiCliCheckHistory = data;
    } catch (error) {
      console.error('加载 Gemini CLI 模型检测历史失败', error);
    }
  },

  /**
   * 清空模型检测历史
   */
  async clearGeminiCliCheckHistory() {
    const confirmed = await store.showConfirm({
      title: '确认清空',
      message: '确定要清空所有模型检测历史吗？',
      icon: 'fa-trash',
      confirmText: '清空',
      confirmClass: 'btn-danger',
    });

    if (!confirmed) return;

    try {
      const response = await fetch('/api/gemini-cli/models/check-history/clear', {
        method: 'POST',
        headers: store.getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('检测历史已清空');
        store.geminiCliCheckHistory = { models: [], times: [], matrix: {} };
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
  formatGeminiCliCheckTime(timestamp) {
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
   */
  getGeminiCliCheckBadgeClass(checkData, accountIndex) {
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
    if (checkComplete && (checkData.status === 'ok' || checkData.status === 'error')) {
      return 'check-badge-error';
    }

    return 'check-badge-unknown';
  },

  /**
   * 获取检测徽章的标题提示
   */
  getGeminiCliCheckBadgeTitle(checkData, accountIndex) {
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

  // ========== 额度查询功能 ==========

  /**
   * 加载所有账号的额度信息
   */
  async loadGeminiCliQuotas(forceRefresh = false) {
    store.geminiCliQuotaLoading = true;
    try {
      const url = `/api/gemini-cli/quotas/all${forceRefresh ? '?refresh=1' : ''}`;
      const response = await fetch(url, {
        headers: store.getAuthHeaders(),
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        store.geminiCliQuotaData = data.filter(d => d && d.buckets);

        // 提取所有模型 ID 并排序
        const modelSet = new Set();
        store.geminiCliQuotaData.forEach(q => {
          (q.buckets || []).forEach(b => {
            if (b.modelId !== 'gemini-2.0-flash') {
              modelSet.add(b.modelId);
            }
          });
        });
        store.geminiCliQuotaModels = Array.from(modelSet).sort();

        if (forceRefresh) {
          toast.success(`额度查询完成 (${store.geminiCliQuotaData.length} 个账号)`);
        }
      }
    } catch (error) {
      console.error('加载 Gemini CLI 额度失败:', error);
      toast.error('额度查询失败: ' + error.message);
    } finally {
      store.geminiCliQuotaLoading = false;
    }
  },

  /**
   * 获取指定账号的指定模型的 bucket 数据
   */
  getQuotaBucket(quotaData, modelId) {
    return quotaData?.buckets?.find(b => b.modelId === modelId) || null;
  },

  /**
   * 获取额度进度条颜色
   */
  getQuotaBarColor(fraction) {
    if (fraction == null) return 'var(--text-tertiary)';
    const pct = fraction * 100;
    if (pct >= 70) return '#10b981'; // 绿色
    if (pct >= 40) return '#f59e0b'; // 黄色
    if (pct >= 15) return '#f97316'; // 橙色
    return '#ef4444'; // 红色
  },

  /**
   * 格式化额度重置时间
   */
  formatQuotaResetTime(resetTime) {
    if (!resetTime) return '未知';
    const reset = new Date(resetTime);
    const now = new Date();
    const diffMs = reset - now;

    if (diffMs <= 0) return '已重置';

    const hours = Math.floor(diffMs / 3600000);
    const minutes = Math.floor((diffMs % 3600000) / 60000);

    if (hours > 0) return `${hours}h ${minutes}m 后重置`;
    return `${minutes}m 后重置`;
  },

  /**
   * 判定额度是否处于冷却期
   */
  isQuotaInCooldown(bucket) {
    if (!bucket) return false;
    if (bucket.remainingFraction > 0) return false;
    if (!bucket.resetTime) return false;

    const reset = new Date(bucket.resetTime);
    return reset > new Date();
  },
};
