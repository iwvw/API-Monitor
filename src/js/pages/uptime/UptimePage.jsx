import React, { useState, useEffect, useRef, useMemo } from 'react';
import io from 'socket.io-client';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Input } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Loader, Tabs, Toolbar } from '@cloudflare/kumo';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../../modules/kumoTabs.js';
import { AppCard, EmptyState, ResponsiveSearchInput, SectionCard, StatusBadge, TabBarOverflowActions, stickyTabsBaseClass } from '../../components/ui/AppPrimitives.jsx';
import useStore from '../../store.js';
import {
  Activity,
  Plus,
  Trash,
  RotateCw,
  Save,
  Globe,
  Terminal,
  Shield,
  Bell,
  Info,
  Download,
  Upload,
} from '../../components/Icons.jsx';
import { createEmptyStatusPageForm } from './constants.js';
import { uptimeTabs } from './tabs.jsx';
import {
  buildUptimeImportSections,
  getUptimeImportActionMeta,
  normalizeStatusDomain,
  normalizeStatusSlug,
  normalizeUptimeBeat,
} from './utils.js';
import MonitorCard from './MonitorCard.jsx';
import MaintenancePanel from './MaintenancePanel.jsx';
import StatusPagesPanel from './StatusPagesPanel.jsx';

// ==================== 主 UptimePage 组件 ====================
function UptimePage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const theme = useStore((state) => state.theme);
  const isDarkMode = theme === 'dark';
  const [uptimeCurrentTab, setUptimeCurrentTab] = useState('list'); // 'list' | 'add' | 'stats'
  const [uptimeMonitors, setUptimeMonitors] = useState([]);
  const [uptimeStatusPages, setUptimeStatusPages] = useState([]);
  const [uptimeMaintenanceWindows, setUptimeMaintenanceWindows] = useState([]);
  const [uptimeHeartbeats, setUptimeHeartbeats] = useState({});
  const [uptimeHeartbeatLoading, setUptimeHeartbeatLoading] = useState({});
  const [uptimeRateCache, setUptimeRateCache] = useState({});
  const [uptimeStats, setUptimeStats] = useState({ up: 0, down: 0, pending: 0, unknown: 0 });

  // UI 筛选与搜索
  const [uptimeStatusFilter, setUptimeStatusFilter] = useState(null); // null | 'up' | 'down' | 'pending'
  const [uptimeSearchText, setUptimeSearchText] = useState('');
  const [uptimeLoading, setUptimeLoading] = useState(false);
  const [uptimeSaving, setUptimeSaving] = useState(false);
  const [uptimeMetaLoading, setUptimeMetaLoading] = useState(false);
  const [uptimeImportPreview, setUptimeImportPreview] = useState(null);
  const [uptimeImportPayload, setUptimeImportPayload] = useState(null);
  const [selectedMonitorIds, setSelectedMonitorIds] = useState([]);
  const [monitorSelectionMode, setMonitorSelectionMode] = useState(false);
  const [expandedMonitorId, setExpandedMonitorId] = useState(null);
  const [statusPageForm, setStatusPageForm] = useState(() => createEmptyStatusPageForm());

  // 通知渠道配置
  const [notificationChannels, setNotificationChannels] = useState([]);

  const uptimeImportSections = useMemo(
    () => buildUptimeImportSections(uptimeImportPreview),
    [uptimeImportPreview]
  );

  const uptimeImportSummary = useMemo(() => {
    const totals = uptimeImportSections.reduce((acc, section) => ({
      total: acc.total + section.total,
      creates: acc.creates + section.creates,
      updates: acc.updates + section.updates,
    }), { total: 0, creates: 0, updates: 0 });

    return {
      ...totals,
      nonEmptySections: uptimeImportSections.filter((section) => section.total > 0),
    };
  }, [uptimeImportSections]);

  // 表单状态
  const [uptimeForm, setUptimeForm] = useState({
    id: null,
    name: '',
    type: 'http',
    url: '',
    hostname: '',
    port: 443,
    method: 'GET',
    interval: 60,
    timeout: 30,
    retries: 0,
    active: true,
    accepted_status_codes: '200-299',
    keyword: '',
    jsonQueryPath: '',
    jsonQueryOperator: 'equals',
    jsonExpectedValue: '',
    dns_resolve_type: 'A',
    dns_resolve_server: '',
    pushToken: '',
    pushGraceSeconds: 120,
    headers: '',
    body: '',
    ignoreTls: false,
    expiryNotification: 7,
    tagsInput: '',
    notificationChannels: []
  });

  const socketRef = useRef(null);
  const uptimeImportInputRef = useRef(null);

  // 获取请求 Header
  const getAuthHeaders = () => {
    return {
      'Content-Type': 'application/json',
    };
  };

  // ==================== 1. 数据载入 ====================
  const loadUptimeMonitors = async () => {
    setUptimeLoading(true);
    setSelectedMonitorIds([]);
    try {
      const headers = getAuthHeaders();
      const [monitorsRes, channelsRes] = await Promise.all([
        fetch('/api/uptime/monitors', { headers }),
        fetch('/api/notification/channels', { headers })
      ]);

      const monitorsData = await monitorsRes.json();
      const channelsData = await channelsRes.json();

      if (Array.isArray(monitorsData)) {
        setUptimeMonitors(monitorsData);
        // 初始化心跳容器并缓存末次状态
        const initialBeats = {};
        monitorsData.forEach(m => {
          if (m.lastHeartbeat) {
            initialBeats[m.id] = [normalizeUptimeBeat(m.lastHeartbeat)];
          } else {
            initialBeats[m.id] = [];
          }
        });
        setUptimeHeartbeats(initialBeats);

        // 延时加载具体历史记录与可用率
        monitorsData.forEach(m => {
          loadHeartbeats(m.id);
          loadUptimeRates(m.id);
        });
      }

      if (channelsData.success && Array.isArray(channelsData.data)) {
        setNotificationChannels(channelsData.data);
      }
    } catch (e) {
      console.error(e);
      toast.error('载入 Uptime 监测数据失败');
    } finally {
      setUptimeLoading(false);
    }
  };

  const loadHeartbeats = async (monitorId) => {
    setUptimeHeartbeatLoading(prev => ({ ...prev, [monitorId]: true }));
    try {
      const res = await fetch(`/api/uptime/monitors/${monitorId}/history`, { headers: getAuthHeaders() });
      const data = await res.json();
      if (Array.isArray(data)) {
        const normalized = data.map(normalizeUptimeBeat);
        setUptimeHeartbeats(prev => ({ ...prev, [monitorId]: normalized }));
      }
    } catch (e) {
      console.error(`加载心跳历史失败 (${monitorId}):`, e);
    } finally {
      setUptimeHeartbeatLoading(prev => ({ ...prev, [monitorId]: false }));
    }
  };

  const loadUptimeRates = async (monitorId) => {
    try {
      const headers = getAuthHeaders();
      const [res1, res30] = await Promise.all([
        fetch(`/api/uptime/monitors/${monitorId}/uptime?days=1`, { headers }),
        fetch(`/api/uptime/monitors/${monitorId}/uptime?days=30`, { headers }),
      ]);
      const d1 = await res1.json();
      const d30 = await res30.json();
      // 后端错误响应（无 uptime 字段）时显示 —，绝不退化为 100.000
      setUptimeRateCache(prev => ({
        ...prev,
        [monitorId]: {
          1: d1.error || d1.success === false ? '—' : (d1.uptime ?? '0.000'),
          30: d30.error || d30.success === false ? '—' : (d30.uptime ?? '0.000'),
        }
      }));
    } catch (e) {
      // 静默失败
    }
  };

  const loadUptimeStatusPages = async () => {
    setUptimeMetaLoading(true);
    try {
      const res = await fetch('/api/uptime/status-pages', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success && Array.isArray(data.data)) {
        setUptimeStatusPages(data.data);
      }
    } catch (e) {
      console.error(e);
      toast.error('载入状态页失败');
    } finally {
      setUptimeMetaLoading(false);
    }
  };

  const loadUptimeMaintenance = async () => {
    setUptimeMetaLoading(true);
    try {
      const res = await fetch('/api/uptime/maintenance', { headers: getAuthHeaders() });
      const data = await res.json();
      if (data.success && Array.isArray(data.data)) {
        setUptimeMaintenanceWindows(data.data);
      }
    } catch (e) {
      console.error(e);
      toast.error('载入维护窗口失败');
    } finally {
      setUptimeMetaLoading(false);
    }
  };

  const createDefaultStatusPage = async () => {
    if (uptimeMonitors.length === 0) {
      toast.warning('请先创建监测目标');
      return;
    }
    try {
      const res = await fetch('/api/uptime/status-pages', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          title: '主状态页',
          slug: 'main-status',
          monitorIds: uptimeMonitors.map(m => m.id),
        }),
      });
      const data = await res.json();
      if (!res.ok || !data.success) throw new Error(data.error || '创建失败');
      toast.success('状态页已创建');
      await loadUptimeStatusPages();
    } catch (e) {
      toast.error(e.message || '创建状态页失败');
    }
  };

  const getStatusPageBaseOrigin = () => {
    const configured = String(useStore.getState().publicApiUrl || '').trim().replace(/\/+$/g, '');
    return configured || window.location.origin;
  };

  const getStatusPagePublicUrl = (pageOrForm, mode = 'status') => {
    const slug = normalizeStatusSlug(pageOrForm?.slug || pageOrForm?.title || 'status');
    return `${getStatusPageBaseOrigin()}/${mode}/${encodeURIComponent(slug)}`;
  };

  const getStatusPageDomainUrl = (pageOrForm) => {
    const domain = normalizeStatusDomain(pageOrForm?.domain);
    return domain ? `https://${domain}` : '';
  };

  const copyStatusUrl = async (value, label = '公开地址') => {
    if (!value) {
      toast.warning('没有可复制的地址');
      return;
    }
    try {
      await navigator.clipboard.writeText(value);
      toast.success(`${label}已复制`);
    } catch (error) {
      toast.error('复制失败');
    }
  };

  const resetStatusPageForm = () => {
    setStatusPageForm(createEmptyStatusPageForm());
  };

  const editStatusPage = (page) => {
    const config = page.config || {};
    setStatusPageForm({
      id: page.id,
      title: page.title || '',
      slug: page.slug || '',
      domain: page.domain || '',
      description: page.description || '',
      public: page.public !== false,
      hideTargets: !!config.hideTargets,
      linkMonitorNames: !!config.linkMonitorNames,
      showOnDashboard: !!config.showOnDashboard,
      publicIconId: String(config.publicIconId || '').trim(),
      cacheSeconds: page.cacheSeconds || 300,
      monitorIds: Array.isArray(page.monitorIds) ? page.monitorIds : [],
    });
  };

  const saveStatusPage = async () => {
    const title = statusPageForm.title.trim();
    const slug = normalizeStatusSlug(statusPageForm.slug || title);
    if (!title) {
      toast.warning('请填写状态页名称');
      return;
    }
    if (statusPageForm.monitorIds.length === 0) {
      toast.warning('请至少绑定一个监测目标');
      return;
    }
    setUptimeMetaLoading(true);
    try {
      const payload = {
        title,
        slug,
        domain: normalizeStatusDomain(statusPageForm.domain),
        description: statusPageForm.description.trim(),
        public: !!statusPageForm.public,
        cacheSeconds: Math.max(30, Number(statusPageForm.cacheSeconds) || 300),
        config: {
          hideTargets: !!statusPageForm.hideTargets,
          linkMonitorNames: !!statusPageForm.linkMonitorNames,
          showOnDashboard: !!statusPageForm.showOnDashboard,
          ...(statusPageForm.publicIconId ? { publicIconId: statusPageForm.publicIconId } : {}),
        },
        monitorIds: statusPageForm.monitorIds,
      };
      const isEdit = !!statusPageForm.id;
      const response = await fetch(isEdit ? `/api/uptime/status-pages/${statusPageForm.id}` : '/api/uptime/status-pages', {
        method: isEdit ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(payload),
      });
      const result = await response.json();
      if (!response.ok || result.success === false) {
        throw new Error(result.error || '保存状态页失败');
      }
      toast.success(isEdit ? '状态页已更新' : '状态页已创建');
      resetStatusPageForm();
      await loadUptimeStatusPages();
    } catch (error) {
      toast.error(error.message || '保存状态页失败');
    } finally {
      setUptimeMetaLoading(false);
    }
  };

  const deleteStatusPage = async (page) => {
    if (!confirmPress(`status-page:${page.id}`, `删除状态页「${page.title || page.slug}」`)) return;
    setUptimeMetaLoading(true);
    try {
      const response = await fetch(`/api/uptime/status-pages/${page.id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const result = await response.json().catch(() => ({}));
      if (!response.ok || result.success === false) {
        throw new Error(result.error || '删除状态页失败');
      }
      toast.success('状态页已删除');
      if (statusPageForm.id === page.id) resetStatusPageForm();
      await loadUptimeStatusPages();
    } catch (error) {
      toast.error(error.message || '删除状态页失败');
    } finally {
      setUptimeMetaLoading(false);
    }
  };

  const toggleStatusPageMonitor = (monitorId, checked) => {
    setStatusPageForm(prev => {
      const ids = new Set(prev.monitorIds);
      if (checked) ids.add(monitorId);
      else ids.delete(monitorId);
      return { ...prev, monitorIds: Array.from(ids) };
    });
  };

  const createQuickMaintenance = async () => {
    if (selectedMonitorIds.length === 0) {
      toast.warning('请先在仪表盘选择监测目标');
      return;
    }
    try {
      const res = await fetch('/api/uptime/maintenance', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          title: '快速维护窗口',
          description: '由 Uptime 工作台创建',
          strategy: 'manual',
          startAt: new Date().toISOString(),
          endAt: new Date(Date.now() + 60 * 60 * 1000).toISOString(),
          targets: selectedMonitorIds,
        }),
      });
      const data = await res.json();
      if (!res.ok || !data.success) throw new Error(data.error || '创建失败');
      toast.success('维护窗口已创建');
      await loadUptimeMaintenance();
    } catch (e) {
      toast.error(e.message || '创建维护窗口失败');
    }
  };

  const exportUptimeConfig = async () => {
    setUptimeMetaLoading(true);
    try {
      const res = await fetch('/api/uptime/export', { headers: getAuthHeaders() });
      const data = await res.json();
      if (!res.ok || !data.success) throw new Error(data.error || '导出失败');

      const blob = new Blob([JSON.stringify(data.data, null, 2)], { type: 'application/json' });
      const blobUrl = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = blobUrl;
      link.download = `api-monitor-uptime-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(blobUrl);
      toast.success('Uptime 配置已导出');
    } catch (e) {
      console.error(e);
      toast.error(e.message || '导出 Uptime 配置失败');
    } finally {
      setUptimeMetaLoading(false);
    }
  };

  const previewUptimeImportFile = async (event) => {
    const file = event.target.files?.[0];
    if (!file) return;

    setUptimeMetaLoading(true);
    setUptimeImportPreview(null);
    setUptimeImportPayload(null);
    try {
      const text = await file.text();
      const parsed = JSON.parse(text);
      const payload = parsed?.data?.type === 'api-monitor-uptime-export' ? parsed.data : parsed;
      const res = await fetch('/api/uptime/import/preview', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ data: payload }),
      });
      const data = await res.json();
      if (!res.ok || !data.success) throw new Error(data.error || '导入预览失败');
      setUptimeImportPayload(payload);
      setUptimeImportPreview(data.data);
      toast.success('导入预览已生成');
    } catch (e) {
      console.error(e);
      toast.error(e.message || '导入预览失败');
    } finally {
      setUptimeMetaLoading(false);
      if (event.target) event.target.value = '';
    }
  };

  const commitUptimeImport = async () => {
    if (!uptimeImportPayload) {
      toast.warning('请先选择导入文件并完成预览');
      return;
    }
    if (!(await dialog.confirm('确定要导入 Uptime 配置吗？同名监测、状态页和维护窗口将被更新。'))) {
      return;
    }

    setUptimeMetaLoading(true);
    try {
      const res = await fetch('/api/uptime/import', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ data: uptimeImportPayload }),
      });
      const data = await res.json();
      if (!res.ok || !data.success) throw new Error(data.error || '导入失败');
      toast.success(`导入完成：监测 ${data.data?.monitorsChanged || 0}，状态页 ${data.data?.pagesChanged || 0}，维护窗口 ${data.data?.maintenanceChanged || 0}`);
      setUptimeImportPreview(null);
      setUptimeImportPayload(null);
      await Promise.all([loadUptimeMonitors(), loadUptimeStatusPages(), loadUptimeMaintenance()]);
    } catch (e) {
      console.error(e);
      toast.error(e.message || '导入失败');
    } finally {
      setUptimeMetaLoading(false);
    }
  };

  // ==================== 2. Socket 实时更新 ====================
  useEffect(() => {
    loadUptimeMonitors();

    // 建立 Socket 推送连接
    const socket = io('/', {
      transports: ['polling']
    });

    socket.on('connect', () => {
      console.log('✅ Uptime Socket Connected');
    });

    socket.on('uptime:heartbeat', ({ monitorId, beat }) => {
      const normalizedBeat = normalizeUptimeBeat(beat);

      setUptimeHeartbeats(prev => {
        const list = prev[monitorId] ? [...prev[monitorId]] : [];
        list.unshift(normalizedBeat);
        if (list.length > 60) {
          list.length = 60;
        }
        return { ...prev, [monitorId]: list };
      });
    });

    socketRef.current = socket;

    return () => {
      if (socketRef.current) {
        socketRef.current.disconnect();
      }
    };
  }, []);

  // ==================== 3. 统计状态运算 ====================
  useEffect(() => {
    const stats = { up: 0, down: 0, pending: 0, unknown: 0 };
    uptimeMonitors.forEach(m => {
      if (!m.active) {
        stats.unknown++;
        return;
      }
      const beats = uptimeHeartbeats[m.id] || [];
      const lastBeat = beats[0];

      if (!lastBeat) {
        stats.unknown++;
      } else if (lastBeat.status === 'up') {
        stats.up++;
      } else if (lastBeat.status === 'down') {
        stats.down++;
      } else if (lastBeat.status === 'pending') {
        stats.pending++;
      } else {
        stats.unknown++;
      }
    });
    setUptimeStats(stats);
  }, [uptimeMonitors, uptimeHeartbeats]);

  // ==================== 4. 筛选与数据处理 ====================
  const filteredMonitors = useMemo(() => {
    let result = [...uptimeMonitors];

    // 按可用状态过滤
    if (uptimeStatusFilter) {
      result = result.filter(m => {
        if (!m.active) return uptimeStatusFilter === 'pending'; // 暂停算等待/未知
        const beats = uptimeHeartbeats[m.id] || [];
        const lastBeat = beats[0];
        return lastBeat?.status === uptimeStatusFilter;
      });
    }

    // 按搜索关键字过滤
    if (uptimeSearchText.trim()) {
      const q = uptimeSearchText.toLowerCase();
      result = result.filter(
        m =>
          m.name.toLowerCase().includes(q) ||
          (m.url && m.url.toLowerCase().includes(q)) ||
          (m.hostname && m.hostname.toLowerCase().includes(q)) ||
          (m.tags && m.tags.some(t => t.toLowerCase().includes(q)))
      );
    }

    return result;
  }, [uptimeMonitors, uptimeStatusFilter, uptimeSearchText, uptimeHeartbeats]);

  // 获取可用率辅助函数
  const getUptimeRate = (monitorId, days = 1) => {
    const cache = uptimeRateCache[monitorId];
    if (cache && cache[days]) return cache[days];
    return '100.000';
  };

  const getUptimeRateClass = (rateStr) => {
    const rate = parseFloat(rateStr);
    if (rate >= 99) return 'text-kumo-success';
    if (rate >= 95) return 'text-kumo-warning';
    return 'text-kumo-danger';
  };

  const formatUptimeRateCompact = (rateStr) => {
    const rate = Number(rateStr);
    if (!Number.isFinite(rate)) return '--';
    if (rate >= 100) return '100';
    // 保留两位小数并去掉末尾多余的 0，避免 99.84% 被四舍五入成 100%
    return String(Math.round(rate * 100) / 100);
  };

  // 格式化连接地址
  const getDisplayUrl = (monitor) => {
    if (monitor.type === 'http' || monitor.type === 'keyword' || monitor.type === 'json') {
      return monitor.url;
    }
    if (monitor.type === 'tcp') {
      return `${monitor.hostname}:${monitor.port}`;
    }
    if (monitor.type === 'push') {
      return monitor.pushToken ? `/api/uptime/push/${monitor.pushToken}` : 'Push token 待生成';
    }
    return monitor.hostname;
  };

  const getUptimeTypeIcon = (type) => {
    switch (type) {
      case 'http':
      case 'keyword':
      case 'json':
        return <Globe className="w-3.5 h-3.5" />;
      case 'tcp':
        return <Terminal className="w-3.5 h-3.5" />;
      default:
        return <Activity className="w-3.5 h-3.5" />;
    }
  };

  const formatDateTime = (timeStr) => {
    if (!timeStr) return '--';
    return new Date(timeStr).toLocaleString('zh-CN', {
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit'
    });
  };

  // ==================== 5. 单项控制与 CRUD ====================
  const handleToggleActive = async (monitor) => {
    try {
      const res = await fetch(`/api/uptime/monitors/${monitor.id}/toggle`, {
        method: 'POST',
        headers: getAuthHeaders()
      });
      const data = await res.json();
      if (res.ok) {
        setUptimeMonitors(prev =>
          prev.map(m => (m.id === monitor.id ? { ...m, active: data.active } : m))
        );
        toast.success(data.active ? '监测目标已恢复' : '监测目标已暂停');
      } else {
        toast.error('切换状态失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('操作异常');
    }
  };

  const handleDeleteMonitor = async (id) => {
    const monitor = uptimeMonitors.find(item => item.id === id);
    if (!confirmPress(`monitor:${id}`, `删除监测目标「${monitor?.name || '#' + id}」`)) return;
    try {
      const res = await fetch(`/api/uptime/monitors/${id}`, {
        method: 'DELETE',
        headers: getAuthHeaders()
      });
      if (res.ok) {
        setUptimeMonitors(prev => prev.filter(m => m.id !== id));
        setSelectedMonitorIds(prev => prev.filter(x => x !== id));
        if (expandedMonitorId === id) setExpandedMonitorId(null);
        toast.success('监测目标已删除');
      } else {
        toast.error('删除目标失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('删除目标失败');
    }
  };

  // ==================== 6. 批量操作 ====================
  const isAllSelected = useMemo(() => {
    if (filteredMonitors.length === 0) return false;
    return filteredMonitors.every(m => selectedMonitorIds.includes(m.id));
  }, [filteredMonitors, selectedMonitorIds]);
  const showMonitorSelectionControls = monitorSelectionMode || selectedMonitorIds.length > 0;

  const handleToggleSelectAll = () => {
    if (isAllSelected) {
      const filteredIds = filteredMonitors.map(m => m.id);
      setSelectedMonitorIds(prev => prev.filter(id => !filteredIds.includes(id)));
    } else {
      const newIds = [...selectedMonitorIds];
      filteredMonitors.forEach(m => {
        if (!newIds.includes(m.id)) {
          newIds.push(m.id);
        }
      });
      setSelectedMonitorIds(newIds);
    }
  };

  const handleToggleMonitorSelect = (id, checked) => {
    setSelectedMonitorIds(prev =>
      checked ? [...prev, id] : prev.filter(x => x !== id)
    );
  };

  const handleBatchDelete = async () => {
    if (selectedMonitorIds.length === 0) return;
    if (!confirmPress('batch-delete-monitors', `批量删除选中的 ${selectedMonitorIds.length} 个监测目标`)) return;

    try {
      const res = await fetch('/api/uptime/monitors/batch-delete', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ ids: selectedMonitorIds })
      });
      const data = await res.json();
      if (res.ok) {
        setUptimeMonitors(prev => prev.filter(m => !selectedMonitorIds.includes(m.id)));
        setSelectedMonitorIds([]);
        setExpandedMonitorId(null);
        toast.success(`成功删除 ${data.count || selectedMonitorIds.length} 个监测目标`);
      } else {
        toast.error(data.error || '批量删除失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('批量删除失败');
    }
  };

  // ==================== 7. 表单操作 ====================
  const handleOpenAdd = () => {
    const defaultChannels = notificationChannels
      .filter(c => c.enabled === true || c.enabled === 1)
      .map(c => c.id);

    setUptimeForm({
      id: null,
      name: '',
      type: 'http',
      url: '',
      hostname: '',
      port: 443,
      method: 'GET',
      interval: 60,
      timeout: 30,
      retries: 0,
      active: true,
      accepted_status_codes: '200-299',
      keyword: '',
      jsonQueryPath: '',
      jsonQueryOperator: 'equals',
      jsonExpectedValue: '',
      dns_resolve_type: 'A',
      dns_resolve_server: '',
      pushToken: '',
      pushGraceSeconds: 120,
      headers: '',
      body: '',
      ignoreTls: false,
      expiryNotification: 7,
      tagsInput: '',
      notificationChannels: defaultChannels
    });
    setUptimeCurrentTab('add');
  };

  const handleOpenEdit = (monitor) => {
    setUptimeForm({
      id: monitor.id,
      name: monitor.name || '',
      type: monitor.type || 'http',
      url: monitor.url || '',
      hostname: monitor.hostname || '',
      port: monitor.port || 443,
      method: monitor.method || 'GET',
      interval: monitor.interval || 60,
      timeout: monitor.timeout || 30,
      retries: monitor.retries || 0,
      active: !!monitor.active,
      accepted_status_codes: monitor.accepted_status_codes || '200-299',
      keyword: monitor.keyword || '',
      jsonQueryPath: monitor.jsonQueryPath || monitor.config?.jsonQueryPath || '',
      jsonQueryOperator: monitor.jsonQueryOperator || monitor.config?.jsonQueryOperator || 'equals',
      jsonExpectedValue: monitor.jsonExpectedValue || monitor.config?.jsonExpectedValue || '',
      dns_resolve_type: monitor.dns_resolve_type || 'A',
      dns_resolve_server: monitor.dns_resolve_server || '',
      pushToken: monitor.pushToken || '',
      pushGraceSeconds: monitor.pushGraceSeconds || monitor.config?.graceSeconds || 120,
      headers: monitor.headers || '',
      body: monitor.body || '',
      ignoreTls: !!monitor.ignoreTls,
      expiryNotification: monitor.expiryNotification || 7,
      tagsInput: Array.isArray(monitor.tags) ? monitor.tags.join(',') : '',
      notificationChannels: monitor.notificationChannels || []
    });
    setUptimeCurrentTab('add');
  };

  const handleSaveMonitor = async () => {
    if (!uptimeForm.name.trim()) {
      toast.warning('请输入显示名称');
      return;
    }
    if (['http', 'keyword', 'json'].includes(uptimeForm.type) && !uptimeForm.url.trim()) {
      toast.warning('请输入 URL');
      return;
    }
    if (['tcp', 'ping', 'dns'].includes(uptimeForm.type) && !uptimeForm.hostname.trim()) {
      toast.warning('请输入 Hostname');
      return;
    }
    if (uptimeForm.type === 'json' && !uptimeForm.jsonQueryPath.trim()) {
      toast.warning('请输入 JSON 查询路径');
      return;
    }

    // 处理标签数组
    const tags = uptimeForm.tagsInput
      ? uptimeForm.tagsInput.split(/[,，]/).map(t => t.trim()).filter(Boolean)
      : [];

    setUptimeSaving(true);
    try {
      const isEdit = !!uptimeForm.id;
      const url = isEdit ? `/api/uptime/monitors/${uptimeForm.id}` : '/api/uptime/monitors';
      const method = isEdit ? 'PUT' : 'POST';

      const payload = {
        ...uptimeForm,
        tags,
        notificationChannels: uptimeForm.notificationChannels,
        config: {
          jsonQueryPath: uptimeForm.jsonQueryPath,
          jsonQueryOperator: uptimeForm.jsonQueryOperator,
          jsonExpectedValue: uptimeForm.jsonExpectedValue,
          graceSeconds: uptimeForm.pushGraceSeconds,
        },
      };

      const res = await fetch(url, {
        method,
        headers: getAuthHeaders(),
        body: JSON.stringify(payload)
      });
      const result = await res.json();

      if (res.ok) {
        toast.success(isEdit ? '监测目标已更新' : '监测目标已创建');
        setUptimeCurrentTab('list');
        await loadUptimeMonitors();
      } else {
        toast.error(result.error || '保存失败');
      }
    } catch (e) {
      console.error(e);
      toast.error('保存请求异常');
    } finally {
      setUptimeSaving(false);
    }
  };

  return (
    <div className="flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4">
      {/* ==================== 顶部 Tab 导航 ==================== */}
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={uptimeCurrentTab}
          onValueChange={(value) => {
            if (value === 'add') {
              handleOpenAdd();
              return;
            }
            setUptimeCurrentTab(value);
            if (value === 'status-pages') loadUptimeStatusPages();
            if (value === 'maintenance') loadUptimeMaintenance();
          }}
          tabs={uptimeTabs}
        />

        {uptimeCurrentTab === 'list' && (
          <div className="flex min-w-0 items-center gap-2">
            <ResponsiveSearchInput
              value={uptimeSearchText}
              onChange={(e) => setUptimeSearchText(e.target.value)}
              placeholder="搜索监测目标..."
              ariaLabel="搜索监测目标"
              className="cq-md:w-56"
            />

            <TabBarOverflowActions
              items={[
                {
                  key: 'add-target',
                  label: '新建目标',
                  icon: <Plus className="w-4 h-4" />,
                  onClick: handleOpenAdd,
                  variant: 'primary',
                },
              ]}
            />
          </div>
        )}
      </div>

      {/* ==================== 1. 监测目标仪表盘 (Dashboard) ==================== */}
      {uptimeCurrentTab === 'list' && (
        <div className="space-y-4">
          {/* 可用状态概览胶囊栏 */}
          <div className="flex flex-wrap items-center gap-2 pb-2">
            <Button
              onClick={() => setUptimeStatusFilter(null)}
              variant={uptimeStatusFilter === null ? 'primary' : 'secondary'} size="sm"
            >
              全部 ({uptimeMonitors.length})
            </Button>
            <Button
              onClick={() => setUptimeStatusFilter('up')}
              variant="secondary" size="sm"
              className={uptimeStatusFilter === 'up' ? 'text-kumo-success ring-kumo-success/30' : ''}
            >
              正常 ({uptimeStats.up})
            </Button>
            <Button
              onClick={() => setUptimeStatusFilter('down')}
              variant="secondary" size="sm"
              className={uptimeStatusFilter === 'down' ? 'text-kumo-danger ring-kumo-danger/30' : ''}
            >
              故障 ({uptimeStats.down})
            </Button>
            <Button
              onClick={() => setUptimeStatusFilter('pending')}
              variant="secondary" size="sm"
              className={uptimeStatusFilter === 'pending' ? 'text-kumo-warning ring-kumo-warning/30' : ''}
            >
              等待 ({uptimeStats.pending})
            </Button>

            <Button
              onClick={() => {
                if (showMonitorSelectionControls) {
                  setSelectedMonitorIds([]);
                  setMonitorSelectionMode(false);
                } else {
                  setMonitorSelectionMode(true);
                }
              }}
              variant={showMonitorSelectionControls ? 'primary' : 'secondary'} size="sm"
            >
              {showMonitorSelectionControls ? '取消选择' : '选择'}
            </Button>

            <Button
              onClick={loadUptimeMonitors}
              loading={uptimeLoading}
              variant="secondary" size="sm"
              shape="square"
              aria-label="刷新监测目标"
              className="ml-auto"
              title="刷新"
            >
              {!uptimeLoading && <RotateCw className="w-3.5 h-3.5" />}
            </Button>
          </div>

          {uptimeLoading && uptimeMonitors.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle">
              <Loader size={32} className="text-brand mb-4" />
              <span>载入监控目标中...</span>
            </div>
          ) : filteredMonitors.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-20 text-kumo-subtle app-empty-panel">
              <Activity className="w-12 h-12 opacity-30 mb-4" />
              <div className="text-sm">
                {uptimeSearchText ? '未找到匹配的监测目标' : '暂无监测目标，开始添加一个吧'}
              </div>
              {!uptimeSearchText && (
                <Button size="sm" variant="primary" className="mt-4" onClick={handleOpenAdd}>
                  添加第一个监测
                </Button>
              )}
            </div>
          ) : (
            <div className="space-y-3">
              {/* 批量控制条 */}
              {showMonitorSelectionControls && (
                <AppCard padding="none" className="flex items-center justify-between bg-kumo-recessed/30 px-4 py-2.5">
                  <Checkbox
                    checked={isAllSelected}
                    onCheckedChange={handleToggleSelectAll}
                    label={`全选 (已选 ${selectedMonitorIds.length} 个)`}
                  />
                  {selectedMonitorIds.length > 0 && (
                    <Button
                      variant={isArmed('batch-delete-monitors') ? 'destructive' : 'secondary-destructive'} size="sm"
                      onClick={handleBatchDelete}
                      icon={<Trash className="w-3 h-3" />}
                    >
                      批量删除
                    </Button>
                  )}
                </AppCard>
              )}

              {/* 监测卡片列表 */}
              <div className="flex flex-col gap-3">
                {filteredMonitors.map((monitor) => (
                  <MonitorCard
                    key={monitor.id}
                    monitor={monitor}
                    beats={uptimeHeartbeats[monitor.id] || []}
                    isExpanded={expandedMonitorId === monitor.id}
                    showMonitorSelectionControls={showMonitorSelectionControls}
                    selectedMonitorIds={selectedMonitorIds}
                    onToggleSelect={handleToggleMonitorSelect}
                    onToggleExpand={setExpandedMonitorId}
                    getUptimeTypeIcon={getUptimeTypeIcon}
                    getDisplayUrl={getDisplayUrl}
                    getUptimeRate={getUptimeRate}
                    getUptimeRateClass={getUptimeRateClass}
                    formatUptimeRateCompact={formatUptimeRateCompact}
                    uptimeHeartbeatLoading={uptimeHeartbeatLoading}
                    isDarkMode={isDarkMode}
                    onPauseResume={handleToggleActive}
                    onEdit={handleOpenEdit}
                    onDelete={handleDeleteMonitor}
                  />
                ))}
              </div>
            </div>
          )}
        </div>
      )}

      {uptimeCurrentTab === 'status-pages' && (
        <StatusPagesPanel
          statusPageForm={statusPageForm}
          setStatusPageForm={setStatusPageForm}
          uptimeMonitors={uptimeMonitors}
          uptimeStatusPages={uptimeStatusPages}
          uptimeMetaLoading={uptimeMetaLoading}
          isArmed={isArmed}
          getDisplayUrl={getDisplayUrl}
          getStatusPagePublicUrl={getStatusPagePublicUrl}
          getStatusPageDomainUrl={getStatusPageDomainUrl}
          copyStatusUrl={copyStatusUrl}
          editStatusPage={editStatusPage}
          deleteStatusPage={deleteStatusPage}
          saveStatusPage={saveStatusPage}
          resetStatusPageForm={resetStatusPageForm}
          toggleStatusPageMonitor={toggleStatusPageMonitor}
          createDefaultStatusPage={createDefaultStatusPage}
          loadUptimeStatusPages={loadUptimeStatusPages}
        />
      )}

      {uptimeCurrentTab === 'maintenance' && (
        <MaintenancePanel
          maintenanceWindows={uptimeMaintenanceWindows}
          selectedMonitorIds={selectedMonitorIds}
          metaLoading={uptimeMetaLoading}
          formatDateTime={formatDateTime}
          onRefresh={loadUptimeMaintenance}
          onCreateQuick={createQuickMaintenance}
        />
      )}

      {/* ==================== 2. 添加/修改监测目标 ==================== */}
      {uptimeCurrentTab === 'add' && (
        <SectionCard
          title={uptimeForm.id ? '编辑监测目标' : '新建监测目标'}
          icon={<Activity className="h-4 w-4 text-brand" />}
          bodyPadding="xl"
          bodyClassName="space-y-6"
        >

          <div className="grid grid-cols-1 cq-md:grid-cols-12 gap-5">
            {/* 监控类型选择 (Full Width) */}
            <div className="cq-md:col-span-12 space-y-1.5">
              <label className="text-xs font-semibold text-kumo-subtle">监测类型</label>
              <Tabs
                {...TOOL_TABS_PROPS}
                value={uptimeForm.type}
                onValueChange={(value) => setUptimeForm(prev => ({ ...prev, type: value }))}
                tabs={[
                  { value: 'http', label: 'HTTP(s)' },
                  { value: 'keyword', label: '网页关键词' },
                  { value: 'json', label: 'JSON 查询' },
                  { value: 'tcp', label: 'TCP 端口' },
                  { value: 'ping', label: 'Ping（TCP 80/443/53）' },
                  { value: 'dns', label: 'DNS 解析' },
                  { value: 'push', label: 'Push' },
                ]}
              />
            </div>

            {/* 目标显示名称 */}
            <div className="cq-md:col-span-4">
              <Input
                label="名称 *"
                type="text" size="sm"
                placeholder="如：生产数据库端口"
                value={uptimeForm.name}
                onChange={(e) => setUptimeForm(prev => ({ ...prev, name: e.target.value }))}
                className="w-full"
              />
            </div>

            {/* 地址输入域 */}
            {['http', 'keyword', 'json'].includes(uptimeForm.type) ? (
              <div className="cq-md:col-span-8">
                <Input
                  label="请求 URL *"
                  type="text" size="sm"
                  placeholder="https://api.domain.com/v1/health"
                  value={uptimeForm.url}
                  onChange={(e) => setUptimeForm(prev => ({ ...prev, url: e.target.value }))}
                  className="w-full"
                />
              </div>
            ) : uptimeForm.type === 'push' ? (
              <div className="cq-md:col-span-8">
                <AppCard padding="none" className="bg-kumo-recessed/40 p-3">
                  <div className="text-[10px] font-semibold text-kumo-subtle">Push URL</div>
                  <div className="mt-1 truncate font-mono text-xs text-kumo-strong">
                    {uptimeForm.pushToken ? `/api/uptime/push/${uptimeForm.pushToken}` : '保存后自动生成 token URL'}
                  </div>
                </AppCard>
              </div>
            ) : (
              <>
                <div className={['tcp', 'dns'].includes(uptimeForm.type) ? 'cq-md:col-span-6' : 'cq-md:col-span-8'}>
                  <Input
                    label="主机名 / IP *"
                    type="text" size="sm"
                    placeholder="如：192.168.1.100 或 db.server.internal"
                    value={uptimeForm.hostname}
                    onChange={(e) => setUptimeForm(prev => ({ ...prev, hostname: e.target.value }))}
                    className="w-full"
                  />
                </div>
                {uptimeForm.type === 'tcp' && (
                  <div className="cq-md:col-span-2">
                    <Input
                      label="连接端口 *"
                      type="number" size="sm"
                      placeholder="3306"
                      value={uptimeForm.port}
                      onChange={(e) => setUptimeForm(prev => ({ ...prev, port: parseInt(e.target.value, 10) || 0 }))}
                      className="w-full font-mono"
                    />
                  </div>
                )}
                {uptimeForm.type === 'dns' && (
                  <>
                    <div className="cq-md:col-span-2">
                      <Select alignItemWithTrigger
                        size="sm"
                        label="解析类型"
                        value={uptimeForm.dns_resolve_type}
                        onValueChange={(value) => setUptimeForm(prev => ({ ...prev, dns_resolve_type: String(value) }))}
                        items={[
                          { value: 'A', label: 'A（IPv4）' },
                          { value: 'AAAA', label: 'AAAA（IPv6）' },
                          { value: 'MX', label: 'MX（邮件）' },
                          { value: 'TXT', label: 'TXT' },
                          { value: 'NS', label: 'NS（名称服务器）' },
                          { value: 'CNAME', label: 'CNAME（别名）' },
                        ]}
                        className="w-full"
                      />
                    </div>
                    <div className="cq-md:col-span-4">
                      <Input
                        label="DNS 服务器（可选）"
                        type="text" size="sm"
                        placeholder="如：8.8.8.8 或 127.0.0.1:5353"
                        value={uptimeForm.dns_resolve_server}
                        onChange={(e) => setUptimeForm(prev => ({ ...prev, dns_resolve_server: e.target.value }))}
                        className="w-full font-mono"
                      />
                    </div>
                  </>
                )}
              </>
            )}

            {/* 监测频率与重试参数 */}
            <div className="cq-md:col-span-6">
              <Input
                label="检测频率（秒）"
                type="number" size="sm"
                min="20"
                value={uptimeForm.interval}
                onChange={(e) => setUptimeForm(prev => ({ ...prev, interval: parseInt(e.target.value, 10) || 60 }))}
                className="w-full font-mono"
              />
            </div>
            <div className="cq-md:col-span-6">
              <Input
                label="重试次数"
                type="number" size="sm"
                min="0"
                value={uptimeForm.retries}
                onChange={(e) => setUptimeForm(prev => ({ ...prev, retries: parseInt(e.target.value, 10) || 0 }))}
                className="w-full font-mono"
              />
            </div>

            {/* 高级设置小节 */}
            <div className="cq-md:col-span-12 border-t border-kumo-line pt-4 mt-2">
              <h4 className="text-xs font-semibold text-kumo-strong flex items-center gap-1.5 select-none">
                <Shield className="w-3.5 h-3.5" />
                安全与高级设置
              </h4>
            </div>

            {/* 证书过期设置 */}
            {['http', 'json'].includes(uptimeForm.type) && (
              <div className="cq-md:col-span-6">
                <Input
                label="SSL 到期提醒（天）"
                  type="number" size="sm"
                  placeholder="7"
                  value={uptimeForm.expiryNotification}
                  onChange={(e) => setUptimeForm(prev => ({ ...prev, expiryNotification: parseInt(e.target.value, 10) || 7 }))}
                  className="w-full font-mono"
                />
              </div>
            )}

            {/* 忽略 TLS 选项 */}
            {['http', 'keyword', 'json'].includes(uptimeForm.type) && (
              <div className="cq-md:col-span-6 flex items-end pb-2">
                <Checkbox
                  checked={uptimeForm.ignoreTls}
                  onCheckedChange={(checked) => setUptimeForm(prev => ({ ...prev, ignoreTls: checked }))}
                  label="忽略不可信 / 自签 TLS"
                />
              </div>
            )}

            {/* 网页关键字匹配 */}
            {uptimeForm.type === 'keyword' && (
              <div className="cq-md:col-span-12">
                <Input
                  label="关键字匹配 *"
                  type="text" size="sm"
                  placeholder="如：success 或 正常"
                  value={uptimeForm.keyword}
                  onChange={(e) => setUptimeForm(prev => ({ ...prev, keyword: e.target.value }))}
                  className="w-full"
                />
              </div>
            )}

            {uptimeForm.type === 'dns' && (
              <div className="cq-md:col-span-6">
                <Input
                  label="期望值（可选）"
                  type="text" size="sm"
                  placeholder="如：93.184.216 或 google.com. 10（MX）"
                  value={uptimeForm.keyword}
                  onChange={(e) => setUptimeForm(prev => ({ ...prev, keyword: e.target.value }))}
                  className="w-full"
                />
              </div>
            )}

            {uptimeForm.type === 'json' && (
              <>
                <div className="cq-md:col-span-4">
                  <Input
                    label="JSON 路径 *"
                    type="text" size="sm"
                    placeholder="如：$.data.status"
                    value={uptimeForm.jsonQueryPath}
                    onChange={(e) => setUptimeForm(prev => ({ ...prev, jsonQueryPath: e.target.value }))}
                    className="w-full font-mono"
                  />
                </div>
                <div className="cq-md:col-span-4">
                  <Input
                    label="操作符"
                    type="text" size="sm"
                    placeholder="如：equals / regex"
                    value={uptimeForm.jsonQueryOperator}
                    onChange={(e) => setUptimeForm(prev => ({ ...prev, jsonQueryOperator: e.target.value }))}
                    className="w-full font-mono"
                  />
                </div>
                <div className="cq-md:col-span-4">
                  <Input
                    label="期望值"
                    type="text" size="sm"
                    placeholder="如：ok"
                    value={uptimeForm.jsonExpectedValue}
                    onChange={(e) => setUptimeForm(prev => ({ ...prev, jsonExpectedValue: e.target.value }))}
                    className="w-full font-mono"
                  />
                </div>
              </>
            )}

            {uptimeForm.type === 'push' && (
              <div className="cq-md:col-span-6">
                <Input
                  label="Push 宽限（秒）"
                  type="number" size="sm"
                  min="30"
                  value={uptimeForm.pushGraceSeconds}
                  onChange={(e) => setUptimeForm(prev => ({ ...prev, pushGraceSeconds: parseInt(e.target.value, 10) || 120 }))}
                  className="w-full font-mono"
                />
              </div>
            )}

            {/* 告警通知渠道设置 */}
            <div className="cq-md:col-span-12 border-t border-kumo-line pt-4 mt-2">
              <h4 className="text-xs font-semibold text-kumo-strong flex items-center gap-1.5 select-none">
                <Bell className="w-3.5 h-3.5" />
                故障通知分发渠道
              </h4>
            </div>

            <div className="cq-md:col-span-12 space-y-2">
              <AppCard padding="none" className="flex flex-wrap gap-4 bg-kumo-recessed/50 p-3.5">
                {notificationChannels.filter(c => c.enabled).map((channel) => (
                  <Checkbox
                    key={channel.id}
                    checked={uptimeForm.notificationChannels.includes(channel.id)}
                    onCheckedChange={(checked) => {
                      const id = channel.id;
                      setUptimeForm(prev => ({
                        ...prev,
                        notificationChannels: checked
                          ? [...prev.notificationChannels, id]
                          : prev.notificationChannels.filter(x => x !== id)
                      }));
                    }}
                    label={`${channel.name} (${channel.type === 'email' ? '邮箱' : 'TG'})`}
                  />
                ))}

                {notificationChannels.filter(c => c.enabled).length === 0 && (
                  <div className="text-xs text-kumo-subtle flex items-center gap-1.5 select-none w-full">
                    <Info className="w-4 h-4 text-kumo-subtle/60" />
                    <span>暂无启用的告警通道。</span>
                  </div>
                )}
              </AppCard>
            </div>

            {/* 标签管理 */}
            <div className="cq-md:col-span-12">
              <Input
                label="分组标签"
                type="text" size="sm"
                placeholder="prod, api, test（逗号或空格分隔）"
                value={uptimeForm.tagsInput}
                onChange={(e) => setUptimeForm(prev => ({ ...prev, tagsInput: e.target.value }))}
                className="w-full"
              />
            </div>
          </div>

          {/* 表单按钮栏 */}
          <div className="flex justify-end gap-3 border-t border-kumo-line pt-4 select-none">
            <Button size="sm" onClick={() => setUptimeCurrentTab('list')}>取消</Button>
            <Button size="sm" variant="primary" onClick={handleSaveMonitor} loading={uptimeSaving} icon={<Save className="w-3.5 h-3.5" />}>
              保存目标
            </Button>
          </div>
        </SectionCard>
      )}

      {/* ==================== 3. 配置迁移 Tab ==================== */}
      {uptimeCurrentTab === 'stats' && (
        <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(0,1fr)_24rem]">
          <SectionCard
            title="配置导入预览"
            icon={<Upload className="h-4 w-4 text-brand" />}
            actions={(
              <>
                <Input
                  ref={uptimeImportInputRef}
                  type="file"
                  accept=".json,application/json"
                  aria-label="选择 Uptime 配置文件"
                  className="hidden"
                  onChange={previewUptimeImportFile}
                />
                <Toolbar size="sm" aria-label="导出导入 Uptime 配置" className="shrink-0">
                    <Toolbar.Button onClick={exportUptimeConfig} loading={uptimeMetaLoading} aria-label="导出当前配置" icon={<Upload className="h-3.5 w-3.5" />}>
                      <span className="hidden cq-sm:inline">导出</span>
                    </Toolbar.Button>
                    <Toolbar.Button onClick={() => uptimeImportInputRef.current?.click()} loading={uptimeMetaLoading} aria-label="导入配置文件" icon={<Download className="h-3.5 w-3.5" />}>
                      <span className="hidden cq-sm:inline">导入</span>
                    </Toolbar.Button>
                  </Toolbar>
              </>
            )}
            bodyPadding="lg"
            bodyClassName="space-y-4"
          >

            {!uptimeImportPreview ? (
              <EmptyState
                card={false}
                icon={Upload}
                title="尚未选择配置文件"
                description="导入 JSON 并预览"
                className="min-h-[20rem] py-16"
              />
            ) : (
              <div className="space-y-4">
                <AppCard padding="none" className="bg-kumo-recessed/40 px-4 py-3.5">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="min-w-0 space-y-1">
                      <div className="text-sm font-semibold text-kumo-strong">
                        本次将同步 {uptimeImportSummary.total} 个配置对象
                      </div>
                      <div className="text-xs leading-relaxed text-kumo-subtle">
                        系统会按现有监测、状态页和维护窗口判断创建或更新，确认前可先检查影响范围。
                      </div>
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      <StatusBadge tone="success">创建 {uptimeImportSummary.creates}</StatusBadge>
                      <StatusBadge tone="warning">更新 {uptimeImportSummary.updates}</StatusBadge>
                    </div>
                  </div>
                  <div className="mt-3 flex flex-wrap gap-x-5 gap-y-2 text-xs">
                    {uptimeImportSections.map((section) => (
                      <div key={section.key} className="flex items-center gap-2 text-kumo-subtle">
                        <span className="font-semibold text-kumo-strong">{section.title}</span>
                        <span>{section.total} 项</span>
                        {section.updates > 0 && <span>更新 {section.updates}</span>}
                      </div>
                    ))}
                  </div>
                </AppCard>

                <div className="grid gap-3 cq-xl:grid-cols-3">
                  {uptimeImportSections.map((section) => (
                    <AppCard key={section.key} padding="none" className="overflow-hidden bg-kumo-recessed/40">
                      <div className="flex flex-wrap items-start justify-between gap-3 border-b border-kumo-line/70 bg-kumo-recessed/20 px-4 py-3">
                        <div className="min-w-0">
                          <div className="text-sm font-semibold text-kumo-strong">{section.title}</div>
                          <div className="mt-1 text-xs leading-relaxed text-kumo-subtle">{section.description}</div>
                        </div>
                        <div className="flex flex-wrap items-center gap-1.5">
                          <StatusBadge tone="neutral">{section.total} 项</StatusBadge>
                          {section.creates > 0 && <StatusBadge tone="success">创建 {section.creates}</StatusBadge>}
                          {section.updates > 0 && <StatusBadge tone="warning">更新 {section.updates}</StatusBadge>}
                        </div>
                      </div>

                      {section.total === 0 ? (
                        <div className="px-4 py-6 text-xs text-kumo-subtle">{section.emptyLabel}</div>
                      ) : (
                        <div className="max-h-80 overflow-y-auto">
                          {section.items.map((item) => {
                            const actionMeta = getUptimeImportActionMeta(item.action);
                            return (
                              <div
                                key={item.id}
                                className="flex items-start justify-between gap-3 border-b border-kumo-line/60 px-4 py-3 last:border-b-0"
                              >
                                <div className="min-w-0">
                                  <div className="truncate text-sm font-semibold text-kumo-strong">{item.label}</div>
                                  <div className="mt-1 truncate text-xs text-kumo-subtle">{item.detail}</div>
                                </div>
                                <StatusBadge tone={actionMeta.tone}>{actionMeta.label}</StatusBadge>
                              </div>
                            );
                          })}
                        </div>
                      )}
                    </AppCard>
                  ))}
                </div>
              </div>
            )}
          </SectionCard>

          <SectionCard
            title="导入执行"
            icon={<Download className="h-4 w-4 text-brand" />}
            className="self-start"
            bodyPadding="lg"
            bodyClassName="space-y-3.5"
          >
            <AppCard padding="none" className="bg-kumo-recessed/40 px-4 py-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="text-sm font-semibold text-kumo-strong">执行状态</div>
                {uptimeImportPreview ? (
                  <StatusBadge tone="info">预览就绪</StatusBadge>
                ) : (
                  <StatusBadge tone="neutral">等待文件</StatusBadge>
                )}
              </div>
              <div className="mt-2 text-xs leading-relaxed text-kumo-subtle">
                {uptimeImportPreview
                  ? `已预览 ${uptimeImportSummary.total} 个对象，确认后会按匹配规则创建或更新。`
                  : '先选择配置文件生成预览，再决定是否导入当前 Uptime。'}
              </div>
              {uptimeImportPreview && (
                <div className="mt-3 flex flex-wrap gap-1.5">
                  <StatusBadge tone="success">创建 {uptimeImportSummary.creates}</StatusBadge>
                  <StatusBadge tone="warning">更新 {uptimeImportSummary.updates}</StatusBadge>
                  <StatusBadge tone="neutral">{uptimeImportSummary.nonEmptySections.length} 类对象</StatusBadge>
                </div>
              )}
            </AppCard>

            <div className="space-y-2 rounded-lg border border-kumo-line/70 bg-kumo-recessed/20 px-4 py-3">
              <div className="text-xs font-semibold text-kumo-strong">匹配规则</div>
              <div className="space-y-1.5 text-xs leading-relaxed text-kumo-subtle">
                <div>监测目标会按名称、类型、地址等字段匹配已有对象。</div>
                <div>状态页按 `slug` 匹配，维护窗口按标题匹配；命中后会执行更新。</div>
              </div>
            </div>

            <div className="space-y-2">
              <Button
                size="sm"
                variant="primary"
                className="w-full"
                onClick={commitUptimeImport}
                disabled={!uptimeImportPreview}
                loading={uptimeMetaLoading}
              >
                确认导入配置
              </Button>
              {uptimeImportPreview && (
                <Button
                  size="sm"
                  variant="secondary"
                  className="w-full"
                  onClick={() => {
                    setUptimeImportPreview(null);
                    setUptimeImportPayload(null);
                  }}
                >
                  清除预览
                </Button>
              )}
            </div>
          </SectionCard>
        </div>
      )}
    </div>
  );
}

export default UptimePage;
