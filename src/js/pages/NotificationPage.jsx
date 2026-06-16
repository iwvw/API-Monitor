import React, { useState, useEffect, useMemo } from 'react';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Tabs } from '@cloudflare/kumo';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import {
  Bell,
  Plus,
  Trash,
  RotateCw,
  Edit,
  Mail,
  Send,
  History,
  Settings,
  Info,
  AlertTriangle,
  Save,
  CheckDouble
} from '../components/Icons.jsx';

// ==================== 辅助格式化与映射 ====================
const getChannelTypeName = (type) => {
  const names = {
    email: 'Email 邮箱',
    telegram: 'Telegram Bot',
  };
  return names[type] || type;
};

const getSourceModuleName = (module) => {
  const names = {
    uptime: 'Uptime 监控',
    server: 'Host 主机',
    openai: 'OpenAI 接口',
    system: '系统设置',
    filebox: '文件柜',
    totp: '双因子认证',
  };
  return names[module] || module;
};

const getEventTypeName = (type) => {
  const names = {
    down: '服务宕机 (Down)',
    up: '服务恢复 (Up)',
    offline: '主机离线',
    online: '主机上线',
    cpu_high: 'CPU高负载',
    cpu_normal: 'CPU恢复正常',
    memory_high: '内存不足',
    memory_normal: '内存恢复',
    disk_high: '磁盘空间不足',
    disk_normal: '磁盘恢复正常',
    balance_low: '余额不足',
    log_too_large: '日志体积过大',
    pending: '状态待确认',
    'resource.created': '资源已创建',
    'resource.deleted': '资源已删除',
    cleanup: '清理任务',
    'database.backup': '数据库备份',
    'database.import': '数据库导入',
    'log.cleanup': '日志清理',
    'migration.failed': '迁移失败',
    login: '登录',
    logout: '登出',
    'playback.error': '播放错误',
    'proxy.blocked': '代理拦截',
    created: '已创建',
    updated: '已更新',
    deleted: '已删除',
    revealed: '已查看',
    imported: '已导入',
    exported: '已导出',
  };
  return names[type] || type;
};

const FALLBACK_EVENT_CATALOG = [
  { module: 'uptime', events: ['down', 'up', 'pending'] },
  { module: 'server', events: ['offline', 'online', 'cpu_high', 'memory_high', 'disk_high'] },
];

const buildSampleEventData = (rule = {}) => ({
  severity: rule.severity || 'warning',
  eventType: rule.event_type || 'down',
  monitorName: 'API Gateway',
  serverName: 'prod-node-01',
  url: 'https://api.example.com/health',
  host: 'prod-node-01',
  hostname: 'prod-node-01',
  error: 'Connection timeout',
  ping: 128,
  cpu_usage: 92,
  mem_percent: 84,
  threshold: 90,
  downDuration: '3 分钟',
});

function NotificationPage() {
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
    }
  });

  const [showRuleModal, setShowRuleModal] = useState(false);
  const [ruleModalMode, setRuleModalMode] = useState('add');
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
    enabled: true,
  });

  // 获取请求 Headers
  const getAuthHeaders = () => {
    const password = localStorage.getItem('admin_password') || '';
    return {
      'Content-Type': 'application/json',
      'x-admin-password': password,
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
    if (!(await dialog.deleteResource({
      resourceType: '通知渠道',
      resourceName: channel?.name || `#${id}`,
    }))) return;
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
    toast.info('正在发送测试通知...');
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
  const handleOpenAddRule = () => {
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
    });
    setTemplatePreview(null);
    setRuleModalMode('add');
    setShowRuleModal(true);
  };

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
    if (!(await dialog.deleteResource({
      resourceType: '告警规则',
      resourceName: rule?.name || `#${id}`,
    }))) return;
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

  const filteredHistory = useMemo(() => {
    if (!notificationHistoryFilter) return notificationHistory;
    return notificationHistory.filter(h => h.status === notificationHistoryFilter);
  }, [notificationHistory, notificationHistoryFilter]);

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

  return (
    <div className="space-y-6">
      {/* ==================== 顶部 Tab 导航 ==================== */}
      <div className="flex flex-wrap items-center justify-between border-b border-kumo-line pb-3 gap-4">
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

        {notificationCurrentTab === 'channels' && (
          <Button size="sm" variant="primary" icon={<Plus className="w-4 h-4" />} onClick={handleOpenAddChannel}>
            添加渠道
          </Button>
        )}

        {notificationCurrentTab === 'rules' && (
          <Button size="sm" variant="primary" icon={<Plus className="w-4 h-4" />} onClick={handleOpenAddRule}>
            添加规则
          </Button>
        )}
      </div>

      {/* ==================== 1. 通知渠道 Tab ==================== */}
      {notificationCurrentTab === 'channels' && (
        <div className="space-y-4">
          {notificationLoading && notificationChannels.length === 0 ? (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
              {[...Array(4)].map((_, i) => (
                <div key={i} className="app-card p-4 space-y-4">
                  <div className="flex items-start justify-between gap-3">
                    <SkeletonLine className="w-8 h-8 rounded-lg" />
                    <div className="flex-1 space-y-1.5">
                      <SkeletonLine className="w-1/2 h-3.5" />
                      <SkeletonLine className="w-1/3 h-2.5" />
                    </div>
                  </div>
                  <SkeletonLine className="w-full h-1" />
                </div>
              ))}
            </div>
          ) : notificationChannels.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle app-empty-panel">
              <Bell className="w-12 h-12 opacity-30 mb-4" />
              <div className="text-sm">暂无通知渠道，配置通知以便发生故障时接收提醒</div>
              <Button size="sm" variant="primary" className="mt-4" onClick={handleOpenAddChannel}>
                创建第一个渠道
              </Button>
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
              {notificationChannels.map((channel) => (
                <div
                  key={channel.id}
                  className="app-card hover:border-kumo-brand p-4 transition-all flex flex-col justify-between min-h-[128px]"
                >
                  <div className="flex items-start justify-between gap-3">
                    {/* Icon */}
                    <div className={`w-8 h-8 rounded-lg flex items-center justify-center text-kumo-inverse text-base flex-shrink-0 ${
                      channel.type === 'email' ? 'bg-kumo-info' : 'bg-kumo-brand'
                    }`}>
                      {channel.type === 'email' ? <Mail className="w-4 h-4" /> : <Send className="w-4 h-4" />}
                    </div>

                    {/* Information */}
                    <div className="min-w-0 flex-1">
                      <h4 className="text-xs font-bold text-kumo-strong truncate leading-tight">
                        {channel.name}
                      </h4>
                      <p className="text-[10px] text-kumo-subtle mt-1 select-none">
                        {getChannelTypeName(channel.type)}
                      </p>
                    </div>

                    {/* Actions */}
                    <div className="flex items-center gap-1.5 flex-shrink-0">
                      <Button
                        onClick={() => handleTestChannel(channel.id)}
                        variant="ghost" size="sm"
                        shape="square"
                        aria-label="测试投递"
                        className="text-kumo-subtle hover:text-kumo-brand"
                        title="测试投递"
                      >
                        <Send className="w-3 h-3" />
                      </Button>
                      <Button
                        onClick={() => handleOpenEditChannel(channel)}
                        variant="ghost" size="sm"
                        shape="square"
                        aria-label="编辑通知渠道"
                        className="text-kumo-subtle hover:text-kumo-strong"
                        title="编辑"
                      >
                        <Edit className="w-3.5 h-3.5" />
                      </Button>
                      <Button
                        onClick={() => handleDeleteChannel(channel.id)}
                        variant="ghost" size="sm"
                        shape="square"
                        aria-label="删除通知渠道"
                        className="text-kumo-subtle hover:text-kumo-danger"
                        title="删除"
                      >
                        <Trash className="w-3.5 h-3.5" />
                      </Button>
                    </div>
                  </div>

                  <div className="flex items-center justify-between border-t border-kumo-line pt-3 mt-4">
                    <span className={`text-[10px] font-semibold flex items-center gap-1 ${
                      channel.enabled ? 'text-kumo-success' : 'text-kumo-subtle'
                    }`}>
                      <span className={`w-1.5 h-1.5 rounded-full ${channel.enabled ? 'bg-kumo-success' : 'bg-kumo-subtle'}`} />
                      {channel.enabled ? '启用中' : '已禁用'}
                    </span>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* ==================== 2. 告警规则 Tab ==================== */}
      {notificationCurrentTab === 'rules' && (
        <div className="space-y-4">
          {/* 筛选控制栏 */}
          <div className="flex items-center justify-between pb-2 select-none">
            <Select
              aria-label="告警规则模块筛选" size="sm"
              value={notificationRuleFilter}
              onValueChange={setNotificationRuleFilter}
              placeholder="所有模块"
              items={[
                { value: '', label: '所有模块' },
                { value: 'uptime', label: 'Uptime 监测' },
                { value: 'server', label: 'Host 主机' },
              ]}
            />

            <Button
              onClick={loadNotificationRules}
              disabled={notificationLoading}
              variant="secondary" size="sm"
              shape="square"
              aria-label="刷新告警规则"
              className="text-kumo-subtle hover:text-kumo-strong"
              title="刷新"
              icon={<RotateCw className={`w-3.5 h-3.5 ${notificationLoading ? 'animate-spin' : ''}`} />}
            />
          </div>

          {notificationLoading && notificationRules.length === 0 ? (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {[...Array(3)].map((_, i) => (
                <div key={i} className="app-card p-4 space-y-4">
                  <div className="flex items-start justify-between gap-3">
                    <SkeletonLine className="w-8 h-8 rounded-lg" />
                    <div className="flex-1 space-y-1.5">
                      <SkeletonLine className="w-2/3 h-3.5" />
                      <SkeletonLine className="w-1/3 h-2.5" />
                    </div>
                  </div>
                  <SkeletonLine className="w-full h-1" />
                </div>
              ))}
            </div>
          ) : filteredRules.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle app-empty-panel">
              <AlertTriangle className="w-12 h-12 opacity-30 mb-4" />
              <div className="text-sm">暂无匹配的告警规则</div>
              <Button size="sm" variant="primary" className="mt-4" onClick={handleOpenAddRule}>
                创建告警规则
              </Button>
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {filteredRules.map((rule) => (
                <div
                  key={rule.id}
                  className="app-card hover:border-kumo-brand p-4 transition-all flex flex-col justify-between min-h-[148px]"
                >
                  <div className="flex items-start justify-between gap-3">
                    {/* Severity Indicator */}
                    <div className={`w-8 h-8 rounded-lg flex items-center justify-center text-kumo-inverse text-base flex-shrink-0 ${
                      rule.severity === 'critical'
                        ? 'bg-kumo-danger'
                        : rule.severity === 'warning'
                        ? 'bg-kumo-warning'
                        : 'bg-kumo-info'
                    }`}>
                      <AlertTriangle className="w-4 h-4" />
                    </div>

                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-1.5 flex-wrap">
                        <span className="text-xs font-bold text-kumo-strong truncate leading-tight">
                          {rule.name}
                        </span>
                        <span className={`text-[8.5px] px-1 py-px rounded font-bold uppercase select-none ${
                          rule.severity === 'critical'
                            ? 'bg-kumo-danger/10 text-kumo-danger'
                            : rule.severity === 'warning'
                            ? 'bg-kumo-warning/10 text-kumo-warning'
                            : 'bg-kumo-info/10 text-kumo-info'
                        }`}>
                          {rule.severity}
                        </span>
                      </div>
                      <div className="flex items-center gap-1.5 mt-2 flex-wrap select-none">
                        <span className="text-[9px] app-subcard bg-kumo-recessed text-kumo-subtle px-1.5 py-0.5 rounded font-medium">
                          {getSourceModuleName(rule.source_module)}
                        </span>
                        <span className="text-[9px] app-subcard bg-kumo-recessed text-kumo-subtle px-1.5 py-0.5 rounded font-medium">
                          {getEventTypeName(rule.event_type)}
                        </span>
                      </div>
                    </div>

                    {/* Actions */}
                    <div className="flex items-center gap-1.5 flex-shrink-0">
                      <Button
                        onClick={() => handleDryRunRule(rule)}
                        variant="ghost" size="sm"
                        shape="square"
                        aria-label="预演告警规则"
                        className="text-kumo-subtle hover:text-kumo-brand"
                        title="Dry-run"
                        loading={dryRunLoadingId === rule.id}
                      >
                        <CheckDouble className="w-3.5 h-3.5" />
                      </Button>
                      <Button
                        onClick={() => handleOpenEditRule(rule)}
                        variant="ghost" size="sm"
                        shape="square"
                        aria-label="编辑告警规则"
                        className="text-kumo-subtle hover:text-kumo-strong"
                        title="编辑"
                      >
                        <Edit className="w-3.5 h-3.5" />
                      </Button>
                      <Button
                        onClick={() => handleDeleteRule(rule.id)}
                        variant="ghost" size="sm"
                        shape="square"
                        aria-label="删除告警规则"
                        className="text-kumo-subtle hover:text-kumo-danger"
                        title="删除"
                      >
                        <Trash className="w-3.5 h-3.5" />
                      </Button>
                    </div>
                  </div>

                  {/* Settings status summary */}
                  <div className="border-t border-kumo-line pt-3 mt-4 space-y-1.5">
                    {rule.suppression && (
                      <div className="flex items-center gap-1.5 text-[10px] text-kumo-subtle font-mono select-none">
                        <span>• 重复抑制: {rule.suppression.repeat_count || 1} 次</span>
                        <span className="text-kumo-subtle/30">/</span>
                        <span>静默期: {rule.suppression.silence_minutes || 0} 分钟</span>
                      </div>
                    )}
                    <div className="flex items-center justify-between">
                      <span className={`text-[10px] font-semibold flex items-center gap-1 ${
                        rule.enabled ? 'text-kumo-success' : 'text-kumo-subtle'
                      }`}>
                        <span className={`w-1.5 h-1.5 rounded-full ${rule.enabled ? 'bg-kumo-success' : 'bg-kumo-subtle'}`} />
                        {rule.enabled ? '开启中' : '已禁用'}
                      </span>
                      <Button
                        onClick={() => handleToggleRuleEnabled(rule)}
                        variant="ghost" size="sm"
                        className="h-6 text-[10px] text-kumo-brand hover:underline font-semibold"
                      >
                        {rule.enabled ? '一键禁用' : '一键启用'}
                      </Button>
                    </div>
                    {dryRunResults[rule.id] && (
                      <div className="rounded-md border border-kumo-line bg-kumo-recessed/50 p-2 text-[10px] leading-relaxed text-kumo-subtle">
                        <div className="flex items-center justify-between gap-2">
                          <span className="font-semibold text-kumo-strong">
                            {dryRunResults[rule.id].wouldNotify ? '预演结果：会发送' : '预演结果：不会发送'}
                          </span>
                          <span className="font-mono">{dryRunResults[rule.id].fingerprint}</span>
                        </div>
                        <div className="mt-1 truncate font-mono">{dryRunResults[rule.id].title}</div>
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* ==================== 事件目录 Tab ==================== */}
      {notificationCurrentTab === 'events' && (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-3">
          {notificationEventCatalog.map((item) => (
            <div key={item.module} className="app-card p-4">
              <div className="flex items-center justify-between gap-3 border-b border-kumo-line pb-3">
                <div className="flex items-center gap-2">
                  <Info className="w-4 h-4 text-kumo-brand" />
                  <h3 className="text-sm font-bold text-kumo-strong">{getSourceModuleName(item.module)}</h3>
                </div>
                <span className="text-[10px] font-mono text-kumo-subtle">{item.events?.length || 0}</span>
              </div>
              <div className="mt-3 flex flex-wrap gap-2">
                {(item.events || []).map((eventName) => (
                  <span
                    key={`${item.module}-${eventName}`}
                    className="rounded border border-kumo-line bg-kumo-recessed px-2 py-1 text-[10px] font-semibold text-kumo-subtle"
                  >
                    {getEventTypeName(eventName)}
                  </span>
                ))}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* ==================== 3. 通知历史 Tab ==================== */}
      {notificationCurrentTab === 'history' && (
        <div className="space-y-4">
          {/* 筛选控制条 */}
          <div className="flex items-center justify-between pb-2 gap-3 select-none">
            <div className="flex items-center gap-2">
              <Select
                aria-label="通知历史状态筛选" size="sm"
                value={notificationHistoryFilter}
                onValueChange={setNotificationHistoryFilter}
                placeholder="全部状态"
                items={[
                  { value: '', label: '全部状态' },
                  { value: 'sent', label: '已发送' },
                  { value: 'failed', label: '失败' },
                  { value: 'pending', label: '队列中' },
                ]}
              />

              <Button
                onClick={loadNotificationHistory}
                disabled={notificationLoading}
              variant="secondary" size="sm"
              shape="square"
              aria-label="刷新通知历史"
              className="text-kumo-subtle hover:text-kumo-strong"
              title="刷新"
                icon={<RotateCw className={`w-3.5 h-3.5 ${notificationLoading ? 'animate-spin' : ''}`} />}
              />
            </div>

            {notificationHistory.length > 0 && (
              <Button
                variant="destructive" size="sm"
                onClick={handleClearHistory}
                icon={<Trash className="w-3.5 h-3.5" />}
              >
                清空历史
              </Button>
            )}
          </div>

          {notificationLoading && notificationHistory.length === 0 ? (
            <div className="space-y-3">
              {[...Array(3)].map((_, i) => (
                <div key={i} className="app-card p-4 space-y-3">
                  <div className="flex items-center justify-between">
                    <SkeletonLine className="w-1/4 h-3.5" />
                    <SkeletonLine className="w-1/6 h-2.5" />
                  </div>
                  <SkeletonLine className="w-full h-12 rounded-md" />
                </div>
              ))}
            </div>
          ) : filteredHistory.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle app-empty-panel">
              <History className="w-12 h-12 opacity-30 mb-4" />
              <div className="text-sm">暂无匹配的通知历史记录</div>
            </div>
          ) : (
            <div className="flex flex-col gap-3">
              {filteredHistory.map((log) => (
                <div
                  key={log.id}
                  className="app-card p-4 flex items-start gap-3.5"
                >
                  {/* Status indicator pill */}
                  <div className={`w-7 h-7 rounded-full flex items-center justify-center text-xs flex-shrink-0 select-none ${
                    log.status === 'sent'
                      ? 'bg-kumo-success/15 text-kumo-success'
                      : log.status === 'failed'
                      ? 'bg-kumo-danger/15 text-kumo-danger font-bold'
                      : 'bg-kumo-warning/15 text-kumo-warning'
                  }`}>
                    {log.status === 'sent' ? '✓' : log.status === 'failed' ? '✗' : '...'}
                  </div>

                  <div className="min-w-0 flex-1">
                    <div className="flex items-center justify-between flex-wrap gap-2">
                      <span className="text-xs font-bold text-kumo-strong truncate">
                        {log.title}
                      </span>
                      <span className="text-[9px] text-kumo-subtle font-mono select-none">
                        {new Date(log.created_at).toLocaleString()}
                      </span>
                    </div>
                    {/* Message block */}
                    <div className="text-xs text-kumo-subtle font-mono p-3 app-subcard bg-kumo-recessed rounded-md mt-2.5 whitespace-pre-wrap select-all">
                      {log.message}
                    </div>

                    {/* Metadata indicators */}
                    {(log.error_message || log.retry_count > 0) && (
                      <div className="flex items-center gap-2 mt-2 select-none">
                        {log.error_message && (
                          <span className="text-[9px] font-semibold text-kumo-danger bg-kumo-danger/5 px-2 py-0.5 rounded border border-kumo-danger/10">
                            报错: {log.error_message}
                          </span>
                        )}
                        {log.retry_count > 0 && (
                          <span className="text-[9px] font-semibold text-kumo-warning bg-kumo-warning/5 px-2 py-0.5 rounded border border-kumo-warning/10 font-mono">
                            重试: {log.retry_count} 次
                          </span>
                        )}
                      </div>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {/* ==================== 4. 全局配置 Tab ==================== */}
      {notificationCurrentTab === 'settings' && (
        <div className="app-card p-6 space-y-6 max-w-2xl">
          <h3 className="text-sm font-semibold text-kumo-strong border-b border-kumo-line pb-3 select-none">
            全局配置选项
          </h3>

          <div className="space-y-4">
            {/* Base URL */}
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">看板基准 URL (用于生成通知卡片链接)</label>
              <Input size="sm"
                aria-label="看板基准 URL"
                type="text"
                placeholder="https://monitor.domain.com"
                value={notificationGlobalConfig.base_url || ''}
                onChange={(e) => setNotificationGlobalConfig(prev => ({ ...prev, base_url: e.target.value }))}
                className="w-full text-kumo-strong text-xs px-3 py-2"
              />
              <p className="text-[10px] text-kumo-subtle leading-tight select-none">设置后，Telegram/Email 发送出的故障消息将附带“查看看板详情”按钮与直达链接。</p>
            </div>

            {/* Rate cap & aggregation timer */}
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 border-t border-kumo-line pt-4">
              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">全局限频阀值 (条 / 小时)</label>
                <Input size="sm"
                  aria-label="全局限频阀值"
                  type="number"
                  value={notificationGlobalConfig.global_rate_limit_per_hour || 100}
                  onChange={(e) => setNotificationGlobalConfig(prev => ({ ...prev, global_rate_limit_per_hour: parseInt(e.target.value) || 0 }))}
                  className="w-full text-kumo-strong text-xs px-3 py-2 font-mono"
                />
                <p className="text-[10px] text-kumo-subtle leading-tight select-none">设置每小时发出通知的上限限制，达到阈值后将仅保留 Critical 极高等级事件推送。</p>
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">消息聚合归并时间 (秒)</label>
                <Input size="sm"
                  aria-label="消息聚合归并时间"
                  type="number"
                  value={notificationGlobalConfig.batch_interval_seconds || 30}
                  onChange={(e) => setNotificationGlobalConfig(prev => ({ ...prev, batch_interval_seconds: parseInt(e.target.value) || 0 }))}
                  className="w-full text-kumo-strong text-xs px-3 py-2 font-mono"
                />
                <p className="text-[10px] text-kumo-subtle leading-tight select-none">聚合聚合时间窗。在此窗口中下发的同渠道通知将合并作为单条推送，避免信息轰炸。</p>
              </div>
            </div>

            {/* Enable aggregate batch toggle */}
            <div className="flex items-start justify-between border-t border-kumo-line pt-4">
              <div className="space-y-0.5">
                <h4 className="text-xs font-semibold text-kumo-strong">启用通知聚合机制</h4>
                <p className="text-[10px] text-kumo-subtle leading-tight select-none">勾选此项后，系统将自动汇总相同设备或接口的告警，聚合后统一发送。</p>
              </div>
              <Switch
                checked={!!notificationGlobalConfig.enable_batch}
                onCheckedChange={(checked) => setNotificationGlobalConfig(prev => ({ ...prev, enable_batch: checked }))}
                size="sm"
              />
            </div>
          </div>

          <div className="flex justify-end pt-4 border-t border-kumo-line select-none">
            <Button size="sm" variant="primary" onClick={handleSaveGlobalConfig} loading={notificationSaving} icon={<Save className="w-3.5 h-3.5" />}>
              保存全局配置
            </Button>
          </div>
        </div>
      )}

      {/* ==================== 6. 弹窗 1: 添加/编辑通道 ==================== */}
      <Dialog.Root open={showChannelModal} onOpenChange={setShowChannelModal}>
        <Dialog className="p-6 sm:max-w-lg">
          <Dialog.Title className="text-base font-bold text-kumo-strong mb-1 select-none">
            {channelForm.id ? '编辑通知渠道' : '新建通知渠道'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4 select-none">
            配置系统警报发送的目标分发端口
          </Dialog.Description>

          <div className="-mx-1 space-y-4 max-h-[60vh] overflow-y-auto px-1 pr-2 scrollbar-thin">
            {/* Channel Type */}
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">渠道类型</label>
              <Select size="sm"
                aria-label="渠道类型"
                value={channelForm.type}
                disabled={channelForm.id !== null}
                onValueChange={(value) => setChannelForm(prev => ({ ...prev, type: String(value) }))}
                className="w-full"
                items={[
                  { value: 'email', label: '电子邮件' },
                  { value: 'telegram', label: 'Telegram Bot' },
                ]}
              />
            </div>

            {/* Channel Name */}
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">显示名称 *</label>
              <Input size="sm"
                aria-label="显示名称"
                type="text"
                placeholder="例如：运维值班邮箱"
                value={channelForm.name}
                onChange={(e) => setChannelForm(prev => ({ ...prev, name: e.target.value }))}
                className="w-full"
              />
            </div>

            {/* Email configuration */}
            {channelForm.type === 'email' && (
              <>
                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">SMTP 主机服务器地址 *</label>
                  <Input size="sm"
                    aria-label="SMTP 主机服务器地址"
                    type="text"
                    placeholder="smtp.gmail.com or smtp.exmail.qq.com"
                    value={channelForm.config.host}
                    onChange={(e) => setChannelForm(prev => ({
                      ...prev,
                      config: { ...prev.config, host: e.target.value }
                    }))}
                    className="w-full"
                  />
                </div>

                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1.5">
                    <label className="text-xs font-semibold text-kumo-subtle">连接端口</label>
                    <Input size="sm"
                      aria-label="连接端口"
                      type="number"
                      placeholder="465"
                      value={channelForm.config.port}
                      onChange={(e) => syncEmailSecure(parseInt(e.target.value) || 0)}
                      className="w-full font-mono"
                    />
                  </div>

                  <div className="space-y-1.5">
                    <label className="text-xs font-semibold text-kumo-subtle">加密安全协议</label>
                    <Select size="sm"
                      aria-label="加密安全协议"
                      value={channelForm.config.secure ? 'ssl' : 'tls'}
                      onValueChange={(value) => setChannelForm(prev => ({
                        ...prev,
                        config: { ...prev.config, secure: String(value) === 'ssl' }
                      }))}
                      className="w-full"
                      items={[
                        { value: 'tls', label: 'STARTTLS / TLS（587）' },
                        { value: 'ssl', label: 'SSL（465）' },
                      ]}
                    />
                  </div>
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">发件账户邮箱账号 *</label>
                  <Input size="sm"
                    aria-label="发件账户邮箱账号"
                    type="email"
                    placeholder="account@gmail.com"
                    value={channelForm.config.auth.user}
                    onChange={(e) => setChannelForm(prev => ({
                      ...prev,
                      config: {
                        ...prev.config,
                        auth: { ...prev.config.auth, user: e.target.value }
                      }
                    }))}
                    className="w-full"
                  />
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">SMTP 授权口令 / 应用密码 *</label>
                  <Input size="sm"
                    aria-label="SMTP 授权口令"
                    type="password"
                    placeholder="your_smtp_app_password"
                    value={channelForm.config.auth.pass}
                    onChange={(e) => setChannelForm(prev => ({
                      ...prev,
                      config: {
                        ...prev.config,
                        auth: { ...prev.config.auth, pass: e.target.value }
                      }
                    }))}
                    className="w-full"
                  />
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">发件人昵称（可选）</label>
                  <Input size="sm"
                    aria-label="发件人昵称"
                    type="text"
                    placeholder="API Monitor Alerter"
                    value={channelForm.config.sender_name}
                    onChange={(e) => setChannelForm(prev => ({
                      ...prev,
                      config: { ...prev.config, sender_name: e.target.value }
                    }))}
                    className="w-full"
                  />
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">收件目的邮箱 *</label>
                  <Input size="sm"
                    aria-label="收件目的邮箱"
                    type="email"
                    placeholder="recipient@domain.com"
                    value={channelForm.config.to}
                    onChange={(e) => setChannelForm(prev => ({
                      ...prev,
                      config: { ...prev.config, to: e.target.value }
                    }))}
                    className="w-full"
                  />
                </div>
              </>
            )}

            {/* Telegram configuration */}
            {channelForm.type === 'telegram' && (
              <>
                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">Telegram Bot 令牌 *</label>
                  <Input size="sm"
                    aria-label="Telegram Bot 令牌"
                    type="text"
                    placeholder="123456789:ABCDefgh..."
                    value={channelForm.config.bot_token}
                    onChange={(e) => setChannelForm(prev => ({
                      ...prev,
                      config: { ...prev.config, bot_token: e.target.value }
                    }))}
                    className="w-full font-mono"
                  />
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">接收目标 Chat ID *</label>
                  <Input size="sm"
                    aria-label="接收目标 Chat ID"
                    type="text"
                    placeholder="例如：123456789 或 -100987654321"
                    value={channelForm.config.chat_id}
                    onChange={(e) => setChannelForm(prev => ({
                      ...prev,
                      config: { ...prev.config, chat_id: e.target.value }
                    }))}
                    className="w-full font-mono"
                  />
                </div>
              </>
            )}

            {/* Status enable toggle */}
            <div className="flex items-center justify-between border-t border-kumo-line pt-4 select-none">
              <span className="text-xs font-semibold text-kumo-strong">启用此通知渠道</span>
              <Switch
                checked={!!channelForm.enabled}
                onCheckedChange={(checked) => setChannelForm(prev => ({ ...prev, enabled: checked }))}
                size="sm"
              />
            </div>
          </div>

          <div className="flex justify-end gap-3 mt-6 border-t border-kumo-line pt-4 select-none">
            <Dialog.Close
              render={(props) => (
                <Button size="sm" {...props} variant="secondary">
                  取消
                </Button>
              )}
            />
            <Button size="sm" variant="primary" onClick={handleSaveChannel} loading={notificationSaving} icon={<Save className="w-3.5 h-3.5" />}>
              保存渠道
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* ==================== 7. 弹窗 2: 添加/编辑规则 ==================== */}
      <Dialog.Root open={showRuleModal} onOpenChange={setShowRuleModal}>
        <Dialog className="p-6 sm:max-w-xl">
          <Dialog.Title className="text-base font-bold text-kumo-strong mb-1 select-none">
            {ruleForm.id ? '编辑告警规则' : '添加告警规则'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4 select-none">
            配置匹配的触发条件与投递渠道
          </Dialog.Description>

          <div className="-mx-1 space-y-4 max-h-[60vh] overflow-y-auto px-1 pr-2 scrollbar-thin">
            {/* Rule Name */}
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">规则名称 *</label>
              <Input size="sm"
                aria-label="规则名称"
                type="text"
                placeholder="例如：数据库故障告警"
                value={ruleForm.name}
                onChange={(e) => setRuleForm(prev => ({ ...prev, name: e.target.value }))}
                className="w-full"
              />
            </div>

            {/* Source & Event Type */}
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">来源监控模块</label>
                <Select size="sm"
                  aria-label="来源监控模块"
                  value={ruleForm.source_module}
                  onValueChange={(value) => handleSourceModuleChange(String(value))}
                  className="w-full"
                  items={catalogModuleItems}
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">触发事件类型</label>
                <Select size="sm"
                  aria-label="触发事件类型"
                  value={ruleForm.event_type}
                  onValueChange={(value) => setRuleForm(prev => ({ ...prev, event_type: String(value) }))}
                  className="w-full"
                  items={catalogEventItems}
                />
              </div>
            </div>

            {/* Severity Level */}
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">告警紧急级别</label>
              <Select size="sm"
                aria-label="告警紧急级别"
                value={ruleForm.severity}
                onValueChange={(value) => setRuleForm(prev => ({ ...prev, severity: String(value) }))}
                className="w-full"
                items={[
                  { value: 'info', label: '常规通知 (Info)' },
                  { value: 'warning', label: '重要警告 (Warning)' },
                  { value: 'critical', label: '紧急呼叫 (Critical)' },
                ]}
              />
            </div>

            {/* Target Delivery Channels Checkboxes */}
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">发送通知的渠道 *</label>
              <div className="flex flex-wrap gap-2.5 p-3.5 app-subcard bg-kumo-recessed/50">
                {notificationChannels.filter(c => c.enabled).map((channel) => (
                  <Checkbox
                    key={channel.id}
                    checked={ruleForm.channels.includes(String(channel.id))}
                    onCheckedChange={(checked) => {
                      const id = String(channel.id);
                      setRuleForm(prev => ({
                        ...prev,
                        channels: checked
                          ? [...prev.channels, id]
                          : prev.channels.filter(x => x !== id)
                      }));
                    }}
                    label={channel.name}
                  />
                ))}
              </div>
            </div>

            {/* Repeats & Cooldown Suppression */}
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">累计触发次数再告警</label>
                <Input size="sm"
                  aria-label="累计触发次数再告警"
                  type="number"
                  min="1"
                  value={ruleForm.suppression.repeat_count}
                  onChange={(e) => setRuleForm(prev => ({
                    ...prev,
                    suppression: { ...prev.suppression, repeat_count: parseInt(e.target.value) || 1 }
                  }))}
                  className="w-full font-mono"
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">冷却静默期 (分钟)</label>
                <Input size="sm"
                  aria-label="冷却静默期"
                  type="number"
                  min="0"
                  value={ruleForm.suppression.silence_minutes}
                  onChange={(e) => setRuleForm(prev => ({
                    ...prev,
                    suppression: { ...prev.suppression, silence_minutes: parseInt(e.target.value) || 0 }
                  }))}
                  className="w-full font-mono"
                />
              </div>
            </div>

            {/* Backup Notification Channels Checkboxes */}
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">首选失败时的备用通知渠道</label>
              <div className="flex flex-wrap gap-2.5 p-3.5 app-subcard bg-kumo-recessed/50">
                {notificationChannels.filter(c => c.enabled).map((channel) => (
                  <Checkbox
                    key={`backup_${channel.id}`}
                    checked={ruleForm.backup_channels.includes(String(channel.id))}
                    onCheckedChange={(checked) => {
                      const id = String(channel.id);
                      setRuleForm(prev => ({
                        ...prev,
                        backup_channels: checked
                          ? [...prev.backup_channels, id]
                          : prev.backup_channels.filter(x => x !== id)
                      }));
                    }}
                    label={channel.name}
                  />
                ))}
              </div>
            </div>

            {/* Custom Template Titles */}
            <div className="space-y-1.5">
              <div className="flex items-center justify-between gap-2">
                <label className="text-xs font-semibold text-kumo-subtle">自定义标题模板 (可选，支持 {'{{变量}}'})</label>
                <Button
                  size="sm"
                  variant="secondary"
                  onClick={handlePreviewTemplate}
                  loading={notificationSaving}
                  icon={<CheckDouble className="w-3.5 h-3.5" />}
                >
                  预览
                </Button>
              </div>
              <Input size="sm"
                aria-label="自定义标题模板"
                type="text"
                placeholder="例: 🚨 [{{severity}}] 主机 {{serverName}} 离线!"
                value={ruleForm.title_template}
                onChange={(e) => setRuleForm(prev => ({ ...prev, title_template: e.target.value }))}
                className="w-full"
              />
            </div>

            {/* Custom Template Content */}
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">自定义内容模板 (可选)</label>
              <Textarea
                aria-label="自定义内容模板"
                placeholder="例: 服务 {{monitorName}} 无法连通，出错原因: {{error}}"
                value={ruleForm.message_template}
                onChange={(e) => setRuleForm(prev => ({ ...prev, message_template: e.target.value }))}
                className="w-full min-h-16"
              />
            </div>

            {templatePreview && (
              <div className="rounded-lg border border-kumo-line bg-kumo-recessed/50 p-3">
                <div className="text-[11px] font-bold text-kumo-strong">模板预览</div>
                <div className="mt-2 rounded border border-kumo-line bg-kumo-base p-2 text-xs font-semibold text-kumo-strong">
                  {templatePreview.title}
                </div>
                <div className="mt-2 max-h-28 overflow-y-auto whitespace-pre-wrap rounded border border-kumo-line bg-kumo-base p-2 font-mono text-[11px] leading-relaxed text-kumo-subtle">
                  {templatePreview.message}
                </div>
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {(templatePreview.variables || []).map((variable) => (
                    <span key={variable} className="rounded border border-kumo-line bg-kumo-base px-1.5 py-0.5 font-mono text-[9px] text-kumo-subtle">
                      {`{{${variable}}}`}
                    </span>
                  ))}
                </div>
              </div>
            )}

            {/* Quiet until */}
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">手动全局静默直至 (在此时间前屏蔽此规则)</label>
              <Input size="sm"
                aria-label="手动全局静默直至"
                type="datetime-local"
                value={ruleForm.quiet_until}
                onChange={(e) => setRuleForm(prev => ({ ...prev, quiet_until: e.target.value }))}
                className="w-full font-mono"
              />
            </div>

            {/* Rule Status Switch */}
            <div className="flex items-center justify-between border-t border-kumo-line pt-4 select-none">
              <span className="text-xs font-semibold text-kumo-strong">启用此告警规则</span>
              <Switch
                checked={!!ruleForm.enabled}
                onCheckedChange={(checked) => setRuleForm(prev => ({ ...prev, enabled: checked }))}
                size="sm"
              />
            </div>
          </div>

          <div className="flex justify-end gap-3 mt-6 border-t border-kumo-line pt-4 select-none">
            <Dialog.Close
              render={(props) => (
                <Button size="sm" {...props} variant="secondary">
                  取消
                </Button>
              )}
            />
            <Button size="sm" variant="primary" onClick={handleSaveRule} loading={notificationSaving} icon={<Save className="w-3.5 h-3.5" />}>
              保存规则
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default NotificationPage;
