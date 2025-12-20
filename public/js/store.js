/**
 * API Monitor - 全局状态管理 (Lightweight Store)
 * 使用 Vue 3 的 reactive API 实现简单的状态分发
 */

// 确保 reactive 始终从全局 Vue 对象获取，增加鲁棒性
const reactive = (obj) => {
    if (window.Vue && window.Vue.reactive) {
        return window.Vue.reactive(obj);
    }
    // 回退方案：如果此时 Vue 还没加载（在 module script 中理论上不应该发生），
    // 则返回一个普通对象，并在之后通过某种方式使其响应式，
    // 但最简单的是确保调用时 Vue 已存在。
    console.warn('Vue not yet loaded when store was initialized, using plain object');
    return obj;
};

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
    mainActiveTab: 'openai',
    moduleVisibility: {
        openai: true,
        antigravity: true,
        'gemini-cli': true,
        zeabur: true,
        dns: true,
        server: true
    },
    channelEnabled: {
        antigravity: true,
        'gemini-cli': true
    },
    channelModelPrefix: {
        antigravity: '',
        'gemini-cli': ''
    },
    moduleOrder: ['openai', 'antigravity', 'gemini-cli', 'zeabur', 'dns', 'server'],
    mainTabsLayout: 'top', // 'top' = 顶部横向, 'sidebar' = 左侧竖向(仅图标)

    // 全局设置
    opacity: 39,
    serverIpDisplayMode: 'normal',
    showSettingsModal: false,
    showLogsModal: false,
    logsAutoScroll: true,
    logsRealTime: false,

    // 服务器管理 (Server)
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
    expandedServers: new Set(),
    monitorConfig: {
        interval: 60,
        timeout: 10,
        logRetentionDays: 7
    },
    serverCredentials: [],



    // Zeabur
    accounts: [],
    managedAccounts: [],
    zeaburCurrentTab: 'monitor',
    loading: false, // Zeabur loading
    zeaburRefreshInterval: 30000,
    refreshCountdown: 30,
    refreshProgress: 100,
    dataRefreshPaused: false,
    projectCosts: {},

    // DNS
    dnsAccounts: [],
    dnsZones: [],
    dnsCurrentTab: 'dns',
    dnsSelectedAccountId: null,
    dnsSelectedZoneId: null,
    dnsLoadingZones: false,
    dnsLoadingRecords: false,
    dnsRecords: [],
    dnsSelectedRecords: [],
    dnsSearchText: '',

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
    openaiChatModel: '',
    openaiChatSystemPrompt: '你是一个有用的 AI 助手。',
    openaiChatMessageInput: '',
    openaiChatLoading: false,
    openaiAllModels: [],
    openaiModelSearch: '',
    openaiChatSettings: {
        temperature: 0.7,
        top_p: 1,
        max_tokens: 2000,
        presence_penalty: 0,
        frequency_penalty: 0
    },

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
    antigravitySettings: [],
    agRefreshingAll: false,
    antigravityQuotaViewMode: 'list',
    antigravityModelRedirects: [],
    newRedirectSource: '',
    newRedirectTarget: '',
    agEditingRedirectSource: null,

    // Gemini CLI
    geminiCliAccounts: [],
    geminiCliCurrentTab: 'models',
    geminiCliLoading: false,
    geminiCliSaving: false,
    showGeminiCliAccountModal: false,
    showGeminiCliOAuthExpand: false,
    geminiCliOAuthUrl: '',
    geminiCliOauthReturnUrl: '',
    // 使用 Antigravity 的 Client ID 以获得 API 访问权限
    geminiCliCustomClientId: '1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com',
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

    // NextChat 模块
    nextchat: {
        sessions: [],
        currentSessionId: null,
        messages: [],
        inputText: '',
        isStreaming: false,
        selectedModel: 'gemini-2.5-flash',
        availableModels: [
            { id: 'gemini-2.5-flash', name: 'Gemini 2.5 Flash', provider: 'gemini' },
            { id: 'gemini-2.5-pro', name: 'Gemini 2.5 Pro', provider: 'gemini' },
            { id: 'gemini-2.0-flash', name: 'Gemini 2.0 Flash', provider: 'gemini' },
            { id: 'gpt-4o', name: 'GPT-4o', provider: 'openai' },
            { id: 'gpt-4o-mini', name: 'GPT-4o Mini', provider: 'openai' },
            { id: 'claude-3-5-sonnet', name: 'Claude 3.5 Sonnet', provider: 'anthropic' }
        ],
        isLoading: false
    },

    // 自定义对话框状态
    customDialog: {
        show: false,
        title: '',
        message: '',
        icon: '',
        confirmText: '',
        cancelText: '',
        confirmClass: '',
        isPrompt: false,
        promptValue: '',
        placeholder: '',
        onConfirm: null,
        onCancel: null
    },

    // 常用工具方法
    getAuthHeaders() {
        return {
            'Content-Type': 'application/json',
            'x-admin-password': this.loginPassword
        };
    },

    showAlert(message, title = '提示', icon = 'fa-info-circle') {
        return new Promise((resolve) => {
            this.customDialog = {
                show: true,
                title: title,
                message: message,
                icon: icon,
                confirmText: '确定',
                cancelText: '',
                confirmClass: 'btn-primary',
                onConfirm: () => {
                    this.customDialog.show = false;
                    resolve(true);
                },
                onCancel: null
            };
        });
    },

    showConfirm(options) {
        return new Promise((resolve) => {
            this.customDialog = {
                show: true,
                title: options.title || '确认',
                message: options.message || '',
                icon: options.icon || 'fa-question-circle',
                confirmText: options.confirmText || '确定',
                cancelText: options.cancelText || '取消',
                confirmClass: options.confirmClass || 'btn-primary',
                onConfirm: () => {
                    this.customDialog.show = false;
                    resolve(true);
                },
                onCancel: () => {
                    this.customDialog.show = false;
                    resolve(false);
                }
            };
        });
    }
});
