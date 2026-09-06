/**
 * API Monitor - 全局状态管理 (Zustand Store)
 * 基于 Zustand 实现轻量、模块化、标准化的全局状态管理，彻底摆脱 Vue 3 依赖
 */

import { create } from 'zustand';
import { dialog } from './modules/dialog.js';
import toastManager from './modules/toast.js';
import { triggerHapticFeedback } from './modules/haptics.js';
import { setDisplayTimeZone } from './modules/utils.js';

// ==================== 模块元数据配置 ====================
export const MODULE_CONFIG = {
  dashboard: {
    name: '仪表盘',
    shortName: '总览',
    icon: 'fa-tachometer-alt',
    description: '系统概览',
  },
  settings: {
    name: '系统设置',
    shortName: '设置',
    icon: 'fa-cog',
    description: '全局偏好',
  },
  openai: {
    name: '模型网关',
    shortName: '模型',
    icon: 'fa-wand',
    description: 'OpenAI 网关',
  },
  subscription: {
    name: '订阅分发',
    shortName: '订阅',
    icon: 'fa-link',
    description: '订阅与节点',
  },

  paas: {
    name: 'PaaS',
    shortName: 'PaaS',
    icon: 'fa-rocket',
    description: 'Koyeb / Fly.io',
  },
  dns: {
    name: 'Cloudflare',
    shortName: 'CF',
    icon: 'fa-globe',
    description: 'DNS / Workers / Pages',
  },
  aliyun: {
    name: '阿里云',
    shortName: '阿里',
    icon: 'fa-cloud',
    description: 'DNS / ECS',
  },
  m365: {
    name: 'Microsoft 365',
    shortName: 'M365',
    icon: 'fa-cloud',
    description: '租户与许可证',
  },
  scheduler: {
    name: '定时任务',
    shortName: '任务',
    icon: 'fa-clock',
    description: '周期执行',
  },
  github: {
    name: 'GitHub',
    shortName: 'GitHub',
    icon: 'fa-github',
    description: '仓库与 Actions',
  },
  dockerhub: {
    name: 'Docker Hub',
    shortName: 'Docker Hub',
    icon: 'fa-docker',
    description: '账号与镜像仓库',
  },
  tencent: {
    name: '腾讯云',
    shortName: '腾讯',
    icon: 'fa-hdd',
    description: 'DNS / CVM',
  },
  oracle: {
    name: '甲骨文云',
    shortName: 'Oracle',
    icon: 'fa-cloud',
    description: 'OCI 实例',
  },
  gcp: {
    name: 'Google Cloud',
    shortName: 'GCP',
    icon: 'fa-cloud',
    description: 'GCP 资源',
  },
  huawei: {
    name: '华为云',
    shortName: 'Huawei',
    icon: 'fa-cloud',
    description: '华为云资源',
  },
  server: {
    name: '主机实例',
    shortName: '主机',
    icon: 'fa-server',
    description: '实例与终端',
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
    description: '站点状态',
  },
  filebox: {
    name: '文件柜',
    shortName: '文件',
    icon: 'fa-box-open',
    description: '文件分享',
  },
  notification: {
    name: '通知中心',
    shortName: '通知中心',
    icon: 'fa-bell',
    description: '告警与推送',
  },
  apidocs: {
    name: 'API 接口',
    shortName: '接口',
    icon: 'fa-file-code',
    description: '密钥与文档',
  },
  systemlogs: {
    name: '系统日志',
    shortName: '日志',
    icon: 'fa-file-alt',
    description: '统一日志查看器',
  },
  drawio: {
    name: '图编辑器',
    shortName: '图编辑',
    icon: 'fa-diagram-project',
    description: 'Draw.io 图文档',
  },
  prompts: {
    name: '提示词库',
    shortName: '提示词',
    icon: 'fa-message',
    description: '管理与发布',
  },
  bookmarks: {
    name: '网址导航',
    shortName: '导航',
    icon: 'fa-bookmark',
    description: '收藏与快速访问',
  },
  adminai: {
    name: '管理 AI',
    shortName: 'AI',
    icon: 'fa-robot',
    description: '智能助手',
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
    id: 'infrastructure',
    name: '云服务',
    icon: 'fa-cubes',
    modules: [],
    subgroups: [
      {
        id: 'cloud-vendors',
        name: '云厂商',
        modules: ['dns', 'aliyun', 'tencent', 'oracle', 'm365', 'gcp', 'huawei'],
      },
      {
        id: 'devops',
        name: 'DevOps',
        modules: ['github', 'dockerhub'],
      },
    ],
    trailingModules: ['server', 'paas'],
  },
  {
    id: 'toolbox',
    name: '工具箱',
    icon: 'fa-toolbox',
    modules: [],
    subgroups: [
      {
        id: 'system-tools',
        name: '系统工具',
        modules: ['scheduler', 'uptime'],
      },
      {
        id: 'utility-tools',
        name: '实用工具',
        modules: ['filebox', 'drawio', 'prompts', 'bookmarks'],
      },
    ],
    trailingModules: ['totp'],
  },
  {
    id: 'api-gateway',
    name: 'API 服务',
    icon: 'fa-bolt',
    modules: ['openai', 'subscription'],
  },
  {
    id: 'system',
    name: '系统',
    icon: 'fa-cog',
    modules: [],
    subgroups: [
      {
        id: 'global-config',
        name: '全局配置',
        modules: ['notification', 'apidocs', 'systemlogs', 'settings'],
      },
    ],
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
const UI_FONT_STORAGE_KEY = 'app_ui_font';
const UI_FONT_SIZE_STORAGE_KEY = 'app_ui_font_size';
const SIDEBAR_COLLAPSED_STORAGE_KEY = 'app_sidebar_collapsed';
const DASHBOARD_FOOTER_VISIBLE_STORAGE_KEY = 'app_dashboard_footer_visible';
const DASHBOARD_FOOTER_RECORD_NUMBER_STORAGE_KEY = 'app_dashboard_footer_record_number';
const ASKAI_OPEN_STORAGE_KEY = 'app_askai_open';
const AUTH_LOGGED_OUT_STORAGE_KEY = 'auth_explicitly_logged_out';
const AUTH_PENDING_PROVIDER_STORAGE_KEY = 'auth_pending_provider';

export function hasExplicitLogoutMarker() {
  try {
    return !!localStorage.getItem(AUTH_LOGGED_OUT_STORAGE_KEY);
  } catch {
    return false;
  }
}

export function clearExplicitLogoutMarker() {
  try {
    localStorage.removeItem(AUTH_LOGGED_OUT_STORAGE_KEY);
  } catch (error) {
    console.error('Failed to clear explicit logout marker:', error);
  }
}

export function markExplicitLogout() {
  try {
    localStorage.setItem(AUTH_LOGGED_OUT_STORAGE_KEY, Date.now().toString());
  } catch (error) {
    console.error('Failed to persist explicit logout marker:', error);
  }
}

export function getPendingAuthProvider() {
  try {
    return String(sessionStorage.getItem(AUTH_PENDING_PROVIDER_STORAGE_KEY) || '').trim();
  } catch {
    return '';
  }
}

export function setPendingAuthProvider(provider) {
  try {
    const value = String(provider || '').trim();
    if (!value) {
      sessionStorage.removeItem(AUTH_PENDING_PROVIDER_STORAGE_KEY);
      return;
    }
    sessionStorage.setItem(AUTH_PENDING_PROVIDER_STORAGE_KEY, value);
  } catch (error) {
    console.error('Failed to persist pending auth provider:', error);
  }
}

export function clearPendingAuthProvider() {
  try {
    sessionStorage.removeItem(AUTH_PENDING_PROVIDER_STORAGE_KEY);
  } catch (error) {
    console.error('Failed to clear pending auth provider:', error);
  }
}

export const THEME_MODE_OPTIONS = ['auto', 'light', 'dark'];

const normalizeThemeMode = (mode, fallback = 'auto') => (
  THEME_MODE_OPTIONS.includes(mode) ? mode : fallback
);

const normalizeSidebarCollapsed = (value, fallback = false) => {
  if (value === true || value === 'true' || value === 1 || value === '1') return true;
  if (value === false || value === 'false' || value === 0 || value === '0') return false;
  return fallback;
};

const normalizeDashboardFooterRecordNumber = (value, fallback = '') => (
  typeof value === 'string' ? value.trim().slice(0, 80) : fallback
);

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

export const getGroupModuleIds = (group) => [
  ...(group.modules || []),
  ...(group.subgroups || []).flatMap((subgroup) => subgroup.modules || []),
  ...(group.trailingModules || []),
];

export const DEFAULT_MODULE_ORDER = MODULE_GROUPS.flatMap(getGroupModuleIds);

export const DEFAULT_MODULE_VISIBILITY = DEFAULT_MODULE_ORDER.reduce((acc, moduleId) => {
  acc[moduleId] = true;
  return acc;
}, {});

DEFAULT_MODULE_VISIBILITY.drawio = false;
DEFAULT_MODULE_VISIBILITY.prompts = false;


export const DEFAULT_CHANNEL_ENABLED = {};

export const DEFAULT_CHANNEL_MODEL_PREFIX = {};

const LEGACY_MODULE_ALIASES = {
  'self-h': 'scheduler',
};

const normalizeModuleId = (moduleId) => LEGACY_MODULE_ALIASES[moduleId] || moduleId;

const getAuthHeaders = () => ({
  'Content-Type': 'application/json',
});

// Older releases persisted the administrator password in localStorage. Remove
// those legacy values immediately; authentication now uses the HttpOnly session cookie.
localStorage.removeItem('admin_password');
localStorage.removeItem('password_time');

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

export const FONT_OPTIONS = [
  { value: 'default', label: '系统默认' },
  { value: 'serif', label: '衬线字体' },
  { value: 'lxgw-wenkai-screen', label: '霞鹜文楷屏幕阅读版' },
  { value: 'sora', label: 'Sora 圆润现代' },
];

export const FONT_SIZE_OPTIONS = [
  { value: 'small', label: '小' },
  { value: 'default', label: '默认' },
  { value: 'large', label: '大' },
  { value: 'xlarge', label: '特大' },
];

export const FONT_SIZE_SCALE = { small: 0.875, default: 1, large: 1.125, xlarge: 1.25 };

const FONT_LINK_ID = 'lxgw-wenkai-font-link';
const SERIF_FONT_LINK_ID = 'noto-serif-font-link';
const SORA_FONT_LINK_ID = 'sora-font-link';
// 本地自托管：公共 CDN 在移动端网络中常加载失败导致字体静默回退系统字体
const LXGW_WENKAI_CSS_URL = '/fonts/lxgw-wenkai-screen/lxgwwenkaiscreen.css';
const NOTO_SERIF_CSS_URL = '/fonts/noto-serif-sc/notoserifsc.css';
const LORA_CSS_URL = '/fonts/lora/lora.css';
const SORA_CSS_URL = '/fonts/sora/sora.css';
const SERIF_CLASS = 'app-serif';
const SORA_CLASS = 'app-sora';

export const applyUIFont = (font) => {
  if (typeof document === 'undefined') return;

  const existing = document.getElementById(FONT_LINK_ID);
  const root = document.documentElement;

  // 除 body 继承链外，还要同步 --font-sans：.font-mono 等工具类显式引用
  // `var(--font-sans)` 不参与继承，不同步的话统计数字、代码等元素不会
  // 跟随界面字体，视觉上仍显示系统字体。

  if (font === 'default' || !font) {
    if (existing) existing.remove();
    document.getElementById(SERIF_FONT_LINK_ID)?.remove();
    root.classList.remove(SERIF_CLASS, SORA_CLASS);
    if (!document.getElementById(SORA_FONT_LINK_ID)) {
      const link = document.createElement('link');
      link.id = SORA_FONT_LINK_ID;
      link.rel = 'stylesheet';
      link.href = SORA_CSS_URL;
      document.head.appendChild(link);
    }
    const fontStack = '"Sora", "HarmonyOS Sans SC", ui-sans-serif, system-ui, sans-serif';
    if (document.body) {
      document.body.style.setProperty('font-family', fontStack);
      document.body.style.removeProperty('font-weight');
    }
    root.style.setProperty('--font-sans', fontStack);
    return;
  }

  if (font === 'lxgw-wenkai-screen') {
    document.getElementById(SERIF_FONT_LINK_ID)?.remove();
    document.getElementById(SORA_FONT_LINK_ID)?.remove();
    root.classList.remove(SERIF_CLASS, SORA_CLASS);
    if (!existing) {
      const link = document.createElement('link');
      link.id = FONT_LINK_ID;
      link.rel = 'stylesheet';
      link.href = LXGW_WENKAI_CSS_URL;
      document.head.appendChild(link);
    }
    const fontStack = '"LXGW WenKai Screen", ui-sans-serif, system-ui, sans-serif';
    if (document.body) {
      document.body.style.setProperty('font-family', fontStack);
      document.body.style.removeProperty('font-weight');
    }
    root.style.setProperty('--font-sans', fontStack);
    return;
  }

  if (existing) existing.remove();

  if (font === 'serif') {
    // 西文用 Lora Variable（400-700 可变），中文回退 Noto Serif SC，
    // 与 api.dsuk.top 的 serif 主题保持一致：正文默认字重、标题 500 + 负字距
    document.getElementById(SORA_FONT_LINK_ID)?.remove();
    root.classList.remove(SORA_CLASS);
    if (!document.getElementById(SERIF_FONT_LINK_ID)) {
      const link = document.createElement('link');
      link.id = SERIF_FONT_LINK_ID;
      link.rel = 'stylesheet';
      link.href = NOTO_SERIF_CSS_URL;
      document.head.appendChild(link);
    }
    const loraLink = document.createElement('link');
    loraLink.id = 'lora-font-link';
    loraLink.rel = 'stylesheet';
    loraLink.href = LORA_CSS_URL;
    if (!document.getElementById('lora-font-link')) document.head.appendChild(loraLink);
    const fontStack = '"Lora Variable", "Lora", "Noto Serif SC", "Songti SC", "SimSun", serif';
    if (document.body) {
      document.body.style.setProperty('font-family', fontStack);
      document.body.style.removeProperty('font-weight');
    }
    root.classList.add(SERIF_CLASS);
    root.style.setProperty('--font-sans', fontStack);
    return;
  }

  if (font === 'sora') {
    // 组合字体：西文标题 Sora + 正文 Manrope，中文 HarmonyOS Sans SC，
    // 代码 JetBrains Mono，均本地自托管（含按需汉字子集）
    document.getElementById(SERIF_FONT_LINK_ID)?.remove();
    root.classList.remove(SERIF_CLASS);
    const soraLink = document.createElement('link');
    soraLink.id = SORA_FONT_LINK_ID;
    soraLink.rel = 'stylesheet';
    soraLink.href = SORA_CSS_URL;
    if (!document.getElementById(SORA_FONT_LINK_ID)) document.head.appendChild(soraLink);
    const fontStack = '"Manrope", "HarmonyOS Sans SC", ui-sans-serif, system-ui, sans-serif';
    if (document.body) {
      document.body.style.setProperty('font-family', fontStack);
      document.body.style.removeProperty('font-weight');
    }
    root.classList.add(SORA_CLASS);
    root.style.setProperty('--font-sans', fontStack);
    return;
  }

  root.classList.remove(SERIF_CLASS, SORA_CLASS);
  document.getElementById(SERIF_FONT_LINK_ID)?.remove();
  document.getElementById(SORA_FONT_LINK_ID)?.remove();
  if (document.body) {
    document.body.style.removeProperty('font-family');
    document.body.style.removeProperty('font-weight');
  }
  root.style.removeProperty('--font-sans');
};

export const applyUIFontSize = (size) => {
  if (typeof document === 'undefined') return;

  const root = document.documentElement;
  const scale = FONT_SIZE_SCALE[size];
  if (!scale || scale === 1) {
    root.style.removeProperty('font-size');
    root.removeAttribute('data-font-size');
    return;
  }
  // 全局 rem 基准缩放：Tailwind/Kumo 的 text-*、间距等均基于 rem，
  // 调整 html 根字号即可全站统一缩放；品牌 logo/图表等固定 px 不受影响。
  root.style.fontSize = `${16 * scale}px`;
  root.dataset.fontSize = size;
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
  moduleVisibility.prompts = false;

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
    uiFont: settings.uiFont || settings.ui_font || 'default',
    themeMode: normalizeThemeMode(
      settings.themeMode || settings.theme_mode,
      typeof getInitialThemeMode === 'function' ? getInitialThemeMode() : 'auto'
    ),
    sidebarCollapsed: normalizeSidebarCollapsed(
      settings.sidebarCollapsed ?? settings.sidebar_collapsed,
      typeof getInitialSidebarCollapsed === 'function' ? getInitialSidebarCollapsed() : false
    ),
    dashboardFooterVisible: normalizeSidebarCollapsed(
      settings.dashboardFooterVisible ?? settings.dashboard_footer_visible,
      typeof getInitialDashboardFooterVisible === 'function' ? getInitialDashboardFooterVisible() : true
    ),
    dashboardFooterRecordNumber: normalizeDashboardFooterRecordNumber(
      settings.dashboardFooterRecordNumber ?? settings.dashboard_footer_record_number,
      typeof getInitialDashboardFooterRecordNumber === 'function' ? getInitialDashboardFooterRecordNumber() : ''
    ),
    siteBrandIconId: typeof settings.siteBrandIconId === 'string'
      ? settings.siteBrandIconId
      : typeof settings.site_brand_icon_id === 'string'
        ? settings.site_brand_icon_id
        : '',
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
    timezone: settings.timezone || settings.time_zone || 'system',
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

const getInitialUIFontSize = () => {
  try {
    const saved = localStorage.getItem(UI_FONT_SIZE_STORAGE_KEY);
    if (saved && FONT_SIZE_SCALE[saved]) return saved;
  } catch (e) {
    console.error('Failed to get initial ui font size:', e);
  }
  return 'default';
};

const initialUIFontSize = getInitialUIFontSize();

const getInitialSidebarCollapsed = () => {
  try {
    return normalizeSidebarCollapsed(localStorage.getItem(SIDEBAR_COLLAPSED_STORAGE_KEY), false);
  } catch (e) {
    console.error('Failed to get initial sidebar mode:', e);
  }
  return false;
};

const initialSidebarCollapsed = getInitialSidebarCollapsed();

const getInitialDashboardFooterVisible = () => {
  try {
    return normalizeSidebarCollapsed(localStorage.getItem(DASHBOARD_FOOTER_VISIBLE_STORAGE_KEY), true);
  } catch (e) {
    console.error('Failed to get dashboard footer visibility:', e);
  }
  return true;
};

const initialDashboardFooterVisible = getInitialDashboardFooterVisible();

const getInitialDashboardFooterRecordNumber = () => {
  try {
    return normalizeDashboardFooterRecordNumber(localStorage.getItem(DASHBOARD_FOOTER_RECORD_NUMBER_STORAGE_KEY));
  } catch (e) {
    console.error('Failed to get dashboard footer record number:', e);
  }
  return '';
};

const initialDashboardFooterRecordNumber = getInitialDashboardFooterRecordNumber();

const getInitialAskAIOpen = () => {
  try {
    return localStorage.getItem(ASKAI_OPEN_STORAGE_KEY) === '1';
  } catch (e) {
    console.error('Failed to get initial ask ai open state:', e);
  }
  return false;
};

const initialAskAIOpen = getInitialAskAIOpen();

const persistAskAIOpen = (open) => {
  try {
    localStorage.setItem(ASKAI_OPEN_STORAGE_KEY, open ? '1' : '0');
  } catch (e) {
    console.error('Failed to save ask ai open state:', e);
  }
};

// 网关/反向代理出错时响应体可能为空或非 JSON，response.json() 会抛引擎级
// SyntaxError；向用户展示原始英文报错没有意义，统一转成可读提示。
const readJsonSafely = async (response) => {
  try {
    return await response.json();
  } catch {
    return null;
  }
};

const toFriendlyAuthError = (error) => {
  const message = String(error?.message || '');
  if (
    message.toLowerCase().includes('failed to fetch') ||
    message.toLowerCase().includes('unexpected end of json input')
  ) {
    return '无法连接服务器，请检查网络后重试';
  }
  return message || '网络验证失败';
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
  sidebarCollapsed: initialSidebarCollapsed,
  dashboardFooterVisible: initialDashboardFooterVisible,
  dashboardFooterRecordNumber: initialDashboardFooterRecordNumber,
  siteBrandIconId: '',
  appProcessUptimeSeconds: 0,
  appProcessUptimeMeasuredAt: 0,
  themeMode: initialThemeMode,
  theme: resolveThemeMode(initialThemeMode),
  navGroupExpanded: null,
  userSettingsLoaded: false,
  userSettingsLoading: false,
  customCss: '',
  uiFont: 'default',
  uiFontSize: initialUIFontSize,
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
  timezone: 'system',
  koyebRefreshInterval: 30000,
  flyRefreshInterval: 30000,

  showAskAI: initialAskAIOpen,

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
  setDashboardFooterVisible: (visible, persist = true) => {
    const normalizedVisible = normalizeSidebarCollapsed(visible, true);
    if (persist) {
      try {
        localStorage.setItem(DASHBOARD_FOOTER_VISIBLE_STORAGE_KEY, String(normalizedVisible));
      } catch (e) {
        console.error('Failed to save dashboard footer visibility:', e);
      }
      if (get().isAuthenticated) {
        scheduleAppearanceSettingsSave({ dashboardFooterVisible: normalizedVisible });
      }
    }
    set({ dashboardFooterVisible: normalizedVisible });
  },
  setDashboardFooterRecordNumber: (recordNumber, persist = true) => {
    const normalizedRecordNumber = normalizeDashboardFooterRecordNumber(recordNumber);
    if (persist) {
      try {
        localStorage.setItem(DASHBOARD_FOOTER_RECORD_NUMBER_STORAGE_KEY, normalizedRecordNumber);
      } catch (e) {
        console.error('Failed to save dashboard footer record number:', e);
      }
      if (get().isAuthenticated) {
        scheduleAppearanceSettingsSave({ dashboardFooterRecordNumber: normalizedRecordNumber });
      }
    }
    set({ dashboardFooterRecordNumber: normalizedRecordNumber });
  },
  setAppProcessUptimeSeconds: (seconds) => {
    const normalizedSeconds = Number(seconds);
    if (!Number.isFinite(normalizedSeconds) || normalizedSeconds < 0) return;
    set({
      appProcessUptimeSeconds: normalizedSeconds,
      appProcessUptimeMeasuredAt: Date.now(),
    });
  },
  setNavGroupExpanded: (group) => set({ navGroupExpanded: group }),

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

  setUIFont: (uiFont, persist = true) => {
    const normalized = FONT_OPTIONS.some(o => o.value === uiFont) ? uiFont : 'default';
    applyUIFont(normalized);
    if (persist && get().isAuthenticated) {
      scheduleAppearanceSettingsSave({ uiFont: normalized });
    }
    try {
      localStorage.setItem(UI_FONT_STORAGE_KEY, normalized);
    } catch (error) {
      console.error('Failed to persist ui font:', error);
    }
    set({ uiFont: normalized });
  },

  setUIFontSize: (uiFontSize, persist = true) => {
    const normalized = FONT_SIZE_OPTIONS.some(o => o.value === uiFontSize) ? uiFontSize : 'default';
    applyUIFontSize(normalized);
    if (persist) {
      try {
        localStorage.setItem(UI_FONT_SIZE_STORAGE_KEY, normalized);
      } catch (error) {
        console.error('Failed to persist ui font size:', error);
      }
    }
    set({ uiFontSize: normalized });
  },

  setVibrationEnabled: (enabled, persist = true) => {
    const nextEnabled = Boolean(enabled);
    if (persist && get().isAuthenticated) {
      scheduleAppearanceSettingsSave({ vibrationEnabled: nextEnabled });
    }
    set({ vibrationEnabled: nextEnabled });
  },

  toggleAskAI: () => set((state) => {
    const next = !state.showAskAI;
    persistAskAIOpen(next);
    return { showAskAI: next };
  }),
  setShowAskAI: (show) => {
    const next = Boolean(show);
    persistAskAIOpen(next);
    set({ showAskAI: next });
  },

  triggerHaptic: (type = 'selection') => {
    if (!get().vibrationEnabled) return false;
    return triggerHapticFeedback(type);
  },

  applyUserSettings: (settings) => {
    const normalized = normalizeUserSettings(settings);
    setDisplayTimeZone(normalized.timezone);
    applyCustomCss(normalized.customCss);
    applyUIFont(normalized.uiFont);
    applyThemeMode(normalized.themeMode);
    try {
      localStorage.setItem(THEME_STORAGE_KEY, normalized.themeMode);
      localStorage.removeItem(LEGACY_THEME_STORAGE_KEY);
      localStorage.setItem(UI_FONT_STORAGE_KEY, normalized.uiFont);
      localStorage.setItem(SIDEBAR_COLLAPSED_STORAGE_KEY, String(normalized.sidebarCollapsed));
      localStorage.setItem(DASHBOARD_FOOTER_VISIBLE_STORAGE_KEY, String(normalized.dashboardFooterVisible));
      localStorage.setItem(DASHBOARD_FOOTER_RECORD_NUMBER_STORAGE_KEY, normalized.dashboardFooterRecordNumber);
    } catch (e) {
      console.error('Failed to cache appearance settings:', e);
    }
    set({
      userSettingsLoaded: true,
      themeMode: normalized.themeMode,
      theme: resolveThemeMode(normalized.themeMode),
      sidebarCollapsed: normalized.sidebarCollapsed,
      dashboardFooterVisible: normalized.dashboardFooterVisible,
      dashboardFooterRecordNumber: normalized.dashboardFooterRecordNumber,
      siteBrandIconId: normalized.siteBrandIconId,
      customCss: normalized.customCss,
      uiFont: normalized.uiFont,
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
      timezone: normalized.timezone,
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
      if (rawSettings.sidebarCollapsed === undefined && rawSettings.sidebar_collapsed === undefined) {
        appearancePatch.sidebarCollapsed = normalized.sidebarCollapsed;
      }
      if (rawSettings.dashboardFooterVisible === undefined && rawSettings.dashboard_footer_visible === undefined) {
        appearancePatch.dashboardFooterVisible = normalized.dashboardFooterVisible;
      }
      if (rawSettings.dashboardFooterRecordNumber === undefined && rawSettings.dashboard_footer_record_number === undefined) {
        appearancePatch.dashboardFooterRecordNumber = normalized.dashboardFooterRecordNumber;
      }
      if (rawSettings.vibrationEnabled === undefined) {
        appearancePatch.vibrationEnabled = normalized.vibrationEnabled;
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
      const explicitlyLoggedOut = hasExplicitLogoutMarker();

      // 1. 优先检查当前 Session 是否已认证；显式退出后不再信任残留 Cookie。
      if (!explicitlyLoggedOut) {
        const sessionRes = await fetch('/api/auth/session');
        const sessionData = await readJsonSafely(sessionRes);
        if (sessionData?.authenticated) {
          clearPendingAuthProvider();
          set({ isAuthenticated: true, showLoginModal: false, isCheckingAuth: false });
          return true;
        }
      }

      // 2. 如果 Session 不存在，再检查基本配置并尝试自动登录
      const res = await fetch('/api/auth/check-password');
      const checkData = await readJsonSafely(res);
      if (checkData === null) {
        // 后端不可达或响应非 JSON：回到登录页，避免误判为首次安装
        set({ showLoginModal: true });
        return false;
      }
      const { hasPassword, isDemoMode } = checkData;
      set({ isDemoMode });

      if (isDemoMode) {
        if (explicitlyLoggedOut) {
          set({ isAuthenticated: false, showLoginModal: true, loginPassword: '' });
          return false;
        }

        set({ loginPassword: '' });
        return await get().verifyPassword();
      }

      if (!hasPassword) {
        set({ showSetPasswordModal: true, isAuthenticated: false });
        return false;
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

      const result = await readJsonSafely(response);

      if (result === null) {
        const errorMsg = response.ok ? '服务器响应异常，请稍后重试' : '无法连接服务器，请检查网络后重试';
        set({ loginError: errorMsg });
        if (!silent) {
          toastManager.error(errorMsg);
        }
        return false;
      }

      if (response.status === 429) {
        const errorMsg = result.error || '登录过于频繁，请稍后再试';
        set({ loginError: errorMsg });
        toastManager.warning(errorMsg);
        return false;
      }

      if (result.require2FA && !result.success) {
        const isInitial2FA = !loginRequire2FA || !loginTotpToken;
        set({
          loginRequire2FA: true,
          loginTotpToken: '',
          loginError: isInitial2FA ? '' : (result.error || ''),
        });
        return false;
      }

      if (result.success) {
        set({
          isAuthenticated: true,
          showLoginModal: false,
          loginPassword: '',
          loginRequire2FA: false,
          loginTotpToken: '',
        });

        clearExplicitLogoutMarker();
        clearPendingAuthProvider();

        if (!silent) {
          toastManager.success('登录成功');
        }
        return true;
      } else {
        let errorMsg = '密码错误';
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
      const catchMsg = toFriendlyAuthError(error);
      set({ loginError: catchMsg });
      if (!silent) {
        toastManager.error(catchMsg);
      }
      return false;
    } finally {
      set({ loginLoading: false });
    }
  },

  // 处理会话过期（由 authGuard 在受保护接口返回 401 时触发）
  handleAuthExpired: () => {
    if (!get().isAuthenticated) return;
    clearPendingAuthProvider();
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
    toastManager.warning('登录已过期，请重新登录');
  },

  // 登出
  logout: async () => {
    try {
      markExplicitLogout();
      clearPendingAuthProvider();
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
      toastManager.success('已登出');
    } catch (error) {
      console.error('Logout request failed:', error);
      toastManager.warning('本地已登出，后端注销失败');
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
