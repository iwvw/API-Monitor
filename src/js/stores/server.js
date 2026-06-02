/**
 * 主机管理状态存储
 */

import { defineStore } from 'pinia';
import { toast } from '../modules/toast.js';

export const useServerStore = defineStore('server', {
    state: () => ({
        serverList: [],
        serverLoading: false,
        serverCurrentTab: 'list', // 'list', 'management', 'terminal', 'metrics'
        serverSearchText: '',
        serverStatusFilter: 'all',
        expandedServers: [],

        // 监控设置
        monitorConfig: {
            interval: 60,
            timeout: 10,
            logRetentionDays: 7,
            metrics_retention_days: 30,
        },

        // 凭据管理
        serverCredentials: [],
    }),

    getters: {
        filteredServerList: (state) => {
            let list = state.serverList;

            // 状态筛选
            if (state.serverStatusFilter !== 'all') {
                list = list.filter(item => item.status === state.serverStatusFilter);
            }

            // 搜索文本筛选
            if (state.serverSearchText.trim()) {
                const search = state.serverSearchText.toLowerCase();
                list = list.filter(item => {
                    const nameMatch = item.name && item.name.toLowerCase().includes(search);
                    const hostMatch = item.host && item.host.toLowerCase().includes(search);
                    const tagMatch = item.tags && item.tags.some(tag => tag.toLowerCase().includes(search));
                    return nameMatch || hostMatch || tagMatch;
                });
            }

            return list;
        },

        isServerExpanded: (state) => (serverId) => state.expandedServers.includes(serverId),
    },

    actions: {
        async loadServerList() {
            if (this.serverLoading) return;
            this.serverLoading = true;
            try {
                const response = await fetch('/api/server/accounts');
                const data = await response.json();

                if (data.success) {
                    const existingServersMap = new Map(this.serverList.map(s => [s.id, s]));

                    this.serverList = data.data.map(server => {
                        const existing = existingServersMap.get(server.id);
                        return {
                            ...server,
                            info: existing && existing.info && !server.info ? existing.info : server.info,
                            metricsCache: existing && existing.metricsCache ? existing.metricsCache : null,
                            gpuChartVisible: existing && existing.gpuChartVisible ? existing.gpuChartVisible : false,
                            gpuLoading: existing && existing.gpuLoading ? existing.gpuLoading : false,
                            netChartVisible: existing && existing.netChartVisible ? existing.netChartVisible : false,
                            netLoading: existing && existing.netLoading ? existing.netLoading : false,
                            error: existing && existing.error ? existing.error : null,
                            loading: existing && existing.loading ? existing.loading : false,
                        };
                    });
                }
            } catch (error) {
                console.error('加载主机列表失败:', error);
            } finally {
                this.serverLoading = false;
            }
        },

        toggleServer(serverId) {
            const index = this.expandedServers.indexOf(serverId);
            if (index !== -1) {
                this.expandedServers.splice(index, 1);
            } else {
                const server = this.serverList.find(s => s.id === serverId);
                if (server && server.status === 'online' && server.info) {
                    this.expandedServers.push(serverId);
                }
            }
        },

        async loadCredentials() {
            try {
                const response = await fetch('/api/server/credentials');
                const data = await response.json();
                if (data.success) {
                    this.serverCredentials = data.data;
                }
            } catch (error) {
                console.error('加载凭据失败:', error);
            }
        }
    }
});
