/**
 * API Monitor - 全局状态管理 (Zustand Store)
 * 基于 Zustand 实现轻量、模块化、标准化的全局状态管理，彻底摆脱 Vue 3 依赖
 */

import { create } from 'zustand';
import toastManager from './modules/toast.js';

// ==================== 模块元数据配置 ====================
export const MODULE_CONFIG = {
  dashboard: {
    name: '仪表盘',
    shortName: 'Dash',
    icon: 'fa-tachometer-alt',
    description: '系统状态与数据概览',
  },
  settings: {
    name: '系统设置',
    shortName: 'Settings',
    icon: 'fa-cog',
    description: '全局配置、安全认证与外观主题',
  },
  openai: {
    name: 'OpenAI',
    shortName: 'OAI',
    icon: 'fa-robot',
    description: 'OpenAI 兼容 API 管理与聊天',
  },
  antigravity: {
    name: 'AntiG',
    shortName: 'AntiG',
    icon: 'fa-rocket',
    description: 'Antigravity API 代理服务',
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
    shortName: 'Aliyun',
    icon: 'fa-cloud',
    description: '阿里云 DNS / ECS 管理',
  },
  'self-h': {
    name: 'SelfH',
    shortName: 'Self-H',
    icon: 'fa-server',
    description: '自建服务管理',
  },
  tencent: {
    name: '腾讯云',
    shortName: 'Tencent',
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
    name: '2FA',
    shortName: '2FA',
    icon: 'fa-shield-alt',
    description: 'TOTP 验证器',
  },
  music: {
    name: 'Music',
    shortName: 'Music',
    icon: 'fa-music',
    description: '网易云音乐播放器',
  },
  uptime: {
    name: 'Uptime',
    shortName: 'Uptime',
    icon: 'fa-heartbeat',
    description: '站点与服务可用性监测',
  },
  filebox: {
    name: '文件柜',
    shortName: 'FileBox',
    icon: 'fa-box-open',
    description: '文件分享与暂存',
  },
  notification: {
    name: '通知',
    shortName: 'Alerts',
    icon: 'fa-bell',
    description: '通知渠道与告警规则管理',
  },
  qwen: {
    name: '通义千问',
    shortName: 'Qwen',
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
    modules: ['openai', 'gemini-cli', 'qwen', 'antigravity'],
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
      return () => ({
        'Content-Type': 'application/json',
        'x-admin-password': localStorage.getItem('admin_password') || useStore.getState().loginPassword || '',
      });
    }
    return useStore.getState()[prop];
  },
  set(target, prop, value) {
    useStore.setState({ [prop]: value });
    return true;
  }
});

export default useStore;
