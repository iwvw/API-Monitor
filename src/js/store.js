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
  'gemini-cli': {
    name: 'GCLI',
    shortName: 'GCLI',
    icon: 'fa-terminal',
    description: 'Gemini CLI API 代理服务',
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
  'self-h': {
    name: '自建服务',
    shortName: '自建',
    icon: 'fa-server',
    description: '自建服务管理',
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
    description: '主机实例管理与终端监控',
  },
  totp: {
    name: '双因子认证',
    shortName: '2FA',
    icon: 'fa-shield-alt',
    description: 'TOTP 验证器',
  },
  music: {
    name: '音乐',
    shortName: '音乐',
    icon: 'fa-music',
    description: '网易云音乐播放器',
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
  qwen: {
    name: '通义千问',
    shortName: '千问',
    icon: 'fa-magic',
    description: '通义千问 API 代理服务',
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
    modules: ['openai', 'gemini-cli', 'qwen'],
  },
  {
    id: 'infrastructure',
    name: '基础设施',
    icon: 'fa-cubes',
    modules: ['paas', 'dns', 'aliyun', 'tencent', 'server'],
  },
  {
    id: 'toolbox',
    name: '工具箱',
    icon: 'fa-toolbox',
    modules: ['self-h', 'totp', 'music', 'uptime', 'filebox', 'notification'],
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

export const THEME_MODE_OPTIONS = ['auto', 'light', 'dark'];
export const PAGE_WIDTH_OPTIONS = ['standard', 'wide', 'full'];

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

export const DEFAULT_CHANNEL_ENABLED = {
  'gemini-cli': true,
  qwen: true,
};

export const DEFAULT_CHANNEL_MODEL_PREFIX = {
  'gemini-cli': '',
  qwen: '',
};

const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
  'x-admin-password': localStorage.getItem('admin_password') || useStore.getState().loginPassword || '',
});

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
    ? settings.moduleOrder.filter((moduleId) => validModules.has(moduleId))
    : [];
  const moduleOrder = [
    ...savedOrder,
    ...DEFAULT_MODULE_ORDER.filter((moduleId) => !savedOrder.includes(moduleId)),
  ];

  const rawVisibility = settings.moduleVisibility || {};
  const moduleVisibility = DEFAULT_MODULE_ORDER.reduce((acc, moduleId) => {
    acc[moduleId] = moduleId === 'dashboard' ? true : rawVisibility[moduleId] !== false;
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
  root.dataset.theme = effectiveTheme;
  root.dataset.themeMode = themeMode;
};

// ==================== Zustand Store ====================
const getInitialThemeMode = () => {
  try {
    const savedMode = localStorage.getItem(THEME_STORAGE_KEY);
    if (THEME_MODE_OPTIONS.includes(savedMode)) return savedMode;

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
    if (PAGE_WIDTH_OPTIONS.includes(savedMode)) return savedMode;
  } catch (e) {
    console.error('Failed to get initial page width mode:', e);
  }
  return 'standard';
};

const initialPageWidthMode = getInitialPageWidthMode();

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
  sidebarCollapsed: false,
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

  // --- 4. 音乐播放器状态 ---
  musicPlaylist: [],
  musicCurrentIndex: -1,
  musicCurrentSong: null,
  musicPlaying: false,
  musicBuffering: false,
  musicCurrentTime: 0,
  musicDuration: 0,
  musicProgress: 0,
  musicVolume: 80,
  musicRepeatMode: 'all', // 'all' | 'one' | 'none'
  musicShuffleEnabled: false,
  musicLyrics: [],
  musicLyricsTranslation: [],
  musicCurrentLyricIndex: -1,
  musicCurrentLyricText: '',
  musicCurrentLyricTranslation: '',
  musicNextLyricText: '',
  musicNextLyricTranslation: '',
  musicShowFullPlayer: false,
  musicIsDragging: false,
  musicUser: null,
  musicShowLoginModal: false,
  musicCurrentTab: 'home',
  musicSearchKeyword: '',
  musicSearchResults: [],
  musicSearchPlaylists: [],
  musicSearchArtists: [],
  musicSearchType: 'songs',
  musicSearchOffset: 0,
  musicSearchHasMore: true,
  musicSearchLoading: false,
  musicSearchLoadingMore: false,
  musicMyPlaylists: [],
  musicCurrentPlaylistDetail: null,
  musicVirtualScrollTop: 0,
  musicPlaylistContainerHeight: 0,
  musicVirtualStartIndex: 0,
  musicPlaylistVisibleCount: 50,
  musicShowDetail: false,
  mfpLyricsMode: false,
  musicWidgetLoading: false,
  musicMuted: false,

  // --- 5. 修改状态的方法 ---
  setMainActiveTab: (tab) => set({ mainActiveTab: tab }),
  setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
  setNavGroupExpanded: (group) => set({ navGroupExpanded: group }),
  setPageWidthMode: (mode, persist = true) => {
    const normalizedMode = PAGE_WIDTH_OPTIONS.includes(mode) ? mode : 'standard';
    if (persist) {
      try {
        localStorage.setItem(PAGE_WIDTH_STORAGE_KEY, normalizedMode);
      } catch (e) {
        console.error('Failed to save page width mode:', e);
      }
    }
    set({ pageWidthMode: normalizedMode });
  },
  
  setThemeMode: (themeMode, persist = true) => {
    const normalizedMode = THEME_MODE_OPTIONS.includes(themeMode) ? themeMode : 'auto';
    const effectiveTheme = resolveThemeMode(normalizedMode);

    if (persist) {
      try {
        localStorage.setItem(THEME_STORAGE_KEY, normalizedMode);
        localStorage.removeItem(LEGACY_THEME_STORAGE_KEY);
      } catch (e) {
        console.error('Failed to save theme mode:', e);
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
    set({
      userSettingsLoaded: true,
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
      return get().applyUserSettings(result.data || {});
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
      // 1. 优先检查当前 Session 是否已认证
      const sessionRes = await fetch('/api/session');
      const { authenticated } = await sessionRes.json();
      if (authenticated) {
        set({ isAuthenticated: true, showLoginModal: false, isCheckingAuth: false });
        return true;
      }

      // 2. 如果 Session 不存在，再检查基本配置并尝试自动登录
      const res = await fetch('/api/check-password');
      const { hasPassword, isDemoMode } = await res.json();
      set({ isDemoMode });

      if (isDemoMode) {
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

      if (savedPassword && savedTime) {
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

      const response = await fetch('/api/login', {
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
  logout: () => {
    set({ isAuthenticated: false, loginPassword: '' });
    localStorage.removeItem('admin_password');
    localStorage.removeItem('password_time');
    toastManager.success('已安全登出');
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
