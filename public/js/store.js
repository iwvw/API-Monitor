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
    mainActiveTab: 'server',
    moduleVisibility: {
        server: true,
        zeabur: true,
        dns: true,
        openai: true,
        antigravity: true
    },
    moduleOrder: ['server', 'zeabur', 'dns', 'openai', 'antigravity'],

    // 全局设置
    opacity: 39,
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
