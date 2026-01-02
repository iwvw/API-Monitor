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
    this.paasCurrentPlatform = platform;
    this.paasCurrentTab = platform; // 确保同步切换子标签页


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
      } else if (platform === 'fly') {
        // 先加载缓存，让 UI 立即有数据显示
        if (store.flyAccounts.length === 0) {
          this.loadFromFlyCache();
        }
        // 后台加载最新数据
        this.loadFlyData();
      }
    });
  },

  /**
   * 获取平台显示名称
   */
  getPaasPlatformName(platform) {
    const names = {
      zeabur: 'Zeabur',
      koyeb: 'Koyeb',
      railway: 'Railway',
      render: 'Render',
      fly: 'Fly.io',
    };
    return names[platform] || platform;
  },

  /**
   * 获取平台图标
   */
  getPaasPlatformIcon(platform) {
    const icons = {
      zeabur: 'fa-rocket',
      koyeb: 'fa-cube',
      railway: 'fa-train',
      render: 'fa-server',
      fly: 'fa-plane',
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
        cost: this.totalCost,
      };
    } else if (this.paasCurrentPlatform === 'koyeb') {
      return {
        accounts: this.koyebAccounts.length,
        projects: this.koyebTotalProjects,
        services: this.koyebTotalServices,
        running: this.koyebRunningServices,
        cost: this.koyebTotalBalance,
      };
    } else if (this.paasCurrentPlatform === 'fly') {
      const accounts = this.flyAccounts || [];
      return {
        accounts: accounts.length,
        projects: accounts.reduce((acc, curr) => acc + (curr.projects?.length || 0), 0),
        services: accounts.reduce((acc, curr) => acc + (curr.projects?.length || 0), 0),
        running: accounts.reduce(
          (acc, curr) =>
            acc +
            (curr.projects?.filter(p => p.status === 'deployed' || p.status === 'running')
              ?.length || 0),
          0
        ),
        cost: 0,
      };
    }
    return { accounts: 0, projects: 0, services: 0, running: 0, cost: 0 };
  },

  /**
   * 保存 PaaS 模块刷新间隔配置
   */
  async saveZeaburSettings() {
    try {
      // 同步秒到毫秒
      store.zeaburRefreshInterval = store.zeaburRefreshIntervalSec * 1000;
      store.koyebRefreshInterval = store.koyebRefreshIntervalSec * 1000;
      store.flyRefreshInterval = store.flyRefreshIntervalSec * 1000;

      // 保存到服务器
      const settings = {
        zeaburRefreshInterval: store.zeaburRefreshInterval,
        koyebRefreshInterval: store.koyebRefreshInterval,
        flyRefreshInterval: store.flyRefreshInterval,
      };

      const response = await fetch('/api/settings', {
        method: 'POST',
        headers: store.getAuthHeaders(),
        body: JSON.stringify(settings),
      });

      if (response.ok) {
        toast.success('模块配置已保存');
        // 如果正在刷新 Zeabur，则应用新间隔
        if (this.mainActiveTab === 'paas' && !store.dataRefreshPaused) {
          if (this.paasCurrentPlatform === 'zeabur') {
            this.startAutoRefresh?.();
          } else if (this.paasCurrentPlatform === 'koyeb') {
            this.startKoyebAutoRefresh?.();
          } else if (this.paasCurrentPlatform === 'fly') {
            this.startFlyAutoRefresh?.();
          }
        }
      } else {
        toast.error('保存失败');
      }
    } catch (error) {
      console.error('保存 PaaS 设置失败:', error);
      toast.error('保存失败: ' + error.message);
    }
  },
};
