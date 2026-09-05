import React, { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react';
import { Badge } from '@cloudflare/kumo/components/badge';
import { Button } from '@cloudflare/kumo/components/button';
import { Input, Textarea } from '@cloudflare/kumo/components/input';
import { Select } from '@cloudflare/kumo/components/select';
import { Switch } from '@cloudflare/kumo/components/switch';
import { Table } from '@cloudflare/kumo/components/table';
import { Dialog } from '@cloudflare/kumo/components/dialog';
import { ClipboardText, LayerCard, Tabs } from '@cloudflare/kumo';
import { toast } from '../modules/toast.js';
import { dialog } from '../modules/dialog.js';
import { useConfirmPress } from '../hooks/useConfirmPress.js';
import useStore, {
  DEFAULT_MODULE_ORDER,
  FONT_OPTIONS,
  FONT_SIZE_OPTIONS,
  MODULE_CONFIG,
  MODULE_GROUPS,
  applyCustomCss,
  getGroupModuleIds,
  normalizeUserSettings,
} from '../store.js';
import { useShallow } from 'zustand/react/shallow';
import { MODULE_TABS_PROPS } from '../modules/kumoTabs.js';
import { APP_BUILD_TIME, APP_VERSION, FRAMEWORK_VERSIONS } from '../modules/appVersion.js';
import { applySiteBrandFaviconHref, getDefaultSiteBrandPreviewUrl } from '../modules/siteBrand.js';
import { AppCard, FieldRow, ResponsiveSearchInput, SectionCard, TabBarOverflowActions, cx, stickyTabsBaseClass } from '../components/ui/AppPrimitives.jsx';
import CodeEditor from '../components/ui/CodeEditor.jsx';
import { BackupPanel } from './BackupPage.jsx';
import { browserSupportsWebAuthn, createPasskeyCredential } from '../modules/webauthn.js';
import {
  Activity,
  Bell,
  Check,
  ChevronDown,
  ChevronUp,
  Clock,
  Columns,
  Database,
  Download,
  ExternalLink,
  FileText,
  Globe,
  GitHubBrand,
  HardDrive,
  LayoutDashboard,
  Lock,
  RefreshCw,
  Save,
  Search,
  Settings,
  Shield,
  Sun,
  Terminal,
  Trash,
  Upload,
  getModuleIconComponent,
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

const SECURITY_MASONRY_CARD_CLASS = '';

const THEME_OPTIONS = [
  { value: 'auto', label: '跟随系统' },
  { value: 'light', label: '浅色' },
  { value: 'dark', label: '深色' },
];

const GITHUB_NEW_OAUTH_APP_URL = 'https://github.com/settings/applications/new';

const TIMEZONE_OPTIONS = [
  { value: 'system', label: '跟随服务器' },
  { value: 'UTC', label: 'UTC' },
  { value: 'Asia/Shanghai', label: '中国标准时间 (Asia/Shanghai)' },
  { value: 'Asia/Tokyo', label: '日本时间 (Asia/Tokyo)' },
  { value: 'Asia/Singapore', label: '新加坡时间 (Asia/Singapore)' },
  { value: 'Europe/London', label: '伦敦时间 (Europe/London)' },
  { value: 'Europe/Berlin', label: '柏林时间 (Europe/Berlin)' },
  { value: 'America/New_York', label: '纽约时间 (America/New_York)' },
  { value: 'America/Los_Angeles', label: '洛杉矶时间 (America/Los_Angeles)' },
];



const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

const getUploadHeaders = () => ({
});

const formatFileSize = (bytes) => {
  const size = Number(bytes) || 0;
  if (size >= 1024 * 1024 * 1024) return `${(size / 1024 / 1024 / 1024).toFixed(2)} GB`;
  if (size >= 1024 * 1024) return `${(size / 1024 / 1024).toFixed(2)} MB`;
  if (size >= 1024) return `${(size / 1024).toFixed(2)} KB`;
  return `${size} B`;
};

const formatSessionTime = (value) => {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString('zh-CN', { hour12: false });
};

const describeUserAgent = (value) => {
  const ua = String(value || '');
  const browser = ua.includes('Edg/') ? 'Edge' : ua.includes('Chrome/') ? 'Chrome' : ua.includes('Firefox/') ? 'Firefox' : ua.includes('Safari/') ? 'Safari' : '浏览器';
  const platform = ua.includes('Windows') ? 'Windows' : ua.includes('Mac OS') ? 'macOS' : ua.includes('Android') ? 'Android' : ua.includes('iPhone') || ua.includes('iPad') ? 'iOS' : ua.includes('Linux') ? 'Linux' : '未知系统';
  return `${browser} · ${platform}`;
};

const toInt = (value, fallback = 0) => {
  const parsed = parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : fallback;
};

const moduleRows = DEFAULT_MODULE_ORDER.filter((moduleId) => moduleId !== 'prompts').map((moduleId) => {
  const group = MODULE_GROUPS.find((item) => getGroupModuleIds(item).includes(moduleId));
  return {
    id: moduleId,
    groupId: group?.id || 'other',
    groupName: group?.name || '其他模块',
    config: MODULE_CONFIG[moduleId] || { name: moduleId },
  };
});

function SettingsPage() {
  const { isArmed, confirmPress } = useConfirmPress();
  const {
    themeMode,
    theme,
    setThemeMode,
    setDashboardFooterVisible,
    setDashboardFooterRecordNumber,
    setVibrationEnabled,
    setUIFont,
    uiFontSize,
    setUIFontSize,
    applyUserSettings,
    loadUserSettings,
    logout,
    isDemoMode,
  } = useStore(
    useShallow(s => ({
      themeMode: s.themeMode,
      theme: s.theme,
      setThemeMode: s.setThemeMode,
      setDashboardFooterVisible: s.setDashboardFooterVisible,
      setDashboardFooterRecordNumber: s.setDashboardFooterRecordNumber,
      setVibrationEnabled: s.setVibrationEnabled,
      setUIFont: s.setUIFont,
      uiFontSize: s.uiFontSize,
      setUIFontSize: s.setUIFontSize,
      applyUserSettings: s.applyUserSettings,
      loadUserSettings: s.loadUserSettings,
      logout: s.logout,
      isDemoMode: s.isDemoMode,
    }))
  );

  const fileInputRef = useRef(null);
  const siteBrandInputRef = useRef(null);
  const [activeTab, setActiveTab] = useState('general');
  const [backendOnline, setBackendOnline] = useState(true);
  const [settings, setSettings] = useState(() => normalizeUserSettings());
  const [settingsPatch, setSettingsPatch] = useState({});
  const [settingsLoading, setSettingsLoading] = useState(true);
  const [settingsSaving, setSettingsSaving] = useState(false);
  const [moduleSearch, setModuleSearch] = useState('');
  const [siteBrandIcons, setSiteBrandIcons] = useState([]);
  const [siteBrandIconsLoading, setSiteBrandIconsLoading] = useState(false);
  const [siteBrandIconsLoaded, setSiteBrandIconsLoaded] = useState(false);
  const [siteBrandIconUploading, setSiteBrandIconUploading] = useState(false);



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
  const [databaseLoaded, setDatabaseLoaded] = useState(false);
  const [dbTablesExpanded, setDbTablesExpanded] = useState(false);
  const [dbImportPreview, setDbImportPreview] = useState(null);
  const [healthInfo, setHealthInfo] = useState(null);

  const [logSettings, setLogSettings] = useState({
    days: 0,
    count: 0,
    dbSizeMB: 0,
    logFileSizeMB: 10,
    autoCleanup: false,
    autoCleanupHours: 24,
  });
  const [logFileInfo, setLogFileInfo] = useState(null);
  const [operationLogs, setOperationLogs] = useState([]);
  const [logsBusy, setLogsBusy] = useState(false);
  const [logPreview, setLogPreview] = useState(null);
  const [logsLoaded, setLogsLoaded] = useState(false);
  const [twoFALoaded, setTwoFALoaded] = useState(false);
  const [loginSessions, setLoginSessions] = useState([]);
  const [sessionsLoading, setSessionsLoading] = useState(false);
  const [sessionsLoaded, setSessionsLoaded] = useState(false);
  const [githubAuth, setGitHubAuth] = useState({
    enabled: false,
    clientId: '',
    clientSecret: '',
    hasClientSecret: false,
    allowedLoginsText: '',
    allowedEmailsText: '',
  });
  const [githubAuthLoading, setGitHubAuthLoading] = useState(false);
  const [githubAuthSaving, setGitHubAuthSaving] = useState(false);
  const [githubAuthLoaded, setGitHubAuthLoaded] = useState(false);
  const [passkeys, setPasskeys] = useState([]);
  const [passkeysLoading, setPasskeysLoading] = useState(false);
  const [passkeysLoaded, setPasskeysLoaded] = useState(false);
  const [passkeyForm, setPasskeyForm] = useState({
    label: '',
  });
  const [passkeyBusy, setPasskeyBusy] = useState(false);

  const currentOrigin = useMemo(() => {
    if (typeof window === 'undefined') return 'http://localhost';
    return window.location.origin;
  }, []);
  const githubOAuthCallback = useMemo(() => `${settings.publicApiUrl || currentOrigin}/api/auth/github/callback`, [currentOrigin, settings.publicApiUrl]);

  const tableRows = useMemo(() => {
    if (dbAnalysis?.tables?.length) return dbAnalysis.tables;
    return Object.entries(dbStats?.tables || {})
      .map(([table, rows]) => ({ table, rows }))
      .sort((a, b) => Number(b.rows) - Number(a.rows));
  }, [dbAnalysis, dbStats]);
  const dbTableDisplayRows = useMemo(
    () => (dbTablesExpanded ? tableRows : []),
    [tableRows, dbTablesExpanded],
  );
  const formatTableRows = useCallback((rows) => {
    const value = Number(rows);
    return Number.isFinite(value) && value >= 0 ? value : '-';
  }, []);
  const formatTableMetricSize = useCallback((value) => {
    const parsed = Number(value);
    return Number.isFinite(parsed) && parsed >= 0 ? formatFileSize(parsed) : '-';
  }, []);

  const patchSettings = useCallback((patch) => {
    setSettings((prev) => normalizeUserSettings({ ...prev, ...patch }));
    setSettingsPatch((prev) => ({ ...prev, ...patch }));
  }, []);

  const handleThemeModeChange = useCallback((value) => {
    const nextMode = String(value);
    setThemeMode(nextMode);
    patchSettings({ themeMode: nextMode });
  }, [patchSettings, setThemeMode]);

  const handleVibrationEnabledChange = useCallback((checked) => {
    setVibrationEnabled(checked);
    patchSettings({ vibrationEnabled: Boolean(checked) });
  }, [patchSettings, setVibrationEnabled]);

  const handleUIFontChange = useCallback((value) => {
    const nextFont = String(value);
    setUIFont(nextFont);
    patchSettings({ uiFont: nextFont });
  }, [patchSettings, setUIFont]);

  const handleUIFontSizeChange = useCallback((value) => {
    setUIFontSize(String(value));
  }, [setUIFontSize]);

  const handleDashboardFooterVisibleChange = useCallback((checked) => {
    setDashboardFooterVisible(checked);
    patchSettings({ dashboardFooterVisible: Boolean(checked) });
  }, [patchSettings, setDashboardFooterVisible]);

  const handleDashboardFooterRecordNumberChange = useCallback((event) => {
    const recordNumber = event.target.value;
    setDashboardFooterRecordNumber(recordNumber);
    patchSettings({ dashboardFooterRecordNumber: recordNumber });
  }, [patchSettings, setDashboardFooterRecordNumber]);

  const fetchSettings = useCallback(async () => {
    const response = await fetch('/api/settings', { headers: getAuthHeaders() });
    const result = await response.json();
    if (!response.ok || !result.success) throw new Error(result.error || '加载用户设置失败');
    const normalized = normalizeUserSettings(result.data || {});
    setSettings(normalized);
    setSettingsPatch({});
    applyUserSettings(normalized);
    return normalized;
  }, [applyUserSettings]);

  const fetchDbState = useCallback(async () => {
    setDatabaseBusy(true);
    try {
      const [statsResponse, analysisResponse, deprecatedResponse] = await Promise.all([
        fetch('/api/settings/database-stats', { headers: getAuthHeaders() }),
        fetch('/api/settings/database-analysis?deep=1', { headers: getAuthHeaders() }),
        fetch('/api/settings/deprecated-tables', { headers: getAuthHeaders() }),
      ]);

      const statsResult = await statsResponse.json();
      if (statsResult.success) setDbStats(statsResult.data);

      const analysisResult = await analysisResponse.json();
      if (analysisResult.success) setDbAnalysis(analysisResult.data);

      const deprecatedResult = await deprecatedResponse.json();
      if (deprecatedResult.success) setDeprecatedTables(deprecatedResult.data);
      setDatabaseLoaded(true);
    } finally {
      setDatabaseBusy(false);
    }
  }, []);

  const fetchLogState = useCallback(async () => {
    setLogsBusy(true);
    try {
      const [logSettingsResponse, operationLogsResponse] = await Promise.all([
        fetch('/api/settings/log-settings', { headers: getAuthHeaders() }),
        fetch('/api/settings/operation-logs', { headers: getAuthHeaders() }),
      ]);

      const logSettingsResult = await logSettingsResponse.json();
      if (logSettingsResult.success) {
        setLogSettings({ ...logSettingsResult.data, autoCleanup: !!logSettingsResult.data?.autoCleanup });
        setLogFileInfo(logSettingsResult.fileInfo || null);
      }

      const operationLogsResult = await operationLogsResponse.json();
      if (operationLogsResult.success) {
        setOperationLogs(operationLogsResult.data || []);
      }
      setLogsLoaded(true);
    } finally {
      setLogsBusy(false);
    }
  }, []);

  const fetchRuntimeState = useCallback(async () => {
    try {
      const [statsResponse, logSettingsResponse] = await Promise.all([
        fetch('/api/settings/database-stats', { headers: getAuthHeaders() }),
        fetch('/api/settings/log-settings', { headers: getAuthHeaders() }),
      ]);

      const statsResult = await statsResponse.json();
      if (statsResult.success) setDbStats(statsResult.data);

      const logSettingsResult = await logSettingsResponse.json();
      if (logSettingsResult.success) {
        setLogSettings((prev) => ({
          ...prev,
          ...logSettingsResult.data,
          autoCleanup: !!logSettingsResult.data?.autoCleanup,
        }));
        setLogFileInfo(logSettingsResult.fileInfo || null);
      }
    } catch (error) {
      console.error('获取运行状态失败', error);
    }
  }, []);

  const fetchTwoFAStatus = useCallback(async () => {
    const response = await fetch('/api/auth/2fa/status', { headers: getAuthHeaders() });
    const result = await response.json();
    if (result.success) {
      setTwoFA((prev) => ({ ...prev, enabled: !!result.enabled }));
      setTwoFALoaded(true);
    }
  }, []);

  const fetchLoginSessions = useCallback(async () => {
    setSessionsLoading(true);
    try {
      const response = await fetch('/api/auth/sessions', { headers: getAuthHeaders() });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '加载登录设备失败');
      const payload = result.data || result;
      setLoginSessions(payload.sessions || []);
      setSessionsLoaded(true);
    } finally {
      setSessionsLoading(false);
    }
  }, []);

  const fetchGitHubAuthConfig = useCallback(async () => {
    setGitHubAuthLoading(true);
    try {
      const response = await fetch('/api/auth/github/config', { headers: getAuthHeaders() });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '加载 GitHub 登录配置失败');
      const payload = result.data || result;
      setGitHubAuth({
        enabled: !!payload.enabled,
        clientId: payload.clientId || '',
        clientSecret: '',
        hasClientSecret: !!payload.hasClientSecret,
        allowedLoginsText: payload.allowedLoginsText || '',
        allowedEmailsText: payload.allowedEmailsText || '',
      });
      setGitHubAuthLoaded(true);
    } finally {
      setGitHubAuthLoading(false);
    }
  }, []);

  const fetchPasskeys = useCallback(async () => {
    setPasskeysLoading(true);
    try {
      const response = await fetch('/api/auth/webauthn/credentials', { headers: getAuthHeaders() });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '加载通行密钥失败');
      const payload = result.data || result;
      setPasskeys(payload.credentials || []);
      setPasskeysLoaded(true);
    } finally {
      setPasskeysLoading(false);
    }
  }, []);

  const loadSiteBrandIcons = useCallback(async () => {
    setSiteBrandIconsLoading(true);
    try {
      const response = await fetch('/api/settings/site-brand/icons', { headers: getAuthHeaders() });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '加载站点图标失败');
      const items = Array.isArray(result.data) ? result.data : [];
      setSiteBrandIcons(items);
      setSiteBrandIconsLoaded(true);
      return items;
    } finally {
      setSiteBrandIconsLoading(false);
    }
  }, []);



  const fetchHealth = useCallback(async () => {
    try {
      const response = await fetch('/health');
      const result = await response.json();
      if (response.ok) setHealthInfo(result);
    } catch (error) {
      console.error('获取后端健康信息失败', error);
    }
  }, []);

  useEffect(() => {
    if (activeTab === 'about' && !healthInfo) {
      fetchHealth();
    }
  }, [activeTab, healthInfo, fetchHealth]);

  const refreshCurrent = useCallback(async (showFeedback = false) => {
    setSettingsLoading(true);
    try {
      await fetchSettings();
      if (activeTab === 'general') await fetchRuntimeState();
      if (activeTab === 'appearance') await loadSiteBrandIcons();
      if (activeTab === 'database') await fetchDbState();
      if (activeTab === 'logs') await fetchLogState();
      if (activeTab === 'security') await Promise.all([fetchTwoFAStatus(), fetchLoginSessions(), fetchGitHubAuthConfig(), fetchPasskeys()]);
      if (activeTab === 'about') await fetchHealth();
      if (showFeedback) toast.success('设置已刷新');
    } catch (error) {
      toast.error(error.message || '加载设置失败');
    } finally {
      setSettingsLoading(false);
    }
  }, [activeTab, fetchDbState, fetchGitHubAuthConfig, fetchHealth, fetchLogState, fetchLoginSessions, fetchPasskeys, fetchRuntimeState, fetchSettings, fetchTwoFAStatus, loadSiteBrandIcons]);

  useEffect(() => {
    if (activeTab === 'general') {
      fetchRuntimeState();
    }
  }, [activeTab, fetchRuntimeState]);

  useEffect(() => {
    if (activeTab !== 'general' || typeof window.EventSource !== 'function') {
      setBackendOnline(true);
      return undefined;
    }
    const source = new EventSource('/api/system/status/stream');
    source.onopen = () => setBackendOnline(true);
    source.onmessage = () => setBackendOnline(true);
    source.onerror = () => setBackendOnline(false);
    return () => source.close();
  }, [activeTab]);

  useEffect(() => {
    let cancelled = false;
    setSettingsLoading(true);
    fetchSettings()
      .catch((error) => {
        if (!cancelled) toast.error(error.message || '加载设置失败');
      })
      .finally(() => {
        if (!cancelled) setSettingsLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [fetchSettings]);

  useEffect(() => {
    if (activeTab === 'database' && !databaseLoaded && !databaseBusy) {
      fetchDbState().catch((error) => toast.error(error.message || '加载数据库统计失败'));
    }
  }, [activeTab, databaseBusy, databaseLoaded, fetchDbState]);

  useEffect(() => {
    if (activeTab === 'logs' && !logsLoaded && !logsBusy) {
      fetchLogState().catch((error) => toast.error(error.message || '加载审计日志失败'));
    }
  }, [activeTab, fetchLogState, logsBusy, logsLoaded]);

  useEffect(() => {
    if (activeTab === 'security' && !twoFALoaded) {
      fetchTwoFAStatus().catch((error) => toast.error(error.message || '加载 2FA 状态失败'));
    }
  }, [activeTab, fetchTwoFAStatus, twoFALoaded]);

  useEffect(() => {
    if (activeTab === 'security' && !sessionsLoaded && !sessionsLoading) {
      fetchLoginSessions().catch((error) => toast.error(error.message || '加载登录设备失败'));
    }
  }, [activeTab, fetchLoginSessions, sessionsLoaded, sessionsLoading]);

  useEffect(() => {
    if (activeTab === 'security' && !githubAuthLoaded && !githubAuthLoading) {
      fetchGitHubAuthConfig().catch((error) => toast.error(error.message || '加载 GitHub 登录配置失败'));
    }
  }, [activeTab, fetchGitHubAuthConfig, githubAuthLoaded, githubAuthLoading]);

  useEffect(() => {
    if (activeTab === 'security' && !passkeysLoaded && !passkeysLoading) {
      fetchPasskeys().catch((error) => toast.error(error.message || '加载通行密钥失败'));
    }
  }, [activeTab, fetchPasskeys, passkeysLoaded, passkeysLoading]);

  useEffect(() => {
    if (activeTab === 'appearance' && !siteBrandIconsLoaded && !siteBrandIconsLoading) {
      loadSiteBrandIcons().catch((error) => toast.error(error.message || '加载站点图标失败'));
    }
  }, [activeTab, loadSiteBrandIcons, siteBrandIconsLoaded, siteBrandIconsLoading]);

  const forceSessionOffline = async (session) => {
    if (!confirmPress(`session-offline:${session.id}`, session.current ? '下线当前设备' : '强制下线该设备')) return;
    try {
      const response = await fetch(`/api/auth/sessions/${encodeURIComponent(session.id)}`, { method: 'DELETE', headers: getAuthHeaders() });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '强制下线失败');
      toast.success(session.current ? '当前设备已下线' : '设备已强制下线');
      if (session.current) {
        await logout();
        return;
      }
      await fetchLoginSessions();
    } catch (error) {
      toast.error(error.message || '强制下线失败');
    }
  };

  const forceAllSessionsOffline = async () => {
    const confirmed = await dialog.confirm({
      title: '确认强制全部设备下线',
      message: '这会立即终止全部主程序会话，并使浏览器插件停止取码。确定要继续吗？',
      confirmText: '确认全部下线',
      confirmClass: '!bg-kumo-danger !text-white',
    });
    if (!confirmed) return;
    try {
      const response = await fetch('/api/auth/sessions/revoke-all', { method: 'POST', headers: getAuthHeaders() });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '全部下线失败');
      toast.success('全部设备已下线');
      await logout();
    } catch (error) {
      toast.error(error.message || '全部下线失败');
    }
  };

  const persistSettings = async (successMessage = '设置已保存') => {
    const patch = settingsPatch;
    if (Object.keys(patch).length === 0) {
      toast.info('没有需要保存的设置');
      return true;
    }
    setSettingsSaving(true);
    try {
      const response = await fetch('/api/settings', {
        method: 'PATCH',
        headers: getAuthHeaders(),
        body: JSON.stringify(patch),
      });
      const result = await response.json();
      if (!response.ok || !result.success) throw new Error(result.error || '保存设置失败');

      const normalized = normalizeUserSettings(result.data || { ...settings, ...patch });
      setSettingsPatch({});
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

  const selectedSiteBrandIcon = useMemo(
    () => siteBrandIcons.find((item) => item.id === settings.siteBrandIconId) || null,
    [settings.siteBrandIconId, siteBrandIcons],
  );

  const siteBrandPreviewUrl = selectedSiteBrandIcon?.url || getDefaultSiteBrandPreviewUrl();

  const previewSiteBrandIcon = useCallback((iconId) => {
    const nextSelected = siteBrandIcons.find((item) => item.id === iconId) || null;
    applySiteBrandFaviconHref(nextSelected?.url || getDefaultSiteBrandPreviewUrl());
  }, [siteBrandIcons]);

  const chooseSiteBrandIcon = useCallback((iconId) => {
    patchSettings({ siteBrandIconId: iconId });
    previewSiteBrandIcon(iconId);
  }, [patchSettings, previewSiteBrandIcon]);

  const triggerSiteBrandUpload = useCallback(() => {
    siteBrandInputRef.current?.click();
  }, []);

  const uploadSiteBrandIcon = useCallback(async (event) => {
    const file = event.target.files?.[0];
    if (!file) return;

    const formData = new FormData();
    formData.append('file', file);
    formData.append('name', file.name.replace(/\.[^.]+$/, ''));
    setSiteBrandIconUploading(true);
    try {
      const response = await fetch('/api/settings/site-brand/icons', {
        method: 'POST',
        headers: getUploadHeaders(),
        body: formData,
      });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '上传站点图标失败');
      const item = result.data || result;
      setSiteBrandIcons((prev) => [item, ...prev.filter((entry) => entry.id !== item.id)]);
      setSiteBrandIconsLoaded(true);
      chooseSiteBrandIcon(item.id || '');
      toast.success('图标已上传，保存当前页设置后生效');
    } catch (error) {
      toast.error(error.message || '上传站点图标失败');
    } finally {
      setSiteBrandIconUploading(false);
      if (event.target) event.target.value = '';
    }
  }, [chooseSiteBrandIcon]);



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

  const saveGitHubLoginConfig = async () => {
    setGitHubAuthSaving(true);
    try {
      const response = await fetch('/api/auth/github/config', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(githubAuth),
      });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '保存 GitHub 登录配置失败');
      const payload = result.data || result;
      setGitHubAuth({
        enabled: !!payload.enabled,
        clientId: payload.clientId || '',
        clientSecret: '',
        hasClientSecret: !!payload.hasClientSecret,
        allowedLoginsText: payload.allowedLoginsText || '',
        allowedEmailsText: payload.allowedEmailsText || '',
      });
      toast.success('GitHub 登录配置已保存');
    } catch (error) {
      toast.error(error.message || '保存 GitHub 登录配置失败');
    } finally {
      setGitHubAuthSaving(false);
    }
  };

  const registerPasskey = async () => {
    if (!browserSupportsWebAuthn()) {
      toast.error('当前浏览器不支持通行密钥');
      return;
    }

    setPasskeyBusy(true);
    try {
      const beginResponse = await fetch('/api/auth/webauthn/register/begin', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify(passkeyForm),
      });
      const beginResult = await beginResponse.json();
      if (!beginResponse.ok || beginResult.success === false) throw new Error(beginResult.error || '创建通行密钥挑战失败');

      const credential = await createPasskeyCredential(beginResult.options);
      const finishResponse = await fetch('/api/auth/webauthn/register/finish', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({
          flowId: beginResult.flowId,
          credential,
        }),
      });
      const finishResult = await finishResponse.json();
      if (!finishResponse.ok || finishResult.success === false) throw new Error(finishResult.error || '保存通行密钥失败');

      toast.success('通行密钥已添加');
      setPasskeyForm({ label: '' });
      await fetchPasskeys();
    } catch (error) {
      const message = error?.name === 'NotAllowedError'
        ? '通行密钥操作已取消或被浏览器拦截'
        : (error.message || '保存通行密钥失败');
      toast.error(message);
    } finally {
      setPasskeyBusy(false);
    }
  };

  const removePasskey = async (passkey) => {
    if (!confirmPress(`passkey:${passkey.id}`, `删除通行密钥「${passkey.label || '通行密钥'}」`)) return;

    setPasskeyBusy(true);
    try {
      const response = await fetch(`/api/auth/webauthn/credentials/${encodeURIComponent(passkey.id)}/delete`, {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (!response.ok || result.success === false) throw new Error(result.error || '删除通行密钥失败');
      toast.success('通行密钥已删除');
      await fetchPasskeys();
    } catch (error) {
      toast.error(error.message || '删除通行密钥失败');
    } finally {
      setPasskeyBusy(false);
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

  // 数据库压缩在后台执行（VACUUM 可能耗时数分钟），提交后轮询任务状态，
  // 避免压缩期间面板无响应。
  const runDatabaseVacuum = async () => {
    setDatabaseBusy(true);
    try {
      const response = await fetch('/api/settings/vacuum-database', {
        method: 'POST',
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (!response.ok) throw new Error(result.error || '启动数据库压缩失败');
      if (result.data?.running) {
        if (result.data?.mode === 'migrate') {
          toast.info('首次压缩需几分钟（数据库迁移到增量回收模式），期间部分请求可能短暂报错，请勿刷新或重启');
        } else {
          toast.info('数据库压缩已开始，将在后台执行…');
        }
      } else {
        toast.success(result.message || '数据库已压缩');
      }
      // 轮询直到压缩完成（最多 10 分钟）
      const deadline = Date.now() + 10 * 60 * 1000;
      while (Date.now() < deadline) {
        await new Promise((resolve) => setTimeout(resolve, 2000));
        const statusResponse = await fetch('/api/settings/vacuum-database', {
          headers: getAuthHeaders(),
        });
        const status = await statusResponse.json();
        if (!statusResponse.ok) break;
        const snapshot = status.data || {};
        if (!snapshot.running) {
          if (snapshot.error) {
            toast.error(`数据库压缩失败: ${snapshot.error}`);
          } else if (snapshot.savedMB && snapshot.savedMB !== '0 B') {
            toast.success(`数据库已压缩（节省 ${snapshot.savedMB}）`);
          } else {
            toast.success('数据库已压缩');
          }
          break;
        }
      }
      await fetchDbState();
    } catch (error) {
      toast.error(error.message || '数据库压缩失败');
    } finally {
      setDatabaseBusy(false);
    }
  };

  const runEnforceLogLimits = async () => {
    setLogsBusy(true);
    try {
      const previewResponse = await fetch('/api/settings/enforce-log-limits', {
        method: 'POST',
        headers: getAuthHeaders(),
        body: JSON.stringify({ preview: true }),
      });
      const previewResult = await previewResponse.json();
      if (!previewResponse.ok || previewResult.success === false) {
        throw new Error(previewResult.error || '获取清理预览失败');
      }
      if (previewResult.totalDeleted === 0 && !previewResult.sizeOverLimit) {
        toast.info('当前无需清理');
        return;
      }
      setLogPreview(previewResult);
    } catch (error) {
      toast.error(error.message || '获取清理预览失败');
    } finally {
      setLogsBusy(false);
    }
  };

  const confirmEnforceLogLimits = async () => {
    setLogPreview(null);
    await postSettingsAction('/api/settings/enforce-log-limits', '日志限制已执行', fetchLogState);
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

  const toggleModule = (moduleId, checked) => {
    patchSettings({
      moduleVisibility: {
        ...settings.moduleVisibility,
        [moduleId]: moduleId === 'dashboard' ? true : checked,
      },
    });
  };

  const orderedModuleRows = useMemo(() => {
    const rowById = new Map(moduleRows.map((row) => [row.id, row]));
    const orderedIds = MODULE_GROUPS.flatMap((group) => [
      ...settings.moduleOrder.filter((moduleId) => (group.modules || []).includes(moduleId)),
      ...(group.subgroups || []).flatMap((subgroup) => (
        settings.moduleOrder.filter((moduleId) => (subgroup.modules || []).includes(moduleId))
      )),
      ...settings.moduleOrder.filter((moduleId) => (group.trailingModules || []).includes(moduleId)),
    ]);

    return orderedIds.map((moduleId) => rowById.get(moduleId)).filter(Boolean);
  }, [settings.moduleOrder]);
  const moduleGroups = useMemo(() => (
    MODULE_GROUPS
      .map((group) => ({
        id: group.id,
        name: group.name,
        count: orderedModuleRows.filter((item) => item.groupId === group.id).length,
      }))
      .filter((group) => group.count > 0)
  ), [orderedModuleRows]);
  const normalizedModuleSearch = moduleSearch.trim().toLocaleLowerCase();
  const filteredModuleRows = orderedModuleRows.filter((item) => {
    const matchesSearch = !normalizedModuleSearch || [item.config.name, item.config.description, item.groupName]
      .filter(Boolean)
      .some((value) => value.toLocaleLowerCase().includes(normalizedModuleSearch));
    return matchesSearch;
  });

  const databaseStorage = dbStats?.storage || dbAnalysis?.storage || null;
  const databaseSegments = useMemo(() => {
    if (!databaseStorage) return [];
    const walShm = (databaseStorage.walSizeBytes || 0) + (databaseStorage.shmSizeBytes || 0);
    const freePage = databaseStorage.freePageBytes || 0;
    const used = Math.max((databaseStorage.mainSizeBytes || 0) - freePage, 0);
    const total = Math.max(used + freePage + walShm, 1);
    return [
      { label: '有效数据', value: used, barClass: 'bg-brand' },
      { label: '空闲页', value: freePage, barClass: 'bg-kumo-info' },
      { label: 'WAL / SHM', value: walShm, barClass: 'bg-kumo-warning' },
    ].map((s) => ({ ...s, percent: (s.value / total) * 100 }));
  }, [databaseStorage]);
  const databaseSizeBytes = dbStats?.totalSize ?? dbStats?.dbSize;
  const databaseSizeHint = databaseStorage
    ? `主库 ${formatFileSize(databaseStorage.mainSizeBytes)} · WAL ${formatFileSize(databaseStorage.walSizeBytes)} · 空闲 ${formatFileSize(databaseStorage.freePageBytes)}`
    : (dbStats?.dbPath || '等待统计');
  const deprecatedTableItems = deprecatedTables?.tables || [];
  const contentViewportClassName = 'min-w-0';

  return (
    <div className="flex min-h-full w-full min-w-0 flex-col gap-3 cq-sm:gap-4">
      <div className={`${stickyTabsBaseClass} justify-between gap-2 border-b border-kumo-line [&>*]:min-w-0`}>
        <Tabs
          {...MODULE_TABS_PROPS}
          value={activeTab}
          onValueChange={setActiveTab}
          tabs={SETTINGS_TABS}
        />

        <TabBarOverflowActions
          items={[
            {
              key: 'refresh',
              label: '刷新',
              icon: <RefreshCw className="h-4 w-4" />,
              onClick: () => refreshCurrent(true),
              loading: settingsLoading || (activeTab === 'database' && databaseBusy) || (activeTab === 'logs' && logsBusy),
            },
            ...(['general', 'modules', 'appearance'].includes(activeTab)
              ? [
                  {
                    key: 'save',
                    label: '保存当前页设置',
                    icon: <Save className="h-4 w-4" />,
                    onClick: () => persistSettings(),
                    loading: settingsSaving,
                    variant: 'primary',
                  },
                ]
              : []),
          ]}
        />
      </div>

      <div className={contentViewportClassName}>
      {activeTab === 'general' && (
        <div className="grid min-h-0 items-start gap-4 cq-md:h-full cq-md:overflow-auto cq-xl:grid-cols-[minmax(16rem,1fr)_minmax(0,3fr)]">
          <SectionCard
            title="运行状态"
            icon={<Check className="h-4 w-4 text-brand" />}
            className="min-h-0 self-start"
            bodyPadding="none"
          >
            <FieldRow title="运行状态">
              <span className={`font-mono text-sm font-semibold ${backendOnline ? 'text-kumo-success' : 'text-kumo-danger'}`}>{backendOnline ? '正常' : '离线'}</span>
            </FieldRow>
            <FieldRow title="公网入口" >
              <span className="truncate font-mono text-sm font-medium text-kumo-strong">{settings.publicApiUrl || currentOrigin}</span>
            </FieldRow>
            <FieldRow title="数据库大小">
              <span className="font-mono text-sm font-medium text-kumo-strong">{formatFileSize(databaseSizeBytes)}</span>
            </FieldRow>
            <FieldRow title="日志文件">
              <span className="font-mono text-sm font-medium text-kumo-strong">上限 {logFileInfo?.sizeFormatted || `${logSettings.logFileSizeMB || 10} MB`}</span>
            </FieldRow>
          </SectionCard>

          <SectionCard
            title="部署访问地址"
            icon={<Globe className="h-4 w-4 text-brand" />}
            className="min-h-0 self-start"
            bodyPadding="none"
          >
            <FieldRow title="公网 API 地址" description="公网可访问时填写，留空用当前来源。">
              <Input size="sm"
                value={settings.publicApiUrl}
                onChange={(e) => patchSettings({ publicApiUrl: e.target.value })}
                placeholder="https://monitor.example.com"
                aria-label="公网 API 地址"
              />
            </FieldRow>
            <FieldRow title="系统时区" description="本地化时间；跟随服务器用默认时区。">
              <Select alignItemWithTrigger
                size="sm"
                value={settings.timezone}
                onValueChange={(value) => patchSettings({ timezone: value })}
                items={TIMEZONE_OPTIONS}
              />
            </FieldRow>
          </SectionCard>
        </div>
      )}



      {activeTab === 'modules' && (
        <div className="min-h-0 overflow-auto cq-md:h-full">
        <SectionCard
          className="flex min-h-0 cq-md:h-full"
          headerClassName="max-sm:min-h-12 max-sm:flex-row max-sm:items-center max-sm:px-3 max-sm:py-2"
          title="功能模块"
          icon={<Activity className="h-4 w-4 text-brand" />}
          actionsClassName="max-sm:ml-auto max-sm:w-auto max-sm:gap-1.5"
          actions={
              <>
                <ResponsiveSearchInput
                  value={moduleSearch}
                  onChange={(event) => setModuleSearch(event.target.value)}
                  placeholder="搜索模块"
                  ariaLabel="搜索模块"
                  className="cq-sm:w-52"
                />
              </>
          }
          bodyClassName="flex min-h-0 flex-1 flex-col gap-3 overflow-auto"
        >
          <div className="flex flex-col gap-3 cq-sm:gap-4">
            {moduleGroups.map((group) => {
              const groupRows = filteredModuleRows.filter((row) => row.groupId === group.id);
              if (groupRows.length === 0) return null;

              return (
                <section key={group.id} className="min-w-0">
                  <div className="mb-2 flex items-center gap-2 text-xs font-semibold text-kumo-subtle">
                    <span>{group.name}</span>
                    <span className="h-px min-w-4 flex-1 bg-kumo-line" />
                    <span className="font-normal">{groupRows.length} 项</span>
                  </div>
                  <div className="grid gap-1.5 cq-lg:grid-cols-2 cq-xl:grid-cols-3">
                    {groupRows.map((row) => {
                      const ModuleIcon = getModuleIconComponent(row.id);
                      const isVisible = settings.moduleVisibility[row.id] !== false;

                      return (
                        <div key={row.id} className={cx('flex min-h-15 items-center gap-2.5 rounded-md border px-2.5 py-2 cq-sm:min-h-16 cq-sm:gap-3 cq-sm:px-3', isVisible ? 'border-kumo-line bg-kumo-base' : 'border-kumo-line/70 bg-kumo-recessed/35 opacity-75')}>
                          <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border border-kumo-line bg-kumo-recessed text-brand">
                            <ModuleIcon className="h-4 w-4" />
                          </div>
                          <div className="min-w-0 flex-1">
                            <div className="truncate text-sm font-semibold text-kumo-strong">{row.config.name}</div>
                            <div className="hidden truncate text-xs text-kumo-subtle cq-sm:block">{row.config.description}</div>
                          </div>
                          <Switch
                            checked={isVisible}
                            onCheckedChange={(checked) => toggleModule(row.id, checked)}
                            disabled={row.id === 'dashboard'}
                            aria-label={`切换 ${row.config.name}`}
                          />
                        </div>
                      );
                    })}
                  </div>
                </section>
              );
            })}
          </div>
          {filteredModuleRows.length === 0 && (
            <div className="rounded-lg border border-dashed border-kumo-line p-8 text-center text-sm text-kumo-subtle">没有匹配模块，请调整搜索。</div>
          )}
        </SectionCard>
        </div>
      )}

      {activeTab === 'security' && (
        <div className="grid min-w-0 items-start gap-4 cq-xl:grid-cols-[minmax(22rem,0.9fr)_minmax(0,1.1fr)]">
          <div className="top-[calc(var(--app-header-height)+0.5rem)] z-20 flex min-w-0 flex-col gap-4 cq-xl:sticky">
          <SectionCard
            className={SECURITY_MASONRY_CARD_CLASS}
            title="管理员密码"
            icon={<Lock className="h-4 w-4 text-brand" />}
            bodyPadding="none"
          >
            <div className="flex w-full flex-col gap-4 p-5">
              <div>
                <Input size="sm"
                  label="当前密码"
                  type="text"
                  value={passwordForm.oldPassword}
                  onChange={(e) => setPasswordForm((prev) => ({ ...prev, oldPassword: e.target.value }))}
                  disabled={isDemoMode}
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  spellCheck={false}
                  className="w-full"
                />
              </div>
              <div className="grid grid-cols-1 gap-4 cq-sm:grid-cols-2">
                <Input size="sm"
                  label="新密码"
                  type="text"
                  value={passwordForm.newPassword}
                  onChange={(e) => setPasswordForm((prev) => ({ ...prev, newPassword: e.target.value }))}
                  disabled={isDemoMode}
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  spellCheck={false}
                  className="w-full"
                />
                <Input size="sm"
                  label="确认新密码"
                  type="text"
                  value={passwordForm.confirmPassword}
                  onChange={(e) => setPasswordForm((prev) => ({ ...prev, confirmPassword: e.target.value }))}
                  disabled={isDemoMode}
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  spellCheck={false}
                  className="w-full"
                />
              </div>
              <div>
                <Button size="sm" variant="primary" onClick={changePassword} loading={passwordSaving} disabled={isDemoMode}>
                  更新密码
                </Button>
              </div>
            </div>
          </SectionCard>

          <SectionCard
            className={SECURITY_MASONRY_CARD_CLASS}
            title="GitHub 一键登录"
            icon={<GitHubBrand className="h-4 w-4 text-brand" />}
            meta={(
              <Badge variant={githubAuth.enabled ? 'success' : 'secondary'}>
                {githubAuth.enabled ? '已启用' : '未启用'}
              </Badge>
            )}
            bodyPadding="lg"
          >
            <div className="grid gap-4">
              <div className="grid gap-4 border-b border-kumo-line/70 pb-4 cq-xl:grid-cols-2 cq-xl:gap-0">
                  <div className="grid gap-2 cq-xl:pr-5">
                    <div className="inline-flex items-center gap-2 text-sm font-semibold text-kumo-strong">
                      <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-brand/10 text-xs font-semibold text-brand">1</span>
                      <span>创建 OAuth App</span>
                    </div>
                    <div className="text-xs leading-relaxed text-kumo-subtle">
                      <code className="app-inline-code">Homepage URL</code> 填当前站点地址即可。
                    </div>
                    <ClipboardText
                      size="sm"
                      text={settings.publicApiUrl || currentOrigin}
                      className="min-w-0 w-full font-mono text-[11px]"
                      tooltip={{ text: '复制主页地址', copiedText: '主页地址已复制' }}
                      labels={{ copyAction: '复制主页地址' }}
                    />
                    <div className="flex flex-wrap gap-2 pt-1">
                      <a href={GITHUB_NEW_OAUTH_APP_URL} target="_blank" rel="noreferrer">
                        <Button size="sm" variant="secondary" icon={<ExternalLink className="h-4 w-4" />}>
                          新建 OAuth App
                        </Button>
                      </a>
                    </div>
                  </div>

                  <div className="grid gap-2 border-t border-kumo-line/70 pt-4 cq-xl:border-l cq-xl:border-t-0 cq-xl:pl-5 cq-xl:pt-0">
                    <div className="inline-flex items-center gap-2 text-sm font-semibold text-kumo-strong">
                      <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-brand/10 text-xs font-semibold text-brand">2</span>
                      <span>填回调并保存到下方</span>
                    </div>
                    <div className="text-xs leading-relaxed text-kumo-subtle">
                      <code className="app-inline-code">Authorization callback URL</code> 用下方地址；创建后把 <code className="app-inline-code">Client ID / Secret</code> 填到下面。
                    </div>
                    <ClipboardText
                      size="sm"
                      text={githubOAuthCallback}
                      className="min-w-0 w-full font-mono text-[11px]"
                      tooltip={{ text: '复制回调地址', copiedText: 'GitHub 回调地址已复制' }}
                      labels={{ copyAction: '复制回调地址' }}
                    />
                  </div>
              </div>

              <div className="grid gap-3 cq-sm:grid-cols-2">
                <Input
                  size="sm"
                  label="Client ID"
                  value={githubAuth.clientId}
                  onChange={(event) => setGitHubAuth((prev) => ({ ...prev, clientId: event.target.value }))}
                />
                <Input
                  size="sm"
                  label={githubAuth.hasClientSecret ? 'Client Secret（留空表示保持不变）' : 'Client Secret'}
                  type="password"
                  value={githubAuth.clientSecret}
                  onChange={(event) => setGitHubAuth((prev) => ({ ...prev, clientSecret: event.target.value }))}
                  autoComplete="off"
                  data-1p-ignore
                  data-lpignore="true"
                  data-bwignore="true"
                  data-form-type="other"
                  spellCheck={false}
                />
              </div>

              <div className="grid gap-3 cq-xl:grid-cols-2">
                <label className="grid gap-1.5 text-xs text-kumo-subtle">
                  <span className="font-semibold text-kumo-strong">允许登录的 GitHub 用户名</span>
                  <Textarea
                    value={githubAuth.allowedLoginsText}
                    onChange={(event) => setGitHubAuth((prev) => ({ ...prev, allowedLoginsText: event.target.value }))}
                    placeholder={'一行一个或逗号分隔\n如：iwvw'}
                    className="min-h-24"
                  />
                </label>
                <label className="grid gap-1.5 text-xs text-kumo-subtle">
                  <span className="font-semibold text-kumo-strong">允许登录的邮箱</span>
                  <Textarea
                    value={githubAuth.allowedEmailsText}
                    onChange={(event) => setGitHubAuth((prev) => ({ ...prev, allowedEmailsText: event.target.value }))}
                    placeholder={'可选；支持私人邮箱校验\n如：admin@example.com'}
                    className="min-h-24"
                  />
                </label>
              </div>

              <div className="flex flex-wrap items-center gap-3">
                <Switch
                  checked={githubAuth.enabled}
                  onCheckedChange={(checked) => setGitHubAuth((prev) => ({ ...prev, enabled: checked }))}
                  aria-label="启用 GitHub 登录"
                />
                <span className="text-sm text-kumo-strong">启用 GitHub 登录入口</span>
                <span className="text-xs text-kumo-subtle">保存后显示 GitHub 按钮。</span>
              </div>

              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  variant="primary"
                  onClick={saveGitHubLoginConfig}
                  loading={githubAuthSaving || githubAuthLoading}
                  disabled={isDemoMode}
                >
                  保存 GitHub 配置
                </Button>
              </div>
            </div>
          </SectionCard>
          </div>
          <div className="flex min-w-0 flex-col gap-4">
          <SectionCard
            className={SECURITY_MASONRY_CARD_CLASS}
            title="双因子认证与通行密钥"
            icon={<Shield className="h-4 w-4 text-brand" />}
            meta={(
              <div className="flex items-center gap-2">
                <Badge variant={twoFA.enabled ? 'success' : 'warning'}>
                  {twoFA.enabled ? 'TOTP 已启用' : 'TOTP 未启用'}
                </Badge>
                <Badge variant={passkeys.length > 0 ? 'success' : 'secondary'}>
                  {passkeys.length > 0 ? `${passkeys.length} 个通行密钥` : '无通行密钥'}
                </Badge>
              </div>
            )}
            bodyPadding="lg"
          >
            <div className="grid items-start gap-4 cq-xl:grid-cols-2">
              <AppCard padding="md" className="flex h-auto flex-col gap-4 self-start border border-kumo-line/80">
                <div className="space-y-1">
                  <div className="text-sm font-semibold text-kumo-strong">验证器</div>
                  <div className="text-xs leading-relaxed text-kumo-subtle">为密码和 GitHub 登录增加 6 位验证码</div>
                </div>

                {twoFA.error && (
                  <div className="rounded-md border border-kumo-danger/20 bg-kumo-danger/10 px-3 py-2 text-xs text-kumo-danger">
                    {twoFA.error}
                  </div>
                )}

                {!twoFA.enabled && !twoFA.setupMode && (
                  <Button size="sm" variant="primary" onClick={start2FASetup} loading={twoFA.loading} disabled={isDemoMode}>
                    启用 2FA
                  </Button>
                )}

                {twoFA.setupMode && (
                  <div className="grid gap-4">
                    {twoFA.qrCode && (
                      <AppCard padding="none" className="flex justify-center p-4">
                        <img src={twoFA.qrCode} alt="2FA QR Code" className="h-44 w-44" />
                      </AppCard>
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
                  <Button size="sm" variant="secondary-destructive" onClick={() => setTwoFA((prev) => ({ ...prev, disableMode: true, error: '' }))} disabled={isDemoMode}>
                    禁用 2FA
                  </Button>
                )}

                {twoFA.disableMode && (
                  <div className="grid gap-4">
                    <Input size="sm"
                      label="当前密码"
                      type="password"
                      value={twoFA.disablePassword}
                      onChange={(e) => setTwoFA((prev) => ({ ...prev, disablePassword: e.target.value }))}
                      autoComplete="off"
                      data-1p-ignore
                      data-lpignore="true"
                      data-bwignore="true"
                      data-form-type="other"
                      spellCheck={false}
                    />
                    <div className="flex gap-2">
                      <Button size="sm" onClick={() => setTwoFA((prev) => ({ ...prev, disableMode: false, disablePassword: '', error: '' }))}>取消</Button>
                      <Button size="sm" variant="destructive" onClick={disable2FA} loading={twoFA.loading}>确认禁用</Button>
                    </div>
                  </div>
                )}
              </AppCard>

              <AppCard padding="md" className="flex h-auto flex-col gap-4 self-start border border-kumo-line/80">
                <div className="space-y-1">
                  <div className="text-sm font-semibold text-kumo-strong">通行密钥</div>
                  <div className="text-xs leading-relaxed text-kumo-subtle">支持 Windows Hello、Touch ID、安全密钥等。</div>
                </div>

                <div className="grid gap-3">
                  <Input
                    size="sm"
                    label="通行密钥名称"
                    value={passkeyForm.label}
                    onChange={(event) => setPasskeyForm((prev) => ({ ...prev, label: event.target.value }))}
                    placeholder="如：Windows Hello"
                  />
                </div>

                <div className="flex flex-wrap gap-2">
                  <Button
                    size="sm"
                    variant="primary"
                    onClick={registerPasskey}
                    loading={passkeyBusy}
                    disabled={isDemoMode || !browserSupportsWebAuthn()}
                  >
                    添加通行密钥
                  </Button>
                  {!browserSupportsWebAuthn() && (
                    <span className="text-xs text-kumo-warning">当前环境不支持 WebAuthn</span>
                  )}
                </div>

                <div className="divide-y divide-kumo-line rounded-md border border-kumo-line/80">
                  {passkeysLoading && (
                    <div className="px-4 py-6 text-sm text-kumo-subtle">加载中...</div>
                  )}
                  {!passkeysLoading && passkeys.map((passkey) => (
                    <div key={passkey.id} className="grid gap-3 px-4 py-3 cq-md:grid-cols-[minmax(0,1fr)_auto] cq-md:items-center">
                      <div className="min-w-0">
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-sm font-semibold text-kumo-strong">{passkey.label || '通行密钥'}</span>
                          {passkey.attachment && <Badge variant="secondary">{passkey.attachment}</Badge>}
                          {passkey.backedUp ? <Badge variant="success">可同步</Badge> : null}
                        </div>
                        <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-xs text-kumo-subtle">
                          <span>添加时间: <span className="text-kumo-strong">{formatSessionTime(passkey.createdAt)}</span></span>
                          <span>最近使用: <span className="text-kumo-strong">{formatSessionTime(passkey.lastUsedAt)}</span></span>
                        </div>
                        <div className="mt-1 truncate font-mono text-[10px] text-kumo-subtle">{passkey.id}</div>
                      </div>
                      <Button
                        size="sm"
                        variant={isArmed(`passkey:${passkey.id}`) ? 'destructive' : 'secondary-destructive'}
                        onClick={() => removePasskey(passkey)}
                        loading={passkeyBusy}
                        disabled={isDemoMode}
                      >
                        删除
                      </Button>
                    </div>
                  ))}
                  {!passkeysLoading && passkeys.length === 0 && (
                    <div className="px-4 py-8 text-center text-sm text-kumo-subtle">暂无通行密钥</div>
                  )}
                </div>
              </AppCard>
            </div>
          </SectionCard>

          <SectionCard
            className={SECURITY_MASONRY_CARD_CLASS}
            title="登录设备"
            icon={<Globe className="h-4 w-4 text-brand" />}
            actions={(
              <div className="flex items-center gap-2">
                <Button
                  size="sm"
                  shape="square"
                  variant="secondary"
                  onClick={() => fetchLoginSessions().catch((error) => toast.error(error.message || '加载登录设备失败'))}
                  loading={sessionsLoading}
                  icon={<RefreshCw className="h-3.5 w-3.5" />}
                  aria-label="刷新登录设备"
                  title="刷新登录设备"
                />
                <Button size="sm" variant="secondary-destructive" onClick={forceAllSessionsOffline}>
                  全部下线
                </Button>
              </div>
            )}
            bodyPadding="none"
          >
            <div className="divide-y divide-kumo-line">
              {loginSessions.map((session) => (
                <div key={session.id} className="grid gap-3 px-4 py-3 cq-md:grid-cols-[minmax(0,1fr)_auto] cq-md:items-center">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-sm font-semibold text-kumo-strong">{describeUserAgent(session.userAgent)}</span>
                      {session.current && <Badge variant="success">当前设备</Badge>}
                      <span className="font-mono text-[10px] text-kumo-subtle">{session.id}</span>
                    </div>
                    <div className="mt-1 flex flex-wrap gap-x-4 gap-y-1 text-xs text-kumo-subtle">
                      <span>IP: <span className="font-mono text-kumo-strong">{session.ipAddress || '-'}</span></span>
                      <span>最后活动: <span className="text-kumo-strong">{formatSessionTime(session.lastAccessedAt)}</span></span>
                      <span>会话到期: <span className="text-kumo-strong">{formatSessionTime(session.expiresAt)}</span></span>
                    </div>
                    {session.userAgent && <div className="mt-1 truncate text-[10px] text-kumo-subtle" title={session.userAgent}>{session.userAgent}</div>}
                  </div>
                  <Button
                    size="sm"
                    variant={isArmed(`session-offline:${session.id}`) ? 'destructive' : 'secondary-destructive'}
                    onClick={() => forceSessionOffline(session)}
                  >
                    {isArmed(`session-offline:${session.id}`) ? '确认下线' : '强制下线'}
                  </Button>
                </div>
              ))}
              {!sessionsLoading && loginSessions.length === 0 && (
                <div className="px-4 py-8 text-center text-sm text-kumo-subtle">暂无有效登录设备</div>
              )}
            </div>
          </SectionCard>
          </div>
        </div>
      )}
      {activeTab === 'database' && (
        <div className="grid min-w-0 items-start gap-4 cq-xl:grid-cols-[minmax(22rem,0.9fr)_minmax(0,1.1fr)]">
          <div className="flex min-w-0 flex-col gap-4">
          <SectionCard
            className="min-w-0"
            title="数据库导入导出"
            icon={<Download className="h-4 w-4 text-brand" />}
            bodyPadding="sm"
            bodyClassName="space-y-3"
          >
            <Input
              ref={fileInputRef}
              type="file"
              accept=".db"
              aria-label="选择数据库文件"
              className="hidden"
              onChange={previewDatabaseImport}
            />
            <div className="flex flex-wrap gap-2">
              <Button
                size="sm"
                variant="secondary"
                onClick={exportDatabase}
                aria-label="导出数据库"
                title="导出数据库"
                icon={<Upload className="h-3.5 w-3.5" />}
              >
                导出数据库
              </Button>
              <Button
                size="sm"
                variant="primary"
                onClick={importDatabase}
                loading={databaseBusy}
                aria-label="导入数据库"
                title="导入数据库"
                icon={<Download className="h-3.5 w-3.5" />}
              >
                导入数据库
              </Button>
            </div>
            {dbImportPreview && (
              <AppCard padding="none" className="bg-kumo-recessed/40 p-3 text-xs">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-semibold text-kumo-strong truncate">{dbImportPreview.originalName}</span>
                  <Badge variant={dbImportPreview.analysis?.integrity === 'ok' ? 'success' : 'warning'}>
                    {dbImportPreview.analysis?.integrity || 'unknown'}
                  </Badge>
                </div>
                <div className="mt-3 grid grid-cols-2 gap-2">
                  <div className="rounded-md border border-kumo-line/70 bg-kumo-base px-3 py-2">
                    <div className="text-[10px] font-semibold uppercaser text-kumo-subtle">大小</div>
                    <div className="mt-1 font-mono text-kumo-strong">{formatFileSize(dbImportPreview.analysis?.sizeBytes)}</div>
                  </div>
                  <div className="rounded-md border border-kumo-line/70 bg-kumo-base px-3 py-2">
                    <div className="text-[10px] font-semibold uppercaser text-kumo-subtle">表数量</div>
                    <div className="mt-1 font-mono text-kumo-strong">{dbImportPreview.analysis?.tableCount || 0}</div>
                  </div>
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
                  <Button size="sm" variant="primary" className="flex-1 justify-center" onClick={commitDatabaseImport} loading={databaseBusy}>
                    确认导入
                  </Button>
                  <Button size="sm" variant="secondary" className="flex-1 justify-center" onClick={() => setDbImportPreview(null)}>
                    取消
                  </Button>
                </div>
              </AppCard>
            )}
          </SectionCard>

          <BackupPanel embedded />

          </div>
          <div className="flex min-w-0 flex-col gap-4">
          <SectionCard
            title="维护操作"
              icon={<HardDrive className="h-4 w-4 text-brand" />}
              bodyPadding="none"
            >
              <FieldRow title="压缩数据库">
                <Button
                  size="sm"
                  onClick={() => runDatabaseVacuum()}
                  loading={databaseBusy}
                >
                  立即压缩
                </Button>
              </FieldRow>

              <FieldRow title="清理运行日志">
                <Button
                  size="sm"
                  variant="secondary-destructive"
                  onClick={() => postSettingsAction('/api/settings/clear-logs', '数据库日志已清理', fetchDbState)}
                  loading={databaseBusy}
                  icon={<Trash className="h-4 w-4" />}
                >
                  清理日志
                </Button>
              </FieldRow>

              <FieldRow title="清理废弃表">
                <div className="flex items-center gap-2">
                  <Badge variant={deprecatedTableItems.length > 0 ? 'warning' : 'secondary'}>{deprecatedTableItems.length} 张</Badge>
                  <Button
                    size="sm"
                    variant="secondary-destructive"
                    onClick={cleanupDeprecatedTables}
                    loading={databaseBusy}
                    disabled={deprecatedTableItems.length === 0}
                    icon={<Trash className="h-4 w-4" />}
                  >
                    清理废弃表
                  </Button>
                </div>
              </FieldRow>

              <div className="mx-4 mb-4 mt-3 rounded-lg border border-kumo-line/80 bg-kumo-base px-3 pt-3 pb-2">
                <div className="flex flex-wrap items-start justify-between gap-3">
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-semibold text-kumo-strong">废弃表候选</div>
                  </div>
                  <Badge variant={deprecatedTableItems.length > 0 ? 'warning' : 'secondary'}>
                    {deprecatedTableItems.length} 张
                  </Badge>
                </div>

                <div className="mt-2 rounded-md border border-kumo-line/70 bg-kumo-recessed/20 px-3 py-2.5">
                  <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
                    <span className="text-kumo-subtle">候选表 <span className="font-semibold text-kumo-strong">{deprecatedTableItems.length}</span></span>
                    <span className="text-kumo-subtle">记录数 <span className="font-semibold text-kumo-strong">{deprecatedTables?.totalRows || 0}</span></span>
                    <span className="text-kumo-subtle">占用 <span className="font-semibold text-kumo-strong">{formatFileSize(deprecatedTables?.totalSize)}</span></span>
                  </div>
                </div>

                {deprecatedTableItems.length > 0 && (
                  <div className="mt-3 max-h-40 overflow-y-auto divide-y divide-kumo-line rounded-md border border-kumo-line/70 bg-kumo-recessed/10 text-[11px]">
                    {deprecatedTableItems.slice(0, 8).map((item) => (
                      <div key={item.table} className="grid grid-cols-[minmax(0,1fr)_auto] gap-3 px-3 py-2">
                        <div className="min-w-0">
                          <div className="truncate font-mono text-kumo-strong" title={item.table}>{item.table}</div>
                          <div className="mt-0.5 truncate text-kumo-subtle" title={item.reason}>{item.reason}</div>
                        </div>
                        <span className="font-mono text-kumo-subtle">{formatFileSize(item.sizeBytes)}</span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </SectionCard>

          <SectionCard
            title="数据库统计"
            description={dbStats?.dbPath || 'SQLite 数据文件'}
            icon={<Database className="h-4 w-4 text-brand" />}
            actions={
                <Button size="sm" onClick={() => fetchDbState().catch((error) => toast.error(error.message || '加载数据库统计失败'))} loading={databaseBusy} icon={<RefreshCw className="h-4 w-4" />}>刷新统计</Button>
            }
            bodyPadding="none"
            bodyClassName="flex min-h-0 flex-1 flex-col overflow-hidden"
          >
            {databaseStorage && (
              <div className="shrink-0 border-b border-kumo-line">
                <div className="p-3.5">
                  <div className="grid grid-cols-2 gap-2 cq-sm:grid-cols-4 cq-sm:gap-3">
                  {[
                    { title: '总占用', description: '主库 + WAL/SHM + 空闲页合计', value: formatFileSize(databaseStorage.totalSizeBytes), icon: <Database className="h-3.5 w-3.5 text-brand" />, valueClassName: 'text-brand' },
                    { title: '主库文件', description: 'SQLite 主数据库', value: formatFileSize(databaseStorage.mainSizeBytes), icon: <FileText className="h-3.5 w-3.5 text-kumo-strong" />, valueClassName: 'text-kumo-strong' },
                    { title: 'WAL / SHM', description: '预写日志与共享内存', value: formatFileSize((databaseStorage.walSizeBytes || 0) + (databaseStorage.shmSizeBytes || 0)), icon: <Activity className="h-3.5 w-3.5 text-kumo-warning" />, valueClassName: 'text-kumo-warning' },
                    { title: '空闲页', description: '可直接回收的空间', value: formatFileSize(databaseStorage.freePageBytes), icon: <Columns className="h-3.5 w-3.5 text-kumo-info" />, valueClassName: 'text-kumo-info' },
                  ].map((item) => (
                    <LayerCard key={item.title} className="min-w-0 p-2.5 cq-sm:p-3">
                      <div title={item.description} className="flex items-center justify-between gap-2 text-[11px] text-kumo-subtle cq-sm:gap-3 cq-sm:text-xs">
                        <span className="truncate">{item.title}</span>
                        <span className="shrink-0">{item.icon}</span>
                      </div>
                      <div className={`mt-1 truncate text-base font-semibold tabular-nums ${item.valueClassName}`} title={item.value}>{item.value}</div>
                    </LayerCard>
                  ))}
                  </div>
                </div>
                {databaseSegments.length > 0 && (
                  <div className="flex flex-col gap-2 border-t border-kumo-line bg-kumo-surface px-3.5 py-3">
                    <div className="flex h-1.5 w-full items-center overflow-hidden rounded-full bg-kumo-recessed">
                      {databaseSegments.map((s) => (
                        <div key={s.label} className={`h-full ${s.barClass}`} style={{ width: `${s.percent}%` }} title={`${s.label} ${formatFileSize(s.value)}`} />
                      ))}
                    </div>
                    <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
                      {databaseSegments.map((s) => (
                        <span key={s.label} className="inline-flex items-center gap-1.5 text-[10px] text-kumo-subtle">
                          <span className={`h-2 w-2 rounded-full ${s.barClass}`} />
                          {s.label}
                          <span className="tabular-nums text-kumo-default">{formatFileSize(s.value)}</span>
                        </span>
                      ))}
                    </div>
                  </div>
                )}
              </div>
            )}
            {tableRows.length > 0 && (
              <div className="flex shrink-0 items-center justify-between border-b border-kumo-line bg-kumo-surface px-3.5 py-1.5">
                <span className="text-xs font-semibold text-kumo-strong">数据库表（{tableRows.length} 张）</span>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  onClick={() => setDbTablesExpanded((v) => !v)}
                  className="gap-1 text-xs font-medium text-brand"
                >
                  {dbTablesExpanded ? '收起' : '展开全部'}
                  {dbTablesExpanded ? <ChevronUp className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
                </Button>
              </div>
            )}
            <div className="min-h-0 flex-1 overflow-auto">
              <Table layout="fixed">
                <colgroup>
                  <col className="w-[28%]" />
                  <col className="w-[14%]" />
                  <col className="w-[19%]" />
                  <col className="w-[18%]" />
                  <col className="w-[21%]" />
                </colgroup>
                <Table.Header>
                  <Table.Row>
                    <Table.Head>表名</Table.Head>
                    <Table.Head>记录数</Table.Head>
                    <Table.Head>占用</Table.Head>
                    <Table.Head>索引</Table.Head>
                    <Table.Head>行大小</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {tableRows.length === 0 ? (
                    <Table.Row>
                      <Table.Cell colSpan={5} className="p-8 text-center text-kumo-subtle">
                        {databaseBusy ? '正在加载统计...' : '暂无统计数据'}
                      </Table.Cell>
                    </Table.Row>
                  ) : !dbTablesExpanded ? (
                    <Table.Row>
                      <Table.Cell colSpan={5} className="p-8 text-center text-kumo-subtle">
                        已折叠 {tableRows.length} 张表，点击上方展开全部
                      </Table.Cell>
                    </Table.Row>
                  ) : dbTableDisplayRows.map((row) => (
                    <Table.Row key={row.table}>
                      <Table.Cell className="truncate font-mono text-xs text-kumo-strong" title={row.table}>{row.table}</Table.Cell>
                      <Table.Cell className="font-mono text-xs">{formatTableRows(row.rows)}</Table.Cell>
                      <Table.Cell className="font-mono text-xs">{formatTableMetricSize(row.estimatedSizeBytes)}</Table.Cell>
                      <Table.Cell className="font-mono text-xs">{formatTableMetricSize(row.indexSizeBytes)}</Table.Cell>
                      <Table.Cell className="font-mono text-xs">{formatTableMetricSize(row.avgRowSizeBytes)}</Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </div>
          </SectionCard>
          </div>
        </div>
      )}
      {activeTab === 'logs' && (
        <div className="grid items-start gap-4 cq-xl:grid-cols-[minmax(22rem,0.9fr)_minmax(0,1.1fr)]">
          <SectionCard
            className="top-[calc(var(--app-header-height)+0.5rem)] z-20 min-w-0 cq-xl:sticky"
            title="审计与保留"
            icon={<FileText className="h-4 w-4 text-brand" />}
            bodyPadding="none"
            actions={
              <Switch
                label="自动执行保留限制"
                checked={logSettings.autoCleanup}
                onCheckedChange={(checked) => setLogSettings((prev) => ({ ...prev, autoCleanup: checked }))}
              />
            }
          >
            <div className="p-5">
              <div className="grid grid-cols-1 gap-3 cq-sm:grid-cols-2 cq-md:grid-cols-3">
                <Input size="sm" label="保留天数" type="number" min="0" value={logSettings.days} onChange={(e) => setLogSettings((prev) => ({ ...prev, days: Math.max(0, toInt(e.target.value, 0)) }))} />
                <Input size="sm" label="单表最大条数" type="number" min="0" value={logSettings.count} onChange={(e) => setLogSettings((prev) => ({ ...prev, count: Math.max(0, toInt(e.target.value, 0)) }))} />
                <Input size="sm" label="数据库最大 MB" type="number" min="0" value={logSettings.dbSizeMB} onChange={(e) => setLogSettings((prev) => ({ ...prev, dbSizeMB: Math.max(0, toInt(e.target.value, 0)) }))} />
                <Input size="sm" label="app.log 最大 MB" type="number" min="1" value={logSettings.logFileSizeMB} onChange={(e) => setLogSettings((prev) => ({ ...prev, logFileSizeMB: Math.max(1, toInt(e.target.value, 10)) }))} />
                <Input size="sm" label="执行间隔（小时）" type="number" min="1" value={logSettings.autoCleanupHours} onChange={(e) => setLogSettings((prev) => ({ ...prev, autoCleanupHours: Math.max(1, toInt(e.target.value, 24)) }))} disabled={!logSettings.autoCleanup} />
              </div>

                <div className="mt-4 flex justify-end gap-2">
                  <Button size="sm" variant="secondary" onClick={saveLogSettings} loading={logsBusy} icon={<Save className="h-4 w-4" />}>保存策略</Button>
                  <Button size="sm" onClick={runEnforceLogLimits} loading={logsBusy} icon={<Trash className="h-4 w-4" />}>执行保留限制</Button>
                </div>
              </div>
            </SectionCard>

          <div className="min-w-0">
            <SectionCard
              className="min-w-0"
              title="审计记录"
              description="最近 100 条记录"
              icon={<Database className="h-4 w-4 text-brand" />}
              bodyPadding="none"
              bodyClassName="overflow-x-auto"
            >
              <Table layout="fixed" className="min-w-[700px]">
                <colgroup>
                  <col className="w-[170px]" />
                  <col className="w-[220px]" />
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
            </SectionCard>
          </div>
        </div>
      )}

      {activeTab === 'appearance' && (
        <div className="grid min-h-0 items-start gap-3 overflow-auto cq-xl:grid-cols-[minmax(20rem,0.82fr)_minmax(0,1.18fr)]">
          <SectionCard
            title="界面外观"
            icon={<Sun className="h-4 w-4 text-brand" />}
            bodyPadding="none"
          >
            <FieldRow title="主题模式">
              <Select alignItemWithTrigger size="sm" value={themeMode} onValueChange={handleThemeModeChange} items={THEME_OPTIONS} />
            </FieldRow>
            <FieldRow title="界面字体">
              <Select alignItemWithTrigger size="sm" value={settings.uiFont || 'default'} onValueChange={handleUIFontChange} items={FONT_OPTIONS} />
            </FieldRow>
            <FieldRow title="字号与布局">
              <Select alignItemWithTrigger size="sm" value={uiFontSize} onValueChange={handleUIFontSizeChange} items={FONT_SIZE_OPTIONS} />
            </FieldRow>
            <FieldRow title="显示首页页脚">
              <Switch aria-label="显示首页页脚" checked={settings.dashboardFooterVisible} onCheckedChange={handleDashboardFooterVisibleChange} />
            </FieldRow>
            <FieldRow title="备案号">
              <Input
                size="sm"
                aria-label="首页页脚备案号"
                value={settings.dashboardFooterRecordNumber}
                onChange={handleDashboardFooterRecordNumberChange}
                placeholder="例如：京ICP备12345678号"
                className="w-full min-w-52"
              />
            </FieldRow>
            <FieldRow title="触感反馈">
              <Switch checked={settings.vibrationEnabled} onCheckedChange={handleVibrationEnabledChange} />
            </FieldRow>
          </SectionCard>

          <SectionCard
            title="自定义 CSS"
            icon={<Terminal className="h-4 w-4 text-brand" />}
            actions={
                <>
                  <Button size="sm" onClick={() => applyCustomCss(settings.customCss)}>预览</Button>
                  <Button size="sm" variant="secondary-destructive" onClick={() => {
                    patchSettings({ customCss: '' });
                    applyCustomCss('');
                  }}>清空</Button>
                </>
            }
            bodyPadding="none"
          >
            <CodeEditor
              variant="embedded"
              label="CSS"
              language="css"
              value={settings.customCss}
              onChange={(customCss) => patchSettings({ customCss })}
              placeholder="/* 在此输入自定义 CSS */"
              minHeight="18rem"
              showHeader={false}
              showLanguage={false}
            />
          </SectionCard>
        </div>
      )}

      {activeTab === 'about' && (
        <div className="grid items-start gap-4 overflow-auto cq-lg:grid-cols-1">
          <SectionCard
            title={<span className="app-brand-wordmark">API Monitor</span>}
            icon={<img src="/logo.svg" alt="" className="h-6 w-6 object-contain" />}
          >
            <div className="grid gap-3 cq-sm:grid-cols-2">
              <div className="flex flex-col gap-2.5 rounded-lg border border-kumo-line bg-kumo-base/60 p-4 hover:border-brand/50">
                <div className="flex items-center gap-2">
                  <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                    <HardDrive className="h-3.5 w-3.5" />
                  </span>
                  <span className="text-xs font-medium text-kumo-subtle">当前版本</span>
                </div>
                <span className="truncate font-mono text-sm leading-6 text-kumo-strong">{APP_VERSION}</span>
              </div>
              <div className="flex flex-col gap-2.5 rounded-lg border border-kumo-line bg-kumo-base/60 p-4 hover:border-brand/50">
                <div className="flex items-center gap-2">
                  <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                    <Clock className="h-3.5 w-3.5" />
                  </span>
                  <span className="text-xs font-medium text-kumo-subtle">构建时间</span>
                </div>
                <span className="truncate font-mono text-sm leading-6 text-kumo-strong">
                  {APP_BUILD_TIME ? new Date(APP_BUILD_TIME).toLocaleString() : '未知'}
                </span>
              </div>
            </div>
            <div className="mt-3 grid gap-3 cq-sm:grid-cols-2 cq-lg:grid-cols-3">
              {[
                { label: 'React', value: FRAMEWORK_VERSIONS.react, icon: <Activity className="h-3.5 w-3.5" /> },
                { label: 'Vite', value: FRAMEWORK_VERSIONS.vite, icon: <Terminal className="h-3.5 w-3.5" /> },
                { label: 'Tailwind CSS', value: FRAMEWORK_VERSIONS.tailwind, icon: <Columns className="h-3.5 w-3.5" /> },
                { label: 'Kumo', value: FRAMEWORK_VERSIONS.kumo, icon: <LayoutDashboard className="h-3.5 w-3.5" /> },
                { label: 'Zustand', value: FRAMEWORK_VERSIONS.zustand, icon: <Database className="h-3.5 w-3.5" /> },
                { label: 'Go 后端', value: healthInfo?.goVersion || '…', icon: <Globe className="h-3.5 w-3.5" /> },
              ].map((item) => (
                <div key={item.label} className="flex flex-col gap-2.5 rounded-lg border border-kumo-line bg-kumo-base/60 p-4 hover:border-brand/50">
                  <div className="flex items-center gap-2">
                    <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                      {item.icon}
                    </span>
                    <span className="text-xs font-medium text-kumo-subtle">{item.label}</span>
                  </div>
                  <span className="truncate font-mono text-sm leading-6 text-kumo-strong">{item.value || '-'}</span>
                </div>
              ))}
            </div>
            <div className="mt-3 grid gap-3 cq-sm:grid-cols-2 cq-lg:grid-cols-3">
              <div className="flex flex-col gap-2.5 rounded-lg border border-kumo-line bg-kumo-base/60 p-4 hover:border-brand/50">
                <div className="flex items-center gap-2">
                  <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                    <Globe className="h-3.5 w-3.5" />
                  </span>
                  <span className="text-xs font-medium text-kumo-subtle">当前源</span>
                </div>
                <span className="truncate font-mono text-sm leading-6 text-kumo-strong">{currentOrigin}</span>
              </div>
              <div className="flex flex-col gap-2.5 rounded-lg border border-kumo-line bg-kumo-base/60 p-4 hover:border-brand/50">
                <div className="flex items-center gap-2">
                  <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                    <Terminal className="h-3.5 w-3.5" />
                  </span>
                  <span className="text-xs font-medium text-kumo-subtle">API 地址</span>
                </div>
                <span className="truncate font-mono text-sm leading-6 text-kumo-strong">{`${currentOrigin}/api`}</span>
              </div>
              <div className="flex flex-col gap-2.5 rounded-lg border border-kumo-line bg-kumo-base/60 p-4 hover:border-brand/50">
                <div className="flex items-center gap-2">
                  <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-lg bg-kumo-fill text-kumo-default">
                    <GitHubBrand className="h-3.5 w-3.5" />
                  </span>
                  <span className="text-xs font-medium text-kumo-subtle">仓库地址</span>
                </div>
                <a
                  href="https://github.com/iwvw/API-Monitor"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="truncate font-mono text-sm leading-6 text-kumo-strong hover:text-brand hover:underline"
                >
                  https://github.com/iwvw/API-Monitor
                </a>
              </div>
            </div>
          </SectionCard>

          {/* <LayerCard className="p-6">
            <h2 className="text-base font-semibold text-kumo-strong">已对接接口</h2>
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

      <Dialog.Root open={!!logPreview} onOpenChange={(open) => { if (!open) setLogPreview(null); }} role="alertdialog">
        <Dialog className="p-6" size="xl">
          <div className="flex items-center gap-3">
            <Trash className="h-5 w-5 text-kumo-danger" />
            <Dialog.Title>执行保留限制</Dialog.Title>
          </div>
          <Dialog.Description className="mt-3 text-kumo-subtle">
            将按当前保留策略清理以下{logPreview?.tables?.length || 0}张日志/自动生成表，预计删除 {logPreview?.totalDeleted ?? 0} 条记录。此操作不可恢复。
          </Dialog.Description>
          {logPreview?.tables?.length > 0 && (
            <div className="mt-4 max-h-64 overflow-auto rounded-lg border border-kumo-line">
              <Table layout="fixed">
                <colgroup>
                  <col />
                  <col className="w-[96px]" />
                  <col className="w-[96px]" />
                  <col className="w-[96px]" />
                </colgroup>
                <Table.Header>
                  <Table.Row>
                    <Table.Head>表</Table.Head>
                    <Table.Head>当前行数</Table.Head>
                    <Table.Head>保留</Table.Head>
                    <Table.Head>删除</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {logPreview.tables.map((row) => (
                    <Table.Row key={row.table}>
                      <Table.Cell className="font-mono text-xs">{row.table}</Table.Cell>
                      <Table.Cell>{row.current}</Table.Cell>
                      <Table.Cell>{row.kept}</Table.Cell>
                      <Table.Cell className="text-kumo-danger">{row.deleted}</Table.Cell>
                    </Table.Row>
                  ))}
                </Table.Body>
              </Table>
            </div>
          )}
          {logPreview?.sizeOverLimit && (
            <div className="mt-3 rounded-lg border border-kumo-warning/40 bg-kumo-warning/10 p-3 text-xs text-kumo-warning">
              数据库当前大小超过 {logPreview.dbSizeMB} MB 上限（当前 {logPreview.currentSizeMB}），还将循环删除各表最旧数据直至达标。
            </div>
          )}
          {logPreview?.tables?.some((row) => row.floor > 0) && (
            <div className="mt-3 rounded-lg border border-kumo-line bg-kumo-recessed p-3 text-xs text-kumo-subtle">
              已按「每个实体的最新记录」保底：单表条数低于实体数时，也不会删除各实体最新数据。
            </div>
          )}
          <div className="mt-6 flex justify-end gap-2">
            <Button size="sm" variant="secondary" onClick={() => setLogPreview(null)}>取消</Button>
            <Button size="sm" variant="destructive" onClick={confirmEnforceLogLimits}>确认清理</Button>
          </div>
        </Dialog>
      </Dialog.Root>
      </div>
    </div>
  );
}

export default SettingsPage;
