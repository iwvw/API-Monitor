import React, { useState, useEffect, useMemo } from 'react';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { collapseNotificationHistory, parseLifecycleHistoryMeta } from '../modules/notificationHistory.js';

// SQLite CURRENT_TIMESTAMP 是空格格式（'YYYY-MM-DD HH:mm:ss'），Safari 的
// Date 无法直接解析；统一归一化为 ISO 后再格式化，解析失败返回原串。
const formatHistoryDate = (raw) => {
  if (!raw) return '';
  const iso = String(raw).includes(' ') ? String(raw).replace(' ', 'T') : String(raw);
  const date = new Date(iso);
  return Number.isNaN(date.getTime()) ? String(raw) : date.toLocaleString();
};
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Tabs } from '@cloudflare/kumo';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { AppCard, SectionCard, TabBarOverflowActions, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
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
    uptime: '可用性监测',
    server: '主机实例',
    github: 'GitHub',
    openai: 'OpenAI 接口',
    system: '系统设置',
    filebox: '文件柜',
    totp: '双因子认证',
    cron: '定时任务',
  };
  return names[module] || module;
};

const getEventTypeName = (type) => {
  const names = {
    down: '服务宕机 (Down)',
    up: '服务恢复 (Up)',
    offline: '主机离线',
    online: '主机上线',
    interrupted: '连接中断',
    degraded: '采集异常',
    cpu_high: 'CPU高负载',
    cpu_normal: 'CPU恢复正常',
    memory_high: '内存不足',
    memory_normal: '内存恢复',
    disk_high: '磁盘空间不足',
    disk_normal: '磁盘恢复正常',
    traffic_high: '流量超额',
    traffic_normal: '流量恢复',
    balance_low: '余额不足',
    log_too_large: '日志体积过大',
    pending: '状态待确认',
    ssl_expiry: 'SSL 证书即将到期',
    'resource.created': '资源已创建',
    'resource.updated': '资源已更新',
    'resource.deleted': '资源已删除',
    'security.revealed': '密钥已查看',
    'backup.imported': '备份已导入',
    'backup.exported': '备份已导出',
    cleanup: '清理任务',
    'database.backup': '数据库备份',
    'database.import': '数据库导入',
    'log.cleanup': '日志清理',
    'migration.failed': '迁移失败',
    login: '登录',
    logout: '登出',
    'playback.error': '播放错误',
    'proxy.blocked': '代理拦截',
    action_failed: 'Actions 执行失败',
    action_recovered: 'Actions 恢复正常',
    release_published: '发布新版本',
    star_spike: 'Star 激增',
    issue_opened: '新增 Issue',
    pull_request_opened: '新增拉取请求',
    repository_unreachable: '仓库无法访问',
    token_invalid: 'Token 已失效',
    rate_limit_low: 'API 限额偏低',
    webhook_delivery_failed: 'Webhook 投递失败',
    webhook_ping: 'Webhook 连通成功',
    'task.completed': '定时任务执行完成',
    'task.failed': '定时任务执行失败',
    'workflow.completed': '工作流执行完成',
    'workflow.failed': '工作流执行失败',
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
  { module: 'uptime', events: ['down', 'up', 'pending', 'resource.created', 'resource.deleted', 'ssl_expiry'], dynamic_events: ['down', 'up'] },
  { module: 'server', events: ['offline', 'online', 'interrupted', 'degraded', 'cpu_high', 'cpu_normal', 'memory_high', 'memory_normal', 'disk_high', 'disk_normal', 'traffic_high', 'traffic_normal'], dynamic_events: ['offline', 'online', 'interrupted', 'degraded', 'cpu_high', 'cpu_normal', 'memory_high', 'memory_normal', 'disk_high', 'disk_normal', 'traffic_high', 'traffic_normal'] },
  { module: 'github', events: ['action_failed', 'action_recovered', 'release_published', 'star_spike', 'issue_opened', 'pull_request_opened', 'repository_unreachable', 'token_invalid', 'rate_limit_low', 'webhook_delivery_failed', 'webhook_ping'], dynamic_events: ['action_failed', 'action_recovered'] },
  { module: 'system', events: ['database.backup', 'database.import', 'log.cleanup', 'migration.failed', 'cpu_high', 'cpu_normal', 'memory_high', 'memory_normal', 'disk_high', 'disk_normal'], dynamic_events: ['cpu_high', 'cpu_normal', 'memory_high', 'memory_normal', 'disk_high', 'disk_normal'] },
  { module: 'filebox', events: ['resource.created', 'resource.deleted', 'cleanup'] },
  { module: 'totp', events: ['resource.created', 'resource.updated', 'resource.deleted', 'security.revealed', 'backup.imported', 'backup.exported'] },
  { module: 'cron', events: ['task.completed', 'task.failed', 'workflow.completed', 'workflow.failed'] },
];

const buildSampleEventData = (rule = {}) => {
  if (rule.source_module === 'cron') {
    const isWorkflow = (rule.event_type || '').startsWith('workflow');
    return {
      severity: rule.severity || 'info',
      eventType: rule.event_type || 'task.completed',
      taskId: 12,
      taskName: '每日 Token 用量统计',
      workflowId: 8,
      workflowName: '每日Token用量分析',
      status: 'success',
      summary: '成功 3，失败 0，跳过 0',
      output: '读取 GET /api/openai/analytics/summary 完成，总量 1,283,990 tokens',
      duration: 11,
      triggerType: 'cron',
      time: new Date().toLocaleString('zh-CN'),
      ...(isWorkflow ? { workflowId: 8, workflowName: '每日Token用量分析' } : { taskId: 12, taskName: '每日 Token 用量统计' }),
    };
  }
  return {
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
    disk_usage: 91,
    traffic_percent: 86.35,
    traffic_used: '863.5 GB',
    traffic_limit: '1 TB',
    threshold: 90,
    downDuration: '3 分钟',
  };
};

const parseNotificationPreviewLine = (line = '') => {
  const trimmed = line.trim();
  if (!trimmed) return { empty: true };
  const asciiIndex = trimmed.indexOf(':');
  const chineseIndex = trimmed.indexOf('：');
  const separator = asciiIndex < 0
    ? chineseIndex
    : (chineseIndex < 0 ? asciiIndex : Math.min(asciiIndex, chineseIndex));
  if (separator <= 0) return { value: trimmed };
  const label = trimmed.slice(0, separator).trim();
  const rawValue = trimmed.slice(separator + 1).trim();
  const statusIcons = {
    在线: '🟢', 已恢复: '🟢', 成功: '🟢',
    离线: '🔴', 故障: '🔴', 失败: '🔴',
    中断: '🟠', 告警: '🟠', 警告: '🟠', 采集异常: '🟠',
  };
  return {
    label,
    value: label === '状态' && statusIcons[rawValue]
      ? `${statusIcons[rawValue]} ${rawValue}`
      : rawValue,
    code: ['地址', '链接', '云端链接', 'URL', 'Host'].includes(label),
  };
};

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
  const handleOpenAddRule = (overrides = {}) => {
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
  };

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
        <div className="space-y-4">
          {notificationLoading && notificationChannels.length === 0 ? (
            <div className="grid grid-cols-1 cq-md:grid-cols-2 cq-lg:grid-cols-3 cq-xl:grid-cols-4 gap-4">
              {[...Array(4)].map((_, i) => (
                <AppCard key={i} padding="none" className="space-y-4 p-4">
                  <div className="flex min-w-0 items-start justify-between gap-3">
                    <SkeletonLine className="w-8 h-8 rounded-lg" />
                    <div className="flex-1 space-y-1.5">
                      <SkeletonLine className="w-1/2 h-3.5" />
                      <SkeletonLine className="w-1/3 h-2.5" />
                    </div>
                  </div>
                  <SkeletonLine className="w-full h-1" />
                </AppCard>
              ))}
            </div>
          ) : notificationChannels.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle app-empty-panel">
              <Bell className="w-12 h-12 opacity-30 mb-4" />
              <div className="text-sm">暂无通知渠道，配置通知以便故障时接收提醒</div>
              <Button size="sm" variant="primary" className="mt-4" onClick={handleOpenAddChannel}>
                创建第一个渠道
              </Button>
            </div>
          ) : (
            <div className="grid grid-cols-1 cq-md:grid-cols-2 cq-lg:grid-cols-3 cq-xl:grid-cols-4 gap-4">
              {notificationChannels.map((channel) => (
                <AppCard
                  key={channel.id}
                  padding="none"
                  interactive
                  className="flex min-h-[128px] flex-col justify-between p-4 transition-all duration-200 hover:border-brand/50 hover:shadow-sm"
                >
                  <div className="flex min-w-0 items-start justify-between gap-3">
                    {/* Icon */}
                    <div className={`w-8 h-8 rounded-lg flex items-center justify-center text-kumo-inverse text-base flex-shrink-0 shadow-xs ${
                      channel.type === 'email' ? 'bg-kumo-info' : 'bg-kumo-brand'
                    }`}>
                      {channel.type === 'email' ? <Mail className="w-4 h-4" /> : <Send className="w-4 h-4" />}
                    </div>

                    {/* Information */}
                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-1.5 min-w-0">
                        <h4 className="text-xs font-bold text-kumo-strong truncate leading-tight" title={channel.name}>
                          {channel.name}
                        </h4>
                        <span className={`h-1.5 w-1.5 rounded-full shrink-0 ${channel.enabled ? 'bg-kumo-success' : 'bg-kumo-subtle/50'}`} />
                      </div>
                      <p className="text-[10px] text-kumo-subtle mt-1 select-none font-medium">
                        {getChannelTypeName(channel.type)}
                      </p>
                    </div>

                    {/* Actions */}
                    <div className="flex items-center gap-1.5 flex-shrink-0">
                      <Button
                        onClick={() => handleTestChannel(channel.id)}
                        variant="secondary" size="sm"
                        shape="square"
                        aria-label="测试投递"
                        title="测试投递"
                        icon={<Send className="w-3 h-3" />}
                      />
                      <Button
                        onClick={() => handleOpenEditChannel(channel)}
                        variant="secondary" size="sm"
                        shape="square"
                        aria-label="编辑通知渠道"
                        title="编辑"
                        icon={<Edit className="w-3.5 h-3.5" />}
                      />
                      <Button
                        onClick={() => handleDeleteChannel(channel.id)}
                        variant={isArmed(`channel:${channel.id}`) ? 'destructive' : 'secondary-destructive'} size="sm"
                        shape="square"
                        aria-label="删除通知渠道"
                        title="删除"
                        icon={<Trash className="w-3.5 h-3.5" />}
                      />
                    </div>
                  </div>

                  <div className="flex items-center justify-between border-t border-kumo-line/60 pt-2.5 mt-3 select-none">
                    <span className={`text-[10px] font-semibold flex items-center gap-1.5 ${
                      channel.enabled ? 'text-kumo-success' : 'text-kumo-subtle'
                    }`}>
                      <span className={`w-1.5 h-1.5 rounded-full ${channel.enabled ? 'bg-kumo-success animate-pulse' : 'bg-kumo-subtle'}`} />
                      {channel.enabled ? '已启用投递' : '已暂停投递'}
                    </span>
                  </div>
                </AppCard>
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
              items={ruleFilterItems}
            />

            <Button
              onClick={loadNotificationRules}
              loading={notificationLoading}
              variant="secondary" size="sm"
              shape="square"
              aria-label="刷新告警规则"
              className="text-kumo-subtle hover:text-kumo-strong"
              title="刷新"
              icon={<RotateCw className="w-3.5 h-3.5" />}
            />
          </div>

          {notificationLoading && notificationRules.length === 0 ? (
            <div className="grid grid-cols-1 cq-md:grid-cols-2 cq-lg:grid-cols-3 gap-4">
              {[...Array(3)].map((_, i) => (
                <AppCard key={i} padding="none" className="space-y-4 p-4">
                  <div className="flex items-start justify-between gap-3">
                    <SkeletonLine className="w-8 h-8 rounded-lg" />
                    <div className="flex-1 space-y-1.5">
                      <SkeletonLine className="w-2/3 h-3.5" />
                      <SkeletonLine className="w-1/3 h-2.5" />
                    </div>
                  </div>
                  <SkeletonLine className="w-full h-1" />
                </AppCard>
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
            <div className="grid grid-cols-1 cq-md:grid-cols-2 cq-lg:grid-cols-3 gap-4">
              {filteredRules.map((rule) => (
                <AppCard
                  key={rule.id}
                  padding="none"
                  interactive
                  className={`flex min-h-[148px] flex-col justify-between p-4 transition-all duration-200 hover:border-brand/50 hover:shadow-sm ${
                    highlightRuleId === rule.id ? 'border-brand/60 ring-2 ring-brand/20' : ''
                  }`}
                >
                  <div className="flex items-start justify-between gap-3">
                    {/* Severity Indicator */}
                    <div className={`w-8 h-8 rounded-lg flex items-center justify-center text-kumo-inverse text-base flex-shrink-0 shadow-xs ${
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
                        <span className="text-xs font-bold text-kumo-strong truncate leading-tight" title={rule.name}>
                          {rule.name}
                        </span>
                        <span className={`text-[8.5px] px-1.5 py-0.5 rounded font-bold uppercase select-none ${
                          rule.severity === 'critical'
                            ? 'bg-kumo-danger/10 text-kumo-danger border border-kumo-danger/20'
                            : rule.severity === 'warning'
                            ? 'bg-kumo-warning/10 text-kumo-warning border border-kumo-warning/20'
                            : 'bg-kumo-info/10 text-kumo-info border border-kumo-info/20'
                        }`}>
                          {rule.severity}
                        </span>
                      </div>
                      <div className="flex items-center gap-1.5 mt-2 flex-wrap select-none">
                        <Badge className="bg-kumo-recessed text-[9px] font-medium text-kumo-subtle border border-kumo-line/40">
                          {getSourceModuleName(rule.source_module)}
                        </Badge>
                        <Badge className="bg-kumo-recessed text-[9px] font-medium text-kumo-subtle border border-kumo-line/40">
                          {getEventTypeName(rule.event_type)}
                        </Badge>
                      </div>
                    </div>

                    {/* Actions */}
                    <div className="flex items-center gap-1.5 flex-shrink-0">
                      <Button
                        onClick={() => handleDryRunRule(rule)}
                        variant="secondary" size="sm"
                        shape="square"
                        aria-label="预演告警规则"
                        title="Dry-run"
                        loading={dryRunLoadingId === rule.id}
                        icon={<CheckDouble className="w-3.5 h-3.5" />}
                      />
                      <Button
                        onClick={() => handleOpenEditRule(rule)}
                        variant="secondary" size="sm"
                        shape="square"
                        aria-label="编辑告警规则"
                        title="编辑"
                        icon={<Edit className="w-3.5 h-3.5" />}
                      />
                      <Button
                        onClick={() => handleDeleteRule(rule.id)}
                        variant={isArmed(`rule:${rule.id}`) ? 'destructive' : 'secondary-destructive'} size="sm"
                        shape="square"
                        aria-label="删除告警规则"
                        title="删除"
                        icon={<Trash className="w-3.5 h-3.5" />}
                      />
                    </div>
                  </div>

                  {/* Settings status summary */}
                  <div className="border-t border-kumo-line/60 pt-3 mt-3.5 space-y-2">
                    {rule.suppression && (
                      <div className="flex items-center gap-1.5 text-[10px] text-kumo-subtle font-mono select-none">
                        <span>• 重复抑制: {rule.suppression.repeat_count || 1} 次</span>
                        <span className="text-kumo-subtle/30">/</span>
                        <span>静默期: {rule.suppression.silence_minutes || 0} 分钟</span>
                      </div>
                    )}
                    <div className="flex items-center justify-between">
                      <span className={`text-[10px] font-semibold flex items-center gap-1.5 ${
                        rule.enabled ? 'text-kumo-success' : 'text-kumo-subtle'
                      }`}>
                        <span className={`w-1.5 h-1.5 rounded-full ${rule.enabled ? 'bg-kumo-success animate-pulse' : 'bg-kumo-subtle'}`} />
                        {rule.enabled ? '已开启规则' : '已禁用规则'}
                      </span>
                      <Button
                        onClick={() => handleToggleRuleEnabled(rule)}
                        variant="secondary" size="sm"
                        className="h-6 text-[10px] font-semibold px-2"
                      >
                        {rule.enabled ? '一键禁用' : '一键启用'}
                      </Button>
                    </div>
                    {dryRunResults[rule.id] && (
                      <div className="rounded-md border border-kumo-line/60 bg-kumo-recessed/40 p-2.5 text-[10px] leading-relaxed text-kumo-subtle space-y-1">
                        <div className="flex items-center justify-between gap-2">
                          <span className="font-bold text-kumo-strong">
                            {dryRunResults[rule.id].wouldNotify ? '✅ 预演结果：触发发送' : 'ℹ️ 预演结果：未触发'}
                          </span>
                          <span className="font-mono text-[9px] text-kumo-subtle">{dryRunResults[rule.id].fingerprint}</span>
                        </div>
                        <div className="truncate font-mono text-[10px] text-kumo-subtle">{dryRunResults[rule.id].title}</div>
                      </div>
                    )}
                  </div>
                </AppCard>
              ))}
            </div>
          )}
        </div>
      )}

      {/* ==================== 事件目录 Tab ==================== */}
      {notificationCurrentTab === 'events' && (
        <div className="space-y-4">
          <div className="flex flex-wrap items-center gap-2 text-[10px] text-kumo-subtle">
            <Badge className="border border-kumo-brand/25 bg-kumo-brand/10 text-kumo-brand">↻ 动态消息</Badge>
            <span>同一 Telegram 消息会随告警打开、变化和恢复持续更新；每次变化仍会写入通知历史。</span>
          </div>
          <div className="columns-1 gap-4 cq-md:columns-2 cq-xl:columns-3 [&>*]:mb-4 [&>*]:break-inside-avoid [&>*:last-child]:mb-0">
            {notificationEventCatalog.map((item) => (
              <SectionCard
                key={item.module}
                title={getSourceModuleName(item.module)}
                icon={<Info className="w-4 h-4 text-kumo-brand" />}
                meta={<span className="text-[10px] font-mono text-kumo-subtle">{item.events?.length || 0}</span>}
                bodyClassName="p-4"
              >
                <div className="flex flex-wrap gap-2">
                  {(item.events || []).map((eventName) => {
                    const isDynamic = (item.dynamic_events || []).includes(eventName);
                    return isDynamic ? (
                      <Badge
                        key={`${item.module}-${eventName}`}
                        title="支持 Telegram 动态消息"
                        className="border border-kumo-brand/25 bg-kumo-brand/10 text-[10px] font-semibold text-kumo-brand"
                      >
                        ↻ {getEventTypeName(eventName)}
                      </Badge>
                    ) : (
                      <span
                        key={`${item.module}-${eventName}`}
                        className="rounded border border-kumo-line bg-kumo-recessed px-2 py-1 text-[10px] font-semibold text-kumo-subtle"
                      >
                        {getEventTypeName(eventName)}
                      </span>
                    );
                  })}
                </div>
              </SectionCard>
            ))}
          </div>
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
                loading={notificationLoading}
              variant="secondary" size="sm"
              shape="square"
              aria-label="刷新通知历史"
              className="text-kumo-subtle hover:text-kumo-strong"
              title="刷新"
                icon={<RotateCw className="w-3.5 h-3.5" />}
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
                <AppCard key={i} padding="none" className="space-y-3 p-4">
                  <div className="flex items-center justify-between">
                    <SkeletonLine className="w-1/4 h-3.5" />
                    <SkeletonLine className="w-1/6 h-2.5" />
                  </div>
                  <SkeletonLine className="w-full h-12 rounded-md" />
                </AppCard>
              ))}
            </div>
          ) : filteredHistory.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle app-empty-panel">
              <History className="w-12 h-12 opacity-30 mb-4" />
              <div className="text-sm">暂无匹配的通知历史记录</div>
            </div>
          ) : (
            <div className="flex flex-col gap-2.5">
              {filteredHistory.map((log) => {
                const lifecycleMeta = log.lifecycle_meta || parseLifecycleHistoryMeta(log.data);
                const mutationLabel = lifecycleMeta ? ({
                  open: '告警打开',
                  refresh: '动态更新',
                  resolve: '告警恢复',
                }[lifecycleMeta.mutation] || lifecycleMeta.mutation) : null;

                // 从 data JSON 提取来源模块与事件类型（历史行无独立列）
                let logEventType = '';
                let logSourceModule = '';
                if (log.data) {
                  try {
                    const parsed = typeof log.data === 'string' ? JSON.parse(log.data) : log.data;
                    logEventType = parsed.eventType || parsed.event_type || '';
                    logSourceModule = parsed.sourceModule || parsed.source_module || '';
                  } catch { /* ignore malformed data */ }
                }

                // 获取匹配的通知渠道名称
                const matchedChannel = notificationChannels.find(c => String(c.id) === String(log.channel_id));
                const channelDisplayName = log.channel_name || matchedChannel?.name || (log.channel_type ? getChannelTypeName(log.channel_type) : null);

                return (
                  <AppCard
                    key={log.id}
                    padding="none"
                    className="grid grid-cols-1 gap-3 p-3.5 transition-all duration-200 hover:border-brand/40 hover:shadow-sm cq-md:grid-cols-[280px_1fr] cq-md:items-start"
                  >
                    {/* 左栏：状态与元数据快照 (固定 280px 宽度，饱满工整) */}
                    <div className="flex flex-col gap-2 min-w-0 pr-0 cq-md:pr-3.5 cq-md:border-r cq-md:border-kumo-line/50">
                      {/* 1. 状态指示 + 标题 + 投递状态 */}
                      <div className="flex items-center justify-between gap-2 min-w-0">
                        <div className="flex items-center gap-2 min-w-0">
                          <span className={`h-2 w-2 rounded-full shrink-0 ${
                            log.status === 'sent'
                              ? 'bg-kumo-success animate-pulse'
                              : log.status === 'failed'
                                ? 'bg-kumo-danger'
                                : 'bg-kumo-warning'
                          }`} />
                          <span className="text-[13px] font-bold text-kumo-strong truncate leading-snug" title={log.title}>
                            {log.title}
                          </span>
                        </div>
                        <Badge className={`text-[10px] font-semibold px-2 py-0.5 border shrink-0 ${
                          log.status === 'sent'
                            ? 'bg-kumo-success/10 text-kumo-success border-kumo-success/20'
                            : log.status === 'failed'
                              ? 'bg-kumo-danger/10 text-kumo-danger border-kumo-danger/20'
                              : 'bg-kumo-warning/10 text-kumo-warning border-kumo-warning/20'
                        }`}>
                          {log.status === 'sent' ? '发送成功' : log.status === 'failed' ? '发送失败' : '队列处理中'}
                        </Badge>
                      </div>

                      {/* 2. 投递渠道标识 (Notification Channel) */}
                      {channelDisplayName && (
                        <div className="flex items-center gap-1.5 text-[11px] text-kumo-subtle select-none">
                          <span className="font-medium">📢 渠道:</span>
                          <span className="rounded border border-kumo-line/60 bg-kumo-recessed/60 px-1.5 py-0.5 text-[10px] font-medium text-kumo-strong">
                            {channelDisplayName}
                          </span>
                        </div>
                      )}

                      {/* 2.5 来源模块与事件类型标识（cron 等链路可溯源） */}
                      {(logSourceModule || logEventType) && (
                        <div className="flex flex-wrap items-center gap-1.5 text-[10px]">
                          {logSourceModule && (
                            <Badge className="border border-kumo-brand/25 bg-kumo-brand/10 text-[10px] font-semibold text-kumo-brand py-0.5 px-2">
                              {getSourceModuleName(logSourceModule)}
                            </Badge>
                          )}
                          {logEventType && (
                            <span className="rounded border border-kumo-line/60 bg-kumo-recessed/60 px-1.5 py-0.5 font-mono text-[10px] text-kumo-subtle">
                              {getEventTypeName(logEventType.replace(/^cron\./, ''))}
                            </span>
                          )}
                        </div>
                      )}

                      {/* 3. 动态生命周期 Badges & 细节 */}
                      {lifecycleMeta && (
                        <div className="flex flex-wrap items-center gap-1.5 text-[10px]">
                          <Badge className="border border-kumo-brand/25 bg-kumo-brand/10 text-[10px] font-semibold text-kumo-brand py-0.5 px-2">
                            ↻ {mutationLabel}
                          </Badge>
                          {lifecycleMeta.kind && (
                            <span className="rounded bg-kumo-recessed px-1.5 py-0.5 font-mono text-[10px] text-kumo-subtle border border-kumo-line/40">
                              {lifecycleMeta.kind}
                            </span>
                          )}
                          {log.lifecycle_update_count > 1 && (
                            <span className="rounded border border-kumo-brand/20 bg-kumo-brand/5 px-1.5 py-0.5 text-[10px] font-medium text-kumo-subtle">
                              更新 {log.lifecycle_update_count - 1} 次
                            </span>
                          )}
                          {lifecycleMeta.duration && (
                            <span className="text-[11px] text-kumo-subtle">
                              持续 {lifecycleMeta.duration}
                            </span>
                          )}
                          {lifecycleMeta.changedFields.length > 0 && (
                            <span className="text-[11px] text-kumo-subtle" title={lifecycleMeta.changedFields.join(', ')}>
                              变化 {lifecycleMeta.changedFields.length} 项
                            </span>
                          )}
                        </div>
                      )}

                      {/* 4. 时间记录 */}
                      <div className="font-mono text-[11px] text-kumo-subtle select-none pt-0.5 flex items-center gap-1">
                        <span className="opacity-60">🕒</span>
                        {formatHistoryDate(log.created_at)}
                      </div>
                      {log.lifecycle_update_count > 1 && log.lifecycle_first_created_at && log.lifecycle_first_created_at !== log.created_at && (
                        <div className="font-mono text-[11px] text-kumo-subtle/80 select-none flex items-center gap-1">
                          <span className="opacity-50">↺</span>
                          首次告警 {formatHistoryDate(log.lifecycle_first_created_at)}
                        </div>
                      )}
                    </div>

                    {/* 右栏：详细消息内容与异常/重试提示 */}
                    <div className="min-w-0 flex-1 space-y-1.5">
                      {log.message && (
                        <div className="rounded-md border border-kumo-line/60 bg-kumo-recessed/30 px-3.5 py-2.5 font-mono text-[11px] leading-relaxed text-kumo-subtle whitespace-pre-wrap break-all">
                          {log.message}
                        </div>
                      )}

                      {(log.error_message || log.retry_count > 0) && (
                        <div className="flex flex-wrap items-center gap-2 pt-0.5 select-none">
                          {log.error_message && (
                            <span className="rounded border border-kumo-danger/20 bg-kumo-danger/10 px-2 py-0.5 text-[11px] font-semibold text-kumo-danger">
                              报错: {log.error_message}
                            </span>
                          )}
                          {log.retry_count > 0 && (
                            <span className="rounded border border-kumo-warning/20 bg-kumo-warning/10 px-2 py-0.5 font-mono text-[11px] font-semibold text-kumo-warning">
                              重试: {log.retry_count} 次
                            </span>
                          )}
                        </div>
                      )}
                    </div>
                  </AppCard>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* ==================== 4. 全局配置 Tab ==================== */}
      {notificationCurrentTab === 'settings' && (
        <SectionCard
          title="全局配置选项"
          icon={<Settings className="w-4 h-4 text-kumo-brand" />}
          bodyPadding="sm"
          bodyClassName="space-y-4"
        >
          <div className="grid gap-4">
            <Input
              size="sm"
              label="看板基准 URL"
              description="设置后，通知会附带看板链接。"
              placeholder="https://monitor.domain.com"
              value={notificationGlobalConfig.base_url || ''}
              onChange={(e) => setNotificationGlobalConfig(prev => ({ ...prev, base_url: e.target.value }))}
            />

            <div className="grid gap-4 cq-sm:grid-cols-2">
              <Input
                size="sm"
                label="全局限频（条/小时）"
                description="每小时通知上限；超限后仅推 Critical。"
                type="number"
                min="0"
                value={notificationGlobalConfig.global_rate_limit_per_hour ?? 100}
                onChange={(e) => setNotificationGlobalConfig(prev => ({ ...prev, global_rate_limit_per_hour: parseInt(e.target.value) || 0 }))}
              />
              <Input
                size="sm"
                label="聚合窗口（秒）"
                description="窗口内同渠道通知合并发送。"
                type="number"
                min="0"
                value={notificationGlobalConfig.batch_interval_seconds ?? 30}
                onChange={(e) => setNotificationGlobalConfig(prev => ({ ...prev, batch_interval_seconds: parseInt(e.target.value) || 0 }))}
              />
            </div>

            <div className="flex items-center justify-between rounded-lg border border-kumo-line bg-kumo-recessed/30 p-3.5">
              <div>
                <div className="text-xs font-semibold text-kumo-strong">启用通知聚合</div>
                <div className="mt-0.5 text-[11px] text-kumo-subtle">窗口内相同告警合并发送。</div>
              </div>
              <Switch
                size="sm"
                checked={!!notificationGlobalConfig.enable_batch}
                onCheckedChange={(checked) => setNotificationGlobalConfig(prev => ({ ...prev, enable_batch: checked }))}
              />
            </div>
          </div>

          <div className="flex justify-end border-t border-kumo-line pt-3 mt-2">
            <Button size="sm" variant="primary" onClick={handleSaveGlobalConfig} loading={notificationSaving} icon={<Save className="w-3.5 h-3.5" />}>
              保存全局配置
            </Button>
          </div>
        </SectionCard>
      )}

      {/* ==================== 6. 弹窗 1: 添加/编辑通道 ==================== */}
      <Dialog.Root open={showChannelModal} onOpenChange={setShowChannelModal}>
        <Dialog className="flex max-h-[calc(100dvh-1rem)] flex-col overflow-hidden !w-[min(40rem,calc(100vw-2rem))] !max-w-[min(40rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="text-base font-bold text-kumo-strong mb-1 select-none">
            {channelForm.id ? '编辑通知渠道' : '新建通知渠道'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4 select-none">
            配置告警投递渠道
          </Dialog.Description>

          <div className="-mx-1 min-h-0 flex-1 space-y-4 overflow-y-auto px-1 pb-2 pr-2 scrollbar-thin">
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
                placeholder="如：运维值班邮箱"
                value={channelForm.name}
                onChange={(e) => setChannelForm(prev => ({ ...prev, name: e.target.value }))}
                className="w-full"
              />
            </div>

            {/* Email configuration */}
            {channelForm.type === 'email' && (
              <>
                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">SMTP 主机 *</label>
                  <Input size="sm"
                    aria-label="SMTP 主机服务器地址"
                    type="text"
                    placeholder="smtp.gmail.com / smtp.exmail.qq.com"
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
                    <label className="text-xs font-semibold text-kumo-subtle">加密方式</label>
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
                  <label className="text-xs font-semibold text-kumo-subtle">发件邮箱 *</label>
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
                  <label className="text-xs font-semibold text-kumo-subtle">SMTP 授权码 *</label>
                  <Input size="sm"
                    aria-label="SMTP 授权口令"
                    type="text"
                    placeholder="your_smtp_app_password"
                    value={channelForm.config.auth.pass}
                    autoComplete="off"
                    data-1p-ignore
                    data-lpignore="true"
                    data-bwignore="true"
                    data-form-type="other"
                    spellCheck={false}
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
                  <label className="text-xs font-semibold text-kumo-subtle">发件人名（可选）</label>
                  <Input size="sm"
                    aria-label="发件人昵称"
                    type="text"
                    placeholder="告警机器人"
                    value={channelForm.config.sender_name}
                    onChange={(e) => setChannelForm(prev => ({
                      ...prev,
                      config: { ...prev.config, sender_name: e.target.value }
                    }))}
                    className="w-full"
                  />
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">收件邮箱 *</label>
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
                  <label className="text-xs font-semibold text-kumo-subtle">Chat ID *</label>
                  <Input size="sm"
                    aria-label="接收目标 Chat ID"
                    type="text"
                    placeholder="如：123456789 或 -100987654321"
                    value={channelForm.config.chat_id}
                    onChange={(e) => setChannelForm(prev => ({
                      ...prev,
                      config: { ...prev.config, chat_id: e.target.value }
                    }))}
                    className="w-full font-mono"
                  />
                </div>

                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-kumo-subtle">代理（可选）</label>
                  <Input size="sm"
                    aria-label="Telegram 代理地址"
                    type="text"
                    placeholder="如：http://127.0.0.1:7890"
                    value={channelForm.config.proxy_url || ''}
                    onChange={(e) => setChannelForm(prev => ({
                      ...prev,
                      config: { ...prev.config, proxy_url: e.target.value }
                    }))}
                    className="w-full font-mono"
                  />
                </div>
              </>
            )}

            {/* Status enable toggle */}
            <div className="flex items-center justify-between border-t border-kumo-line pt-4 select-none">
              <span className="text-xs font-semibold text-kumo-strong">启用渠道</span>
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
        <Dialog className="flex max-h-[calc(100dvh-1rem)] flex-col overflow-hidden !w-[min(48rem,calc(100vw-2rem))] !max-w-[min(48rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="text-base font-bold text-kumo-strong mb-1 select-none">
            {ruleForm.id ? '编辑告警规则' : '添加告警规则'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4 select-none">
            配置触发条件和投递渠道
          </Dialog.Description>

          <div className="-mx-1 min-h-0 flex-1 space-y-4 overflow-y-auto px-1 pb-2 pr-2 scrollbar-thin">
            {/* Rule Name */}
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">规则名称 *</label>
              <Input size="sm"
                aria-label="规则名称"
                type="text"
                placeholder="如：数据库故障告警"
                value={ruleForm.name}
                onChange={(e) => setRuleForm(prev => ({ ...prev, name: e.target.value }))}
                className="w-full"
              />
            </div>

            {/* Source & Event Type */}
            <div className="grid grid-cols-3 gap-3">
              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">来源模块</label>
                <Select size="sm"
                  aria-label="来源监控模块"
                  value={ruleForm.source_module}
                  onValueChange={(value) => handleSourceModuleChange(String(value))}
                  className="w-full"
                  items={catalogModuleItems}
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">事件类型</label>
                <Select size="sm"
                  aria-label="触发事件类型"
                  value={ruleForm.event_type}
                  onValueChange={(value) => setRuleForm(prev => ({ ...prev, event_type: String(value) }))}
                  className="w-full"
                  items={catalogEventItems}
                />
              </div>

              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">告警级别</label>
                <Select size="sm"
                  aria-label="告警紧急级别"
                  value={ruleForm.severity}
                  onValueChange={(value) => setRuleForm(prev => ({ ...prev, severity: String(value) }))}
                  className="w-full"
                  items={[
                    { value: 'info', label: '常规（Info）' },
                    { value: 'warning', label: '警告（Warning）' },
                    { value: 'critical', label: '紧急（Critical）' },
                  ]}
                />
              </div>
            </div>

            {/* Target Delivery Channels Checkboxes */}
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">通知渠道 *</label>
              <AppCard padding="none" className="flex flex-wrap gap-2.5 bg-kumo-recessed/50 p-3.5">
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
              </AppCard>
            </div>

            {/* Repeats & Cooldown Suppression */}
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-kumo-subtle">累计触发后告警</label>
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
                <label className="text-xs font-semibold text-kumo-subtle">静默期（分钟）</label>
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
              <label className="text-xs font-semibold text-kumo-subtle">失败时备用渠道</label>
              <AppCard padding="none" className="flex flex-wrap gap-2.5 bg-kumo-recessed/50 p-3.5">
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
              </AppCard>
            </div>

            {/* Custom Template Titles */}
            <div className="space-y-1.5">
              <div className="flex items-center justify-between gap-2">
                <label className="text-xs font-semibold text-kumo-subtle">标题模板（可选，支持 {'{{变量}}'}）</label>
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
                placeholder="如：[{{severity}}] {{serverName}} 离线"
                value={ruleForm.title_template}
                onChange={(e) => setRuleForm(prev => ({ ...prev, title_template: e.target.value }))}
                className="w-full"
              />
            </div>

            {/* Custom Template Content */}
            <div className="space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">内容模板（可选）</label>
              <Textarea
                aria-label="自定义内容模板"
                placeholder={'状态: 故障\n监控项: {{monitorName}}\n地址: {{url}}\n原因: {{error}}\n时间: {{time}}'}
                value={ruleForm.message_template}
                onChange={(e) => setRuleForm(prev => ({ ...prev, message_template: e.target.value }))}
                className="w-full min-h-16"
              />
            </div>

            {templatePreview && (
              <div className="overflow-hidden rounded-lg border border-kumo-line bg-kumo-base">
                <div className="h-1 bg-kumo-brand" />
                <div className="p-3.5">
                  <div className="text-[9px] font-bold uppercase text-kumo-brand">API Monitor</div>
                  <div className="mt-1 text-xs font-bold text-kumo-strong">{templatePreview.title}</div>
                  <div className="mt-3 max-h-36 space-y-1 overflow-y-auto border-l-2 border-kumo-brand bg-kumo-recessed/60 px-3 py-2">
                    {(templatePreview.message || '').split('\n').map((line, index) => {
                      const item = parseNotificationPreviewLine(line);
                      if (item.empty) return <div key={`empty-${index}`} className="h-1.5" />;
                      if (!item.label) return <div key={`line-${index}`} className="text-[11px] leading-relaxed text-kumo-subtle">{item.value}</div>;
                      return (
                        <div key={`${item.label}-${index}`} className="grid grid-cols-[88px_minmax(0,1fr)] gap-2 text-[11px] leading-relaxed">
                          <span className="text-kumo-subtle">{item.label}</span>
                          <span className={`min-w-0 break-words font-semibold text-kumo-strong ${item.code ? 'font-mono text-[10px]' : ''}`}>{item.value}</span>
                        </div>
                      );
                    })}
                  </div>
                </div>
                <div className="flex flex-wrap gap-1.5 px-3.5 pb-3.5">
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
              <label className="text-xs font-semibold text-kumo-subtle">静默至（此前不发送）</label>
              <Input size="sm"
                aria-label="手动全局静默直至"
                type="datetime-local"
                value={ruleForm.quiet_until}
                onChange={(e) => setRuleForm(prev => ({ ...prev, quiet_until: e.target.value }))}
                className="w-full font-mono"
              />
            </div>

            </div>

          <div className="flex items-center justify-between gap-3 mt-6 border-t border-kumo-line pt-4 select-none">
            <div className="flex items-center gap-2">
              <Switch
                checked={!!ruleForm.enabled}
                onCheckedChange={(checked) => setRuleForm(prev => ({ ...prev, enabled: checked }))}
                size="sm"
                aria-label="启用规则"
              />
              <span className="text-xs font-semibold text-kumo-strong">启用规则</span>
            </div>
            <div className="flex gap-3">
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
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default NotificationPage;
