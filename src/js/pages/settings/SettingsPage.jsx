import React, { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Tabs } from '@cloudflare/kumo';
import { toast } from '../../modules/toast.js';
import { dialog } from '../../modules/dialog.js';
import { useConfirmPress } from '../../hooks/useConfirmPress.js';
import useStore, {
  MODULE_GROUPS,
  applyCustomCss,
  normalizeUserSettings,
} from '../../store.js';
import { useShallow } from 'zustand/react/shallow';
import { MODULE_TABS_PROPS } from '../../modules/kumoTabs.js';
import { applySiteBrandFaviconHref, getDefaultSiteBrandPreviewUrl } from '../../modules/siteBrand.js';
import { TabBarOverflowActions, stickyTabsBaseClass } from '../../components/ui/AppPrimitives.jsx';
import { browserSupportsWebAuthn, createPasskeyCredential } from '../../modules/webauthn.js';
import { RefreshCw, Save } from '../../components/Icons.jsx';
import { SETTINGS_TABS } from './tabs.jsx';
import { moduleRows } from './constants.js';
import { formatFileSize, getAuthHeaders, getUploadHeaders } from './utils.js';
import { GeneralPanel } from './GeneralPanel.jsx';
import { ModulesPanel } from './ModulesPanel.jsx';
import { SecurityPanel } from './SecurityPanel.jsx';
import { DatabasePanel } from './DatabasePanel.jsx';
import { LogsPanel } from './LogsPanel.jsx';
import { AppearancePanel } from './AppearancePanel.jsx';
import { AboutPanel } from './AboutPanel.jsx';
import { LogPreviewDialog } from './LogPreviewDialog.jsx';

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
        <GeneralPanel
          backendOnline={backendOnline}
          currentOrigin={currentOrigin}
          databaseSizeBytes={databaseSizeBytes}
          logFileInfo={logFileInfo}
          logSettings={logSettings}
          patchSettings={patchSettings}
          settings={settings}
        />
      )}


      {activeTab === 'modules' && (
        <ModulesPanel
          filteredModuleRows={filteredModuleRows}
          moduleGroups={moduleGroups}
          moduleSearch={moduleSearch}
          setModuleSearch={setModuleSearch}
          settings={settings}
          toggleModule={toggleModule}
        />
      )}

      {activeTab === 'security' && (
        <SecurityPanel
          changePassword={changePassword}
          confirm2FASetup={confirm2FASetup}
          currentOrigin={currentOrigin}
          disable2FA={disable2FA}
          fetchLoginSessions={fetchLoginSessions}
          forceAllSessionsOffline={forceAllSessionsOffline}
          forceSessionOffline={forceSessionOffline}
          githubAuth={githubAuth}
          githubAuthLoading={githubAuthLoading}
          githubAuthSaving={githubAuthSaving}
          githubOAuthCallback={githubOAuthCallback}
          isArmed={isArmed}
          isDemoMode={isDemoMode}
          loginSessions={loginSessions}
          passkeyBusy={passkeyBusy}
          passkeyForm={passkeyForm}
          passkeys={passkeys}
          passkeysLoading={passkeysLoading}
          passwordForm={passwordForm}
          passwordSaving={passwordSaving}
          registerPasskey={registerPasskey}
          removePasskey={removePasskey}
          saveGitHubLoginConfig={saveGitHubLoginConfig}
          sessionsLoading={sessionsLoading}
          setGitHubAuth={setGitHubAuth}
          setPasskeyForm={setPasskeyForm}
          setPasswordForm={setPasswordForm}
          setTwoFA={setTwoFA}
          settings={settings}
          start2FASetup={start2FASetup}
          twoFA={twoFA}
        />
      )}

      {activeTab === 'database' && (
        <DatabasePanel
          cleanupDeprecatedTables={cleanupDeprecatedTables}
          commitDatabaseImport={commitDatabaseImport}
          databaseBusy={databaseBusy}
          databaseSegments={databaseSegments}
          databaseStorage={databaseStorage}
          dbImportPreview={dbImportPreview}
          dbStats={dbStats}
          dbTableDisplayRows={dbTableDisplayRows}
          dbTablesExpanded={dbTablesExpanded}
          deprecatedTableItems={deprecatedTableItems}
          deprecatedTables={deprecatedTables}
          exportDatabase={exportDatabase}
          fetchDbState={fetchDbState}
          fileInputRef={fileInputRef}
          formatTableMetricSize={formatTableMetricSize}
          formatTableRows={formatTableRows}
          importDatabase={importDatabase}
          postSettingsAction={postSettingsAction}
          previewDatabaseImport={previewDatabaseImport}
          runDatabaseVacuum={runDatabaseVacuum}
          setDbImportPreview={setDbImportPreview}
          setDbTablesExpanded={setDbTablesExpanded}
          tableRows={tableRows}
        />
      )}

      {activeTab === 'logs' && (
        <LogsPanel
          logSettings={logSettings}
          logsBusy={logsBusy}
          operationLogs={operationLogs}
          runEnforceLogLimits={runEnforceLogLimits}
          saveLogSettings={saveLogSettings}
          setLogSettings={setLogSettings}
        />
      )}

      {activeTab === 'appearance' && (
        <AppearancePanel
          handleDashboardFooterRecordNumberChange={handleDashboardFooterRecordNumberChange}
          handleDashboardFooterVisibleChange={handleDashboardFooterVisibleChange}
          handleThemeModeChange={handleThemeModeChange}
          handleUIFontChange={handleUIFontChange}
          handleUIFontSizeChange={handleUIFontSizeChange}
          handleVibrationEnabledChange={handleVibrationEnabledChange}
          patchSettings={patchSettings}
          settings={settings}
          themeMode={themeMode}
          uiFontSize={uiFontSize}
        />
      )}

      {activeTab === 'about' && (
        <AboutPanel
          currentOrigin={currentOrigin}
          healthInfo={healthInfo}
        />
      )}

      <LogPreviewDialog
        confirmEnforceLogLimits={confirmEnforceLogLimits}
        logPreview={logPreview}
        setLogPreview={setLogPreview}
      />
      </div>
    </div>
  );
}

export default SettingsPage;
