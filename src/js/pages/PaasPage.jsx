import React, { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Badge, ClipboardText, Empty, LayerCard, Link, Loader, Tabs, Text, Toolbar } from '@cloudflare/kumo';
import { AnimatedCollapse } from '../components/AnimatedCollapse.jsx';
import useStore from '../store.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { getStatusPillClass, ResponsiveSearchInput, SectionCard, TabBarOverflowActions, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
import {
  Server,
  Users,
  Plus,
  Trash,
  Search,
  Play,
  Pause,
  Square,
  Lock,
  Info,
  ExternalLink,
  Upload,
  Download,
  X,
  ChevronDown,
  ChevronUp,
  RefreshCw,
  History,
  Activity,
  Settings,
  Mail,
  Folder,
  FileText,
  Save,
  Globe,
  Terminal,
  Rocket,
  KoyebBrand,
  FlyIoBrand
} from '../components/Icons.jsx';

const DEFAULT_PAAS_REFRESH_INTERVAL_SEC = 30;
const MIN_PAAS_REFRESH_INTERVAL_SEC = 5;
const MAX_PAAS_REFRESH_INTERVAL_SEC = 3600;

const clampRefreshIntervalSec = (value) => (
  Math.min(MAX_PAAS_REFRESH_INTERVAL_SEC, Math.max(MIN_PAAS_REFRESH_INTERVAL_SEC, Math.round(value)))
);

const normalizeRefreshIntervalInputSec = (value) => {
  const numeric = Number(value);
  if (!Number.isFinite(numeric) || numeric <= 0) return DEFAULT_PAAS_REFRESH_INTERVAL_SEC;
  return clampRefreshIntervalSec(numeric);
};

const normalizeStoredRefreshIntervalSec = (value) => {
  const numeric = Number(value);
  if (!Number.isFinite(numeric) || numeric <= 0) return DEFAULT_PAAS_REFRESH_INTERVAL_SEC;

  let seconds = numeric >= 1000 ? numeric / 1000 : numeric;
  while (
    seconds > MAX_PAAS_REFRESH_INTERVAL_SEC &&
    Number.isInteger(seconds / 1000) &&
    seconds / 1000 >= MIN_PAAS_REFRESH_INTERVAL_SEC
  ) {
    seconds /= 1000;
  }

  return clampRefreshIntervalSec(seconds);
};

const normalizeFlyImageForInput = (image) => {
  const value = String(image || '').trim();
  const mirrorPrefix = 'docker-hub-mirror.fly.io/';
  if (value.startsWith(mirrorPrefix)) {
    return value.slice(mirrorPrefix.length);
  }
  return value;
};

function PaasPage() {
  const { theme } = useStore();
  const { isArmed, confirmPress } = useConfirmPress();
  const [activeTab, setActiveTab] = useState('fly'); // 'fly' | 'koyeb' | 'config'
  const didInitialLoadRef = useRef(false);

  // Global Auth Header
  const getAuthHeaders = useCallback(() => {
    return {
      'Content-Type': 'application/json',
    };
  }, []);

  // Format region code helper
  const formatRegion = (region) => {
    if (!region) return '';
    return region.toUpperCase();
  };

  // ==================== 1. Settings & Intervals State ====================
  const [koyebIntervalSec, setKoyebIntervalSec] = useState(30);
  const [flyIntervalSec, setFlyIntervalSec] = useState(30);
  const koyebIntervalSecRef = useRef(koyebIntervalSec);
  const flyIntervalSecRef = useRef(flyIntervalSec);
  const koyebRefreshingRef = useRef(false);
  const flyRefreshingRef = useRef(false);
  const koyebAccountsRef = useRef([]);
  const flyAccountsRef = useRef([]);

  useEffect(() => {
    koyebIntervalSecRef.current = normalizeRefreshIntervalInputSec(koyebIntervalSec);
  }, [koyebIntervalSec]);

  useEffect(() => {
    flyIntervalSecRef.current = normalizeRefreshIntervalInputSec(flyIntervalSec);
  }, [flyIntervalSec]);

  // Load Settings
  const loadSettings = useCallback(async () => {
    try {
      const response = await fetch('/api/settings', {
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        const result = await response.json();
        if (result.success && result.data) {
          const settings = result.data;
          if (settings.koyebRefreshInterval) {
            const sec = normalizeStoredRefreshIntervalSec(settings.koyebRefreshInterval);
            setKoyebIntervalSec(sec);
          }
          if (settings.flyRefreshInterval) {
            const sec = normalizeStoredRefreshIntervalSec(settings.flyRefreshInterval);
            setFlyIntervalSec(sec);
          }
        }
      }
    } catch (e) {
      console.error('加载 PaaS 设置失败:', e);
    }
  }, [getAuthHeaders]);

  const saveSettings = async () => {
    try {
      const koyebSec = normalizeRefreshIntervalInputSec(koyebIntervalSec);
      const flySec = normalizeRefreshIntervalInputSec(flyIntervalSec);
      const settings = {
        koyebRefreshInterval: koyebSec * 1000,
        flyRefreshInterval: flySec * 1000,
      };
      const response = await fetch('/api/settings', {
        method: 'PATCH',
        headers: getAuthHeaders(),
        body: JSON.stringify(settings),
      });
      if (response.ok) {
        toast.success('PaaS 自动刷新配置已保存');
        setKoyebIntervalSec(koyebSec);
        setFlyIntervalSec(flySec);
      } else {
        toast.error('保存失败');
      }
    } catch (error) {
      toast.error('保存出错: ' + error.message);
    }
  };

  // ==================== 2. Log Viewer State & Component ====================
  const [logViewerOpen, setLogViewerOpen] = useState(false);
  const [logTitle, setLogTitle] = useState('日志查看器');
  const [logSubtitle, setLogSubtitle] = useState('');
  const [logs, setLogs] = useState([]);
  const [logLoading, setLogLoading] = useState(false);
  const [logFilterText, setLogFilterText] = useState('');
  const [logLevelFilter, setLogLevelFilter] = useState('ALL');
  const [logWrapText, setLogWrapText] = useState(true);
  const [logAutoScroll, setLogAutoScroll] = useState(true);
  const [logTailActive, setLogTailActive] = useState(false);
  const [logTailConnected, setLogTailConnected] = useState(false);
  const logContainerRef = useRef(null);
  const logTailAbortRef = useRef(null);

  const appendLogs = useCallback((newLogs) => {
    const processEntry = (log) => {
      if (typeof log === 'string') {
        const timeMatch = log.match(/^\(?(\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2})\]?/);
        const levelMatch = log.match(/\[(INFO|WARN|ERROR|DEBUG|FATAL)\]/i);
        return {
          id: Date.now() + Math.random().toString(36).substring(2, 9),
          timestamp: timeMatch ? new Date(timeMatch[1]).getTime() : Date.now(),
          level: levelMatch ? levelMatch[1].toUpperCase() : 'INFO',
          message: log,
        };
      } else if (typeof log === 'object') {
        let messageStr = log.message;
        if (typeof messageStr === 'object' && messageStr !== null) {
          messageStr = JSON.stringify(messageStr);
        } else if (messageStr === undefined || messageStr === null) {
          messageStr = JSON.stringify(log);
        }
        return {
          id: log.id || Date.now() + Math.random().toString(36).substring(2, 9),
          timestamp: log.timestamp || Date.now(),
          level: log.level || 'INFO',
          message: String(messageStr),
        };
      }
      return null;
    };

    setLogs((prev) => {
      const processed = Array.isArray(newLogs) ? newLogs.map(processEntry).filter(Boolean) : [processEntry(newLogs)].filter(Boolean);
      const combined = [...prev, ...processed];
      if (combined.length > 3000) {
        return combined.slice(combined.length - 3000);
      }
      return combined;
    });
  }, []);

  const openLogViewer = useCallback(async ({ title, subtitle, fetcher }) => {
    setLogTitle(title || '日志查看器');
    setLogSubtitle(subtitle || '');
    setLogs([]);
    setLogViewerOpen(true);
    setLogLoading(true);

    try {
      const result = await fetcher();
      if (Array.isArray(result)) {
        appendLogs(result);
      } else {
        appendLogs([result]);
      }
    } catch (error) {
      appendLogs({
        timestamp: Date.now(),
        level: 'ERROR',
        message: '获取日志发生错误: ' + error.message,
      });
    } finally {
      setLogLoading(false);
    }
  }, [appendLogs]);

  const stopLogTail = useCallback(() => {
    logTailAbortRef.current?.abort();
    logTailAbortRef.current = null;
    setLogTailActive(false);
    setLogTailConnected(false);
  }, []);

  const parseLogTailEvent = useCallback((eventText) => {
    const dataLines = eventText.split('\n').filter((l) => l.startsWith('data:')).map((l) => l.slice(5).trim());
    for (const data of dataLines) {
      if (!data || data === '[DONE]') continue;
      try {
        const payload = JSON.parse(data);
        const entry = payload.result || payload;
        const msg = entry.msg ?? (typeof entry.message === 'string' ? entry.message : null);
        if (msg == null) continue;
        appendLogs({
          timestamp: entry.created_at ? new Date(entry.created_at).getTime() : Date.now(),
          level: 'INFO',
          message: msg,
        });
      } catch (e) {
        // 非 JSON（如心跳/注释行）忽略
      }
    }
  }, [appendLogs]);

  const startKoyebLogTail = useCallback((account, service) => {
    stopLogTail();
    setLogTitle(`实时日志: ${service.name}`);
    setLogSubtitle(`${account.name} · 实时跟随中`);
    setLogs([]);
    setLogViewerOpen(true);
    setLogLoading(true);
    const controller = new AbortController();
    logTailAbortRef.current = controller;
    const url = `/api/koyeb/services/${service._id}/logs/tail?accountId=${account.id}&stream=stdout`;
    fetch(url, { headers: getAuthHeaders(), signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) {
          const errText = await response.text().catch(() => '');
          throw new Error(`HTTP ${response.status}${errText ? ': ' + errText : ''}`);
        }
        if (!response.body) throw new Error('浏览器不支持流式读取');
        setLogLoading(false);
        setLogTailActive(true);
        setLogTailConnected(true);
        const reader = response.body.getReader();
        const decoder = new TextDecoder('utf-8');
        let buffer = '';
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          buffer += decoder.decode(value, { stream: true });
          let boundary;
          while ((boundary = buffer.indexOf('\n\n')) !== -1) {
            const event = buffer.slice(0, boundary);
            buffer = buffer.slice(boundary + 2);
            parseLogTailEvent(event);
          }
        }
      })
      .catch((error) => {
        if (error.name === 'AbortError') return;
        appendLogs({ timestamp: Date.now(), level: 'ERROR', message: '实时日志连接中断: ' + error.message });
      })
      .finally(() => {
        setLogLoading(false);
        setLogTailActive(false);
        setLogTailConnected(false);
      });
  }, [getAuthHeaders, stopLogTail, parseLogTailEvent, appendLogs]);

  // Filtered logs
  const filteredLogs = useMemo(() => {
    let result = logs;
    if (logLevelFilter !== 'ALL') {
      result = result.filter(l => String(l.level).toUpperCase() === logLevelFilter);
    }
    if (logFilterText) {
      const lower = logFilterText.toLowerCase();
      result = result.filter(l => String(l.message).toLowerCase().includes(lower));
    }
    return result;
  }, [logs, logLevelFilter, logFilterText]);

  // Auto-scroll inside logs
  useEffect(() => {
    if (logAutoScroll && logContainerRef.current) {
      logContainerRef.current.scrollTop = logContainerRef.current.scrollHeight;
    }
  }, [filteredLogs, logAutoScroll]);

  const downloadLogs = () => {
    if (logs.length === 0) {
      toast.info('暂无日志', { isManual: true });
      return;
    }
    const content = logs
      .map((l) => `[${new Date(l.timestamp).toLocaleString()}] [${l.level}] ${l.message}`)
      .join('\n');
    const blob = new Blob([content], { type: 'text/plain' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `paas-logs-${Date.now()}.log`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
  };

  // ==================== 3. Koyeb State & Logic ====================
  const [koyebAccounts, setKoyebAccounts] = useState([]);
  const [koyebLoading, setKoyebLoading] = useState(false);
  const [koyebRefreshing, setKoyebRefreshing] = useState(false);
  const [koyebExpandedAccounts, setKoyebExpandedAccounts] = useState({});

  // Local Snapshots Cache
  const saveToKoyebCache = useCallback((data) => {
    try {
      localStorage.setItem('koyeb_cache', JSON.stringify({
        timestamp: Date.now(),
        accounts: data,
      }));
    } catch (e) {
      console.warn('缓存 Koyeb 数据失败:', e);
    }
  }, []);

  const loadFromKoyebCache = useCallback(() => {
    try {
      const cached = localStorage.getItem('koyeb_cache');
      if (cached) {
        const data = JSON.parse(cached);
        if (data.accounts) {
          setKoyebAccounts(data.accounts);
          return true;
        }
      }
    } catch (e) {
      console.warn('读取 Koyeb 缓存失败:', e);
    }
    return false;
  }, []);

  // Load Koyeb Data Snapshot
  const loadKoyebData = useCallback(async (isManual = false) => {
    if (koyebRefreshingRef.current) return;
    koyebRefreshingRef.current = true;
    setKoyebRefreshing(true);
    if (isManual || koyebAccountsRef.current.length === 0) {
      setKoyebLoading(true);
    }

    try {
      const response = await fetch('/api/koyeb/data', {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (result.success) {
        const accs = result.accounts || [];
        // Map fields to prevent UI errors on app/service fields
        const formatted = accs.map(acc => ({
          ...acc,
          projects: (acc.projects || []).map(app => ({
            ...app,
            isEditing: false,
            editingName: app.name,
            services: (app.services || []).map(svc => ({
              ...svc,
              isEditing: false,
              editingName: svc.name,
              loadingInstances: false,
              showInstances: false,
              instances: svc.instances || [],
              metrics: svc.metrics || null,
            }))
          }))
        }));

        setKoyebAccounts(formatted);
        saveToKoyebCache(formatted);
        if (isManual) {
          toast.success('Koyeb 监控数据已刷新');
        }
      } else {
        throw new Error(result.error || '获取失败');
      }
    } catch (error) {
      console.error('Koyeb 刷新出错:', error);
      if (isManual) {
        toast.error('刷新 Koyeb 数据失败: ' + error.message);
      }
    } finally {
      setKoyebLoading(false);
      koyebRefreshingRef.current = false;
      setKoyebRefreshing(false);
    }
  }, [getAuthHeaders, saveToKoyebCache]);

  const toggleKoyebAccount = (accountName) => {
    setKoyebExpandedAccounts(prev => ({
      ...prev,
      [accountName]: !prev[accountName]
    }));
  };

  const isKoyebAccountExpanded = (accountName) => {
    if (koyebExpandedAccounts[accountName] === undefined) {
      return true; // default expanded
    }
    return koyebExpandedAccounts[accountName];
  };

  // Koyeb Service & App Operations
  const restartKoyebService = async (account, app, service) => {
    const action = service.status === 'SUSPENDED' ? '启动' : '重启';
    try {
      const response = await fetch(`/api/koyeb/services/${service._id}/restart`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success(`服务 ${service.name} ${action}中...`);
        // Update locally
        setKoyebAccounts(prev => prev.map(acc => {
          if (acc.name !== account.name) return acc;
          return {
            ...acc,
            projects: acc.projects.map(p => {
              if (p._id !== app._id) return p;
              return {
                ...p,
                services: p.services.map(s => {
                  if (s._id !== service._id) return s;
                  return { ...s, status: 'STARTING' };
                })
              };
            })
          };
        }));
        setTimeout(() => loadKoyebData(false), 3000);
      } else {
        toast.error(`${action}失败: ${result.error}`);
      }
    } catch (error) {
      toast.error(`${action}请求出错: ` + error.message);
    }
  };

  const redeployKoyebService = async (account, app, service) => {
    if (!(await dialog.confirm(`重新部署 Koyeb 服务 "${service.name}"？`))) return;
    try {
      const response = await fetch(`/api/koyeb/services/${service._id}/redeploy`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success(`服务 ${service.name} 重新部署中...`);
        setTimeout(() => loadKoyebData(false), 3000);
      } else {
        toast.error(`部署失败: ${result.error}`);
      }
    } catch (error) {
      toast.error('部署请求出错: ' + error.message);
    }
  };

  const fetchKoyebServiceInstances = async (account, service) => {
    // If showing, toggle off
    if (service.showInstances) {
      setKoyebAccounts(prev => prev.map(acc => ({
        ...acc,
        projects: acc.projects.map(p => ({
          ...p,
          services: p.services.map(s => {
            if (s._id !== service._id) return s;
            return { ...s, showInstances: false };
          })
        }))
      })));
      return;
    }

    // Load instances
    setKoyebAccounts(prev => prev.map(acc => ({
      ...acc,
      projects: acc.projects.map(p => ({
        ...p,
        services: p.services.map(s => {
          if (s._id !== service._id) return s;
          return { ...s, loadingInstances: true };
        })
      }))
    })));

    try {
      const response = await fetch(`/api/koyeb/services/${service._id}/instances?accountId=${account.id}`, {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (result.success) {
        setKoyebAccounts(prev => prev.map(acc => ({
          ...acc,
          projects: acc.projects.map(p => ({
            ...p,
            services: p.services.map(s => {
              if (s._id !== service._id) return s;
              return {
                ...s,
                instances: result.instances || [],
                showInstances: true,
                loadingInstances: false
              };
            })
          }))
        })));
      } else {
        toast.error('获取实例列表失败: ' + result.error);
        setKoyebAccounts(prev => prev.map(acc => ({
          ...acc,
          projects: acc.projects.map(p => ({
            ...p,
            services: p.services.map(s => {
              if (s._id !== service._id) return s;
              return { ...s, loadingInstances: false };
            })
          }))
        })));
      }
    } catch (error) {
      toast.error('获取实例请求错误: ' + error.message);
      setKoyebAccounts(prev => prev.map(acc => ({
        ...acc,
        projects: acc.projects.map(p => ({
          ...p,
          services: p.services.map(s => {
            if (s._id !== service._id) return s;
            return { ...s, loadingInstances: false };
          })
        }))
      })));
    }
  };

  const showKoyebServiceLogs = (account, app, service) => {
    openLogViewer({
      title: `容器日志: ${service.name}`,
      subtitle: `${app.name} / ${account.name}`,
      fetcher: async () => {
        const response = await fetch(`/api/koyeb/services/${service._id}/logs?accountId=${account.id}`, {
          headers: getAuthHeaders(),
        });
        const result = await response.json();
        if (result.success) {
          const rawLogs = result.logs || [];
          return rawLogs.map(l => {
            const entry = l.result || l;
            return {
              timestamp: entry.created_at ? new Date(entry.created_at).getTime() : Date.now(),
              message: entry.msg || JSON.stringify(entry),
              level: 'INFO',
            };
          });
        }
        throw new Error(result.error || '获取日志失败');
      }
    });
  };

  // Renaming app and services
  const startEditKoyebAppName = (app) => {
    setKoyebAccounts(prev => prev.map(acc => ({
      ...acc,
      projects: acc.projects.map(p => {
        if (p._id !== app._id) return p;
        return { ...p, isEditing: true, editingName: p.name };
      })
    })));
  };

  const cancelEditKoyebAppName = (app) => {
    setKoyebAccounts(prev => prev.map(acc => ({
      ...acc,
      projects: acc.projects.map(p => {
        if (p._id !== app._id) return p;
        return { ...p, isEditing: false };
      })
    })));
  };

  const renameKoyebApp = async (account, app) => {
    const finalName = app.editingName?.trim();
    if (!finalName || finalName === app.name) {
      cancelEditKoyebAppName(app);
      return;
    }
    // Check duplicates
    if (account.projects.some(p => p._id !== app._id && p.name.toLowerCase() === finalName.toLowerCase())) {
      toast.error(`应用名称 "${finalName}" 已存在`);
      return;
    }

    try {
      const response = await fetch(`/api/koyeb/apps/${app._id}/rename`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id, name: finalName }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('应用重命名成功');
        setKoyebAccounts(prev => prev.map(acc => ({
          ...acc,
          projects: acc.projects.map(p => {
            if (p._id !== app._id) return p;
            return { ...p, name: finalName, isEditing: false };
          })
        })));
      } else {
        throw new Error(result.error);
      }
    } catch (e) {
      toast.error('重命名失败: ' + e.message);
      cancelEditKoyebAppName(app);
    }
  };

  const startEditKoyebServiceName = (service) => {
    setKoyebAccounts(prev => prev.map(acc => ({
      ...acc,
      projects: acc.projects.map(p => ({
        ...p,
        services: p.services.map(s => {
          if (s._id !== service._id) return s;
          return { ...s, isEditing: true, editingName: s.name };
        })
      }))
    })));
  };

  const cancelEditKoyebServiceName = (service) => {
    setKoyebAccounts(prev => prev.map(acc => ({
      ...acc,
      projects: acc.projects.map(p => ({
        ...p,
        services: p.services.map(s => {
          if (s._id !== service._id) return s;
          return { ...s, isEditing: false };
        })
      }))
    })));
  };

  const renameKoyebService = async (account, app, service) => {
    const finalName = service.editingName?.trim();
    if (!finalName || finalName === service.name) {
      cancelEditKoyebServiceName(service);
      return;
    }
    if (app.services.some(s => s._id !== service._id && s.name.toLowerCase() === finalName.toLowerCase())) {
      toast.error(`服务名称 "${finalName}" 已存在`);
      return;
    }

    try {
      const response = await fetch(`/api/koyeb/services/${service._id}/rename`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id, name: finalName }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('服务重命名成功');
        setKoyebAccounts(prev => prev.map(acc => ({
          ...acc,
          projects: acc.projects.map(p => ({
            ...p,
            services: p.services.map(s => {
              if (s._id !== service._id) return s;
              return { ...s, name: finalName, isEditing: false };
            })
          }))
        })));
      } else {
        throw new Error(result.error);
      }
    } catch (e) {
      toast.error('服务重命名失败: ' + e.message);
      cancelEditKoyebServiceName(service);
    }
  };

  const getKoyebStatusBadge = (status) => {
    const up = String(status || '').toUpperCase();
    if (up === 'RUNNING' || up === 'HEALTHY') {
      return getStatusPillClass('success');
    }
    if (up === 'STARTING') {
      return getStatusPillClass('info');
    }
    if (up === 'SUSPENDED' || up === 'PAUSED' || up === 'STOPPED') {
      return getStatusPillClass('warning');
    }
    if (up === 'ERROR' || up === 'ERRORED' || up === 'UNHEALTHY') {
      return getStatusPillClass('danger');
    }
    return getStatusPillClass('neutral');
  };

  const getKoyebStatusText = (status) => {
    const map = {
      RUNNING: '运行中',
      HEALTHY: '运行中',
      STARTING: '启动中',
      SUSPENDED: '已暂停',
      PAUSED: '已暂停',
      STOPPED: '已停止',
      ERROR: '错误',
      ERRORED: '错误',
      UNHEALTHY: '异常',
    };
    return map[String(status).toUpperCase()] || status || '未知';
  };

  const getKoyebStatusTone = (status) => {
    const up = String(status || '').toUpperCase();
    if (up === 'RUNNING' || up === 'HEALTHY') return 'success';
    if (up === 'STARTING') return 'info';
    if (up === 'SUSPENDED' || up === 'PAUSED' || up === 'STOPPED') return 'warning';
    if (up === 'ERROR' || up === 'ERRORED' || up === 'UNHEALTHY') return 'danger';
    return 'neutral';
  };

  const koyebServiceTypeVariant = (type) => {
    const value = String(type || 'web').toLowerCase();
    if (value === 'web') return 'blue';
    if (value === 'worker') return 'purple';
    if (value === 'job') return 'orange';
    return 'neutral';
  };

  // ==================== 3.5 Koyeb Advanced Operations ====================
  // 服务配置编辑（镜像 / 命令 / 环境变量 / 端口 / 区域 / 实例规格）
  const [koyebConfigTarget, setKoyebConfigTarget] = useState(null);
  const [koyebConfigForm, setKoyebConfigForm] = useState({ image: '', command: '', args: '', env: '', ports: '', regions: '', instanceType: '', skipBuild: false });
  const [koyebConfigSaving, setKoyebConfigSaving] = useState(false);
  const [koyebConfigError, setKoyebConfigError] = useState('');

  // 部署历史 + 取消
  const [koyebDeployTarget, setKoyebDeployTarget] = useState(null);
  const [koyebDeployments, setKoyebDeployments] = useState([]);
  const [koyebDeployLoading, setKoyebDeployLoading] = useState(false);
  const [koyebDeployError, setKoyebDeployError] = useState('');

  // 手动扩容
  const [koyebScaleTarget, setKoyebScaleTarget] = useState(null);
  const [koyebScaleScope, setKoyebScaleScope] = useState('');
  const [koyebScaleInstances, setKoyebScaleInstances] = useState(1);
  const [koyebScaleSaving, setKoyebScaleSaving] = useState(false);
  const [koyebScaleError, setKoyebScaleError] = useState('');

  // 域名管理
  const [koyebDomainsTarget, setKoyebDomainsTarget] = useState(null);
  const [koyebDomains, setKoyebDomains] = useState([]);
  const [koyebDomainsLoading, setKoyebDomainsLoading] = useState(false);
  const [koyebDomainsError, setKoyebDomainsError] = useState('');
  const [koyebNewDomain, setKoyebNewDomain] = useState('');

  // Secrets 管理
  const [koyebSecretsTarget, setKoyebSecretsTarget] = useState(null);
  const [koyebSecrets, setKoyebSecrets] = useState([]);
  const [koyebSecretsLoading, setKoyebSecretsLoading] = useState(false);
  const [koyebSecretsError, setKoyebSecretsError] = useState('');
  const [koyebNewSecretName, setKoyebNewSecretName] = useState('');
  const [koyebNewSecretValue, setKoyebNewSecretValue] = useState('');

  // 创建服务
  const [koyebCreateTarget, setKoyebCreateTarget] = useState(null);
  const [koyebCreateForm, setKoyebCreateForm] = useState({ name: '', type: 'web', image: '', command: '', env: '', ports: '', regions: '', instanceType: '' });
  const [koyebCatalogInstances, setKoyebCatalogInstances] = useState([]);
  const [koyebCatalogRegions, setKoyebCatalogRegions] = useState([]);
  const [koyebCreateSaving, setKoyebCreateSaving] = useState(false);
  const [koyebCreateError, setKoyebCreateError] = useState('');

  // 用量明细
  const [koyebUsageTarget, setKoyebUsageTarget] = useState(null);
  const [koyebUsageData, setKoyebUsageData] = useState(null);
  const [koyebUsageLoading, setKoyebUsageLoading] = useState(false);
  const [koyebUsageError, setKoyebUsageError] = useState('');

  const koyebParseDefinition = (service) => {
    const def = service?.latestDeployment?.definition || {};
    const docker = def.docker || {};
    const envText = (def.env || []).map((e) => {
      const key = e?.key ?? '';
      const val = e?.value ?? '';
      return val ? `${key}=${val}` : key;
    }).join('\n');
    const portsText = (def.ports || []).map((p) => `${p.port}${p.protocol ? ':' + p.protocol : ''}`).join(',');
    const regionsText = (def.regions || []).join(',');
    const instanceType = (def.instance_types || [])[0]?.type || '';
    return {
      image: docker.image || '',
      command: docker.command || '',
      args: (docker.args || []).join(','),
      env: envText,
      ports: portsText,
      regions: regionsText,
      instanceType,
    };
  };

  const openKoyebConfig = (account, service) => {
    setKoyebConfigForm({ ...koyebParseDefinition(service), skipBuild: false });
    setKoyebConfigError('');
    setKoyebConfigTarget({ account, service });
  };

  const saveKoyebConfig = async () => {
    const { account, service } = koyebConfigTarget;
    const env = koyebConfigForm.env.split('\n').map((l) => l.trim()).filter(Boolean);
    const ports = koyebConfigForm.ports.split(',').map((s) => s.trim()).filter(Boolean).map((s) => {
      const [port, protocol] = s.split(':');
      const p = Number(port);
      return p ? { port: p, protocol: protocol || 'http' } : null;
    }).filter(Boolean);
    const regions = koyebConfigForm.regions.split(',').map((s) => s.trim()).filter(Boolean);
    const args = koyebConfigForm.args.split(',').map((s) => s.trim()).filter(Boolean);
    setKoyebConfigSaving(true);
    setKoyebConfigError('');
    try {
      const response = await fetch(`/api/koyeb/services/${service._id}/update`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          accountId: account.id,
          image: koyebConfigForm.image.trim(),
          command: koyebConfigForm.command,
          args,
          env,
          ports,
          regions,
          instanceType: koyebConfigForm.instanceType,
          skipBuild: koyebConfigForm.skipBuild,
        }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success(`服务 ${service.name} 配置已更新，正在部署...`);
        setKoyebConfigTarget(null);
        setTimeout(() => loadKoyebData(false), 2500);
      } else {
        setKoyebConfigError(result.error || '更新失败');
      }
    } catch (e) {
      setKoyebConfigError('请求出错: ' + e.message);
    } finally {
      setKoyebConfigSaving(false);
    }
  };

  const openKoyebDeployments = async (account, service) => {
    setKoyebDeployTarget({ account, service });
    setKoyebDeployments([]);
    setKoyebDeployError('');
    setKoyebDeployLoading(true);
    try {
      const response = await fetch(`/api/koyeb/services/${service._id}/deployments?accountId=${account.id}&limit=20`, { headers: getAuthHeaders() });
      const result = await response.json();
      if (result.success) {
        setKoyebDeployments(result.deployments || []);
      } else {
        setKoyebDeployError(result.error || '获取失败');
      }
    } catch (e) {
      setKoyebDeployError('请求出错: ' + e.message);
    } finally {
      setKoyebDeployLoading(false);
    }
  };

  const cancelKoyebDeployment = async (deployment) => {
    if (!(await dialog.confirm(`取消部署 ${deployment.id}？`))) return;
    const { account, service } = koyebDeployTarget;
    try {
      const response = await fetch(`/api/koyeb/deployments/${deployment.id}/cancel`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('已提交取消');
        openKoyebDeployments(account, service);
      } else {
        toast.error('取消失败: ' + result.error);
      }
    } catch (e) {
      toast.error('请求出错: ' + e.message);
    }
  };

  const openKoyebScale = async (account, service) => {
    setKoyebScaleTarget({ account, service });
    setKoyebScaleScope('');
    setKoyebScaleInstances(1);
    setKoyebScaleError('');
    try {
      const response = await fetch(`/api/koyeb/services/${service._id}/scale?accountId=${account.id}`, { headers: getAuthHeaders() });
      const result = await response.json();
      if (result.success && result.scale?.scalings?.length) {
        const s = result.scale.scalings[0];
        setKoyebScaleScope((s.scopes || []).join(','));
        setKoyebScaleInstances(s.instances ?? 1);
      }
    } catch (e) {
      setKoyebScaleError('读取当前扩容配置失败: ' + e.message);
    }
  };

  const saveKoyebScale = async () => {
    const { account, service } = koyebScaleTarget;
    setKoyebScaleSaving(true);
    setKoyebScaleError('');
    try {
      const scopes = koyebScaleScope.split(',').map((s) => s.trim()).filter(Boolean);
      const response = await fetch(`/api/koyeb/services/${service._id}/scale`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id, scalings: [{ scopes, instances: Number(koyebScaleInstances) || 1 }] }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('扩容配置已保存');
        setKoyebScaleTarget(null);
      } else {
        setKoyebScaleError(result.error || '保存失败');
      }
    } catch (e) {
      setKoyebScaleError('请求出错: ' + e.message);
    } finally {
      setKoyebScaleSaving(false);
    }
  };

  const resetKoyebScale = async () => {
    if (!(await dialog.confirm('重置手动扩容配置，恢复为部署定义中的扩缩容设置？'))) return;
    const { account, service } = koyebScaleTarget;
    try {
      const response = await fetch(`/api/koyeb/services/${service._id}/scale`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('已重置扩容配置');
        setKoyebScaleTarget(null);
      } else {
        toast.error('重置失败: ' + result.error);
      }
    } catch (e) {
      toast.error('请求出错: ' + e.message);
    }
  };

  const openKoyebDomains = async (account, app) => {
    setKoyebDomainsTarget({ account, app });
    setKoyebNewDomain('');
    setKoyebDomainsError('');
    setKoyebDomainsLoading(true);
    try {
      const qs = app ? `?accountId=${account.id}&appId=${app._id}` : `?accountId=${account.id}`;
      const response = await fetch(`/api/koyeb/domains${qs}`, { headers: getAuthHeaders() });
      const result = await response.json();
      if (result.success) {
        setKoyebDomains(result.domains || []);
      } else {
        setKoyebDomainsError(result.error || '获取失败');
      }
    } catch (e) {
      setKoyebDomainsError('请求出错: ' + e.message);
    } finally {
      setKoyebDomainsLoading(false);
    }
  };

  const addKoyebDomain = async () => {
    const name = koyebNewDomain.trim();
    if (!name) return;
    const { account, app } = koyebDomainsTarget;
    try {
      const response = await fetch('/api/koyeb/domains', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id, name, appId: app?._id || '', type: 'CUSTOM' }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('域名已添加');
        setKoyebNewDomain('');
        openKoyebDomains(account, app);
      } else {
        toast.error('添加失败: ' + result.error);
      }
    } catch (e) {
      toast.error('请求出错: ' + e.message);
    }
  };

  const deleteKoyebDomain = async (domain) => {
    if (!confirmPress(`koyeb-domain:${domain.id}`, '删除域名')) return;
    const { account, app } = koyebDomainsTarget;
    try {
      const response = await fetch(`/api/koyeb/domains/${domain.id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('域名已删除');
        openKoyebDomains(account, app);
      } else {
        toast.error('删除失败: ' + result.error);
      }
    } catch (e) {
      toast.error('请求出错: ' + e.message);
    }
  };

  const refreshKoyebDomain = async (domain) => {
    const { account, app } = koyebDomainsTarget;
    try {
      const response = await fetch(`/api/koyeb/domains/${domain.id}/refresh`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('已请求刷新校验');
        openKoyebDomains(account, app);
      } else {
        toast.error('刷新失败: ' + result.error);
      }
    } catch (e) {
      toast.error('请求出错: ' + e.message);
    }
  };

  const openKoyebSecrets = async (account) => {
    setKoyebSecretsTarget(account);
    setKoyebSecrets([]);
    setKoyebSecretsError('');
    setKoyebNewSecretName('');
    setKoyebNewSecretValue('');
    setKoyebSecretsLoading(true);
    try {
      const response = await fetch(`/api/koyeb/secrets?accountId=${account.id}`, { headers: getAuthHeaders() });
      const result = await response.json();
      if (result.success) {
        setKoyebSecrets(result.secrets || []);
      } else {
        setKoyebSecretsError(result.error || '获取失败');
      }
    } catch (e) {
      setKoyebSecretsError('请求出错: ' + e.message);
    } finally {
      setKoyebSecretsLoading(false);
    }
  };

  const addKoyebSecret = async () => {
    const name = koyebNewSecretName.trim();
    if (!name || !koyebNewSecretValue) return;
    const account = koyebSecretsTarget;
    try {
      const response = await fetch('/api/koyeb/secrets', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id, name, value: koyebNewSecretValue }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('密钥已创建');
        setKoyebNewSecretName('');
        setKoyebNewSecretValue('');
        openKoyebSecrets(account);
      } else {
        toast.error('创建失败: ' + result.error);
      }
    } catch (e) {
      toast.error('请求出错: ' + e.message);
    }
  };

  const updateKoyebSecretValue = async (secret) => {
    const newValue = await dialog.prompt({ message: `为密钥 ${secret.name} 输入新值：`, defaultValue: '', placeholder: '新密钥值' });
    if (newValue === null || newValue === '') return;
    const account = koyebSecretsTarget;
    try {
      const response = await fetch(`/api/koyeb/secrets/${secret.id}/update`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id, value: newValue }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('密钥已更新');
        openKoyebSecrets(account);
      } else {
        toast.error('更新失败: ' + result.error);
      }
    } catch (e) {
      toast.error('请求出错: ' + e.message);
    }
  };

  const deleteKoyebSecret = async (secret) => {
    if (!confirmPress(`koyeb-secret:${secret.id}`, `删除密钥 ${secret.name}`)) return;
    const account = koyebSecretsTarget;
    try {
      const response = await fetch(`/api/koyeb/secrets/${secret.id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('密钥已删除');
        openKoyebSecrets(account);
      } else {
        toast.error('删除失败: ' + result.error);
      }
    } catch (e) {
      toast.error('请求出错: ' + e.message);
    }
  };

  const openKoyebCreate = async (account, app) => {
    setKoyebCreateTarget({ account, app });
    setKoyebCreateForm({ name: '', type: 'web', image: '', command: '', env: '', ports: '', regions: '', instanceType: '' });
    setKoyebCreateError('');
    try {
      const [inst, reg] = await Promise.all([
        fetch(`/api/koyeb/catalog/instances?accountId=${account.id}`, { headers: getAuthHeaders() }).then((r) => r.json()),
        fetch(`/api/koyeb/catalog/regions?accountId=${account.id}`, { headers: getAuthHeaders() }).then((r) => r.json()),
      ]);
      if (inst.success) setKoyebCatalogInstances(inst.items || []);
      if (reg.success) setKoyebCatalogRegions(reg.items || []);
    } catch (e) {
      // 目录加载失败不阻塞创建
    }
  };

  const createKoyebService = async () => {
    const { account, app } = koyebCreateTarget;
    const name = koyebCreateForm.name.trim();
    if (!name) {
      setKoyebCreateError('请填写服务名称');
      return;
    }
    const env = koyebCreateForm.env.split('\n').map((l) => l.trim()).filter(Boolean);
    const ports = koyebCreateForm.ports.split(',').map((s) => s.trim()).filter(Boolean).map((s) => {
      const [port, protocol] = s.split(':');
      const p = Number(port);
      return p ? { port: p, protocol: protocol || 'http' } : null;
    }).filter(Boolean);
    const regions = koyebCreateForm.regions.split(',').map((s) => s.trim()).filter(Boolean);
    setKoyebCreateSaving(true);
    setKoyebCreateError('');
    try {
      const response = await fetch('/api/koyeb/services', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          accountId: account.id,
          appId: app._id,
          name,
          type: koyebCreateForm.type,
          image: koyebCreateForm.image.trim(),
          command: koyebCreateForm.command,
          env,
          ports,
          regions,
          instanceType: koyebCreateForm.instanceType,
        }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success(`服务 ${name} 创建成功，正在部署...`);
        setKoyebCreateTarget(null);
        setTimeout(() => loadKoyebData(false), 2500);
      } else {
        setKoyebCreateError(result.error || '创建失败');
      }
    } catch (e) {
      setKoyebCreateError('请求出错: ' + e.message);
    } finally {
      setKoyebCreateSaving(false);
    }
  };

  const openKoyebUsage = async (account) => {
    setKoyebUsageTarget(account);
    setKoyebUsageData(null);
    setKoyebUsageError('');
    setKoyebUsageLoading(true);
    try {
      const response = await fetch(`/api/koyeb/usage/details?accountId=${account.id}`, { headers: getAuthHeaders() });
      const result = await response.json();
      if (result.success) {
        setKoyebUsageData(result.usage || {});
      } else {
        setKoyebUsageError(result.error || '获取失败');
      }
    } catch (e) {
      setKoyebUsageError('请求出错: ' + e.message);
    } finally {
      setKoyebUsageLoading(false);
    }
  };

  const pauseKoyebApp = async (account, app) => {
    if (!(await dialog.confirm(`暂停应用 "${app.name}"？其下所有服务将停止，暂停期间不产生计费。`))) return;
    try {
      const response = await fetch(`/api/koyeb/apps/${app._id}/pause`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success(`应用 ${app.name} 暂停中...`);
        setTimeout(() => loadKoyebData(false), 3000);
      } else {
        toast.error('暂停失败: ' + result.error);
      }
    } catch (e) {
      toast.error('请求出错: ' + e.message);
    }
  };

  const resumeKoyebApp = async (account, app) => {
    try {
      const response = await fetch(`/api/koyeb/apps/${app._id}/resume`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success(`应用 ${app.name} 恢复中...`);
        setTimeout(() => loadKoyebData(false), 3000);
      } else {
        toast.error('恢复失败: ' + result.error);
      }
    } catch (e) {
      toast.error('请求出错: ' + e.message);
    }
  };

  // ==================== 4. Fly.io State & Logic ====================
  const [flyAccounts, setFlyAccounts] = useState([]);
  const [flyLoading, setFlyLoading] = useState(false);
  const [flyRefreshing, setFlyRefreshing] = useState(false);
  const [flyExpandedAccounts, setFlyExpandedAccounts] = useState({});

  useEffect(() => {
    koyebAccountsRef.current = koyebAccounts;
  }, [koyebAccounts]);

  useEffect(() => {
    flyAccountsRef.current = flyAccounts;
  }, [flyAccounts]);

  const saveToFlyCache = useCallback((data) => {
    try {
      localStorage.setItem('fly_data_snapshots', JSON.stringify([{
        timestamp: Date.now(),
        accounts: data,
      }]));
    } catch (e) {}
  }, []);

  const loadFromFlyCache = useCallback(() => {
    try {
      const saved = localStorage.getItem('fly_data_snapshots');
      if (saved) {
        const history = JSON.parse(saved);
        if (history && history.length > 0) {
          setFlyAccounts(history[0].accounts || []);
          return true;
        }
      }
    } catch (e) {}
    return false;
  }, []);

  const loadFlyData = useCallback(async (isManual = false) => {
    if (flyRefreshingRef.current) return;
    flyRefreshingRef.current = true;
    setFlyRefreshing(true);
    if (isManual || flyAccountsRef.current.length === 0) {
      setFlyLoading(true);
    }

    try {
      const response = await fetch('/api/flyio/proxy/apps', {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (result.success) {
        const formatted = (result.data || []).map(acc => ({
          name: acc.accountName,
          id: acc.accountId,
          projects: (acc.apps || []).map(app => ({
            id: app.id,
            name: app.name,
            status: app.status,
            region: app.machines?.nodes?.length > 0 ? app.machines.nodes[0].region : '',
            appUrl: app.appUrl,
            deployed: app.deployed,
            hostname: app.hostname,
            machines: app.machines?.nodes || [],
            ips: (app.ipAddresses?.nodes || []).map(ip => ({
              address: ip.address,
              type: ip.type,
            })),
            domains: (app.certificates?.nodes || []).map(cert => ({
              domain: cert.hostname,
              status: cert.clientStatus,
              isVerified: cert.clientStatus === 'Ready',
            })),
            isEditing: false,
            editingName: app.name,
            showMachines: false,
            loadingMachines: false,
          })),
          error: acc.error,
        }));

        setFlyAccounts(formatted);
        saveToFlyCache(formatted);
        if (isManual) {
          toast.success('Fly.io 监控数据已刷新');
        }
      }
    } catch (error) {
      console.error('加载 Fly.io 数据失败:', error);
      if (isManual) {
        toast.error('刷新 Fly.io 失败: ' + error.message);
      }
    } finally {
      setFlyLoading(false);
      flyRefreshingRef.current = false;
      setFlyRefreshing(false);
    }
  }, [getAuthHeaders, saveToFlyCache]);

  const toggleFlyAccount = (accountName) => {
    setFlyExpandedAccounts(prev => ({
      ...prev,
      [accountName]: !prev[accountName]
    }));
  };

  const isFlyAccountExpanded = (accountName) => {
    if (flyExpandedAccounts[accountName] === undefined) {
      return true;
    }
    return flyExpandedAccounts[accountName];
  };

  // Fly.io Service Operations
  const redeployFlyApp = async (account, app) => {
    if (!(await dialog.confirm(`重启 Fly.io 应用 "${app.name}"？（触发一次重新部署）`))) return;
    try {
      const response = await fetch(`/api/flyio/apps/${app.name}/redeploy`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('应用重启命令已提交');
        loadFlyData(false);
      } else {
        toast.error('重启失败: ' + result.error);
      }
    } catch (e) {
      toast.error('操作错误: ' + e.message);
    }
  };

  const updateFlyAppImage = async (account, app) => {
    let currentImage = '';
    // Fetch machines to find current image
    try {
      const response = await fetch(`/api/flyio/apps/${app.name}/machines?accountId=${account.id}`, {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (result.success && result.data.length > 0) {
        currentImage = result.data[0].config?.image || '';
      }
    } catch (e) {
      console.warn('获取当前镜像失败:', e);
    }

    const defaultImage = normalizeFlyImageForInput(currentImage);
    const newImage = await dialog.prompt({
      message: currentImage
        ? `当前正在运行：\n${currentImage}\n\n默认使用 Docker Hub 标准镜像引用；如需切换镜像，请输入完整镜像地址：`
        : '未能检测到当前运行的镜像。请输入完整的容器镜像地址：',
      defaultValue: defaultImage,
    });

    if (newImage === null || newImage.trim() === '') return;

    toast.info('正在更新容器镜像，请稍候...', { isManual: true });
    try {
      const response = await fetch(`/api/flyio/apps/${app.name}/update-image`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          accountId: account.id,
          image: newImage.trim(),
        }),
      });
      const result = await response.json();
      if (result.success) {
        const details = Array.isArray(result.details) ? result.details : [];
        const changedCount = details.filter((item) => item?.changed).length;
        const digestChangedCount = details.filter((item) => item?.digestChanged).length;
        const digest = details.find((item) => item?.currentDigest)?.currentDigest;
        const digestHint = digest ? `，digest ${String(digest).slice(0, 19)}...` : '';
        if (changedCount > 0) {
          toast.success(`容器镜像已更新：${result.updated} 台已提交，${changedCount} 台有变化${digestHint}`);
        } else if (result.updated > 0) {
          toast.warning(`已提交 ${result.updated} 台，但镜像 digest 未变化；Fly 官网 Release Activity 可能不会新增记录`);
        } else {
          toast.warning('没有 Machine 被更新');
        }
        if (digestChangedCount === 0 && changedCount > 0) {
          toast.info('Machine 配置/版本有变化，但镜像 digest 未变化', { isManual: true });
        }
        loadFlyData(false);
      } else {
        const detail = result.error || result.errors?.[0]?.message || '请查看后端日志';
        toast.error('更新失败: ' + detail);
      }
    } catch (error) {
      toast.error('更新异常: ' + error.message);
    }
  };

  const updateAllFlyAppsImage = async (account) => {
    if (!(await dialog.confirm(`为 Fly.io 账号 "${account.name}" 下的所有应用批量更新最新镜像？`))) return;
    toast.info('正在提交批量更新，请稍候...', { isManual: true });
    try {
      const response = await fetch(`/api/flyio/accounts/${account.id}/update-all-images`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ image: '' }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success(`批量更新完成！成功: ${result.updated}, 失败: ${result.failed}`);
        loadFlyData(false);
      } else {
        toast.error('批量更新失败: ' + result.error);
      }
    } catch (e) {
      toast.error('请求出错: ' + e.message);
    }
  };

  const fetchFlyMachines = async (account, app) => {
    if (app.showMachines) {
      setFlyAccounts(prev => prev.map(acc => ({
        ...acc,
        projects: acc.projects.map(p => {
          if (p.id !== app.id) return p;
          return { ...p, showMachines: false };
        })
      })));
      return;
    }

    setFlyAccounts(prev => prev.map(acc => ({
      ...acc,
      projects: acc.projects.map(p => {
        if (p.id !== app.id) return p;
        return { ...p, loadingMachines: true };
      })
    })));

    try {
      const response = await fetch(`/api/flyio/apps/${app.name}/machines?accountId=${account.id}`, {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (result.success) {
        setFlyAccounts(prev => prev.map(acc => ({
          ...acc,
          projects: acc.projects.map(p => {
            if (p.id !== app.id) return p;
            return {
              ...p,
              machines: result.data || [],
              showMachines: true,
              loadingMachines: false
            };
          })
        })));
      } else {
        toast.error('获取机器列表失败: ' + result.error);
        setFlyAccounts(prev => prev.map(acc => ({
          ...acc,
          projects: acc.projects.map(p => {
            if (p.id !== app.id) return p;
            return { ...p, loadingMachines: false };
          })
        })));
      }
    } catch (e) {
      toast.error('获取机器列表异常: ' + e.message);
      setFlyAccounts(prev => prev.map(acc => ({
        ...acc,
        projects: acc.projects.map(p => {
          if (p.id !== app.id) return p;
          return { ...p, loadingMachines: false };
        })
      })));
    }
  };

  const refreshFlyMachines = async (account, app) => {
    try {
      const response = await fetch(`/api/flyio/apps/${app.name}/machines?accountId=${account.id}`, {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (!result.success) throw new Error(result.error || '获取机器列表失败');
      setFlyAccounts(prev => prev.map(acc => ({
        ...acc,
        projects: acc.projects.map(p => {
          if (p.id !== app.id) return p;
          return { ...p, machines: result.data || [], showMachines: true, loadingMachines: false };
        })
      })));
      return result.data || [];
    } catch (e) {
      toast.error('刷新机器列表失败: ' + e.message);
      return null;
    }
  };

  const callFlyMachineEndpoint = async (account, app, machine, action, options = {}) => {
    const machineID = machine?.id;
    if (!machineID) return null;
    const method = options.method || 'POST';
    const path = action
      ? `/api/flyio/apps/${app.name}/machines/${machineID}/${action}`
      : `/api/flyio/apps/${app.name}/machines/${machineID}`;
    const response = await fetch(path, {
      method,
      headers: getAuthHeaders(),
      body: method === 'GET' ? undefined : JSON.stringify({ accountId: account.id, ...(options.body || {}) }),
    });
    const result = await response.json();
    if (!result.success) throw new Error(result.error || 'Fly.io 请求失败');
    return result.data;
  };

  const runFlyMachineAction = async (account, app, machine, action, label, options = {}) => {
    if (options.confirm && !(await dialog.confirm(options.confirm))) return;
    try {
      await callFlyMachineEndpoint(account, app, machine, action, options);
      toast.success(label);
      await refreshFlyMachines(account, app);
    } catch (e) {
      toast.error(`${label}失败: ${e.message}`);
    }
  };

  const createFlyMachine = async (account, app) => {
    const currentImage = app.machines?.[0]?.config?.image || '';
    const image = await dialog.prompt({
      message: '请输入新机器使用的完整镜像地址：',
      defaultValue: normalizeFlyImageForInput(currentImage),
    });
    if (image === null || image.trim() === '') return;
    const region = await dialog.prompt({
      message: '请输入部署区域代码，例如 hkg、nrt、sin。留空则由 Fly.io 决定：',
      defaultValue: app.machines?.[0]?.region || '',
    });
    if (region === null) return;
    try {
      const response = await fetch(`/api/flyio/apps/${app.name}/machines`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          accountId: account.id,
          image: image.trim(),
          region: region.trim() || undefined,
        }),
      });
      const result = await response.json();
      if (!result.success) throw new Error(result.error || '创建机器失败');
      toast.success('Fly.io 机器已创建');
      await refreshFlyMachines(account, app);
    } catch (e) {
      toast.error('创建机器失败: ' + e.message);
    }
  };

  const deleteFlyMachine = async (account, app, machine) => {
    if (!confirmPress(`fly-machine:${machine.id}`, `删除 Fly.io 机器「${machine.id}」`)) return;
    try {
      await callFlyMachineEndpoint(account, app, machine, '', { method: 'DELETE', body: { force: true } });
      toast.success('机器已删除');
      await refreshFlyMachines(account, app);
    } catch (e) {
      toast.error('删除机器失败: ' + e.message);
    }
  };

  const showFlyMachineDetails = (account, app, machine) => {
    openLogViewer({
      title: `机器详情: ${machine.id}`,
      subtitle: `Fly.io / ${account.name} / ${app.name}`,
      fetcher: async () => {
        const [detailResponse, metadataResponse, leaseResponse, eventsResponse, memoryResponse, psResponse, versionsResponse] = await Promise.all([
          fetch(`/api/flyio/apps/${app.name}/machines/${machine.id}?accountId=${account.id}`, { headers: getAuthHeaders() }),
          fetch(`/api/flyio/apps/${app.name}/machines/${machine.id}/metadata?accountId=${account.id}`, { headers: getAuthHeaders() }),
          fetch(`/api/flyio/apps/${app.name}/machines/${machine.id}/lease?accountId=${account.id}`, { headers: getAuthHeaders() }),
          fetch(`/api/flyio/apps/${app.name}/machines/${machine.id}/events?accountId=${account.id}`, { headers: getAuthHeaders() }),
          fetch(`/api/flyio/apps/${app.name}/machines/${machine.id}/memory?accountId=${account.id}`, { headers: getAuthHeaders() }),
          fetch(`/api/flyio/apps/${app.name}/machines/${machine.id}/ps?accountId=${account.id}`, { headers: getAuthHeaders() }),
          fetch(`/api/flyio/apps/${app.name}/machines/${machine.id}/versions?accountId=${account.id}`, { headers: getAuthHeaders() }),
        ]);
        const detail = await detailResponse.json();
        const metadata = await metadataResponse.json();
        const lease = await leaseResponse.json();
        const events = await eventsResponse.json();
        const memory = await memoryResponse.json();
        const ps = await psResponse.json();
        const versions = await versionsResponse.json();
        const payload = {
          machine: detail.success ? detail.data : { error: detail.error },
          metadata: metadata.success ? metadata.data : { error: metadata.error },
          lease: lease.success ? lease.data : { error: lease.error },
          events: events.success ? events.data : { error: events.error },
          memory: memory.success ? memory.data : { error: memory.error },
          processes: ps.success ? ps.data : { error: ps.error },
          versions: versions.success ? versions.data : { error: versions.error },
        };
        return JSON.stringify(payload, null, 2).split('\n').map(line => ({
          timestamp: Date.now(),
          level: 'INFO',
          message: line,
        }));
      }
    });
  };

  const setFlyMachineMetadata = async (account, app, machine) => {
    const key = await dialog.prompt({ message: '请输入 metadata key：', defaultValue: 'api-monitor-note' });
    if (key === null || key.trim() === '') return;
    const value = await dialog.prompt({ message: `请输入 ${key.trim()} 的值：`, defaultValue: '' });
    if (value === null) return;
    try {
      const response = await fetch(`/api/flyio/apps/${app.name}/machines/${machine.id}/metadata/${encodeURIComponent(key.trim())}`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id, value }),
      });
      const result = await response.json();
      if (!result.success) throw new Error(result.error || '设置 metadata 失败');
      toast.success('机器 metadata 已更新');
    } catch (e) {
      toast.error('设置 metadata 失败: ' + e.message);
    }
  };

  const acquireFlyMachineLease = async (account, app, machine) => {
    try {
      const data = await callFlyMachineEndpoint(account, app, machine, 'lease', { body: { ttl: 60 } });
      const nonce = data?.nonce || data?.Nonce || '';
      toast.success(nonce ? `Lease 已获取: ${nonce}` : 'Lease 已获取');
    } catch (e) {
      toast.error('获取 lease 失败: ' + e.message);
    }
  };

  const showFlyAppLogs = (account, app) => {
    openLogViewer({
      title: `容器日志: ${app.name}`,
      subtitle: `Fly.io / ${account.name}`,
      fetcher: async () => {
        try {
          const response = await fetch(`/api/flyio/apps/${app.name}/logs?accountId=${account.id}`, {
            headers: getAuthHeaders(),
          });
          const result = await response.json();
          if (result.success && result.data && result.data.length > 0) {
            return result.data.map(l => ({
              timestamp: l.timestamp ? new Date(l.timestamp).getTime() : Date.now(),
              message: l.message,
              level: l.level || 'INFO',
            }));
          }

          // Fallback to events
          const eventResponse = await fetch(`/api/flyio/apps/${app.name}/events?accountId=${account.id}`, {
            headers: getAuthHeaders(),
          });
          const eventResult = await eventResponse.json();
          if (eventResult.success && eventResult.data.length > 0) {
            return eventResult.data;
          }
          return [{ timestamp: Date.now(), level: 'INFO', message: '暂无容器日志或系统事件。' }];
        } catch (e) {
          throw new Error('加载日志失败: ' + e.message, { cause: e });
        }
      }
    });
  };

  const viewFlyConfig = (account, app) => {
    openLogViewer({
      title: `应用配置: ${app.name}`,
      subtitle: `Fly.io / ${account.name}`,
      fetcher: async () => {
        const response = await fetch(`/api/flyio/apps/${app.name}/config?accountId=${account.id}`, {
          headers: getAuthHeaders(),
        });
        const result = await response.json();
        if (result.success) {
          const configStr = JSON.stringify(result.data, null, 2);
          return [
            { timestamp: Date.now(), level: 'INFO', message: '--- 当前激活配置 (JSON) ---' },
            ...configStr.split('\n').map(line => ({
              timestamp: Date.now(),
              level: 'INFO',
              message: line
            }))
          ];
        }
        throw new Error(result.error || '获取配置失败');
      }
    });
  };

  const startEditFlyAppName = (app) => {
    setFlyAccounts(prev => prev.map(acc => ({
      ...acc,
      projects: acc.projects.map(p => {
        if (p.id !== app.id) return p;
        return { ...p, isEditing: true, editingName: p.name };
      })
    })));
  };

  const cancelEditFlyAppName = (app) => {
    setFlyAccounts(prev => prev.map(acc => ({
      ...acc,
      projects: acc.projects.map(p => {
        if (p.id !== app.id) return p;
        return { ...p, isEditing: false };
      })
    })));
  };

  const saveFlyAppName = async (account, app) => {
    const newName = app.editingName?.trim();
    if (!newName || newName === app.name) {
      cancelEditFlyAppName(app);
      return;
    }
    try {
      const response = await fetch(`/api/flyio/apps/${app.name}/rename`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          accountId: account.id,
          newName: newName,
        }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('应用已成功重命名');
        setFlyAccounts(prev => prev.map(acc => ({
          ...acc,
          projects: acc.projects.map(p => {
            if (p.id !== app.id) return p;
            return { ...p, name: newName, isEditing: false };
          })
        })));
        loadFlyData(false);
      } else {
        toast.error('重命名失败: ' + result.error);
        cancelEditFlyAppName(app);
      }
    } catch (e) {
      toast.error('重命名错误: ' + e.message);
      cancelEditFlyAppName(app);
    }
  };

  const createFlyApp = async (account) => {
    const name = await dialog.prompt({
      message: '请输入新应用名称 (留空将自动生成)：',
    });
    if (name === null) return;
    try {
      const response = await fetch('/api/flyio/apps', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          accountId: account.id,
          name: name.trim() || undefined,
        }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success(`Fly.io 应用 ${result.data?.name} 已创建成功`);
        loadFlyData(false);
      } else {
        toast.error('创建失败: ' + result.error);
      }
    } catch (e) {
      toast.error('创建出错: ' + e.message);
    }
  };

  const deleteFlyApp = async (account, app) => {
    if (!confirmPress(`fly-app:${app.id}`, `永久删除应用「${app.name}」`)) return;
    try {
      const response = await fetch(`/api/flyio/apps/${app.name}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
        body: JSON.stringify({ accountId: account.id }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('应用删除成功');
        loadFlyData(false);
      } else {
        toast.error('删除失败: ' + result.error);
      }
    } catch (e) {
      toast.error('删除错误: ' + e.message);
    }
  };

  const getFlyStatusBadge = (status) => {
    const up = String(status || '').toLowerCase();
    if (['deployed', 'running', 'started'].includes(up)) {
      return getStatusPillClass('success');
    }
    if (['suspended', 'dead', 'stopped', 'destroyed'].includes(up)) {
      return getStatusPillClass('warning');
    }
    if (['pending', 'created'].includes(up)) {
      return getStatusPillClass('info');
    }
    return getStatusPillClass('neutral');
  };

  const getFlyStatusTone = (status) => {
    const up = String(status || '').toLowerCase();
    if (['deployed', 'running', 'started'].includes(up)) return 'success';
    if (['pending', 'created', 'launching'].includes(up)) return 'info';
    if (['suspended', 'stopped', 'stopping'].includes(up)) return 'warning';
    if (['dead', 'destroyed', 'failed'].includes(up)) return 'error';
    return 'neutral';
  };

  const getFlyStatusText = (status) => {
    const map = {
      deployed: '已部署',
      running: '运行中',
      suspended: '已暂停',
      dead: '已停止',
      pending: '部署中',
    };
    return map[String(status).toLowerCase()] || status || '未知';
  };

  // ==================== 5. Account Management Tab State ====================
  const [koyebManagedAccounts, setKoyebManagedAccounts] = useState([]);
  const [flyManagedAccounts, setFlyManagedAccounts] = useState([]);

  // Koyeb Add Modal
  const [showAddKoyebModal, setShowAddKoyebModal] = useState(false);
  const [newKoyebName, setNewKoyebName] = useState('');
  const [newKoyebToken, setNewKoyebToken] = useState('');
  const [koyebAddingAccount, setKoyebAddingAccount] = useState(false);
  const [koyebAddAccountError, setKoyebAddAccountError] = useState('');

  // Fly Add Modal
  const [showAddFlyModal, setShowAddFlyModal] = useState(false);
  const [newFlyName, setNewFlyName] = useState('');
  const [newFlyToken, setNewFlyToken] = useState('');
  const [flyAddingAccount, setFlyAddingAccount] = useState(false);
  const [flyAddAccountError, setFlyAddAccountError] = useState('');

  // Loads Koyeb managed
  const loadKoyebManagedAccounts = useCallback(async () => {
    try {
      const response = await fetch('/api/koyeb/accounts', {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (result.success) {
        setKoyebManagedAccounts(result.accounts || []);
      }
    } catch (e) {
      console.error('加载 Koyeb 托管账号失败:', e);
    }
  }, [getAuthHeaders]);

  // Loads Fly managed
  const loadFlyManagedAccounts = useCallback(async () => {
    try {
      const response = await fetch('/api/flyio/accounts', {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (result.success) {
        setFlyManagedAccounts(result.data || []);
      }
    } catch (e) {
      console.error('加载 Fly.io 托管账号失败:', e);
    }
  }, [getAuthHeaders]);

  const addKoyebAccount = async () => {
    if (!newKoyebName.trim() || !newKoyebToken.trim()) {
      setKoyebAddAccountError('请填写备注名称和 API 令牌');
      return;
    }
    setKoyebAddingAccount(true);
    setKoyebAddAccountError('');
    try {
      const response = await fetch('/api/koyeb/accounts', {
        method: 'POST',
        headers: {
          ...getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ name: newKoyebName.trim(), token: newKoyebToken.trim() }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('Koyeb 账号添加成功');
        setNewKoyebName('');
        setNewKoyebToken('');
        setShowAddKoyebModal(false);
        loadKoyebManagedAccounts();
        loadKoyebData(false);
      } else {
        setKoyebAddAccountError(result.error || '添加失败');
      }
    } catch (e) {
      setKoyebAddAccountError('添加请求出错: ' + e.message);
    } finally {
      setKoyebAddingAccount(false);
    }
  };

  const removeKoyebAccount = async (id) => {
    if (!confirmPress(`koyeb-account:${id}`, '删除 Koyeb 账号')) return;
    try {
      const response = await fetch(`/api/koyeb/accounts/${id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('账号已删除');
        loadKoyebManagedAccounts();
        loadKoyebData(false);
      } else {
        toast.error('删除失败: ' + result.error);
      }
    } catch (e) {
      toast.error('删除请求出错: ' + e.message);
    }
  };

  // Fly.io Accounts Operations
  const addFlyAccount = async () => {
    if (!newFlyName.trim() || !newFlyToken.trim()) {
      setFlyAddAccountError('请填写备注名称和 API 令牌');
      return;
    }
    setFlyAddingAccount(true);
    setFlyAddAccountError('');
    try {
      const response = await fetch('/api/flyio/accounts', {
        method: 'POST',
        headers: {
          ...getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ name: newFlyName.trim(), api_token: newFlyToken.trim() }),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('Fly.io 账号添加成功');
        setNewFlyName('');
        setNewFlyToken('');
        setShowAddFlyModal(false);
        loadFlyManagedAccounts();
        loadFlyData(false);
      } else {
        setFlyAddAccountError(result.error || '添加失败');
      }
    } catch (e) {
      setFlyAddAccountError('添加请求出错: ' + e.message);
    } finally {
      setFlyAddingAccount(false);
    }
  };

  const removeFlyAccount = async (id, name) => {
    if (!confirmPress(`fly-account:${id}`, `删除 Fly.io 账号「${name}」`)) return;
    try {
      const response = await fetch(`/api/flyio/accounts/${id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (result.success) {
        toast.success('账号已成功删除');
        loadFlyManagedAccounts();
        loadFlyData(false);
      } else {
        toast.error('删除失败: ' + result.error);
      }
    } catch (e) {
      toast.error('删除请求出错: ' + e.message);
    }
  };

  const exportPaasAccounts = async () => {
    try {
      const [koyebResponse, flyResponse] = await Promise.all([
        fetch('/api/koyeb/accounts/export', { headers: getAuthHeaders() }),
        fetch('/api/flyio/accounts/export', { headers: getAuthHeaders() }),
      ]);
      const [koyebResult, flyResult] = await Promise.all([
        koyebResponse.json(),
        flyResponse.json(),
      ]);
      const koyebAccountsForExport = koyebResult.success ? (koyebResult.accounts || []) : [];
      const flyAccountsForExport = flyResult.success ? (flyResult.accounts || []) : [];
      if (koyebAccountsForExport.length === 0 && flyAccountsForExport.length === 0) {
        toast.warning('没有可导出的账号');
        return;
      }

      const exportData = {
        version: '2.0',
        type: 'paas-accounts',
        exportTime: new Date().toISOString(),
        koyeb: koyebAccountsForExport,
        fly: flyAccountsForExport,
      };
      const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `paas-accounts-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);
      toast.success(`已导出 ${koyebAccountsForExport.length + flyAccountsForExport.length} 个账号`);
    } catch (error) {
      toast.error(`导出失败：${error.message}`);
    }
  };

  const parseAccountLines = (text) => text
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const separator = line.includes(':') || line.includes('：') ? /[:：]/ : /[,，]/;
      const parts = line.split(separator);
      return {
        name: (parts[0] || '').trim(),
        token: parts.slice(1).join(line.includes(',') || line.includes('，') ? ',' : ':').trim(),
      };
    })
    .filter((account) => account.name && account.token);

  const importPaasAccounts = async () => {
    if (!(await dialog.confirm('导入会逐条写入 PaaS 账号配置，继续吗？'))) return;
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json,.txt';
    input.onchange = async (event) => {
      const file = event.target.files?.[0];
      if (!file) return;
      try {
        const text = await file.text();
        let koyebAccountsToImport = [];
        let flyAccountsToImport = [];

        try {
          const data = JSON.parse(text);
          if (data.type === 'paas-accounts' || data.koyeb || data.fly) {
            koyebAccountsToImport = Array.isArray(data.koyeb) ? data.koyeb : [];
            flyAccountsToImport = Array.isArray(data.fly) ? data.fly : [];
          } else if (data.platform === 'fly') {
            flyAccountsToImport = Array.isArray(data.accounts) ? data.accounts : [];
          } else if (data.platform === 'koyeb') {
            koyebAccountsToImport = Array.isArray(data.accounts) ? data.accounts : [];
          } else if (Array.isArray(data.accounts)) {
            koyebAccountsToImport = data.accounts;
          }
        } catch {
          koyebAccountsToImport = parseAccountLines(text);
        }

        if (koyebAccountsToImport.length === 0 && flyAccountsToImport.length === 0) {
          toast.error('没有识别到可导入的账号');
          return;
        }

        setKoyebAddingAccount(true);
        setFlyAddingAccount(true);
        let successCount = 0;
        const errors = [];

        for (const account of koyebAccountsToImport) {
          const name = String(account.name || '').trim();
          const token = String(account.token || account.api_token || '').replace(/[^\x21-\x7E]/g, '');
          if (!name || !token) continue;
          try {
            const response = await fetch('/api/koyeb/accounts', {
              method: 'POST',
              headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
              body: JSON.stringify({ name, token }),
            });
            const result = await response.json();
            if (result.success) successCount++;
            else errors.push(`Koyeb/${name}: ${result.error || '导入失败'}`);
          } catch (error) {
            errors.push(`Koyeb/${name}: ${error.message}`);
          }
        }

        for (const account of flyAccountsToImport) {
          const name = String(account.name || '').trim();
          const apiToken = String(account.api_token || account.token || '').replace(/[^\x21-\x7E]/g, '');
          if (!name || !apiToken) continue;
          try {
            const response = await fetch('/api/flyio/accounts', {
              method: 'POST',
              headers: { ...getAuthHeaders(), 'Content-Type': 'application/json' },
              body: JSON.stringify({ name, api_token: apiToken }),
            });
            const result = await response.json();
            if (result.success) successCount++;
            else errors.push(`Fly.io/${name}: ${result.error || '导入失败'}`);
          } catch (error) {
            errors.push(`Fly.io/${name}: ${error.message}`);
          }
        }

        await Promise.all([
          loadKoyebManagedAccounts(),
          loadFlyManagedAccounts(),
          loadKoyebData(false),
          loadFlyData(false),
        ]);
        if (successCount > 0) toast.success(`已导入 ${successCount} 个账号`);
        if (errors.length > 0) toast.error(`部分账号导入失败：${errors.slice(0, 3).join('；')}`);
      } catch (error) {
        toast.error(`导入失败：${error.message}`);
      } finally {
        setKoyebAddingAccount(false);
        setFlyAddingAccount(false);
      }
    };
    input.click();
  };

  // ==================== 6. Automatic Refresher Effect ====================
  useEffect(() => {
    if (didInitialLoadRef.current) return;
    didInitialLoadRef.current = true;

    loadSettings();
    loadFromKoyebCache();
    loadFromFlyCache();

    loadKoyebData(false);
    loadFlyData(false);

    loadKoyebManagedAccounts();
    loadFlyManagedAccounts();
  }, [loadSettings, loadFromKoyebCache, loadFromFlyCache, loadKoyebData, loadFlyData, loadKoyebManagedAccounts, loadFlyManagedAccounts]);

  // Koyeb Timer
  useEffect(() => {
    if (activeTab !== 'koyeb') return;
    const timer = setInterval(() => loadKoyebData(false), normalizeRefreshIntervalInputSec(koyebIntervalSec) * 1000);
    return () => clearInterval(timer);
  }, [activeTab, koyebIntervalSec, loadKoyebData]);

  // Fly Timer
  useEffect(() => {
    if (activeTab !== 'fly') return;
    const timer = setInterval(() => loadFlyData(false), normalizeRefreshIntervalInputSec(flyIntervalSec) * 1000);
    return () => clearInterval(timer);
  }, [activeTab, flyIntervalSec, loadFlyData]);

  return (
    <div className="flex w-full min-w-0 flex-col gap-3 cq-sm:gap-4">
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <div className="min-w-0 w-full cq-md:w-auto">
          <Tabs
            {...MODULE_TABS_PROPS}
            value={activeTab}
            onValueChange={setActiveTab}
            tabs={[
              { value: 'fly', label: <span className="inline-flex items-center gap-1.5"><FlyIoBrand className="size-3.5 text-brand" />Fly.io</span> },
              { value: 'koyeb', label: <span className="inline-flex items-center gap-1.5"><KoyebBrand className="size-3.5 text-kumo-info" />Koyeb</span> },
              { value: 'config', label: <span className="inline-flex items-center gap-1.5"><Settings className="w-4 h-4 text-kumo-success" />配置</span> },
            ]}
          />
        </div>

        <TabBarOverflowActions
          items={
            activeTab === 'koyeb' || activeTab === 'fly'
              ? [
                  {
                    key: 'refresh',
                    label: '刷新',
                    icon: <RefreshCw className="w-3.5 h-3.5" />,
                    onClick: () => (activeTab === 'koyeb' ? loadKoyebData(true) : loadFlyData(true)),
                    disabled: activeTab === 'koyeb' ? koyebRefreshing : flyRefreshing,
                    loading: activeTab === 'koyeb' ? koyebRefreshing : flyRefreshing,
                  },
                ]
              : []
          }
        />
      </div>

      <div className="min-w-0">

      {/* ==================== Koyeb Tab Content ==================== */}
      {activeTab === 'koyeb' && (
        <div className="space-y-3">
          {koyebLoading && koyebAccounts.length === 0 ? (
            <Empty
              size="base"
              icon={<Loader size={32} className="text-kumo-info" />}
              title="正在加载 Koyeb"
              description="同步应用和域名状态"
            />
          ) : koyebAccounts.length === 0 ? (
            <Empty
              size="base"
              icon={<KoyebBrand className="h-8 w-8 text-kumo-info" />}
              title="暂无 Koyeb 账号"
              description="需先添加 Koyeb API Token"
            />
          ) : (
            <div className="space-y-5">
              {koyebAccounts.map((account) => {
                const expanded = isKoyebAccountExpanded(account.name);
                const balance = Number(account.data?.balance ?? account.balance ?? 0);
                return (
                  <section key={account.name} className="space-y-3">
                    <LayerCard className="self-start">
                      <LayerCard.Secondary
                        role="button"
                        tabIndex={0}
                        aria-expanded={expanded}
                        className="flex min-w-0 cursor-pointer flex-wrap items-center gap-3 cq-sm:justify-between"
                        onClick={() => toggleKoyebAccount(account.name)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            toggleKoyebAccount(account.name);
                          }
                        }}
                      >
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          tabIndex={-1}
                          className="min-w-0 flex-1 justify-start px-0 text-left"
                          icon={<ChevronDown className={`h-4 w-4 shrink-0 transition-transform duration-200 ${expanded ? 'rotate-180' : ''}`} />}
                        >
                          <span className="flex min-w-0 items-center gap-2 text-left">
                            <KoyebBrand className="h-4 w-4 shrink-0 text-kumo-info" />
                            <Text as="span" bold truncate>{account.name}</Text>
                          </span>
                        </Button>
                        <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
                          <Button size="sm" variant="secondary" onClick={(e) => { e.stopPropagation(); openKoyebSecrets(account); }} icon={<Lock className="h-3.5 w-3.5" />}>
                            Secrets
                          </Button>
                          <Button size="sm" variant="secondary" onClick={(e) => { e.stopPropagation(); openKoyebUsage(account); }} icon={<Activity className="h-3.5 w-3.5" />}>
                            用量
                          </Button>
                          {account.data?.email ? (
                            <Badge variant="outline" className="max-w-full">
                              <Mail className="h-3 w-3 shrink-0" />
                              <span className="truncate">{account.data.email}</span>
                            </Badge>
                          ) : null}
                          <Badge variant="info">${Number.isFinite(balance) ? balance.toFixed(2) : '0.00'}</Badge>
                        </div>
                      </LayerCard.Secondary>
                    </LayerCard>

                    <AnimatedCollapse open={expanded} panelClassName="overflow-visible">
                      {account.error ? (
                        <Empty
                          size="sm"
                          icon={<Info className="h-8 w-8 text-kumo-danger" />}
                          title="Koyeb 同步失败"
                          description={account.error}
                        />
                      ) : !account.projects || account.projects.length === 0 ? (
                        <Empty
                          size="sm"
                          icon={<KoyebBrand className="h-8 w-8 text-kumo-info" />}
                          title="暂无应用"
                        />
                      ) : (
                        <div className="grid min-w-0 grid-cols-1 items-start gap-3 cq-sm:grid-cols-2 cq-md:grid-cols-3 cq-lg:grid-cols-4">
                          {account.projects.map((app) => {
                            const services = app.services || [];
                            return (
                              <LayerCard key={app._id} className="self-start">
                                <LayerCard.Secondary className="flex min-w-0 items-start justify-between gap-3">
                                  <div className="min-w-0 flex-1">
                                    {app.isEditing ? (
                                      <Input
                                        size="sm"
                                        aria-label="Koyeb 应用名称"
                                        type="text"
                                        value={app.editingName}
                                        onChange={(e) => {
                                          const val = e.target.value;
                                          setKoyebAccounts(prev => prev.map(acc => ({
                                            ...acc,
                                            projects: acc.projects.map(p => {
                                              if (p._id !== app._id) return p;
                                              return { ...p, editingName: val };
                                            })
                                          })));
                                        }}
                                        onKeyDown={(e) => {
                                          if (e.key === 'Enter') renameKoyebApp(account, app);
                                          if (e.key === 'Escape') cancelEditKoyebAppName(app);
                                        }}
                                        onBlur={() => renameKoyebApp(account, app)}
                                        autoFocus
                                      />
                                    ) : (
                                      <Button
                                        type="button"
                                        variant="ghost"
                                        size="sm"
                                        onDoubleClick={() => startEditKoyebAppName(app)}
                                        className="min-w-0 justify-start px-0"
                                        title="双击重命名"
                                        icon={<KoyebBrand className="h-4 w-4 text-kumo-info" />}
                                      >
                                        <Text as="span" bold truncate>{app.name}</Text>
                                      </Button>
                                    )}
                                  </div>
                                  <div className="flex shrink-0 flex-wrap justify-end gap-1.5">
                                    <Badge variant="neutral">{services.length} 服务</Badge>
                                    {app.region && app.region !== 'unknown' ? (
                                      <Badge variant="outline">{app.region}</Badge>
                                    ) : null}
                                  </div>
                                </LayerCard.Secondary>

                                <LayerCard.Primary className="space-y-3">
                                  <div className="flex flex-wrap justify-start gap-1">
                                    <Button shape="square" size="sm" variant="secondary" aria-label="新建服务" title="新建服务" onClick={() => openKoyebCreate(account, app)} icon={<Plus className="h-3.5 w-3.5" />} />
                                    <Button shape="square" size="sm" variant="secondary" aria-label="暂停应用" title="暂停应用" onClick={() => pauseKoyebApp(account, app)} icon={<Pause className="h-3.5 w-3.5" />} />
                                    <Button shape="square" size="sm" variant="secondary" aria-label="恢复应用" title="恢复应用" onClick={() => resumeKoyebApp(account, app)} icon={<Play className="h-3.5 w-3.5" />} />
                                    <Button shape="square" size="sm" variant="secondary" aria-label="管理域名" title="管理域名" onClick={() => openKoyebDomains(account, app)} icon={<Globe className="h-3.5 w-3.5" />} />
                                  </div>
                                  {services.length === 0 ? (
                                    <Empty
                                      size="sm"
                                      icon={<Server className="h-7 w-7 text-kumo-subtle" />}
                                      title="暂无服务"
                                    />
                                  ) : (
                                    <div className="space-y-3">
                                      {services.map((service) => (
                                        <div key={service._id} className="space-y-2 border-b border-kumo-line/70 pb-3 last:border-b-0 last:pb-0">
                                          <div className="flex min-w-0 items-start justify-between gap-2">
                                            <div className="min-w-0 space-y-1">
                                              {service.isEditing ? (
                                                <Input
                                                  size="sm"
                                                  aria-label="Koyeb 服务名称"
                                                  type="text"
                                                  value={service.editingName}
                                                  onChange={(e) => {
                                                    const val = e.target.value;
                                                    setKoyebAccounts(prev => prev.map(acc => ({
                                                      ...acc,
                                                      projects: acc.projects.map(p => ({
                                                        ...p,
                                                        services: p.services.map(s => {
                                                          if (s._id !== service._id) return s;
                                                          return { ...s, editingName: val };
                                                        })
                                                      }))
                                                    })));
                                                  }}
                                                  onKeyDown={(e) => {
                                                    if (e.key === 'Enter') renameKoyebService(account, app, service);
                                                    if (e.key === 'Escape') cancelEditKoyebServiceName(service);
                                                  }}
                                                  onBlur={() => renameKoyebService(account, app, service)}
                                                  autoFocus
                                                />
                                              ) : (
                                                <Button
                                                  type="button"
                                                  variant="ghost"
                                                  size="sm"
                                                  onDoubleClick={() => startEditKoyebServiceName(service)}
                                                  className="min-w-0 justify-start px-0"
                                                  title="双击重命名"
                                                  icon={<Server className="h-3.5 w-3.5 text-kumo-info" />}
                                                >
                                                  <Text as="span" bold truncate>{service.name}</Text>
                                                </Button>
                                              )}
                                              <div className="flex flex-wrap items-center gap-1.5">
                                                <Badge variant={koyebServiceTypeVariant(service.type)}>{service.type || 'web'}</Badge>
                                                {service.resourceLimit?.cpu || service.resourceLimit?.memory ? (
                                                  <Badge variant="outline">
                                                    {service.resourceLimit?.cpu || '-'} CPU / {service.resourceLimit?.memory || '-'} RAM
                                                  </Badge>
                                                ) : null}
                                              </div>
                                            </div>
                                            <Badge variant={getKoyebStatusTone(service.status)} appearance="dot">
                                              {getKoyebStatusText(service.status)}
                                            </Badge>
                                          </div>

                                          {service.domains?.length ? (
                                            <div className="space-y-1">
                                              <div className="flex min-w-0 flex-wrap gap-1.5">
                                                {service.domains.map((dom) => {
                                                  const domain = dom.domain || dom.name || String(dom);
                                                  if (!domain) return null;
                                                  return (
                                                    <Link key={`${service._id}-${domain}`} href={`https://${domain}`} target="_blank" rel="noreferrer" variant="plain" className="min-w-0">
                                                      <Badge variant={dom.isGenerated ? 'outline' : 'success'} appearance={dom.isGenerated ? undefined : 'dot'} className="max-w-full">
                                                        <Globe className="h-3 w-3 shrink-0" />
                                                        <span className="truncate">{domain}</span>
                                                        {dom.isGenerated ? <span>默认</span> : null}
                                                      </Badge>
                                                    </Link>
                                                  );
                                                })}
                                              </div>
                                            </div>
                                          ) : null}

                                          {service.showInstances && service.instances ? (
                                            <div className="space-y-2">
                                              <div className="flex items-center justify-between gap-2">
                                                <Text as="span" size="xs" bold>实例列表 ({service.instances.length})</Text>
                                                <Button size="xs" variant="ghost" onClick={() => {
                                                  setKoyebAccounts(prev => prev.map(acc => ({
                                                    ...acc,
                                                    projects: acc.projects.map(p => ({
                                                      ...p,
                                                      services: p.services.map(s => {
                                                        if (s._id !== service._id) return s;
                                                        return { ...s, showInstances: false };
                                                      })
                                                    }))
                                                  })));
                                                }} icon={<X className="h-3 w-3" />} aria-label="关闭实例列表" title="关闭实例列表" />
                                              </div>
                                              <div className="space-y-2">
                                                {service.instances.map(inst => (
                                                  <div key={inst.id} className="flex min-w-0 items-center justify-between gap-2 border-t border-kumo-line/70 pt-2 first:border-t-0 first:pt-0">
                                                    <div className="min-w-0">
                                                      <ClipboardText size="sm" text={inst.id} />
                                                    </div>
                                                    <div className="flex shrink-0 items-center gap-1.5">
                                                      <Badge variant="neutral">{inst.region?.toUpperCase() || '-'}</Badge>
                                                      <Badge variant={getKoyebStatusTone(inst.status)} appearance="dot">{inst.status || 'unknown'}</Badge>
                                                    </div>
                                                  </div>
                                                ))}
                                              </div>
                                            </div>
                                          ) : null}

                                          <div className="flex flex-wrap justify-start gap-1">
                                            <Button shape="square" size="sm" variant="secondary" aria-label="重启服务" onClick={() => restartKoyebService(account, app, service)} title={service.status === 'SUSPENDED' ? '启动服务' : '重启服务'} icon={<RefreshCw className="h-3.5 w-3.5" />} />
                                            <Button shape="square" size="sm" variant="secondary" aria-label="重新部署服务" onClick={() => redeployKoyebService(account, app, service)} title="重新部署" icon={<Rocket className="h-3.5 w-3.5" />} />
                                            <Button shape="square" size="sm" variant="secondary" aria-label="编辑服务配置/镜像" onClick={() => openKoyebConfig(account, service)} title="编辑配置/镜像" icon={<Settings className="h-3.5 w-3.5" />} />
                                            <Button shape="square" size="sm" variant="secondary" aria-label="部署历史" onClick={() => openKoyebDeployments(account, service)} title="部署历史" icon={<History className="h-3.5 w-3.5" />} />
                                            <Button shape="square" size="sm" variant="secondary" aria-label="手动扩容" onClick={() => openKoyebScale(account, service)} title="手动扩容" icon={<Activity className="h-3.5 w-3.5" />} />
                                            <Button shape="square" size="sm" variant="secondary" aria-label="查看服务实例" onClick={() => fetchKoyebServiceInstances(account, service)} title="查看实例" loading={service.loadingInstances} icon={<Server className="h-3.5 w-3.5" />} />
                                            <Button shape="square" size="sm" variant="secondary" aria-label="查看服务日志" onClick={() => showKoyebServiceLogs(account, app, service)} title="查看日志" icon={<FileText className="h-3.5 w-3.5" />} />
                                            <Button shape="square" size="sm" variant="secondary" aria-label="实时日志" onClick={() => startKoyebLogTail(account, service)} title="实时日志（跟随）" icon={<Terminal className="h-3.5 w-3.5" />} />
                                          </div>
                                        </div>
                                      ))}
                                    </div>
                                  )}
                                </LayerCard.Primary>
                              </LayerCard>
                            );
                          })}
                        </div>
                      )}
                    </AnimatedCollapse>
                  </section>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* ==================== Fly.io Tab Content ==================== */}
      {activeTab === 'fly' && (
        <div className="space-y-3">
          {flyLoading && flyAccounts.length === 0 ? (
            <Empty
              size="base"
              icon={<Loader size={32} className="text-brand" />}
              title="正在加载 Fly.io"
              description="同步应用和域名状态"
            />
          ) : flyAccounts.length === 0 ? (
            <Empty
              size="base"
              icon={<FlyIoBrand className="h-8 w-8 text-brand" />}
              title="暂无 Fly.io 账号"
              description="需先添加 Fly.io API Token"
            />
          ) : (
            <div className="space-y-5">
              {flyAccounts.map((account) => {
                const expanded = isFlyAccountExpanded(account.name);
                return (
                  <section key={account.name} className="space-y-3">
                    <LayerCard className="self-start">
                      <LayerCard.Secondary
                        role="button"
                        tabIndex={0}
                        aria-expanded={expanded}
                        className="flex min-w-0 cursor-pointer flex-wrap items-center gap-3 cq-sm:justify-between"
                        onClick={() => toggleFlyAccount(account.name)}
                        onKeyDown={(e) => {
                          if (e.key === 'Enter' || e.key === ' ') {
                            e.preventDefault();
                            toggleFlyAccount(account.name);
                          }
                        }}
                      >
                        <Button
                          type="button"
                          variant="ghost"
                          size="sm"
                          tabIndex={-1}
                          className="min-w-0 flex-1 justify-start px-0 text-left"
                          icon={<ChevronDown className={`h-4 w-4 shrink-0 transition-transform duration-200 ${expanded ? 'rotate-180' : ''}`} />}
                        >
                          <span className="flex min-w-0 items-center gap-2 text-left">
                            <FlyIoBrand className="h-4 w-4 shrink-0 text-brand" />
                            <Text as="span" bold truncate>{account.name}</Text>
                          </span>
                        </Button>
                        <div className="flex shrink-0 flex-wrap items-center justify-end gap-2">
                          <Button size="sm" variant="secondary" onClick={(e) => { e.stopPropagation(); updateAllFlyAppsImage(account); }} icon={<Rocket className="h-3.5 w-3.5" />}>
                            批量更新
                          </Button>
                          <Button size="sm" variant="primary" onClick={(e) => { e.stopPropagation(); createFlyApp(account); }} icon={<Plus className="h-3.5 w-3.5" />}>
                            新建应用
                          </Button>
                        </div>
                      </LayerCard.Secondary>
                    </LayerCard>

                    <AnimatedCollapse open={expanded} panelClassName="overflow-visible">
                      {account.error ? (
                        <Empty
                          size="sm"
                          icon={<Info className="h-8 w-8 text-kumo-danger" />}
                          title="Fly.io 同步失败"
                          description={account.error}
                        />
                      ) : !account.projects || account.projects.length === 0 ? (
                        <Empty
                          size="sm"
                          icon={<FlyIoBrand className="h-8 w-8 text-brand" />}
                          title="暂无应用"
                          contents={<Button size="sm" variant="primary" onClick={() => createFlyApp(account)} icon={<Plus className="h-3.5 w-3.5" />}>新建应用</Button>}
                        />
                      ) : (
                        <div className="grid min-w-0 grid-cols-1 items-start gap-3 cq-sm:grid-cols-2 cq-md:grid-cols-3 cq-lg:grid-cols-4">
                          {account.projects.map((app) => (
                            <LayerCard key={app.id} className="self-start">
                              <LayerCard.Secondary className="flex min-w-0 items-start justify-between gap-3">
                                <div className="min-w-0 flex-1">
                                  {app.isEditing ? (
                                    <Input
                                      size="sm"
                                      aria-label="Fly 应用名称"
                                      type="text"
                                      value={app.editingName}
                                      onChange={(e) => {
                                        const val = e.target.value;
                                        setFlyAccounts(prev => prev.map(acc => ({
                                          ...acc,
                                          projects: acc.projects.map(p => {
                                            if (p.id !== app.id) return p;
                                            return { ...p, editingName: val };
                                          })
                                        })));
                                      }}
                                      onKeyDown={(e) => {
                                        if (e.key === 'Enter') saveFlyAppName(account, app);
                                        if (e.key === 'Escape') cancelEditFlyAppName(app);
                                      }}
                                      onBlur={() => saveFlyAppName(account, app)}
                                      autoFocus
                                    />
                                  ) : (
                                    <Button
                                      type="button"
                                      variant="ghost"
                                      size="sm"
                                      onDoubleClick={() => startEditFlyAppName(app)}
                                      className="min-w-0 justify-start px-0"
                                      title="双击重命名"
                                      icon={<FlyIoBrand className="h-4 w-4 text-brand" />}
                                    >
                                      <Text as="span" bold truncate>{app.name}</Text>
                                    </Button>
                                  )}
                                </div>
                                <div className="flex shrink-0 flex-wrap justify-end gap-1.5">
                                  <Badge variant={getFlyStatusTone(app.status)} appearance="dot">
                                    {getFlyStatusText(app.status)}
                                  </Badge>
                                  <Badge variant="neutral">{app.machines?.length ?? '-'} 机器</Badge>
                                </div>
                              </LayerCard.Secondary>

                              <LayerCard.Primary className="space-y-3">
                                <div className="space-y-2">
                                  <div className="flex min-w-0 flex-wrap gap-1.5">
                                    {app.hostname ? (
                                      <Link href={`https://${app.hostname}`} target="_blank" rel="noreferrer" variant="plain" className="min-w-0">
                                        <Badge variant="outline" className="max-w-full">
                                          <Globe className="h-3 w-3 shrink-0" />
                                          <span className="truncate">{app.hostname}</span>
                                          <span>默认</span>
                                        </Badge>
                                      </Link>
                                    ) : (
                                      <ClipboardText size="sm" text={app.name} />
                                    )}
                                    {app.domains?.map((dom) => (
                                      <Link key={dom.domain} href={`https://${dom.domain}`} target="_blank" rel="noreferrer" variant="plain" className="min-w-0">
                                        <Badge variant={dom.isVerified ? 'success' : 'warning'} appearance={dom.isVerified ? 'dot' : 'filled'} className="max-w-full">
                                          <Globe className="h-3 w-3 shrink-0" />
                                          <span className="truncate">{dom.domain}</span>
                                        </Badge>
                                      </Link>
                                    ))}
                                  </div>
                                </div>

                                {app.showMachines && app.machines ? (
                                  <div className="space-y-2">
                                    <div className="flex items-center justify-between gap-2">
                                      <Text as="span" size="xs" bold>机器实例 ({app.machines.length})</Text>
                                      <Button size="xs" variant="ghost" onClick={() => {
                                        setFlyAccounts(prev => prev.map(acc => ({
                                          ...acc,
                                          projects: acc.projects.map(p => {
                                            if (p.id !== app.id) return p;
                                            return { ...p, showMachines: false };
                                          })
                                        })));
                                      }} icon={<X className="h-3 w-3" />} aria-label="关闭机器列表" title="关闭机器列表" />
                                    </div>
                                    <div className="space-y-2">
                                    {app.machines.map(m => (
                                      <div key={m.id} className="space-y-2">
                                          <div className="flex items-start justify-between gap-2">
                                            <div className="min-w-0 space-y-1">
                                              <ClipboardText size="sm" text={m.id} />
                                              <div className="flex flex-wrap items-center gap-1.5">
                                                <Badge variant={getFlyStatusTone(m.state)} appearance="dot">{m.state || 'unknown'}</Badge>
                                                <Badge variant="neutral">{formatRegion(m.region) || 'REGION'}</Badge>
                                              </div>
                                            </div>
                                            <Button shape="square" size="sm" variant="ghost" aria-label="机器详情" title="机器详情" onClick={() => showFlyMachineDetails(account, app, m)} icon={<Info className="h-3.5 w-3.5" />} />
                                          </div>
                                          <div className="flex flex-wrap gap-1">
                                            <Button shape="square" size="xs" variant="secondary" aria-label="启动机器" title="启动" onClick={() => runFlyMachineAction(account, app, m, 'start', '机器已启动')} icon={<Play className="h-3 w-3" />} />
                                            <Button shape="square" size="xs" variant="secondary" aria-label="停止机器" title="停止" onClick={() => runFlyMachineAction(account, app, m, 'stop', '机器已停止', { body: { signal: 'SIGTERM', timeout: '30s' } })} icon={<Square className="h-3 w-3" />} />
                                            <Button shape="square" size="xs" variant="secondary" aria-label="挂起机器" title="挂起" onClick={() => runFlyMachineAction(account, app, m, 'suspend', '机器已挂起')} icon={<Pause className="h-3 w-3" />} />
                                            <Button shape="square" size="xs" variant="secondary" aria-label="禁用调度" title="Cordon" onClick={() => runFlyMachineAction(account, app, m, 'cordon', '机器已 cordon')} icon={<Lock className="h-3 w-3" />} />
                                            <Button shape="square" size="xs" variant="secondary" aria-label="恢复调度" title="Uncordon" onClick={() => runFlyMachineAction(account, app, m, 'uncordon', '机器已 uncordon')} icon={<ExternalLink className="h-3 w-3" />} />
                                            <Button shape="square" size="xs" variant="secondary" aria-label="获取 lease" title="获取 lease" onClick={() => acquireFlyMachineLease(account, app, m)} icon={<Lock className="h-3 w-3" />} />
                                            <Button shape="square" size="xs" variant="secondary" aria-label="设置 metadata" title="设置 metadata" onClick={() => setFlyMachineMetadata(account, app, m)} icon={<Settings className="h-3 w-3" />} />
                                            <Button shape="square" size="xs" variant={isArmed(`fly-machine:${m.id}`) ? 'destructive' : 'secondary-destructive'} aria-label="删除机器" title="删除机器" onClick={() => deleteFlyMachine(account, app, m)} icon={<Trash className="h-3 w-3" />} />
                                          </div>
                                      </div>
                                    ))}
                                    </div>
                                  </div>
                                ) : null}

                                <div className="flex flex-wrap justify-start gap-1">
                                    <Button shape="square" size="sm" variant="secondary" aria-label="重启应用" onClick={() => redeployFlyApp(account, app)} title="重启应用" icon={<RefreshCw className="h-3.5 w-3.5" />} />
                                    <Button shape="square" size="sm" variant="secondary" aria-label="更新容器镜像" onClick={() => updateFlyAppImage(account, app)} title="更新容器镜像" icon={<Rocket className="h-3.5 w-3.5" />} />
                                    <Button shape="square" size="sm" variant="secondary" aria-label="创建机器" onClick={() => createFlyMachine(account, app)} title="创建机器" icon={<Plus className="h-3.5 w-3.5" />} />
                                    <Button shape="square" size="sm" variant="secondary" aria-label="查看机器实例" onClick={() => fetchFlyMachines(account, app)} title="查看机器/实例" loading={app.loadingMachines} icon={<Server className="h-3.5 w-3.5" />} />
                                    <Button shape="square" size="sm" variant="secondary" aria-label="查看运行日志" onClick={() => showFlyAppLogs(account, app)} title="查看运行日志" icon={<FileText className="h-3.5 w-3.5" />} />
                                    <Button shape="square" size="sm" variant="secondary" aria-label="查看应用配置" onClick={() => viewFlyConfig(account, app)} title="查看应用配置" icon={<Terminal className="h-3.5 w-3.5" />} />
                                    <Button shape="square" size="sm" variant={isArmed(`fly-app:${app.id}`) ? 'destructive' : 'secondary-destructive'} aria-label="删除 Fly 应用" onClick={() => deleteFlyApp(account, app)} title="删除应用" icon={<Trash className="h-3.5 w-3.5" />} />
                                </div>
                              </LayerCard.Primary>
                            </LayerCard>
                          ))}
                        </div>
                      )}
                    </AnimatedCollapse>
                  </section>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* ==================== Configuration Tab Content ==================== */}
      {activeTab === 'config' && (
        <div className="grid min-w-0 gap-3 cq-xl:grid-cols-[minmax(18rem,24rem)_minmax(0,1fr)] cq-xl:items-start">
          <SectionCard
            title="自动刷新"
            icon={<Settings className="h-4 w-4 text-brand" />}
            actions={(
              <Button size="sm" onClick={saveSettings} icon={<Save className="h-3.5 w-3.5" />}>
                保存配置
              </Button>
            )}
            bodyClassName="flex flex-col gap-3"
          >
            <div className="flex min-w-0 items-center gap-2 rounded-md border border-kumo-line bg-kumo-recessed/20 px-3 py-2">
              <div className="flex w-20 shrink-0 items-center gap-2 text-xs font-semibold text-kumo-strong">
                <KoyebBrand className="h-3.5 w-3.5 text-kumo-info" />
                Koyeb
              </div>
              <Input
                size="sm"
                aria-label="Koyeb 自动刷新间隔"
                type="number"
                min="5"
                max={MAX_PAAS_REFRESH_INTERVAL_SEC}
                value={koyebIntervalSec}
                onChange={(e) => setKoyebIntervalSec(normalizeRefreshIntervalInputSec(e.target.value))}
                className="w-24 font-mono text-xs"
              />
              <span className="text-xs text-kumo-subtle">秒</span>
            </div>
            <div className="flex min-w-0 items-center gap-2 rounded-md border border-kumo-line bg-kumo-recessed/20 px-3 py-2">
              <div className="flex w-20 shrink-0 items-center gap-2 text-xs font-semibold text-kumo-strong">
                <FlyIoBrand className="h-3.5 w-3.5 text-brand" />
                Fly.io
              </div>
              <Input
                size="sm"
                aria-label="Fly.io 自动刷新间隔"
                type="number"
                min="5"
                max={MAX_PAAS_REFRESH_INTERVAL_SEC}
                value={flyIntervalSec}
                onChange={(e) => setFlyIntervalSec(normalizeRefreshIntervalInputSec(e.target.value))}
                className="w-24 font-mono text-xs"
              />
              <span className="text-xs text-kumo-subtle">秒</span>
            </div>
            <div className="text-xs text-kumo-subtle">
              自动刷新只在对应监控页打开时生效。
            </div>
          </SectionCard>

          <SectionCard
            title="账号管理"
            icon={<Users className="h-4 w-4 text-kumo-success" />}
            className="min-w-0"
            actions={(
                <>
                  <Button size="sm" onClick={() => setShowAddKoyebModal(true)} icon={<KoyebBrand className="h-3.5 w-3.5" />}>添加 Koyeb</Button>
                  <Button size="sm" onClick={() => setShowAddFlyModal(true)} icon={<FlyIoBrand className="h-3.5 w-3.5" />}>添加 Fly.io</Button>
                  <Toolbar size="sm" aria-label="导出导入 PaaS 账号" className="shrink-0">
                    <Toolbar.Button onClick={exportPaasAccounts} aria-label="导出 PaaS 账号" title="导出账号" icon={<Upload className="h-3.5 w-3.5" />}>
                      <span className="hidden cq-sm:inline">导出</span>
                    </Toolbar.Button>
                    <Toolbar.Button onClick={importPaasAccounts} aria-label="导入 PaaS 账号" title="导入账号" icon={<Download className="h-3.5 w-3.5" />}>
                      <span className="hidden cq-sm:inline">导入</span>
                    </Toolbar.Button>
                  </Toolbar>
              </>
            )}
            bodyPadding="none"
          >
            <div className="overflow-x-auto">
              <Table layout="fixed" className="min-w-[56rem]">
                <colgroup>
                  <col className="w-[9rem]" />
                  <col className="w-[20%]" />
                  <col />
                  <col className="w-[8rem]" />
                  <col className="w-[7rem]" />
                </colgroup>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head>平台</Table.Head>
                    <Table.Head>备注</Table.Head>
                    <Table.Head>邮箱 / 标识</Table.Head>
                    <Table.Head className="text-right">余额</Table.Head>
                    <Table.Head className="app-table-action">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {koyebManagedAccounts.length === 0 && flyManagedAccounts.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={5} className="py-8 text-center text-kumo-subtle">暂无 PaaS 账号</Table.Cell>
                    </Table.Row>
                  ) : (
                    <>
                      {koyebManagedAccounts.map((account) => (
                        <Table.Row key={`koyeb-${account.id}`} className="hover:bg-kumo-recessed/25">
                          <Table.Cell>
                            <Badge variant="outline" className="inline-flex items-center gap-1">
                              <KoyebBrand className="h-3.5 w-3.5 text-kumo-info" />
                              Koyeb
                            </Badge>
                          </Table.Cell>
                          <Table.Cell className="font-medium text-kumo-strong">{account.name}</Table.Cell>
                          <Table.Cell className="truncate font-mono text-xs text-kumo-subtle">{account.email || '-'}</Table.Cell>
                          <Table.Cell className="text-right font-mono text-xs text-kumo-info">${(account.balance || 0).toFixed(2)}</Table.Cell>
                          <Table.Cell className="text-right">
                            <Button shape="square" size="sm" variant={isArmed(`koyeb-account:${account.id}`) ? 'destructive' : 'secondary-destructive'} aria-label="删除 Koyeb 账号" title="删除" onClick={() => removeKoyebAccount(account.id)} icon={<Trash className="h-4 w-4" />} />
                          </Table.Cell>
                        </Table.Row>
                      ))}
                      {flyManagedAccounts.map((account) => (
                        <Table.Row key={`fly-${account.id}`} className="hover:bg-kumo-recessed/25">
                          <Table.Cell>
                            <Badge variant="outline" className="inline-flex items-center gap-1">
                              <FlyIoBrand className="h-3.5 w-3.5 text-brand" />
                              Fly.io
                            </Badge>
                          </Table.Cell>
                          <Table.Cell className="font-medium text-kumo-strong">{account.name}</Table.Cell>
                          <Table.Cell className="truncate font-mono text-xs text-kumo-subtle">{account.email || '-'}</Table.Cell>
                          <Table.Cell className="text-right font-mono text-xs text-kumo-subtle">-</Table.Cell>
                          <Table.Cell className="text-right">
                            <Button shape="square" size="sm" variant={isArmed(`fly-account:${account.id}`) ? 'destructive' : 'secondary-destructive'} aria-label="删除 Fly.io 账号" title="删除" onClick={() => removeFlyAccount(account.id, account.name)} icon={<Trash className="h-4 w-4" />} />
                          </Table.Cell>
                        </Table.Row>
                      ))}
                    </>
                  )}
                </Table.Body>
              </Table>
            </div>
          </SectionCard>
        </div>
      )}
      </div>

      {/* ==================== Modals & Dialogs ==================== */}

      {/* Add Koyeb Dialog */}
      <Dialog.Root open={showAddKoyebModal} onOpenChange={setShowAddKoyebModal}>
        <Dialog className="!w-[min(32rem,calc(100vw-2rem))] !max-w-[min(32rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            添加 Koyeb 账号
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            请输入 Koyeb API 令牌。以备注名区分。
          </Dialog.Description>
          <div className="space-y-4">
            <div className="space-y-1">
              <label className="text-[11px] font-bold text-kumo-subtle">备注名称</label>
              <Input size="sm"
                aria-label="Koyeb 备注名称"
                type="text"
                value={newKoyebName}
                onChange={(e) => setNewKoyebName(e.target.value)}
                placeholder="我的 Koyeb 账号 1"
                className="w-full text-kumo-strong p-2 text-xs"
              />
            </div>
            <div className="space-y-1">
              <label className="text-[11px] font-bold text-kumo-subtle">API 令牌</label>
              <Input size="sm"
                aria-label="Koyeb API 令牌"
                type="text"
                value={newKoyebToken}
                onChange={(e) => setNewKoyebToken(e.target.value)}
                placeholder="koyeb_api_token"
                autoComplete="off"
                data-1p-ignore
                data-lpignore="true"
                data-bwignore="true"
                data-form-type="other"
                spellCheck={false}
                className="w-full text-kumo-strong p-2 text-xs"
              />
            </div>
            {koyebAddAccountError && (
              <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">
                {koyebAddAccountError}
              </div>
            )}
            <div className="flex justify-end gap-2 pt-2">
              <Dialog.Close
                render={(props) => (
                  <Button size="sm"
                    {...props}
                    variant="secondary"
                    className="text-xs"
                  >
                    取消
                  </Button>
                )}
              />
              <Button size="sm" onClick={addKoyebAccount} disabled={koyebAddingAccount} className="text-xs">
                {koyebAddingAccount ? '正在验证并添加...' : '确定添加'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* Add Fly Dialog */}
      <Dialog.Root open={showAddFlyModal} onOpenChange={setShowAddFlyModal}>
        <Dialog className="!w-[min(32rem,calc(100vw-2rem))] !max-w-[min(32rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            添加 Fly.io 账号
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            请输入 Fly.io API 令牌（以 flyv1_ 开头）。
          </Dialog.Description>
          <div className="space-y-4">
            <div className="space-y-1">
              <label className="text-[11px] font-bold text-kumo-subtle">备注名称</label>
              <Input size="sm"
                aria-label="Fly.io 备注名称"
                type="text"
                value={newFlyName}
                onChange={(e) => setNewFlyName(e.target.value)}
                placeholder="我的 Fly.io 账号 1"
                className="w-full text-kumo-strong p-2 text-xs"
              />
            </div>
            <div className="space-y-1">
              <label className="text-[11px] font-bold text-kumo-subtle">API 令牌</label>
              <Input size="sm"
                aria-label="Fly.io API 令牌"
                type="text"
                value={newFlyToken}
                onChange={(e) => setNewFlyToken(e.target.value)}
                placeholder="flyv1_xxxx"
                autoComplete="off"
                data-1p-ignore
                data-lpignore="true"
                data-bwignore="true"
                data-form-type="other"
                spellCheck={false}
                className="w-full text-kumo-strong p-2 text-xs"
              />
            </div>
            {flyAddAccountError && (
              <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">
                {flyAddAccountError}
              </div>
            )}
            <div className="flex justify-end gap-2 pt-2">
              <Dialog.Close
                render={(props) => (
                  <Button size="sm"
                    {...props}
                    variant="secondary"
                    className="text-xs"
                  >
                    取消
                  </Button>
                )}
              />
              <Button size="sm" onClick={addFlyAccount} disabled={flyAddingAccount} className="text-xs">
                {flyAddingAccount ? '正在验证并添加...' : '确定添加'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* Koyeb 服务配置编辑 Dialog */}
      <Dialog.Root open={!!koyebConfigTarget} onOpenChange={(open) => { if (!open) setKoyebConfigTarget(null); }}>
        <Dialog className="@container flex !w-[min(40rem,calc(100vw-2rem))] !max-w-[min(40rem,calc(100vw-2rem))] flex-col p-6">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            编辑服务配置
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            {koyebConfigTarget ? `${koyebConfigTarget.service.name} · 更新后将触发一次新部署` : ''}
          </Dialog.Description>
          <div className="space-y-3 overflow-y-auto">
            <div className="space-y-1">
              <label className="text-[11px] font-bold text-kumo-subtle">镜像地址</label>
              <Input size="sm" aria-label="镜像地址" type="text" value={koyebConfigForm.image} onChange={(e) => setKoyebConfigForm((f) => ({ ...f, image: e.target.value }))} placeholder="registry.hub.docker.com/xxx/app:latest" className="w-full text-xs" />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <label className="text-[11px] font-bold text-kumo-subtle">启动命令</label>
                <Input size="sm" aria-label="启动命令" type="text" value={koyebConfigForm.command} onChange={(e) => setKoyebConfigForm((f) => ({ ...f, command: e.target.value }))} placeholder="留空使用镜像默认" className="w-full text-xs" />
              </div>
              <div className="space-y-1">
                <label className="text-[11px] font-bold text-kumo-subtle">参数（逗号分隔）</label>
                <Input size="sm" aria-label="启动参数" type="text" value={koyebConfigForm.args} onChange={(e) => setKoyebConfigForm((f) => ({ ...f, args: e.target.value }))} placeholder="--port,3000" className="w-full text-xs" />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <label className="text-[11px] font-bold text-kumo-subtle">端口（port:protocol）</label>
                <Input size="sm" aria-label="端口" type="text" value={koyebConfigForm.ports} onChange={(e) => setKoyebConfigForm((f) => ({ ...f, ports: e.target.value }))} placeholder="8080:http, 3000:tcp" className="w-full text-xs" />
              </div>
              <div className="space-y-1">
                <label className="text-[11px] font-bold text-kumo-subtle">区域（逗号分隔）</label>
                <Input size="sm" aria-label="区域" type="text" value={koyebConfigForm.regions} onChange={(e) => setKoyebConfigForm((f) => ({ ...f, regions: e.target.value }))} placeholder="fra,sin,tok" className="w-full text-xs" />
              </div>
            </div>
            <div className="space-y-1">
              <label className="text-[11px] font-bold text-kumo-subtle">实例规格</label>
              <Select
                aria-label="实例规格" size="sm"
                value={koyebConfigForm.instanceType}
                onValueChange={(value) => setKoyebConfigForm((f) => ({ ...f, instanceType: String(value) }))}
                items={['nano', 'micro', 'small', 'medium', 'large', 'xlarge'].map((t) => ({ value: t, label: t }))}
                className="w-full"
              />
            </div>
            <div className="space-y-1">
              <label className="text-[11px] font-bold text-kumo-subtle">环境变量（每行 K=V）</label>
              <Textarea size="sm" aria-label="环境变量" value={koyebConfigForm.env} onChange={(e) => setKoyebConfigForm((f) => ({ ...f, env: e.target.value }))} placeholder={'DB_HOST=db.internal\nPORT=3000'} className="min-h-24 w-full text-xs font-mono" />
            </div>
            <Checkbox
              checked={koyebConfigForm.skipBuild}
              onCheckedChange={(checked) => setKoyebConfigForm((f) => ({ ...f, skipBuild: !!checked }))}
              label="跳过构建（复用上次构建镜像，仅改配置不重建）"
            />
            {koyebConfigError && (
              <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebConfigError}</div>
            )}
          </div>
          <div className="flex justify-end gap-2 pt-4">
            <Dialog.Close render={(props) => <Button size="sm" {...props} variant="secondary" className="text-xs">取消</Button>} />
            <Button size="sm" onClick={saveKoyebConfig} disabled={koyebConfigSaving} className="text-xs">
              {koyebConfigSaving ? '更新中...' : '保存并部署'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* Koyeb 部署历史 Dialog */}
      <Dialog.Root open={!!koyebDeployTarget} onOpenChange={(open) => { if (!open) setKoyebDeployTarget(null); }}>
        <Dialog className="flex h-[70vh] !w-[min(48rem,calc(100vw-2rem))] !max-w-[min(48rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-2 border-b border-kumo-line p-4">
            <div>
              <Dialog.Title className="text-sm font-bold text-kumo-strong">部署历史</Dialog.Title>
              {koyebDeployTarget && <p className="text-[10px] text-kumo-subtle mt-0.5">{koyebDeployTarget.service.name}</p>}
            </div>
            <div className="flex items-center gap-2">
              {koyebDeployTarget && (
                <Button size="sm" variant="secondary" onClick={() => openKoyebDeployments(koyebDeployTarget.account, koyebDeployTarget.service)} icon={<RefreshCw className="h-3.5 w-3.5" />}>刷新</Button>
              )}
              <Button shape="square" size="sm" variant="ghost" aria-label="关闭" onClick={() => setKoyebDeployTarget(null)} icon={<X className="h-4 w-4" />} />
            </div>
          </div>
          <div className="flex-1 overflow-y-auto p-4">
            {koyebDeployLoading ? (
              <div className="flex h-full items-center justify-center gap-2 text-kumo-subtle"><Loader size={16} />正在加载部署记录...</div>
            ) : koyebDeployError ? (
              <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebDeployError}</div>
            ) : koyebDeployments.length === 0 ? (
              <div className="py-12 text-center text-kumo-subtle text-sm">暂无部署记录</div>
            ) : (
              <div className="space-y-2">
                {koyebDeployments.map((dep) => {
                  const image = dep.definition?.docker?.image || '';
                  const status = String(dep.status || '').toUpperCase();
                  const cancellable = ['PENDING', 'PROVISIONING', 'SCHEDULED', 'ALLOCATING', 'STARTING'].includes(status);
                  return (
                    <div key={dep.id} className="space-y-1.5 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 p-3">
                      <div className="flex min-w-0 flex-wrap items-center justify-between gap-2">
                        <div className="min-w-0">
                          <ClipboardText size="sm" text={dep.id} />
                          {image && <div className="mt-1 truncate font-mono text-[10px] text-kumo-subtle">{image}</div>}
                        </div>
                        <div className="flex shrink-0 items-center gap-2">
                          <Badge variant={getKoyebStatusTone(status)} appearance="dot">{getKoyebStatusText(status)}</Badge>
                          {dep.version ? <Badge variant="neutral">v{dep.version}</Badge> : null}
                          {cancellable && (
                            <Button shape="square" size="xs" variant="secondary-destructive" aria-label="取消部署" title="取消部署" onClick={() => cancelKoyebDeployment(dep)} icon={<X className="h-3 w-3" />} />
                          )}
                        </div>
                      </div>
                      <div className="flex flex-wrap items-center gap-2 text-[10px] text-kumo-subtle">
                        <span>创建于 {dep.created_at ? new Date(dep.created_at).toLocaleString() : '-'}</span>
                        {dep.metadata?.git?.sha ? <span>· SHA {String(dep.metadata.git.sha).slice(0, 12)}</span> : null}
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </Dialog>
      </Dialog.Root>

      {/* Koyeb 手动扩容 Dialog */}
      <Dialog.Root open={!!koyebScaleTarget} onOpenChange={(open) => { if (!open) setKoyebScaleTarget(null); }}>
        <Dialog className="!w-[min(28rem,calc(100vw-2rem))] !max-w-[min(28rem,calc(100vw-2rem))] p-6">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">手动扩容</Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            {koyebScaleTarget ? `${koyebScaleTarget.service.name} · 设置实例副本数（覆盖自动扩缩容策略）` : ''}
          </Dialog.Description>
          <div className="space-y-3">
            <div className="space-y-1">
              <label className="text-[11px] font-bold text-kumo-subtle">作用域（scopes，逗号分隔，通常为服务类型名）</label>
              <Input size="sm" aria-label="作用域" type="text" value={koyebScaleScope} onChange={(e) => setKoyebScaleScope(e.target.value)} placeholder="web" className="w-full text-xs" />
            </div>
            <div className="space-y-1">
              <label className="text-[11px] font-bold text-kumo-subtle">实例数量</label>
              <Input size="sm" aria-label="实例数量" type="number" min="1" value={koyebScaleInstances} onChange={(e) => setKoyebScaleInstances(Number(e.target.value) || 1)} className="w-full text-xs" />
            </div>
            {koyebScaleError && (
              <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebScaleError}</div>
            )}
          </div>
          <div className="flex justify-end gap-2 pt-4">
            <Button size="sm" variant="secondary-destructive" onClick={resetKoyebScale} className="text-xs">重置为自动</Button>
            <Dialog.Close render={(props) => <Button size="sm" {...props} variant="secondary" className="text-xs">取消</Button>} />
            <Button size="sm" onClick={saveKoyebScale} disabled={koyebScaleSaving} className="text-xs">
              {koyebScaleSaving ? '保存中...' : '保存'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* Koyeb 域名管理 Dialog */}
      <Dialog.Root open={!!koyebDomainsTarget} onOpenChange={(open) => { if (!open) setKoyebDomainsTarget(null); }}>
        <Dialog className="flex h-[70vh] !w-[min(44rem,calc(100vw-2rem))] !max-w-[min(44rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-2 border-b border-kumo-line p-4">
            <div>
              <Dialog.Title className="text-sm font-bold text-kumo-strong">域名管理</Dialog.Title>
              {koyebDomainsTarget && <p className="text-[10px] text-kumo-subtle mt-0.5">{koyebDomainsTarget.app ? `应用 ${koyebDomainsTarget.app.name}` : '组织全部域名'}</p>}
            </div>
            <Button shape="square" size="sm" variant="ghost" aria-label="关闭" onClick={() => setKoyebDomainsTarget(null)} icon={<X className="h-4 w-4" />} />
          </div>
          <div className="border-b border-kumo-line p-4">
            <div className="flex gap-2">
              <Input size="sm" aria-label="新域名" type="text" value={koyebNewDomain} onChange={(e) => setKoyebNewDomain(e.target.value)} placeholder="app.example.com" className="flex-1 text-xs" onKeyDown={(e) => { if (e.key === 'Enter') addKoyebDomain(); }} />
              <Button size="sm" onClick={addKoyebDomain} icon={<Plus className="h-3.5 w-3.5" />} className="text-xs">添加</Button>
            </div>
            <p className="mt-1.5 text-[10px] text-kumo-subtle">绑定后按 Koyeb 提示配置 DNS 记录（CNAME/A 记录）。</p>
          </div>
          <div className="flex-1 overflow-y-auto p-4">
            {koyebDomainsLoading ? (
              <div className="flex h-full items-center justify-center gap-2 text-kumo-subtle"><Loader size={16} />正在加载域名...</div>
            ) : koyebDomainsError ? (
              <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebDomainsError}</div>
            ) : koyebDomains.length === 0 ? (
              <div className="py-12 text-center text-kumo-subtle text-sm">暂无域名</div>
            ) : (
              <div className="space-y-2">
                {koyebDomains.map((domain) => (
                  <div key={domain.id} className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 p-3">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-mono text-xs text-kumo-strong">{domain.name}</span>
                        <Badge variant={domain.type === 'AUTOASSIGNED' ? 'outline' : 'success'}>{domain.type === 'AUTOASSIGNED' ? '自动' : '自定义'}</Badge>
                      </div>
                      <div className="mt-1 text-[10px] text-kumo-subtle">
                        状态 {getKoyebStatusText(domain.status)}
                        {domain.verified_at ? ` · 已验证 ${new Date(domain.verified_at).toLocaleString()}` : ''}
                        {domain.intended_cname ? ` · CNAME: ${domain.intended_cname}` : ''}
                      </div>
                    </div>
                    <div className="flex shrink-0 items-center gap-1">
                      <Button shape="square" size="xs" variant="secondary" aria-label="刷新校验" title="刷新校验" onClick={() => refreshKoyebDomain(domain)} icon={<RefreshCw className="h-3 w-3" />} />
                      <Button shape="square" size="xs" variant={isArmed(`koyeb-domain:${domain.id}`) ? 'destructive' : 'secondary-destructive'} aria-label="删除域名" title="删除域名" onClick={() => deleteKoyebDomain(domain)} icon={<Trash className="h-3 w-3" />} />
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </Dialog>
      </Dialog.Root>

      {/* Koyeb Secrets 管理 Dialog */}
      <Dialog.Root open={!!koyebSecretsTarget} onOpenChange={(open) => { if (!open) setKoyebSecretsTarget(null); }}>
        <Dialog className="flex h-[70vh] !w-[min(44rem,calc(100vw-2rem))] !max-w-[min(44rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-2 border-b border-kumo-line p-4">
            <div>
              <Dialog.Title className="text-sm font-bold text-kumo-strong">Secrets 密钥管理</Dialog.Title>
              {koyebSecretsTarget && <p className="text-[10px] text-kumo-subtle mt-0.5">{koyebSecretsTarget.name} · 组织级密钥，可在部署环境变量中以 secret 引用</p>}
            </div>
            <Button shape="square" size="sm" variant="ghost" aria-label="关闭" onClick={() => setKoyebSecretsTarget(null)} icon={<X className="h-4 w-4" />} />
          </div>
          <div className="border-b border-kumo-line p-4">
            <div className="flex gap-2">
              <Input size="sm" aria-label="密钥名称" type="text" value={koyebNewSecretName} onChange={(e) => setKoyebNewSecretName(e.target.value)} placeholder="密钥名称（如 DB_PASSWORD）" className="w-48 text-xs" />
              <Input size="sm" aria-label="密钥值" type="password" value={koyebNewSecretValue} onChange={(e) => setKoyebNewSecretValue(e.target.value)} placeholder="密钥值" className="flex-1 text-xs" onKeyDown={(e) => { if (e.key === 'Enter') addKoyebSecret(); }} />
              <Button size="sm" onClick={addKoyebSecret} icon={<Plus className="h-3.5 w-3.5" />} className="text-xs">添加</Button>
            </div>
          </div>
          <div className="flex-1 overflow-y-auto p-4">
            {koyebSecretsLoading ? (
              <div className="flex h-full items-center justify-center gap-2 text-kumo-subtle"><Loader size={16} />正在加载密钥...</div>
            ) : koyebSecretsError ? (
              <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebSecretsError}</div>
            ) : koyebSecrets.length === 0 ? (
              <div className="py-12 text-center text-kumo-subtle text-sm">暂无密钥</div>
            ) : (
              <div className="space-y-2">
                {koyebSecrets.map((secret) => (
                  <div key={secret.id} className="flex min-w-0 flex-wrap items-center justify-between gap-2 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 p-3">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="font-mono text-xs text-kumo-strong">{secret.name}</span>
                        <Badge variant="neutral">{secret.type || 'SIMPLE'}</Badge>
                      </div>
                      <div className="mt-1 text-[10px] text-kumo-subtle">
                        更新于 {secret.updated_at ? new Date(secret.updated_at).toLocaleString() : '-'}
                        {secret.project_id ? ' · 项目级' : ' · 组织级'}
                      </div>
                    </div>
                    <div className="flex shrink-0 items-center gap-1">
                      <Button shape="square" size="xs" variant="secondary" aria-label="更新值" title="更新值" onClick={() => updateKoyebSecretValue(secret)} icon={<Settings className="h-3 w-3" />} />
                      <Button shape="square" size="xs" variant={isArmed(`koyeb-secret:${secret.id}`) ? 'destructive' : 'secondary-destructive'} aria-label="删除密钥" title="删除密钥" onClick={() => deleteKoyebSecret(secret)} icon={<Trash className="h-3 w-3" />} />
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        </Dialog>
      </Dialog.Root>

      {/* Koyeb 创建服务 Dialog */}
      <Dialog.Root open={!!koyebCreateTarget} onOpenChange={(open) => { if (!open) setKoyebCreateTarget(null); }}>
        <Dialog className="flex max-h-[90vh] !w-[min(40rem,calc(100vw-2rem))] !max-w-[min(40rem,calc(100vw-2rem))] flex-col p-6">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">新建服务</Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            {koyebCreateTarget ? `在应用 ${koyebCreateTarget.app.name} 下创建服务` : ''}
          </Dialog.Description>
          <div className="space-y-3 overflow-y-auto">
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <label className="text-[11px] font-bold text-kumo-subtle">服务名称</label>
                <Input size="sm" aria-label="服务名称" type="text" value={koyebCreateForm.name} onChange={(e) => setKoyebCreateForm((f) => ({ ...f, name: e.target.value }))} placeholder="web" className="w-full text-xs" />
              </div>
              <div className="space-y-1">
                <label className="text-[11px] font-bold text-kumo-subtle">类型</label>
                <Select
                  aria-label="服务类型" size="sm"
                  value={koyebCreateForm.type}
                  onValueChange={(value) => setKoyebCreateForm((f) => ({ ...f, type: String(value) }))}
                  items={[{ value: 'web', label: 'web' }, { value: 'worker', label: 'worker' }, { value: 'job', label: 'job' }]}
                  className="w-full"
                />
              </div>
            </div>
            <div className="space-y-1">
              <label className="text-[11px] font-bold text-kumo-subtle">镜像地址</label>
              <Input size="sm" aria-label="镜像地址" type="text" value={koyebCreateForm.image} onChange={(e) => setKoyebCreateForm((f) => ({ ...f, image: e.target.value }))} placeholder="registry.hub.docker.com/xxx/app:latest" className="w-full text-xs" />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <label className="text-[11px] font-bold text-kumo-subtle">启动命令</label>
                <Input size="sm" aria-label="启动命令" type="text" value={koyebCreateForm.command} onChange={(e) => setKoyebCreateForm((f) => ({ ...f, command: e.target.value }))} placeholder="留空使用镜像默认" className="w-full text-xs" />
              </div>
              <div className="space-y-1">
                <label className="text-[11px] font-bold text-kumo-subtle">端口（port:protocol）</label>
                <Input size="sm" aria-label="端口" type="text" value={koyebCreateForm.ports} onChange={(e) => setKoyebCreateForm((f) => ({ ...f, ports: e.target.value }))} placeholder="8080:http" className="w-full text-xs" />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <label className="text-[11px] font-bold text-kumo-subtle">实例规格</label>
                <Select
                  aria-label="实例规格" size="sm"
                  value={koyebCreateForm.instanceType}
                  onValueChange={(value) => setKoyebCreateForm((f) => ({ ...f, instanceType: String(value) }))}
                  items={(koyebCatalogInstances.length > 0 ? koyebCatalogInstances : ['nano', 'micro', 'small', 'medium', 'large', 'xlarge']).map((t) => {
                    const id = typeof t === 'string' ? t : (t.id || t.name || '');
                    const label = typeof t === 'string' ? t : `${id}${t.memory ? ' · ' + t.memory : ''}${t.price_monthly ? ' · $' + t.price_monthly + '/月' : ''}`;
                    return { value: id, label };
                  })}
                  className="w-full"
                />
              </div>
              <div className="space-y-1">
                <label className="text-[11px] font-bold text-kumo-subtle">区域（逗号分隔）</label>
                <Input size="sm" aria-label="区域" type="text" value={koyebCreateForm.regions} onChange={(e) => setKoyebCreateForm((f) => ({ ...f, regions: e.target.value }))} placeholder="fra,sin,tok" className="w-full text-xs" />
              </div>
            </div>
            {koyebCatalogRegions.length > 0 && (
              <div className="flex flex-wrap gap-1">
                {koyebCatalogRegions.slice(0, 12).map((region) => {
                  const id = region.id || region.name || '';
                  return (
                    <Button key={id} size="xs" variant={koyebCreateForm.regions.split(',').map((s) => s.trim()).includes(id) ? 'primary' : 'secondary'} onClick={() => {
                      const current = koyebCreateForm.regions.split(',').map((s) => s.trim()).filter(Boolean);
                      const next = current.includes(id) ? current.filter((r) => r !== id) : [...current, id];
                      setKoyebCreateForm((f) => ({ ...f, regions: next.join(',') }));
                    }} className="text-[10px]">{region.name || id}</Button>
                  );
                })}
              </div>
            )}
            <div className="space-y-1">
              <label className="text-[11px] font-bold text-kumo-subtle">环境变量（每行 K=V）</label>
              <Textarea size="sm" aria-label="环境变量" value={koyebCreateForm.env} onChange={(e) => setKoyebCreateForm((f) => ({ ...f, env: e.target.value }))} placeholder={'DB_HOST=db.internal\nPORT=3000'} className="min-h-24 w-full text-xs font-mono" />
            </div>
            {koyebCreateError && (
              <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebCreateError}</div>
            )}
          </div>
          <div className="flex justify-end gap-2 pt-4">
            <Dialog.Close render={(props) => <Button size="sm" {...props} variant="secondary" className="text-xs">取消</Button>} />
            <Button size="sm" onClick={createKoyebService} disabled={koyebCreateSaving} className="text-xs">
              {koyebCreateSaving ? '创建中...' : '创建并部署'}
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* Koyeb 用量明细 Dialog */}
      <Dialog.Root open={!!koyebUsageTarget} onOpenChange={(open) => { if (!open) setKoyebUsageTarget(null); }}>
        <Dialog className="flex h-[70vh] !w-[min(52rem,calc(100vw-2rem))] !max-w-[min(52rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          <div className="flex items-center justify-between gap-2 border-b border-kumo-line p-4">
            <div>
              <Dialog.Title className="text-sm font-bold text-kumo-strong">用量明细</Dialog.Title>
              {koyebUsageTarget && <p className="text-[10px] text-kumo-subtle mt-0.5">{koyebUsageTarget.name}</p>}
            </div>
            <div className="flex items-center gap-2">
              {koyebUsageTarget && (
                <Button size="sm" variant="secondary" onClick={() => openKoyebUsage(koyebUsageTarget)} icon={<RefreshCw className="h-3.5 w-3.5" />}>刷新</Button>
              )}
              <Button shape="square" size="sm" variant="ghost" aria-label="关闭" onClick={() => setKoyebUsageTarget(null)} icon={<X className="h-4 w-4" />} />
            </div>
          </div>
          <div className="flex-1 overflow-auto p-4">
            {koyebUsageLoading ? (
              <div className="flex h-full items-center justify-center gap-2 text-kumo-subtle"><Loader size={16} />正在加载用量...</div>
            ) : koyebUsageError ? (
              <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">{koyebUsageError}</div>
            ) : !koyebUsageData ? (
              <div className="py-12 text-center text-kumo-subtle text-sm">暂无用量数据</div>
            ) : (
              <pre className="whitespace-pre-wrap break-all rounded-md bg-kumo-recessed/40 p-4 text-xs text-kumo-strong">{JSON.stringify(koyebUsageData, null, 2)}</pre>
            )}
          </div>
        </Dialog>
      </Dialog.Root>

      {/* Global Self-Contained Log Viewer Dialog */}
      <Dialog.Root open={logViewerOpen} onOpenChange={(open) => { setLogViewerOpen(open); if (!open) stopLogTail(); }}>
        <Dialog className="@container flex h-[80vh] !w-[min(56rem,calc(100vw-2rem))] !max-w-[min(56rem,calc(100vw-2rem))] flex-col overflow-hidden p-0">
          {/* Header */}
          <div className="p-4 border-b border-kumo-line bg-kumo-recessed/40 flex justify-between items-center">
            <div>
              <Dialog.Title className="text-sm font-bold text-kumo-strong">{logTitle}</Dialog.Title>
              {logSubtitle && <p className="text-[10px] text-kumo-subtle mt-0.5">{logSubtitle}</p>}
            </div>
            <Button
              shape="square" size="sm"
              variant="ghost"
              aria-label="关闭日志查看器"
              onClick={() => setLogViewerOpen(false)}
              className="text-kumo-subtle hover:text-kumo-default"
            >
              <X className="w-4 h-4" />
            </Button>
          </div>

          {/* Log Controls */}
          <div className="p-3 border-b border-kumo-line flex flex-col cq-sm:flex-row justify-between items-start cq-sm:items-center gap-3 text-xs bg-kumo-base">
            <div className="flex items-center gap-2 w-full cq-sm:w-auto">
              <ResponsiveSearchInput
                value={logFilterText}
                onChange={(e) => setLogFilterText(e.target.value)}
                placeholder="搜索日志消息..."
                ariaLabel="搜索日志消息"
                className="cq-sm:w-48"
              />
            </div>
            <div className="flex flex-wrap items-center gap-2 text-[10px]">
              <Select
                aria-label="日志级别筛选" size="sm"
                value={logLevelFilter}
                onValueChange={(value) => setLogLevelFilter(String(value))}
                className="px-2 py-1 text-kumo-strong font-semibold"
                items={[
                  { value: 'ALL', label: '全部级别' },
                  { value: 'INFO', label: 'INFO' },
                  { value: 'WARN', label: 'WARN' },
                  { value: 'ERROR', label: 'ERROR' },
                  { value: 'DEBUG', label: 'DEBUG' },
                ]}
              />

              <Checkbox
                checked={logWrapText}
                onCheckedChange={(checked) => setLogWrapText(checked)}
                label="自动换行"
              />

              <Checkbox
                checked={logAutoScroll}
                onCheckedChange={(checked) => setLogAutoScroll(checked)}
                label="滚动到底部"
              />

              {logTailActive && (
                <>
                  <Badge variant={logTailConnected ? 'success' : 'warning'} appearance="dot">
                    {logTailConnected ? '已连接' : '已断开'}
                  </Badge>
                  <Button size="sm" variant="secondary-destructive" onClick={stopLogTail} className="text-kumo-danger font-semibold">
                    停止跟随
                  </Button>
                </>
              )}

              <Button size="sm"
                variant="secondary-destructive"
                onClick={() => setLogs([])}
                className="text-kumo-danger font-semibold"
              >
                清空
              </Button>

              <Button size="sm"
                variant="primary"
                onClick={downloadLogs}
                className="text-kumo-inverse font-semibold flex items-center gap-1"
              >
                <Download className="w-3 h-3" />
                <span>下载日志</span>
              </Button>
            </div>
          </div>

          {/* Logs Terminal Area */}
          <div
            ref={logContainerRef}
            id="log-viewer-container"
            className="flex-1 bg-kumo-control text-kumo-strong font-mono text-xs p-4 overflow-y-auto leading-relaxed select-text"
          >
            {logLoading ? (
              <div className="h-full flex items-center justify-center text-kumo-subtle gap-2">
                <Loader size={16} />
                <span>正在获取日志流中...</span>
              </div>
            ) : filteredLogs.length === 0 ? (
              <div className="text-center text-kumo-subtle py-12">暂无匹配日志记录</div>
            ) : (
              <div className="space-y-1">
                {filteredLogs.map((log) => {
                  let levelColor = 'text-kumo-info';
                  if (log.level === 'WARN') levelColor = 'text-kumo-warning';
                  if (log.level === 'ERROR' || log.level === 'FATAL') levelColor = 'text-kumo-danger';
                  if (log.level === 'DEBUG') levelColor = 'text-kumo-success';

                  return (
                    <div
                      key={log.id}
                      className={`${logWrapText ? 'break-all whitespace-pre-wrap' : 'whitespace-nowrap'} hover:bg-kumo-base/40 py-0.5`}
                    >
                      <span className="text-kumo-subtle mr-2">[{new Date(log.timestamp).toLocaleTimeString()}]</span>
                      <span className={`${levelColor} font-bold mr-2`}>[{log.level}]</span>
                      <span>{log.message}</span>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default PaasPage;
