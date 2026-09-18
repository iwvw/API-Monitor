import { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { CalendarDotsIcon } from '@phosphor-icons/react';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { Button, RefreshButton } from '@cloudflare/kumo/components/button';
import { ChartPalette, Popover, Tabs, Toolbar } from '@cloudflare/kumo';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import useStore from '../../store.js';
import { endpointModelIds, modelHealthKey } from '../../modules/openaiModelHealth.js';
import {
  PageStack,
  stickyTabsBaseClass,
  TabBarOverflowActions,
  iconButtonIconClass,
} from '../../components/ui/AppPrimitives.jsx';
import {
  Server,
  Plus,
  Trash,
  Upload,
  Download,
  Edit,
  RefreshCw,
  History,
  Rocket,
  Activity,
  Check,
  Key,
} from '../../components/Icons.jsx';
import {
  formatCompact,
  formatTokensZh,
  formatTokensAxis,
  getAuthHeaders,
} from './utils.js';
import { useAnalytics } from './useAnalytics.js';
import { TimeRangePicker } from '../../components/ui/TimeRangePicker.jsx';
import { useGatewayKeys } from './useGatewayKeys.js';
import { useHealthChecks } from './useHealthChecks.js';
import { useEndpoints } from './useEndpoints.js';
import { OpenAIPluginsPanel } from './OpenAIPluginsPanel.jsx';
import { GatewayKeysTab } from './GatewayKeysTab.jsx';
import { AnalyticsTab } from './AnalyticsTab.jsx';
import { GatewayLogsTab } from './GatewayLogsTab.jsx';
import { EndpointsTab } from './EndpointsTab.jsx';
import { ImportModeDialog } from './ImportModeDialog.jsx';
import { LogDetailDialog } from './LogDetailDialog.jsx';
import { EndpointFormDialog } from './EndpointFormDialog.jsx';
import { AddModelDialog } from './AddModelDialog.jsx';
import { ProxyManagerDialog } from './ProxyManagerDialog.jsx';
import { GatewayKeyDialogs } from './GatewayKeyDialogs.jsx';
import { HealthCheckDialog } from './HealthCheckDialog.jsx';

function OpenAIPage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const theme = useStore(s => s.theme);
  const isDarkMode = theme === 'dark';
  const gatewayOrigin = useMemo(() => {
    if (typeof window === 'undefined') return 'http://localhost:3000';
    const url = new URL(window.location.origin);
    if (url.port === '5173' || url.port === '4173') url.port = '3000';
    return url.origin;
  }, []);

  // Tab State
  const [activeTab, setActiveTab] = useState('analytics'); // 'analytics' | 'endpoints' | 'keys' | 'logs'

  // 独立代理池插件列表（/api/proxypool），供端点表单「使用独立代理池」下拉选择。
  const [proxypoolPools, setProxypoolPools] = useState([]);
  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const res = await fetch('/api/proxypool', { headers: getAuthHeaders() });
        const data = await res.json();
        if (res.ok && data?.success && !cancelled) setProxypoolPools(data.pools || []);
      } catch {
        /* 插件未启用时静默 */
      }
    })();
    return () => { cancelled = true; };
  }, [activeTab]);

  // Gateway Analytics（状态/拉取/SSE/日志清理由 useAnalytics 统一管理）
  const analyticsApi = useAnalytics(activeTab);
  const {
    analyticsDays, setAnalyticsDays,
    analyticsMinutes,
    analyticsGranularity, setAnalyticsGranularity,
    analyticsRangeLabel, setAnalyticsRangeLabel, applyAnalyticsRange,
    analyticsSummary,
    requestTrendMode, setRequestTrendMode,
    tokenTrendMode, setTokenTrendMode,
    latencyTrendMode, setLatencyTrendMode,
    errorTrendMode, setErrorTrendMode,
    modelTrendMode, setModelTrendMode,
    modelTrendMetric, setModelTrendMetric,
    modelTrendCache, setModelTrendCache,
    tokenShareMode, setTokenShareMode,
    countShareMode, setCountShareMode,
    analyticsCharts,
    analyticsLogs,
    analyticsPage, setAnalyticsPage,
    analyticsPageSize, setAnalyticsPageSize,
    analyticsTotal,
    analyticsLoading,
    logStatusFilter, setLogStatusFilter,
    logModelFilter, setLogModelFilter,
    logEndpointFilter, setLogEndpointFilter,
    logDetail, setLogDetail,
    logDetailExpanded, setLogDetailExpanded,
    fetchAnalytics,
    clearGatewayLogs,
    clearDashboardHistory,
  } = analyticsApi;

  // 网关 API 密钥（列表/表单/轮换/默认，由 useGatewayKeys 统一管理）
  const keysApi = useGatewayKeys();
  const {
    gatewayKeys,
    gatewayKeysLoading,
    gatewayKeyToggleLoading,
    gatewayKeyDialogOpen, setGatewayKeyDialogOpen,
    editingGatewayKey,
    gatewayKeyForm, setGatewayKeyForm,
    gatewayKeyAdvancedOpen, setGatewayKeyAdvancedOpen,
    gatewayKeyFormError,
    gatewayKeySaving,
    newGatewayKey, setNewGatewayKey,
    loadGatewayKeys,
    defaultGatewayKey,
    openAddGatewayKeyModal,
    openEditGatewayKeyModal,
    applyGatewayKeyExpiryPreset,
    updateGatewayKeyExpiryDate,
    updateGatewayKeyExpiryTime,
    toggleGatewayKeyListItem,
    removeGatewayKeyListItem,
    saveGatewayKey,
    toggleGatewayKey,
    setDefaultGatewayKey,
    rotateGatewayKey,
    deleteGatewayKey,
  } = keysApi;

  // 端点与模型（CRUD/排序/模型开关/代理池/导入导出，由 useEndpoints 统一管理）
  const endpointsApi = useEndpoints();
  const {
    endpoints, setEndpoints,
    endpointsLoading, setEndpointsLoading,
    endpointsRefreshing, setEndpointsRefreshing,
    endpointToggleLoading, setEndpointToggleLoading,
    selectedEndpointId, setSelectedEndpointId,
    endpointFormOpen, setEndpointFormOpen,
    editingEndpoint, setEditingEndpoint,
    endpointForm, setEndpointForm,
    endpointFormError, setEndpointFormError,
    endpointSaving, setEndpointSaving,
    endpointKeyChecks, setEndpointKeyChecks,
    endpointKeyChecking, setEndpointKeyChecking,
    loadEndpoints,
    endpointImportInputRef,
    endpointImporting, setEndpointImporting,
    importModeDialog, setImportModeDialog,
    endpointExporting, setEndpointExporting,
    exportEndpoints,
    importEndpointsFromFile,
    runEndpointImport,
    selectedEndpoint,
    enabledModelCount,
    verifyEndpoint,
    refreshEndpointModels,
    refreshAllEndpoints,
    toggleEndpointEnabled,
    saveEndpointRouting,
    modelSwitchLoadingRef,
    modelSwitchLoading, setModelSwitchLoading,
    toggleModelEnabled,
    modelEnabledForEndpoint,
    openAddEndpointModal,
    openEditEndpointModal,
    updateEndpointProxy,
    addEndpointProxy,
    removeEndpointProxy,
    proxyBatchOpen, setProxyBatchOpen,
    proxyBatchText, setProxyBatchText,
    proxyImportLoading, setProxyImportLoading,
    subscriptionUrlOpen, setSubscriptionUrlOpen,
    subscriptionUrl, setSubscriptionUrl,
    editingProxyIndex, setEditingProxyIndex,
    proxyManagerOpen, setProxyManagerOpen,
    manualProxyEntries,
    saveProxyBatch,
    addProxyBatch,
    proxyFileInputRef,
    importProxyFile,
    expandedBatchId, setExpandedBatchId,
    manualProxyExpanded, setManualProxyExpanded,
    proxyRuntimeStates, setProxyRuntimeStates,
    disabledProxyUntil,
    disabledProxyCount,
    unbanningProxies, setUnbanningProxies,
    unbanAllProxies,
    probingProxies, setProbingProxies,
    probeAllProxies,
    removeProxyBatch,
    removeProxyFromBatch,
    resolveSubscriptionProxies,
    updateEndpointHeader,
    addEndpointHeader,
    removeEndpointHeader,
    saveEndpoint,
    checkEndpointKeys,
    appendEndpointKey,
    removeEndpointKey,
    keyDeleteConfirmActive,
    pendingDeleteEndpointId, setPendingDeleteEndpointId,
    DELETE_ENDPOINT_CONFIRM_MS,
    deleteEndpointConfirmActive,
    deleteEndpoint,
    allModels, setAllModels,
    loadAllModels,
    mappingEditKey, setMappingEditKey,
    mappingDraft, setMappingDraft,
    routingEditKey, setRoutingEditKey,
    routingDraft, setRoutingDraft,
    batchToggleEndpointModels,
    modelBatchActionLoading, setModelBatchActionLoading,
    saveEndpointMapping,
    batchEnableDisabledModels,
    addEndpointModels,
  } = endpointsApi;

  // 进入「API 端点」页时静默刷新端点与模型列表：插件中心接入/断开端点后
  // 无需手动刷新即可看到最新状态（端点列表只在挂载时拉取过一次）。
  useEffect(() => {
    if (activeTab === 'endpoints') {
      loadEndpoints(true);
      loadAllModels(true);
    }
  }, [activeTab, loadEndpoints, loadAllModels]);

  // 端点模型自愈：端点列表里出现模型为空的启用端点时（端点不稳定/首次接入
  // 拉取失败留下空列表），自动静默重取一次模型，避免必须手动点「刷新模型列表」。
  // 已触发过的端点不会重复打扰（每个会话每端点至多自愈一次）。
  const autoHealedEndpointRef = useRef(new Set());
  useEffect(() => {
    if (activeTab !== 'endpoints' || endpointsLoading) return;
    const targets = endpoints.filter(
      ep =>
        ep.enabled &&
        Array.isArray(ep.models) &&
        ep.models.length === 0 &&
        !ep.refreshing &&
        !autoHealedEndpointRef.current.has(ep.id)
    );
    targets.forEach(ep => {
      autoHealedEndpointRef.current.add(ep.id);
      refreshEndpointModels(ep, true);
    });
  }, [activeTab, endpoints, endpointsLoading, refreshEndpointModels]);

  // 端点模型健康检测（单测/批量/进度，由 useHealthChecks 统一管理）
  const healthApi = useHealthChecks({ endpoints, selectedEndpointId });
  const {
    openaiModelHealth, setOpenaiModelHealth,
    modelHealthBatchLoading,
    healthCheckProgress, setHealthCheckProgress,
    healthCheckModal, setHealthCheckModal,
    healthCheckForm, setHealthCheckForm,
    modelHealthAbortControllersRef,
    testModelHealth,
    startBatchHealthCheck,
    openHealthCheckForEndpoint,
  } = healthApi;

  // 对外暴露模型实时查询（/v1/models 同源的 /api/openai/models，每次打开 popover 即时拉取）
  const [exposedModels, setExposedModels] = useState([]);
  const [exposedModelsLoading, setExposedModelsLoading] = useState(false);
  const [exposedModelsError, setExposedModelsError] = useState(null);
  const [exposedPopoverOpen, setExposedPopoverOpen] = useState(false);
  const exposedModelsAbortRef = useRef(null);
  // 手动添加模型（Vertex AI 等无法自动拉取模型列表的上游）。
  // 打开时把端点现有模型填入文本框：所见即最终列表，追加/删改均可，保存全量同步。
  const [addModelOpen, setAddModelOpen] = useState(false);
  const [addModelText, setAddModelText] = useState('');
  const [addModelTarget, setAddModelTarget] = useState(null);
  const [addModelSaving, setAddModelSaving] = useState(false);
  const handleAddModelSubmit = useCallback(async () => {
    if (!addModelTarget || addModelSaving) return;
    setAddModelSaving(true);
    // Vertex：models 全部来自手动添加，文本框即最终列表，全量替换（删行即移除）；
    // 可自动拉取的上游：追加合并（不把自动获取的模型纳入替换范围，避免误删）。
    try {
      const replace = addModelTarget.upstreamType === 'vertex';
      const ok = await addEndpointModels(addModelTarget, addModelText, { replace });
      if (ok) {
        setAddModelOpen(false);
      }
    } finally {
      setAddModelSaving(false);
    }
  }, [addModelTarget, addModelText, addModelSaving, addEndpointModels]);
  const loadExposedModels = useCallback(async () => {
    if (exposedModelsAbortRef.current) exposedModelsAbortRef.current.abort();
    const controller = new AbortController();
    exposedModelsAbortRef.current = controller;
    setExposedModelsLoading(true);
    setExposedModelsError(null);
    try {
      const res = await fetch('/api/openai/models', { signal: controller.signal, headers: { Accept: 'application/json' } });
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const json = await res.json();
      setExposedModels(Array.isArray(json?.data) ? json.data : []);
    } catch (e) {
      if (e?.name === 'AbortError') return;
      setExposedModelsError(e?.message || '加载失败');
    } finally {
      if (exposedModelsAbortRef.current === controller) {
        exposedModelsAbortRef.current = null;
        setExposedModelsLoading(false);
      }
    }
  }, []);
  const handleExposedModelsOpenChange = useCallback((open) => {
    setExposedPopoverOpen(open);
    if (open) loadExposedModels();
  }, [loadExposedModels]);
  // Popover 打开期间每 15s 自动刷新「对外暴露的模型」，实时反映端点模型变化，
  // 无需手动点击刷新。
  useEffect(() => {
    if (!exposedPopoverOpen) return undefined;
    const timer = setInterval(loadExposedModels, 15000);
    return () => clearInterval(timer);
  }, [exposedPopoverOpen, loadExposedModels]);
  // 「模型」按钮上的对外模型数量：进入端点页即拉取一次，并在端点/模型状态变化后
  // 跟随刷新。否则该计数停留在 0，直到手动点开 popover 才触发 loadExposedModels。
  useEffect(() => {
    if (activeTab !== 'endpoints' || endpointsLoading) return;
    loadExposedModels();
  }, [activeTab, endpoints, endpointsLoading, loadExposedModels]);
  const copyExposedModelName = useCallback(async (id) => {
    try {
      await navigator.clipboard.writeText(id);
      toast.success(`模型名已复制：${id}`);
    } catch {
      toast.error('复制失败');
    }
  }, []);
  const renderExposedModels = () => {
    if (exposedModelsLoading && exposedModels.length === 0) {
      return <p className="px-2 py-4 text-center text-xs leading-normal text-kumo-subtle">加载中…</p>;
    }
    if (exposedModelsError) {
      return <p className="px-2 py-4 text-center text-xs leading-normal text-kumo-danger">{exposedModelsError}</p>;
    }
    if (exposedModels.length === 0) {
      return <p className="px-2 py-4 text-center text-xs leading-normal text-kumo-subtle">暂无对外模型</p>;
    }
    return (
      <div className="grid gap-0.5">
        {exposedModels.map((m, i) => (
          <Button
            type="button"
            key={m.id || i}
            variant="ghost"
            size="sm"
            onClick={() => copyExposedModelName(m.id)}
            title="点击复制模型名"
            className="flex w-full cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-left hover:bg-kumo-recessed"
          >
            <span className="min-w-0 flex-1 truncate font-mono text-[0.8em] leading-normal text-kumo-strong">{m.id}</span>
            {m.owned_by && (
              <span className="shrink-0 text-[0.7em] leading-normal text-kumo-subtle">{m.owned_by}</span>
            )}
          </Button>
        ))}
      </div>
    );
  };

  // ==================== 2. Models 联动（健康检测结果驱动的批量关停） ====================

  useEffect(() => {
    if (activeTab === 'keys') {
      loadGatewayKeys();
    }
  }, [activeTab, loadGatewayKeys]);


  const trendSeries = useMemo(() => {
    const buckets = Array.isArray(analyticsCharts.daily) ? analyticsCharts.daily : [];
    const build = (color, pick, name) => {
      const labels = buckets.map(point => point.day || '');
      const values = buckets.map(point => Number(pick(point)) || 0);
      return { name, color, labels, values };
    };
    return {
      requests: {
        ...build(ChartPalette.categorical(0, isDarkMode), p => p.count, '请求数'),
        formatValue: value => formatCompact(value, 0),
        formatAxis: value => formatCompact(value, 0),
      },
      requestsSuccess: {
        ...build(
          ChartPalette.semantic('Success', isDarkMode),
          p => Math.max(0, (Number(p.count) || 0) - (Number(p.errors) || 0)),
          '成功请求'
        ),
        formatValue: value => formatCompact(value, 0),
        formatAxis: value => formatCompact(value, 0),
      },
      requestsFailed: {
        ...build(ChartPalette.semantic('Attention', isDarkMode), p => p.errors, '失败请求'),
        formatValue: value => formatCompact(value, 0),
        formatAxis: value => formatCompact(value, 0),
      },
      tokens: {
        ...build(ChartPalette.categorical(1, isDarkMode), p => p.tokens, '词元用量'),
        formatValue: formatTokensZh,
        formatAxis: value => formatTokensAxis(value),
      },
      tokensUncached: {
        ...build(
          ChartPalette.categorical(1, isDarkMode),
          p => Math.max(0, (Number(p.tokens) || 0) - (Number(p.cachedTokens) || 0)),
          '未缓存词元'
        ),
        formatValue: formatTokensZh,
        formatAxis: value => formatTokensAxis(value),
      },
      latency: {
        ...build(ChartPalette.categorical(2, isDarkMode), p => p.avgLatency, '平均延迟 (s)'),
        formatValue: value => `${(Number(value) / 1000).toFixed(2)} s`,
        formatAxis: value => `${(Number(value) / 1000).toFixed(0)}`,
      },
      latencyTtfb: {
        ...build(ChartPalette.categorical(2, isDarkMode), p => p.avgTtfbMs, '平均首字延迟 (s)'),
        formatValue: value => `${(Number(value) / 1000).toFixed(2)} s`,
        formatAxis: value => `${(Number(value) / 1000).toFixed(0)}`,
      },
      errorRate: {
        ...build(
          ChartPalette.categorical(3, isDarkMode),
          p => (Number(p.count) > 0 ? ((Number(p.errors) || 0) / Number(p.count)) * 100 : 0),
          '错误率 (%)'
        ),
        formatValue: value => `${Number(value).toFixed(2)}%`,
        formatAxis: value => `${Number(value).toFixed(0)}%`,
      },
      errorCount: {
        ...build(ChartPalette.categorical(3, isDarkMode), p => p.errors, '错误数'),
        formatValue: value => formatCompact(value, 0),
        formatAxis: value => formatCompact(value, 0),
      },
    };
  }, [analyticsCharts, isDarkMode]);

  // 全宽「模型调用趋势」数据：按后端 byModel / buckets 对齐。
  const byModelTrend = useMemo(() => {
    const daily = Array.isArray(analyticsCharts.daily) ? analyticsCharts.daily : [];
    const labels =
      Array.isArray(analyticsCharts.buckets) && analyticsCharts.buckets.length
        ? analyticsCharts.buckets
        : trendSeries.requests.labels;
    const tsValues = daily.map(point => (Number(point.tsSec) || 0) * 1000);
    const models = Array.isArray(analyticsCharts.byModel) ? analyticsCharts.byModel : [];
    const endpoints = Array.isArray(analyticsCharts.byEndpoint) ? analyticsCharts.byEndpoint : [];
    return { labels, tsValues, models, endpoints };
  }, [analyticsCharts, trendSeries]);


  useEffect(() => {
    if (activeTab === 'endpoints' || activeTab === 'keys') {
      loadGatewayKeys();
    }
  }, [activeTab, loadGatewayKeys]);


  // Endpoint Verification & Model Refresh

  // ==================== 2. Health Checking ====================


  const failedModelIdsForEndpoint = endpoint => {
    if (!endpoint) return [];
    return endpointModelIds(endpoint).filter(
      modelId => openaiModelHealth[modelHealthKey(endpoint.id, modelId)]?.status === 'error'
    );
  };


  // 检测为有效的模型（healthy/degraded）不在此列，由每行手动开关控制。
  const batchCloseNonHealthyModels = async endpoint => {
    if (modelBatchActionLoading) return;
    const targets = endpointModelIds(endpoint).filter(modelId => {
      if (!modelEnabledForEndpoint(endpoint, modelId)) return false;
      const health = openaiModelHealth[modelHealthKey(endpoint.id, modelId)];
      return health?.status !== 'healthy' && health?.status !== 'degraded';
    });
    if (targets.length === 0) {
      toast.info('当前没有可批量关闭的模型（非有效模型均为空）', { isManual: true });
      return;
    }
    await batchToggleEndpointModels(endpoint, targets, false, `已关闭 ${targets.length} 个非有效模型`);
  };

  // 兼容旧调用：全局一键关闭失败的模型（保留，供顶栏使用）。
  const batchCloseFailedModels = async endpoint => {
    if (modelBatchActionLoading) return;
    const failed = failedModelIdsForEndpoint(endpoint).filter(modelId =>
      modelEnabledForEndpoint(endpoint, modelId)
    );
    if (failed.length === 0) {
      toast.info('当前端点没有检测失败的模型', { isManual: true });
      return;
    }
    await batchToggleEndpointModels(endpoint, failed, false, `已关闭 ${failed.length} 个检测失败的模型`);
  };

  // 全局一键：跨全部启用端点，关闭所有检测失败的模型（仅停用，不隐藏）。
  const batchCloseAllFailedModels = async () => {
    if (modelBatchActionLoading) return;
    const byEndpoint = {};
    endpoints.forEach(endpoint => {
      if (!endpoint.enabled) return;
      failedModelIdsForEndpoint(endpoint).forEach(modelId => {
        if (modelEnabledForEndpoint(endpoint, modelId)) {
          byEndpoint[endpoint.id] = byEndpoint[endpoint.id] || { endpoint, models: [] };
          byEndpoint[endpoint.id].models.push(modelId);
        }
      });
    });
    const entries = Object.values(byEndpoint);
    const total = entries.reduce((sum, entry) => sum + entry.models.length, 0);
    if (total === 0) {
      toast.info('当前没有检测失败的模型', { isManual: true });
      return;
    }
    if (!(await dialog.confirm(`确认关闭全部 ${total} 个检测失败的模型吗？（仅停用对应模型）`))) {
      return;
    }
    setModelBatchActionLoading(true);
    try {
      for (const entry of entries) {
        const response = await fetch(
          `/api/openai/endpoints/${entry.endpoint.id}/models/toggle-batch`,
          {
            method: 'POST',
            headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
            body: JSON.stringify({ models: entry.models, enabled: false }),
          }
        );
        const data = await response.json().catch(() => ({}));
        if (!response.ok || !data.success) throw new Error(data.error || '更新失败');
        setEndpoints(prev =>
          prev.map(e =>
            e.id === entry.endpoint.id
              ? { ...e, disabledModels: Array.isArray(data.disabledModels) ? data.disabledModels : [] }
              : e
          )
        );
      }
      await loadAllModels(true);
      toast.success(`已关闭 ${total} 个检测失败的模型`);
    } catch (error) {
      toast.error(`批量更新失败: ${error.message}`);
    } finally {
      setModelBatchActionLoading(false);
    }
  };


  return (
    <PageStack viewport>
      {/* Tab Navigation */}
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={[
            {
              value: 'analytics',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Activity className="w-3.5 h-3.5" />
                  数据看板
                </span>
              ),
            },
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
              value: 'keys',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Key className="w-3.5 h-3.5" />
                  API 密钥
                </span>
              ),
            },
            {
              value: 'logs',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <History className="w-3.5 h-3.5" />
                  网关日志
                </span>
              ),
            },
            {
              value: 'beta',
              label: (
                <span className="inline-flex items-center gap-1.5">
                  <Rocket className="w-3.5 h-3.5" />
                  插件
                </span>
              ),
            },
          ]}
        />
        <div className="flex min-w-0 items-center justify-end gap-2">
            {activeTab === 'analytics' && (
              <>
                <TimeRangePicker value={analyticsRangeLabel} onApply={applyAnalyticsRange} />
                <TabBarOverflowActions
                  items={[
                    {
                      key: 'granularity',
                      type: 'select',
                      label: '时间粒度',
                      icon: <CalendarDotsIcon className="h-3.5 w-3.5" />,
                      value: analyticsMinutes ? 'hour' : analyticsGranularity,
                      onValueChange: val => {
                        if (!analyticsMinutes) setAnalyticsGranularity(val || 'day');
                      },
                      options: [
                        { value: 'hour', label: '按小时' },
                        { value: 'day', label: '按天' },
                        { value: 'week', label: '按周' },
                      ],
                      selectClassName: 'w-28',
                    },
                    {
                      key: 'clear-history',
                      label: '清除历史',
                      icon: <Trash className="w-3.5 h-3.5" />,
                      onClick: clearDashboardHistory,
                      danger: true,
                    },
                    {
                      key: 'refresh',
                      label: '刷新',
                      icon: <RefreshCw className="w-3.5 h-3.5" />,
                      onClick: fetchAnalytics,
                      disabled: analyticsLoading,
                      loading: analyticsLoading,
                    },
                  ]}
                />
              </>
            )}
            {activeTab === 'keys' && (
              <TabBarOverflowActions
                items={[
                  {
                    key: 'refresh',
                    label: '刷新',
                    icon: <RefreshCw className={iconButtonIconClass} />,
                    onClick: loadGatewayKeys,
                    disabled: gatewayKeysLoading,
                    loading: gatewayKeysLoading,
                  },
                  {
                    key: 'add',
                    label: '新建密钥',
                    icon: <Plus className={iconButtonIconClass} />,
                    onClick: openAddGatewayKeyModal,
                    variant: 'primary',
                  },
                ]}
              />
            )}
            {activeTab === 'logs' && (
              <div className="flex shrink-0 items-center gap-2">
                <TabBarOverflowActions
                  items={[
                    {
                      key: 'range',
                      type: 'select',
                      label: '时间范围',
                      icon: <CalendarDotsIcon className="h-3.5 w-3.5" />,
                      value: String(analyticsDays),
                      onValueChange: val => {
                        setAnalyticsDays(Number(val));
                        const d = Number(val);
                        setAnalyticsRangeLabel(d === 1 ? '过去 24 小时' : d === 30 ? '过去 30 天' : '过去 7 天');
                        setAnalyticsPage(1);
                      },
                      options: [
                        { value: '1', label: '最近 24 小时' },
                        { value: '7', label: '最近 7 天' },
                        { value: '30', label: '最近 30 天' },
                      ],
                      selectClassName: 'w-36',
                    },
                    {
                      key: 'clear',
                      label: '清除日志',
                      icon: <Trash className="h-3.5 w-3.5" />,
                      onClick: clearGatewayLogs,
                      danger: true,
                    },
                  ]}
                />
                <RefreshButton
                  size="sm"
                  variant="secondary"
                  loading={analyticsLoading}
                  disabled={analyticsLoading}
                  aria-label="刷新网关日志"
                  title="刷新网关日志"
                  onClick={() => fetchAnalytics()}
                />
              </div>
            )}
            {activeTab === 'endpoints' && (
              <div className="flex shrink-0 items-center gap-2">
                <Toolbar size="sm" aria-label="端点导入导出" className="shrink-0">
                  <Toolbar.Button
                    onClick={exportEndpoints}
                    disabled={endpoints.length === 0 || endpointExporting}
                    icon={<Upload className="h-3.5 w-3.5" />}
                  >
                    <span className="hidden cq-sm:inline">导出</span>
                  </Toolbar.Button>
                  <Toolbar.Button
                    onClick={() => endpointImportInputRef.current?.click()}
                    disabled={endpointImporting}
                    icon={<Download className="h-3.5 w-3.5" />}
                  >
                    <span className="hidden cq-sm:inline">导入</span>
                  </Toolbar.Button>
                </Toolbar>
                <TabBarOverflowActions
                  items={[
                    {
                      key: 'health',
                      label: '健康检测',
                      icon: <Activity className={iconButtonIconClass} />,
                      onClick: () => setHealthCheckModal(true),
                      disabled: modelHealthBatchLoading,
                      loading: modelHealthBatchLoading,
                    },
                    {
                      key: 'refresh',
                      label: '刷新列表',
                      icon: <RefreshCw className={iconButtonIconClass} />,
                      onClick: refreshAllEndpoints,
                      disabled: endpointsRefreshing,
                      loading: endpointsRefreshing,
                    },
                    {
                      key: 'add',
                      label: '新增端点',
                      icon: <Plus className={iconButtonIconClass} />,
                      onClick: openAddEndpointModal,
                      variant: 'primary',
                    },
                  ]}
                />
              </div>
            )}
          </div>
      </div>

      {/* ==================== 1. API 端点 Tab ==================== */}
      {activeTab === 'endpoints' && (
        <EndpointsTab
          endpointsApi={endpointsApi}
          healthApi={healthApi}
          keysApi={keysApi}
          exposedModels={exposedModels}
          exposedModelsLoading={exposedModelsLoading}
          gatewayOrigin={gatewayOrigin}
          renderExposedModels={renderExposedModels}
          handleExposedModelsOpenChange={handleExposedModelsOpenChange}
          loadExposedModels={loadExposedModels}
          batchCloseNonHealthyModels={batchCloseNonHealthyModels}
          modelBatchActionLoading={modelBatchActionLoading}
          setAddModelOpen={setAddModelOpen}
          setAddModelTarget={setAddModelTarget}
          setAddModelText={setAddModelText}
        />
      )}

      {/* ==================== 2. API 密钥 Tab ==================== */}
      {activeTab === 'keys' && (
        <GatewayKeysTab keys={keysApi} isArmed={isArmed} />
      )}

      {/* ==================== 3. 网关分析 Tab ==================== */}
      {activeTab === 'analytics' && (
        <AnalyticsTab
          analytics={analyticsApi}
          isDarkMode={isDarkMode}
          trendSeries={trendSeries}
          byModelTrend={byModelTrend}
        />
      )}

      {/* ==================== 4. 网关日志 Tab ==================== */}
      {activeTab === 'logs' && (
        <GatewayLogsTab analytics={analyticsApi} endpoints={endpoints} />
      )}

      {/* ==================== dialogs & modals ==================== */}

      {/* 0. 网关日志报错详情 Dialog（仅失败请求记录 errorKind/errorMessage/errorResponse） */}
      <ImportModeDialog endpointsApi={endpointsApi} />
      <LogDetailDialog analytics={analyticsApi} />

      {/* 1. Endpoint Add/Edit Dialog */}
      <EndpointFormDialog endpointsApi={endpointsApi} proxypoolPools={proxypoolPools} />

      {/* 1b. 出口代理池管理弹窗 */}
      <AddModelDialog
        addModelOpen={addModelOpen}
        setAddModelOpen={setAddModelOpen}
        addModelText={addModelText}
        setAddModelText={setAddModelText}
        handleAddModelSubmit={handleAddModelSubmit}
        addModelSaving={addModelSaving}
      />
      <ProxyManagerDialog endpointsApi={endpointsApi} />

      {/* 2. Gateway Key Dialogs */}
      <GatewayKeyDialogs keysApi={keysApi} endpointsApi={endpointsApi} />

      {/* 3. Health Check Config Dialog */}
      <HealthCheckDialog healthApi={healthApi} />
      {activeTab === 'beta' && <OpenAIPluginsPanel />}
    </PageStack>
  );
}

export default OpenAIPage;
