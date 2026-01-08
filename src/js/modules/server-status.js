/**
 * 服务器状态模块
 * 显示当前 SSH 会话服务器的实时状态信息
 */

import { toast } from './toast.js';

/**
 * 服务器状态方法集合
 */
export const serverStatusMethods = {
    serverStatusTimer: null, // 自动刷新定时器
    serverStatusInterval: 5000, // 默认刷新间隔

    /**
     * 切换服务器状态侧栏
     */
    toggleServerStatusSidebar() {
        this.showServerStatusSidebar = !this.showServerStatusSidebar;

        if (this.showServerStatusSidebar) {
            this.startServerStatusAutoRefresh();
        } else {
            this.stopServerStatusAutoRefresh();
        }
    },

    /**
     * 启动自动刷新
     */
    startServerStatusAutoRefresh() {
        this.stopServerStatusAutoRefresh();
        this.loadServerStatus();

        // 使用递归 setTimeout 以支持动态调整间隔
        const runTimer = () => {
            this.serverStatusTimer = setTimeout(async () => {
                if (this.showServerStatusSidebar && this.currentSSHSession) {
                    await this.loadServerStatus();
                    runTimer();
                } else {
                    this.stopServerStatusAutoRefresh();
                }
            }, this.serverStatusInterval);
        };
        runTimer();
    },

    /**
     * 停止自动刷新
     */
    stopServerStatusAutoRefresh() {
        if (this.serverStatusTimer) {
            clearTimeout(this.serverStatusTimer);
            this.serverStatusTimer = null;
        }
    },

    /**
     * 加载服务器状态
     */
    async loadServerStatus() {
        const session = this.currentSSHSession;
        if (!session || !session.server) {
            this.serverStatusError = '请先连接服务器';
            return;
        }

        this.serverStatusLoading = true;
        this.serverStatusError = '';

        try {
            const response = await fetch('/api/server/info', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ serverId: session.server.id }),
            });
            const data = await response.json();

            if (data.success) {
                this.serverStatusData = data;
                this.serverStatusInterval = data.is_agent ? 1500 : 1500;
            } else {
                this.serverStatusError = data.error || '获取状态失败';
            }
        } catch (error) {
            this.serverStatusError = '请求失败: ' + error.message;
        } finally {
            this.serverStatusLoading = false;
        }
    },

    /**
     * 刷新服务器状态
     */
    refreshServerStatus() {
        this.loadServerStatus();
    },

    /**
     * 获取 CPU 使用率颜色
     */
    getCpuColor(usage) {
        if (!usage) return 'var(--text-tertiary)';
        const value = parseFloat(usage);
        if (value >= 90) return '#ef4444';
        if (value >= 70) return '#f59e0b';
        return '#22c55e';
    },

    /**
     * 获取内存使用率颜色
     */
    getMemoryColor(percent) {
        if (!percent) return 'var(--text-tertiary)';
        const value = parseFloat(percent);
        if (value >= 90) return '#ef4444';
        if (value >= 70) return '#f59e0b';
        return '#22c55e';
    },

    /**
     * 获取磁盘使用率颜色
     */
    getDiskColor(usage) {
        if (!usage) return 'var(--text-tertiary)';
        const value = parseFloat(usage);
        if (value >= 90) return '#ef4444';
        if (value >= 80) return '#f59e0b';
        return '#22c55e';
    },

    /**
     * 格式化运行时间
     */
    formatUptime(seconds) {
        if (!seconds) return '-';
        const days = Math.floor(seconds / 86400);
        const hours = Math.floor((seconds % 86400) / 3600);
        const mins = Math.floor((seconds % 3600) / 60);

        if (days > 0) return `${days}天 ${hours}时`;
        if (hours > 0) return `${hours}时 ${mins}分`;
        return `${mins}分`;
    },

    /**
     * 获取容器状态颜色
     */
    getContainerStatusColor(state) {
        if (!state) return 'var(--text-tertiary)';
        const s = state.toLowerCase();
        if (s === 'running') return '#22c55e';
        if (s === 'paused') return '#f59e0b';
        if (s === 'exited' || s === 'dead') return '#ef4444';
        return 'var(--text-secondary)';
    },
};
