import React, { useState, useEffect, useRef, useMemo, useCallback } from 'react';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Table } from '@cloudflare/kumo/components/table';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Tabs } from '@cloudflare/kumo';
import { AnimatedCollapse } from '../components/AnimatedCollapse.jsx';
import useStore from '../store.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { getStatusPillClass } from '../components/ui/AppPrimitives.jsx';
import {
  Server,
  Users,
  Plus,
  Trash,
  Search,
  Upload,
  Download,
  X,
  ChevronDown,
  ChevronUp,
  RefreshCw,
  History,
  Activity,
  Check,
  Settings,
  Mail,
  Play,
  Pause,
  Folder,
  FileText,
  Save,
  Globe,
  Terminal,
  Database,
  Rocket
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

function PaasPage() {
  const { theme } = useStore();
  const [activeTab, setActiveTab] = useState('koyeb'); // 'koyeb' | 'fly' | 'accounts' | 'settings'
  const didInitialLoadRef = useRef(false);

  // Global Auth Header
  const getAuthHeaders = useCallback(() => {
    const password = localStorage.getItem('admin_password') || '';
    return {
      'Content-Type': 'application/json',
      'x-admin-password': password,
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
  const [koyebAutoPaused, setKoyebAutoPaused] = useState(false);
  const [flyAutoPaused, setFlyAutoPaused] = useState(false);

  const [koyebCountdown, setKoyebCountdown] = useState(30);
  const [flyCountdown, setFlyCountdown] = useState(30);
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
            setKoyebCountdown(sec);
          }
          if (settings.flyRefreshInterval) {
            const sec = normalizeStoredRefreshIntervalSec(settings.flyRefreshInterval);
            setFlyIntervalSec(sec);
            setFlyCountdown(sec);
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
        setKoyebCountdown(koyebSec);
        setFlyCountdown(flySec);
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
  const logContainerRef = useRef(null);

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
      toast.info('暂无日志可以下载');
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
      setKoyebCountdown(koyebIntervalSecRef.current);
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
    if (!(await dialog.confirm(`确认重新部署 Koyeb 服务 "${service.name}" 吗？`))) return;
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
      setFlyCountdown(flyIntervalSecRef.current);
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
    if (!(await dialog.confirm(`确认重启 Fly.io 应用 "${app.name}" 吗？（触发一次重新部署）`))) return;
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

    let suggestedImage = currentImage;
    if (currentImage && currentImage.includes(':')) {
      suggestedImage = currentImage.split(':')[0] + ':latest';
    } else if (currentImage) {
      suggestedImage = currentImage + ':latest';
    }

    const newImage = await dialog.prompt({
      message: currentImage
        ? `当前正在运行：\n${currentImage}\n\n请输入新容器镜像：`
        : '未能检测到当前运行的镜像。请输入完整的容器镜像地址：',
      defaultValue: suggestedImage || currentImage,
    });

    if (newImage === null || newImage.trim() === '') return;

    toast.info('正在更新容器镜像，请稍候...');
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
        toast.success(`容器镜像已成功提交更新！成功: ${result.updated}, 失败: ${result.failed}`);
        loadFlyData(false);
      } else {
        toast.error('更新失败: ' + result.error);
      }
    } catch (error) {
      toast.error('更新异常: ' + error.message);
    }
  };

  const updateAllFlyAppsImage = async (account) => {
    if (!(await dialog.confirm(`确定要为 Fly.io 账号 "${account.name}" 下的所有应用批量更新最新镜像吗？`))) return;
    toast.info('正在提交批量更新，请稍候...');
    try {
      const response = await fetch(`/api/flyio/accounts/${account.id}/update-all-images`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ image: 'latest' }),
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
          throw new Error('加载日志失败: ' + e.message);
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
    if (!(await dialog.confirm(`确定要永久删除 Fly.io 应用 "${app.name}" 吗？此操作会销毁所有底层的机器并且不可逆！`))) return;
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

  // Batch states
  const [koyebBatchText, setKoyebBatchText] = useState('');
  const [koyebBatchError, setKoyebBatchError] = useState('');
  const [koyebBatchSuccess, setKoyebBatchSuccess] = useState('');

  const [flyBatchText, setFlyBatchText] = useState('');
  const [flyBatchError, setFlyBatchError] = useState('');
  const [flyBatchSuccess, setFlyBatchSuccess] = useState('');

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
    if (!(await dialog.confirm('确认删除此 Koyeb 账号吗？'))) return;
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

  const batchAddKoyebAccounts = async () => {
    if (!koyebBatchText.trim()) return;
    setKoyebAddingAccount(true);
    setKoyebBatchError('');
    setKoyebBatchSuccess('');

    const lines = koyebBatchText.trim().split('\n');
    let successCount = 0;
    let failCount = 0;
    const errors = [];

    for (const line of lines) {
      if (!line.trim()) continue;
      let name, token;
      if (line.includes(':') || line.includes('：')) {
        const parts = line.split(/[:：]/);
        name = parts[0].trim();
        token = parts.slice(1).join(':').trim();
      } else if (line.includes(',') || line.includes('，')) {
        const parts = line.split(/[,，]/);
        name = parts[0].trim();
        token = parts.slice(1).join(',').trim();
      }

      if (!name || !token) {
        failCount++;
        errors.push(`格式错误: ${line}`);
        continue;
      }

      token = token.replace(/[^\x21-\x7E]/g, ''); // Clear whitespace/hidden

      try {
        const response = await fetch('/api/koyeb/accounts', {
          method: 'POST',
          headers: {
            ...getAuthHeaders(),
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ name, token }),
        });
        const result = await response.json();
        if (result.success) {
          successCount++;
        } else {
          failCount++;
          errors.push(`${name}: ${result.error}`);
        }
      } catch (err) {
        failCount++;
        errors.push(`${name}: ${err.message}`);
      }
    }

    setKoyebAddingAccount(false);
    if (successCount > 0) {
      setKoyebBatchSuccess(`成功批量添加 ${successCount} 个账号`);
      setKoyebBatchText('');
      loadKoyebManagedAccounts();
      loadKoyebData(false);
    }
    if (failCount > 0) {
      setKoyebBatchError(`${failCount} 个账号添加失败:\n${errors.join('\n')}`);
    }
  };

  const exportKoyebAccounts = async () => {
    try {
      const response = await fetch('/api/koyeb/accounts/export', {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (result.success) {
        const accounts = result.accounts || [];
        if (accounts.length === 0) {
          toast.warning('没有可导出的账号');
          return;
        }
        const text = accounts.map(a => `${a.name}:${a.token}`).join('\n');
        const blob = new Blob([text], { type: 'text/plain' });
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = `koyeb_accounts_${new Date().toISOString().slice(0, 10)}.txt`;
        document.body.appendChild(link);
        link.click();
        document.body.removeChild(link);
        URL.revokeObjectURL(url);
        toast.success(`成功导出 ${accounts.length} 个账号`);
      } else {
        toast.error('导出失败: ' + result.error);
      }
    } catch (e) {
      toast.error('导出异常: ' + e.message);
    }
  };

  const importKoyebAccounts = () => {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.txt';
    input.onchange = async (e) => {
      const file = e.target.files[0];
      if (!file) return;
      try {
        const text = await file.text();
        setKoyebBatchText(text);
        toast.info('账号已载入批量输入框，请核对后点击批量验证按钮');
      } catch (err) {
        toast.error('加载文件失败: ' + err.message);
      }
    };
    input.click();
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
    if (!(await dialog.confirm(`确认删除 Fly.io 账号 "${name}" 吗？`))) return;
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

  const batchAddFlyAccounts = async () => {
    if (!flyBatchText.trim()) return;
    setFlyAddingAccount(true);
    setFlyBatchError('');
    setFlyBatchSuccess('');

    const lines = flyBatchText.trim().split('\n');
    let successCount = 0;
    let failCount = 0;
    const errors = [];

    for (const line of lines) {
      if (!line.trim()) continue;
      let name, token;
      if (line.includes(':') || line.includes('：')) {
        const parts = line.split(/[:：]/);
        name = parts[0].trim();
        token = parts.slice(1).join(':').trim();
      } else if (line.includes(',') || line.includes('，')) {
        const parts = line.split(/[,，]/);
        name = parts[0].trim();
        token = parts.slice(1).join(',').trim();
      }

      if (!name || !token) {
        failCount++;
        errors.push(`格式错误: ${line}`);
        continue;
      }

      token = token.replace(/[^\x21-\x7E]/g, '');

      try {
        const response = await fetch('/api/flyio/accounts', {
          method: 'POST',
          headers: {
            ...getAuthHeaders(),
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ name, api_token: token }),
        });
        const result = await response.json();
        if (result.success) {
          successCount++;
        } else {
          failCount++;
          errors.push(`${name}: ${result.error}`);
        }
      } catch (err) {
        failCount++;
        errors.push(`${name}: ${err.message}`);
      }
    }

    setFlyAddingAccount(false);
    if (successCount > 0) {
      setFlyBatchSuccess(`成功批量添加 ${successCount} 个账号`);
      setFlyBatchText('');
      loadFlyManagedAccounts();
      loadFlyData(false);
    }
    if (failCount > 0) {
      setFlyBatchError(`${failCount} 个账号添加失败:\n${errors.join('\n')}`);
    }
  };

  const exportFlyAccounts = () => {
    if (flyManagedAccounts.length === 0) {
      toast.warning('没有可导出的账号');
      return;
    }
    const exportData = {
      version: '1.0',
      platform: 'fly',
      exportTime: new Date().toISOString(),
      accounts: flyManagedAccounts,
    };
    const blob = new Blob([JSON.stringify(exportData, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `fly-accounts-${Date.now()}.json`;
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
    URL.revokeObjectURL(url);
    toast.success('Fly.io 账号导出成功');
  };

  const importFlyAccounts = async () => {
    if (!(await dialog.confirm('导入账号将覆盖当前 Fly.io 账号配置，是否继续？'))) return;
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = '.json';
    input.onchange = async (e) => {
      const file = e.target.files[0];
      if (!file) return;
      try {
        const text = await file.text();
        const data = JSON.parse(text);
        if (!data.accounts || !Array.isArray(data.accounts)) {
          toast.error('无效的 Fly.io 备份文件');
          return;
        }

        setFlyAddingAccount(true);
        let count = 0;
        for (const acc of data.accounts) {
          const res = await fetch('/api/flyio/accounts', {
            method: 'POST',
            headers: {
              ...getAuthHeaders(),
              'Content-Type': 'application/json',
            },
            body: JSON.stringify({
              name: acc.name,
              api_token: acc.api_token || acc.token,
            }),
          });
          const r = await res.json();
          if (r.success) count++;
        }
        toast.success(`成功导入 ${count} 个 Fly.io 账号`);
        loadFlyManagedAccounts();
        loadFlyData(false);
      } catch (err) {
        toast.error('导入失败: ' + err.message);
      } finally {
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
    if (activeTab !== 'koyeb' || koyebAutoPaused) return;
    const timer = setInterval(() => {
      setKoyebCountdown(prev => {
        if (prev <= 1) {
          loadKoyebData(false);
          return koyebIntervalSec;
        }
        return prev - 1;
      });
    }, 1000);
    return () => clearInterval(timer);
  }, [activeTab, koyebAutoPaused, koyebIntervalSec, loadKoyebData]);

  // Fly Timer
  useEffect(() => {
    if (activeTab !== 'fly' || flyAutoPaused) return;
    const timer = setInterval(() => {
      setFlyCountdown(prev => {
        if (prev <= 1) {
          loadFlyData(false);
          return flyIntervalSec;
        }
        return prev - 1;
      });
    }, 1000);
    return () => clearInterval(timer);
  }, [activeTab, flyAutoPaused, flyIntervalSec, loadFlyData]);

  return (
    <div className="flex w-full min-w-0 flex-col gap-3 sm:gap-4">
      {/* Tab bar header */}
      {/* <div className="flex flex-col md:flex-row justify-between items-start md:items-center gap-3 border-b border-kumo-line pb-3">
        <div>
          <h1 className="text-xl font-bold text-kumo-strong">PaaS 平台管理</h1>
        </div>
        <div className="flex gap-2">
          {activeTab === 'koyeb' && (
            <div className="flex items-center gap-2 text-xs app-subcard bg-kumo-recessed rounded px-3 py-1.5 font-semibold text-kumo-strong">
              <RefreshCw className={`w-3.5 h-3.5 ${koyebRefreshing ? 'animate-spin' : ''}`} />
              <span>{koyebCountdown} 秒后刷新</span>
              <Button
                shape="square" size="sm"
                variant="ghost"
                aria-label={koyebAutoPaused ? '恢复 Koyeb 自动刷新' : '暂停 Koyeb 自动刷新'}
                onClick={() => setKoyebAutoPaused(!koyebAutoPaused)}
                className="hover:text-kumo-brand"
              >
                {koyebAutoPaused ? <Play className="w-3.5 h-3.5" /> : <Pause className="w-3.5 h-3.5" />}
              </Button>
            </div>
          )}
          {activeTab === 'fly' && (
            <div className="flex items-center gap-2 text-xs app-subcard bg-kumo-recessed rounded px-3 py-1.5 font-semibold text-kumo-strong">
              <RefreshCw className={`w-3.5 h-3.5 ${flyRefreshing ? 'animate-spin' : ''}`} />
              <span>{flyCountdown} 秒后刷新</span>
              <Button
                shape="square" size="sm"
                variant="ghost"
                aria-label={flyAutoPaused ? '恢复 Fly 自动刷新' : '暂停 Fly 自动刷新'}
                onClick={() => setFlyAutoPaused(!flyAutoPaused)}
                className="hover:text-kumo-brand"
              >
                {flyAutoPaused ? <Play className="w-3.5 h-3.5" /> : <Pause className="w-3.5 h-3.5" />}
              </Button>
            </div>
          )}
        </div>
      </div> */}

      {/* Main Tabs Navigation */}
      <Tabs
        {...MODULE_TABS_PROPS}
        value={activeTab}
        onValueChange={setActiveTab}
        tabs={[
          { value: 'koyeb', label: <span className="inline-flex items-center gap-1.5"><Database className="w-4 h-4 text-kumo-info" />Koyeb</span> },
          { value: 'fly', label: <span className="inline-flex items-center gap-1.5"><Rocket className="w-4 h-4 text-kumo-brand" />Fly.io</span> },
          { value: 'accounts', label: <span className="inline-flex items-center gap-1.5"><Users className="w-4 h-4 text-kumo-success" />账号管理</span> },
          { value: 'settings', label: <span className="inline-flex items-center gap-1.5"><Settings className="w-4 h-4" />模块配置</span> },
        ]}
      />

      {/* ==================== Koyeb Tab Content ==================== */}
      {activeTab === 'koyeb' && (
        <div className="space-y-3">
          <div className="bg-kumo-info/10 border border-kumo-info/20 text-kumo-default px-3 py-2.5 rounded-md flex flex-wrap items-center justify-between gap-2">
            <div className="text-xs leading-tight">
              <strong>Koyeb 监控自动拉取中</strong>
              <span className="ml-2 text-kumo-subtle">每 {koyebIntervalSec} 秒同步全部账号状态</span>
            </div>
            <Button size="sm" onClick={() => loadKoyebData(true)} disabled={koyebRefreshing} className="text-xs font-semibold flex items-center gap-1.5">
              <RefreshCw className={`w-3.5 h-3.5 ${koyebRefreshing ? 'animate-spin' : ''}`} />
              <span>立即刷新</span>
            </Button>
          </div>

          {koyebLoading && koyebAccounts.length === 0 ? (
            <div className="text-center py-12 text-kumo-subtle text-xs flex flex-col items-center justify-center gap-2">
              <RefreshCw className="w-8 h-8 animate-spin text-kumo-brand" />
              <span>正在加载 Koyeb 数据快照...</span>
            </div>
          ) : koyebAccounts.length === 0 ? (
            <div className="app-card p-12 text-center text-kumo-subtle text-xs">
              暂无配置 Koyeb 账号。请前往“账号管理”添加账号 API 令牌。
            </div>
          ) : (
            <div className="space-y-3">
              {koyebAccounts.map((account) => {
                const expanded = isKoyebAccountExpanded(account.name);
                return (
                  <div key={account.name} className="app-card overflow-hidden">
                    {/* Header */}
                    <div
                      className="px-3 py-2.5 flex justify-between items-center bg-kumo-recessed/50 cursor-pointer border-b border-kumo-line"
                      onClick={() => toggleKoyebAccount(account.name)}
                    >
                      <div className="flex items-center gap-3">
                        <ChevronDown className={`w-4 h-4 text-kumo-subtle transition-transform duration-200 ${expanded ? 'rotate-180' : ''}`} />
                        <div className="w-8 h-8 rounded-full bg-kumo-info flex items-center justify-center text-kumo-inverse font-bold text-sm">
                          {account.name.charAt(0).toUpperCase()}
                        </div>
                        <div>
                          <div className="text-sm font-bold text-kumo-strong">{account.name}</div>
                          {account.data?.email && (
                            <div className="text-xs text-kumo-subtle flex items-center gap-1">
                              <Mail className="w-3.5 h-3.5" />
                              <span>{account.data.email}</span>
                            </div>
                          )}
                        </div>
                      </div>
                      <div className="text-xs font-mono bg-kumo-info/10 text-kumo-info px-2.5 py-1 rounded border border-kumo-info/20">
                        Balance: ${(account.data?.balance || 0).toFixed(2)}
                      </div>
                    </div>

                    {/* Body */}
                    <AnimatedCollapse open={expanded}>
                      <div className="p-2.5">
                        {account.error ? (
                          <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">
                            出错了: {account.error}
                          </div>
                        ) : !account.projects || account.projects.length === 0 ? (
                          <div className="text-center py-6 text-xs text-kumo-subtle">暂无应用服务</div>
                        ) : (
                          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                            {account.projects.map((app) => (
                              <div key={app._id} className="app-subcard bg-kumo-recessed/30 p-4 space-y-4">
                                {/* App Name Editor */}
                                <div className="flex justify-between items-center border-b border-kumo-line pb-2">
                                  <div className="flex items-center gap-1.5 flex-1 min-w-0">
                                    <span className="text-base">📦</span>
                                    {app.isEditing ? (
                                      <Input size="sm"
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
                                        className="text-kumo-strong text-xs font-bold px-2 py-0.5 w-full"
                                        autoFocus
                                      />
                                    ) : (
                                      <span
                                        onDoubleClick={() => startEditKoyebAppName(app)}
                                        className="text-xs font-bold text-kumo-strong truncate cursor-pointer hover:text-kumo-brand"
                                        title="双击重命名应用"
                                      >
                                        {app.name}
                                      </span>
                                    )}
                                  </div>
                                </div>

                                {/* Services */}
                                <div className="space-y-3">
                                  {(app.services || []).map((service) => (
                                    <div key={service._id} className="app-card p-3 space-y-2">
                                      <div className="flex justify-between items-start">
                                        <div className="flex-1 min-w-0">
                                          {service.isEditing ? (
                                            <Input size="sm"
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
                                              className="text-kumo-strong text-xs px-2 py-0.5 w-full"
                                              autoFocus
                                            />
                                          ) : (
                                            <div
                                              onDoubleClick={() => startEditKoyebServiceName(service)}
                                              className="text-xs font-semibold text-kumo-strong truncate cursor-pointer hover:text-kumo-brand"
                                              title="双击重命名服务"
                                            >
                                              {service.name}
                                            </div>
                                          )}
                                        </div>
                                        <span className={`app-status-pill ${getKoyebStatusBadge(service.status)}`}>
                                          {getKoyebStatusText(service.status)}
                                        </span>
                                      </div>

                                      {/* Actions */}
                                      <div className="flex justify-end gap-1.5 pt-1">
                                        <Button
                                          shape="square" size="sm"
                                          variant="ghost"
                                          aria-label="重启服务"
                                          onClick={() => restartKoyebService(account, app, service)}
                                          title="重启服务"
                                          className="text-kumo-subtle hover:text-kumo-brand"
                                        >
                                          <RefreshCw className="w-3.5 h-3.5" />
                                        </Button>
                                        <Button
                                          shape="square" size="sm"
                                          variant="ghost"
                                          aria-label="重新部署服务"
                                          onClick={() => redeployKoyebService(account, app, service)}
                                          title="重新部署"
                                          className="text-kumo-subtle hover:text-kumo-info"
                                        >
                                          <Rocket className="w-3.5 h-3.5" />
                                        </Button>
                                        <Button
                                          shape="square" size="sm"
                                          variant="ghost"
                                          aria-label="查看服务实例"
                                          onClick={() => fetchKoyebServiceInstances(account, service)}
                                          title="查看实例"
                                          className="text-kumo-subtle hover:text-kumo-brand"
                                        >
                                          <Server className={`w-3.5 h-3.5 ${service.loadingInstances ? 'animate-spin' : ''}`} />
                                        </Button>
                                        <Button
                                          shape="square" size="sm"
                                          variant="ghost"
                                          aria-label="查看服务日志"
                                          onClick={() => showKoyebServiceLogs(account, app, service)}
                                          title="查看日志"
                                          className="text-kumo-subtle hover:text-kumo-success"
                                        >
                                          <FileText className="w-3.5 h-3.5" />
                                        </Button>
                                      </div>

                                      {/* Service Instances */}
                                      <AnimatedCollapse open={Boolean(service.showInstances && service.instances)}>
                                        {service.instances ? (
                                        <div className="mt-2 border-t border-kumo-line pt-2 text-[10px] space-y-1 bg-kumo-recessed/30 p-2 rounded">
                                          <div className="font-bold text-kumo-subtle flex justify-between">
                                            <span>实例列表 ({service.instances.length})</span>
                                            <Button size="sm" variant="ghost" onClick={() => {
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
                                            }} className="text-kumo-danger hover:underline">关闭</Button>
                                          </div>
                                          {service.instances.map(inst => (
                                            <div key={inst.id} className="flex justify-between items-center py-0.5 border-b border-kumo-line last:border-0">
                                              <span className="font-mono text-kumo-subtle">{inst.id?.substring(0, 8)}...</span>
                                              <div className="flex gap-2">
                                                <span className="text-kumo-strong font-semibold">{inst.region?.toUpperCase()}</span>
                                                <span className={`px-1.5 rounded text-[9px] font-bold ${getKoyebStatusBadge(inst.status)}`}>{inst.status}</span>
                                              </div>
                                            </div>
                                          ))}
                                        </div>
                                        ) : null}
                                      </AnimatedCollapse>
                                    </div>
                                  ))}
                                </div>
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    </AnimatedCollapse>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* ==================== Fly.io Tab Content ==================== */}
      {activeTab === 'fly' && (
        <div className="space-y-3">
          <div className="bg-kumo-info/10 border border-kumo-info/20 text-kumo-default px-3 py-2.5 rounded-md flex flex-wrap items-center justify-between gap-2">
            <div className="text-xs leading-tight">
              <strong>Fly.io 监控自动拉取中</strong>
              <span className="ml-2 text-kumo-subtle">每 {flyIntervalSec} 秒同步全部账号状态</span>
            </div>
            <Button size="sm" onClick={() => loadFlyData(true)} disabled={flyRefreshing} className="text-xs font-semibold flex items-center gap-1.5">
              <RefreshCw className={`w-3.5 h-3.5 ${flyRefreshing ? 'animate-spin' : ''}`} />
              <span>立即刷新</span>
            </Button>
          </div>

          {flyLoading && flyAccounts.length === 0 ? (
            <div className="text-center py-12 text-kumo-subtle text-xs flex flex-col items-center justify-center gap-2">
              <RefreshCw className="w-8 h-8 animate-spin text-kumo-brand" />
              <span>正在加载 Fly.io 数据快照...</span>
            </div>
          ) : flyAccounts.length === 0 ? (
            <div className="app-card p-12 text-center text-kumo-subtle text-xs">
              暂无配置 Fly.io 账号。请前往“账号管理”添加账号 API 令牌。
            </div>
          ) : (
            <div className="space-y-3">
              {flyAccounts.map((account) => {
                const expanded = isFlyAccountExpanded(account.name);
                return (
                  <div key={account.name} className="app-card overflow-hidden">
                    {/* Header */}
                    <div
                      className="px-3 py-2.5 flex justify-between items-center bg-kumo-recessed/50 cursor-pointer border-b border-kumo-line"
                      onClick={() => toggleFlyAccount(account.name)}
                    >
                      <div className="flex items-center gap-3">
                        <ChevronDown className={`w-4 h-4 text-kumo-subtle transition-transform duration-200 ${expanded ? 'rotate-180' : ''}`} />
                        <div className="w-8 h-8 rounded-full bg-kumo-brand flex items-center justify-center text-kumo-inverse font-bold text-sm">
                          {(account.name || 'F').charAt(0).toUpperCase()}
                        </div>
                        <div>
                          <div className="text-sm font-bold text-kumo-strong">{account.name}</div>
                        </div>
                      </div>
                      <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
                        <Button size="sm"
                          variant="secondary"
                          onClick={() => updateAllFlyAppsImage(account)}
                          className="text-[10px] font-semibold text-kumo-info flex items-center gap-1"
                        >
                          批量更新
                        </Button>
                        <Button size="sm"
                          variant="secondary"
                          onClick={() => createFlyApp(account)}
                          className="text-[10px] font-semibold text-kumo-brand flex items-center gap-1"
                        >
                          <Plus className="w-3 h-3" />
                          <span>新建应用</span>
                        </Button>
                      </div>
                    </div>

                    {/* Body */}
                    <AnimatedCollapse open={expanded}>
                      <div className="p-3">
                        {account.error ? (
                          <div className="text-xs text-kumo-danger p-2 bg-kumo-danger/10 border border-kumo-danger/20 rounded">
                            出错了: {account.error}
                          </div>
                        ) : !account.projects || account.projects.length === 0 ? (
                          <div className="text-center py-6 text-xs text-kumo-subtle">暂无配置应用</div>
                        ) : (
                          <div className="grid grid-cols-1 lg:grid-cols-2 2xl:grid-cols-3 gap-2.5 items-start">
                            {account.projects.map((app) => (
                              <div key={app.id} className="app-subcard bg-kumo-recessed/30 p-2.5 space-y-2">
                                {/* App Header */}
                                <div className="flex justify-between items-center border-b border-kumo-line pb-1.5">
                                  <div className="flex items-center gap-1.5 flex-1 min-w-0">
                                    <Rocket className="h-3.5 w-3.5 shrink-0 text-kumo-brand" />
                                    {app.isEditing ? (
                                      <Input size="sm"
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
                                        className="text-kumo-strong text-xs px-2 py-0.5 w-full"
                                        autoFocus
                                      />
                                    ) : (
                                      <span
                                        onDoubleClick={() => startEditFlyAppName(app)}
                                        className="text-xs font-bold text-kumo-strong truncate cursor-pointer hover:text-kumo-brand"
                                        title="双击重命名"
                                      >
                                        {app.name}
                                      </span>
                                    )}
                                  </div>
                                  <Button
                                    shape="square" size="sm"
                                    variant="ghost"
                                    aria-label="删除 Fly 应用"
                                    onClick={() => deleteFlyApp(account, app)}
                                    className="text-kumo-subtle hover:text-kumo-danger"
                                  >
                                    <Trash className="w-3.5 h-3.5" />
                                  </Button>
                                </div>

                                {/* Main application service card */}
                                <div className="app-card app-card-md px-2.5 py-2">
                                  <div className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2">
                                    <div className="flex min-w-0 items-center gap-2">
                                      <span className="text-[10px] font-semibold text-kumo-subtle font-mono truncate">
                                        {app.hostname || app.name}
                                      </span>
                                      <span className={`shrink-0 app-status-pill ${getFlyStatusBadge(app.status)}`}>
                                        {getFlyStatusText(app.status)}
                                      </span>
                                    </div>

                                    {/* App actions */}
                                    <div className="flex justify-end gap-1">
                                    <Button
                                      shape="square" size="sm"
                                      variant="ghost"
                                      aria-label="重启应用"
                                      onClick={() => redeployFlyApp(account, app)}
                                      title="重启应用"
                                      className="text-kumo-subtle hover:text-kumo-brand"
                                    >
                                      <RefreshCw className="w-3.5 h-3.5" />
                                    </Button>
                                    <Button
                                      shape="square" size="sm"
                                      variant="ghost"
                                      aria-label="更新容器镜像"
                                      onClick={() => updateFlyAppImage(account, app)}
                                      title="更新容器镜像"
                                      className="text-kumo-subtle hover:text-kumo-brand"
                                    >
                                      <Rocket className="w-3.5 h-3.5" />
                                    </Button>
                                    <Button
                                      shape="square" size="sm"
                                      variant="ghost"
                                      aria-label="查看机器实例"
                                      onClick={() => fetchFlyMachines(account, app)}
                                      title="查看机器/实例"
                                      className="text-kumo-subtle hover:text-kumo-info"
                                    >
                                      <Server className={`w-3.5 h-3.5 ${app.loadingMachines ? 'animate-spin' : ''}`} />
                                    </Button>
                                    <Button
                                      shape="square" size="sm"
                                      variant="ghost"
                                      aria-label="查看运行日志"
                                      onClick={() => showFlyAppLogs(account, app)}
                                      title="查看运行日志"
                                      className="text-kumo-subtle hover:text-kumo-success"
                                    >
                                      <FileText className="w-3.5 h-3.5" />
                                    </Button>
                                    <Button
                                      shape="square" size="sm"
                                      variant="ghost"
                                      aria-label="查看应用配置"
                                      onClick={() => viewFlyConfig(account, app)}
                                      title="查看应用配置"
                                      className="text-kumo-subtle hover:text-kumo-strong"
                                    >
                                      <Terminal className="w-3.5 h-3.5" />
                                    </Button>
                                    </div>
                                  </div>

                                  {/* Machines container */}
                                  <AnimatedCollapse open={Boolean(app.showMachines && app.machines)}>
                                    {app.machines ? (
                                    <div className="mt-1.5 border-t border-kumo-line bg-kumo-recessed/30 px-2 py-1.5 text-[10px] space-y-1 rounded">
                                      <div className="font-bold text-kumo-subtle flex justify-between">
                                        <span>运 (Machines - {app.machines.length})</span>
                                        <Button size="sm" variant="ghost" onClick={() => {
                                          setFlyAccounts(prev => prev.map(acc => ({
                                            ...acc,
                                            projects: acc.projects.map(p => {
                                              if (p.id !== app.id) return p;
                                              return { ...p, showMachines: false };
                                            })
                                          })));
                                        }} className="text-kumo-danger hover:underline">关闭</Button>
                                      </div>
                                      {app.machines.map(m => (
                                        <div key={m.id} className="flex justify-between items-center py-0.5 border-b border-kumo-line last:border-0">
                                          <span className="font-mono text-kumo-subtle">{m.id?.substring(0, 8)}...</span>
                                          <div className="flex gap-2">
                                            <span className="text-kumo-strong font-semibold">{formatRegion(m.region)}</span>
                                            <span className={`px-1.5 rounded text-[9px] font-bold ${getFlyStatusBadge(m.state)}`}>{m.state}</span>
                                          </div>
                                        </div>
                                      ))}
                                    </div>
                                    ) : null}
                                  </AnimatedCollapse>
                                </div>

                                {/* Domains List */}
                                {(app.hostname || (app.domains && app.domains.length > 0)) && (
                                  <div className="mt-1.5 space-y-1.5">
                                    <div className="text-[10px] font-bold text-kumo-subtle">域名绑定</div>
                                    <div className="flex flex-wrap gap-1.5 text-xs">
                                      {app.hostname && (
                                        <a
                                          href={`https://${app.hostname}`}
                                          target="_blank"
                                          rel="noreferrer"
                                          className="px-2 py-0.5 bg-kumo-recessed hover:bg-kumo-recessed/80 border border-kumo-line rounded text-[10px] text-kumo-brand font-semibold flex items-center gap-1"
                                        >
                                          <Globe className="w-3 h-3" />
                                          <span>{app.hostname}</span>
                                          <span className="text-[8px] px-1 bg-kumo-subtle/20 text-kumo-strong rounded">默认</span>
                                        </a>
                                      )}
                                      {app.domains && app.domains.map((dom) => (
                                        <a
                                          key={dom.domain}
                                          href={`https://${dom.domain}`}
                                          target="_blank"
                                          rel="noreferrer"
                                          className="px-2 py-0.5 bg-kumo-recessed hover:bg-kumo-recessed/80 border border-kumo-line rounded text-[10px] text-kumo-strong font-semibold flex items-center gap-1"
                                        >
                                          <Globe className="w-3 h-3" />
                                          <span>{dom.domain}</span>
                                          <span className={`text-[8px] px-1 rounded font-bold ${dom.isVerified ? 'bg-kumo-success/10 text-kumo-success' : 'bg-kumo-warning/10 text-kumo-warning'}`}>
                                            {dom.isVerified ? '已就绪' : '配置中'}
                                          </span>
                                        </a>
                                      ))}
                                    </div>
                                  </div>
                                )}
                              </div>
                            ))}
                          </div>
                        )}
                      </div>
                    </AnimatedCollapse>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* ==================== Accounts Management Tab Content ==================== */}
      {activeTab === 'accounts' && (
        <div className="space-y-6">
          {/* Koyeb Panel */}
          <div className="app-card p-4 space-y-4">
            <div className="flex justify-between items-center border-b border-kumo-line pb-3">
              <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
                <Database className="w-4 h-4 text-kumo-info" />
                <span>Koyeb 账号管理</span>
              </h3>
              <div className="flex gap-2">
                <Button size="sm" onClick={() => setShowAddKoyebModal(true)} className="text-xs flex items-center gap-1">
                  <Plus className="w-3.5 h-3.5" />
                  <span>添加账号</span>
                </Button>
                <Button size="sm" onClick={exportKoyebAccounts} className="text-xs flex items-center gap-1">
                  <Upload className="w-3.5 h-3.5" />
                  <span>导出</span>
                </Button>
                <Button size="sm" onClick={importKoyebAccounts} className="text-xs flex items-center gap-1">
                  <Download className="w-3.5 h-3.5" />
                  <span>导入</span>
                </Button>
              </div>
            </div>

            {/* Managed accounts table */}
            <div className="overflow-x-auto">
              <Table>
                <Table.Header>
                  <Table.Row className="border-b border-kumo-line text-kumo-subtle font-bold bg-kumo-recessed/30">
                    <Table.Head className="p-3">备注名称</Table.Head>
                    <Table.Head className="p-3">账号邮箱</Table.Head>
                    <Table.Head className="p-3 text-center">余额</Table.Head>
                    <Table.Head className="p-3 text-center">状态</Table.Head>
                    <Table.Head className="p-3 text-center w-20">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {koyebManagedAccounts.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={5} className="p-6 text-center text-kumo-subtle">暂无 Koyeb 账号</Table.Cell>
                    </Table.Row>
                  ) : (
                    koyebManagedAccounts.map((account) => (
                      <Table.Row key={account.id} className="border-b border-kumo-line last:border-0 hover:bg-kumo-recessed/10">
                        <Table.Cell className="p-3 font-semibold text-kumo-strong">{account.name}</Table.Cell>
                        <Table.Cell className="p-3 text-kumo-default font-mono">{account.email || '-'}</Table.Cell>
                        <Table.Cell className="p-3 text-center text-kumo-info font-bold">${(account.balance || 0).toFixed(2)}</Table.Cell>
                        <Table.Cell className="p-3 text-center">
                          <span className="px-2 py-0.5 rounded border bg-kumo-success/10 border-kumo-success text-kumo-success text-[10px] font-bold">
                            正常
                          </span>
                        </Table.Cell>
                        <Table.Cell className="p-3 text-center">
                          <Button
                            shape="square" size="sm"
                            variant="ghost"
                            aria-label="删除 Koyeb 账号"
                            onClick={() => removeKoyebAccount(account.id)}
                            className="text-kumo-danger"
                          >
                            <Trash className="w-4 h-4" />
                          </Button>
                        </Table.Cell>
                      </Table.Row>
                    ))
                  )}
                </Table.Body>
              </Table>
            </div>

            {/* Batch add Koyeb */}
            <div className="border border-kumo-line border-dashed rounded-lg p-4 bg-kumo-recessed/20 space-y-3">
              <div className="text-xs font-bold text-kumo-strong">
                批量添加 Koyeb 账号 (格式: 备注,koyeb_api_token)
              </div>
              <Textarea
                aria-label="批量添加 Koyeb 账号"
                value={koyebBatchText}
                onChange={(e) => setKoyebBatchText(e.target.value)}
                placeholder="我的Koyeb1,koyeb_xxxx&#10;我的Koyeb2,koyeb_yyyy"
                className="w-full min-h-[80px] text-xs font-mono text-kumo-strong p-2.5"
              />
              {koyebBatchError && (
                <pre className="text-[10px] text-kumo-danger bg-kumo-danger/10 border border-kumo-danger/20 rounded p-2 overflow-x-auto font-mono whitespace-pre-wrap">
                  {koyebBatchError}
                </pre>
              )}
              {koyebBatchSuccess && (
                <div className="text-xs text-kumo-success bg-kumo-success/10 border border-kumo-success/20 rounded p-2">
                  {koyebBatchSuccess}
                </div>
              )}
              <Button size="sm"
                onClick={batchAddKoyebAccounts}
                disabled={koyebAddingAccount || !koyebBatchText.trim()}
                className="w-full text-xs flex items-center justify-center gap-1.5"
              >
                <Database className="w-3.5 h-3.5" />
                <span>批量验证并添加 Koyeb 账号</span>
              </Button>
            </div>
          </div>

          {/* Fly.io Panel */}
          <div className="app-card p-4 space-y-4">
            <div className="flex justify-between items-center border-b border-kumo-line pb-3">
              <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
                <Rocket className="w-4 h-4 text-kumo-brand" />
                <span>Fly.io 账号管理</span>
              </h3>
              <div className="flex gap-2">
                <Button size="sm" onClick={() => setShowAddFlyModal(true)} className="text-xs flex items-center gap-1">
                  <Plus className="w-3.5 h-3.5" />
                  <span>添加账号</span>
                </Button>
                <Button size="sm" onClick={exportFlyAccounts} className="text-xs flex items-center gap-1">
                  <Upload className="w-3.5 h-3.5" />
                  <span>导出</span>
                </Button>
                <Button size="sm" onClick={importFlyAccounts} className="text-xs flex items-center gap-1">
                  <Download className="w-3.5 h-3.5" />
                  <span>导入</span>
                </Button>
              </div>
            </div>

            {/* Managed fly accounts */}
            <div className="overflow-x-auto">
              <Table>
                <Table.Header>
                  <Table.Row className="border-b border-kumo-line text-kumo-subtle font-bold bg-kumo-recessed/30">
                    <Table.Head className="p-3">备注名称</Table.Head>
                    <Table.Head className="p-3">账号邮箱</Table.Head>
                    <Table.Head className="p-3 text-center">状态</Table.Head>
                    <Table.Head className="p-3 text-center w-20">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {flyManagedAccounts.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={4} className="p-6 text-center text-kumo-subtle">暂无 Fly.io 账号</Table.Cell>
                    </Table.Row>
                  ) : (
                    flyManagedAccounts.map((account) => (
                      <Table.Row key={account.id} className="border-b border-kumo-line last:border-0 hover:bg-kumo-recessed/10">
                        <Table.Cell className="p-3 font-semibold text-kumo-strong">{account.name}</Table.Cell>
                        <Table.Cell className="p-3 text-kumo-default font-mono">{account.email || '-'}</Table.Cell>
                        <Table.Cell className="p-3 text-center">
                          <span className="px-2 py-0.5 rounded border bg-kumo-success/10 border-kumo-success text-kumo-success text-[10px] font-bold">
                            正常
                          </span>
                        </Table.Cell>
                        <Table.Cell className="p-3 text-center">
                          <Button
                            shape="square" size="sm"
                            variant="ghost"
                            aria-label="删除 Fly 账号"
                            onClick={() => removeFlyAccount(account.id, account.name)}
                            className="text-kumo-danger"
                          >
                            <Trash className="w-4 h-4" />
                          </Button>
                        </Table.Cell>
                      </Table.Row>
                    ))
                  )}
                </Table.Body>
              </Table>
            </div>

            {/* Batch add Fly */}
            <div className="border border-kumo-line border-dashed rounded-lg p-4 bg-kumo-recessed/20 space-y-3">
              <div className="text-xs font-bold text-kumo-strong">
                批量添加 Fly.io 账号（格式：备注:Fly-令牌）
              </div>
              <Textarea
                aria-label="批量添加 Fly.io 账号"
                value={flyBatchText}
                onChange={(e) => setFlyBatchText(e.target.value)}
                placeholder="我的Fly1:flyv1_xxxx&#10;我的Fly2:flyv1_yyyy"
                className="w-full min-h-[80px] text-xs font-mono text-kumo-strong p-2.5"
              />
              {flyBatchError && (
                <pre className="text-[10px] text-kumo-danger bg-kumo-danger/10 border border-kumo-danger/20 rounded p-2 overflow-x-auto font-mono whitespace-pre-wrap">
                  {flyBatchError}
                </pre>
              )}
              {flyBatchSuccess && (
                <div className="text-xs text-kumo-success bg-kumo-success/10 border border-kumo-success/20 rounded p-2">
                  {flyBatchSuccess}
                </div>
              )}
              <Button size="sm"
                onClick={batchAddFlyAccounts}
                disabled={flyAddingAccount || !flyBatchText.trim()}
                className="w-full text-xs flex items-center justify-center gap-1.5"
              >
                <Rocket className="w-3.5 h-3.5" />
                <span>批量验证并添加 Fly.io 账号</span>
              </Button>
            </div>
          </div>
        </div>
      )}

      {/* ==================== Settings Tab Content ==================== */}
      {activeTab === 'settings' && (
        <div className="app-card p-5 space-y-4">
          <div className="flex justify-between items-center border-b border-kumo-line pb-3">
            <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
              <Settings className="w-4 h-4 text-kumo-brand" />
              <span>模块刷新策略配置</span>
            </h3>
            <Button size="sm" onClick={saveSettings} className="text-xs flex items-center gap-1">
              <Save className="w-3.5 h-3.5" />
              <span>保存配置</span>
            </Button>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div className="space-y-1.5">
              <label className="text-xs font-bold text-kumo-strong">Koyeb 自动刷新间隔 (秒)</label>
              <Input size="sm"
                aria-label="Koyeb 自动刷新间隔"
                type="number"
                min="5"
                max={MAX_PAAS_REFRESH_INTERVAL_SEC}
                value={koyebIntervalSec}
                onChange={(e) => setKoyebIntervalSec(normalizeRefreshIntervalInputSec(e.target.value))}
                className="text-kumo-strong px-3 py-1.5 text-xs font-semibold w-full"
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-bold text-kumo-strong">Fly.io 自动刷新间隔 (秒)</label>
              <Input size="sm"
                aria-label="Fly.io 自动刷新间隔"
                type="number"
                min="5"
                max={MAX_PAAS_REFRESH_INTERVAL_SEC}
                value={flyIntervalSec}
                onChange={(e) => setFlyIntervalSec(normalizeRefreshIntervalInputSec(e.target.value))}
                className="text-kumo-strong px-3 py-1.5 text-xs font-semibold w-full"
              />
            </div>
          </div>
        </div>
      )}

      {/* ==================== Modals & Dialogs ==================== */}

      {/* Add Koyeb Dialog */}
      <Dialog.Root open={showAddKoyebModal} onOpenChange={setShowAddKoyebModal}>
        <Dialog className="p-6 sm:max-w-md">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            添加 Koyeb 账号
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            请输入您的 Koyeb API 令牌。建议以备注名区分。
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
                type="password"
                value={newKoyebToken}
                onChange={(e) => setNewKoyebToken(e.target.value)}
                placeholder="koyeb_api_token"
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
        <Dialog className="p-6 sm:max-w-md">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            添加 Fly.io 账号
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            请输入您的 Fly.io API 令牌（以 flyv1_ 开头）。
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
                type="password"
                value={newFlyToken}
                onChange={(e) => setNewFlyToken(e.target.value)}
                placeholder="flyv1_xxxx"
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

      {/* Global Self-Contained Log Viewer Dialog */}
      <Dialog.Root open={logViewerOpen} onOpenChange={setLogViewerOpen}>
        <Dialog className="p-0 sm:max-w-4xl overflow-hidden flex flex-col h-[80vh]">
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
          <div className="p-3 border-b border-kumo-line flex flex-col sm:flex-row justify-between items-start sm:items-center gap-3 text-xs bg-kumo-base">
            <div className="flex items-center gap-2 w-full sm:w-auto">
              <Search className="w-3.5 h-3.5 text-kumo-subtle" />
              <Input size="sm"
                aria-label="搜索日志消息"
                type="text"
                value={logFilterText}
                onChange={(e) => setLogFilterText(e.target.value)}
                placeholder="搜索日志消息..."
                className="px-2 py-1 w-full sm:w-48 text-kumo-strong"
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
                <RefreshCw className="w-4 h-4 animate-spin" />
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
