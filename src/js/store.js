/**
 * API Monitor - 全局状态管理 (Zustand Store)
 * 基于 Zustand 实现轻量、模块化、标准化的全局状态管理，彻底摆脱 Vue 3 依赖
 */

import { create } from 'zustand';
import { dialog } from './modules/dialog.js';
import toastManager from './modules/toast.js';

// ==================== 模块元数据配置 ====================
export const MODULE_CONFIG = {
  dashboard: {
    name: '仪表盘',
    shortName: '总览',
    icon: 'fa-tachometer-alt',
    description: '系统状态与数据概览',
  },
  settings: {
    name: '系统设置',
    shortName: '设置',
    icon: 'fa-cog',
    description: '全局配置、安全认证与外观主题',
  },
  openai: {
    name: 'OpenAI',
    shortName: 'OAI',
    icon: 'fa-robot',
    description: 'OpenAI 兼容 API 管理与聊天',
  },

  paas: {
    name: 'PaaS',
    shortName: 'PaaS',
    icon: 'fa-cloud',
    description: 'Koyeb / Fly.io 平台监控',
  },
  dns: {
    name: 'Cloudflare',
    shortName: 'CF',
    icon: 'fa-globe',
    description: 'Cloudflare DNS / Workers / Pages 管理',
  },
  aliyun: {
    name: '阿里云',
    shortName: '阿里',
    icon: 'fa-cloud',
    description: '阿里云 DNS / ECS 管理',
  },
  scheduler: {
    name: '定时任务',
    shortName: '任务',
    icon: 'fa-clock',
    description: '定时任务管理',
  },
  tencent: {
    name: '腾讯云',
    shortName: '腾讯',
    icon: 'fa-hdd',
    description: '腾讯云 DNS / CVM 管理',
  },
  server: {
    name: '主机实例',
    shortName: '主机',
    icon: 'fa-server',
    description: '主机管理与终端监控',
  },
  totp: {
    name: '双因子认证',
    shortName: '2FA',
    icon: 'fa-shield-alt',
    description: 'TOTP 验证器',
  },
  uptime: {
    name: '可用性监测',
    shortName: '监控',
    icon: 'fa-heartbeat',
    description: '站点与服务可用性监测',
  },
  filebox: {
    name: '文件柜',
    shortName: '文件',
    icon: 'fa-box-open',
    description: '文件分享与暂存',
  },
  notification: {
    name: '通知',
    shortName: '通知',
    icon: 'fa-bell',
    description: '通知渠道与告警规则管理',
  },
  apidocs: {
    name: 'API 文档',
    shortName: '文档',
    icon: 'fa-file-code',
    description: '系统接口索引与 AI 接入蓝图',
  },
  systemlogs: {
    name: '系统日志',
    shortName: '日志',
    icon: 'fa-file-alt',
    description: '统一日志查看器',
  },

};

// ==================== 模块分组配置 ====================
export const MODULE_GROUPS = [
  {
    id: 'overview',
    name: '仪表盘',
    icon: 'fa-tachometer-alt',
    modules: ['dashboard'],
  },
  {
    id: 'api-gateway',
    name: 'API 网关',
    icon: 'fa-bolt',
    modules: ['openai'],
  },
  {
    id: 'infrastructure',
    name: '云服务',
    icon: 'fa-cubes',
    modules: ['paas', 'dns', 'aliyun', 'tencent', 'server'],
  },
  {
    id: 'toolbox',
    name: '工具箱',
    icon: 'fa-toolbox',
    modules: ['scheduler', 'totp', 'uptime', 'filebox', 'notification'],
  },
  {
    id: 'system',
    name: '系统',
    icon: 'fa-cog',
    modules: ['apidocs', 'systemlogs'],
  },
];

// ==================== 兼容性助手函数 ====================
export function getModuleName(moduleId, short = false) {
  const config = MODULE_CONFIG[moduleId];
  if (!config) return moduleId;
  return short ? config.shortName : config.name;
}

export function getModuleIcon(moduleId) {
  const config = MODULE_CONFIG[moduleId];
  return config ? config.icon : 'fa-cube';
}

const THEME_STORAGE_KEY = 'app_theme_mode';
const LEGACY_THEME_STORAGE_KEY = 'app_theme';
const PAGE_WIDTH_STORAGE_KEY = 'app_page_width_mode';
const SIDEBAR_COLLAPSED_STORAGE_KEY = 'app_sidebar_collapsed';
const AUTH_LOGGED_OUT_STORAGE_KEY = 'auth_explicitly_logged_out';

export const THEME_MODE_OPTIONS = ['auto', 'light', 'dark'];
export const PAGE_WIDTH_OPTIONS = ['standard', 'wide', 'full'];
export const DEFAULT_PAGE_WIDTH_MODE = 'full';

const normalizeThemeMode = (mode, fallback = 'auto') => (
  THEME_MODE_OPTIONS.includes(mode) ? mode : fallback
);

const normalizePageWidthMode = (mode, fallback = DEFAULT_PAGE_WIDTH_MODE) => (
  PAGE_WIDTH_OPTIONS.includes(mode) ? mode : fallback
);

const normalizeSidebarCollapsed = (value, fallback = false) => {
  if (value === true || value === 'true' || value === 1 || value === '1') return true;
  if (value === false || value === 'false' || value === 0 || value === '0') return false;
  return fallback;
};

export const DEFAULT_TOTP_SETTINGS = {
  hideCode: false,
  allowRevealCode: true,
  groupByPlatform: true,
  showPlatformHeaders: true,
  hidePlatformText: false,
  maskAccount: false,
  autoSave: true,
  lockInputMode: false,
  defaultInputMode: 'scan',
};

export const DEFAULT_MODULE_ORDER = MODULE_GROUPS.flatMap((group) => group.modules);

export const DEFAULT_MODULE_VISIBILITY = DEFAULT_MODULE_ORDER.reduce((acc, moduleId) => {
  acc[moduleId] = true;
  return acc;
}, {});


export const DEFAULT_CHANNEL_ENABLED = {};

export const DEFAULT_CHANNEL_MODEL_PREFIX = {};

const LEGACY_MODULE_ALIASES = {
  'self-h': 'scheduler',
};

const normalizeModuleId = (moduleId) => LEGACY_MODULE_ALIASES[moduleId] || moduleId;

const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
  'x-admin-password': localStorage.getItem('admin_password') || useStore.getState().loginPassword || '',
});

let appearanceSettingsSaveTimer = null;
let pendingAppearanceSettingsPatch = {};

const scheduleAppearanceSettingsSave = (patch) => {
  pendingAppearanceSettingsPatch = {
    ...pendingAppearanceSettingsPatch,
    ...patch,
  };

  if (appearanceSettingsSaveTimer) {
    window.clearTimeout(appearanceSettingsSaveTimer);
  }

  appearanceSettingsSaveTimer = window.setTimeout(async () => {
    const payload = pendingAppearanceSettingsPatch;
    pendingAppearanceSettingsPatch = {};
    appearanceSettingsSaveTimer = null;

    try {
      const response = await fetch('/api/settings', {
        method: 'PATCH',
        headers: getAuthHeaders(),
        body: JSON.stringify(payload),
      });
      const result = await response.json().catch(() => ({}));
      if (!response.ok || result.success === false) {
        throw new Error(result.error || 'Failed to save appearance settings');
      }
    } catch (error) {
      console.error('Failed to save appearance settings:', error);
    }
  }, 250);
};

export const applyCustomCss = (css = '') => {
  if (typeof document === 'undefined') return;

  let style = document.getElementById('custom-css-dynamic');
  if (!style) {
    style = document.createElement('style');
    style.id = 'custom-css-dynamic';
    document.head.appendChild(style);
  }
  style.textContent = css || '';
};

export const normalizeUserSettings = (settings = {}) => {
  const validModules = new Set(DEFAULT_MODULE_ORDER);
  const savedOrder = Array.isArray(settings.moduleOrder)
    ? settings.moduleOrder
      .map(normalizeModuleId)
      .filter((moduleId, index, order) => validModules.has(moduleId) && order.indexOf(moduleId) === index)
    : [];
  const moduleOrder = [
    ...savedOrder,
    ...DEFAULT_MODULE_ORDER.filter((moduleId) => !savedOrder.includes(moduleId)),
  ];

  const rawVisibility = settings.moduleVisibility || {};
  const moduleVisibility = DEFAULT_MODULE_ORDER.reduce((acc, moduleId) => {
    const legacyModuleId = Object.entries(LEGACY_MODULE_ALIASES).find(([, current]) => current === moduleId)?.[0];
    acc[moduleId] = moduleId === 'dashboard'
      ? true
      : rawVisibility[moduleId] ?? rawVisibility[legacyModuleId] ?? DEFAULT_MODULE_VISIBILITY[moduleId] ?? true;
    return acc;
  }, {});

  const rawChannelEnabled = {
    ...DEFAULT_CHANNEL_ENABLED,
    ...(settings.channelEnabled || {}),
  };
  const rawChannelModelPrefix = {
    ...DEFAULT_CHANNEL_MODEL_PREFIX,
    ...(settings.channelModelPrefix || {}),
  };
  const allowedChannels = new Set(Object.keys(DEFAULT_CHANNEL_ENABLED));
  const channelEnabled = Object.fromEntries(
    Object.entries(rawChannelEnabled).filter(([channel]) => allowedChannels.has(channel))
  );
  const channelModelPrefix = Object.fromEntries(
    Object.entries(rawChannelModelPrefix).filter(([channel]) => allowedChannels.has(channel))
  );

  return {
    customCss: settings.customCss || '',
    themeMode: normalizeThemeMode(
      settings.themeMode || settings.theme_mode,
      typeof getInitialThemeMode === 'function' ? getInitialThemeMode() : 'auto'
    ),
    pageWidthMode: normalizePageWidthMode(
      settings.pageWidthMode || settings.page_width_mode,
      typeof getInitialPageWidthMode === 'function' ? getInitialPageWidthMode() : DEFAULT_PAGE_WIDTH_MODE
    ),
    sidebarCollapsed: normalizeSidebarCollapsed(
      settings.sidebarCollapsed ?? settings.sidebar_collapsed,
      typeof getInitialSidebarCollapsed === 'function' ? getInitialSidebarCollapsed() : false
    ),
    koyebRefreshInterval: Number(settings.koyebRefreshInterval) || 30000,
    flyRefreshInterval: Number(settings.flyRefreshInterval) || 30000,
    moduleVisibility,
    channelEnabled,
    channelModelPrefix,
    moduleOrder,
    load_balancing_strategy: settings.load_balancing_strategy || 'random',
    serverIpDisplayMode: settings.serverIpDisplayMode || 'normal',
    vibrationEnabled: settings.vibrationEnabled !== undefined ? Boolean(settings.vibrationEnabled) : true,
    navLayout: settings.navLayout || 'top',
    totpSettings: {
      ...DEFAULT_TOTP_SETTINGS,
      ...(settings.totpSettings || {}),
      defaultInputMode: ['scan', 'upload', 'manual'].includes(settings.totpSettings?.defaultInputMode)
        ? settings.totpSettings.defaultInputMode
        : DEFAULT_TOTP_SETTINGS.defaultInputMode,
    },
    agentDownloadUrl: settings.agentDownloadUrl || '',
    publicApiUrl: settings.publicApiUrl || '',
  };
};

export const getSystemTheme = () => {
  if (typeof window !== 'undefined' && window.matchMedia) {
    return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
  }
  return 'dark';
};

export const resolveThemeMode = (themeMode) => {
  if (themeMode === 'light' || themeMode === 'dark') return themeMode;
  return getSystemTheme();
};

export const applyThemeMode = (themeMode) => {
  if (typeof document === 'undefined') return;

  const effectiveTheme = resolveThemeMode(themeMode);
  const root = document.documentElement;
  root.classList.toggle('dark', effectiveTheme === 'dark');
  root.classList.toggle('light', effectiveTheme === 'light');
  root.dataset.mode = effectiveTheme;
  root.dataset.theme = 'kumo';
  root.dataset.themeMode = themeMode;
  root.style.colorScheme = effectiveTheme;
};

// ==================== Zustand Store ====================
const getInitialThemeMode = () => {
  try {
    const savedMode = localStorage.getItem(THEME_STORAGE_KEY);
    if (normalizeThemeMode(savedMode, null)) return savedMode;

    const legacyTheme = localStorage.getItem(LEGACY_THEME_STORAGE_KEY);
    if (legacyTheme === 'dark' || legacyTheme === 'light') return legacyTheme;
  } catch (e) {
    console.error('Failed to get initial theme mode:', e);
  }
  return 'auto';
};

const initialThemeMode = getInitialThemeMode();

const getInitialPageWidthMode = () => {
  try {
    const savedMode = localStorage.getItem(PAGE_WIDTH_STORAGE_KEY);
    if (normalizePageWidthMode(savedMode, null)) return savedMode;
  } catch (e) {
    console.error('Failed to get initial page width mode:', e);
  }
  return DEFAULT_PAGE_WIDTH_MODE;
};

const initialPageWidthMode = getInitialPageWidthMode();

const getInitialSidebarCollapsed = () => {
  try {
    return normalizeSidebarCollapsed(localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY), false);
  } catch (e) {
    console.error('Failed to get initial sidebar mode:', e);
  }
  return false;
};

const initialSidebarCollapsed = getInitialSidebarCollapsed();

const useStore = create((set, get) => ({
  // --- 1. 认证状态 ---
  isAuthenticated: false,
  isCheckingAuth: true,
  showLoginModal: false,
  showSetPasswordModal: false,
  loginError: '',
  loginPassword: '',
  loginLoading: false,
  loginRequire2FA: false,
  loginTotpToken: '',
  isDemoMode: false,

  // --- 2. 界面与布局状态 ---
  mainActiveTab: 'dashboard',
  sidebarCollapsed: initialSidebarCollapsed,
  themeMode: initialThemeMode,
  theme: resolveThemeMode(initialThemeMode),
  pageWidthMode: initialPageWidthMode,
  navGroupExpanded: null,
  userSettingsLoaded: false,
  userSettingsLoading: false,
  customCss: '',
  moduleVisibility: DEFAULT_MODULE_VISIBILITY,
  moduleOrder: DEFAULT_MODULE_ORDER,
  channelEnabled: DEFAULT_CHANNEL_ENABLED,
  channelModelPrefix: DEFAULT_CHANNEL_MODEL_PREFIX,
  loadBalancingStrategy: 'random',
  serverIpDisplayMode: 'normal',
  vibrationEnabled: true,
  navLayout: 'top',
  totpSettings: DEFAULT_TOTP_SETTINGS,
  agentDownloadUrl: '',
  publicApiUrl: '',
  koyebRefreshInterval: 30000,
  flyRefreshInterval: 30000,

  showAlert: (message, title) => dialog.alert(message, title),
  showConfirm: (options) => dialog.confirm(options),
  showPrompt: (options) => dialog.prompt(options),
  
  // --- 3. 页面数据占位 ---
  serverList: [],
  dnsZones: [],
  dnsRecords: [],

  // --- 5. 修改状态的方法 ---
  setMainActiveTab: (tab) => set({ mainActiveTab: tab }),
  setSidebarCollapsed: (collapsed, persist = true) => {
    const normalizedCollapsed = normalizeSidebarCollapsed(collapsed);
    if (persist) {
      try {
        localStorage.setItem(SIDEBAR_COLLAPSED_STORAGE_KEY, String(normalizedCollapsed));
      } catch (e) {
        console.error('Failed to save sidebar mode:', e);
      }
      if (get().isAuthenticated) {
        scheduleAppearanceSettingsSave({ sidebarCollapsed: normalizedCollapsed });
      }
    }
    set({ sidebarCollapsed: normalizedCollapsed });
  },
  setNavGroupExpanded: (group) => set({ navGroupExpanded: group }),
  setPageWidthMode: (mode, persist = true) => {
    const normalizedMode = normalizePageWidthMode(mode);
    if (persist) {
      try {
        localStorage.setItem(PAGE_WIDTH_STORAGE_KEY, normalizedMode);
      } catch (e) {
        console.error('Failed to save page width mode:', e);
      }
      if (get().isAuthenticated) {
        scheduleAppearanceSettingsSave({ pageWidthMode: normalizedMode });
      }
    }
    set({ pageWidthMode: normalizedMode });
  },
  
  setThemeMode: (themeMode, persist = true) => {
    const normalizedMode = normalizeThemeMode(themeMode);
    const effectiveTheme = resolveThemeMode(normalizedMode);

    if (persist) {
      try {
        localStorage.setItem(THEME_STORAGE_KEY, normalizedMode);
        localStorage.removeItem(LEGACY_THEME_STORAGE_KEY);
      } catch (e) {
        console.error('Failed to save theme mode:', e);
      }
      if (get().isAuthenticated) {
        scheduleAppearanceSettingsSave({ themeMode: normalizedMode });
      }
    }
    applyThemeMode(normalizedMode);
    set({ themeMode: normalizedMode, theme: effectiveTheme });
  },

  setTheme: (theme, persist = true) => {
    get().setThemeMode(theme, persist);
  },

  applyUserSettings: (settings) => {
    const normalized = normalizeUserSettings(settings);
    applyCustomCss(normalized.customCss);
    applyThemeMode(normalized.themeMode);
    try {
      localStorage.setItem(THEME_STORAGE_KEY, normalized.themeMode);
      localStorage.removeItem(LEGACY_THEME_STORAGE_KEY);
      localStorage.setItem(PAGE_WIDTH_STORAGE_KEY, normalized.pageWidthMode);
      localStorage.setItem(SIDEBAR_COLLAPSED_STORAGE_KEY, String(normalized.sidebarCollapsed));
    } catch (e) {
      console.error('Failed to cache appearance settings:', e);
    }
    set({
      userSettingsLoaded: true,
      themeMode: normalized.themeMode,
      theme: resolveThemeMode(normalized.themeMode),
      pageWidthMode: normalized.pageWidthMode,
      sidebarCollapsed: normalized.sidebarCollapsed,
      customCss: normalized.customCss,
      moduleVisibility: normalized.moduleVisibility,
      moduleOrder: normalized.moduleOrder,
      channelEnabled: normalized.channelEnabled,
      channelModelPrefix: normalized.channelModelPrefix,
      loadBalancingStrategy: normalized.load_balancing_strategy,
      serverIpDisplayMode: normalized.serverIpDisplayMode,
      vibrationEnabled: normalized.vibrationEnabled,
      navLayout: normalized.navLayout,
      totpSettings: normalized.totpSettings,
      agentDownloadUrl: normalized.agentDownloadUrl,
      publicApiUrl: normalized.publicApiUrl,
      koyebRefreshInterval: normalized.koyebRefreshInterval,
      flyRefreshInterval: normalized.flyRefreshInterval,
    });
    return normalized;
  },

  loadUserSettings: async () => {
    set({ userSettingsLoading: true });
    try {
      const response = await fetch('/api/settings', {
        headers: getAuthHeaders(),
      });
      const result = await response.json();
      if (!response.ok || !result.success) {
        throw new Error(result.error || 'Failed to load settings');
      }
      const rawSettings = result.data || {};
      const normalized = get().applyUserSettings(rawSettings);
      const appearancePatch = {};
      if (!rawSettings.themeMode && !rawSettings.theme_mode) {
        appearancePatch.themeMode = normalized.themeMode;
      }
      if (!rawSettings.pageWidthMode && !rawSettings.page_width_mode) {
        appearancePatch.pageWidthMode = normalized.pageWidthMode;
      }
      if (rawSettings.sidebarCollapsed === undefined && rawSettings.sidebar_collapsed === undefined) {
        appearancePatch.sidebarCollapsed = normalized.sidebarCollapsed;
      }
      if (Object.keys(appearancePatch).length > 0) {
        scheduleAppearanceSettingsSave(appearancePatch);
      }
      return normalized;
    } catch (error) {
      console.error('Failed to load user settings:', error);
      return null;
    } finally {
      set({ userSettingsLoading: false });
    }
  },

  setLoginPassword: (password) => set({ loginPassword: password }),
  setLoginTotpToken: (token) => set({ loginTotpToken: token }),
  
  cancelLogin2FA: () => set({ loginRequire2FA: false, loginTotpToken: '', loginError: '' }),

  // --- 5. 异步认证动作 ---
  
  // 校验当前登录状态
  checkAuth: async () => {
    set({ isCheckingAuth: true });
    try {
      const explicitlyLoggedOut = !!localStorage.getItem(AUTH_LOGGED_OUT_STORAGE_KEY);

      // 1. 优先检查当前 Session 是否已认证；显式退出后不再信任残留 Cookie。
      if (!explicitlyLoggedOut) {
        const sessionRes = await fetch('/api/auth/session');
        const { authenticated } = await sessionRes.json();
        if (authenticated) {
          set({ isAuthenticated: true, showLoginModal: false, isCheckingAuth: false });
          return true;
        }
      }

      // 2. 如果 Session 不存在，再检查基本配置并尝试自动登录
      const res = await fetch('/api/auth/check-password');
      const { hasPassword, isDemoMode } = await res.json();
      set({ isDemoMode });

      if (isDemoMode) {
        if (explicitlyLoggedOut) {
          set({ isAuthenticated: false, showLoginModal: true, loginPassword: '' });
          return false;
        }

        const savedTime = localStorage.getItem('password_time');
        const now = Date.now();
        const isValidSession = savedTime && now - parseInt(savedTime) < 4 * 24 * 60 * 60 * 1000;

        if (!isValidSession) {
          set({ loginPassword: '' });
          return await get().verifyPassword();
        } else {
          set({ isAuthenticated: true, showLoginModal: false });
          return true;
        }
      }

      if (!hasPassword) {
        set({ showSetPasswordModal: true, isAuthenticated: false });
        return false;
      }

      const savedPassword = localStorage.getItem('admin_password');
      const savedTime = localStorage.getItem('password_time');

      if (!explicitlyLoggedOut && savedPassword && savedTime) {
        const now = Date.now();
        if (now - parseInt(savedTime) < 4 * 24 * 60 * 60 * 1000) {
          set({ loginPassword: savedPassword });
          await get().verifyPassword(true); // 静默验证
          if (!get().isAuthenticated) {
            set({ showLoginModal: true });
          }
          return get().isAuthenticated;
        }
      }

      set({ showLoginModal: true });
      return false;
    } catch (e) {
      console.error('Auth check error:', e);
      set({ showLoginModal: true });
      return false;
    } finally {
      set({ isCheckingAuth: false });
    }
  },

  // 验证登录密码
  verifyPassword: async (silent = false) => {
    set({ loginError: '', loginLoading: true });
    try {
      const { loginPassword, loginRequire2FA, loginTotpToken } = get();
      const requestBody = { password: loginPassword };
      
      if (loginRequire2FA && loginTotpToken) {
        requestBody.totpToken = loginTotpToken;
      }

      const response = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(requestBody),
      });

      const result = await response.json();

      if (response.status === 429) {
        const errorMsg = result.error || '登录尝试过于频繁，请稍后再试';
        set({ loginError: errorMsg });
        toastManager.warning(errorMsg);
        return false;
      }

      if (result.require2FA && !result.success) {
        set({ loginRequire2FA: true, loginTotpToken: '' });
        if (result.error) {
          set({ loginError: result.error });
        }
        return false;
      }

      if (result.success) {
        set({
          isAuthenticated: true,
          showLoginModal: false,
          loginRequire2FA: false,
          loginTotpToken: '',
        });

        localStorage.setItem('admin_password', loginPassword);
        localStorage.setItem('password_time', Date.now().toString());
        localStorage.removeItem(AUTH_LOGGED_OUT_STORAGE_KEY);

        if (!silent) {
          toastManager.success('登录成功，欢迎回来！');
        }
        return true;
      } else {
        let errorMsg = '密码错误，请重试';
        const errData = result.error;
        if (errData) {
          if (typeof errData === 'string') {
            errorMsg = errData;
          } else if (errData.message) {
            errorMsg = errData.message;
          }
        }
        set({ loginError: errorMsg });
        if (!silent) {
          toastManager.error(errorMsg);
        }
        return false;
      }
    } catch (error) {
      const catchMsg = error.message || '网络验证失败';
      set({ loginError: catchMsg });
      if (!silent) {
        toastManager.error(catchMsg);
      }
      return false;
    } finally {
      set({ loginLoading: false });
    }
  },

  // 登出
  logout: async () => {
    try {
      localStorage.setItem(AUTH_LOGGED_OUT_STORAGE_KEY, Date.now().toString());
      localStorage.removeItem('admin_password');
      localStorage.removeItem('password_time');
    } catch (error) {
      console.error('Failed to clear auth cache:', error);
    }

    set({
      isAuthenticated: false,
      showLoginModal: true,
      showSetPasswordModal: false,
      loginPassword: '',
      loginError: '',
      loginLoading: false,
      loginRequire2FA: false,
      loginTotpToken: '',
    });

    try {
      const response = await fetch('/api/auth/logout', {
        method: 'POST',
      });
      const result = await response.json().catch(() => ({}));
      if (!response.ok || result.success === false) {
        throw new Error(result.error || '后端会话注销失败');
      }
      toastManager.success('已安全登出');
    } catch (error) {
      console.error('Logout request failed:', error);
      toastManager.warning('已清除本地登录状态，但后端会话注销失败，请重试或重启服务后确认');
    }
  },
}));

export const store = new Proxy({}, {
  get(target, prop) {
    if (prop === 'getAuthHeaders') {
      return getAuthHeaders;
    }
    return useStore.getState()[prop];
  },
  set(target, prop, value) {
    useStore.setState({ [prop]: value });
    return true;
  }
});

export default useStore;
