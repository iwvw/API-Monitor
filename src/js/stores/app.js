/**
 * 应用全局状态与 UI 存储
 */

import { defineStore } from 'pinia';

export const MODULE_CONFIG = {
    dashboard: { name: '仪表盘', shortName: 'Dash', icon: 'fa-tachometer-alt', description: '系统状态与数据概览' },
    openai: { name: 'OpenAI', shortName: 'OAI', icon: 'fa-robot', description: 'OpenAI 兼容 API 管理与聊天' },
    antigravity: { name: 'AntiG', shortName: 'AntiG', icon: 'fa-rocket', description: 'Antigravity API 代理服务' },
    'gemini-cli': { name: 'GCLI', shortName: 'GCLI', icon: 'fa-terminal', description: 'Gemini CLI API 代理服务' },
    paas: { name: 'PaaS', shortName: 'PaaS', icon: 'fa-cloud', description: 'Zeabur / Koyeb / Fly.io 平台监控' },
    dns: { name: 'DNS', shortName: 'CF', icon: 'fa-globe', description: 'Cloudflare DNS / Workers / Pages 管理' },
    'self-h': { name: 'SelfH', shortName: 'Self-H', icon: 'fa-server', description: '自建服务管理' },
    server: { name: 'Hosts', shortName: 'Hosts', icon: 'fa-hdd', description: '终端与服务器监控' },
    totp: { name: '2FA', shortName: '2FA', icon: 'fa-shield-alt', description: 'TOTP 验证器' },
    music: { name: 'Music', shortName: 'Music', icon: 'fa-music', description: '网易云音乐播放器' },
    uptime: { name: 'Uptime', shortName: 'Uptime', icon: 'fa-heartbeat', description: '站点与服务可用性监测' },
};

export const MODULE_GROUPS = [
    { id: 'overview', name: '仪表盘', icon: 'fa-tachometer-alt', modules: ['dashboard'] },
    { id: 'api-gateway', name: 'API 网关', icon: 'fa-bolt', modules: ['openai', 'antigravity', 'gemini-cli'] },
    { id: 'infrastructure', name: '基础设施', icon: 'fa-cubes', modules: ['paas', 'dns', 'server', 'uptime'] },
    { id: 'toolbox', name: '工具箱', icon: 'fa-toolbox', modules: ['self-h', 'totp', 'music'] },
];

export const useAppStore = defineStore('app', {
    state: () => ({
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
            music: false,
            uptime: true,
        },
        moduleOrder: [
            'dashboard', 'openai', 'antigravity', 'gemini-cli', 'paas', 'dns', 'self-h', 'server', 'totp', 'music', 'uptime'
        ],
        // 界面设置
        opacity: 100,
        serverIpDisplayMode: 'normal', // 'normal', 'masked', 'hidden'
        vibrationEnabled: true,
        navLayout: 'bottom', // 'sidebar', 'top', 'bottom'
        agentDownloadUrl: '',
        showSettingsModal: false,
        singlePageMode: false,
        navGroupExpanded: null,

        // 全局当前时间
        currentTime: Date.now(),
    }),

    getters: {
        visibleModulesCount: (state) => Object.values(state.moduleVisibility).filter(v => v).length,
        moduleGroups: () => MODULE_GROUPS,
        moduleConfig: () => MODULE_CONFIG,
    },

    actions: {
        setMainActiveTab(tab) {
            this.mainActiveTab = tab;
        },
        toggleSettingsModal() {
            this.showSettingsModal = !this.showSettingsModal;
        },
        updateCurrentTime() {
            this.currentTime = Date.now();
        },
        getModuleName(moduleId, short = false) {
            const config = MODULE_CONFIG[moduleId];
            if (!config) return moduleId;
            return short ? config.shortName : config.name;
        },
        getModuleIcon(moduleId) {
            const config = MODULE_CONFIG[moduleId];
            return config ? config.icon : 'fa-cube';
        }
    }
});
