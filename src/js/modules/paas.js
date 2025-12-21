/**
 * PaaS 模块 - 统一云平台监控入口
 * 管理多个 PaaS 平台（Zeabur、Koyeb 等）的监控和操作
 */

import { store } from '../store.js';
import { toast } from './toast.js';

export const paasMethods = {

    // ============ PaaS 平台切换 ============

    /**
     * 切换 PaaS 子平台
     */
    switchPaasPlatform(platform) {
        if (this.paasCurrentPlatform === platform) return;

        this.paasCurrentPlatform = platform;

        // 切换平台时加载对应数据
        this.$nextTick(() => {
            if (platform === 'zeabur') {
                if (this.accounts.length === 0) {
                    this.loadFromZeaburCache();
                }
            } else if (platform === 'koyeb') {
                if (this.koyebAccounts.length === 0) {
                    this.loadKoyebData();
                }
            }
        });
    },

    /**
     * 获取平台显示名称
     */
    getPaasPlatformName(platform) {
        const names = {
            'zeabur': 'Zeabur',
            'koyeb': 'Koyeb',
            'railway': 'Railway',
            'render': 'Render',
            'fly': 'Fly.io'
        };
        return names[platform] || platform;
    },

    /**
     * 获取平台图标
     */
    getPaasPlatformIcon(platform) {
        const icons = {
            'zeabur': 'fa-rocket',
            'koyeb': 'fa-cube',
            'railway': 'fa-train',
            'render': 'fa-server',
            'fly': 'fa-plane'
        };
        return icons[platform] || 'fa-cloud';
    },

    /**
     * 获取平台颜色类
     */
    getPaasPlatformColorClass(platform) {
        return `paas-${platform}`;
    },

    // ============ 统计计算 ============

    /**
     * 获取当前平台的统计数据
     */
    getCurrentPlatformStats() {
        if (this.paasCurrentPlatform === 'zeabur') {
            return {
                accounts: this.accounts.length,
                projects: this.totalProjects,
                services: this.totalServices,
                running: this.runningServices,
                cost: this.totalCost
            };
        } else if (this.paasCurrentPlatform === 'koyeb') {
            return {
                accounts: this.koyebAccounts.length,
                projects: this.koyebTotalProjects,
                services: this.koyebTotalServices,
                running: this.koyebRunningServices,
                cost: this.koyebTotalBalance
            };
        }
        return { accounts: 0, projects: 0, services: 0, running: 0, cost: 0 };
    }
};
