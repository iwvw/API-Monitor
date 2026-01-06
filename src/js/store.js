/**
 * API Monitor - 全局状态管理 (Lightweight Store)
 * 使用 Vue 3 的 reactive API 实现简单的状态分发
 */

import { reactive } from 'vue';

// 确保 reactive 始终从全局 Vue 对象获取，增加鲁棒性
// 纯 Vue 3 (ESM) 不需要 window.Vue 检测，直接使用导入的 reactive

/**
 * 模块配置 - 统一管理所有模块的元数据
 * @type {Object<string, {name: string, shortName: string, icon: string, description: string}>}
 */
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
    description: 'Zeabur / Koyeb / Fly.io 平台监控',
  },
  dns: {
    name: 'Cloudflare',
    shortName: 'CF',
    icon: 'fa-globe',
    description: 'Cloudflare DNS / Workers / Pages 管理',
  },
  'self-h': {
    name: 'SelfH',
    shortName: 'Self-H',
    icon: 'fa-server',
    description: '自建服务管理',
  },
  server: {
    name: 'Hosts',
    shortName: 'Hosts',
    icon: 'fa-hdd',
    description: '终端与服务器监控',
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
};

/**
 * 模块分组配置 - 将模块按功能分类
 * @type {Array<{id: string, name: string, icon: string, modules: string[]}>}
 */
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
    modules: ['openai', 'antigravity', 'gemini-cli', 'ai-chat'],
  },
  {
    id: 'infrastructure',
    name: '基础设施',
    icon: 'fa-cubes',
    modules: ['paas', 'dns', 'server', 'uptime'],
  },
  {
    id: 'toolbox',
    name: '工具箱',
    icon: 'fa-toolbox',
    modules: ['self-h', 'totp', 'music'],
  },
];

/**
 * 获取模块名称
 * @param {string} moduleId - 模块 ID
 * @param {boolean} short - 是否返回简短名称
 * @returns {string}
 */
export function getModuleName(moduleId, short = false) {
  const config = MODULE_CONFIG[moduleId];
  if (!config) return moduleId;
  return short ? config.shortName : config.name;
}

/**
 * 获取模块图标
 * @param {string} moduleId - 模块 ID
 * @returns {string} FontAwesome 图标类名
 */
export function getModuleIcon(moduleId) {
  const config = MODULE_CONFIG[moduleId];
  return config ? config.icon : 'fa-cube';
}

export const store = reactive({
  // 认证与基础状态
  isAuthenticated: false,
  isCheckingAuth: true,
  showLoginModal: false,
  showSetPasswordModal: false,
  loginError: '',
  loginPassword: '',
  setPassword: '',
  setPasswordConfirm: '',
  setPasswordError: '',

  // 导航与布局
  mainActiveTab: 'dashboard',
  moduleVisibility: {
    dashboard: true,
    openai: true,
    antigravity: true,
    'gemini-cli': true,
    paas: true,
    dns: true,
    'self-h': true,
    server: true,
    totp: true,
    music: false, // 音乐模块默认隐藏
    uptime: true,
  },
  channelEnabled: {
    antigravity: true,
    'gemini-cli': true,
  },
  channelModelPrefix: {
    antigravity: '',
    'gemini-cli': '',
  },
  moduleOrder: [
    'dashboard',
    'openai',
    'antigravity',
    'gemini-cli',
    'paas',
    'dns',
    'self-h',
    'server',
    'totp',
    'music',
    'uptime',
  ],

  // 界面设置
  opacity: 100,
  serverIpDisplayMode: 'normal', // 'normal', 'masked', 'hidden'
  vibrationEnabled: true, // 移动端震动反馈开关
  navLayout: 'bottom', // 'sidebar' (左侧), 'top' (顶部) or 'bottom' (底栏)
  agentDownloadUrl: '', // 自定义 Agent 下载地址，空则使用主控端
  showSettingsModal: false, // 设置面板显示状态
  appRevealed: false, // 首屏淡入动画完成
  appReady: false, // 首屏加载完成，可以启用过渡动画
  singlePageMode: false, // 单页模式（通过 URL 路径访问特定模块时隐藏导航）
  mobileSettingsNavExpanded: false, // 移动端设置导航展开状态
  navGroupExpanded: null, // 当前展开的分组 ID (null 表示都收起)

  // TOTP 模块设置
  totpSettings: {
    hideCode: false, // 隐藏验证码
    allowRevealCode: false, // 允许临时显示
    groupByPlatform: true, // 按平台分组显示
    showPlatformHeaders: false, // 显示平台分隔标题
    hidePlatformText: false, // 隐藏标题文字
    maskAccount: false, // 账号名称打码
    autoSave: true, // 自动保存账号
    lockInputMode: false, // 锁定录入方式
    defaultInputMode: 'qr', // 默认录入模式
  },

  // Self-H (Self-Hosted) module state
  selfHCurrentTab: 'openlist',
  openListSubTab: 'overview',

  // Cron Scheduler (Added)
  cronTasks: [],
  cronLogs: [],
  cronEditingTask: null, // 当前编辑的任务对象 { id, name, schedule, command, type, enabled } 或 null
  cronLoading: false,

  openListAccounts: [],
  openListStats: { onlineCount: 0 },
  currentOpenListAccount: null,
  newOpenListAcc: { name: '', api_url: '', api_token: '' },
  openListStorages: [], // 存储挂载点详细信息
  openListFiles: [],
  openListFilesLoading: false,
  openListPath: '/',
  openListReadme: '',
  openListSortKey: null, // null 表示跟随后端原始排序
  openListSortOrder: 'asc', // 'asc', 'desc'
  openListFileCache: {}, // 路径 -> 文件列表缓存
  openListPreviewSize: 800, // 预览图最大尺寸
  openListSearchScope: 0, // 0: 全部, 1: 文件夹, 2: 文件
  openListSearchActive: false, // 是否处于搜索激活状态 (主列表)
  openListSearchInput: '', // 搜索输入框内容
  openListSearchExpanded: false, // 搜索框展开状态
  // 临时标签页状态 (支持多个)
  openListTempTabs: [], // 数组项: { id, name, path, files: [], loading: false, pathParts: [] }
  openListActiveTempTabId: null,
  // 右键菜单状态
  openListContextMenu: {
    visible: false,
    x: 0,
    y: 0,
    file: null,
    baseDir: '/',
  },
  // 交互辅助状态
  openListInteraction: {
    lastTapTime: 0,
    lastTapTabId: null,
    longPressTimer: null,
    longPressTriggered: false,
  },
  openListLayoutMode: 'list', // 'list' | 'grid'

  // 图片预览弹窗状态
  imagePreview: {
    visible: false,
    url: '',
    filename: '',
    loading: false,
  },

  // 全局数据刷新控制
  serverList: [],
  serverLoading: false,
  serverCurrentTab: 'list',
  serverPollingEnabled: true,
  serverPollingTimer: null,
  serverCountdownInterval: null,
  serverRefreshCountdown: 0,
  serverRefreshProgress: 100,
  serverSearchText: '',
  serverStatusFilter: 'all',
  expandedServers: [],
  monitorConfig: {
    interval: 60,
    timeout: 10,
    logRetentionDays: 7,
    metrics_retention_days: 30,
  },
  // Agent 部署弹窗
  showAgentModal: false,
  agentModalData: null,
  agentInstallLoading: false,
  agentInstallLog: '', // 安装日志输出
  agentInstallResult: null, // 'success' | 'error' | null
  agentInstallOS: 'linux', // 'linux' | 'windows'
  agentInstallProtocol: window.location.protocol.replace(':', ''), // 默认为当前页面协议 (http 或 https)
  agentInstallHostType: 'domain', // 'domain' | 'ip'

  // 批量 Agent 部署
  showBatchAgentModal: false,
  selectedBatchServers: [],
  batchInstallResults: [],

  // Agent 升级相关
  showUpgradeModal: false,
  upgradeLog: '',
  upgradeProgress: 0,
  upgrading: false,
  forceUpgrade: false,
  upgradeFallbackSsh: false, // 是否使用 SSH 覆盖安装作为保底策略

  // 快速部署模式
  serverAddMode: 'ssh', // 'ssh' | 'agent'
  quickDeployName: '', // 快速部署输入的服务器名称
  quickDeployResult: null, // 快速部署生成的结果 { serverId, serverName, isNew, installCommand }

  serverCredentials: [],
  showSSHQuickMenu: false, // SSH 快速连接下拉菜单

  // PaaS 平台监控 (Zeabur + Koyeb + ...)
  paasCurrentPlatform: 'zeabur', // 'zeabur', 'koyeb', etc.
  paasCurrentTab: 'zeabur', // 'zeabur', 'koyeb', 'accounts', 'settings'

  // Zeabur 子模块
  accounts: [],
  managedAccounts: [],
  zeaburCurrentTab: 'monitor',
  loading: false, // Zeabur loading
  zeaburRefreshInterval: 30000,
  zeaburRefreshIntervalSec: 30, // 秒（用于表单绑定）
  refreshCountdown: 30,
  refreshProgress: 30,
  dataRefreshPaused: false,
  projectCosts: {},

  // Koyeb 子模块
  koyebAccounts: [],
  koyebManagedAccounts: [],
  koyebLoading: false,
  koyebRefreshing: false,
  koyebLastUpdate: '',
  koyebRefreshInterval: 30000,
  koyebRefreshIntervalSec: 30, // 秒（用于表单绑定）
  koyebRefreshCountdown: 30,
  koyebRefreshProgress: 30,
  koyebDataRefreshPaused: false,
  koyebExpandedAccounts: {},
  showAddKoyebAccountModal: false,
  newKoyebAccount: { name: '', token: '' },
  koyebAddingAccount: false,
  koyebAddAccountError: '',
  koyebAddAccountSuccess: '',
  koyebBatchAccounts: '',
  koyebBatchAddError: '',
  koyebBatchAddSuccess: '',

  // Fly.io 子模块
  flyAccounts: [],
  flyManagedAccounts: [],
  flyLoading: false,
  flyRefreshing: false,
  flyLastUpdate: '',
  flyRefreshInterval: 30000,
  flyRefreshIntervalSec: 30, // 秒（用于表单绑定）
  flyRefreshCountdown: 300,
  flyRefreshProgress: 300,
  flyDataRefreshPaused: false,
  flyExpandedAccounts: {},
  showAddFlyAccountModal: false,
  newFlyAccount: { name: '', token: '' },
  flyAddingAccount: false,
  flyAddAccountError: '',
  flyAddAccountSuccess: '',
  flyBatchAccounts: '',
  flyBatchAddError: '',
  flyBatchAddSuccess: '',

  // DNS
  dnsAccounts: [],
  dnsZones: [],
  dnsCurrentTab: 'dns',
  computeCurrentTab: 'workers',
  dnsSelectedAccountId: null,
  dnsSelectedZoneId: null,
  dnsSelectedZoneName: '', // 当前选中的域名名称
  dnsSelectedZoneNameServers: [], // 存储当前域名的 NS 记录
  dnsSelectedZoneSsl: null, // 当前域名的SSL/TLS信息
  dnsSelectedZoneAnalytics: null, // 当前域名的Analytics数据
  showNsPopover: false, // 控制 NS 记录弹出层的显示
  dnsLoadingZones: false,
  dnsLoadingRecords: false,
  dnsLoadingSsl: false, // SSL信息加载中
  showSslModal: false, // SSL 模式选择弹窗
  dnsLoadingAnalytics: false, // Analytics加载中
  dnsAnalyticsTimeRange: '24h', // Analytics时间范围
  dnsPurgingCache: false, // 缓存清除中状态
  dnsRecords: [],
  dnsSelectedRecords: [],
  dnsSearchText: '',
  isEditingWorker: false, // 是否正在编辑现有 Worker

  // R2 存储
  r2Buckets: [],
  r2Objects: [],
  r2SelectedBucketName: null,
  r2LoadingBuckets: false,
  r2LoadingObjects: false,
  r2PrefixStack: [], // 用于对象浏览的路径栈
  r2CurrentPrefix: '', // 当前浏览的前缀
  r2SearchText: '',
  showR2CreateBucketModal: false,
  newR2BucketName: '',
  r2Uploading: false,
  r2DeletingBucket: false,
  r2DeletingObject: false,
  // R2 自定义域名模态框
  showR2DomainModal: false,
  r2CustomDomainInput: '',
  r2PendingDownloadObj: null, // 等待下载的对象
  r2SelectedObjects: [], // 已选中的对象 keys

  // Cloudflare Tunnel
  tunnels: [],
  tunnelsLoading: false,
  showCreateTunnelModal: false,
  newTunnelName: '',
  tunnelSaving: false,
  // Tunnel Token
  showTunnelTokenModal: false,
  selectedTunnelToken: '',
  selectedTunnelForToken: null,
  // Tunnel Config (Ingress)
  showTunnelConfigModal: false,
  selectedTunnelForConfig: null,
  tunnelConfig: { ingress: [] },
  tunnelConfigLoading: false,
  tunnelConfigSaving: false,
  // Tunnel Connections
  showTunnelConnectionsModal: false,
  selectedTunnelForConnections: null,
  tunnelConnections: [],
  tunnelConnectionsLoading: false,

  // OpenAI
  openaiEndpoints: [],
  openaiCurrentTab: 'endpoints',
  openaiLoading: false,
  openaiRefreshing: false,
  showOpenaiEndpointModal: false,
  showOpenaiEndpointsList: false,
  showHChatSettingsModal: false,

  // OpenAI Chat
  openaiChatMessages: [],
  openaiChatAttachments: [], // 当前待发送的附件
  openaiChatModel: localStorage.getItem('openai_default_model') || '',
  openaiChatEndpoint: localStorage.getItem('openai_chat_endpoint') || '', // 当前选中的对话端点
  openaiDefaultChatModel: localStorage.getItem('openai_default_model') || '',
  openaiChatSystemPrompt: localStorage.getItem('openai_system_prompt') || '你是一个有用的 AI 助手。',
  openaiChatMessageInput: '',
  openaiChatLoading: false,
  openaiAllModels: [],
  openaiModelSearch: '',
  dropdownModelSearch: '', // 下拉框内部的搜索文本
  openaiShowEndpointDropdown: false, // 是否显示端点选择下拉框
  openaiShowModelDropdown: false, // 是否显示模型选择下拉框
  openaiChatSettings: (() => {
    try {
      const saved = localStorage.getItem('openai_chat_settings');
      return saved ? JSON.parse(saved) : {
        temperature: 0.7,
        top_p: 1,
        max_tokens: 2000,
        presence_penalty: 0,
        frequency_penalty: 0,
      };
    } catch {
      return {
        temperature: 0.7,
        top_p: 1,
        max_tokens: 2000,
        presence_penalty: 0,
        frequency_penalty: 0,
      };
    }
  })(),

  // Personas (人设系统) - 从后端加载
  openaiPersonas: [],
  openaiCurrentPersonaId: null,
  showPersonaModal: false,
  editingPersona: null,
  personaForm: { name: '', systemPrompt: '', icon: 'fa-robot' },
  showPersonaDropdown: false,

  // Chat History
  openaiChatSessions: [],           // 所有会话列表
  openaiChatCurrentSessionId: null, // 当前会话 ID
  openaiChatHistoryLoading: false,  // 加载状态
  openaiChatHistoryCollapsed: true, // 侧边栏折叠状态 (默认隐藏)
  openaiChatMobileSidebarOpen: false, // 移动端侧边栏是否打开
  openaiChatSelectedSessionIds: [], // 选中的会话 ID（批量删除使用）
  openaiChatAutoScroll: true,        // 自动滚动开关（用户向上滚动时自动关闭）
  openaiChatLastMessageCount: 0,     // 上一次消息数量（用于检测新消息）

  // Model Management
  openaiSettingsTab: 'general',     // 设置弹窗当前标签页: general, models, endpoints
  openaiPinnedModels: (() => { try { return JSON.parse(localStorage.getItem('openai_pinned_models')) || []; } catch { return []; } })(),
  openaiHiddenModels: (() => { try { return JSON.parse(localStorage.getItem('openai_hidden_models')) || []; } catch { return []; } })(),
  openaiModelPresets: (() => { try { return JSON.parse(localStorage.getItem('openai_model_presets')) || {}; } catch { return {}; } })(),
  openaiShowHiddenModels: false,    // 是否显示隐藏的模型
  openaiModelHealth: {},            // 模型健康状态: { modelId: { status: 'healthy'|'unhealthy'|'unknown', loading: false, latency: 0 } }
  openaiModelHealthBatchLoading: false, // 批量检测加载状态
  openaiHealthCheckModal: false,    // 健康检测弹窗显示
  openaiHealthCheckForm: {          // 健康检测表单
    useKey: 'single',               // single: 单端点, all: 所有端点
    concurrency: false,             // 是否开启并发检测
    timeout: 15                     // 超时时间(s)
  },

  // 自动标题生成配置
  openaiAutoTitleEnabled: (() => {
    const saved = localStorage.getItem('openai_auto_title_enabled');
    return saved !== null ? saved === 'true' : true; // 默认启用
  })(),
  openaiTitleModels: (() => {
    try {
      return JSON.parse(localStorage.getItem('openai_title_models')) || [];
    } catch { return []; }
  })(), // 标题生成使用的模型列表（支持容灾）
  openaiTitleModelToAdd: '', // 待添加的标题模型（下拉框绑定）
  openaiTitleGenerating: false, // 标题生成中状态
  openaiTitleLastResult: null, // 上次生成结果 { success, model, title, error }

  openaiSelectedEndpointId: '',     // 当前选择的端点 ID (用于筛选模型)

  // Antigravity
  antigravityAccounts: [],
  antigravityCurrentTab: 'quotas',
  antigravityQuotaSelectedAccountId: '',
  antigravityLoading: false,
  antigravitySaving: false,
  antigravityQuotaLoading: false,
  antigravityQuotas: {},
  antigravityQuotasLastUpdated: '',
  antigravityStats: null,
  antigravityLogs: [],
  antigravityLogFilterAccount: '',
  antigravityLogFilterModel: '',
  antigravitySettings: [],
  agShowApiKey: false,
  agRefreshingAll: false,
  agLogDetailShowRaw: false, // 是否显示原始 JSON
  antigravityLogDetail: null, // 移入 store
  currentTime: Date.now(), // 全局当前时间，用于驱动倒计时
  antigravityQuotaViewMode: 'list',
  antigravityModelRedirects: [],
  antigravityMatrix: null,
  newRedirectSource: '',
  newRedirectTarget: '',
  agEditingRedirectSource: null,
  antigravityCheckLoading: false, // 模型检测中
  antigravityChecking: false, // 正在执行检测
  antigravityCheckHistory: { models: [], times: [], matrix: {} }, // 检测历史矩阵
  antigravityAutoCheck: false, // 定时检测开关 (UI 状态)
  antigravityAutoCheckInterval: 3600000, // 默认 1 小时
  antigravityAutoCheckStatus: null, // 后端定时器状态 { running, enabled, intervalMs, nextRunTime }
  antigravityDisabledCheckModels: [], // 禁用检测的模型列表

  // Gemini CLI
  geminiCliAccounts: [],
  geminiCliCurrentTab: 'models',
  geminiCliLoading: false,
  geminiCliSaving: false,
  showGeminiCliAccountModal: false,
  showGeminiCliLogDetailModal: false, // 移入 store
  geminiCliLogDetail: null, // 移入 store
  showGeminiCliOAuthExpand: false,
  geminiCliOAuthUrl: '',
  geminiCliOauthReturnUrl: '',
  // 使用 Antigravity 的 Client ID 以获得 API 访问权限
  geminiCliCustomClientId:
    '1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com',
  geminiCliCustomClientSecret: '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',
  geminiCliOAuthRedirectUri: 'http://localhost:11451',
  geminiCliCustomProjectId: '',
  geminiCliAllowRandomProjectId: true,
  geminiCliStats: null,
  geminiCliLogs: [],
  geminiCliSettings: {},
  geminiCliEditingAccount: null,
  geminiCliModels: {},
  geminiCliMatrix: null, // Stores the model matrix configuration
  geminiCliModelLoading: false,
  geminiCliModelRedirects: [], // Stores model redirect rules
  newGeminiCliRedirectSource: '',
  newGeminiCliRedirectTarget: '',
  gcliEditingRedirectSource: null,
  geminiCliModelSelectedAccountId: '',
  geminiCliLogFilterAccount: '',
  geminiCliLogFilterModel: '',
  gcliLogDetailShowRaw: false, // 是否显示原始 JSON (Gemini CLI)
  geminiCliCheckLoading: false, // 模型检测中
  geminiCliChecking: false, // 正在执行检测
  geminiCliCheckHistory: { models: [], times: [], matrix: {} }, // 检测历史矩阵
  geminiCliAutoCheck: false, // 定时检测开关 (UI 状态)
  geminiCliAutoCheckInterval: 3600000, // 默认 1 小时
  geminiCliAutoCheckStatus: null, // 后端定时器状态 { running, enabled, intervalMs, nextRunTime }
  geminiCliDisabledCheckModels: [], // 禁用检测的模型列表

  // ===== 音乐播放器模块 =====
  musicReady: false,
  musicCurrentTab: 'home', // 'home', 'discover', 'search', 'library', 'settings'
  musicSearchKeyword: '',
  musicSearchLoading: false,
  musicSearchResults: [], // 歌曲搜索结果
  musicSearchPlaylists: [], // 歌单搜索结果
  musicSearchArtists: [], // 歌手搜索结果
  musicSearchType: 'songs', // 'songs', 'playlists', 'artists'
  musicSearchOffset: 0, // 分页偏移
  musicSearchHasMore: true, // 是否有更多结果
  musicSearchLoadingMore: false, // 加载更多中
  musicShowSearchTab: false, // 是否显示搜索标签
  musicPlaying: false,
  musicBuffering: false,
  musicWidgetLoading: false, // 仪表盘音乐卡片加载状态
  musicCurrentSong: null,
  musicPlaylist: [],
  musicCurrentIndex: -1,
  musicVolume: 80,
  musicMuted: false,
  musicRepeatMode: 'none', // 'none', 'all', 'one'
  musicShuffleEnabled: false,
  musicIsDragging: false, // 进度条拖动状态
  musicCurrentTime: 0,
  musicDuration: 0,
  musicProgress: 0,
  musicLyrics: [],
  musicLyricsTranslation: [],
  musicCurrentLyricIndex: 0,
  musicCurrentLyricText: '', // 当前主歌词文字
  musicCurrentLyricTranslation: '', // 当前翻译歌词文字
  musicNextLyricText: '', // 下一句主歌词
  musicNextLyricTranslation: '', // 下一句翻译
  musicCurrentLyricPercent: 0, // 当前行播放进度 (0-100)
  musicShowFullPlayer: false,
  musicShowPlaylistDrawer: false,
  musicDailyRecommend: [],
  musicRecommendLoading: false,
  musicHotPlaylists: [],
  musicPlaylistsLoading: false,
  musicCurrentPlaylistDetail: null,
  musicPlaylistDetailLoading: false,
  musicApiUrl: '', // NCM API 地址，空则使用内置
  musicUnblockUrl: '', // 解锁服务地址，空则使用内置
  musicQuality: 'exhigh', // 'standard', 'higher', 'exhigh', 'lossless'
  musicAutoPlay: true,
  mfpLyricsMode: false,
  mfpPlaylistMode: false,

  // 登录相关
  musicUser: null, // { userId, nickname, avatarUrl, vipType }
  musicLoginLoading: false,
  musicShowLoginModal: false, // 显示网易云登录弹窗
  musicLoginStatusText: '请使用网易云音乐 App 扫码登录', // 登录状态文字
  musicQrKey: '',
  musicQrImg: '',
  musicQrExpired: false,
  musicQrChecking: false,
  musicMyPlaylists: [], // 用户创建的歌单
  musicCollectedPlaylists: [], // 用户收藏的歌单
  musicLikedPlaylist: null, // 喜欢的音乐歌单
  musicShowDetail: false, // 显示歌单详情页
  musicPlaylistVisibleCount: 50, // 歌单懒加载：当前可见的歌曲数量
  musicShowUserDropdown: false, // 显示用户下拉菜单

  // 流媒体播放器 (Stream Player)
  streamPlayer: {
    visible: false,
    loading: false,
    playing: false,
    currentTime: 0,
    duration: 0,
    buffered: 0,
    volume: 1,
    muted: false,
    playbackRate: 1,
    fullscreen: false,
    filename: '',
    url: '',

    // 音频警告
    audioWarning: false,
    audioWarningMessage: '',

    // 不支持格式对话框
    showUnsupportedDialog: false,
    unsupportedFormat: '',
    unsupportedUrl: '',
    unsupportedFilename: '',

    // 播放器 UI 交互
    showControls: true,
    controlsTimer: null,
    hideTimer: null,
    isLongPressing: false,
    lastTapTime: 0,
    animationType: null,
    animationText: '',
    bufferedTime: 0,
    isDragging: false,
    dragTime: 0,
    webFullscreen: false,

    // 播放速度选项
    playbackRates: [0.5, 0.75, 1, 1.25, 1.5, 1.75, 2],
  },

  // 统一日志查看器 (Log Viewer)
  logViewer: {
    visible: false,
    title: '日志查看器',
    subtitle: '',
    logs: [], // { id, timestamp, level, message, raw }
    loading: false,
    autoScroll: true,
    filterText: '',
    levelFilter: 'ALL', // ALL, INFO, WARN, ERROR
    wrapText: true,
    fullscreen: false,
    source: null, // 'zeabur', 'koyeb', 'system', etc.
    streamActive: false, // 是否正在接收实时流
  },

  // 系统日志流 (System Log Stream)
  systemLogs: [],
  logWs: null,
  logWsConnected: false,
  logWsConnecting: false,
  logWsAutoReconnect: false,
  autoScrollLogs: true,
  logFileSize: '',
  logFileInfo: null,

  // 实时指标流 (Real-time Metrics Stream)
  metricsWs: null,
  metricsWsConnected: false,
  metricsWsConnecting: false,

  // 自定义对话框状态
  customDialog: {
    show: false,
    title: '',
    message: '',
    icon: '',
    confirmText: '',
    cancelText: '',
    deleteText: '', // 新增删除按钮文本
    confirmClass: '',
    isPrompt: false,
    promptValue: '',
    placeholder: '',
    onConfirm: null,
    onCancel: null,
    onDelete: null, // 新增删除回调
  },

  // 常用工具方法
  getAuthHeaders() {
    return {
      'Content-Type': 'application/json',
      'x-admin-password': this.loginPassword,
    };
  },

  showAlert(message, title = '提示', icon = 'fa-info-circle', isHtml = false) {
    return new Promise(resolve => {
      this.customDialog = {
        show: true,
        title: title,
        message: message,
        icon: icon,
        confirmText: '确定',
        cancelText: '',
        deleteText: '',
        confirmClass: 'btn-primary',
        isHtml: isHtml,
        onConfirm: () => {
          this.customDialog.show = false;
          resolve(true);
        },
        onCancel: null,
        onDelete: null,
      };
    });
  },

  showConfirm(options) {
    return new Promise(resolve => {
      this.customDialog = {
        show: true,
        title: options.title || '确认',
        message: options.message || '',
        icon: options.icon || 'fa-question-circle',
        confirmText: options.confirmText || '确定',
        cancelText: options.cancelText || '取消',
        deleteText: options.deleteText || '',
        confirmClass: options.confirmClass || 'btn-primary',
        onConfirm: () => {
          this.customDialog.show = false;
          resolve(true);
        },
        onCancel: () => {
          this.customDialog.show = false;
          resolve(false);
        },
        onDelete: options.onDelete || null,
      };
    });
  },

  showPrompt(options) {
    return new Promise(resolve => {
      this.customDialog = {
        show: true,
        title: options.title || '输入',
        message: options.message || '',
        icon: options.icon || 'fa-edit',
        confirmText: options.confirmText || '确定',
        cancelText: options.cancelText || '取消',
        deleteText: options.deleteText || '',
        isPrompt: true,
        promptValue: options.promptValue || '',
        placeholder: options.placeholder || '',
        confirmClass: options.confirmClass || 'btn-primary',
        onConfirm: () => {
          const val = this.customDialog.promptValue;
          this.customDialog.show = false;
          resolve({ action: 'confirm', value: val });
        },
        onCancel: () => {
          this.customDialog.show = false;
          resolve({ action: 'cancel' });
        },
        onDelete: () => {
          this.customDialog.show = false;
          resolve({ action: 'delete' });
        },
      };
    });
  },
});
