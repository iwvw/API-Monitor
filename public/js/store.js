/**
 * API Monitor - 全局状态管理 (Lightweight Store)
 * 使用 Vue 3 的 reactive API 实现简单的状态分发
 */

const { reactive } = Vue;

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
