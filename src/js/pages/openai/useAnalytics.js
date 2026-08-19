import { useCallback, useEffect, useRef, useState } from 'react';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { getAuthHeaders } from './utils.js';

export function useAnalytics(activeTab) {
  const [analyticsDays, setAnalyticsDays] = useState(() => {
    const stored = Number(localStorage.getItem('openai_analytics_days'));
    return [1, 7, 30].includes(stored) ? stored : 7;
  });
  const [analyticsGranularity, setAnalyticsGranularity] = useState(() => {
    const stored = localStorage.getItem('openai_analytics_granularity');
    return ['hour', 'day', 'week'].includes(stored) ? stored : 'day';
  });
  const [analyticsSummary, setAnalyticsSummary] = useState({
    totalRequests: 0,
    avgLatency: 0,
    avgTtfbMs: 0,
    totalTokens: 0,
    totalCachedTokens: 0,
    cachedRatio: 0,
    totalPromptTokens: 0,
    totalCompletionTokens: 0,
    errorRate: 0,
    errorCount: 0,
    endpointErrorRates: [],
  });
  // 词元趋势视角：all（全部词元）| uncached（未缓存词元）。
  const [tokenTrendMode, setTokenTrendMode] = useState('all');
  // 延迟趋势视角：total（端到端总耗时均值）| ttfb（首字延迟均值）。
  const [latencyTrendMode, setLatencyTrendMode] = useState('total');
  // 错误趋势视角：rate（错误率）| count（错误数）。
  const [errorTrendMode, setErrorTrendMode] = useState('rate');
  // 全宽趋势视角：model（按模型调用次数）| endpoint（按站点调用次数）。
  const [modelTrendMode, setModelTrendMode] = useState('model');
  // 排行视角：model（按模型）| endpoint（按站点），词元分布与调用次数两个排行独立切换。
  const [tokenShareMode, setTokenShareMode] = useState('model');
  const [countShareMode, setCountShareMode] = useState('model');
  const [analyticsCharts, setAnalyticsCharts] = useState({
    models: [],
  });
  const [analyticsLogs, setAnalyticsLogs] = useState([]);
  const [analyticsPage, setAnalyticsPage] = useState(1);
  const [analyticsPageSize, setAnalyticsPageSize] = useState(() => {
    const stored = Number(localStorage.getItem('openai_analytics_page_size'));
    return [10, 20, 50, 100].includes(stored) ? stored : 20;
  });
  const [analyticsTotal, setAnalyticsTotal] = useState(0);
  const [analyticsLoading, setAnalyticsLoading] = useState(false);
  // 分析请求竞态防护：序号递增，过期响应（慢请求覆盖新筛选/新分页）直接丢弃。
  const analyticsSeqRef = useRef(0);
  // 日志筛选：status(全部/成功/失败/429/5xx)、model、endpoint。
  const [logStatusFilter, setLogStatusFilter] = useState('');
  const [logModelFilter, setLogModelFilter] = useState('');
  const [logEndpointFilter, setLogEndpointFilter] = useState('');
  // 日志行的报错详情弹窗（仅失败请求记录 errorKind/errorMessage/errorResponse）。
  const [logDetail, setLogDetail] = useState(null);
  // 超长请求体在弹窗内默认折叠，点击「展开全部」后显示完整内容。
  const [logDetailExpanded, setLogDetailExpanded] = useState(false);

  const fetchAnalytics = useCallback(async ({ silent = false, skipSummary = false } = {}) => {
    const seq = ++analyticsSeqRef.current;
    if (!silent) setAnalyticsLoading(true);
    try {
      const headers = getAuthHeaders();
      // 日志筛选参数：data 由各路状态拼成查询串，供 logs/summary 复用。
      const logQuery = new URLSearchParams({
        days: String(analyticsDays),
        page: String(analyticsPage),
        pageSize: String(analyticsPageSize),
      });
      if (logStatusFilter) logQuery.set('status', logStatusFilter);
      if (logModelFilter) logQuery.set('model', logModelFilter);
      if (logEndpointFilter) logQuery.set('endpoint', logEndpointFilter);
      const logsURL = `/api/openai/analytics/logs?${logQuery.toString()}`;
      // skipSummary（切换时间粒度触发）：跳过 summary，只刷图表+日志。
      const [sumRes, chartsRes, logsRes] = skipSummary
        ? [undefined, await fetch(`/api/openai/analytics/charts?days=${analyticsDays}&granularity=${analyticsGranularity}`, { headers }),
           await fetch(logsURL, { headers })]
        : await Promise.all([
            fetch(`/api/openai/analytics/summary?days=${analyticsDays}&model=${encodeURIComponent(logModelFilter)}&endpoint=${encodeURIComponent(logEndpointFilter)}`, { headers }),
            fetch(`/api/openai/analytics/charts?days=${analyticsDays}&granularity=${analyticsGranularity}`, { headers }),
            fetch(logsURL, { headers }),
          ]);

      if (seq !== analyticsSeqRef.current) return; // 已有更新的请求，丢弃过期响应

      if (sumRes?.ok) {
        const data = await sumRes.json();
        setAnalyticsSummary(data);
      }
      if (chartsRes?.ok) {
        const data = await chartsRes.json();
        setAnalyticsCharts(data);
      }
      if (logsRes?.ok) {
        const data = await logsRes.json();
        setAnalyticsLogs(data.records || []);
        setAnalyticsTotal(data.total || 0);
      }
    } catch (err) {
      if (seq !== analyticsSeqRef.current) return;
      console.error('Failed to fetch analytics:', err);
      toast.error('获取分析数据失败');
    } finally {
      if (seq === analyticsSeqRef.current && !silent) setAnalyticsLoading(false);
    }
  }, [analyticsDays, analyticsGranularity, analyticsPage, analyticsPageSize, logStatusFilter, logModelFilter, logEndpointFilter]);

  // 参数变化触发的刷新：切换时间粒度只刷图表+日志（summary 不依赖粒度），
  // 切换分析范围/翻页则全量刷新（summary 也依赖天数）。首次进入 Tab 全量刷。
  const prevDaysRef = useRef(analyticsDays);
  const prevGranularityRef = useRef(analyticsGranularity);
  useEffect(() => {
    if (activeTab !== 'analytics' && activeTab !== 'logs') return;
    const daysChanged = prevDaysRef.current !== analyticsDays;
    const granularityChanged = prevGranularityRef.current !== analyticsGranularity;
    prevDaysRef.current = analyticsDays;
    prevGranularityRef.current = analyticsGranularity;
    if (granularityChanged && !daysChanged) {
      fetchAnalytics({ silent: true, skipSummary: true });
    } else {
      fetchAnalytics();
    }
  }, [activeTab, analyticsDays, analyticsGranularity, fetchAnalytics]);

  // 网关实时推送（SSE）：仅在网关日志 Tab 连接，后端出现请求立即插入日志列表顶部。
  useEffect(() => {
    if (activeTab !== 'logs') return undefined;
    let source = null;
    try {
      source = new EventSource('/api/openai/analytics/stream');
      source.addEventListener('log', event => {
        try {
          const log = JSON.parse(event.data);
          setAnalyticsLogs(prev => {
            if (!prev || prev.length === 0) return [log, ...prev];
            const dedupOf = item => `${item.timestamp}|${item.model ?? ''}|${item.statusCode ?? ''}|${item.endpoint ?? ''}|${item.clientIp ?? ''}|${item.latencyMs ?? ''}|${item.completionTokens ?? ''}`;
            const existing = new Set(prev.map(dedupOf));
            if (existing.has(dedupOf(log))) return prev;
            return [log, ...prev].slice(0, analyticsPageSize);
          });
        } catch {
          // 忽略无法解析的事件
        }
      });
    } catch {
      source = null;
    }
    return () => {
      if (source) source.close();
    };
  }, [activeTab, analyticsPageSize]);

  // 记住日志分页数量，下次进入自动沿用。
  useEffect(() => {
    localStorage.setItem('openai_analytics_page_size', String(analyticsPageSize));
  }, [analyticsPageSize]);

  // 记住数据看板的时间粒度（小时/天/周）。
  useEffect(() => {
    localStorage.setItem('openai_analytics_granularity', analyticsGranularity);
  }, [analyticsGranularity]);

  // 记住数据看板/网关日志的分析范围（天数）与时间粒度。
  useEffect(() => {
    localStorage.setItem('openai_analytics_days', String(analyticsDays));
  }, [analyticsDays]);

  // 清空全部网关日志数据。
  const clearGatewayLogs = useCallback(async () => {
    if (!(await dialog.confirm('确认清除全部网关日志记录？此操作不可恢复。'))) return;
    try {
      const response = await fetch('/api/openai/analytics/clear', {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '清除失败');
      toast.success(`已清除 ${data.deleted ?? 0} 条网关日志`);
      await fetchAnalytics();
    } catch (error) {
      toast.error('清除日志失败: ' + error.message);
    }
  }, [fetchAnalytics]);

  return {
    analyticsDays, setAnalyticsDays,
    analyticsGranularity, setAnalyticsGranularity,
    analyticsSummary,
    tokenTrendMode, setTokenTrendMode,
    latencyTrendMode, setLatencyTrendMode,
    errorTrendMode, setErrorTrendMode,
    modelTrendMode, setModelTrendMode,
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
  };
}