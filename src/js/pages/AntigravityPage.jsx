import React, { useState, useEffect, useCallback, useMemo, useRef } from 'react';
import { toast } from '../modules/toast.js';
import { Button } from '@cloudflare/kumo/components/button';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { formatDateTime } from '../modules/utils.js';
import {
  Cpu,
  Users,
  History,
  Settings as SettingsIcon,
  Plus,
  Trash,
  RotateCw,
  Search,
  Upload,
  Download,
  Edit,
  X,
  Check,
  Eye,
  Globe,
  Sliders,
  Plug,
  Copy,
  ArrowRight,
  TrendingUp,
  Database,
  Activity,
  AlertTriangle,
  Lock,
  MessageSquare,
  PieChart,
  Heart,
  Grid,
  Google,
  Settings,
  ChevronDown
} from '../components/Icons.jsx';

function AntigravityPage() {
  const [activeTab, setActiveTab] = useState('quotas'); // 'quotas' | 'matrix' | 'accounts' | 'logs' | 'settings'

  // Authentication helper
  const getAuthHeaders = useCallback(() => {
    const password = localStorage.getItem('admin_password') || '';
    return {
      'Content-Type': 'application/json',
      'x-admin-password': password,
    };
  }, []);

  // Time counting for countdowns
  const [currentTime, setCurrentTime] = useState(new Date());
  useEffect(() => {
    const timer = setInterval(() => setCurrentTime(new Date()), 10000);
    return () => clearInterval(timer);
  }, []);

  // Format countdown text helper
  const formatResetCountdown = (isoTime) => {
    if (!isoTime) return '无';
    try {
      const resetDate = new Date(isoTime);
      if (isNaN(resetDate.getTime())) return '无';

      const diffMs = resetDate - currentTime;
      if (diffMs <= 0) return '已重置';

      const totalMinutes = Math.floor(diffMs / (1000 * 60));
      const totalHours = Math.floor(totalMinutes / 60);
      const remainMinutes = totalMinutes % 60;

      if (totalHours >= 24) {
        const days = Math.floor(totalHours / 24);
        const remainHours = totalHours % 24;
        return `${days}天${remainHours}时`;
      }
      if (totalHours > 0) {
        return `${totalHours}时${remainMinutes}分`;
      }
      return `${remainMinutes}分`;
    } catch (e) {
      return '无';
    }
  };

  const formatDisplayDate = (isoTime) => {
    if (!isoTime) return '无';
    try {
      const date = new Date(isoTime);
      if (isNaN(date.getTime())) return isoTime;
      return date
        .toLocaleString('zh-CN', {
          month: '2-digit',
          day: '2-digit',
          hour: '2-digit',
          minute: '2-digit',
          hour12: false,
        })
        .replace(/\//g, '-');
    } catch (e) {
      return isoTime;
    }
  };

  // Color helper
  const getAgQuotaColor = (percent) => {
    if (percent > 40) return '#10b981'; // Green
    if (percent > 10) return '#f59e0b'; // Amber
    return '#ef4444'; // Red
  };

  // ==================== State definitions ====================
  const [accounts, setAccounts] = useState([]);
  const [stats, setStats] = useState(null);
  const [accountsLoading, setAccountsLoading] = useState(false);
  const [agRefreshingAll, setAgRefreshingAll] = useState(false);

  // Load Accounts & Quotas
  const loadAccounts = useCallback(async (showFeedback = false) => {
    setAccountsLoading(true);
    try {
      const response = await fetch('/api/antigravity/accounts', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        setAccounts(data);
        if (showFeedback) {
          toast.success('账号列表已刷新');
        }
        // Load miniature quota info for each online account
        loadAllAccountQuotas(data);
      }
    } catch (error) {
      console.error('加载 Antigravity 账号失败:', error);
    } finally {
      setAccountsLoading(false);
    }
  }, [getAuthHeaders]);

  const loadAllAccountQuotas = async (accList) => {
    const KEY_MODELS = [
      'gemini-3-pro-high',
      'gemini-3-flash',
      'gemini-3-pro-image',
      'claude-sonnet-4-5',
    ];
    const updated = [...accList];

    for (let i = 0; i < updated.length; i++) {
      const account = updated[i];
      if (account.status !== 'online') continue;
      try {
        const response = await fetch(`/api/antigravity/accounts/${account.id}/quotas`, {
          headers: getAuthHeaders(),
        });
        if (response.ok) {
          const quotaData = await response.json();
          const quotas = {};
          for (const groupId of Object.keys(quotaData)) {
            const group = quotaData[groupId];
            if (!group.models) continue;
            for (const model of group.models) {
              if (KEY_MODELS.includes(model.id)) {
                quotas[model.id] = { percent: model.remaining || 0 };
              }
            }
          }
          account.quotas = quotas;
        }
      } catch (error) {
        console.error(`加载账号 ${account.name} 简要额度失败:`, error);
      }
    }
    setAccounts([...updated]);
  };

  // refresh all accounts from server
  const refreshAllAgAccounts = async () => {
    setAgRefreshingAll(true);
    toast.info('正在刷新所有凭证及邮箱信息...');
    try {
      const response = await fetch('/api/antigravity/accounts/refresh-all', {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (response.ok) {
        const s = data.success_count ?? data.refreshed ?? 0;
        const f = data.fail_count ?? data.failed ?? 0;
        toast.success(`同步完成: 成功 ${s}, 失败 ${f}`);
        loadAccounts(false);
      } else {
        toast.error(data.error || '刷新失败');
      }
    } catch (error) {
      toast.error('请求刷新失败: ' + error.message);
    } finally {
      setAgRefreshingAll(false);
    }
  };

  const toggleAccountEnabled = async (id, currentVal) => {
    try {
      const response = await fetch(`/api/antigravity/accounts/${id}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enable: !currentVal }),
      });
      if (response.ok) {
        toast.success(!currentVal ? '账号已启用' : '账号已停用');
        loadAccounts(false);
      }
    } catch (e) {
      toast.error('操作失败');
    }
  };

  const deleteAccount = async (id, name) => {
    if (!window.confirm(`确定要删除账号 "${name}" 吗？`)) return;
    try {
      const response = await fetch(`/api/antigravity/accounts/${id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('账号已安全删除');
        loadAccounts(false);
      } else {
        toast.error('删除账号失败');
      }
    } catch (error) {
      toast.error('请求异常: ' + error.message);
    }
  };

  // Manual Add Modal & Edit Config modal
  const [manualAddOpen, setManualAddOpen] = useState(false);
  const [manualForm, setManualForm] = useState({
    name: '',
    accessToken: '',
    refreshToken: '',
    projectId: '',
    expiresIn: 3599,
  });
  const [manualSaving, setManualSaving] = useState(false);

  const [accountFormOpen, setAccountFormOpen] = useState(false);
  const [editingAccount, setEditingAccount] = useState(null);
  const [accountForm, setAccountForm] = useState({
    name: '',
    email: '',
    password: '',
  });
  const [accountSaving, setAccountSaving] = useState(false);

  const openManualModal = () => {
    setManualForm({
      name: '',
      accessToken: '',
      refreshToken: '',
      projectId: '',
      expiresIn: 3599,
    });
    setManualAddOpen(true);
  };

  const saveManualAccount = async () => {
    if (!manualForm.accessToken || !manualForm.refreshToken) {
      toast.warning('Access Token 和 Refresh Token 均为必填项');
      return;
    }
    setManualSaving(true);
    try {
      const response = await fetch('/api/antigravity/accounts/manual', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(manualForm),
      });
      const data = await response.json();
      if (response.ok) {
        toast.success('手动凭证添加成功');
        setManualAddOpen(false);
        loadAccounts(false);
      } else {
        toast.error(data.error || '添加失败');
      }
    } catch (e) {
      toast.error('请求失败: ' + e.message);
    } finally {
      setManualSaving(false);
    }
  };

  const openEditAccountModal = (account) => {
    setEditingAccount(account);
    setAccountForm({
      name: account.name || '',
      email: account.email || '',
      password: account.password || '',
    });
    setAccountFormOpen(true);
  };

  const saveEditingAccount = async () => {
    if (!accountForm.name) {
      toast.warning('请填写账号名称');
      return;
    }
    setAccountSaving(true);
    try {
      const url = editingAccount
        ? `/api/antigravity/accounts/${editingAccount.id}`
        : '/api/antigravity/accounts';

      const response = await fetch(url, {
        method: editingAccount ? 'PUT' : 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(accountForm),
      });

      if (response.ok) {
        toast.success(editingAccount ? '账号配置已更新' : '账号已添加');
        setAccountFormOpen(false);
        loadAccounts(false);
      } else {
        const err = await response.json();
        toast.error(err.error || '保存失败');
      }
    } catch (e) {
      toast.error('请求失败: ' + e.message);
    } finally {
      setAccountSaving(false);
    }
  };

  // Google OAuth flow
  const [showOAuthExpand, setShowOAuthExpand] = useState(false);
  const [agOauthUrl, setAgOauthUrl] = useState('');
  const [agCustomProjectId, setAgCustomProjectId] = useState('');
  const [agAllowRandomProjectId, setAgAllowRandomProjectId] = useState(false);
  const [oauthVerifying, setOauthVerifying] = useState(false);

  const getGoogleAuthUrl = async () => {
    try {
      const response = await fetch('/api/antigravity/oauth/url', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.url) {
        window.open(data.url, '_blank');
      }
    } catch (error) {
      toast.error('获取授权链接失败');
    }
  };

  const verifyOauthUrl = async () => {
    if (!agOauthUrl.trim()) return;
    setOauthVerifying(true);
    try {
      const response = await fetch('/api/antigravity/oauth/parse-url', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          url: agOauthUrl,
          customProjectId: agCustomProjectId,
          allowRandomProjectId: agAllowRandomProjectId,
        }),
      });
      const data = await response.json();
      if (response.ok) {
        toast.success('账号 OAuth 授权成功');
        setAgOauthUrl('');
        setShowOAuthExpand(false);
        loadAccounts(false);
      } else {
        toast.error(data.error || '授权验证失败');
      }
    } catch (error) {
      toast.error('请求异常: ' + error.message);
    } finally {
      setOauthVerifying(false);
    }
  };

  // Account Import / Export
  const exportAccounts = async () => {
    try {
      const response = await fetch('/api/antigravity/accounts/export', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.error) {
        toast.error('导出失败: ' + data.error);
        return;
      }
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `antigravity-accounts-${new Date().toISOString().slice(0, 10)}.json`;
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
      toast.success(`已成功导出 ${data.accounts?.length || 0} 个账号`);
    } catch (e) {
      toast.error('导出请求异常: ' + e.message);
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
          toast.error('无效的文件数据格式');
          return;
        }

        toast.info(`正在导入 ${data.accounts.length} 个凭证...`);
        const response = await fetch('/api/antigravity/accounts/import', {
          method: 'POST',
          headers: getAuthHeaders(),
          body: JSON.stringify({ accounts: data.accounts }),
        });
        const result = await response.json();
        if (result.success) {
          toast.success(`导入成功: ${result.imported} 个, 跳过 ${result.skipped || 0} 个`);
          loadAccounts(false);
        } else {
          toast.error('导入失败: ' + result.error);
        }
      } catch (err) {
        toast.error('导入出错: ' + err.message);
      }
    };
    input.click();
  };

  const getAccountQuotaDisplay = (quotas) => {
    const DISPLAY_MAP = {
      'gemini-3-pro-high': 'G3 Pro',
      'gemini-3-flash': 'G3 Flash',
      'gemini-3-pro-image': 'G3 Image',
      'claude-sonnet-4-5': 'Claude 4.5',
    };
    const result = [];
    for (const [modelId, label] of Object.entries(DISPLAY_MAP)) {
      if (quotas && quotas[modelId]) {
        result.push({
          key: modelId,
          label: label,
          percent: quotas[modelId].percent || 0,
        });
      } else {
        result.push({
          key: modelId,
          label: label,
          percent: 0,
        });
      }
    }
    return result;
  };

  // ==================== Quota State & Logic ====================
  const [selectedAccountId, setSelectedAccountId] = useState('');
  const [quotaViewMode, setQuotaViewMode] = useState('grouped'); // 'grouped' | 'list'
  const [quotas, setQuotas] = useState({});
  const [quotasLoading, setQuotasLoading] = useState(false);
  const [quotasLastUpdated, setQuotasLastUpdated] = useState('');
  const quotaTimerRef = useRef(null);

  const loadQuotas = useCallback(async (isAuto = false) => {
    if (!isAuto) setQuotasLoading(true);
    try {
      let url = '/api/antigravity/quotas';
      if (selectedAccountId) {
        url = `/api/antigravity/accounts/${selectedAccountId}/quotas`;
      }
      const response = await fetch(url, { headers: getAuthHeaders() });
      const data = await response.json();
      setQuotas(data);
      setQuotasLastUpdated(new Date().toLocaleString('zh-CN'));
      if (!isAuto) {
        toast.success('配额额度已刷新');
      }
    } catch (e) {
      if (!isAuto) toast.error('获取额度失败');
    } finally {
      if (!isAuto) setQuotasLoading(false);
    }
  }, [selectedAccountId, getAuthHeaders]);

  // Handle selected account default
  useEffect(() => {
    if (accounts.length > 0 && !selectedAccountId) {
      const firstOnline = accounts.find(a => a.status === 'online');
      setSelectedAccountId(firstOnline ? firstOnline.id : accounts[0].id);
    }
  }, [accounts, selectedAccountId]);

  // Trigger loading quotas when account changes
  useEffect(() => {
    if (activeTab === 'quotas') {
      loadQuotas(false);
    }
  }, [selectedAccountId, activeTab, loadQuotas]);

  // Set up auto polling for quotas (30 seconds)
  useEffect(() => {
    if (activeTab === 'quotas') {
      quotaTimerRef.current = setInterval(() => {
        if (document.visibilityState === 'visible') {
          loadQuotas(true);
        }
      }, 30000);
    }
    return () => {
      if (quotaTimerRef.current) {
        clearInterval(quotaTimerRef.current);
        quotaTimerRef.current = null;
      }
    };
  }, [activeTab, loadQuotas]);

  const toggleModelStatus = async (modelId, currentVal) => {
    const newVal = !currentVal;
    try {
      const response = await fetch(`/api/antigravity/models/${modelId}/status`, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enabled: newVal }),
      });
      if (response.ok) {
        toast.success(`模型 ${modelId} 已${newVal ? '启用' : '禁用'}`);
        loadQuotas(true);
      } else {
        toast.error('切换状态失败');
      }
    } catch (e) {
      toast.error('请求异常');
    }
  };

  const getOrderedAllModelsList = () => {
    if (!quotas) return [];
    let list = [];
    const groupOrder = ['图像生成', 'claude_gpt', 'tab_completion', 'gemini', 'others'];
    groupOrder.forEach(gId => {
      const group = quotas[gId];
      if (group && Array.isArray(group.models)) {
        const modelsWithInfo = group.models.map(m => ({
          ...m,
          groupIcon: group.icon,
          groupName: group.name,
        }));
        list = list.concat(modelsWithInfo);
      }
    });
    return list;
  };

  // ==================== Model Matrix Tab Logic ====================
  const [matrix, setMatrix] = useState({});
  const [matrixLoading, setMatrixLoading] = useState(false);

  const loadMatrix = useCallback(async () => {
    setMatrixLoading(true);
    try {
      const response = await fetch('/api/antigravity/config/matrix', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      setMatrix(data || {});
    } catch (e) {
      toast.error('获取矩阵配置失败');
    } finally {
      setMatrixLoading(false);
    }
  }, [getAuthHeaders]);

  const saveMatrix = async () => {
    setMatrixLoading(true);
    try {
      const response = await fetch('/api/antigravity/config/matrix', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(matrix),
      });
      if (response.ok) {
        toast.success('矩阵配置已成功保存到云端');
      } else {
        toast.error('保存配置失败');
      }
    } catch (e) {
      toast.error('网络请求失败');
    } finally {
      setMatrixLoading(false);
    }
  };

  const getMatrixList = () => {
    if (!matrix) return [];
    // Only display enabled models in quotas
    const enabledModelsInQuotas = getOrderedAllModelsList()
      .filter(m => m.enabled !== false)
      .map(m => m.id);

    // Populate missing models in matrix
    enabledModelsInQuotas.forEach(id => {
      if (!matrix[id]) {
        matrix[id] = { base: false, fakeStream: false, antiTrunc: false };
      }
    });

    return Object.keys(matrix)
      .filter(id => enabledModelsInQuotas.includes(id))
      .sort()
      .map(id => ({
        id,
        ...matrix[id],
      }));
  };

  const toggleMatrixCell = (modelId, field) => {
    const updated = { ...matrix };
    if (!updated[modelId]) {
      updated[modelId] = { base: false, fakeStream: false, antiTrunc: false };
    }
    updated[modelId][field] = !updated[modelId][field];
    setMatrix(updated);
  };

  const isMatrixColumnAllChecked = (field) => {
    const list = getMatrixList();
    if (list.length === 0) return false;
    return list.every(item => item[field]);
  };

  const toggleMatrixColumn = (field) => {
    const list = getMatrixList();
    const allChecked = isMatrixColumnAllChecked(field);
    const updated = { ...matrix };
    list.forEach(item => {
      if (!updated[item.id]) {
        updated[item.id] = { base: false, fakeStream: false, antiTrunc: false };
      }
      updated[item.id][field] = !allChecked;
    });
    setMatrix(updated);
  };

  const toggleMatrixRow = (modelId) => {
    const updated = { ...matrix };
    if (!updated[modelId]) return;
    const row = updated[modelId];
    const newState = !(row.base || row.fakeStream || row.antiTrunc);
    row.base = newState;
    row.fakeStream = newState;
    row.antiTrunc = newState;
    setMatrix(updated);
  };

  // ==================== Health Check Logic ====================
  const [checkingStatus, setCheckingStatus] = useState(false);
  const [checkHistory, setCheckHistory] = useState(null);
  const [autoCheck, setAutoCheck] = useState(false);
  const [autoCheckInterval, setAutoCheckInterval] = useState(3600000); // 1h
  const [disabledCheckModels, setDisabledCheckModels] = useState([]);
  const [backendTimerStatus, setBackendTimerStatus] = useState(null);

  const loadCheckHistory = useCallback(async () => {
    try {
      const response = await fetch('/api/antigravity/models/check-history', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      setCheckHistory(data);
    } catch (e) {
      console.error('加载检测历史失败:', e);
    }
  }, [getAuthHeaders]);

  const loadAutoCheckSettings = useCallback(async () => {
    try {
      const response = await fetch('/api/antigravity/settings', {
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
      console.error('获取自动检测参数失败:', e);
    }
  }, [getAuthHeaders]);

  const saveAutoCheckSettings = async (enabled, interval, disabledModels) => {
    try {
      await fetch('/api/antigravity/settings', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          autoCheckEnabled: enabled ? '1' : '0',
          autoCheckInterval: String(interval),
          disabledCheckModels: JSON.stringify(disabledModels),
        }),
      });
    } catch (e) {
      console.error('保存检测设置失败:', e);
    }
  };

  const handleToggleAutoCheck = async (e) => {
    const val = e.target.checked;
    setAutoCheck(val);
    await saveAutoCheckSettings(val, autoCheckInterval, disabledCheckModels);
    toast.success(val ? '自动检测已开启' : '自动检测已关闭');
  };

  const handleIntervalChange = async (e) => {
    const val = parseInt(e.target.value);
    setAutoCheckInterval(val);
    await saveAutoCheckSettings(autoCheck, val, disabledCheckModels);
    toast.success(`检测间隔已更新为 ${Math.round(val / 60000)} 分钟`);
  };

  const handleToggleCheckModel = async (model) => {
    const list = [...disabledCheckModels];
    const idx = list.indexOf(model);
    if (idx >= 0) {
      list.splice(idx, 1);
    } else {
      list.push(model);
    }
    setDisabledCheckModels(list);
    await saveAutoCheckSettings(autoCheck, autoCheckInterval, list);
  };

  const runModelCheck = async () => {
    setCheckingStatus(true);
    toast.info('正在开启模型健康检测，请稍候...');
    // Poll updates
    const poll = setInterval(() => {
      loadCheckHistory();
    }, 2000);

    try {
      const response = await fetch('/api/antigravity/accounts/check', {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (response.ok && data.success) {
        toast.success(`检测完成: ${data.totalAccounts} 账号, ${data.totalModels} 模型`);
      } else {
        toast.error(data.error || '检测失败');
      }
    } catch (e) {
      toast.error('检测请求异常');
    } finally {
      clearInterval(poll);
      setCheckingStatus(false);
      loadCheckHistory();
    }
  };

  const clearCheckHistory = async () => {
    if (!window.confirm('确定要清空模型检测历史吗？')) return;
    try {
      const response = await fetch('/api/antigravity/models/check-history/clear', {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('检测历史已清空');
        setCheckHistory({ models: [], times: [], matrix: {} });
      }
    } catch (e) {
      toast.error('操作异常');
    }
  };

  const getCheckBadgeClass = (checkData, accountIndex) => {
    if (!checkData) return 'bg-kumo-recessed text-kumo-subtle';
    if (checkData.error_log === 'Waiting...' || checkData.error_log === 'Checking...') {
      return 'bg-kumo-recessed text-kumo-subtle animate-pulse';
    }
    const passedList = (checkData.passedAccounts || '').split(',').filter(s => s);
    if (passedList.includes(String(accountIndex))) {
      return 'bg-kumo-success text-white';
    }
    const errorLog = checkData.error_log || '';
    const checkComplete = errorLog.length > 0 && errorLog !== 'Waiting...' && errorLog !== 'Checking...';
    if (checkComplete) {
      if (checkData.status === 'ok') return 'bg-kumo-success text-white';
      if (checkData.status === 'quota') return 'bg-kumo-warning text-white';
      if (checkData.status === 'error') return 'bg-kumo-danger text-white';
    }
    return 'bg-kumo-recessed text-kumo-subtle';
  };

  const getCheckBadgeTitle = (checkData, accountIndex) => {
    if (!checkData) return '未检测';
    if (checkData.error_log === 'Waiting...' || checkData.error_log === 'Checking...') {
      return `账号 #${accountIndex} 检测中`;
    }
    const passedList = (checkData.passedAccounts || '').split(',').filter(s => s);
    if (passedList.includes(String(accountIndex))) {
      return `账号 #${accountIndex} 通过`;
    }
    if (passedList.length > 0) {
      return `账号 #${accountIndex} 失败`;
    }
    if (checkData.status === 'quota') {
      return `账号 #${accountIndex} 额度超限 (429)`;
    }
    if (checkData.status === 'error') {
      return `账号 #${accountIndex} 失败`;
    }
    return `账号 #${accountIndex} 未通过`;
  };

  // ==================== Logs Tab Logic ====================
  const [logs, setLogs] = useState([]);
  const [logFilterAccount, setLogFilterAccount] = useState('');
  const [logFilterModel, setLogFilterModel] = useState('');
  const [logsLoading, setLogsLoading] = useState(false);
  const [logDetailOpen, setLogDetailOpen] = useState(false);
  const [logDetail, setLogDetail] = useState(null);
  const [logDetailRaw, setLogDetailRaw] = useState(false);

  const loadLogs = useCallback(async (showFeedback = false) => {
    setLogsLoading(true);
    try {
      const response = await fetch('/api/antigravity/logs', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data && Array.isArray(data.logs)) {
        setLogs(data.logs);
        if (showFeedback) toast.success('调用日志已刷新');
      }
    } catch (e) {
      console.error(e);
    } finally {
      setLogsLoading(false);
    }
  }, [getAuthHeaders]);

  const clearLogs = async () => {
    if (!window.confirm('确定要清空反重力网关的调用日志吗？')) return;
    try {
      const response = await fetch('/api/antigravity/logs/clear', {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('调用日志已清空');
        setLogs([]);
      }
    } catch (e) {
      toast.error('请求清空出错');
    }
  };

  const getLogModelsList = useMemo(() => {
    const list = new Set();
    logs.forEach(l => { if (l.model) list.add(l.model); });
    return Array.from(list).sort();
  }, [logs]);

  const filteredLogs = useMemo(() => {
    let list = [...logs];
    if (logFilterAccount) {
      list = list.filter(l => l.accountId === logFilterAccount);
    }
    if (logFilterModel) {
      list = list.filter(l => l.model === logFilterModel);
    }
    return list;
  }, [logs, logFilterAccount, logFilterModel]);

  const showLogDetail = async (log) => {
    setLogDetailRaw(false);
    try {
      const response = await fetch(`/api/antigravity/logs/${log.id}`, {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (data.log) {
        setLogDetail(data.log);
        setLogDetailOpen(true);
      } else {
        toast.error('获取日志详情失败');
      }
    } catch (e) {
      toast.error('获取详情异常');
    }
  };

  // ==================== Settings Tab Logic ====================
  const [settingsForm, setSettingsForm] = useState({
    DEFAULT_TEMPERATURE: '',
    DEFAULT_TOP_P: '',
    DEFAULT_TOP_K: '',
    DEFAULT_MAX_TOKENS: '',
    MAX_IMAGES: '',
    IMAGE_BASE_URL: '',
    CREDENTIAL_MAX_USAGE_PER_HOUR: '',
    TIMEOUT: '',
    REQUEST_LOG_RETENTION_DAYS: '',
    API_KEY: '',
    PROXY: '',
  });
  const [settingsSaving, setSettingsSaving] = useState(false);

  // Model redirects state
  const [redirects, setRedirects] = useState([]);
  const [editingRedirectSource, setEditingRedirectSource] = useState(null);
  const [newRedirectSource, setNewRedirectSource] = useState('');
  const [newRedirectTarget, setNewRedirectTarget] = useState('');

  const loadSettings = useCallback(async (showFeedback = false) => {
    try {
      const response = await fetch('/api/antigravity/settings', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      const form = {
        DEFAULT_TEMPERATURE: data.DEFAULT_TEMPERATURE || '1',
        DEFAULT_TOP_P: data.DEFAULT_TOP_P || '0.85',
        DEFAULT_TOP_K: data.DEFAULT_TOP_K || '50',
        DEFAULT_MAX_TOKENS: data.DEFAULT_MAX_TOKENS || '8096',
        MAX_IMAGES: data.MAX_IMAGES || '10',
        IMAGE_BASE_URL: data.IMAGE_BASE_URL || '',
        CREDENTIAL_MAX_USAGE_PER_HOUR: data.CREDENTIAL_MAX_USAGE_PER_HOUR || '20',
        TIMEOUT: data.TIMEOUT || '120000',
        REQUEST_LOG_RETENTION_DAYS: data.REQUEST_LOG_RETENTION_DAYS || '7',
        API_KEY: data.API_KEY || '',
        PROXY: data.PROXY || '',
      };
      setSettingsForm(form);
      if (showFeedback) toast.success('全局设置已同步');
    } catch (e) {
      console.error(e);
    }
  }, [getAuthHeaders]);

  const loadRedirects = useCallback(async () => {
    try {
      const response = await fetch('/api/antigravity/models/redirects', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      setRedirects(data || []);
    } catch (e) {
      console.error(e);
    }
  }, [getAuthHeaders]);

  const saveSettings = async () => {
    setSettingsSaving(true);
    try {
      let saved = 0;
      for (const [key, value] of Object.entries(settingsForm)) {
        if (value !== undefined) {
          await fetch('/api/antigravity/settings', {
            method: 'POST',
            headers: getAuthHeaders(),
            body: JSON.stringify({ key, value: String(value) }),
          });
          saved++;
        }
      }
      toast.success(`全局设置保存成功: 已更新 ${saved} 项`);
      loadSettings(false);
    } catch (e) {
      toast.error('保存设置异常: ' + e.message);
    } finally {
      setSettingsSaving(false);
    }
  };

  const addRedirectRule = async () => {
    const source = newRedirectSource.trim();
    const target = newRedirectTarget.trim();
    if (!source || !target) return;

    try {
      if (editingRedirectSource && editingRedirectSource !== source) {
        await fetch(`/api/antigravity/models/redirects/${encodeURIComponent(editingRedirectSource)}`, {
          method: 'DELETE',
          headers: getAuthHeaders(),
        });
      }

      const response = await fetch('/api/antigravity/models/redirects', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ sourceModel: source, targetModel: target }),
      });

      if (response.ok) {
        toast.success('重定向别名已保存');
        setNewRedirectSource('');
        setNewRedirectTarget('');
        setEditingRedirectSource(null);
        loadRedirects();
      } else {
        const data = await response.json();
        toast.error('保存失败: ' + (data.error || '未知错误'));
      }
    } catch (e) {
      toast.error('请求失败: ' + e.message);
    }
  };

  const removeRedirectRule = async (source) => {
    if (!window.confirm(`确认删除 ${source} 的重定向规则别名吗？`)) return;
    try {
      const response = await fetch(`/api/antigravity/models/redirects/${encodeURIComponent(source)}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('规则已删除');
        loadRedirects();
      }
    } catch (e) {
      toast.error('删除重定向规则失败');
    }
  };

  // ==================== Initial loaders by activeTab ====================
  useEffect(() => {
    if (activeTab === 'quotas') {
      loadAccounts();
    } else if (activeTab === 'matrix') {
      loadQuotas(true).then(() => {
        loadMatrix();
      });
    } else if (activeTab === 'accounts') {
      loadAccounts();
      loadCheckHistory();
      loadAutoCheckSettings();
    } else if (activeTab === 'logs') {
      loadLogs();
      loadAccounts();
    } else if (activeTab === 'settings') {
      loadSettings();
      loadRedirects();
    }
  }, [activeTab, loadAccounts, loadQuotas, loadMatrix, loadCheckHistory, loadAutoCheckSettings, loadLogs, loadSettings, loadRedirects]);

  return (
    <div className="space-y-6 flex flex-col h-full min-h-[75vh]">
      {/* Sub Tabs */}
      <div className="flex border border-kumo-line rounded-lg p-0.5 bg-kumo-recessed self-start select-none">
        <button
          onClick={() => setActiveTab('quotas')}
          className={`flex items-center gap-2 px-4 py-1.5 rounded-md text-xs font-semibold cursor-pointer transition-colors ${
            activeTab === 'quotas'
              ? 'bg-kumo-base text-kumo-strong shadow-sm'
              : 'text-kumo-subtle hover:text-kumo-strong'
          }`}
        >
          <PieChart className="w-3.5 h-3.5" />
          <span>额度使用</span>
        </button>
        <button
          onClick={() => setActiveTab('matrix')}
          className={`flex items-center gap-2 px-4 py-1.5 rounded-md text-xs font-semibold cursor-pointer transition-colors ${
            activeTab === 'matrix'
              ? 'bg-kumo-base text-kumo-strong shadow-sm'
              : 'text-kumo-subtle hover:text-kumo-strong'
          }`}
        >
          <Grid className="w-3.5 h-3.5" />
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
          <span>账号管理</span>
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

      {/* ==================== 1. 额度使用 Tab ==================== */}
      {activeTab === 'quotas' && (
        <div className="quick-fade-in space-y-4">
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-4 flex flex-col md:flex-row justify-between items-start md:items-center gap-4">
            <div className="flex flex-wrap items-center gap-3">
              <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
                <PieChart className="w-4 h-4 text-kumo-brand" />
                额度使用状态
              </h3>
              <select
                value={selectedAccountId}
                onChange={(e) => setSelectedAccountId(e.target.value)}
                className="bg-kumo-base text-kumo-strong border border-kumo-line rounded px-2.5 py-1 text-xs font-semibold focus:outline-none"
              >
                <option value="">全部在线网关</option>
                {accounts.filter(a => a.enable !== false).map(a => (
                  <option key={a.id} value={a.id}>
                    {a.name} ({a.status === 'online' ? '在线' : '离线'})
                  </option>
                ))}
              </select>
            </div>
            <div className="flex gap-2">
              <Button onClick={() => setQuotaViewMode(quotaViewMode === 'grouped' ? 'list' : 'grouped')} className="flex items-center gap-1">
                <span>{quotaViewMode === 'grouped' ? '列表视图' : '分组视图'}</span>
              </Button>
              <Button onClick={() => loadQuotas(false)} disabled={quotasLoading} className="flex items-center gap-1">
                <RotateCw className={`w-3.5 h-3.5 ${quotasLoading ? 'animate-spin' : ''}`} />
                <span>刷新额度</span>
              </Button>
            </div>
          </div>

          {quotasLoading && Object.keys(quotas).length === 0 ? (
            <div className="text-center p-10 text-kumo-subtle text-xs">正在刷新云端配额信息...</div>
          ) : (
            <>
              {/* Grouped View */}
              {quotaViewMode === 'grouped' && (
                <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                  {Object.entries(quotas).map(([groupId, group]) => (
                    <div key={groupId} className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden flex flex-col justify-between">
                      <div>
                        {/* Group Header */}
                        <div className="p-3 bg-kumo-recessed/30 border-b border-kumo-line flex justify-between items-center">
                          <span className="font-bold text-xs text-kumo-strong flex items-center gap-1.5">
                            <span className="text-lg">{group.icon}</span>
                            <span>{group.name}</span>
                          </span>
                          {group.remaining !== undefined && (
                            <span
                              className="text-xs font-bold font-mono px-2 py-0.5 rounded border"
                              style={{ color: getAgQuotaColor(group.remaining), borderColor: getAgQuotaColor(group.remaining) + '33' }}
                            >
                              {group.remaining}%
                            </span>
                          )}
                        </div>

                        {/* Group Content */}
                        <div className="p-4 space-y-3">
                          <p className="text-[11px] text-kumo-subtle leading-relaxed">{group.description}</p>

                          {/* Remaining compact progress bar */}
                          {group.remaining !== undefined && (
                            <div className="w-full bg-kumo-recessed/60 h-1.5 rounded-full overflow-hidden">
                              <div
                                className="h-full rounded-full transition-all"
                                style={{ width: `${group.remaining}%`, backgroundColor: getAgQuotaColor(group.remaining) }}
                              />
                            </div>
                          )}

                          {group.resetTime && (
                            <div className="text-[10px] text-kumo-subtle font-semibold flex items-center gap-1">
                              <span className="w-1 h-1 rounded-full bg-current"></span>
                              <span>重置时间: {formatDisplayDate(group.resetTime)} ({formatResetCountdown(group.resetTime)})</span>
                            </div>
                          )}

                          {/* Models status list */}
                          <div className="space-y-2 pt-2 border-t border-kumo-line/60">
                            {group.models?.map(m => (
                              <div key={m.id} className="flex justify-between items-center p-2 bg-kumo-recessed/20 border border-kumo-line rounded text-xs">
                                <span className="font-mono text-kumo-strong truncate max-w-[180px]">{m.id}</span>
                                <div className="flex items-center gap-3">
                                  <span className="font-mono font-bold" style={{ color: getAgQuotaColor(m.remaining) }}>
                                    {m.remaining}%
                                  </span>
                                  <input
                                    type="checkbox"
                                    checked={m.enabled !== false}
                                    onChange={() => toggleModelStatus(m.id, m.enabled !== false)}
                                    className="w-8 h-4 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-3 before:h-3 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
                                  />
                                </div>
                              </div>
                            ))}
                          </div>
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}

              {/* List View */}
              {quotaViewMode === 'list' && (
                <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
                  <div className="overflow-x-auto">
                    <table className="w-full border-collapse text-left text-xs">
                      <thead>
                        <tr className="border-b border-kumo-line bg-kumo-recessed/20">
                          <th className="p-3 font-semibold text-kumo-strong">模型 ID</th>
                          <th className="p-3 font-semibold text-kumo-strong">分组</th>
                          <th className="p-3 font-semibold text-kumo-strong" style={{ width: '250px' }}>剩余配额</th>
                          <th className="p-3 font-semibold text-kumo-strong text-center" style={{ width: '180px' }}>重置时间</th>
                          <th className="p-3 font-semibold text-kumo-strong text-center w-20">启用状态</th>
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-kumo-line">
                        {getOrderedAllModelsList().map(m => (
                          <tr key={m.id} className="hover:bg-kumo-recessed/5">
                            <td className="p-3 font-mono font-semibold text-kumo-strong">{m.id}</td>
                            <td className="p-3 text-kumo-subtle">
                              {m.groupIcon} {m.groupName}
                            </td>
                            <td className="p-3">
                              <div className="flex items-center gap-3">
                                <div className="flex-1 bg-kumo-recessed/60 h-2 rounded-full overflow-hidden">
                                  <div
                                    className="h-full rounded-full transition-all"
                                    style={{ width: `${m.remaining}%`, backgroundColor: getAgQuotaColor(m.remaining) }}
                                  />
                                </div>
                                <span className="font-mono font-bold w-8 text-right" style={{ color: getAgQuotaColor(m.remaining) }}>
                                  {m.remaining}%
                                </span>
                              </div>
                            </td>
                            <td className="p-3 text-center text-kumo-subtle text-[10px] font-mono">
                              {m.resetTime ? (
                                <div>
                                  <div>{formatDisplayDate(m.resetTime)}</div>
                                  <div className="opacity-70">({formatResetCountdown(m.resetTime)})</div>
                                </div>
                              ) : '-'}
                            </td>
                            <td className="p-3 text-center">
                              <input
                                type="checkbox"
                                checked={m.enabled !== false}
                                onChange={() => toggleModelStatus(m.id, m.enabled !== false)}
                                className="w-8 h-4 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-3 before:h-3 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box animate-none"
                              />
                            </td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </div>
              )}
            </>
          )}
        </div>
      )}

      {/* ==================== 2. 模型矩阵 Tab ==================== */}
      {activeTab === 'matrix' && (
        <div className="quick-fade-in space-y-4">
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-4 flex justify-between items-center">
            <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
              <Grid className="w-4 h-4 text-kumo-brand" />
              Model Matrix (Antigravity)
            </h3>
            <div className="flex gap-2">
              <Button variant="primary" onClick={saveMatrix} disabled={matrixLoading}>
                <Check className="w-3.5 h-3.5 mr-1" />
                <span>保存配置</span>
              </Button>
              <Button onClick={loadMatrix} disabled={matrixLoading}>
                <RotateCw className={`w-3.5 h-3.5 mr-1 ${matrixLoading ? 'animate-spin' : ''}`} />
                <span>刷新矩阵</span>
              </Button>
            </div>
          </div>

          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full border-collapse text-left text-xs">
                <thead>
                  <tr className="border-b border-kumo-line bg-kumo-recessed/20">
                    <th className="p-3 font-semibold text-kumo-strong">内部模型 ID (点击切换整行)</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center w-36">
                      <button
                        onClick={() => toggleMatrixColumn('base')}
                        className="font-bold flex items-center justify-center gap-2 mx-auto hover:text-kumo-brand cursor-pointer"
                      >
                        <span>基础功能</span>
                        <input
                          type="checkbox"
                          checked={isMatrixColumnAllChecked('base')}
                          readOnly
                          className="pointer-events-none w-3.5 h-3.5 rounded border border-kumo-line accent-kumo-brand"
                        />
                      </button>
                    </th>
                    <th className="p-3 font-semibold text-kumo-strong text-center w-36">
                      <button
                        onClick={() => toggleMatrixColumn('fakeStream')}
                        className="font-bold flex items-center justify-center gap-2 mx-auto hover:text-kumo-brand cursor-pointer"
                      >
                        <span>假流</span>
                        <input
                          type="checkbox"
                          checked={isMatrixColumnAllChecked('fakeStream')}
                          readOnly
                          className="pointer-events-none w-3.5 h-3.5 rounded border border-kumo-line accent-kumo-brand"
                        />
                      </button>
                    </th>
                    <th className="p-3 font-semibold text-kumo-strong text-center w-36">
                      <button
                        onClick={() => toggleMatrixColumn('antiTrunc')}
                        className="font-bold flex items-center justify-center gap-2 mx-auto hover:text-kumo-brand cursor-pointer"
                      >
                        <span>流抗</span>
                        <input
                          type="checkbox"
                          checked={isMatrixColumnAllChecked('antiTrunc')}
                          readOnly
                          className="pointer-events-none w-3.5 h-3.5 rounded border border-kumo-line accent-kumo-brand"
                        />
                      </button>
                    </th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-kumo-line">
                  {getMatrixList().length === 0 ? (
                    <tr>
                      <td colSpan={4} className="p-8 text-center text-kumo-subtle">
                        暂无配置数据，请先在「额度使用」页中启用需要配对的官网模型。
                      </td>
                    </tr>
                  ) : (
                    getMatrixList().map(row => (
                      <tr key={row.id} className="hover:bg-kumo-recessed/5">
                        <td
                          className="p-3 font-mono font-semibold text-kumo-strong cursor-pointer select-none hover:text-kumo-brand"
                          onClick={() => toggleMatrixRow(row.id)}
                          title="双击切换整行开关"
                        >
                          {row.id}
                        </td>
                        <td className="p-3 text-center">
                          <input
                            type="checkbox"
                            checked={!!row.base}
                            onChange={() => toggleMatrixCell(row.id, 'base')}
                            className="w-8 h-4 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-3 before:h-3 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
                          />
                        </td>
                        <td className="p-3 text-center">
                          <input
                            type="checkbox"
                            checked={!!row.fakeStream}
                            onChange={() => toggleMatrixCell(row.id, 'fakeStream')}
                            className="w-8 h-4 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-3 before:h-3 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
                          />
                        </td>
                        <td className="p-3 text-center">
                          <input
                            type="checkbox"
                            checked={!!row.antiTrunc}
                            onChange={() => toggleMatrixCell(row.id, 'antiTrunc')}
                            className="w-8 h-4 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-3 before:h-3 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
                          />
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}

      {/* ==================== 3. 账号管理 Tab ==================== */}
      {activeTab === 'accounts' && (
        <div className="quick-fade-in space-y-6">
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-4 flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
            <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
              <Users className="w-4 h-4 text-kumo-brand" />
              连接服务凭证
            </h3>
            <div className="flex flex-wrap gap-2">
              <Button
                onClick={() => setShowOAuthExpand(!showOAuthExpand)}
                className="bg-blue-600 hover:bg-blue-700 text-white border-none flex items-center gap-1"
              >
                <span>OAuth 授权</span>
              </Button>
              <Button variant="primary" onClick={openManualModal} className="flex items-center gap-1">
                <Plus className="w-3.5 h-3.5" />
                <span>手动添加</span>
              </Button>
              <Button onClick={refreshAllAgAccounts} disabled={agRefreshingAll} className="flex items-center gap-1">
                <RotateCw className={`w-3.5 h-3.5 ${agRefreshingAll ? 'animate-spin' : ''}`} />
                <span>同步授权</span>
              </Button>
              <Button onClick={exportAccounts} title="导出账号" className="p-2">
                <Upload className="w-4 h-4" />
              </Button>
              <Button onClick={importAccounts} title="导入账号" className="p-2">
                <Download className="w-4 h-4" />
              </Button>
            </div>
          </div>

          {/* OAuth Panel */}
          {showOAuthExpand && (
            <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-5 space-y-4 quick-fade-in">
              <div className="flex items-center gap-2 border-b border-kumo-line pb-3">
                <span className="font-bold text-sm text-kumo-strong">Google OAuth 自动连接授权流程</span>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
                {/* Step 1 */}
                <div className="space-y-2.5">
                  <div className="flex items-center gap-2">
                    <span className="w-5 h-5 rounded-full bg-kumo-brand text-white flex items-center justify-center font-bold text-xs">1</span>
                    <span className="text-xs font-semibold text-kumo-strong">启动 Google 登录以捕获凭证</span>
                  </div>
                  <p className="text-[11px] text-kumo-subtle leading-relaxed">
                    点击下方按钮跳转到 Google Cloud 进行登录。重定向后，由于本地暂无回调域名，您的浏览器会显示「无法访问此网站」页面，此属正常现象。
                  </p>
                  <Button onClick={getGoogleAuthUrl} className="w-full flex items-center justify-center gap-1.5 py-2">
                    <span>获取 Google 授权链接</span>
                  </Button>
                </div>

                {/* Step 2 */}
                <div className="space-y-3">
                  <div className="flex items-center gap-2">
                    <span className="w-5 h-5 rounded-full bg-kumo-brand text-white flex items-center justify-center font-bold text-xs">2</span>
                    <span className="text-xs font-semibold text-kumo-strong">解析回调 URL 连接网关</span>
                  </div>
                  <textarea
                    value={agOauthUrl}
                    onChange={(e) => setAgOauthUrl(e.target.value)}
                    placeholder="在这里粘贴浏览器地址栏中，重定向后的完整 URL (以 http:// 或者是 https:// 开头)..."
                    rows={2}
                    className="w-full bg-kumo-recessed text-kumo-strong text-xs p-2.5 border border-kumo-line rounded-lg focus:outline-none font-mono resize-none"
                  />
                  <div className="flex gap-2">
                    <input
                      type="text"
                      placeholder="自定义项目 ID (可选)"
                      value={agCustomProjectId}
                      onChange={(e) => setAgCustomProjectId(e.target.value)}
                      className="bg-kumo-base text-kumo-strong text-[11px] px-2.5 py-1 border border-kumo-line rounded flex-1 focus:outline-none"
                    />
                    <label className="flex items-center gap-1.5 text-xs text-kumo-subtle select-none cursor-pointer">
                      <input
                        type="checkbox"
                        checked={agAllowRandomProjectId}
                        onChange={(e) => setAgAllowRandomProjectId(e.target.checked)}
                        className="rounded border-kumo-line"
                      />
                      <span>允许随机 ID</span>
                    </label>
                  </div>
                </div>
              </div>

              <div className="pt-2 border-t border-kumo-line flex justify-end">
                <Button variant="primary" disabled={!agOauthUrl || oauthVerifying} onClick={verifyOauthUrl} className="px-6">
                  {oauthVerifying ? '验证连通中...' : '开始连接'}
                </Button>
              </div>
            </div>
          )}

          {/* Accounts list Table */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full border-collapse text-left text-xs">
                <thead>
                  <tr className="border-b border-kumo-line bg-kumo-recessed/20">
                    <th className="p-3 font-semibold text-kumo-strong w-12 text-center">#</th>
                    <th className="p-3 font-semibold text-kumo-strong">备注名称</th>
                    <th className="p-3 font-semibold text-kumo-strong" style={{ minWidth: '280px' }}>核心模型配额简图</th>
                    <th className="p-3 font-semibold text-kumo-strong">关联邮箱</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center" style={{ width: '100px' }}>成功 / 失败数</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center w-24">状态</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center w-28">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-kumo-line">
                  {accounts.length === 0 ? (
                    <tr>
                      <td colSpan={7} className="p-8 text-center text-kumo-subtle">
                        当前暂无绑定的服务凭证。
                      </td>
                    </tr>
                  ) : (
                    accounts.map((acc, idx) => (
                      <tr key={acc.id} className="hover:bg-kumo-recessed/5">
                        <td className="p-3 text-center text-kumo-subtle font-semibold">{idx + 1}</td>
                        <td className="p-3 font-bold text-kumo-strong">{acc.name || '未命名'}</td>
                        <td className="p-3">
                          <div className="grid grid-cols-2 gap-2 text-[10px]">
                            {getAccountQuotaDisplay(acc.quotas).map(q => (
                              <div key={q.key} className="flex items-center gap-1.5 bg-kumo-recessed/35 p-1 rounded border border-kumo-line/50">
                                <span className="font-semibold text-kumo-subtle scale-90 origin-left">{q.label}</span>
                                <div className="flex-1 bg-kumo-recessed h-1 rounded overflow-hidden">
                                  <div
                                    className="h-full rounded-full transition-all"
                                    style={{ width: `${q.percent}%`, backgroundColor: getAgQuotaColor(q.percent) }}
                                  />
                                </div>
                                <span className="font-bold scale-90" style={{ color: getAgQuotaColor(q.percent) }}>
                                  {q.percent}%
                                </span>
                              </div>
                            ))}
                          </div>
                        </td>
                        <td className="p-3 font-mono text-kumo-subtle text-[11px]">{acc.email || '-'}</td>
                        <td className="p-3 text-center">
                          <div className="flex justify-center items-center gap-1 font-semibold">
                            <span className="text-kumo-success">{acc.success_count || 0}</span>
                            <span className="text-kumo-subtle">/</span>
                            <span className="text-kumo-danger">{acc.error_count || 0}</span>
                          </div>
                        </td>
                        <td className="p-3 text-center">
                          <span
                            className={`px-2 py-0.5 rounded text-[10px] font-bold border ${
                              acc.status === 'online'
                                ? 'bg-kumo-success/10 text-kumo-success border-kumo-success/20'
                                : acc.status === 'error'
                                ? 'bg-kumo-danger/10 text-kumo-danger border-kumo-danger/20'
                                : 'bg-kumo-recessed text-kumo-subtle border-kumo-line'
                            }`}
                          >
                            {acc.status === 'online' ? '在线' : acc.status === 'error' ? '异常' : '未知'}
                          </span>
                        </td>
                        <td className="p-3">
                          <div className="flex justify-center gap-2">
                            <button
                              onClick={() => toggleAccountEnabled(acc.id, acc.enable)}
                              className={`p-1.5 rounded hover:bg-kumo-recessed transition-colors ${
                                acc.enable !== false ? 'text-kumo-success' : 'text-kumo-subtle'
                              }`}
                              title={acc.enable !== false ? '禁用' : '启用'}
                            >
                              <Check className="w-4 h-4" />
                            </button>
                            <button
                              onClick={() => openEditAccountModal(acc)}
                              className="p-1.5 rounded hover:bg-kumo-recessed text-kumo-subtle hover:text-kumo-strong transition-colors"
                              title="编辑"
                            >
                              <Edit className="w-4 h-4" />
                            </button>
                            <button
                              onClick={() => deleteAccount(acc.id, acc.name)}
                              className="p-1.5 rounded hover:bg-kumo-danger/10 text-kumo-subtle hover:text-kumo-danger transition-colors"
                              title="删除"
                            >
                              <Trash className="w-4 h-4" />
                            </button>
                          </div>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>

          {/* Health check section */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-4 space-y-4">
            <div className="flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4 pb-3 border-b border-kumo-line">
              <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-1.5">
                <Heart className="w-4 h-4 text-kumo-brand" />
                模型健康检测系统
              </h4>
              <div className="flex flex-wrap items-center gap-2.5 text-xs">
                <label className="flex items-center gap-1.5 text-xs text-kumo-subtle select-none cursor-pointer">
                  <input
                    type="checkbox"
                    checked={autoCheck}
                    onChange={handleToggleAutoCheck}
                    className="rounded border-kumo-line"
                  />
                  <span>定时检测</span>
                </label>

                <select
                  value={autoCheckInterval}
                  onChange={handleIntervalChange}
                  className="bg-kumo-base text-kumo-strong border border-kumo-line rounded px-1.5 py-0.5 text-xs font-semibold focus:outline-none"
                >
                  <option value="1800000">30分钟</option>
                  <option value="3600000">1小时</option>
                  <option value="7200000">2小时</option>
                  <option value="14400000">4小时</option>
                  <option value="21600000">6小时</option>
                </select>

                <Button variant="primary" disabled={checkingStatus} onClick={runModelCheck} className="flex items-center gap-1.5 py-1 px-2.5">
                  <Activity className={`w-3.5 h-3.5 ${checkingStatus ? 'animate-pulse' : ''}`} />
                  <span>{checkingStatus ? '正在检测...' : '立即运行'}</span>
                </Button>
                <Button onClick={loadCheckHistory} className="p-1.5"><RotateCw className="w-3.5 h-3.5" /></Button>
                <Button onClick={clearCheckHistory} className="text-kumo-danger p-1.5"><Trash className="w-3.5 h-3.5" /></Button>
              </div>
            </div>

            {/* Check history Matrix */}
            {checkHistory && checkHistory.models && checkHistory.models.length > 0 ? (
              <div className="overflow-x-auto">
                <table className="w-full border-collapse text-left text-xs">
                  <thead>
                    <tr className="border-b border-kumo-line bg-kumo-recessed/20 font-mono text-[10px]">
                      <th className="p-2 font-semibold text-kumo-strong" style={{ width: '200px' }}>测试模型</th>
                      {checkHistory.times.slice().reverse().map(t => (
                        <th key={t} className="p-2 font-semibold text-kumo-strong text-center whitespace-nowrap min-w-20">
                          {formatDateTime(t * 1000).substring(5, 16)}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-kumo-line font-mono text-[11px]">
                    {checkHistory.models.map(model => (
                      <tr key={model} className="hover:bg-kumo-recessed/5">
                        <td className="p-2">
                          <div className="flex items-center gap-1.5">
                            <input
                              type="checkbox"
                              checked={!disabledCheckModels.includes(model)}
                              onChange={() => handleToggleCheckModel(model)}
                              className="rounded border-kumo-line"
                            />
                            <span className={disabledCheckModels.includes(model) ? 'opacity-40 line-through' : 'text-kumo-strong'}>
                              {model}
                            </span>
                          </div>
                        </td>
                        {checkHistory.times.slice().reverse().map(time => {
                          const item = checkHistory.matrix[model]?.[time];
                          return (
                            <td key={time} className="p-2 text-center whitespace-nowrap">
                              {item ? (
                                <div className="flex justify-center gap-1">
                                  {accounts.map((acc, nIdx) => {
                                    const acNum = nIdx + 1;
                                    return (
                                      <span
                                        key={acNum}
                                        className={`w-4 h-4 rounded-full flex items-center justify-center text-[9px] font-bold select-none transition-all cursor-help ${getCheckBadgeClass(
                                          item,
                                          acNum
                                        )}`}
                                        title={getCheckBadgeTitle(item, acNum)}
                                      >
                                        {acNum}
                                      </span>
                                    );
                                  })}
                                </div>
                              ) : (
                                <span className="text-kumo-subtle">-</span>
                              )}
                            </td>
                          );
                        })}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="text-center p-8 text-kumo-subtle text-xs">暂无历史检测结果。</div>
            )}
          </div>
        </div>
      )}

      {/* ==================== 4. 调用日志 Tab ==================== */}
      {activeTab === 'logs' && (
        <div className="quick-fade-in space-y-4">
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-4 flex flex-col md:flex-row justify-between items-start md:items-center gap-4">
            <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
              <History className="w-4 h-4 text-kumo-brand" />
              历史调用日志
            </h3>
            <div className="flex flex-wrap gap-2 text-xs">
              <select
                value={logFilterAccount}
                onChange={(e) => setLogFilterAccount(e.target.value)}
                className="bg-kumo-base text-kumo-strong border border-kumo-line rounded px-2 py-1 font-semibold focus:outline-none"
              >
                <option value="">全部账号</option>
                {accounts.filter(a => a.enable !== false).map(a => (
                  <option key={a.id} value={a.id}>{a.name || a.id}</option>
                ))}
              </select>

              <select
                value={logFilterModel}
                onChange={(e) => setLogFilterModel(e.target.value)}
                className="bg-kumo-base text-kumo-strong border border-kumo-line rounded px-2 py-1 font-semibold focus:outline-none"
              >
                <option value="">全部模型</option>
                {getLogModelsList.map(m => (
                  <option key={m} value={m}>{m}</option>
                ))}
              </select>

              <Button onClick={() => loadLogs(true)} className="flex items-center gap-1">
                <RotateCw className="w-3.5 h-3.5" />
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
              <table className="w-full border-collapse text-left text-xs">
                <thead>
                  <tr className="border-b border-kumo-line bg-kumo-recessed/20">
                    <th className="p-3 font-semibold text-kumo-strong text-center" style={{ width: '150px' }}>调用时间</th>
                    <th className="p-3 font-semibold text-kumo-strong">网关备注</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center" style={{ width: '140px' }}>调用模型</th>
                    <th className="p-3 font-semibold text-kumo-strong">请求路径</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center" style={{ width: '70px' }}>状态</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center" style={{ width: '80px' }}>耗时</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center w-16">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-kumo-line">
                  {filteredLogs.length === 0 ? (
                    <tr>
                      <td colSpan={7} className="p-10 text-center text-kumo-subtle">
                        未查询到可用调用历史。
                      </td>
                    </tr>
                  ) : (
                    filteredLogs.map((log) => (
                      <tr key={log.id} className="hover:bg-kumo-recessed/5">
                        <td className="p-3 text-center text-kumo-subtle font-mono">{formatDateTime(log.timestamp)}</td>
                        <td className="p-3 font-bold text-kumo-strong">
                          <div className="flex items-center gap-1">
                            <span>{log.accountName || 'System'}</span>
                            {log.isBalanced && (
                              <span className="px-1 rounded bg-kumo-brand/10 text-kumo-brand text-[9px] border border-kumo-brand/20 font-bold scale-90">
                                LB
                              </span>
                            )}
                          </div>
                        </td>
                        <td className="p-3 text-center font-mono text-kumo-subtle">{log.model || '-'}</td>
                        <td className="p-3">
                          <div className="flex items-center gap-1.5 font-mono">
                            <span className="px-1 py-0.2 rounded text-[9px] bg-kumo-brand/10 text-kumo-brand border border-kumo-brand/20 font-bold uppercase">
                              {log.method || 'POST'}
                            </span>
                            <span className="truncate text-kumo-strong max-w-[200px]" title={log.path}>{log.path}</span>
                          </div>
                        </td>
                        <td className="p-3 text-center">
                          <span
                            className={`px-2 py-0.5 rounded text-[10px] font-bold ${
                              log.statusCode >= 200 && log.statusCode < 300
                                ? 'bg-kumo-success/10 text-kumo-success border border-kumo-success/20'
                                : 'bg-kumo-danger/10 text-kumo-danger border border-kumo-danger/20'
                            }`}
                          >
                            {log.statusCode || 200}
                          </span>
                        </td>
                        <td className="p-3 text-center font-mono font-semibold text-kumo-strong">{log.durationMs}ms</td>
                        <td className="p-3 text-center">
                          <button
                            onClick={() => showLogDetail(log)}
                            className="p-1 hover:bg-kumo-recessed rounded text-kumo-subtle hover:text-kumo-strong transition-colors"
                          >
                            <Eye className="w-4 h-4" />
                          </button>
                        </td>
                      </tr>
                    ))
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}

      {/* ==================== 5. 模块配置 Tab ==================== */}
      {activeTab === 'settings' && (
        <div className="quick-fade-in space-y-6">
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            <div className="p-4 border-b border-kumo-line flex justify-between items-center bg-kumo-recessed/10">
              <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
                <Sliders className="w-4 h-4 text-kumo-brand" />
                Antigravity 参数设置
              </h3>
              <div className="flex gap-2">
                <Button onClick={() => loadSettings(true)}>
                  <RotateCw className="w-3.5 h-3.5" />
                </Button>
                <Button variant="primary" onClick={saveSettings} disabled={settingsSaving}>
                  <span>{settingsSaving ? '保存中...' : '保存全部设置'}</span>
                </Button>
              </div>
            </div>

            <div className="p-5 space-y-6">
              {/* Generation configs */}
              <div className="space-y-3">
                <span className="font-bold text-xs text-kumo-brand uppercase tracking-wider block">1. 基础生成参数</span>
                <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
                  <div className="space-y-1">
                    <label className="text-[11px] font-semibold text-kumo-subtle block">默认 Temperature</label>
                    <input
                      type="number"
                      step="0.1"
                      value={settingsForm.DEFAULT_TEMPERATURE}
                      onChange={(e) => setSettingsForm({ ...settingsForm, DEFAULT_TEMPERATURE: e.target.value })}
                      className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-1.5 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-[11px] font-semibold text-kumo-subtle block">默认 Top P</label>
                    <input
                      type="number"
                      step="0.01"
                      value={settingsForm.DEFAULT_TOP_P}
                      onChange={(e) => setSettingsForm({ ...settingsForm, DEFAULT_TOP_P: e.target.value })}
                      className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-1.5 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-[11px] font-semibold text-kumo-subtle block">默认 Top K</label>
                    <input
                      type="number"
                      value={settingsForm.DEFAULT_TOP_K}
                      onChange={(e) => setSettingsForm({ ...settingsForm, DEFAULT_TOP_K: e.target.value })}
                      className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-1.5 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-[11px] font-semibold text-kumo-subtle block">默认 Max Tokens</label>
                    <input
                      type="number"
                      value={settingsForm.DEFAULT_MAX_TOKENS}
                      onChange={(e) => setSettingsForm({ ...settingsForm, DEFAULT_MAX_TOKENS: e.target.value })}
                      className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-1.5 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono"
                    />
                  </div>
                </div>
              </div>

              {/* Advanced configs */}
              <div className="grid grid-cols-1 md:grid-cols-3 gap-6 pt-4 border-t border-kumo-line/60">
                {/* Images */}
                <div className="space-y-3">
                  <span className="font-bold text-xs text-kumo-brand uppercase block">2. 图片保存选项</span>
                  <div className="space-y-3">
                    <div className="space-y-1">
                      <label className="text-[11px] font-semibold text-kumo-subtle block">图片缓存上限</label>
                      <input
                        type="number"
                        value={settingsForm.MAX_IMAGES}
                        onChange={(e) => setSettingsForm({ ...settingsForm, MAX_IMAGES: e.target.value })}
                        className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-1.5 border border-kumo-line rounded-lg focus:outline-none"
                      />
                    </div>
                    <div className="space-y-1">
                      <label className="text-[11px] font-semibold text-kumo-subtle block">自定义图片 Base URL</label>
                      <input
                        type="text"
                        value={settingsForm.IMAGE_BASE_URL}
                        onChange={(e) => setSettingsForm({ ...settingsForm, IMAGE_BASE_URL: e.target.value })}
                        placeholder="默认使用当前主机地址"
                        className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-1.5 border border-kumo-line rounded-lg focus:outline-none font-mono"
                      />
                    </div>
                  </div>
                </div>

                {/* Quotas limits */}
                <div className="space-y-3">
                  <span className="font-bold text-xs text-kumo-brand uppercase block">3. 呼叫频次控制</span>
                  <div className="space-y-3">
                    <div className="space-y-1">
                      <label className="text-[11px] font-semibold text-kumo-subtle block">每小时调用次数限制</label>
                      <input
                        type="number"
                        value={settingsForm.CREDENTIAL_MAX_USAGE_PER_HOUR}
                        onChange={(e) => setSettingsForm({ ...settingsForm, CREDENTIAL_MAX_USAGE_PER_HOUR: e.target.value })}
                        className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-1.5 border border-kumo-line rounded-lg focus:outline-none"
                      />
                    </div>
                    <div className="space-y-1">
                      <label className="text-[11px] font-semibold text-kumo-subtle block">HTTP 代理 (PROXY)</label>
                      <input
                        type="text"
                        value={settingsForm.PROXY}
                        onChange={(e) => setSettingsForm({ ...settingsForm, PROXY: e.target.value })}
                        placeholder="例如 http://127.0.0.1:7890"
                        className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-1.5 border border-kumo-line rounded-lg focus:outline-none font-mono"
                      />
                    </div>
                  </div>
                </div>

                {/* Retentions */}
                <div className="space-y-3">
                  <span className="font-bold text-xs text-kumo-brand uppercase block">4. 系统响应配置</span>
                  <div className="space-y-3">
                    <div className="space-y-1">
                      <label className="text-[11px] font-semibold text-kumo-subtle block">超时限制 Timeout (ms)</label>
                      <input
                        type="number"
                        value={settingsForm.TIMEOUT}
                        onChange={(e) => setSettingsForm({ ...settingsForm, TIMEOUT: e.target.value })}
                        className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-1.5 border border-kumo-line rounded-lg focus:outline-none font-mono"
                      />
                    </div>
                    <div className="space-y-1">
                      <label className="text-[11px] font-semibold text-kumo-subtle block">调用历史保留时长 (天)</label>
                      <input
                        type="number"
                        value={settingsForm.REQUEST_LOG_RETENTION_DAYS}
                        onChange={(e) => setSettingsForm({ ...settingsForm, REQUEST_LOG_RETENTION_DAYS: e.target.value })}
                        className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-1.5 border border-kumo-line rounded-lg focus:outline-none"
                      />
                    </div>
                  </div>
                </div>
              </div>
            </div>
          </div>

          {/* Model redirect management */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-5 space-y-4">
            <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-1.5">
              <Globe className="w-4 h-4 text-kumo-brand" />
              模型重定向别名规则
            </h4>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              {redirects.length === 0 ? (
                <div className="col-span-2 text-center py-4 border border-dashed border-kumo-line rounded-lg text-kumo-subtle text-xs">
                  当前无重定向别名规则。
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
                          <div className="flex items-center gap-1.5 flex-1 min-w-0 font-mono">
                            <input
                              type="text"
                              value={newRedirectSource}
                              onChange={(e) => setNewRedirectSource(e.target.value)}
                              className="w-1/2 bg-kumo-base text-kumo-strong px-2 py-0.5 border border-kumo-line rounded focus:outline-none"
                            />
                            <ArrowRight className="w-3.5 h-3.5 text-kumo-subtle flex-shrink-0" />
                            <input
                              type="text"
                              value={newRedirectTarget}
                              onChange={(e) => setNewRedirectTarget(e.target.value)}
                              className="w-1/2 bg-kumo-base text-kumo-strong px-2 py-0.5 border border-kumo-line rounded focus:outline-none"
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
                              onClick={addRedirectRule}
                              className="p-1 bg-kumo-success/15 hover:bg-kumo-success/25 rounded text-kumo-success cursor-pointer"
                              title="保存"
                            >
                              <Check className="w-3.5 h-3.5" />
                            </button>
                            <button
                              onClick={() => setEditingRedirectSource(null)}
                              className="p-1 bg-kumo-recessed rounded text-kumo-subtle cursor-pointer"
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
                              className="p-1 hover:bg-kumo-recessed rounded text-kumo-subtle cursor-pointer"
                              title="编辑"
                            >
                              <Edit className="w-3.5 h-3.5" />
                            </button>
                            <button
                              onClick={() => removeRedirectRule(r.source_model)}
                              className="p-1 hover:bg-kumo-danger/10 rounded text-kumo-subtle hover:text-kumo-danger cursor-pointer"
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

            {/* Redirection add input */}
            {!editingRedirectSource && (
              <div className="flex flex-wrap gap-2.5 items-center p-4 bg-kumo-brand/5 border border-dashed border-kumo-brand/20 rounded-lg">
                <input
                  type="text"
                  placeholder="请求源模型 (例如: gpt-4o)"
                  value={newRedirectSource}
                  onChange={(e) => setNewRedirectSource(e.target.value)}
                  className="bg-kumo-base text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand flex-1 min-w-[140px] font-mono"
                />
                <ArrowRight className="w-4 h-4 text-kumo-subtle" />
                <input
                  type="text"
                  placeholder="映射目标模型 (例如: gemini-1.5-pro)"
                  value={newRedirectTarget}
                  onChange={(e) => setNewRedirectTarget(e.target.value)}
                  className="bg-kumo-base text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand flex-1 min-w-[140px] font-mono"
                />
                <Button onClick={addRedirectRule} disabled={!newRedirectSource || !newRedirectTarget} className="flex items-center gap-1.5">
                  <Plus className="w-3.5 h-3.5" />
                  <span>保存规则</span>
                </Button>
              </div>
            )}
          </div>
        </div>
      )}

      {/* ==================== Dialogs & modals ==================== */}

      {/* 1. Manual Add Credentials Dialog */}
      <Dialog.Root open={manualAddOpen} onOpenChange={setManualAddOpen}>
        <Dialog className="p-6 sm:max-w-md bg-kumo-base border border-kumo-line rounded-lg shadow-lg">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            手动添加凭证 (Access & Refresh Tokens)
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            手动录入已获得的 Google OAuth Access / Refresh 令牌细节。
          </Dialog.Description>

          <div className="space-y-4 text-xs">
            <div className="space-y-1">
              <label className="font-semibold text-kumo-strong">凭证备注名称</label>
              <input
                type="text"
                value={manualForm.name}
                onChange={(e) => setManualForm({ ...manualForm, name: e.target.value })}
                placeholder="我的谷歌主账号"
                className="w-full bg-kumo-recessed text-kumo-strong px-3 py-2 border border-kumo-line rounded-lg focus:outline-none"
              />
            </div>

            <div className="space-y-1">
              <label className="font-semibold text-kumo-strong">Access Token</label>
              <textarea
                value={manualForm.accessToken}
                onChange={(e) => setManualForm({ ...manualForm, accessToken: e.target.value })}
                placeholder="Google OAuth 获得的短期 Access Token..."
                rows={3}
                className="w-full bg-kumo-recessed text-kumo-strong p-2.5 border border-kumo-line rounded-lg focus:outline-none font-mono resize-none"
              />
            </div>

            <div className="space-y-1">
              <label className="font-semibold text-kumo-strong">Refresh Token</label>
              <input
                type="text"
                value={manualForm.refreshToken}
                onChange={(e) => setManualForm({ ...manualForm, refreshToken: e.target.value })}
                placeholder="长期持久化的 Refresh Token"
                className="w-full bg-kumo-recessed text-kumo-strong px-3 py-2 border border-kumo-line rounded-lg focus:outline-none font-mono"
              />
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1">
                <label className="font-semibold text-kumo-strong">Project ID</label>
                <input
                  type="text"
                  value={manualForm.projectId}
                  onChange={(e) => setManualForm({ ...manualForm, projectId: e.target.value })}
                  placeholder="项目 ID (留空为自动)"
                  className="w-full bg-kumo-recessed text-kumo-strong px-3 py-2 border border-kumo-line rounded-lg focus:outline-none font-mono"
                />
              </div>
              <div className="space-y-1">
                <label className="font-semibold text-kumo-strong">失效时长 (秒)</label>
                <input
                  type="number"
                  value={manualForm.expiresIn}
                  onChange={(e) => setManualForm({ ...manualForm, expiresIn: parseInt(e.target.value) || 3599 })}
                  className="w-full bg-kumo-recessed text-kumo-strong px-3 py-2 border border-kumo-line rounded-lg focus:outline-none"
                />
              </div>
            </div>

            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close>
                <Button>取消</Button>
              </Dialog.Close>
              <Button variant="primary" disabled={manualSaving} onClick={saveManualAccount}>
                {manualSaving ? '录入中...' : '录入凭证'}
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 2. Add/Edit Account remark/detail Dialog */}
      <Dialog.Root open={accountFormOpen} onOpenChange={setAccountFormOpen}>
        <Dialog className="p-6 sm:max-w-md bg-kumo-base border border-kumo-line rounded-lg shadow-lg">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            {editingAccount ? '编辑凭证参数' : '新建授权账号'}
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            更改用于在系统标识和通信使用的备注及邮箱设定。
          </Dialog.Description>

          <div className="space-y-4 text-xs">
            <div className="space-y-1">
              <label className="font-semibold text-kumo-strong">备注名称</label>
              <input
                type="text"
                value={accountForm.name}
                onChange={(e) => setAccountForm({ ...accountForm, name: e.target.value })}
                className="w-full bg-kumo-recessed text-kumo-strong px-3 py-2 border border-kumo-line rounded-lg focus:outline-none"
              />
            </div>

            <div className="space-y-1">
              <label className="font-semibold text-kumo-strong">邮箱地址</label>
              <input
                type="email"
                value={accountForm.email}
                onChange={(e) => setAccountForm({ ...accountForm, email: e.target.value })}
                className="w-full bg-kumo-recessed text-kumo-strong px-3 py-2 border border-kumo-line rounded-lg focus:outline-none"
              />
            </div>

            <div className="space-y-1">
              <label className="font-semibold text-kumo-strong">登录密码 (选填)</label>
              <input
                type="password"
                value={accountForm.password}
                onChange={(e) => setAccountForm({ ...accountForm, password: e.target.value })}
                className="w-full bg-kumo-recessed text-kumo-strong px-3 py-2 border border-kumo-line rounded-lg focus:outline-none"
              />
            </div>

            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close>
                <Button>取消</Button>
              </Dialog.Close>
              <Button variant="primary" disabled={accountSaving} onClick={saveEditingAccount}>
                保存
              </Button>
            </div>
          </div>
        </Dialog>
      </Dialog.Root>

      {/* 3. Log details dialog */}
      <Dialog.Root open={logDetailOpen} onOpenChange={setLogDetailOpen}>
        <Dialog className="p-0 sm:max-w-xl bg-kumo-base border border-kumo-line rounded-lg shadow-lg flex flex-col max-h-[85vh]">
          <div className="p-4 border-b border-kumo-line flex items-center justify-between">
            <h3 className="text-sm font-bold text-kumo-strong">调用详细日志信息</h3>
            <button onClick={() => setLogDetailOpen(false)} className="p-1 hover:bg-kumo-recessed rounded text-kumo-subtle">
              <X className="w-4 h-4" />
            </button>
          </div>

          {logDetail && (
            <div className="flex-1 overflow-y-auto p-4 space-y-4 text-xs leading-relaxed max-h-[60vh]">
              <div className="grid grid-cols-2 gap-3 bg-kumo-recessed/35 p-3 rounded-lg border border-kumo-line font-mono text-[10px]">
                <div>
                  <span className="text-kumo-subtle font-bold block">日志 ID</span>
                  <span className="text-kumo-strong font-semibold">#{logDetail.id}</span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">触发时间</span>
                  <span className="text-kumo-strong font-semibold">{formatDateTime(logDetail.timestamp)}</span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">状态状态码</span>
                  <span
                    className={`px-1.5 rounded font-bold ${
                      logDetail.statusCode >= 200 && logDetail.statusCode < 300
                        ? 'bg-kumo-success/10 text-kumo-success border border-kumo-success/20'
                        : 'bg-kumo-danger/10 text-kumo-danger border border-kumo-danger/20'
                    }`}
                  >
                    {logDetail.statusCode}
                  </span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">耗时延迟</span>
                  <span className="text-kumo-strong font-bold">{logDetail.durationMs}ms</span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">请求路径</span>
                  <span className="text-kumo-strong font-semibold font-mono">{logDetail.path}</span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">关联模型</span>
                  <span className="text-kumo-strong font-semibold">{logDetail.model || '-'}</span>
                </div>
              </div>

              {/* Raw JSON switch */}
              <div className="flex justify-between items-center bg-kumo-recessed/10 p-2 border border-kumo-line rounded-lg">
                <span className="font-bold text-kumo-strong">显示原始完整 JSON 报文</span>
                <input
                  type="checkbox"
                  checked={logDetailRaw}
                  onChange={(e) => setLogDetailRaw(e.target.checked)}
                  className="w-8 h-4 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-3 before:h-3 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
                />
              </div>

              {logDetailRaw ? (
                <pre className="p-3 bg-kumo-recessed border border-kumo-line rounded-lg text-[10px] text-kumo-strong overflow-x-auto font-mono whitespace-pre">
                  {JSON.stringify(logDetail, null, 2)}
                </pre>
              ) : (
                <div className="space-y-4">
                  {/* Messages flow details */}
                  {logDetail.requestBody && (
                    <div className="space-y-2">
                      <h4 className="font-bold text-kumo-strong">Request Body (载荷)</h4>
                      <pre className="p-3 bg-kumo-recessed border border-kumo-line rounded-lg text-[10px] text-kumo-strong overflow-x-auto font-mono whitespace-pre-wrap max-h-40">
                        {typeof logDetail.requestBody === 'string'
                          ? logDetail.requestBody
                          : JSON.stringify(logDetail.requestBody, null, 2)}
                      </pre>
                    </div>
                  )}

                  {logDetail.responseBody && (
                    <div className="space-y-2">
                      <h4 className="font-bold text-kumo-strong">Response Body (响应)</h4>
                      <pre className="p-3 bg-kumo-recessed border border-kumo-line rounded-lg text-[10px] text-kumo-strong overflow-x-auto font-mono whitespace-pre-wrap max-h-40">
                        {typeof logDetail.responseBody === 'string'
                          ? logDetail.responseBody
                          : JSON.stringify(logDetail.responseBody, null, 2)}
                      </pre>
                    </div>
                  )}

                  {logDetail.errorLog && (
                    <div className="p-3 bg-kumo-danger/10 border border-kumo-danger/25 rounded-lg">
                      <div className="text-kumo-danger font-bold flex items-center gap-1.5 mb-1.5">
                        <AlertTriangle className="w-4 h-4" />
                        错误详细日志
                      </div>
                      <pre className="text-[11px] text-kumo-danger font-mono whitespace-pre-wrap leading-relaxed">
                        {logDetail.errorLog}
                      </pre>
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

export default AntigravityPage;
