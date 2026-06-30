import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { LayerCard, Tabs } from '@cloudflare/kumo';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import useStore, {
  DEFAULT_MODULE_ORDER,
  MODULE_CONFIG,
  MODULE_GROUPS,
  applyCustomCss,
  normalizeUserSettings,
} from '../store.js';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { BackupPanel } from './BackupPage.jsx';
import {
  Activity,
  Bell,
  Check,
  Database,
  Download,
  Eye,
  EyeOff,
  FileText,
  Globe,
  HardDrive,
  LayoutDashboard,
  Lock,
  RefreshCw,
  Save,
  Settings,
  Shield,
  Sun,
  Terminal,
  Trash,
  Upload,
} from '../components/Icons.jsx';

const SETTINGS_TABS = [
  { value: 'general', label: <span className="inline-flex items-center gap-1.5"><LayoutDashboard className="h-4 w-4" />常规</span> },
  { value: 'modules', label: <span className="inline-flex items-center gap-1.5"><Activity className="h-4 w-4" />模块</span> },
  { value: 'security', label: <span className="inline-flex items-center gap-1.5"><Shield className="h-4 w-4" />安全</span> },
  { value: 'database', label: <span className="inline-flex items-center gap-1.5"><Database className="h-4 w-4" />数据库</span> },
  { value: 'logs', label: <span className="inline-flex items-center gap-1.5"><FileText className="h-4 w-4" />审计</span> },
  { value: 'appearance', label: <span className="inline-flex items-center gap-1.5"><Sun className="h-4 w-4" />外观</span> },
  { value: 'about', label: <span className="inline-flex items-center gap-1.5"><Settings className="h-4 w-4" />关于</span> },
];

const THEME_OPTIONS = [
  { value: 'auto', label: '跟随系统' },
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
];

const PAGE_WIDTH_OPTIONS = [
  { value: 'standard', label: '标准' },
  { value: 'wide', label: '宽屏' },
  { value: 'full', label: '全宽' },
];

const LOAD_BALANCING_OPTIONS = [
  { value: 'random', label: '随机' },
  { value: 'round_robin', label: '轮询' },
];

const SERVER_IP_DISPLAY_OPTIONS = [
  { value: 'normal', label: '明文' },
  { value: 'masked', label: '打码' },
  { value: 'hidden', label: '隐藏' },
];

const TOTP_INPUT_MODE_OPTIONS = [
  { value: 'scan', label: '扫码导入' },
  { value: 'upload', label: '上传二维码' },
  { value: 'manual', label: '手动录入' },
];



const GROUP_LABELS = {
  overview: '总览',
  'api-gateway': 'API 网关',
  infrastructure: '云服务',
  toolbox: '工具箱',
};

const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
  'x-admin-password': localStorage.getItem('admin_password') || useStore.getState().loginPassword || '',
});

const getUploadHeaders = () => ({
  'x-admin-password': localStorage.getItem('admin_password') || useStore.getState().loginPassword || '',
});

const formatFileSize = (bytes) => {
  const size = Number(bytes) || 0;
  if (size >= 1024 * 1024 * 1024) return `${(size / 1024 / 1024 / 1024).toFixed(2)} GB`;
  if (size >= 1024 * 1024) return `${(size / 1024 / 1024).toFixed(2)} MB`;
  if (size >= 1024) return `${(size / 1024).toFixed(2)} KB`;
  return `${size} B`;
};

const toInt = (value, fallback = 0) => {
  const parsed = parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : fallback;
};

const moveItem = (items, fromIndex, toIndex) => {
  const next = [...items];
  const [item] = next.splice(fromIndex, 1);
  next.splice(toIndex, 0, item);
  return next;
};

const moduleRows = DEFAULT_MODULE_ORDER.map((moduleId) => {
  const group = MODULE_GROUPS.find((item) => item.modules.includes(moduleId));
  return {
    id: moduleId,
    groupId: group?.id || 'other',
    groupName: GROUP_LABELS[group?.id] || group?.name || '其他',
    config: MODULE_CONFIG[moduleId] || { name: moduleId, icon: 'fa-cube' },
  };
});

function SectionHeader({ title, description, actions }) {
  return (
    <div className="flex flex-col gap-3 border-b border-kumo-line p-5 sm:flex-row sm:items-start sm:justify-between">
      <div className="min-w-0">
        <h2 className="text-base font-bold text-kumo-strong">{title}</h2>
        {description && <p className="mt-1 text-xs leading-relaxed text-kumo-subtle">{description}</p>}
      </div>
      {actions && <div className="flex shrink-0 flex-wrap gap-2">{actions}</div>}
    </div>
  );
}

function FieldRow({ title, description, children }) {
  return (
    <div className="grid gap-3 border-b border-kumo-line px-5 py-4 last:border-b-0 md:grid-cols-[minmax(0,1fr)_minmax(16rem,22rem)] md:items-center">
      <div className="min-w-0">
        <div className="text-sm font-semibold text-kumo-strong">{title}</div>
        {description && <div className="mt-1 text-xs leading-relaxed text-kumo-subtle">{description}</div>}
      </div>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

function StatCard({ label, value, hint, icon: Icon }) {
  return (
    <LayerCard className="p-5">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="text-[11px] font-semibold uppercase tracking-normal text-kumo-subtle">{label}</div>
          <div className="mt-2 truncate text-lg font-bold text-kumo-strong">{value}</div>
          {hint && <div className="mt-1 text-xs text-kumo-subtle">{hint}</div>}
        </div>
        {Icon && (
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg border border-kumo-line bg-kumo-recessed text-kumo-brand">
            <Icon className="h-4 w-4" />
          </div>
        )}
      </div>
    </LayerCard>
  );
}

function ToggleLine({ title, description, checked, onCheckedChange, disabled = false }) {
  return (
    <div className="flex items-start justify-between gap-4 border-b border-kumo-line py-4 last:border-b-0">
      <div className="min-w-0">
        <div className="text-sm font-semibold text-kumo-strong">{title}</div>
        {description && <div className="mt-1 text-xs leading-relaxed text-kumo-subtle">{description}</div>}
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} disabled={disabled} />
    </div>
  );
}

function SettingsPage() {
  const {
    themeMode,
    theme,
    setThemeMode,
    pageWidthMode,
    setPageWidthMode,
    applyUserSettings,
    loadUserSettings,
    logout,
    isDemoMode,
  } = useStore();

  const fileInputRef = useRef(null);
  const [activeTab, setActiveTab] = useState('general');
  const [settings, setSettings] = useState(() => normalizeUserSettings());
  const [settingsLoading, setSettingsLoading] = useState(true);
  const [settingsSaving, setSettingsSaving] = useState(false);



  const [passwordForm, setPasswordForm] = useState({
    oldPassword: '',
    newPassword: '',
    confirmPassword: '',
  });
  const [passwordSaving, setPasswordSaving] = useState(false);

  const [twoFA, setTwoFA] = useState({
    enabled: false,
    setupMode: false,
    disableMode: false,
    loading: false,
    secret: '',
    qrCode: '',
    token: '',
    disablePassword: '',
    error: '',
  });

  const [dbStats, setDbStats] = useState(null);
  const [dbAnalysis, setDbAnalysis] = useState(null);
  const [deprecatedTables, setDeprecatedTables] = useState(null);
  const [databaseBusy, setDatabaseBusy] = useState(false);
  const [dbImportPreview, setDbImportPreview] = useState(null);

  const [logSettings, setLogSettings] = useState({
    days: 0,
    count: 0,
    dbSizeMB: 0,
    logFileSizeMB: 10,
  });
  const [logFileInfo, setLogFileInfo] = useState(null);
  const [operationLogs, setOperationLogs] = useState([]);
  const [logsBusy, setLogsBusy] = useState(false);

  const currentOrigin = useMemo(() => {
    if (typeof window === 'undefined') return 'http://localhost';
    return window.location.origin;
  }, []);

  const tableRows = useMemo(() => {
    if (dbAnalysis?.tables?.length) return dbAnalysis.tables;
    return Object.entries(dbStats?.tables || {}).map(([table, rows]) => ({ table, rows }));
  }, [dbAnalysis, dbStats]);

  const patchSettings = useCallback((patch) => {
    setSettings((prev) => normalizeUserSettings({ ...prev, ...patch }));
  }, []);

  const updateTotpSetting = useCallback((key, value) => {
    setSettings((prev) => normalizeUserSettings({
      ...prev,
      totpSettings: {
        ...prev.totpSettings,
        [key]: value,
      },
    }));
  }, []);

  const handleThemeModeChange = useCallback((value) => {
    const nextMode = String(value);
    setThemeMode(nextMode);
    patchSettings({ themeMode: nextMode });
  }, [patchSettings, setThemeMode]);

  const handlePageWidthModeChange = useCallback((value) => {
    const nextMode = String(value);
    setPageWidthMode(nextMode);
    patchSettings({ pageWidthMode: nextMode });
  }, [patchSettings, setPageWidthMode]);

  const fetchSettings = useCallback(async () => {
    const response = await fetch('/api/settings', { headers: getAuthHeaders() });
    const result = await response.json();
    if (!response.ok || !result.success) throw new Error(result.error || '加载用户设置失败');
    const normalized = normalizeUserSettings(result.data || {});
    setSettings(normalized);
    applyUserSettings(normalized);
    return normalized;
  }, [applyUserSettings]);

  const fetchDbState = useCallback(async () => {
    const [statsResponse, analysisResponse, deprecatedResponse] = await Promise.all([
      fetch('/api/settings/database-stats', { headers: getAuthHeaders() }),
      fetch('/api/settings/database-analysis', { headers: getAuthHeaders() }),
      fetch('/api/settings/deprecated-tables', { headers: getAuthHeaders() }),
    ]);

    const statsResult = await statsResponse.json();
    if (statsResult.success) setDbStats(statsResult.data);

    const analysisResult = await analysisResponse.json();
    if (analysisResult.success) setDbAnalysis(analysisResult.data);

    const deprecatedResult = await deprecatedResponse.json();
    if (deprecatedResult.success) setDeprecatedTables(deprecatedResult.data);
  }, []);

  const fetchLogState = useCallback(async () => {
    const [logSettingsResponse, operationLogsResponse] = await Promise.all([
      fetch('/api/settings/log-settings', { headers: getAuthHeaders() }),
      fetch('/api/settings/operation-logs', { headers: getAuthHeaders() }),
    ]);

    const logSettingsResult = await logSettingsResponse.json();
    if (logSettingsResult.success) {
      setLogSettings(logSettingsResult.data);
      setLogFileInfo(logSettingsResult.fileInfo || null);
    }

    const operationLogsResult = await operationLogsResponse.json();
    if (operationLogsResult.success) {
      setOperationLogs(operationLogsResult.data || []);
    }
  }, []);

  const fetchTwoFAStatus = useCallback(async () => {
    const response = await fetch('/api/auth/2fa/status', { headers: getAuthHeaders() });
    const result = await response.json();
    if (result.success) {
      setTwoFA((prev) => ({ ...prev, enabled: !!result.enabled }));
    }
  }, []);



  const refreshAll = useCallback(async (showFeedback = false) => {
    setSettingsLoading(true);
    try {
      await Promise.all([
        fetchSettings(),
        fetchDbState(),
        fetchLogState(),
        fetchTwoFAStatus(),
      ]);
      if (showFeedback) toast.success('设置已刷新');
    } catch (error) {
      toast.error(error.message || '加载设置失败');
    } finally {
      setSettingsLoading(false);
    }
  }, [fetchDbState, fetchLogState, fetchSettings, fetchTwoFAStatus]);

  useEffect(() => {
    refreshAll(false);
  }, [refreshAll]);

  const persistSettings = async (nextSettings = settings, successMessage = '设置已保存') => {
    const normalized = normalizeUserSettings(nextSettings);
    setSettingsSaving(true);
    try {
      const response = await fetch('/api/settings', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(normalized),
      });
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || '保存设置失败');

      setSettings(normalized);
      applyUserSettings(normalized);
      applyCustomCss(normalized.customCss);
      toast.success(successMessage);
      return true;
    } catch (error) {
      toast.error(error.message || '保存设置失败');
      return false;
    } finally {
      setSettingsSaving(false);
    }
  };



  const changePassword = async () => {
    if (passwordForm.newPassword.length < 6) {
      toast.warning('新密码至少需要 6 位');
      return;
    }
    if (passwordForm.newPassword !== passwordForm.confirmPassword) {
      toast.error('两次输入的新密码不一致');
      return;
    }

    setPasswordSaving(true);
    try {
      const response = await fetch('/api/auth/change-password', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          oldPassword: passwordForm.oldPassword,
          newPassword: passwordForm.newPassword,
        }),
      });
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || result.msg || '修改密码失败');

      toast.success('密码已修改，请重新登录');
      setPasswordForm({ oldPassword: '', newPassword: '', confirmPassword: '' });
      setTimeout(() => logout(), 1200);
    } catch (error) {
      toast.error(error.message || '修改密码失败');
    } finally {
      setPasswordSaving(false);
    }
  };

  const start2FASetup = async () => {
    setTwoFA((prev) => ({ ...prev, loading: true, error: '' }));
    try {
      const response = await fetch('/api/auth/2fa/setup', {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || '获取 2FA 二维码失败');

      setTwoFA((prev) => ({
        ...prev,
        setupMode: true,
        secret: result.secret,
        qrCode: result.qrCode,
        token: '',
        error: '',
      }));
    } catch (error) {
      setTwoFA((prev) => ({ ...prev, error: error.message || '获取 2FA 二维码失败' }));
      toast.error(error.message || '获取 2FA 二维码失败');
    } finally {
      setTwoFA((prev) => ({ ...prev, loading: false }));
    }
  };

  const confirm2FASetup = async () => {
    if (!/^\d{6}$/.test(twoFA.token)) {
      setTwoFA((prev) => ({ ...prev, error: '请输入 6 位验证码' }));
      return;
    }

    setTwoFA((prev) => ({ ...prev, loading: true, error: '' }));
    try {
      const response = await fetch('/api/auth/2fa/enable', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ secret: twoFA.secret, token: twoFA.token }),
      });
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || '启用 2FA 失败');

      setTwoFA((prev) => ({
        ...prev,
        enabled: true,
        setupMode: false,
        secret: '',
        qrCode: '',
        token: '',
        error: '',
      }));
      toast.success('2FA 已启用');
    } catch (error) {
      setTwoFA((prev) => ({ ...prev, error: error.message || '启用 2FA 失败' }));
    } finally {
      setTwoFA((prev) => ({ ...prev, loading: false }));
    }
  };

  const disable2FA = async () => {
    if (!twoFA.disablePassword) {
      setTwoFA((prev) => ({ ...prev, error: '请输入当前密码' }));
      return;
    }

    setTwoFA((prev) => ({ ...prev, loading: true, error: '' }));
    try {
      const response = await fetch('/api/auth/2fa/disable', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ password: twoFA.disablePassword }),
      });
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || '禁用 2FA 失败');

      setTwoFA((prev) => ({
        ...prev,
        enabled: false,
        disableMode: false,
        disablePassword: '',
        error: '',
      }));
      toast.success('2FA 已禁用');
    } catch (error) {
      setTwoFA((prev) => ({ ...prev, error: error.message || '禁用 2FA 失败' }));
    } finally {
      setTwoFA((prev) => ({ ...prev, loading: false }));
    }
  };

  const saveLogSettings = async () => {
    setLogsBusy(true);
    try {
      const response = await fetch('/api/settings/log-settings', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(logSettings),
      });
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || '保存日志设置失败');
      setLogFileInfo(result.fileInfo || logFileInfo);
      toast.success('日志保留设置已保存');
    } catch (error) {
      toast.error(error.message || '保存日志设置失败');
    } finally {
      setLogsBusy(false);
    }
  };

  const postSettingsAction = async (path, successMessage, refresh = null, body = undefined) => {
    setLogsBusy(true);
    setDatabaseBusy(true);
    try {
      const response = await fetch(path, {
        method: 'POST',
        headers: getAuthHeaders(),
        body: body ? JSON.stringify(body) : undefined,
      });
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || '操作失败');
      toast.success(successMessage || result.message || '操作完成');
      if (refresh) await refresh();
    } catch (error) {
      toast.error(error.message || '操作失败');
    } finally {
      setLogsBusy(false);
      setDatabaseBusy(false);
    }
  };

  const exportDatabase = () => {
    window.location.href = '/api/settings/export-database';
  };

  const importDatabase = () => {
    fileInputRef.current?.click();
  };

  const previewDatabaseImport = async (event) => {
    const file = event.target.files?.[0];
    if (!file) return;

    const formData = new FormData();
    formData.append('database', file);
    setDatabaseBusy(true);
    setDbImportPreview(null);
    try {
      const response = await fetch('/api/settings/database/import/preview', {
        method: 'POST',
        headers: getUploadHeaders(),
        body: formData,
      });
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || '数据库预检失败');
      setDbImportPreview(result.data);
      if (result.data?.warnings?.length) {
        toast.warning('数据库预检通过，但存在警告');
      } else {
        toast.success('数据库预检通过，请确认后导入');
      }
    } catch (error) {
      toast.error(error.message || '数据库预检失败');
    } finally {
      setDatabaseBusy(false);
      if (event.target) event.target.value = '';
    }
  };

  const commitDatabaseImport = async () => {
    if (!dbImportPreview?.token) {
      toast.warning('请先上传数据库并完成预检');
      return;
    }
    if (!(await dialog.confirm('确定要替换当前数据库吗？系统会先备份当前数据库，导入后页面将刷新。'))) {
      return;
    }

    setDatabaseBusy(true);
    try {
      const response = await fetch('/api/settings/database/import/commit', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ token: dbImportPreview.token, confirm: true }),
      });
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || '导入数据库失败');
      toast.success('数据库已导入，页面将刷新');
      setTimeout(() => window.location.reload(), 800);
    } catch (error) {
      toast.error(error.message || '导入数据库失败');
    } finally {
      setDatabaseBusy(false);
    }
  };

  const cleanupDeprecatedTables = async () => {
    const candidates = deprecatedTables?.tables || [];
    if (candidates.length === 0) {
      toast.success('没有可清理的废弃表');
      return;
    }
    const ok = await dialog.confirm({
      title: '清理废弃表',
      message: `将删除 ${candidates.length} 张废弃表、${deprecatedTables.totalRows || 0} 行数据。系统会先自动备份当前数据库。`,
      confirmText: '清理',
      cancelText: '取消',
      variant: 'destructive',
    });
    if (!ok) return;
    await postSettingsAction(
      '/api/settings/cleanup-deprecated-tables',
      '废弃表已清理',
      fetchDbState,
      { tables: candidates.map((item) => item.table) }
    );
  };

  const setAllModulesVisibility = (visible) => {
    const moduleVisibility = DEFAULT_MODULE_ORDER.reduce((acc, moduleId) => {
      acc[moduleId] = moduleId === 'dashboard' ? true : visible;
      return acc;
    }, {});
    patchSettings({ moduleVisibility });
  };

  const toggleModule = (moduleId, checked) => {
    patchSettings({
      moduleVisibility: {
        ...settings.moduleVisibility,
        [moduleId]: moduleId === 'dashboard' ? true : checked,
      },
    });
  };

  const reorderModule = (moduleId, direction) => {
    const index = settings.moduleOrder.indexOf(moduleId);
    const nextIndex = index + direction;
    if (index < 0 || nextIndex < 0 || nextIndex >= settings.moduleOrder.length) return;
    patchSettings({ moduleOrder: moveItem(settings.moduleOrder, index, nextIndex) });
  };

  const databaseStorage = dbStats?.storage || dbAnalysis?.storage || null;
  const databaseSizeBytes = dbStats?.totalSize ?? dbStats?.dbSize;
  const databaseSizeHint = databaseStorage
    ? `主库 ${formatFileSize(databaseStorage.mainSizeBytes)} · WAL ${formatFileSize(databaseStorage.walSizeBytes)} · 空闲 ${formatFileSize(databaseStorage.freePageBytes)}`
    : (dbStats?.dbPath || '等待统计');
  const deprecatedTableItems = deprecatedTables?.tables || [];

  return (
    <div className="flex w-full flex-col gap-6">
      <div className="flex flex-col gap-3 border-b border-kumo-line pb-3 lg:flex-row lg:items-center lg:justify-between">
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={SETTINGS_TABS}
        />

        <div className="flex flex-col gap-2 sm:flex-row sm:items-center lg:justify-end">
          <Button size="sm"
            onClick={() => refreshAll(true)}
            loading={settingsLoading}
            icon={<RefreshCw className="h-4 w-4" />}
          >
            刷新
          </Button>
          <Button size="sm"
            variant="primary"
            onClick={() => persistSettings()}
            loading={settingsSaving}
            icon={<Save className="h-4 w-4" />}
          >
            保存用户设置
          </Button>
        </div>
      </div>

      {activeTab === 'general' && (
        <div className="grid gap-4 lg:grid-cols-4">
          <StatCard label="运行状态" value="正常" hint={settingsLoading ? '同步中' : '已连接后端'} icon={Check} />
          <StatCard label="公网入口" value={settings.publicApiUrl || currentOrigin} hint="/api 自动拼接" icon={Globe} />
          <StatCard label="数据库大小" value={formatFileSize(databaseSizeBytes)} hint={databaseSizeHint} icon={Database} />
          <StatCard label="日志文件" value={logFileInfo?.sizeFormatted || `${logSettings.logFileSizeMB || 10} MB 上限`} hint="app.log" icon={FileText} />

          <LayerCard className="lg:col-span-2">
            <SectionHeader title="公网访问与 Agent" description="这些值会被后端用于生成 Agent 安装命令、下载地址和对外 API 连接配置。" />
            <FieldRow title="公网 API 地址" description="主控端可从公网访问时填写，留空则使用当前访问来源。">
              <Input size="sm"
                label="公网 API 地址"
                value={settings.publicApiUrl}
                onChange={(e) => patchSettings({ publicApiUrl: e.target.value })}
                placeholder="https://monitor.example.com"
              />
            </FieldRow>
            <FieldRow title="Agent 下载目录" description="留空使用主控端内置 /agent 目录；自定义时填写目录 URL，不填写文件名。">
              <Input size="sm"
                label="Agent 下载目录"
                value={settings.agentDownloadUrl}
                onChange={(e) => patchSettings({ agentDownloadUrl: e.target.value })}
                placeholder="https://cdn.example.com/agent"
              />
            </FieldRow>
          </LayerCard>

          <LayerCard className="lg:col-span-2">
            <SectionHeader title="运行偏好" description="这些设置会同步给对应业务页面和后端用户设置表。" />
            <FieldRow title="主机地址显示" description="控制主机实例页和安装命令中的地址脱敏策略。">
              <Select size="sm"
                label="主机地址显示"
                value={settings.serverIpDisplayMode}
                onValueChange={(value) => patchSettings({ serverIpDisplayMode: String(value) })}
                items={SERVER_IP_DISPLAY_OPTIONS}
              />
            </FieldRow>
            <FieldRow title="PaaS 自动刷新" description="Koyeb 和 Fly.io 状态拉取间隔，单位秒。">
              <div className="grid gap-3 sm:grid-cols-2">
                <Input size="sm"
                  label="Koyeb 秒数"
                  type="number"
                  min="5"
                  value={Math.round(settings.koyebRefreshInterval / 1000)}
                  onChange={(e) => patchSettings({ koyebRefreshInterval: Math.max(5, toInt(e.target.value, 30)) * 1000 })}
                />
                <Input size="sm"
                  label="Fly.io 秒数"
                  type="number"
                  min="5"
                  value={Math.round(settings.flyRefreshInterval / 1000)}
                  onChange={(e) => patchSettings({ flyRefreshInterval: Math.max(5, toInt(e.target.value, 30)) * 1000 })}
                />
              </div>
            </FieldRow>
          </LayerCard>
        </div>
      )}



      {activeTab === 'modules' && (
        <LayerCard className="overflow-x-auto p-0">
          <SectionHeader
            title="功能模块"
            description="模块顺序和显隐会立即影响左侧导航；系统设置入口固定显示。"
            actions={
              <>
                <Button size="sm" onClick={() => setAllModulesVisibility(true)} icon={<Eye className="h-4 w-4" />}>显示全部</Button>
                <Button size="sm" onClick={() => setAllModulesVisibility(false)} icon={<EyeOff className="h-4 w-4" />}>隐藏可选模块</Button>
              </>
            }
          />
          <Table layout="fixed">
            <colgroup>
              <col className="w-[82px]" />
              <col />
              <col className="w-[150px]" />
              <col className="w-[120px]" />
              <col className="w-[150px]" />
            </colgroup>
            <Table.Header>
              <Table.Row>
                <Table.Head>顺序</Table.Head>
                <Table.Head>模块</Table.Head>
                <Table.Head>分组</Table.Head>
                <Table.Head>可见</Table.Head>
                <Table.Head>排序</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {settings.moduleOrder.map((moduleId, index) => {
                const row = moduleRows.find((item) => item.id === moduleId);
                if (!row) return null;

                return (
                  <Table.Row key={moduleId}>
                    <Table.Cell className="font-mono text-xs text-kumo-subtle">{index + 1}</Table.Cell>
                    <Table.Cell>
                      <div className="flex min-w-0 items-center gap-3">
                        <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-kumo-line bg-kumo-recessed text-kumo-brand">
                          <i className={`fas ${row.config.icon || 'fa-cube'} text-xs`} />
                        </div>
                        <div className="min-w-0">
                          <div className="truncate text-sm font-semibold text-kumo-strong">{row.config.name}</div>
                          <div className="truncate text-xs text-kumo-subtle">{row.config.description}</div>
                        </div>
                      </div>
                    </Table.Cell>
                    <Table.Cell><Badge variant="outline">{row.groupName}</Badge></Table.Cell>
                    <Table.Cell>
                      <Switch
                        checked={settings.moduleVisibility[moduleId] !== false}
                        onCheckedChange={(checked) => toggleModule(moduleId, checked)}
                        disabled={moduleId === 'dashboard'}
                        aria-label={`切换 ${row.config.name}`}
                      />
                    </Table.Cell>
                    <Table.Cell>
                      <div className="flex gap-2">
                        <Button size="sm" onClick={() => reorderModule(moduleId, -1)} disabled={index === 0}>上移</Button>
                        <Button size="sm" onClick={() => reorderModule(moduleId, 1)} disabled={index === settings.moduleOrder.length - 1}>下移</Button>
                      </div>
                    </Table.Cell>
                  </Table.Row>
                );
              })}
            </Table.Body>
          </Table>
        </LayerCard>
      )}

      {activeTab === 'security' && (
        <div className="grid gap-4 xl:grid-cols-2">
          <LayerCard>
            <SectionHeader title="管理员密码" description="后端接口为 /api/auth/change-password，修改成功后会退出当前会话。" />
            <div className="grid max-w-xl gap-4 p-5">
              <Input size="sm"
                label="当前密码"
                type="password"
                value={passwordForm.oldPassword}
                onChange={(e) => setPasswordForm((prev) => ({ ...prev, oldPassword: e.target.value }))}
                disabled={isDemoMode}
                autoComplete="current-password"
              />
              <Input size="sm"
                label="新密码"
                type="password"
                value={passwordForm.newPassword}
                onChange={(e) => setPasswordForm((prev) => ({ ...prev, newPassword: e.target.value }))}
                disabled={isDemoMode}
                autoComplete="new-password"
              />
              <Input size="sm"
                label="确认新密码"
                type="password"
                value={passwordForm.confirmPassword}
                onChange={(e) => setPasswordForm((prev) => ({ ...prev, confirmPassword: e.target.value }))}
                disabled={isDemoMode}
                autoComplete="new-password"
              />
              <div>
                <Button size="sm" variant="primary" onClick={changePassword} loading={passwordSaving} disabled={isDemoMode}>
                  更新密码
                </Button>
              </div>
            </div>
          </LayerCard>

          <LayerCard className="p-5">
            <div className="flex items-start justify-between gap-3">
              <div>
                <h2 className="text-base font-bold text-kumo-strong">双因子认证</h2>
                <p className="mt-1 text-xs text-kumo-subtle">当前登录保护状态</p>
              </div>
              <Badge variant={twoFA.enabled ? 'success' : 'warning'}>
                {twoFA.enabled ? '已启用' : '未启用'}
              </Badge>
            </div>

            {twoFA.error && (
              <div className="mt-4 rounded-md border border-kumo-danger/20 bg-kumo-danger/10 px-3 py-2 text-xs text-kumo-danger">
                {twoFA.error}
              </div>
            )}

            {!twoFA.enabled && !twoFA.setupMode && (
              <Button size="sm" className="mt-5 w-full" variant="primary" onClick={start2FASetup} loading={twoFA.loading} disabled={isDemoMode}>
                启用 2FA
              </Button>
            )}

            {twoFA.setupMode && (
              <div className="mt-5 grid gap-4">
                {twoFA.qrCode && (
                  <div className="flex justify-center app-card p-4">
                    <img src={twoFA.qrCode} alt="2FA QR Code" className="h-44 w-44" />
                  </div>
                )}
                <Input size="sm" label="手动密钥" value={twoFA.secret} readOnly className="font-mono" />
                <Input size="sm"
                  label="6 位验证码"
                  value={twoFA.token}
                  onChange={(e) => setTwoFA((prev) => ({ ...prev, token: e.target.value.replace(/\D/g, '').slice(0, 6) }))}
                  placeholder="000000"
                  className="font-mono"
                />
                <div className="flex gap-2">
                  <Button size="sm" onClick={() => setTwoFA((prev) => ({ ...prev, setupMode: false, token: '', error: '' }))}>取消</Button>
                  <Button size="sm" variant="primary" onClick={confirm2FASetup} loading={twoFA.loading}>确认启用</Button>
                </div>
              </div>
            )}

            {twoFA.enabled && !twoFA.disableMode && (
              <Button size="sm" className="mt-5 w-full" variant="secondary-destructive" onClick={() => setTwoFA((prev) => ({ ...prev, disableMode: true, error: '' }))} disabled={isDemoMode}>
                禁用 2FA
              </Button>
            )}

            {twoFA.disableMode && (
              <div className="mt-5 grid gap-4">
                <Input size="sm"
                  label="当前密码"
                  type="password"
                  value={twoFA.disablePassword}
                  onChange={(e) => setTwoFA((prev) => ({ ...prev, disablePassword: e.target.value }))}
                  autoComplete="current-password"
                />
                <div className="flex gap-2">
                  <Button size="sm" onClick={() => setTwoFA((prev) => ({ ...prev, disableMode: false, disablePassword: '', error: '' }))}>取消</Button>
                  <Button size="sm" variant="destructive" onClick={disable2FA} loading={twoFA.loading}>确认禁用</Button>
                </div>
              </div>
            )}
          </LayerCard>
        </div>
      )}

      {activeTab === 'database' && (
        <div className="grid items-start gap-4 xl:grid-cols-2">
          <LayerCard className="overflow-x-auto p-0">
            <SectionHeader
              title="数据库统计"
              description={dbStats?.dbPath || 'SQLite 数据文件'}
              actions={
                <Button size="sm" onClick={fetchDbState} loading={databaseBusy} icon={<RefreshCw className="h-4 w-4" />}>刷新统计</Button>
              }
            />
            {databaseStorage && (
              <div className="grid gap-3 border-b border-kumo-line px-5 py-3 text-xs text-kumo-subtle md:grid-cols-4">
                <div>
                  <div className="font-semibold text-kumo-strong">{formatFileSize(databaseStorage.totalSizeBytes)}</div>
                  <div>总占用</div>
                </div>
                <div>
                  <div className="font-semibold text-kumo-strong">{formatFileSize(databaseStorage.mainSizeBytes)}</div>
                  <div>主库文件</div>
                </div>
                <div>
                  <div className="font-semibold text-kumo-strong">{formatFileSize((databaseStorage.walSizeBytes || 0) + (databaseStorage.shmSizeBytes || 0))}</div>
                  <div>WAL / SHM</div>
                </div>
                <div>
                  <div className="font-semibold text-kumo-strong">{formatFileSize(databaseStorage.freePageBytes)}</div>
                  <div>空闲页</div>
                </div>
              </div>
            )}
            <Table layout="fixed">
              <colgroup>
                <col />
                <col className="w-[120px]" />
                <col className="w-[140px]" />
                <col className="w-[120px]" />
                <col className="w-[140px]" />
              </colgroup>
              <Table.Header>
                <Table.Row>
                  <Table.Head>表名</Table.Head>
                  <Table.Head>记录数</Table.Head>
                  <Table.Head>实际占用</Table.Head>
                  <Table.Head>索引</Table.Head>
                  <Table.Head>平均行大小</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {tableRows.length === 0 ? (
                  <Table.Row>
                    <Table.Cell colSpan={5} className="p-8 text-center text-kumo-subtle">暂无统计数据</Table.Cell>
                  </Table.Row>
                ) : tableRows.map((row) => (
                  <Table.Row key={row.table}>
                    <Table.Cell className="font-mono text-xs text-kumo-strong">{row.table}</Table.Cell>
                    <Table.Cell className="font-mono text-xs">{row.rows ?? '-'}</Table.Cell>
                    <Table.Cell className="font-mono text-xs">{row.estimatedSizeBytes ? formatFileSize(row.estimatedSizeBytes) : '-'}</Table.Cell>
                    <Table.Cell className="font-mono text-xs">{row.indexSizeBytes ? formatFileSize(row.indexSizeBytes) : '-'}</Table.Cell>
                    <Table.Cell className="font-mono text-xs">{row.avgRowSizeBytes ? formatFileSize(row.avgRowSizeBytes) : '-'}</Table.Cell>
                  </Table.Row>
                ))}
              </Table.Body>
            </Table>
          </LayerCard>

          <div className="grid content-start gap-3">
            <LayerCard className="p-4">
              <h2 className="text-base font-bold text-kumo-strong">备份与恢复</h2>
              <Input
                ref={fileInputRef}
                type="file"
                accept=".db"
                aria-label="选择数据库文件"
                className="hidden"
                onChange={previewDatabaseImport}
              />
              <div className="mt-3 grid gap-2">
                <Button size="sm" className="justify-start" onClick={exportDatabase} icon={<Download className="h-4 w-4" />}>导出数据库</Button>
                <Button size="sm" className="justify-start" onClick={importDatabase} loading={databaseBusy} icon={<Upload className="h-4 w-4" />}>上传并预检数据库</Button>
              </div>
              {dbImportPreview && (
                <div className="mt-4 app-subcard p-3 text-xs">
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-semibold text-kumo-strong truncate">{dbImportPreview.originalName}</span>
                    <Badge variant={dbImportPreview.analysis?.integrity === 'ok' ? 'success' : 'warning'}>
                      {dbImportPreview.analysis?.integrity || 'unknown'}
                    </Badge>
                  </div>
                  <div className="mt-2 grid grid-cols-2 gap-2 font-mono text-[11px] text-kumo-subtle">
                    <span>大小 {formatFileSize(dbImportPreview.analysis?.sizeBytes)}</span>
                    <span>表 {dbImportPreview.analysis?.tableCount || 0} 个</span>
                  </div>
                  {dbImportPreview.warnings?.length > 0 && (
                    <div className="mt-2 space-y-1 rounded border border-kumo-warning/30 bg-kumo-warning/10 p-2 text-[11px] text-kumo-warning">
                      {dbImportPreview.warnings.map((warning) => (
                        <div key={warning}>{warning}</div>
                      ))}
                    </div>
                  )}
                  <div className="mt-3 max-h-44 overflow-y-auto rounded border border-kumo-line bg-kumo-base">
                    <Table layout="fixed">
                      <Table.Header variant="compact">
                        <Table.Row>
                          <Table.Head>表名</Table.Head>
                          <Table.Head className="w-20">记录数</Table.Head>
                        </Table.Row>
                      </Table.Header>
                      <Table.Body>
                        {(dbImportPreview.analysis?.tables || []).slice(0, 20).map((row) => (
                          <Table.Row key={row.name}>
                            <Table.Cell className="truncate font-mono text-[11px]">{row.name}</Table.Cell>
                            <Table.Cell className="font-mono text-[11px]">{row.rows}</Table.Cell>
                          </Table.Row>
                        ))}
                      </Table.Body>
                    </Table>
                  </div>
                  <div className="mt-3 flex gap-2">
                    <Button size="sm" variant="primary" onClick={commitDatabaseImport} loading={databaseBusy}>
                      确认导入
                    </Button>
                    <Button size="sm" variant="secondary" onClick={() => setDbImportPreview(null)}>
                      取消
                    </Button>
                  </div>
                </div>
              )}
              <div className="mt-4 border-t border-kumo-line pt-4">
                <BackupPanel embedded />
              </div>
            </LayerCard>

            <LayerCard className="p-4">
              <h2 className="text-base font-bold text-kumo-strong">维护操作</h2>
              <div className="mt-3 grid gap-2">
                <Button size="sm" className="justify-start" onClick={() => postSettingsAction('/api/settings/vacuum-database', '数据库已压缩', fetchDbState)} loading={databaseBusy}>
                  压缩数据库
                </Button>
                <Button size="sm"
                  className="justify-start"
                  variant="secondary-destructive"
                  onClick={() => postSettingsAction('/api/settings/clear-logs', '数据库日志已清理', fetchDbState)}
                  loading={databaseBusy}
                  icon={<Trash className="h-4 w-4" />}
                >
                  清理数据库日志
                </Button>
                <Button size="sm"
                  className="justify-start"
                  variant="secondary-destructive"
                  onClick={cleanupDeprecatedTables}
                  loading={databaseBusy}
                  disabled={deprecatedTableItems.length === 0}
                  icon={<Trash className="h-4 w-4" />}
                >
                  清理废弃表
                </Button>
              </div>
              <div className="mt-3 border-t border-kumo-line pt-3">
                <div className="flex items-center justify-between gap-2 text-xs">
                  <span className="font-semibold text-kumo-strong">废弃表候选</span>
                  <Badge variant={deprecatedTableItems.length > 0 ? 'warning' : 'secondary'}>
                    {deprecatedTableItems.length} 张
                  </Badge>
                </div>
                <div className="mt-2 grid grid-cols-2 gap-2 text-[11px] text-kumo-subtle">
                  <span>记录 {deprecatedTables?.totalRows || 0}</span>
                  <span>占用 {formatFileSize(deprecatedTables?.totalSize)}</span>
                </div>
                {deprecatedTableItems.length > 0 && (
                  <div className="mt-2 max-h-40 overflow-y-auto divide-y divide-kumo-line text-[11px]">
                    {deprecatedTableItems.slice(0, 8).map((item) => (
                      <div key={item.table} className="grid grid-cols-[minmax(0,1fr)_auto] gap-2 py-1.5">
                        <span className="truncate font-mono text-kumo-strong" title={item.reason}>{item.table}</span>
                        <span className="font-mono text-kumo-subtle">{formatFileSize(item.sizeBytes)}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </LayerCard>
          </div>
        </div>
      )}

      {activeTab === 'logs' && (
        <div className="grid gap-4">
          <LayerCard>
            <SectionHeader
              title="审计与保留"
              description="这里只管理数据库审计记录与日志保留策略；应用运行日志请到左侧「系统日志」查看。"
              actions={
                <>
                  <Button size="sm" onClick={saveLogSettings} loading={logsBusy} icon={<Save className="h-4 w-4" />}>保存保留策略</Button>
                  <Button size="sm" onClick={() => postSettingsAction('/api/settings/enforce-log-limits', '日志限制已执行', fetchLogState)} loading={logsBusy}>立即执行限制</Button>
                </>
              }
            />
            <div className="grid gap-4 p-5 md:grid-cols-4">
              <Input size="sm" label="保留天数" type="number" min="0" value={logSettings.days} onChange={(e) => setLogSettings((prev) => ({ ...prev, days: Math.max(0, toInt(e.target.value, 0)) }))} />
              <Input size="sm" label="单表最大条数" type="number" min="0" value={logSettings.count} onChange={(e) => setLogSettings((prev) => ({ ...prev, count: Math.max(0, toInt(e.target.value, 0)) }))} />
              <Input size="sm" label="数据库最大 MB" type="number" min="0" value={logSettings.dbSizeMB} onChange={(e) => setLogSettings((prev) => ({ ...prev, dbSizeMB: Math.max(0, toInt(e.target.value, 0)) }))} />
              <Input size="sm" label="app.log 最大 MB" type="number" min="1" value={logSettings.logFileSizeMB} onChange={(e) => setLogSettings((prev) => ({ ...prev, logFileSizeMB: Math.max(1, toInt(e.target.value, 10)) }))} />
            </div>
          </LayerCard>

          <div className="grid gap-4">
            <LayerCard className="overflow-x-auto p-0">
              <SectionHeader title="审计记录" description="最近 100 条数据库操作记录" />
              <Table layout="fixed">
                <colgroup>
                  <col className="w-[170px]" />
                  <col className="w-[150px]" />
                  <col className="w-[130px]" />
                  <col />
                </colgroup>
                <Table.Header>
                  <Table.Row>
                    <Table.Head>时间</Table.Head>
                    <Table.Head>操作</Table.Head>
                    <Table.Head>对象</Table.Head>
                    <Table.Head>Trace</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {operationLogs.slice(0, 100).map((log) => (
                    <Table.Row key={log.id}>
                      <Table.Cell className="font-mono text-xs">{log.created_at}</Table.Cell>
                      <Table.Cell><Badge variant="outline">{log.operation_type}</Badge></Table.Cell>
                      <Table.Cell className="font-mono text-xs">{log.table_name}</Table.Cell>
                      <Table.Cell className="truncate font-mono text-xs text-kumo-subtle">{log.trace_id || '-'}</Table.Cell>
                    </Table.Row>
                  ))}
                  {operationLogs.length === 0 && (
                    <Table.Row>
                      <Table.Cell colSpan={4} className="p-8 text-center text-kumo-subtle">暂无审计记录</Table.Cell>
                    </Table.Row>
                  )}
                </Table.Body>
              </Table>
            </LayerCard>
          </div>
        </div>
      )}

      {activeTab === 'appearance' && (
        <div className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_24rem]">
          <LayerCard>
            <SectionHeader title="界面外观" description={`当前生效主题: ${theme === 'dark' ? '深色' : '浅色'}`} />
            <FieldRow title="主题模式" description="云端偏好，切换后立即生效并自动同步。">
              <Select size="sm" label="主题模式" value={themeMode} onValueChange={handleThemeModeChange} items={THEME_OPTIONS} />
            </FieldRow>
            <FieldRow title="页面宽度" description="云端偏好，顶部宽度切换器也会同步。">
              <Select size="sm" label="页面宽度" value={pageWidthMode} onValueChange={handlePageWidthModeChange} items={PAGE_WIDTH_OPTIONS} />
            </FieldRow>
            <FieldRow title="触感反馈" description="移动端交互振动开关。">
              <Switch checked={settings.vibrationEnabled} onCheckedChange={(checked) => patchSettings({ vibrationEnabled: checked })} />
            </FieldRow>
          </LayerCard>

          <LayerCard className="p-5">
            <h2 className="text-base font-bold text-kumo-strong">TOTP 显示偏好</h2>
            <div className="mt-2 text-xs leading-relaxed text-kumo-subtle">这些选项会被 2FA 工具页读取。</div>
            <div className="mt-4">
              <ToggleLine title="账号名称打码" checked={!!settings.totpSettings.maskAccount} onCheckedChange={(checked) => updateTotpSetting('maskAccount', checked)} />
              <ToggleLine title="遮挡验证码" checked={!!settings.totpSettings.hideCode} onCheckedChange={(checked) => updateTotpSetting('hideCode', checked)} />
              <ToggleLine title="允许悬浮显示验证码" checked={!!settings.totpSettings.allowRevealCode} onCheckedChange={(checked) => updateTotpSetting('allowRevealCode', checked)} />
              <ToggleLine title="按站点分组" checked={!!settings.totpSettings.groupByPlatform} onCheckedChange={(checked) => updateTotpSetting('groupByPlatform', checked)} />
              <ToggleLine title="显示站点标题" checked={!!settings.totpSettings.showPlatformHeaders} onCheckedChange={(checked) => updateTotpSetting('showPlatformHeaders', checked)} />
              <ToggleLine title="隐藏站点文字" checked={!!settings.totpSettings.hidePlatformText} onCheckedChange={(checked) => updateTotpSetting('hidePlatformText', checked)} />
              <ToggleLine title="扫码后自动导入" checked={!!settings.totpSettings.autoSave} onCheckedChange={(checked) => updateTotpSetting('autoSave', checked)} />
              <ToggleLine title="锁定默认录入方式" checked={!!settings.totpSettings.lockInputMode} onCheckedChange={(checked) => updateTotpSetting('lockInputMode', checked)} />
            </div>
            <div className="mt-4">
              <Select size="sm"
                label="默认录入方式"
                value={settings.totpSettings.defaultInputMode}
                onValueChange={(value) => updateTotpSetting('defaultInputMode', String(value))}
                items={TOTP_INPUT_MODE_OPTIONS}
              />
            </div>
          </LayerCard>

          <LayerCard className="xl:col-span-2">
            <SectionHeader
              title="自定义 CSS"
              description="应用会立即注入当前页面，保存后写入后端用户设置。"
              actions={
                <>
                  <Button size="sm" onClick={() => applyCustomCss(settings.customCss)}>预览</Button>
                  <Button size="sm" variant="secondary-destructive" onClick={() => {
                    patchSettings({ customCss: '' });
                    applyCustomCss('');
                  }}>清空</Button>
                </>
              }
            />
            <div className="p-5">
              <Textarea
                label="CSS"
                value={settings.customCss}
                onChange={(e) => patchSettings({ customCss: e.target.value })}
                placeholder="/* 在此输入自定义 CSS */"
                className="min-h-64 font-mono text-sm"
              />
            </div>
          </LayerCard>
        </div>
      )}

      {activeTab === 'about' && (
        <div className="grid gap-4 lg:grid-cols-1">
          <LayerCard className="p-6 lg:col-span-2">
            <div className="flex items-center gap-4">
              <img src="/logo.svg" alt="" className="h-12 w-12 object-contain" />
              <div>
                <h2 className="text-xl font-bold text-kumo-strong">API Monitor</h2>
                <p className="mt-1 text-xs text-kumo-subtle">React 前端 + Go 后端</p>
              </div>
            </div>
            <div className="mt-6 grid gap-3 sm:grid-cols-3">
              <div className="rounded-lg border border-kumo-line bg-kumo-recessed p-4">
                <div className="text-xs text-kumo-subtle">当前源</div>
                <div className="mt-1 truncate font-mono text-sm text-kumo-strong">{currentOrigin}</div>
              </div>
              <div className="rounded-lg border border-kumo-line bg-kumo-recessed p-4">
                <div className="text-xs text-kumo-subtle">API 地址</div>
                <div className="mt-1 truncate font-mono text-sm text-kumo-strong">{`${currentOrigin}/api`}</div>
              </div>
              <div className="rounded-lg border border-kumo-line bg-kumo-recessed p-4">
                <div className="text-xs text-kumo-subtle">仓库地址</div>
                <div className="mt-1 truncate font-mono text-sm text-kumo-strong">
                  <a
                    href="https://github.com/iwvw/API-Monitor"
                    target="_blank"
                    rel="noopener noreferrer"
                    className="hover:underline text-kumo-strong"
                  >
                    https://github.com/iwvw/API-Monitor
                  </a>
                </div>
              </div>
            </div>
          </LayerCard>

          {/* <LayerCard className="p-6">
            <h2 className="text-base font-bold text-kumo-strong">已对接接口</h2>
            <div className="mt-4 grid gap-2 text-xs text-kumo-default">
              {[
                '/api/settings',
                '/api/settings/log-settings',
                '/api/settings/database-stats',
                '/api/settings/database-analysis',
                '/api/auth/change-password',
                '/api/auth/2fa/*',
              ].map((item) => (
                <div key={item} className="flex items-center gap-2">
                  <Check className="h-3.5 w-3.5 text-kumo-success" />
                  <span className="font-mono">{item}</span>
                </div>
              ))}
            </div>
          </LayerCard> */}
        </div>
      )}
    </div>
  );
}

export default SettingsPage;
