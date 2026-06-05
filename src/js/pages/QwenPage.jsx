import React, { useState, useEffect, useCallback, useMemo } from 'react';
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
  RefreshCw,
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
  MessageSquare
} from '../components/Icons.jsx';

function QwenPage() {
  const [activeTab, setActiveTab] = useState('models'); // 'models' | 'accounts' | 'logs' | 'settings'

  // Authentication helper
  const getAuthHeaders = useCallback(() => {
    const password = localStorage.getItem('admin_password') || '';
    return {
      'Content-Type': 'application/json',
      'x-admin-password': password,
    };
  }, []);

  const formatTokens = (tokens) => {
    if (tokens >= 1000000) {
      return (tokens / 1000000).toFixed(2) + ' M';
    }
    if (tokens >= 1000) {
      return (tokens / 1000).toFixed(1) + ' K';
    }
    return tokens;
  };

  // ==================== 1. Stats & Model Matrix State ====================
  const [stats, setStats] = useState(null);
  const [matrix, setMatrix] = useState({});
  const [matrixLoading, setMatrixLoading] = useState(false);
  const [modelsSyncing, setModelsSyncing] = useState(false);

  const loadStats = useCallback(async () => {
    try {
      const response = await fetch('/api/qwen/stats', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      setStats(data);
    } catch (error) {
      console.error('加载 Qwen 统计失败:', error);
    }
  }, [getAuthHeaders]);

  const loadMatrix = useCallback(async (showFeedback = false) => {
    setMatrixLoading(true);
    try {
      const response = await fetch('/api/qwen/matrix', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      setMatrix(data);
      if (showFeedback) {
        toast.success('矩阵配置已刷新');
      }
    } catch (error) {
      console.error('加载 Qwen 矩阵失败:', error);
    } finally {
      setMatrixLoading(false);
    }
  }, [getAuthHeaders]);

  const syncQwenModels = async () => {
    setModelsSyncing(true);
    toast.info('正在同步云端模型列表...');
    try {
      const response = await fetch('/api/qwen/sync-models', {
        method: 'POST',
        headers: getAuthHeaders(),
      });

      if (!response.ok) {
        const errData = await response.json().catch(() => ({}));
        throw new Error(errData.error || `HTTP ${response.status}`);
      }

      const result = await response.json();
      toast.success(`同步完成！发现 ${result.count} 个模型${result.added > 0 ? ` (新增 ${result.added} 个)` : ''}`);
      loadMatrix(false);
    } catch (error) {
      toast.error('同步失败: ' + error.message);
    } finally {
      setModelsSyncing(false);
    }
  };

  const updateMatrixItem = async (modelId, field, value) => {
    try {
      const response = await fetch(`/api/qwen/matrix/${modelId}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ [field]: value }),
      });
      if (response.ok) {
        loadMatrix(false);
      } else {
        toast.error('更新失败');
      }
    } catch (error) {
      console.error('更新矩阵失败:', error);
    }
  };

  // ==================== 2. Accounts State ====================
  const [accounts, setAccounts] = useState([]);
  const [accountsLoading, setAccountsLoading] = useState(false);
  const [addAccountOpen, setAddAccountOpen] = useState(false);
  const [accountForm, setAccountForm] = useState({ name: '', token: '' });
  const [accountSaving, setAccountSaving] = useState(false);

  const loadAccounts = useCallback(async (showFeedback = false) => {
    setAccountsLoading(true);
    try {
      const response = await fetch('/api/qwen/accounts', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        setAccounts(data);
        if (showFeedback) {
          toast.success('凭证列表已刷新');
        }
      }
    } catch (error) {
      console.error('加载 Qwen 凭证失败:', error);
    } finally {
      setAccountsLoading(false);
    }
  }, [getAuthHeaders]);

  const refreshAccounts = async () => {
    await loadAccounts(true);
  };

  const addQwenAccount = async () => {
    if (!accountForm.token.trim()) {
      toast.warning('请输入凭证内容 (Cookie)');
      return;
    }
    setAccountSaving(true);
    try {
      const response = await fetch('/api/qwen/accounts', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(accountForm),
      });

      if (response.ok) {
        toast.success('凭证添加成功');
        setAddAccountOpen(false);
        setAccountForm({ name: '', token: '' });
        loadAccounts(false);
      } else {
        const err = await response.json().catch(() => ({}));
        toast.error('添加失败: ' + (err.error || '未知错误'));
      }
    } catch (error) {
      toast.error('请求失败: ' + error.message);
    } finally {
      setAccountSaving(false);
    }
  };

  const toggleAccountEnabled = async (id) => {
    const acc = accounts.find(a => a.id === id);
    if (!acc) return;
    const newEnable = acc.enable === false ? true : false;
    try {
      const response = await fetch(`/api/qwen/accounts/${id}`, {
        method: 'PUT',
        headers: getAuthHeaders(),
        body: JSON.stringify({ enable: newEnable }),
      });
      if (response.ok) {
        toast.success(newEnable ? '凭证已启用' : '凭证已禁用');
        loadAccounts(false);
      } else {
        toast.error('操作失败');
      }
    } catch (error) {
      console.error('切换状态失败:', error);
    }
  };

  const deleteAccount = async (id) => {
    if (!window.confirm('确定要删除此凭证吗？此操作不可恢复。')) return;
    try {
      const response = await fetch(`/api/qwen/accounts/${id}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('凭证已安全删除');
        loadAccounts(false);
      } else {
        toast.error('删除凭证失败');
      }
    } catch (error) {
      toast.error('请求异常: ' + error.message);
    }
  };

  // ==================== 3. Logs State ====================
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
      const response = await fetch('/api/qwen/logs', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      if (Array.isArray(data)) {
        setLogs(data);
        if (showFeedback) {
          toast.success('调用日志已刷新');
        }
      }
    } catch (error) {
      console.error('加载调用日志失败:', error);
    } finally {
      setLogsLoading(false);
    }
  }, [getAuthHeaders]);

  const clearLogs = async () => {
    if (!window.confirm('确定要清空 Qwen 的所有调用日志吗？')) return;
    try {
      const response = await fetch('/api/qwen/logs', {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('调用日志已清空');
        setLogs([]);
      } else {
        toast.error('清空日志失败');
      }
    } catch (error) {
      toast.error('请求失败: ' + error.message);
    }
  };

  const getLogModelsList = useMemo(() => {
    const list = new Set();
    logs.forEach(l => {
      if (l.model) list.add(l.model);
    });
    return Array.from(list).sort();
  }, [logs]);

  const filteredLogs = useMemo(() => {
    let list = [...logs];
    if (logFilterAccount) {
      list = list.filter(l => l.account_id === logFilterAccount || l.account_name === logFilterAccount);
    }
    if (logFilterModel) {
      list = list.filter(l => l.model === logFilterModel);
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

  const showLogDetailDialog = (log) => {
    setLogDetailShowRaw(false);
    setLogDetail(log);
    setLogDetailOpen(true);
  };

  const parseLogMessages = (msgs) => {
    if (!msgs) return [];
    try {
      return typeof msgs === 'string' ? JSON.parse(msgs) : msgs;
    } catch (e) {
      return [];
    }
  };

  const copyLogDetailJson = () => {
    if (!logDetail) return;
    const text = JSON.stringify(logDetail, null, 2);
    navigator.clipboard.writeText(text).then(() => {
      toast.success('JSON 已成功复制');
    }).catch(() => {
      toast.error('复制失败');
    });
  };

  // ==================== 4. Settings State ====================
  const [settingsForm, setSettingsForm] = useState({
    API_KEY: '',
    SYSTEM_INSTRUCTION: ''
  });
  const [settingsSaving, setSettingsSaving] = useState(false);
  const [showApiKey, setShowApiKey] = useState(false);

  // Model redirects
  const [redirects, setRedirects] = useState([]);
  const [editingRedirectSource, setEditingRedirectSource] = useState(null);
  const [newRedirectSource, setNewRedirectSource] = useState('');
  const [newRedirectTarget, setNewRedirectTarget] = useState('');

  const loadSettings = useCallback(async (showFeedback = false) => {
    try {
      const response = await fetch('/api/qwen/settings', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      setSettingsForm({
        API_KEY: data.API_KEY || '',
        SYSTEM_INSTRUCTION: data.SYSTEM_INSTRUCTION || ''
      });
      if (showFeedback) {
        toast.success('配置已从服务器更新');
      }
    } catch (error) {
      console.error('加载设置失败:', error);
    }
  }, [getAuthHeaders]);

  const saveSettings = async () => {
    setSettingsSaving(true);
    try {
      const response = await fetch('/api/qwen/settings', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(settingsForm),
      });
      if (response.ok) {
        toast.success('全局设置保存成功');
        loadSettings(false);
      } else {
        toast.error('保存设置失败');
      }
    } catch (error) {
      toast.error('请求保存配置失败: ' + error.message);
    } finally {
      setSettingsSaving(false);
    }
  };

  const loadRedirects = useCallback(async () => {
    try {
      const response = await fetch('/api/qwen/models/redirects', {
        headers: getAuthHeaders(),
      });
      const data = await response.json();
      setRedirects(data);
    } catch (error) {
      console.error('加载模型重定向失败:', error);
    }
  }, [getAuthHeaders]);

  const addRedirectRule = async () => {
    const source = newRedirectSource.trim();
    const target = newRedirectTarget.trim();
    if (!source || !target) return;

    try {
      if (editingRedirectSource && editingRedirectSource !== source) {
        await fetch(`/api/qwen/models/redirects/${encodeURIComponent(editingRedirectSource)}`, {
          method: 'DELETE',
          headers: getAuthHeaders(),
        });
      }

      const response = await fetch('/api/qwen/models/redirects', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ sourceModel: source, targetModel: target }),
      });

      if (response.ok) {
        toast.success('保存成功');
        setNewRedirectSource('');
        setNewRedirectTarget('');
        setEditingRedirectSource(null);
        loadRedirects();
      } else {
        const err = await response.json().catch(() => ({}));
        toast.error('保存失败: ' + (err.error || '未知原因'));
      }
    } catch (error) {
      toast.error('请求异常: ' + error.message);
    }
  };

  const removeRedirectRule = async (sourceModel) => {
    if (!window.confirm(`确认删除 ${sourceModel} 的重定向别名规则吗？`)) return;
    try {
      const response = await fetch(`/api/qwen/models/redirects/${encodeURIComponent(sourceModel)}`, {
        method: 'DELETE',
        headers: getAuthHeaders(),
      });
      if (response.ok) {
        toast.success('已删除');
        loadRedirects();
      }
    } catch (error) {
      toast.error('删除重定向失败: ' + error.message);
    }
  };

  const getBaseUrl = () => {
    const hostUrl = window.location.origin;
    return `${hostUrl}/v1`;
  };

  const copyQwenEndpoint = () => {
    navigator.clipboard.writeText(getBaseUrl()).then(() => {
      toast.success('Base URL 已复制');
    }).catch(() => {
      toast.error('复制失败，请手动复制');
    });
  };

  // Initial loader
  useEffect(() => {
    if (activeTab === 'models') {
      loadStats();
      loadMatrix();
    } else if (activeTab === 'accounts') {
      loadAccounts();
    } else if (activeTab === 'logs') {
      loadLogs();
      loadAccounts();
    } else if (activeTab === 'settings') {
      loadSettings();
      loadRedirects();
    }
  }, [activeTab, loadStats, loadMatrix, loadAccounts, loadLogs, loadSettings, loadRedirects]);

  return (
    <div className="space-y-6 flex flex-col h-full min-h-[75vh]">
      {/* Sub Tabs */}
      <div className="flex border border-kumo-line rounded-lg p-0.5 bg-kumo-recessed self-start select-none">
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
          <span>凭证管理</span>
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

      {/* ==================== 1. 模型矩阵 Tab ==================== */}
      {activeTab === 'models' && (
        <div className="quick-fade-in space-y-6">
          {/* Stats Summary */}
          {stats && (
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
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
                    {stats.avg_duration ? stats.avg_duration.toFixed(0) : '0'}
                    <small className="text-[10px] ml-0.5 text-kumo-subtle font-semibold">ms</small>
                  </span>
                </div>
                <div className="p-2 bg-kumo-brand/10 rounded-lg text-kumo-brand">
                  <Activity className="w-5 h-5" />
                </div>
              </div>
            </div>
          )}

          {/* Model Matrix table */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            <div className="p-4 border-b border-kumo-line flex justify-between items-center bg-kumo-recessed/10">
              <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
                <Cpu className="w-4 h-4 text-kumo-brand" />
                官网模型矩阵
              </h3>
              <div className="flex gap-2">
                <Button onClick={syncQwenModels} disabled={modelsSyncing} className="flex items-center gap-1">
                  <Download className={`w-3.5 h-3.5 ${modelsSyncing ? 'animate-spin' : ''}`} />
                  <span>同步云端</span>
                </Button>
                <Button onClick={() => loadMatrix(true)} disabled={matrixLoading} className="flex items-center gap-1">
                  <RefreshCw className={`w-3.5 h-3.5 ${matrixLoading ? 'animate-spin' : ''}`} />
                  <span>刷新矩阵</span>
                </Button>
              </div>
            </div>

            <div className="overflow-x-auto">
              <table className="w-full border-collapse text-left text-xs">
                <thead>
                  <tr className="border-b border-kumo-line bg-kumo-recessed/20">
                    <th className="p-3 font-semibold text-kumo-strong w-1/2">模型 ID</th>
                    <th className="p-3 font-semibold text-kumo-strong">数据源</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center w-28">启用状态</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-kumo-line">
                  {Object.keys(matrix).length === 0 ? (
                    <tr>
                      <td colSpan={3} className="p-8 text-center text-kumo-subtle">
                        暂无模型配置数据，请尝试同步云端。
                      </td>
                    </tr>
                  ) : (
                    Object.entries(matrix).map(([id, config]) => (
                      <tr key={id} className="hover:bg-kumo-recessed/5">
                        <td className="p-3 font-mono font-semibold text-kumo-strong">{id}</td>
                        <td className="p-3 text-kumo-subtle font-mono text-[10px]">Official API</td>
                        <td className="p-3 text-center">
                          <input
                            type="checkbox"
                            checked={!!config.enabled}
                            onChange={(e) => updateMatrixItem(id, 'enabled', e.target.checked)}
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

      {/* ==================== 2. 凭证管理 Tab ==================== */}
      {activeTab === 'accounts' && (
        <div className="quick-fade-in space-y-6">
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-4 flex flex-col sm:flex-row justify-between items-start sm:items-center gap-4">
            <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
              <Users className="w-4 h-4 text-kumo-brand" />
              Qwen 凭证管理
            </h3>
            <div className="flex gap-2">
              <Button variant="primary" onClick={() => setAddAccountOpen(true)} className="flex items-center gap-1">
                <Plus className="w-3.5 h-3.5" />
                <span>添加凭证</span>
              </Button>
              <Button onClick={refreshAccounts} disabled={accountsLoading} className="flex items-center gap-1">
                <RefreshCw className={`w-3.5 h-3.5 ${accountsLoading ? 'animate-spin' : ''}`} />
                <span>刷新全部</span>
              </Button>
            </div>
          </div>

          {/* Accounts list table */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            <div className="overflow-x-auto">
              <table className="w-full border-collapse text-left text-xs">
                <thead>
                  <tr className="border-b border-kumo-line bg-kumo-recessed/20">
                    <th className="p-3 font-semibold text-kumo-strong">标识名称</th>
                    <th className="p-3 font-semibold text-kumo-strong">凭证指纹</th>
                    <th className="p-3 font-semibold text-kumo-strong w-36">状态</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center w-28">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-kumo-line">
                  {accounts.length === 0 ? (
                    <tr>
                      <td colSpan={4} className="p-8 text-center text-kumo-subtle">
                        暂无可用凭证，请点击「添加凭证」提交配置。
                      </td>
                    </tr>
                  ) : (
                    accounts.map((acc) => (
                      <tr key={acc.id} className="hover:bg-kumo-recessed/5">
                        <td className="p-3 font-bold text-kumo-strong">{acc.name || 'Default'}</td>
                        <td className="p-3 font-mono text-kumo-subtle text-[10px]">
                          <code>{acc.token ? acc.token.substring(0, 24) : '---'}...</code>
                        </td>
                        <td className="p-3">
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
                              onClick={() => toggleAccountEnabled(acc.id)}
                              className={`p-1.5 rounded hover:bg-kumo-recessed transition-colors ${
                                acc.enable !== false ? 'text-kumo-success' : 'text-kumo-subtle'
                              }`}
                              title={acc.enable !== false ? '禁用' : '启用'}
                            >
                              <Check className="w-4 h-4" />
                            </button>
                            <button
                              onClick={() => deleteAccount(acc.id)}
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
        </div>
      )}

      {/* ==================== 3. 调用日志 Tab ==================== */}
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
              <table className="w-full border-collapse text-left text-xs">
                <thead>
                  <tr className="border-b border-kumo-line bg-kumo-recessed/20">
                    <th className="p-3 font-semibold text-kumo-strong text-center" style={{ width: '150px' }}>调用时间</th>
                    <th className="p-3 font-semibold text-kumo-strong">账号备注</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center" style={{ width: '140px' }}>调用模型</th>
                    <th className="p-3 font-semibold text-kumo-strong">接口路径</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center" style={{ width: '70px' }}>状态</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center" style={{ width: '80px' }}>耗时</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center" style={{ width: '80px' }}>首字时间</th>
                    <th className="p-3 font-semibold text-kumo-strong text-center w-16">操作</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-kumo-line">
                  {filteredLogs.length === 0 ? (
                    <tr>
                      <td colSpan={8} className="p-10 text-center text-kumo-subtle">
                        尚无可用调用记录。
                      </td>
                    </tr>
                  ) : (
                    filteredLogs.map((log) => (
                      <tr key={log.id} className="hover:bg-kumo-recessed/5">
                        <td className="p-3 text-center text-kumo-subtle font-mono">{formatDateTime(log.created_at)}</td>
                        <td className="p-3 font-bold text-kumo-strong">
                          <div className="flex items-center gap-1">
                            <span>{log.account_name || log.account_id || '-'}</span>
                          </div>
                        </td>
                        <td className="p-3 text-center font-mono text-kumo-subtle">{log.model || '-'}</td>
                        <td className="p-3">
                          <div className="flex items-center gap-1.5 font-mono">
                            <span className="px-1 py-0.2 rounded text-[9px] bg-kumo-brand/10 text-kumo-brand border border-kumo-brand/20 font-bold uppercase">
                              POST
                            </span>
                            <span className="truncate text-kumo-strong">/v1/chat/completions</span>
                          </div>
                        </td>
                        <td className="p-3 text-center">
                          <span
                            className={`px-2 py-0.5 rounded text-[10px] font-bold ${getLogStatusClass(
                              log.status_code || (log.status === 'success' ? 200 : 500)
                            )}`}
                          >
                            {log.status_code || (log.status === 'success' ? 200 : 500)}
                          </span>
                        </td>
                        <td className="p-3 text-center font-mono font-semibold text-kumo-strong">{log.duration}ms</td>
                        <td className="p-3 text-center font-mono text-kumo-success">
                          {log.first_token_time_ms != null ? `${log.first_token_time_ms}ms` : '-'}
                        </td>
                        <td className="p-3 text-center">
                          <button
                            onClick={() => showLogDetailDialog(log)}
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

      {/* ==================== 4. 模块配置 Tab ==================== */}
      {activeTab === 'settings' && (
        <div className="quick-fade-in space-y-6">
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm overflow-hidden">
            <div className="p-4 border-b border-kumo-line flex justify-between items-center bg-kumo-recessed/10">
              <h3 className="text-sm font-bold text-kumo-strong flex items-center gap-2">
                <Sliders className="w-4 h-4 text-kumo-brand" />
                Qwen 模块全局变量配置
              </h3>
              <div className="flex gap-2">
                <Button onClick={() => loadSettings(true)}>
                  <RefreshCw className="w-3.5 h-3.5" />
                </Button>
                <Button variant="primary" onClick={saveSettings} disabled={settingsSaving}>
                  <span>{settingsSaving ? '保存中...' : '保存全局配置'}</span>
                </Button>
              </div>
            </div>

            <div className="p-5 space-y-5">
              <div className="space-y-1">
                <label className="text-xs font-semibold text-kumo-strong block">API 访问密钥 (API_KEY)</label>
                <div className="relative">
                  <input
                    type={showApiKey ? 'text' : 'password'}
                    value={settingsForm.API_KEY}
                    onChange={(e) => setSettingsForm({ ...settingsForm, API_KEY: e.target.value })}
                    placeholder="留空则不启用外部接口鉴权"
                    className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 pr-10 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono"
                  />
                  <button
                    onClick={() => setShowApiKey(!showApiKey)}
                    className="absolute right-3 top-2.5 text-kumo-subtle hover:text-kumo-strong"
                  >
                    {showApiKey ? <Eye className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                  </button>
                </div>
              </div>

              <div className="space-y-1">
                <label className="text-xs font-semibold text-kumo-strong block">全局系统指令 (Global System Instruction)</label>
                <textarea
                  value={settingsForm.SYSTEM_INSTRUCTION}
                  onChange={(e) => setSettingsForm({ ...settingsForm, SYSTEM_INSTRUCTION: e.target.value })}
                  placeholder="设置后将作为 System Prompt 注入到每次 Qwen 的对话之中..."
                  rows={3}
                  className="w-full bg-kumo-recessed text-kumo-strong text-xs p-3 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono resize-y"
                />
              </div>
            </div>
          </div>

          {/* Model redirect management */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-5 space-y-4">
            <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-1.5">
              <Globe className="w-4 h-4 text-kumo-brand" />
              模型重定向别名路由
            </h4>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
              {redirects.length === 0 ? (
                <div className="col-span-2 text-center py-4 border border-dashed border-kumo-line rounded-lg text-kumo-subtle text-xs">
                  暂无重定向配置。外部请求的 source 模型将自动转发给实际的 target 模型。
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
                              onClick={addRedirectRule}
                              className="p-1 bg-kumo-success/15 hover:bg-kumo-success/25 rounded text-kumo-success"
                              title="保存"
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
                              onClick={() => removeRedirectRule(r.source_model)}
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

            {/* Redirection add input */}
            {!editingRedirectSource && (
              <div className="flex flex-wrap gap-2.5 items-center p-4 bg-kumo-brand/5 border border-dashed border-kumo-brand/20 rounded-lg">
                <input
                  type="text"
                  placeholder="源别名模型 (例如: gpt-3.5-turbo)"
                  value={newRedirectSource}
                  onChange={(e) => setNewRedirectSource(e.target.value)}
                  className="bg-kumo-base text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand flex-1 min-w-[140px] font-mono"
                />
                <ArrowRight className="w-4 h-4 text-kumo-subtle" />
                <input
                  type="text"
                  placeholder="实际重定向目标 (例如: qwen-plus)"
                  value={newRedirectTarget}
                  onChange={(e) => setNewRedirectTarget(e.target.value)}
                  className="bg-kumo-base text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand flex-1 min-w-[140px] font-mono"
                />
                <Button onClick={addRedirectRule} disabled={!newRedirectSource || !newRedirectTarget} className="flex items-center gap-1.5">
                  <Plus className="w-3.5 h-3.5" />
                  <span>添加重定向</span>
                </Button>
              </div>
            )}
          </div>

          {/* API guide */}
          <div className="bg-kumo-base border border-kumo-line rounded-lg shadow-sm p-5 space-y-4">
            <h4 className="text-xs font-bold text-kumo-strong flex items-center gap-2">
              <Plug className="w-4 h-4 text-kumo-brand" />
              API 统一接入指引
            </h4>

            <div className="space-y-3 text-xs leading-relaxed text-kumo-strong">
              <div className="border border-kumo-line rounded-lg overflow-hidden">
                <div className="p-2.5 bg-kumo-recessed/40 font-bold border-b border-kumo-line">Base URL</div>
                <div
                  onClick={copyQwenEndpoint}
                  className="p-3 bg-kumo-recessed/25 font-mono text-[11px] text-kumo-brand flex items-center justify-between cursor-pointer group"
                >
                  <span>{getBaseUrl()}</span>
                  <Copy className="w-4 h-4 opacity-50 group-hover:opacity-100 transition-opacity" />
                </div>
              </div>

              <div className="border border-kumo-line rounded-lg overflow-hidden">
                <div className="p-2.5 bg-kumo-recessed/40 font-bold border-b border-kumo-line">可用端点</div>
                <div className="p-3 space-y-1 bg-kumo-recessed/25 font-mono text-[11px]">
                  <div><span className="text-kumo-brand font-bold mr-2">POST</span>/v1/chat/completions (OpenAI SDK 对齐)</div>
                  <div><span className="text-kumo-brand font-bold mr-2">POST</span>/v1/images/generations (绘图接口)</div>
                </div>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* ==================== dialogs & modals ==================== */}

      {/* 1. Add Credential Dialog */}
      <Dialog.Root open={addAccountOpen} onOpenChange={setAddAccountOpen}>
        <Dialog className="p-6 sm:max-w-md bg-kumo-base border border-kumo-line rounded-lg shadow-lg">
          <Dialog.Title className="text-sm font-bold text-kumo-strong mb-1">
            添加通义千问官方凭证
          </Dialog.Title>
          <Dialog.Description className="text-xs text-kumo-subtle mb-4">
            配置用于请求 Qwen 官网免费额度接口的 Web Session Cookie 凭证。
          </Dialog.Description>

          <div className="space-y-4">
            <div className="p-3 bg-kumo-recessed/40 border border-kumo-line rounded-lg text-xs space-y-1">
              <div className="font-bold text-kumo-strong flex items-center gap-1">
                <AlertTriangle className="w-3.5 h-3.5 text-yellow-500" />
                如何获取 Cookie？
              </div>
              <p className="text-kumo-subtle text-[10px] leading-relaxed">
                在浏览器中登录通义千问官网，按 F12 打开开发者工具，在网络 (Network) 面板中任意选中一个带有 <code>Request Headers</code> 的请求，复制其中完整的 <code>Cookie</code> 文本值。
              </p>
            </div>

            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">凭证备注</label>
              <input
                type="text"
                value={accountForm.name}
                onChange={(e) => setAccountForm({ ...accountForm, name: e.target.value })}
                placeholder="例如：通义千问主账号-免费额度"
                className="w-full bg-kumo-recessed text-kumo-strong text-xs px-3 py-2 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand"
              />
            </div>

            <div className="space-y-1">
              <label className="text-xs font-semibold text-kumo-strong">Cookie 内容</label>
              <textarea
                value={accountForm.token}
                onChange={(e) => setAccountForm({ ...accountForm, token: e.target.value })}
                placeholder="粘贴全量 Cookie 字符串..."
                rows={4}
                className="w-full bg-kumo-recessed text-kumo-strong text-xs p-3 border border-kumo-line rounded-lg focus:outline-none focus:border-kumo-brand font-mono resize-y"
              />
            </div>

            <div className="flex justify-end gap-3 pt-2">
              <Dialog.Close>
                <Button>取消</Button>
              </Dialog.Close>
              <Button variant="primary" disabled={accountSaving} onClick={addQwenAccount}>
                {accountSaving ? '提交中...' : '提交入库'}
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
            <h3 className="text-sm font-bold text-kumo-strong">Qwen 调用详情</h3>
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
                  <span className="text-kumo-subtle font-bold block">请求标识 ID</span>
                  <span className="text-kumo-strong font-semibold">#{logDetail.id || logDetail.trace_id?.substring(0,8) || '-'}</span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">发生时间</span>
                  <span className="text-kumo-strong font-semibold">{formatDateTime(logDetail.created_at)}</span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">状态状态</span>
                  <span className={`px-1.5 rounded font-bold ${getLogStatusClass(logDetail.status_code || (logDetail.status === 'success' ? 200 : 500))}`}>
                    {logDetail.status_code || (logDetail.status === 'success' ? 200 : 500)}
                  </span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">耗时延迟</span>
                  <span className="text-kumo-strong font-bold">{logDetail.duration}ms</span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">请求模型</span>
                  <span className="text-kumo-strong font-semibold">{logDetail.model || '-'}</span>
                </div>
                <div>
                  <span className="text-kumo-subtle font-bold block">Token 消耗</span>
                  <span className="text-kumo-strong font-bold">{logDetail.tokens || 0} tokens</span>
                </div>
              </div>

              {/* Mode Raw toggle */}
              <div className="flex justify-between items-center bg-kumo-recessed/10 p-2 border border-kumo-line rounded-lg">
                <span className="font-bold text-kumo-strong">以原始 JSON 视图显示</span>
                <input
                  type="checkbox"
                  checked={logDetailShowRaw}
                  onChange={(e) => setLogDetailShowRaw(e.target.checked)}
                  className="w-8 h-4 bg-kumo-recessed border border-kumo-line rounded-full cursor-pointer focus:outline-none appearance-none checked:bg-kumo-brand relative before:absolute before:left-0.5 before:top-0.5 before:w-3 before:h-3 before:bg-white before:rounded-full before:transition-transform checked:before:translate-x-4 border-box"
                />
              </div>

              {logDetailShowRaw ? (
                <div className="space-y-2">
                  <div className="flex justify-end">
                    <Button onClick={copyLogDetailJson} className="text-[10px] py-1 px-2.5">复制 JSON</Button>
                  </div>
                  <pre className="p-3 bg-kumo-recessed border border-kumo-line rounded-lg text-[10px] text-kumo-strong overflow-x-auto font-mono whitespace-pre">
                    {JSON.stringify(logDetail, null, 2)}
                  </pre>
                </div>
              ) : (
                <div className="space-y-4">
                  {/* Messages flow */}
                  {parseLogMessages(logDetail.messages).length > 0 && (
                    <div className="space-y-3">
                      <h4 className="font-bold text-kumo-strong">请求对话历史 (Messages)</h4>
                      <div className="space-y-2 max-h-48 overflow-y-auto pr-1">
                        {parseLogMessages(logDetail.messages).map((m, idx) => (
                          <div key={idx} className="p-2.5 rounded border border-kumo-line bg-kumo-recessed/20">
                            <span className="font-bold text-kumo-brand uppercase block text-[9px] mb-1">
                              {m.role === 'user' ? '用户 (User)' : '助手 (AI)'}
                            </span>
                            <p className="whitespace-pre-wrap text-[11px] text-kumo-strong">{m.content}</p>
                          </div>
                        ))}
                      </div>
                    </div>
                  )}

                  {/* final response choices */}
                  {(logDetail.response || logDetail.reasoning_content) && (
                    <div className="space-y-2.5">
                      <h4 className="font-bold text-kumo-strong">最终生成响应</h4>
                      <div className="p-3 rounded border border-kumo-line bg-kumo-recessed/45">
                        {logDetail.reasoning_content && (
                          <div className="mb-2.5 p-2 bg-yellow-500/10 border-l-2 border-yellow-500 rounded font-mono text-[10px] text-kumo-strong whitespace-pre-wrap">
                            <span className="font-bold block text-yellow-600 mb-0.5">思考过程:</span>
                            {logDetail.reasoning_content}
                          </div>
                        )}
                        <p className="whitespace-pre-wrap text-[11px] text-kumo-strong font-mono leading-relaxed">
                          {logDetail.response || '(空)'}
                        </p>
                      </div>
                    </div>
                  )}

                  {/* Error Log */}
                  {logDetail.status === 'error' && logDetail.error && (
                    <div className="p-3 bg-kumo-danger/10 border border-kumo-danger/25 rounded-lg">
                      <div className="text-kumo-danger font-bold flex items-center gap-1.5 mb-1.5">
                        <AlertTriangle className="w-4 h-4" />
                        错误日志信息
                      </div>
                      <pre className="text-[11px] text-kumo-danger font-mono whitespace-pre-wrap leading-relaxed">
                        {logDetail.error}
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

export default QwenPage;
