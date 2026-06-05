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
  'ai-chat': {
    name: 'AI Chat',
    shortName: 'Chat',
    icon: 'fa-comments',
    description: 'AI 对话助手',
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
    modules: ['self-h', 'totp', 'music', 'uptime', 'filebox', 'notification', 'ai-chat'],
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

export const applyThemeMode = (theme) => {
  if (typeof document === 'undefined') return;

  const root = document.documentElement;
  root.classList.toggle('dark', theme === 'dark');
  root.classList.toggle('light', theme === 'light');
  root.dataset.mode = theme;
};

// ==================== Zustand Store ====================
const getInitialTheme = () => {
  try {
    const saved = localStorage.getItem('app_theme');
    if (saved === 'dark' || saved === 'light') return saved;
    if (typeof window !== 'undefined' && window.matchMedia) {
      return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }
  } catch (e) {
    console.error('Failed to get initial theme:', e);
  }
  return 'dark';
};

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
  theme: getInitialTheme(),
  navGroupExpanded: null,
  
  // --- 3. 页面数据占位 ---
  serverList: [],
  dnsZones: [],
  dnsRecords: [],

  // --- 4. 修改状态的方法 ---
  setMainActiveTab: (tab) => set({ mainActiveTab: tab }),
  setSidebarCollapsed: (collapsed) => set({ sidebarCollapsed: collapsed }),
  setNavGroupExpanded: (group) => set({ navGroupExpanded: group }),
  
  setTheme: (theme, persist = true) => {
    if (persist) {
      try {
        localStorage.setItem('app_theme', theme);
      } catch (e) {
        console.error('Failed to save theme:', e);
      }
    }
    applyThemeMode(theme);
    set({ theme });
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

export default useStore;
