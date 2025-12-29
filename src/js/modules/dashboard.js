/**
 * Dashboard Module - 系统状态概览
 */
import { store } from '../store.js';

export const dashboardMethods = {
    /**
     * 初始化仪表盘数据
     */
    async initDashboard() {
        console.log('[Dashboard] Initializing...');
        this.refreshDashboardData();
        if (this.musicAutoLoadFavorites) {
            this.musicAutoLoadFavorites();
        }
    },

    /**
     * 刷新仪表盘所有数据
     */
    async refreshDashboardData() {
        if (store.dashboardLoading) return;
        store.dashboardLoading = true;

        try {
            // 使用 Promise.allSettled 确保部分失败不影响整体
            await Promise.allSettled([
                this.fetchServerSummary(),
                this.fetchApiSummary(),
                this.fetchPaaSSummary(),
                this.fetchDnsSummary(),
                // 确保加载 TOTP 数据以显示计数
                this.loadTotpAccounts ? this.loadTotpAccounts() : Promise.resolve()
            ]);
        } catch (error) {
            console.error('[Dashboard] Refresh error:', error);
        } finally {
            store.dashboardLoading = false;
            store.dashboardLastUpdate = new Date().toLocaleTimeString();
        }
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
     */
    async fetchApiSummary() {
        try {
            // Antigravity Stats
            const agRes = await fetch('/api/antigravity/stats', { headers: store.getAuthHeaders() });
            if (agRes.ok) {
                const agData = await agRes.json();
                store.dashboardStats.antigravity = agData.data || agData;
            }

            // Gemini CLI Stats
            const gRes = await fetch('/api/gemini-cli/stats', { headers: store.getAuthHeaders() });
            if (gRes.ok) {
                const gData = await gRes.json();
                store.dashboardStats.geminiCli = gData.data || gData;
            }
        } catch (e) {
            console.error('[Dashboard] Fetch API summary failed:', e);
        }
    },

    /**
     * 获取 PaaS 摘要 (Zeabur, Koyeb, Fly.io)
     */
    async fetchPaaSSummary() {
        try {
            // Zeabur
            const zRes = await fetch('/api/zeabur/projects', { headers: store.getAuthHeaders() });
            if (zRes.ok) {
                const zData = await zRes.json();
                let appCount = 0;
                let runningCount = 0;
                if (Array.isArray(zData)) {
                    zData.forEach(acc => {
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

            // Koyeb
            const kRes = await fetch('/api/koyeb/data', { headers: store.getAuthHeaders() });
            if (kRes.ok) {
                const kData = await kRes.json();
                let appCount = 0;
                let runningCount = 0;
                if (kData.success && kData.accounts) {
                    kData.accounts.forEach(acc => {
                        if (acc.projects) {
                            acc.projects.forEach(p => {
                                if (p.services) {
                                    p.services.forEach(s => {
                                        appCount++;
                                        // 状态可能是 HEALTHY, RUNNING, STARTING 等
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

            // Fly.io
            const fRes = await fetch('/api/fly/proxy/apps', { headers: store.getAuthHeaders() });
            if (fRes.ok) {
                const fData = await fRes.json();
                let appCount = 0;
                let runningCount = 0;
                if (fData.success && fData.data) {
                    fData.data.forEach(acc => {
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
            console.error('[Dashboard] Fetch PaaS summary failed:', e);
        }
    },

    /**
     * 获取 DNS 摘要
     */
    async fetchDnsSummary() {
        try {
            const res = await fetch('/api/cf-dns/zones', { headers: store.getAuthHeaders() });
            if (res.ok) {
                const data = await res.json();
                console.log('[Dashboard] DNS zones fetched:', data);
                if (data.success && Array.isArray(data.data)) {
                    store.dashboardStats.dns.zones = data.data.length;
                } else if (typeof data.zones === 'number') {
                    // 兼容直接返回数量的情况
                    store.dashboardStats.dns.zones = data.zones;
                }
            } else {
                console.error('[Dashboard] Fetch DNS summary failed with status:', res.status);
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
