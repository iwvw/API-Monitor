/**
 * Dashboard Module - 系统状态概览
 * 优化版：支持缓存预加载、并行请求、后台静默刷新
 */
import { store } from '../store.js';

// 缓存 key
const CACHE_KEY = 'dashboard_stats_cache';
const CACHE_EXPIRY = 5 * 60 * 1000; // 5 分钟缓存有效期

/**
 * 从 localStorage 加载缓存
 */
function loadFromCache() {
    try {
        const cached = localStorage.getItem(CACHE_KEY);
        if (cached) {
            const { data, timestamp } = JSON.parse(cached);
            // 缓存有效期内直接使用
            if (Date.now() - timestamp < CACHE_EXPIRY) {
                return data;
            }
        }
    } catch (e) {
        console.warn('[Dashboard] Cache load failed:', e);
    }
    return null;
}

/**
 * 保存到 localStorage 缓存
 */
function saveToCache(data) {
    try {
        localStorage.setItem(CACHE_KEY, JSON.stringify({
            data,
            timestamp: Date.now()
        }));
    } catch (e) {
        console.warn('[Dashboard] Cache save failed:', e);
    }
}

export const dashboardMethods = {
    /**
     * 初始化仪表盘数据
     * 优化：先从缓存加载实现瞬时显示，再后台刷新
     */
    async initDashboard() {
        console.log('[Dashboard] Initializing...');

        // 1. 优先从缓存加载（实现瞬时展示）
        const cached = loadFromCache();
        if (cached) {
            console.log('[Dashboard] Loaded from cache');
            Object.assign(store.dashboardStats, cached);
            store.dashboardLastUpdate = '缓存';

            // 后台静默刷新（不显示 loading 状态）
            this.refreshDashboardDataSilent();
        } else {
            // 无缓存时也直接渲染页面（使用默认初始值），后台异步加载数据
            // 不使用 await，让页面立即展示结构
            this.refreshDashboardData();
        }

        // 音乐收藏异步加载（不阻塞仪表盘）
        if (this.musicAutoLoadFavorites) {
            setTimeout(() => this.musicAutoLoadFavorites(), 100);
        }
    },

    /**
     * 刷新仪表盘所有数据（显示 loading 状态）
     */
    async refreshDashboardData() {
        if (store.dashboardLoading) return;
        store.dashboardLoading = true;

        try {
            await this._fetchAllData();
        } catch (error) {
            console.error('[Dashboard] Refresh error:', error);
        } finally {
            store.dashboardLoading = false;
            store.dashboardLastUpdate = new Date().toLocaleTimeString();
        }
    },

    /**
     * 静默刷新（不显示 loading 状态，用于后台更新）
     */
    async refreshDashboardDataSilent() {
        try {
            await this._fetchAllData();
            store.dashboardLastUpdate = new Date().toLocaleTimeString();
        } catch (error) {
            console.error('[Dashboard] Silent refresh error:', error);
        }
    },

    /**
     * 内部方法：并行获取所有数据
     */
    async _fetchAllData() {
        // 使用 Promise.allSettled 确保部分失败不影响整体
        // 所有请求完全并行，不串行等待
        await Promise.allSettled([
            this.fetchServerSummary(),
            this.fetchApiSummary(),
            this.fetchPaaSSummary(),
            this.fetchDnsSummary(),
            this.loadTotpAccounts ? this.loadTotpAccounts() : Promise.resolve()
        ]);

        // 保存到缓存
        saveToCache({
            servers: store.dashboardStats.servers,
            antigravity: store.dashboardStats.antigravity,
            geminiCli: store.dashboardStats.geminiCli,
            paas: store.dashboardStats.paas,
            dns: store.dashboardStats.dns
        });
    },

    /**
     * 获取主机状态摘要
     */
    async fetchServerSummary() {
        try {
            const response = await fetch('/api/server/accounts', { headers: store.getAuthHeaders() });
            const data = await response.json();
            if (data.success) {
                const servers = data.data || [];
                store.dashboardStats.servers = {
                    total: servers.length,
                    online: servers.filter(s => s.status === 'online').length,
                    offline: servers.filter(s => s.status === 'offline').length,
                    error: servers.filter(s => s.status === 'error').length
                };
            }
        } catch (e) {
            console.error('[Dashboard] Fetch server summary failed:', e);
        }
    },

    /**
     * 获取 API 网关摘要 (Antigravity & Gemini CLI)
     * 优化：两个请求并行执行
     */
    async fetchApiSummary() {
        const updateAntigravity = async () => {
            try {
                const res = await fetch('/api/antigravity/stats', { headers: store.getAuthHeaders() });
                if (res.ok) {
                    const data = await res.json();
                    store.dashboardStats.antigravity = data.data || data;
                }
            } catch (e) {
                console.error('[Dashboard] Antigravity stats failed:', e);
            }
        };

        const updateGemini = async () => {
            try {
                const res = await fetch('/api/gemini-cli/stats', { headers: store.getAuthHeaders() });
                if (res.ok) {
                    const data = await res.json();
                    store.dashboardStats.geminiCli = data.data || data;
                }
            } catch (e) {
                console.error('[Dashboard] Gemini stats failed:', e);
            }
        };

        // Fire both, don't wait for all to finish before updating individual stats
        // But await the group to know when API section is fully done (for loading state)
        await Promise.allSettled([updateAntigravity(), updateGemini()]);
    },

    /**
     * 获取 PaaS 摘要 (Zeabur, Koyeb, Fly.io)
     * 优化：三个平台的请求完全并行
     */
    async fetchPaaSSummary() {
        const updateZeabur = async () => {
            try {
                const res = await fetch('/api/zeabur/projects', { headers: store.getAuthHeaders() });
                if (res.ok) {
                    const data = await res.json();
                    let appCount = 0;
                    let runningCount = 0;
                    if (Array.isArray(data)) {
                        data.forEach(acc => {
                            if (acc.projects) {
                                acc.projects.forEach(p => {
                                    if (p.services) {
                                        appCount += p.services.length;
                                        runningCount += p.services.filter(s => s.status === 'RUNNING').length;
                                    }
                                });
                            }
                        });
                    }
                    store.dashboardStats.paas.zeabur = { total: appCount, running: runningCount };
                }
            } catch (e) {
                console.error('[Dashboard] Zeabur stats failed:', e);
            }
        };

        const updateKoyeb = async () => {
            try {
                const res = await fetch('/api/koyeb/data', { headers: store.getAuthHeaders() });
                if (res.ok) {
                    const data = await res.json();
                    let appCount = 0;
                    let runningCount = 0;
                    if (data.success && data.accounts) {
                        data.accounts.forEach(acc => {
                            if (acc.projects) {
                                acc.projects.forEach(p => {
                                    if (p.services) {
                                        p.services.forEach(s => {
                                            appCount++;
                                            if (s.status === 'HEALTHY' || s.status === 'RUNNING') {
                                                runningCount++;
                                            }
                                        });
                                    }
                                });
                            }
                        });
                    }
                    store.dashboardStats.paas.koyeb = { total: appCount, running: runningCount };
                }
            } catch (e) {
                console.error('[Dashboard] Koyeb stats failed:', e);
            }
        };

        const updateFly = async () => {
            try {
                const res = await fetch('/api/fly/proxy/apps', { headers: store.getAuthHeaders() });
                if (res.ok) {
                    const data = await res.json();
                    let appCount = 0;
                    let runningCount = 0;
                    if (data.success && data.data) {
                        data.data.forEach(acc => {
                            if (acc.apps) {
                                acc.apps.forEach(app => {
                                    appCount++;
                                    if (app.status === 'deployed' || app.status === 'running') {
                                        runningCount++;
                                    }
                                });
                            }
                        });
                    }
                    store.dashboardStats.paas.fly = { total: appCount, running: runningCount };
                }
            } catch (e) {
                console.error('[Dashboard] Fly.io stats failed:', e);
            }
        };

        // Parallel execution with independent cache/store updates
        await Promise.allSettled([updateZeabur(), updateKoyeb(), updateFly()]);
    },

    /**
     * 获取 DNS 摘要
     */
    async fetchDnsSummary() {
        try {
            const res = await fetch('/api/cf-dns/zones', { headers: store.getAuthHeaders() });
            if (res.ok) {
                const data = await res.json();
                if (data.success && Array.isArray(data.data)) {
                    store.dashboardStats.dns.zones = data.data.length;
                } else if (typeof data.zones === 'number') {
                    store.dashboardStats.dns.zones = data.zones;
                }
            }
        } catch (e) {
            console.error('[Dashboard] Fetch DNS summary failed:', e);
        }
    }
};

// 在 store 中初始化相关状态
Object.assign(store, {
    dashboardLoading: false,
    dashboardLastUpdate: '',
    dashboardStats: {
        servers: { total: 0, online: 0, offline: 0, error: 0 },
        antigravity: { total_calls: 0, success_calls: 0, fail_calls: 0 },
        geminiCli: { total_calls: 0, success_calls: 0, fail_calls: 0 },
        paas: {
            zeabur: { total: 0, running: 0 },
            koyeb: { total: 0, running: 0 },
            fly: { total: 0, running: 0 }
        },
        dns: { zones: 0 }
    }
});
