import React, { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { Switch } from '@cloudflare/kumo/components/switch';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Autocomplete } from '@cloudflare/kumo/components/autocomplete';
import { Tabs } from '@cloudflare/kumo';
import useTableResize from '../composables/useTableResize.js';
import useStore from '../store.js';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { handleEditableRowDoubleClick } from '../modules/tableInteractions.js';
import { renderMarkdown, formatDateTime } from '../modules/utils.js';
import { AnimatedCollapse } from '../components/AnimatedCollapse.jsx';
import {
  PageStack,
  PageToolbar,
  AppCard,
  DataTableFrame,
  AppTable,
  InlineStatusPill,
  EmptyState,
  SectionHeader,
  iconButtonIconClass,
  actionIconClass,
  cx,
} from '../components/ui/AppPrimitives.jsx';
import {
  Server,
  Users,
  MessageSquare,
  Plus,
  Trash,
  RotateCw,
  Search,
  Upload,
  Download,
  Edit,
  X,
  ChevronDown,
  ChevronUp,
  RefreshCw,
  History,
  PieChart,
  Bot,
  Star,
  Pin,
  Activity,
  Send,
  Check,
  Paperclip,
  Eye,
  EyeOff,
  Plug,
  Brain,
  Sliders,
  Settings as SettingsIcon,
  Copy,
  AlertTriangle,
} from '../components/Icons.jsx';

function SVGAnalyticsChart({ dailyData }) {
  if (!dailyData || dailyData.length === 0) {
    return (
      <div className="h-full flex items-center justify-center text-kumo-subtle text-xs">
        暂无趋势数据
      </div>
    );
  }

  const width = 500;
  const height = 180;
  const paddingLeft = 40;
  const paddingRight = 40;
  const paddingTop = 20;
  const paddingBottom = 30;

  const maxCount = Math.max(...dailyData.map(d => d.count), 5);
  const maxLatency = Math.max(...dailyData.map(d => d.avgLatency), 500);

  const pointsCount = dailyData.map((d, index) => {
    const x =
      paddingLeft + (index / (dailyData.length - 1 || 1)) * (width - paddingLeft - paddingRight);
    const y = height - paddingBottom - (d.count / maxCount) * (height - paddingTop - paddingBottom);
    return { x, y, label: d.count, day: d.day };
  });

  const pointsLatency = dailyData.map((d, index) => {
    const x =
      paddingLeft + (index / (dailyData.length - 1 || 1)) * (width - paddingLeft - paddingRight);
    const y =
      height - paddingBottom - (d.avgLatency / maxLatency) * (height - paddingTop - paddingBottom);
    return { x, y, label: d.avgLatency, day: d.day };
  });

  const countPath = pointsCount.reduce(
    (path, p, i) => (i === 0 ? `M ${p.x} ${p.y}` : `${path} L ${p.x} ${p.y}`),
    ''
  );
  const latencyPath = pointsLatency.reduce(
    (path, p, i) => (i === 0 ? `M ${p.x} ${p.y}` : `${path} L ${p.x} ${p.y}`),
    ''
  );

  return (
    <svg viewBox={`0 0 ${width} ${height}`} className="w-full h-full text-kumo-strong">
      {/* Grid Lines */}
      {[0, 0.25, 0.5, 0.75, 1].map((ratio, i) => {
        const y = height - paddingBottom - ratio * (height - paddingTop - paddingBottom);
        return (
          <g key={i}>
            <line
              x1={paddingLeft}
              y1={y}
              x2={width - paddingRight}
              y2={y}
              stroke="var(--kumo-line, #e2e8f0)"
              strokeDasharray="3 3"
              strokeWidth="0.5"
            />
            {/* Left Axis (Requests) */}
            <text
              x={paddingLeft - 8}
              y={y + 4}
              textAnchor="end"
              className="text-[9px] fill-kumo-subtle font-mono"
            >
              {Math.round(ratio * maxCount)}
            </text>
            {/* Right Axis (Latency) */}
            <text
              x={width - paddingRight + 8}
              y={y + 4}
              textAnchor="start"
              className="text-[9px] fill-kumo-subtle font-mono"
            >
              {Math.round(ratio * maxLatency)} ms
            </text>
          </g>
        );
      })}

      {/* X Axis Labels */}
      {dailyData.map((d, index) => {
        const x =
          paddingLeft +
          (index / (dailyData.length - 1 || 1)) * (width - paddingLeft - paddingRight);
        return (
          <text
            key={index}
            x={x}
            y={height - 10}
            textAnchor="middle"
            className="text-[9px] fill-kumo-subtle font-mono"
          >
            {d.day}
          </text>
        );
      })}

      {/* Paths */}
      <path
        d={countPath}
        fill="none"
        stroke="var(--kumo-brand, #3b82f6)"
        strokeWidth="2.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <path
        d={latencyPath}
        fill="none"
        stroke="var(--kumo-warning, #f59e0b)"
        strokeWidth="2"
        strokeDasharray="4 2"
        strokeLinecap="round"
        strokeLinejoin="round"
      />

      {/* Dots & Tooltips */}
      {pointsCount.map((p, i) => (
        <g key={`count-dot-${i}`} className="group/dot cursor-pointer">
          <circle
            cx={p.x}
            cy={p.y}
            r="3.5"
            className="fill-kumo-brand stroke-kumo-base"
            strokeWidth="1.5"
          />
          <circle cx={p.x} cy={p.y} r="8" className="fill-transparent hover:fill-kumo-brand/10" />
          <title>{`日期: ${p.day}\n请求数: ${p.label} 次`}</title>
        </g>
      ))}

      {pointsLatency.map((p, i) => (
        <g key={`latency-dot-${i}`} className="group/dot cursor-pointer">
          <circle
            cx={p.x}
            cy={p.y}
            r="3"
            className="fill-kumo-warning stroke-kumo-base"
            strokeWidth="1.5"
          />
          <circle cx={p.x} cy={p.y} r="8" className="fill-transparent hover:fill-kumo-warning/10" />
          <title>{`日期: ${p.day}\n延迟: ${p.label.toFixed(0)} ms`}</title>
        </g>
      ))}
    </svg>
  );
}

function OpenAIPage() {
  const { theme } = useStore();
  const [colWidths, startResize] = useTableResize([150, 250, 150, 80, 80, 100, 120]);

  // Tab State
  const [activeTab, setActiveTab] = useState('endpoints'); // 'endpoints' | 'accounts' | 'chat'

  // Gateway Analytics States
  const [analyticsDays, setAnalyticsDays] = useState(7);
  const [analyticsSummary, setAnalyticsSummary] = useState({
    totalRequests: 0,
    avgLatency: 0,
    totalTokens: 0,
    errorRate: 0,
  });
  const [analyticsCharts, setAnalyticsCharts] = useState({
    daily: [],
    models: [],
  });
  const [analyticsLogs, setAnalyticsLogs] = useState([]);
  const [analyticsPage, setAnalyticsPage] = useState(1);
  const [analyticsTotal, setAnalyticsTotal] = useState(0);
  const [analyticsLoading, setAnalyticsLoading] = useState(false);

  const getAuthHeaders = useCallback(() => {
    const password = localStorage.getItem('admin_password') || '';
    return {
      'Content-Type': 'application/json',
      'x-admin-password': password,
    };
  }, []);

  const fetchAnalytics = useCallback(async () => {
    setAnalyticsLoading(true);
    try {
      const headers = getAuthHeaders();
      const [sumRes, chartsRes, logsRes] = await Promise.all([
        fetch(`/api/openai/analytics/summary?days=${analyticsDays}`, { headers }),
        fetch(`/api/openai/analytics/charts?days=${analyticsDays}`, { headers }),
        fetch(`/api/openai/analytics/logs?page=${analyticsPage}&pageSize=10`, { headers }),
      ]);

      if (sumRes.ok) {
        const data = await sumRes.json();
        setAnalyticsSummary(data);
      }
      if (chartsRes.ok) {
        const data = await chartsRes.json();
        setAnalyticsCharts(data);
      }
      if (logsRes.ok) {
        const data = await logsRes.json();
        setAnalyticsLogs(data.records || []);
        setAnalyticsTotal(data.total || 0);
      }
    } catch (err) {
      console.error('Failed to fetch analytics:', err);
      toast.error('获取分析数据失败');
    } finally {
      setAnalyticsLoading(false);
    }
  }, [analyticsDays, analyticsPage, getAuthHeaders]);

  useEffect(() => {
    if (activeTab === 'analytics') {
      fetchAnalytics();
    }
  }, [activeTab, fetchAnalytics]);

  // IP/Address Masking Helper
  const maskAddress = address => {
    if (!address) return '';
    try {
      const url = new URL(address);
      return `${url.protocol}//${url.hostname.slice(0, 3)}****${url.hostname.slice(-3)}${url.port ? ':' + url.port : ''}${url.pathname}`;
    } catch {
      if (address.length > 15) {
        return address.slice(0, 6) + '****' + address.slice(-6);
      }
      return address;
    }
  };

  const maskApiKey = key => {
    if (!key) return '';
    if (key.length <= 8) return '****';
    return key.substring(0, 4) + '****' + key.substring(key.length - 4);
  };

  const chatStorage = useMemo(() => {
    const personasKey = 'openai_chat_personas_v2';
    const sessionsKey = 'openai_chat_sessions_v2';
    const messagesKey = 'openai_chat_messages_v2';
    const defaultPersona = {
      id: 1,
      name: '默认助手',
      icon: 'fa-robot',
      system_prompt: '你是一个有用的 AI 助手。',
      is_default: 1,
    };

    const readJson = (key, fallback) => {
      try {
        const value = localStorage.getItem(key);
        return value ? JSON.parse(value) : fallback;
      } catch {
        return fallback;
      }
    };
    const writeJson = (key, value) => localStorage.setItem(key, JSON.stringify(value));
    const readPersonas = () => {
      const loaded = readJson(personasKey, [defaultPersona]);
      return Array.isArray(loaded) && loaded.length > 0 ? loaded : [defaultPersona];
    };
    const readSessions = () => {
      const loaded = readJson(sessionsKey, []);
      return Array.isArray(loaded) ? loaded : [];
    };
    const readMessages = () => readJson(messagesKey, {});
    const writeMessagesForSession = (sessionId, nextMessages) => {
      const bySession = readMessages();
      bySession[sessionId] = nextMessages;
      writeJson(messagesKey, bySession);
    };

    return {
      defaultPersona,
      readPersonas,
      savePersonas: nextPersonas => writeJson(personasKey, nextPersonas),
      readSessions,
      saveSessions: nextSessions => writeJson(sessionsKey, nextSessions),
      readSessionMessages: sessionId => {
        const messagesBySession = readMessages();
        return Array.isArray(messagesBySession[sessionId]) ? messagesBySession[sessionId] : [];
      },
      saveSessionMessages: writeMessagesForSession,
      deleteSessionMessages: sessionId => {
        const bySession = readMessages();
        delete bySession[sessionId];
        writeJson(messagesKey, bySession);
      },
      newId: () => `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`,
    };
  }, []);

  // ==================== 1. Endpoints & Accounts State ====================
  const [endpoints, setEndpoints] = useState([]);
  const [endpointsLoading, setEndpointsLoading] = useState(false);
  const [endpointsRefreshing, setEndpointsRefreshing] = useState(false);
  const [expandedEndpoints, setExpandedEndpoints] = useState({});
  const [endpointFormOpen, setEndpointFormOpen] = useState(false);
  const [editingEndpoint, setEditingEndpoint] = useState(null);
  const [endpointForm, setEndpointForm] = useState({
    name: '',
    baseUrl: '',
    apiKey: '',
    notes: '',
  });
  const [endpointFormError, setEndpointFormError] = useState('');
  const [endpointSaving, setEndpointSaving] = useState(false);

  // Batch adding endpoints
  const [batchText, setBatchText] = useState('');
  const [batchError, setBatchError] = useState('');
  const [batchSuccess, setBatchSuccess] = useState('');
  const [batchAdding, setBatchAdding] = useState(false);

  // Load Endpoints
  const loadEndpoints = useCallback(
    async (silent = false) => {
      if (!silent) setEndpointsLoading(true);
      try {
        const response = await fetch('/api/openai/endpoints', {
          headers: getAuthHeaders(),
        });
        const data = await response.json();
        if (Array.isArray(data)) {
          setEndpoints(data.map(ep => ({ ...ep, showKey: false, refreshing: false })));
          localStorage.setItem(
            'openai_endpoints_cache',
            JSON.stringify({
              endpoints: data,
              timestamp: Date.now(),
            })
          );
        }
      } catch (error) {
        console.error('Failed to load endpoints:', error);
        toast.error('加载端点失败');
      } finally {
        if (!silent) setEndpointsLoading(false);
      }
    },
    [getAuthHeaders]
  );

  useEffect(() => {
    // Try cache first
    try {
      const cached = localStorage.getItem('openai_endpoints_cache');
      if (cached) {
        const parsed = JSON.parse(cached);
        if (parsed && Array.isArray(parsed.endpoints)) {
          setEndpoints(parsed.endpoints.map(ep => ({ ...ep, showKey: false, refreshing: false })));
        }
      }
    } catch (e) {
      console.warn('Load endpoints cache failed:', e);
    }
    loadEndpoints(true);
  }, [loadEndpoints]);

  // Expand endpoint models grid
  const toggleEndpointExpand = id => {
    setExpandedEndpoints(prev => ({ ...prev, [id]: !prev[id] }));
  };

  // Endpoint Verification & Model Refresh
  const verifyEndpoint = async endpoint => {
    try {
      toast.info(`正在验证 ${endpoint.name || '端点'}...`);
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.valid) {
        toast.success(`验证成功！找到 ${data.modelsCount || 0} 个模型`);
        await loadEndpoints(true);
      } else {
        toast.error('验证失败: ' + (data.error || 'API Key 无效'));
      }
    } catch (error) {
      toast.error('验证失败: ' + error.message);
    }
  };

  const refreshEndpointModels = async endpoint => {
    if (endpoint.refreshing) return;
    // Set local refreshing
    setEndpoints(prev => prev.map(e => (e.id === endpoint.id ? { ...e, refreshing: true } : e)));
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/verify`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.valid) {
        toast.success(`${endpoint.name || '端点'} 模型列表已更新`);
        await loadEndpoints(true);
      } else {
        toast.error('刷新失败: ' + (data.error || 'API Key 无效'));
      }
    } catch (error) {
      toast.error('刷新失败: ' + error.message);
    } finally {
      setEndpoints(prev => prev.map(e => (e.id === endpoint.id ? { ...e, refreshing: false } : e)));
    }
  };

  const refreshAllEndpoints = async () => {
    setEndpointsRefreshing(true);
    try {
      const response = await fetch('/api/openai/endpoints/refresh', {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.success) {
        const successCount = data.results?.filter(r => r.success).length || 0;
        toast.success(`刷新完成！已更新 ${successCount} 个启用端点`);
        await loadEndpoints(true);
      } else {
        toast.error('刷新失败: ' + (data.error || '未知错误'));
      }
    } catch (error) {
      toast.error('刷新失败: ' + error.message);
    } finally {
      setEndpointsRefreshing(false);
    }
  };

  const toggleEndpointEnabled = async endpoint => {
    const updatedEnabled = !endpoint.enabled;
    // Optimistic UI update
    setEndpoints(prev =>
      prev.map(e => (e.id === endpoint.id ? { ...e, enabled: updatedEnabled } : e))
    );
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled: updatedEnabled }),
      });
      const data = await response.json();
      if (!data.success) {
        toast.error('操作失败: ' + (data.error || '未知错误'));
        // Rollback
        setEndpoints(prev =>
          prev.map(e => (e.id === endpoint.id ? { ...e, enabled: !updatedEnabled } : e))
        );
      } else {
        toast.success(updatedEnabled ? '端点已启用' : '端点已禁用');
        loadAllModels(true);
      }
    } catch (error) {
      toast.error('操作失败: ' + error.message);
      setEndpoints(prev =>
        prev.map(e => (e.id === endpoint.id ? { ...e, enabled: !updatedEnabled } : e))
      );
    }
  };

  const openAddEndpointModal = () => {
    setEditingEndpoint(null);
    setEndpointForm({ name: '', baseUrl: '', apiKey: '', notes: '' });
    setEndpointFormError('');
    setEndpointFormOpen(true);
  };

  const openEditEndpointModal = endpoint => {
    setEditingEndpoint(endpoint);
    setEndpointForm({
      name: endpoint.name || '',
      baseUrl: endpoint.baseUrl || '',
      apiKey: endpoint.apiKey || '',
      notes: endpoint.notes || '',
    });
    setEndpointFormError('');
    setEndpointFormOpen(true);
  };

  const saveEndpoint = async () => {
    if (!endpointForm.baseUrl || !endpointForm.apiKey) {
      setEndpointFormError('请填写 API 地址和 API Key');
      return;
    }
    setEndpointSaving(true);
    setEndpointFormError('');
    try {
      const url = editingEndpoint
        ? `/api/openai/endpoints/${editingEndpoint.id}`
        : '/api/openai/endpoints';
      const response = await fetch(url, {
        method: editingEndpoint ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(endpointForm),
      });
      const data = await response.json();
      if (response.ok && (data.success || data.endpoint || data.id)) {
        toast.success(editingEndpoint ? '端点已更新' : '端点已添加');
        setEndpointFormOpen(false);
        await loadEndpoints(true);
        loadAllModels(true);
      } else {
        setEndpointFormError(data.error || '保存失败');
      }
    } catch (error) {
      setEndpointFormError('保存失败: ' + error.message);
    } finally {
      setEndpointSaving(false);
    }
  };

  const deleteEndpoint = async endpoint => {
    if (!(await dialog.confirm(`确定要删除端点 "${endpoint.name || endpoint.baseUrl}" 吗？`))) {
      return;
    }
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (response.ok && data.success) {
        toast.success('端点已删除');
        await loadEndpoints(true);
        loadAllModels(true);
      } else {
        toast.error('删除失败: ' + (data.error || '未知错误'));
      }
    } catch (error) {
      toast.error('删除失败: ' + error.message);
    }
  };

  const batchAddEndpoints = async () => {
    setBatchError('');
    setBatchSuccess('');
    if (!batchText.trim()) {
      setBatchError('请输入端点信息');
      return;
    }
    setBatchAdding(true);
    try {
      let payload = { text: batchText };
      try {
        const parsed = JSON.parse(batchText);
        if (Array.isArray(parsed)) {
          payload = { endpoints: parsed };
        }
      } catch (e) {
        // Fallback to text format
      }

      const response = await fetch('/api/openai/batch-add', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(payload),
      });
      const data = await response.json();
      if (data.success) {
        setBatchSuccess(`成功添加 ${data.added || 0} 个端点`);
        setBatchText('');
        await loadEndpoints(true);
        loadAllModels(true);
      } else {
        setBatchError(data.error || '添加失败');
      }
    } catch (error) {
      setBatchError('添加失败: ' + error.message);
    } finally {
      setBatchAdding(false);
    }
  };

  const exportEndpoints = () => {
    if (endpoints.length === 0) {
      toast.warning('没有可导出的端点');
      return;
    }
    try {
      const exportData = {
        version: '1.0',
        exportTime: new Date().toISOString(),
        endpoints: endpoints.map(ep => ({
          name: ep.name,
          baseUrl: ep.baseUrl,
          apiKey: ep.apiKey,
          notes: ep.notes,
        })),
      };
      const dataStr = JSON.stringify(exportData, null, 2);
      const dataBlob = new Blob([dataStr], { type: 'application/json' });
      const url = URL.createObjectURL(dataBlob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `openai-endpoints-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);
      toast.success('端点导出成功');
    } catch (error) {
      toast.error('导出失败: ' + error.message);
    }
  };

  const importEndpoints = async () => {
    if (!(await dialog.confirm('确认导入？导入端点将添加到现有列表中。'))) {
      return;
    }
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json';
    input.onchange = async event => {
      const file = event.target.files[0];
      if (!file) return;

      const reader = new FileReader();
      reader.onload = async e => {
        try {
          const importedData = JSON.parse(e.target.result);
          if (!importedData.endpoints) {
            toast.error('无效的备份文件格式');
            return;
          }
          const response = await fetch('/api/openai/import', {
            method: 'POST',
            headers: getAuthHeaders(),
            body: JSON.stringify({ endpoints: importedData.endpoints }),
          });
          const data = await response.json();
          if (data.success) {
            let msg = `成功导入 ${data.imported || 0} 个端点`;
            if (data.skipped > 0) msg += `，跳过 ${data.skipped} 个重复项`;
            toast.success(msg);
            await loadEndpoints(true);
            loadAllModels(true);
          } else {
            toast.error('导入失败: ' + (data.error || '未知错误'));
          }
        } catch (error) {
          toast.error('导入失败: ' + error.message);
        }
      };
      reader.readAsText(file);
    };
    input.click();
  };

  // ==================== 2. Health Checking ====================
  const [openaiModelHealth, setOpenaiModelHealth] = useState(() => {
    try {
      const saved = localStorage.getItem('openai_model_health_cache');
      return saved ? JSON.parse(saved) : {};
    } catch {
      return {};
    }
  });

  const saveModelHealth = health => {
    setOpenaiModelHealth(health);
    localStorage.setItem('openai_model_health_cache', JSON.stringify(health));
  };

  const [modelHealthBatchLoading, setModelHealthBatchLoading] = useState(false);
  const [healthCheckModal, setHealthCheckModal] = useState(false);
  const [healthCheckForm, setHealthCheckForm] = useState({
    useKey: 'single', // 'single' | 'all'
    concurrency: false,
    timeout: 10,
  });

  const testModelHealth = async (model, targetEndpointId = null) => {
    const modelId = model.id;
    saveModelHealth(prev => ({
      ...prev,
      [modelId]: { status: 'checking', loading: true, latency: null, checkedAt: Date.now() },
    }));

    try {
      const headers = { ...getAuthHeaders(), 'Content-Type': 'application/json' };
      if (targetEndpointId) {
        headers['x-endpoint-id'] = targetEndpointId;
      }
      const startTime = Date.now();
      const response = await fetch('/api/openai/v1/chat/completions', {
        method: 'POST',
        headers,
        body: JSON.stringify({
          model: modelId,
          messages: [{ role: 'user', content: 'ping' }],
          max_tokens: 5,
        }),
      });

      const latency = Date.now() - startTime;
      if (response.ok) {
        saveModelHealth(prev => ({
          ...prev,
          [modelId]: { status: 'healthy', loading: false, latency, checkedAt: Date.now() },
        }));
      } else {
        saveModelHealth(prev => ({
          ...prev,
          [modelId]: {
            status: 'error',
            loading: false,
            latency: null,
            checkedAt: Date.now(),
            error: `HTTP ${response.status}`,
          },
        }));
      }
    } catch (e) {
      saveModelHealth(prev => ({
        ...prev,
        [modelId]: {
          status: 'error',
          loading: false,
          latency: null,
          checkedAt: Date.now(),
          error: e.message,
        },
      }));
    }
  };

  const startBatchHealthCheck = async () => {
    setHealthCheckModal(false);
    setModelHealthBatchLoading(true);
    toast.info('已启动后台模型可用性健康检测，请稍候...');

    // Collect all unique models
    const allModelsMap = new Map();
    endpoints.forEach(ep => {
      if (ep.enabled && ep.models) {
        ep.models.forEach(m => {
          const id = typeof m === 'string' ? m : m.id;
          if (id) allModelsMap.set(id, ep.id);
        });
      }
    });

    const modelsToCheck = Array.from(allModelsMap.entries()).map(([id, epId]) => ({ id, epId }));
    if (modelsToCheck.length === 0) {
      toast.warning('没有找到任何启用的端点或模型');
      setModelHealthBatchLoading(false);
      return;
    }

    if (healthCheckForm.concurrency) {
      // Parallel execution
      await Promise.all(modelsToCheck.map(m => testModelHealth(m, m.epId)));
    } else {
      // Sequential execution
      for (const m of modelsToCheck) {
        await testModelHealth(m, m.epId);
      }
    }

    toast.success('所有模型可用性健康检测完成');
    setModelHealthBatchLoading(false);
  };

  const openHealthCheckForEndpoint = async endpointId => {
    const ep = endpoints.find(e => e.id === endpointId);
    if (!ep || !ep.models || ep.models.length === 0) {
      toast.warning('该端点无可用模型');
      return;
    }
    setModelHealthBatchLoading(true);
    toast.info(`正在检测 ${ep.name || '端点'} 的所有模型...`);
    for (const m of ep.models) {
      const modelId = typeof m === 'string' ? m : m.id;
      await testModelHealth({ id: modelId }, ep.id);
    }
    toast.success(`${ep.name || '端点'} 模型检测完成`);
    setModelHealthBatchLoading(false);
  };

  // ==================== 3. Models List & Pinning ====================
  const [allModels, setAllModels] = useState([]);
  const [pinnedModels, setPinnedModels] = useState(() => {
    try {
      const saved = localStorage.getItem('openai_pinned_models');
      return saved ? JSON.parse(saved) : [];
    } catch {
      return [];
    }
  });
  const [hiddenModels, setHiddenModels] = useState(() => {
    try {
      const saved = localStorage.getItem('openai_hidden_models');
      return saved ? JSON.parse(saved) : [];
    } catch {
      return [];
    }
  });

  const [chatEndpoint, setChatEndpoint] = useState(() => {
    return localStorage.getItem('openai_chat_endpoint') || '';
  });
  const [chatModel, setChatModel] = useState(() => {
    return localStorage.getItem('openai_chat_model') || '';
  });
  const [defaultChatModel, setDefaultChatModel] = useState(() => {
    return localStorage.getItem('openai_default_model') || '';
  });

  // Settings configurations
  const [showHChatSettingsModal, setShowHChatSettingsModal] = useState(false);
  const [openaiSettingsTab, setOpenaiSettingsTab] = useState('general');
  const [openaiChatSystemPrompt, setOpenaiChatSystemPrompt] = useState(() => {
    return localStorage.getItem('openai_system_prompt') || '你是一个有用的 AI 助手。';
  });
  const [openaiChatSettings, setOpenaiChatSettings] = useState(() => {
    try {
      const saved = localStorage.getItem('openai_chat_settings');
      return saved ? JSON.parse(saved) : { temperature: 0.7, max_tokens: 2000 };
    } catch {
      return { temperature: 0.7, max_tokens: 2000 };
    }
  });

  const [openaiAutoTitleEnabled, setOpenaiAutoTitleEnabled] = useState(() => {
    return localStorage.getItem('openai_auto_title_enabled') === 'true';
  });
  const [openaiTitleModels, setOpenaiTitleModels] = useState(() => {
    try {
      const saved = localStorage.getItem('openai_title_models');
      return saved ? JSON.parse(saved) : [];
    } catch {
      return [];
    }
  });
  const [openaiTitleModelToAdd, setOpenaiTitleModelToAdd] = useState('');
  const [openaiTitleGenerating, setOpenaiTitleGenerating] = useState(false);
  const [openaiTitleLastResult, setOpenaiTitleLastResult] = useState(null);

  // Model selection helper dropdowns UI states
  const [showEndpointDropdown, setShowEndpointDropdown] = useState(false);
  const [showModelDropdown, setShowModelDropdown] = useState(false);
  const [dropdownModelSearch, setDropdownModelSearch] = useState('');
  const [openaiModelSearch, setOpenaiModelSearch] = useState('');
  const [openaiSelectedEndpointId, setOpenaiSelectedEndpointId] = useState('');
  const [openaiShowHiddenModels, setOpenaiShowHiddenModels] = useState(false);

  // Close dropdowns on outside click
  useEffect(() => {
    const handleOutsideClick = () => {
      setShowEndpointDropdown(false);
      setShowModelDropdown(false);
      setShowPersonaDropdown(false);
    };
    window.addEventListener('click', handleOutsideClick);
    return () => window.removeEventListener('click', handleOutsideClick);
  }, []);

  const loadAllModels = useCallback(
    async (silent = false) => {
      try {
        const response = await fetch('/api/openai/v1/models', {
          headers: getAuthHeaders(),
        });
        const data = await response.json();
        if (data && Array.isArray(data.data)) {
          const sorted = data.data.sort((a, b) => {
            if (a.owned_by !== b.owned_by) return a.owned_by.localeCompare(b.owned_by);
            return a.id.localeCompare(b.id);
          });
          setAllModels(sorted);

          // Smart initialize model
          if (sorted.length > 0) {
            const currentModel = localStorage.getItem('openai_chat_model');
            let modelIsValid = false;
            if (currentModel) {
              modelIsValid = sorted.some(m => m.id === currentModel);
            }
            if (!modelIsValid) {
              const defModel = localStorage.getItem('openai_default_model');
              if (defModel && sorted.some(m => m.id === defModel)) {
                setChatModel(defModel);
                localStorage.setItem('openai_chat_model', defModel);
              } else {
                setChatModel(sorted[0].id);
                localStorage.setItem('openai_chat_model', sorted[0].id);
              }
            }
          }
        }
      } catch (error) {
        console.error('Failed to load models list:', error);
      }
    },
    [getAuthHeaders]
  );

  useEffect(() => {
    loadAllModels(true);
  }, [loadAllModels]);

  const togglePinModel = modelId => {
    if (!modelId) return;
    setPinnedModels(prev => {
      let next;
      if (prev.includes(modelId)) {
        next = prev.filter(id => id !== modelId);
      } else {
        next = [...prev, modelId];
      }
      localStorage.setItem('openai_pinned_models', JSON.stringify(next));
      return next;
    });
  };

  const toggleHideModel = modelId => {
    if (!modelId) return;
    setHiddenModels(prev => {
      let next;
      if (prev.includes(modelId)) {
        next = prev.filter(id => id !== modelId);
      } else {
        next = [...prev, modelId];
      }
      localStorage.setItem('openai_hidden_models', JSON.stringify(next));
      return next;
    });
  };

  const handleSetDefaultModel = () => {
    if (!chatModel) return;
    setDefaultChatModel(chatModel);
    localStorage.setItem('openai_default_model', chatModel);
    toast.success(`已将 ${chatModel} 设为默认模型`);
  };

  const handleClearDefaultModel = () => {
    setDefaultChatModel('');
    localStorage.removeItem('openai_default_model');
    toast.success('已清除默认模型');
  };

  const saveChatSettings = () => {
    localStorage.setItem('openai_system_prompt', openaiChatSystemPrompt);
    localStorage.setItem('openai_chat_settings', JSON.stringify(openaiChatSettings));
    setShowHChatSettingsModal(false);
    toast.success('对话设置已保存');
  };

  const saveAutoTitleSettings = (enabled, models) => {
    localStorage.setItem('openai_auto_title_enabled', enabled ? 'true' : 'false');
    localStorage.setItem('openai_title_models', JSON.stringify(models));
  };

  const addTitleModel = () => {
    if (!openaiTitleModelToAdd) return;
    if (!openaiTitleModels.includes(openaiTitleModelToAdd)) {
      const next = [...openaiTitleModels, openaiTitleModelToAdd];
      setOpenaiTitleModels(next);
      saveAutoTitleSettings(openaiAutoTitleEnabled, next);
    }
    setOpenaiTitleModelToAdd('');
  };

  const removeTitleModel = modelId => {
    const next = openaiTitleModels.filter(m => m !== modelId);
    setOpenaiTitleModels(next);
    saveAutoTitleSettings(openaiAutoTitleEnabled, next);
  };

  // Helper title models filtering
  const filteredTitleModelOptions = () => {
    const allModelsMap = new Map();
    allModels.forEach(m => allModelsMap.set(m.id, m));
    endpoints.forEach(ep => {
      if (ep.models) {
        ep.models.forEach(m => {
          const id = typeof m === 'string' ? m : m.id;
          if (id && !allModelsMap.has(id)) {
            allModelsMap.set(id, { id, owned_by: ep.name || 'custom' });
          }
        });
      }
    });
    return Array.from(allModelsMap.values()).filter(m => !openaiTitleModels.includes(m.id));
  };

  // ==================== 4. Personas State ====================
  const [personas, setPersonas] = useState([]);
  const [currentPersonaId, setCurrentPersonaId] = useState(null);
  const [showPersonaDropdown, setShowPersonaDropdown] = useState(false);
  const [personaModalOpen, setPersonaModalOpen] = useState(false);
  const [editingPersona, setEditingPersona] = useState(null);
  const [personaForm, setPersonaForm] = useState({ name: '', icon: 'fa-robot', systemPrompt: '' });

  const fetchPersonas = useCallback(async () => {
    try {
      const response = await fetch('/api/openai/personas', { headers: getAuthHeaders() });
      if (response.ok) {
        const data = await response.json();
        setPersonas(data || []);
        if (data && data.length > 0 && !currentPersonaId) {
          setCurrentPersonaId(data[0].id);
          setOpenaiChatSystemPrompt(data[0].system_prompt);
        }
      }
    } catch (e) {
      console.error('Failed to fetch personas:', e);
    }
  }, [getAuthHeaders, currentPersonaId]);

  useEffect(() => {
    if (activeTab === 'chat') {
      fetchPersonas();
    }
  }, [activeTab, fetchPersonas]);

  const handleSelectPersona = personaId => {
    setCurrentPersonaId(personaId);
    setShowPersonaDropdown(false);
    const p = personas.find(item => item.id === personaId);
    if (p) {
      setOpenaiChatSystemPrompt(p.system_prompt);
      toast.success(`切换人设为: ${p.name}`);
    }
  };

  const openPersonaModal = (persona = null) => {
    setEditingPersona(persona);
    if (persona) {
      setPersonaForm({
        name: persona.name || '',
        icon: persona.icon || 'fa-robot',
        systemPrompt: persona.system_prompt || '',
      });
    } else {
      setPersonaForm({ name: '', icon: 'fa-robot', systemPrompt: '' });
    }
    setPersonaModalOpen(true);
  };

  const savePersona = async () => {
    if (!personaForm.name.trim() || !personaForm.systemPrompt.trim()) {
      toast.warning('请输入名称和提示词');
      return;
    }
    try {
      const id = editingPersona?.id || `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
      const payload = {
        id,
        name: personaForm.name,
        icon: personaForm.icon,
        system_prompt: personaForm.systemPrompt,
      };

      const response = await fetch(
        editingPersona ? `/api/openai/personas/${editingPersona.id}` : '/api/openai/personas',
        {
          method: editingPersona ? 'PUT' : 'POST',
          headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        }
      );

      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }

      await fetchPersonas();
      if (!currentPersonaId) {
        setCurrentPersonaId(id);
        setOpenaiChatSystemPrompt(personaForm.systemPrompt);
      }
      toast.success(editingPersona ? '人设已更新' : '人设已创建');
      setPersonaModalOpen(false);
    } catch (e) {
      toast.error('保存失败: ' + e.message);
    }
  };

  const deletePersona = async personaId => {
    if (!(await dialog.confirm('确定要删除这个 AI 人设吗？'))) {
      return;
    }
    try {
      const persona = personas.find(item => item.id === personaId);
      if (persona?.is_default) {
        toast.warning('无法删除默认人设');
        return;
      }
      const response = await fetch(`/api/openai/personas/${personaId}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      if (!response.ok) {
        throw new Error(`HTTP ${response.status}`);
      }
      await fetchPersonas();
      if (currentPersonaId === personaId) {
        const fallback = personas.find(item => item.is_default === 1) || {
          id: '1',
          system_prompt: '你是一个有用的 AI 助手。',
        };
        setCurrentPersonaId(fallback.id);
        setOpenaiChatSystemPrompt(fallback.system_prompt);
      }
      toast.success('人设已删除');
    } catch (e) {
      toast.error('删除失败: ' + e.message);
    }
  };

  // ==================== 5. Chat History & Streaming ====================
  const [sessions, setSessions] = useState([]);
  const [currentSessionId, setCurrentSessionId] = useState(null);
  const [messages, setMessages] = useState([]);
  const [messageInput, setMessageInput] = useState('');
  const [attachments, setAttachments] = useState([]);
  const [chatLoading, setChatLoading] = useState(false);
  const [chatHistoryLoading, setChatHistoryLoading] = useState(false);
  const [selectedSessionIds, setSelectedSessionIds] = useState([]);
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
  const [chatHistoryCollapsed, setChatHistoryCollapsed] = useState(false);

  const abortControllerRef = useRef(null);
  const messagesEndRef = useRef(null);

  const fetchSessions = useCallback(async () => {
    try {
      const response = await fetch('/api/openai/sessions', { headers: getAuthHeaders() });
      if (response.ok) {
        const data = await response.json();
        setSessions(data || []);
      }
    } catch (e) {
      console.error('Failed to fetch sessions:', e);
    }
  }, [getAuthHeaders]);

  const fetchMessages = useCallback(
    async sessionId => {
      if (!sessionId) {
        setMessages([]);
        return;
      }
      setChatHistoryLoading(true);
      try {
        const response = await fetch(`/api/openai/sessions/${sessionId}/messages`, {
          headers: getAuthHeaders(),
        });
        if (response.ok) {
          const data = await response.json();
          setMessages(data || []);
        }
      } catch (e) {
        console.error('Failed to fetch messages:', e);
        toast.error('加载消息失败');
      } finally {
        setChatHistoryLoading(false);
      }
    },
    [getAuthHeaders]
  );

  useEffect(() => {
    if (activeTab === 'chat') {
      fetchSessions();
    }
  }, [activeTab, fetchSessions]);

  // One-time data migration from localStorage to backend SQLite
  useEffect(() => {
    const migrateData = async () => {
      try {
        const legacyPersonas = localStorage.getItem('openai_chat_personas_v2');
        const legacySessions = localStorage.getItem('openai_chat_sessions_v2');
        const legacyMessages = localStorage.getItem('openai_chat_messages_v2');

        if (legacyPersonas) {
          const parsedPersonas = JSON.parse(legacyPersonas);
          for (const p of parsedPersonas) {
            if (String(p.id) === '1') continue; // Skip default
            await fetch('/api/openai/personas', {
              method: 'POST',
              headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
              body: JSON.stringify({
                id: String(p.id),
                name: p.name,
                icon: p.icon,
                system_prompt: p.system_prompt,
              }),
            });
          }
          localStorage.removeItem('openai_chat_personas_v2');
        }

        if (legacySessions) {
          const parsedSessions = JSON.parse(legacySessions);
          const parsedMessages = legacyMessages ? JSON.parse(legacyMessages) : {};

          for (const s of parsedSessions) {
            await fetch('/api/openai/sessions', {
              method: 'POST',
              headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
              body: JSON.stringify({
                id: String(s.id),
                title: s.title,
                model: s.model,
                endpoint_id: s.endpoint_id,
                persona_id: String(s.persona_id),
                system_prompt: s.system_prompt,
              }),
            });

            const msgs = parsedMessages[s.id] || [];
            for (const m of msgs) {
              await fetch(`/api/openai/sessions/${s.id}/messages`, {
                method: 'POST',
                headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
                body: JSON.stringify({
                  id: m.id || `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`,
                  role: m.role,
                  content: typeof m.content === 'string' ? m.content : JSON.stringify(m.content),
                  reasoning: m.reasoning || '',
                  timestamp: m.timestamp,
                }),
              });
            }
          }
          localStorage.removeItem('openai_chat_sessions_v2');
          if (legacyMessages) {
            localStorage.removeItem('openai_chat_messages_v2');
          }
        }

        if (activeTab === 'chat') {
          await fetchPersonas();
          await fetchSessions();
        }
      } catch (err) {
        console.error('Data migration error:', err);
      }
    };

    migrateData();
  }, [activeTab, fetchPersonas, fetchSessions, getAuthHeaders]);

  const loadSession = async sessionId => {
    if (chatLoading) return;
    setCurrentSessionId(sessionId);
    await fetchMessages(sessionId);

    const session = sessions.find(s => s.id === sessionId);
    if (session) {
      if (session.model) {
        setChatModel(session.model);
        localStorage.setItem('openai_chat_model', session.model);
      }
      if (session.endpoint_id) {
        setChatEndpoint(session.endpoint_id);
        localStorage.setItem('openai_chat_endpoint', session.endpoint_id);
      }
      if (session.persona_id) {
        setCurrentPersonaId(session.persona_id);
        const p = personas.find(item => item.id === session.persona_id);
        if (p) setOpenaiChatSystemPrompt(p.system_prompt);
      }
    }
    setMobileSidebarOpen(false);
  };

  const createSession = async (resetToDefault = false) => {
    try {
      const globalSystemPrompt =
        localStorage.getItem('openai_system_prompt') || '你是一个有用的 AI 助手。';
      let finalModel = chatModel;
      if (defaultChatModel && (resetToDefault || !chatModel)) {
        finalModel = defaultChatModel;
        setChatModel(finalModel);
        localStorage.setItem('openai_chat_model', finalModel);
      }

      const currentPersona = personas.find(p => p.id === currentPersonaId);
      const systemPrompt = currentPersona ? currentPersona.system_prompt : globalSystemPrompt;
      const id = `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;

      const response = await fetch('/api/openai/sessions', {
        method: 'POST',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({
          id,
          title: '新对话',
          model: finalModel,
          endpoint_id: chatEndpoint || '',
          persona_id: currentPersonaId || '1',
          system_prompt: systemPrompt,
        }),
      });

      if (!response.ok) throw new Error(`HTTP ${response.status}`);

      await fetchSessions();
      setCurrentSessionId(id);
      setMessages([]);
      toast.success('新建会话成功');
    } catch (error) {
      toast.error('创建会话失败: ' + error.message);
    }
  };

  const deleteSession = async (sessionId, e) => {
    if (e) e.stopPropagation();
    if (!(await dialog.confirm('确定要删除这个对话吗？此操作不可撤销。'))) return;
    try {
      const response = await fetch(`/api/openai/sessions/${sessionId}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      await fetchSessions();
      if (currentSessionId === sessionId) {
        setCurrentSessionId(null);
        setMessages([]);
      }
      toast.success('会话已删除');
    } catch (error) {
      toast.error('删除会话失败: ' + error.message);
    }
  };

  const deleteSelectedSessions = async () => {
    if (selectedSessionIds.length === 0) return;
    if (!(await dialog.confirm(`确定要删除选中的 ${selectedSessionIds.length} 个对话吗？`))) return;
    try {
      for (const id of selectedSessionIds) {
        await fetch(`/api/openai/sessions/${id}`, {
          method: 'DELETE',
          headers: getAuthHeaders(),
        });
      }
      await fetchSessions();
      if (selectedSessionIds.includes(currentSessionId)) {
        setCurrentSessionId(null);
        setMessages([]);
      }
      setSelectedSessionIds([]);
      toast.success('所选会话已删除');
    } catch (error) {
      toast.error('删除会话失败: ' + error.message);
    }
  };

  const clearAllSessions = async () => {
    if (sessions.length === 0) return;
    if (!(await dialog.confirm('确定要清空所有会话历史吗？此操作不可撤销。'))) return;
    try {
      const response = await fetch('/api/openai/sessions', {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      await fetchSessions();
      setCurrentSessionId(null);
      setMessages([]);
      setSelectedSessionIds([]);
      toast.success('所有会话已清空');
    } catch (error) {
      toast.error('清空会话失败: ' + error.message);
    }
  };

  const toggleSessionSelection = (sessionId, e) => {
    if (e) e.stopPropagation();
    setSelectedSessionIds(prev =>
      prev.includes(sessionId) ? prev.filter(id => id !== sessionId) : [...prev, sessionId]
    );
  };

  const toggleSelectAllSessions = () => {
    if (selectedSessionIds.length === sessions.length) {
      setSelectedSessionIds([]);
    } else {
      setSelectedSessionIds(sessions.map(s => s.id));
    }
  };

  // Generate Title
  const generateTitleWithFallback = async messagesList => {
    const modelsToTry = openaiTitleModels.length > 0 ? [...openaiTitleModels] : [chatModel];
    const conversationText = messagesList
      .slice(0, 4)
      .map(msg => {
        const role = msg.role === 'user' ? '用户' : '助手';
        let text = '';
        if (typeof msg.content === 'string') {
          text = msg.content;
        } else if (Array.isArray(msg.content)) {
          const textParts = msg.content.filter(p => p.type === 'text').map(p => p.text);
          text = textParts.join(' ') || '[图片]';
        }
        return `${role}: ${text.slice(0, 200)}`;
      })
      .join('\n');

    const titlePrompt = `请根据以下对话内容，生成一个简洁的中文标题（最多15个字，不要使用标点符号，直接输出标题内容）：\n\n${conversationText}\n\n标题：`;

    for (const modelId of modelsToTry) {
      try {
        const headers = { ...getAuthHeaders(), 'Content-Type': 'application/json' };
        const endpoint = endpoints.find(
          ep => ep.models && ep.models.some(m => (typeof m === 'string' ? m : m.id) === modelId)
        );
        if (endpoint) {
          headers['x-endpoint-id'] = endpoint.id;
        }

        const response = await fetch('/api/openai/v1/chat/completions', {
          method: 'POST',
          headers,
          body: JSON.stringify({
            model: modelId,
            messages: [{ role: 'user', content: titlePrompt }],
            max_tokens: 30,
            temperature: 0.7,
          }),
        });

        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const result = await response.json();
        let generatedTitle = result.choices?.[0]?.message?.content?.trim() || '';

        if (!generatedTitle && result.choices?.[0]?.message?.reasoning_content) {
          const reasoning = result.choices[0].message.reasoning_content.trim();
          const lines = reasoning.split('\n').filter(l => l.trim());
          if (lines.length > 0) generatedTitle = lines[lines.length - 1].trim();
        }

        if (generatedTitle) {
          return { success: true, title: generatedTitle, model: modelId };
        }
      } catch (e) {
        console.warn(`Generate title with model ${modelId} failed:`, e);
      }
    }
    throw new Error('All models failed to generate title');
  };

  const updateSession = useCallback(
    async (sessionId, patch) => {
      try {
        const response = await fetch(`/api/openai/sessions/${sessionId}`, {
          method: 'PUT',
          headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
          body: JSON.stringify(patch),
        });
        if (response.ok) {
          setSessions(prev =>
            prev.map(session =>
              session.id === sessionId
                ? { ...session, ...patch, updated_at: new Date().toISOString() }
                : session
            )
          );
        }
      } catch (e) {
        console.error('Failed to update session:', e);
      }
    },
    [getAuthHeaders]
  );

  const generateChatTitle = async (currentMsgs, sessionId) => {
    if (!sessionId || currentMsgs.length < 2) return;
    const session = sessions.find(s => s.id === sessionId);
    if (!session || session.title !== '新对话') return;

    if (!openaiAutoTitleEnabled) {
      const firstUser = currentMsgs.find(m => m.role === 'user');
      if (firstUser) {
        let simpleTitle = typeof firstUser.content === 'string' ? firstUser.content : '📷 图片对话';
        simpleTitle = simpleTitle.slice(0, 18) + (simpleTitle.length > 18 ? '...' : '');
        updateSession(sessionId, { title: simpleTitle });
      }
      return;
    }

    try {
      const result = await generateTitleWithFallback(currentMsgs);
      if (result.success) {
        updateSession(sessionId, {
          title: result.title,
          model: chatModel,
          endpoint_id: chatEndpoint || '',
          system_prompt: openaiChatSystemPrompt,
        });
      }
    } catch (error) {
      const firstUser = currentMsgs.find(m => m.role === 'user');
      if (firstUser) {
        let fallbackTitle =
          typeof firstUser.content === 'string' ? firstUser.content : '📷 图片对话';
        fallbackTitle = fallbackTitle.slice(0, 18) + (fallbackTitle.length > 18 ? '...' : '');
        updateSession(sessionId, { title: fallbackTitle });
      }
    }
  };

  const testTitleGeneration = async () => {
    setOpenaiTitleGenerating(true);
    setOpenaiTitleLastResult(null);
    const testMessages = [
      { role: 'user', content: '帮我解释一下什么是机器学习' },
      { role: 'assistant', content: '机器学习是人工智能的一个分支，它使计算机能够从数据中学习...' },
    ];
    try {
      const result = await generateTitleWithFallback(testMessages);
      setOpenaiTitleLastResult(result);
    } catch (e) {
      setOpenaiTitleLastResult({ success: false, error: e.message });
    } finally {
      setOpenaiTitleGenerating(false);
    }
  };

  // Chat message sending / streaming API
  const saveChatMessage = async (sessionId, role, content, reasoning = null) => {
    if (!sessionId) return null;
    const id = `${Date.now()}-${Math.random().toString(36).slice(2, 10)}`;
    const message = {
      id,
      role,
      content,
      reasoning: reasoning || '',
      timestamp: new Date().toISOString(),
    };
    try {
      const response = await fetch(`/api/openai/sessions/${sessionId}/messages`, {
        method: 'POST',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify(message),
      });
      if (response.ok) {
        await updateSession(sessionId, { model: chatModel, endpoint_id: chatEndpoint || '' });
        return message;
      }
    } catch (e) {
      console.error('Failed to save message:', e);
    }
    return null;
  };

  const stopGenerating = () => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      abortControllerRef.current = null;
    }
    setChatLoading(false);
  };

  const scrollToBottom = (behavior = 'smooth') => {
    if (messagesEndRef.current) {
      messagesEndRef.current.scrollIntoView({ behavior });
    }
  };

  useEffect(() => {
    scrollToBottom();
  }, [messages, chatLoading]);

  const handleSendChat = async () => {
    if ((!messageInput.trim() && attachments.length === 0) || chatLoading) return;

    const userText = messageInput;
    const currentAttachments = [...attachments];
    setMessageInput('');
    setAttachments([]);

    let activeSessionId = currentSessionId;
    if (!activeSessionId) {
      // Create session first
      try {
        const session = {
          id: chatStorage.newId(),
          title: '新对话',
          model: chatModel,
          endpoint_id: chatEndpoint || '',
          persona_id: currentPersonaId,
          system_prompt: openaiChatSystemPrompt,
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        };
        persistSessions(prev => [session, ...prev]);
        activeSessionId = session.id;
        setCurrentSessionId(activeSessionId);
        chatStorage.saveSessionMessages(activeSessionId, []);
      } catch (err) {
        toast.error('创建会话失败');
        return;
      }
    }

    let userContent;
    if (currentAttachments.length > 0) {
      userContent = [{ type: 'text', text: userText }];
      currentAttachments.forEach(att => {
        userContent.push({
          type: 'image_url',
          image_url: { url: att.url },
        });
      });
    } else {
      userContent = userText;
    }

    const contentToSave =
      typeof userContent === 'string' ? userContent : JSON.stringify(userContent);
    const userMsg = {
      role: 'user',
      content: userContent,
      timestamp: new Date().toISOString(),
      isNew: true,
    };

    setMessages(prev => [...prev, userMsg]);
    setChatLoading(true);

    // Save user message
    saveChatMessage(activeSessionId, 'user', contentToSave).then(saved => {
      if (saved && saved.id) {
        userMsg.id = saved.id;
      }
    });

    abortControllerRef.current = new AbortController();

    try {
      const messagesPayload = [
        { role: 'system', content: openaiChatSystemPrompt },
        ...messages.map(m => ({
          role: m.role,
          content: typeof m.content === 'string' ? m.content : JSON.stringify(m.content),
        })),
        { role: 'user', content: contentToSave },
      ];

      const headers = {
        ...getAuthHeaders(),
        'Content-Type': 'application/json',
      };

      let targetEpId = chatEndpoint;
      if (!targetEpId && chatModel) {
        const found = endpoints.find(
          ep => ep.models && ep.models.some(m => (typeof m === 'string' ? m : m.id) === chatModel)
        );
        if (found) targetEpId = found.id;
      }
      if (targetEpId) {
        headers['x-endpoint-id'] = targetEpId;
      }

      const response = await fetch('/api/openai/v1/chat/completions', {
        method: 'POST',
        headers,
        body: JSON.stringify({
          model: chatModel,
          messages: messagesPayload,
          stream: true,
          ...openaiChatSettings,
        }),
        signal: abortControllerRef.current?.signal,
      });

      if (!response.ok) {
        let errText = `HTTP 错误 ${response.status}`;
        try {
          const json = await response.json();
          errText = json.error?.message || json.message || JSON.stringify(json);
        } catch {}
        throw new Error(errText);
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let assistantMsg = {
        role: 'assistant',
        content: '',
        reasoning: '',
        showReasoning: true,
        timestamp: new Date().toISOString(),
        model: chatModel,
        isNew: true,
      };

      setMessages(prev => [...prev, assistantMsg]);

      let buffer = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
          const trimmed = line.trim();
          if (trimmed.startsWith('data:')) {
            const dataStr = trimmed.slice(5).trim();
            if (dataStr === '[DONE]') break;
            try {
              const parsed = JSON.parse(dataStr);
              const delta = parsed.choices?.[0]?.delta;
              if (delta) {
                if (delta.reasoning_content) {
                  assistantMsg.reasoning += delta.reasoning_content;
                }
                if (delta.content) {
                  assistantMsg.content += delta.content;
                }
                setMessages(prev =>
                  prev.map((m, idx) => (idx === prev.length - 1 ? { ...assistantMsg } : m))
                );
              }
            } catch (e) {}
          }
        }
      }

      // Save assistant message to DB
      const saved = await saveChatMessage(
        activeSessionId,
        'assistant',
        assistantMsg.content,
        assistantMsg.reasoning || null
      );
      if (saved && saved.id) {
        assistantMsg.id = saved.id;
        setMessages(prev =>
          prev.map((m, idx) => (idx === prev.length - 1 ? { ...m, id: saved.id } : m))
        );
      }

      // Check auto title
      const sess = sessions.find(s => s.id === activeSessionId);
      if (sess && sess.title === '新对话') {
        const currentMsgs = [...messages, userMsg, assistantMsg];
        generateChatTitle(currentMsgs, activeSessionId);
      }
    } catch (error) {
      if (error.name === 'AbortError') return;
      toast.error('对话失败: ' + error.message);
      setMessages(prev => [
        ...prev,
        {
          role: 'assistant',
          content: `**??**: ${error.message}`,
          timestamp: new Date().toISOString(),
        },
      ]);
    } finally {
      setChatLoading(false);
      abortControllerRef.current = null;
    }
  };

  const deleteChatMessage = async index => {
    if (index < 0 || index >= messages.length) return;
    const msg = messages[index];
    if (msg && msg.id && currentSessionId) {
      try {
        await fetch(`/api/openai/sessions/${currentSessionId}/messages/${msg.id}`, {
          method: 'DELETE',
          headers: getAuthHeaders(),
        });
      } catch (e) {
        console.error('Failed to delete message from backend:', e);
      }
    }
    setMessages(prev => prev.filter((_, idx) => idx !== index));
  };

  const regenerateChat = async (index = -1) => {
    if (chatLoading || messages.length === 0) return;
    let targetIndex = index;
    if (targetIndex === -1) {
      for (let i = messages.length - 1; i >= 0; i--) {
        if (messages[i].role === 'assistant') {
          targetIndex = i;
          break;
        }
      }
    }
    if (targetIndex === -1) {
      targetIndex = messages.length - 1;
    }

    const targetMsg = messages[targetIndex];
    if (!targetMsg) return;

    const deleteCount =
      messages.length - (targetMsg.role === 'assistant' ? targetIndex : targetIndex + 1);
    const msgsToKeep = messages.slice(0, messages.length - deleteCount);
    const msgsToDelete = messages.slice(messages.length - deleteCount);
    for (const m of msgsToDelete) {
      if (m.id && currentSessionId) {
        try {
          await fetch(`/api/openai/sessions/${currentSessionId}/messages/${m.id}`, {
            method: 'DELETE',
            headers: getAuthHeaders(),
          });
        } catch (e) {
          console.error('Failed to delete message:', e);
        }
      }
    }

    setMessages(msgsToKeep);
    setChatLoading(true);
    abortControllerRef.current = new AbortController();

    try {
      const messagesPayload = [
        { role: 'system', content: openaiChatSystemPrompt },
        ...msgsToKeep.map(m => ({
          role: m.role,
          content: typeof m.content === 'string' ? m.content : JSON.stringify(m.content),
        })),
      ];

      const headers = { ...getAuthHeaders(), 'Content-Type': 'application/json' };
      if (chatEndpoint) {
        headers['x-endpoint-id'] = chatEndpoint;
      }

      const response = await fetch('/api/openai/v1/chat/completions', {
        method: 'POST',
        headers,
        body: JSON.stringify({
          model: chatModel,
          messages: messagesPayload,
          stream: true,
          ...openaiChatSettings,
        }),
        signal: abortControllerRef.current?.signal,
      });

      if (!response.ok) throw new Error(`HTTP ${response.status}`);

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      let assistantMsg = {
        role: 'assistant',
        content: '',
        reasoning: '',
        showReasoning: true,
        timestamp: new Date().toISOString(),
        model: chatModel,
        isNew: true,
      };

      setMessages(prev => [...prev, assistantMsg]);

      let buffer = '';
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
          const trimmed = line.trim();
          if (trimmed.startsWith('data:')) {
            const dataStr = trimmed.slice(5).trim();
            if (dataStr === '[DONE]') break;
            try {
              const parsed = JSON.parse(dataStr);
              const delta = parsed.choices?.[0]?.delta;
              if (delta) {
                if (delta.reasoning_content) {
                  assistantMsg.reasoning += delta.reasoning_content;
                }
                if (delta.content) {
                  assistantMsg.content += delta.content;
                }
                setMessages(prev =>
                  prev.map((m, idx) => (idx === prev.length - 1 ? { ...assistantMsg } : m))
                );
              }
            } catch (e) {}
          }
        }
      }

      const saved = await saveChatMessage(
        currentSessionId,
        'assistant',
        assistantMsg.content,
        assistantMsg.reasoning || null
      );
      if (saved && saved.id) {
        setMessages(prev =>
          prev.map((m, idx) => (idx === prev.length - 1 ? { ...m, id: saved.id } : m))
        );
      }
    } catch (error) {
      if (error.name === 'AbortError') return;
      toast.error('重新生成失败: ' + error.message);
    } finally {
      setChatLoading(false);
      abortControllerRef.current = null;
    }
  };

  const clearChatLocal = async () => {
    if (currentSessionId) {
      try {
        const response = await fetch(`/api/openai/sessions/${currentSessionId}/messages`, {
          method: 'DELETE',
          headers: getAuthHeaders(),
        });
        if (response.ok) {
          setMessages([]);
          toast.success('已清空当前对话消息');
        }
      } catch (e) {
        console.error('Failed to clear messages:', e);
        toast.error('清空消息失败');
      }
    } else {
      setMessages([]);
    }
  };

  // Image Upload handler
  const fileInputRef = useRef(null);
  const handleFileChange = e => {
    const files = Array.from(e.target.files);
    files.forEach(file => {
      if (!file.type.startsWith('image/')) return;
      const reader = new FileReader();
      reader.onload = event => {
        setAttachments(prev => [...prev, { file, url: event.target.result }]);
      };
      reader.readAsDataURL(file);
    });
  };

  const removeAttachment = idx => {
    setAttachments(prev => prev.filter((_, i) => i !== idx));
  };

  // Paste handler for images
  const handlePaste = e => {
    const items = e.clipboardData?.items;
    if (!items) return;
    for (const item of items) {
      if (item.type.startsWith('image/')) {
        const file = item.getAsFile();
        const reader = new FileReader();
        reader.onload = event => {
          setAttachments(prev => [...prev, { file, url: event.target.result }]);
        };
        reader.readAsDataURL(file);
      }
    }
  };

  // Model Selector Filtering
  const filteredModelsList = useMemo(() => {
    const list = allModels.filter(m => {
      const matchesSearch = m.id.toLowerCase().includes(openaiModelSearch.toLowerCase());
      const matchesEndpoint =
        !openaiSelectedEndpointId ||
        m.owned_by === endpoints.find(e => e.id === openaiSelectedEndpointId)?.name;
      const matchesHidden = openaiShowHiddenModels ? true : !hiddenModels.includes(m.id);
      return matchesSearch && matchesEndpoint && matchesHidden;
    });
    return list;
  }, [
    allModels,
    openaiModelSearch,
    openaiSelectedEndpointId,
    endpoints,
    hiddenModels,
    openaiShowHiddenModels,
  ]);

  const chatDropdownFilteredModels = useMemo(() => {
    const allModelsMap = new Map();
    // Gather all models
    allModels.forEach(m => allModelsMap.set(m.id, m));
    // Complement with models from enabled endpoints
    endpoints.forEach(ep => {
      if (ep.models) {
        ep.models.forEach(m => {
          const id = typeof m === 'string' ? m : m.id;
          if (id && !allModelsMap.has(id)) {
            allModelsMap.set(id, { id, owned_by: ep.name || 'custom' });
          }
        });
      }
    });

    const fullList = Array.from(allModelsMap.values());
    return fullList.filter(m => {
      const matchesSearch = m.id.toLowerCase().includes(dropdownModelSearch.toLowerCase());
      // Filter by active endpoint
      const matchesEndpoint =
        !chatEndpoint || m.owned_by === endpoints.find(e => e.id === chatEndpoint)?.name;
      const isHidden = hiddenModels.includes(m.id);
      return matchesSearch && matchesEndpoint && !isHidden;
    });
  }, [allModels, endpoints, chatEndpoint, dropdownModelSearch, hiddenModels]);

  const selectChatModel = modelId => {
    setChatModel(modelId);
    localStorage.setItem('openai_chat_model', modelId);
    setShowModelDropdown(false);
  };

  const selectEndpoint = epId => {
    setChatEndpoint(epId);
    if (epId) {
      localStorage.setItem('openai_chat_endpoint', epId);
    } else {
      localStorage.removeItem('openai_chat_endpoint');
    }
    setShowEndpointDropdown(false);
  };

  // Auto-resize chat textarea
  const textareaRef = useRef(null);
  const handleTextareaInput = () => {
    if (textareaRef.current) {
      textareaRef.current.style.height = 'auto';
      textareaRef.current.style.height = `${Math.min(200, textareaRef.current.scrollHeight)}px`;
    }
  };

  return (
    <PageStack>
      {/* Tab Navigation */}
      <PageToolbar className="select-none">
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={[
            {
              value: 'endpoints',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Server className="w-3.5 h-3.5" />
                  API 端点
                </span>
              ),
            },
            {
              value: 'accounts',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Users className="w-3.5 h-3.5" />
                  账号管理
                </span>
              ),
            },
            {
              value: 'analytics',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Activity className="w-3.5 h-3.5" />
                  网关分析
                </span>
              ),
            },
          ]}
        />
      </PageToolbar>

      {/* ==================== 1. API 端点 Tab ==================== */}
      {activeTab === 'endpoints' && (
        <div className="space-y-3">
          <SectionHeader
            title="API 端点"
            description={
              modelHealthBatchLoading
                ? '正在批量检测模型可用性...'
                : `共 ${endpoints.length} 个端点`
            }
            action={
              <div className="flex gap-2">
                <Button
                  size="sm"
                  onClick={() => setHealthCheckModal(true)}
                  className="flex items-center gap-1.5"
                >
                  <Activity className={iconButtonIconClass} />
                  <span>健康检测</span>
                </Button>
                <Button
                  size="sm"
                  onClick={refreshAllEndpoints}
                  disabled={endpointsRefreshing}
                  className="flex items-center gap-1.5"
                >
                  <RefreshCw
                    className={cx(iconButtonIconClass, endpointsRefreshing && 'animate-spin')}
                  />
                  <span>刷新列表</span>
                </Button>
              </div>
            }
          />

          <div className="space-y-2.5">
            {endpointsLoading ? (
              <div className="space-y-2.5">
                {[...Array(2)].map((_, i) => (
                  <AppCard key={i} padding="md" className="space-y-2.5">
                    <div className="flex items-center gap-3">
                      <SkeletonLine className="w-10 h-10 rounded-lg" />
                      <div className="flex-1 space-y-1.5">
                        <SkeletonLine className="w-1/4 h-3.5" />
                        <SkeletonLine className="w-1/2 h-2.5" />
                      </div>
                    </div>
                  </AppCard>
                ))}
              </div>
            ) : endpoints.length === 0 ? (
              <EmptyState
                icon={Bot}
                title="暂无 API 端点"
                description="请先在账号管理中添加您的 OpenAI 兼容 API 端点"
              />
            ) : (
              endpoints.map(endpoint => {
                const isExpanded = !!expandedEndpoints[endpoint.id];
                const validStatus = endpoint.status === 'valid';
                const invalidStatus = endpoint.status === 'invalid';

                return (
                  <AppCard key={endpoint.id} padding="none" className="overflow-hidden">
                    {/* Header */}
                    <div
                      onClick={() => toggleEndpointExpand(endpoint.id)}
                      className="flex items-center justify-between px-4 py-3 cursor-pointer hover:bg-kumo-recessed/10 transition-colors"
                    >
                      <div className="flex min-w-0 items-center gap-3">
                        <ChevronDown
                          className={cx(
                            iconButtonIconClass,
                            'text-kumo-subtle transition-transform duration-200',
                            isExpanded && 'transform rotate-180'
                          )}
                        />
                        <div className="w-10 h-10 rounded-lg flex items-center justify-center font-bold text-kumo-inverse text-sm bg-kumo-brand">
                          {(endpoint.name || 'A').charAt(0).toUpperCase()}
                        </div>
                        <div className="min-w-0">
                          <div className="flex min-w-0 items-center gap-2">
                            <InlineStatusPill
                              tone={validStatus ? 'success' : invalidStatus ? 'danger' : 'neutral'}
                            >
                              {validStatus ? '有效' : invalidStatus ? '无效' : '未验证'}
                            </InlineStatusPill>
                            <span className="min-w-0 truncate font-semibold text-kumo-strong text-xs">
                              {endpoint.name || '未命名端点'}
                            </span>
                          </div>
                          <span className="block mt-0.5 truncate text-[10px] text-kumo-subtle font-mono">
                            {maskAddress(endpoint.baseUrl)}
                          </span>
                        </div>
                      </div>

                      <div className="flex items-center gap-2">
                        <Button
                          shape="square"
                          size="sm"
                          variant="ghost"
                          aria-label="模型健康检测"
                          onClick={e => {
                            e.stopPropagation();
                            openHealthCheckForEndpoint(endpoint.id);
                          }}
                          className="text-kumo-subtle hover:text-kumo-strong"
                          title="模型健康检测"
                        >
                          <Activity className={actionIconClass} />
                        </Button>
                        <Button
                          shape="square"
                          size="sm"
                          variant="ghost"
                          aria-label="刷新模型列表"
                          onClick={e => {
                            e.stopPropagation();
                            refreshEndpointModels(endpoint);
                          }}
                          className="text-kumo-subtle hover:text-kumo-strong"
                          title="刷新模型列表"
                        >
                          <RefreshCw
                            className={cx(actionIconClass, endpoint.refreshing && 'animate-spin')}
                          />
                        </Button>
                        <InlineStatusPill tone="neutral">
                          模型: {endpoint.models ? endpoint.models.length : 0}
                        </InlineStatusPill>
                      </div>
                    </div>

                    {/* Expandable Model Tags */}
                    <AnimatedCollapse open={isExpanded}>
                      <div className="border-t border-kumo-line bg-kumo-recessed/10 px-4 py-3">
                        {endpoint.models && endpoint.models.length > 0 ? (
                          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 md:grid-cols-3">
                            {endpoint.models.map(model => {
                              const modelId =
                                typeof model === 'string' ? model.trim() : (model.id || '').trim();
                              const health = openaiModelHealth[modelId];

                              return (
                                <AppCard
                                  key={modelId}
                                  padding="sm"
                                  className="flex items-center justify-between text-xs group"
                                >
                                  <div className="flex items-center gap-2 min-w-0">
                                    {/* Health indicator dot */}
                                    <div
                                      onClick={() => testModelHealth({ id: modelId }, endpoint.id)}
                                      className={`w-2 h-2 rounded-full cursor-pointer ${
                                        health?.loading
                                          ? 'bg-kumo-brand animate-pulse'
                                          : health?.status === 'healthy'
                                            ? 'bg-kumo-success'
                                            : health?.status === 'error'
                                              ? 'bg-kumo-danger'
                                              : 'bg-kumo-subtle'
                                      }`}
                                      title={
                                        health
                                          ? `检测时间: ${formatDateTime(health.checkedAt)}\n延迟: ${health.latency || '-'}ms`
                                          : '点击测试模型健康'
                                      }
                                    />
                                    <span
                                      onClick={() => {
                                        selectEndpoint(endpoint.id);
                                        selectChatModel(modelId);
                                        setActiveTab('chat');
                                      }}
                                      className="font-mono text-[11px] text-kumo-strong hover:text-kumo-brand cursor-pointer truncate"
                                      title="点击进入对话"
                                    >
                                      {modelId}
                                    </span>
                                  </div>
                                  <Button
                                    shape="square"
                                    size="sm"
                                    variant="ghost"
                                    aria-label="复制模型名称"
                                    onClick={() => {
                                      navigator.clipboard.writeText(modelId);
                                      toast.success('已复制模型名称');
                                    }}
                                    className="text-kumo-subtle hover:text-kumo-strong opacity-0 group-hover:opacity-100 transition-opacity"
                                    title="复制名称"
                                  >
                                    <Copy className="w-3.5 h-3.5" />
                                  </Button>
                                </AppCard>
                              );
                            })}
                          </div>
                        ) : (
                          <div className="py-4 text-center text-xs text-kumo-subtle">
                            暂无模型数据，可在账号管理中刷新获取
                          </div>
                        )}
                      </div>
                    </AnimatedCollapse>
                  </AppCard>
                );
              })
            )}
          </div>
        </div>
      )}

      {/* ==================== 2. 账号管理 Tab ==================== */}
      {activeTab === 'accounts' && (
        <div className="space-y-4">
          {/* Toolbar */}
          <SectionHeader
            title="API 端点管理"
            description="管理和配置您的 OpenAI 兼容 API 端点"
            action={
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  onClick={() => setHealthCheckModal(true)}
                  className="flex items-center gap-1"
                >
                  <Activity className={iconButtonIconClass} />
                  <span>健康检测</span>
                </Button>
                <Button
                  size="sm"
                  variant="primary"
                  onClick={openAddEndpointModal}
                  className="flex items-center gap-1"
                >
                  <Plus className={iconButtonIconClass} />
                  <span>添加账号</span>
                </Button>
                <Button
                  size="sm"
                  onClick={refreshAllEndpoints}
                  disabled={endpointsRefreshing}
                  className="flex items-center gap-1"
                >
                  <RefreshCw
                    className={cx(iconButtonIconClass, endpointsRefreshing && 'animate-spin')}
                  />
                  <span>刷新全部</span>
                </Button>
                <Button size="sm" onClick={exportEndpoints} className="flex items-center gap-1">
                  <Upload className={iconButtonIconClass} />
                  <span>导出</span>
                </Button>
                <Button size="sm" onClick={importEndpoints} className="flex items-center gap-1">
                  <Download className={iconButtonIconClass} />
                  <span>导入</span>
                </Button>
              </div>
            }
          />

          {/* Table */}
          <DataTableFrame>
            <AppTable widths={colWidths}>
              <Table.Header variant="compact">
                <Table.Row>
                  <Table.Head className="relative group pr-6">
                    名称
                    <Table.ResizeHandle onMouseDown={e => startResize(0, e)} />
                  </Table.Head>
                  <Table.Head className="relative group pr-6">
                    API 地址
                    <Table.ResizeHandle onMouseDown={e => startResize(1, e)} />
                  </Table.Head>
                  <Table.Head className="relative group pr-6">
                    API Key
                    <Table.ResizeHandle onMouseDown={e => startResize(2, e)} />
                  </Table.Head>
                  <Table.Head className="text-center relative group pr-6">
                    状态
                    <Table.ResizeHandle onMouseDown={e => startResize(3, e)} />
                  </Table.Head>
                  <Table.Head className="text-center relative group pr-6">
                    启用
                    <Table.ResizeHandle onMouseDown={e => startResize(4, e)} />
                  </Table.Head>
                  <Table.Head className="text-center relative group pr-6">
                    模型数量
                    <Table.ResizeHandle onMouseDown={e => startResize(5, e)} />
                  </Table.Head>
                  <Table.Head className="text-center">操作</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {endpointsLoading ? (
                  [...Array(3)].map((_, i) => (
                    <Table.Row key={i}>
                      <Table.Cell>
                        <SkeletonLine className="w-20 h-4" />
                      </Table.Cell>
                      <Table.Cell>
                        <SkeletonLine className="w-40 h-4" />
                      </Table.Cell>
                      <Table.Cell>
                        <SkeletonLine className="w-32 h-4" />
                      </Table.Cell>
                      <Table.Cell className="text-center">
                        <SkeletonLine className="w-12 h-4 mx-auto" />
                      </Table.Cell>
                      <Table.Cell className="text-center">
                        <SkeletonLine className="w-8 h-4 mx-auto" />
                      </Table.Cell>
                      <Table.Cell className="text-center">
                        <SkeletonLine className="w-6 h-4 mx-auto" />
                      </Table.Cell>
                      <Table.Cell>
                        <SkeletonLine className="w-24 h-4 mx-auto" />
                      </Table.Cell>
                    </Table.Row>
                  ))
                ) : endpoints.length === 0 ? (
                  <Table.Row>
                    <Table.Cell colSpan={7} className="p-8 text-center text-kumo-subtle">
                      暂无 API 账号，点击上方按钮添加
                    </Table.Cell>
                  </Table.Row>
                ) : (
                  endpoints.map(endpoint => (
                    <Table.Row
                      key={endpoint.id}
                      className="hover:bg-kumo-recessed/5 cursor-pointer"
                      title="双击编辑端点"
                      onDoubleClick={event =>
                        handleEditableRowDoubleClick(event, () => openEditEndpointModal(endpoint))
                      }
                    >
                      <Table.Cell className="font-bold text-kumo-strong">
                        {endpoint.name || '未命名'}
                      </Table.Cell>
                      <Table.Cell className="font-mono">{maskAddress(endpoint.baseUrl)}</Table.Cell>
                      <Table.Cell>
                        <div className="flex items-center gap-1.5 font-mono">
                          <span>
                            {endpoint.showKey ? endpoint.apiKey : maskApiKey(endpoint.apiKey)}
                          </span>
                          <Button
                            shape="square"
                            size="sm"
                            variant="ghost"
                            aria-label={endpoint.showKey ? '隐藏 API Key' : '显示 API Key'}
                            onClick={() =>
                              setEndpoints(prev =>
                                prev.map(e =>
                                  e.id === endpoint.id ? { ...e, showKey: !e.showKey } : e
                                )
                              )
                            }
                            className="text-kumo-subtle"
                          >
                            {endpoint.showKey ? (
                              <EyeOff className="w-3.5 h-3.5" />
                            ) : (
                              <Eye className="w-3.5 h-3.5" />
                            )}
                          </Button>
                        </div>
                      </Table.Cell>
                      <Table.Cell className="text-center">
                        <InlineStatusPill
                          tone={
                            endpoint.status === 'valid'
                              ? 'success'
                              : endpoint.status === 'invalid'
                                ? 'danger'
                                : 'neutral'
                          }
                        >
                          {endpoint.status === 'valid'
                            ? '有效'
                            : endpoint.status === 'invalid'
                              ? '无效'
                              : '未验证'}
                        </InlineStatusPill>
                      </Table.Cell>
                      <Table.Cell className="text-center">
                        <Switch
                          checked={!!endpoint.enabled}
                          onCheckedChange={() => toggleEndpointEnabled(endpoint)}
                          size="sm"
                        />
                      </Table.Cell>
                      <Table.Cell className="text-center text-kumo-strong font-semibold">
                        {endpoint.models ? endpoint.models.length : 0}
                      </Table.Cell>
                      <Table.Cell>
                        <div className="flex justify-center gap-1.5">
                          <Button
                            shape="square"
                            size="sm"
                            variant="ghost"
                            aria-label="验证连接"
                            onClick={() => verifyEndpoint(endpoint)}
                            className="hover:text-kumo-brand text-kumo-subtle"
                            title="验证连接"
                          >
                            <Plug className="w-3.5 h-3.5" />
                          </Button>
                          <Button
                            shape="square"
                            size="sm"
                            variant="ghost"
                            aria-label="健康检测"
                            onClick={() => openHealthCheckForEndpoint(endpoint.id)}
                            className="hover:text-kumo-brand text-kumo-subtle"
                            title="健康检测"
                          >
                            <Activity className="w-3.5 h-3.5" />
                          </Button>
                          <Button
                            shape="square"
                            size="sm"
                            variant="ghost"
                            aria-label="编辑配置"
                            onClick={() => openEditEndpointModal(endpoint)}
                            className="hover:text-kumo-brand text-kumo-subtle"
                            title="编辑配置"
                          >
                            <Edit className="w-3.5 h-3.5" />
                          </Button>
                          <Button
                            shape="square"
                            size="sm"
                            variant="ghost"
                            aria-label="删除端点"
                            onClick={() => deleteEndpoint(endpoint)}
                            className="hover:text-kumo-danger text-kumo-subtle"
                            title="删除账号"
                          >
                            <Trash className="w-3.5 h-3.5" />
                          </Button>
                        </div>
                      </Table.Cell>
                    </Table.Row>
                  ))
                )}
              </Table.Body>
            </AppTable>
          </DataTableFrame>

          {/* Batch add panel */}
          <AppCard padding="lg" className="space-y-4">
            <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-2">
              <Plus className="w-4 h-4 text-kumo-brand" />
              批量添加端点
            </h4>
            <p className="text-[11px] text-kumo-subtle leading-relaxed">
              每行一个端点，格式：<code>名称:API地址:API_Key</code>
            </p>
            <Textarea
              aria-label="批量添加端点"
              value={batchText}
              onChange={e => setBatchText(e.target.value)}
              placeholder="每行一个：名称:https://api.example.com:sk-xxx"
              rows={4}
              className="w-full text-kumo-strong text-xs font-mono p-3 resize-none"
            />
            {batchError && <p className="text-xs text-kumo-danger font-semibold">{batchError}</p>}
            {batchSuccess && (
              <p className="text-xs text-kumo-success font-semibold">{batchSuccess}</p>
            )}
            <Button
              size="sm"
              variant="primary"
              disabled={batchAdding || !batchText.trim()}
              onClick={batchAddEndpoints}
              className="w-full font-semibold"
            >
              {batchAdding ? '添加中...' : '批量添加'}
            </Button>
          </AppCard>
        </div>
      )}

      {/* ==================== 3. 网关分析 Tab ==================== */}
      {activeTab === 'analytics' && (
        <div className="space-y-4">
          {/* Header & Controls */}
          <SectionHeader
            title="网关分析"
            description="API 代理流量与性能多维分析"
            action={
              <div className="flex items-center gap-3">
                <Select
                  size="sm"
                  aria-label="选择分析范围"
                  value={String(analyticsDays)}
                  onValueChange={val => setAnalyticsDays(Number(val))}
                  items={[
                    { value: '1', label: '最近 24 小时' },
                    { value: '7', label: '最近 7 天' },
                    { value: '30', label: '最近 30 天' },
                  ]}
                  className="w-36 text-xs text-kumo-strong"
                />
                <Button
                  size="sm"
                  onClick={fetchAnalytics}
                  disabled={analyticsLoading}
                  className="flex items-center gap-1.5"
                >
                  <RefreshCw className={cx('w-3.5 h-3.5', analyticsLoading && 'animate-spin')} />
                  <span>刷新</span>
                </Button>
              </div>
            }
          />

          {/* Analytics Summary Cards */}
          <div className="grid grid-cols-1 sm:grid-cols-4 gap-4">
            <AppCard padding="md" className="flex flex-col">
              <span className="text-[10px] font-bold text-kumo-subtle uppercase tracking-wider">
                总请求次数
              </span>
              <span className="text-xl font-bold text-kumo-strong mt-1 font-mono">
                {analyticsLoading ? (
                  <SkeletonLine className="w-16 h-5" />
                ) : (
                  analyticsSummary.totalRequests
                )}
              </span>
            </AppCard>
            <AppCard padding="md" className="flex flex-col">
              <span className="text-[10px] font-bold text-kumo-subtle uppercase tracking-wider">
                平均响应延迟
              </span>
              <span className="text-xl font-bold text-kumo-warning mt-1 font-mono">
                {analyticsLoading ? (
                  <SkeletonLine className="w-16 h-5" />
                ) : (
                  `${analyticsSummary.avgLatency.toFixed(0)} ms`
                )}
              </span>
            </AppCard>
            <AppCard padding="md" className="flex flex-col">
              <span className="text-[10px] font-bold text-kumo-subtle uppercase tracking-wider">
                Token 消耗量
              </span>
              <span className="text-xl font-bold text-kumo-brand mt-1 font-mono">
                {analyticsLoading ? (
                  <SkeletonLine className="w-20 h-5" />
                ) : (
                  analyticsSummary.totalTokens.toLocaleString()
                )}
              </span>
            </AppCard>
            <AppCard padding="md" className="flex flex-col">
              <span className="text-[10px] font-bold text-kumo-subtle uppercase tracking-wider">
                请求错误率
              </span>
              <span className="text-xl font-bold text-kumo-danger mt-1 font-mono">
                {analyticsLoading ? (
                  <SkeletonLine className="w-16 h-5" />
                ) : (
                  `${(analyticsSummary.errorRate * 100).toFixed(1)}%`
                )}
              </span>
            </AppCard>
          </div>

          {/* Charts Row */}
          <div className="grid grid-cols-1 lg:grid-cols-3 gap-4">
            {/* SVG Line Chart */}
            <AppCard padding="lg" className="col-span-2 space-y-3">
              <div className="flex justify-between items-center">
                <h4 className="text-xs font-bold text-kumo-strong">请求量与耗时趋势</h4>
                <div className="flex gap-4 text-[10px]">
                  <span className="flex items-center gap-1.5 text-kumo-brand font-semibold">
                    <span className="w-2 h-2 rounded-full bg-kumo-brand" />
                    请求数 (次)
                  </span>
                  <span className="flex items-center gap-1.5 text-kumo-warning font-semibold">
                    <span className="w-2.5 h-0.5 border-t-2 border-dashed border-kumo-warning" />
                    延迟 (ms)
                  </span>
                </div>
              </div>
              <div className="h-56">
                {analyticsLoading ? (
                  <div className="h-full flex items-center justify-center">
                    <SkeletonLine className="w-full h-full" />
                  </div>
                ) : (
                  <SVGAnalyticsChart dailyData={analyticsCharts.daily} />
                )}
              </div>
            </AppCard>

            {/* Model Tokens share */}
            <AppCard padding="lg" className="space-y-4">
              <div className="flex items-center gap-1.5">
                <PieChart className="w-4 h-4 text-kumo-brand" />
                <h4 className="text-xs font-bold text-kumo-strong">模型消耗分布 (Tokens)</h4>
              </div>
              <div className="space-y-3.5 max-h-56 overflow-y-auto pr-1">
                {analyticsLoading ? (
                  <div className="space-y-2">
                    <SkeletonLine className="w-full h-4" />
                    <SkeletonLine className="w-full h-4" />
                  </div>
                ) : !analyticsCharts.models || analyticsCharts.models.length === 0 ? (
                  <div className="text-center py-16 text-kumo-subtle text-xs">暂无模型数据</div>
                ) : (
                  (() => {
                    const totalTokens =
                      analyticsCharts.models.reduce((sum, m) => sum + m.tokens, 0) || 1;
                    return analyticsCharts.models.map(m => {
                      const pct = ((m.tokens / totalTokens) * 100).toFixed(1);
                      return (
                        <div key={m.model} className="space-y-1 text-xs">
                          <div className="flex justify-between text-kumo-strong">
                            <span className="font-mono font-semibold truncate max-w-[150px]">
                              {m.model}
                            </span>
                            <span className="font-mono text-kumo-subtle">
                              {m.tokens.toLocaleString()} ({pct}%)
                            </span>
                          </div>
                          <div className="w-full bg-kumo-recessed rounded-full h-2 overflow-hidden">
                            <div
                              className="bg-gradient-to-r from-kumo-brand/80 to-kumo-brand h-full rounded-full"
                              style={{ width: `${pct}%` }}
                            />
                          </div>
                        </div>
                      );
                    });
                  })()
                )}
              </div>
            </AppCard>
          </div>

          {/* Logs Table */}
          <DataTableFrame>
            <AppTable>
              <Table.Header>
                <Table.Row>
                  <Table.Head className="text-left text-xs font-bold">时间</Table.Head>
                  <Table.Head className="text-left text-xs font-bold">
                    端点渠道
                  </Table.Head>
                  <Table.Head className="text-left text-xs font-bold">
                    使用模型
                  </Table.Head>
                  <Table.Head className="text-center text-xs font-bold">
                    状态
                  </Table.Head>
                  <Table.Head className="text-right text-xs font-bold">延迟</Table.Head>
                  <Table.Head className="text-right text-xs font-bold">
                    Prompt / Completion
                  </Table.Head>
                  <Table.Head className="text-right text-xs font-bold">
                    总消耗
                  </Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {analyticsLoading && analyticsLogs.length === 0 ? (
                  <Table.Row>
                    <Table.Cell colSpan={7} className="text-center py-8">
                      <RotateCw className="w-5 h-5 animate-spin mx-auto text-kumo-subtle" />
                    </Table.Cell>
                  </Table.Row>
                ) : analyticsLogs.length === 0 ? (
                  <Table.Row>
                    <Table.Cell colSpan={7} className="text-center py-8 text-kumo-subtle text-xs">
                      暂无网关日志记录
                    </Table.Cell>
                  </Table.Row>
                ) : (
                  analyticsLogs.map(log => (
                    <Table.Row key={log.id} className="text-xs">
                      <Table.Cell className="text-kumo-subtle font-mono">
                        {formatDateTime(log.timestamp)}
                      </Table.Cell>
                      <Table.Cell className="text-kumo-strong font-semibold">
                        {log.endpointName}
                      </Table.Cell>
                      <Table.Cell className="text-kumo-strong font-mono font-medium">
                        {log.model}
                      </Table.Cell>
                      <Table.Cell className="text-center">
                        <InlineStatusPill tone={log.statusCode < 400 ? 'success' : 'danger'}>
                          {log.statusCode}
                        </InlineStatusPill>
                      </Table.Cell>
                      <Table.Cell className="text-right text-kumo-strong font-mono font-semibold">
                        {log.latencyMs} ms
                      </Table.Cell>
                      <Table.Cell className="text-right text-kumo-subtle font-mono">
                        {log.promptTokens} / {log.completionTokens}
                      </Table.Cell>
                      <Table.Cell className="text-right text-kumo-brand font-mono font-bold">
                        {log.totalTokens}
                      </Table.Cell>
                    </Table.Row>
                  ))
                )}
              </Table.Body>
            </AppTable>
          </DataTableFrame>

          {/* Table Pagination */}
          {analyticsTotal > 10 && (
            <div className="flex justify-between items-center px-4 py-2 bg-kumo-base border border-kumo-line rounded-xl shadow-sm text-xs">
              <span className="text-kumo-subtle">
                共 {analyticsTotal} 条记录，第 {analyticsPage} / {Math.ceil(analyticsTotal / 10)} 页
              </span>
              <div className="flex gap-2">
                <Button
                  size="sm"
                  disabled={analyticsPage === 1 || analyticsLoading}
                  onClick={() => setAnalyticsPage(p => Math.max(1, p - 1))}
                >
                  上一页
                </Button>
                <Button
                  size="sm"
                  disabled={analyticsPage * 10 >= analyticsTotal || analyticsLoading}
                  onClick={() => setAnalyticsPage(p => p + 1)}
                >
                  下一页
                </Button>
              </div>
            </div>
          )}
        </div>
      )}

      {/* ==================== dialogs & modals ==================== */}

      {/* 1. Endpoint Add/Edit Dialog */}
      <Dialog.Root open={endpointFormOpen} onOpenChange={setEndpointFormOpen}>
        <Dialog className="p-6 sm:max-w-md">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            {editingEndpoint ? '编辑端点' : '添加 API 端点'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            配置 OpenAI 兼容的 API 端点以供中转或对话使用。
          </Dialog.Description>

          <div className="space-y-4">
            <Input
              size="sm"
              label="名称"
              type="text"
              value={endpointForm.name}
              onChange={e => setEndpointForm({ ...endpointForm, name: e.target.value })}
              placeholder="例如：DeepSeek 官方"
              className="w-full text-kumo-strong text-xs font-sans"
            />

            <Input
              size="sm"
              label="API 接口地址 (Base URL)"
              type="text"
              value={endpointForm.baseUrl}
              onChange={e => setEndpointForm({ ...endpointForm, baseUrl: e.target.value })}
              placeholder="https://api.openai.com/v1"
              className="w-full text-kumo-strong text-xs font-mono"
            />

            <Input
              size="sm"
              label="API Key"
              type="password"
              value={endpointForm.apiKey}
              onChange={e => setEndpointForm({ ...endpointForm, apiKey: e.target.value })}
              placeholder="sk-xxxxxxxxxxxxxxxxxxxxxxxx"
              className="w-full text-kumo-strong text-xs font-mono"
            />

            <Input
              size="sm"
              label="备注"
              type="text"
              value={endpointForm.notes}
              onChange={e => setEndpointForm({ ...endpointForm, notes: e.target.value })}
              placeholder="选填"
              className="w-full text-kumo-strong text-xs font-sans"
            />

            {endpointFormError && (
              <p className="text-xs text-kumo-danger font-semibold">{endpointFormError}</p>
            )}

            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close
                render={props => (
                  <Button size="sm" {...props} variant="secondary">
                    取消
                  </Button>
                )}
              />
              <Button size="sm" variant="primary" disabled={endpointSaving} onClick={saveEndpoint}>
                {endpointSaving ? '保存中...' : '保存端点'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 2. Health Check Config Dialog */}
      <Dialog.Root open={healthCheckModal} onOpenChange={setHealthCheckModal}>
        <Dialog className="p-6 sm:max-w-md">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            模型健康检测
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            设置健康检测参数，批量发送轻量请求测试连接可用性与延迟。
          </Dialog.Description>

          <div className="space-y-4">
            <div className="bg-kumo-warning/10 border border-kumo-warning/20 text-kumo-warning p-3 rounded text-xs space-y-1">
              <p className="font-bold flex items-center gap-1.5">
                <AlertTriangle className="w-3.5 h-3.5" />
                警告
              </p>
              <p>
                健康检测需要向 API
                发送真实请求，按令牌或次数收费的模型可能会产生小额账单，请谨慎使用。
              </p>
            </div>

            <div className="flex items-center justify-between text-xs">
              <span className="font-semibold text-kumo-strong">使用密钥</span>
              <div className="flex border border-kumo-line rounded bg-kumo-recessed p-0.5">
                <Button
                  size="sm"
                  variant={healthCheckForm.useKey === 'single' ? 'secondary' : 'ghost'}
                  onClick={() => setHealthCheckForm({ ...healthCheckForm, useKey: 'single' })}
                  className={`px-3 py-1 text-[10px] font-semibold ${
                    healthCheckForm.useKey === 'single' ? 'text-kumo-strong' : 'text-kumo-subtle'
                  }`}
                >
                  单个
                </Button>
                <Button
                  size="sm"
                  variant={healthCheckForm.useKey === 'all' ? 'secondary' : 'ghost'}
                  onClick={() => setHealthCheckForm({ ...healthCheckForm, useKey: 'all' })}
                  className={`px-3 py-1 text-[10px] font-semibold ${
                    healthCheckForm.useKey === 'all' ? 'text-kumo-strong' : 'text-kumo-subtle'
                  }`}
                >
                  所有
                </Button>
              </div>
            </div>

            <div className="flex items-center justify-between text-xs">
              <span className="font-semibold text-kumo-strong">并发检测</span>
              <Switch
                checked={healthCheckForm.concurrency}
                onCheckedChange={checked =>
                  setHealthCheckForm({ ...healthCheckForm, concurrency: checked })
                }
                size="sm"
              />
            </div>

            <div className="flex items-center justify-between text-xs">
              <span className="font-semibold text-kumo-strong">超时限制</span>
              <div className="flex items-center gap-1.5">
                <Input
                  size="sm"
                  aria-label="健康检测超时限制"
                  type="number"
                  value={healthCheckForm.timeout}
                  onChange={e =>
                    setHealthCheckForm({ ...healthCheckForm, timeout: Number(e.target.value) })
                  }
                  min={1}
                  max={60}
                  className="w-16 text-kumo-strong text-xs px-2 py-1 text-center"
                />
                <span className="text-kumo-subtle">秒</span>
              </div>
            </div>

            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close
                render={props => (
                  <Button size="sm" {...props} variant="secondary">
                    取消
                  </Button>
                )}
              />
              <Button size="sm" variant="primary" onClick={startBatchHealthCheck}>
                开始检测
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
    </PageStack>
  );
}

export default OpenAIPage;
