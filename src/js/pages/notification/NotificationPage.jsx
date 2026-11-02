import React, { useState, useEffect, useMemo, useCallback } from 'react';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { collapseNotificationHistory } from '../../modules/notificationHistory.js';
import { Tabs } from '@cloudflare/kumo';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import { TabBarOverflowActions, stickyTabsBaseClass } from '../../components/ui/AppPrimitives.jsx';
import { Bell, Plus, AlertTriangle, History, Settings, Info } from '../../components/Icons.jsx';
import {
  FALLBACK_EVENT_CATALOG,
  getSourceModuleName,
  getEventTypeName,
} from './constants.js';
import { buildSampleEventData } from './utils.js';
import { ChannelsPanel } from './ChannelsPanel.jsx';
import { RulesPanel } from './RulesPanel.jsx';
import { EventsPanel } from './EventsPanel.jsx';
import { HistoryPanel } from './HistoryPanel.jsx';
import { SettingsPanel } from './SettingsPanel.jsx';
import { ChannelDialog } from './ChannelDialog.jsx';
import { RuleDialog } from './RuleDialog.jsx';

function NotificationPage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const [notificationCurrentTab, setNotificationCurrentTab] = useState('channels'); // 'channels' | 'rules' | 'history' | 'settings'
  const [notificationChannels, setNotificationChannels] = useState([]);
  const [notificationRules, setNotificationRules] = useState([]);
  const [notificationHistory, setNotificationHistory] = useState([]);
  const [notificationEventCatalog, setNotificationEventCatalog] = useState(FALLBACK_EVENT_CATALOG);
  const [notificationGlobalConfig, setNotificationGlobalConfig] = useState({
    enable_batch: true,
    batch_interval_seconds: 30,
    global_rate_limit_per_hour: 100,
    base_url: '',
  });

  // UI 状态
  const [notificationLoading, setNotificationLoading] = useState(false);
  const [notificationSaving, setNotificationSaving] = useState(false);
  const [templatePreview, setTemplatePreview] = useState(null);
  const [dryRunResults, setDryRunResults] = useState({});
  const [dryRunLoadingId, setDryRunLoadingId] = useState(null);
  
  // 过滤选项
  const [notificationRuleFilter, setNotificationRuleFilter] = useState(''); // '' | 'uptime' | 'server'
  const [notificationHistoryFilter, setNotificationHistoryFilter] = useState(''); // '' | 'sent' | 'failed' | 'pending'
  const [highlightRuleId, setHighlightRuleId] = useState(null);

  // Modals 控制
  const [showChannelModal, setShowChannelModal] = useState(false);
  const [channelModalMode, setChannelModalMode] = useState('add');
  const [channelForm, setChannelForm] = useState({
    id: null,
    name: '',
    type: 'email',
    enabled: true,
    config: {
      host: 'smtp.gmail.com',
      port: 465,
      secure: true,
      auth: { user: '', pass: '' },
      sender_name: 'API Monitor',
      to: '',
      bot_token: '',
      chat_id: '',
      proxy_url: '',
    }
  });

  const [showRuleModal, setShowRuleModal] = useState(false);
  const [ruleModalMode, setRuleModalMode] = useState('add');
  const [ruleCreateIntent, setRuleCreateIntent] = useState(null); // 跨页意图：待预填的 cron 事件类型（如 workflow.completed）
  const [ruleCreateWorkflowId, setRuleCreateWorkflowId] = useState(''); // 跨页意图：目标工作流 ID（精确匹配用）
  const [ruleCreateWorkflowName, setRuleCreateWorkflowName] = useState(''); // 跨页意图：目标工作流名称（写入规则名）
  const [ruleForm, setRuleForm] = useState({
    id: null,
    name: '',
    source_module: 'uptime',
    event_type: 'down',
    severity: 'warning',
    channels: [],
    suppression: {
      repeat_count: 2,
      silence_minutes: 30,
    },
    time_window: { enabled: false },
    description: '',
    title_template: '',
    message_template: '',
    backup_channels: [],
    quiet_until: '',
    conditions: [],
    enabled: true,
  });

  // 获取请求 Headers
  const getAuthHeaders = () => {
    return {
      'Content-Type': 'application/json',
    };
  };

  // ==================== 1. 数据载入 ====================
  const loadNotificationChannels = async () => {
    setNotificationLoading(true);
    try {
      const res = await fetch('/api/notification/channels', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success) {
        setNotificationChannels(data.data || []);
      }
    } catch (e) {
      console.error(e);
      toast.error('载入通知渠道失败');
    } finally {
      setNotificationLoading(false);
    }
  };

  const loadNotificationRules = async () => {
    setNotificationLoading(true);
    try {
      const res = await fetch('/api/notification/rules', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success) {
        setNotificationRules(data.data || []);
      }
    } catch (e) {
      console.error(e);
      toast.error('载入告警规则失败');
    } finally {
      setNotificationLoading(false);
    }
  };

  const loadNotificationHistory = async () => {
    setNotificationLoading(true);
    try {
      const res = await fetch('/api/notification/history?limit=100', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success) {
        setNotificationHistory(data.data || []);
      }
    } catch (e) {
      console.error(e);
      toast.error('载入通知历史失败');
    } finally {
      setNotificationLoading(false);
    }
  };

  const loadNotificationGlobalConfig = async () => {
    try {
      const res = await fetch('/api/notification/config', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success && data.data) {
        setNotificationGlobalConfig(data.data);
      }
    } catch (e) {
      console.error(e);
    }
  };

  const loadNotificationEventCatalog = async () => {
    try {
      const res = await fetch('/api/notification/event-catalog', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success && Array.isArray(data.data)) {
        setNotificationEventCatalog(data.data);
      }
    } catch (e) {
      console.error(e);
      setNotificationEventCatalog(FALLBACK_EVENT_CATALOG);
    }
  };

  useEffect(() => {
    loadNotificationChannels();
    loadNotificationRules();
    loadNotificationHistory();
    loadNotificationGlobalConfig();
    loadNotificationEventCatalog();
  }, []);

  // ==================== 2. 渠道管理 CRUD ====================
  const handleOpenAddChannel = () => {
    setChannelForm({
      id: null,
      name: '',
      type: 'email',
      enabled: true,
      config: {
        host: '',
        port: 465,
        secure: true,
        auth: { user: '', pass: '' },
        sender_name: 'API Monitor',
        to: '',
        bot_token: '',
        chat_id: '',
        proxy_url: '',
      }
    });
    setChannelModalMode('add');
    setShowChannelModal(true);
  };

  const handleOpenEditChannel = (channel) => {
    const defaultConfig = {
      host: '',
      port: 465,
      secure: true,
      auth: { user: '', pass: '' },
      sender_name: 'API Monitor',
      to: '',
      bot_token: '',
      chat_id: '',
      proxy_url: '',
    };

    let config = { ...defaultConfig };
    try {
      const parsedConfig = typeof channel.config === 'string'
        ? JSON.parse(channel.config)
        : (channel.config || {});
      config = { ...config, ...parsedConfig };
      if (parsedConfig.auth) {
        config.auth = { ...config.auth, ...parsedConfig.auth };
      }
    } catch (e) {
      console.warn('解析渠道配置失败:', e);
    }

    setChannelForm({
      id: channel.id,
      name: channel.name || '',
      type: channel.type || 'email',
      enabled: !!channel.enabled,
      config
    });
    setChannelModalMode('edit');
    setShowChannelModal(true);
  };

  const handleSaveChannel = async () => {
    if (!channelForm.name.trim()) {
      toast.warning('请输入渠道名称');
      return;
    }

    if (channelForm.type === 'email') {
      const config = channelForm.config;
      if (!config.host || !config.auth.user || !config.auth.pass || !config.to) {
        toast.warning('请填写完整的 Email SMTP 与收发件人配置');
        return;
      }
    } else if (channelForm.type === 'telegram') {
      const config = channelForm.config;
      if (!config.bot_token || !config.chat_id) {
        toast.warning('请填写完整的 Telegram Bot 令牌与 Chat ID');
        return;
      }
      if (config.proxy_url) {
        try {
          const proxy = new URL(config.proxy_url);
          if (!['http:', 'https:', 'socks5:', 'socks5h:'].includes(proxy.protocol)) {
            throw new Error('unsupported proxy scheme');
          }
        } catch {
          toast.warning('请输入有效的 HTTP、HTTPS 或 SOCKS5 代理地址');
          return;
        }
      }
    }

    setNotificationSaving(true);
    try {
      const isEdit = !!channelForm.id;
      const url = isEdit
        ? `/api/notification/channels/${channelForm.id}`
        : '/api/notification/channels';
      const method = isEdit ? 'PUT' : 'POST';

      const res = await fetch(url, {
        method,
        headers: getAuthHeaders(),
        body: JSON.stringify(channelForm)
      });
      const data = await res.json();

      if (data.success) {
        toast.success(isEdit ? '通知渠道已更新' : '通知渠道已创建');
        setShowChannelModal(false);
        loadNotificationChannels();
      } else {
        toast.error(data.error || '保存失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('保存失败');
    } finally {
      setNotificationSaving(false);
    }
  };

  const handleDeleteChannel = async (id) => {
    const channel = notificationChannels.find(item => item.id === id);
    if (!confirmPress(`channel:${id}`, `删除通知渠道「${channel?.name || '#' + id}」`)) return;
    try {
      const res = await fetch(`/api/notification/channels/${id}`, {
        method: 'DELETE',
        headers: getAuthHeaders()
      });
      const data = await res.json();
      if (data.success) {
        toast.success('渠道已删除');
        loadNotificationChannels();
      } else {
        toast.error(data.error || '删除失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('删除请求失败');
    }
  };

  const handleTestChannel = async (id) => {
    toast.info('正在发送测试通知...', { isManual: true });
    try {
      const res = await fetch(`/api/notification/channels/${id}/test`, {
        method: 'POST',
        headers: getAuthHeaders()
      });
      const data = await res.json();
      if (data.success) {
        toast.success('🎉 测试消息已成功下发，请注意查收！');
      } else {
        toast.error(data.error || '测试消息发送失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('网络请求异常');
    }
  };

  const syncEmailSecure = (port) => {
    const isSSL = port === 465;
    setChannelForm(prev => ({
      ...prev,
      config: {
        ...prev.config,
        port,
        secure: isSSL
      }
    }));
  };

  // ==================== 3. 告警规则 CRUD ====================
  // useCallback 固定引用：跨页意图 effect 依赖它，普通函数每次渲染新引用会导致 effect 结构性重跑
  const handleOpenAddRule = useCallback((overrides = {}) => {
    setRuleForm({
      id: null,
      name: '',
      source_module: notificationEventCatalog[0]?.module || 'uptime',
      event_type: notificationEventCatalog[0]?.events?.[0] || 'down',
      severity: 'warning',
      channels: [],
      suppression: {
        repeat_count: 2,
        silence_minutes: 30,
      },
      time_window: { enabled: false },
      description: '',
      title_template: '',
      message_template: '',
      backup_channels: [],
      quiet_until: '',
      enabled: true,
      ...overrides,
    });
    setTemplatePreview(null);
    setRuleModalMode('add');
    setShowRuleModal(true);
  }, [notificationEventCatalog]);

  // ==================== 跨页意图：从定时任务卡片「配置通知规则」跳转而来 ====================
  // 读取 ?newRule=<cron 事件类型>（可带 workflowId/workflowName）并立即清理 URL，避免刷新或返回时重复触发。
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const eventType = params.get('newRule');
    if (!eventType) return;
    setRuleCreateIntent(eventType);
    setRuleCreateWorkflowId(params.get('workflowId') || '');
    setRuleCreateWorkflowName(params.get('workflowName') || '');
    window.history.replaceState({}, '', window.location.pathname);
  }, []);

  // 等数据加载完成后跳转「规则」Tab、过滤 cron 源；已有匹配规则则定位高亮，否则预填新建弹窗。
  // 带工作流 ID 时按 conditions 中的 workflowId 精确匹配，避免多个工作流互相误判为「已有」。
  useEffect(() => {
    if (!ruleCreateIntent || notificationLoading) return;
    const cronEvents = notificationEventCatalog.find(item => item.module === 'cron')?.events || [];
    if (!cronEvents.includes(ruleCreateIntent)) {
      setRuleCreateIntent(null); // 无法处理的意图立即消费，避免空转
      return;
    }
    setNotificationCurrentTab('rules');
    setNotificationRuleFilter('cron');
    const matchesWorkflow = (rule) => {
      if (!ruleCreateWorkflowId) return true;
      const conditions = typeof rule.conditions === 'string' ? (() => { try { return JSON.parse(rule.conditions); } catch { return null; } })() : rule.conditions;
      const items = Array.isArray(conditions) ? conditions : conditions?.items;
      return Array.isArray(items) && items.some(item => item.field === 'workflowId' && String(item.value) === ruleCreateWorkflowId);
    };
    const existing = notificationRules.find(rule => rule.source_module === 'cron' && rule.event_type === ruleCreateIntent && matchesWorkflow(rule));
    if (existing) {
      setHighlightRuleId(existing.id);
      // 意图同样要立即消费：否则依赖数组变化时 effect 反复重跑（高亮周期性闪烁），
      // 且删掉该规则后刷新会突然弹出预填弹窗
      setRuleCreateIntent(null); // 高亮清理由下方 timer 兜底
      const timer = window.setTimeout(() => setHighlightRuleId(null), 2500);
      return () => window.clearTimeout(timer);
    } else {
      handleOpenAddRule({
        name: ruleCreateWorkflowName
          ? `${ruleCreateWorkflowName} - ${getEventTypeName(ruleCreateIntent)}通知`
          : `${getEventTypeName(ruleCreateIntent)}通知`,
        source_module: 'cron',
        event_type: ruleCreateIntent,
        severity: ruleCreateIntent.includes('failed') ? 'critical' : 'info',
        ...(ruleCreateWorkflowId ? {
          conditions: {
            mode: 'all',
            items: [{ field: 'workflowId', operator: 'equals', value: ruleCreateWorkflowId }],
          },
        } : {}),
      });
    }
    setRuleCreateIntent(null); // 只消费一次
  }, [ruleCreateIntent, notificationLoading, notificationEventCatalog, notificationRules, handleOpenAddRule, ruleCreateWorkflowId, ruleCreateWorkflowName]);

  const handleOpenEditRule = (rule) => {
    let channels = rule.channels || [];
    if (typeof channels === 'string') {
      try {
        channels = JSON.parse(channels);
      } catch (e) {
        channels = [];
      }
    }

    let backupChannels = rule.backup_channels || [];
    if (typeof backupChannels === 'string') {
      try {
        backupChannels = JSON.parse(backupChannels);
      } catch (e) {
        backupChannels = [];
      }
    }

    setRuleForm({
      id: rule.id,
      name: rule.name || '',
      source_module: rule.source_module || 'uptime',
      event_type: rule.event_type || 'down',
      severity: rule.severity || 'warning',
      channels: channels.map(String),
      suppression: typeof rule.suppression === 'string' ? JSON.parse(rule.suppression) : (rule.suppression || { repeat_count: 1, silence_minutes: 30 }),
      time_window: typeof rule.time_window === 'string' ? JSON.parse(rule.time_window) : (rule.time_window || { enabled: false }),
      description: rule.description || '',
      title_template: rule.title_template || '',
      message_template: rule.message_template || '',
      backup_channels: backupChannels.map(String),
      quiet_until: rule.quiet_until || '',
      conditions: typeof rule.conditions === 'string' ? (() => { try { return JSON.parse(rule.conditions); } catch { return null; } })() : (rule.conditions || []),
      enabled: !!rule.enabled
    });
    setTemplatePreview(null);
    setRuleModalMode('edit');
    setShowRuleModal(true);
  };

  const handleSaveRule = async () => {
    if (!ruleForm.name.trim()) {
      toast.warning('请输入规则名称');
      return;
    }
    if (ruleForm.channels.length === 0) {
      toast.warning('请至少选择一个通知渠道');
      return;
    }

    setNotificationSaving(true);
    try {
      const isEdit = !!ruleForm.id;
      const url = isEdit
        ? `/api/notification/rules/${ruleForm.id}`
        : '/api/notification/rules';
      const method = isEdit ? 'PUT' : 'POST';

      const res = await fetch(url, {
        method,
        headers: getAuthHeaders(),
        body: JSON.stringify(ruleForm)
      });
      const data = await res.json();
      if (data.success) {
        toast.success(isEdit ? '告警规则已更新' : '告警规则已创建');
        setShowRuleModal(false);
        loadNotificationRules();
      } else {
        toast.error(data.error || '保存失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('规则保存异常');
    } finally {
      setNotificationSaving(false);
    }
  };

  const handleDeleteRule = async (id) => {
    const rule = notificationRules.find(item => item.id === id);
    if (!confirmPress(`rule:${id}`, `删除告警规则「${rule?.name || '#' + id}」`)) return;
    try {
      const res = await fetch(`/api/notification/rules/${id}`, {
        method: 'DELETE',
        headers: getAuthHeaders()
      });
      const data = await res.json();
      if (data.success) {
        toast.success('告警规则已删除');
        loadNotificationRules();
      } else {
        toast.error(data.error || '删除失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('删除规则异常');
    }
  };

  const handleToggleRuleEnabled = async (rule) => {
    const nextState = !rule.enabled;
    try {
      const url = `/api/notification/rules/${rule.id}/${nextState ? 'enable' : 'disable'}`;
      const res = await fetch(url, {
        method: 'POST',
        headers: getAuthHeaders()
      });
      const data = await res.json();
      if (data.success) {
        toast.success(nextState ? '告警规则已启用' : '告警规则已禁用');
        loadNotificationRules();
      } else {
        toast.error(data.error || '操作失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('切换操作异常');
    }
  };

  const handleSourceModuleChange = (module) => {
    const catalogItem = notificationEventCatalog.find(item => item.module === module);
    const defaultEvent = catalogItem?.events?.[0] || (module === 'uptime' ? 'down' : 'offline');
    setRuleForm(prev => ({
      ...prev,
      source_module: module,
      event_type: defaultEvent
    }));
  };

  const handlePreviewTemplate = async () => {
    setNotificationSaving(true);
    try {
      const res = await fetch('/api/notification/templates/preview', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          title_template: ruleForm.title_template,
          message_template: ruleForm.message_template,
          data: buildSampleEventData(ruleForm),
        }),
      });
      const data = await res.json();
      if (!res.ok || !data.success) throw new Error(data.error || '模板预览失败');
      setTemplatePreview(data.data);
      toast.success('模板预览已生成');
    } catch (e) {
      console.error(e);
      toast.error(e.message || '模板预览失败');
    } finally {
      setNotificationSaving(false);
    }
  };

  const handleDryRunRule = async (rule) => {
    setDryRunLoadingId(rule.id);
    try {
      const res = await fetch(`/api/notification/rules/${rule.id}/dry-run`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ data: buildSampleEventData(rule) }),
      });
      const data = await res.json();
      if (!res.ok || !data.success) throw new Error(data.error || 'dry-run 失败');
      setDryRunResults(prev => ({ ...prev, [rule.id]: data.data }));
      toast.success(data.data?.wouldNotify ? 'dry-run 命中：会发送通知' : 'dry-run 完成：不会发送');
    } catch (e) {
      console.error(e);
      toast.error(e.message || 'dry-run 失败');
    } finally {
      setDryRunLoadingId(null);
    }
  };

  // ==================== 4. 历史记录与全局设置 ====================
  const handleClearHistory = async () => {
    if (!(await dialog.confirm('确定要清空所有通知历史记录吗？清空后将无法找回！'))) return;
    try {
      const res = await fetch('/api/notification/history', {
        method: 'DELETE',
        headers: getAuthHeaders()
      });
      const data = await res.json();
      if (data.success) {
        toast.success('通知历史记录已清空');
        loadNotificationHistory();
      } else {
        toast.error(data.error || '清空失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('操作异常');
    }
  };

  const handleSaveGlobalConfig = async () => {
    setNotificationSaving(true);
    try {
      const res = await fetch('/api/notification/config', {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify(notificationGlobalConfig)
      });
      const data = await res.json();
      if (data.success) {
        toast.success('全局通知配置已保存');
      } else {
        toast.error(data.error || '保存失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('网络配置更新异常');
    } finally {
      setNotificationSaving(false);
    }
  };

  // ==================== 5. 过滤运算 ====================
  const filteredRules = useMemo(() => {
    if (!notificationRuleFilter) return notificationRules;
    return notificationRules.filter(r => r.source_module === notificationRuleFilter);
  }, [notificationRules, notificationRuleFilter]);

  const visibleHistory = useMemo(() => collapseNotificationHistory(notificationHistory), [notificationHistory]);

  const filteredHistory = useMemo(() => {
    if (!notificationHistoryFilter) return visibleHistory;
    return visibleHistory.filter(h => h.status === notificationHistoryFilter);
  }, [visibleHistory, notificationHistoryFilter]);

  const catalogModuleItems = useMemo(() => {
    const source = notificationEventCatalog.length > 0 ? notificationEventCatalog : FALLBACK_EVENT_CATALOG;
    return source.map(item => ({ value: item.module, label: getSourceModuleName(item.module) }));
  }, [notificationEventCatalog]);

  const catalogEventItems = useMemo(() => {
    const source = notificationEventCatalog.length > 0 ? notificationEventCatalog : FALLBACK_EVENT_CATALOG;
    const catalogItem = source.find(item => item.module === ruleForm.source_module);
    const events = catalogItem?.events?.length ? catalogItem.events : ['down', 'up'];
    return events.map(event => ({ value: event, label: getEventTypeName(event) }));
  }, [notificationEventCatalog, ruleForm.source_module]);

  const ruleFilterItems = useMemo(() => ([
    { value: '', label: '所有模块' },
    ...catalogModuleItems,
  ]), [catalogModuleItems]);

  return (
    <div className="flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4">
      {/* ==================== 顶部 Tab 导航 ==================== */}
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={notificationCurrentTab}
          onValueChange={setNotificationCurrentTab}
          tabs={[
            { value: 'channels', label: <span className="inline-flex items-center gap-1.5"><Bell className="w-3.5 h-3.5" />通知渠道</span> },
            { value: 'rules', label: <span className="inline-flex items-center gap-1.5"><AlertTriangle className="w-3.5 h-3.5" />告警规则</span> },
            { value: 'events', label: <span className="inline-flex items-center gap-1.5"><Info className="w-3.5 h-3.5" />事件目录</span> },
            { value: 'history', label: <span className="inline-flex items-center gap-1.5"><History className="w-3.5 h-3.5" />通知历史</span> },
            { value: 'settings', label: <span className="inline-flex items-center gap-1.5"><Settings className="w-3.5 h-3.5" />全局配置</span> },
          ]}
        />

        <TabBarOverflowActions
          items={
            notificationCurrentTab === 'channels'
              ? [
                  {
                    key: 'add-channel',
                    label: '添加渠道',
                    icon: <Plus className="w-4 h-4" />,
                    onClick: handleOpenAddChannel,
                    variant: 'primary',
                  },
                ]
              : notificationCurrentTab === 'rules'
                ? [
                    {
                      key: 'add-rule',
                      label: '添加规则',
                      icon: <Plus className="w-4 h-4" />,
                      onClick: handleOpenAddRule,
                      variant: 'primary',
                    },
                  ]
                : []
          }
        />
      </div>

      {/* ==================== 1. 通知渠道 Tab ==================== */}
      {notificationCurrentTab === 'channels' && (
        <ChannelsPanel
          isArmed={isArmed}
          notificationLoading={notificationLoading}
          notificationChannels={notificationChannels}
          handleOpenAddChannel={handleOpenAddChannel}
          handleTestChannel={handleTestChannel}
          handleOpenEditChannel={handleOpenEditChannel}
          handleDeleteChannel={handleDeleteChannel}
        />
      )}

      {/* ==================== 2. 告警规则 Tab ==================== */}
      {notificationCurrentTab === 'rules' && (
        <RulesPanel
          isArmed={isArmed}
          notificationLoading={notificationLoading}
          notificationRules={notificationRules}
          filteredRules={filteredRules}
          notificationRuleFilter={notificationRuleFilter}
          setNotificationRuleFilter={setNotificationRuleFilter}
          ruleFilterItems={ruleFilterItems}
          highlightRuleId={highlightRuleId}
          dryRunResults={dryRunResults}
          dryRunLoadingId={dryRunLoadingId}
          loadNotificationRules={loadNotificationRules}
          handleOpenAddRule={handleOpenAddRule}
          handleDryRunRule={handleDryRunRule}
          handleOpenEditRule={handleOpenEditRule}
          handleDeleteRule={handleDeleteRule}
          handleToggleRuleEnabled={handleToggleRuleEnabled}
        />
      )}

      {/* ==================== 事件目录 Tab ==================== */}
      {notificationCurrentTab === 'events' && (
        <EventsPanel notificationEventCatalog={notificationEventCatalog} />
      )}

      {/* ==================== 3. 通知历史 Tab ==================== */}
      {notificationCurrentTab === 'history' && (
        <HistoryPanel
          notificationLoading={notificationLoading}
          notificationHistory={notificationHistory}
          filteredHistory={filteredHistory}
          notificationHistoryFilter={notificationHistoryFilter}
          setNotificationHistoryFilter={setNotificationHistoryFilter}
          notificationChannels={notificationChannels}
          loadNotificationHistory={loadNotificationHistory}
          handleClearHistory={handleClearHistory}
        />
      )}

      {/* ==================== 4. 全局配置 Tab ==================== */}
      {notificationCurrentTab === 'settings' && (
        <SettingsPanel
          notificationGlobalConfig={notificationGlobalConfig}
          setNotificationGlobalConfig={setNotificationGlobalConfig}
          notificationSaving={notificationSaving}
          handleSaveGlobalConfig={handleSaveGlobalConfig}
        />
      )}

      {/* ==================== 6. 弹窗 1: 添加/编辑通道 ==================== */}
      <ChannelDialog
        showChannelModal={showChannelModal}
        setShowChannelModal={setShowChannelModal}
        channelForm={channelForm}
        setChannelForm={setChannelForm}
        syncEmailSecure={syncEmailSecure}
        handleSaveChannel={handleSaveChannel}
        notificationSaving={notificationSaving}
      />

      {/* ==================== 7. 弹窗 2: 添加/编辑规则 ==================== */}
      <RuleDialog
        showRuleModal={showRuleModal}
        setShowRuleModal={setShowRuleModal}
        ruleForm={ruleForm}
        setRuleForm={setRuleForm}
        catalogModuleItems={catalogModuleItems}
        catalogEventItems={catalogEventItems}
        handleSourceModuleChange={handleSourceModuleChange}
        handlePreviewTemplate={handlePreviewTemplate}
        templatePreview={templatePreview}
        notificationChannels={notificationChannels}
        handleSaveRule={handleSaveRule}
        notificationSaving={notificationSaving}
      />
    </div>
  );
}

export default NotificationPage;
