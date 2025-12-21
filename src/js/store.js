/**
 * API Monitor - 全局状态管理 (Lightweight Store)
 * 使用 Vue 3 的 reactive API 实现简单的状态分发
 */

import { reactive } from 'vue';

// 确保 reactive 始终从全局 Vue 对象获取，增加鲁棒性
// 纯 Vue 3 (ESM) 不需要 window.Vue 检测，直接使用导入的 reactive
// const reactive = (obj) => { ... } - REMOVED


export const store = reactive({
    // 认证与基础状态
    isAuthenticated: false,
    isCheckingAuth: true,
    showLoginModal: false,
    showSetPasswordModal: false,
    showWelcomeScreen: false,
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
        paas: true,
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
    moduleOrder: ['openai', 'antigravity', 'gemini-cli', 'paas', 'dns', 'server'],

    // 界面设置
    opacity: 100,
    serverIpDisplayMode: 'normal', // 'normal', 'masked', 'hidden'
    navLayout: 'top', // 'sidebar' (左侧) or 'top' (顶部)
    showSettingsModal: false, // 设置面板显示状态
    mobileSettingsNavExpanded: false, // 移动端设置导航展开状态

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
    expandedServers: new Set(),
    monitorConfig: {
        interval: 60,
        timeout: 10,
        logRetentionDays: 7
    },
    serverCredentials: [],



    // PaaS 平台监控 (Zeabur + Koyeb + ...)
    paasCurrentPlatform: 'zeabur', // 'zeabur', 'koyeb', etc.
    paasCurrentTab: 'zeabur', // 'zeabur', 'koyeb', 'accounts', 'settings'

    // Zeabur 子模块
    accounts: [],
    managedAccounts: [],
    zeaburCurrentTab: 'monitor',
    loading: false, // Zeabur loading
    zeaburRefreshInterval: 30000,
    refreshCountdown: 30,
    refreshProgress: 100,
    dataRefreshPaused: false,
    projectCosts: {},

    // Koyeb 子模块
    koyebAccounts: [],
    koyebManagedAccounts: [],
    koyebLoading: false,
    koyebRefreshing: false,
    koyebLastUpdate: '',
    koyebRefreshInterval: 30000,
    koyebRefreshCountdown: 30,
    koyebRefreshProgress: 100,
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

    // DNS
    dnsAccounts: [],
    dnsZones: [],
    dnsCurrentTab: 'dns',
    computeCurrentTab: 'workers',
    dnsSelectedAccountId: null,
    dnsSelectedZoneId: null,
    dnsLoadingZones: false,
    dnsLoadingRecords: false,
    dnsRecords: [],
    dnsSelectedRecords: [],
    dnsSearchText: '',
    isEditingWorker: false, // 是否正在编辑现有 Worker

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
    antigravityLogFilterAccount: '',
    antigravityLogFilterModel: '',
    antigravitySettings: [],
    agRefreshingAll: false,
    agLogDetailShowRaw: false, // 是否显示原始 JSON
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
    geminiCliLogFilterAccount: '',
    geminiCliLogFilterModel: '',

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
        streamActive: false // 是否正在接收实时流
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
