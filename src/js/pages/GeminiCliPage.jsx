import React, { useState, useEffect, useCallback, useMemo } from 'react';
import { toast } from '../modules/toast.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Checkbox } from '@cloudflare/kumo/components/checkbox';
import { Table } from '@cloudflare/kumo/components/table';
import { SkeletonLine } from '@cloudflare/kumo/components/loader';
import useTableResize from '../composables/useTableResize.js';
import { formatDateTime } from '../modules/utils.js';
import {
  Cpu,
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
  Bot,
  Activity,
  Check,
  Eye,
  EyeOff,
  Plug,
  Sliders,
  Settings as SettingsIcon,
  Globe,
  Terminal,
  Database,
  PieChart,
  Copy,
  AlertTriangle,
  Lock,
  ArrowRight,
  TrendingUp
} from '../components/Icons.jsx';

function GeminiCliPage() {
  const [activeTab, setActiveTab] = useState('models'); // 'models' | 'accounts' | 'logs' | 'settings'

  // Table resizing columns widths
  const [matrixColWidths, startMatrixResize] = useTableResize([220, 80, 80, 80, 80, 80, 80]);
  const [accountsColWidths, startAccountsResize] = useTableResize([50, 150, 150, 200, 80, 100, 120]);
  const [logsColWidths, startLogsResize] = useTableResize([150, 150, 140, 200, 70, 80, 80, 60]);

  // Admin password helper
  const getAuthHeaders = useCallback(() => {
    const password = localStorage.getItem('admin_password') || '';
    return {
      'Content-Type': 'application/json',
      'x-admin-password': password,
    };
  }, []);

  // IP/Address Masking Helper
  const maskEmail = (email) => {
    if (!email) return '-';
    const parts = email.split('@');
    if (parts.length !== 2) return email;
    const name = parts[0];
    const domain = parts[1];
    if (name.length <= 3) return `***@${domain}`;
    return `${name.slice(0, 2)}***${name.slice(-1)}@${domain}`;
  };

  const formatTokens = (tokens) => {
    if (tokens >= 1000000) {
      return (tokens / 1000000).toFixed(2) + ' M';
    }
    if (tokens >= 1000) {
      return (tokens / 1000).toFixed(1) + ' K';
    }
    return tokens;
  };

  // ==================== 1. Stats State ====================
  const [stats, setStats] = useState(null);
  const loadStats = useCallback(async () => {
    try {
      const response = await fetch('/api/gemini-cli/stats', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      setStats(data);
    } catch (error) {
      console.error('加载 Gemini CLI 统计失败:', error);
    }
  }, [getAuthHeaders]);

  // ==================== 2. Model Matrix Tab State ====================
  const [matrix, setMatrix] = useState({});
  const [matrixLoading, setMatrixLoading] = useState(false);

  const loadMatrix = useCallback(async (showFeedback = false) => {
    setMatrixLoading(true);
    try {
      const response = await fetch('/api/gemini-cli/config/matrix', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      setMatrix(data);
      if (showFeedback) {
        toast.success('矩阵配置已刷新');
      }
    } catch (error) {
      console.error('加载模型矩阵失败:', error);
      toast.error('加载矩阵配置失败');
    } finally {
      setMatrixLoading(false);
    }
  }, [getAuthHeaders]);

  const saveMatrix = async (updatedMatrix) => {
    const finalMatrix = updatedMatrix || matrix;
    try {
      const response = await fetch('/api/gemini-cli/config/matrix', {
        method: 'POST',
        headers: {
          ...getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(finalMatrix),
      });

      if (response.ok) {
        toast.success('矩阵配置已保存');
        loadMatrix(false);
      } else {
        toast.error('保存失败');
      }
    } catch (error) {
      toast.error('保存失败: ' + error.message);
    }
  };

  const toggleMatrixItem = (modelId, field) => {
    if (!matrix[modelId]) return;
    const updated = {
      ...matrix,
      [modelId]: {
        ...matrix[modelId],
        [field]: !matrix[modelId][field]
      }
    };
    setMatrix(updated);
    saveMatrix(updated);
  };

  const isMatrixColumnAllChecked = (field) => {
    const keys = Object.keys(matrix);
    if (keys.length === 0) return false;
    return keys.every(key => matrix[key][field]);
  };

  const toggleMatrixColumn = (field) => {
    const isAllChecked = isMatrixColumnAllChecked(field);
    const newValue = !isAllChecked;

    const updated = { ...matrix };
    Object.keys(updated).forEach(key => {
      updated[key] = {
        ...updated[key],
        [field]: newValue
      };
    });
    setMatrix(updated);
    saveMatrix(updated);
  };

  const toggleMatrixRow = (modelId) => {
    if (!matrix[modelId]) return;
    const row = matrix[modelId];
    const fields = ['base', 'maxThinking', 'noThinking', 'search', 'fakeStream', 'antiTrunc'];
    const hasAnyOn = fields.some(f => row[f]);
    const newState = !hasAnyOn;

    const updated = {
      ...matrix,
      [modelId]: {
        ...row,
        base: newState,
        maxThinking: newState,
        noThinking: newState,
        search: newState,
        fakeStream: newState,
        antiTrunc: newState
      }
    };
    setMatrix(updated);
    saveMatrix(updated);
  };

  const sortedMatrixList = useMemo(() => {
    const order = [
      'gemini-2.5-pro',
      'gemini-2.5-flash',
      'gemini-3.1-pro-preview',
      'gemini-3-pro-preview',
      'gemini-3-flash-preview',
    ];

    const allKeys = Object.keys(matrix);
    const sortedKeys = allKeys.sort((a, b) => {
      const idxA = order.indexOf(a);
      const idxB = order.indexOf(b);
      if (idxA !== -1 && idxB !== -1) return idxA - idxB;
      if (idxA !== -1) return -1;
      if (idxB !== -1) return 1;
      return a.localeCompare(b);
    });

    return sortedKeys.map(key => ({
      id: key,
      ...matrix[key],
    }));
  }, [matrix]);

  // ==================== 3. Accounts Tab State ====================
  const [accounts, setAccounts] = useState([]);
  const [accountsLoading, setAccountsLoading] = useState(false);
  const [accountFormOpen, setAccountFormOpen] = useState(false);
  const [editingAccount, setEditingAccount] = useState(null);
  const [accountForm, setAccountForm] = useState({
    name: '',
    client_id: '',
    client_secret: '',
    refresh_token: '',
    project_id: '',
    email: ''
  });
  const [accountFormError, setAccountFormError] = useState('');
  const [accountSaving, setAccountSaving] = useState(false);

  // OAuth Authorization drawer
  const [showOAuthExpand, setShowOAuthExpand] = useState(false);
  const [oauthReturnUrl, setOauthReturnUrl] = useState('');
  const [customProjectId, setCustomProjectId] = useState('');
  const [allowRandomProjectId, setAllowRandomProjectId] = useState(true);

  // Settings for OAuth
  const [gcliSettings, setGcliSettings] = useState({
    API_KEY: '',
    CLIENT_ID: '',
    CLIENT_SECRET: '',
    REDIRECT_URI: 'http://localhost:3000/oauth-callback' // fallback
  });

  const loadAccounts = useCallback(async (showFeedback = false) => {
    setAccountsLoading(true);
    try {
      const response = await fetch('/api/gemini-cli/accounts', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        setAccounts(data);
        loadStats();
        if (showFeedback) {
          toast.success('账号列表已刷新');
        }
      }
    } catch (error) {
      console.error('加载 Gemini CLI 账号失败:', error);
      toast.error('加载账号列表失败');
    } finally {
      setAccountsLoading(false);
    }
  }, [getAuthHeaders, loadStats]);

  const refreshAccounts = async () => {
    setAccountsLoading(true);
    toast.info('正在刷新所有账号及邮箱信息...');
    try {
      const response = await fetch('/api/gemini-cli/accounts/refresh', {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (response.ok) {
        toast.success(`刷新完成: 成功 ${data.refreshed}, 失败 ${data.failed}`);
        if (Array.isArray(data.accounts)) {
          setAccounts(data.accounts);
          loadStats();
        } else {
          loadAccounts(false);
        }
      } else {
        toast.error(data.error || '刷新失败');
      }
    } catch (error) {
      toast.error('请求失败: ' + error.message);
    } finally {
      setAccountsLoading(false);
    }
  };

  const toggleAccountEnabled = async (account) => {
    try {
      const response = await fetch(`/api/gemini-cli/accounts/${account.id}/toggle`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        toast.success(account.enable ? '账号已禁用' : '账号已启用');
        loadAccounts(false);
      } else {
        toast.error('操作失败');
      }
    } catch (error) {
      toast.error('操作失败: ' + error.message);
    }
  };

  const openAddAccountModal = () => {
    setEditingAccount(null);
    setAccountForm({
      name: '',
      client_id: gcliSettings.CLIENT_ID || '',
      client_secret: gcliSettings.CLIENT_SECRET || '',
      refresh_token: '',
      project_id: '',
      email: ''
    });
    setAccountFormError('');
    setAccountFormOpen(true);
  };

  const openEditAccountModal = (account) => {
    setEditingAccount(account);
    setAccountForm({
      name: account.name || '',
      client_id: account.client_id || '',
      client_secret: account.client_secret || '',
      refresh_token: account.refresh_token || '',
      project_id: account.project_id || '',
      email: account.email || ''
    });
    setAccountFormError('');
    setAccountFormOpen(true);
  };

  const saveAccount = async () => {
    if (!accountForm.name || !accountForm.client_id || !accountForm.refresh_token) {
      setAccountFormError('请填写名称、Client ID 和 Refresh Token');
      return;
    }
    setAccountSaving(true);
    setAccountFormError('');
    try {
      const url = editingAccount
        ? `/api/gemini-cli/accounts/${editingAccount.id}`
        : '/api/gemini-cli/accounts';
      const response = await fetch(url, {
        method: editingAccount ? 'PUT' : 'POST',
        headers: {
          ...getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(accountForm),
      });

      if (response.ok) {
        toast.success(editingAccount ? '账号已更新' : '账号已添加');
        setAccountFormOpen(false);
        loadAccounts(false);
      } else {
        const data = await response.json();
        setAccountFormError(data.error || '保存失败');
      }
    } catch (error) {
      setAccountFormError('保存失败: ' + error.message);
    } finally {
      setAccountSaving(false);
    }
  };

  const fetchEmailInfo = async () => {
    if (!accountForm.client_id || !accountForm.client_secret || !accountForm.refresh_token) {
      toast.error('请填写 Client ID, Secret 和 Refresh Token');
      return;
    }
    setAccountSaving(true);
    try {
      const response = await fetch('/api/gemini-cli/accounts/fetch-email', {
        method: 'POST',
        headers: {
          ...getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          client_id: accountForm.client_id,
          client_secret: accountForm.client_secret,
          refresh_token: accountForm.refresh_token,
        }),
      });

      const result = await response.json();
      if (response.ok && result.email) {
        setAccountForm(prev => ({ ...prev, email: result.email }));
        toast.success(`已自动获取邮箱: ${result.email}`);
      } else {
        toast.error(result.error || '获取邮箱失败');
      }
    } catch (error) {
      toast.error('获取邮箱失败: ' + error.message);
    } finally {
      setAccountSaving(false);
    }
  };

  const deleteAccount = async (account) => {
    if (!window.confirm(`确定要删除账号 "${account.name}" 吗？`)) {
      return;
    }
    try {
      const response = await fetch(`/api/gemini-cli/accounts/${account.id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('账号已删除');
        loadAccounts(false);
      } else {
        toast.error('删除失败');
      }
    } catch (error) {
      toast.error('删除失败: ' + error.message);
    }
  };

  // OAuth Actions
  const openOAuthUrl = () => {
    const clientId = gcliSettings.CLIENT_ID || 'YOUR_CLIENT_ID';
    const redirectUri = encodeURIComponent(gcliSettings.REDIRECT_URI || 'http://localhost:3000/oauth-callback');
    const scope = encodeURIComponent(
      'https://www.googleapis.com/auth/cloud-platform https://www.googleapis.com/auth/userinfo.email https://www.googleapis.com/auth/userinfo.profile'
    );
    const state = `111_${Math.random().toString(36).slice(2)}`;

    const url = `https://accounts.google.com/o/oauth2/auth?client_id=${clientId}&redirect_uri=${redirectUri}&scope=${scope}&response_type=code&access_type=offline&prompt=consent&include_granted_scopes=true&state=${state}`;
    window.open(url, '_blank');
  };

  const parseOAuthUrl = async () => {
    if (!oauthReturnUrl.trim()) {
      toast.error('请粘贴回调 URL');
      return;
    }
    let parsedUrl;
    try {
      parsedUrl = new URL(oauthReturnUrl);
    } catch (e) {
      toast.error('无效的 URL 格式');
      return;
    }

    const code = parsedUrl.searchParams.get('code');
    if (!code) {
      toast.error('URL 中未包含授权码 (code)');
      return;
    }

    setAccountsLoading(true);
    try {
      const response = await fetch('/api/gemini-cli/oauth/exchange', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          code,
          redirect_uri: gcliSettings.REDIRECT_URI,
          client_id: gcliSettings.CLIENT_ID,
          client_secret: gcliSettings.CLIENT_SECRET,
          project_id: customProjectId || undefined,
        }),
      });

      const result = await response.json();
      if (response.ok) {
        // Save account automatically
        const newForm = {
          name: `Gemini Project ${result.project_id || 'Auto'}`,
          email: result.email || '',
          client_id: gcliSettings.CLIENT_ID,
          client_secret: gcliSettings.CLIENT_SECRET,
          refresh_token: result.refresh_token,
          project_id: result.project_id,
        };

        const saveResponse = await fetch('/api/gemini-cli/accounts', {
          method: 'POST',
          headers: {
            ...getAuthHeaders(),
            'Content-Type': 'application/json',
          },
          body: JSON.stringify(newForm),
        });

        if (saveResponse.ok) {
          toast.success('OAuth 账号连接并保存成功！');
          setShowOAuthExpand(false);
          setOauthReturnUrl('');
          setCustomProjectId('');
          loadAccounts(false);
        } else {
          const err = await saveResponse.json();
          toast.error('保存 OAuth 账号失败: ' + (err.error || '未知错误'));
        }
      } else {
        toast.error(result.error || '交换 Token 失败');
      }
    } catch (error) {
      toast.error('请求失败: ' + error.message);
    } finally {
      setAccountsLoading(false);
    }
  };

  const exportAccounts = async () => {
    try {
      const response = await fetch('/api/gemini-cli/accounts/export', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.error) {
        toast.error('导出失败: ' + data.error);
        return;
      }

      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `gemini-cli-accounts-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(link);
      link.click();
      document.body.removeChild(link);
      URL.revokeObjectURL(url);
      toast.success(`已导出 ${data.accounts?.length || 0} 个账号`);
    } catch (error) {
      toast.error('导出失败: ' + error.message);
    }
  };

  const importAccounts = () => {
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
          toast.error('无效的备份文件格式');
          return;
        }

        setAccountsLoading(true);
        const response = await fetch('/api/gemini-cli/accounts/import', {
          method: 'POST',
          headers: {
            ...getAuthHeaders(),
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ accounts: data.accounts }),
        });

        const result = await response.json();
        if (result.success) {
          toast.success(`导入成功: ${result.imported} 个账号，跳过 ${result.skipped || 0} 个重复项`);
          loadAccounts(false);
        } else {
          toast.error('导入失败: ' + (result.error || '未知错误'));
        }
      } catch (error) {
        toast.error('导入失败: ' + error.message);
      } finally {
        setAccountsLoading(false);
      }
    };
    input.click();
  };

  // Cooldown text formatter
  const formatCoolDownTitle = (coolDowns) => {
    if (!coolDowns || coolDowns.length === 0) return '';
    return coolDowns
      .map(c => {
        const time = new Date(c.resetTime).toLocaleTimeString();
        return `${c.model} 冷却至 ${time}`;
      })
      .join('\n');
  };

  // ==================== 4. Quotas Overview State ====================
  const [quotaData, setQuotaData] = useState([]);
  const [quotaModels, setQuotaModels] = useState([]);
  const [quotaLoading, setQuotaLoading] = useState(false);

  const loadQuotas = useCallback(async (forceRefresh = false) => {
    setQuotaLoading(true);
    try {
      const url = `/api/gemini-cli/quotas/all${forceRefresh ? '?refresh=1' : ''}`;
      const response = await fetch(url, {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        const filtered = data.filter(d => d && d.buckets);
        setQuotaData(filtered);

        const modelSet = new Set();
        filtered.forEach(q => {
          (q.buckets || []).forEach(b => {
            if (b.modelId !== 'gemini-2.0-flash') {
              modelSet.add(b.modelId);
            }
          });
        });
        setQuotaModels(Array.from(modelSet).sort());

        if (forceRefresh) {
          toast.success(`额度查询完成 (${filtered.length} 个账号)`);
        }
      }
    } catch (error) {
      console.error('加载 Gemini CLI 额度失败:', error);
      if (forceRefresh) toast.error('额度查询失败: ' + error.message);
    } finally {
      setQuotaLoading(false);
    }
  }, [getAuthHeaders]);

  const getQuotaBucket = (qData, modelId) => {
    return qData?.buckets?.find(b => b.modelId === modelId) || null;
  };

  const getQuotaBarColor = (fraction) => {
    if (fraction == null) return 'var(--color-kumo-line)';
    const pct = fraction * 100;
    if (pct >= 70) return '#10b981'; // Green
    if (pct >= 40) return '#f59e0b'; // Amber
    if (pct >= 15) return '#f97316'; // Orange
    return '#ef4444'; // Red
  };

  const formatQuotaResetTime = (resetTime) => {
    if (!resetTime) return '未知';
    const reset = new Date(resetTime);
    const now = new Date();
    const diffMs = reset - now;

    if (diffMs <= 0) return '已重置';

    const hours = Math.floor(diffMs / 3600000);
    const minutes = Math.floor((diffMs % 3600000) / 60000);

    if (hours > 0) return `${hours}时 ${minutes}分后重置`;
    return `${minutes}分后重置`;
  };

  const isQuotaInCooldown = (bucket) => {
    if (!bucket) return false;
    if (bucket.remainingFraction > 0) return false;
    if (!bucket.resetTime) return false;

    const reset = new Date(bucket.resetTime);
    return reset > new Date();
  };

  // ==================== 5. Health Check State ====================
  const [autoCheck, setAutoCheck] = useState(false);
  const [autoCheckInterval, setAutoCheckInterval] = useState(3600000);
  const [disabledCheckModels, setDisabledCheckModels] = useState([]);
  const [checkHistory, setCheckHistory] = useState({ models: [], times: [], matrix: {} });
  const [checking, setChecking] = useState(false);

  const loadCheckSettings = useCallback(async () => {
    try {
      const response = await fetch('/api/gemini-cli/settings', {
        headers: getAuthHeaders(),
      });
      const settings = await response.json();

      if (settings.autoCheckEnabled !== undefined) {
        setAutoCheck(settings.autoCheckEnabled === '1' || settings.autoCheckEnabled === true);
      }
      if (settings.autoCheckInterval !== undefined) {
        setAutoCheckInterval(parseInt(settings.autoCheckInterval) || 3600000);
      }
      if (settings.disabledCheckModels) {
        try {
          setDisabledCheckModels(JSON.parse(settings.disabledCheckModels) || []);
        } catch (e) {
          setDisabledCheckModels([]);
        }
      }
    } catch (e) {
      console.error('加载定时检测配置失败:', e);
    }
  }, [getAuthHeaders]);

  const loadCheckHistory = useCallback(async () => {
    try {
      const response = await fetch('/api/gemini-cli/models/check-history', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      setCheckHistory(data);
    } catch (error) {
      console.error('加载健康检测历史失败:', error);
    }
  }, [getAuthHeaders]);

  const saveCheckSettings = async (updatedCheck, updatedInterval, updatedDisabled) => {
    try {
      await fetch('/api/gemini-cli/settings', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          ...getAuthHeaders(),
        },
        body: JSON.stringify({
          autoCheckEnabled: updatedCheck ? '1' : '0',
          autoCheckInterval: String(updatedInterval),
          disabledCheckModels: JSON.stringify(updatedDisabled),
        }),
      });
      loadCheckSettings();
    } catch (error) {
      console.error('保存定时检测设置失败:', error);
    }
  };

  const handleToggleAutoCheck = () => {
    const nextVal = !autoCheck;
    setAutoCheck(nextVal);
    saveCheckSettings(nextVal, autoCheckInterval, disabledCheckModels);
    if (nextVal) {
      toast.success(`已开启定时检测 (每 ${Math.round(autoCheckInterval / 60000)} 分钟)`);
    } else {
      toast.info('已关闭定时检测');
    }
  };

  const handleIntervalChange = (interval) => {
    setAutoCheckInterval(interval);
    saveCheckSettings(autoCheck, interval, disabledCheckModels);
    if (autoCheck) {
      toast.success(`定时检测间隔已更新为 ${Math.round(interval / 60000)} 分钟`);
    }
  };

  const toggleCheckModel = (modelId) => {
    const updated = disabledCheckModels.includes(modelId)
      ? disabledCheckModels.filter(m => m !== modelId)
      : [...disabledCheckModels, modelId];
    setDisabledCheckModels(updated);
    saveCheckSettings(autoCheck, autoCheckInterval, updated);
  };

  const executeHealthCheck = async () => {
    setChecking(true);
    toast.info('正在检测模型健康状态...');
    const pollInterval = setInterval(() => {
      loadCheckHistory();
    }, 2000);

    try {
      const response = await fetch('/api/gemini-cli/accounts/check', {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (response.ok && data.success) {
        toast.success('检测完成');
      } else {
        toast.error(data.error || '检测失败');
      }
    } catch (error) {
      toast.error('检测连接失败: ' + error.message);
    } finally {
      clearInterval(pollInterval);
      setChecking(false);
      loadCheckHistory();
    }
  };

  const clearCheckHistory = async () => {
    if (!window.confirm('确定要清空所有健康检测历史吗？')) return;
    try {
      const response = await fetch('/api/gemini-cli/models/check-history/clear', {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('检测历史已清空');
        setCheckHistory({ models: [], times: [], matrix: {} });
      } else {
        toast.error('清空失败');
      }
    } catch (error) {
      toast.error('请求错误: ' + error.message);
    }
  };

  const getCheckBadgeClass = (checkData, accountIndex) => {
    if (!checkData) return 'bg-kumo-recessed text-kumo-subtle';
    if (checkData.error_log === 'Waiting...' || checkData.error_log === 'Checking...') {
      return 'bg-kumo-brand/10 text-kumo-brand animate-pulse border border-kumo-brand/20';
    }

    const passedList = (checkData.passedAccounts || '').split(',').filter(Boolean);
    if (passedList.includes(String(accountIndex))) {
      return 'bg-kumo-success/15 text-kumo-success border border-kumo-success/25 font-bold';
    }

    const errorLog = checkData.error_log || '';
    const checkComplete = errorLog.length > 0 && errorLog !== 'Waiting...' && errorLog !== 'Checking...';

    if (checkComplete && (checkData.status === 'ok' || checkData.status === 'error')) {
      return 'bg-kumo-danger/15 text-kumo-danger border border-kumo-danger/25';
    }

    return 'bg-kumo-recessed text-kumo-subtle border border-kumo-line';
  };

  const getCheckBadgeTitle = (checkData, accountIndex) => {
    if (!checkData) return '未检测';
    if (checkData.error_log === 'Waiting...' || checkData.error_log === 'Checking...') {
      return `账号 #${accountIndex} 检测中`;
    }

    const passedList = (checkData.passedAccounts || '').split(',').filter(Boolean);
    if (passedList.includes(String(accountIndex))) {
      return `账号 #${accountIndex} 通过 (可用)`;
    }
    return `账号 #${accountIndex} 失败 (异常或受限)\n日志: ${checkData.error_log || '-'}`;
  };

  // ==================== 6. Logs Tab State ====================
  const [logs, setLogs] = useState([]);
  const [logsLoading, setLogsLoading] = useState(false);
  const [logFilterAccount, setLogFilterAccount] = useState('');
  const [logFilterModel, setLogFilterModel] = useState('');
  const [logDetail, setLogDetail] = useState(null);
  const [logDetailOpen, setLogDetailOpen] = useState(false);
  const [logDetailShowRaw, setLogDetailShowRaw] = useState(false);

  const loadLogs = useCallback(async (showFeedback = false) => {
    setLogsLoading(true);
    try {
      const response = await fetch('/api/gemini-cli/logs', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        setLogs(data);
        if (showFeedback) {
          toast.success('调用日志已更新');
        }
      }
    } catch (error) {
      toast.error('加载调用日志失败');
    } finally {
      setLogsLoading(false);
    }
  }, [getAuthHeaders]);

  const viewLogDetail = async (log) => {
    try {
      const response = await fetch(`/api/gemini-cli/logs/${log.id}`, {
        headers: getAuthHeaders(),
      });
      const data = await response.json();

      if (data) {
        data.timestamp = data.timestamp || data.created_at;
        data.durationMs = data.durationMs || data.duration_ms;
        data.statusCode = data.statusCode || data.status_code;
        data.accountId = data.accountId || data.account_id;
        data.path = data.path || data.request_path || '/v1/chat/completions';
        data.method = data.method || data.request_method || 'POST';
        data.clientIp = data.clientIp || data.client_ip;
        data.userAgent = data.userAgent || data.user_agent;

        if (data.detail) {
          if (data.detail.request && data.detail.request.messages && !data.detail.messages) {
            data.detail.messages = data.detail.request.messages;
          }
          if (data.detail.request && data.detail.request.contents && !data.detail.messages) {
            data.detail.messages = data.detail.request.contents.map(c => ({
              role: c.role === 'model' ? 'assistant' : c.role,
              content: c.parts ? c.parts.map(p => p.text).join('') : '',
            }));
          }
          if (data.detail.response && data.detail.response.candidates && !data.detail.response.choices) {
            data.detail.response.choices = data.detail.response.candidates.map(c => ({
              message: {
                role: 'assistant',
                content: c.content && c.content.parts ? c.content.parts.map(p => p.text).join('') : '',
                reasoning_content: null,
              },
            }));
          }
        }
      }

      setLogDetailShowRaw(false);
      setLogDetail(data);
      setLogDetailOpen(true);
    } catch (error) {
      toast.error('获取日志详情失败');
    }
  };

  const clearLogs = async () => {
    if (!window.confirm('确定要清空所有 Gemini CLI 调用日志吗？')) return;
    try {
      const response = await fetch('/api/gemini-cli/logs', {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('调用日志已清空');
        setLogs([]);
      }
    } catch (error) {
      toast.error('清空日志失败');
    }
  };

  const logModelsList = useMemo(() => {
    const models = new Set();
    logs.forEach(log => {
      if (log.model) models.add(log.model);
    });
    return Array.from(models).sort();
  }, [logs]);

  const filteredLogs = useMemo(() => {
    let list = [...logs];
    if (logFilterAccount) {
      list = list.filter(log => log.accountId === logFilterAccount);
    }
    if (logFilterModel) {
      list = list.filter(log => log.model === logFilterModel);
    }
    return list;
  }, [logs, logFilterAccount, logFilterModel]);

  const getLogStatusClass = (code) => {
    if (!code) return 'bg-kumo-recessed text-kumo-subtle';
    if (code >= 200 && code < 300) return 'bg-kumo-success/10 text-kumo-success border border-kumo-success/20';
    if (code === 429) return 'bg-kumo-danger/10 text-kumo-danger border border-kumo-danger/20';
    if (code >= 400) return 'bg-kumo-danger/10 text-kumo-danger border border-kumo-danger/20';
    return 'bg-kumo-recessed text-kumo-subtle';
  };

  // ==================== 7. Settings Tab State ====================
  const [settingsForm, setSettingsForm] = useState({
    DEFAULT_TEMPERATURE: 1.0,
    DEFAULT_TOP_P: 0.95,
    DEFAULT_TOP_K: 64,
    DEFAULT_MAX_TOKENS: 8192,
    CREDENTIAL_MAX_USAGE_PER_HOUR: 1000,
    TIMEOUT: 300,
    LOG_RETENTION_DAYS: 7,
    API_KEY: ''
  });
  const [settingsSaving, setSettingsSaving] = useState(false);

  // Model Redirects inside settings
  const [redirects, setRedirects] = useState([]);
  const [editingRedirectSource, setEditingRedirectSource] = useState(null);
  const [newRedirectSource, setNewRedirectSource] = useState('');
  const [newRedirectTarget, setNewRedirectTarget] = useState('');

  const loadSettings = useCallback(async (showFeedback = false) => {
    try {
      const response = await fetch('/api/gemini-cli/settings', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      setGcliSettings(data);
      setSettingsForm({
        DEFAULT_TEMPERATURE: Number(data.DEFAULT_TEMPERATURE) || 1.0,
        DEFAULT_TOP_P: Number(data.DEFAULT_TOP_P) || 0.95,
        DEFAULT_TOP_K: Number(data.DEFAULT_TOP_K) || 64,
        DEFAULT_MAX_TOKENS: Number(data.DEFAULT_MAX_TOKENS) || 8192,
        CREDENTIAL_MAX_USAGE_PER_HOUR: Number(data.CREDENTIAL_MAX_USAGE_PER_HOUR) || 1000,
        TIMEOUT: Number(data.TIMEOUT) || 300,
        LOG_RETENTION_DAYS: Number(data.LOG_RETENTION_DAYS) || 7,
        API_KEY: data.API_KEY || ''
      });
      if (showFeedback) {
        toast.success('配置已从服务器更新');
      }
    } catch (error) {
      toast.error('加载模块设置失败');
    }
  }, [getAuthHeaders]);

  const saveSettings = async () => {
    setSettingsSaving(true);
    try {
      const response = await fetch('/api/gemini-cli/settings', {
        method: 'POST',
        headers: {
          ...getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(settingsForm),
      });

      if (response.ok) {
        toast.success('模块配置保存成功');
        loadSettings(false);
      } else {
        toast.error('保存设置失败');
      }
    } catch (error) {
      toast.error('保存设置失败: ' + error.message);
    } finally {
      setSettingsSaving(false);
    }
  };

  const loadRedirects = useCallback(async () => {
    try {
      const response = await fetch('/api/gemini-cli/models/redirects', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      setRedirects(data);
    } catch (error) {
      console.error('加载模型重定向失败:', error);
    }
  }, [getAuthHeaders]);

  const addRedirectRule = async (source, target) => {
    if (!source.trim() || !target.trim()) {
      toast.warning('请输入源模型和目标模型');
      return;
    }
    try {
      if (editingRedirectSource && editingRedirectSource !== source) {
        await fetch(`/api/gemini-cli/models/redirects/${encodeURIComponent(editingRedirectSource)}`, {
          method: 'DELETE',
          headers: getAuthHeaders(),
        });
      }

      const response = await fetch('/api/gemini-cli/models/redirects', {
        method: 'POST',
        headers: {
          ...getAuthHeaders(),
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ sourceModel: source, targetModel: target }),
      });

      if (response.ok) {
        toast.success('操作成功');
        setNewRedirectSource('');
        setNewRedirectTarget('');
        setEditingRedirectSource(null);
        loadRedirects();
      } else {
        const err = await response.json();
        toast.error('保存规则失败: ' + (err.error || '未知错误'));
      }
    } catch (error) {
      toast.error('请求失败: ' + error.message);
    }
  };

  const deleteRedirectRule = async (sourceModel) => {
    if (!window.confirm(`确认删除源模型 "${sourceModel}" 的重定向规则吗？`)) return;
    try {
      const response = await fetch(`/api/gemini-cli/models/redirects/${encodeURIComponent(sourceModel)}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('规则已删除');
        loadRedirects();
      } else {
        toast.error('删除规则失败');
      }
    } catch (error) {
      toast.error('删除规则失败: ' + error.message);
    }
  };

  const getBaseUrl = () => {
    const hostUrl = window.location.origin;
    return `${hostUrl}/v1`;
  };

  const copyEndpoint = () => {
    navigator.clipboard.writeText(getBaseUrl()).then(() => {
      toast.success('已复制 API 端点地址');
    }).catch(() => {
      toast.error('复制失败，请手动复制');
    });
  };

  // Initial loader hook
  useEffect(() => {
    if (activeTab === 'models') {
      loadMatrix(false);
      loadStats();
    } else if (activeTab === 'accounts') {
      loadAccounts(false);
      loadQuotas(false);
      loadCheckSettings();
      loadCheckHistory();
      loadSettings(false);
    } else if (activeTab === 'logs') {
      loadLogs(false);
      loadAccounts(false);
    } else if (activeTab === 'settings') {
      loadSettings(false);
      loadRedirects();
    }
  }, [activeTab, loadMatrix, loadStats, loadAccounts, loadQuotas, loadCheckSettings, loadCheckHistory, loadSettings, loadLogs, loadRedirects]);

  return (
    <div className="space-y-6 flex flex-col pb-20">
      {/* Sub Tabs */}
      <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center border-b border-kumo-line pb-4 gap-4 select-none">
        <div className="flex border border-kumo-line rounded-lg p-0.5 bg-kumo-recessed">
          <button
            onClick={() => setActiveTab('models')}
            className={`flex items-center gap-2 px-4 py-1.5 rounded-md text-xs font-semibold cursor-pointer transition-colors ${
              activeTab === 'models'
                ? 'bg-kumo-base text-kumo-strong shadow-sm'
                : 'text-kumo-subtle hover:text-kumo-strong'
            }`}
          >
            <Cpu className="w-3.5 h-3.5" />
            <span>模型矩阵</span>
          </button>
          <button
            onClick={() => setActiveTab('accounts')}
            className={`flex items-center gap-2 px-4 py-1.5 rounded-md text-xs font-semibold cursor-pointer transition-colors ${
              activeTab === 'accounts'
                ? 'bg-kumo-base text-kumo-strong shadow-sm'
                : 'text-kumo-subtle hover:text-kumo-strong'
            }`}
          >
            <Users className="w-3.5 h-3.5" />
            <span>账号与检测</span>
          </button>
          <button
            onClick={() => setActiveTab('logs')}
            className={`flex items-center gap-2 px-4 py-1.5 rounded-md text-xs font-semibold cursor-pointer transition-colors ${
              activeTab === 'logs'
                ? 'bg-kumo-base text-kumo-strong shadow-sm'
                : 'text-kumo-subtle hover:text-kumo-strong'
            }`}
          >
            <History className="w-3.5 h-3.5" />
            <span>调用日志</span>
          </button>
          <button
            onClick={() => setActiveTab('settings')}
            className={`flex items-center gap-2 px-4 py-1.5 rounded-md text-xs font-semibold cursor-pointer transition-colors ${
              activeTab === 'settings'
                ? 'bg-kumo-base text-kumo-strong shadow-sm'
                : 'text-kumo-subtle hover:text-kumo-strong'
            }`}
          >
            <SettingsIcon className="w-3.5 h-3.5" />
            <span>模块配置</span>
          </button>
        </div>
      </div>

      {/* ==================== 1. 模型矩阵 Tab ==================== */}
      {activeTab === 'models' && (
        <div className="quick-fade-in space-y-6">
          {/* Stats widgets */}
          {stats && (
            <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
              <div className="bg-kumo-base p-4 border border-kumo-line rounded-lg shadow-sm flex items-center justify-between">
                <div>
                  <span className="text-[10px] font-bold text-kumo-subtle block uppercase">总调用量</span>
                  <span className="text-xl font-bold text-kumo-strong">{stats.total_calls || 0}</span>
                </div>
                <div className="p-2 bg-kumo-brand/10 rounded-lg text-kumo-brand">
                  <TrendingUp className="w-5 h-5" />
                </div>
              </div>
              <div className="bg-kumo-base p-4 border border-kumo-line rounded-lg shadow-sm flex items-center justify-between">
                <div>
                  <span className="text-[10px] font-bold text-kumo-subtle block uppercase">消耗 Token</span>
                  <span className="text-xl font-bold text-kumo-strong">{formatTokens(stats.total_tokens || 0)}</span>
                </div>
                <div className="p-2 bg-kumo-brand/10 rounded-lg text-kumo-brand">
                  <Database className="w-5 h-5" />
                </div>
              </div>
              <div className="bg-kumo-base p-4 border border-kumo-line rounded-lg shadow-sm flex items-center justify-between">
                <div>
                  <span className="text-[10px] font-bold text-kumo-subtle block uppercase">平均响应</span>
                  <span className="text-xl font-bold text-kumo-strong">
                    {stats.avg_duration || 0}
                    <small className="text-[10px] ml-0.5 text-kumo-subtle font-semibold">ms</small>
                  </span>
                </div>
                <div className="p-2 bg-kumo-brand/10 rounded-lg text-kumo-brand">
                  <Activity className="w-5 h-5" />
                </div>
              </div>
              <div className="bg-kumo-base p-4 border border-kumo-line rounded-lg shadow-sm flex items-center justify-between">
                <div>
                  <span className="text-[10px] font-bold text-kumo-subtle block uppercase">调用成功率</span>
                  <span className="text-xl font-bold text-kumo-strong">
                    {stats.success_rate || '0.0'}
                    <small className="text-[10px] ml-0.5 text-kumo-subtle font-semibold">%</small>
                  </span>
                </div>
                <div className="p-2 bg-kumo-success/10 rounded-lg text-kumo-success">
                  <Check className="w-5 h-5" />
                </div>
              </div>
            </div>
          )}

          {/* Matrix Table */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            <div className="p-4 border-b border-kumo-line flex justify-between items-center bg-kumo-recessed/10">
              <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
                <Cpu className="w-4 h-4 text-kumo-brand" />
                Model Matrix
              </h3>
              <div className="flex gap-2">
                <Button onClick={() => saveMatrix(null)} className="flex items-center gap-1">
                  <Check className="w-3.5 h-3.5" />
                  <span>保存矩阵</span>
                </Button>
                <Button onClick={() => loadMatrix(true)} className="flex items-center gap-1">
                  <RefreshCw className={`w-3.5 h-3.5 ${matrixLoading ? 'animate-spin' : ''}`} />
                  <span>刷新</span>
                </Button>
              </div>
            </div>

            <div className="overflow-x-auto">
              <Table layout="fixed">
                <colgroup>
                  {matrixColWidths.map((w, idx) => (
                    <col key={idx} style={{ width: w }} />
                  ))}
                </colgroup>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head className="relative group pr-6">
                      模型名称
                      <Table.ResizeHandle onMouseDown={(e) => startMatrixResize(0, e)} />
                    </Table.Head>
                    <Table.Head className="text-center relative group pr-6">
                      <div className="flex flex-col items-center gap-1">
                        <Checkbox
                          checked={isMatrixColumnAllChecked('base')}
                          onCheckedChange={() => toggleMatrixColumn('base')}
                          label="基础模型"
                        />
                      </div>
                      <Table.ResizeHandle onMouseDown={(e) => startMatrixResize(1, e)} />
                    </Table.Head>
                    <Table.Head className="text-center relative group pr-6">
                      <div className="flex flex-col items-center gap-1">
                        <Checkbox
                          checked={isMatrixColumnAllChecked('maxThinking')}
                          onCheckedChange={() => toggleMatrixColumn('maxThinking')}
                          label="深度思考"
                        />
                      </div>
                      <Table.ResizeHandle onMouseDown={(e) => startMatrixResize(2, e)} />
                    </Table.Head>
                    <Table.Head className="text-center relative group pr-6">
                      <div className="flex flex-col items-center gap-1">
                        <Checkbox
                          checked={isMatrixColumnAllChecked('noThinking')}
                          onCheckedChange={() => toggleMatrixColumn('noThinking')}
                          label="快速思考"
                        />
                      </div>
                      <Table.ResizeHandle onMouseDown={(e) => startMatrixResize(3, e)} />
                    </Table.Head>
                    <Table.Head className="text-center relative group pr-6">
                      <div className="flex flex-col items-center gap-1">
                        <Checkbox
                          checked={isMatrixColumnAllChecked('search')}
                          onCheckedChange={() => toggleMatrixColumn('search')}
                          label="联网搜索"
                        />
                      </div>
                      <Table.ResizeHandle onMouseDown={(e) => startMatrixResize(4, e)} />
                    </Table.Head>
                    <Table.Head className="text-center relative group pr-6">
                      <div className="flex flex-col items-center gap-1">
                        <Checkbox
                          checked={isMatrixColumnAllChecked('fakeStream')}
                          onCheckedChange={() => toggleMatrixColumn('fakeStream')}
                          label="假流"
                        />
                      </div>
                      <Table.ResizeHandle onMouseDown={(e) => startMatrixResize(5, e)} />
                    </Table.Head>
                    <Table.Head className="text-center">
                      <div className="flex flex-col items-center gap-1">
                        <Checkbox
                          checked={isMatrixColumnAllChecked('antiTrunc')}
                          onCheckedChange={() => toggleMatrixColumn('antiTrunc')}
                          label="流抗"
                        />
                      </div>
                    </Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {matrixLoading ? (
                    [...Array(5)].map((_, i) => (
                      <Table.Row key={i}>
                        <Table.Cell><SkeletonLine className="w-32 h-4" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-8 h-4 mx-auto" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-8 h-4 mx-auto" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-8 h-4 mx-auto" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-8 h-4 mx-auto" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-8 h-4 mx-auto" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-8 h-4 mx-auto" /></Table.Cell>
                      </Table.Row>
                    ))
                  ) : sortedMatrixList.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={7} className="p-8 text-center text-kumo-subtle">
                        暂无配置数据
                      </Table.Cell>
                    </Table.Row>
                  ) : (
                    sortedMatrixList.map((row) => (
                      <Table.Row key={row.id} className="hover:bg-kumo-recessed/5">
                        <Table.Cell
                          onClick={() => toggleMatrixRow(row.id)}
                          className="p-3 font-mono font-semibold text-kumo-strong hover:text-kumo-brand cursor-pointer"
                          title="双击或点击整行快速切换"
                        >
                          {row.id}
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <div className="flex justify-center">
                            <Switch
                              size="sm"
                              checked={!!row.base}
                              onCheckedChange={() => toggleMatrixItem(row.id, 'base')}
                            />
                          </div>
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <div className="flex justify-center">
                            <Switch
                              size="sm"
                              checked={!!row.maxThinking}
                              onCheckedChange={() => toggleMatrixItem(row.id, 'maxThinking')}
                            />
                          </div>
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <div className="flex justify-center">
                            <Switch
                              size="sm"
                              checked={!!row.noThinking}
                              onCheckedChange={() => toggleMatrixItem(row.id, 'noThinking')}
                            />
                          </div>
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <div className="flex justify-center">
                            <Switch
                              size="sm"
                              checked={!!row.search}
                              onCheckedChange={() => toggleMatrixItem(row.id, 'search')}
                            />
                          </div>
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <div className="flex justify-center">
                            <Switch
                              size="sm"
                              checked={!!row.fakeStream}
                              onCheckedChange={() => toggleMatrixItem(row.id, 'fakeStream')}
                            />
                          </div>
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <div className="flex justify-center">
                            <Switch
                              size="sm"
                              checked={!!row.antiTrunc}
                              onCheckedChange={() => toggleMatrixItem(row.id, 'antiTrunc')}
                            />
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ))
                  )}
                </Table.Body>
              </Table>
            </div>
          </div>
        </div>
      )}

      {/* ==================== 2. 账号与检测 Tab ==================== */}
      {activeTab === 'accounts' && (
        <div className="quick-fade-in space-y-6">
          {/* Actions toolbar */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-4 flex flex-col md:flex-row justify-between items-start md:items-center gap-4">
            <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
              <Users className="w-4 h-4 text-kumo-brand" />
              账号管理与监控
            </h3>
            <div className="flex flex-wrap gap-2">
              <Button onClick={() => setShowOAuthExpand(!showOAuthExpand)} className="flex items-center gap-1">
                <Globe className="w-3.5 h-3.5 text-blue-500" />
                <span>OAuth 授权</span>
              </Button>
              <Button variant="primary" onClick={openAddAccountModal} className="flex items-center gap-1">
                <Plus className="w-3.5 h-3.5" />
                <span>手动添加</span>
              </Button>
              <Button onClick={refreshAccounts} disabled={accountsLoading} className="flex items-center gap-1">
                <RefreshCw className={`w-3.5 h-3.5 ${accountsLoading ? 'animate-spin' : ''}`} />
                <span>刷新账号</span>
              </Button>
              <Button onClick={exportAccounts} className="flex items-center gap-1">
                <Upload className="w-3.5 h-3.5" />
                <span>导出</span>
              </Button>
              <Button onClick={importAccounts} className="flex items-center gap-1">
                <Download className="w-3.5 h-3.5" />
                <span>导入</span>
              </Button>
            </div>
          </div>

          {/* OAuth Expansion Panel */}
          {showOAuthExpand && (
            <div className="bg-kumo-base border border-kumo-brand/30 rounded-lg shadow-sm p-5 space-y-5 border-dashed">
              <div className="flex justify-between items-center pb-2 border-b border-kumo-line">
                <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-1.5">
                  <Globe className="w-4 h-4 text-kumo-brand" />
                  连接 Google 账号 (OAuth2 授权验证)
                </h4>
                <button onClick={() => setShowOAuthExpand(false)} className="text-kumo-subtle hover:text-kumo-strong">
                  <X className="w-4 h-4" />
                </button>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                {/* Step 1 */}
                <div className="space-y-3 p-4 bg-kumo-recessed/30 rounded-lg border border-kumo-line relative">
                  <div className="absolute top-3 right-3 text-xl font-black text-kumo-brand/20">01</div>
                  <h5 className="text-xs font-bold text-kumo-strong">第一步：获取谷歌授权</h5>
                  <p className="text-[11px] text-kumo-subtle leading-relaxed">
                    在弹出的 Google 登录页面授权以获取认证 Code 回调。
                  </p>
                  <Button variant="primary" onClick={openOAuthUrl} className="w-full">
                    <span>打开谷歌授权页面</span>
                  </Button>
                  <div className="text-[10px] text-kumo-subtle flex items-start gap-1 p-2 bg-kumo-recessed border border-kumo-line rounded">
                    <AlertTriangle className="w-3.5 h-3.5 text-yellow-500 flex-shrink-0 mt-0.5" />
                    <span>
                      授权时如果提示安全审核或 App Name 为 "Google Antigravity" 是正常情况（系统共享相同的 API Client 凭证）。
                    </span>
                  </div>
                </div>

                {/* Step 2 */}
                <div className="space-y-3 p-4 bg-kumo-recessed/30 rounded-lg border border-kumo-line relative">
                  <div className="absolute top-3 right-3 text-xl font-black text-kumo-brand/20">02</div>
                  <h5 className="text-xs font-bold text-kumo-strong">第二步：解析并连接</h5>
                  <p className="text-[11px] text-kumo-subtle leading-relaxed">
                    请将您在授权完成重定向后浏览器地址栏中的完整 URL 粘贴在下方。
                  </p>
                  <textarea
                    value={oauthReturnUrl}
                    onChange={(e) => setOauthReturnUrl(e.target.value)}
                    placeholder="粘贴以 http:// 或 https:// 开头的完整回调 URL..."
                    rows={2}
                    className="w-full bg-kumo-base text-kumo-strong text-xs font-mono p-2 border border-kumo-line rounded focus:outline-none focus:border-kumo-brand resize-none"
                  />
                </div>
              </div>

              {/* Extras */}
              <div className="bg-kumo-recessed/20 p-3 rounded-lg border border-kumo-line flex flex-wrap gap-4 items-center justify-between text-xs">
                <div className="flex items-center gap-2">
                  <span className="font-semibold text-kumo-strong">自定义 Project ID (可选)</span>
                  <input
                    type="text"
                    value={customProjectId}
                    onChange={(e) => setCustomProjectId(e.target.value)}
                    placeholder="例如: project-id-123"
                    className="bg-kumo-base border border-kumo-line rounded px-2.5 py-1 text-xs focus:outline-none focus:border-kumo-brand"
                  />
                </div>
                <Checkbox
                  checked={allowRandomProjectId}
                  onCheckedChange={setAllowRandomProjectId}
                  label="允许随机 Project ID"
                />
                <Button
                  variant="primary"
                  onClick={parseOAuthUrl}
                  disabled={!oauthReturnUrl || accountsLoading}
                  className="font-semibold flex items-center gap-1"
                >
                  <Check className="w-3.5 h-3.5" />
                  <span>{accountsLoading ? '验证连接中...' : '提交并授权'}</span>
                </Button>
              </div>
            </div>
          )}

          {/* Accounts Table */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            <div className="overflow-x-auto">
              <Table layout="fixed">
                <colgroup>
                  {accountsColWidths.map((w, idx) => (
                    <col key={idx} style={{ width: w }} />
                  ))}
                </colgroup>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head className="text-center relative group pr-6">
                      #
                      <Table.ResizeHandle onMouseDown={(e) => startAccountsResize(0, e)} />
                    </Table.Head>
                    <Table.Head className="relative group pr-6">
                      备注名称
                      <Table.ResizeHandle onMouseDown={(e) => startAccountsResize(1, e)} />
                    </Table.Head>
                    <Table.Head className="relative group pr-6">
                      项目 ID
                      <Table.ResizeHandle onMouseDown={(e) => startAccountsResize(2, e)} />
                    </Table.Head>
                    <Table.Head className="relative group pr-6">
                      谷歌邮箱
                      <Table.ResizeHandle onMouseDown={(e) => startAccountsResize(3, e)} />
                    </Table.Head>
                    <Table.Head className="text-center relative group pr-6">
                      连接状态
                      <Table.ResizeHandle onMouseDown={(e) => startAccountsResize(4, e)} />
                    </Table.Head>
                    <Table.Head className="text-center relative group pr-6">
                      冻结冷却
                      <Table.ResizeHandle onMouseDown={(e) => startAccountsResize(5, e)} />
                    </Table.Head>
                    <Table.Head className="text-center">操作</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {accountsLoading ? (
                    [...Array(3)].map((_, i) => (
                      <Table.Row key={i}>
                        <Table.Cell className="text-center"><SkeletonLine className="w-4 h-4 mx-auto" /></Table.Cell>
                        <Table.Cell><SkeletonLine className="w-24 h-4" /></Table.Cell>
                        <Table.Cell><SkeletonLine className="w-32 h-4" /></Table.Cell>
                        <Table.Cell><SkeletonLine className="w-40 h-4" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-12 h-4 mx-auto" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-16 h-4 mx-auto" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-20 h-4 mx-auto" /></Table.Cell>
                      </Table.Row>
                    ))
                  ) : accounts.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={7} className="p-8 text-center text-kumo-subtle">
                        暂无账号数据，请点击上方按钮新增授权。
                      </Table.Cell>
                    </Table.Row>
                  ) : (
                    accounts.map((account, index) => (
                      <Table.Row key={account.id} className="hover:bg-kumo-recessed/5">
                        <Table.Cell className="text-center text-kumo-subtle font-semibold">{index + 1}</Table.Cell>
                        <Table.Cell className="font-bold text-kumo-strong">{account.name || '未命名'}</Table.Cell>
                        <Table.Cell className="font-mono">{account.project_id || '-'}</Table.Cell>
                        <Table.Cell className="font-mono text-kumo-subtle">{maskEmail(account.email)}</Table.Cell>
                        <Table.Cell className="text-center">
                          <span
                            className={`px-2 py-0.5 rounded text-[10px] font-bold border ${
                              account.status === 'online'
                                ? 'bg-kumo-success/10 text-kumo-success border-kumo-success/20'
                                : account.status === 'error'
                                ? 'bg-kumo-danger/10 text-kumo-danger border-kumo-danger/20'
                                : 'bg-kumo-recessed text-kumo-subtle border-kumo-line'
                            }`}
                          >
                            {account.status === 'online' ? '在线' : account.status === 'error' ? '异常' : '未知'}
                          </span>
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          {!account.coolDowns || account.coolDowns.length === 0 ? (
                            <span className="px-2 py-0.5 rounded text-[10px] bg-kumo-success/10 text-kumo-success border border-kumo-success/20 font-semibold">
                              正常
                            </span>
                          ) : (
                            <span
                              className="px-2 py-0.5 rounded text-[10px] bg-yellow-500/10 text-yellow-600 border border-yellow-500/20 font-semibold cursor-pointer inline-flex items-center gap-0.5"
                              title={formatCoolDownTitle(account.coolDowns)}
                            >
                              <span>❄️ {account.coolDowns.length} 模型受限</span>
                            </span>
                          )}
                        </Table.Cell>
                        <Table.Cell>
                          <div className="flex justify-center gap-2">
                            <button
                              onClick={() => toggleAccountEnabled(account)}
                              className={`p-1.5 rounded hover:bg-kumo-recessed transition-colors ${
                                account.enable ? 'text-kumo-success' : 'text-kumo-subtle'
                              }`}
                              title={account.enable ? '禁用账号' : '启用账号'}
                            >
                              <Check className="w-4 h-4" />
                            </button>
                            <button
                              onClick={() => openEditAccountModal(account)}
                              className="p-1.5 rounded hover:bg-kumo-recessed text-kumo-subtle hover:text-kumo-strong transition-colors"
                              title="编辑"
                            >
                              <Edit className="w-4 h-4" />
                            </button>
                            <button
                              onClick={() => deleteAccount(account)}
                              className="p-1.5 rounded hover:bg-kumo-danger/10 text-kumo-subtle hover:text-kumo-danger transition-colors"
                              title="删除"
                            >
                              <Trash className="w-4 h-4" />
                            </button>
                          </div>
                        </Table.Cell>
                      </Table.Row>
                    ))
                  )}
                </Table.Body>
              </Table>
            </div>
          </div>

          {/* Quotas Overview Panel */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            <div className="p-4 border-b border-kumo-line flex justify-between items-center bg-kumo-recessed/10">
              <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-1.5">
                <PieChart className="w-4 h-4 text-kumo-brand" />
                各账号实时额度总览
              </h4>
              <Button onClick={() => loadQuotas(true)} disabled={quotaLoading} className="flex items-center gap-1 text-xs">
                <RefreshCw className={`w-3.5 h-3.5 ${quotaLoading ? 'animate-spin' : ''}`} />
                <span>{quotaLoading ? '正在刷新...' : '刷新额度'}</span>
              </Button>
            </div>

            <div className="overflow-x-auto">
              {quotaLoading && quotaData.length === 0 ? (
                <div className="text-center py-8 text-kumo-subtle">
                  <RotateCw className="w-5 h-5 animate-spin mx-auto" />
                </div>
              ) : quotaData.length > 0 ? (
                <Table>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head className="w-44">模型</Table.Head>
                      {quotaData.map((q) => (
                        <Table.Head key={q.accountId} className="text-center" style={{ minWidth: '120px' }}>
                          {q.accountName || q.accountId}
                        </Table.Head>
                      ))}
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {quotaModels.map((modelId) => (
                      <Table.Row key={modelId} className="hover:bg-kumo-recessed/5">
                        <Table.Cell className="font-mono font-semibold text-kumo-strong">{modelId}</Table.Cell>
                        {quotaData.map((q) => {
                          const bucket = getQuotaBucket(q, modelId);
                          if (!bucket) return <Table.Cell key={q.accountId} className="text-center text-kumo-subtle/30">-</Table.Cell>;
                          const inCooldown = isQuotaInCooldown(bucket);

                          return (
                            <Table.Cell key={q.accountId}>
                              <div className="flex flex-col items-center gap-1">
                                {inCooldown ? (
                                  <>
                                    <span className="px-1.5 py-0.2 rounded text-[9px] bg-kumo-danger/10 text-kumo-danger border border-kumo-danger/20 font-bold">
                                      ❄️ 冷却中
                                    </span>
                                    <span className="text-[9px] text-kumo-subtle">
                                      {formatQuotaResetTime(bucket.resetTime)}
                                    </span>
                                  </>
                                ) : (
                                  <>
                                    <div className="flex items-center gap-2 justify-center">
                                      <div className="w-16 h-2 bg-kumo-recessed rounded-full overflow-hidden border border-kumo-line">
                                        <div
                                          className="h-full rounded-full"
                                          style={{
                                            width: `${Math.round(bucket.remainingFraction * 100)}%`,
                                            backgroundColor: getQuotaBarColor(bucket.remainingFraction)
                                          }}
                                        />
                                      </div>
                                      <span className="font-bold text-[10px]" style={{ color: getQuotaBarColor(bucket.remainingFraction) }}>
                                        {Math.round(bucket.remainingFraction * 100)}%
                                      </span>
                                    </div>
                                    <span className="text-[9px] text-kumo-subtle/70">
                                      {formatQuotaResetTime(bucket.resetTime)}
                                    </span>
                                  </>
                                )}
                              </div>
                            </Table.Cell>
                          );
                        })}
                      </Table.Row>
                    ))}
                  </Table.Body>
                </Table>
              ) : (
                <div className="text-center py-6 text-kumo-subtle">
                  点击上方「刷新额度」加载各账号的实时剩余配额百分比
                </div>
              )}
            </div>
          </div>

          {/* Model Health Check History */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            <div className="p-4 border-b border-kumo-line flex flex-wrap justify-between items-center bg-kumo-recessed/10 gap-4">
              <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-1.5">
                <Activity className="w-4 h-4 text-kumo-brand" />
                模型连通性健康检测历史
              </h4>
              <div className="flex flex-wrap items-center gap-2.5 text-xs">
                {/* Auto check toggle */}
                <div className="flex items-center gap-2.5">
                  <Switch
                    checked={autoCheck}
                    onCheckedChange={handleToggleAutoCheck}
                    size="sm"
                  />
                  <span className="text-kumo-strong font-semibold">开启定时检测</span>
                  <select
                    value={autoCheckInterval}
                    onChange={(e) => handleIntervalChange(Number(e.target.value))}
                    className="bg-kumo-base text-kumo-strong border border-kumo-line rounded px-2 py-0.5 text-xs font-semibold"
                  >
                    <option value="1800000">30分钟</option>
                    <option value="3600000">1小时</option>
                    <option value="7200000">2小时</option>
                    <option value="14400000">4小时</option>
                  </select>
                </div>

                <div className="w-px h-4 bg-kumo-line" />

                <div className="flex gap-1.5">
                  <Button onClick={executeHealthCheck} disabled={checking} variant="primary">
                    <span>{checking ? '检测中...' : '执行检测'}</span>
                  </Button>
                  <Button onClick={loadCheckHistory}>
                    <RefreshCw className="w-3.5 h-3.5" />
                  </Button>
                  <Button onClick={clearCheckHistory} className="text-kumo-danger">
                    <Trash className="w-3.5 h-3.5" />
                  </Button>
                </div>
              </div>
            </div>

            <div className="overflow-x-auto">
              {checkHistory.models && checkHistory.models.length > 0 ? (
                <Table>
                  <Table.Header variant="compact">
                    <Table.Row>
                      <Table.Head className="w-52">模型</Table.Head>
                      {checkHistory.times.slice().reverse().map((time) => (
                        <Table.Head key={time} className="text-center" style={{ minWidth: '90px' }}>
                          {formatDateTime(time).slice(5, 16)}
                        </Table.Head>
                      ))}
                    </Table.Row>
                  </Table.Header>
                  <Table.Body>
                    {checkHistory.models.map((model) => {
                      const isDisabled = disabledCheckModels.includes(model);

                      return (
                        <Table.Row key={model} className="hover:bg-kumo-recessed/5">
                          <Table.Cell className="font-mono font-semibold">
                            <div className="flex items-center gap-2">
                              <Checkbox
                                checked={!isDisabled}
                                onCheckedChange={() => toggleCheckModel(model)}
                                aria-label={`检测 ${model}`}
                              />
                              <span className={isDisabled ? 'opacity-40 line-through' : 'text-kumo-strong'}>
                                {model}
                              </span>
                            </div>
                          </Table.Cell>
                          {checkHistory.times.slice().reverse().map((time) => {
                            const checkData = checkHistory.matrix[model]?.[time];
                            if (!checkData) {
                              return <Table.Cell key={time} className="text-center text-kumo-subtle/30">-</Table.Cell>;
                            }

                            return (
                              <Table.Cell key={time} className="text-center">
                                <div className="flex flex-wrap gap-1 justify-center">
                                  {accounts.map((acc, index) => {
                                    const accIdx = index + 1;
                                    const badgeClass = getCheckBadgeClass(checkData, accIdx);
                                    const badgeTitle = getCheckBadgeTitle(checkData, accIdx);

                                    return (
                                      <span
                                        key={accIdx}
                                        className={`w-5 h-5 rounded-md flex items-center justify-center text-[9px] font-bold select-none cursor-help ${badgeClass}`}
                                        title={badgeTitle}
                                      >
                                        {accIdx}
                                      </span>
                                    );
                                  })}
                                </div>
                              </Table.Cell>
                            );
                          })}
                        </Table.Row>
                      );
                    })}
                  </Table.Body>
                </Table>
              ) : (
                <div className="text-center py-8 text-kumo-subtle">
                  暂无检测记录。点击「执行检测」开始。
                </div>
              )}
            </div>
          </div>
        </div>
      )}

      {/* ==================== 3. 调用日志 Tab ==================== */}
      {activeTab === 'logs' && (
        <div className="quick-fade-in space-y-4">
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-4 flex flex-col md:flex-row justify-between items-start md:items-center gap-4">
            <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
              <History className="w-4 h-4 text-kumo-brand" />
              日志管理
            </h3>
            <div className="flex flex-wrap gap-2 text-xs">
              <select
                value={logFilterAccount}
                onChange={(e) => setLogFilterAccount(e.target.value)}
                className="bg-kumo-base text-kumo-strong border border-kumo-line rounded px-2 py-1 font-semibold focus:outline-none"
              >
                <option value="">全部账号</option>
                {accounts.filter(a => a.enable).map(a => (
                  <option key={a.id} value={a.id}>{a.name || a.id}</option>
                ))}
              </select>

              <select
                value={logFilterModel}
                onChange={(e) => setLogFilterModel(e.target.value)}
                className="bg-kumo-base text-kumo-strong border border-kumo-line rounded px-2 py-1 font-semibold focus:outline-none"
              >
                <option value="">全部模型</option>
                {logModelsList.map(m => (
                  <option key={m} value={m}>{m}</option>
                ))}
              </select>

              <Button onClick={() => loadLogs(true)} disabled={logsLoading} className="flex items-center gap-1">
                <RefreshCw className={`w-3.5 h-3.5 ${logsLoading ? 'animate-spin' : ''}`} />
                <span>刷新</span>
              </Button>
              <Button onClick={clearLogs} className="text-kumo-danger flex items-center gap-1">
                <Trash className="w-3.5 h-3.5" />
                <span>清空日志</span>
              </Button>
            </div>
          </div>

          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            <div className="overflow-x-auto">
              <Table layout="fixed">
                <colgroup>
                  {logsColWidths.map((w, idx) => (
                    <col key={idx} style={{ width: w }} />
                  ))}
                </colgroup>
                <Table.Header variant="compact">
                  <Table.Row>
                    <Table.Head className="text-center relative group pr-6">
                      调用时间
                      <Table.ResizeHandle onMouseDown={(e) => startLogsResize(0, e)} />
                    </Table.Head>
                    <Table.Head className="relative group pr-6">
                      账号备注
                      <Table.ResizeHandle onMouseDown={(e) => startLogsResize(1, e)} />
                    </Table.Head>
                    <Table.Head className="text-center relative group pr-6">
                      调用模型
                      <Table.ResizeHandle onMouseDown={(e) => startLogsResize(2, e)} />
                    </Table.Head>
                    <Table.Head className="relative group pr-6">
                      接口路径
                      <Table.ResizeHandle onMouseDown={(e) => startLogsResize(3, e)} />
                    </Table.Head>
                    <Table.Head className="text-center relative group pr-6">
                      状态
                      <Table.ResizeHandle onMouseDown={(e) => startLogsResize(4, e)} />
                    </Table.Head>
                    <Table.Head className="text-center relative group pr-6">
                      耗时
                      <Table.ResizeHandle onMouseDown={(e) => startLogsResize(5, e)} />
                    </Table.Head>
                    <Table.Head className="text-center relative group pr-6">
                      首字延迟
                      <Table.ResizeHandle onMouseDown={(e) => startLogsResize(6, e)} />
                    </Table.Head>
                    <Table.Head className="text-center w-16">详情</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {logsLoading ? (
                    [...Array(5)].map((_, i) => (
                      <Table.Row key={i}>
                        <Table.Cell className="text-center"><SkeletonLine className="w-24 h-4 mx-auto" /></Table.Cell>
                        <Table.Cell><SkeletonLine className="w-20 h-4" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-24 h-4 mx-auto" /></Table.Cell>
                        <Table.Cell><SkeletonLine className="w-40 h-4" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-12 h-4 mx-auto" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-16 h-4 mx-auto" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-16 h-4 mx-auto" /></Table.Cell>
                        <Table.Cell className="text-center"><SkeletonLine className="w-8 h-4 mx-auto" /></Table.Cell>
                      </Table.Row>
                    ))
                  ) : filteredLogs.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={8} className="p-10 text-center text-kumo-subtle">
                        暂无匹配的调用日志记录。
                      </Table.Cell>
                    </Table.Row>
                  ) : (
                    filteredLogs.map((log) => (
                      <Table.Row key={log.id} className="hover:bg-kumo-recessed/5">
                        <Table.Cell className="text-center text-kumo-subtle font-mono">{formatDateTime(log.timestamp)}</Table.Cell>
                        <Table.Cell className="font-bold text-kumo-strong">
                          <div className="flex items-center gap-1">
                            <span>{log.accountName || log.accountId}</span>
                            {log.isBalanced && (
                              <span className="px-1 bg-kumo-brand/10 text-kumo-brand text-[8px] font-bold rounded">LB</span>
                            )}
                          </div>
                        </Table.Cell>
                        <Table.Cell className="text-center font-mono text-kumo-subtle">{log.model || '-'}</Table.Cell>
                        <Table.Cell>
                          <div className="flex items-center gap-1.5 font-mono">
                            <span className="px-1 py-0.2 rounded text-[9px] bg-kumo-brand/10 text-kumo-brand border border-kumo-brand/20 font-bold uppercase">
                              {log.method || 'POST'}
                            </span>
                            <span className="truncate max-w-[200px] text-kumo-strong" title={log.path}>{log.path}</span>
                          </div>
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <span className={`px-2 py-0.5 rounded text-[10px] font-bold ${getLogStatusClass(log.statusCode)}`}>
                            {log.statusCode || '-'}
                          </span>
                        </Table.Cell>
                        <Table.Cell className="text-center font-mono font-semibold text-kumo-strong">{log.durationMs || 0}ms</Table.Cell>
                        <Table.Cell className="text-center font-mono text-kumo-success">
                          {log.firstTokenTimeMs != null ? `${log.firstTokenTimeMs}ms` : '-'}
                        </Table.Cell>
                        <Table.Cell className="text-center">
                          <button
                            onClick={() => viewLogDetail(log)}
                            className="p-1 hover:bg-kumo-recessed rounded text-kumo-subtle hover:text-kumo-strong transition-colors"
                          >
                            <Eye className="w-4 h-4" />
                          </button>
                        </Table.Cell>
                      </Table.Row>
                    ))
                  )}
                </Table.Body>
              </Table>
            </div>
          </div>
        </div>
      )}

      {/* ==================== 4. 模块配置 Tab ==================== */}
      {activeTab === 'settings' && (
        <div className="quick-fade-in space-y-6">
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            <div className="p-4 border-b border-kumo-line flex justify-between items-center bg-kumo-recessed/10">
              <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
                <Sliders className="w-4 h-4 text-kumo-brand" />
                GCLI 全局路由策略与生成参数配置
              </h3>
              <div className="flex gap-2">
                <Button onClick={() => loadSettings(true)}>
                  <RefreshCw className="w-3.5 h-3.5" />
                </Button>
                <Button variant="primary" onClick={saveSettings} disabled={settingsSaving}>
                  <span>{settingsSaving ? '保存中...' : '保存配置'}</span>
                </Button>
              </div>
            </div>

            <div className="p-5 space-y-6">
              {/* Generation Parameters */}
              <div className="space-y-4">
                <h4 className="text-xs font-bold text-kumo-brand uppercase border-b border-kumo-line pb-1">
                  1. 生成与控制参数
                </h4>
                <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                  <div className="space-y-1">
                    <label className="text-xs font-semibold text-kumo-strong">默认温度 (Temperature)</label>
                    <input
                      type="number"
                      step={0.1}
                      min={0}
                      max={2}
                      value={settingsForm.DEFAULT_TEMPERATURE}
                      onChange={(e) => setSettingsForm({ ...settingsForm, DEFAULT_TEMPERATURE: Number(e.target.value) })}
                      className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-xs font-semibold text-kumo-strong">默认 top_p</label>
                    <input
                      type="number"
                      step={0.01}
                      min={0}
                      max={1}
                      value={settingsForm.DEFAULT_TOP_P}
                      onChange={(e) => setSettingsForm({ ...settingsForm, DEFAULT_TOP_P: Number(e.target.value) })}
                      className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-xs font-semibold text-kumo-strong">默认 top_k</label>
                    <input
                      type="number"
                      value={settingsForm.DEFAULT_TOP_K}
                      onChange={(e) => setSettingsForm({ ...settingsForm, DEFAULT_TOP_K: Number(e.target.value) })}
                      className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-xs font-semibold text-kumo-strong">默认最大 Tokens</label>
                    <input
                      type="number"
                      value={settingsForm.DEFAULT_MAX_TOKENS}
                      onChange={(e) => setSettingsForm({ ...settingsForm, DEFAULT_MAX_TOKENS: Number(e.target.value) })}
                      className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono"
                    />
                  </div>
                </div>
              </div>

              {/* Safety and load limit */}
              <div className="space-y-4">
                <h4 className="text-xs font-bold text-kumo-brand uppercase border-b border-kumo-line pb-1">
                  2. 负载与配额限制
                </h4>
                <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
                  <div className="space-y-1">
                    <label className="text-xs font-semibold text-kumo-strong">每小时调用次数上限</label>
                    <input
                      type="number"
                      value={settingsForm.CREDENTIAL_MAX_USAGE_PER_HOUR}
                      onChange={(e) => setSettingsForm({ ...settingsForm, CREDENTIAL_MAX_USAGE_PER_HOUR: Number(e.target.value) })}
                      className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-xs font-semibold text-kumo-strong">请求超时时间 (s)</label>
                    <input
                      type="number"
                      value={settingsForm.TIMEOUT}
                      onChange={(e) => setSettingsForm({ ...settingsForm, TIMEOUT: Number(e.target.value) })}
                      className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-xs font-semibold text-kumo-strong">调用日志保留天数</label>
                    <input
                      type="number"
                      value={settingsForm.LOG_RETENTION_DAYS}
                      onChange={(e) => setSettingsForm({ ...settingsForm, LOG_RETENTION_DAYS: Number(e.target.value) })}
                      className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand"
                    />
                  </div>
                </div>
              </div>

              {/* Model Redirect (Aliases) */}
              <div className="space-y-4">
                <h4 className="text-xs font-bold text-kumo-brand uppercase border-b border-kumo-line pb-1">
                  3. 模型重定向与别名路由 (Model Redirects)
                </h4>
                <p className="text-[10px] text-kumo-subtle">
                  您可以将外部系统调用的模型名称重定向为底层的真实部署模型（如将 <code>gpt-4o</code> 路由到真实的 <code>gemini-1.5-pro</code>）。
                </p>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  {redirects.length === 0 ? (
                    <div className="col-span-2 text-center py-4 border border-dashed border-kumo-line rounded-lg text-kumo-subtle text-xs">
                      暂无路由别名重定向规则
                    </div>
                  ) : (
                    redirects.map((r) => {
                      const isEditing = editingRedirectSource === r.source_model;

                      return (
                        <div
                          key={r.source_model}
                          className="flex justify-between items-center p-3 bg-kumo-recessed/30 border border-kumo-line rounded-lg text-xs"
                        >
                          <div className="flex items-center gap-2 min-w-0 flex-1">
                            {isEditing ? (
                              <div className="flex items-center gap-1.5 flex-1 min-w-0">
                                <input
                                  type="text"
                                  value={newRedirectSource}
                                  onChange={(e) => setNewRedirectSource(e.target.value)}
                                  className="w-1/2 bg-kumo-base text-kumo-strong px-2 py-0.5 border border-kumo-line rounded"
                                />
                                <ArrowRight className="w-3.5 h-3.5 text-kumo-subtle flex-shrink-0" />
                                <input
                                  type="text"
                                  value={newRedirectTarget}
                                  onChange={(e) => setNewRedirectTarget(e.target.value)}
                                  className="w-1/2 bg-kumo-base text-kumo-strong px-2 py-0.5 border border-kumo-line rounded"
                                />
                              </div>
                            ) : (
                              <>
                                <span className="bg-kumo-brand/10 border border-kumo-brand/20 px-2 py-0.5 rounded text-kumo-brand font-mono font-semibold">
                                  {r.source_model}
                                </span>
                                <ArrowRight className="w-3.5 h-3.5 text-kumo-subtle" />
                                <span className="font-mono text-kumo-strong font-semibold truncate">
                                  {r.target_model}
                                </span>
                              </>
                            )}
                          </div>

                          <div className="flex gap-1 ml-3">
                            {isEditing ? (
                              <>
                                <button
                                  onClick={() => addRedirectRule(newRedirectSource, newRedirectTarget)}
                                  className="p-1 bg-kumo-success/15 hover:bg-kumo-success/25 rounded text-kumo-success"
                                  title="确认保存"
                                >
                                  <Check className="w-3.5 h-3.5" />
                                </button>
                                <button
                                  onClick={() => setEditingRedirectSource(null)}
                                  className="p-1 bg-kumo-recessed rounded text-kumo-subtle"
                                  title="取消"
                                >
                                  <X className="w-3.5 h-3.5" />
                                </button>
                              </>
                            ) : (
                              <>
                                <button
                                  onClick={() => {
                                    setEditingRedirectSource(r.source_model);
                                    setNewRedirectSource(r.source_model);
                                    setNewRedirectTarget(r.target_model);
                                  }}
                                  className="p-1 hover:bg-kumo-recessed rounded text-kumo-subtle"
                                  title="编辑"
                                >
                                  <Edit className="w-3.5 h-3.5" />
                                </button>
                                <button
                                  onClick={() => deleteRedirectRule(r.source_model)}
                                  className="p-1 hover:bg-kumo-danger/10 rounded text-kumo-subtle hover:text-kumo-danger"
                                  title="删除"
                                >
                                  <Trash className="w-3.5 h-3.5" />
                                </button>
                              </>
                            )}
                          </div>
                        </div>
                      );
                    })
                  )}
                </div>

                {/* Form to add redirection */}
                {!editingRedirectSource && (
                  <div className="flex flex-wrap gap-2.5 items-center p-4 bg-kumo-brand/5 border border-dashed border-kumo-brand/20 rounded-lg">
                    <input
                      type="text"
                      placeholder="源模型名称 (e.g. gpt-4o)"
                      value={newRedirectSource}
                      onChange={(e) => setNewRedirectSource(e.target.value)}
                      className="bg-kumo-base text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand flex-1 min-w-[140px] font-mono"
                    />
                    <ArrowRight className="w-4 h-4 text-kumo-subtle" />
                    <input
                      type="text"
                      placeholder="真实路由模型 (e.g. gemini-1.5-pro)"
                      value={newRedirectTarget}
                      onChange={(e) => setNewRedirectTarget(e.target.value)}
                      className="bg-kumo-base text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand flex-1 min-w-[140px] font-mono"
                    />
                    <Button onClick={() => addRedirectRule(newRedirectSource, newRedirectTarget)} className="flex items-center gap-1.5">
                      <Plus className="w-3.5 h-3.5" />
                      <span>新增重定向</span>
                    </Button>
                  </div>
                )}
              </div>
            </div>
          </div>

          {/* API Access guide panel */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-5 space-y-4">
            <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-2">
              <Plug className="w-4 h-4 text-kumo-brand" />
              API 统一接入指引
            </h4>

            <div className="space-y-3 text-xs leading-relaxed text-kumo-strong">
              <div className="border border-kumo-line rounded-lg overflow-hidden">
                <div className="p-2.5 bg-kumo-recessed/40 font-bold border-b border-kumo-line">Base URL</div>
                <div
                  onClick={copyEndpoint}
                  className="p-3 bg-kumo-recessed/25 font-mono text-[11px] text-kumo-brand flex items-center justify-between cursor-pointer group"
                >
                  <span>{getBaseUrl()}</span>
                  <Copy className="w-4 h-4 opacity-50 group-hover:opacity-100 transition-opacity" />
                </div>
              </div>

              <div className="border border-kumo-line rounded-lg overflow-hidden">
                <div className="p-2.5 bg-kumo-recessed/40 font-bold border-b border-kumo-line">可用端点</div>
                <div className="p-3 space-y-1 bg-kumo-recessed/25 font-mono text-[11px]">
                  <div><span className="text-kumo-brand font-bold mr-2">POST</span>/v1/chat/completions (兼容 OpenAI SDK)</div>
                  <div><span className="text-kumo-brand font-bold mr-2">GET</span>/v1/models (获取模型矩阵列表)</div>
                </div>
              </div>

              <div className="border border-kumo-line rounded-lg overflow-hidden">
                <div className="p-2.5 bg-kumo-recessed/40 font-bold border-b border-kumo-line">Curl 调用示例</div>
                <pre className="p-3 bg-kumo-recessed/25 text-[10px] text-kumo-subtle overflow-x-auto font-mono whitespace-pre leading-relaxed">
{`curl ${getBaseUrl()}/chat/completions \\
  -H "Authorization: Bearer ${settingsForm.API_KEY || 'YOUR_API_KEY'}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "gemini-2.5-flash",
    "messages": [{"role": "user", "content": "你好，请用一句话自我介绍。"}],
    "stream": true
  }'`}
                </pre>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ==================== dialogs & modals ==================== */}

      {/* 1. Account Add/Edit Dialog */}
      <Dialog.Root open={accountFormOpen} onOpenChange={setAccountFormOpen}>
        <Dialog className="p-6 sm:max-w-md bg-kumo-base border border-kumo-line rounded-lg shadow-lg">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            {editingAccount ? '编辑 Gemini 账号凭证' : '手动添加 Gemini 账号'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            手动配置 Google Cloud Application Client 凭证及 Refresh Token。
          </Dialog.Description>

          <div className="space-y-4">
            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">备注名称</label>
              <input
                type="text"
                value={accountForm.name}
                onChange={(e) => setAccountForm({ ...accountForm, name: e.target.value })}
                placeholder="例如：主账号-开发环境"
                className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand"
              />
            </div>

            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">OAuth Client ID</label>
              <input
                type="text"
                value={accountForm.client_id}
                onChange={(e) => setAccountForm({ ...accountForm, client_id: e.target.value })}
                placeholder="Google OAuth Client ID"
                className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono"
              />
            </div>

            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">OAuth Client Secret</label>
              <input
                type="password"
                value={accountForm.client_secret}
                onChange={(e) => setAccountForm({ ...accountForm, client_secret: e.target.value })}
                placeholder="Google OAuth Client Secret"
                className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono"
              />
            </div>

            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">OAuth Refresh Token</label>
              <input
                type="password"
                value={accountForm.refresh_token}
                onChange={(e) => setAccountForm({ ...accountForm, refresh_token: e.target.value })}
                placeholder="刷新 Token"
                className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono"
              />
            </div>

            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">谷歌邮箱 (Email)</label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={accountForm.email}
                  onChange={(e) => setAccountForm({ ...accountForm, email: e.target.value })}
                  placeholder="关联的 Google 邮箱，可点击右侧按钮自动获取"
                  className="flex-1 bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono"
                />
                <Button onClick={fetchEmailInfo} disabled={accountSaving} className="text-xs">
                  获取邮箱
                </Button>
              </div>
            </div>

            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">绑定 Project ID</label>
              <input
                type="text"
                value={accountForm.project_id}
                onChange={(e) => setAccountForm({ ...accountForm, project_id: e.target.value })}
                placeholder="留空自动检测或指定特定的谷歌项目ID"
                className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono"
              />
            </div>

            {accountFormError && (
              <p className="text-xs text-kumo-danger font-semibold">{accountFormError}</p>
            )}

            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close asChild>
  <Button>取消</Button>
</Dialog.Close>
              <Button variant="primary" disabled={accountSaving} onClick={saveAccount}>
                {accountSaving ? '保存中...' : '确认保存'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 2. Log Detail Dialog */}
      <Dialog.Root open={logDetailOpen} onOpenChange={setLogDetailOpen}>
        <Dialog className="p-0 sm:max-w-xl bg-kumo-base border border-kumo-line rounded-lg shadow-lg overflow-hidden flex flex-col max-h-[85vh]">
          {/* Header */}
          <div className="p-4 border-b border-kumo-line flex items-center justify-between">
            <h3 className="text-sm font-bold text-kumo-strong">调用日志详细分析</h3>
            <button onClick={() => setLogDetailOpen(false)} className="p-1 hover:bg-kumo-recessed rounded text-kumo-subtle">
              <X className="w-4 h-4" />
            </button>
          </div>

          {/* Details Body */}
          {logDetail && (
            <div className="flex-1 overflow-y-auto p-4 space-y-4 text-xs leading-relaxed max-h-[60vh]">
              {/* Metadata details */}
              <div className="grid grid-cols-2 gap-3 bg-kumo-recessed/35 p-3 rounded-lg border border-kumo-line font-mono text-[10px]">
                <div>
                  <span className="text-kumo-subtle font-bold block">请求时间</span>
                  <span className="text-kumo-strong font-semibold">{formatDateTime(logDetail.timestamp)}</span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">状态状态</span>
                  <span className={`px-1.5 rounded font-bold ${getLogStatusClass(logDetail.statusCode)}`}>
                    {logDetail.statusCode}
                  </span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">花费延迟</span>
                  <span className="text-kumo-strong font-bold">{logDetail.durationMs}ms</span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">首字响应</span>
                  <span className="text-kumo-strong font-bold">
                    {logDetail.firstTokenTimeMs != null ? `${logDetail.firstTokenTimeMs}ms` : '-'}
                  </span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">请求账号</span>
                  <span className="text-kumo-strong font-semibold">{logDetail.accountName || logDetail.accountId}</span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">接口 IP / UserAgent</span>
                  <span className="text-kumo-strong font-semibold truncate block" title={logDetail.userAgent}>
                    {logDetail.clientIp || '-'} ({logDetail.userAgent ? logDetail.userAgent.split(' ')[0] : 'Unknown'})
                  </span>
                </div>
              </div>

              {/* Mode Raw toggle */}
              <div className="flex justify-between items-center bg-kumo-recessed/10 p-2 border border-kumo-line rounded-lg">
                <span className="font-bold text-kumo-strong">以原始 JSON 视图显示</span>
                <Switch
                  checked={logDetailShowRaw}
                  onCheckedChange={logDetailShowRaw => setLogDetailShowRaw(logDetailShowRaw)}
                  size="sm"
                />
              </div>

              {logDetailShowRaw ? (
                <pre className="p-3 bg-kumo-recessed border border-kumo-line rounded-lg text-[10px] text-kumo-strong overflow-x-auto font-mono whitespace-pre">
                  {JSON.stringify(logDetail.detail || logDetail, null, 2)}
                </pre>
              ) : (
                <div className="space-y-4">
                  {/* Messages flow (OpenAI Spec format) */}
                  {logDetail.detail?.messages && (
                    <div className="space-y-3">
                      <h4 className="font-bold text-kumo-strong">请求对话历史 (Messages)</h4>
                      <div className="space-y-2 max-h-48 overflow-y-auto pr-1">
                        {logDetail.detail.messages.map((m, idx) => (
                          <div key={idx} className="p-2.5 rounded border border-kumo-line bg-kumo-recessed/20">
                            <span className="font-bold text-kumo-brand uppercase block text-[9px] mb-1">
                              {m.role}
                            </span>
                            <p className="whitespace-pre-wrap text-[11px] text-kumo-strong">{m.content}</p>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* Choices outputs */}
                  {logDetail.detail?.response?.choices && (
                    <div className="space-y-2">
                      <h4 className="font-bold text-kumo-strong">最终生成响应 (Choices Response)</h4>
                      {logDetail.detail.response.choices.map((c, idx) => (
                        <div key={idx} className="p-3 rounded border border-kumo-line bg-kumo-recessed/40">
                          {c.message?.reasoning_content && (
                            <div className="mb-2 p-2 bg-yellow-500/10 border-l-2 border-yellow-500 rounded font-mono text-[10px] text-kumo-strong whitespace-pre-wrap">
                              <span className="font-bold block text-yellow-600 mb-0.5">Thinking Process:</span>
                              {c.message.reasoning_content}
                            </div>
                          )}
                          <p className="whitespace-pre-wrap text-[11px] text-kumo-strong font-mono">
                            {c.message?.content || '[未获取到数据]'}
                          </p>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>
          )}

          <div className="p-4 border-t border-kumo-line flex justify-end bg-kumo-recessed/20">
            <Button variant="primary" onClick={() => setLogDetailOpen(false)}>
              关闭详情
            </Button>
          </div>
        </Dialog>
      </Dialog.Root>
    </div>
  );
}

export default GeminiCliPage;
