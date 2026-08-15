import React, { useState, useEffect, useRef, useMemo, useCallback, memo } from 'react';
import { ArrowDown, ArrowUp, CalendarDotsIcon } from '@phosphor-icons/react';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { Button, RefreshButton } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import { Autocomplete } from '@cloudflare/kumo/components/autocomplete';
import {
  Chart,
  ChartLegend,
  ClipboardText,
  ChartPalette,
  Collapsible,
  Badge,
  DatePicker,
  Label,
  LayerCard,
  Loader,
  Pagination,
  Popover,
  Table,
  Tabs,
  Toolbar,
} from '@cloudflare/kumo';
import { MODULE_TABS_PROPS, TOOL_TABS_PROPS } from '../modules/kumoTabs.js';
import { handleEditableRowDoubleClick } from '../modules/tableInteractions.js';
import { formatDateTime } from '../modules/utils.js';
import { renderMarkdown } from '../modules/markdown.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import useStore from '../store.js';
import * as echarts from 'echarts/core';
import { BarChart } from 'echarts/charts';
import {
  AriaComponent,
  AxisPointerComponent,
  GridComponent,
  TooltipComponent,
} from 'echarts/components';
import { CanvasRenderer } from 'echarts/renderers';

echarts.use([
  BarChart,
  AxisPointerComponent,
  GridComponent,
  TooltipComponent,
  AriaComponent,
  CanvasRenderer,
]);
const ENDPOINT_PROTOCOL_OPTIONS = [
  { value: 'auto', label: '自动（HTTP/2 优先）' },
  { value: 'http1', label: 'HTTP/1.1' },
  { value: 'h2', label: 'HTTP/2' },
];
// 大代理池（文件批量导入可达数千条）在表单/管理弹窗中只预览前 N 条，避免渲染卡顿。
const PROXY_PREVIEW_LIMIT = 120;
import {
  DEFAULT_MODEL_HEALTH_CONCURRENCY,
  DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS,
  countModelHealthResults,
  endpointModelIds,
  modelHealthKey,
  modelHealthTargets,
  normalizeModelHealthRecord,
  resolveModelHealthConcurrency,
} from '../modules/openaiModelHealth.js';
import {
  PageStack,
  AppCard,
  StatusBadge,
  EmptyState,
  stickyTabsBaseClass,
  TabBarOverflowActions,
  iconButtonIconClass,
  actionIconClass,
  cx,
} from '../components/ui/AppPrimitives.jsx';
import {
  Server,
  MessageSquare,
  Plus,
  Trash,
  RotateCw,
  Search,
  Upload,
  Download,
  Edit,
  X,
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
  Brain,
  Settings as SettingsIcon,
  Copy,
  Cpu,
  AlertTriangle,
  Key,
  Reboot,
  Play,
  Clock,
  TrendingUp,
  LogList,
  Globe,
  Sliders,
  ChevronDown,
} from '../components/Icons.jsx';

// 从 Kumo CSS 变量读取主题色，供 ECharts 等需要真实颜色值的场景使用。
const kumoHex = name => {
  try {
    const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    return value || undefined;
  } catch {
    return undefined;
  }
};

// 大数字压缩为「万 / 亿」单位；小数位默认 2 位。
const formatCompact = (value, decimals = 2) => {
  const num = Number(value);
  if (!Number.isFinite(num)) return String(value);
  const abs = Math.abs(num);
  if (abs >= 1e8) return `${(num / 1e8).toFixed(decimals)}亿`;
  if (abs >= 1e4) return `${(num / 1e4).toFixed(decimals)}万`;
  if (Number.isInteger(num)) return String(num);
  return num.toFixed(decimals);
};

// 词元统一以百万（M）为单位，保留 2 位小数。
const formatTokensM = value => `${(Number(value) / 1e6).toFixed(2)}M`;

function createHealthCheckProgress(total = 0, running = false) {  return { running, total, completed: 0, healthy: 0, degraded: 0, failed: 0 };
}

// parseProxyEntry 解析代理 URL 为可读摘要与完整值：
// 返回 { label, full, host, ip }。label 为友好名称（优先 # 后的节点名），
// ip 为纯主机地址（不含端口与用户信息），host 为 host:port 或 user:pass@host:port。
function parseProxyEntry(raw) {
  const value = String(raw || '').trim();
  if (!value) return { label: '', full: value, host: '', ip: '' };
  let label = value;
  let host = '';
  let ip = '';
  try {
    let rest = value;
    let hash = '';
    const hashIndex = rest.indexOf('#');
    if (hashIndex !== -1) {
      hash = rest.slice(hashIndex);
      rest = rest.slice(0, hashIndex);
    }
    // 去掉 scheme://，兼容 socks/http/https 等自定义协议（new URL 对 socks 不解析 host）。
    const schemeEnd = rest.indexOf('://');
    if (schemeEnd !== -1) rest = rest.slice(schemeEnd + 3);
    // 去掉 userinfo，得到 host:port。
    const atIndex = rest.lastIndexOf('@');
    const authority = atIndex !== -1 ? rest.slice(atIndex + 1) : rest;
    host = authority;
    // 去掉端口得到纯 IP/主机名。
    ip = authority.replace(/:\d+$/, '');
    // # 后的 fragment 通常是节点名。
    const fallback = hash ? decodeURIComponent(hash.slice(1)) : '';
    label = fallback || authority || value;
  } catch {
    // 解析失败时展示原文。
  }
  return { label, full: value, host, ip };
}

// 自绘 ECharts 柱状时间桶：每个桶(小时/天/周)一根柱，类目轴标签 = 桶名，避免时间轴重复标签。

function activeModelIdsForEndpoint(endpoint) {
  const disabled = Array.isArray(endpoint?.disabledModels) ? endpoint.disabledModels : [];
  return Array.from(
    new Set(
      (Array.isArray(endpoint?.models) ? endpoint.models : [])
        .map(model => (typeof model === 'string' ? model.trim() : (model?.id || '').trim()))
        .filter(id => id && !disabled.includes(id))
    )
  );
}

// 按请求结果给 pill 上色：先看状态码（失败语义优先），成功再看总耗时。
// 耗时档位相对首字放宽（总耗时含上传+推理+输出）：绿 < 15s，蓝 15-45s，
// 黄 45-120s，红 >= 120s；成功但无输出的低完成度请求保持黄，提醒关注。
function resultTone(statusCode, completionTokens, latencyMs) {
  const status = Number(statusCode) || 0;
  const ms = Number(latencyMs) || 0;
  if (status === 429) return 'warning';
  if (status >= 500) return 'danger';
  if (status >= 400) return 'warning';
  if (!(Number(completionTokens) > 0)) return 'warning';
  if (ms >= 120000) return 'danger';
  if (ms >= 45000) return 'warning';
  if (ms >= 15000) return 'info';
  return 'success';
}

// ttfbTone 根据首字耗时（毫秒）返回色阶。相对当前网关实际分布（响应常见
// 5-130s，空闲时可低至 1-3s）取档：绿 < 5s 正常，蓝 5-15s 偏慢，黄 15-45s
// 很慢，红 >= 45s 异常/接近超时；无数据（'—'）用灰 neutral 与蓝色区分。
function ttfbTone(ms) {
  if (ms <= 0) return 'neutral';
  if (ms < 5000) return 'success';
  if (ms < 15000) return 'info';
  if (ms < 45000) return 'warning';
  return 'danger';
}

function statusCodeTone(code) {
  if (code === 429) return 'warning';
  if (code >= 500) return 'danger';
  if (code >= 400) return 'warning';
  return 'success';
}

// ProxyRuntimeMeta 展示单个代理的运行时观测信息：出口公网 IP 与最近首字延迟。
// state 来自 /proxy-state 的 proxyRuntimeStateItem；两者皆可缺省（尚未产生探活
// 记录时静默不渲染，保持列表紧凑）。
function ProxyRuntimeMeta({ proxy, state }) {
  const hasExit = state && state.lastExitIP;
  const hasTTFB = state && Number(state.lastTTFB) > 0;
  if (!hasExit && !hasTTFB) return null;
  return (
    <div className="mt-0.5 flex items-center gap-2 font-mono text-[10px] leading-none text-kumo-subtle">
      {hasExit && (
        <span title={`出口 IP（经代理出网，探活记录）\n${proxy}`} className="truncate">
          <Globe className="mr-0.5 inline h-2.5 w-2.5" />{state.lastExitIP}
        </span>
      )}
      {hasTTFB && (
        <span title="最近一次请求的首字耗时">
          ~{(state.lastTTFB / 1000).toFixed(1)}s 首字
        </span>
      )}
    </div>
  );
}

// IpCell 展示脱敏 IP，点击弹出 Popover 显示完整 IP。
function IpCell({ value, viaProxy, placeholder }) {
  if (!value) return <>{placeholder || '—'}</>;
  return (
    <Popover>
      <Popover.Trigger
        nativeButton={false}
        render={
          <span
            className={`cursor-pointer truncate ${viaProxy ? 'text-kumo-info' : ''}`}
          >
            {maskIp(value)}
          </span>
        }
      />
      <Popover.Content className="p-3 max-w-xs">
        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
          {viaProxy ? '出口 IP' : '客户端 IP'}
        </Popover.Title>
        <div className="mt-2">
          <code className="rounded bg-kumo-surface-2 px-2 py-1 text-xs font-mono text-kumo-strong select-all">
            {value}
          </code>
        </div>
      </Popover.Content>
    </Popover>
  );
}

// maskIp 压缩 IP 展示：去掉端口，仅保留首尾片段、中间用 ••• 隐藏，用于日志表格
// 减少宽度占用。IPv4 保留前 2 段 + 后 1 段；IPv6 保留前 2 段 + 后 2 段。
function maskIp(raw) {
  if (!raw) return raw || '';
  let value = String(raw).trim();
  // 剥掉方括号包裹的 IPv6 端口：[2001:db8::1]:443 → 2001:db8::1。
  const bracketed = value.match(/^\[(.+)\]:\d+$/);
  if (bracketed) value = bracketed[1];
  const colonIdx = value.lastIndexOf(':');
  // 形如 1.2.3.4:5678 时去掉端口；IPv6（含 :: 分隔）不剥端口。
  if (/^\d{1,3}(\.\d{1,3}){3}/.test(value) && colonIdx > -1) {
    value = value.slice(0, colonIdx);
  }
  value = value.replace('[', '').replace(']', '');
  if (value.includes(':')) {
    // IPv6
    const segments = value.split(':');
    if (segments.length <= 2) return value;
    const head = segments.slice(0, 2).join(':');
    const tail = segments.slice(-2).join(':');
    return `${head}•••${tail}`;
  }
  const parts = value.split('.');
  if (parts.length === 4) {
    return `${parts[0]}.•••.•••.${parts[3]}`;
  }
  return value;
}

// FailoverPathBadge 在端点列展示渠道迁移标记：当一次请求经历过多个端点尝试时，
// 端点名以橙色高亮，点击弹出完整迁移路径。
function FailoverPathBadge({ path, endpointName }) {
  let steps = [];
  if (path) {
    try {
      const parsed = JSON.parse(path);
      if (Array.isArray(parsed)) steps = parsed;
    } catch (e) {}
  }
  if (steps.length < 2) return <span className="truncate">{endpointName}</span>;
  return (
    <Popover>
      <Popover.Trigger
        nativeButton={false}
        render={
          <span className="cursor-pointer truncate font-medium text-kumo-warning">
            {endpointName}
          </span>
        }
      />
      <Popover.Content className="p-3 max-w-xs">
        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
          渠道迁移路径
        </Popover.Title>
        <div className="mt-2 flex flex-col gap-1">
          {steps.map((s, i) => (
            <div key={`${s.endpoint}-${i}`} className="flex items-center gap-1.5 text-xs">
              <span className="font-mono truncate max-w-[140px]" title={s.endpoint}>
                {s.endpoint || 'unknown'}
              </span>
              <StatusBadge tone={statusCodeTone(s.status)}>{s.status || '-'}</StatusBadge>
              {i < steps.length - 1 && <span className="text-kumo-subtle">→</span>}
            </div>
          ))}
        </div>
      </Popover.Content>
    </Popover>
  );
}

// 端点多 key 检测结果 -> 状态徽标。check 为 null 表示该行尚未检测。
function keyCheckBadgeProps(check) {
  if (!check) return null;
  switch (check.status) {
    case 'checking':
      return { tone: 'info', label: '检测中' };
    case 'valid':
      return { tone: 'success', label: '有效' };
    case 'invalid':
      return { tone: 'danger', label: '失效' };
    case 'overdue':
      return { tone: 'warning', label: '欠费' };
    default:
      return { tone: 'neutral', label: '异常' };
  }
}

function KeyStatusBadge({ check }) {
  const props = keyCheckBadgeProps(check);
  if (!props) return <span className="w-9 shrink-0" />;
  return (
    <StatusBadge tone={props.tone} title={check?.message} className="shrink-0">
      {props.label}
    </StatusBadge>
  );
}

const GATEWAY_EXPIRY_HOURS = Array.from({ length: 24 }, (_, hour) => {
  const value = String(hour).padStart(2, '0');
  return { value, label: value };
});

const GATEWAY_EXPIRY_MINUTES = Array.from({ length: 60 }, (_, minute) => {
  const value = String(minute).padStart(2, '0');
  return { value, label: value };
});

function toLocalDateTimeValue(date) {
  if (!(date instanceof Date) || Number.isNaN(date.getTime())) return '';
  const pad = value => String(value).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(
    date.getHours()
  )}:${pad(date.getMinutes())}`;
}

function parseLocalDateTime(value) {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date;
}

function OpenAIPage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const { theme } = useStore();
  const isDarkMode = theme === 'dark';
  const gatewayOrigin = useMemo(() => {
    if (typeof window === 'undefined') return 'http://localhost:3000';
    const url = new URL(window.location.origin);
    if (url.port === '5173' || url.port === '4173') url.port = '3000';
    return url.origin;
  }, []);

  // Tab State
  const [activeTab, setActiveTab] = useState('analytics'); // 'analytics' | 'endpoints' | 'keys' | 'logs'

  // Gateway Analytics States
  const [analyticsDays, setAnalyticsDays] = useState(7);
  const [analyticsGranularity, setAnalyticsGranularity] = useState(() => {
    const stored = localStorage.getItem('openai_analytics_granularity');
    return ['hour', 'day', 'week'].includes(stored) ? stored : 'day';
  });
  const [analyticsSummary, setAnalyticsSummary] = useState({
    totalRequests: 0,
    avgLatency: 0,
    totalTokens: 0,
    totalCachedTokens: 0,
    cachedRatio: 0,
    totalPromptTokens: 0,
    totalCompletionTokens: 0,
    errorRate: 0,
  });
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
  // 日志筛选：status(全部/成功/失败/429/5xx)、model、endpoint。
  const [logStatusFilter, setLogStatusFilter] = useState('');
  const [logModelFilter, setLogModelFilter] = useState('');
  const [logEndpointFilter, setLogEndpointFilter] = useState('');
  const getAuthHeaders = useCallback(() => {
    return {
      'Content-Type': 'application/json',
    };
  }, []);

  const fetchAnalytics = useCallback(async ({ silent = false, skipSummary = false } = {}) => {
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
      console.error('Failed to fetch analytics:', err);
      toast.error('获取分析数据失败');
    } finally {
      if (!silent) setAnalyticsLoading(false);
    }
  }, [analyticsDays, analyticsGranularity, analyticsPage, analyticsPageSize, logStatusFilter, logModelFilter, logEndpointFilter, getAuthHeaders]);

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
            const existing = new Set(prev.map(item => `${item.timestamp}:${item.model}:${item.clientIp ?? ''}:${item.latencyMs ?? ''}`));
            const key = `${log.timestamp}:${log.model ?? ''}:${log.clientIp ?? ''}:${log.latencyMs ?? ''}`;
            if (existing.has(key)) return prev;
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

  // ==================== 1. Endpoints & Gateway Keys State ====================
  const [endpoints, setEndpoints] = useState([]);
  const [endpointsLoading, setEndpointsLoading] = useState(false);
  const [endpointsRefreshing, setEndpointsRefreshing] = useState(false);
  const [endpointToggleLoading, setEndpointToggleLoading] = useState({});
  const [selectedEndpointId, setSelectedEndpointId] = useState('');
  const [draggedEndpointId, setDraggedEndpointId] = useState(null);
  const [endpointReorderSaving, setEndpointReorderSaving] = useState(false);
  const [endpointFormOpen, setEndpointFormOpen] = useState(false);
  const [editingEndpoint, setEditingEndpoint] = useState(null);
  const [endpointForm, setEndpointForm] = useState({
    name: '',
    baseUrl: '',
    apiKey: '',
    apiKeys: [],
    notes: '',
    headers: [],
    proxyPool: [],
    autoSwitch: false,
    proxyEnabled: false,
    allowDirectFallback: true,
    protocol: 'auto',
  });
  const [endpointFormError, setEndpointFormError] = useState('');
  const [endpointSaving, setEndpointSaving] = useState(false);
  // 端点编辑弹窗中的多 key 状态：数组下标与 key 行对齐（0=主 key/K1，n=备用 key/K(n+1)）。
  const [endpointKeyChecks, setEndpointKeyChecks] = useState([]);
  const [endpointKeyChecking, setEndpointKeyChecking] = useState(false);
  const [gatewayKeys, setGatewayKeys] = useState([]);
  const [gatewayKeysLoading, setGatewayKeysLoading] = useState(false);
  const [gatewayKeyToggleLoading, setGatewayKeyToggleLoading] = useState({});
  const [gatewayKeyDialogOpen, setGatewayKeyDialogOpen] = useState(false);
  const [editingGatewayKey, setEditingGatewayKey] = useState(null);
  const [gatewayKeyForm, setGatewayKeyForm] = useState({
    name: '',
    expiresAt: '',
    allowedModels: [],
    allowedEndpoints: [],
    maxTokensQuota: '',
  });
  const [gatewayKeyModelInput, setGatewayKeyModelInput] = useState('');
  const [gatewayKeyEndpointInput, setGatewayKeyEndpointInput] = useState('');
  const [gatewayKeyAdvancedOpen, setGatewayKeyAdvancedOpen] = useState(false);
  const [gatewayKeyFormError, setGatewayKeyFormError] = useState('');
  const [gatewayKeySaving, setGatewayKeySaving] = useState(false);
  const [newGatewayKey, setNewGatewayKey] = useState(null);

  // Batch adding endpoints
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
    localStorage.removeItem('openai_endpoints_cache');
    loadEndpoints();
  }, [loadEndpoints]);

  const endpointImportInputRef = useRef(null);
  const [endpointImporting, setEndpointImporting] = useState(false);
  const [endpointExporting, setEndpointExporting] = useState(false);

  const exportEndpoints = async () => {
    setEndpointExporting(true);
    try {
      const response = await fetch('/api/openai/export', { headers: getAuthHeaders() });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok || payload.success !== true) throw new Error(payload.error || '导出端点失败');
      const list = Array.isArray(payload.endpoints) ? payload.endpoints : [];
      if (list.length === 0) { toast.warning('暂无端点可导出'); return; }
      const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = `openai-endpoints-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(url);
      toast.success(`已导出 ${list.length} 个端点（包含 API Key，请注意保管）`);
    } catch (error) {
      toast.error(error.message || '导出端点失败');
    } finally {
      setEndpointExporting(false);
    }
  };

  const importEndpointsFromFile = async (event) => {
    const file = event.target.files?.[0];
    event.target.value = '';
    if (!file) return;
    setEndpointImporting(true);
    try {
      const text = await file.text();
      const data = JSON.parse(text);
      const list = Array.isArray(data) ? data : (data.endpoints || []);
      if (list.length === 0) throw new Error('文件中没有端点数据');
      if (!(await dialog.confirm(`确认导入 ${list.length} 个端点？已存在相同 baseUrl 的端点会自动跳过。`))) return;
      const response = await fetch('/api/openai/import', {
        method: 'POST',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ endpoints: list, overwrite: false }),
      });
      const payload = await response.json().catch(() => ({}));
      if (!response.ok || payload.success !== true) throw new Error(payload.error || '导入端点失败');
      await loadEndpoints(true);
      toast.success(`导入完成：新增 ${payload.imported ?? 0} 个，跳过 ${payload.skipped ?? 0} 个`);
    } catch (error) {
      toast.error(error.message || '导入端点失败');
    } finally {
      setEndpointImporting(false);
    }
  };

  const loadGatewayKeys = useCallback(async () => {
    setGatewayKeysLoading(true);
    try {
      const response = await fetch('/api/openai/keys', { headers: getAuthHeaders() });
      const data = await response.json().catch(() => []);
      if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
      setGatewayKeys(Array.isArray(data) ? data : []);
    } catch (error) {
      toast.error('加载网关密钥失败: ' + error.message);
    } finally {
      setGatewayKeysLoading(false);
    }
  }, [getAuthHeaders]);

  useEffect(() => {
    if (activeTab === 'keys') {
      loadGatewayKeys();
    }
  }, [activeTab, loadGatewayKeys]);

  const selectedEndpoint = useMemo(
    () => endpoints.find(endpoint => endpoint.id === selectedEndpointId) || endpoints[0] || null,
    [endpoints, selectedEndpointId]
  );

  // 实际启用（未被禁用）的模型总数，跨启用端点去重。
  const enabledModelCount = useMemo(() => {
    const ids = new Set();
    endpoints
      .filter(endpoint => endpoint.enabled)
      .forEach(endpoint => activeModelIdsForEndpoint(endpoint).forEach(id => ids.add(id)));
    return ids.size;
  }, [endpoints]);

  // 时间序列（小时/天/周粒度）：为每根柱提供独立可对齐的类目轴。
// 后端每个桶返回 day(bucket label) + count/tokens/avgLatency/errors，仅用于柱状展示。
const TrendBarChart = memo(function TrendBarChart({
  labels,
  values,
  color,
  isDarkMode,
  formatValue = value => (Number.isFinite(Number(value)) ? String(Number(value)) : String(value)),
  formatAxis = formatValue,
}) {
  const options = useMemo(() => {
    if (!labels || labels.length === 0) return null;
    const axisColor = kumoHex('--color-kumo-contrast');
    const gridColor = kumoHex('--color-kumo-line');
    return {
      grid: { left: 8, right: 12, top: 10, bottom: 0, containLabel: true },
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: 'shadow' },
        appendTo: 'body',
        backgroundColor: kumoHex('--color-kumo-base'),
        textStyle: { color: axisColor, fontSize: 11 },
        valueFormatter: formatValue,
      },
      xAxis: {
        type: 'category',
        data: labels,
        boundaryGap: false,
        axisLine: { lineStyle: { color: gridColor } },
        axisTick: { show: false },
        axisLabel: { color: axisColor, fontSize: 10, hideOverlap: true },
      },
      yAxis: {
        type: 'value',
        splitLine: { lineStyle: { color: gridColor } },
        axisLabel: { color: axisColor, fontSize: 10, formatter: formatAxis },
      },
      series: [
        {
          type: 'bar',
          data: values,
          barMaxWidth: 26,
          itemStyle: { color, borderRadius: [2, 2, 0, 0] },
        },
      ],
    };
  }, [labels, values, color, isDarkMode, formatValue, formatAxis]);

  if (!labels || labels.length === 0) return null;

  return <Chart echarts={echarts} isDarkMode={isDarkMode} options={options} height={168} />;
});

// 全宽「模型 × 时间」折线趋势：类别轴（每桶唯一刻度），稀疏段断线成 Trend；
// 顶部图例按调用次数降序，颜色与折线同一份映射，点击隔离/恢复。
const ModelTrendChart = memo(function ModelTrendChart({ labels, series, isDarkMode }) {
  const containerRef = useRef(null);
  const chartRef = useRef(null);
  const [hiddenSeries, setHiddenSeries] = useState({});

  // 排序必须完全确定：调用次数降序、相同次数按模型名升序，避免图例顺序
  // 随接口返回顺序（后端 map 迭代随机）漂移；颜色在排序后按固定位次分配，
  // 保证同一模型始终同色。
  const ordered = useMemo(() => {
    const withMeta = (series || []).map(item => ({
      model: item.model,
      total: (item.data || []).reduce((sum, value) => sum + (Number(value) || 0), 0),
      values: (item.data || []).map(value => Number(value) || 0),
    }));
    withMeta.sort((a, b) => b.total - a.total || (a.model < b.model ? -1 : a.model > b.model ? 1 : 0));
    return withMeta.map((item, index) => ({
      ...item,
      color: ChartPalette.categorical(index, isDarkMode),
    }));
  }, [series, isDarkMode]);

  const visibleSeries = useMemo(
    () => ordered.filter(item => !hiddenSeries[item.model]),
    [ordered, hiddenSeries]
  );

  // 图表实例只在挂载时初始化一次；labels/系列的更新全部走 setOption，
  // 否则 30 秒自动刷新带来的新数组引用会触发 dispose+重建，造成闪烁与交互状态丢失。
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const chart = echarts.init(el);
    chartRef.current = chart;
    const observer = new ResizeObserver(() => chart.resize());
    observer.observe(el);
    return () => {
      observer.disconnect();
      chart.dispose();
      chartRef.current = null;
    };
  }, []);

  useEffect(() => {
    const chart = chartRef.current;
    if (!chart || !labels || labels.length === 0) return;
    const axisColor = kumoHex('--color-kumo-contrast');
    const gridColor = kumoHex('--color-kumo-line');
    chart.setOption(
      {
        grid: { left: 8, right: 12, top: 8, bottom: 0, containLabel: true },
        tooltip: {
          trigger: 'axis',
          traceHigh: true,
          // 挂到 body：LayerCard 自带 overflow-hidden，悬浮框默认渲染在图表
          // 容器内会被卡片裁剪遮挡。
          appendTo: 'body',
          backgroundColor: kumoHex('--color-kumo-base'),
          textStyle: { color: axisColor, fontSize: 11 },
        },
        xAxis: {
          type: 'category',
          data: labels,
          boundaryGap: false,
          axisLine: { lineStyle: { color: gridColor } },
          axisTick: { show: false },
          axisLabel: { color: axisColor, fontSize: 10, hideOverlap: true },
        },
        yAxis: {
          type: 'value',
          splitLine: { lineStyle: { color: gridColor } },
          axisLabel: { color: axisColor, fontSize: 10 },
        },
        series: visibleSeries.map(item => ({
          type: 'line',
          name: item.model,
          data: item.values,
          smooth: true,
          connectNulls: false,
          showSymbol: true,
          symbolSize: 4,
          lineStyle: { width: 2, color: item.color },
          itemStyle: { color: item.color },
          areaStyle: { opacity: 0 },
        })),
      },
      // replaceMerge：隐藏/恢复系列时按 name 精确替换，避免默认 merge 模式下
      //「新 series 数组变短 → 按 index 合并」导致被隐藏的旧系列残留、颜色错位。
      { replaceMerge: ['series'] }
    );
  }, [labels, visibleSeries, isDarkMode]);

  const handleClick = name => {
    setHiddenSeries(prev => {
      const isIsolated = ordered.every(
        item => (item.model === name ? !prev[item.model] : prev[item.model])
      );
      const next = {};
      for (const item of ordered) {
        next[item.model] = isIsolated ? false : item.model !== name;
      }
      return next;
    });
  };

  return (
    <div className="flex min-h-0 flex-col gap-2">
      <div className="flex flex-nowrap gap-x-4 overflow-x-auto overscroll-x-contain px-1 pb-1 touch-pan-x scrollbar-thin">
        {ordered.map(item => (
          <ChartLegend.LargeItem
            key={item.model}
            name={item.model}
            color={item.color}
            value={item.total.toLocaleString('en-US', { useGrouping: false })}
            unit="次"
            inactive={hiddenSeries[item.model] ?? false}
            onClick={() => handleClick(item.model)}
          />
        ))}
      </div>
      <div ref={containerRef} className="h-[280px] w-full" />
    </div>
  );
});

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
      tokens: {
        ...build(ChartPalette.categorical(1, isDarkMode), p => p.tokens, '词元 (M)'),
        formatValue: formatTokensM,
        formatAxis: value => `${Math.round(Number(value) / 1e6)}M`,
      },
      latency: {
        ...build(ChartPalette.categorical(2, isDarkMode), p => p.avgLatency, '平均延迟 (s)'),
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
    return { labels, tsValues, models };
  }, [analyticsCharts, trendSeries]);

  const defaultGatewayKey = useMemo(
    () => gatewayKeys.find(key => key.isDefault) || gatewayKeys[0] || null,
    [gatewayKeys]
  );

  useEffect(() => {
    if (activeTab === 'endpoints' || activeTab === 'keys') {
      loadGatewayKeys();
    }
  }, [activeTab, loadGatewayKeys]);

  useEffect(() => {
    if (endpoints.length === 0) {
      setSelectedEndpointId('');
      return;
    }
    if (!endpoints.some(endpoint => endpoint.id === selectedEndpointId)) {
      setSelectedEndpointId(endpoints[0].id);
    }
  }, [endpoints, selectedEndpointId]);

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
    if (endpointToggleLoading[endpoint.id]) return;
    const updatedEnabled = !endpoint.enabled;
    setEndpointToggleLoading(prev => ({ ...prev, [endpoint.id]: true }));
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled: updatedEnabled }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '未知错误');

      const confirmedEnabled = Boolean(data.enabled);
      setEndpoints(prev =>
        prev.map(e => (e.id === endpoint.id ? { ...e, enabled: confirmedEnabled } : e))
      );
      const endpointName = endpoint.name || '端点';
      toast.success(confirmedEnabled ? `${endpointName} 已启用` : `${endpointName} 已停用`);
      await loadAllModels(true);
    } catch (error) {
      toast.error('操作失败: ' + error.message);
    } finally {
      setEndpointToggleLoading(prev => ({ ...prev, [endpoint.id]: false }));
    }
  };

  // 保存端点路由优先级/权重：PUT /api/openai/endpoints/:id/routing（照搬模型映射模式）。
  const saveEndpointRouting = async (endpointId, field, value) => {
    setRoutingEditKey(null);
    const payload = field === 'priority' ? { priority: value } : { weight: value };
    try {
      const res = await fetch(`/api/openai/endpoints/${endpointId}/routing`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify(payload),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || !data.success) throw new Error(data.error || '保存失败');
      setEndpoints(prev =>
        prev.map(e =>
          e.id === endpointId
            ? {
                ...e,
                priority: typeof data.priority === 'number' ? data.priority : e.priority,
                weight: typeof data.weight === 'number' ? data.weight : e.weight,
              }
            : e
        )
      );
    } catch (error) {
      toast.error('路由设置保存失败: ' + error.message);
    }
  };

  // 端点列表拖拽排序：本地先更新顺序，再持久化到后端；失败时回滚。
  const saveEndpointOrder = async nextEndpoints => {
    const orderedIds = nextEndpoints.map(ep => ep.id);
    setEndpointReorderSaving(true);
    try {
      const response = await fetch('/api/openai/endpoints/reorder', {
        method: 'POST',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ endpointIds: orderedIds }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '保存失败');
      toast.success('端点顺序已保存');
    } catch (error) {
      toast.error('排序保存失败: ' + error.message);
      await loadEndpoints(true);
    } finally {
      setEndpointReorderSaving(false);
    }
  };

  const handleEndpointDragStart = (item, event) => {
    // 如果拖拽起点是输入框，忽略拖拽（避免 priority/weight inline 编辑时误触）。
    if (event.target && event.target.tagName === 'INPUT') return;
    setDraggedEndpointId(String(item.id));
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', String(item.id));
  };

  const handleEndpointDragOver = event => {
    if (!draggedEndpointId) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = 'move';
  };

  const handleEndpointDrop = async (targetItem, event) => {
    event.preventDefault();
    const sourceId = draggedEndpointId || event.dataTransfer.getData('text/plain');
    setDraggedEndpointId(null);
    if (!sourceId || String(sourceId) === String(targetItem.id)) return;
    const fromIndex = endpoints.findIndex(ep => String(ep.id) === String(sourceId));
    const toIndex = endpoints.findIndex(ep => String(ep.id) === String(targetItem.id));
    if (fromIndex < 0 || toIndex < 0) return;
    const next = [...endpoints];
    const [moved] = next.splice(fromIndex, 1);
    next.splice(toIndex, 0, moved);
    setEndpoints(next);
    await saveEndpointOrder(next);
  };

  const handleEndpointDragEnd = () => {
    setDraggedEndpointId(null);
  };

  // 模型开关的进行中标记：ref 用于同步去重，state 用于驱动按钮禁用态渲染。
  const modelSwitchLoadingRef = useRef({});
  const [modelSwitchLoading, setModelSwitchLoading] = useState({});

  const toggleModelEnabled = async (endpoint, modelId, enabled, silent = false, skipReload = false) => {
    const key = `${endpoint.id}:${modelId}`;
    if (modelSwitchLoadingRef.current[key]) return;
    modelSwitchLoadingRef.current[key] = true;
    setModelSwitchLoading(prev => ({ ...prev, [key]: true }));
    const prevDisabled = Array.isArray(endpoint.disabledModels) ? endpoint.disabledModels : [];
    // 乐观更新：立即切换开关状态，无需等待后端往返。
    setEndpoints(prev =>
      prev.map(e =>
        e.id === endpoint.id
          ? {
              ...e,
              disabledModels: enabled
                ? (Array.isArray(e.disabledModels) ? e.disabledModels : []).filter(id => id !== modelId)
                : [
                    ...(Array.isArray(e.disabledModels) ? e.disabledModels : []).filter(
                      id => id !== modelId
                    ),
                    modelId,
                  ],
            }
          : e
      )
    );
    try {
      const response = await fetch(`/api/openai/endpoints/${endpoint.id}/models/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ model: modelId, enabled }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '更新失败');
      setEndpoints(prev =>
        prev.map(e =>
          e.id === endpoint.id
            ? { ...e, disabledModels: Array.isArray(data.disabledModels) ? data.disabledModels : [] }
            : e
        )
      );
      if (!silent) toast.success(enabled ? `${modelId} 已启用` : `${modelId} 已停用`);
      if (!skipReload) await loadAllModels(true);
    } catch (error) {
      setEndpoints(prev =>
        prev.map(e => (e.id === endpoint.id ? { ...e, disabledModels: prevDisabled } : e))
      );
      toast.error(`更新模型状态失败: ${error.message}`);
    } finally {
      modelSwitchLoadingRef.current[key] = false;
      setModelSwitchLoading(prev => ({ ...prev, [key]: false }));
    }
  };

  const modelEnabledForEndpoint = (endpoint, modelId) => {
    const disabled = Array.isArray(endpoint?.disabledModels) ? endpoint.disabledModels : [];
    return !disabled.includes(modelId);
  };

  const openAddEndpointModal = () => {
    setEditingEndpoint(null);
    setEndpointForm({
      name: '',
      baseUrl: '',
      apiKey: '',
      apiKeys: [],
      notes: '',
      headers: [],
      proxyPool: [],
      proxyBatches: [],
      autoSwitch: false,
      proxyEnabled: false,
      allowDirectFallback: true,
      protocol: 'auto',
    });
    setEndpointFormError('');
    setEndpointFormOpen(true);
    setEndpointKeyChecks([]);
  };

  const openEditEndpointModal = endpoint => {
    setEditingEndpoint(endpoint);
    setEndpointForm({
      name: endpoint.name || '',
      baseUrl: endpoint.baseUrl || '',
      apiKey: endpoint.apiKey || '',
      apiKeys: Array.isArray(endpoint.apiKeys) ? endpoint.apiKeys : [],
      notes: endpoint.notes || '',
      headers: Array.isArray(endpoint.headers) ? endpoint.headers : [],
      proxyPool: Array.isArray(endpoint.proxyPool) ? endpoint.proxyPool : [],
      proxyBatches: Array.isArray(endpoint.proxyBatches) ? endpoint.proxyBatches : [],
      autoSwitch: Boolean(endpoint.autoSwitch),
      proxyEnabled: Boolean(endpoint.proxyEnabled),
      allowDirectFallback: !endpoint.forceProxy,
      protocol: endpoint.protocol || 'auto',
    });
    setEndpointFormError('');
    setEndpointFormOpen(true);
    setEndpointKeyChecks([]);
    checkEndpointKeys(
      [endpoint.apiKey || '', ...(Array.isArray(endpoint.apiKeys) ? endpoint.apiKeys : [])],
      endpoint.id
    );
  };

  const updateEndpointProxy = (index, value) => {
    setEndpointForm(current => {
      const proxyPool = (current.proxyPool || []).map((proxy, i) => (i === index ? value : proxy));
      return { ...current, proxyPool };
    });
  };

  const addEndpointProxy = () => {
    setEndpointForm(current => ({
      ...current,
      proxyPool: [...(current.proxyPool || []), ''],
    }));
  };

  const removeEndpointProxy = index => {
    setEndpointForm(current => ({
      ...current,
      proxyPool: (current.proxyPool || []).filter((_, i) => i !== index),
    }));
  };

  const [proxyBatchOpen, setProxyBatchOpen] = useState(false);
  const [proxyBatchText, setProxyBatchText] = useState('');
  const [proxyImportLoading, setProxyImportLoading] = useState(false);
  const [subscriptionUrlOpen, setSubscriptionUrlOpen] = useState(false);
  const [subscriptionUrl, setSubscriptionUrl] = useState('');
  // editingProxyIndex 标记当前正在编辑完整 URL 的代理条目索引；-1 表示无。
  const [editingProxyIndex, setEditingProxyIndex] = useState(-1);
  // proxyManagerOpen 控制「出口代理池」独立管理弹窗。
  const [proxyManagerOpen, setProxyManagerOpen] = useState(false);

  // manualProxyEntries：池中不属于任何导入批次的代理（手动添加），
  // 携带真实池下标，供管理弹窗内编辑与删除。
  const manualProxyEntries = useMemo(() => {
    const batchUrls = new Set((endpointForm.proxyBatches || []).flatMap(batch => batch.proxies || []));
    return (endpointForm.proxyPool || [])
      .map((proxy, index) => ({ proxy, index }))
      .filter(({ proxy }) => !batchUrls.has(proxy));
  }, [endpointForm.proxyPool, endpointForm.proxyBatches]);

  const saveProxyBatch = () => {
    const lines = proxyBatchText
      .split(/\r?\n/)
      .map(line => line.trim())
      .filter(Boolean);
    if (lines.length === 0) {
      toast.warning('请粘贴至少一个代理地址');
      return;
    }
    const added = addProxyBatch(`批量添加 ${new Date().toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}`, lines);
    if (added === 0) {
      toast.info('粘贴的代理已全部属于其他批次，无需重复添加');
      return;
    }
    setProxyBatchText('');
    setProxyBatchOpen(false);
    toast.success(`已批量添加 ${added} 个代理`);
  };

  // addProxyBatch 把一批代理（文件/订阅/批量粘贴）登记为一个「批次」：
  //   a) 池中尚不存在 → 新增并入池；
  //   b) 已在池中但无任何批次归属（历史导入/手动加入的同 URL）→ 一并记入本批次；
  //   c) 已属于其他批次 → 跳过，避免两个批次拥有同一 URL 导致删除互相牵连。
  // 这样旧数据（批次功能上线前导入的池）重新导入同一来源即可获得批次管理能力。
  // 返回本次归入批次的条数（0 表示无新增）。
  const addProxyBatch = (batchName, urls) => {
    const list = (Array.isArray(urls) ? urls : []).filter(Boolean);
    if (list.length === 0) return 0;
    const pool = endpointForm.proxyPool || [];
    const poolSet = new Set(pool);
    const owned = new Set((endpointForm.proxyBatches || []).flatMap(batch => batch.proxies || []));
    const fresh = list.filter(proxy => !poolSet.has(proxy));
    const newlyOwned = list.filter(proxy => poolSet.has(proxy) && !owned.has(proxy));
    const batchProxies = [...fresh, ...newlyOwned];
    if (batchProxies.length === 0) return 0;
    const batchId = `pb_${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`;
    setEndpointForm(current => ({
      ...current,
      proxyPool: [...pool, ...fresh],
      proxyBatches: [
        ...(current.proxyBatches || []),
        {
          id: batchId,
          name: batchName,
          createdAt: new Date().toISOString(),
          proxies: batchProxies,
        },
      ],
    }));
    return batchProxies.length;
  };

  // 文件导入：读取本地代理列表文件（.txt，每行一个代理），交给后端解析清洗后，
  // 以文件为单位建立一个「批次」追加到池，便于之后按文件批量删除/管理。
  const proxyFileInputRef = useRef(null);
  const importProxyFile = file => {
    if (!file) return;
    const reader = new FileReader();
    reader.onload = async e => {
      const text = String(e.target?.result || '');
      if (!text.trim()) {
        toast.warning('文件内容为空');
        return;
      }
      const rawLineCount = text.split(/\r?\n/).map(l => l.trim()).filter(Boolean).length;
      if (proxyImportLoading) return;
      setProxyImportLoading(true);
      try {
        const response = await fetch('/api/openai/proxies/import-list', {
          method: 'POST',
          headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
          body: JSON.stringify({ text }),
        });
        const data = await response.json().catch(() => ({}));
        if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
        const list = Array.isArray(data.proxies) ? data.proxies : [];
        if (list.length === 0) {
          toast.info('文件中没有找到可导入的代理（支持 http(s)://、socks5://、host:port）');
          return;
        }
        const batchName = file.name || '代理列表';
        const added = addProxyBatch(batchName, list);
        if (added === 0) {
          toast.info(`文件中的 ${list.length} 个代理已全部属于其他批次，无需重复导入`);
          return;
        }
        const skipped = rawLineCount - list.length;
        toast.success(
          `已导入批次「${batchName}」${added} 条${skipped > 0 ? `（跳过 ${skipped} 行无效/重复）` : ''}`,
        );
      } catch (err) {
        toast.error(err.message || '文件导入失败');
      } finally {
        setProxyImportLoading(false);
        if (proxyFileInputRef.current) proxyFileInputRef.current.value = '';
      }
    };
    reader.readAsText(file);
  };

  // 批次管理：展开预览 / 删除整批 / 移出单条。
  const [expandedBatchId, setExpandedBatchId] = useState(null);
  const [manualProxyExpanded, setManualProxyExpanded] = useState(false);
  // proxyRuntimeStates：代理池各出口的运行时禁用状态（冷却 / 429 冻结），
  // 用于在管理弹窗里把被禁用的代理标红。key 为代理 URL。
  const [proxyRuntimeStates, setProxyRuntimeStates] = useState({});
  useEffect(() => {
    if (!proxyManagerOpen || !editingEndpoint?.id) return;
    let cancelled = false;
    const load = async () => {
      try {
        const response = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state`, {
          headers: getAuthHeaders(),
        });
        const data = await response.json().catch(() => ({}));
        if (!response.ok || !Array.isArray(data.proxies)) return;
        if (!cancelled) {
          const map = {};
          data.proxies.forEach(item => {
            map[item.proxy] = item;
          });
          setProxyRuntimeStates(map);
        }
      } catch {
        // 状态加载失败不阻断弹窗使用。
      }
    };
    load();
    const timer = setInterval(load, 15000);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [proxyManagerOpen, editingEndpoint?.id]);
  // disabledProxyUntil 返回某代理的禁用信息：{label, disableUntil} 或 null（可用）。
  const disabledProxyUntil = proxy => {
    const item = proxyRuntimeStates[proxy];
    if (!item) return null;
    if (item.rateLimitedUntil && new Date(item.rateLimitedUntil).getTime() > Date.now()) {
      return { label: `429 冻结至 ${formatDateTime(item.rateLimitedUntil)}`, until: item.rateLimitedUntil };
    }
    if (item.cooldownUntil && new Date(item.cooldownUntil).getTime() > Date.now()) {
      return { label: `连接失败冷却至 ${formatDateTime(item.cooldownUntil)}`, until: item.cooldownUntil };
    }
    if (item.sunkUntil && new Date(item.sunkUntil).getTime() > Date.now()) {
      return { label: `坏代理沉淀至 ${formatDateTime(item.sunkUntil)}`, until: item.sunkUntil };
    }
    return null;
  };
  // disabledProxyCount 返回当前被禁用（冷却/冻结/沉淀）的代理条数。
  const disabledProxyCount = useMemo(() => {
    if (!editingEndpoint?.id) return 0;
    return (endpointForm.proxyPool || []).filter(proxy => disabledProxyUntil(proxy)).length;
  }, [endpointForm.proxyPool, proxyRuntimeStates]);
  // unbanAllProxies 一键解封端点代理池全部出口：清除冷却/429 冻结/坏代理沉淀，
  // 解封后重新拉取运行时状态使 UI 同步。
  const [unbanningProxies, setUnbanningProxies] = useState(false);
  const unbanAllProxies = async () => {
    if (!editingEndpoint?.id || unbanningProxies) return;
    setUnbanningProxies(true);
    try {
      const response = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state/unban`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || `HTTP ${response.status}`);
      toast.success(data.cleared ? `已解封 ${data.cleared} 条代理` : '代理池无被禁用的出口');
      const stateRes = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state`, {
        headers: getAuthHeaders(),
      });
      const stateData = await stateRes.json().catch(() => ({}));
      if (stateRes.ok && Array.isArray(stateData.proxies)) {
        const map = {};
        stateData.proxies.forEach(item => {
          map[item.proxy] = item;
        });
        setProxyRuntimeStates(map);
      }
    } catch (error) {
      toast.error('解封失败: ' + error.message);
    } finally {
      setUnbanningProxies(false);
    }
  };
  // probeAllProxies 对端点代理池全部出口发起一次手动探活，完成后刷新运行时状态。
  const [probingProxies, setProbingProxies] = useState(false);
  const probeAllProxies = async () => {
    if (!editingEndpoint?.id || probingProxies) return;
    setProbingProxies(true);
    try {
      const response = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state/probe`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || `HTTP ${response.status}`);
      toast.success(data.probed ? `已探测 ${data.probed} 条代理，可达 ${data.reachable} 条` : '代理池为空');
      const stateRes = await fetch(`/api/openai/endpoints/${editingEndpoint.id}/proxy-state`, {
        headers: getAuthHeaders(),
      });
      const stateData = await stateRes.json().catch(() => ({}));
      if (stateRes.ok && Array.isArray(stateData.proxies)) {
        const map = {};
        stateData.proxies.forEach(item => {
          map[item.proxy] = item;
        });
        setProxyRuntimeStates(map);
      }
    } catch (error) {
      toast.error('批量测试失败: ' + error.message);
    } finally {
      setProbingProxies(false);
    }
  };
  const removeProxyBatch = batch => {
    if (!confirmPress(`proxy-batch:${batch.id}`, `移除文件批次「${batch.name}」及其全部 ${batch.proxies.length} 条代理？`)) return;
    const members = new Set(batch.proxies || []);
    setEndpointForm(current => ({
      ...current,
      proxyPool: (current.proxyPool || []).filter(proxy => !members.has(proxy)),
      proxyBatches: (current.proxyBatches || []).filter(item => item.id !== batch.id),
    }));
    toast.success(`已移除批次「${batch.name}」的 ${batch.proxies.length} 条代理`);
  };
  const removeProxyFromBatch = (batch, proxy) => {
    setEndpointForm(current => {
      const batches = (current.proxyBatches || [])
        .map(item =>
          item.id === batch.id
            ? { ...item, proxies: (item.proxies || []).filter(p => p !== proxy) }
            : item,
        )
        .filter(item => item.id !== batch.id || (item.proxies || []).length > 0);
      return {
        ...current,
        proxyPool: (current.proxyPool || []).filter(item => item !== proxy),
        proxyBatches: batches,
      };
    });
  };

  // resolveSubscriptionProxies 通过后端拉取并解析订阅链接中的 socks/http 节点。
  const resolveSubscriptionProxies = async () => {
    const url = subscriptionUrl.trim();
    if (!url) {
      toast.warning('请填写订阅链接');
      return;
    }
    if (proxyImportLoading) return;
    setProxyImportLoading(true);
    try {
      const response = await fetch('/api/openai/proxies/resolve-subscription', {
        method: 'POST',
        headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ url }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
      const list = Array.isArray(data.proxies) ? data.proxies : [];
      if (list.length === 0) {
        toast.info(data.message || '订阅内容中没有找到 socks/http 节点');
        return;
      }
      let batchName = url;
      try {
        batchName = `订阅 ${new URL(url).hostname}`;
      } catch {
        // URL 解析失败时退化为完整链接。
      }
      const added = addProxyBatch(batchName, list.map(item => item.proxy).filter(Boolean));
      if (added === 0) {
        toast.info('订阅链接中的代理已全部属于其他批次，无需重复导入');
      } else {
        toast.success(`已从订阅链接导入 ${added} 个代理`);
      }
      setSubscriptionUrl('');
      setSubscriptionUrlOpen(false);
    } catch (error) {
      toast.error('解析订阅链接失败: ' + error.message);
    } finally {
      setProxyImportLoading(false);
    }
  };

  const updateEndpointHeader = (index, field, value) => {
    setEndpointForm(current => {
      const headers = (current.headers || []).map((header, i) =>
        i === index ? { ...header, [field]: value } : header
      );
      return { ...current, headers };
    });
  };

  const addEndpointHeader = () => {
    setEndpointForm(current => ({
      ...current,
      headers: [...(current.headers || []), { name: '', value: '' }],
    }));
  };

  const removeEndpointHeader = index => {
    setEndpointForm(current => ({
      ...current,
      headers: (current.headers || []).filter((_, i) => i !== index),
    }));
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
      const apiKeys = (endpointForm.apiKeys || []).map(k => (k || '').trim()).filter(Boolean);
      const response = await fetch(url, {
        method: editingEndpoint ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ ...endpointForm, apiKeys, forceProxy: !endpointForm.allowDirectFallback }),
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

  // 对端点多 key 逐个做有效性检测（调后端 GET /models 判定）。keysArray 与弹窗行对齐：
  // 第 0 项=主 key（K1），后续项=备用 key（K2...），空值行跳过但仍占据对应下标。
  const checkEndpointKeys = useCallback(async (keysArray, endpointId) => {
    const rows = keysArray.map(k => (k || '').trim());
    const entries = rows
      .map((key, rowIndex) => ({ rowIndex, key }))
      .filter(e => e.key !== '');
    if (!endpointId) {
      return;
    }
    if (entries.length === 0) {
      setEndpointKeyChecks(Array(rows.length).fill(null));
      return;
    }
    setEndpointKeyChecking(true);
    setEndpointKeyChecks(rows.map(() => ({ status: 'checking' })));
    try {
      const response = await fetch(`/api/openai/endpoints/${endpointId}/key-check`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          keys: entries.map(e => e.key),
          timeout: 10000,
        }),
      });
      const data = await response.json();
      if (!response.ok) {
        throw new Error(data.error || '检测失败');
      }
      const next = Array(rows.length).fill(null);
      (data.results || []).forEach((result, idx) => {
        const rowIndex = entries[idx]?.rowIndex;
        if (rowIndex != null) next[rowIndex] = result;
      });
      setEndpointKeyChecks(next);
    } catch (error) {
      setEndpointKeyChecks(rows.map(() => ({ status: 'error', message: error.message })));
      toast.error(`Key 检测失败：${error.message}`);
    } finally {
      setEndpointKeyChecking(false);
    }
  }, [getAuthHeaders]);

  const appendEndpointKey = () => {
    setEndpointForm(current => ({
      ...current,
      apiKeys: [...(current.apiKeys || []), ''],
    }));
    setEndpointKeyChecks(prev => [...prev, null]);
  };

  const removeEndpointKey = rowIndex => {
    setEndpointForm(current => {
      const keys = [current.apiKey || '', ...(current.apiKeys || [])];
      keys.splice(rowIndex, 1);
      return {
        ...current,
        apiKey: keys[0] || '',
        apiKeys: keys.slice(1),
      };
    });
    setEndpointKeyChecks(prev => {
      const next = [...prev];
      next.splice(rowIndex, 1);
      return next;
    });
  };

  const [pendingDeleteEndpointId, setPendingDeleteEndpointId] = useState(null);
  const DELETE_ENDPOINT_CONFIRM_MS = 3000;
  const deleteEndpointConfirmActive = id =>
    pendingDeleteEndpointId?.id === id && pendingDeleteEndpointId.expiresAt > Date.now();

  const deleteEndpoint = async endpoint => {
    if (!deleteEndpointConfirmActive(endpoint.id)) {
      setPendingDeleteEndpointId({ id: endpoint.id, expiresAt: Date.now() + DELETE_ENDPOINT_CONFIRM_MS });
      toast.info(`删除端点 ${endpoint.name || endpoint.baseUrl}？请再次点击确认`);
      return;
    }
    setPendingDeleteEndpointId(null);
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

  // ==================== 2. Health Checking ====================
  const [openaiModelHealth, setOpenaiModelHealth] = useState(() => {
    try {
      const saved = localStorage.getItem('openai_model_health_cache');
      return saved ? JSON.parse(saved) : {};
    } catch {
      return {};
    }
  });

  useEffect(() => {
    localStorage.setItem('openai_model_health_cache', JSON.stringify(openaiModelHealth));
  }, [openaiModelHealth]);

  const [modelHealthBatchLoading, setModelHealthBatchLoading] = useState(false);
  const [healthCheckProgress, setHealthCheckProgress] = useState(() => createHealthCheckProgress());
  const [healthCheckModal, setHealthCheckModal] = useState(false);
  const [healthCheckForm, setHealthCheckForm] = useState({
    timeout: DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS,
    concurrency: DEFAULT_MODEL_HEALTH_CONCURRENCY,
  });
  const modelHealthAbortControllersRef = useRef(new Map());
  // 批量检测进行中请求：切换端点时 abort，避免旧端点的检测状态带偏新端点。
  const batchHealthAbortRef = useRef(null);

  // 切换选中端点时立即终止该端点所有检测（单个 + 批量），并清理「检测中」状态。
  useEffect(() => {
    modelHealthAbortControllersRef.current.forEach(controller => controller.abort());
    modelHealthAbortControllersRef.current.clear();
    batchHealthAbortRef.current?.abort();
    batchHealthAbortRef.current = null;
    setModelHealthBatchLoading(false);
    setOpenaiModelHealth(prev => {
      const next = {};
      for (const [key, record] of Object.entries(prev)) {
        next[key] = record?.loading ? { ...record, loading: false } : record;
      }
      return next;
    });
  }, [selectedEndpointId]);

  const markModelsChecking = targets => {
    const checkedAt = Date.now();
    setOpenaiModelHealth(prev => {
      const next = { ...prev };
      targets.forEach(({ endpointId, modelId }) => {
        next[modelHealthKey(endpointId, modelId)] = {
          status: 'checking',
          loading: true,
          latency: null,
          checkedAt,
        };
      });
      return next;
    });
  };

  const openAddGatewayKeyModal = () => {
    setEditingGatewayKey(null);
    setGatewayKeyForm({
      name: '',
      expiresAt: '',
      allowedModels: [],
      allowedEndpoints: [],
      maxTokensQuota: '',
    });
    setGatewayKeyModelInput('');
    setGatewayKeyEndpointInput('');
    setGatewayKeyFormError('');
    setGatewayKeyDialogOpen(true);
  };

  const openEditGatewayKeyModal = key => {
    setEditingGatewayKey(key);
    setGatewayKeyForm({
      name: key.name || '',
      expiresAt: key.expiresAt ? toLocalDateTimeValue(new Date(key.expiresAt)) : '',
      allowedModels: Array.isArray(key.allowedModels) ? key.allowedModels : [],
      allowedEndpoints: Array.isArray(key.allowedEndpoints) ? key.allowedEndpoints : [],
      maxTokensQuota: key.maxTokensQuota ? String(key.maxTokensQuota) : '',
    });
    setGatewayKeyModelInput('');
    setGatewayKeyEndpointInput('');
    setGatewayKeyFormError('');
    setGatewayKeyDialogOpen(true);
  };

  const normalizeGatewayKeyForm = () => ({
    name: gatewayKeyForm.name.trim(),
    expiresAt: gatewayKeyForm.expiresAt ? new Date(gatewayKeyForm.expiresAt).toISOString() : '',
    allowedModels: Array.isArray(gatewayKeyForm.allowedModels)
      ? gatewayKeyForm.allowedModels
      : [],
    allowedEndpoints: Array.isArray(gatewayKeyForm.allowedEndpoints)
      ? gatewayKeyForm.allowedEndpoints
      : [],
    maxTokensQuota: gatewayKeyForm.maxTokensQuota
      ? Number(gatewayKeyForm.maxTokensQuota)
      : 0,
  });

  // 白名单列表项添加/删除（模型与端点共用）。
  const addGatewayKeyListItem = (field, value) => {
    const trimmed = (value || '').trim();
    if (!trimmed) return;
    setGatewayKeyForm(current => {
      const list = Array.isArray(current[field]) ? current[field] : [];
      if (list.includes(trimmed)) return current;
      return { ...current, [field]: [...list, trimmed] };
    });
    if (field === 'allowedModels') setGatewayKeyModelInput('');
    if (field === 'allowedEndpoints') setGatewayKeyEndpointInput('');
  };

  const removeGatewayKeyListItem = (field, value) => {
    setGatewayKeyForm(current => ({
      ...current,
      [field]: (Array.isArray(current[field]) ? current[field] : []).filter(item => item !== value),
    }));
  };

  // 过期时间预设：相对当前时间 +N 天，保留当天剩余时刻（23:59 或当前时分）。
  const applyGatewayKeyExpiryPreset = days => {
    setGatewayKeyForm(current => {
      if (!days) {
        return { ...current, expiresAt: '' };
      }
      const existing = parseLocalDateTime(current.expiresAt);
      const next = new Date(Date.now() + days * 24 * 60 * 60 * 1000);
      if (existing) {
        next.setHours(existing.getHours(), existing.getMinutes(), 0, 0);
      } else {
        next.setHours(23, 59, 0, 0);
      }
      return { ...current, expiresAt: toLocalDateTimeValue(next) };
    });
  };

  const updateGatewayKeyExpiryDate = date => {
    if (!date) return;
    setGatewayKeyForm(current => {
      const existing = parseLocalDateTime(current.expiresAt);
      const next = new Date(date);
      next.setHours(existing?.getHours() ?? 23, existing?.getMinutes() ?? 59, 0, 0);
      return { ...current, expiresAt: toLocalDateTimeValue(next) };
    });
  };

  const updateGatewayKeyExpiryTime = (part, value) => {
    setGatewayKeyForm(current => {
      const next = parseLocalDateTime(current.expiresAt);
      if (!next) return current;
      if (part === 'hour') next.setHours(Number(value));
      if (part === 'minute') next.setMinutes(Number(value));
      return { ...current, expiresAt: toLocalDateTimeValue(next) };
    });
  };

  const saveGatewayKey = async () => {
    const payload = normalizeGatewayKeyForm();
    if (!payload.name) {
      setGatewayKeyFormError('请填写密钥名称');
      return;
    }
    setGatewayKeySaving(true);
    setGatewayKeyFormError('');
    try {
      const response = await fetch(
        editingGatewayKey ? `/api/openai/keys/${editingGatewayKey.id}` : '/api/openai/keys',
        {
          method: editingGatewayKey ? 'PUT' : 'POST',
          headers: getAuthHeaders(),
          body: JSON.stringify(payload),
        }
      );
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '保存失败');
      setGatewayKeyDialogOpen(false);
      if (data.apiKey) {
        setNewGatewayKey({ name: payload.name, apiKey: data.apiKey });
      }
      toast.success(editingGatewayKey ? '密钥已更新' : '密钥已创建');
      await loadGatewayKeys();
    } catch (error) {
      setGatewayKeyFormError(error.message);
    } finally {
      setGatewayKeySaving(false);
    }
  };

  const toggleGatewayKey = async key => {
    if (gatewayKeyToggleLoading[key.id]) return;
    const nextEnabled = !key.enabled;
    setGatewayKeyToggleLoading(prev => ({ ...prev, [key.id]: true }));
    try {
      const response = await fetch(`/api/openai/keys/${key.id}/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled: nextEnabled }),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '更新失败');
      const confirmedEnabled = Boolean(data.enabled);
      setGatewayKeys(prev =>
        prev.map(item => (item.id === key.id ? { ...item, enabled: confirmedEnabled } : item))
      );
      toast.success(confirmedEnabled ? `${key.name} 已启用` : `${key.name} 已停用`);
    } catch (error) {
      toast.error('更新密钥状态失败: ' + error.message);
    } finally {
      setGatewayKeyToggleLoading(prev => ({ ...prev, [key.id]: false }));
    }
  };

  const setDefaultGatewayKey = async key => {
    try {
      const response = await fetch(`/api/openai/keys/${key.id}/default`, {
        method: 'PUT',
        headers: getAuthHeaders(),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '设置默认密钥失败');
      toast.success(`已将 "${key.name}" 设为默认密钥`);
      await loadGatewayKeys();
    } catch (error) {
      toast.error('设置默认密钥失败: ' + error.message);
    }
  };

  const rotateGatewayKey = async key => {
    if (!(await dialog.confirm(`确认轮换 "${key.name}"？旧密钥会立即失效。`))) return;
    try {
      const response = await fetch(`/api/openai/keys/${key.id}/rotate`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '轮换失败');
      setNewGatewayKey({ name: key.name, apiKey: data.apiKey });
      toast.success('密钥已轮换');
      await loadGatewayKeys();
    } catch (error) {
      toast.error('轮换密钥失败: ' + error.message);
    }
  };

  const deleteGatewayKey = async key => {
    if (!confirmPress(`gateway-key-${key.id}`, `删除网关密钥「${key.name}」`)) return;
    try {
      const response = await fetch(`/api/openai/keys/${key.id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '删除失败');
      toast.success('密钥已删除');
      await loadGatewayKeys();
    } catch (error) {
      toast.error('删除密钥失败: ' + error.message);
    }
  };

  const applyEndpointHealthResults = (endpointId, modelIds, records, fallbackError) => {
    const recordsByModel = new Map(
      (Array.isArray(records) ? records : []).map(record => [
        String(record?.model || '').trim(),
        record,
      ])
    );
    const results = modelIds.map(modelId =>
      normalizeModelHealthRecord(recordsByModel.get(modelId), fallbackError)
    );

    setOpenaiModelHealth(prev => {
      const next = { ...prev };
      modelIds.forEach((modelId, index) => {
        next[modelHealthKey(endpointId, modelId)] = results[index];
      });
      return next;
    });

    return results;
  };

  const testModelHealth = async (model, targetEndpointId, silentToast = false) => {
    const modelId = String(model?.id || '').trim();
    if (!modelId || !targetEndpointId) return null;
    const healthKey = modelHealthKey(targetEndpointId, modelId);
    const activeController = modelHealthAbortControllersRef.current.get(healthKey);
    if (activeController) {
      activeController.abort();
      modelHealthAbortControllersRef.current.delete(healthKey);
      setOpenaiModelHealth(prev => ({
        ...prev,
        [healthKey]: {
          status: 'cancelled',
          loading: false,
          latency: null,
          checkedAt: Date.now(),
          error: '检测已停止',
        },
      }));
      if (!silentToast) toast.warning(`${modelId} 检测已停止`);
      return null;
    }

    const controller = new AbortController();
    modelHealthAbortControllersRef.current.set(healthKey, controller);

    markModelsChecking([{ endpointId: targetEndpointId, modelId }]);

    try {
      const response = await fetch(
        `/api/openai/endpoints/${encodeURIComponent(targetEndpointId)}/health-check`,
        {
          method: 'POST',
          headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
          signal: controller.signal,
          body: JSON.stringify({
            model: modelId,
            timeout: Math.max(1, Number(healthCheckForm.timeout) || DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS) * 1000,
          }),
        }
      );
      const data = await response.json().catch(() => ({}));
      if (!response.ok) {
        throw new Error(data.error || `HTTP ${response.status}`);
      }

      const result = applyEndpointHealthResults(targetEndpointId, [modelId], [data])[0];
      if (!silentToast) {
        if (result.status === 'healthy') {
          toast.success(`${modelId} 可用，延迟 ${result.latency ?? '-'} ms`);
        } else if (result.status === 'degraded') {
          toast.warning(`${modelId} 响应较慢，延迟 ${result.latency ?? '-'} ms`);
        } else {
          toast.error(`${modelId} 检测失败: ${result.error || '未知错误'}`);
        }
      }
      return result;
    } catch (e) {
      if (controller.signal.aborted) return null;
      const result = applyEndpointHealthResults(targetEndpointId, [modelId], [], e.message)[0];
      if (!silentToast) toast.error(`${modelId} 检测失败: ${result.error || e.message}`);
      return result;
    } finally {
      if (modelHealthAbortControllersRef.current.get(healthKey) === controller) {
        modelHealthAbortControllersRef.current.delete(healthKey);
      }
    }
  };

  // 批量检测：前端并发逐模型发请求，每个完成立即回填状态（无需等全部完成）。
  const runBatchHealthCheckRequest = async (targets, fallbackMessage) => {
    if (!Array.isArray(targets) || targets.length === 0) return [];
    const concurrency = resolveModelHealthConcurrency(healthCheckForm.concurrency, targets.length);
    markModelsChecking(targets);
    const results = new Array(targets.length);
    let cursor = 0;
    const workers = Array.from({ length: concurrency }, async () => {
      while (true) {
        const index = cursor;
        cursor += 1;
        if (index >= targets.length) return;
        const target = targets[index];
        // silentToast=true：每个模型的 toast 由批量结果统一汇总，避免刷屏。
        const result = await testModelHealth(
          { id: target.modelId },
          target.endpointId,
          true
        );
        results[index] =
          result ||
          normalizeModelHealthRecord(
            { status: 'failed', error: '检测未返回结果', checkedAt: Date.now() },
            '检测未返回结果'
          );
      }
    });
    await Promise.all(workers);
    return results;
  };

  const startBatchHealthCheck = async () => {
    const endpointTargets = endpoints.filter(
      endpoint => endpoint.enabled && endpointModelIds(endpoint).length > 0
    );
    const allTargets = modelHealthTargets(endpointTargets);
    if (allTargets.length === 0) {
      toast.warning('没有找到任何启用的端点或模型');
      return;
    }

    setHealthCheckModal(false);
    setModelHealthBatchLoading(true);
    setHealthCheckProgress(createHealthCheckProgress(allTargets.length, true));
    const concurrency = resolveModelHealthConcurrency(
      healthCheckForm.concurrency,
      allTargets.length
    );
    toast.info(`正在按 ${concurrency} 并发批量检测 ${allTargets.length} 个模型...`);

    try {
      const results = await runBatchHealthCheckRequest(allTargets, '批量检测失败');
      const counts = countModelHealthResults(results);
      setHealthCheckProgress({
        running: false,
        total: allTargets.length,
        completed: results.length,
        ...counts,
      });

      const message = `检测完成：可用 ${counts.healthy}，较慢 ${counts.degraded}，失败 ${counts.failed}`;
      if (counts.failed > 0) toast.warning(message);
      else toast.success(message);
    } catch {
      // 错误已在单模型检测内提示，此处仅终止流程。
    } finally {
      setModelHealthBatchLoading(false);
    }
  };

  const openHealthCheckForEndpoint = async endpointId => {
    const ep = endpoints.find(e => e.id === endpointId);
    const modelIds = endpointModelIds(ep);
    if (!ep || modelIds.length === 0) {
      toast.warning('该端点无可用模型');
      return;
    }

    setModelHealthBatchLoading(true);
    setHealthCheckProgress(createHealthCheckProgress(modelIds.length, true));
    const concurrency = resolveModelHealthConcurrency(healthCheckForm.concurrency, modelIds.length);
    toast.info(
      `正在按 ${concurrency} 并发批量检测 ${ep.name || '端点'} 的 ${modelIds.length} 个模型...`
    );

    try {
      const targets = modelIds.map(modelId => ({ endpointId, modelId }));
      const results = await runBatchHealthCheckRequest(targets, '端点检测失败');
      const counts = countModelHealthResults(results);
      setHealthCheckProgress({
        running: false,
        total: modelIds.length,
        completed: results.length,
        ...counts,
      });
      const message = `${ep.name || '端点'}：可用 ${counts.healthy}，较慢 ${counts.degraded}，失败 ${counts.failed}`;
      if (counts.failed > 0) toast.warning(message);
      else toast.success(message);
    } catch {
      // 错误已在单模型检测内提示，此处仅终止流程。
    } finally {
      setModelHealthBatchLoading(false);
    }
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

  const failedModelIdsForEndpoint = endpoint => {
    if (!endpoint) return [];
    return endpointModelIds(endpoint).filter(
      modelId => openaiModelHealth[modelHealthKey(endpoint.id, modelId)]?.status === 'error'
    );
  };

  const [modelBatchActionLoading, setModelBatchActionLoading] = useState(false);

  // 模型映射（对外名称）行内编辑状态。
  const [mappingEditKey, setMappingEditKey] = useState(null);
  const [mappingDraft, setMappingDraft] = useState('');

  // 端点路由优先级/权重行内编辑状态（照搬模型映射的编辑模式）。
  const [routingEditKey, setRoutingEditKey] = useState(null); // `${endpointId}:priority|weight`
  const [routingDraft, setRoutingDraft] = useState('');

  // 批量切换端点模型的启用状态（原子接口，避免并发逐个 toggle 丢失）。
  const batchToggleEndpointModels = async (endpoint, modelIds, enabled, successMessage) => {
    if (modelBatchActionLoading) return;
    const ids = Array.from(new Set((modelIds || []).filter(Boolean)));
    if (ids.length === 0) return;
    setModelBatchActionLoading(true);
    try {
      const response = await fetch(
        `/api/openai/endpoints/${endpoint.id}/models/toggle-batch`,
        {
          method: 'POST',
          headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
          body: JSON.stringify({ models: ids, enabled }),
        }
      );
      const data = await response.json().catch(() => ({}));
      if (!response.ok || !data.success) throw new Error(data.error || '更新失败');
      setEndpoints(prev =>
        prev.map(e =>
          e.id === endpoint.id
            ? { ...e, disabledModels: Array.isArray(data.disabledModels) ? data.disabledModels : [] }
            : e
        )
      );
      await loadAllModels(true);
      toast.success(successMessage || `已${enabled ? '启用' : '关闭'} ${ids.length} 个模型`);
      return true;
    } catch (error) {
      toast.error(`批量更新失败: ${error.message}`);
      return false;
    } finally {
      setModelBatchActionLoading(false);
    }
  };

  // 关闭端点上所有「非有效」模型（未检测/检测失败/较慢之外的），仅停用不隐藏。
  // 检测为有效的模型（healthy/degraded）不在此列，由每行手动开关控制。
  const batchCloseNonHealthyModels = async endpoint => {
    if (modelBatchActionLoading) return;
    const targets = endpointModelIds(endpoint).filter(modelId => {
      if (!modelEnabledForEndpoint(endpoint, modelId)) return false;
      const health = openaiModelHealth[modelHealthKey(endpoint.id, modelId)];
      return health?.status !== 'healthy' && health?.status !== 'degraded';
    });
    if (targets.length === 0) {
      toast.info('当前没有可批量关闭的模型（非有效模型均为空）');
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
      toast.info('当前端点没有检测失败的模型');
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
      toast.info('当前没有检测失败的模型');
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

  // 清空全部网关日志数据。
  const clearGatewayLogs = async () => {
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
  };

  // 保存模型映射：PUT /api/openai/endpoints/:id/model-mappings。
  const saveEndpointMapping = async (endpoint, modelId, alias) => {
    setMappingEditKey(null);
    const clean = (alias || '').trim();
    try {
      const res = await fetch(`/api/openai/endpoints/${endpoint.id}/model-mappings`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          mappings: { ...(endpoint.modelMappings || {}), [modelId]: clean },
        }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok || !data.success) throw new Error(data.error || '保存失败');
      toast.success(clean ? `已映射 ${modelId} → ${clean}` : `已清除 ${modelId} 的映射`);
      setEndpoints(prev =>
        prev.map(e => (e.id === endpoint.id ? { ...e, modelMappings: data.modelMappings } : e))
      );
      await loadAllModels(true);
    } catch (error) {
      toast.error('保存映射失败: ' + error.message);
    }
  };

  // 批量启用被停用的模型（与「关闭检测失败的模型」拆分为两个明确动作）。
  const batchEnableDisabledModels = async endpoint => {
    if (modelBatchActionLoading) return;
    const disabled = Array.isArray(endpoint.disabledModels) ? endpoint.disabledModels : [];
    if (disabled.length === 0) return;
    await batchToggleEndpointModels(endpoint, disabled, true, `已启用 ${disabled.length} 个被停用的模型`);
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
    if (!confirmPress(`persona-${personaId}`, '删除这个 AI 人设')) {
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
    if (!confirmPress(`session-${sessionId}`, '删除这个对话')) return;
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
    if (!confirmPress('batch-sessions', `删除选中的 ${selectedSessionIds.length} 个对话`)) return;
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
    if (!confirmPress('clear-sessions', '清空所有会话历史')) return;
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

if (!response.ok) {
        let errText = `HTTP 错误 ${response.status}`;
        try {
          const json = await response.json();
          errText = json.error?.message || json.message || JSON.stringify(json);
        } catch {}
        const retryable = response.status === 429 || response.status === 503;
        if (retryable) {
          toast.error(errText || '网关繁忙，请稍后重试');
          throw Object.assign(new Error(errText || '网关繁忙，请稍后重试'), { retryable: true });
        }
        throw new Error(errText);
      }
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
        chatStorage.saveSessions([session, ...chatStorage.readSessions()]);
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
        // 网关无可用渠道（429 限流 / 全部端点耗尽）属于可重试状态：
        // 不打断对话、不插入错误占位，仅提示用户稍后重试。
        const retryable = response.status === 429 || response.status === 503;
        if (retryable) {
          toast.error(errText || '网关繁忙，请稍后重试');
          throw Object.assign(new Error(errText || '网关繁忙，请稍后重试'), { retryable: true });
        }
        throw new Error(errText);
      }

      const reader = response.body.getReader();
      const decoder = new TextDecoder();
      const assistantMsg = {
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
      let streamDone = false;
      while (true) {
        let chunk;
        try {
          const { done, value } = await reader.read();
          if (done) { streamDone = true; break; }
          chunk = value;
        } catch (e) {
          // 流读取出错（如网络闪断），静默收尾，不打断对话。
          break;
        }

        buffer += decoder.decode(chunk, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
          const trimmed = line.trim();
          if (trimmed.startsWith('data:')) {
            const dataStr = trimmed.slice(5).trim();
            if (dataStr === '[DONE]') { streamDone = true; break; }
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
        if (streamDone) break;
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
      // 可重试网关错误（429/503）不插入错误占位，不清空对话，用户可直接重试。
      if (error.retryable) return;
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
      const assistantMsg = {
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
      let streamDone = false;
      while (true) {
        let chunk;
        try {
          const { done, value } = await reader.read();
          if (done) { streamDone = true; break; }
          chunk = value;
        } catch (e) {
          // 流读取出错（如网络闪断），静默收尾，不打断对话。
          break;
        }

        buffer += decoder.decode(chunk, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
          const trimmed = line.trim();
          if (trimmed.startsWith('data:')) {
            const dataStr = trimmed.slice(5).trim();
            if (dataStr === '[DONE]') { streamDone = true; break; }
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
        if (streamDone) break;
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
      return matchesSearch && matchesEndpoint;
    });
    return list;
  }, [
    allModels,
    openaiModelSearch,
    openaiSelectedEndpointId,
    endpoints,
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
      return matchesSearch && matchesEndpoint;
    });
  }, [allModels, endpoints, chatEndpoint, dropdownModelSearch]);

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
          ]}
        />
        <div className="flex min-w-0 items-center justify-end gap-2">
            {activeTab === 'analytics' && (
              <TabBarOverflowActions
                items={[
                  {
                    key: 'granularity',
                    type: 'select',
                    label: '时间粒度',
                    icon: <CalendarDotsIcon className="h-3.5 w-3.5" />,
                    value: analyticsGranularity,
                    onValueChange: val => setAnalyticsGranularity(val || 'day'),
                    options: [
                      { value: 'hour', label: '按小时' },
                      { value: 'day', label: '按天' },
                      { value: 'week', label: '按周' },
                    ],
                    selectClassName: 'w-28',
                  },
                  {
                    key: 'range',
                    type: 'select',
                    label: '分析范围',
                    icon: <CalendarDotsIcon className="h-3.5 w-3.5" />,
                    value: String(analyticsDays),
                    onValueChange: val => {
                      setAnalyticsDays(Number(val));
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
                    key: 'refresh',
                    label: '刷新',
                    icon: <RefreshCw className="w-3.5 h-3.5" />,
                    onClick: fetchAnalytics,
                    disabled: analyticsLoading,
                    loading: analyticsLoading,
                  },
                ]}
              />
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
                      label: '清除数据',
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
        <div className="flex min-h-0 flex-1 flex-col gap-2.5">
          <Input
            ref={endpointImportInputRef}
            type="file"
            accept="application/json,.json"
            aria-label="导入端点 JSON"
            className="hidden"
            onChange={importEndpointsFromFile}
          />
          <div className="flex flex-col gap-2 rounded-lg border border-kumo-line bg-kumo-base p-2 shadow-none cq-sm:flex-row cq-sm:items-center cq-sm:justify-between">
<div className="flex min-w-0 items-center gap-2">
              <ClipboardText
                size="sm"
                text={`${gatewayOrigin}/v1`}
                className="min-w-0 max-w-md flex-1 font-mono text-[0.9em]"
                tooltip={{ text: '复制 API Base URL', copiedText: '地址已复制', side: 'bottom' }}
                labels={{ copyAction: '复制 API Base URL' }}
              />
              {defaultGatewayKey?.apiKey ? (
                <ClipboardText
                  size="sm"
                  text={defaultGatewayKey.apiKey}
                  className="min-w-0 max-w-md flex-1 font-mono text-[0.9em]"
                  tooltip={{ text: '复制默认密钥', copiedText: '密钥已复制', side: 'bottom' }}
                  labels={{ copyAction: '复制默认密钥' }}
                />
              ) : (
                <span className="shrink-0 text-xs font-medium text-kumo-subtle">
                  未设置默认密钥
                </span>
              )}
            </div>
            <div className="flex shrink-0 items-center gap-1.5">
              <StatusBadge tone="neutral">
                {endpoints.filter(endpoint => endpoint.enabled).length} 个启用端点
              </StatusBadge>
              <StatusBadge tone="brand">
                {enabledModelCount} 个启用模型
              </StatusBadge>
            </div>
          </div>
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
              description="新增 OpenAI 兼容端点"
            />
          ) : (
            (() => {
              const endpoint = selectedEndpoint;
              const validStatus = endpoint.status === 'valid';
              const invalidStatus = endpoint.status === 'invalid';
              const disabledModelCount = Array.isArray(endpoint.disabledModels)
                ? endpoint.disabledModels.length
                : 0;

              return (
                <div className="grid min-w-0 gap-3 cq-lg:grid-cols-[1fr_2fr]">
                  <section className="flex min-w-0 flex-col gap-2 cq-lg:sticky cq-lg:top-[70px] cq-lg:self-start">
                    <div className="flex min-h-8 items-center justify-between gap-2 px-1">
                      <div className="flex items-center gap-2 text-xs text-kumo-subtle">
                        <Server className="h-3.5 w-3.5" />
                        <span className="font-medium text-kumo-strong">上游端点</span>
                      </div>
                      <span className="text-xs text-kumo-subtle">{endpoints.length} 个</span>
                    </div>
                    <LayerCard className="min-w-0 p-0 shadow-none">
                      <div className="overflow-x-auto overscroll-x-contain touch-pan-x scrollbar-thin">
                        <Table layout="fixed" className="min-w-[500px] text-xs">
                          <colgroup>
                            <col style={{ minWidth: 140 }} />
                            <col style={{ width: 64 }} />
                            <col style={{ width: 64 }} />
                            <col style={{ width: 64 }} />
                            <col style={{ width: 64 }} />
                          </colgroup>
                          <Table.Header sticky variant="compact">
                            <Table.Row className="h-8">
                              <Table.Head className="!px-2.5 !py-1.5">端点</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center">模型</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center" title="路由优先级（值越大越优先）">优先</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center" title="同优先级内的加权因子">权重</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center">状态</Table.Head>
                            </Table.Row>
                          </Table.Header>
                          <Table.Body>
                            {endpoints.map(item => (
                              <Table.Row
                                key={item.id}
                                variant={item.id === endpoint.id ? 'selected' : 'default'}
                                className={`h-11 cursor-pointer ${draggedEndpointId === String(item.id) ? 'opacity-40' : ''}`}
                                draggable={!endpointReorderSaving}
                                onDragStart={event => handleEndpointDragStart(item, event)}
                                onDragOver={handleEndpointDragOver}
                                onDrop={event => handleEndpointDrop(item, event)}
                                onDragEnd={handleEndpointDragEnd}
                                onClick={() => setSelectedEndpointId(item.id)}
                                onDoubleClick={() => openEditEndpointModal(item)}
                              >
                                <Table.Cell className="!px-2.5 !py-1.5">
                                  <div className="min-w-0">
                                    <div
                                      className="truncate font-semibold text-kumo-strong"
                                      title={item.name}
                                    >
                                      {item.name || '未命名端点'}
                                    </div>
                                    <div
                                      className="truncate font-mono text-[10px] text-kumo-subtle"
                                      title={item.baseUrl}
                                    >
                                      {item.baseUrl}
                                    </div>
                                  </div>
                                </Table.Cell>
                                <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-strong">
                                  {activeModelIdsForEndpoint(item).length}
                                </Table.Cell>
                                <Table.Cell className="!px-1.5 !py-1.5 text-center">
                                {routingEditKey === `${item.id}:priority` ? (
                                  <Input
                                    autoFocus
                                    size="sm"
                                    type="number"
                                    min={0}
                                    max={999}
                                    value={routingDraft}
                                    onChange={event => setRoutingDraft(event.target.value)}
                                    onKeyDown={event => {
                                      event.stopPropagation();
                                      if (event.key === 'Enter') {
                                        saveEndpointRouting(item.id, 'priority', Number(routingDraft) || 0);
                                      } else if (event.key === 'Escape') {
                                        setRoutingEditKey(null);
                                      }
                                    }}
                                    onBlur={() => {
                                      if (routingEditKey === `${item.id}:priority`) {
                                        saveEndpointRouting(item.id, 'priority', Number(routingDraft) || 0);
                                      }
                                    }}
                                    className="h-6 w-12 text-center font-mono text-[11px]"
                                  />
                                ) : (
                                  <span
                                    className="block cursor-text font-mono text-[11px] text-kumo-strong"
                                    title="双击编辑路由优先级（值越大越优先）"
                                    onDoubleClick={event => {
                                      event.stopPropagation();
                                      setRoutingDraft(String(item.priority ?? 0));
                                      setRoutingEditKey(`${item.id}:priority`);
                                    }}
                                  >
                                    {item.priority ?? 0}
                                  </span>
                                )}
                              </Table.Cell>
                              <Table.Cell className="!px-1.5 !py-1.5 text-center">
                                {routingEditKey === `${item.id}:weight` ? (
                                  <Input
                                    autoFocus
                                    size="sm"
                                    type="number"
                                    min={1}
                                    max={9999}
                                    value={routingDraft}
                                    onChange={event => setRoutingDraft(event.target.value)}
                                    onKeyDown={event => {
                                      event.stopPropagation();
                                      if (event.key === 'Enter') {
                                        saveEndpointRouting(item.id, 'weight', Number(routingDraft) || 1);
                                      } else if (event.key === 'Escape') {
                                        setRoutingEditKey(null);
                                      }
                                    }}
                                    onBlur={() => {
                                      if (routingEditKey === `${item.id}:weight`) {
                                        saveEndpointRouting(item.id, 'weight', Number(routingDraft) || 1);
                                      }
                                    }}
                                    className="h-6 w-12 text-center font-mono text-[11px]"
                                  />
                                ) : (
                                  <span
                                    className="block cursor-text font-mono text-[11px] text-kumo-strong"
                                    title="双击编辑加权因子（值越大被选中概率越高）"
                                    onDoubleClick={event => {
                                      event.stopPropagation();
                                      setRoutingDraft(String(item.weight ?? 100));
                                      setRoutingEditKey(`${item.id}:weight`);
                                    }}
                                  >
                                    {item.weight ?? 100}
                                  </span>
                                )}
                              </Table.Cell>
                                <Table.Cell className="!px-2 !py-1.5 text-center">
                                  <div
                                    className="flex justify-center"
                                    onClick={event => event.stopPropagation()}
                                  >
                                    <Switch
                                      size="sm"
                                      aria-label={item.enabled ? '停用端点' : '启用端点'}
                                      checked={item.enabled}
                                      onCheckedChange={() => toggleEndpointEnabled(item)}
                                      disabled={!!endpointToggleLoading[item.id]}
                                    />
                                  </div>
                                </Table.Cell>
                              </Table.Row>
                            ))}
                          </Table.Body>
                        </Table>
                      </div>
                    </LayerCard>
                  </section>

                  <section className="flex min-h-0 min-w-0 flex-col gap-2">
                    <div className="flex min-h-8 flex-wrap items-center justify-between gap-2 px-1">
                      <div className="flex min-w-0 items-center gap-2 text-xs">
                        <span className="truncate font-medium text-kumo-strong">
                          {endpoint.name || '未命名端点'}
                        </span>
                        <Button
                          size="sm"
                          variant="secondary"
                          aria-label="复制端点地址"
                          title={endpoint.baseUrl}
                          onClick={() => {
                            navigator.clipboard
                              .writeText(endpoint.baseUrl)
                              .then(() => toast.success('端点地址已复制'))
                              .catch(() => toast.error('复制失败'));
                          }}
                        >
                          端点
                        </Button>
                        <Button
                          size="sm"
                          variant="secondary"
                          aria-label="复制 API Key"
                          title="复制 API Key（默认首个）"
                          onClick={() => {
                            const keys = [endpoint.apiKey, ...(endpoint.apiKeys || [])].filter(Boolean);
                            navigator.clipboard
                              .writeText(keys[0] || '')
                              .then(() => toast.success(keys.length > 1 ? '已复制首个 API Key' : 'API Key 已复制'))
                              .catch(() => toast.error('复制失败'));
                          }}
                        >
                          密钥
                        </Button>
                      </div>
                      <div className="flex shrink-0 flex-wrap items-center gap-1.5">
                        <StatusBadge
                          tone={validStatus ? 'success' : invalidStatus ? 'danger' : 'neutral'}
                        >
                          {validStatus ? '有效' : invalidStatus ? '无效' : '待检测'}
                        </StatusBadge>
                        {Array.isArray(endpoint.headers) && endpoint.headers.length > 0 && (
                          <StatusBadge
                            tone="info"
                            title={(endpoint.headers || [])
                              .map(h => `${h.name}: ${h.value}`)
                              .join('\n')}
                          >
                            {endpoint.headers.length} 请求头
                          </StatusBadge>
                        )}
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary"
                          aria-label="模型健康检测"
                          onClick={() => openHealthCheckForEndpoint(endpoint.id)}
                          disabled={modelHealthBatchLoading}
                          title="模型健康检测"
                          icon={
                            modelHealthBatchLoading ? (
                              <Loader size="sm" />
                            ) : (
                              <Activity className={actionIconClass} />
                            )
                          }
                        />
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary"
                          aria-label="刷新模型列表"
                          onClick={() => refreshEndpointModels(endpoint)}
                          loading={endpoint.refreshing}
                          title="刷新模型列表"
                          icon={<RefreshCw className={actionIconClass} />}
                        />
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary"
                          aria-label="编辑端点"
                          onClick={() => openEditEndpointModal(endpoint)}
                          title="编辑端点"
                        >
                          <Edit className={actionIconClass} />
                        </Button>
                        <Button
                          shape="square"
                          size="sm"
                          variant={
                            deleteEndpointConfirmActive(endpoint.id)
                              ? 'primary'
                              : 'secondary-destructive'
                          }
                          aria-label={
                            deleteEndpointConfirmActive(endpoint.id)
                              ? `再次点击确认删除 ${endpoint.name || endpoint.baseUrl}`
                              : `删除 ${endpoint.name || endpoint.baseUrl}`
                          }
                          onClick={() => deleteEndpoint(endpoint)}
                          title={
                            deleteEndpointConfirmActive(endpoint.id)
                              ? '再次点击确认删除'
                              : '删除端点'
                          }
                        >
                          <Trash className={actionIconClass} />
                        </Button>
                      </div>
                    </div>

                    <LayerCard className="min-w-0 p-0 shadow-none">
                      <div className="overflow-x-auto overscroll-x-contain touch-pan-x scrollbar-thin">
                        <Table layout="fixed" className="min-w-[820px] text-xs">
                          <colgroup>
                            <col style={{ width: 56 }} />
                            <col style={{ width: 260 }} />
                            <col style={{ width: 150 }} />
                            <col style={{ width: 92 }} />
                            <col style={{ width: 96 }} />
                            <col style={{ width: 150 }} />
                          </colgroup>
                          <Table.Header sticky variant="compact">
                            <Table.Row className="h-8">
                              <Table.Head className="!px-2 !py-1.5 text-center">
                                <div
                                  className="flex items-center justify-center"
                                  onClick={event => event.stopPropagation()}
                                  title={
                                    disabledModelCount > 0
                                      ? `启用 ${disabledModelCount} 个被停用的模型`
                                      : '关闭所有非有效模型（检测有效的保留）'
                                  }
                                >
                                  <Switch
                                    size="sm"
                                    aria-label={
                                      disabledModelCount > 0
                                        ? `启用 ${disabledModelCount} 个被停用的模型`
                                        : '关闭所有非有效模型（检测有效的保留）'
                                    }
                                    checked={disabledModelCount === 0}
                                    onCheckedChange={checked => {
                                      if (checked) {
                                        // 从关→开：启用全部被停用的模型
                                        batchEnableDisabledModels(endpoint);
                                      } else {
                                        // 从开→关：关闭所有非有效模型
                                        batchCloseNonHealthyModels(endpoint);
                                      }
                                    }}
                                    disabled={modelBatchActionLoading || modelHealthBatchLoading}
                                  />
                                </div>
                              </Table.Head>
                              <Table.Head className="!px-2.5 !py-1.5">模型</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center">模型映射</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center">健康</Table.Head>
                              <Table.Head className="!px-2 !py-1.5 text-center">延迟</Table.Head>
                              <Table.Head className="app-table-action !px-2 !py-1.5">操作</Table.Head>
                            </Table.Row>
                          </Table.Header>
                          <Table.Body>
                            {endpoint.models && endpoint.models.length > 0 ? (
                              endpoint.models.map(model => {
                                const modelId =
                                  typeof model === 'string'
                                    ? model.trim()
                                    : (model.id || '').trim();
                                const healthKey = modelHealthKey(endpoint.id, modelId);
                                const health = openaiModelHealth[healthKey];
                                const canStopHealthCheck =
                                  health?.loading &&
                                  modelHealthAbortControllersRef.current.has(healthKey);
                                const healthCheckAnimating = !!health?.loading;
                                const healthTone = health?.loading
                                  ? 'info'
                                  : health?.status === 'healthy'
                                    ? 'success'
                                    : health?.status === 'degraded'
                                      ? 'warning'
                                      : health?.status === 'error'
                                        ? 'danger'
                                        : 'neutral';
                                const healthLabel = health?.loading
                                  ? '检测中'
                                  : health?.status === 'healthy'
                                    ? '可用'
                                    : health?.status === 'degraded'
                                      ? '较慢'
                                      : health?.status === 'error'
                                        ? '失败'
                                        : health?.status === 'cancelled'
                                          ? '已停止'
                                          : '未检测';

                                return (
                                  <Table.Row key={`${endpoint.id}:${modelId}`} className="h-9">
                                    <Table.Cell className="!px-2 !py-1.5 text-center">
                                      <div
                                        className="flex justify-center"
                                        onClick={event => event.stopPropagation()}
                                      >
                                        <Switch
                                          size="sm"
                                          aria-label={modelEnabledForEndpoint(endpoint, modelId) ? `停用 ${modelId}` : `启用 ${modelId}`}
                                          checked={modelEnabledForEndpoint(endpoint, modelId)}
                                          onCheckedChange={enabled =>
                                            toggleModelEnabled(endpoint, modelId, enabled)
                                          }
                                          disabled={!!modelSwitchLoading[`${endpoint.id}:${modelId}`]}
                                        />
                                      </div>
                                    </Table.Cell>
                                    <Table.Cell className="!px-2.5 !py-1.5">
                                      <span
                                        className="block truncate font-medium text-kumo-strong"
                                        title={modelId}
                                      >
                                        {modelId}
                                      </span>
                                    </Table.Cell>
                                    <Table.Cell className="!px-2 !py-1.5 text-center">
                                      {mappingEditKey === `${endpoint.id}:${modelId}` ? (
                                        <Input
                                          autoFocus
                                          size="sm"
                                          value={mappingDraft}
                                          onChange={event => setMappingDraft(event.target.value)}
                                          onKeyDown={event => {
                                            event.stopPropagation();
                                            if (event.key === 'Enter') {
                                              saveEndpointMapping(endpoint, modelId, mappingDraft);
                                            } else if (event.key === 'Escape') {
                                              setMappingEditKey(null);
                                            }
                                          }}
                                          onBlur={() => {
                                            if (mappingEditKey === `${endpoint.id}:${modelId}`) {
                                              saveEndpointMapping(endpoint, modelId, mappingDraft);
                                            }
                                          }}
                                          className="w-full font-mono text-[10px] text-center"
                                          placeholder="对外名称"
                                        />
                                      ) : (
                                        <span
                                          className="block cursor-text truncate font-mono text-[10px]"
                                          title="双击编辑对外映射名称"
                                          onDoubleClick={event => {
                                            event.stopPropagation();
                                            setMappingDraft(endpoint.modelMappings?.[modelId] || '');
                                            setMappingEditKey(`${endpoint.id}:${modelId}`);
                                          }}
                                        >
                                          {endpoint.modelMappings?.[modelId] ? (
                                            <span className="text-kumo-brand">
                                              {endpoint.modelMappings[modelId]}
                                            </span>
                                          ) : (
                                            <span className="text-kumo-subtle">双击设置</span>
                                          )}
                                        </span>
                                      )}
                                    </Table.Cell>
                                    <Table.Cell className="!px-2 !py-1.5 text-center">
                                      <StatusBadge tone={healthTone}>
                                        {healthLabel}
                                      </StatusBadge>
                                    </Table.Cell>
                                    <Table.Cell className="!px-2 !py-1.5 text-center font-mono text-kumo-strong">
                                      {health?.latency != null ? `${health.latency} ms` : '-'}
                                    </Table.Cell>
                                    <Table.Cell className="!px-2 !py-1.5 text-center">
                                      <div className="inline-flex gap-1">
                                        <Button
                                          shape="square"
                                          size="sm"
                                          variant={
                                            canStopHealthCheck
                                              ? 'secondary-destructive'
                                              : 'secondary'
                                          }
                                          aria-label={
                                            canStopHealthCheck
                                              ? `停止检测 ${modelId}`
                                              : `检测 ${modelId}`
                                          }
                                          onClick={() =>
                                            testModelHealth({ id: modelId }, endpoint.id)
                                          }
                                          disabled={modelHealthBatchLoading}
                                          title={
                                            health?.error ||
                                            (canStopHealthCheck
                                              ? '停止检测'
                                              : health?.loading
                                                ? '检测中'
                                                : '检测模型')
                                          }
                                          icon={
                                            healthCheckAnimating ? (
                                              <Loader size="sm" />
                                            ) : (
                                              <Activity className="h-3.5 w-3.5" />
                                            )
                                          }
                                        />
<Button
                                            shape="square"
                                            size="sm"
                                            variant="secondary"
                                            aria-label={`复制 ${modelId}`}
                                            onClick={() => {
                                              navigator.clipboard.writeText(modelId);
                                              toast.success('已复制模型名称');
                                            }}
                                            title="复制模型名称"
                                            icon={<Copy className="h-3.5 w-3.5" />}
                                          />
                                        </div>
                                      </Table.Cell>
                                    </Table.Row>
                                );
                              })
                            ) : (
                              <Table.Row>
                                <Table.Cell
                                  colSpan={6}
                                  className="py-10 text-center text-kumo-subtle"
                                >
                                  暂无模型数据，可刷新端点获取
                                </Table.Cell>
                              </Table.Row>
                            )}
                          </Table.Body>
                        </Table>
                      </div>
                    </LayerCard>
                  </section>
                </div>
              );
            })()
          )}
        </div>
      )}

      {/* ==================== 2. API 密钥 Tab ==================== */}
      {activeTab === 'keys' && (
        <div className="flex grow flex-col gap-3">
          <LayerCard className="w-full min-w-0 overflow-hidden p-0 shadow-none">
            <div className="min-w-0 overflow-x-auto scrollbar-thin">
              <Table layout="fixed" className="min-w-[1200px]">
                <colgroup>
                  <col style={{ width: 180 }} />
                  <col style={{ width: 240 }} />
                  <col style={{ width: 88 }} />
                  <col style={{ width: 150 }} />
                  <col style={{ width: 150 }} />
                  <col style={{ width: 100 }} />
                  <col style={{ width: 140 }} />
                  <col style={{ width: 152 }} />
                </colgroup>
                <Table.Header sticky variant="compact">
                  <Table.Row>
                    <Table.Head>名称</Table.Head>
                    <Table.Head className="text-center">密钥</Table.Head>
                    <Table.Head className="text-center">状态</Table.Head>
                    <Table.Head className="text-center">最近使用</Table.Head>
                    <Table.Head className="text-center">过期时间</Table.Head>
                    <Table.Head className="text-center">请求数</Table.Head>
                    <Table.Head className="text-center">Token 用量</Table.Head>
                    <Table.Head className="app-table-action">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {gatewayKeysLoading ? (
                    [...Array(3)].map((_, i) => (
                      <Table.Row key={i}>
                        <Table.Cell>
                          <SkeletonLine className="h-4 w-24" />
                        </Table.Cell>
                        <Table.Cell>
                          <SkeletonLine className="h-4 w-28" />
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <SkeletonLine className="mx-auto h-4 w-12" />
                        </Table.Cell>
                        <Table.Cell>
                          <SkeletonLine className="h-4 w-24" />
                        </Table.Cell>
                        <Table.Cell>
                          <SkeletonLine className="h-4 w-24" />
                        </Table.Cell>
                        <Table.Cell className="text-right">
                          <SkeletonLine className="ml-auto h-4 w-12" />
                        </Table.Cell>
                        <Table.Cell>
                          <SkeletonLine className="mx-auto h-4 w-24" />
                        </Table.Cell>
                      </Table.Row>
                    ))
                  ) : gatewayKeys.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={7} className="p-8 text-center text-kumo-subtle">
                        暂无网关 API 密钥
                      </Table.Cell>
                    </Table.Row>
                  ) : (
                    gatewayKeys.map(key => (
                      <Table.Row
                        key={key.id}
                        className="hover:bg-kumo-recessed/5 cursor-pointer"
                        title="双击编辑密钥"
                        onDoubleClick={event =>
                          handleEditableRowDoubleClick(event, () => openEditGatewayKeyModal(key))
                        }
                      >
                        <Table.Cell
                          className="truncate font-semibold text-kumo-strong"
                          title={key.name}
                        >
                          {key.name || '未命名密钥'}
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          {key.apiKey ? (
                            <ClipboardText
                              size="sm"
                              text={key.apiKey}
                              className="min-w-0 w-full font-mono text-[0.9em]"
                              tooltip={{ text: '复制 API Key', copiedText: 'API Key 已复制', side: 'bottom' }}
                              labels={{ copyAction: `复制 ${key.name} 的 API Key` }}
                            />
                          ) : (
                            <span className="text-sm text-kumo-subtle">轮换后可查看并复制</span>
                          )}
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <StatusBadge tone={key.enabled ? 'success' : 'neutral'}>
                            {key.enabled ? '已启用' : '已停用'}
                          </StatusBadge>
                        </Table.Cell>
                        <Table.Cell className="truncate text-center text-sm text-kumo-subtle">
                          {key.lastUsed ? formatDateTime(key.lastUsed) : '从未使用'}
                        </Table.Cell>
                        <Table.Cell className="truncate text-center text-sm text-kumo-subtle">
                          {key.expiresAt ? formatDateTime(key.expiresAt) : '永不过期'}
                        </Table.Cell>
                        <Table.Cell className="text-center font-mono text-[0.9em] text-kumo-strong">
                          {(key.requestCount || 0).toLocaleString('en-US', { useGrouping: false })}
                        </Table.Cell>
                        <Table.Cell className="text-center font-mono text-[0.9em]">
                          {key.maxTokensQuota > 0 ? (
                            <span className="inline-flex items-center gap-1">
                              <span
                                className={
                                  (key.totalTokensUsed || 0) >= key.maxTokensQuota
                                    ? 'text-kumo-danger'
                                    : 'text-kumo-strong'
                                }
                              >
{(key.totalTokensUsed || 0).toLocaleString('en-US', { useGrouping: false })}
                              </span>
                              <span className="text-kumo-subtle">
                                / {key.maxTokensQuota.toLocaleString('en-US', { useGrouping: false })}
                              </span>
                            </span>
                          ) : (
                            <span className="text-kumo-subtle">
                              {(key.totalTokensUsed || 0).toLocaleString('en-US', { useGrouping: false })}
                            </span>
                          )}
                        </Table.Cell>
                        <Table.Cell>
                          <div className="flex justify-center gap-1.5">
                            <Button
                              shape="square"
                              size="sm"
                              variant={key.enabled ? 'secondary-destructive' : 'primary'}
                              aria-label={key.enabled ? '停用密钥' : '启用密钥'}
                              onClick={() => toggleGatewayKey(key)}
                              title={key.enabled ? '停用密钥' : '启用密钥'}
                              loading={!!gatewayKeyToggleLoading[key.id]}
                              icon={<Reboot className="h-3.5 w-3.5" />}
                            />
                            <Button
                              shape="square"
                              size="sm"
                              variant={key.isDefault ? 'primary' : 'outline'}
                              aria-label={key.isDefault ? '默认密钥' : '设为默认密钥'}
                              onClick={() => setDefaultGatewayKey(key)}
                              disabled={key.isDefault}
                              className={key.isDefault ? undefined : 'text-kumo-subtle hover:text-kumo-brand'}
                              title={key.isDefault ? '当前为默认密钥' : '设为默认密钥'}
                            >
                              <Star className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                              shape="square"
                              size="sm"
                              variant="outline"
                              aria-label="轮换密钥"
                              onClick={() => rotateGatewayKey(key)}
                              className="text-kumo-subtle hover:text-kumo-brand"
                              title="轮换密钥"
                            >
                              <RotateCw className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                              shape="square"
                              size="sm"
                              variant="outline"
                              aria-label="编辑密钥"
                              onClick={() => openEditGatewayKeyModal(key)}
                              className="hover:text-kumo-brand text-kumo-subtle"
                              title="编辑密钥"
                            >
                              <Edit className="w-3.5 h-3.5" />
                            </Button>
                            <Button
                              shape="square"
                              size="sm"
                              variant={
                                isArmed(`gateway-key-${key.id}`)
                                  ? 'destructive'
                                  : 'secondary-destructive'
                              }
                              aria-label="删除密钥"
                              onClick={() => deleteGatewayKey(key)}
                              title={isArmed(`gateway-key-${key.id}`) ? '再次点击确认删除' : '删除密钥'}
                            >
                              <Trash className="w-3.5 h-3.5" />
                            </Button>
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ))
                  )}
                </Table.Body>
              </Table>
            </div>
          </LayerCard>
        </div>
      )}

      {/* ==================== 3. 网关分析 Tab ==================== */}
      {activeTab === 'analytics' && (
        <div className="flex min-h-0 flex-1 flex-col gap-3">
          <div className="grid grid-cols-2 gap-2 cq-sm:grid-cols-3 cq-sm:gap-3 cq-xl:grid-cols-6">
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">网关请求</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-kumo-brand">
                    <Activity className="h-3.5 w-3.5" />
                  </span>
                </div>
                {analyticsLoading ? (
                  <SkeletonLine className="h-6 w-20" />
                ) : (
                  <div className="flex min-w-0 items-baseline gap-1">
                    <span className="truncate font-mono text-lg font-semibold leading-none text-kumo-strong cq-sm:text-xl cq-xl:text-2xl">
                      {String(analyticsSummary.totalRequests)}
                    </span>
                  </div>
                )}
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">最近 {analyticsDays} 天</span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">平均端到端延迟</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-kumo-warning">
                    <Clock className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex min-w-0 items-baseline gap-1">
                  {analyticsLoading ? (
                    <SkeletonLine className="h-6 w-20" />
                  ) : (
                    <>
                      <span className="truncate font-mono text-lg font-semibold leading-none text-kumo-warning cq-sm:text-xl cq-xl:text-2xl">
                        {(analyticsSummary.avgLatency / 1000).toFixed(2)}
                      </span>
                      <span className="shrink-0 text-xs font-medium text-kumo-subtle">s</span>
                    </>
                  )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">最近 {analyticsDays} 天</span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">词元用量</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-kumo-brand">
                    <Brain className="h-3.5 w-3.5" />
                  </span>
                </div>
                {analyticsLoading ? (
                  <SkeletonLine className="h-6 w-24" />
                ) : (
                  <div className="flex min-w-0 items-baseline gap-1">
                    <Popover>
                      <Popover.Trigger
                        nativeButton={false}
                        title="查看输入/输出详情"
                        render={
                          <span className="w-fit cursor-pointer truncate font-mono text-lg font-semibold leading-none text-kumo-brand cq-sm:text-xl cq-xl:text-2xl">
                            {formatTokensM(analyticsSummary.totalTokens)}
                          </span>
                        }
                      />
                      <Popover.Content className="w-64 p-3">
                        <Popover.Title className="truncate text-sm font-semibold text-kumo-strong">
                          词元用量详情
                        </Popover.Title>
                        <div className="mt-2 flex flex-col gap-1.5 text-xs text-kumo-strong">
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">输入（含缓存）</span>
                            <span className="font-mono">
                              {formatTokensM(analyticsSummary.totalPromptTokens || 0)}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">缓存命中</span>
                            <span className="font-mono">
                              {formatTokensM(analyticsSummary.totalCachedTokens || 0)}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3">
                            <span className="text-kumo-subtle">输出</span>
                            <span className="font-mono">
                              {formatTokensM(analyticsSummary.totalCompletionTokens || 0)}
                            </span>
                          </div>
                          <div className="flex items-center justify-between gap-3 pt-1.5 text-kumo-strong border-t border-kumo-line">
                            <span className="text-kumo-subtle">合计</span>
                            <span className="font-mono">
                              {formatTokensM(analyticsSummary.totalTokens || 0)}
                            </span>
                          </div>
                        </div>
                      </Popover.Content>
                    </Popover>
                  </div>
                )}
                <span
                  className="hidden truncate font-mono text-[11px] text-kumo-subtle cq-xl:block"
                  title="非缓存输入 = 输入（含缓存）− 缓存命中的词元"
                >
                  <ArrowDown
                    className="inline h-3 w-3 align-[-1px]"
                    aria-hidden="true"
                  />{' '}
                  {formatTokensM(Math.max(0, analyticsSummary.totalPromptTokens - analyticsSummary.totalCachedTokens))}（
                  {analyticsSummary.totalPromptTokens > 0
                    ? `${(
                        (Math.max(0, analyticsSummary.totalPromptTokens - analyticsSummary.totalCachedTokens) /
                          analyticsSummary.totalPromptTokens) *
                        100
                      ).toFixed(1)}%`
                    : '0.0%'}
                  ） · <ArrowUp className="inline h-3 w-3 align-[-1px]" aria-hidden="true" />{' '}
                  {formatTokensM(analyticsSummary.totalCompletionTokens || 0)}
                </span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">平均 TPM</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-kumo-brand">
                    <Cpu className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex min-w-0 items-baseline gap-1">
                  {analyticsLoading ? (
                    <SkeletonLine className="h-6 w-20" />
                  ) : (
                    <>
                      <span className="truncate font-mono text-lg font-semibold leading-none text-kumo-brand cq-sm:text-xl cq-xl:text-2xl">
                        {((analyticsSummary.totalTokens || 0) / Math.max(1, analyticsDays * 24 * 60)).toFixed(1)}
                      </span>
                      <span className="shrink-0 text-xs font-medium text-kumo-subtle">/min</span>
                    </>
                  )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">每分钟词元</span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">平均 RPM</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-kumo-brand">
                    <TrendingUp className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex min-w-0 items-baseline gap-1">
                  {analyticsLoading ? (
                    <SkeletonLine className="h-6 w-20" />
                  ) : (
                    <>
                      <span className="truncate font-mono text-lg font-semibold leading-none text-kumo-brand cq-sm:text-xl cq-xl:text-2xl">
                        {((analyticsSummary.totalRequests || 0) / Math.max(1, analyticsDays * 24 * 60)).toFixed(1)}
                      </span>
                      <span className="shrink-0 text-xs font-medium text-kumo-subtle">/min</span>
                    </>
                  )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">每分钟请求</span>
              </AppCard>
              <AppCard padding="md" className="flex min-h-0 min-w-0 flex-col justify-between gap-1.5 max-sm:!p-2.5">
                <div className="flex items-center justify-between gap-2">
                  <span className="truncate text-[11px] font-medium text-kumo-subtle cq-sm:text-xs">上游错误率</span>
                  <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-kumo-recessed text-kumo-danger">
                    <AlertTriangle className="h-3.5 w-3.5" />
                  </span>
                </div>
                <div className="flex min-w-0 items-baseline gap-1">
                  {analyticsLoading ? (
                    <SkeletonLine className="h-6 w-20" />
                  ) : (
                    <>
                      <span className="truncate font-mono text-lg font-semibold leading-none text-kumo-danger cq-sm:text-xl cq-xl:text-2xl">
                        {(analyticsSummary.errorRate * 100).toFixed(1)}
                      </span>
                      <span className="shrink-0 text-xs font-medium text-kumo-subtle">%</span>
                    </>
                  )}
                </div>
                <span className="hidden truncate text-[11px] text-kumo-subtle cq-xl:block">请求失败占比</span>
              </AppCard>
            </div>

            <div className="grid gap-3 cq-xl:grid-cols-2">
            {[
              {
                key: 'requests',
                icon: <Activity className="h-4 w-4 text-kumo-brand" />,
                title: '请求量趋势',
                series: trendSeries.requests,
              },
              {
                key: 'tokens',
                icon: <Brain className="h-4 w-4 text-kumo-brand" />,
                title: '词元趋势',
                series: trendSeries.tokens,
              },
              {
                key: 'latency',
                icon: <Clock className="h-4 w-4 text-kumo-warning" />,
                title: '平均延迟趋势',
                series: trendSeries.latency,
              },
              {
                key: 'errors',
                icon: <AlertTriangle className="h-4 w-4 text-kumo-danger" />,
                title: '错误率趋势',
                series: trendSeries.errorRate,
              },
            ].map(card => (
              <LayerCard key={card.key} className="min-w-0 p-0">
                <LayerCard.Secondary>{card.title}</LayerCard.Secondary>
                <LayerCard.Primary className="flex min-h-0 flex-col gap-2 !p-3">
                  <div className="min-h-0 w-full" style={{ height: 168 }}>
                    {analyticsLoading && !analyticsCharts.daily?.length ? (
                      <SkeletonLine className="h-full w-full" />
                    ) : card.series.labels.length === 0 ? (
                      <div className="flex h-full items-center justify-center text-sm text-kumo-subtle">
                        暂无数据
                      </div>
                    ) : (
                      <TrendBarChart
                        labels={card.series.labels}
                        values={card.series.values}
                        color={card.series.color}
                        isDarkMode={isDarkMode}
                        formatValue={card.series.formatValue}
                        formatAxis={card.series.formatAxis}
                      />
                    )}
                  </div>
                </LayerCard.Primary>
              </LayerCard>
            ))}
          </div>

            <div className="grid">
            <LayerCard className="min-w-0 p-0">
              <LayerCard.Secondary>模型调用趋势</LayerCard.Secondary>
              <LayerCard.Primary className="!p-3">
              {analyticsLoading && !analyticsCharts.daily?.length ? (
                <SkeletonLine className="h-[280px] w-full" />
              ) : !Array.isArray(byModelTrend.labels) || byModelTrend.labels.length === 0 ? (
                <div className="flex h-[280px] items-center justify-center text-sm text-kumo-subtle">
                  暂无数据
                </div>
              ) : (
                <ModelTrendChart
                  labels={byModelTrend.labels}
                  series={byModelTrend.models}
                  isDarkMode={isDarkMode}
                />
              )}
              </LayerCard.Primary>
            </LayerCard>
          </div>

            <div className="grid gap-3 cq-xl:grid-cols-2">
            <LayerCard className="min-w-0 p-0">
              <LayerCard.Secondary>模型词元分布</LayerCard.Secondary>
              <LayerCard.Primary className="!p-3">
                <div className="min-h-0">
                {analyticsLoading ? (
                  <div className="space-y-2">
                    <SkeletonLine className="w-full h-4" />
                    <SkeletonLine className="w-full h-4" />
                  </div>
                ) : !analyticsCharts.models || analyticsCharts.models.length === 0 ? (
                  <div className="py-16 text-center text-sm text-kumo-subtle">暂无模型数据</div>
                ) : (
                  (() => {
                    const totalTokens =
                      analyticsCharts.models.reduce(
                        (sum, model) => sum + (Number(model.tokens) || 0),
                        0
                      ) || 1;
                    const sorted = [...analyticsCharts.models]
                      .sort((a, b) => (Number(b.tokens) || 0) - (Number(a.tokens) || 0))
                      .slice(0, 20);
                    return (
                      <div className="flex flex-col gap-1.5">
                        {sorted.map((model, index) => {
                          const tokens = Number(model.tokens) || 0;
                          const percent = (tokens / totalTokens) * 100;
                          return (
                            <div
                              key={model.model}
                              className="flex items-center gap-2 text-xs"
                            >
                              <span
                                className="w-40 shrink-0 truncate font-medium text-kumo-strong"
                                title={model.model}
                              >
                                {model.model}
                              </span>
                              <div className="h-3 min-w-0 flex-1 overflow-hidden rounded-full bg-kumo-recessed">
                                <div
                                  className="h-full rounded-full"
                                  style={{
                                    width: `${Math.max(2, Math.min(100, percent))}%`,
                                    background: ChartPalette.categorical(index, isDarkMode),
                                  }}
                                />
                              </div>
                              <span className="w-14 shrink-0 text-right font-mono text-[11px] text-kumo-subtle">
                                {formatTokensM(tokens)}
                              </span>
                              <span className="w-11 shrink-0 text-right font-mono text-[10px] text-kumo-subtle">
                                {percent.toFixed(1)}%
                              </span>
                            </div>
                          );
                        })}
                      </div>
                    );
                  })()
                )}
              </div>
              </LayerCard.Primary>
            </LayerCard>

            <LayerCard className="min-w-0 p-0">
              <LayerCard.Secondary>模型调用次数</LayerCard.Secondary>
              <LayerCard.Primary className="!p-3">
                <div className="min-h-0">
                {analyticsLoading ? (
                  <div className="space-y-2">
                    <SkeletonLine className="h-4 w-full" />
                    <SkeletonLine className="h-4 w-full" />
                  </div>
                ) : !analyticsCharts.models || analyticsCharts.models.length === 0 ? (
                  <div className="py-16 text-center text-sm text-kumo-subtle">暂无模型数据</div>
                ) : (
                  (() => {
                    const totalCount =
                      analyticsCharts.models.reduce(
                        (sum, model) => sum + (Number(model.count) || 0),
                        0
                      ) || 1;
                    const sorted = [...analyticsCharts.models]
                      .sort((a, b) => (Number(b.count) || 0) - (Number(a.count) || 0))
                      .slice(0, 20);
                    return (
                      <div className="flex flex-col gap-1.5">
                        {sorted.map((model, index) => {
                          const count = Number(model.count) || 0;
                          const percent = (count / totalCount) * 100;
                          return (
                            <div
                              key={model.model}
                              className="flex items-center gap-2 text-xs"
                            >
                              <span
                                className="w-40 shrink-0 truncate font-medium text-kumo-strong"
                                title={model.model}
                              >
                                {model.model}
                              </span>
                              <div className="h-3 min-w-0 flex-1 overflow-hidden rounded-full bg-kumo-recessed">
                                <div
                                  className="h-full rounded-full"
                                  style={{
                                    width: `${Math.max(2, Math.min(100, percent))}%`,
                                    background: ChartPalette.categorical(index, isDarkMode),
                                  }}
                                />
                              </div>
                              <span className="w-14 shrink-0 text-right font-mono text-[11px] text-kumo-subtle">
                                {formatCompact(count, 0)}
                              </span>
                              <span className="w-11 shrink-0 text-right font-mono text-[10px] text-kumo-subtle">
                                {percent.toFixed(1)}%
                              </span>
                            </div>
                          );
                        })}
                      </div>
                    );
                  })()
                )}
              </div>
              </LayerCard.Primary>
            </LayerCard>
          </div>
        </div>
      )}

      {/* ==================== 4. 网关日志 Tab ==================== */}
      {activeTab === 'logs' && (
        <div className="flex min-h-0 flex-1 flex-col gap-3">
          {/* 日志筛选区：状态 / 模型 / 端点，均即时生效 */}
          <div className="flex flex-wrap items-center gap-2">
            <Select
              size="sm"
              className="w-28"
              value={logStatusFilter || undefined}
              onValueChange={value => {
                setLogStatusFilter(value || '');
                setAnalyticsPage(1);
              }}
              placeholder="全部状态"
              aria-label="状态筛选"
            >
              <Select.Option value="">全部状态</Select.Option>
              <Select.Option value="success">成功</Select.Option>
              <Select.Option value="error">失败 (≥400)</Select.Option>
              <Select.Option value="429">限流 429</Select.Option>
              <Select.Option value="5xx">服务端 5xx</Select.Option>
            </Select>
            <Input
              size="sm"
              className="w-52"
              value={logModelFilter}
              onChange={e => {
                setLogModelFilter(e.target.value);
                setAnalyticsPage(1);
              }}
              placeholder="按模型筛选，如 deepseek-v4-flash"
              spellCheck={false}
            />
            <Input
              size="sm"
              className="w-52"
              value={logEndpointFilter}
              onChange={e => {
                setLogEndpointFilter(e.target.value);
                setAnalyticsPage(1);
              }}
              placeholder="按端点筛选（完整名称或 ID）"
              spellCheck={false}
            />
            {(logStatusFilter || logModelFilter || logEndpointFilter) && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => {
                  setLogStatusFilter('');
                  setLogModelFilter('');
                  setLogEndpointFilter('');
                  setAnalyticsPage(1);
                }}
                icon={<X className="h-3.5 w-3.5" />}
              >
                清除筛选
              </Button>
            )}
          </div>
          {/* Logs table and pagination */}
          <LayerCard className="flex min-h-0 w-full min-w-0 flex-1 flex-col overflow-hidden p-0 shadow-none">
            <div className="min-h-0 min-w-0 flex-1 overflow-auto scrollbar-thin">
              <Table layout="fixed" className="min-w-[1380px] [&_td]:!px-2 [&_td]:!py-2 [&_th]:!px-2 [&_th]:!py-2">
<colgroup>
                  <col style={{ width: 140 }} />
                  <col style={{ width: 80 }} />
                  <col style={{ width: 104 }} />
                  <col style={{ width: 140 }} />
                  <col style={{ width: 132 }} />
                  <col style={{ width: 120 }} />
                  <col style={{ width: 40 }} />
                  <col style={{ width: 160 }} />
                  <col style={{ width: 132 }} />
                  <col style={{ width: 132 }} />
                  <col style={{ width: 132 }} />
                </colgroup>
                <Table.Header sticky variant="compact">
                  <Table.Row>
                    <Table.Head className="text-center">时间</Table.Head>
                    <Table.Head className="text-center">路由</Table.Head>
                    <Table.Head className="text-center">端点</Table.Head>
                    <Table.Head className="text-center">模型</Table.Head>
                    <Table.Head className="text-center">出口 IP</Table.Head>
                    <Table.Head className="text-center">客户端 IP</Table.Head>
                    <Table.Head className="text-center">状态</Table.Head>
                    <Table.Head className="text-center">耗时/首字</Table.Head>
                    <Table.Head className="text-left">输入 / 输出</Table.Head>
                    <Table.Head className="text-left">缓存</Table.Head>
                    <Table.Head className="text-left">总消耗</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {analyticsLoading && analyticsLogs.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={11} className="text-center py-8">
                        <Loader size={20} className="mx-auto text-kumo-subtle" />
                      </Table.Cell>
                    </Table.Row>
                  ) : analyticsLogs.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={11} className="text-center py-8 text-kumo-subtle text-sm">
                        暂无网关日志记录
                      </Table.Cell>
                    </Table.Row>
                  ) : (
                    analyticsLogs.map(log => {
                      return (
                        <Table.Row key={log.id} className="text-sm">
                          <Table.Cell className="truncate text-center font-mono text-kumo-subtle">
                            {formatDateTime(log.timestamp)}
                          </Table.Cell>
                          <Table.Cell
                            className="truncate text-center font-mono text-kumo-subtle"
                            title={log.route}
                          >
                            {!log.route ? '-' : log.route.replace(/^chat\./, '')}
                          </Table.Cell>
                          <Table.Cell
                            className="truncate text-center font-semibold text-kumo-strong"
                            title={log.endpointName}
                          >
                            <span className="inline-flex items-center justify-center gap-1.5">
                              <FailoverPathBadge path={log.failoverPath} endpointName={log.endpointName} />
                              {typeof log.keyIndex === 'number' && log.keyIndex >= 0 && (
                                <StatusBadge tone="info" title={`使用的 API Key 序号（0=主 key）`}>
                                  K{log.keyIndex + 1}
                                </StatusBadge>
                              )}
                            </span>
                          </Table.Cell>
                          <Table.Cell
                            className="truncate text-center font-mono font-medium text-kumo-strong"
                            title={log.model}
                          >
                            {log.model}
                          </Table.Cell>
                          <Table.Cell
                            className="text-center font-mono text-kumo-subtle"
                            title={log.upstreamIp || '本机出口'}
                          >
                            <div
                              className="inline-flex items-center justify-center gap-1"
                              title="经代理池出口"
                            >
                              <IpCell value={log.upstreamIp} viaProxy={log.viaProxy} />
                            </div>
                          </Table.Cell>
                          <Table.Cell
                            className="truncate text-center font-mono text-kumo-subtle"
                            title={log.clientIp || '无客户端 IP'}
                          >
                            <IpCell value={log.clientIp} />
                          </Table.Cell>
                          <Table.Cell className="text-center">
                            <span className="inline-flex items-center gap-1">
                              <StatusBadge tone={statusCodeTone(log.statusCode)}>
                                {log.statusCode}
                              </StatusBadge>
                              {log.statusCode === 429 && (
                                <StatusBadge tone="warning" title="上游限流或网关无可用渠道">
                                  限
                                </StatusBadge>
                              )}
                              {log.statusCode === 503 && (
                                <StatusBadge tone="warning" title="网关无可用渠道">
                                  无
                                </StatusBadge>
                              )}
                            </span>
                          </Table.Cell>
                          <Table.Cell className="text-center">
                            <div
                              className="inline-flex items-center gap-1"
                              title={log.stream ? '流式响应' : '非流式响应'}
                            >
                              <StatusBadge tone={resultTone(log.statusCode, log.completionTokens, log.latencyMs)}>
                                {(log.latencyMs / 1000).toFixed(1)}s
                              </StatusBadge>
                              <StatusBadge tone={ttfbTone(log.ttfbMs)}>
                                {log.ttfbMs > 0 ? (log.ttfbMs / 1000).toFixed(1) + 's' : '—'}
                              </StatusBadge>
                              <StatusBadge
                                tone={log.stream ? 'info' : 'warning'}
                                className="!px-1.5 !text-[10px]"
                              >
                                {log.stream ? '流' : '非流'}
                              </StatusBadge>
                            </div>
                          </Table.Cell>
                          <Table.Cell className="text-left font-mono">
                            <div className="flex w-full items-baseline justify-start whitespace-nowrap">
                              <span className="text-right text-kumo-strong">
                                {log.promptTokens}
                              </span>
                              <span className="shrink-0 px-0.5 text-kumo-subtle">/</span>
                              <span className="text-left text-kumo-strong">
                                {log.completionTokens}
                              </span>
                            </div>
                          </Table.Cell>
                          <Table.Cell
                            className="text-left font-mono"
                            title="缓存命中 词元（占比 = 缓存 / 输入）"
                          >
                            <div className="flex w-full items-baseline justify-start whitespace-nowrap">
                              <span className="text-right text-kumo-strong">
                                {log.cachedTokens}
                              </span>
                              <span className="shrink-0 px-0.5 text-kumo-subtle">（</span>
                              <span className="text-left text-kumo-strong">
                                {log.promptTokens > 0
                                  ? `${((log.cachedTokens / log.promptTokens) * 100).toFixed(1)}%）`
                                  : '0.0%）'}
                              </span>
                            </div>
                          </Table.Cell>
<Table.Cell
                            className="text-left font-mono"
                            title="总消耗（实际消耗 = 总消耗 − 缓存）"
                          >
                            <div className="flex w-full items-baseline justify-start whitespace-nowrap">
                              <span className="text-right font-semibold leading-none text-kumo-brand">
                                {log.totalTokens}
                              </span>
                              <span className="shrink-0 px-0.5 leading-none text-kumo-subtle">（</span>
                              <span className="text-left font-mono leading-none text-kumo-subtle">
                                {Math.max(0, log.totalTokens - log.cachedTokens)}）
                              </span>
                            </div>
                          </Table.Cell>
                        </Table.Row>
                      );
                    })
                  )}
                </Table.Body>
              </Table>
            </div>

            {analyticsTotal > 0 && (
              <Pagination
                page={analyticsPage}
                setPage={setAnalyticsPage}
                perPage={analyticsPageSize}
                totalCount={analyticsTotal}
                labels={{
                  navigation: '网关日志分页',
                  firstPage: '第一页',
                  previousPage: '上一页',
                  nextPage: '下一页',
                  lastPage: '最后一页',
                  pageNumber: '页码',
                  pageSize: '每页数量',
                }}
                className="shrink-0 flex-wrap gap-x-3 gap-y-1 border-x-0 border-b-0 border-t border-kumo-line bg-kumo-base px-3 py-2 text-sm shadow-none [&_[data-slot=pagination-controls]]:ml-auto [&_[data-slot=pagination-info]]:min-w-0 max-sm:[&_[data-slot=pagination-info]]:hidden max-sm:[&_[data-slot=pagination-page-size]]:hidden max-sm:[&_[data-slot=pagination-separator]]:hidden max-sm:[&_[data-slot=pagination-controls]]:m-auto"
              >
                <Pagination.Info>
                  {({ pageShowingRange, totalCount }) => (
                    <span className="text-kumo-subtle">
                      显示 {pageShowingRange}，共 {totalCount} 条
                    </span>
                  )}
                </Pagination.Info>
                <Pagination.Separator />
                <Pagination.PageSize
                  value={analyticsPageSize}
                  onChange={size => {
                    setAnalyticsPageSize(size);
                    setAnalyticsPage(1);
                  }}
                  options={[10, 20, 50, 100]}
                  label="每页"
                />
                <Pagination.Controls />
              </Pagination>
            )}
          </LayerCard>
        </div>
      )}

      {/* ==================== dialogs & modals ==================== */}

      {/* 1. Endpoint Add/Edit Dialog */}
      <Dialog.Root
        open={endpointFormOpen}
        onOpenChange={open => {
          setEndpointFormOpen(open);
          if (!open) setEndpointKeyChecks([]);
        }}
      >
        <Dialog className="flex max-h-[min(calc(100dvh-2rem),42rem)] !w-[min(48rem,calc(100vw-2rem))] !max-w-[min(48rem,calc(100vw-2rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 px-6 pt-5">
            <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
              {editingEndpoint ? '编辑端点' : '添加 API 端点'}
            </Dialog.Title>
            <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
              配置 OpenAI 兼容的 API 端点以供中转或对话使用。
            </Dialog.Description>
          </div>

          <div className="min-h-0 flex-1 overflow-y-auto px-6 py-3 scrollbar-thin">
            <div className="grid grid-cols-2 gap-x-5 gap-y-4">
              {/* ====== 左列：基本信息 ====== */}
              <div className="space-y-4">
                <Input
                  size="sm"
                  label="名称"
                  type="text"
                  value={endpointForm.name}
                  onChange={e => setEndpointForm({ ...endpointForm, name: e.target.value })}
                  placeholder="如：DeepSeek 官方"
                  className="w-full text-kumo-strong text-sm font-sans"
                />

                <Input
                  size="sm"
                  label="Base URL"
                  type="text"
                  value={endpointForm.baseUrl}
                  onChange={e => setEndpointForm({ ...endpointForm, baseUrl: e.target.value })}
                  placeholder="https://api.openai.com/v1"
                  className="w-full text-kumo-strong text-[0.9em] font-mono"
                />

                <div className="space-y-1.5">
                  <div className="flex items-center justify-between gap-2">
                    <Label>API Key 列表</Label>
                    <Button
                      size="xs"
                      variant="secondary"
                      type="button"
                      disabled={endpointKeyChecking || !editingEndpoint}
                      onClick={() =>
                        checkEndpointKeys(
                          [endpointForm.apiKey, ...(endpointForm.apiKeys || [])],
                          editingEndpoint?.id
                        )
                      }
                    >
                      <RotateCw className={cx(endpointKeyChecking && 'animate-spin')} size={14} />
                      {endpointKeyChecking ? '检测中' : '重新检测'}
                    </Button>
                  </div>

                  <div className="space-y-1.5">
                    {[endpointForm.apiKey, ...(endpointForm.apiKeys || [])].map((key, rowIndex) => (
                      <div
                        key={rowIndex}
                        className="grid grid-cols-[2rem_minmax(0,1fr)_auto_1.75rem] items-center gap-1.5"
                      >
                        <span className="text-center text-[11px] font-semibold text-kumo-subtle select-none">
                          K{rowIndex + 1}
                        </span>
                        <Input
                          size="sm"
                          type="text"
                          value={key}
                          onChange={e => {
                            const value = e.target.value;
                            setEndpointForm(current => {
                              if (rowIndex === 0) {
                                return { ...current, apiKey: value };
                              }
                              return {
                                ...current,
                                apiKeys: (current.apiKeys || []).map((k, j) =>
                                  j === rowIndex - 1 ? value : k
                                ),
                              };
                            });
                            setEndpointKeyChecks(prev => {
                              const next = [...prev];
                              next[rowIndex] = null;
                              return next;
                            });
                          }}
                          placeholder="sk-xxxxxxxxxxxxxxxxxxxxxxxx"
                          autoComplete="off"
                          data-1p-ignore
                          data-lpignore="true"
                          data-bwignore="true"
                          data-form-type="other"
                          spellCheck={false}
                          className="w-full text-kumo-strong text-[0.9em] font-mono"
                        />
                        <KeyStatusBadge check={endpointKeyChecks?.[rowIndex]} />
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary-destructive"
                          aria-label={`删除 Key K${rowIndex + 1}`}
                          onClick={() => removeEndpointKey(rowIndex)}
                          title="删除此 Key"
                          icon={<Trash className="h-3.5 w-3.5" />}
                        />
                      </div>
                    ))}

                    <Button
                      size="xs"
                      variant="outline"
                      onClick={appendEndpointKey}
                      icon={<Plus className="h-3.5 w-3.5" />}
                    >
                      添加 Key
                    </Button>
                  </div>
                </div>

                <Input
                  size="sm"
                  label="备注"
                  type="text"
                  value={endpointForm.notes}
                  onChange={e => setEndpointForm({ ...endpointForm, notes: e.target.value })}
                  placeholder="选填"
                  className="w-full text-kumo-strong text-sm font-sans"
                />
              </div>

              {/* ====== 右列：连接与代理 ====== */}
              <div className="space-y-4">
                <div className="space-y-1.5">
                  <Label>
                    自定义请求头
                    <span className="font-normal text-kumo-subtle">（可选）</span>
                  </Label>
                  <div className="space-y-2">
                    {(endpointForm.headers || []).map((header, index) => (
                      <div
                        key={index}
                        className="grid grid-cols-[minmax(0,1.2fr)_minmax(0,1.8fr)_2rem] items-center gap-2"
                      >
                        <Input
                          size="sm"
                          type="text"
                          value={header.name}
                          onChange={e => updateEndpointHeader(index, 'name', e.target.value)}
                          placeholder="Header 名称"
                          spellCheck={false}
                          autoComplete="off"
                          data-1p-ignore
                          className="w-full text-kumo-strong font-mono text-[0.85em]"
                        />
                        <Input
                          size="sm"
                          type="text"
                          value={header.value}
                          onChange={e => updateEndpointHeader(index, 'value', e.target.value)}
                          placeholder="Header 值"
                          spellCheck={false}
                          autoComplete="off"
                          data-1p-ignore
                          className="w-full text-kumo-strong font-mono text-[0.85em]"
                        />
                        <Button
                          shape="square"
                          size="sm"
                          variant="secondary-destructive"
                          aria-label="删除请求头"
                          onClick={() => removeEndpointHeader(index)}
                          title="删除请求头"
                          icon={<Trash className="h-3.5 w-3.5" />}
                        />
                      </div>
                    ))}
                  </div>
                  <Button
                    size="xs"
                    variant="outline"
                    onClick={addEndpointHeader}
                    icon={<Plus className="h-3.5 w-3.5" />}
                  >
                    添加请求头
                  </Button>
                </div>

                <Select
                  size="sm"
                  label="连接协议"
                  value={endpointForm.protocol || 'auto'}
                  onValueChange={value => setEndpointForm(current => ({ ...current, protocol: value }))}
                  items={ENDPOINT_PROTOCOL_OPTIONS}
                  className="w-full"
                />

                <div className="space-y-1.5">
                  <div className="flex min-w-0 items-center justify-between gap-2">
                    <Label>
                      出口代理池
                      <span className="font-normal text-kumo-subtle">（可选）</span>
                    </Label>
                    <Button
                      size="xs"
                      variant="outline"
                      onClick={() => setProxyManagerOpen(true)}
                      icon={<Sliders className="h-3.5 w-3.5" />}
                    >
                      管理代理池（{endpointForm.proxyPool?.length || 0}）
                    </Button>
                  </div>
                  <div className="rounded-md border border-kumo-line bg-kumo-recessed/25 px-3 py-2">
                    {endpointForm.proxyPool?.length > 0 ? (
                      <div className="flex flex-wrap items-center gap-1.5">
                        {(endpointForm.proxyBatches || []).map(batch => (
                          <Badge
                            key={batch.id}
                            variant="outline"
                            className="w-fit gap-1 !text-[11px] font-medium"
                            title={`${batch.name}\n${batch.proxies?.length || 0} 条`}
                          >
                            <span className="max-w-40 truncate">{batch.name}</span>
                            <span className="shrink-0 font-mono text-[10px] text-kumo-subtle">
                              {batch.proxies?.length || 0} 条
                            </span>
                          </Badge>
                        ))}
                        {manualProxyEntries.length > 0 && (
                          <Badge
                            variant="outline"
                            className="w-fit gap-1 !text-[11px] font-medium"
                            title="手动添加/粘贴/订阅导入的代理"
                          >
                            <span>手动</span>
                            <span className="shrink-0 font-mono text-[10px] text-kumo-subtle">
                              {manualProxyEntries.length} 条
                            </span>
                          </Badge>
                        )}
                        <span className="text-[11px] text-kumo-subtle">
                          共 {endpointForm.proxyPool.length} 条
                        </span>
                      </div>
                    ) : (
                      <span className="text-xs text-kumo-subtle">
                        未配置代理，请求将直连上游。
                      </span>
                    )}
                  </div>
                </div>

                <div className="flex min-h-8 items-center gap-2">
                  <Switch
                    size="sm"
                    aria-label="代理开关"
                    checked={!!endpointForm.proxyEnabled}
                    onCheckedChange={checked =>
                      setEndpointForm(current => ({ ...current, proxyEnabled: checked }))
                    }
                  />
                  <span className="text-xs text-kumo-strong">代理开关</span>
                </div>
                <div className="flex min-h-8 items-center gap-2">
                  <Switch
                    size="sm"
                    aria-label="限流自动切换代理"
                    checked={!!endpointForm.autoSwitch}
                    disabled={!endpointForm.proxyEnabled}
                    onCheckedChange={checked =>
                      setEndpointForm(current => ({ ...current, autoSwitch: checked }))
                    }
                  />
                  <span className="text-xs text-kumo-subtle">限流或连接失败自动切换代理</span>
                </div>
                <div className="flex min-h-8 items-center gap-2">
                  <Switch
                    size="sm"
                    aria-label="允许直连兜底"
                    checked={!!endpointForm.allowDirectFallback}
                    disabled={!endpointForm.proxyEnabled}
                    onCheckedChange={checked =>
                      setEndpointForm(current => ({ ...current, allowDirectFallback: checked }))
                    }
                  />
                  <span className="text-xs text-kumo-subtle">代理池异常时允许直连兜底</span>
                </div>
              </div>
            </div>

            {endpointFormError && (
                <p className="mt-4 text-sm text-kumo-danger font-semibold">{endpointFormError}</p>
              )}
            </div>

            <div className="flex shrink-0 flex-wrap items-center justify-end gap-3 border-t border-kumo-line bg-kumo-base px-6 py-4">
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
        </Dialog>
      </Dialog.Root>

      {/* 1b. 出口代理池管理弹窗 */}
      <Dialog.Root open={proxyManagerOpen} onOpenChange={setProxyManagerOpen}>
        <Dialog className="@container flex max-h-[min(calc(100dvh-2rem),44rem)] !w-[min(38rem,calc(100vw-1rem))] !max-w-[min(38rem,calc(100vw-1rem))] flex-col overflow-hidden !p-0">
          <div className="shrink-0 border-b border-kumo-line px-4 py-3 cq-sm:px-5 cq-sm:py-4">
            <Dialog.Title className="text-sm font-semibold text-kumo-strong">
              出口代理池（{endpointForm.proxyPool?.length || 0}）
            </Dialog.Title>
            <Dialog.Description className="mt-0.5 text-xs text-kumo-subtle">
              请求按池轮换出口 IP。适合 IP 敏感的源；留空则直连。
            </Dialog.Description>
          </div>

          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-4 py-3 scrollbar-thin cq-sm:px-5 cq-sm:py-4">
            <div className="flex flex-wrap items-center gap-2">
              <Button
                size="xs"
                variant="outline"
                onClick={addEndpointProxy}
                icon={<Plus className="h-3.5 w-3.5" />}
              >
                添加代理
              </Button>
              <Button
                size="xs"
                variant="outline"
                onClick={() => setProxyBatchOpen(current => !current)}
                icon={<LogList className="h-3.5 w-3.5" />}
              >
                批量添加
              </Button>
              <Button
                size="xs"
                variant="outline"
                onClick={() => proxyFileInputRef.current?.click()}
                disabled={proxyImportLoading}
                icon={proxyImportLoading ? <Loader size="sm" /> : <ArrowDown className="h-3.5 w-3.5" />}
              >
                {proxyImportLoading ? '解析中...' : '导入文件'}
              </Button>
              <input
                ref={proxyFileInputRef}
                type="file"
                accept=".txt,.list,.conf,.csv,text/plain"
                className="hidden"
                onChange={e => importProxyFile(e.target.files?.[0])}
              />
              <Button
                size="xs"
                variant="outline"
                onClick={() => setSubscriptionUrlOpen(current => !current)}
                icon={<Globe className="h-3.5 w-3.5" />}
              >
                订阅链接导入
              </Button>
              <Button
                size="xs"
                variant="outline"
                onClick={probeAllProxies}
                disabled={probingProxies}
                icon={probingProxies ? <Loader size="sm" /> : <Activity className="h-3.5 w-3.5" />}
                title="立即对全部出口做一次连通性探活并记录出口 IP"
              >
                {probingProxies ? '测试中...' : '批量测试'}
              </Button>
              {disabledProxyCount > 0 && (
                <Button
                  size="xs"
                  variant="secondary-destructive"
                  onClick={unbanAllProxies}
                  disabled={unbanningProxies}
                  icon={unbanningProxies ? <Loader size="sm" /> : <RefreshCw className="h-3.5 w-3.5" />}
                  title="清除全部冷却 / 429 冻结 / 坏代理沉淀，使被禁用的出口立即恢复可选"
                >
                  {unbanningProxies ? '解封中...' : `一键解封（${disabledProxyCount}）`}
                </Button>
              )}
            </div>

            {proxyBatchOpen && (
              <div className="space-y-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-3">
                <Textarea
                  size="sm"
                  value={proxyBatchText}
                  onChange={e => setProxyBatchText(e.target.value)}
                  placeholder={'每行一个代理地址，支持 socks5://、http(s):// 或 host:port\n如：\nsocks5://user:pass@1.2.3.4:1080\nhttp://5.6.7.8:8080'}
                  spellCheck={false}
                  rows={5}
                  className="w-full font-mono text-[0.85em]"
                />
                <div className="flex items-center justify-end gap-2">
                  <Button
                    size="xs"
                    variant="ghost"
                    onClick={() => {
                      setProxyBatchText('');
                      setProxyBatchOpen(false);
                    }}
                  >
                    取消
                  </Button>
                  <Button
                    size="xs"
                    variant="primary"
                    onClick={saveProxyBatch}
                    icon={<Check className="h-3.5 w-3.5" />}
                  >
                    确定添加
                  </Button>
                </div>
              </div>
            )}

            {subscriptionUrlOpen && (
              <div className="space-y-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-3">
                <Input
                  size="sm"
                  type="url"
                  value={subscriptionUrl}
                  onChange={e => setSubscriptionUrl(e.target.value)}
                  placeholder="https://example.com/sub?token=xxx"
                  spellCheck={false}
                  autoComplete="off"
                  data-1p-ignore
                  className="w-full font-mono text-[0.85em]"
                />
                <p className="text-xs leading-snug text-kumo-subtle">
                  后端将拉取订阅并解析其中的 socks/http 节点，导入为出口代理。仅本机/服务器能访问的节点可用。
                </p>
                <div className="flex items-center justify-end gap-2">
                  <Button
                    size="xs"
                    variant="ghost"
                    onClick={() => {
                      setSubscriptionUrl('');
                      setSubscriptionUrlOpen(false);
                    }}
                  >
                    取消
                  </Button>
                  <Button
                    size="xs"
                    variant="primary"
                    onClick={resolveSubscriptionProxies}
                    disabled={proxyImportLoading}
                    icon={proxyImportLoading ? <Loader size="sm" /> : <Globe className="h-3.5 w-3.5" />}
                  >
                    {proxyImportLoading ? '解析中...' : '解析并导入'}
                  </Button>
                </div>
              </div>
            )}

            <div className="space-y-2">
              {(endpointForm.proxyBatches || []).length > 0 && (
                <div className="space-y-2 rounded-md border border-kumo-line bg-kumo-recessed/25 p-2">
                  <div className="px-1 pt-0.5 text-xs font-semibold text-kumo-strong">
                    导入批次
                  </div>
                  {(endpointForm.proxyBatches || []).map(batch => {
                    const expanded = expandedBatchId === batch.id;
                    return (
                      <div key={batch.id} className="overflow-hidden rounded-md border border-kumo-line bg-kumo-base">
                        <div className="flex min-w-0 items-center gap-2 px-3 py-2">
                          <Button
                            shape="square"
                            size="sm"
                            variant="ghost"
                            aria-label={expanded ? '收起' : '展开'}
                            onClick={() => setExpandedBatchId(expanded ? null : batch.id)}
                            icon={<ChevronDown className={`h-3.5 w-3.5 transition-transform ${expanded ? 'rotate-180' : ''}`} />}
                          />
                          <div className="min-w-0 flex-1">
                            <div className="truncate text-xs font-semibold text-kumo-strong">{batch.name}</div>
                            <div className="truncate font-mono text-[11px] text-kumo-subtle">
                              {batch.proxies?.length || 0} 条 · {formatDateTime(batch.createdAt)}
                            </div>
                          </div>
                          <Button
                            size="sm"
                            shape="square"
                            variant="secondary-destructive"
                            aria-label={`移除批次 ${batch.name}`}
                            onClick={() => removeProxyBatch(batch)}
                            icon={<Trash className="h-3.5 w-3.5" />}
                          />
                        </div>
                        {expanded && (
                          <div className="space-y-1.5 border-t border-kumo-line p-2">
                            {(batch.proxies || []).slice(0, PROXY_PREVIEW_LIMIT).map(proxy => {
                            const disabled = disabledProxyUntil(proxy);
                            return (
                              <div key={proxy} className="flex min-w-0 items-center gap-2">
                                <span
                                  className={`min-w-0 flex-1 truncate rounded border px-2 py-1 font-mono text-[11px] ${
                                    disabled
                                      ? 'border-kumo-danger/50 bg-kumo-danger/10 text-kumo-danger'
                                      : 'border-kumo-line bg-kumo-recessed/25 text-kumo-subtle'
                                  }`}
                                  title={disabled ? `${proxy}\n${disabled.label}` : proxy}
                                >
                                  {proxy}
                                </span>
                                <Button
                                  shape="square"
                                  size="sm"
                                  variant="secondary-destructive"
                                  aria-label="移出此条"
                                  onClick={() => removeProxyFromBatch(batch, proxy)}
                                  title="移出此条"
                                  icon={<X className="h-3 w-3" />}
                                />
                              </div>
                            );
                          })}
                            {(batch.proxies || []).length > PROXY_PREVIEW_LIMIT && (
                              <p className="px-1 text-[11px] text-kumo-subtle">
                                仅预览前 {PROXY_PREVIEW_LIMIT} 条，共 {batch.proxies.length} 条
                              </p>
                            )}
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
            </div>

            <div className="space-y-2">
              {manualProxyEntries.length > 0 && (
                <div className="flex items-center gap-2 px-1 pt-1">
                  <span className="text-xs font-semibold text-kumo-strong">
                    手动 / 未分组代理（{manualProxyEntries.length}）
                  </span>
                  <Button
                    shape="square"
                    size="xs"
                    variant="ghost"
                    aria-label={manualProxyExpanded ? '收起手动代理' : '展开手动代理'}
                    onClick={() => setManualProxyExpanded(current => !current)}
                    icon={<ChevronDown className={`h-3.5 w-3.5 transition-transform ${manualProxyExpanded ? 'rotate-180' : ''}`} />}
                  />
                </div>
              )}
              {manualProxyExpanded &&
                manualProxyEntries.slice(0, PROXY_PREVIEW_LIMIT).map(({ proxy, index }) => {
                const entry = parseProxyEntry(proxy);
                const editing = editingProxyIndex === index;
                const disabled = disabledProxyUntil(proxy);
                return (
                  <div key={`m-${index}`} className="flex min-w-0 items-center gap-2">
                    {editing ? (
                      <Input
                        size="sm"
                        type="text"
                        value={proxy}
                        onChange={e => updateEndpointProxy(index, e.target.value)}
                        onBlur={() => setEditingProxyIndex(-1)}
                        onKeyDown={e => {
                          if (e.key === 'Enter' || e.key === 'Escape') setEditingProxyIndex(-1);
                        }}
                        spellCheck={false}
                        autoComplete="off"
                        data-1p-ignore
                        autoFocus
                        className="min-w-0 flex-1 font-mono text-[0.85em] text-kumo-strong"
                      />
                    ) : (
                      <div
                        className={`min-w-0 flex-1 cursor-pointer rounded-md border px-3 py-2 ${
                          disabled
                            ? 'border-kumo-danger/50 bg-kumo-danger/10'
                            : 'border-kumo-line bg-kumo-recessed/25'
                        }`}
                        onClick={() => setEditingProxyIndex(index)}
                        title={`${entry.full}${disabled ? `\n\n${disabled.label}` : ''}\n点击可编辑完整代理地址`}
                      >
                        {entry.label ? (
                          <div className="min-w-0">
                            <div className="flex min-w-0 items-baseline gap-1.5">
                              <span className={`truncate text-sm font-semibold ${disabled ? 'text-kumo-danger' : 'text-kumo-strong'}`}>
                                {entry.label}
                              </span>
                              {entry.host && entry.host !== entry.label && (
                                <span className={`shrink-0 font-mono text-[11px] ${disabled ? 'text-kumo-danger/80' : 'text-kumo-subtle'}`}>
                                  {entry.host}
                                </span>
                              )}
                            </div>
                            <ProxyRuntimeMeta proxy={proxy} state={proxyRuntimeStates[proxy]} />
                          </div>
                        ) : (
                          <div className={`truncate text-sm ${disabled ? 'text-kumo-danger' : 'text-kumo-subtle'}`}>空代理（点击编辑）</div>
                        )}
                      </div>
                    )}
                    <Button
                      shape="square"
                      size="sm"
                      variant={editing ? 'primary' : 'secondary'}
                      aria-label="编辑代理"
                      onClick={() => setEditingProxyIndex(editing ? -1 : index)}
                      title={editing ? '完成编辑' : '编辑代理'}
                      icon={editing ? <Check className="h-3.5 w-3.5" /> : <Edit className="h-3.5 w-3.5" />}
                    />
                    <Button
                      shape="square"
                      size="sm"
                      variant="secondary-destructive"
                      aria-label="删除代理"
                      onClick={() => removeEndpointProxy(index)}
                      title="删除代理"
                      icon={<Trash className="h-3.5 w-3.5" />}
                    />
                  </div>
                );
              })}
              {!endpointForm.proxyPool?.length && (
                <div className="rounded-md border border-dashed border-kumo-line py-8 text-center text-xs text-kumo-subtle">
                  暂无代理。点击上方「添加代理」「导入文件」或「订阅链接导入」开始配置。
                </div>
              )}
              {manualProxyEntries.length > PROXY_PREVIEW_LIMIT && (
                <p className="text-xs text-kumo-subtle">
                  仅预览前 {PROXY_PREVIEW_LIMIT} 条，共 {manualProxyEntries.length} 条（保存后全部生效）。
                </p>
              )}
            </div>
          </div>

          <div className="flex justify-end gap-2 border-t border-kumo-line bg-kumo-recessed/25 px-4 py-3 cq-sm:px-5">
            <Dialog.Close
              render={props => (
                <Button size="sm" {...props} variant="secondary">
                  完成
                </Button>
              )}
            />
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 2. Gateway Key Dialogs */}
      <Dialog.Root open={gatewayKeyDialogOpen} onOpenChange={setGatewayKeyDialogOpen}>
        <Dialog className="!w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
            {editingGatewayKey ? '编辑 API 密钥' : '新建 API 密钥'}
          </Dialog.Title>

          <div className="space-y-4">
            <Input
              size="sm"
              label="名称"
              value={gatewayKeyForm.name}
              onChange={e => setGatewayKeyForm({ ...gatewayKeyForm, name: e.target.value })}
              placeholder="如：生产环境、Open WebUI"
              className="w-full text-sm text-kumo-strong"
            />

            <div className="space-y-1.5">
              <Label>
                过期时间
                <span className="font-normal text-kumo-subtle">（可选）</span>
              </Label>
              <div className="flex flex-wrap items-center gap-1.5">
                {[
                  { label: '1 天', days: 1 },
                  { label: '14 天', days: 14 },
                  { label: '30 天', days: 30 },
                  { label: '永久', days: 0 },
                ].map(preset => (
                  <Button
                    key={preset.label}
                    size="xs"
                    variant={
                      (preset.days === 0 && !gatewayKeyForm.expiresAt) ||
                      (preset.days > 0 &&
                        gatewayKeyForm.expiresAt &&
                        Math.abs(
                          new Date(gatewayKeyForm.expiresAt).getTime() -
                            (Date.now() + preset.days * 24 * 60 * 60 * 1000)
                        ) < 60 * 1000)
                        ? 'primary'
                        : 'outline'
                    }
                    onClick={() => applyGatewayKeyExpiryPreset(preset.days)}
                  >
                    {preset.label}
                  </Button>
                ))}
              </div>
              <div className="flex items-center gap-1.5">
                <Popover>
                  <Popover.Trigger
                    render={
                      <Button
                        size="sm"
                        variant="outline"
                        icon={CalendarDotsIcon}
                        className="min-w-0 flex-1 justify-start font-normal"
                      />
                    }
                  >
                    <span className="truncate">
                      {gatewayKeyForm.expiresAt
                        ? formatDateTime(gatewayKeyForm.expiresAt)
                        : '永不过期'}
                    </span>
                  </Popover.Trigger>
                  <Popover.Content className="p-3">
                    <DatePicker
                      size="sm"
                      mode="single"
                      selected={parseLocalDateTime(gatewayKeyForm.expiresAt)}
                      onChange={updateGatewayKeyExpiryDate}
                    />
                    {gatewayKeyForm.expiresAt && (
                      <div className="mt-2 flex justify-end border-t border-kumo-line pt-2">
                        <Button
                          size="xs"
                          variant="ghost"
                          onClick={() =>
                            setGatewayKeyForm(current => ({ ...current, expiresAt: '' }))
                          }
                        >
                          清除
                        </Button>
                      </div>
                    )}
                  </Popover.Content>
                </Popover>
                <Select
                  size="sm"
                  aria-label="过期小时"
                  disabled={!gatewayKeyForm.expiresAt}
                  value={gatewayKeyForm.expiresAt.slice(11, 13)}
                  onValueChange={value => updateGatewayKeyExpiryTime('hour', value)}
                  items={GATEWAY_EXPIRY_HOURS}
                  className="w-[3.25rem] shrink-0"
                />
                <span className="shrink-0 text-sm text-kumo-subtle">:</span>
                <Select
                  size="sm"
                  aria-label="过期分钟"
                  disabled={!gatewayKeyForm.expiresAt}
                  value={gatewayKeyForm.expiresAt.slice(14, 16)}
                  onValueChange={value => updateGatewayKeyExpiryTime('minute', value)}
                  items={GATEWAY_EXPIRY_MINUTES}
                  className="w-[3.25rem] shrink-0"
                />
              </div>
            </div>

            <Collapsible.Root
              open={gatewayKeyAdvancedOpen}
              onOpenChange={setGatewayKeyAdvancedOpen}
            >
              <Collapsible.DefaultTrigger>高级过滤</Collapsible.DefaultTrigger>
              <Collapsible.DefaultPanel className="mt-2">
                <div className="space-y-4">
                  <div className="space-y-1.5">
                    <Label>
                      允许的模型（白名单）
                      <span className="font-normal text-kumo-subtle">（可选）</span>
                    </Label>
                    <div className="flex flex-wrap items-center gap-1.5">
                      {(gatewayKeyForm.allowedModels || []).map(model => (
                        <Badge
                          key={model}
                          variant="outline"
                          className="max-w-full gap-1 font-mono !text-[11px] font-medium"
                        >
                          <span className="truncate">{model}</span>
                          <Button
                            size="xs"
                            shape="square"
                            variant="ghost"
                            aria-label={`移除 ${model}`}
                            onClick={() => removeGatewayKeyListItem('allowedModels', model)}
                            icon={<X className="h-3 w-3" />}
                          />
                        </Badge>
                      ))}
                      <Input
                        size="sm"
                        type="text"
                        value={gatewayKeyModelInput}
                        onChange={e => setGatewayKeyModelInput(e.target.value)}
                        onKeyDown={e => {
                          if (e.key === 'Enter') {
                            e.preventDefault();
                            addGatewayKeyListItem('allowedModels', gatewayKeyModelInput);
                          }
                        }}
                        placeholder="输入模型名后回车"
                        className="w-full font-mono text-[0.85em] text-kumo-strong"
                      />
                    </div>
                  </div>
                  <div className="space-y-1.5">
                    <Label>
                      允许的端点（白名单）
                      <span className="font-normal text-kumo-subtle">（可选）</span>
                    </Label>
                    <div className="flex flex-wrap items-center gap-1.5">
                      {(gatewayKeyForm.allowedEndpoints || []).map(endpointId => (
                        <Badge
                          key={endpointId}
                          variant="outline"
                          className="max-w-full gap-1 font-mono !text-[11px] font-medium"
                        >
                          <span className="truncate">{endpointId}</span>
                          <Button
                            size="xs"
                            shape="square"
                            variant="ghost"
                            aria-label={`移除 ${endpointId}`}
                            onClick={() => removeGatewayKeyListItem('allowedEndpoints', endpointId)}
                            icon={<X className="h-3 w-3" />}
                          />
                        </Badge>
                      ))}
                      <Input
                        size="sm"
                        type="text"
                        value={gatewayKeyEndpointInput}
                        onChange={e => setGatewayKeyEndpointInput(e.target.value)}
                        onKeyDown={e => {
                          if (e.key === 'Enter') {
                            e.preventDefault();
                            addGatewayKeyListItem('allowedEndpoints', gatewayKeyEndpointInput);
                          }
                        }}
                        placeholder="输入端点 ID 后回车"
                        className="w-full font-mono text-[0.85em] text-kumo-strong"
                      />
                    </div>
                  </div>
                  <div className="space-y-1.5">
                    <Label>
                    Token 配额
                    <span className="font-normal text-kumo-subtle">（可选）</span>
                  </Label>
                    <Input
                      size="sm"
                      type="number"
                      min="0"
                      value={gatewayKeyForm.maxTokensQuota}
                      onChange={e =>
                        setGatewayKeyForm({ ...gatewayKeyForm, maxTokensQuota: e.target.value })
                      }
                      placeholder="0 = 不限制"
                      className="w-full font-mono text-[0.85em] text-kumo-strong"
                    />
                  </div>
                </div>
              </Collapsible.DefaultPanel>
            </Collapsible.Root>

            {gatewayKeyFormError && (
              <p className="text-sm font-semibold text-kumo-danger">{gatewayKeyFormError}</p>
            )}
            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close
                render={props => (
                  <Button size="sm" {...props} variant="secondary">
                    取消
                  </Button>
                )}
              />
              <Button
                size="sm"
                variant="primary"
                disabled={gatewayKeySaving}
                onClick={saveGatewayKey}
              >
                {gatewayKeySaving ? '保存中...' : '保存密钥'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      <Dialog.Root open={!!newGatewayKey} onOpenChange={open => !open && setNewGatewayKey(null)}>
        <Dialog className="!w-[min(34rem,calc(100vw-2rem))] !max-w-[min(34rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="mb-1 text-sm font-semibold text-kumo-strong">
            API 密钥已创建
          </Dialog.Title>
          <Dialog.Description className="mb-4 text-sm text-kumo-subtle">
            可立即复制，也可稍后从 API 密钥列表查看并复制。
          </Dialog.Description>
          <div className="space-y-4">
            <p className="text-sm font-medium text-kumo-strong">
              {newGatewayKey?.name || 'API Key'}
            </p>
            <ClipboardText
              size="sm"
              text={newGatewayKey?.apiKey || ''}
              className="min-w-0 w-full"
              tooltip={{ text: '复制 API Key', copiedText: 'API Key 已复制' }}
              labels={{ copyAction: '复制 API Key' }}
            />
            <div className="flex justify-end">
              <Dialog.Close
                render={props => (
                  <Button size="sm" variant="primary" {...props}>
                    我已保存
                  </Button>
                )}
              />
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 3. Health Check Config Dialog */}
      <Dialog.Root open={healthCheckModal} onOpenChange={setHealthCheckModal}>
        <Dialog className="!w-[min(32rem,calc(100vw-2rem))] !max-w-[min(32rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="text-sm font-semibold text-kumo-strong mb-1">
            模型健康检测
          </Dialog.Title>
          <Dialog.Description className="text-sm text-kumo-subtle mb-4">
            按设定并发逐批发送轻量请求，测试每个模型的可用性与延迟。
          </Dialog.Description>

          <div className="space-y-4">
            <div className="bg-kumo-warning/10 border border-kumo-warning/20 text-kumo-warning px-3 py-2 text-sm space-y-1">
              <p className="font-semibold flex items-center gap-1.5">
                <AlertTriangle className="w-3.5 h-3.5" />
                警告
              </p>
              <p>批量检测会发送真实请求；并发数越高，越容易触发供应商限流、风控或短时失败。</p>
            </div>

            <div className="flex items-center justify-between text-sm">
              <span className="font-semibold text-kumo-strong">检测方式</span>
              <StatusBadge tone="info">后端批量检测</StatusBadge>
            </div>

            <div className="flex items-center justify-between text-sm">
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
                  className="w-16 text-kumo-strong text-sm px-2 py-1 text-center"
                />
                <span className="text-kumo-subtle">秒</span>
              </div>
            </div>

            <div className="flex items-center justify-between text-sm">
              <span className="font-semibold text-kumo-strong">并发数</span>
              <div className="flex items-center gap-1.5">
                <Input
                  size="sm"
                  aria-label="健康检测并发数"
                  type="number"
                  value={healthCheckForm.concurrency}
                  onChange={e =>
                    setHealthCheckForm({
                      ...healthCheckForm,
                      concurrency: Number(e.target.value),
                    })
                  }
                  min={1}
                  max={30}
                  className="w-16 text-kumo-strong text-sm px-2 py-1 text-center"
                />
                <span className="text-kumo-subtle">个请求</span>
              </div>
            </div>

            <p className="text-xs text-kumo-subtle">
              默认并发 {DEFAULT_MODEL_HEALTH_CONCURRENCY}、超时{' '}
              {DEFAULT_MODEL_HEALTH_TIMEOUT_SECONDS} 秒；批量检测全部启用端点上的模型，完成后统一回填结果。
            </p>

            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close
                render={props => (
                  <Button size="sm" {...props} variant="secondary">
                    取消
                  </Button>
                )}
              />
              <Button
                size="sm"
                variant="primary"
                disabled={modelHealthBatchLoading}
                onClick={startBatchHealthCheck}
              >
                {modelHealthBatchLoading ? '检测中...' : '开始检测'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>
    </PageStack>
  );
}

export default OpenAIPage;
