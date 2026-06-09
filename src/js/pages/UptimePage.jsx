import React, { useState, useEffect, useRef, useMemo } from 'react';
import { io } from 'socket.io-client';
import * as echarts from 'echarts/core';
import { LineChart } from 'echarts/charts';
import {
  AriaComponent,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Input } from '@cloudflare/kumo/components/input';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Table } from '@cloudflare/kumo/components/table';
import { Tabs, TimeseriesChart } from '@cloudflare/kumo';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { AnimatedCollapse, DeferredRender } from '../components/AnimatedCollapse.jsx';
import { AppCard, ChartCard, ChartWarmupSkeleton, DataTableFrame } from '../components/ui/AppPrimitives.jsx';
import {
  Activity,
  Plus,
  Trash,
  Play,
  Pause,
  Save,
  RotateCw,
  Search,
  Edit,
  Globe,
  Terminal,
  TrendingUp,
  Server,
  Shield,
  Bell,
  Info,
  Download,
  Upload
} from '../components/Icons.jsx';

echarts.use([
  LineChart,
  AxisPointerComponent,
  BrushComponent,
  GridComponent,
  ToolboxComponent,
  TooltipComponent,
  CanvasRenderer,
  AriaComponent,
]);

// ==================== 样式辅助 ====================
const formatUptimeChartTime = (timestamp) => {
  const date = new Date(timestamp);
  if (Number.isNaN(date.getTime())) return '';
  return `${date.getHours().toString().padStart(2, '0')}:${date.getMinutes().toString().padStart(2, '0')}`;
};

const formatLatencyAxis = (value) => {
  const latency = Number(value) || 0;
  const abs = Math.abs(latency);
  if (abs >= 1000) return `${(latency / 1000).toFixed(abs >= 10000 ? 0 : 1)}s`;
  return `${Math.round(latency)}ms`;
};

const getKumoToken = (tokenName, fallback) => {
  if (typeof window === 'undefined') return fallback;
  const value = window.getComputedStyle(document.documentElement).getPropertyValue(tokenName).trim();
  return value || fallback;
};

const getKumoChartColors = () => ({
  brand: getKumoToken('--color-kumo-brand', '#2f80ed'),
  success: getKumoToken('--color-kumo-success', '#10b981'),
  warning: getKumoToken('--color-kumo-warning', '#f59e0b'),
  danger: getKumoToken('--color-kumo-danger', '#ef4444'),
  recessed: getKumoToken('--color-kumo-recessed', '#f3f4f6'),
});

// ==================== UptimeMonitorDetails 子组件 ====================
// 使用独立的子组件隔离 Kumo TimeseriesChart，在折叠/销毁时由组件自身清理 ECharts 实例
function UptimeMonitorDetails({
  monitor,
  heartbeats = [],
  loading = false,
  uptime24h,
  uptime30d,
  onPauseResume,
  onEdit,
  onDelete,
  formatDateTime,
  expanded = true,
}) {
  // 处理心跳时间范围标签
  const getHeartbeatTimeLabel = () => {
    if (heartbeats.length === 0) return '--';
    const count = heartbeats.length > 60 ? 60 : heartbeats.length;
    const oldestBeat = heartbeats[count - 1];
    if (!oldestBeat || !oldestBeat.time) return '--';

    const diffMs = Date.now() - new Date(oldestBeat.time).getTime();
    const seconds = Math.floor(diffMs / 1000);

    if (seconds < 60) return `${seconds}秒`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}分钟`;
    if (seconds < 86400) return `${Math.floor(seconds / 3600)}小时`;
    return `${Math.floor(seconds / 86400)}天`;
  };

  const chartData = useMemo(() => {
    const colors = getKumoChartColors();
    return [{
      name: '响应时间',
      color: colors.success,
      data: [...heartbeats]
        .slice(0, 60)
        .reverse()
        .map((beat) => [new Date(beat.time).getTime(), Number(beat.ping) || 0])
        .filter(([timestamp]) => Number.isFinite(timestamp)),
    }];
  }, [heartbeats]);

  // 生成 60 颗心跳丸
  const detailedBeats = useMemo(() => {
    const result = [];
    for (let i = 0; i < 60; i++) {
      const beat = heartbeats[i];
      if (beat) {
        result.unshift(beat);
      } else {
        result.unshift({ status: 'empty', time: null, ping: null });
      }
    }
    return result;
  }, [heartbeats]);

  return (
    <div className="border-t border-kumo-line bg-kumo-recessed/30 p-4 space-y-4">
      {/* 头部操作栏 */}
      <div className="flex items-center justify-between">
        <h5 className="text-[11px] font-bold text-kumo-strong uppercase tracking-wider flex items-center gap-1.5 select-none">
          <TrendingUp className="w-3.5 h-3.5" />
          监控图表与统计
        </h5>
        <div className="flex items-center gap-2">
          <Button size="sm"
            onClick={(e) => {
              e.stopPropagation();
              onPauseResume(monitor);
            }}
            icon={monitor.active ? <Pause className="w-3 h-3" /> : <Play className="w-3 h-3" />}
          >
            {monitor.active ? '暂停' : '启用'}
          </Button>
          <Button size="sm"
            onClick={(e) => {
              e.stopPropagation();
              onEdit(monitor);
            }}
            icon={<Edit className="w-3 h-3" />}
          >
            编辑
          </Button>
          <Button size="sm"
            variant="destructive"
            onClick={(e) => {
              e.stopPropagation();
              onDelete(monitor.id);
            }}
            icon={<Trash className="w-3 h-3" />}
          >
            删除
          </Button>
        </div>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-4 gap-4">
        {/* 图表主栏 (Span 3) */}
        <ChartCard className="relative h-36 lg:col-span-3">
          {(tooltipBoundary) => (
            <DeferredRender open={expanded} fallback={<ChartWarmupSkeleton height={120} />}>
              <TimeseriesChart
                echarts={echarts}
                data={chartData}
                height={120}
                yAxisName="ms"
                loading={loading}
                tooltipBoundary={tooltipBoundary ?? undefined}
                xAxisTickCount={3}
                yAxisTickCount={3}
                yAxisTickFormat={formatLatencyAxis}
                tooltipValueFormat={(value) => `${Math.round(value)} ms`}
                xAxisTickFormat={formatUptimeChartTime}
                tooltipMode="single"
                gradient
                ariaDescription="Uptime monitor response time history"
              />
            </DeferredRender>
          )}
        </ChartCard>

        {/* 右侧可用率统计指标 */}
        <div className="grid grid-cols-2 lg:grid-cols-1 gap-2.5">
          <AppCard padding="sm" className="flex flex-col justify-center">
            <span className="text-[9px] text-kumo-subtle select-none">24小时可用率</span>
            <span className="text-base font-bold text-kumo-strong font-mono mt-0.5">{uptime24h}%</span>
          </AppCard>
          <AppCard padding="sm" className="flex flex-col justify-center">
            <span className="text-[9px] text-kumo-subtle select-none">30天可用率</span>
            <span className="text-base font-bold text-kumo-strong font-mono mt-0.5">{uptime30d}%</span>
          </AppCard>
        </div>
      </div>

      {/* 心跳可视化图表 (60 pills) */}
      <div>
        <div className="flex gap-[3px] h-4 items-center">
          {detailedBeats.map((beat, idx) => {
            let colorClass = 'bg-kumo-line opacity-20';
            if (beat.status === 'up') colorClass = 'bg-kumo-success';
            if (beat.status === 'down') colorClass = 'bg-kumo-danger';
            if (beat.status === 'pending') colorClass = 'bg-kumo-warning';

            let tooltipText = '';
            if (beat.status !== 'empty') {
              tooltipText = `${formatDateTime(beat.time)} - ${beat.status === 'up' ? `正常 (${beat.ping}ms)` : `故障 (${beat.msg || 'Timeout'})`}`;
            }

            return (
              <div
                key={idx}
                className={`flex-1 h-3.5 rounded-sm transition-all ${colorClass}`}
                title={tooltipText}
              />
            );
          })}
        </div>
        <div className="flex justify-between text-[9px] text-kumo-subtle mt-1.5 font-mono select-none">
          <span>{getHeartbeatTimeLabel()}前</span>
          <span>现在</span>
        </div>
      </div>
    </div>
  );
}

// ==================== 主 UptimePage 组件 ====================
function UptimePage() {
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
  const [expandedMonitorId, setExpandedMonitorId] = useState(null);

  // 通知渠道配置
  const [notificationChannels, setNotificationChannels] = useState([]);

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
    const password = localStorage.getItem('admin_password') || '';
    return {
      'Content-Type': 'application/json',
      'x-admin-password': password,
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
            const beat = { ...m.lastHeartbeat };
            if (typeof beat.status === 'number') {
              beat.status = beat.status === 1 ? 'up' : 'down';
            }
            initialBeats[m.id] = [beat];
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
        const normalized = data.map(beat => {
          if (typeof beat.status === 'number') {
            return { ...beat, status: beat.status === 1 ? 'up' : 'down' };
          }
          return beat;
        });
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
      setUptimeRateCache(prev => ({
        ...prev,
        [monitorId]: { 1: d1.uptime || '100.000', 30: d30.uptime || '100.000' }
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
          title: 'Main Status',
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
      transports: ['websocket', 'polling']
    });

    socket.on('connect', () => {
      console.log('✅ Uptime Socket Connected');
    });

    socket.on('uptime:heartbeat', ({ monitorId, beat }) => {
      if (typeof beat.status === 'number') {
        beat.status = beat.status === 1 ? 'up' : 'down';
      }

      setUptimeHeartbeats(prev => {
        const list = prev[monitorId] ? [...prev[monitorId]] : [];
        list.unshift(beat);
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
    if (!(await dialog.deleteResource({
      resourceType: '监测目标',
      resourceName: monitor?.name || `#${id}`,
    }))) return;
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

  const handleBatchDelete = async () => {
    if (selectedMonitorIds.length === 0) return;
    if (!(await dialog.deleteResource({
      resourceType: '监测目标',
      resourceName: `选中的 ${selectedMonitorIds.length} 个监测目标`,
      confirmText: '批量删除',
    }))) return;

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
    <div className="space-y-6">
      {/* ==================== 顶部 Tab 导航 ==================== */}
      <div className="flex flex-wrap items-center justify-between border-b border-kumo-line pb-3 gap-4">
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
          tabs={[
            { value: 'list', label: <span className="inline-flex items-center gap-1.5"><Activity className="w-3.5 h-3.5" />仪表盘</span> },
            { value: 'add', label: <span className="inline-flex items-center gap-1.5"><Plus className="w-3.5 h-3.5" />添加监测</span> },
            { value: 'status-pages', label: <span className="inline-flex items-center gap-1.5"><Globe className="w-3.5 h-3.5" />状态页</span> },
            { value: 'maintenance', label: <span className="inline-flex items-center gap-1.5"><Shield className="w-3.5 h-3.5" />维护窗口</span> },
            { value: 'stats', label: <span className="inline-flex items-center gap-1.5"><TrendingUp className="w-3.5 h-3.5" />统计报表</span> },
          ]}
        />

        {uptimeCurrentTab === 'list' && (
          <div className="flex items-center gap-2 w-full md:w-auto">
            {/* 搜索框 */}
            <div className="relative flex-1 md:w-56">
              <span className="absolute left-2.5 top-1/2 -translate-y-1/2 text-kumo-subtle">
                <Search className="w-3.5 h-3.5" />
              </span>
              <Input size="sm"
                type="text"
                aria-label="搜索监测目标"
                placeholder="搜索监测目标..."
                value={uptimeSearchText}
                onChange={(e) => setUptimeSearchText(e.target.value)}
                className="w-full text-kumo-strong text-xs pl-8 pr-3 py-1.5"
              />
            </div>

            <Button size="sm" variant="primary" icon={<Plus className="w-4 h-4" />} onClick={handleOpenAdd}>
              新建目标
            </Button>
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
              <RotateCw className="w-8 h-8 animate-spin text-kumo-brand mb-4" />
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
              <div className="flex items-center justify-between app-subcard bg-kumo-recessed/30 px-4 py-2.5">
                <Checkbox
                  checked={isAllSelected}
                  onCheckedChange={handleToggleSelectAll}
                  label={`全选 (已选 ${selectedMonitorIds.length} 个)`}
                />
                {selectedMonitorIds.length > 0 && (
                  <Button
                    variant="destructive" size="sm"
                    onClick={handleBatchDelete}
                    icon={<Trash className="w-3 h-3" />}
                  >
                    批量删除
                  </Button>
                )}
              </div>

              {/* 监测卡片列表 */}
              <div className="flex flex-col gap-3">
                {filteredMonitors.map((monitor) => {
                  const beats = uptimeHeartbeats[monitor.id] || [];
                  const lastBeat = beats[0];
                  const isExpanded = expandedMonitorId === monitor.id;

                  // 状态指示
                  let statusClass = 'border-kumo-line';
                  let statusPillClass = 'bg-kumo-line/20 text-kumo-subtle';
                  let statusText = '暂停/未激活';

                  if (monitor.active) {
                    if (!lastBeat) {
                      statusClass = 'border-kumo-line';
                      statusPillClass = 'bg-kumo-line/20 text-kumo-subtle';
                      statusText = '等待中';
                    } else if (lastBeat.status === 'up') {
                      statusClass = 'border-l-4 border-l-kumo-success border-kumo-line';
                      statusPillClass = 'bg-kumo-success/10 text-kumo-success border border-kumo-success/20';
                      statusText = '正常';
                    } else if (lastBeat.status === 'down') {
                      statusClass = 'border-l-4 border-l-kumo-danger border-kumo-line';
                      statusPillClass = 'bg-kumo-danger/10 text-kumo-danger border border-kumo-danger/20';
                      statusText = '故障';
                    } else if (lastBeat.status === 'pending') {
                      statusClass = 'border-l-4 border-l-kumo-warning border-kumo-line';
                      statusPillClass = 'bg-kumo-warning/10 text-kumo-warning border border-kumo-warning/20';
                      statusText = '检测中';
                    }
                  }

                  // 30 个心跳迷你丸
                  const miniBeats = [];
                  for (let i = 0; i < 30; i++) {
                    const beat = beats[i];
                    if (beat) {
                      miniBeats.unshift(beat);
                    } else {
                      miniBeats.unshift({ status: 'empty' });
                    }
                  }

                  return (
                    <div
                      key={monitor.id}
                      className={`bg-kumo-base border rounded-lg overflow-hidden    ${statusClass}`}
                    >
                      {/* 卡片头部行 */}
                      <div
                        onClick={() => setExpandedMonitorId(isExpanded ? null : monitor.id)}
                        className="flex flex-col md:flex-row items-start md:items-center justify-between p-4 gap-4 cursor-pointer"
                      >
                        {/* 左侧选择复选框 & 图标 & 核心信息 */}
                        <div className="flex items-center gap-3 min-w-0 flex-1">
                          <Checkbox
                            checked={selectedMonitorIds.includes(monitor.id)}
                            onCheckedChange={(checked) => {
                              setSelectedMonitorIds(prev =>
                                checked ? [...prev, monitor.id] : prev.filter(id => id !== monitor.id)
                              );
                            }}
                            onClick={(e) => e.stopPropagation()}
                            aria-label={`选择监测目标: ${monitor.name}`}
                          />

                          {/* 类型图标 */}
                          <div className="w-8 h-8 rounded-lg bg-kumo-recessed flex items-center justify-center text-kumo-strong flex-shrink-0">
                            {getUptimeTypeIcon(monitor.type)}
                          </div>

                          <div className="min-w-0 flex-1">
                            <div className="flex flex-wrap items-center gap-2">
                              <span className="text-xs font-bold text-kumo-strong truncate">
                                {monitor.name}
                              </span>
                              <span className={`text-[9px] px-1.5 py-0.5 rounded font-semibold ${statusPillClass}`}>
                                {statusText}
                              </span>
                              {/* 标签 */}
                              {monitor.tags && monitor.tags.map(t => (
                                <span key={t} className="text-[9px] app-subcard bg-kumo-recessed text-kumo-subtle px-1.5 py-0.5 rounded font-medium">
                                  {t}
                                </span>
                              ))}
                            </div>
                            <div className="text-[10px] text-kumo-subtle truncate max-w-[320px] mt-1 select-all" onClick={(e) => e.stopPropagation()}>
                              {getDisplayUrl(monitor)}
                              <span className="text-kumo-subtle/40 mx-1.5 select-none">•</span>
                              <span className="select-none">{monitor.interval}s 频率</span>
                            </div>
                          </div>
                        </div>

                        {/* 右侧数据 & Heartbeat 迷你丸列 */}
                        <div className="flex items-center gap-4 w-full md:w-auto justify-between md:justify-end flex-shrink-0">
                          {/* 实时响应时延 & 可用率 */}
                          <div className="flex items-center gap-3 text-right">
                            <div className="flex flex-col">
                              <span className="text-[9px] text-kumo-subtle select-none">时延</span>
                              <span className="text-xs font-bold text-kumo-strong font-mono">
                                {lastBeat && lastBeat.status === 'up' ? `${lastBeat.ping}ms` : '--'}
                              </span>
                            </div>
                            <div className="flex flex-col">
                              <span className="text-[9px] text-kumo-subtle select-none">可用率</span>
                              <span className={`text-xs font-bold font-mono ${getUptimeRateClass(getUptimeRate(monitor.id, 1))}`}>
                                {getUptimeRate(monitor.id, 1)}%
                              </span>
                            </div>
                          </div>

                          {/* 30 心跳丸小条 */}
                          <div className="flex gap-[2px] items-center h-3 select-none">
                            {miniBeats.map((beat, idx) => {
                              let colorClass = 'bg-kumo-line opacity-20';
                              if (beat.status === 'up') colorClass = 'bg-kumo-success';
                              if (beat.status === 'down') colorClass = 'bg-kumo-danger';
                              if (beat.status === 'pending') colorClass = 'bg-kumo-warning';
                              return (
                                <div key={idx} className={`w-[3px] h-3 rounded-[1px] ${colorClass}`} />
                              );
                            })}
                          </div>
                        </div>
                      </div>

                      {/* 卡片下半部详情抽屉 */}
                      <AnimatedCollapse open={isExpanded}>
                        <UptimeMonitorDetails
                          monitor={monitor}
                          heartbeats={beats}
                          loading={!!uptimeHeartbeatLoading[monitor.id]}
                          uptime24h={getUptimeRate(monitor.id, 1)}
                          uptime30d={getUptimeRate(monitor.id, 30)}
                          onPauseResume={handleToggleActive}
                          onEdit={handleOpenEdit}
                          onDelete={handleDeleteMonitor}
                          formatDateTime={formatDateTime}
                          expanded={isExpanded}
                        />
                      </AnimatedCollapse>
                    </div>
                  );
                })}
              </div>
            </div>
          )}
        </div>
      )}

      {uptimeCurrentTab === 'status-pages' && (
        <AppCard padding="lg" className="space-y-4">
          <div className="flex flex-col md:flex-row md:items-center justify-between gap-3 border-b border-kumo-line pb-4">
            <div>
              <h3 className="text-sm font-semibold text-kumo-strong flex items-center gap-2">
                <Globe className="w-4 h-4" />
                状态页
              </h3>
              <p className="text-xs text-kumo-subtle mt-1">
                对外公开监控摘要，默认页会绑定当前全部监测目标。
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button
                size="sm"
                variant="secondary"
                icon={<RotateCw className="w-3.5 h-3.5" />}
                onClick={loadUptimeStatusPages}
                disabled={uptimeMetaLoading}
              >
                刷新
              </Button>
              <Button
                size="sm"
                variant="primary"
                icon={<Plus className="w-3.5 h-3.5" />}
                onClick={createDefaultStatusPage}
                disabled={uptimeMetaLoading}
              >
                生成默认页
              </Button>
            </div>
          </div>

          <DataTableFrame>
            <Table layout="fixed">
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head className="w-44">名称</Table.Head>
                  <Table.Head className="w-36">Slug</Table.Head>
                  <Table.Head>公开地址</Table.Head>
                  <Table.Head className="w-24 text-center">公开</Table.Head>
                  <Table.Head className="w-28">缓存</Table.Head>
                  <Table.Head className="w-36">更新时间</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {uptimeMetaLoading ? (
                  Array.from({ length: 3 }).map((_, index) => (
                    <Table.Row key={index}>
                      <Table.Cell colSpan={6}>
                        <SkeletonLine className="h-4 w-full" />
                      </Table.Cell>
                    </Table.Row>
                  ))
                ) : uptimeStatusPages.length === 0 ? (
                  <Table.Row>
                    <Table.Cell colSpan={6} className="py-10 text-center text-kumo-subtle">
                      暂无状态页。
                    </Table.Cell>
                  </Table.Row>
                ) : (
                  uptimeStatusPages.map((page) => {
                    const publicPath = `/api/uptime/public/status-pages/${page.slug}`;
                    return (
                      <Table.Row key={page.id}>
                        <Table.Cell className="font-semibold text-kumo-strong truncate">
                          {page.title || page.slug}
                        </Table.Cell>
                        <Table.Cell className="font-mono text-xs text-kumo-subtle truncate">
                          {page.slug}
                        </Table.Cell>
                        <Table.Cell>
                          <a
                            href={publicPath}
                            target="_blank"
                            rel="noreferrer"
                            className="block truncate font-mono text-xs text-kumo-brand hover:underline"
                          >
                            {publicPath}
                          </a>
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <span className={`text-[10px] px-2 py-0.5 rounded font-semibold ${page.public ? 'bg-kumo-success/10 text-kumo-success' : 'bg-kumo-line/30 text-kumo-subtle'}`}>
                            {page.public ? '公开' : '私有'}
                          </span>
                        </Table.Cell>
                        <Table.Cell className="font-mono text-xs">
                          {page.cacheSeconds || 300}s
                        </Table.Cell>
                        <Table.Cell className="text-xs text-kumo-subtle">
                          {formatDateTime(page.updatedAt || page.createdAt)}
                        </Table.Cell>
                      </Table.Row>
                    );
                  })
                )}
              </Table.Body>
            </Table>
          </DataTableFrame>
        </AppCard>
      )}

      {uptimeCurrentTab === 'maintenance' && (
        <AppCard padding="lg" className="space-y-4">
          <div className="flex flex-col md:flex-row md:items-center justify-between gap-3 border-b border-kumo-line pb-4">
            <div>
              <h3 className="text-sm font-semibold text-kumo-strong flex items-center gap-2">
                <Shield className="w-4 h-4" />
                维护窗口
              </h3>
              <p className="text-xs text-kumo-subtle mt-1">
                维护期内仍记录检测结果，但会抑制对应告警通知。
              </p>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              <Button
                size="sm"
                variant="secondary"
                icon={<RotateCw className="w-3.5 h-3.5" />}
                onClick={loadUptimeMaintenance}
                disabled={uptimeMetaLoading}
              >
                刷新
              </Button>
              <Button
                size="sm"
                variant="primary"
                icon={<Plus className="w-3.5 h-3.5" />}
                onClick={createQuickMaintenance}
                disabled={uptimeMetaLoading}
              >
                创建 1 小时窗口
              </Button>
            </div>
          </div>

          <div className="text-[11px] text-kumo-subtle">
            已选择 {selectedMonitorIds.length} 个监测目标用于快速维护窗口。
          </div>

          <DataTableFrame>
            <Table layout="fixed">
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head className="w-48">标题</Table.Head>
                  <Table.Head className="w-24 text-center">状态</Table.Head>
                  <Table.Head>时间窗口</Table.Head>
                  <Table.Head className="w-28">策略</Table.Head>
                  <Table.Head className="w-32">时区</Table.Head>
                  <Table.Head className="w-36">更新时间</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {uptimeMetaLoading ? (
                  Array.from({ length: 3 }).map((_, index) => (
                    <Table.Row key={index}>
                      <Table.Cell colSpan={6}>
                        <SkeletonLine className="h-4 w-full" />
                      </Table.Cell>
                    </Table.Row>
                  ))
                ) : uptimeMaintenanceWindows.length === 0 ? (
                  <Table.Row>
                    <Table.Cell colSpan={6} className="py-10 text-center text-kumo-subtle">
                      暂无维护窗口。
                    </Table.Cell>
                  </Table.Row>
                ) : (
                  uptimeMaintenanceWindows.map((item) => (
                    <Table.Row key={item.id}>
                      <Table.Cell className="font-semibold text-kumo-strong truncate">
                        {item.title}
                      </Table.Cell>
                      <Table.Cell className="text-center">
                        <span className={`text-[10px] px-2 py-0.5 rounded font-semibold ${item.active ? 'bg-kumo-success/10 text-kumo-success' : 'bg-kumo-line/30 text-kumo-subtle'}`}>
                          {item.active ? '启用' : '停用'}
                        </span>
                      </Table.Cell>
                      <Table.Cell className="font-mono text-xs text-kumo-subtle truncate">
                        {formatDateTime(item.startAt)} - {formatDateTime(item.endAt)}
                      </Table.Cell>
                      <Table.Cell className="text-xs">
                        {item.strategy || 'manual'}
                      </Table.Cell>
                      <Table.Cell className="text-xs">
                        {item.timezone || 'UTC'}
                      </Table.Cell>
                      <Table.Cell className="text-xs text-kumo-subtle">
                        {formatDateTime(item.updatedAt || item.createdAt)}
                      </Table.Cell>
                    </Table.Row>
                  ))
                )}
              </Table.Body>
            </Table>
          </DataTableFrame>
        </AppCard>
      )}

      {/* ==================== 2. 添加/修改监测目标 ==================== */}
      {uptimeCurrentTab === 'add' && (
        <AppCard padding="xl" className="space-y-6">
          <h3 className="text-sm font-semibold text-kumo-strong border-b border-kumo-line pb-3 select-none">
            {uptimeForm.id ? '编辑监测目标' : '新建监测目标'}
          </h3>

          <div className="grid grid-cols-1 md:grid-cols-12 gap-5">
            {/* 监控类型选择 (Full Width) */}
            <div className="md:col-span-12 space-y-1.5">
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
                  { value: 'ping', label: 'ICMP Ping' },
                  { value: 'dns', label: 'DNS 解析' },
                  { value: 'push', label: 'Push' },
                ]}
              />
            </div>

            {/* 目标显示名称 */}
            <div className="md:col-span-4">
              <Input
                label="显示名称 *"
                type="text" size="sm"
                placeholder="例如：生产数据库端口"
                value={uptimeForm.name}
                onChange={(e) => setUptimeForm(prev => ({ ...prev, name: e.target.value }))}
                className="w-full"
              />
            </div>

            {/* 地址输入域 */}
            {['http', 'keyword', 'json'].includes(uptimeForm.type) ? (
              <div className="md:col-span-8">
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
              <div className="md:col-span-8">
                <div className="app-subcard p-3">
                  <div className="text-[10px] font-semibold text-kumo-subtle">Push URL</div>
                  <div className="mt-1 truncate font-mono text-xs text-kumo-strong">
                    {uptimeForm.pushToken ? `/api/uptime/push/${uptimeForm.pushToken}` : '保存后自动生成 token URL'}
                  </div>
                </div>
              </div>
            ) : (
              <>
                <div className={uptimeForm.type === 'tcp' ? 'md:col-span-6' : 'md:col-span-8'}>
                  <Input
                    label="主机名 / IP *"
                    type="text" size="sm"
                    placeholder="例如：192.168.1.100 或 db.server.internal"
                    value={uptimeForm.hostname}
                    onChange={(e) => setUptimeForm(prev => ({ ...prev, hostname: e.target.value }))}
                    className="w-full"
                  />
                </div>
                {uptimeForm.type === 'tcp' && (
                  <div className="md:col-span-2">
                    <Input
                      label="连接端口 *"
                      type="number" size="sm"
                      placeholder="3306"
                      value={uptimeForm.port}
                      onChange={(e) => setUptimeForm(prev => ({ ...prev, port: parseInt(e.target.value) || 0 }))}
                      className="w-full font-mono"
                    />
                  </div>
                )}
              </>
            )}

            {/* 监测频率与重试参数 */}
            <div className="md:col-span-6">
              <Input
                label="检测频率（秒）"
                type="number" size="sm"
                min="20"
                value={uptimeForm.interval}
                onChange={(e) => setUptimeForm(prev => ({ ...prev, interval: parseInt(e.target.value) || 60 }))}
                className="w-full font-mono"
              />
            </div>
            <div className="md:col-span-6">
              <Input
                label="重试次数"
                type="number" size="sm"
                min="0"
                value={uptimeForm.retries}
                onChange={(e) => setUptimeForm(prev => ({ ...prev, retries: parseInt(e.target.value) || 0 }))}
                className="w-full font-mono"
              />
            </div>

            {/* 高级设置小节 */}
            <div className="md:col-span-12 border-t border-kumo-line pt-4 mt-2">
              <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-1.5 select-none">
                <Shield className="w-3.5 h-3.5" />
                安全与高级设置
              </h4>
            </div>

            {/* 证书过期设置 */}
            {['http', 'json'].includes(uptimeForm.type) && (
              <div className="md:col-span-6">
                <Input
                  label="SSL 证书到期提醒（天）"
                  type="number" size="sm"
                  placeholder="7"
                  value={uptimeForm.expiryNotification}
                  onChange={(e) => setUptimeForm(prev => ({ ...prev, expiryNotification: parseInt(e.target.value) || 7 }))}
                  className="w-full font-mono"
                />
              </div>
            )}

            {/* 忽略 TLS 选项 */}
            {['http', 'keyword', 'json'].includes(uptimeForm.type) && (
              <div className="md:col-span-6 flex items-end pb-2">
                  <Checkbox
                    checked={uptimeForm.ignoreTls}
                    onCheckedChange={(checked) => setUptimeForm(prev => ({ ...prev, ignoreTls: checked }))}
                    label="忽略不可信或自签名 TLS 证书"
                  />
              </div>
            )}

            {/* 网页关键字匹配 */}
            {uptimeForm.type === 'keyword' && (
              <div className="md:col-span-12">
                <Input
                  label="关键字匹配（网页中必须包含此文字）*"
                  type="text" size="sm"
                  placeholder="例如：success 或 正常"
                  value={uptimeForm.keyword}
                  onChange={(e) => setUptimeForm(prev => ({ ...prev, keyword: e.target.value }))}
                  className="w-full"
                />
              </div>
            )}

            {uptimeForm.type === 'json' && (
              <>
                <div className="md:col-span-4">
                  <Input
                    label="JSON 路径 *"
                    type="text" size="sm"
                    placeholder="例如：$.data.status"
                    value={uptimeForm.jsonQueryPath}
                    onChange={(e) => setUptimeForm(prev => ({ ...prev, jsonQueryPath: e.target.value }))}
                    className="w-full font-mono"
                  />
                </div>
                <div className="md:col-span-4">
                  <Input
                    label="比较操作"
                    type="text" size="sm"
                    placeholder="equals / contains / gt / regex"
                    value={uptimeForm.jsonQueryOperator}
                    onChange={(e) => setUptimeForm(prev => ({ ...prev, jsonQueryOperator: e.target.value }))}
                    className="w-full font-mono"
                  />
                </div>
                <div className="md:col-span-4">
                  <Input
                    label="期望值"
                    type="text" size="sm"
                    placeholder="例如：ok"
                    value={uptimeForm.jsonExpectedValue}
                    onChange={(e) => setUptimeForm(prev => ({ ...prev, jsonExpectedValue: e.target.value }))}
                    className="w-full font-mono"
                  />
                </div>
              </>
            )}

            {uptimeForm.type === 'push' && (
              <div className="md:col-span-6">
                <Input
                  label="Push 宽限时间（秒）"
                  type="number" size="sm"
                  min="30"
                  value={uptimeForm.pushGraceSeconds}
                  onChange={(e) => setUptimeForm(prev => ({ ...prev, pushGraceSeconds: parseInt(e.target.value) || 120 }))}
                  className="w-full font-mono"
                />
              </div>
            )}

            {/* 告警通知渠道设置 */}
            <div className="md:col-span-12 border-t border-kumo-line pt-4 mt-2">
              <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-1.5 select-none">
                <Bell className="w-3.5 h-3.5" />
                故障通知分发渠道
              </h4>
            </div>

            <div className="md:col-span-12 space-y-2">
              <div className="flex flex-wrap gap-4 p-3.5 app-subcard bg-kumo-recessed/50">
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
                    <span>暂无启用的告警通道。请先在 "通知渠道" 标签中配置并启用。</span>
                  </div>
                )}
              </div>
            </div>

            {/* 标签管理 */}
            <div className="md:col-span-12">
              <Input
                label="分组标签 (Tags)"
                type="text" size="sm"
                placeholder="prod, api, test (逗号或空格分割)"
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
        </AppCard>
      )}

      {/* ==================== 3. 统计报表 Tab ==================== */}
      {uptimeCurrentTab === 'stats' && (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
          <AppCard padding="lg" className="space-y-4">
            <div className="flex flex-col gap-3 border-b border-kumo-line pb-4 md:flex-row md:items-center md:justify-between">
              <div>
                <h3 className="text-sm font-semibold text-kumo-strong flex items-center gap-2">
                  <TrendingUp className="w-4 h-4" />
                  配置迁移预览
                </h3>
                <p className="text-xs text-kumo-subtle mt-1">
                  导入前会先比对监测目标、状态页和维护窗口，确认后才写入。
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <Input
                  ref={uptimeImportInputRef}
                  type="file"
                  accept=".json,application/json"
                  aria-label="选择 Uptime 配置文件"
                  className="hidden"
                  onChange={previewUptimeImportFile}
                />
                <Button size="sm" variant="secondary" onClick={exportUptimeConfig} loading={uptimeMetaLoading} icon={<Download className="w-3.5 h-3.5" />}>
                  导出配置
                </Button>
                <Button size="sm" variant="primary" onClick={() => uptimeImportInputRef.current?.click()} loading={uptimeMetaLoading} icon={<Upload className="w-3.5 h-3.5" />}>
                  选择导入文件
                </Button>
              </div>
            </div>

            {!uptimeImportPreview ? (
              <div className="flex flex-col items-center justify-center py-16 text-kumo-subtle">
                <Upload className="w-12 h-12 opacity-30 mb-3" />
                <div className="text-sm font-semibold text-kumo-strong">尚未选择导入文件</div>
                <div className="mt-1 text-xs">支持由本页面导出的 Uptime JSON 配置。</div>
              </div>
            ) : (
              <div className="space-y-4">
                <div className="grid grid-cols-3 gap-3">
                  <div className="app-subcard p-3">
                    <div className="text-[10px] text-kumo-subtle">监测目标</div>
                    <div className="mt-1 font-mono text-lg font-bold text-kumo-strong">{uptimeImportPreview.counts?.monitors || 0}</div>
                  </div>
                  <div className="app-subcard p-3">
                    <div className="text-[10px] text-kumo-subtle">状态页</div>
                    <div className="mt-1 font-mono text-lg font-bold text-kumo-strong">{uptimeImportPreview.counts?.statusPages || 0}</div>
                  </div>
                  <div className="app-subcard p-3">
                    <div className="text-[10px] text-kumo-subtle">维护窗口</div>
                    <div className="mt-1 font-mono text-lg font-bold text-kumo-strong">{uptimeImportPreview.counts?.maintenanceWindows || 0}</div>
                  </div>
                </div>

                <DataTableFrame>
                  <Table layout="fixed">
                    <Table.Header variant="compact">
                      <Table.Row>
                        <Table.Head>对象</Table.Head>
                        <Table.Head className="w-32">类型</Table.Head>
                        <Table.Head className="w-24">动作</Table.Head>
                      </Table.Row>
                    </Table.Header>
                    <Table.Body>
                      {[
                        ...(uptimeImportPreview.monitors || []).map(item => ({ ...item, label: item.name, kind: '监测' })),
                        ...(uptimeImportPreview.statusPages || []).map(item => ({ ...item, label: item.title || item.slug, kind: '状态页' })),
                        ...(uptimeImportPreview.maintenanceWindows || []).map(item => ({ ...item, label: item.title, kind: '维护' })),
                      ].map((item, index) => (
                        <Table.Row key={`${item.kind}-${item.label}-${index}`}>
                          <Table.Cell className="truncate text-xs font-semibold text-kumo-strong">{item.label}</Table.Cell>
                          <Table.Cell className="text-xs text-kumo-subtle">{item.kind}</Table.Cell>
                          <Table.Cell>
                            <span className={`rounded px-1.5 py-0.5 text-[10px] font-semibold ${
                              item.action === 'create'
                                ? 'bg-kumo-success/10 text-kumo-success'
                                : 'bg-kumo-warning/10 text-kumo-warning'
                            }`}>
                              {item.action === 'create' ? '创建' : '更新'}
                            </span>
                          </Table.Cell>
                        </Table.Row>
                      ))}
                    </Table.Body>
                  </Table>
                </DataTableFrame>
              </div>
            )}
          </AppCard>

          <AppCard padding="lg" className="space-y-4">
            <h3 className="text-sm font-semibold text-kumo-strong">导入确认</h3>
            <p className="text-xs leading-relaxed text-kumo-subtle">
              导入会按名称、类型、地址等字段匹配已有监测目标；同 slug 状态页、同标题维护窗口会更新。
            </p>
            <Button
              size="sm"
              variant="primary"
              className="w-full"
              onClick={commitUptimeImport}
              disabled={!uptimeImportPreview}
              loading={uptimeMetaLoading}
            >
              确认导入预览配置
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
          </AppCard>
        </div>
      )}
    </div>
  );
}

export default UptimePage;
