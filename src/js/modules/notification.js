/**
 * 通知管理模块
 * 负责通知渠道、告警规则、通知历史管理
 */

/**
 * 通知数据对象
 */
export const notificationData = {
  // 通知渠道列表
  notificationChannels: [],

  // 告警规则列表
  notificationRules: [],

  // 通知历史
  notificationHistory: [],

  // UI 状态
  notificationCurrentTab: 'channels', // 'channels' | 'rules' | 'history'
  notificationLoading: false,
  notificationSaving: false,

  // 筛选
  notificationRuleFilter: '',
  notificationHistoryFilter: '',

  // 弹窗
  showChannelModal: false,
  showRuleModal: false,

  // 渠道表单
  channelForm: {
    id: null,
    name: '',
    type: 'email',
    enabled: true,
    config: {
      // Email 配置
      host: '',
      port: 587,
      secure: false,
      auth: { user: '', pass: '' },
      to: '',
      // Telegram 配置
      bot_token: '',
      chat_id: '',
    },
  },

  // 规则表单
  ruleForm: {
    id: null,
    name: '',
    source_module: 'uptime',
    event_type: 'down',
    severity: 'warning',
    channels: [],
    conditions: {},
    suppression: {
      repeat_count: 1,
      silence_minutes: 30,
    },
    time_window: { enabled: false },
    description: '',
    enabled: true,
  },
};

/**
 * 通知方法对象
 */
export const notificationMethods = {
  // ==================== 初始化 ====================

  /**
   * 初始化通知模块
   */
  initNotificationModule() {
    this.loadNotificationChannels();
    this.loadNotificationRules();
    this.loadNotificationHistory();
  },

  // ==================== 数据加载 ====================

  /**
   * 加载通知渠道
   */
  async loadNotificationChannels() {
    this.notificationLoading = true;
    try {
      const res = await fetch('/api/notification/channels');
      const data = await res.json();
      if (data.success) {
        this.notificationChannels = data.data;
      }
    } catch (error) {
      console.error('[Notification] Failed to load channels:', error);
      this.showGlobalToast('加载渠道失败', 'error');
    } finally {
      this.notificationLoading = false;
    }
  },

  /**
   * 加载告警规则
   */
  async loadNotificationRules() {
    this.notificationLoading = true;
    try {
      const res = await fetch('/api/notification/rules');
      const data = await res.json();
      if (data.success) {
        this.notificationRules = data.data;
      }
    } catch (error) {
      console.error('[Notification] Failed to load rules:', error);
      this.showGlobalToast('加载规则失败', 'error');
    } finally {
      this.notificationLoading = false;
    }
  },

  /**
   * 加载通知历史
   */
  async loadNotificationHistory() {
    this.notificationLoading = true;
    try {
      const res = await fetch('/api/notification/history?limit=100');
      const data = await res.json();
      if (data.success) {
        this.notificationHistory = data.data;
      }
    } catch (error) {
      console.error('[Notification] Failed to load history:', error);
      this.showGlobalToast('加载历史失败', 'error');
    } finally {
      this.notificationLoading = false;
    }
  },

  // ==================== 渠道管理 ====================

  /**
   * 显示添加渠道弹窗
   */
  showAddChannelModal() {
    this.channelForm = {
      id: null,
      name: '',
      type: 'email',
      enabled: true,
      config: {
        host: 'smtp.gmail.com',
        port: 465,
        secure: true,
        auth: { user: '', pass: '' },
        sender_name: '',
        to: '',
        bot_token: '',
        chat_id: '',
      },
    };
    this.showChannelModal = true;
  },

  /**
   * 编辑渠道
   */
  editNotificationChannel(channel) {
    // 基础配置模板
    const defaultConfig = {
      host: '',
      port: 465,
      secure: true,
      auth: { user: '', pass: '' },
      sender_name: '',
      to: '',
      bot_token: '',
      chat_id: '',
    };

    // 解析并合并原有配置
    let config = { ...defaultConfig };
    try {
      const sourceConfig = typeof channel.config === 'string'
        ? JSON.parse(channel.config)
        : (channel.config || {});

      // 递归处理嵌套的 auth 对象
      config = { ...config, ...sourceConfig };
      if (sourceConfig.auth) {
        config.auth = { ...config.auth, ...sourceConfig.auth };
      }
    } catch (e) {
      console.warn('[Notification] Failed to parse channel config:', e);
    }

    this.channelForm = {
      id: channel.id,
      name: channel.name,
      type: channel.type,
      enabled: !!channel.enabled,
      config: config,
    };
    this.showChannelModal = true;
  },

  /**
   * 保存渠道
   */
  async saveChannel() {
    if (!this.channelForm.name || !this.channelForm.type) {
      this.showGlobalToast('请填写必要信息', 'error');
      return;
    }

    // 验证配置
    const config = this.channelForm.config;
    if (this.channelForm.type === 'email') {
      if (!config.host || !config.auth.user || !config.auth.pass) {
        this.showGlobalToast('请填写完整的 Email 配置', 'error');
        return;
      }
    } else if (this.channelForm.type === 'telegram') {
      if (!config.bot_token || !config.chat_id) {
        this.showGlobalToast('请填写完整的 Telegram 配置', 'error');
        return;
      }
    }

    this.notificationSaving = true;
    try {
      const url = this.channelForm.id
        ? `/api/notification/channels/${this.channelForm.id}`
        : '/api/notification/channels';
      const method = this.channelForm.id ? 'PUT' : 'POST';

      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(this.channelForm),
      });

      const data = await res.json();
      if (data.success) {
        this.showGlobalToast('渠道保存成功', 'success');
        this.showChannelModal = false;
        await this.loadNotificationChannels();
      } else {
        this.showGlobalToast(data.error || '保存失败', 'error');
      }
    } catch (error) {
      console.error('[Notification] Failed to save channel:', error);
      this.showGlobalToast('保存失败', 'error');
    } finally {
      this.notificationSaving = false;
    }
  },

  /**
   * 删除渠道
   */
  async deleteNotificationChannel(channelId) {
    if (!confirm('确定要删除此渠道吗?')) return;

    try {
      const res = await fetch(`/api/notification/channels/${channelId}`, {
        method: 'DELETE',
      });
      const data = await res.json();
      if (data.success) {
        this.showGlobalToast('渠道已删除', 'success');
        await this.loadNotificationChannels();
      } else {
        this.showGlobalToast(data.error || '删除失败', 'error');
      }
    } catch (error) {
      console.error('[Notification] Failed to delete channel:', error);
      this.showGlobalToast('删除失败', 'error');
    }
  },

  /**
   * 测试渠道
   */
  async testNotificationChannel(channelId) {
    try {
      const res = await fetch(`/api/notification/channels/${channelId}/test`, {
        method: 'POST',
      });
      const data = await res.json();
      if (data.success) {
        this.showGlobalToast('测试消息已发送,请检查接收', 'success');
      } else {
        this.showGlobalToast(data.error || '测试失败', 'error');
      }
    } catch (error) {
      console.error('[Notification] Failed to test channel:', error);
      this.showGlobalToast('测试失败', 'error');
    }
  },

  // ==================== 规则管理 ====================

  /**
   * 显示添加规则弹窗
   */
  showAddRuleModal() {
    this.ruleForm = {
      id: null,
      name: '',
      source_module: 'uptime',
      event_type: 'down',
      severity: 'warning',
      channels: [],
      conditions: {},
      suppression: {
        repeat_count: 2,
        silence_minutes: 30,
      },
      time_window: { enabled: false },
      description: '',
      enabled: true,
    };
    this.showRuleModal = true;
  },

  /**
   * 编辑规则
   */
  editNotificationRule(rule) {
    // 处理渠道列表（可能是 JSON 字符串）
    let channels = rule.channels || [];
    if (typeof channels === 'string') {
      try {
        channels = JSON.parse(channels);
      } catch (e) {
        channels = [];
      }
    }

    this.ruleForm = {
      id: rule.id,
      name: rule.name,
      source_module: rule.source_module,
      event_type: rule.event_type,
      severity: rule.severity,
      channels: channels,
      conditions: typeof rule.conditions === 'string' ? JSON.parse(rule.conditions) : (rule.conditions || {}),
      suppression: typeof rule.suppression === 'string' ? JSON.parse(rule.suppression) : (rule.suppression || { repeat_count: 1, silence_minutes: 0 }),
      time_window: typeof rule.time_window === 'string' ? JSON.parse(rule.time_window) : (rule.time_window || { enabled: false }),
      description: rule.description || '',
      enabled: !!rule.enabled,
    };
    this.showRuleModal = true;
  },

  /**
   * 保存规则
   */
  async saveRule() {
    if (!this.ruleForm.name || !this.ruleForm.source_module || !this.ruleForm.event_type) {
      this.showGlobalToast('请填写必要信息', 'error');
      return;
    }

    if (this.ruleForm.channels.length === 0) {
      this.showGlobalToast('请至少选择一个通知渠道', 'error');
      return;
    }

    this.notificationSaving = true;
    try {
      const url = this.ruleForm.id
        ? `/api/notification/rules/${this.ruleForm.id}`
        : '/api/notification/rules';
      const method = this.ruleForm.id ? 'PUT' : 'POST';

      const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(this.ruleForm),
      });

      const data = await res.json();
      if (data.success) {
        this.showGlobalToast('规则保存成功', 'success');
        this.showRuleModal = false;
        await this.loadNotificationRules();
      } else {
        this.showGlobalToast(data.error || '保存失败', 'error');
      }
    } catch (error) {
      console.error('[Notification] Failed to save rule:', error);
      this.showGlobalToast('保存失败', 'error');
    } finally {
      this.notificationSaving = false;
    }
  },

  /**
   * 删除规则
   */
  async deleteNotificationRule(ruleId) {
    if (!confirm('确定要删除此规则吗?')) return;

    try {
      const res = await fetch(`/api/notification/rules/${ruleId}`, {
        method: 'DELETE',
      });
      const data = await res.json();
      if (data.success) {
        this.showGlobalToast('规则已删除', 'success');
        await this.loadNotificationRules();
      } else {
        this.showGlobalToast(data.error || '删除失败', 'error');
      }
    } catch (error) {
      console.error('[Notification] Failed to delete rule:', error);
      this.showGlobalToast('删除失败', 'error');
    }
  },

  /**
   * 启用规则
   */
  async enableNotificationRule(ruleId) {
    try {
      const res = await fetch(`/api/notification/rules/${ruleId}/enable`, {
        method: 'POST',
      });
      const data = await res.json();
      if (data.success) {
        this.showGlobalToast('规则已启用', 'success');
        await this.loadNotificationRules();
      } else {
        this.showGlobalToast(data.error || '启用失败', 'error');
      }
    } catch (error) {
      console.error('[Notification] Failed to enable rule:', error);
      this.showGlobalToast('启用失败', 'error');
    }
  },

  /**
   * 禁用规则
   */
  async disableNotificationRule(ruleId) {
    try {
      const res = await fetch(`/api/notification/rules/${ruleId}/disable`, {
        method: 'POST',
      });
      const data = await res.json();
      if (data.success) {
        this.showGlobalToast('规则已禁用', 'success');
        await this.loadNotificationRules();
      } else {
        this.showGlobalToast(data.error || '禁用失败', 'error');
      }
    } catch (error) {
      console.error('[Notification] Failed to disable rule:', error);
      this.showGlobalToast('禁用失败', 'error');
    }
  },

  /**
   * 来源模块变更回调
   */
  onSourceModuleChange() {
    if (this.ruleForm.source_module === 'uptime') {
      this.ruleForm.event_type = 'down';
    } else if (this.ruleForm.source_module === 'server') {
      this.ruleForm.event_type = 'offline';
    }
  },

  /**
   * 同步 Email 安全连接设置
   */
  syncEmailSecure() {
    if (this.channelForm.config.port === 465) {
      this.channelForm.config.secure = true;
    } else if (this.channelForm.config.port === 587) {
      this.channelForm.config.secure = false;
    }
  },

  // ==================== 历史管理 ====================

  /**
   * 清空通知历史
   */
  async clearNotificationHistory() {
    if (!confirm('确定要清空所有通知历史吗? 此操作不可恢复。')) return;

    try {
      const res = await fetch('/api/notification/history', {
        method: 'DELETE',
      });
      const data = await res.json();
      if (data.success) {
        this.showGlobalToast('历史记录已清空', 'success');
        await this.loadNotificationHistory();
      } else {
        this.showGlobalToast(data.error || '清空失败', 'error');
      }
    } catch (error) {
      console.error('[Notification] Failed to clear history:', error);
      this.showGlobalToast('清空失败', 'error');
    }
  },

  // ==================== 辅助方法 ====================

  /**
   * 获取过滤后的规则列表
   */
  getFilteredNotificationRules() {
    if (!this.notificationRuleFilter) return this.notificationRules;
    return this.notificationRules.filter(rule => rule.source_module === this.notificationRuleFilter);
  },

  /**
   * 获取过滤后的历史列表
   */
  getFilteredNotificationHistory() {
    if (!this.notificationHistoryFilter) return this.notificationHistory;
    return this.notificationHistory.filter(log => log.status === this.notificationHistoryFilter);
  },

  /**
   * 获取渠道类型名称
   */
  getChannelTypeName(type) {
    const names = {
      email: 'Email 邮箱',
      telegram: 'Telegram',
    };
    return names[type] || type;
  },

  /**
   * 获取来源模块名称
   */
  getSourceModuleName(module) {
    const names = {
      uptime: 'Uptime 监控',
      server: '主机监控',
      zeabur: 'Zeabur',
      openai: 'OpenAI',
    };
    return names[module] || module;
  },

  /**
   * 获取事件类型名称
   */
  getEventTypeName(type) {
    const names = {
      down: '宕机',
      up: '恢复',
      offline: '离线',
      cpu_high: 'CPU高负载',
      memory_high: '内存不足',
      disk_high: '磁盘不足',
      balance_low: '余额不足',
      log_too_large: '日志过大',
    };
    return names[type] || type;
  },

  /**
   * 格式化通知时间
   */
  formatNotificationTime(timeStr) {
    if (!timeStr) return '-';
    const date = new Date(timeStr);
    return date.toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    });
  },

  /**
   * 获取标签页动画类名
   */
  getTabAnimationClass(tab) {
    return this.mainActiveTab === tab ? 'active' : '';
  },
};
